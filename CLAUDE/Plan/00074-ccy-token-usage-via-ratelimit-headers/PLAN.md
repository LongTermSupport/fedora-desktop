# Plan 00074: Token usage limits via rate-limit response headers, on demand

**Status**: In Progress
**Created**: 2026-08-17
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00073 tried to show each stored account's 5-hour and weekly utilisation in
the `ccy` token selection menu, and failed on a hard wall: `GET /api/oauth/usage`
returns **403 — "OAuth token does not meet scope requirement `user:profile`"**
for every stored `sk-ant-oat01` setup-token. The scope is fixed when
`claude setup-token` mints the token, so the free status route is unreachable.

The same figures, however, travel as **response headers on `/v1/messages`** —
which is precisely the scope an `oat01` token *does* hold. This plan tests that
route and, if it works, surfaces the numbers.

The critical difference from 00073 is **who pays and when**. Reading a status
endpoint is free; reading response headers means making a real, billed request
that itself consumes a sliver of the allowance being reported. So this is
deliberately **not** an automatic fetch on every launch. It is a key the user
presses when they want the answer, which then redraws the selector with the
usage shown.

## Goals

- Establish whether a stored setup-token gets `anthropic-ratelimit-unified-*`
  headers back from `/v1/messages`.
- If it does: add a **human-triggered** option to the token selector that fetches
  usage and redraws the menu with it.
- Keep the cost per press explicit, minimal, and visible to the user.

## Non-Goals

- **No automatic fetching.** Nothing happens on launch unless a key is pressed.
  This is the whole point of the plan's shape.
- No background refresh, no daemon, no pre-warming.
- Not reviving `/api/oauth/usage` — Plan 00073 closed that definitively.

## Context & Background

### Facts carried forward from Plan 00073

| ID  | Fact                                                                                                                                    |
| --- | --------------------------------------------------------------------------------------------------------------------------------------- |
| F9  | `/api/oauth/usage` **and** `/api/oauth/profile` both return 403 `permission_error` — *"does not meet scope requirement `user:profile`"* |
| F10 | The unified figures exist as response headers: `anthropic-ratelimit-unified-{5h,7d}-{utilization,reset,surpassed-threshold}`, `-status` |

### Established here

| ID  | Fact                                                                                                                                                                                                                                 | Source                       |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------- |
| F11 | Claude Code's OAuth requests carry `anthropic-beta: oauth-2025-04-20` — the only oauth-dated beta string in the binary                                                                                                               | string table in `claude.exe` |
| F12 | The header names appear in a **mock/scenario harness** (`setScenario`, `addExceededLimit`, `setEarlyWarning`) that *synthesises* them for testing — confirming they are read from real responses, with no code path that prints them | JS in `claude.exe`           |

F12 matters for the mechanism choice: there is no debug or print path that
surfaces these headers, so the CLI cannot be the vehicle. Only a direct HTTP
call exposes them.

### Q1 — ANSWERED YES (HOST run)

| ID  | Fact                                                                                                                                                                                                                                               | Source         |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| F13 | A bare `POST /v1/messages` (`max_tokens: 1`) with a stored `oat01` setup-token returns **200** and carries the full `anthropic-ratelimit-unified-*` header set                                                                                     | HOST prototype |
| F14 | **F12 confirmed empirically**: `claude -p --tools "" --debug --debug-file` exited 0 and produced a 17 KB debug log containing **no** unified header. The CLI is not a viable vehicle — curl is the only route                                      | HOST prototype |
| F15 | The response carries **more than the four headers assumed**: per-bucket `-5h-status` / `-7d-status`, a `-representative-claim` naming the binding bucket (`five_hour`), plus `-fallback-percentage`, `-overage-status`, `-overage-disabled-reason` | HOST prototype |
| F16 | Utilisation values are **floats, not integers** (`0.0`, `0.02` observed) — the renderer's `%.0f` assumption held only because the probed account was near-idle                                                                                     | HOST prototype |

F15 is a bonus: `-representative-claim` says which bucket is actually binding,
which is exactly what a one-line menu column should lead with.

### Open question

| ID  | Question                                                                                                                  | How it gets answered             |
| --- | ------------------------------------------------------------------------------------------------------------------------- | -------------------------------- |
| Q2  | What is the **scale** of `-utilization` — a percentage (`0`–`100`) or a fraction (`0`–`1`)? `0.02` is either 0.02% or 2%. | Probe a **heavily-used** account |

Q2 is not cosmetic: guessing wrong misreports usage by 100×, in the direction
that matters (showing `0%` to someone who is actually at 2%, or `2%` to someone
at 0.02%). The near-idle account probed cannot discriminate — both readings fit.

**The discriminator is one-way and cheap**: any observed value **greater than 1**
proves the `0`–`100` scale, because a fraction cannot exceed 1. A value ≤ 1 on a
busy account leaves it open and means probing a busier one still.

## Tasks

### Phase 1: Prototype ✅

- [x] ✅ **Task 1.1**: Confirm the mechanism from the binary (F11, F12) — establish
  that `claude -p` cannot surface headers, so a direct call is required
- [x] ✅ **Task 1.2**: Write `prototype.bash` with two arms — bare `curl` and
  `claude -p --model haiku` as a control — rendering the exact menu line ccy
  would show, not just a header dump
- [x] ✅ **Task 1.3**: Verify against stubs: headers present, 403, and
  200-without-headers all render legibly. Two bugs found and fixed this way
- [x] ✅ **Task 1.4**: Run `prototype.bash` on the HOST — **Q1 answered yes** (F13,
  F14, F15, F16)

### Phase 2: Decision gate ✅

- [x] ✅ **Task 2.1**: Cancel-on-refusal branch — **not triggered**; the request
  succeeded rather than hitting a second scope wall
- [x] ✅ **Task 2.2**: Header names and value formats recorded — resets are epoch
  seconds, utilisation is a **float** (F16), and the set is wider than assumed
  (F15)
- [x] ✅ **Task 2.3**: Q2 deferred by owner decision, not left open — see
  Decision 4. Ships on the percent reading with a `<1%` guard

### Phase 3: Human-triggered display in the selector 🔄

- [x] ✅ **Task 3.1**: `u) Show usage limits (costs 1 small API call per account)`
  added to `select_token()`, cost stated in the option text itself
- [x] ✅ **Task 3.2**: On press, fetches in parallel and **redraws the selector**
  with a usage column; the option then disappears so it cannot be double-spent
- [x] ✅ **Task 3.3**: 00073 machinery reused — parallel fan-out, render-at-fetch,
  worst-percentage colouring, visible degradation, worker that cannot abort the
  menu. jq dropped entirely: headers parse in pure bash in a single pass
- [x] ✅ **Task 3.4**: Cached with a 15-minute TTL. Long on purpose — in 00073 a
  miss cost latency, here it costs quota
- [x] ✅ **Task 3.5**: `CCY_VERSION` 3.34.0, `token-management.bash` 1.9.0
- [x] ✅ **Task 3.6**: `./scripts/qa-all.bash` green
- [x] ✅ **Task 3.7**: `acceptance.bash` — verifies the deploy landed (deployed
  library `cmp`s to the repo, versions match, usage functions and `u)` option
  really present). Closes the 00073 wrong-play gap that
  `qa-deployed-drift.bash` cannot see, since it covers only
  `files/home/.local/bin/`
- [ ] 🔄 **Task 3.8**: Deploy on the HOST, run `acceptance.bash`, then press `u`
  against real accounts — **HOST action**

## Technical Decisions

### Decision 1: Human-triggered, never automatic

**Context**: Plan 00073's version fetched on every launch, which was free there.
Here every fetch spends billed quota out of the allowance it reports.
**Decision**: a keypress in the selector, never a launch-time fetch. The cost is
stated in the option text so the user knows what pressing it does. This also
removes the whole class of problems 00073 hit — no launch latency, no timeout
tuning, no IP-throttle risk from a burst nobody asked for.
**Date**: 2026-08-17

### Decision 2: Direct HTTP, not `claude -p`

**Context**: The obvious "minimal" call is `claude -p --model haiku` with tools
off.
**Decision**: bare `curl`. Two reasons, both grounded: the headers are the
payload and F12 shows no code path prints them, so the CLI cannot deliver them
at all; and `claude -p` is the *less* minimal option — it ships a large system
prompt and tool schemas as input tokens, where the bare request sends one
character with `max_tokens: 1`. The prototype still runs the `claude -p` arm as
a control, so this is tested rather than asserted.
**Date**: 2026-08-17

### Decision 3: Probe with Haiku

**Context**: Which model to spend the token on.
**Decision**: Haiku. The weekly buckets are per-model (`seven_day_opus`,
`seven_day_sonnet` in F3), so probing with the cheapest model avoids drawing
down the allowance that actually matters to the user.
**Date**: 2026-08-17

### Decision 4: Ship on the percent reading rather than hold for Q2

**Context**: Q2 (is utilisation 0-100 or 0-1?) could not be settled from the
near-idle account probed, and settling it needs one more billed request.
**Decision**: ship. The owner's steer was explicit — the spend in question is a
few Haiku requests, and holding a finished feature for it is disproportionate.
Mitigations rather than a guess left bare: the scale lives behind `_usage_pct()`
alone, so flipping it is a one-function change; and a value that is non-zero but
rounds to zero renders `<1%`, not `0%`, so the display never claims an account is
untouched when it is not. If real accounts show implausible figures, that is the
signal to flip it.
**Date**: 2026-08-17

## Success Criteria

- [x] Q1 answered from a HOST run, not inference
- [x] Q2 handled — deferred with mitigations (Decision 4), not silently guessed
- [x] Either cancelled with the refusal recorded, or usage shown on demand
- [x] No token value ever appears in a process argv or a committed file
- [x] Nothing fetches usage without an explicit human action
- [x] The per-press cost is visible in the UI, not buried in docs
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` clean

## Risks & Mitigations

| Risk                                            | Impact | Probability | Mitigation                                                                       |
| ----------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------- |
| ~~Setup-token also refused on `/v1/messages`~~  | —      | —           | **Retired** — F13: the HOST run returned 200 with the full header set            |
| ~~200 but no unified headers~~                  | —      | —           | **Retired** — F13: the headers were present                                      |
| Utilisation scale misread, misreporting by 100× | H      | M           | Q2 gate: no renderer ships until a value > 1 is observed and the scale is proven |
| Users press it habitually and burn quota        | M      | M           | Cost stated in the option text; result cached for the menu's lifetime            |
| Request shape wrong, read as a scope refusal    | M      | M           | Prototype prints the body and calls out the difference explicitly                |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00074-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Supersedes Plan 00073's Task 5.2; 00073 stays Cancelled
- `prototype.bash` verified against three stub response shapes
- **Q1 answered on the HOST: the approach works.** Where 00073 died on a scope
  refusal, this route returns 200 with the full unified header set
- Shipped in CCY 3.34.0 / `token-management.bash` 1.9.0 — awaiting HOST deploy
