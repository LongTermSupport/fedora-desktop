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

F5 is why this plan reads an internal endpoint at all: nothing supported reports
the figure before a session starts.

### Open questions

| ID  | Question                                                                                                                          | How it gets answered      |
| --- | --------------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| Q1  | Does a stored long-lived `sk-ant-oat01-…` setup-token authenticate to `/api/oauth/usage`, or is it scoped to `/v1/messages` only? | `triage.bash` on the HOST |
| Q2  | What is the exact JSON envelope — a bare array, or an object wrapping one?                                                        | `triage.bash` on the HOST |
| Q3  | Does the endpoint answer per-**token**, or per-**account** (two tokens for one account reporting one figure)?                     | `triage.bash` on the HOST |

Q1 is the gate. Everything downstream is dead if it comes back 401/403, and the
correct outcome in that case is to record the finding and cancel the plan — not
to reach for a workaround.

**This plan asserts no cause and ships no code until `triage.bash` has run on the
HOST.** The container cannot answer Q1: it holds no token and no
`~/.claude/.credentials.json`.

## Tasks

### Phase 1: Establish the facts (HOST)

- [x] ✅ **Task 1.1**: Read `select_token()` / `list_tokens()` and confirm what the
  menu shows today (name + colourised expiry only)
- [x] ✅ **Task 1.2**: Confirm from the shipped binary how Claude Code fetches usage,
  and that no supported CLI route exists (F1–F5)
- [ ] ⬜ **Task 1.3**: Write `triage.bash` — probe every stored token against
  `/api/oauth/usage`, report HTTP status and response shape, redact the bearer
- [ ] ⬜ **Task 1.4**: Run `triage.bash` on the HOST and record Q1/Q2/Q3 in the journal

### Phase 2: Decision gate

- [ ] ⬜ **Task 2.1**: If Q1 is a refusal — record the finding, mark the plan
  Cancelled, and stop. Do not engineer around a scoped credential
- [ ] ⬜ **Task 2.2**: If Q1 succeeds — write the confirmed response schema into this
  plan before any parsing code is written

### Phase 3: On-demand usage reporting

- [ ] ⬜ **Task 3.1**: Add a usage fetch helper to
  `files/var/local/claude-yolo/lib/token-management.bash`, passing the token via
  `curl --config -` on stdin so it never reaches argv (the BSH-09 rule already
  applied in `validate_token()`)
- [ ] ⬜ **Task 3.2**: Surface it in `list_tokens()` / a `--token-usage` flag —
  off the interactive launch path, so a broken endpoint breaks a diagnostic
  command rather than every launch
- [ ] ⬜ **Task 3.3**: Bump `CCY_VERSION` and the `token-management.bash` header
  version (required for any change under `files/var/local/claude-yolo/`)
- [ ] ⬜ **Task 3.4**: Run `./scripts/qa-all.bash`

### Phase 4: Selection-menu annotation

- [ ] ⬜ **Task 4.1**: Cache each token's usage under `$CCY_ROOT` with a short TTL,
  so repeat launches cost no network call
- [ ] ⬜ **Task 4.2**: Annotate the `select_token()` rows with 5-hour and weekly
  utilisation, fetching in parallel and falling back to `usage: unavailable`
- [ ] ⬜ **Task 4.3**: Confirm the menu still renders promptly with the endpoint
  unreachable (simulate by pointing the fetch at an unroutable host)
- [ ] ⬜ **Task 4.4**: Run `./scripts/qa-all.bash`, then the `qa-reviewer` agent

## Technical Decisions

### Decision 1: On-demand command before menu integration

**Context**: The obvious implementation annotates `select_token()` directly.
**Options considered**:

- (A) Annotate the launch menu straight away — best UX, but puts N HTTPS calls to
  an undocumented endpoint on the path of the most-run interactive command.
- (B) On-demand command first, menu annotation second behind a cache.

**Decision**: B. The endpoint is internal and will change without notice; when it
does, the blast radius should be a diagnostic command, not every `ccy` launch.
Phase 4 remains in scope — it is sequenced, not dropped.
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
