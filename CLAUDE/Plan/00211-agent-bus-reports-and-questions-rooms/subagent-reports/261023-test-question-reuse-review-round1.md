# Plan 00211 Task 2.12 — test-question reuse, adversarial review round 1

Reviewed: the staged diff (`git diff --cached`, which equals
`untracked/scratch/00211-t212-round1.diff` plus the later fix where fake_ask builds COORD from
`${CCY_PINGBUS_HOME}`, now staged too).

## What was run

- `acceptance.bash --stage 2 --fake`: FAKE ACCEPTED, rc 0. All five new legs behave as
  labelled: reuse PASS (Q3, `reused`, "still open" reported), new question PASS (Q4, `asked`),
  and the three refusals (another rate limit exit 9, the same text with exit 4, the same text
  naming team t9) each rejected, with no test-question saved.
- `acceptance.bash --stage 1 --fake`: rc 0 (CLI_PROG read at source time does not break stage 1).
- `python3 -m unittest tests.helpers.pingbus.test_cli_rooms tests.helpers.plan_acceptance.test_cited_constant_copy_rule`: OK (34 tests).

## Correctness of the reuse path

The matcher accepts a line only when **all** of these hold:

- rc equals `cli.EXIT_RATE` (9), read from the product.
- The line starts at column 1 with `${CLI_PROG}: ` followed by the product's own
  `QUESTION_DUPLICATE` head for `team=${QTEAM}`.
- It ends with the template's tail.
- The middle is exactly `Q[0-9]+`.

What that rules out:

- **Another team:** the team is part of the head.
- **Partial match:** the line is anchored at both ends and the middle is checked against a
  regex, so a line with a prefix or suffix fails.
- **A stray stderr line:** it would have to reproduce the exact product line and also come with
  exit 9.
- **Wrong text:** head and tail come from `product_value`, which fills `{team}` and leaves
  `{question}` literal, and refuses a template whose placeholders do not match.
- **awk -v escapes:** only a backslash in a team name could matter, and that makes the match
  fail, which is safe.

Every error path still fails:

- `stage2_value` fails: return 1.
- `EXIT_RATE` cannot be read: return 1, with a message.
- awk fails: return 1.
- Any non-matching refusal: the "failed ... (exit N), for the reason above" message and
  return 1.

stderr is now echoed in full. in_guest leaves stderr alone, so the same works for an LXC
coordinator. Nothing passes silently.

The product is unchanged:

- `f"{PROG}: {QUESTION_DUPLICATE.format(...)}"` gives the same bytes as before (the team is a
  format value, not part of the template).
- The exit code is unchanged.
- test_cli_rooms now asserts the whole line.

`QUESTION_DUPLICATE` is in `OWNED`. No checker copies product text: the fake's
"rate limited: ..." lines are fixture stderr in a fake podman, which the rule does not flag;
the rule test is green.

A5 handles both file formats:

- A 4-field file reads `how`.
- An older 3-field file leaves `how` empty, so the line falls back to the plain wording.
- `a5_find` takes the newest run that has a test-question. A reuse run writes one with the same
  Q, so A5 points at it. A failed ask writes none, which the fake proves.

## Findings

1. **HIGH — CLAUDE/Plan/meta-deploy.list (not in the diff); PLAN.md:255.** Task 2.12 says
   "left: the host rerun, queued in meta-deploy", but the queue is empty. d2612e8d7 emptied it
   and said the 00211 stage-2 rerun "comes back once its checker reuses an open test question",
   and this diff does not add it back. CLAUDE.md's hard rule: a host run goes into the list in
   the same commit. The CCY 3.100.14 / container 2.79.9 bump also has to reach the host.
   Fix: in this commit, add `00211-agent-bus-reports-and-questions-rooms/deploy-stage-2.bash`
   with a description. That script runs play-agent-bus and play-claude-yolo itself, so no
   separate play entry is needed. The description should say:

   - why it runs again (the reuse fix);
   - that `ccy --version` should then say 3.100.14;
   - that on a rerun the test question is the earlier, still-open one.

2. **LOW — \_acceptance-stage2.inc.bash ask_test_question messages; ACCEPTANCE-owner.md:6-10;
   journal; Task 2.12.**

   - **"Still open" is not quite what the product checks.** `QuestionIndex.open_duplicate`
     (protocol.py:1562) matches any question whose state is not closed, so an answered one
     counts too, even though pingbus's message says "is open". The reuse may therefore be of a
     question the owner has already answered. ACCEPTANCE-owner.md half covers this ("if you
     have already answered and edited it, steps 4 and 5 are done").
     Fix: say "not closed (open or answered)" in the run's PASS line and in ACCEPTANCE-owner.md,
     or check that the answered case is the intended one.
   - **On a rerun, the check no longer proves its label.** The label is "asked by the
     coordinator in its own session". Pingbus refuses a duplicate before it posts anything. So
     on a rerun the post path is not exercised, and the reused question's asker is not checked
     against the current coordinator (test-question records QHANDLE whatever the original asker
     was). The run's last line does say "found the test question an earlier run asked still
     open", which is honest. This is the cost of option a, so it does not block.
     Fix: optionally name it in the check's PASS line ("not re-asked: posting not exercised
     this run").

3. **LOW — ACCEPTANCE-owner.md:10.** The new sentence leaves line 10 far wider than the rest
   of the paragraph (unwrapped). Fix: re-wrap the paragraph.

4. **NIT — JOURNAL/00211-Journal-26-10-23.md, "Fix (the coordinator's option a): exit
   EXIT_RATE with a line that is pingbus's duplicate-question refusal for this team reuses the
   question it names".** The sentence does not parse. It is append-only, so if it matters, add
   a short correction entry.

No install-specific names were found. The fixtures use t1/t9/alpha/c1/`*.internal`. The
changelog entry and the version bump agree with each other: Dockerfile label 2.79.9 =
REQUIRED_CONTAINER_VERSION, and CCY 3.100.14.

VERDICT: CHANGES REQUIRED (finding 1; findings 2-4 are optional polish)
