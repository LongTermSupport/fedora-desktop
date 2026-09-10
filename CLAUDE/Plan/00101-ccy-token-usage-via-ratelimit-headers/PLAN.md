# Plan 00101: Token usage limits via rate-limit response headers, on demand

**Status**: In Progress
**Created**: 2026-08-17
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00100 tried to show each stored account's 5-hour and weekly utilisation in
the `ccy` token selection menu, and failed on a hard wall: `GET /api/oauth/usage`
returns **403 — "OAuth token does not meet scope requirement `user:profile`"**
for every stored `sk-ant-oat01` setup-token. The scope is fixed when
`claude setup-token` mints the token, so the free status route is unreachable.

The same figures, however, travel as **response headers on `/v1/messages`** —
which is precisely the scope an `oat01` token *does* hold. This plan tests that
route and, if it works, surfaces the numbers.

The critical difference from 00100 is **who pays and when**. Reading a status
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
- Not reviving `/api/oauth/usage` — Plan 00100 closed that definitively.

## Context & Background

**All established facts live in [FINDINGS.md](FINDINGS.md)** — F9 through F24,
with their sources. Cite IDs from there; do not restate them here.

The three that shape everything below:

- **Q1 is answered yes.** A stored setup-token gets the full header set from a
  bare `POST /v1/messages`, and the CLI has no path that prints them, so curl is
  the only vehicle (F13, F14).
- **Q2 is answered and now proven.** Utilisation is a fraction in `0`–`1`
  (F17, F20). Guessing wrong misreports usage by 100×, and it did for a week.
- **Q3 is open.** See below.

### Q3 — OPEN: ccy shows no Fable allowance. Is one on the wire?

Raised in use: the display has a 5-hour bar and a weekly bar, and says nothing
about the Fable limit. These facts come from reading the installed Claude Code
bundle (`@anthropic-ai/claude-code` v2.1.267) — a **reading, not an
observation**, which is what Q3 exists to settle.

Facts **F18–F22** in [FINDINGS.md](FINDINGS.md); bundle excerpts and schemas in
[RESEARCH-bucket-headers.md](RESEARCH-bucket-headers.md). In short: the Fable
bucket is `seven_day_overage_included`, suffix **`7d_oi`**, labelled "Fable
limit" by the bundle itself.

**What F18 implies and what it does not.** If the Fable figure arrives as
`7d_oi-utilization`, it is already on the response `_usage_fetch_one` makes, and
`_usage_extract` throws it away: that `case` matches five exact header names and
lets every other unified header fall through. The fix would then be a parser
change and two more cache fields, not a new request.

Three hypotheses, leading to different work:

- **H3** — a **Haiku** probe carries the `7d_oi` pair. Then ccy needs a parser
  change only, and the Fable bar costs nothing beyond the request already made.
  **Weakened by F23**: the one Haiku response seen did not carry it.
- **H4** — only a **Fable** probe carries it, i.e. the bucket set is scoped to
  the request's model. Then showing the bar costs a Fable request, which
  Decision 3 deliberately avoided, and the cost has to be put to the user.
- **H5** — Fable is metered against the overage-included bucket, so an account
  `out_of_credits` is refused Fable *and* has no `7d_oi` bucket to report
  (F23 carried exactly that pair). Then for such an account there is **no figure
  to show**, and the honest display is the reason, not a blank or a zero bar.
  That is a different piece of work from parsing a header.

F21 is the reason this is worth settling rather than assuming symmetry: the
buckets are **not** uniformly available. Fable has a utilisation header and Opus
does not, so "add the per-model buckets" is not one piece of work.

## Tasks

### Phase 1: Prototype ✅

- [x] ✅ **Task 1.1**: Mechanism confirmed from the binary — `claude -p` cannot
  surface the headers, so a direct call is required (F11, F12)
- [x] ✅ **Task 1.2**: `prototype.bash`, two arms — bare `curl` and
  `claude -p --model haiku` as a control — rendering the menu line, not a dump
- [x] ✅ **Task 1.3**: Verified against stubs (headers, 403, 200-without-headers).
  Two bugs found this way
- [x] ✅ **Task 1.4**: HOST run — **Q1 answered yes** (F13, F14, F15, F16)

### Phase 2: Decision gate ✅

- [x] ✅ **Task 2.1**: Cancel-on-refusal branch **not triggered** — the request
  succeeded rather than hitting a second scope wall
- [x] ✅ **Task 2.2**: Header names and formats recorded — epoch resets, float
  utilisation (F16), a wider set than assumed (F15)
- [x] ✅ **Task 2.3**: Q2 deferred by owner decision, not left open — Decision 4

### Phase 3: Human-triggered display in the selector 🔄

- [x] ✅ **Task 3.1**: `u) Show usage limits (costs 1 small API call per account)`
  added to `select_token()`, cost stated in the option text itself
- [x] ✅ **Task 3.2**: On press, fetches in parallel and **redraws the selector**
  with a usage column; the option then disappears so it cannot be double-spent
- [x] ✅ **Task 3.3**: 00100 machinery reused — parallel fan-out, visible
  degradation, a worker that cannot abort the menu. jq dropped: pure bash, one pass
- [x] ✅ **Task 3.4**: 15-minute cache TTL, long on purpose — in 00100 a miss cost
  latency, here it costs quota
- [x] ✅ **Task 3.5**: `CCY_VERSION` 3.34.0, `token-management.bash` 1.9.0
- [x] ✅ **Task 3.6**: `./scripts/qa-all.bash` green
- [x] ✅ **Task 3.7**: `acceptance.bash` verifies the deploy landed. Closes the
  00100 wrong-play gap `qa-deployed-drift.bash` cannot see, since it covers only
  `files/home/.local/bin/`
- [x] ✅ **Task 3.8**: Deployed on the HOST; all four accounts rendered

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
- [x] ✅ **Task 4.7**: Q2 settled from observed values — closed by Task 5.2, and
  proven outright by F20

### Phase 5: Every account reports `<1%` — Q2 comes due

Every account displayed `<1%` on both buckets — the exact signature Decision 4
named as the risk of shipping on the unproven `percent` reading. Two
hypotheses, not exclusive: **H1** the scale is `fraction`; **H2** the scale is
right and the *bucket* is wrong, because the probe uses Haiku. Narrative in
[JOURNAL/00101-Journal-26-08-18.md](JOURNAL/00101-Journal-26-08-18.md).

- [x] ✅ **Task 5.1**: `triage.bash` — raw values out of the existing cache, so
  the default run spends nothing; `--headers` spends one request to dump every
  `anthropic-ratelimit-*` header

- [x] ✅ **Task 5.2**: HOST run. Eight samples, four accounts, all `<= 1` (F17).
  **H1 holds; Q2 and Task 4.7 close with it.** H2 concerns which bucket, not the
  scale, and is untouched by this

- [x] ✅ **Task 5.3**: Default flipped to `fraction` (lib 1.11.0). Eight samples
  is not proof, so `_usage_scale_conflict` makes the inference self-refuting
  rather than clamping the bar and hiding a 100× error. (F20 has since supplied
  the proof; the guard stays, now covering a contract change instead)

- [x] ✅ **Task 5.5**: H2 answered by the API rather than by inference —
  `-representative-claim` captured as a fifth cache field, appended last so an
  older library reads a 4-field record unchanged, and shown as a dim
  `binding limit:` line. Unknown bucket names pass through verbatim (lib 1.12.0)

- [x] ✅ **Task 5.4**: A bucket the API did not report was silently dropped from
  the display, three lines below a comment saying that is what must not happen
  (lib 1.10.1)

### Phase 6: The Fable allowance — Q3 🔄

- [x] ✅ **Task 6.1**: `triage-buckets.bash` + `probe-bucket-headers.bash` — one
  billed request per probe model, dumping every `anthropic-ratelimit-*` header
  and tabulating which of the four buckets in F18 came back. Defaults to Haiku
  then Fable, so a single run separates H3 from H4. Accounts are reported as
  `account-N`, the token reaches curl via `--config` on stdin (BSH-09), and the
  header dump is checked for credential-shaped strings before anything is
  written. Host-only, enforced by `plan_require_host` — the CCY container has no
  token mounted, so a run there would report "no tokens" and that reads as a
  fact about the pool

- [x] ✅ **Task 6.2**: First HOST run, account-1 of 4. Results as F23/F24 below.
  **The gate is NOT passed**: the Haiku arm answered, the Fable arm did not

The Haiku arm answered — no `7d_oi`, no `overage`. The Fable arm returned 429
with no headers at all. **F25 then removed the innocent explanation**: the owner
confirms account-1 is Fable-entitled, so that 429 is a refusal of a permitted
request, not a lack of access. H3 is weakened, H4 is favoured, and H5 is now the
likeliest mechanism.

The first run recorded only the error *type*, which cannot separate "out of
credits" from "weekly Fable limit reached". The probe was amended to capture the
*message*, `retry-after`, `request-id` and `x-should-retry`, and to accept
`PROBE_ACCOUNT=all`. Three of the four accounts are unprobed.

- [x] ✅ **Task 6.2a**: Probing one account was the wrong default and produced a
  misleading answer — account-1 was out of credits, so its Fable arm said
  nothing about buckets. `PROBE_ACCOUNT` now defaults to **`all`**: the question
  is about the pool, and one member does not generalise to it. The report also
  gained a summary table, one row per probe, leading with the `7d_oi` column,
  and captures `error.details.error_code` (F27) — the field that actually
  separates a credits refusal from a weekly limit, and which the top-level
  `rate_limit_error` type cannot

- [ ] **Task 6.2b**: Run the sweep — `./triage-buckets.bash`, no arguments.
  **This is the decision gate.** It answers whether *any* account emits a
  `7d_oi` header, and reads the refusal reason on the ones that do not. Needs an
  account with usage credits to be conclusive, since F28 has the current pool
  out of them

- [ ] **Task 6.2c** (H5 only): if every account is refused for credits, then no
  `7d_oi` figure exists to display and Tasks 6.3/6.4 do not apply. The work
  becomes showing the **reason** — `overage-disabled-reason` is already in the
  response ccy fetches, and is already being discarded by the same `case` that
  discards `7d_oi`. A "Fable limit: no usage credits" row is honest; a blank is
  not, and a 0% bar would be a lie

- [ ] **Task 6.3** (H3): parse `7d_oi-utilization` / `-reset` in
  `_usage_extract` and render the bar. Two more cache fields, **appended last**
  so an older library reading a newer record still gets `u5`/`r5`/`u7` right —
  the same compatibility discipline as Task 5.5. Note the existing
  `IFS=$'\t' read -r u5 r5 u7 r7` in `triage.bash` already mis-parses the
  5-field record by folding `claim` into `r7`; fix it in the same change

- [ ] **Task 6.4** (H4 only): decide whether a Fable-model probe is worth its
  cost, and put the cost in the menu option text the way Decision 1 requires.
  **Not** a silent default — Decision 3 chose Haiku precisely to leave the
  expensive allowances alone

- [ ] **Task 6.5**: label the bucket "Fable limit" (F19), not a name invented
  here. `_usage_claim_label` already passes unknown claims through verbatim;
  give `seven_day_overage_included` its real label rather than letting it render
  as a raw token

## Technical Decisions

### Decision 1: Human-triggered, never automatic

**Context**: Plan 00100's version fetched on every launch, which was free there.
Here every fetch spends billed quota out of the allowance it reports.
**Decision**: a keypress in the selector, never a launch-time fetch. The cost is
stated in the option text so the user knows what pressing it does. This also
removes the whole class of problems 00100 hit — no launch latency, no timeout
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
     JOURNAL/00101-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Supersedes Plan 00100's Task 5.2; 00100 stays Cancelled
- `prototype.bash` verified against three stub response shapes
- **Q1 answered on the HOST: the approach works.** Where 00100 died on a scope
  refusal, this route returns 200 with the full unified header set
- Shipped in CCY 3.34.0 / `token-management.bash` 1.9.0; deployed and confirmed
  working against real accounts
- Display rewritten as aligned coloured bars in CCY 3.35.0 / lib 1.10.0 after the
  first version proved unreadable (`r4h`)
