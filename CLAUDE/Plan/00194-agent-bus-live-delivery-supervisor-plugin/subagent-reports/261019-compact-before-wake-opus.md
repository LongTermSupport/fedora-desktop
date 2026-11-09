# Compact before a cold wake: report

Branch `feat/pingbus-compact-before-wake`, from origin/F44 `0ce52a7e`. The spec is
[../SPEC-compact-wake.md](../SPEC-compact-wake.md), and the task is PLAN.md Task 2.0e.

## Verdict

The request cannot work as described, from pingbus or at all safely in its `/compact <message>`
form:

- A socket message is framed by Claude Code ("Another Claude session sent a message:"), so a
  `/compact` in it is text, never a command (F2).
- The supervisor's plugin API 1.0 cannot type for pingbus (F3).
- `/compact <message>` makes the sender's text the summary's instructions, which the owner
  ruled out (F5).
- The supervisor's `continue` would fire after any bus compaction, giving a stray turn before
  the message (F4).

The nearest thing that works is supervisor-actuated (spec C7-C18, upstream U1-U3). Stage 1,
the measurement, is built test first and is observe only.

## Facts

These are F1-F10 in the spec, with file:line evidence. In short:

- **The context source is the transcript.** The last main-thread assistant `usage` gives
  input + cache writes + cache reads. Its timestamp is the cache clock. The `cache_creation`
  split says whether the TTL is one hour or five minutes; this deployment writes one-hour
  entries. `stop_reason` says whether a turn is still running.
- **The status-line data is not usable.** It is not recorded by pingbus. The daemon's sidecar
  is opt-in, and the supervisor deletes it after 30 minutes of idleness.
- **Compaction size and time, measured:** 161k-471k tokens compacted to 11k-18k, in 49-88 s.
  `/compact` ends without a turn.

## Cost

Spec section 2. The compaction call is itself a cold read of the whole context, so the wake is
never cheap. The saving is in later turns, and in avoiding a cold turn plus a later
compaction. At 60k tokens, a one-turn wake costs more if the compaction call writes the cache
(`k` is not measured; HC3).

## Decisions

D1-D7 in spec section 6.

## Tests

- `tests/helpers/pingbus/test_coldwake.py`, 18 tests. They failed first with "cannot import
  name 'coldwake'", then passed.
- `test_notify.WatchDeliveryTest`, three new cases. The cold case failed first with
  `Lists differ: [] != ['pingbus: cold wake: delivery …']`; all passed after the cli wiring.
- The full pingbus suite was run with `PINGBUS_*` unset.

## Host acceptance

HC1-HC4 in spec section 7. Only HC1 and HC2 apply to what is built.
