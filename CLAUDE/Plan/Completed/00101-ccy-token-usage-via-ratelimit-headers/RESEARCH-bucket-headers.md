# Plan 00101 — research: which rate-limit buckets exist, and where each one is readable

Supporting document for [PLAN.md](PLAN.md) Q3. Everything here is a **reading of
the installed Claude Code bundle**, not an observation of a live response. That
distinction is the whole point of Q3: the bundle says what the client is
prepared to parse, which is not the same as what the server sends.

Source: `@anthropic-ai/claude-code` v2.1.267, at
`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe` inside
the CCY container. Read with byte-offset dumps, because the file is 217 MB and a
wide regex over it does not terminate in reasonable time.

## The bucket table (F18)

The bundle pairs each bucket with a header suffix and reads three headers per
bucket:

```js
oL = [["five_hour","5h"], ["seven_day","7d"],
      ["seven_day_overage_included","7d_oi"], ["overage","overage"]]

// for each [bucket, suffix]:
//   anthropic-ratelimit-unified-${suffix}-utilization
//   anthropic-ratelimit-unified-${suffix}-reset
//   anthropic-ratelimit-unified-${suffix}-surpassed-threshold
// a bucket with none of the three present is omitted from the result
```

The reader is called on the real response headers, not only in the mock harness:
its result becomes the `readings` field alongside the `base` object built from
`-status`, `-reset`, `-representative-claim` and the overage headers.

## The label map (F19)

```js
SU = {
  five_hour: "session limit",
  seven_day: "weekly limit",
  seven_day_opus: "Opus limit",
  seven_day_sonnet: "Sonnet limit",
  seven_day_overage_included: "Fable limit",
  overage: "usage credit limit",
}
```

`seven_day_overage_included` is the Fable allowance. The name is about billing
mechanics rather than the model: a companion GrowthBook flag,
`tengu_usage_overage_included_models`, holds the allowlist of models metered
against this bucket, so its membership is server-controlled and can change
without a client release.

## The scale, proven (F20)

```js
let n = e.utilization ? Math.floor(e.utilization * 100) : undefined;
// ... `You've used ${n}% of your ${label}`
```

Utilisation is a fraction in `0`–`1`. Task 5.3 shipped `CCY_USAGE_SCALE=fraction`
on eight measured samples and recorded explicitly that eight samples all `<= 1`
is **not** proof. This is the proof. The self-refuting guard added in that task
(`_usage_scale_conflict`) stays worth keeping — it now guards against the server
changing the contract rather than against the inference being wrong.

## The asymmetry that matters (F21)

`seven_day_opus` and `seven_day_sonnet` appear in the **label** map and in the
set of values `-representative-claim` can take, but **not** in the bucket table
above. So they have no utilisation header. They are observable two ways only:

- as `-representative-claim`, which names the single bucket the API currently
  considers binding, and
- from `GET /api/oauth/usage`.

This kills any plan phrased as "add the per-model buckets" as one job. Fable has
a utilisation header; Opus and Sonnet do not.

## The usage endpoint, and why it stays closed (F22)

```js
utilization: {
  five_hour, seven_day, seven_day_oauth_apps, seven_day_opus,
  seven_day_sonnet, cinder_cove,          // each: { utilization, resets_at }
  extra_usage: { is_enabled, monthly_limit, used_credits, utilization, ... },
  limits: [ { kind, group, percent, resets_at,
              scope: { model: { display_name }, surface: { display_name } } } ],
}
```

`limits[]` is the generic, server-driven array behind the model-specific rows in
`/usage` and the VS Code usage panel; a Fable row would arrive there with a
`display_name` and no client change. The bundle's own changelog confirms the
coupling: it lists a fix for `/usage` "dropping a model-specific weekly limit row
when the usage endpoint is rate limited".

This is the same `GET /api/oauth/usage` that **Plan 00100 closed**: every stored
`sk-ant-oat01` setup-token is refused with 403 `permission_error`, *"does not
meet scope requirement `user:profile`"*, and the scope is fixed when
`claude setup-token` mints the token. Do not re-open it on the strength of the
richer payload — the payload was never the problem.

Note also `cinder_cove`, rendered as "Claude Code and Cowork credit", a one-time
credit rather than a recurring window. Out of scope here; recorded so a future
reader does not mistake it for a limit bucket.

## What the CLI will not do for us

`-representative-claim` is the only bucket name the client ever receives by
header, and the mock harness clears it when nothing is exceeded. The field was
nonetheless observed as `five_hour` on a 200 response with near-zero usage
(F15), so on real responses it names the binding bucket rather than only a
breached one. ccy already captures and displays it (lib 1.12.0).

There is no debug or print path in the CLI that surfaces these headers — F12 and
F14 established that empirically. `curl` remains the only vehicle.
