# Task 4.4: the wider ready-wait rule (handing-off starts, helper liveness)

Builder: Opus 5.5. Nothing committed; all changes are in the working tree.

## What was built

`helpers/ready_wait/bash_ready_waits.py`, rule `ready-wait-ignores-child-exit`:

- **Manager starts arm a scope**, as `cmd &` does. `_MANAGER_START` matches
  `lxc-start`, `virt-install`, and `virsh ... start` (options allowed between) only in
  command position: after a line start, ``` ; & | ( ! { \``, or  ```if/then/do/else/elif/while/until`, optionally behind `!`, `exec`, `nohup`, `sudo [-flags]`or`VAR=value`prefixes. So`have_tool virt-install`, `command -v lxc-start`, a `for ... in lxc-start`list and`virsh autostart`are not starts. A start inside`$(...)`is a start. A function that runs a manager start is a launcher, so calling it arms the caller (no`$!`needed: the guest's name is the handle). This is what reaches`vmtest`, where `cmd_build_base`calls`build_seed_fast`/`build_install_full`and then`wait_for_ssh\`.
- **The manager's state probe is a liveness check**, added to `_LIVENESS`: `lxc-info`
  with a state flag (`-s`, `-sH`, `--state`; `-iH` alone is not), `lxc-ls --running` /
  `--active`, `virsh ... domstate`, `virsh ... list`. Derived from the hand fixes:
  #6 `docker-in-lxc` reads `lxc-info -n "$name" -sH`, #7 `vmtest` reads
  `virsh -c ... domstate` inside `guest_must_be_up`. One shared set: a `&` wait cleared
  by a `virsh domstate` is accepted too (kept simple; no repo case needs the split).
- **One level of function following.** A loop is cleared when its header or body calls,
  by name, a function whose own body holds a liveness check (`checkers` in
  `findings`, passed to `_unguarded_polls`). Only one level: a check made through a
  further function is not seen (pinned by `test_only_one_level_of_helper_is_followed`).

Known limit: a `virsh ... start` or `lxc-info ... -s` split over a `\` line
continuation is not matched (the match stays on one line).

## Tests (TDD)

`tests/helpers/ready_wait/test_bash_ready_waits.py`: new classes `ManagerStarts` (9) and
`LivenessInAHelper` (4). Run before the code: 8 failed (every case that must report,
plus the helper-follow cases); after: all 41 pass. Fixture `.semgrep/ready-wait.bash`
gained a manager-start finding (`container_up`, marked `ruleid`) and a cleared wait
through `guest_must_be_up`; the HEAD helper reports neither marked line on it, so the
self-test proves the new reach. Controls: the pre-fix copies of #6 and #7 (parent of
8965aa3b) are red under the new rule (docker-in-lxc 345; vmtest 487 and 932); the fixed
copies stay green.

## Gate

`scripts/qa-ready-wait-rules.bash`: 280 Python + 401 shell files, 0 findings after the
two fixes below. `ruff check` clean; `shellcheck` clean on every touched shell file.
Full `qa-all.bash` not run (sub-agent; the coordinator runs it).

## New instances found and fixed

1. `files/home/.local/bin/vmtest:951` (`session_provision`): waited up to
   `PROVISION_TIMEOUT_SECONDS` for the session runner's `run.exit` over SSH and never
   asked whether the guest was still up. Fixed: `guest_must_be_up "run.bash in the session"` in the loop condition, as `wait_for_ssh` does. No test exercises
   `session_provision`; verified by the gate and shellcheck only.
2. `CLAUDE/Plan/00161-agent-team-bus-matrix/_acceptance-u23.inc.bash:351`
   (`u23_prepare_lxc`): after `lxc-start` and `lxc-wait -s RUNNING`, polled
   `lxc-info -iH` for 60 s without asking whether the container still ran. Fixed: each
   try reads `lxc-info -sH` and fails with the state when it is not RUNNING. This file
   belongs to Plan 00161 (active); the coordinator may want to note it there.

## Docs

`CLAUDE/QA.md` `ready-wait-ignores-child-exit`: the Bash bullet describes the new
arming, liveness forms and the one-level follow; the "where no rule reaches" entry now
covers only handing-off starts the rule does not name (`podman run -d`,
`systemctl start`, a `--daemon` flag).
