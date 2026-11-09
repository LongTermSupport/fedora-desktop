# Plan 00238: runaway process alert

**Status**: Not Started (research plan: Phase 1 establishes the facts; nothing is built until
the owner answers the questions below)
**Created**: 2026-10-29
**Owner**: joseph
**Priority**: Medium

## Overview

A Firefox tab that runs away (a page that keeps a CPU core or more busy, or grows its memory
fast) slows the whole desktop, and nothing says so or says which tab it is. The owner asked
for an alert, and then (verbatim): "no CLI route into firefox or something to alert on bad
tabs? can we make a plan to look some more into this? Runaway firefox tabs are a curse".

So this is a research plan first. Phase 1 establishes, from Mozilla's documentation and from
tests run by this plan's scripts on the host, how a program outside the Firefox UI can tell
**which tab** is burning CPU or memory, and how to alert on it or act on it, rather than only
watching pids. The research so far, with a source for every fact, is in
[RESEARCH-firefox-tab-identification.md](RESEARCH-firefox-tab-identification.md). Its short
answer: no supported, unprivileged route names the tab from outside the UI; the exact answer
(`ChromeUtils.requestProcInfo()`, what `about:processes` shows) needs privileged browser code.

Phase 2 builds what the owner chooses from the options below. The recommended shape is
layered: a generic watcher for any runaway process on the desktop, which names a Firefox
content process as such and opens `about:processes` for the person; Firefox's own built-ins
set where they help; a resource cap on Firefox's app scope so one tab cannot starve the
desktop; and a browser extension only if Phase 1 shows it earns its cost.

## Goals

- Phase 1 answers, with sources and host evidence, each lead in the research document: a
  control port, a WebExtension (privileged or not), Firefox's built-ins, and containment.
- The owner is alerted when one process holds over a CPU threshold for a sustained period,
  or its memory grows fast, with the process named (a Firefox content process as such), its
  CPU and memory and their trend, and where to look next.
- The alert never ends or signals a process, by default and by a QA gate.
- One tab cannot make the desktop unusable, if the owner chooses the containment cap.

## Non-Goals

- Killing, stopping or pausing any process automatically (default; Question Q4).
- A control port (Marionette, the Remote Agent) on the owner's daily browser: rejected in
  the research document for its security cost.
- Watching other users' processes, or servers (the alert is for a person at a desktop).
- Replacing Plan 00055's container watchdog, which watches container processes only.

## Context & Facts

**From the host agent (2026-10-29):**

- One Firefox content process held 117-154% CPU and grew from 1.2G to 2.3G RSS in about a
  minute, then settled (0.5%, 1.3G). Its command line names no site
  (`-isForBrowser ... tab`); only `about:processes` maps a pid to its tabs.
- Firefox is 158.0.1; `firefox --help` lists `--marionette`, `--remote-debugging-port`,
  `--start-debugger-server` and `--remote-allow-system-access`.

**From this repository (read, not run):**

- **How the repo alerts today.** The login health report
  (`helpers/host_health/login_report.py`) sends one `notify-send --app-name=fedora-desktop`
  notification, only when there are findings, and writes `host-status.json`
  (`helpers/host_health/status_document.py`), which the Fedora Desktop panel extension
  (`extensions/fedora-desktop@fedora-desktop/`, sections `health.js`, `containers.js`,
  `plays.js`) renders. The silent collector `host-health-collect.timer` refreshes it hourly
  on a desktop, so the health report is a snapshot, too coarse to see a spike that lasts a
  minute. The Go speech-to-text tool raises its notifications over
  `org.freedesktop.Notifications` with a replace id (`src-go/internal/wsi/dbus.go`).
- **The nearest existing watcher.** `container-watch` (Plan 00055, Dormant on its host
  verification) samples per-process CPU from `/proc` every 2 minutes, but attributes each
  process to a container by its cgroup and skips host processes
  (`helpers/containerwatch/core.py`, `scan_proc_root`); it writes `report.json` in the
  runtime directory and signals the panel's containers section over D-Bus. Its no-kill
  guarantee is a QA gate, `scripts/qa-nokill-containerwatch.bash` (Go, `src-go/qa/nokill`).
  It is Python, and new tooling here is Go (CLAUDE.md), so extending it to host processes
  would mean porting it.
- **How user services and timers are deployed.** Unit files live in
  `files/home/.config/systemd/user/`; `play-container-watch.yml` is the model: copy the
  units, resolve the user's uid with `getent` (never a default), then enable the timer with
  `scope: user` and `XDG_RUNTIME_DIR` set. Go commands are built on the host by
  `tasks/build-go-command.yml`, and a deployed command takes a row in
  `scripts/go-binary-suites.tsv` (`src-go/CLAUDE.md`, Build and deploy).
- **Firefox here.** `play-firefox.yml` (imported by `playbook-main.yml`) installs Fedora's
  RPM and deploys `files/etc/firefox/policies/policies.json`, which already sets prefs and
  force-installs extensions from addons.mozilla.org. So prefs (Lead 3) and an extension
  (Lead 2) both have an existing IaC home.

## Design Options

Detail and sources: [RESEARCH-firefox-tab-identification.md](RESEARCH-firefox-tab-identification.md).

| Option                                       | Names the tab?                                | Cost and risk                                                                                                                    | Status                                   |
| -------------------------------------------- | --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------- |
| A. Host watcher (Go, user service)           | No: names the process, opens the task manager | Small; reads `/proc` only; any process, not only Firefox                                                                         | Recommended                              |
| B. Control port + `requestProcInfo()`        | Yes, exactly                                  | Unauthenticated full control of the signed-in browser to local processes; marks every page automated; start-up flag only         | Rejected for the daily browser           |
| C1. Privileged experiment extension          | Yes, exactly, and can badge the tab           | Only if Fedora's build does not require signing (Task 1.2); privileged code on an internal API that may change in any release    | Depends on Task 1.2                      |
| C2. Ordinary extension, per-tab lag sensor   | Likely tab, by main-thread starvation         | Content script on all pages; misses workers and memory; needs signing (an unlisted AMO signature) unless Task 1.2 says otherwise | Depends on Task 1.4                      |
| D. Firefox built-ins (prefs)                 | The slow-script bar does, for one case        | Free; catches only scripts that never yield, and memory only when the system is low                                              | Recommended as tuning, not as the answer |
| E. Cap Firefox's app scope (systemd drop-in) | No                                            | Slows all of Firefox, not the bad tab; Firefox started outside GNOME escapes it                                                  | Recommended if the owner wants it (Q6)   |

### Decision 1 (proposed): layer A + D + E, and add C only on Phase 1 evidence

**Context**: the exact tab needs privileged browser code; the safe ways in depend on facts
Phase 1 has not read yet. **Proposal**: build the host watcher (A) whatever Phase 1 finds,
because it covers every process and needs no browser change; set the useful built-ins (D)
through the existing policies file; offer the scope cap (E); and add an extension (C1 if
Task 1.2 allows it, else C2 if Task 1.4 shows the sensor finds the tab) as a later phase,
joined to the watcher by native messaging (no listening port). **Status**: awaiting the
owner.

### The host watcher (Option A), as proposed

- A Go command (`src-go/cmd/runaway-watch`, `internal/runawaywatch`), test first through the
  `execx` fakes, run as a long-running systemd **user service** rather than a timer: the
  incident lasted about a minute, so it samples every 10 seconds (one read of
  `/proc/<pid>/stat` and `status` per process of this user) and keeps a short history in
  memory.
- Rules: CPU over the threshold for the whole duration (Q1); RSS grown by the threshold
  within its window (Q2). One alert per episode, raised again only if it gets worse or
  returns after settling.
- Naming: a process whose parent is Firefox and whose command line carries `-isForBrowser`
  is "a Firefox tab process"; the alert says so, gives its pid, CPU, memory and trend, and
  points at `about:processes` (Shift+Esc), with a notification button that opens it if
  Task 1.5 shows `firefox --new-tab about:processes` works.
- It writes its episodes to a JSON file in the runtime directory; the health report and the
  panel read that file (Q3). It never signals a process: the no-kill gate is extended to
  cover the new package.
- Deployed from an existing play rather than a new one: `play-host-health-login-report.yml`
  (the desktop health surface) is the proposed home, to be confirmed in Task 2.1.

## Questions for the Owner

Each has a recommended default; the plan proceeds on the default unless the owner says
otherwise.

- **Q1. CPU threshold and duration.** Default: **one process at 90% or more of one core,
  sustained over 2 minutes**, checked every 10 seconds. The incident (117-154% for about a
  minute) would alert at 2 minutes; a video call or a compile also hits 100% and lasts
  longer, which is why the duration is not shorter and why Q5's allowlist exists.
- **Q2. Memory-growth threshold.** Default: **RSS grown by 1 GiB or more within 2 minutes**,
  or over 4 GiB in total for one process. The incident grew 1.1 GiB in about a minute.
- **Q3. How it alerts.** Default: **both**: a desktop notification at the episode, and the
  episode listed in the health report and the panel for the rest of the day, so one missed
  notification is not a missed alert.
- **Q4. May it ever act?** Default: **no. It never kills, stops or pauses anything**; it
  names the process and opens the task manager for the person to decide. (An extension,
  if built, could offer "unload this tab" as a button the person presses; never by itself.)
- **Q5. Every process, or named apps only?** Default: **every process of the desktop user**,
  with Firefox content processes named as such, and an allowlist in `host_vars` for
  expected heavy work (compilers, video calls, the speech-to-text server).
- **Q6. Contain Firefox?** Default: **yes, a `CPUWeight` below the desktop's on Firefox's
  app scope** (it costs nothing when the machine is idle), **no hard `CPUQuota`**, and a
  `MemoryHigh` only if Task 1.3 shows Firefox reacts to the scope's memory pressure.
- **Q7. A browser extension?** Default: **decide after Phase 1**: yes for C1 if Task 1.2
  shows the build allows experiments; for C2 only if Task 1.4 shows the sensor finds the tab
  and the owner accepts an addons.mozilla.org account and API key in the vault for signing.

## Tasks

### Phase 1: research (docs, then host tests through this plan's scripts)

- [x] ✅ **Task 1.1**: Mozilla's documentation for each lead, with sources:
  [RESEARCH-firefox-tab-identification.md](RESEARCH-firefox-tab-identification.md)
- [ ] ⬜ **Task 1.2**: `triage.bash`, read-only: the Firefox build's `MOZ_REQUIRE_SIGNING`,
  channel and unsigned add-on scopes, from `AppConstants.sys.mjs` inside the installed
  `omni.ja`. Decides C1.
- [ ] ⬜ **Task 1.3**: `triage.bash`, read-only, continued: Firefox's process shapes (each
  process's `comm`, command line and parent; how a content process is told apart), the app
  scope it runs in (`/proc/<pid>/cgroup` of the parent), the scope's current CPU and memory
  properties, and this install's values of `browser.tabs.unloadOnLowMemory` and the
  slow-script prefs.
- [ ] ⬜ **Task 1.4**: a plan-local test in a **throwaway Firefox profile** (never the daily
  one): a test page that burns CPU on its main thread, then in a worker, then grows memory;
  load the C2 lag sensor (and the C1 experiment, if Task 1.2 allows) as a temporary add-on,
  and record what each sees in a foreground and a background tab. Whether the Remote Agent's
  `webExtension.install` can load it in that profile, or it needs the attended
  `about:debugging` route, is part of the task.
- [ ] ⬜ **Task 1.5**: attended check, listed by the script as needing the owner: whether
  `firefox --new-tab about:processes` opens the task manager in the running browser.
- [ ] ⬜ **Task 1.6**: queue Tasks 1.2-1.5 in `CLAUDE/Plan/meta-deploy.list` for the host
  agent; record the results in the research document and the journal.
- [ ] ⬜ **Task 1.7**: put Q1-Q7 to the owner with Phase 1's results; record the answers as
  Decisions.

### Phase 2: the host watcher (Option A), after the owner's answers

- [ ] ⬜ **Task 2.1**: `SPEC-runaway-watch.md`: numbered expectations (sampling, both rules,
  episodes, naming, the alert, the JSON file, exit codes, the allowlist); confirm the
  deploying play.
- [ ] ⬜ **Task 2.2**: tests first, then the Go command, through the `execx` fakes and a fake
  `/proc`.
- [ ] ⬜ **Task 2.3**: extend the no-kill gate to the new package; prove it red then green.
- [ ] ⬜ **Task 2.4**: the user service unit and the play tasks; the health report and panel
  reading the episodes file (Q3).
- [ ] ⬜ **Task 2.5**: `deploy.bash` and `acceptance.bash` (a test process that burns CPU and
  grows memory, the alert seen, nothing signalled), queued in `meta-deploy.list`.

### Phase 3: Firefox itself (D, E, and C if chosen)

- [ ] ⬜ **Task 3.1**: the chosen prefs in `files/etc/firefox/policies/policies.json` (D).
- [ ] ⬜ **Task 3.2**: the app scope drop-in, if Q6 says so (E), with acceptance reading the
  live scope's properties.
- [ ] ⬜ **Task 3.3**: the extension, if Q7 says so (C1 or C2), joined to the watcher by
  native messaging; its own spec and acceptance.
- [ ] ⬜ **Task 3.4**: the `qa-reviewer` agent over the plan's full diff.

## Dependencies

- Related: Plan 00055 (container watchdog, Dormant): the same `/proc` sampling for container
  processes; this plan does not change it.
- Related: Plan 00109 (the host health report and panel), whose surfaces this plan feeds.

## Success Criteria

- [ ] Every lead in the research document has a documented answer and, where it needs one,
  host evidence from this plan's scripts.
- [ ] A test process that holds a core past Q1's duration, and one that grows past Q2's
  threshold, each raise one alert that names it, on the host, through `acceptance.bash`.
- [ ] A Firefox content process is named as such in the alert, with the way to its tab.
- [ ] Nothing in the watcher can signal a process (the no-kill gate covers it).
- [ ] QA passes (`./scripts/qa-all.bash`), and the `qa-reviewer` agent's findings are
  resolved.

## Risks & Mitigations

| Risk                                                                               | Impact | Probability | Mitigation                                                                               |
| ---------------------------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------- |
| Alerts on expected heavy work get the alert muted                                  | H      | M           | Sustained-duration rule, one alert per episode, the Q5 allowlist                         |
| The tab still cannot be named exactly                                              | M      | H           | The alert opens `about:processes`; C1 or C2 only where Phase 1 proves it                 |
| A privileged extension breaks on a Firefox update                                  | M      | M           | C1 only with an acceptance check run after each Firefox update; C2 uses public APIs only |
| A scope cap slows Firefox when nothing else needs CPU                              | M      | L           | `CPUWeight` (relative, only under contention) by default; no `CPUQuota`                  |
| Adding a section to the Python health report counts as reworking it (a port to Go) | L      | M           | Keep the change to reading the watcher's JSON; raise it in Task 2.1 if it grows          |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00238-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Phase 1 docs research written (Task 1.1)
