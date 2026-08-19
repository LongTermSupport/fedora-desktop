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

### Q2 — ANSWERED: the scale is a fraction (`0`–`1`)

| ID  | Fact                                                                                                                                              | Source           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| F17 | **Eight samples across four accounts, every raw value between `0.04` and `0.41`** — all `<= 1`. Read as fractions: 14%/9%, 6%/4%, 8%/41%, 15%/13% | HOST triage.bash |

Q2 was not cosmetic: guessing wrong misreports usage by 100×, and it did — the
plan shipped on `percent` and every account displayed `<1%` for a week.

**The discriminator is one-way**: any value **greater than 1** proves the
`0`–`100` scale, because a fraction cannot exceed 1. **None was observed**, so
F17 is strong evidence rather than proof. Two things make it decisive enough to
act on: under `percent` these four accounts have used 0.04%–0.41% of their
allowances on a machine running ccy sessions all day, and the fraction reading
puts the weekly bucket *above* the 5-hour one on the busiest account, which is
the shape mid-week usage actually has.

Acted on, and made **self-refuting** rather than left as an assumption — see
Task 5.3. The read cost nothing: `triage.bash` took the values out of the cache
the user had already paid for.

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
- [x] ✅ **Task 3.8**: Deployed on the HOST; all four accounts rendered. Feature
  confirmed working against real accounts

### Phase 4: Legible display 🔄

- [x] ✅ **Task 4.1**: Replace the compressed one-liner (`5h <1% r4h · wk <1% r6d`)
  with per-limit bars, coloured fill on a dim track, aligned across accounts
- [x] ✅ **Task 4.2**: Spell reset times out — "resets in 4 hours", not `r4h`,
  with singular/plural handled
- [x] ✅ **Task 4.3**: Cache the values rather than a rendered line; interpret the
  scale at DISPLAY time so `CCY_USAGE_SCALE` applies to cached data without a
  refetch (a refetch would cost quota)
- [x] ✅ **Task 4.4**: `CCY_USAGE_DEBUG=1` shows the value as the API sent it,
  settling Q2 from the fetch the user already paid for
- [x] ✅ **Task 4.5**: Retarget `acceptance.bash` — it checked for symbols this
  rewrite removed, so it would have failed a correct deploy
- [x] ✅ **Task 4.6**: `CCY_VERSION` 3.35.0, lib 1.10.0, docs + changelog
- [ ] 🔄 **Task 4.7**: Redeploy, then `CCY_USAGE_DEBUG=1 ccy` and press `u` to
  settle Q2 from an observed value — **HOST action**

### Phase 5: Every account reports `<1%` — Q2 comes due

Reported in use: **every** account displays `<1%` on both buckets. That is the
exact signature Decision 4 accepted as the risk of shipping on the unproven
`percent` reading — a 0-1 fraction rendered as if it were already 0-100 puts any
real usage below 0.5 and therefore under the `<1%` guard. It is **not** the only
explanation, so this phase measures rather than assumes:

- **H1** — the scale is `fraction`. A raw value ≤ 1 on every account and every
  bucket is consistent with it; a value > 1 anywhere kills it outright.

- **H2** — the numbers are right and the *bucket* is wrong. The probe uses Haiku
  because the weekly buckets are per-model, so a heavy Opus user could genuinely
  sit near zero on Haiku's weekly allowance while their real limit is elsewhere.
  H2 and H1 are not exclusive.

- [x] ✅ **Task 5.1**: `triage.bash` — reads the raw values **out of the existing
  cache**, so the default run spends nothing; prints RAW alongside what ccy shows
  today and what it would show under `fraction`. `--headers` spends exactly one
  request to dump *every* `anthropic-ratelimit-*` header, which the cache cannot
  answer because it keeps only the four values displayed

- [x] ✅ **Task 5.2**: Ran on the HOST. **Eight samples across four accounts,
  every raw value between 0.04 and 0.41 — all \<= 1.** Read as fractions: 14%/9%,
  6%/4%, 8%/41%, 15%/13%, with the weekly bucket above the 5-hour one on the
  busiest account, which is the shape mid-week usage has. Read as percentages,
  four accounts have used 0.04%-0.41% of their allowances on a machine that runs
  ccy sessions all day. **H1 holds; Q2 and Task 4.7 close with it.** H2 is not
  refuted and does not need to be — it concerns *which* bucket the weekly figure
  describes, not the scale

- [x] ✅ **Task 5.3**: Default flipped to `fraction` (lib 1.11.0), with the
  evidence recorded at the switch rather than in a commit message. **Eight
  samples all \<= 1 is not proof** — one value above 1 would settle it outright
  and none was seen — so `_usage_scale_conflict` makes the inference
  self-refuting: a raw value the assumed scale cannot produce prints
  `SCALE MISMATCH` naming the value and the override, instead of clamping the bar
  to 100%. An unfalsifiable premise is indistinguishable from a wrong one, and
  the clamp is what would have hidden a 100x error indefinitely

- [x] ✅ **Task 5.5**: H2 — the display now answers it instead of leaving it to
  inference. `-representative-claim` names the bucket the API considers binding
  and was recorded as **F15** when the header set was first mapped, then never
  captured. Now extracted into a fifth cache field (appended last, so a 4-field
  record from an older library renders unchanged) and shown as a dim
  `binding limit:` line. A `seven_day_opus` claim states outright that the weekly
  figure above it is not the allowance being reported against. Unknown bucket
  names pass through verbatim rather than being dropped — a future
  `seven_day_haiku` is more useful visible than silently absent. Verified against
  seven stub shapes including a tab-injected claim value and the 4-field
  backward-compatibility path (lib 1.12.0)

- [x] ✅ **Task 5.4**: Fix found while reading: a bucket the API did not report
  was silently dropped from the display, three lines below a comment saying that
  is exactly what must not happen (lib 1.10.1)

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
- Shipped in CCY 3.34.0 / `token-management.bash` 1.9.0; deployed and confirmed
  working against real accounts
- Display rewritten as aligned coloured bars in CCY 3.35.0 / lib 1.10.0 after the
  first version proved unreadable (`r4h`)
