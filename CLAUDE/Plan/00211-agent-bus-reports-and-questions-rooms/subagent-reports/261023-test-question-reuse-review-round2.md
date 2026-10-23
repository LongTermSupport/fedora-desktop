# Plan 00211 Task 2.12, test-question reuse: review round 2

Reviewer: fresh adversarial review of the staged diff (`git diff --cached`), read-only.

## Verdict: MERGE

No blocking defect. Four non-blocking findings below.

## What was run

- `acceptance.bash --stage 2 --fake`: exit 0, FAKE ACCEPTED. The five new legs behave as
  claimed: `open` reuses Q3 and saves `t1 alpha Q3 reused`; `asked` saves `Q4 asked`;
  `limited` (EXIT_RATE, another reason), `open-refused` (the duplicate text, exit 4) and
  `open-elsewhere` (the duplicate text for team t9) each fail, and none saves a test-question.
- `acceptance.bash --stage 1 --fake`: exit 0, FAKE ACCEPTED.
- `python3 -m unittest tests.helpers.pingbus.test_cli_rooms tests.helpers.plan_acceptance.test_cited_constant_copy_rule`:
  34 tests, OK.

## Correctness checks (all hold)

- **Matcher.** `open_duplicate` builds its head (`PROG: team <QTEAM>: `) and tail from the
  product's own `QUESTION_DUPLICATE` and `PROG`, read through `m7_check.py product-value`.
  It needs the whole line to match: `index(...) == 1` anchors the start, and the substring
  check anchors the end. The middle must be `^Q[0-9]+$`. A team prefix cannot collide
  (`team t1: ` against `team t10: `) because of the colon. Team names have no backslashes,
  so `awk -v` escape handling is harmless. Reuse also needs the exit code to be EXIT_RATE,
  read from `cli.py`. Every other path returns 1.
- **Fail-fast.** `out="$(printf … | session_run … 2>err)" || rc=$?` takes session_run's
  status under pipefail. The input is small, so printf cannot take a SIGPIPE that hides it.
  Inside `check_leg`'s `if ! "$@"`, errexit is off. Every substitution that can fail has
  an explicit `|| return 1` (EXIT_RATE read, `open_duplicate`/`stage2_value`). A failed ask
  writes no test-question.
- **stderr.** `2>` is attached to `session_run`, so it captures `podman exec` (ccy) and
  `lxc-attach … runuser` (LXC) alike. stdin is a pipe, so lxc-attach allocates no pty and
  does not merge stderr into stdout. The captured stderr is echoed back to stderr on every
  path.
- **Product unchanged.** `cmd_ask_human` writes the same bytes and still raises
  `RateLimited("duplicate")` (EXIT_RATE). The test now asserts the whole line.
  `QUESTION_DUPLICATE` is in `OWNED`.
- **A5.** `read -r A5_TEAM A5_HANDLE A5_Q how` leaves `how` empty for a 3-field file and
  `reused` for a 4-field one. Fake runs write test-question under `<run>/fixtures/ask-run-*/`,
  which `a5_find`'s one-level glob (`<runs>/*/test-question`) does not match. So `--fake`
  cannot poison A5.
- **Verdict line.** `asked` is only used in the full-ACCEPTED branch. That branch is reached
  only when the test-question leg ran, so it is never empty there.
- **Version bump and docs.** CCY 3.100.14 / container 2.79.9 are consistent in the Dockerfile
  label, `REQUIRED_CONTAINER_VERSION` and the changelog. PLAN.md Task 2.12 says truthfully
  that the coordinator queues the meta-deploy rerun. No install-specific names.

## Findings

1. LOW, `CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/acceptance.bash:275-280`:
   no fake leg drives `a5_find` over a 4-field `reused` file, so A5 reading a reused question
   is proven only by inspection. Fix (optional): add an A5 fake leg that reads a fixture
   runs tree holding a `reused` line.
2. NIT, `helpers/pingbus/cli.py:55`: the comment says the refusal is of "a question an open
   one already asks". `QuestionIndex.open_duplicate` also matches answered questions (the
   same point as round 1's finding 2, fixed elsewhere but not here). Fix: "a not-closed (open
   or answered) one". Changing only a comment needs no further version bump beyond the one
   already in this diff.
3. NIT, `CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/ACCEPTANCE-owner.md:11`:
   one line is 152 characters, so round 1's finding 3 (re-wrap) is only half done. Fix:
   re-wrap the paragraph.
4. NIT, `CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/_acceptance-stage2.inc.bash:617`:
   the comment line is 129 characters, against about 100 in the rest of the comment. Fix:
   re-wrap.
