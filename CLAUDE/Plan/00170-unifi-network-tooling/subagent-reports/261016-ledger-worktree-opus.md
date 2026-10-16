# Play ledger: a play outside the repo root no longer marks BROKEN

Branch `fix/play-ledger-outside-root` (from `origin/F44` 0c61144d), commit a15a42c5, pushed.

## The fix

- `helpers/play_ledger/collector.py`: `_resolve` returns the bare absolute path for a play
  outside the root, and `on_play_start` opens no record for it and returns one line,
  `LEDGER-PLAY-NOT-TRACKED: play '<path>' is outside the repo root '<root>', so it is not tracked and the ledger records nothing for it`. The previous play is still closed at that
  boundary, and results that follow fold into nothing.
- `callback_plugins/play_ledger.py`: prints that line to stderr; DOCUMENTATION says so.
- Narrow: no `except` was added or widened. Only the prefix test changed from raise to
  return; any exception from `realpath`, or from hashing the play, still reaches the
  callback's existing handler and marks BROKEN.

## Tests

- `test_collector.py`: the refused case is now skipped (with the notice's content checked);
  a tracked play gets no notice; `/repo-other` is outside `/repo`; a skipped play closes the
  previous one and takes none of its results; an `untracked/` play inside the root is still
  recorded; an `OSError(ELOOP)` and a `ValueError` from `realpath` still raise.
- `test_source_position_against_real_ansible.py`: a real `ansible-playbook` run, from the
  repo, of a play in a temp dir exits 0, prints the notice exactly once, and writes no
  sentinel and no `LEDGER-WRITE-FAILED`.
- Red before the fix (3 collector errors and the integration test); green after. The
  play_ledger suite run by module name, and `qa-changed.bash`, both pass. These are stdlib
  `unittest` (helpers/CLAUDE.md forbids pytest). qa-all was not run, as instructed.

## Main-checkout decision

A private play under the main checkout's `untracked/` is **in scope**: it is inside the
root, so commit, dirty and play_sha256 describe it, and `check_freshness._drop_never_tracked`
(and `changed_plays`, which counts only `playbooks/imports/`) keeps it out of every report.
The boundary stays the root rather than git tracking: a git call per play would add a
failure mode, and would also skip a new, not yet committed play, which `dirty` and
`play_sha256` exist to catch. The ledger content differs between the two cases (a row vs
none) but the reports agree: neither is ever reported. Recorded in the 26-10-16 journal and
a test.

## Clearing the marker

`meta-deploy.list` accepts only a plan folder, a `playbooks/*.yml` or
`<plan folder>/<script>.bash`, not a bare command. So
`CLAUDE/Plan/00170-unifi-network-tooling/clear-ledger-broken.bash` (planlib, host-only,
`--check` clears nothing) runs `check_freshness --clear-broken` from the repo root, and is
queued with a comment saying why it is safe. `meta-deploy.bash --list` accepts it;
`test-meta-deploy.bash` passes. Plan 00170 Task 1.13 added (in progress until the host run).

Owner: I have updated meta-deploy; run `./CLAUDE/Plan/meta-deploy.bash`. Its new last entry
clears the play ledger's BROKEN marker.
