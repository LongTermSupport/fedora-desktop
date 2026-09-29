# Plan 00142: pointer lag and trackpoint speed

**Status**: In Progress
**Created**: 2026-09-29
**Owner**: joseph
**Priority**: Medium

## Overview

Two pointer problems on the ThinkPad P14s Gen 5 laptop (GNOME on Wayland, Fedora 44):

1. **Intermittent pointer lag.** Touchpad movement becomes hugely laggy for a
   period, then recovers without intervention. It has recurred within minutes of
   itself.
2. **TrackPoint far too fast.** The pointing stick is unusable at the default
   settings, with sensitivity described as "off the scale".

A live capture taken *during* one lag episode ruled out the common system-level
causes, which are listed under Facts, but it could not see per-event latency
through libinput and mutter. So the lag's cause is still unknown. This plan builds
the triage that captures the missing layer on the next episode, and then fixes
whatever that shows, in IaC. The TrackPoint problem already has a grounded lead
(F9) and is fixed independently of the lag.

## Goals

- A `triage.bash` that one person can run mid-episode to record every layer from
  kernel interrupt to compositor, so an episode is never lost to "it's fine now".
- The lag's layer identified from captured evidence, not inference: device,
  kernel, libinput or compositor.
- A TrackPoint that is usable at GNOME's default speed, delivered by a playbook.

## Non-Goals

- No manual tuning on the host, even as an interim step. Every change goes
  through a playbook.
- No upstreaming of a libinput quirk. That can follow once a value is proven.

## Facts

| #   | Fact                                                                                                                                                                                                                                                         | Source                                          |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------- |
| F1  | During a lag episode, CPU, memory and IO pressure (`/proc/pressure/*`) were all 0.00, load was about 1.6, and nothing was hogging CPU. gnome-shell was at about 12% and its KMS thread at about 3%.                                                          | live capture, JOURNAL 26-09-29                  |
| F2  | The only display is the internal eDP panel at 3072x1920@120. The i915 interrupt rate was about 200/s, consistent with normal page flips. DisplayLink was inactive and no dock was attached.                                                                  | live capture                                    |
| F3  | The NVIDIA dGPU was runtime-suspended during the episode (P8, about 2 W, when woken).                                                                                                                                                                        | live capture                                    |
| F4  | There was no thermal throttling during the episode: the package throttle count did not change, and CPU and package temperatures were about 44 °C.                                                                                                            | live capture                                    |
| F5  | The touchpad (I2C-HID, ELAN, on `i2c_designware.0`) delivered about 170 reports/s during the episode. The I2C controller raised about 5,500 IRQ/s, about 31 per report, which is identical to the since-boot average ratio.                                  | `/proc/interrupts`, sampled live and since boot |
| F6  | The kernel logged no I2C, HID or touchpad errors that day. Only one boot-time `i2c_hid` notice exists (Plan 00044, watch-only).                                                                                                                              | `journalctl -k`                                 |
| F7  | The recurring `Can't update stage views` warnings (from the dash-to-dock and blur extensions) came in bursts around the lag window, but none were logged in the 3 minutes of the live episode.                                                               | journal                                         |
| F8  | The TrackPoint (`TPPS/2 Elan TrackPoint`, PS/2, `LEN0321`) is at pure defaults: kernel `sensitivity=128`, `rate=100`, `resolution=200`; GNOME `pointingstick speed=0.0` with `accel-profile=default`. No `/etc/libinput/` overrides and no local hwdb exist. | sysfs, gsettings, `/etc`                        |
| F9  | The libinput 1.31.3 shipped quirks set `AttrTrackpointMultiplier` for several ThinkPads, including 0.4 for the P14s Gen 1, but have **no entry for the P14s Gen 5**. So libinput uses a multiplier of 1.0 here.                                              | `/usr/share/libinput/50-system-lenovo.quirks`   |
| F10 | The EC's ACPI GPE (0x6E) fires about 2.6/s averaged over the uptime, and many `kacpi_notify` kworkers exist. The rate was 0/s during the live episode.                                                                                                       | `/sys/firmware/acpi/interrupts`                 |

| F11 | The NVIDIA dGPU runtime-suspends and wakes by itself, with no probe touching it. One observed resume took **about 1.6s** (`resuming` to `active`), and it re-suspended about 20s later. It was caught in `resuming` on two unrelated probes. | 100ms sysfs watch, JOURNAL 26-09-29 |
| F12 | gnome-shell itself holds `/dev/nvidia0` and `/dev/nvidia-modeset` (the compositor uses the dGPU as a secondary GPU). So do ptyxis (GTK4 terminal) and Firefox's media-decoder (RDD) processes. | `/proc/*/fd` via `triage.bash` |
| F13 | Baseline with no lag: during 30s of touchpad use, 1,624 of 1,776 motion gaps were under 20ms, 113 were 20–50ms and 26 were 50–300ms. | `triage.bash --capture 30` |
| F14 | The owner reports the TrackPoint is "WAY WAY too fast to use", with sensitivity "off the scale". | owner, in session |

## Hypotheses

| #   | Hypothesis                                                                                                                                               | Confirms                                                                                | Refutes                                                       |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| H1  | The lag is in the compositor, with mutter frames delayed despite low CPU (the extensions' stage-view churn, or frame scheduling).                        | libinput events arrive smoothly while the cursor stutters, and the TrackPoint lags too  | the TrackPoint stays smooth during an episode                 |
| H2  | The lag is in the touchpad path: firmware or libinput report timing (bunched or delayed events).                                                         | libinput event timestamps show gaps or bunching; the TrackPoint is fine                 | libinput timestamps are regular during lag                    |
| H3  | The episodes coincide with a bursty system event such as an ACPI/EC notify burst or a dGPU wake.                                                         | a GPE or dGPU state change is recorded at episode onset                                 | nothing changes at onset                                      |
| H5  | The compositor stalls while the dGPU resumes from runtime suspend (F11, F12). The wake comes from gnome-shell or another holder, and mutter waits on it. | a `resuming` span in the capture overlaps the lag and lasts about as long as the freeze | lag with the dGPU steadily `suspended` or `active` throughout |
| H4  | The TrackPoint speed is caused by the missing model quirk (F9), not by a hardware fault.                                                                 | a DMI-matched `AttrTrackpointMultiplier` brings it into a usable range                  | speed unchanged under the quirk                               |

Unverified premise: that the "way too fast" TrackPoint is a constant state and not
something that happens only during a lag episode. The next triage run records both.

## Tasks

### Phase 1: Capture an episode

- [ ] 🔄 **Task 1.1**: Write a read-only `triage.bash` with a passive snapshot
  (the F1 to F12 probes) and a timed `--capture <seconds>` mode. The capture mode
  samples interrupt, GPE and dGPU rates, logs dGPU power transitions at 100ms
  resolution, and records a histogram of `libinput debug-events` timing for the
  **pointer devices only**, never the keyboard.
  - [x] ✅ Snapshot and capture both run on the host and produced F11 to F13
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`
- [ ] ⬜ **Task 1.2**: On the HOST, during an episode, run
  `triage.bash --capture 20` while moving the touchpad and then the TrackPoint.
- [ ] ⬜ **Task 1.3**: Read the report, then record which hypotheses survive in
  the JOURNAL and in the Facts table above.

### Phase 2: TrackPoint speed (H4)

- [ ] ⬜ **Task 2.1**: Decide the IaC home and mechanism. The candidates are a
  DMI-matched `/etc/libinput/local-overrides.quirks` entry in a
  `hardware-specific/` play, or GNOME's `pointingstick speed` gsetting in
  `play-gsettings.yml`. Record the decision below.
- [ ] ⬜ **Task 2.2**: Implement it, run QA, and write `deploy.bash` and
  `acceptance.bash`.
- [ ] ⬜ **Task 2.3**: On the HOST, run `deploy.bash` and `acceptance.bash`, then
  confirm the TrackPoint is usable.

### Phase 3: Lag fix

- [ ] ⬜ **Task 3.1**: Fix the layer identified in Task 1.3 through IaC. This task
  is scoped once the evidence exists. If H5 holds, the candidate fixes are to keep
  the dGPU out of runtime suspend (a battery cost), or to stop the compositor
  opening the dGPU at all. The mechanism for the second is still to be
  researched. Measure before choosing.

### Phase 4: Close

- [ ] ⬜ **Task 4.1**: Run the `qa-reviewer` agent over the plan's diff and
  resolve its findings.

## Technical Decisions

None yet. Task 2.1 records the first one here.

## Success Criteria

- [ ] A captured episode names the lagging layer, with evidence in the JOURNAL.
- [ ] The TrackPoint is usable at default GNOME speed after a playbook run.
- [ ] The lag no longer recurs, or its cause is filed upstream with evidence.
- [ ] QA passes (`./scripts/qa-all.bash`) and the `qa-reviewer` agent has signed off.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00142-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan opened with the live-episode findings
