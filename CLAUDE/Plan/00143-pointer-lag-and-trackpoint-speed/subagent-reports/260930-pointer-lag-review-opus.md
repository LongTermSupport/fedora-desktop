# Plan 00143: Ctrl-C failure and the 11:33 lag episode

This report covers two things. Part 1 explains why Ctrl-C did not stop `triage.bash --capture 600`, and describes the fix. Part 2 analyses the lag episode of 2026-09-30, about 11:33–11:42 BST (10:33–10:42 UTC).

Everything below was measured on the host unless it is marked **guess** or **inference**. Paths are relative to the repo root.

---

## Part 1: Ctrl-C did not stop the capture

### Root cause (reproduced, then fixed)

The fault was reproduced against the unmodified scripts. The test sent a real `^C` byte through a pty with `expect`, so the terminal's line discipline delivered SIGINT to the foreground process group, exactly as a keyboard Ctrl-C does. Four facts combine to cause it:

1. **Background jobs ignore SIGINT.** A non-interactive bash starts every `&` job with SIGINT and SIGQUIT set to ignore. A job started this way showed `SigIgn: 0x6`. In the capture, two jobs were started like this: `( timeout … sudo -n libinput debug-events … | awk ) >events &` and `watch_dgpu … &`. Neither can be stopped by Ctrl-C.
2. **`timeout` and `sudo` leave the terminal's process group.** GNU `timeout` (coreutils 9.10) moves itself into its own process group, and its measured pgid was its own pid. `sudo` then runs libinput on its own pty and session; the second `sudo` process showed `Ss+` with a new session. So a terminal SIGINT reaches none of `timeout`, `sudo` or `libinput`.
3. **`probe-pointer.bash` had no handler.** On Ctrl-C it died and orphaned those jobs. They were reparented to the user's systemd subreaper, which is the "PID 1870" in the incident.
4. **`triage.bash` hung inside its own INT handler.** The library handler `_plan_finalize_log` `wait`s for the run-log `tee`. The orphans had inherited the log fifo as stderr, so `tee` never saw EOF, and `triage.bash` blocked until `timeout N` expired. With `--capture 600` that is up to 10 minutes, which looks exactly like "Ctrl-C did nothing".

Evidence from the unmodified scripts, about 6 s after `^C`:

- `triage.bash` and `tee` were still alive.
- Two `probe-pointer.bash` subshells had ppid equal to the user's systemd.
- `timeout 15 sudo -n libinput …` was running in a process group different from `triage.bash`'s.
- The finalize lines appeared only after the 15 s timeout.

The 11:41:43 run directory is consistent with this:

- `triage.log` has no finalize lines.
- The report's per-second table stops at 11:42:20, the moment of the Ctrl-C.
- `libinput-pointer-events.txt` kept growing until about 11:49.
- `dgpu-transitions.txt` kept growing until 11:44.

A related hazard: if an agent's non-interactive shell starts the run as a background job, SIGINT is ignored for the whole process tree from the start. Such a run can only be stopped with TERM. The fix covers that path.

### The fix (plan folder only)

**`probe-pointer.bash`, capture mode:**

- Recorders now start as individually tracked jobs. libinput writes to a named pipe read by the timestamping `awk`, so `timeout`'s own PID is known.
- `stop_background` sends TERM to the producers: libinput's `timeout`, the dGPU watcher and the new trace reader. It then reaps every job. Readers end on EOF, so no buffered event lines are lost.
- It also removes the kernel tracing instance and the named pipes. TERM to `timeout` takes the same path as normal expiry: `timeout` relays the signal to `sudo`, which passes it on to `libinput`.
- `arm_teardown` traps EXIT, INT, TERM and HUP. On a signal it:
  - writes `**CAPTURE INTERRUPTED** (SIG…)` into the report;
  - tears down, writing its messages to the stderr it saved before any redirect;
  - re-raises the signal, so the parent sees the leg die of it.
- The script owns its traps because it opens no run log, so the library's trap and `plan_on_cleanup` are not armed in its process. The comment explains why this is not the R4 case.

**`triage.bash`:**

- The capture leg now runs as a background job whose PID is recorded, and it is still recorded through `plan_gather_leg … wait "$CAPTURE_PID"`.
- `stop_capture` is registered with `plan_on_cleanup`. The library runs it before draining the log, so a Ctrl-C, or a TERM sent only to the `triage.bash` PID, stops the leg, and the leg then tears down its own recorders.

### Verification

`shellcheck -x` is clean on both scripts, and `./scripts/qa-bash.bash` passed. Every test below checked for leftover processes with bracketed `pgrep` patterns and checked `/sys/kernel/tracing/instances/`.

| Test                                                                      | Result                                                                                                                                          |
| ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------- |
| pty `^C` about 4 s into `--capture 60`                                    | exited by SIGINT within about 1 s; the teardown reaped 5 jobs (producers rc=143, readers rc=0); no leftover processes; no tracing instance left |
| pty `^C` during the snapshot leg                                          | exited by SIGINT at once; the capture never started; no leftover processes                                                                      |
| `kill -TERM <triage pid>` on a background (SIGINT-ignored) `--capture 60` | exited rc=143 about 1 s later; `[teardown] capture leg stopped (rc=143)`; no leftover processes                                                 |
| normal `--capture 5` and `--capture 10`                                   | rc=0; libinput ended rc=124 as scheduled; "all legs OK"; the tracing instance was removed                                                       |
| synthetic input to the new join and aggregator                            | a 50-motion second with 3 cursor moves is flagged; the gap and TrackPoint-latency columns compute as intended                                   |

**Not changed:** `_planlib.inc.bash`, which is outside the plan folder. Its `_plan_finalize_log` still waits with no bound on any process that holds the log fifo. A library follow-up is worth considering, for example a bounded drain or reaping the leg's process group.

---

## Part 2: the lag itself

### What today's data contains

The capture started at **11:41:59**, so it overlaps at most the last few seconds of the reported episode. What it holds:

- **Per-second table, 11:42:00–11:42:20 (20 rows before the Ctrl-C).**
  - From 11:42:13 to 11:42:17 the touchpad reported 94–140 times a second while i915 raised 102–455 IRQ/s. No second shows the "moving but not presenting" signature.
  - `throttle+` stayed 0 and ACPI SCI stayed at its usual 22-per-5-s bursts.
- **libinput events, 11:42–11:49.** The orphaned recorder kept running, so this extends past the Ctrl-C.
  - Touchpad: 2,647 motions. Gap histogram: 2,374 under 20 ms (89.7%), 199 at 20–50 ms (7.5%), 10 at 50–100 ms, 35 at 100–300 ms and 28 at 300 ms or more.
  - The F13 no-lag baseline was 91.5% and 6.4%. These two captures are indistinguishable.
  - TrackPoint: 20 motions at a steady 10 ms cadence (`rate=100`).
  - Three touchpad `kernel bug: Touch jump detected and discarded` errors, at 11:43:00, 11:43:09 and 11:43:23.
- **dGPU.**
  - It was `suspended` at the 11:41:43 snapshot.
  - Wake at 11:42:20.419, `active` at 11:42:22.189 (1.77 s), re-suspended at 11:42:42.
  - Wake at 11:43:59.779, `active` at 11:44:01.539 (1.76 s).
  - These match F11. Neither falls inside the reported 11:33–11:42 window, and wakes during that window were not recorded.
- **Snapshot at 11:41:43.**
  - PSI: cpu `some avg300=0.00`. That window runs from 11:36:43 to 11:41:43, the second half of the episode.
  - Memory 0.00, IO avg300 0.03, load 1.67/2.07/2.34.
  - gnome-shell was at 8.8% CPU, the KMS thread at 1.8% and the Mutter input thread at 0.2%.

### Journal, 11:20–11:50

- **Kernel.** No i2c, hid, elan, psmouse, rmi, i915 or drm errors.
  - `nvidia-modeset: Correcting number of heads` appeared ×3 at 11:15. That is before the episode, and no episode was reported then.
  - A `ydotoold virtual device` input node was re-created at 11:26:33 and 11:40:36, when a playbook restarted `ydotool.service`.
  - LXC and podman veth devices came up between 11:29:58 and 11:30:03.
  - Nothing was logged at episode onset.
- **gnome-shell.**
  - **No `event processing lagging behind` warning this boot.** The string is present in libinput 1.31.3's library. The previous boot has one, at 11:01 with 82 ms, which is not near any episode.
  - Touch-jump warnings hit libinput's 5-per-24-h log limit at 08:07 today, so mutter's log is now silent about them. The plan's own capture still sees them.
  - Bursts of keybinding rebuilds: `Overwriting existing binding of keysym 31..39`, together with `Trying to remove non-existent keybinding "abort-recording"` and a `stack_position` assertion. They came at 11:36:29, 11:37:18/35, 11:38:38, 11:39:46, 11:40:05–15 and 11:41:16–19.
  - **Guess:** the speech-to-text extension adding and removing its recording keybinding.
  - "Can't update stage views" ran at 44–215 per minute.
- **Cross-check against the 29 Sep episode, 18:30–18:40 BST.** None of today's candidates appears in that window: no keybinding bursts, no ydotool restart, and no `lagging behind` warning. So none of them is a necessary condition. The only thing near that episode was a terminal probing Vulkan at 18:28:55: an `nvidia-modeset` head correction and an evdi open and close.
- **Thermal.** `package_throttle_count` rose from 95 at 09:28 to 104 at 10:01 and 219 at 11:41. That is 115 package-throttle events between 10:02 and 11:41, at unknown times, with the package at 48 °C when sampled. This is a new, uncorrelated fact. The `throttle+` column will place it if it coincides with an episode.

### New facts found while analysing

- **The eDP panel runs PSR2 with selective fetch** (`i915_edp_psr_status`: `PSR mode: PSR2 enabled`, `PSR2 selective fetch: enabled`). While idle the source status moves between `DEEP_SLEEP` and `CAPTURE`. `FBC disabled: Selective update enabled`.
- **The cursor is a hardware plane.** Injected `ydotool` relative moves produced matching `cursor A` plane updates: 34 moves for 40 injected steps. So the cursor is *not* composited into the primary plane.

### What the evidence supports

- **CPU, memory or IO starvation is refuted** for the second half of this episode (PSI avg300 of 0.00 from 11:36:43 to 11:41:43) and for the 29 Sep episode (F1).
- **A touchpad-only or I2C fault is unlikely.**
  - The owner reports that the TrackPoint lags too, and it is PS/2 on a separate bus.
  - The kernel log is clean.
  - Mutter's libinput never logged that it was falling behind the kernel's event timestamps.
  - **Inference:** the delay is *downstream* of mutter reading input events.
- **"The pointer lags while typing stays fine" suggests the delay is specific to the cursor, not a whole-compositor stall** (**inference**). Keyboard feedback needs a client redraw and a composited frame. The pointer here moves a hardware cursor plane, and on a PSR2 panel a cursor-only change is delivered as a selective update.

### What it does not support, or cannot yet decide

- H5 (a dGPU wake stalls the compositor): there is no record of dGPU state during 11:33–11:41. It is neither supported nor refuted.
- H1 (compositor frame scheduling) against the new H6 below: today's data holds no per-frame cursor information for the episode.
- Whether the owner still saw lag after 11:42 is unknown, so the healthy post-11:42 histogram does not show that the device path was healthy *during* the lag.

### Proposed new hypothesis H6

**H6:** the lag is in the display's panel self-refresh path. PSR2 selective update delays or drops cursor-only updates on the eDP panel, while full-frame updates, such as those typing causes, still show.

- **Confirms:** during an episode, the cursor plane keeps pace with input (cursor moves roughly equal libinput motions, low TrackPoint latency), yet the owner sees lag.
- **Refutes:** during an episode, the cursor plane stops moving while motions arrive. That puts the delay in the compositor (H1/H5).

That PSR2 cursor stutter is a recurring class of upstream Intel reports is **general knowledge, not verified here**. If H6 survives, the A/B test is PSR2 off (for example the kernel argument `i915.enable_psr=1` for PSR1 only, or `0`), applied through a playbook as a Phase 3 experiment and measured with the same capture.

### The next discriminating probe (implemented)

`--capture` now also records a **display-engine trace**. It is a private tracefs instance (`/sys/kernel/tracing/instances/plan00143-<pid>`, boot clock) with these events enabled:

- `irq:irq_handler_entry`, filtered to the touchpad's IRQ and IRQ 12 (the TrackPoint);
- `i915:intel_plane_update_arm`.

Per second it produces `display-trace.txt`, with these columns:

- pointer IRQs from each device;
- cursor-plane moves (changes of on-screen position);
- primary-plane updates;
- the longest gap between cursor moves while input kept arriving;
- the longest delay from a TrackPoint IRQ to the next cursor move.

A new report section, "cursor on the panel", joins this with the libinput motions. It lists every second that had 40 or more motions but fewer than motions/4 cursor moves.

Taken together, these separate the layers:

| Observation during an episode                           | Where the delay is               |
| ------------------------------------------------------- | -------------------------------- |
| IRQs or libinput motions stop                           | device or kernel (H2)            |
| motions arrive, but the cursor plane does not move      | compositor or commit (H1/H5)     |
| the cursor plane moves promptly, but the owner sees lag | after the plane update: PSR (H6) |

The instance is removed on every exit path, and this was verified.

**Recommended run.** The owner starts `./CLAUDE/Plan/00143-pointer-lag-and-trackpoint-speed/triage.bash --capture 7200` in a spare terminal before the lag. When it happens, the owner notes the clock time and presses Ctrl-C, which now stops everything within about a second.

**One human observation is worth recording at the same time.** Is the cursor *delayed*, catching up later, or *low-rate*, jumping? And do a window drag or the Overview animation stay smooth? A smooth drag with a lagging cursor points at the cursor plane or PSR.

### Suggested plan updates (for whoever commits)

The coordinator owns `PLAN.md` and `JOURNAL/`, so neither was edited.

- **New facts:**
  - PSR2 with selective fetch is active on eDP.
  - The cursor is a hardware plane.
  - No libinput `lagging behind` warning was logged in either episode.
  - PSI avg300 was 0 from 11:36:43 to 11:41:43.
  - `package_throttle_count` went from 95 to 219 today, at unlocated times.
  - Touch-jump warnings are rate-limited out of mutter's log after the fifth each day.
- **New hypothesis:** H6, as stated above.
- **Task 1.1:** a sub-item for the Ctrl-C and TERM teardown fix and the display trace. Both were verified on the host.
- **JOURNAL:** a `finding` entry for the episode analysis and an `action` entry for the fix.

Raw material, not committed:

- the run directory `untracked/plan-runs/00143-pointer-lag-and-trackpoint-speed/triage/20260930-114143/`;
- the journal extracts and test transcripts under `untracked/scratch/00143/`.
