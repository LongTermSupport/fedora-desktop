# Room-ID fix review, round 3 (diff 40b2cae7..04b55f97)

Verdict: MERGE

## Round 2 findings

1. Interprocedural gap: resolved. `_module_taint` carries homeserver taint into same-module
   callees' parameters by function name, and the fixture has the `poll` -> `_take_invites(rooms)`
   shape (marked `ruleid`), plus a parameter-not-from-homeserver negative (`_check_args`).
2. DBF report line numbers: resolved. `protocol.py:1289` and `:1476` are right at HEAD.

## Checks run

- `python3 -m unittest tests.helpers.pingbus.test_identifier_grammar`: 3 tests OK. The rule
  proof passes and the sweep finds nothing at HEAD, so there are no false positives.
- F44's `admin.py` (from `git show f2c59399`) through `findings()`: `[417]`. No false negative.
- `syncer.py` with line 661 changed from `continue` to `raise` in memory: `[659]`. No false
  negative.
- Termination: each callee's parameter set only grows and is bounded by its own parameter
  names. `_homeserver_names` only grows a set bounded by the function's names. Both fixpoints
  terminate.
- CLAUDE/QA.md and the DBF report describe the widening (assignments, loop targets,
  same-module callee parameters) and its limit (no cross-module flow) accurately.

## Non-blocking

1. The test module's docstring (lines 10-12) still says the finding is "a function that calls
   the homeserver". A callee that only gets the data as a parameter is now covered too. Reword
   when next touched.
2. `_passed_params` matches positional arguments by index. A `*args` call, or an unbound
   `Class.method(obj, x)` call, could misalign them. No such call exists in the scanned tree.
