# Checking Fable usage limits on a Claude Max subscription

Research question: how can a Claude Max subscriber using Claude Code check their
usage against the Fable model's limit specifically?

Authoring model: Claude Opus 5. Filename corrected from the `-sonnet` suffix the
dispatch declaration supplied, on the report's own recommendation.

Method: web research only. No API calls were made, no credentials were used, and
no project scripts were run. Nothing here was observed from a live response.

> **Editor's note, added on filing.** The bullet below about
> `seven_day_overage_included` is right that it is **undocumented**, and wrong
> that it is unattested. It was read first-hand out of the shipped bundle at
> `/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`
> (v2.1.267) — both the suffix table that constructs the header name and the
> statusline contract that describes the window. See F18 and F35 in
> [FINDINGS.md](../FINDINGS.md). Nothing in this plan assumed the field.
>
> The distinction the report is reaching for still stands and is the important
> one: **no response has yet been observed carrying that header**, and an
> undocumented internal surface can change without notice. Both facts are already
> recorded against H4.

## Relevance to this plan

This plan reads token usage out of rate-limit headers. Two findings bear directly
on it:

- The `anthropic-ratelimit-unified-*` family is **not** in Anthropic's public API
  documentation. Anything built on it is built on an unsupported surface.
- **`7d_oi` and `seven_day_overage_included` do not appear in any source**,
  official or community. If a probe or prototype in this plan references such a
  field, it needs to be re-derived from a real response rather than assumed.

## a. Practical answer, ranked

1. **`/usage` in Claude Code.** Official and closest to hand. On a Pro, Max, Team
   or Enterprise plan it shows plan usage bars plus a breakdown attributing usage
   to skills, subagents, plugins and individual MCP servers, with behavior flags
   for anything accounting for 10% or more of recent usage. Press `d` or `w` to
   toggle the last 24 hours against the last 7 days. Secondary sources report
   that Max accounts see a second, smaller weekly bar for Fable that drains
   independently of the all-model bar. Anthropic's own documentation describes
   the bars but never names a Fable row.

   Caveat straight from the docs: the attribution figures are approximate and
   computed from local session history on that machine, so usage from other
   devices or from claude.ai is not included.

2. **Settings then Usage on claude.ai.** Account-wide, so it does not have the
   single-machine blind spot above. Anthropic's Fable help article says you track
   both included plan usage and credit spend there, and the usage-credits article
   says the dashboard clearly distinguishes included plan usage from usage-credit
   consumption. Running `/usage-credits` in Claude Code opens this page for a Pro
   or Max subscriber.

3. **VS Code extension Account and usage dialog.** Carries the same attribution
   shares and behavior flags with a Day and Week toggle, without the Loops rows.
   Requires Claude Code 2.1.174 or later.

4. **The `/model` picker.** Shows "Requires usage credits" on the Fable row when
   Fable usage would bill to credits rather than plan-included limits. This is a
   binary eligibility signal, not a usage gauge.

5. **Response headers.** Real but unofficial. See section b.

## b. Officially documented versus not

### Documented

- The Fable share of the weekly allowance and plan eligibility, in the Fable help
  article.
- `/usage` behavior, the plan usage breakdown, the 24h/7d toggle, and the
  usage-credits row, in the Claude Code costs page.
- The claude.ai usage settings page and how to enable and cap usage credits.
- The VS Code Account and usage dialog.
- API tier rate limits per model, where the Fable rate limit is a total limit
  applying to combined traffic across Claude Fable 5.1 and Claude Fable 5.
  Claude Mythos 5.1 and Mythos 5 share a separate combined limit on the same
  terms.

### Not documented

- **Absolute numbers for subscription limits.** Anthropic publishes no token
  count, message count or hours-per-week figure for any plan's session or weekly
  limit. Only the Fable percentage is published.

- **The `anthropic-ratelimit-unified-*` headers.** The official rate limits page
  lists only `retry-after`, and the requests, tokens, input-tokens,
  output-tokens and Priority Tier families. No `unified` header appears anywhere
  on that page.

- **`7d_oi` / `seven_day_overage_included` does not exist in any source I could
  find**, official or community. Treat any such field as unverified.

  A Claude Code GitHub issue quotes the unified headers actually observed on
  responses:

  ```text
  anthropic-ratelimit-unified-status
  anthropic-ratelimit-unified-5h-status
  anthropic-ratelimit-unified-5h-reset
  anthropic-ratelimit-unified-5h-utilization
  anthropic-ratelimit-unified-7d-status
  anthropic-ratelimit-unified-7d-reset
  anthropic-ratelimit-unified-7d-utilization
  anthropic-ratelimit-unified-representative-claim
  anthropic-ratelimit-unified-fallback-percentage
  anthropic-ratelimit-unified-reset
  anthropic-ratelimit-unified-overage-disabled-reason
  ```

  One observed value: `anthropic-ratelimit-unified-representative-claim: "five_hour"`. This is an issue thread, not documentation. There are open
  feature requests asking Anthropic to expose these headers to hooks, status
  lines and the Agent SDK, which is itself evidence that they are not a
  supported public surface.

- **A Fable-specific limit message.** The official Claude Code errors reference
  lists `You've hit your session limit`, `You've hit your weekly limit`,
  `You've hit your Opus limit` and `You've hit your Sonnet limit`. There is no
  documented Fable equivalent. The "You've hit your Fable limit" phrasing appears
  only in secondary write-ups.

### Community claims worth flagging as unverified

- That Fable weighs roughly double an Opus session against the weekly allowance.
  Widely repeated in secondary sources, not stated by Anthropic.
- That `/usage` renders the Fable cap as its own distinct bar. Plausible and
  consistent with the 50% cap existing, but not in Anthropic's docs.

## c. Fable, credits and overage

Fable is not uniformly credit-gated. It depends on the plan.

| Plan                                                | Fable access                                                |
| --------------------------------------------------- | ----------------------------------------------------------- |
| Max, premium Team seats, premium Enterprise seats   | Included, up to 50% of weekly usage limits, no extra cost   |
| Pro, standard Team seats, standard Enterprise seats | Outside plan usage limits, pay-as-you-go usage credits only |
| Usage-based Enterprise and Claude API               | Billed at standard API rates                                |

Key points from the Fable help article:

- The 50% is a **ceiling inside the one weekly allowance**, not a second
  allowance. Other models draw from the same pool, and you can never use more
  than your weekly limit in total.
- On Max, once Fable hits the 50% cap you can either keep using Fable on usage
  credits, or switch to another model and continue inside the plan's limits.
- On Pro, Fable requires credits from the first token. There was no promotional
  credit for Fable 5.1.

Usage credits themselves are a prepaid pay-as-you-go balance billed at standard
API rates, enabled and capped under Settings then Usage on claude.ai. One
practical side effect documented in the Claude Code costs page: the prompt cache
lifetime is one hour on a subscription and drops to five minutes once you are
drawing on usage credits, unless you choose the TTL yourself.

There are open Claude Code bugs where Fable is blocked with a "requires usage
credits" prompt on a Max plan despite remaining weekly Fable quota, in both the
CLI and the VS Code extension. If the meters and the picker disagree, that is a
known failure mode rather than a limit genuinely reached.

## Sources

- https://support.claude.com/en/articles/15424964-claude-fable-models-on-your-plan
- https://code.claude.com/docs/en/costs
- https://code.claude.com/docs/en/model-config
- https://support.claude.com/en/articles/14552983-models-usage-and-limits-in-claude-code
- https://support.claude.com/en/articles/12429409-extra-usage-for-paid-claude-plans
- https://platform.claude.com/docs/en/api/rate-limits
- https://code.claude.com/docs/en/errors
- https://code.claude.com/docs/en/vs-code
- https://github.com/anthropics/claude-code/issues/12829
- https://github.com/anthropics/claude-code/issues/55333
- https://github.com/anthropics/claude-code/issues/50518
- https://github.com/anthropics/claude-code/issues/79441
- https://github.com/anthropics/claude-code/issues/80484
- https://www.anthropic.com/claude-fable-and-mythos-5-1
- https://platform.claude.com/docs/en/models/fable-5-1/overview
- https://www.aifreeapi.com/en/posts/claude-fable-pro-max-weekly-limits
- https://claudefa.st/blog/guide/development/fable-5-usage-credits
