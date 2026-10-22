# Plan 00214 review, round 3 (adversarial, Opus)

Scope: the whole branch, `git diff F44...HEAD` on `feat/update-all-changed-plays` (6 commits,
tip f00ca8b1c), read against issue #110 and its owner comment, PLAN.md, the project rules and
rounds 1 and 2.

## What I ran

- `python3 -m pytest -q tests/helpers/play_ledger tests/helpers/host_health tests/helpers/self_update`:
  985 passed, 1 skipped.
- `node --test tests/extensions/test-panel-sections.mjs`: pass 56, fail 0.
- `bash scripts/test-meta-deploy.bash`: passed 26, including the 5 new `changed-plays` cases.
- `./scripts/qa-changed.bash --base F44`: every check green (toolchain, bash-syntax,
  shellcheck, semgrep, ruff, unit-tests, docs, go, bash-limits, plan-script-logging, node,
  eslint) and every suite passed; exit 0. The one "not covered" file
  (`tests/helpers/pingbus/test_identifier_grammar.py`) is F44's, not this branch's. Its file list
  includes F44's 9 newer commits (the 00211 merge), because `--base` diffs F44 against the
  tree, not against the merge base.
- `git merge-tree --write-tree F44 HEAD`: merges cleanly. In the merged list the 00214 entry is
  still last, with no `changed-plays` above it, and the allowlist record is 161/180.
- `git grep` outside `CLAUDE/Plan/` and `untracked/` for `_drop_never_tracked`, `changes_since`,
  `play_sha256_at_head`, `plays_to_query`, `UNEXPLAINED`/`unexplained`, `freshness.classify`,
  `freshness.retire`, "no longer exists at HEAD", "own file", "never tracked", "watches only",
  "against HEAD", "not fresh", and the panel and state vocabulary (`'fresh'`, `'stale'`).

## Round 2's F1: fixed

`tests/helpers/self_update/test_affected_plays.py:3-4` and `tests/helpers/play_ledger/test_collector.py:105-107`
now match their modules. No deleted name (`_drop_never_tracked`, `changes_since`,
`play_sha256_at_head`, `freshness.classify`/`retire`, UNEXPLAINED) is left in live code, tests
or docs. Every other "own file" and "never tracked" hit is either correct for the new rule
(`changed_plays.py:5`, `freshness.py:26`, `docs/playbooks.md:56,959`, `run.bash:985`) or
unrelated (ccy.env.local, markdown files and so on).

## FIX-FIRST

### F1 (round 3). docs/playbooks.md still describes the panel's old count rule

- `docs/playbooks.md:1056-1057`: "The label carries a count when the freshness check has
  marked any play not fresh, for example "Re-run a play… (2 changed)"."
- Task 1.4 changed that rule. `plays.js:38-46` now counts every state except `fresh` and
  `unresolved`. So an `unresolved` play is "not fresh" but is not counted, and the test
  `the label counts the plays run.bash --changed runs` pins exactly that case (4 rows, 2
  counted). This is live user documentation stating the rule the branch replaced, which is
  the class rounds 1 and 2 made FIX-FIRST. It was missed because it is worded as "not fresh",
  not by any of the deleted names.
- Fix: for example, "The label carries a count of the plays `./run.bash --changed` would run
  (changed or failed), for example "Re-run a play… (2 changed)"; a play it cannot judge (a
  reference it cannot follow) is not counted."

## Checked and sound (independently of rounds 1 and 2)

1. **One judgement.** `check_freshness.run` → `changed_plays.judge` only. `_VERDICT_STATE`
   maps all four non-gone judge states. An unknown state is a loud KeyError, not a drop. GONE
   is checked first, and `states` and `gone` are disjoint, because a missing play `continue`s
   before any state is set. `Judgement.runs` is `order` filtered by `RUN_STATES`, which is the
   same set `run` prints as RUN. The successor rule is the same as before the refactor:
   failed stays failed, everything else becomes stale with `unresolved` popped, and a
   successor never ledgered becomes stale. `NoAnswer` wraps every failure inside the
   judgement, and `check_freshness` returns EXIT_UNTRUSTWORTHY with `judged` and `coverage`
   left empty.
2. **The real-repo agreement test** (`TestAgreesWithRunBashChanged`) covers a changed
   deployed file, a failed run, a dirty run, an uncommitted edit and a current play, through
   the real `changed_plays.run` and `check_freshness.run`. It compares ordered lists, so the
   order is tested too.
3. **Command (gap 1).** `run_changed_command` is keyed on the document's rows (states from
   `freshness`, written by `play_runner.runnable` from the same verdicts). `render` can only
   print it below a non-empty fault list, and the line is not counted by `item_counts`. The
   reminder appends it. The path comes from `realpath(__file__)` and is quoted with `shlex`.
   No real path is in the diff: the fixtures use `/home/me/...` and `/c` placeholders.
   `Owner: joseph` in PLAN.md follows the convention of 192 other plans.
4. **meta-deploy.** `changed-plays` is accepted only alone. It runs as `<repo>/run.bash --changed --yes` through the single-unit branch (it ends in `.bash`, but it is not under
   `PLAN_ROOT`, so it is not renamed), with its log at `run.bash.log`. An exit 2 is FAIL,
   because the unit is not named `*acceptance*.bash`. Playbook and script units get an empty
   `triageArgs`, which is safe under `set -u`. The test runs the real ordering through a
   fixture `run.bash`.
5. **Acceptance.** In `check-changed-plays.bash`, every `$(changed_plays)` is either
   unconditional under `set -e` or explicitly `|| exit 1`. `fail` exits 1. The `same` leg's
   awk labels match `_STATE_LABEL` byte for byte. The `report` leg's grep matches `render`'s
   line, and it also checks the negative case. `fedora-desktop-health` exits with
   `login_message`'s status, which is 0, so `printed=$(…)` cannot trip `set -e` on a host with
   findings. Exit 2 is reachable only when no leg failed.
6. **Stderr hygiene.** Diagnostics go to stderr, and stdout carries only findings and markers.
   The acceptance's `ok:` and `not judged:` lines are leg output that `plan_gather_leg`
   captures, and the failure reasons go to stderr.
7. **Bash limits.** The two new scripts are plan-folder scripts (exempt). `meta-deploy.bash`
   went down, 162/181 → 161/180.

## NOTE

- **N1. `play_sha256` is now written and validated but never read.** Its only reader was
  `freshness.classify` (F44 `freshness.py:89`), which is deleted. Three comments still justify
  the field or a check by that reader. `helpers/play_ledger/repo.py:58-60` calls it "exactly
  the case `play_sha256` was added to catch". `helpers/play_ledger/ledger.py:143-144` speaks of
  "the reason play_sha256 exists at all". `tests/helpers/play_ledger/test_repo.py:74` says "the
  case play_sha256 exists to catch". None of the three is false word for word. The `dirty`
  check they guard matters more now, because `judge` re-runs every dirty run. But they imply a
  consumer that no longer exists. Retiring the field is a ledger schema change and out of
  scope. A follow-up issue should either retire the field or say what it is kept for. It is
  not blocking, because no live text names a deleted function or claims a current behaviour
  that is false.
- **N2. `collector.py:136-137`** still says a play outside the root "cannot be compared
  against this repo's HEAD". That is the old frame: `judge` diffs the commit it ran from
  against the working tree. The conclusion (no row) still holds. Cosmetic.
- **N3. The coverage count.** `coverage_sentence` says "N plays run here", and N is
  `len(states) + len(gone)`. That includes a successor that has never run here (made STALE by
  `_retire`), so N can be one more than the plays that ran here. Also, "1 other ledgered plays
  are" and "1 of them hold" are not singular. The sentence is recorded in the document, not
  shown. Cosmetic.
- **N4. `expected_command`** (`check-changed-plays.bash:39-45`) reproduces `shlex.quote`
  only for a path without a `'`. A checkout path containing a single quote would fail the
  `report` leg spuriously. It is unlikely, and the failure is loud, not silent.
- **N5. The branch is 9 commits behind F44.** It merges cleanly and keeps its list position.
  It needs only the usual merge.
- Rounds 1 and 2's notes (the fetch feeding nothing, the startup-notice reading, the reminder
  race, `_RUN_STATES` against `NOT_RUN` duplication, the `--yes` headless wording, now
  addressed at `docs/playbooks.md:63-65`, and the `playbooks/dev/` row change, now in Task 1.3)
  are not re-raised. None of them is blocking.

VERDICT: FIX-FIRST
