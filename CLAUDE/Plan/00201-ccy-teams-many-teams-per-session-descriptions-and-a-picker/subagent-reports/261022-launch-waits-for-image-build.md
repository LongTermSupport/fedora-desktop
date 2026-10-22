# Task 2.23: M7's launch waits allow for an image build

The b0eb800fa host run failed "session multi's container running: not within 600 s" while the
headless launch rebuilt the project image after a base image rebuild. A checker defect, fixed
with `m7_poll_launch` (`_acceptance-m7.inc.bash`): the 600 s starts again while a live image
build record is under `$XDG_RUNTIME_DIR/ccy/image-builds/`, with no cap on the build, and a
failed build still fails at once because it ends the launch. `LaunchWaitTest` in
`test_m7_check.py` went red against the old waits first.

The full Defence Before Fix record (the class, the proof, the sweep of other plans' checkers
and the review rounds) is shared with Plans 00211 and 00214:
`../../00211-agent-bus-reports-and-questions-rooms/subagent-reports/261022-checker-join-build-exit.md`.
