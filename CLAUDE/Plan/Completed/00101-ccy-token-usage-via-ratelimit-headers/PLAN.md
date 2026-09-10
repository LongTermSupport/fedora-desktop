# Plan 00101: Token usage limits via rate-limit response headers, on demand

**Status**: Complete — 2026-09-10. Phases 1–5 shipped and deployed. **Phase 6
(the Fable allowance) is WON'T DO**, on owner decision after the options were
exhausted; the reason is structural and recorded in Phase 6.
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

**F36 reframes the question, and mostly answers it.** Anthropic documents that
on Max, Fable is included **up to 50% of the weekly usage limit** — a ceiling
*inside* the single weekly allowance, not a second allowance. So there is no
separate Fable limit for ccy to display: Fable spends the same pool ccy already
draws as its weekly bar. What is missing is only *how much of that pool was
Fable*, a sub-cap gauge — which is the most plausible reading of `7d_oi`, and
still unobserved. F37 is the standing caveat: the whole unified header family is
undocumented and unsupported.

**What F18 implies and what it does not.** If the Fable figure arrives as
`7d_oi-utilization`, it is already on the response `_usage_fetch_one` makes, and
`_usage_extract` throws it away: that `case` matches five exact header names and
lets every other unified header fall through. The fix would then be a parser
change and two more cache fields, not a new request.

Three hypotheses, leading to different work:

- ~~**H3** — a Haiku probe carries the `7d_oi` pair~~ — **REFUTED by F29**:
  three clean 200s, absent on every one. The cheap probe cannot show the bar.
- **H4** — only a **Fable** probe carries it, the window being per-model (F35).
  The bar would then cost a Fable request, which Decision 3 avoided, and that
  cost goes to the user. **Untested** — no valid Fable request has been made yet.
- ~~**H5** — Fable is metered against the credits bucket~~ — **STRUCK**: it
  conflated "Fable limit" with "usage credit limit", two separate buckets, one
  subscription and one API-billing. See [FINDINGS.md](FINDINGS.md).

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

### Phase 6: The Fable allowance — Q3 ❌ WON'T DO

**Closed 2026-09-10 on owner decision**: *"i think we've exhausted options lets
mark this as wont do"*. Agreed, and the reason is structural rather than a lack
of effort — **two independent blocks, neither closable from ccy**:

- **F9** — stored `sk-ant-oat01` setup-tokens are refused on
  `GET /api/oauth/usage` for scope, and the scope is fixed when
  `claude setup-token` mints it. That endpoint is the only source of the
  per-model rows (F22, F40). No client-side change reaches it.
- **F29** — no rate-limit header carries the Fable window. Measured, not
  assumed: eight probes, four accounts, three clean 200s, `7d_oi` absent on
  every one.

**It is also less valuable than it looked when raised.** F36/F36a establish that
Fable has no separate allowance on Max — it is a 50% ceiling on Fable's *own*
share of the one weekly pool that ccy already displays. So the bar was never
missing a limit, only a breakdown. And F40 shows the breakdown is already
available in three first-party places: `/usage`, the VS Code usage dialog, and
claude.ai Settings → Usage.

**If revisited, start by re-checking setup-token scopes**, not the header route.
Everything else follows from that one fact. Keep the probe scripts — the
corrected Fable request body (F33) was expensive to find and would otherwise be
rediscovered the hard way.

**Done before closure**: the probe pair (`triage-buckets.bash` +
`probe-bucket-headers.bash` — host-only, `account-N` redaction, every dump
screened for credential-shaped strings), two host sweeps, and three corrections
to the probe itself — sweep the whole pool by default, capture
`error.details.error_code`, and send a *valid* Fable body. Narrative in the
[2026-09-10 journal](JOURNAL/00101-Journal-26-09-10.md).

**Not done, deliberately**: the corrected Fable re-run, parsing `7d_oi`, costing
a Fable probe, and labelling the bucket. Every one was downstream of a figure
that has no route into ccy.

- [x] ✅ **Task 6.6**: fixed while closing — `triage.bash` read a 5-field cache
  record into 4 names, folding `claim` into `r7`, so its 7-day reset column was
  wrong. Found in Task 6.3's notes and repaired rather than left recorded as a
  known defect in a closing plan

## Technical Decisions

Five decisions, with their context and outcomes, in
[DECISIONS.md](DECISIONS.md):

1. **Human-triggered, never automatic** — a keypress, never a launch-time fetch,
   because every fetch spends the allowance it reports.
2. **Direct HTTP, not `claude -p`** — the CLI has no path that prints the
   headers (F12/F14), and the bare request is the more minimal one anyway.
3. **Probe with Haiku** — cheapest model for the 5-hour and weekly buckets.
   Amended: insufficient for the Fable window (F29), and Fable cannot use the
   1-token body at all (F33).
4. **Ship on the percent reading rather than hold for Q2** — wrong, as its own
   mitigation predicted, and recoverable in one function because of it.
5. **Undocumented surface, so degrade rather than assume** — absence is a normal
   state (F35/F37), and no row is ever synthesised from a neighbouring header.

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
