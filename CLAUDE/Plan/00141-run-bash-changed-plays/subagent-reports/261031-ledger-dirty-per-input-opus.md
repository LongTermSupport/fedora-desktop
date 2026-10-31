# A dirty run is judged per input (Plan 00141 Task 5.1)

## Defect

A single stray untracked file under `CLAUDE/Plan/`, which no play reads, was enough to make
every play run record `dirty: true`. `changed_plays.judge` treated any dirty run as stale, so
24 plays read stale for ever, whatever commit they ran at.

## Fix

- `repo.dirty_state` replaces `repo.is_dirty`. It runs
  `git status --porcelain=v1 -z --no-renames --untracked-files=all` and returns the sorted
  paths, cut at 50 with a `truncated` flag. Any entry it cannot read raises, and the callback
  then marks the ledger BROKEN, as it does today.
- A record now carries `dirty_paths` and `dirty_paths_truncated`. `dirty` is kept, and
  `build_record` refuses a `dirty` value that disagrees with the paths. `SCHEMA` stays 1
  because the change only adds fields, so an old reader still reads a dirty run as dirty.
- `ledger.recorded_dirty_paths` is now the only code that reads `dirty`. It returns `()` for a
  clean run, the paths for a dirty run, and `None` ("cannot say") in four cases: a legacy
  record (`dirty: true` alone), a cut list, an unreadable list, or a non-bool flag.
- `judge` marks a play stale for dirtiness in two cases: a dirty path is one of its inputs
  (`affected_plays.affects`, the same set used for committed changes) or is the group_vars
  file; or the record cannot say. The `Judgement` gains a `dirty_inputs` field.
- The reason text has two forms:
  - `it last ran with uncommitted changes to its inputs: <paths>`
  - `...uncommitted changes, and the ledger does not name them all`
- Wording was updated in these places:
  - the health coverage sentence and the `freshness.py` state comments
  - `docs/playbooks.md`
  - the `DESIGN-play-ledger.md` field table, plus a new section
  - the run.bash warning, with run.bash bumped to 1.36.1 and a changelog entry

## Defence Before Fix

- The detector is `tests/helpers/play_ledger/test_dirty_judged_per_input.py`, an AST guard
  run by `qa-helper-tests`. I used a test instead of a semgrep rule because a new semgrep
  file would need a new Go gate. The guard scans `helpers/play_ledger`, `helpers/host_health`
  and `callback_plugins`, and fails on two things:

  - `bool(<... "status" ...>)`
  - a read of `record["dirty"]` or `.get("dirty")` outside `ledger.recorded_dirty_paths`

- The guard was red on the code before the fix, with three instances:

  - `repo.py:62`
  - `changed_plays.py:231`
  - `changed_plays.py:263`

  It is green after the fix. Its own fixtures prove that it finds both shapes and allows both
  the one reader and code that writes the key.

- I swept the rest by grep:

  - `health_refresh.py`, `collector.py` and `helpers/host_health` have no reader of `dirty`.
  - The panel (`extensions/.../health.js`) does not read `dirty`.
  - `run.bash`'s `play_batch_warn_dirty` uses whole-checkout status only for advice. Its text
    promised that every play would be offered again, so I reworded it.

## Host

Nothing is deployed. The callback, the judge and `host-health-collect.service` all run from
the checkout. Records from the last three days are legacy (`dirty: true` with no paths), so
those 24 plays stay stale until each one runs again under the new callback. A
`changed-plays` meta-deploy entry would clear them; I did not queue it, because that is the
coordinator's call.
