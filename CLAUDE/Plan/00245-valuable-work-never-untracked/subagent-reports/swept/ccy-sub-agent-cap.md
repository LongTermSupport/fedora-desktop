# Sub-agent cumulative-spawn cap (`CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION`) false-limits long autonomous sessions

**Context:** an intentionally long-lived autonomous Claude Code session (an unattended "build driver"
running for hours across multiple auto-compactions) hit the sub-agent spawn ceiling mid-work and could
no longer delegate to sub-agents for the remainder of the session. Filing as upstream feedback via ccy.

## What the limit is

- Env var: **`CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION`**, Claude Code **harness** default **200**.
- Semantics: a **cumulative lifetime counter** — the *total* number of sub-agents spawned across the
  whole session, NOT the number running concurrently. Once 200 have been spawned over the session's
  life, every further `Agent`/sub-agent call fails with:
  > `Subagent spawn limit reached (200 of 200 agents spawned). … ask the user to raise CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION.`
- It is **not** set by the project, the hooks-daemon, or php-qa-ci — the env var was unset, so the
  hard-coded harness default applied.

## Why it's a problem

The cap conflates two different concerns:

| Guard                                                     | Protects a real resource?                                                               | Verdict                                |
| --------------------------------------------------------- | --------------------------------------------------------------------------------------- | -------------------------------------- |
| **Concurrency cap** (already exists, ~`min(16, cores−2)`) | Yes — CPU / memory / API rate-limit                                                     | Keep it. This is the meaningful guard. |
| **Cumulative lifetime cap** (this one, 200)               | **No** — nothing. It only ceilings *how much a session may delegate over its lifetime*. | Blunt. Penalises long-lived sessions.  |

A cumulative ceiling makes sense as a *runaway-loop backstop*, but implemented as a raw lifetime count
it fires as a **false limit** for exactly the workload Claude Code is increasingly used for: a
long-running autonomous agent that delegates investigation / QA / triage / review to many short-lived
sub-agents over hours. Steady, legitimate delegation is indistinguishable from a runaway loop under a
pure count.

### Concrete evidence (this session)

A single autonomous session accumulated 200 sub-agent spawns over many hours of *legitimate* work —
e.g. a feature build's QA-runner agents, a 4-agent parallel issue-triage sweep, several read-only
`Explore` agents for code archaeology, and code-map agents for two separate issues. None were runaway;
they were normal fan-out. The 200th spawn hit mid-task, blocking an ad-hoc audit sub-agent the user
had explicitly requested — the work had to be done inline in the driver's own (expensive) context
instead, which is precisely the opposite of what sub-agent delegation is for.

## Recommended upstream fix (Claude Code core)

Replace (or supplement) the raw cumulative cap with a guard that targets runaway loops without
penalising longevity. In rough order of preference:

1. **Rate limit** — spawns per unit time (e.g. N/minute). Catches a tight spawn-loop immediately;
   never trips on steady hours-long delegation.
2. **Token / cost budget guard** — cap on *spend*, not *count*. Aligns the guard with the actual
   resource being protected.
3. **At minimum:** make the cumulative cap (a) **much more generous** by default, (b) **configurable**
   (already is, via the env var — but the default is the problem), (c) **warn before the wall** (emit
   an advisory at, say, 80% so the session can adapt), and ideally (d) resettable / decaying rather
   than monotonic-until-session-exit.

Keep the **concurrency** cap as-is — that is the guard that protects real resources.

## Interim mitigation already applied (project side)

In the affected repo we raised the ceiling for future ccy sessions by exporting an override in
`.claude/ccy/ccy.env`:

```bash
export CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION="${CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION:-10000}"
```

This is a stopgap (applies to NEW sessions only; concurrency remains the real guard). It does not fix
the underlying design.

## Ask of ccy

- Either **raise the default** `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` in ccy's deployed environment
  (ccy launches long autonomous sessions by design, so the harness default of 200 is a poor fit), and/or
- **Relay upstream to Claude Code** for a proper rate/budget-based runaway guard in place of a raw
  cumulative lifetime cap.
