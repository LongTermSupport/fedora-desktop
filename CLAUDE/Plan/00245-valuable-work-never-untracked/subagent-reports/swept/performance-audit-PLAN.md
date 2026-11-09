# Private plan: what is running, and why the laptop struggles

**Status:** Triage (read-only). Nothing on the host has been changed by this plan.
**Private:** lives under `untracked/`; never commit it. The captures in
`untracked/triage/perf-261021/` hold real container and project names.

## Goal

Find what is using the CPU, separate the work the owner wants from waste, and list
improvements to make through IaC later. The owner reports the laptop struggling while it is
already on the highest performance profile.

## Snapshot (2026-10-21, ~11:32 local, 2h45m after boot)

- ThinkPad P14s Gen 5, Core Ultra 7 165H (22 threads), 94 GiB RAM, on AC.
- Load average 15–27. About 10.7 of the 22 threads were busy. Memory is fine (64 GiB
  available, 1 GiB in swap).
- A meta-deploy run (Ansible, image build) and a second session's Ansible run were going
  during the capture, so part of that load is temporary.

## Findings, by likely impact

### F1. The CPU is held to about 1.5 GHz by a 25 W sustained power limit (biggest lever)

**CONFIRMED (turbostat, `t2-turbostat.raw`):** over 6×5 s, PkgWatt 24.74–25.31 W (pinned),
Busy 56–76%, Bzy_MHz 1455–1855, PkgTmp 56–59 °C. `max_power_uw` for the package domain is 28 W
(the 165H's rated base power), so the firmware's "performance" profile sits *below* the chip's
own base. BIOS R2DET42W (1.27), EC R2DHT33W. Profiles offered: low-power, balanced, performance
(no higher mode).

**The owner is cautious about heat** (they have seen it run up to the 60s °C). Options, safest
first:

1. Cut the waste first (F2, Plan 00213, hooks-daemon issue #131). That gives clock back within
   the same 25 W and changes no power settings.
2. If more is still needed, raise the MMIO PL1 only to 28 W, the CPU's own rated base, on AC
   only, with a temperature guard that drops back to 25 W above a chosen package temperature.
   Run it for a day and compare PkgTmp under the same load. Do not go higher without that data.
3. Check whether a newer BIOS changes the performance-profile PL1 (Lenovo release notes).

- Every core reads 1.5–1.8 GHz. The maximum is 4.7 GHz. Turbo is on, `intel_pstate` is
  active and `min_perf_pct` is 100.
- Temperatures are fine (package 54 °C) and the fan runs at about 4800 rpm, so heat is not
  what is limiting the clocks.
- The RAPL limits disagree. The MSR `package-0` limit is long 80 W / short 80 W, but the MMIO
  `intel-rapl-mmio:0` limit is **long 25 W** / short 80 W. The lower of the two applies, so
  sustained load is held to 25 W, and that means about 1.5 GHz on all cores.
- There have been 799k package and 380k core throttle events since boot, but the count did
  not move between 11:32 and 11:52. So no thermal throttling is happening now; the limit
  at work is a power cap, not heat. After the deploy the clocks averaged 1.8 GHz with 11–14
  threads busy.
- tuned is on `throughput-performance` (through tuned-ppd), thermald is inactive and
  `platform_profile` is `performance`. So the "highest performance level" the owner set does
  not lift the MMIO limit.
- **Still to confirm (needs root, read-only):** `sudo turbostat --quiet --show PkgWatt,Busy%,Bzy_MHz,PkgTmp --interval 5 --num_iterations 6`. It should show PkgWatt
  pinned near 25 W under load. Also read MSR 0x610 against the MMIO limit, and check
  whether firmware writes the MMIO value per profile (ThinkPad firmware / DYTC).
- **Possible fix (IaC, after confirming):** a system-level unit that raises the MMIO PL1 to
  match the MSR limit (or a chosen value, such as 45–55 W) on AC. It must not go above what
  the cooling can sustain. Watch temperatures afterwards. This fits next to the existing
  `play-mask-intel-lpmd.yml` power tuning. intel_lpmd is masked, so check that masking it did
  not remove a component that used to manage this.

### F2. Status-line ticks: roughly 20 Python start-ups per second (the biggest waste)

- The agent-bus Claude settings (`files/opt/claude-yolo/optional/agent-bus/settings.json`)
  set `statusLine` to `pingbus status-line` with `refreshInterval: 1`.
- Ten Claude sessions are running, on the host, in podman ccy containers and in LXC
  members. Each one spawns a fresh `pingbus status-line` about twice a second (measured: 23–24
  distinct processes per session in about 11 s).
- Each tick costs about 0.6 CPU-s for the pingbus Python start-up (a 17.8k-line zipapp). It
  then chains into the project's daemon-owned `.claude/hooks/status-line` (bash, about 0.5
  CPU-s). On the host the call also goes through the pyenv shim (`pyenv-exec`), which adds
  another bash.
- Cross-check: an LXC member with nothing running in it (bl-admin) uses 55% CPU, while its
  long-running processes add up to about 10%. bl-admin-api is the same at 50%. So each idle
  session burns about **0.4–0.5 of a thread** on its status line alone, which comes to
  **about 4–5 threads across ten sessions**. Under the 25 W limit from F1, that load also
  lowers the clock for everything else.
- **Post-deploy baseline (11:55, the deploy finished):** 14 threads busy in total. Processes
  that lived through the whole 10 s window used only 4.6 threads, and short-lived processes
  used the other **~9.4 threads** (`t1-shortlived.txt`). Tally of new processes over 9 s
  (`t1-young-top.txt`): 204 pingbus status-line Python processes, 120 `.claude/hooks/status-line`
  bash scripts, 57 inline `python3 -c` hooks-daemon socket senders, plus pyenv shim bash
  (pyenv-exec 63, pyenv-which 17, pyenv-version-name 9, pyenv 8, pyenv-hooks 8). Real hook
  events (pre/post-tool-use) are a small share (about 25). Each status-line tick is a chain of
  pingbus Python → `statusline-inner` sh → project status-line bash → inline Python socket
  send. On the host, every `python3` runs through about 5 pyenv bash scripts first.
- Component costs measured alone (`.claude/hooks/status-line` on the host): pingbus status-line
  0.32 CPU-s; hooks-daemon forwarder 0.11 CPU-s without pyenv on PATH, 0.41 CPU-s with it; one
  `python3` through the pyenv shim 0.19 CPU-s vs 0.028 for /usr/bin/python3. Filed as
  hooks-daemon issue #131 (its part); agent-bus part is Plan 00213 in fedora-desktop.
- So the status-line chain is likely **most of the 9.4 threads**. That is more than F1 costs,
  and it makes F1 worse.
- **Candidate fixes, cheapest first:**
  1. Raise `refreshInterval` (for example to 10 s, or drop it so the line only refreshes on
     events). A one-line change.
  2. Have `pingbus status-line` answer from a file the watcher keeps current (or a tiny Go
     binary) instead of starting the full zipapp every tick.
  3. Find out why each session produces about 2 ticks a second when the interval is 1 s.
     Events may be triggering extra refreshes.
  4. Host only: call `/usr/bin/python3` directly instead of the pyenv shim.
  - The chained hooks-daemon status line is daemon-owned, so its cost goes upstream if it
    matters once the tick rate is fixed.

### F3. mysqld in the bl-main LXC: about 2 threads, all the time

- `mysqld` has used 2–2.05 threads for the whole capture (44 min of CPU time in 2h11m).
- It is probably serving the phpunit run in bl-checkout (php at 20–70%), which is real work.
- **To check:** whether it stays busy when no test suite is running (`SHOW PROCESSLIST` and
  the slow log inside bl-main). Look at `innodb_buffer_pool_size`, the flush settings for a
  dev box (`innodb_flush_log_at_trx_commit=2`, `sync_binlog=0`, binlog off) and a tmpfs
  datadir for test databases. Any change goes through the LXC member's IaC.

### F4. Desktop: gnome-shell at 15–72%, DisplayLinkManager at 7–26%

- gnome-shell bursts were seen alongside the search providers being started at once
  (gnome-characters, clocks, contacts, nautilus, control-center), which looks like an
  overview or search being opened. Idle gnome-shell should be well under 10%.
- Rendering over DisplayLink costs CPU (frames are compressed in software). A known
  overhead; issue #28 covers related trouble.
- **To check:** gnome-shell CPU when idle and with no DisplayLink head attached, the
  extension list, and whether the search providers can be trimmed.

### F5. Smaller items

- Firefox: about 9.4 GiB across content processes, with two tabs at 33–64%. Worth a look at
  `about:processes`.
- The qbz flatpak writes about 38 journal lines a minute. Harmless but noisy.
- Every LXC member runs `atop -w … 600`. Cheap, keep it.
- One hooks-daemon Python process per session: low CPU, about 100–300 MiB each.
- `lm_sensors` (`sensors`) is not on the host. That is an IaC gap if we want it for
  monitoring. Add it, never install it by hand.

## Next steps

- [x] Re-capture after the deploy finishes (`t1-*`): 14 threads busy, ~9.4 of them short-lived.
- [ ] Confirm F2's share directly: cgroup CPU before and after suspending the status line in
  one idle member (needs the owner's go-ahead, since it changes a running session), or count
  the chain's CPU with `perf sched`/`bpftrace` exec accounting (root, read-only).
- [ ] Confirm F1 with turbostat (owner or approved sudo, read-only).
- [ ] Decide F2 fix 1 (refreshInterval) and measure before and after. Probably the quickest
  win.
- [ ] F3: profile mysqld with the test suite idle.
- [ ] F4: gnome-shell idle measurement.
- [ ] Turn the agreed fixes into a tracked plan or issue, anonymised, then into IaC.
