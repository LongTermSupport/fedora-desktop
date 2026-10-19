# The stalled-relay test flake

Branch `fix/relay-test-flake` from F44 `8c2de08c`. The fix is commit `f4a7af85`, and it
changes only the test. `helpers/pingbus` is unchanged, so there is no CCY or container
bump and no meta-deploy entry.

Test: `tests/helpers/pingbus/test_relay_cli.py`,
`WatchRelayTest.test_no_relay_confirmed_for_120_seconds_while_items_wait_is_stalled`.

## Cause

The test's assumption was wrong. The code is correct.

`cli._relay_pass` spools the pending item (`_deliver`) and only then calls
`ledger.stall.observe(...)`. That call reads the injected clock and starts the stall window
(`relay.Stall._since = now`). The old test did this:

1. It waited until the spool line appeared.
2. It moved the clock straight to 1119.
3. It slept for 0.3 s and asserted that the relay was not stalled.
4. It moved the clock to 1121 and waited 10 s for the stall.

If the watcher thread was preempted between writing the line and calling `observe`, which
happens under host load, the first `observe` read 1119 instead of 1000. The window then
started at 1119, so at 1121 only 2 s had passed. The relay never stalled, and `until` failed
after 10 s with "relay never recorded as stalled". The 0.3 s sleep in step 3 also proved
nothing under load, because no pass needed to have run in that time.

## Production: not a bug

A real watcher uses a real clock. When a pass is delayed, the window starts as late as the
delay and the stall is declared that much later. Nothing is misjudged: no stall is missed,
and no stall is declared early.

## Fix

The test now patches `relay.Stall` with a subclass that records each judgment made while
items wait, as a pair: the clock value read inside `observe` and the result. The test moves
the clock only after there is a judgment at the current value. It waits for the judgment at
1000, then moves to 1119. It waits for the judgment at 1119 and asserts that no judgment
so far was "stalled". Then it moves to 1121. The clock is still injected, and the fixed
0.3 s sleep is gone.

## Proof

- **Red: the old test fails when the load window is forced.**
  `untracked/scratch/relayflake/delayed.py` sleeps 0.5 s at the start of
  `Stall.observe`, between the spool write and the clock read. The old test then failed
  with the reported symptom, "relay never recorded as stalled: []", in 10.75 s. The new
  test passed in 2.98 s with the same delay.

- **Under load: 200 of 200 runs passed, 0 failed.**
  `untracked/scratch/relayflake/stress.bash` ran the 200 runs while two loops ran in
  parallel:

  - the full pingbus suite (all 35 `tests/helpers/pingbus/test_*.py` modules): 1587 tests,
    OK, in 477 s;
  - `go test -count=1 -p 16 ./...` in `src-go`: 11 runs, all OK.

  The 1-minute load average during the runs was between 10.96 and 69.96, on 22 CPUs.
  `PINGBUS_*` was unset throughout.

- **Full pingbus suite (PINGBUS\_* unset):*\* OK, the same run as above.

- **`scripts/qa-changed.bash`:** passed (toolchain, ruff, unit-tests on the module).
  `qa-all.bash` was not run, as instructed.

Not pushed and not merged.
