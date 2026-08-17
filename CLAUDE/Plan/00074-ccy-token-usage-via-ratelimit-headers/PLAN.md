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

### Open question

| ID  | Question                                                                                                            | How it gets answered   |
| --- | ------------------------------------------------------------------------------------------------------------------- | ---------------------- |
| Q1  | Does a bare `POST /v1/messages` with a stored setup-token succeed, and does the response carry the unified headers? | `prototype.bash`, HOST |

A 403 naming a scope would kill this the same way 00073 died. A 4xx complaining
about the *request shape* is fixable and must not be mistaken for the former.

## Tasks

### Phase 1: Prototype 🔄

- [x] ✅ **Task 1.1**: Confirm the mechanism from the binary (F11, F12) — establish
  that `claude -p` cannot surface headers, so a direct call is required
- [x] ✅ **Task 1.2**: Write `prototype.bash` with two arms — bare `curl` and
  `claude -p --model haiku` as a control — rendering the exact menu line ccy
  would show, not just a header dump
- [x] ✅ **Task 1.3**: Verify against stubs: headers present, 403, and
  200-without-headers all render legibly. Two bugs found and fixed this way
- [ ] 🔄 **Task 1.4**: Run `prototype.bash` on the HOST — **HOST action**

### Phase 2: Decision gate

- [ ] ⬜ **Task 2.1**: If the headers do not come back — record why and cancel. Do
  not engineer around a second scope refusal
- [ ] ⬜ **Task 2.2**: If they do — record the confirmed header names and value
  formats (integer vs float percent, epoch vs ISO reset) before writing the code

### Phase 3: Human-triggered display in the selector

- [ ] ⬜ **Task 3.1**: Add a `u) Show usage (one API call per account)` option to
  `select_token()`, with the cost stated in the option text itself
- [ ] ⬜ **Task 3.2**: On press, fetch in parallel (Plan 00073 measured 203 ms for
  5 tokens) and **redraw the selector** with a usage column
- [ ] ⬜ **Task 3.3**: Reuse the 00073 machinery from `git show 53a5a10` — parallel
  fan-out, render-once-at-fetch, worst-percentage colouring, visible degradation
- [ ] ⬜ **Task 3.4**: Cache the result for the lifetime of the menu, so a second
  press does not spend a second round of quota
- [ ] ⬜ **Task 3.5**: Bump `CCY_VERSION` and the `token-management.bash` header
- [ ] ⬜ **Task 3.6**: `./scripts/qa-all.bash`, then the `qa-reviewer` agent

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

## Success Criteria

- [ ] Q1 answered from a HOST run, not inference
- [ ] Either cancelled with the refusal recorded, or usage shown on demand
- [ ] No token value ever appears in a process argv or a committed file
- [ ] Nothing fetches usage without an explicit human action
- [ ] The per-press cost is visible in the UI, not buried in docs
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` clean

## Risks & Mitigations

| Risk                                              | Impact | Probability | Mitigation                                                                            |
| ------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------- |
| Setup-token also refused on `/v1/messages`        | H      | L           | Q1 gate; a scope refusal cancels the plan. Low: this is the scope such tokens are for |
| 200 but no unified headers (plan/threshold-gated) | H      | M           | Prototype distinguishes it from a refusal and says to re-run before concluding        |
| Users press it habitually and burn quota          | M      | M           | Cost stated in the option text; result cached for the menu's lifetime                 |
| Request shape wrong, read as a scope refusal      | M      | M           | Prototype prints the body and calls out the difference explicitly                     |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00074-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Supersedes Plan 00073's Task 5.2; 00073 stays Cancelled
- `prototype.bash` verified against three stub response shapes
