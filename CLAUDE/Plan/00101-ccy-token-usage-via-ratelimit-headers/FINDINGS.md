# Plan 00101 — findings register

Every established fact for this plan, in one place. [PLAN.md](PLAN.md) cites IDs
from here rather than restating them. Bundle excerpts and schemas live in
[RESEARCH-bucket-headers.md](RESEARCH-bucket-headers.md).

## Carried forward from Plan 00100

| ID  | Fact                                                                                                                                    |
| --- | --------------------------------------------------------------------------------------------------------------------------------------- |
| F9  | `/api/oauth/usage` **and** `/api/oauth/profile` both return 403 `permission_error` — *"does not meet scope requirement `user:profile`"* |
| F10 | The unified figures exist as response headers: `anthropic-ratelimit-unified-{5h,7d}-{utilization,reset,surpassed-threshold}`, `-status` |

## Mechanism — why curl and not the CLI

| ID  | Fact                                                                                                                                            | Source                       |
| --- | ----------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- |
| F11 | Claude Code's OAuth requests carry `anthropic-beta: oauth-2025-04-20` — the only oauth-dated beta string in the binary                          | string table in `claude.exe` |
| F12 | The header names appear only in a **mock/scenario harness** that synthesises them for testing. There is no code path that prints them           | JS in `claude.exe`           |
| F14 | **F12 confirmed empirically**: `claude -p --tools "" --debug --debug-file` exited 0 and produced a 17 KB debug log containing no unified header | HOST prototype               |

There is no debug or print path that surfaces these headers, so the CLI cannot
be the vehicle. Only a direct HTTP call exposes them.

## Q1 — ANSWERED YES: a setup-token can read them

| ID  | Fact                                                                                                                                                                                                            | Source         |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------- |
| F13 | A bare `POST /v1/messages` (`max_tokens: 1`) with a stored `oat01` setup-token returns **200** and carries the full `anthropic-ratelimit-unified-*` header set                                                  | HOST prototype |
| F15 | The set is wider than the four assumed: per-bucket `-5h-status` / `-7d-status`, a `-representative-claim` naming the binding bucket, plus `-fallback-percentage`, `-overage-status`, `-overage-disabled-reason` | HOST prototype |
| F16 | Utilisation values are **floats, not integers** (`0.0`, `0.02` observed) — the renderer's `%.0f` assumption held only because the probed account was near-idle                                                  | HOST prototype |

F15 is the useful surprise: `-representative-claim` says which bucket is
actually binding, rather than leaving the menu to infer it from whichever
percentage looks worst.

## Q2 — ANSWERED: utilisation is a fraction (`0`–`1`)

| ID  | Fact                                                                                                                                              | Source           |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------- |
| F17 | **Eight samples across four accounts, every raw value between `0.04` and `0.41`** — all `<= 1`. Read as fractions: 14%/9%, 6%/4%, 8%/41%, 15%/13% | HOST triage.bash |

Guessing wrong misreports usage by 100×, and it did: the plan shipped on
`percent` and every account displayed `<1%` for a week.

The discriminator is one-way — any value **greater than 1** proves the `0`–`100`
scale, because a fraction cannot exceed 1, and none was observed. So F17 was
strong evidence rather than proof, and Task 5.3 shipped it as a *self-refuting*
inference rather than an assumption. **F20 has since supplied the proof.**

## Q3 — OPEN: is a Fable bucket on the wire?

Read out of the installed bundle (`@anthropic-ai/claude-code` v2.1.267). A
reading of what the client is prepared to parse, which is not what the server
sends — which is what the host runs below are for.

| ID  | Fact                                                                                                                                                                |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F18 | Buckets map to header suffixes; the Fable one is `seven_day_overage_included` → **`7d_oi`**, read as `anthropic-ratelimit-unified-7d_oi-utilization`                |
| F19 | The bundle's label for that bucket is literally **"Fable limit"**                                                                                                   |
| F20 | Utilisation is a **fraction** — the bundle computes `Math.floor(utilization * 100)`. **This is the proof F17 could not supply**                                     |
| F21 | `seven_day_opus` and `seven_day_sonnet` have **no** utilisation header; Fable does. The buckets are not uniformly available                                         |
| F22 | Model-specific weekly rows come from `GET /api/oauth/usage` — the route F9 closed. The richer payload does not reopen it, because the payload was never the problem |

### Host run 1 — account-1 of 4, Haiku and Fable

| ID  | Fact                                                                                                                                                                                                     | Source     |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| F23 | Haiku probe, HTTP 200: `5h-utilization 0.02`, `7d-utilization 0.69`, claim `five_hour`. **`7d_oi` absent, `overage` absent.** Also `overage-status: rejected`, `overage-disabled-reason: out_of_credits` | HOST run 1 |
| F24 | Fable probe on the same token: **HTTP 429 `rate_limit_error`, carrying NO `anthropic-ratelimit-*` headers at all** — the rejection precedes the header machinery                                         | HOST run 1 |

F23 corroborates F20 independently: `0.69` read as a fraction is 69% of the
weekly allowance, a plausible mid-week figure. Read as a percentage it would be
0.69%, on an account clearly in heavy use.

| ID  | Fact                                                                                                     | Source          |
| --- | -------------------------------------------------------------------------------------------------------- | --------------- |
| F25 | **account-1 does have Fable entitlement.** Confirmed by the owner, so F24's 429 is not an access refusal | owner statement |

F25 kills the first of the two readings F24 admitted. What is left:

- ~~account-1 has no Fable entitlement, so its 429 says nothing about buckets~~
  — **refuted by F25**;
- account-1 is **entitled but currently refused**, so `7d_oi` may well exist and
  simply was not reported to a *Haiku* request. That is **evidence for H4**, not
  yet proof.

**The likeliest mechanism, and it is still a hypothesis (H5).** The bucket is
named `seven_day_overage_included`, and the same Haiku response that omitted it
carried `overage-status: rejected` with `overage-disabled-reason: out_of_credits` (F23). If Fable is metered against the overage-included weekly
bucket, an account out of credits would be refused Fable *and* have no
`7d_oi` bucket to report — which is exactly the pair of observations in hand.

If H5 holds, the Fable bar is not simply missing from ccy: for an account in
this state there is no figure to show, and the honest display is the reason
rather than a blank. That is a different piece of work from parsing a header.

The error **message** is the discriminator between "out of credits" and "weekly
Fable limit reached". The probe now captures it, along with `retry-after`,
`request-id` and `x-should-retry`, and accepts `PROBE_ACCOUNT=all`.
