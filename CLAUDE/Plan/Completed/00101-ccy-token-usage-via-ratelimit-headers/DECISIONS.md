# Plan 00101 — technical decisions

Extracted from [PLAN.md](PLAN.md) to keep it lean. Fact IDs cited here are in
[FINDINGS.md](FINDINGS.md).

## Decision 1: Human-triggered, never automatic

**Context**: Plan 00100's version fetched on every launch, which was free there.
Here every fetch spends billed quota out of the allowance it reports.

**Decision**: a keypress in the selector, never a launch-time fetch. The cost is
stated in the option text so the user knows what pressing it does. This also
removes the whole class of problems 00100 hit — no launch latency, no timeout
tuning, no IP-throttle risk from a burst nobody asked for.

**Date**: 2026-08-17

## Decision 2: Direct HTTP, not `claude -p`

**Context**: The obvious "minimal" call is `claude -p --model haiku` with tools
off.

**Decision**: bare `curl`. Two reasons, both grounded: the headers are the
payload and F12 shows no code path prints them, so the CLI cannot deliver them
at all; and `claude -p` is the *less* minimal option — it ships a large system
prompt and tool schemas as input tokens, where the bare request sends one
character with `max_tokens: 1`. The prototype still runs the `claude -p` arm as
a control, so this is tested rather than asserted.

**Date**: 2026-08-17

## Decision 3: Probe with Haiku

**Context**: Which model to spend the token on.

**Decision**: Haiku, because the weekly buckets are per-model, so probing with
the cheapest model avoids drawing down the allowance that matters to the user.

**Date**: 2026-08-17

**Amended 2026-09-10**: still right for the 5-hour and weekly buckets, and now
known to be *insufficient* for the Fable window — F29 refuted H3, so a Haiku
probe cannot surface `7d_oi`. Note also that a Fable or Mythos probe cannot use
the `max_tokens: 1` body at all (F33): they reject disabled thinking and carry a
mandatory 2048-token budget which `max_tokens` must exceed.

## Decision 4: Ship on the percent reading rather than hold for Q2

**Context**: Q2 (is utilisation 0-100 or 0-1?) could not be settled from the
near-idle account probed, and settling it needs one more billed request.

**Decision**: ship. The owner's steer was explicit — the spend in question is a
few Haiku requests, and holding a finished feature for it is disproportionate.
Mitigations rather than a guess left bare: the scale lives behind one function,
so flipping it is a one-function change; and a value that is non-zero but rounds
to zero renders `<1%`, not `0%`, so the display never claims an account is
untouched when it is not.

**Date**: 2026-08-17

**Outcome**: the reading was **wrong**, and the predicted signal is exactly what
surfaced — every account displayed `<1%` for a week. Flipped in Task 5.3, and
since proven outright by F20 and F35. The decision's own mitigation is what made
it recoverable in one function.

## Decision 5: Undocumented surface, so degrade rather than assume

**Context**: F37 — the entire `anthropic-ratelimit-unified-*` family is absent
from Anthropic's public documentation, and `7d_oi` appears in no public source
at all. Everything this plan reads is an internal surface that can change
without notice.

**Decision**: every bucket is optional and absence is a normal state, not a
fault. F35 makes this the vendor's own position for the Fable window: "present
only for accounts whose responses carry that window". A missing bucket says so
on its own row (Task 5.4), an impossible value announces the contradiction
rather than clamping (Task 5.3), and no row is ever synthesised from a
neighbouring header — which is the specific mistake H5 would have shipped.

**Date**: 2026-09-10
