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

### H5 verified as a mechanism, and the account state confirmed

| ID  | Fact                                                                                                                                                                                                                                              | Source          |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------- |
| F26 | **Fable spending is gated on usage credits.** The bundle defines a `fable_overage_consent_prompt` dialog whose payload is `{overagesEnabled, modelName, balanceCents, currency}` and whose result is `consent`/`switch_default`/`cancelled`       | bundle          |
| F27 | The client carries dedicated `isFableCreditsRequired` / `setFableCreditsRequired` state, and parses a `credits_required` value at **`error.details.error_code`**, alongside `can_user_purchase_credits` and `has_chargeable_saved_payment_method` | bundle          |
| F28 | **The owner's accounts are out of credits.** Confirmed directly, and independently visible as `overage-disabled-reason: out_of_credits` in F23                                                                                                    | owner statement |

F26 and F27 make H5 a **mechanism, not a guess**: Fable is paid for out of usage
credits, and the API has a specific error code for refusing it on that basis.
With F28, the observed 429 on a Fable-entitled account is explained.

**F27 also names a gap in the probe.** `error_code` lives inside
`error.details`, while the top-level `type` stays a generic `rate_limit_error` —
so run 1 recorded the one field that cannot distinguish a credits refusal from a
weekly Fable limit. The probe now captures `error_code` and `disabled_reason`
from the body, plus the message, `retry-after`, `request-id` and
`x-should-retry`, and accepts `PROBE_ACCOUNT=all`.

### Host run 2 — the full pool, four accounts against Haiku and Fable

| ID  | Fact                                                                                                                                                                                                                                           | Source     |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| F29 | **`7d_oi` was absent on all 8 probes**, including three clean Haiku 200s. **H3 is refuted**: a Haiku probe does not carry the Fable bucket                                                                                                     | HOST run 2 |
| F30 | A real limit rejection *does* carry the headers. account-2's **Haiku** probe returned 429 with the full set — `7d-utilization 1.0`, `7d-status rejected`, `7d-surpassed-threshold 1.0`, `retry-after 121933`, and a specific message           | HOST run 2 |
| F31 | All four **Fable** probes failed identically and *degenerately*: 429, body message literally `"Error"`, **no `error_code`, no `disabled_reason`, no `retry-after`, and no `anthropic-ratelimit-*` header at all**, with `x-should-retry: true` | HOST run 2 |
| F32 | `overage-disabled-reason` is **not uniform**: `out_of_credits` on accounts 1–2, `org_level_disabled` on accounts 3–4. `overage-status: rejected` on all four                                                                                   | HOST run 2 |
| F33 | Fable 5.1 carries the `rejects_disabled_thinking` capability, which the bundle pairs with a **2048-token thinking budget**. `max_tokens` must exceed a thinking budget                                                                         | bundle     |

**F30 against F31 is the tell.** The same sweep produced a genuine unified
rate-limit rejection that carried everything, and four Fable rejections that
carried nothing. A refusal from the unified rate-limit system looks like F30. The
Fable responses do not, so they did not come from it.

**F33 explains why, and it is a defect in the probe, not a fact about Fable.**
The probe sent `max_tokens: 1` with no thinking block. Fable rejects disabled
thinking and needs a 2048-token budget, and `max_tokens` must exceed that budget
— so **the Fable arm of runs 1 and 2 was never a valid request**. An invalid
request cannot answer a question about buckets, and F24/F31 must not be read as
evidence that Fable is refused for credits.

**H5 is therefore unsupported by the runs**, though F26/F27 still stand as a
mechanism that exists. F32 further weakens it: two accounts are
`org_level_disabled` rather than out of credits, yet failed identically — which
is what a request-shape fault predicts and a credits fault does not.

Corrected: Fable and Mythos probes now send
`max_tokens: 2100` with `thinking: {type: enabled, budget_tokens: 2048}`, and
the report records the request body verbatim so a future reader can check what
was actually asked.

**Still unobserved: a `7d_oi` header from any response**, and now also **any
successful Fable response**. H4 is untested.

### F35 — what `seven_day_overage_included` actually is, from the bundle's own words

The statusline contract documents the three windows it exposes:

> Per-window usage for the session (5-hour), weekly (7-day), and
> overage-included weekly (**per-model bucket; present only for accounts whose
> responses carry that window**) **subscription** rate-limit windows, as read
> from the `anthropic-ratelimit-unified-*` response headers. […] Windows absent
> from the account state are absent here.

Three things settle from that, and one of them corrects this document:

1. **It is a SUBSCRIPTION window, not API billing.** The "overage-included" in
   the name describes which weekly allowance it is, not a credits balance.
2. **It is per-model**, which is why a Haiku probe carrying no `7d_oi` (F29) does
   not prove the account has no Fable window.
3. **It is legitimately absent for some accounts** — the contract says so
   outright, twice. An absent window is a normal state, not a fault.

It also restates the scale independently: utilisation is a fraction, "usually
0–1", with values above 1 occurring when usage runs past a cap. That is a third
confirmation of F20, and it validates the over-limit band the renderer already
allows.

### ~~H5 — struck~~

H5 held that Fable is metered against the credits bucket, so an out-of-credits
account is refused Fable and has no `7d_oi` to report. **It conflated two
different buckets.** `seven_day_overage_included` ("Fable limit") and `overage`
("usage credit limit") are separate rows in the bundle's own table, and F35 puts
the first on the subscription side entirely.

The owner made the same correction independently: the `overage-*` headers are
the switch-to-API-billing fallback for when subscription usage runs out, which
these accounts never use. F31's degenerate Fable failures were a malformed
request (F33), and nothing about credits.

F26 and F27 remain true — a Fable overage-consent dialog and a
`credits_required` error code both exist — but they describe paying for Fable
*beyond* an included allowance, which is a different subject from the weekly
window this plan is trying to display.

### F36–F39 — from Anthropic's published documentation

Web research, filed at
[subagent-reports/260910-fable-limit-research-opus-5.md](subagent-reports/260910-fable-limit-research-opus-5.md).

| ID  | Fact                                                                                                                                                                                                                                    | Source             |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| F36 | **On Max, Fable is included up to 50% of the weekly usage limit — a CEILING INSIDE the single weekly allowance, not a second allowance.** Other models draw from the same pool. On Pro, Fable is credits-only from the first token      | Fable help article |
| F37 | The `anthropic-ratelimit-unified-*` family is **absent from Anthropic's public API documentation entirely**, and `7d_oi` appears in no public source. It is real (F18, F35) but unsupported and may change without notice               | rate limits docs   |
| F38 | The **documented** ways to see this: `/usage` in Claude Code, Settings → Usage on claude.ai (account-wide), the VS Code usage dialog, and the `/model` picker, which flags "Requires usage credits" on the Fable row as a binary signal | Claude Code docs   |
| F39 | Anthropic publishes **no absolute number** for any subscription session or weekly limit. Only the Fable 50% share is published. There are also open bugs where Fable is refused for credits on Max despite remaining quota              | docs + issues      |

**F36a — the 50% is measured on FABLE's own usage, not on the total.** Verified
verbatim from the help article, because the short form invites the opposite
reading:

> "you can use up to 50% of your weekly usage limits **on Fable models** at no
> extra cost"
>
> "When you reach your Fable limit, you can keep using Fable models with usage
> credits, or switch to another model to stay within your plan's usage limits."

So spending the allowance on Sonnet or Opus does **not** consume Fable headroom,
and hitting 50% of the weekly limit on other models does not disable Fable.

There are **two independent constraints**, and either can bind first:

| Constraint              | Limit                          |
| ----------------------- | ------------------------------ |
| Fable's own usage       | ≤ 50% of the weekly allowance  |
| Total across all models | ≤ 100% of the weekly allowance |

Because it is one pool, heavy non-Fable use still restricts Fable *indirectly*:
at 90% total spent, only 10% of the pool remains for anything, Fable included.
On Max with no credits, reaching either bound stops Fable until the weekly reset.

**F36 reframes the whole plan, and mostly answers the original question.**
ccy cannot show a separate Fable allowance on a Max account because **there is
no separate allowance** — Fable spends the same weekly pool ccy already draws as
its "weekly limit" bar, and 50% of that pool is the most Fable may take. So the
figure the user asked for is already partly on screen; what is missing is *how
much of it was Fable*, which is a sub-cap gauge, not a second limit.

That is the most plausible reading of what `7d_oi` is for: the gauge on that
sub-cap. It stays consistent with F35's "per-model bucket", with the name
"overage-included" (the portion included before overage), and with F36's
ceiling. **Not proven** — no response has carried the header yet.

F37 is the standing caveat on everything this plan builds: the header family is
undocumented, so `usage_render_block` degrading cleanly when a bucket is absent
is a requirement, not a nicety. F35 already says absent is normal.

### F34 — `overage-disabled-reason` values (about the CREDITS bucket, not Fable)

**Kept for completeness, and explicitly out of scope**: these describe the
`overage` bucket, the API-billing fallback. They are recorded because F32
observed two different values across the pool and a future reader will otherwise
re-derive them — not because they bear on the Fable window.

| Header value                                        | Client's wording                              | Meaning                                       |
| --------------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `out_of_credits`                                    | usage credits exhausted                       | Provisioned and spent. Topping up restores it |
| `org_level_disabled`, `org_service_level_disabled`  | usage credits turned off by your organization | An org admin has disabled credit spending     |
| `org_level_disabled_until`, `org_spend_cap_reached` | usage credit limit reached                    | A spend cap, not an empty balance             |
| `member_level_disabled`                             | usage credits turned off for your account     | Disabled for this member specifically         |
| `seat_tier_level_disabled`, `*_zero_credit_limit`   | usage credits not available for your plan     | The seat tier carries no credit allowance     |

A second function in the bundle separates them by whether extra usage is
*obtainable*: it answers true for `out_of_credits` alone, and false for
`org_level_disabled` and the rest. So on accounts 1–2 buying credits would
restore the capability; on accounts 3–4 it would not, because the block is a
setting rather than a balance.

The client renders these strings prefixed "Fast mode disabled · " because that is
the surface the function serves, but the value read is the shared
`anthropic-ratelimit-unified-overage-disabled-reason` header.
