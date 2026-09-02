# Plan 00100: Show per-token usage limits at CCY token selection

**Status**: Cancelled
**Created**: 2026-08-17
**Completed**: 2026-08-17
**Owner**: joseph
**Priority**: Medium

> **Outcome: the feature is impossible with the credential ccy stores, and has
> been removed.** `GET /api/oauth/usage` answers **403 — "OAuth token does not
> meet scope requirement `user:profile`"** for every stored `sk-ant-oat01`
> setup-token, as does `/api/oauth/profile`. That scope is fixed when
> `claude setup-token` mints the token, so no client-side change can obtain it.
>
> Cancelled per this plan's own pre-committed decision rule (Task 4.3), written
> before the answer was known. The gate worked exactly as designed.
>
> **Do not re-add a call to those routes with a setup-token.** A viable
> alternative exists and is specified below — see "Not dead, but different".

## Overview

`ccy` stores multiple named Claude Code OAuth tokens (one per account) under
`~/.claude-tokens/ccy/tokens/` and prompts for one at launch. The menu built by
`select_token()` (`files/var/local/claude-yolo/lib/token-management.bash`) shows
only the token **name** and its **expiry date** — nothing about how much of that
account's 5-hour or weekly allowance is already spent. Picking the right account
is therefore guesswork, and the usual failure mode is starting a long session on
an account that is already near its limit.

Claude Code itself knows this figure: it fetches `GET /api/oauth/usage` on the
user's behalf. This plan establishes whether the **long-lived setup-token** that
`ccy` stores can read that endpoint, and if so surfaces the numbers at selection
time.

The endpoint is internal and unversioned, so the plan is deliberately staged:
prove it works with a stored token first, ship an on-demand command second, and
only then consider putting a network call on the interactive launch path.

## Goals

- Determine, from a real HOST probe, whether a stored `sk-ant-oat01-…` token is
  accepted by `GET /api/oauth/usage` — and capture the exact response shape.
- If accepted: surface 5-hour and weekly utilisation per token on demand.
- If accepted and cheap enough: annotate the launch selection menu, with caching,
  so the numbers cost nothing on repeat launches.
- Degrade **visibly** (`usage: unavailable`), never silently, and never block a
  launch when the endpoint is unreachable or has changed shape.

## Non-Goals

- Reimplementing Claude Code's `/usage` REPL view or its historical breakdown.
- Any local parsing of `.jsonl` transcripts to estimate spend — the limits are
  server-side and per-account; a local estimate would be a different number
  wearing the same label.
- Enforcing or acting on a limit (auto-switching tokens, refusing a launch).
  This plan reports; the choice stays the user's.

## Context & Background

### Established facts

Read directly out of the shipped native binary
(`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`,
version 2.1.233, build `f8d5756`) and one live probe.

| ID  | Fact                                                                                                                                                                                                                    | Source                                                 |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| F1  | Claude Code fetches usage via `GET /api/oauth/usage` with `Content-Type: application/json`, a 5 s timeout and `refreshOAuth`, logging `fetchUtilization: GET …`                                                         | JS bundle in `claude.exe` (function `sje`)             |
| F2  | Each limit entry carries `kind`, `percent`, `resets_at`, and an optional `scope.model.display_name`                                                                                                                     | JS bundle, mapper `eQt`                                |
| F3  | Limit kinds present: `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet`, `seven_day_overage_included`, plus `org_spend_cap_reached`                                                                         | string table in `claude.exe`                           |
| F4  | The route exists — an unauthenticated `GET` returns HTTP **429** `rate_limit_error`, not 404                                                                                                                            | live probe from the CCY container                      |
| F5  | There is **no** supported CLI route: `claude --help` lists no `usage` subcommand, and the statusline `rate_limits` block is documented as populated only *after* the first API response of a session                    | `claude --help`; statusline schema doc in `claude.exe` |
| F6  | With an `Authorization: Bearer` header present the endpoint returns a real **auth verdict** — a bogus bearer gets **401**, not the IP-level 429 of F4                                                                   | live probe, 5 synthetic bearers                        |
| F7  | The feature is confirmed **deployed and running** on the HOST: `ccy --list-tokens` renders a `Usage:` line per token                                                                                                    | HOST run after `deploy.bash`                           |
| F8  | First HOST run (parallel burst): 3 tokens → 401, 1 → 429. The 429 was **our own burst throttle**, not a verdict — sequential probing returned 403 for that token too                                                    | HOST `ccy --list-tokens`, then `triage.bash`           |
| F9  | **DECISIVE.** Sequential probe, all 4 stored tokens, both routes: `/api/oauth/usage` **403** and `/api/oauth/profile` **403**, body `permission_error` — *"OAuth token does not meet scope requirement `user:profile`"* | HOST `triage.bash`, `logs/token-usage-triage.log`      |
| F10 | The unified rate-limit figures also travel as **response headers** — `anthropic-ratelimit-unified-5h-utilization`, `-7d-utilization`, `-5h-reset`, `-7d-reset`, `-status`, and ~18 more                                 | string table in `claude.exe`                           |

F5 is why this plan read an internal endpoint at all: nothing supported reports
the figure before a session starts.

### Q1 — answered: NO

**F9 settles it.** The refusal is not "wrong credential type" but a named missing
scope: a setup-token *is* an OAuth token, it simply lacks `user:profile`, which
both `/usage` and `/profile` require. That scope is fixed when
`claude setup-token` mints the token — there is no client-side change, header, or
flag that can add it. Q2 (envelope) and Q3 (per-token vs per-account) are moot.

The feature was therefore removed, per the decision rule this plan committed to
before the answer was known.

### Not dead, but different — the header route (F10)

The data *is* reachable with a setup-token, just not on the OAuth routes. The
unified figures come back as **response headers on `/v1/messages`**, which is
exactly the scope an `oat01` token does hold. A `max_tokens: 1` request per token
would yield `anthropic-ratelimit-unified-5h-utilization` and friends.

**Not implemented, deliberately.** It costs a real, billed API request per token
per launch, and those requests consume a sliver of the very quota being measured.
That is a materially different bargain from reading a free status endpoint, and
it is the owner's call rather than a silent scope expansion. Recorded as Task 5.2
with everything needed to build it.

## Tasks

Blow-by-blow detail — measurements, the two implementation bugs testing caught,
and the wrong-play incident — is in `JOURNAL/`, not restated here.

### Phase 1: Establish the facts ✅

- [x] ✅ **Task 1.1**: Confirm what the menu shows today (name + expiry only)
- [x] ✅ **Task 1.2**: Establish F1–F5 from the shipped binary
- [x] ✅ **Task 1.3**: Write `triage.bash` (sequential probes, bearer never in argv,
  self-verifying redaction, deployment-state Q0 section, route discriminator)
- [x] ✅ **Task 1.4**: Run it on the HOST — gave F9, the decisive answer

### Phase 2: Decision gate ✅

- [x] ✅ **Task 2.1**: Q1 is a refusal → record the finding, remove the code, mark
  the plan Cancelled. Rule was written before the answer was known
- [x] ❌ **Task 2.2**: N/A — Q1 did not succeed, so there is no schema to confirm

### Phase 3: Built, measured, then removed ✅

Implemented and shipped as CCY 3.32.0 / lib 1.7.0 at the owner's direction, then
removed in 3.33.0 / lib 1.8.0 once F9 landed.

- [x] ✅ **Task 3.1–3.8**: fetch/cache/render core, parallel fan-out, TTL cache,
  colour by worst percentage, `CCY_TOKEN_USAGE=0` kill switch, version bumps,
  cold/warm/failure measurements, QA green

### Phase 4: Confirm on the HOST ✅

- [x] ✅ **Task 4.0**: Add `deploy.bash` naming the correct play. The first attempt
  named `play-claude-code.yml`, which only *asserts* the lib exists and deploys
  none of it — it ran green while the host kept the old library
- [x] ✅ **Task 4.1**: Deploy via `play-claude-yolo.yml` — confirmed live (F7)
- [x] ✅ **Task 4.2**: `ccy --list-tokens` — gave F8, correctly treated as unsettled
- [x] ✅ **Task 4.2b**: `triage.bash` on the HOST — F9, and the 429 retired as our
  own burst throttle
- [x] ✅ **Task 4.3**: Strip the feature back out; record the finding; cancel
  - [x] ✅ Revert `lib/token-management.bash` and `claude-yolo` to pre-feature state
  - [x] ✅ Bump forward: CCY 3.33.0, lib 1.8.0, both stating *why* — so the next
    reader does not re-derive this from scratch
  - [x] ✅ Add a `play-claude-yolo.yml` cleanup task removing the 3.32.0
    `usage-cache/` directory from hosts that ran it
- [x] ❌ **Task 4.4**: N/A — Q1 did not succeed
- [ ] ⬜ **Task 4.5**: `qa-reviewer` over the full diff — **not run**; this session
  is configured not to invoke agents unprompted. Worth running before the branch
  merges

### Phase 5: Follow-up recorded, not yet actioned

- [x] ❌ **Task 5.0**: N/A — the parallel-burst concern died with the feature. The
  429 it predicted did occur, and sequential probing confirmed it as ours
- [ ] ⬜ **Task 5.2**: **The header route (F10) — the one live option left.** Read
  the unified figures from `/v1/messages` response headers, which an `oat01`
  token can reach: `anthropic-ratelimit-unified-5h-utilization`,
  `-7d-utilization`, `-5h-reset`, `-7d-reset`, `-status`. Everything else built
  here is reusable — the parallel fan-out, the TTL cache, the render-once design,
  the colour rule, the kill switch (`git show 53a5a10` has the lot). **The cost
  is the decision**: one billed `max_tokens: 1` request per token per launch,
  spending a sliver of the quota being measured. Cheapest first step is one
  manual `curl -sD-` with a stored token to confirm the headers actually come
  back on a subscription credential
- [ ] ⬜ **Task 5.1**: `scripts/qa-deployed-drift.bash` (Plan 00099) compares only
  `files/home/.local/bin/` against its deployed copies. It does not cover
  `files/var/local/claude-yolo/`, which is why an undeployed library here was
  invisible to QA. Widening it would have caught this class of confusion at
  commit time — **owner's call**, since it changes a gate another plan owns

## Technical Decisions

### Decision 1: Remove rather than leave a permanently-dim row

**Context**: With F9, every row would read `usage: not authorised` forever.
**Decision**: remove the feature outright. A failure note is the right behaviour
for a *transient* fault; as a permanent state it is just noise on every launch
teaching users to ignore the field. The removal carries its reason in both
version headers so the next reader does not re-derive F9 from scratch, and a
playbook cleanup task deletes the 3.32.0 cache directory from hosts that ran it.
**Date**: 2026-08-17

### Decision 2: Do not pivot to the header route unasked

**Context**: F10 shows the figures are obtainable via `/v1/messages` response
headers, which a setup-token *can* reach.
**Decision**: record it, do not build it. It spends billed API requests — and a
sliver of the very quota being measured — on every launch. The owner's "try it
on by default" was a judgement about *latency*, not about consuming quota; those
are different bargains and the second one is theirs to make. Task 5.2.
**Date**: 2026-08-17

### Decision 3: A failed usage fetch must not fail the launch (as built)

**Context**: The repo's #1 rule is fail-fast; this appeared to violate it.
**Decision**: usage display was **decoration**, the same class as the colourised
expiry — it rendered a visible `usage: …` note and the launch proceeded. Kept
here because it was load-bearing: it is why shipping ahead of the Q1 answer was
safe, and why the 403s surfaced as a legible row rather than a broken menu.
**Date**: 2026-08-17

## Success Criteria

- [x] Q1 answered from a HOST `triage.bash` run, not from inference — **F9**
- [x] Cancelled with the refusal recorded, verbatim, including the scope name
- [x] No token value ever appeared in a process argv or in a committed file
- [x] `CCY_VERSION` bumped for every change under `files/var/local/claude-yolo/`
- [x] The removal leaves no orphan state: cache directory cleaned up by playbook
- [x] `./scripts/qa-all.bash` passes
- [ ] `qa-reviewer` agent — not run (see Task 4.5)

## Risks & Mitigations

The first risk was the one that fired, at the probability it was given.

| Risk                                                 | Impact | Probability | Outcome                                                                                       |
| ---------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------- |
| Setup-tokens are scoped and cannot read the endpoint | H      | M           | **FIRED** — 403, missing scope `user:profile`. Gate held; feature removed, no workaround      |
| Endpoint shape changes in a future release           | M      | H           | Moot. The envelope-agnostic renderer was never exercised against a real payload               |
| Launch latency from N HTTPS calls                    | M      | M           | Did not fire — parallel + TTL cache measured at +371 ms cold / +80 ms warm for 5 tokens       |
| Token leaks into argv or into the plan log           | H      | L           | Did not fire. Bearer only ever via `curl --config -` on stdin; triage's redaction self-checks |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00100-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- `933b731` — plan + `triage.bash`; facts F1–F5 from the shipped binary
- `53a5a10` — feature implemented, CCY 3.32.0 / lib 1.7.0 (**reference for the
  Task 5.2 header route — the fan-out, cache and render design are all here**)
- `d7bd318` — `deploy.bash` + the Q0 deployment probe, after the wrong play ran
- `b83121f` — triage extended (sequential, 429 retry, route discriminator)
- **F9 on the HOST: 403, scope `user:profile` missing → cancelled**
- Removal: CCY 3.33.0 / lib 1.8.0, both headers carrying the reason; playbook
  cleanup task for the 3.32.0 cache directory
