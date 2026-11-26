# Plan 00266 Task C.3 rework: the LXC set without sudo

Commit a6490eff5 on v2-smoke (ccy 3.124.106, container 2.84.105). The full design is in
[261126-task-c3.md](261126-task-c3.md); this file covers the rework only.

## Why

The first C.3 build gave the owner group a NOPASSWD rule for `teamchat-admin lxc check-running`. That failed two gates, and both were right. `installplay`'s
TestThePlayReadsNothingTheTargetLacks refused the template's `root_dir`. `qa-ansible`'s
sudoers-rule refused a rule granted to a group. Moving the rule to `user_login` would fail the
first gate again, so no sudoers form passes both.

## What changed

- **New status file.** `lxcset` defines `/var/lib/teamchat-status/lxc-set.json`: root, mode
  0640, group the owner group, written atomically. It holds the boot id, the time, and the
  UP / DOWN / UNRECORDED / NO-RECORD lines.
- **Written by the boot unit's verbs.** `lxc.WriteSetStatus` ends `lxc boot` and
  `lxc reconcile`, whichever one the boot unit runs.
- **Read by verify-restore without privilege.** It reads the file as the user. With no file it
  reports `lxc NO-STATUS`, and with a file from another boot `lxc STALE`. Both fail, and
  `--wait` looks again while either is the case.
- **sudo removed.** The `check-running` verb and the `.j2` file are deleted.
  `/etc/sudoers.d/teamchat-lxc-check-running` is removed unconditionally.
- **Boot id rather than btime.** The status is dated by boot id, not by `/proc/stat` btime.
  btime moves when the clock is stepped (NTP at boot), which could make a fresh file look
  stale.

## Found while dogfooding (not fixed here)

- `scripts/test-ccy-compose-restore.bash` fails 17 of 90 checks, with the same 17 on a clean
  HEAD worktree (848da2c8c). This change does not cause them. They are in the `--compose start`
  restore paths, which Phase A's compose approval (d7ce117bf) touches. Owner: p266-A2 or the
  coordinator. It still needs logging.
- The conflict-marker commit gate cannot check a commit made through `GIT_INDEX_FILE`, which
  the shared-worktree temp-index commit rule requires. Every such commit goes unchecked for
  markers.
