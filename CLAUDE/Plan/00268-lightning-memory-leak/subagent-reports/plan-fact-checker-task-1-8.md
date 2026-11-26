# Plan fact check: 00268 Task 1.8 (and the W1-W13 edit)

Scope: the claims the diff adds or changes. Judged against the current tree only.

## REFUTED (most consequential first)

**R1. "It extends Task 1.7's watch, which already ... writes a dated record beside its log."**
The watch writes one record file, `lightning-watchdog.log`, beside the diagnostic log
(`src-go/internal/lightning/watch/watch.go:69-70`, written at line 189). It is one capped file
that rolls once to `.1` (W7, lines 32-33). It is not a dated record and not a folder. Entries
carry an RFC3339 stamp inside the file (line 326). Task 1.8's "one dated folder" is therefore new
design, not an extension of an existing mechanism. Change the sentence to "writes one record file
beside its log; Task 1.8 adds a dated capture folder".

**R2. "The laptop's baseline is about 650 MB, growing about 12 MiB/h (journal 101655Z)."**
The journal (`JOURNAL/2026-11-26/101655Z-finding-1faab8.md`, lines 8-10) gives 635 to 648 MB at
12.1 MiB/h for the busier team only. The other team is about 410 MB at 1.8 MiB/h. "Baseline"
holds for one instance, not for the laptop. A trip line sized from 650 MB would sit far above
the quieter instance. Say "the busier team's instance" and give the second figure.

## Unverifiable here

**U1.** "`flatpak run` moves the app into its own `app-flatpak-<id>-<pid>.scope`, so a limit on
a wrapping scope may not follow it." This is flatpak/systemd behaviour, and the plan itself marks
it as evidence to gather. No repository file settles it.

**U2.** "`systemctl --user set-property` ... or from the launcher if a launch-time property
holds." This is systemd behaviour. Nothing in the repo sets a MemoryMax on a flatpak scope
(grep of `playbooks`, `files` and `src-go` finds MemoryMax only in the agents slice, the
container limits and the rclone play).

**U3.** "`memory.events` high, or `memory.current` past the line" as trip signals. Kernel cgroup
v2 semantics, not checkable from the repo. Note that `memory.events` `high` counts reclaim
pressure, not "past a line the watch set", so this is worth checking on a host.

**U4.** "SIGABRT, so systemd-coredump keeps a core and a backtrace", and "whether the core is
symbolised depends on Lightning's Flatpak debug extension". Host behaviour. The repo has no
coredump configuration for this, and `RESEARCH.md` has no debug-extension finding. The only
coredump hits in the tree are unrelated (the ccy limits code).

**U5.** "In the batch with 00266's C.2 (batch 24)", and "after batch 23 lands". 00266 has a
Task C.2 (`PLAN.md:151`, status in progress, `teamchat-admin lxc reconcile`). Batch 24 appears
in the repo only in 00265 (`PLAN.md:106`, "it follows batch 24"). No file says that C.2 is in
batch 24. Batch 23 does exist as a 00265 batch (`subagent-reports/261126-qa-reviewer-batch23.md`).
Confirm the batch assignment with the coordinator.

## Verified

| #   | Claim                                                                                              | Verdict               | Evidence                                                                                                                                                                                                     |
| --- | -------------------------------------------------------------------------------------------------- | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | The watch is `internal/lightning/watch`, W1-W13 (changed from W1-W12)                              | VERIFIED              | `watch.go:43` defines W13; `watch_test.go:453` has `TestW13...`; W1-W13 are all in the header                                                                                                                |
| 2   | The watch is L17                                                                                   | VERIFIED              | `internal/lightning/lightning.go:58`                                                                                                                                                                         |
| 3   | The user service `agent-bus-lightning-watch` is run by the play only while a team has the key true | VERIFIED              | `playbooks/imports/optional/common/play-agent-bus-element.yml:23-24, 68-70, 291, 337-341` (enabled/stopped on `agent_bus_lightning_diagnostics_on`)                                                          |
| 4   | The watch "already finds the instance's process"                                                   | VERIFIED              | W4, matches by comm, argv and `--log-file` path (`watch.go:25-27`)                                                                                                                                           |
| 5   | The team's key lives in host_vars                                                                  | VERIFIED (key exists) | `environment/localhost/host_vars/localhost.yml.dist:252-257`. The existing key is a boolean `lightning_diagnostics`. The limit and trip values are new keys, a plan intention                                |
| 6   | No ptrace is needed, so ptrace_scope stays 1 (D3)                                                  | VERIFIED              | D3 at `PLAN.md:165` ("No: it guards the become password"). `play-self-update.yml` and `play-agent-bus-element.yml` both mention `ptrace_scope`. SIGABRT needs no ptrace (kernel semantics, not repo-checked) |
| 7   | Owner's Q20 follow-up quote                                                                        | Not checkable         | Verbatim owner words. Task 1.5 and `Q20-answer.md` exist                                                                                                                                                     |
| 8   | Journal entry 101655Z exists                                                                       | VERIFIED              | `JOURNAL/2026-11-26/101655Z-finding-1faab8.md`                                                                                                                                                               |
| 9   | 00266 has a C.2                                                                                    | VERIFIED              | `CLAUDE/Plan/00266-seat-restart-after-reboot/PLAN.md:151`                                                                                                                                                    |
