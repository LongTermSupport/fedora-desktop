# Room-ID fix review, round 2

Range: `git diff 5f86e270..40b2cae7`, with `f2c59399..HEAD` for context. Read only.

## Verdict: FIX-FIRST

## Checks run

- `python3 -m unittest tests.helpers.pingbus.test_identifier_grammar tests.helpers.agent_bus.test_admin tests.helpers.agent_bus.test_admin_side_rooms`: 93 tests, OK.
- `findings()` on `git show f2c59399:helpers/agent_bus/admin.py` gives `[417]`, so it still fires on F44.
- A sweep of every `helpers/` and `CLAUDE/Plan/` `*.py` at HEAD gives no findings. `cli.py:1189`, which reads `--re` CLI arguments, is no longer a false positive.
- `CCY_VERSION` 3.99.14.12 and `REQUIRED_CONTAINER_VERSION` 2.78.15 match the meta-deploy.list text.

## Findings

1. **Resolved.** Round 1 finding 1: meta-deploy.list now says Container 2.78.15, which matches `claude-yolo`.

2. **Not resolved: the rule still cannot see the syncer's invite loop.** Round 1 finding 2 named this exact site. The taint is followed only inside one function. Parameters are never tainted. `Syncer._take_invites(self, rooms)` gets the `/sync` answer as a parameter, and its loop is `for room_id, invite in _section(rooms, "invite").items()`, so that loop is never treated as homeserver data.

   - Mutation proof: replace the `continue` at `helpers/pingbus/syncer.py:659-661` with `raise ValueError(...)`. `findings()` then returns `[]`.
   - The fixture's `invites()` passes only because it calls `.sync()` in the same function as the loop. The real code does not have that shape.
   - So these claims are not true of the code as it stands:
     - QA.md "a local name bound from one";
     - the DBF report's "examined the syncer" and "nothing in F44's syncer.py";
     - the commit title "the rule follows homeserver data".
   - Fix, either of:
     - (a) Follow the data into a same-module callee: a call `self.f(x)` or `f(x)` with a tainted `x` taints `f`'s matching parameter, iterated to a fixpoint. Add a fixture `ruleid` case shaped like `_take_invites`, with the loop in a helper whose answer arrives as a parameter.
     - (b) Keep the rule as it is, and state the one-function limit in the QA.md section and the DBF report. Drop the claim that the syncer is covered.
   - (a) is the honest closure of round 1's finding.

3. **Minor, documentation.** The DBF report cites `protocol.py:1265` and `:1452`, which are the F44 line numbers. HEAD's are `:1289` and `:1476`, while the same report cites `syncer.py:659` at HEAD. Label the F44 numbers or update them.

## Sound as reviewed

- The `all()`/`any()` form now requires the comprehension's iterable to be homeserver-sourced.
- The loop form requires an `if` on the check whose body raises. The per-item `continue` is correctly ok.
- Assignment taint reaches a fixpoint, covering `Assign`, `AnnAssign` and walrus.
- `parse_rooms_index` (bus-written content) is correctly out of scope.
