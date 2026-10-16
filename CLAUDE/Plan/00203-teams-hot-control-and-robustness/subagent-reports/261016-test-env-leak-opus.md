# Helper tests inherited the agent-bus session (QA defect, filed under 00203)

Branch fix/hermetic-helper-tests, from F44 55e08be7. CCY 3.99.10.3, container 2.75.2 (the
status line fix below is in `helpers/pingbus`, which the image bakes in).

Not plan work: a QA defect found when the coordinator, relaunched on image 2.75.1, ran qa-all.
Every M2+ ccy session carries PINGBUS_HOME, PINGBUS_TEAMS and PINGBUS_MEMBERSHIP, and four
helper tests plus three cases of one bash suite failed there.

## The four helper failures: three causes

| Test                                                                                            | Cause                                                      | Same leak? |
| ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------- | ---------- |
| `test_seat.RealExecTest` (both cases)                                                           | `env = dict(os.environ, ...)` passed PINGBUS_MEMBERSHIP on | yes        |
| `test_statusline...test_a_cancelled_pingbus_kills_the_inner_status_line_group`                  | a real race in `helpers/pingbus/statusline.py`             | no         |
| `vmtest.test_request.EndToEnd.test_pass_end_to_end_exits_zero_and_states_what_it_cannot_verify` | a race in the test's fake bridge                           | no         |

**test_seat.** `seat exec` reads PINGBUS_MEMBERSHIP when it is set, and refuses it beside
PINGBUS_SEATS (78 = EX_CONFIG). The test built its env from `os.environ` plus its own
PINGBUS_SEATS, so in a bus session it ran the session's membership, not the fixture's. It was red
every time with the variables set and green with them removed.

**statusline: a real defect.** `run_command` set `_running = proc` only after `Popen` returned.
But `Popen` returns after the child has exec'd, so `sh -c` can start its own child before then.
A SIGTERM landing in that window found `_running` still None. The handler then raised
SystemExit without killing the group, and the inner command and its child kept running. That is
exactly what the test asserts cannot happen, and Claude Code cancels status lines as a matter of
course. Fix: while Popen runs (`_starting`), the handler only records the signal (`_cancel_pending`).
`run_command` acts on it as soon as `_running` is set: it kills the group and raises
SystemExit(128+n). A signal after that point finds `_running` set, as before. No sleep and no
retry. New test `test_a_cancel_that_lands_while_the_inner_is_starting_still_kills_its_group`
forces the window: a wrapped Popen waits until the grandchild has written its pid, then runs
the handler before returning. On the old code it failed ("process N outlived the status line").

**vmtest: a race in the test.** The fake bridge wrote each response with `write_text`, which
truncates first and then writes. The requester polls every 0.1 s, and `read_json` reads an empty
or partial file as `{}`, so it answers "response has unknown state None" and exits 8. That is the
exact failure in the log. The real watcher and run scope write with `spool.write_file` (a temp
name, then rename), so production never shows a torn file. The fake now writes the same way. A
scratch reproduction held the truncated file for 0.5 s and got the logged failure every time.

**Repetitions** (10 runs each, before the fix, on a 22-core host at load about 8, and again
under 44 busy loops at load about 43): the statusline case and `EndToEnd` failed 0/10 with the
variables set and 0/10 without. So both are rare load races that the environment does not
affect. Only a forced window shows them, as above.

After the fix, 10 runs each of the whole `test_statusline`, `test_request.EndToEnd` and
`test_seat.RealExecTest`, with the session variables real, removed and bogus: 0/10 failures
in all nine combinations.

## Fix: hermetic at two layers, one list

- `helpers/qa_environment/session_env.py` holds the list (prefixes PINGBUS\_ and AGENT_BUS\_,
  plus CCY_SESSION_NAME) and `scrub(environ)`.
- `tests/helpers/__init__.py` scrubs `os.environ` when the package is imported. Every helper test
  module is under it, so `python3 -m unittest tests.helpers.<module>` is hermetic too, and any new
  test inherits the scrub without anyone remembering it. It is the one regular package among the
  helper trees; the directories under it stay namespace packages (documented in
  helpers/CLAUDE.md).
- `helpers/qa_environment/unittest_counts.py` (the runner `qa-helper-tests` starts) scrubs before
  it imports the first module.
- Nothing was added to the Go gate. It execs `unittest_counts`, and a second copy of the prefix
  list in Go could drift from the Python one.

**Guard (cheap, implemented).** The runner fails a run whose `os.environ` holds a session
variable after the suite, names the variable, and still writes the counts. Because of the
package scrub, such a variable can only be one a test set and never removed, which every later
test then inherits. A check that a test SUBPROCESS sees no PINGBUS\_\* would be wrong: tests
hand PINGBUS_HOME and others to their subprocesses on purpose. `test_session_env` also proves
in a subprocess that importing `tests.helpers` removes the variables.

**Proof.** The full helper gate (`scripts/qa-helper-tests.bash`) ran with every session variable
set to a bogus value: `HELPER-TESTS-OK: Ran 4764 tests in 140 modules`. Without the fix, the
pingbus, agent_bus, agent_bus_lxc and ccy_lifecycle suites under the same bogus values failed
exactly the two `RealExecTest` cases.

## The same leak in a bash suite

The log also showed three failures in `scripts/test-ccy-lifecycle.bash`, in the "no option:
... byte-identical" wrapper-line cases. The cause is the same leak: the entrypoint adds the
lifecycle plugin when `PINGBUS_MEMBERSHIP` is not `off` (H22), so the session's value put
`--plugin` on the "no option" line. The suite now unsets PINGBUS\_\*, AGENT_BUS\_\*,
CCY_LIFECYCLE\_\* and CCY_SESSION_NAME at the top. The suite passes 215/215 in the session and
with bogus values.

The other bash suites that mention the bus (`test-cc-teams`, `test-ccy-agent-bus`,
`test-agent-bus-install`, `test-ccy-teams`) pass in a bus session as they are.

**Not done: a guard for bash suites.** They share no library, so the only single place is the
qa-all gate runner (`src-go/qa/all`), starting every suite with the session variables removed.
That would change the gate's process environment, which its golden replay pins, so it is larger
than this fix. It is the follow-up if another bash suite shows the leak.
