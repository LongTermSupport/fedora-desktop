# Plan 00214 adversarial review, round 5

Scope: the whole `F44...HEAD` diff of `feat/update-all-changed-plays` (c07eb888b), reviewed
fresh against issue #110 (body and the owner comment), PLAN.md and rounds 1-4.

Checks run:

- The affected Python unit modules (play_ledger check_freshness, changed_plays, freshness,
  git_history, collector; host_health login_message, login_report, play_runner;
  self_update affected_plays): 321 tests, OK.
- `node --test tests/extensions/test-panel-sections.mjs`: rc 0.
- `bash scripts/test-meta-deploy.bash`: passed 26, including the `changed-plays` cases
  (listing, words refused, list order, summary name, a failing run fails the batch).
- `./scripts/qa-changed.bash --base F44`: rc 1, from one suite only:
  `test-wsi-stop-grace.bash` -> `tests.speech_to_text.test_server_file_job`
  (`test_a_running_job_is_marked_for_the_panel_and_unmarked_when_it_ends`, "a failed job is
  unmarked too"). The branch touches nothing under speech-to-text. Three re-runs of that
  module in this worktree, and one in the main checkout, all passed. It is a timing flake
  under the load of the full changed-suite run, not this branch's (see N4). Every other
  check and suite passed. The "not covered" `tests/helpers/pingbus/test_identifier_grammar.py`
  is F44's (the branch is behind F44), as rounds 3 and 4 noted.

## Round 4 fixes, verified

- F1 `tests/extensions/test-panel-sections.mjs:608-614`: the header now says the count is
  the plays `run.bash --changed` runs, judged by `changed_plays.judge`, unresolved not
  counted. Matches `plays.js:38-46` (`NOT_RUN = {fresh, unresolved}`).
- F2 `files/home/.local/bin/fedora-desktop-health.j2:42-44`: names plays that have to run
  again "(changed or failed since they ran) with the command that runs them". Accurate;
  the dirty case is folded into "changed" (N1). The template is measured by the bash gate
  and qa-changed's bash-limits check passed.
- F3 `PLAN.md` Task 1.3 now says `--run-play` still runs a ledgered `playbooks/dev/` play
  because its gate reads the ledger, which matches `play_runner.main`/`validate`. The two
  play_runner docstrings (`play_runner.py:4-6`, `:54-56`) now name the judged
  `playbooks/imports/` plays.
- `meta-deploy.list` description of the 00214 entry says a later `changed-plays` run also
  carries `play-host-health-login-report.yml` (the help text). Correct: that play deploys
  the template, and `changed_plays.judge` sees the template as one of its inputs.

## Whole-branch re-check, no defect found

- **One judgement.** `check_freshness.run` calls `changed_plays.judge` and maps its states
  (`_VERDICT_STATE`); `changed_plays.run` prints from the same `Judgement`. `REPORTABLE = (STALE, FAILED, GONE)` against `--changed`'s RUN (stale, failed) plus GONE lines; the
  successor of a retired play is STALE in both. `TestAgreesWithRunBashChanged` proves it on
  a real repository and ledger for all four ways a play goes stale, plus one current play.
- **Fail fast.** Every judge failure raises `NoAnswer`, and both callers turn it into
  exit 2 with the reasons on stderr. `judged`/`coverage` stay empty on that path, so no
  partial list reaches the document. `changed_since` checks git's return code.
- **The command (owner comment).** `login_message.run_changed_command` builds
  `<checkout>/run.bash --changed` from `realpath(__file__)` at runtime and quotes it with
  `shlex.quote`. Both callers run the checkout's own copy (`PYTHONPATH="$ROOT_DIR"` in
  `fedora-desktop-health.j2:168` and in `host-health-report.bash.j2:48`), so the path is the
  checkout's. It is printed only when a `plays` row is stale or failed, which are exactly the
  states `--changed` runs; a GONE-only host gets no command, correctly. The remedy line is
  not a ` -` line, so `item_counts` does not count it as a fault. The reminder names the
  same command. The once-a-day digest covers the command line. No real path is in the
  diff: fixtures use `/home/me/...`.
- **meta-deploy.** `changed-plays` must stand alone (words refused before
  `--changed --yes` is appended), maps to `${repoRoot}/run.bash`, is named
  `run.bash --changed --yes` in the listing and the summary, and writes `run.bash.log`. The
  moved `triageArgs` read gives playbook and plan-script units an empty argument list as
  before. Dropping `status=0` before acceptance is safe: `status` is 0 there on every path
  (loop top, or a deploy that passed). The script shrank (161/180) and the allowlist
  ratcheted down.
- **Acceptance.** Queued last with no `changed-plays` above it; it needs plays still to run
  and says NOT JUDGED (exit 2) otherwise. `expected_command` mirrors `shlex.quote`, and
  `PLAN_REPO_ROOT` is physical (`pwd -P`), matching `realpath`. The `report` leg works
  before the play redeploys, because `fedora-desktop-health` runs the checkout's
  `login_message`.
- **Docs and plan.** `docs/playbooks.md`, AgentNotes, PlanWorkflow, QA.md,
  playbooks/CLAUDE.md, retired.py, collector.py, affected_plays.py and git_history.py
  describe the shared judgement. A search for the deleted wording ("no longer exists at
  HEAD", UNEXPLAINED, "not fresh", `changes_since`, `play_sha256_at_head`,
  `freshness.classify/retire`, `plays_to_query`) across helpers, extensions, docs, files,
  playbooks, scripts, tests, src-go and run.bash finds nothing live.
- **Public repo.** No hostnames, container names or real paths in the added lines.
- **Issue #110.** Gap 1 (the report and the terminal reminder name the command, with the
  checkout's runtime path), gap 2 (one judgement, coverage stated), gap 3 (`changed-plays`
  entry with its ordering rule documented). Gap 4 is declined in Non-Goals, with a reason;
  the issue marks it optional.

## FIX-FIRST

None.

## NOTE

- **N1 (carried from round 4, still not blocking).** The short summaries say "changed or
  failed" (`login_message._RUNS_CHANGED`, the `--help` text) or "changed since it ran here"
  (`PlanWorkflow.md:149`). A dirty run is reported as "changed since it was run here" with
  the detail line "it last ran from a checkout with uncommitted changes", so a reader is
  not misled. "Has to run again" would be exact.
- **N2.** `helpers/play_ledger/collector.py:145` and `CLAUDE/PlanWorkflow.md:149` are
  long unwrapped lines left by rewraps. Cosmetic; ruff and docs QA pass them.
- **N3.** `PLAN.md` Success Criteria are all unticked, although the first one is proved by
  `TestAgreesWithRunBashChanged`. Leaving them for the host acceptance and Task 4.3 is
  consistent; tick them when the plan closes.
- **N4 (not this branch).** `tests.speech_to_text.test_server_file_job` failed once under
  qa-changed's load and passed in isolation 4 times. It is a timing flake in a suite the
  branch does not touch. The coordinator's full `qa-all.bash` before the push will show
  whether it recurs. If it does, it is its own defect, not a reason to hold this merge.
- **N5.** `changed_since` now runs `git diff` on the working tree from
  `host-health-collect.service`. git may refresh the index stat cache opportunistically.
  This is harmless and is the same call `run.bash --changed` already makes.

VERDICT: MERGE
