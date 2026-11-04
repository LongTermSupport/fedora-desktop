# Status survey: plans and GitHub issues (2026-10-13)

Method: Status lines, task icons and `git log -1 --format=%cs -- <folder>` for every folder directly under CLAUDE/Plan/ (not Completed/). Caveat: several plans share a last-commit date of 2026-08-24 or 2026-09-02, which looks like bulk edits (formatting, renames), so "stale" below is by commit date, not necessarily by real work. Open issues: 40.

Teams work, one line each: Plan 00161 (bus) in progress, 00177 (ccy-teams) in progress, 00178 (history export) not started, low priority. Not assessed further.

## 1. Open plans

Legend: IP = in progress, OB = blocked on the owner, NS = not started, DORM = dormant, DONE? = effectively done but not closed. Dates are last commit. Dormant means the plan's own Status says so.

### Active (touched in the last 30 days, by date after 2026-09-13)

| Plan                                       | State                        | Unblocks it                                                                         |
| ------------------------------------------ | ---------------------------- | ----------------------------------------------------------------------------------- |
| 00176 dash-to-dock stays up                | OB (10-13)                   | Owner's five observations at the screen                                             |
| 00173 shutdown-with-update                 | OB (10-12)                   | Host deploy, evening test, answer on failed-dnf question                            |
| 00172 bluetooth headset mic                | IP (10-12)                   | Triage done; implement fix in play-hd-audio.yml (agent work), then host deploy      |
| 00171 stt insert/icon sync                 | IP (10-12)                   | Host deploy + acceptance. Covers issue #96                                          |
| 00170 unifi network tooling                | IP (10-12)                   | Agent work: 5 tasks left, then move to files/                                       |
| 00169 ccy image keeps tools current        | IP (10-10)                   | Phase 3 (tool floors, Fedora base, weekly Actions build) is agent work              |
| 00168 managed config changed out of band   | NS (10-09)                   | Agent work; issue #91                                                               |
| 00167 cc refuses outside a repo            | NS (10-09)                   | Agent work; issue #90                                                               |
| 00174 polyglot tooling folders             | NS (10-12)                   | Agent work; prerequisite for 00155's Rust mapper                                    |
| 00164 stt enter lost after focus retake    | IP (10-09)                   | 2 tasks left: host verification                                                     |
| 00165 ccy time zone                        | IP (10-10)                   | 1 task in progress, 0 todo: likely DONE? (shipped CCY 3.90.0), close after check    |
| 00162 ccy-sessions always sign commits     | no status line (10-07)       | Plan has no Status header; check and close or fix header                            |
| 00160 ccy env local override               | IP (10-07)                   | 0 todo, 1 in progress: likely DONE?, close                                          |
| 00158 ccy-sessions take over               | IP (10-05)                   | 2 tasks left (host acceptance probably)                                             |
| 00156 stt auto model never downloaded      | IP (10-09)                   | 5 todo, 1 in progress: host verification                                            |
| 00155 headset button toggles stt           | IP (10-12)                   | Design settled; 20 tasks, needs 00174 (Rust QA) first                               |
| 00159 stt vocabulary                       | NS (10-12)                   | 9 tasks; agent work, then host test                                                 |
| 00154 ccy-sessions freeze and thaw         | NS (10-05)                   | 7 tasks; 2 blocked icons, check what blocks                                         |
| 00153 release tags                         | IP (10-05)                   | Host acceptance of Phase 3, then the owner cuts the first tag (agents never tag)    |
| 00148 stt unlimited dictation              | IP (10-12)                   | Host checks 0.5, 6.2, 8.4, 9.5; owner decisions 11.1-11.4, 12.1, 12.2               |
| 00147 tmux F12 menu items                  | NS (10-02)                   | Owner Phase 1 decisions                                                             |
| 00146 ccy token switch via supervisor      | NS (10-03)                   | Owner Phase 1 decisions                                                             |
| 00144 single fedora-desktop panel icon     | IP (10-06)                   | 0 todo, 3 blocked: host test                                                        |
| 00143 pointer lag / trackpoint             | IP (09-30)                   | 8 todo; hardware tuning at the machine                                              |
| 00141 run-bash changed plays               | IP (10-05)                   | 0 todo, 1 blocked: host run                                                         |
| 00139 commit signing everywhere            | IP (10-09)                   | 6 todo, 1 blocked: host verification                                                |
| 00137 unattended server self-update        | IP (10-08)                   | 0 todo, 3 blocked: host/server test                                                 |
| 00136 host health report                   | IP (10-02)                   | 0 todo, 1 blocked: host run; DONE? once accepted                                    |
| 00135 ccy-sessions survive reboot          | IP (10-09)                   | 0 todo, 2 in progress, 1 blocked: reboot test; ties to issues #69, #86, #87, #88    |
| 00134 startup log triage                   | IP (10-06)                   | 1 todo, 2 blocked: host                                                             |
| 00109 desktop drift detection and panel    | IP (10-05)                   | 7 todo, 6 blocked: host                                                             |
| 00104 suspend aborts on dock unplug        | OB (10-09)                   | Physical laptop and dock                                                            |
| 00098 encrypted claude transcripts at rest | IP (09-23)                   | 15 todo: next phase is agent work after a design check                              |
| 00092 ccy child claude spawn mode          | OB (09-17)                   | Task 6.4: deploy.bash then `ccy --rebuild`                                          |
| 00089-00088-00086 (see below)              | IP (08-24/25)                | stale                                                                               |
| 00080 ccy session network isolation        | DORM (09-24)                 | Owner decision to resume                                                            |
| 00074 grub cgroup check                    | OB (09-12)                   | Host Task 3.3 only                                                                  |
| 00065 / 00063 headless provisioning        | 00063 IP (09-17), 00065 DORM | 00063 in production use per owner: close candidate                                  |
| 00064 open command                         | DORM (10-03)                 | Host deploy + live checks                                                           |
| 00062 disk reclaim TUI                     | OB (10-06)                   | Host only tasks                                                                     |
| 00058 github version pin updates           | DORM (10-03)                 | Darktable 5.6.0 bump + host deploy.bash                                             |
| 00055 container process watchdog           | DORM (10-05)                 | Task 4.2 host deploy and verify                                                     |
| 00048 cc token source parity               | IP (09-12)                   | Phase 4 host deploy; 25 of 32 done                                                  |
| 00049 full repo audit                      | IP (09-14)                   | 3 tasks left; 72 done: close candidate                                              |
| 00032 compression helpers                  | IP (10-03)                   | Accepted on host; 1 task left: close candidate                                      |
| 00128 qa abort silences 30 gates           | IP (09-16)                   | Owner decision gate on Phase 2                                                      |
| 00129, 00131 semgrep coverage              | NS (09-16)                   | Agent work (pair; do together)                                                      |
| 00113, 00117                               | NS (09-14)                   | Agent work; 00113 is the ccy CI runner (25 tasks, large)                            |
| 00114, 00115, 00116, 00118, 00119          | IP (09-14)                   | Near done (0-1 todo, 00115 and 00114 0 todo): close candidates, check blocked icons |
| 00121 run-log secret scrubber              | DORM (09-15)                 | Owner un-parks                                                                      |
| 00127 docker/podman inside LXC             | DORM (09-16)                 | Owner un-parks; issue #48                                                           |
| 00149 up arrow loses interrupted command   | DORM (10-02)                 | Needs a reproduction                                                                |
| 00046 localhost.yml leak guard             | IP (09-15)                   | Phase 1 research done; implementation not started                                   |
| 00022 install security and resilience      | IP (09-16)                   | 4 blocked tasks                                                                     |
| 00063 headless run-bash                    | IP                           | see above                                                                           |

### Stale by commit date (before 2026-09-13)

| Plan                                       | State                          | Unblocks it                                     |
| ------------------------------------------ | ------------------------------ | ----------------------------------------------- |
| 00035 gh multi-account hardening           | DORM (09-02)                   | Owner decision                                  |
| 00037 image watermarking toolkit           | IP (09-02), 24 done / 20 todo  | Owner decision to continue; no work in 6 weeks  |
| 00038 musiccast controller                 | OB (08-24)                     | Owner reads DECISION.md                         |
| 00039 ftp camera viewer TUI                | DORM (09-02)                   | Host manual test matrix                         |
| 00040 raw clipping scanner                 | IP (09-03)                     | 35 todo; host tests                             |
| 00041 remote desktop quick toggle          | IP (08-24)                     | 12 todo; no work                                |
| 00045 project personas                     | NS (09-02)                     | Owner Phase 1 decision gate                     |
| 00051 ansible-lint improvement             | NS (06-12)                     | Not started 4 months; no tasks counted          |
| 00053 fedora 44 fresh install audit        | DORM (09-02)                   | Fresh F44 install on host                       |
| 00054 github ssh 443 host level            | DORM (08-17)                   | 44 of 45 done; host deploy + live test          |
| 00056 displaydlink dock hotplug            | DORM (09-02)                   | Real DisplayLink dock                           |
| 00061 headless server provisioning         | DORM (09-02)                   | Run acceptance.bash on laptop host              |
| 00065 headless cloud base fixes            | DORM (09-02)                   | Fresh Fedora Cloud Base headless run            |
| 00069 plan md edited in place              | NS (07-31)                     | Agent work; 7 tasks                             |
| 00072 ccy assert rootless engine           | DORM (09-02)                   | Desktop check on rootless engine                |
| 00073 headless sudo password file          | DORM (09-02)                   | Host verify on password-sudo box                |
| 00086, 00088, 00089                        | IP (08-24/25)                  | 1-2 tasks each; likely host checks or closeable |
| 00093 ccy version gate covers two of eight | NS (09-02)                     | Agent work; 7 tasks, small                      |
| 004 comprehensive feature docs             | NS (08-24)                     | 114 tasks, never started                        |
| 007 stt resource leak fixes                | IP (08-24)                     | 60 done, 32 todo                                |
| 009 / 011 / 013 claude-devtools            | 009 IP, 011 and 013 NS (02-18) | 011 and 013 look like duplicates of 009         |
| 014 whisper model manager                  | IP (08-24)                     | Research; overlaps 00148 model review           |
| 018 fedora kickstart install               | IP (08-24)                     | No tasks counted; empty shell                   |
| 023 hostname based inventory               | NS (03-15)                     | No tasks counted                                |
| 026 repo spring cleaning                   | IP (08-24)                     | 43 todo                                         |
| 028 fedora screen sharing                  | NS (06-12)                     | Superseded by 031?                              |
| 029 rapid raw cloud ai                     | NS (08-24)                     | 65 todo                                         |
| 030 phpantom lsp                           | DORM (07-07)                   | Owner go/no-go                                  |
| 031 reliable screen sharing                | DORM (07-07)                   | Owner go-ahead for Phase 3; issue #17           |
| 034 localhost config account               | NS (06-12)                     | 16 todo                                         |

## 2. Owner's to-do list (most valuable first)

1. Run `./CLAUDE/Plan/meta-deploy.bash` after the teams work lands. Today its PLANS list is empty (Plan 00177 passed). Plans with built code waiting on a host deploy and acceptance: 00171 (stt insert/icon, the daily annoyance), 00164, 00156, 00148 host checks, 00153 (then cut the first release tag), 00092 (`ccy --rebuild`), 00048, 00139, 00137, 00135 (needs a reboot test), 00141, 00144, 00109, 00134, 00136. Each needs adding to meta-deploy before the owner can run them.
2. Plan 00176 (Dash to Dock over windows): five observations at the screen.
3. Plan 00173 (shutdown-with-update): evening test and the failed-dnf decision.
4. Decisions: 00148 items 11.1-11.4, 12.1, 12.2 (stt recording mode, panel as thin layer, stop grace); 00146 and 00147 Phase 1; 00128 Phase 2; 00045 Phase 1 gate; 00038 DECISION.md; 00030 and 00031 go/no-go.
5. Issue #94: agree the PR QA and merge approach for desktop-only merging (the issue asks for exactly that decision).
6. Hardware: 00104 (laptop and dock), 00056 (DisplayLink dock), 00143 (pointer tuning).
7. Fresh-machine tests: 00053 (fresh F44), 00065 (fresh Cloud Base), 00061, 00073.
8. Triage the dormant/stale list (section 5) and say cancel, dormant or continue.

## 3. Quick wins

- Close effectively-done plans after a one-line check: 00165, 00160, 00115, 00114, 00116, 00118, 00119, 00049 (3 left of 75), 00032 (accepted on host, 1 task left), 00063 (in production use).
- Plan 00162 has no Status header at all; add or close.
- Issue #93: QA is red on the default branch (a gitleaks fixture in Plan 00161's test_u01_probe.py, and root-only agent_bus tests). Small fix, unblocks every PR going green. Check whether the teams work has already fixed it. No plan owns it.
- Issue #96 is covered by Plan 00171 (session-tokened signals): close the issue when 00171 is accepted.
- Issue #100 has an owner decision already (emoji, not SVG): tiny stt panel change; no plan.
- Issue #63 (ouch sha256 pin): small playbook edit; could ride the next `update-versions` pass.
- 00093 (7 tasks, version gate covers two of eight files) is small and a correctness gap.
- 00129 + 00131 (semgrep coverage) are a matched pair of small QA fixes.

## 4. Recommendations: top 5 after the teams work

1. Host acceptance sweep of the built-but-undeployed stt work (00171, 00164, 00156, 00148 checks). Highest daily value, code is done, risk is low; it also retires issue #96.
2. Issue #93, make QA green on the default branch (and give it a plan). Every PR is blocked or noisy until then; small and low risk. Pair with the #94 decision.
3. 00135 plus issues #69, #86, #87, #88 (ccy sessions survive a reboot, with restored sessions needing no human). 35 tasks done, owner is already living with unattended self-update reboots, so the remaining gaps (restore sends `continue`, verify-restore false OK) hurt on every reboot.
4. 00153 release tags: Phase 3 is merged, only host acceptance and the first tag remain; unlocks the self-update following tags (also 00137).
5. 00174 then 00155 (polyglot tooling folders, then the headset button mapper): 00174 is a prerequisite for 00155's Rust mapper, 00155's design is settled, and 00172 (headset mic) can proceed in parallel since triage is done.

Runners-up: 00167 (cc outside a repo, 12 tasks, small, issue #90), 00168 (managed config drift, issue #91), 00169 Phase 3.

## 5. Housekeeping

Issues with a plan: #59 (00161), #91 (00168), #90 (00167), #87/#88/#69 (00135 area; check each plan mention), #44 (00135), #22 (00045), #17 (031), #96 (00171).
Issues with no plan found: #100, #98 (batch transcription of long recordings with VAD; stt, could attach to 00148 or a new plan), #97 (config repo working clone), #94, #93, #86, #84 (voxtype, no body, likely stale research), #83 (ccy read-only paths, generalises 00160), #82 (automated release flow, follow-on to 00153), #81 (tools menu, near 00147), #67 (ccy memory/swap limits), #63, #61 (ccy session max age), #60 (emoji glyph rendering), #55 (LXC claude sessions no reboot warning), #54 (pipx warnings), #52 (prompt shows up-to-date when behind), #49 (run.bash private plays), #48 (docker inside LXC; plan 00127 dormant likely covers it), #41, #31, #12, #11, #10, #9, #8, #7, #4, #3, #1 (old).
Old issues (before 2026-04) to review for closing, since earlier plans cite them or work has moved on: #1, #3, #4, #7, #8, #9, #10, #11, #12 (some are cited by Completed or older plans 00052, 00068, 00082, 00100; confirm those plans completed, then close).

Plans to resolve:

- Mark dormant or cancel (no signs of work, not started): 004 (114 tasks never started), 011 and 013 (duplicate-looking of 009), 018 (empty), 023, 028 (likely superseded by 031), 029, 034, 051, 026, 014 (overlaps 00148 model review), 007, 041.
- 009: decide whether claude-devtools continues; if yes keep one plan and cancel 011 and 013.
- Potential duplicates: 009/011/013; 028/031; 014 and 00148's model review; 00129/00131 could merge.
- Very large plan with stale activity: 00037 (IP but 6+ weeks idle), 00040, 00053.
- IP plans with no task changes since 08-24 (00086, 00088, 00089, 00041): mark dormant or finish.
- The plan README lists only 00148 through 00178 as active; ~70 plan folders are on disk but not in the README. Reconcile the README with the folders.
