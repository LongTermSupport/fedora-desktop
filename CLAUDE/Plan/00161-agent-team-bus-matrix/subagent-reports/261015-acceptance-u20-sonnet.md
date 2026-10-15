# Acceptance abort at U20: second coordinator

Run aborted at leg U20 (making member acca the coordinator): "team acceptance already has a coordinator: <member a>; it must stop being coordinator first". M1.1-M1.7 had passed; cleanup ran.

Cause: the acceptance's own M1 member a is added as coordinator and keeps the role through U20. Plan 00196 allows at most one coordinator per team and `set-role` refuses a second, naming the holder. The ccy --teams launches assigned no role (the members list before U20 showed only a as coordinator; the seats join as workers). The product is right; U20 was wrong.

Fix: `u20_hand_over_coordinator` (`_acceptance-u20.inc.bash`) lists the team, sets every other coordinator to worker (new `u20_check.py` command `other-coordinators`, unit tested in `test_u20_check.py`), then sets acca coordinator. Any `set-role` failure still aborts the leg.

Evidence: `python3 -m unittest test_u20_check` in the plan folder, 72 tests OK. The host re-run is the only proof of the leg itself.
