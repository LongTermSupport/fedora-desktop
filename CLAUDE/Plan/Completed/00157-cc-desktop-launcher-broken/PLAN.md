# Plan 00157: cc desktop launcher broken

**Status**: Complete (2026-10-05)
**Created**: 2026-10-05
**Owner**: joseph
**Priority**: High

## Overview

`cc`, the host Claude Code launcher (`/var/local/claude-code/cc`, deployed by
`play-claude-yolo.yml` and aliased in `~/.bashrc`), stopped working: typing `cc` reported
that no Claude process was found (the owner's wording, by dictation) instead of starting a
session.

Closed once `cc` worked again: the owner judged a further triage of the cause pointless.
Nothing in `cc` or its libraries was changed. The likely cause (H5) was not confirmed.

## Facts

| ID  | Fact                                                                                                                                                                                        | Source                                                   |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| F1  | No tracked or ignored file in the repo contains "claude process found" (case-insensitive)                                                                                                   | `rg --no-ignore` in the CCY container                    |
| F2  | `cc` is `alias cc='/var/local/claude-code/cc'` in the managed block of `~/.bashrc`                                                                                                          | `play-claude-yolo.yml`                                   |
| F3  | The text is in none of: the deployed `cc`, its libs, `~/.local/bin`, the startup files, host claude settings and hooks, the claude 2.1.289 binary                                           | host triage, run 1                                       |
| F4  | In a terminal, `cc` is the alias; deployed `cc` and libs equal this checkout and parse; `claude --version` works; no parked credential; onboarding flag set; every tool cc needs is present | host triage, run 1                                       |
| F5  | The boot restore started the ccy tmux server with `cc-fedora-desktop` running `cc --continue`; by the triage that session no longer existed                                                 | host triage, run 1 (processes, tmux sessions)            |
| F6  | claude 2.1.289 contains "No conversation found to continue" and "No Claude Code sessions found"; no string ends in "process found"                                                          | `grep` of the identical-size binary in the CCY container |
| F7  | After the restored `cc-fedora-desktop` session had gone, a fresh `cc` started normally                                                                                                      | owner                                                    |
| F8  | The first triage run hung: its `bash -ic` probe was stopped (state T, `/dev/tty` open) and the triage blocked reading its pipe, with SIGINT blocked, so Ctrl-C did nothing                  | `reap-stuck-triage.bash` record of that run              |

## Hypotheses

| ID  | Hypothesis                                                                                                                                                                                                         | Status                                                      |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------- |
| H1  | The claude binary prints the message word for word                                                                                                                                                                 | Refuted by F3                                               |
| H2  | `cc` no longer resolves to the wrapper (a function or other binary shadows it)                                                                                                                                     | Refuted by F4                                               |
| H3  | The deployed wrapper or a library differs from this checkout, or fails to parse                                                                                                                                    | Refuted by F4                                               |
| H4  | Credential state cc refuses to walk past (parked backup beside a live file)                                                                                                                                        | Refuted by F4                                               |
| H5  | The words were dictated: the message is claude's "No conversation found to continue", from the restored `cc --continue` in a directory with no conversation; the held session is what the next `cc` re-attached to | Not confirmed; consistent with F5 to F7. Left open at close |

If it recurs after a reboot, `triage.bash` (with its boot-restore probes) is the first thing
to run, and H5 the first thing to check.

## Tasks

### Phase 1: Triage

- [x] ✅ **Task 1.1**: Write `triage.bash`: read-only by default; `--trace` additionally runs
  `bash -x cc --version` under `script`, with token values redacted before the trace is written
- [x] ✅ **Task 1.1a**: The interactive-shell probe runs detached, into a file, killed at its
  limit (F8); `reap-stuck-triage.bash` recorded and killed the hung run.
- [x] ✅ **Task 1.2**: Owner runs the triage on the host through `meta-deploy.bash`
- [x] ✅ **Task 1.3**: Record the facts the report establishes; settle H1 to H4 (F3 to F6).
- [x] ✅ **Task 1.4**: Boot-restore probes added to `triage.bash`; not run, because `cc` works
  again (F7) and the owner closed the investigation.

### Phase 2: Fix

- [x] ✅ **Task 2.1**: No fix: nothing found broken in what this repo deploys.

## Success Criteria

- [x] `cc` in a fresh terminal starts a Claude Code session (owner, F7)

## Delivery & Milestones

- Triage `0bdd926c`, `c108e690`; hung-run reaper `6bfa29fa`
