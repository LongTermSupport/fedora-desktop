# Plan 00214 review, round 1 (adversarial, Opus)

Scope: `git diff F44...HEAD` on `feat/update-all-changed-plays` (4 commits, 6facb1a4c tip),
against issue #110 and its owner comment, PLAN.md, and the project rules.

## What I ran

- `./scripts/qa-changed.bash --base F44`: green (32 files; ruff, shellcheck, semgrep,
  bash-limits, eslint, unit tests, and the named suites, including test-meta-deploy (26),
  test-run-bash-changed (45), test-run-bash-rerun (60), test-fedora-desktop-health (48),
  test-host-health-login-snippet (16), test-panel-sections (317)).
- Unit modules by hand: test_check_freshness (47), test_changed_plays (40), test_freshness,
  test_git_history, test_login_message (77), test_login_report (62), test_play_runner,
  test_collector: all OK. `node --test tests/extensions/test-panel-sections.mjs`: 56 pass.
- `bash scripts/test-meta-deploy.bash`: exit 0.

## Claims checked

1. **One judgement (gap 2).** Verified. `check_freshness.run` now calls
   `changed_plays.judge` only. The deleted names (`changes_since`, `play_sha256_at_head`,
   `freshness.classify`, `freshness.retire`, `plays_to_query`, `UNEXPLAINED`,
   `_drop_never_tracked`, `_apply_retirements`, `Verdict.changes`/`.successor`) have no
   remaining callers in code or tests. `changed_plays.run` output is unchanged for every
   state: RUN for stale/failed/successors, GONE, UNRESOLVED; `--all` PLAY/GONE. Successor
   handling matches the old code (failed stays failed, unresolved popped). An unknown state
   raises KeyError in `_VERDICT_STATE`, which is loud. `TestAgreesWithRunBashChanged` builds
   a real git repository and ledger (deployed-file change, failed, dirty, uncommitted,
   current) and asserts the report's plays equal `changed_plays.run`'s RUN lines.
2. **Coverage reaches the document.** The wiring in `login_report.main`
   (`play_freshness_findings` into `stated_coverage[FRESHNESS]`) is correct by reading, but see
   N1 and N2.
3. **Panel count.** `rerunLabel` counts every state except `fresh` and `unresolved`; rows
   already exclude GONE. Correct.
4. **Gap 1 command.** `login_message._checkout()` comes from `realpath(__file__)`; both
   callers (`fedora-desktop-health.j2:168`, `host-health-report.bash.j2:48`) run the module
   from the checkout with `PYTHONPATH`, so the path is resolved at runtime. It is quoted with
   `shlex.quote`, printed in a line that is not a ` -` line (so `item_counts` does not count
   it), and named in the reminder. No real path is in the diff (the test fixtures use
   `/home/me/...` placeholders).
5. **Gap 3, meta-deploy.** `changed-plays` validates alone (extra words are fatal), maps to
   `<repo>/run.bash --changed --yes`, runs at its place, is named in the summary and in
   `--list`, and fails the batch on a non-zero exit. The removed `status=0` before acceptance
   is safe: `status` is 0 at loop top and after a passing deploy. The bash-limits record
   tightened (161/180).
6. **Acceptance.** It is sound bash: `plays_to_run` is used as an `if` condition but
   re-exits explicitly with `|| exit 1`; `fail` inside `$(changed_plays)` exits the subshell
   and the assignment then trips `set -e`; the EXIT trap uses a global. Exit 2 is correctly
   produced only when no leg failed. Queued last in `meta-deploy.list` with no
   `changed-plays` above it. Docs in `docs/playbooks.md` match the code.

## FIX-FIRST

### F1. Live docs still describe the deleted rules and names

The plan deleted the play-file-only judgement and `_drop_never_tracked`, but three live
(non-plan) files still state them as current fact:

- `helpers/play_ledger/collector.py:135-142`: "the same reasoning as
  `check_freshness._drop_never_tracked`" and "check_freshness drops it as never tracked". That
  function is gone. The judgement is now `changed_plays.judge`, which drops a play not tracked
  at its commit (`tracked_at`) and never judges plays outside `playbooks/imports/`.
- `helpers/self_update/affected_plays.py:3-4`: "The play ledger watches only each play's OWN
  file, so a commit that changes the ccy lib ... leaves `play-claude-yolo.yml` reading as up
  to date." After this branch the health report reads those inputs too, so the sentence is
  now false.
- `playbooks/CLAUDE.md:30-31`: "a deleted play is reported as 'no longer exists at HEAD' for
  ever". The label is now "no longer in the checkout" (`check_freshness._STATE_LABEL`), and it
  covers only plays under `playbooks/imports/`.

Fix: reword the three passages to describe `changed_plays.judge`, and use the new label.

## NOTE

- **N1. Nothing reads the coverage sentence.** `status_document.COVERAGE_KEY` is written
  (`status_document.py:216`) and read by no consumer: not `login_message`, not the panel, not
  `fedora-desktop-health`. So "its coverage says what it leaves out" (PLAN Goals) and
  `docs/playbooks.md:962-963` ("The section's coverage names the plays it cannot judge ...")
  describe a JSON field no human sees. In practice, unresolved plays and plays outside
  `playbooks/imports/` no longer appear anywhere a reader looks; only `run.bash --changed`
  prints its UNRESOLVED warning. This follows the pins precedent and is not a regression in
  what is shown, but the docs sentence overstates it. Either say "the status document
  records", or show it (for example as one `unchecked` line when `unresolved` is non-empty).
- **N2. The Task 1.3 path is not tested end to end.** `test_the_coverage_sentence_reaches_the_callers_sink`
  proves only that `freshness_findings` forwards the list. The closure in `login_report.main`
  that puts `coverage[0]` into `stated_coverage[FRESHNESS]`, and from there into the document,
  has no test. A main-level test that reads the written document's
  `sections["play-freshness"]["coverage"]` would cover it.
- **N3. Behaviour change for plays outside `playbooks/imports/`.** Before this branch the
  report judged every ledgered play (for example `playbooks/dev/*`) and listed it as STALE or
  GONE, and the panel's rows included them. Now they are only counted in the unread coverage
  (N1). This is deliberate (it matches `--changed`), but neither the PLAN nor the docs
  mention that those findings went away.
- **N4.** `TestAgreesWithRunBashChanged` does not assert the return value of
  `check_freshness.run`. Add `assertEqual(..., EXIT_FINDINGS)`, so that an
  EXIT_UNTRUSTWORTHY run with partial stdout cannot pass.
- **N5. The legs depend on each other's order.** The `reminder` leg reads the status document
  that the `report` leg's collection wrote. If the collection fails, the reminder judges an
  older document. That is harmless while the report leg's failure already fails the run, but
  the coupling is not stated in the script.
- **N6.** `expected_command` handles a path with unsafe characters, but not one containing a
  `'`, which `shlex.quote` escapes as `'"'"'`. A path like that is unlikely on a host checkout.
- **N7. Two encodings of the same set.** `login_message._RUN_STATES` lists the states that
  run (stale, failed); `plays.js NOT_RUN` lists the states that do not (fresh, unresolved). A
  new state would be counted by the panel but would not print the command. Consider deriving
  the Python side from `changed_plays.RUN_STATES`, which already exists.
- **N8. Gap 3 is not tested against the real run.bash.** The meta-deploy test uses a
  fixture `run.bash`. In the real one, `meta-deploy.bash` exports `RUN_BASH_OFFER_ISSUE=0`, and
  `run.bash:1266` then sets HEADLESS when stdin is not a tty. In that case
  `play_batch_preflight` refuses `--changed` ("cannot run unattended"). From the owner's
  terminal (stdin is a tty) it works, and the refusal fails loudly, so this is a NOTE. But
  `--yes` is documented as being "for a caller nobody answers (meta-deploy)", and a truly
  unattended meta-deploy cannot use it. The acceptance also never runs a `changed-plays`
  entry on the host, by design.
- **N9.** `coverage_sentence` counts never-run successors among the "plays run here".
- **N10.** The design pointers in `freshness.py` and `check_freshness.py` lead to
  `CLAUDE/Plan/00109.../DESIGN-play-ledger.md`. That plan is still In Progress, and the
  pointed-at section describes the deleted four-verdict model (`UNEXPLAINED`,
  `plays_to_query`). Add a one-line superseded-by note there, or drop the pointer.
- **N11.** `JOURNAL/` has only the scaffold entry, although four commits of work (including
  the root cause of the 10 against 25) have landed. Add a finding entry and a handoff entry
  through `mkplan.bash --journal`.

VERDICT: FIX-FIRST
