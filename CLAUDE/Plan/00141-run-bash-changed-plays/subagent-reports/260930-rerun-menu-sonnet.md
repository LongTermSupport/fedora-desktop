# Report: `run.bash --rerun` and the one-row panel (Plan 00141, Phase 4)

## What was built

- `helpers/play_ledger/changed_plays.py --all`: prints every play run here as
  `PLAY <state> <play>` (states `stale`, `failed`, `unresolved`, `current`) plus `GONE`
  lines, in `--changed`'s run order. Same judgement as `--changed`, not a copy.
- `run.bash --rerun` (version 1.29.0): numbered menu, `*` marks stale or failed plays.
  Answers: `3`, `1 4`, `1,4`, `a` (every `*` play), `q`. Strict validation, three tries,
  EOF cancels (exit 1), `q` exits 0. The pick runs in menu order under the play lock and
  stops at the first failure. `--changed` and `--rerun` now share one preflight, dirty
  warning and run loop (`play_batch_*` functions). Refused with a playbook path,
  `--optional-only`, `--headless`, `--changed`. In `--help`.
- `fedora-desktop-health --rerun`: runs the checkout's `run.bash --rerun` (the panel does
  not know the checkout path; this command does).
- Panel `sections/plays.js`: one row, "Re-run a play…", "(N changed)" when any play is not
  `fresh`; launches `fedora-desktop-health --rerun --hold` through the existing
  `terminal.js` (async). `documentSections` stays `[]`, so the icon is unchanged.
- Docs: `docs/playbooks.md`, `docs/run-bash-changelog.md`, `CLAUDE/QA.md`, pointer note in
  Plan 00109 `DESIGN-panel.md`. Plan 00141 Phase 4 tasks and a 26-09-30 journal.

## Findings

- `run.bash` narrows `IFS` (no space), so `read -ra` did not split "4 1". Fixed with
  `IFS=$' \t' read`. Covered by a test.

## Verification (run in the worktree)

- `python3 -m unittest tests.helpers.play_ledger.test_changed_plays`: 40 OK (8 new).
- `scripts/test-run-bash-rerun.bash`: 60 pass, 0 fail (new), wired into `qa-all.bash` as
  gate `run-bash-rerun`.
- `scripts/test-run-bash-changed.bash`: 36 pass. `scripts/test-run-bash-single-play.bash`:
  51 pass. `scripts/test-fedora-desktop-health.bash`: 48 pass (new `--rerun` cases).
- `scripts/test-panel-sections.bash`: pass (126 tests earlier run, 0 failing after the fix
  to `test-panel-indicator.mjs`).
- ESLint: clean, run as `cd extensions && ../../../../extensions/node_modules/.bin/eslint .`
  because the worktree has no `extensions/node_modules`.
- Run separately: `qa-docs.bash` OK; `qa-helper-tests.bash` 2258 tests OK (1 skipped).
- `qa-all.bash` in the worktree: toolchain, bash (338 files), python, patterns and ansible
  stages pass. It did NOT complete: `ansible-syntax` fails 82/82 because the worktree has no
  `vault-pass.secret`, and the `js` gate aborts (exit 2) because the worktree has no
  `extensions/node_modules`. Both are gitignored files absent from a fresh worktree, not
  caused by this change. The gates after `js` (including `run-bash-rerun`) were therefore
  run by hand as listed above. The coordinator should run the full `qa-all.bash` from the
  main checkout after the merge. `ccy-relabel-preflight` and deployed-drift were not reached.

## Files changed

- `helpers/play_ledger/changed_plays.py`, `tests/helpers/play_ledger/test_changed_plays.py`
- `run.bash`, `scripts/test-run-bash-rerun.bash` (new), `scripts/qa-all.bash`
- `files/home/.local/bin/fedora-desktop-health.j2`, `scripts/test-fedora-desktop-health.bash`
- `extensions/fedora-desktop@fedora-desktop/sections/plays.js`,
  `tests/extensions/test-panel-sections.mjs`, `tests/extensions/test-panel-indicator.mjs`
- `docs/playbooks.md`, `docs/run-bash-changelog.md`, `CLAUDE/QA.md`
- `CLAUDE/Plan/00109-…/DESIGN-panel.md`, `CLAUDE/Plan/00141-…/PLAN.md`,
  `CLAUDE/Plan/00141-…/JOURNAL/00141-Journal-26-09-30.md`, this report

## Deploy

The panel is deployed by `playbooks/imports/optional/common/play-fedora-desktop-panel.yml`
(log out and in afterwards, Wayland). `fedora-desktop-health` is deployed by
`play-host-health-login-report.yml`, which must also run for the panel row to work.
`run.bash` needs no deploy. Nothing was deployed or run with Ansible here.
