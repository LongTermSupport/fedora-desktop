# Plan Journalling

> **Reference, not gospel.** This document describes how the hooks daemon
> supports per-plan journalling. It is deliberately copy-and-customisable:
> a client project can adopt it verbatim, tune the conventions to taste, or
> replace the narrative wholesale — the only hard contract is the small set
> of **daemon-enforced** rules called out in the POLICY section at the end.

## Why journal a plan?

`PLAN.md` captures **what** the work is and **why** — goals, tasks, decisions,
success criteria. It is a living *specification*, curated and rewritten as the
plan evolves. What it structurally cannot hold is the **linear time-series of
what actually happened**: the findings, the dead-ends, the in-flight decisions,
the hand-off state a future agent needs to pick the work back up.

A **journal** is that stream. Each plan folder gains a `JOURNAL/` sub-directory
holding per-day, append-only files. The journal grounds future agents in ways
`PLAN.md` cannot — especially **roads not taken** (why an approach was
abandoned) and **hand-off context** (where the last session left off).

| Question                              | Look in    |
| ------------------------------------- | ---------- |
| What are we building and why?         | `PLAN.md`  |
| What tasks remain? What's the status? | `PLAN.md`  |
| What did we try at 14:00 that failed? | `JOURNAL/` |
| Where did the last session hand off?  | `JOURNAL/` |
| Why was option B rejected mid-flight? | `JOURNAL/` |

Two diaries would drift; one specification (`PLAN.md`) plus one activity log
(`JOURNAL/`) stay coherent.

## Layout

```
CLAUDE/Plan/NNNNN-name/
  PLAN.md
  JOURNAL/
    NNNNN-Journal-YY-MM-DD.md      # one file per LOCAL day with activity
    NNNNN-Journal-YY-MM-DD.md
```

- `JOURNAL/` is an upper-case landmark sibling of `PLAN.md`, **inside** the plan
  folder — so archiving a plan (`git mv` into `Completed/`) carries the journal
  for free.
- Day-files are named `NNNNN-Journal-YY-MM-DD.md`: the redundant `NNNNN` plan
  number survives copy/paste and greps cleanly; `YY-MM-DD` is the local day.
- **One file per local day.** Multiple entries append to that day's file. A day
  with no activity has **no file** — never scaffold empty day-files.
- `mkplan.bash` scaffolds `JOURNAL/` plus a seeded day-1 file automatically when
  a `_JOURNAL_TEMPLATE_.md` is present in the plan directory.

## Entry grammar

Each entry is a heading with a fixed grammar followed by a free markdown body:

```
## HH:MM · CATEGORY · REF   [— optional short title]
```

- **`HH:MM`** — local 24-hour time (the date lives in the filename). Times run
  monotonically down a file.
- **`CATEGORY`** — one of a small fixed core set:
  `action` · `finding` · `decision` · `thought` · `blocker` · `handoff`.
  (Clients may extend this set — that is *convention*, not enforced.)
- **`REF`** — optional task/phase reference (`T2.1`, `P2`, or `—` for none).
- Separator is the middot `·` (U+00B7).

Bodies may embed fenced logs, diffs, or code snippets — put a one-line takeaway
*above* the fence so a skim reader gets the point without expanding it.

### Append-only discipline

A journal is **append-only**. New entries go at the **bottom**; earlier entries
are never edited. **Corrections are new entries**, not rewrites — if you got
something wrong at 09:00, add an 11:00 `finding` entry that corrects it. This
keeps the log an honest record of what was believed when, and lets the daemon's
`journal-append-only` check confirm each edit only adds.

### Hand-off convention

The resumer's entry point is the **last entry of the newest day-file**. End a
work session with a `handoff` entry naming what's done, what's next, and any
in-flight state — so the next agent (or the failsafe recovery cron) can resume
without re-deriving context.

## Good vs. noise

Journal **meaningful events**, not every tool call. The log is a narrative a
human or agent reads to reconstruct the work — not a keystroke trace.

**Good** (worth an entry):

```
## 14:05 · finding · T1.4 — append-only check can't use try/except-return
The error_hiding audit flags `return None` inside `except`. Fixed by splitting
regex-match (guard-clause None) from calendar validity (precondition via stdlib
`calendar`), so no ValueError is ever caught. QA error_hiding now clean.

## 15:20 · decision · T1.6 — grandfather at plan number, not date
Chose number threshold (≥163) over a date cutoff for `journal-folder-present`:
survives clean across branches and needs no git query. Legacy plans below the
threshold are never nagged.
```

**Noise** (do not journal):

```
## 14:06 · action · — ran pytest      ← tick-spam; only log a meaningful result
## 14:07 · action · — read a file     ← not an event
## 14:08 · thought · — hmm            ← empty; say something or omit
```

Cadence is a *convention*: journal when something is worth grounding a future
agent on. It is never enforced per-tool-call — the daemon never turns
journalling into a heartbeat.

## Lifecycle touchpoints

| When                         | Do                                                                                |
| ---------------------------- | --------------------------------------------------------------------------------- |
| Plan created (`mkplan.bash`) | `JOURNAL/` + day-1 file scaffolded; add a `## HH:MM · action` entry               |
| Work happens                 | Append `action`/`finding`/`decision`/`blocker` entries                            |
| A day rolls over             | Start a new `NNNNN-Journal-YY-MM-DD.md` (naming check accepts today or yesterday) |
| Session ends / context low   | Append a `handoff` entry naming next steps                                        |
| Plan archived                | `JOURNAL/` moves with the folder automatically                                    |

## Notes & Updates migration

Historically the blow-by-blow stream was crammed into `PLAN.md`'s
`## Notes & Updates` section, which strained the file and got compressed away.
Going forward, that stream lives in `JOURNAL/`; `PLAN.md` keeps only a thin,
curated `## Delivery & Milestones` stub (milestone lines + delivery commit
hashes for the completion checklist). Legacy plans that still carry
`## Notes & Updates` are never rewritten — the change applies to new material.

---

## POLICY vs CONVENTION

Everything above is **convention** you can tune, **except** the small set of
rules the daemon actually checks. All journal checks ship **ADVISE** (they add
context, they do not block) and are governed by `plan_workflow.qa.journal.*` in
`.claude/hooks-daemon.yaml`.

### Daemon-enforced (POLICY)

| Check                    | Stage | What it advises                                                                            | Knob                                                |
| ------------------------ | ----- | ------------------------------------------------------------------------------------------ | --------------------------------------------------- |
| `journal-dayfile-naming` | edit  | day-file name matches `NNNNN-Journal-YY-MM-DD.md`, right plan number, today/yesterday date | `mode` (the only check that may ratchet to `block`) |
| `journal-append-only`    | edit  | an edit only appends — never rewrites/removes earlier entries                              | always advise                                       |
| `journal-folder-present` | sweep | an In-Progress plan ≥ `grandfather_before` has a `JOURNAL/`                                | `grandfather_before`                                |
| `journal-freshness`      | sweep | a plan's newest day-file isn't older than `freshness_days`                                 | `freshness_days`                                    |

Configuration block (defaults shown):

```yaml
plan_workflow:
  qa:
    journal:
      enabled: true          # master switch for all journal checks
      mode: advise           # advise | block | off (only naming honours block)
      dir_name: JOURNAL       # journal sub-directory name
      freshness_days: 3       # nag a quiet JOURNAL/ sooner than plan staleness
      enforce_on_completion: false
      grandfather_before: 0   # plans below this number are never nagged for a JOURNAL/
```

Set `grandfather_before` to the plan number at which your project adopted
journalling, so pre-existing journal-less plans are never nagged (no backfill).

### Client-tunable (CONVENTION)

The category set, the middot separator, cadence, hand-off style, the
`## Delivery & Milestones` stub, and everything in the narrative sections above
are yours to adapt. Only the four checks and their knobs are enforced — and only
ever as advisories unless you deliberately ratchet `mode: block`.
