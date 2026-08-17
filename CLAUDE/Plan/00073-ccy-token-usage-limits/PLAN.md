# Plan 00073: Show per-token usage limits at CCY token selection

**Status**: In Progress
**Created**: 2026-08-17
**Owner**: joseph
**Priority**: Medium

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

| ID  | Fact                                                                                                                                                                                                 | Source                                                 |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------ |
| F1  | Claude Code fetches usage via `GET /api/oauth/usage` with `Content-Type: application/json`, a 5 s timeout and `refreshOAuth`, logging `fetchUtilization: GET …`                                      | JS bundle in `claude.exe` (function `sje`)             |
| F2  | Each limit entry carries `kind`, `percent`, `resets_at`, and an optional `scope.model.display_name`                                                                                                  | JS bundle, mapper `eQt`                                |
| F3  | Limit kinds present: `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet`, `seven_day_overage_included`, plus `org_spend_cap_reached`                                                      | string table in `claude.exe`                           |
| F4  | The route exists — an unauthenticated `GET` returns HTTP **429** `rate_limit_error`, not 404                                                                                                         | live probe from the CCY container                      |
| F5  | There is **no** supported CLI route: `claude --help` lists no `usage` subcommand, and the statusline `rate_limits` block is documented as populated only *after* the first API response of a session | `claude --help`; statusline schema doc in `claude.exe` |
| F6  | With an `Authorization: Bearer` header present the endpoint returns a real **auth verdict** — a bogus bearer gets **401**, not the IP-level 429 of F4                                                | live probe, 5 synthetic bearers                        |

F5 is why this plan reads an internal endpoint at all: nothing supported reports
the figure before a session starts.

F6 matters for the gate below: because a bearer produces 401 rather than the
shared-IP 429, a real token on the HOST yields an unambiguous answer — 200 or 401,
with no third "can't tell" outcome to re-run around.

### Open questions

| ID  | Question                                                                                                                          | How it gets answered                               |
| --- | --------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| Q1  | Does a stored long-lived `sk-ant-oat01-…` setup-token authenticate to `/api/oauth/usage`, or is it scoped to `/v1/messages` only? | `triage.bash`, or `ccy --list-tokens`, on the HOST |
| Q2  | What is the exact JSON envelope — a bare array, or an object wrapping one?                                                        | `triage.bash` on the HOST                          |
| Q3  | Does the endpoint answer per-**token**, or per-**account** (two tokens for one account reporting one figure)?                     | comparing rows in either, on the HOST              |

Q1 is the gate. Everything downstream is dead if it comes back 401/403, and the
correct outcome in that case is to strip the feature back out and cancel — not to
reach for a workaround, and not to leave a permanently-dim row on every launch.

**Q2 is de-risked rather than blocking.** The renderer never assumes an envelope:
it walks the whole document for any object carrying both `kind` and `percent`, so
it works against `{"rate_limits":[…]}`, a bare array, or anything else that keeps
the field names Claude Code's own mapper relies on. Verified against three
fixtures including an error body. Only the field names need to hold.

The container cannot answer Q1 itself: it holds no token and no
`~/.claude/.credentials.json`. The code shipped ahead of that answer at the
owner's direction (see Decision 1) — which is safe here precisely because the
feature is decoration that degrades visibly: if Q1 comes back 401, every row
reads `usage: not authorised` and nothing else changes. Task 4.3 then removes it
rather than leaving that in place.

## Tasks

### Phase 1: Establish the facts (HOST)

- [x] ✅ **Task 1.1**: Read `select_token()` / `list_tokens()` and confirm what the
  menu shows today (name + colourised expiry only)
- [x] ✅ **Task 1.2**: Confirm from the shipped binary how Claude Code fetches usage,
  and that no supported CLI route exists (F1–F5)
- [x] ✅ **Task 1.3**: Write `triage.bash` — probe every stored token against
  `/api/oauth/usage`, report HTTP status and response shape, redact the bearer.
  Exercised end to end against a stub endpoint; shellcheck-clean
- [ ] 🔄 **Task 1.4**: Run `triage.bash` on the HOST and record Q1/Q2/Q3 in the
  journal — **HOST action, blocked on the container boundary**

### Phase 2: Decision gate

- [ ] ⬜ **Task 2.1**: If Q1 is a refusal — record the finding, mark the plan
  Cancelled, and stop. Do not engineer around a scoped credential
- [ ] ⬜ **Task 2.2**: If Q1 succeeds — write the confirmed response schema into this
  plan before any parsing code is written

### Phase 3: Usage in the menu, on by default

- [x] ✅ **Task 3.1**: Add the fetch/cache/render core to
  `files/var/local/claude-yolo/lib/token-management.bash`, passing the token via
  `curl --config -` on stdin so it never reaches argv (the BSH-09 rule already
  applied in `validate_token()`)
- [x] ✅ **Task 3.2**: Annotate `select_token()` rows and `list_tokens()` with
  5-hour and weekly utilisation, fanning the fetches out in parallel
- [x] ✅ **Task 3.3**: Cache under `~/.claude-tokens/ccy/usage-cache/` with a TTL,
  storing only the rendered line — never the raw response
- [x] ✅ **Task 3.4**: Colour the line by its worst percentage, in pure bash
- [x] ✅ **Task 3.5**: `CCY_TOKEN_USAGE=0` kill switch, plus `CCY_USAGE_TTL`,
  `CCY_USAGE_TIMEOUT`, `CCY_USAGE_CONNECT_TIMEOUT` overrides, all documented in
  `ccy --help`
- [x] ✅ **Task 3.6**: Bump `CCY_VERSION` (3.32.0) and the `token-management.bash`
  header version (1.7.0)
- [x] ✅ **Task 3.7**: Measure cold/warm/failure paths and confirm a broken
  endpoint still renders the menu
- [x] ✅ **Task 3.8**: Run `./scripts/qa-all.bash`

### Phase 4: Confirm on the HOST

- [ ] 🔄 **Task 4.1**: Deploy `play-claude-code.yml` on the HOST — **HOST action**
- [ ] ⬜ **Task 4.2**: Run `ccy --list-tokens` and record the real status per token.
  A `200` answers Q1 yes; `usage: not authorised` on every row answers it no
- [ ] ⬜ **Task 4.3**: If Q1 is no — strip the feature back out rather than leaving
  a permanently-dim row on every launch; record the finding and cancel
- [ ] ⬜ **Task 4.4**: If Q1 is yes — confirm the rendered line against the real
  envelope, and record the confirmed schema here
- [ ] ⬜ **Task 4.5**: Run the `qa-reviewer` agent over the full diff

## Technical Decisions

### Decision 1: On by default in the menu — revised after measurement

**Context**: The first draft of this plan sequenced an on-demand command ahead of
menu integration, on the argument that N HTTPS calls to an undocumented endpoint
should not sit on the path of the most-run interactive command. The owner asked
to try it on by default and fall back to human-triggered only if it proved slow.
That reframed the question from a judgement call to a measurable one.

**Measured** (this container, real endpoint, 5 tokens, menu render wall-clock):

| Path                        | Wall   | Over baseline                        |
| --------------------------- | ------ | ------------------------------------ |
| Feature disabled (baseline) | 142 ms | —                                    |
| Cold — full fetch of 5      | 513 ms | +371 ms                              |
| Warm — cache hit            | 222 ms | +80 ms                               |
| Unroutable endpoint         | ~2.2 s | bounded by connect-timeout, parallel |

**Decision**: on by default. +371 ms once per TTL window and +80 ms warm is not a
perceptible cost on an interactive menu the user is about to read anyway, and the
figure is most valuable at exactly the moment of choosing. The original concern is
answered by construction rather than by sequencing: the fetch is parallel, bounded
by a connect-timeout, cached, and every failure path renders a dim `usage: …` note
instead of blocking. `CCY_TOKEN_USAGE=0` turns it off outright.
**Date**: 2026-08-17

### Decision 1a: Parse once at fetch time, not per row at render time

**Context**: The first implementation cached the raw JSON and ran `jq` per row.
Measured, that made a **cache hit almost as slow as a cold fetch** (239 ms warm vs
268 ms cold, 2 tokens) — `jq` startup, ~40 ms a time, dominated everything.
**Decision**: render inside the parallel fetch worker and cache the finished line.
The display path now touches no `jq`, no `curl`, and no subprocess beyond reading
two small files; `colorize_usage` finds its worst percentage in pure bash for the
same reason. Side benefit: the raw response is never written to disk, so account
details in the payload do not outlive the fetch.
**Date**: 2026-08-17

### Decision 2: A failed usage fetch must not fail the launch

**Context**: The repo's #1 rule is fail-fast; this would appear to violate it.
**Decision**: Usage display is **decoration**, not an operation that must succeed
to make the launch correct — the same class as the colourised expiry. It renders
`usage: unavailable` (visible, never silent) and the launch proceeds. The rule
still binds everywhere it matters: a *malformed* stored token, a missing `curl`,
or a cache write failure are real errors and fail loudly. What must never appear
is a `2>/dev/null` that hides *why* the fetch failed.
**Date**: 2026-08-17

## Success Criteria

- [ ] Q1, Q2 and Q3 answered from a HOST `triage.bash` run, not from inference
- [ ] Either: the plan is cancelled with the refusal recorded — or — usage figures
  are reportable per stored token
- [ ] No token value ever appears in a process argv or in a committed file
- [ ] An unreachable endpoint yields a visible `usage: unavailable` and a launch
  that still works
- [ ] `CCY_VERSION` bumped for any change under `files/var/local/claude-yolo/`
- [ ] `./scripts/qa-all.bash` passes
- [ ] `qa-reviewer` agent run with no BLOCK or FIX-BEFORE-MERGE findings

## Risks & Mitigations

| Risk                                                   | Impact | Probability | Mitigation                                                                                      |
| ------------------------------------------------------ | ------ | ----------- | ----------------------------------------------------------------------------------------------- |
| Setup-tokens are scoped and cannot read the endpoint   | H      | M           | Q1 is the gate; a refusal cancels the plan rather than triggering a workaround                  |
| Endpoint shape changes in a future Claude Code release | M      | H           | Parse defensively, degrade to `usage: unavailable`, keep it off the launch path until Phase 4   |
| Launch latency from N sequential HTTPS calls           | M      | M           | Cache with a TTL; fetch in parallel; short timeout matching Claude Code's 5 s                   |
| Token leaks into argv or into the plan log             | H      | L           | `curl --config -` on stdin; triage redacts the bearer and verifies the redaction before writing |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00073-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Facts F1–F5 established from the shipped binary and one live probe
- `933b731` — plan + `triage.bash` (HOST run pending; Q1 unanswered)
