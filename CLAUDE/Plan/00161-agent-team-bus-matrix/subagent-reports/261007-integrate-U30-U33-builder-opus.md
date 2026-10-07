# Integrate U30+U33 onto `seats-integration`

## What was merged

- `origin/seats/U30` (U30 seat claim, status and hooks), with `git merge --no-ff`, clean.
- `origin/seats/U33` (U33 seat history), with `git merge --no-ff`, four conflicts.

The local `seats-integration` branch was checked out in another worktree, so the merge was
built on a local branch `integrate-U30-U33` started from `origin/seats-integration` and
pushed to `origin/seats-integration` (a fast-forward of the remote).

## Conflicts and how each was resolved

| File                       | Conflict                                                | Resolution                                                                                                                                                                                                                                 |
| -------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| DESIGN.md decisions        | both units appended a row numbered D52                  | U30's D52-D54 keep their numbers (U30 merged first and its rows are referenced by its journal entry and PLAN line); U33's row is now D55 with a note that it was D52 on its branch. PLAN.md's U33 text and the U33 report's heading follow |
| JOURNAL day-file           | both appended an entry at the same place                | both kept unchanged through a union merge (`core.attributesFile` naming `merge=union` for journal files, for this one merge only); the U33 entry's "D52" means D55, which the integration journal entry says                               |
| docs/agent-bus-protocol.md | §13 command table and §15 line table re-aligned by both | U30's side kept; U33's `history` row, its prose paragraph, the `HISTORY` row of §15's table and its examples re-applied                                                                                                                    |
| SKILL.md                   | both added a section after "Looking without consuming"  | U30's "Seats" first, then U33's "Your history"; the "Taking over a seat" bullet now names `pingbus history` (below)                                                                                                                        |

## The one change beyond the two units' text

U30 left its "Taking over a seat" bullet saying "with the command it names" because the
skill's contract test parses every `pingbus` invocation and `history` did not yet exist (its
D54 says the skill names `pingbus history` once U33 adds it). With U33 in, the bullet says
`pingbus history` and points at the history section. The contract test passes with it.

No new decision row beyond the renumbering; no code change was needed: `cli.py` and
`test_plugin_contract.py` auto-merged and every test passes.

## Verification (on the merged tree)

- All 18 `tests.helpers.pingbus` modules, by module name: 904 tests, OK (covers both units'
  test files: `test_seat`, `test_cli_status`, `test_config`, `test_hooks`, `test_inbox`,
  `test_cli`, `test_cli_history`, `test_plugin_contract`, `test_protocol_doc`, and the rest).
- All 8 `tests.helpers.agent_bus` modules: 226 tests, OK.
- The plan folder's five Python test modules: 231 tests, OK.
- `ruff check helpers/pingbus tests/helpers/pingbus` (ruff 0.16.8): clean.
- Shellcheck: neither unit touched a bash file, so nothing to check.

## Notes for the coordinator

- No CCY change in either unit, so no version bump.
- No host run: U31 brings `seat exec` into the image and U20 exercises both units.
