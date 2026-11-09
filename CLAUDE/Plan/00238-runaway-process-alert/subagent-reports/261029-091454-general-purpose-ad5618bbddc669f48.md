Plan 00238 is written and committed as a research plan. It includes the owner's scope change and the host agent's facts. Nothing is built, and no host run is queued yet: the plan scripts for the host tests (Tasks 1.2–1.5) don't exist so far.

- **Plan:** 00238, at `<worktree>/CLAUDE/Plan/00238-runaway-process-alert/`. It has `PLAN.md`, `RESEARCH-firefox-tab-identification.md` (a source URL for every fact), a journal entry, and an Active entry in the plans README.
- **Branch and commit:** `agent-ad5618bbddc669f48-cf65c521`, commit `3ad741c37`. `qa-changed.bash` passed. Not pushed or merged.

**Main finding.** Only privileged browser code can map a pid to its tabs. The call is `ChromeUtils.requestProcInfo()`, which gives each process's pid, CPU, memory and pages (URL and title). That is what about:processes shows. No WebExtension API reports CPU or memory per tab. Each lead:
- **Lead 1, a control port (Marionette or the Remote Agent with `--remote-allow-system-access`):** works, but I recommend rejecting it on the daily browser. The port has no authentication or encryption and gives full control of the signed-in browser to any local process. It puts stripes on the address bar, and with Marionette every site sees `navigator.webdriver` (not confirmed for the Remote Agent alone). It is a start-up flag only, so "on demand" means a restart, which ends the runaway. A separate profile doesn't see the owner's tabs.
- **Lead 2a, a privileged extension:** could name the exact tab and badge it. It depends on one unknown: whether Fedora's build sets `MOZ_REQUIRE_SIGNING`. If it does, this route is closed on release Firefox. Task 1.2 reads it, read-only, from `omni.ja`.
- **Lead 2b, an ordinary extension:** a content script in each tab measures when that tab's main thread is starved. This points at the likely tab, not a proven one. It misses workers and memory growth, and must allow for Firefox slowing timers in background tabs. It needs signing on addons.mozilla.org. Task 1.4 tests it in a throwaway profile.
- **Lead 3, Firefox's built-ins:**
  - The slow-script warning only catches a script that never yields.
  - Tab unloading reacts only to low system memory. It is reportedly off by default on Linux, with disputed detection.
  - about:processes and about:unloads show the answer, but nothing prompts the person to look.
- **Lead 4, containment:** a systemd drop-in on Firefox's GNOME app scope protects the desktop, but slows all of Firefox, not just the bad tab.

**Recommended design (proposed Decision 1).**
- A Go user service, `runaway-watch`, alerts on any runaway process of the desktop user.
  - It runs continuously and samples `/proc` every 10 s. A timer or the hourly health report would miss a spike that lasts a minute.
  - It names a Firefox content process as "a Firefox tab process", with its pid, CPU, memory and trend. It points at about:processes (Shift+Esc), with a button to open it if Task 1.5 shows `firefox --new-tab about:processes` works.
  - It sends a desktop notification and writes a JSON file that the health report and panel read.
  - It never signals anything; the existing no-kill gate is extended to cover it.
- Firefox prefs are set through the existing `policies.json`, and the app-scope cap is optional.
- An extension is added only if Phase 1 shows it earns its cost. It would talk to the watcher by native messaging, with no listening port.

**Questions for the owner, with recommended defaults:**
1. **CPU threshold and duration:** 90% or more of one core for 2 minutes, sampled every 10 s.
2. **Memory growth:** RSS up by 1 GiB or more within 2 minutes, or over 4 GiB total for one process.
3. **How it alerts:** both a desktop notification and an entry in the health report and panel for the rest of the day.
4. **May it act:** no. It never kills, stops or pauses anything. An extension could at most offer an "unload this tab" button the person presses.
5. **Which processes:** every process of the desktop user, with Firefox tab processes named, plus a `host_vars` allowlist for expected heavy work (compiles, video calls, the speech-to-text server).
6. **Contain Firefox:** yes, a lower `CPUWeight` on its app scope (no cost when idle). No hard `CPUQuota`. `MemoryHigh` only if Task 1.3 shows Firefox reacts to the scope's memory pressure.
7. **A browser extension:** decide after Phase 1. The privileged one if Task 1.2 allows it. The ordinary one only if Task 1.4 shows it finds the tab and the owner accepts an addons.mozilla.org account and API key in the vault for signing.