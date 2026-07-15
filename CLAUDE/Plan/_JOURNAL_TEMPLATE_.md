# Plan {{PLAN_NUMBER}} — Journal {{DATE}}

> **Append-only activity log** for plan {{PLAN_NUMBER}}. One file per day
> (`{{PLAN_NUMBER}}-Journal-YY-MM-DD.md`). `PLAN.md` tracks the plan (what/why,
> current state, tasks); this journal tracks what actually *happened* —
> findings, decisions, dead-ends, hand-off state — the linear lifecycle a plan
> document structurally cannot carry.
>
> **Entry grammar** — append new entries at the BOTTOM; NEVER edit earlier
> entries (corrections are new entries):
>
> ```
> ## HH:MM · category · REF   — optional short title
> ```
>
> - `HH:MM` local 24h (the date is in the filename); times increase down the file.
> - `category` ∈ `action` | `finding` | `decision` | `thought` | `blocker` | `handoff`
> - `REF` = optional task/phase ref (`T1.2`, `P1`) or `—`.
> - Bodies may embed fenced logs/diffs/snippets — no size limit — with a
>   one-line takeaway above the fence.
> - End a working session with a `handoff` entry so the next agent's entry
>   point is the last entry of the newest day-file.

## {{TIME}} · action · — — plan scaffolded

Plan {{PLAN_NUMBER}} created via `mkplan.bash`; `JOURNAL/` initialised. Next:
fill in `PLAN.md`, then log progress here as it happens.
