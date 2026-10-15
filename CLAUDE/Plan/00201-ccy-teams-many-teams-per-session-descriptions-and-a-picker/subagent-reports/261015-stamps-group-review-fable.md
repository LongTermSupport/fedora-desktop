# Review: fix/teams-stamps-group (8b7f38f0) against F44 (0ce29bfd)

Adversarial pre-merge review, read-only. Verdict: **FIX-FIRST** (one blocking finding, a merge
conflict with F44; the fix itself is sound).

## What was checked

- The diff `F44...origin/fix/teams-stamps-group` (14 files) and the merged result in a scratch
  worktree of the branch: `scripts/qa-bash-limits.bash` (OK), the agent_bus Python tests and
  `test_acceptance_check.py` (137 passed), `scripts/test-agent-bus-install.bash` (all PASS,
  as root in this container).
- The branch's `StampGroupTest` run against F44's `helpers/agent_bus/teamfile.py`: 6 of 6 fail,
  so the tests would fail without the fix. The bash suite's "a run with no environment keeps the
  recorded group" is the regression reproduced; before the fix the second run put the directory
  back in the runner's primary group.
- `git merge-tree` of the branch against F44 and against `origin/fix/teams-final-review`.

## Blocking

1. **The branch conflicts with F44.** F44 moved after the branch forked (cfb93c38): e19f8369
   appended a 20:34 journal entry to
   `CLAUDE/Plan/00161-agent-team-bus-matrix/JOURNAL/00161-Journal-26-10-15.md`, and dbf0c793
   replaced the queue in `CLAUDE/Plan/meta-deploy.list`. `git merge-tree F44 origin/fix/teams-stamps-group` reports content conflicts in exactly those two files;
   `playbooks/imports/play-agent-bus.yml` and `docs/agent-bus.md` auto-merge cleanly.
   `origin/fix/teams-final-review` is already in F44 (merge 0ce29bfd; `F44..final-review` is
   empty), so "alongside" is moot: the branch merges cleanly with final-review on its own, the
   conflict is with F44 itself. Resolution: merge F44 into the branch; keep both journal
   entries in time order (F44's 20:34 host-run record, then the branch's 20:44 finding, which
   it refers to); in meta-deploy.list keep F44's `play-claude-yolo.yml` entry, one
   `play-agent-bus.yml` entry whose comment carries both the final-review text (the
   config-render backup gate) and the stamp-group text, then the `00161-...` and `00201-...`
   entries from the branch.

## Should fix (not blocking)

2. **The record is written only when a team is declared present.** In play-agent-bus.yml the
   `/etc/agent-bus` directory and `stamp-group` tasks sit inside the block gated by
   `agent_bus_apply_present | length > 0 and not ansible_check_mode`. A host with the software
   installed and no present team (a fresh host before its first team, a host whose teams are all
   absent) never gets the record, and every `agent-bus-install team` run outside the play there
   (00161 `deploy.bash`/`acceptance.bash` throwaway teams, 00201 M7, a hand run) fails with 78.
   The failure is loud and names the file and the play, so it is fail-fast, but it is an IaC gap
   of the same shape as the original one. Move the two getent lookups and the two record tasks
   next to "Install the agent-bus homeserver software", unconditional. On the owner's host, which
   has present teams, the current placement works.
3. **Statement packing to hold the ratchet.** `SVC_OWNER=$ROOT_OWNER SVC_GROUP=$ROOT_GROUP` (two
   places) and `((PUT_CHANGED == 0)) || restart=1 reload=1` exist only so the allowlist row
   could read 991/62/1157 rather than grow. The gate is satisfied and the row did shrink, but
   packing is what the bash-limits rule says the statement count exists to stop; the honest
   outcome is a row that grows by the four lines the fix needs, or a port.

## Verified correct

- **Root cause closed on every path.** `cmd_team` reads the record through `agent-bus render stamp-group` and `ensure_dir`s `/var/lib/agent-bus-applied` 0750 root:<group> before
  `require_running` and `make_work_dir`, so a 78 changes nothing, and an existing directory in
  the wrong group is chowned back (`ensure_dir` corrects mode and owner when they differ; the
  bash suite's "a directory reset to another group is healed" covers it). `AGENT_BUS_STAMP_GROUP`
  survives nowhere but as a negative check in the test. `ccy-teams apply` runs `sudo -n /usr/local/sbin/agent-bus-install team` (src-go/internal/sudo), environment stripped, so it
  reads the same record. `remove` and `restore` only `unstamp` (delete `<team>.json`); neither
  touches the directory's group. The plan scripts call the same installer with no environment,
  so they keep the group (subject to finding 2).
- **Existing hosts heal.** The play writes the record then runs `team` for every present team,
  so the next `play-agent-bus.yml` run puts the directory back in the desktop user's group; no
  homeserver restarts unless config or software changed, as meta-deploy.list says.
- **Validation.** `read_stamp_group`: `lstat` (a symlink is "not a regular file"), owner uid
  must be `--owner`'s and `mode & 0o022 == 0`, exactly one non-empty line, `grp.getgrnam`. The
  group is then only ever an existing group's name, passed to `install -g`/`chown -- owner:group`
  after `--`, so it cannot be an option. A non-root user cannot influence it: the record is
  root 0644 in root 0755 `/etc/agent-bus`; a non-root user can neither write it nor place a
  root-owned regular file there, and with Fedora's `fs.protected_hardlinks=1` cannot hardlink a
  root file in. The `lstat`-then-`read_text` window is a theoretical TOCTOU that needs write
  access to `/etc/agent-bus`, i.e. root; `os.open(O_NOFOLLOW|O_NOCTTY)` + `fstat` would close
  it for free, worth doing when the file is next touched. The parent directory's mode is not
  checked; same reasoning, same remedy. The stamps carry no secret (team name and digests), so
  the exposure a wrong group gives is small in any case.
- **Failure mode.** Missing, symlink, wrong owner, writable, empty, multi-line, unknown group
  all exit 78 (`EXIT_CONFIG`) with `<path> (play-agent-bus.yml records the stamps' group there) ...` on stderr; `set -e` propagates the substitution's status; bootstrap does not run
  (test "before the team is bootstrapped").
- **Play task ordering.** Backup gate, software, sudoers, LXC absent, remove absent, then the
  present-teams block: render, getent user, getent group, mkdir `/etc/agent-bus`, record, team
  loop, `always` cleanup, LXC present. The record precedes every `team`; `remove` needs none.
- **M1.2.** `helpers/pingbus/cli.py:282` prints `say --to {name} --re '{event_id}'`; the check
  now wants exactly that with the HUMAN/PEER line's own event, and a test rejects both the
  missing `--re` and a foreign event.
- **Allowlist** only shrank (995/62/1158 to 991/62/1157); the gate passes on the branch.
- **No install-specific names** in the diff; the journal entry uses "the host".
- **docs/agent-bus.md** manual root route gains the `install -D -m 0644 /dev/stdin` line, a
  working idiom, placed between `software` and `team` where it must run.

## Notes

- The bash test finds a second group from `id -Gn`, or from `/etc/group` when root; a non-root
  runner with a single group fails "this test has a second group to record". CI runners and the
  container are fine; recorded so a future failure is read correctly.
- The journal entry at 20:44 was written in the plan's grammar; the F44 20:34 entry it will sit
  behind already says the fix is "being fixed on fix/teams-stamps-group", so the two read as
  a sequence once merged.
