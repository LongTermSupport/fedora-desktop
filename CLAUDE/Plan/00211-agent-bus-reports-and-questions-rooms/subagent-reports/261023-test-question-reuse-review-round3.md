# Review round 3: the test question reuses an earlier run's not-closed question

Reviewer: fresh adversarial reviewer, staged diff (`git diff --cached`), focus on the delta since
round 2 (fake_a5_find, the two A5 --fake legs, the cli.py comment, re-wraps), whole diff judged.

## Runs

- `acceptance.bash --stage 2 --fake`: exit 0, FAKE ACCEPTED; the five test-question legs and both
  new A5 legs PASS (rejects rejected).
- `acceptance.bash --stage 1 --fake`: exit 0.
- `python3 -m unittest tests.helpers.pingbus.test_cli_rooms tests.helpers.plan_acceptance.test_cited_constant_copy_rule`:
  34 tests, OK.
- After the runs, no `untracked/plan-runs/<plan>/acceptance/*/test-question` exists: the fake runs
  left no saved question a real A5 could pick up.

## fake_a5_find

- Its `local` PLAN_REPO_ROOT, PLAN_SCRIPT_DIR, REPORT and SESSIONS hide the globals for a5_find
  and find_session (bash dynamic scope). The fixture tree is
  `${FIX}/a5-root/untracked/plan-runs/plan/acceptance/1/test-question`, and `basename` of the
  fake PLAN_SCRIPT_DIR gives `plan`, so a5_find reads only the fixture. Real runs are never read or
  written: a5_find's glob `acceptance/*/test-question` goes one level deep, so the fixture, nested
  under `<run>/fixtures/...`, cannot match a real A5 glob either.
- `rm -rf` is confined to `${FIX}/a5-root`. FIX is set by fake_setup (`${PLAN_RUN_DIR}/fixtures`)
  in the `plan_deploy_leg` that comes before every fake leg.
- It leaves A5_TEAM/A5_HANDLE/A5_Q/A5_SESSION set as globals. Harmless: the --fake branch ends in
  `plan_finish` before the real A5 path, and in the real path a5_find sets all four before they are
  read.
- The legs prove what they say. With a 4-field `reused` file, the question and session are read
  and the "which that run found not closed from an earlier run" suffix is printed. With a 3-field
  file, `read` leaves `how` empty, and `"asked by alpha ("` proves that no suffix appears. The
  session check compares find_session's output to the fixture SESSIONS line, which shows that
  find_session matched on team and handle.

## cli.py

The only change is the QUESTION_DUPLICATE constant and its use in cmd_ask_human. The formatted
message `"{PROG}: team {member.team}: Q{same.n} is open and asks the same question\n"` is the same
byte for byte as before, so behaviour is unchanged. The comment is accurate: `open_duplicate`
returns any question that is not closed (open or answered). The test pins the full line.

## Verdict: MERGE

Findings (none blocking):

1. Nit, `_acceptance-stage2.inc.bash` fake_a5_find `rm -rf -- "${FIX}/a5-root"`: it relies on
   FIX being set by fake_setup. That always happens today, but `"${FIX:?}/a5-root"` would make an
   empty FIX fail rather than target `/a5-root`. Optional hardening.
2. Nit, `helpers/pingbus/cli.py` QUESTION_DUPLICATE comment: "a question one not closed already
   asks" is hard to parse. "a question that a not-closed question already asks" reads better.
   Wording only.
