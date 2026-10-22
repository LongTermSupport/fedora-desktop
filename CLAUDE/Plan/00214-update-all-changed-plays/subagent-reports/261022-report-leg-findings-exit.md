# Task 4.3c: the report leg accepts login_report's EXIT_FINDINGS

The b0eb800fa host rerun failed the report leg with "the collection exited 3". 3 is
`login_report.EXIT_FINDINGS`, the collection's normal result while a play is stale, and a
stale play is the leg's precondition, so the leg could never pass. `check_report` in
`check-changed-plays.bash` now captures the status under `set -e`, accepts 0 and
`EXIT_FINDINGS` (read from this checkout's helper), fails on any other status, and fails on 0
when a play is to run. `test_check_changed_plays.py` drives the real checker over a stand-in
checkout and went red against the old checker first.

With no stale play the leg is NOT JUDGED, as before; the meta-deploy description says which
plays are expected to be stale and that NOT JUDGED is the expected result when none is.

The full Defence Before Fix record (the class, the proof, the sweep of other plans' checkers
and the review rounds) is shared with Plans 00211 and 00201:
`../../00211-agent-bus-reports-and-questions-rooms/subagent-reports/261022-checker-join-build-exit.md`.
