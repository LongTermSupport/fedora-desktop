# Review of the blocked and stalled active plans (2026-10-06)

Read-only review, not plan work. Scope: every active plan folder under `CLAUDE/Plan/` except the 13
the current session is driving (00109, 00134, 00135, 00136, 00137, 00139, 00141, 00144, 00148,
00153, 00156, 00158, 00159). That leaves 61 plans. Last-activity dates come from `git log -1`.
Relevance checks were quick greps. Where a cell says "unverified", nobody looked deeper.

## Decisions for the owner (class A)

01. **Watermark tool (00037):** should the proposed defaults in the plan's DECISIONS.md be accepted
    as written, or changed first? Once answered, the `watermark` command can be built.
02. **MusicCast speaker controller (00038):** should it be a terminal app (Python + Textual, the
    recommendation) or a desktop window app (Tauri)? Should a licence question be raised with the
    KsanStone fork's author? Two more questions are in DECISION.md.
03. **One account map for several tools (00045):** should the seven design decisions and six
    platforms in DECISIONS.md be approved as written, or amended?
04. **Guard against leaking `localhost.yml` values into GitHub posts (00046):** the hooks daemon's
    sensitive-content check now covers most of this. Should the plan be closed as superseded, or
    should a small `localhost.yml`-derived deny-list be built on top? If it is built, should the
    machine's hostname be on that list? A hostname that is a common word would block commits until
    it is allowlisted.
05. **Disk reclaim leftovers (00062):** for the one orphaned Podman layer and nine old volumes, which
    do you want? (a) Leave them and only prevent new ones in ccy. (b) Also repair them now with an
    ownership fix. (c) Leave the task open. Nothing else in the plan needs you; you run `reclaim`
    yourself.
06. **Headless provisioning proof (00063):** will you run the GitHub-token test scenario with a
    throwaway account's token? The alternative is to treat months of production use as proof and
    close the plan without it.
07. **Fedora Cloud Base fixes (00065):** do production headless use plus the VM lab count as proof,
    or do you still want a dedicated run on a fresh Cloud Base machine?
08. **Separate network per ccy session (00080):** build it now, or close it as an accepted risk? You
    already judged that sessions reaching each other is not a significant risk.
09. **Two fixes already merged, proof outstanding (00086 kernel-modules check, 00088 Claude state
    folder):** close both as merged, or first re-run the downstream infra repo's live test, which
    lives in another repo on another machine and proves both at once?
10. **Run-log secret scrubber (00121):** un-park it now or leave it parked? The remaining code is
    agent work. One step needs a throwaway GitHub token from you.
11. **Docker and Podman inside LXC (00127):** is this still wanted? If yes, which LXC container
    should the checks run in? If no, cancel it.
12. **QA on a fresh clone stops before about 30 checks (00128):** where should the GNOME extension
    dev tools (npm packages) come from? (a) A playbook always installs them. (b) An optional
    dev-setup step or a documented `npm ci`. (c) Neither, with the exception written into
    CLAUDE.md. The other half of the plan, making QA carry on past a broken tool, can go ahead
    under any answer.
13. **Per-rule coverage in the code-pattern scanner (00129):** accept a scan about 4.9 times slower
    (about 7 extra seconds per QA run) for exact per-rule numbers, or take the cheaper estimate
    described in the plan?
14. **Switching a running ccy session to another token (00146):** the hooks-daemon feature it
    needed now exists, so two questions are left. Where should the live token be read from: the
    running Claude's process environment (recommended), a private in-memory file copy, or as
    today? Should the session list show the current token name (recommended), or keep showing the
    token it was launched with?
15. **"Fedora Desktop" entry in the tmux F12 menu (00147):** accept the plan's recommendations? They
    are: a `fedora-desktop-menu` script; the play reloads tmux config into running tmux servers;
    the token-switch item is for `ccy` only, not `cc`.
16. **Up-arrow loses a Ctrl+C'd command (00149):** cancel it as not reproducible, or leave it
    dormant until it happens again?
17. **Freeze and thaw in `ccy-sessions` (00154):** accept the proposed defaults? They are: a FROZEN
    column, Ctrl-F toggles freeze, Enter on a frozen session attaches with a note, and `podfreeze`
    is a hard requirement. The agent can start the tests either way.
18. **Automated Fedora installer (018):** it was written for Fedora 43. Close it and carry the
    remaining real-hardware test into the Fedora 44 fresh-install audit (00053), or keep it open?
19. **Inventory named per machine (023):** do you want per-machine settings files, so several
    laptops can be managed, or keep the single `localhost.yml` for good? Build it, shelve it, or
    cancel it.
20. **Screen sharing (028 and 031):** both were written for Fedora 43. Are Slack and Meet screen
    sharing still broken on Fedora 44? If they work, cancel both. If not, 028's diagnostics run
    first.
21. **RapidRAW cloud AI experiment (029):** still want to spend the $10 vast.ai credit on it? Run
    Phase 1, park it, or cancel it.
22. **PHPantom PHP language server (030):** it is already built into the ccy image next to
    Intelephense. Rebuild on the host and compare the two, or close it as implemented with no
    comparison?
23. **DisplayLink dock replug (00056):** which GitHub account may comment on issue #28? The
    current one can only read.

## Can proceed without the owner (class C)

- **00131:** widen the `|| true` scanner rule and fix its 18 sites. Keep the planted-secret test
  for the git-hook sites.
- **026 spring cleaning:** still real. curl piped to bash remains in `play-python.yml`,
  `play-nvm-install.yml` and `play-rust-dev.yml`. `scripts/nvidia-status.bash` and the DisplayLink
  status script still lack `set -e`. The plan's paths are stale; the duplicate shebangs and
  tracked backups are already gone.
- **00049 full audit:** continue the remaining batches. Its pytest stage first needs pytest added
  to the ccy image (an IaC change plus a ccy version bump).
- **022 installer hardening:** add `pykickstart` (it is in no playbook) and a `ksvalidator` QA
  check, then do Tasks 2.4 and 5.3.
- **032 compress/uncompress:** extend `acceptance.bash` (Task 4.2), then one host re-run, then
  close.
- **034 config GitHub account in localhost.yml:** re-scope it against today's `run.bash` (the
  plan's line references are stale) and check overlap with 00035 and 00082.
- **00143 TrackPoint speed:** write the libinput quirk or speed setting play now. The touchpad-lag
  half still needs a capture during an episode.
- **00155 headset button:** write `triage.bash` and add it to meta-deploy. The owner then runs it
  and presses the button.
- **00154, 00146 (Phases 2 and 3.1 to 3.3), 00128 (Phase 3):** the parts that need no decision.
- **00069** (PLAN.md edited in place) and **00117** (lab checker version gate): never started, but
  free to start.
- **00056:** rework the wedge-detection heuristic, which the July incident showed is wrong.
- **00035:** the design tasks can be written in the container; the live check is host-only.

## Host runs (class B)

Plans with a plan script that could go into `CLAUDE/Plan/meta-deploy.bash`. Its PLANS list holds
only 00148 today, and that edit is uncommitted.

| Plan  | Script(s)                             | Note                                                                                    |
| ----- | ------------------------------------- | --------------------------------------------------------------------------------------- |
| 00055 | `deploy.bash`, `acceptance.bash`      | container process watchdog, Task 4.2                                                    |
| 00058 | `deploy.bash`                         | verifies pins 2.1 to 2.4; darktable 5.6.0 waits on Fedora, so consider splitting it off |
| 00061 | `acceptance.bash`                     | Task 3.8                                                                                |
| 00074 | `acceptance.bash`                     | Task 3.3, the last one                                                                  |
| 00092 | `triage.bash`, `deploy.bash`          | then `ccy --rebuild`, then `acceptance.bash`                                            |
| 00098 | `triage.bash`                         | Phase 0 facts; then agent work on Phase 1                                               |
| 00054 | `update.bash`                         | 44 of 45 tasks done                                                                     |
| 00104 | `triage.bash`                         | needs the physical dock; Task 4.2 is a manual unplug                                    |
| 00073 | `acceptance.bash`                     | needs a password-sudo machine: another machine                                          |
| 00113 | Plan 00068's `triage.bash` (Task 0.1) | then the agent starts Phase 1                                                           |

Code-complete plans with no plan script. Each needs one manual observation, or an agent could
first add a small `acceptance.bash`:

- **00039, 00040, 00041, 00048, 00064, 00072, 00089, 014, 030:** on this laptop.
- **00114, 00115, 00116, 00118, 00119:** another machine (password sudo, headless, deploy-key
  only, SELinux enforcing, port 22 blocked). Best folded into the next headless or VM run.
- **00053:** needs a fresh Fedora 44 install.

## Cancel candidates (class D)

- **00093 (ccy version gate covers two of eight files):** delivered by the 2026-10-03 pre-commit
  change, "an edit to what the ccy image bakes in needs a container version bump". The plan still
  says Not Started.
- **009 claude-devtools:** delivered. `play-claude-devtools.yml`, `ccdt`, the bashrc include and
  the docs page all exist. Mark it Complete rather than cancel.
- **011 and 013 claude-devtools:** duplicates of 009; cancel.
- **004 feature documentation:** superseded by the doc work in Plans 00095, 00070, 00096 and 006.
- **007 speech-to-text leak fixes:** superseded by 00148. Caveat: the plan ticks server-side
  watchdog and disconnect layers that grep cannot find in `wsi-stream-server`, so check that in
  00148 before cancelling.
- **00051 ansible-lint:** `scripts/lint` and FQCN enforcement already exist, yet nothing is ticked.
  Cancel it, or re-scope it to the remaining violations.
- **00046:** possibly superseded (see decision 4).
- **00086 and 00088:** fixes merged; only an external proof is left (see decision 9).
- **028 and 031:** if screen sharing works on Fedora 44 (see decision 20).

## Never started (class E)

00045, 00051, 00069, 00093, 00113, 00117, 00129, 00131, 00146, 00147, 00154, 00155, 004, 011,
013, 023, 028, 029, 034.

## README and PLAN.md disagreements

- **009:** README says "pending host deployment and testing"; PLAN says In Progress. The work is
  in fact delivered.
- **00146:** README says it needs an upstream feature, "request drafted, not filed". PLAN says the
  API now exists (hooks-daemon issue #71); it is merged but not yet released.
- **00093:** README describes the gate gap as open; the code closed it on 2026-10-03.
- **00074, 00086, 00088:** README wording reads as done ("now distinguishes", "Fixed with", "now
  derived"); PLAN says Blocked or In Progress.
- **00062:** README says "HOST deploy and live test pending"; PLAN says Blocked, and the real
  blocker is the Task 4.8 owner decision.
- **00063:** README says "Depends on Plan 00061"; PLAN says code-complete and in production use.
- **00129:** README says "one owner decision left"; PLAN says Not Started. Compatible, but the
  README is not a status.
- **00121, 00038, 00054, 00055, 00058, 00061, 00072, 00073:** README gives no Dormant, Parked or
  Blocked marker, while the PLAN is Dormant or Blocked. For 00072 and 00073 the README does say
  Dormant in prose.
- **014:** PLAN says "In Progress (research and planning)", but Phases 1 to 4 are built.
- **00051, 011, 013, 004:** "Not Started" in both, but the work exists elsewhere (see Cancel
  candidates).

## Full table

| Plan  | Title                             | Status (PLAN.md)                    | Last       | Waiting on                                     | Still relevant                              | Class |
| ----- | --------------------------------- | ----------------------------------- | ---------- | ---------------------------------------------- | ------------------------------------------- | ----- |
| 00035 | gh multi-account hardening        | Dormant                             | 2026-09-02 | stalled; design work, then host verify         | yes                                         | C, B  |
| 00037 | image watermarking toolkit        | In Progress                         | 2026-09-02 | Task 1.3 decisions                             | yes; play exists, no CLI                    | A     |
| 00038 | MusicCast controller              | Blocked on decision-gate review     | 2026-08-24 | 4 questions in DECISION.md                     | yes                                         | A     |
| 00039 | ftp-camera viewer TUI             | Dormant; code shipped               | 2026-09-02 | host manual test matrix                        | yes                                         | B     |
| 00040 | raw clipping scanner              | In Progress                         | 2026-09-03 | host probes, idempotency, manual tests         | yes; `clip-scan` exists                     | B     |
| 00041 | remote desktop quick toggle       | In Progress                         | 2026-08-24 | host deploy and live test                      | yes; code done                              | B     |
| 00045 | project personas                  | Not Started (Phase 1 gate)          | 2026-09-02 | owner sign-off on Decisions 1 to 7             | yes                                         | A, E  |
| 00046 | localhost.yml leak guard          | In Progress (research done)         | 2026-09-15 | supersede-or-build decision                    | partly; daemon handler covers most          | A, D? |
| 00048 | cc token-source parity            | In Progress; Phase 4 host           | 2026-09-12 | host deploy and smoke tests                    | yes                                         | B     |
| 00049 | full repo audit                   | In Progress                         | 2026-09-14 | agent batches; pytest missing from ccy image   | yes                                         | C     |
| 00051 | ansible-lint improvement          | Not Started                         | 2026-06-12 | nothing; stale                                 | partly done elsewhere                       | D, E  |
| 00053 | F44 fresh-install audit           | Dormant                             | 2026-09-02 | a fresh F44 install                            | yes                                         | B     |
| 00054 | GitHub SSH 443 host-level         | Dormant; 44 of 45                   | 2026-08-17 | host `update.bash`                             | yes                                         | B     |
| 00055 | container process watchdog        | Dormant; Task 4.2                   | 2026-10-05 | host `deploy.bash` and `acceptance.bash`       | yes                                         | B     |
| 00056 | DisplayLink dock hotplug          | Dormant; Task 2.3                   | 2026-09-02 | heuristic rework; dock; gh write access        | yes                                         | C, B  |
| 00058 | version pin updates               | Dormant                             | 2026-10-03 | host `deploy.bash`; darktable 5.6.0 upstream   | yes                                         | B     |
| 00061 | headless server provisioning      | Dormant; Task 3.8                   | 2026-09-02 | host `acceptance.bash`                         | yes                                         | B     |
| 00062 | disk reclaim TUI                  | Blocked on host access              | 2026-10-06 | Task 4.8 scope decision                        | yes; `reclaim` exists                       | A     |
| 00063 | headless run.bash provisioning    | In Progress; in production          | 2026-09-17 | token scenario with throwaway PAT, or waive it | yes                                         | A     |
| 00064 | `open` command                    | Dormant; Phase 4                    | 2026-10-03 | host deploy and eyes-on checks                 | yes                                         | B     |
| 00065 | Cloud Base blocker fixes          | Dormant                             | 2026-09-02 | fresh Cloud Base run, or waive it              | probably overtaken by 00063 and the lab     | A     |
| 00069 | PLAN.md edited in place           | Not Started                         | 2026-07-31 | nothing                                        | yes                                         | E, C  |
| 00072 | ccy asserts rootless engine       | Dormant                             | 2026-09-02 | one host observation                           | yes                                         | B     |
| 00073 | headless sudo password file       | Dormant; Task 3.3                   | 2026-09-02 | a password-sudo machine                        | yes                                         | B     |
| 00074 | grub cgroup check                 | Blocked on host                     | 2026-09-12 | host `acceptance.bash`                         | yes                                         | B     |
| 00080 | ccy session network isolation     | Dormant; low priority               | 2026-09-24 | owner scheduling                               | weak                                        | A     |
| 00086 | kernel-modules enumeration        | In Progress; fix merged             | 2026-08-24 | downstream proof in another repo               | merged                                      | A, D  |
| 00088 | claude state dir HOME fact        | In Progress; fix merged             | 2026-08-24 | same downstream proof                          | merged                                      | A, D  |
| 00089 | runner token guard                | In Progress; code done              | 2026-08-25 | `ccy --rebuild` and a `--no-ssh` launch        | yes                                         | B     |
| 00092 | ccy child claude spawn            | Blocked; Task 6.4                   | 2026-09-17 | host `deploy.bash` and rebuild                 | yes                                         | B     |
| 00093 | ccy version gate coverage         | Not Started                         | 2026-09-02 | nothing                                        | no; delivered on 2026-10-03                 | D     |
| 00098 | transcripts at rest               | In Progress                         | 2026-09-23 | host `triage.bash` (Phase 0)                   | yes                                         | B, C  |
| 00104 | suspend on dock unplug            | Blocked: hardware                   | 2026-09-17 | physical laptop and dock                       | yes                                         | B     |
| 00113 | ccy CI runner                     | Not Started                         | 2026-09-14 | host triage (Task 0.1)                         | yes                                         | B, E  |
| 00114 | run.bash single-play mode         | In Progress                         | 2026-09-14 | live password-sudo run                         | mostly done                                 | B     |
| 00115 | playbook shebang via run.bash     | In Progress                         | 2026-09-14 | headless convergence check                     | mostly done                                 | B     |
| 00116 | ccy deploy keys, forwarded agent  | In Progress                         | 2026-09-14 | deploy-key-only box                            | mostly done                                 | B     |
| 00117 | vmtest checker version gate       | Not Started                         | 2026-09-14 | nothing                                        | yes                                         | E, C  |
| 00118 | ccy on SELinux enforcing          | In Progress                         | 2026-09-14 | enforcing box                                  | mostly done                                 | B     |
| 00119 | headless SSH 443 input            | In Progress                         | 2026-09-14 | port-22-blocked box                            | mostly done                                 | B     |
| 00121 | run-log secret scrubber           | Dormant (parked)                    | 2026-09-15 | un-park decision                               | yes, low priority                           | A     |
| 00127 | Docker/Podman inside LXC          | Dormant (parked)                    | 2026-09-16 | want-it decision                               | if wanted                                   | A     |
| 00128 | QA abort silences 30 gates        | In Progress; Phase 2 gate           | 2026-09-16 | Task 2.1 decision                              | yes                                         | A, C  |
| 00129 | semgrep per-rule coverage         | Not Started                         | 2026-09-16 | Task 1.1 decision                              | yes                                         | A, E  |
| 00131 | semgrep `\|\| true` enclosed form | Not Started                         | 2026-09-16 | nothing                                        | yes; 18 sites                               | C, E  |
| 00143 | pointer lag, TrackPoint speed     | In Progress                         | 2026-09-30 | lag capture (host); TrackPoint is free         | yes                                         | C, B  |
| 00146 | ccy token switch                  | Not Started (decision gate)         | 2026-10-03 | decisions 1.2 and 1.3                          | yes; upstream API exists                    | A     |
| 00147 | tmux F12 menu                     | Not Started (decision gate)         | 2026-10-02 | decisions 1.1 to 1.3                           | yes                                         | A     |
| 00149 | up-arrow loses interrupted cmd    | Dormant (parked)                    | 2026-10-02 | recurrence, or cancel                          | unknown                                     | A     |
| 00154 | ccy sessions freeze/thaw          | Not Started                         | 2026-10-05 | defaults and one host `podman ps` fact         | yes                                         | C, A  |
| 00155 | headset button STT toggle         | Not Started (gate after triage)     | 2026-10-04 | triage script not yet written                  | yes                                         | C, B  |
| 004   | feature documentation             | Not Started                         | 2026-08-24 | nothing                                        | no; done by later doc plans                 | D     |
| 007   | STT resource leak fixes           | In Progress                         | 2026-08-24 | nothing recorded                               | superseded by 00148 (verify watchdog)       | D     |
| 009   | claude-devtools                   | In Progress                         | 2026-08-24 | host test                                      | delivered                                   | D     |
| 011   | claude-devtools                   | Not Started                         | 2026-02-18 | nothing                                        | duplicate of 009                            | D     |
| 013   | claude-devtools                   | Not Started                         | 2026-02-18 | nothing                                        | duplicate of 009                            | D     |
| 014   | whisper model manager             | In Progress (research and planning) | 2026-08-24 | Phase 5 host test                              | mostly built; 00156 has since changed it    | B     |
| 018   | Fedora kickstart install          | In Progress (F43)                   | 2026-08-24 | real-hardware install test                     | partly; F43 target                          | A     |
| 022   | install security and resilience   | In Progress                         | 2026-09-16 | `pykickstart` IaC gap; agent tasks             | yes                                         | C     |
| 023   | hostname-based inventory          | Not Started                         | 2026-03-15 | want-it decision                               | unclear                                     | A, E  |
| 026   | repo spring cleaning              | In Progress                         | 2026-08-24 | agent work                                     | yes; curl\|bash and missing `set -e` remain | C     |
| 028   | F43 screen sharing                | Not Started                         | 2026-06-12 | is it still broken on F44                      | doubtful                                    | A, E  |
| 029   | RapidRAW cloud AI                 | Not Started                         | 2026-08-24 | want-it decision                               | uncertain                                   | A, E  |
| 030   | PHPantom LSP                      | Dormant                             | 2026-07-07 | host rebuild and comparison, or close          | implemented                                 | A, B  |
| 031   | reliable screen sharing           | Dormant; Phase 3 go-ahead           | 2026-07-07 | go-ahead                                       | doubtful (F43 era)                          | A     |
| 032   | compression helpers               | In Progress; accepted               | 2026-10-03 | Task 4.2, then a host re-run                   | yes, nearly done                            | C, B  |
| 034   | config account in localhost.yml   | Not Started                         | 2026-06-12 | re-scope                                       | yes; plan references are stale              | C, E  |

Per-group working tables with longer notes: `untracked/scratch/blocked-review-g1.md` to `-g5.md`.
