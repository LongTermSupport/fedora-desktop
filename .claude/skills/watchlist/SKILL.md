---
name: watchlist
description: Keep the watch list (the owner's GitHub stars plus repositories added by hand) cloned under verified licences, and study it in runs that record the commit studied per repository. "/watchlist sync" fetches the latest stars and triages new ones, "/watchlist baseline [<group>]" runs an in-depth study, "/watchlist diff [<group>]" studies only what changed since the last study, "/watchlist resume <run-id>" finishes a run, "/watchlist status" shows where things stand, "/watchlist add <url>" adds a repository that is not a star. Findings become GitHub issues or plans.
argument-hint: "sync | baseline [<group>] | diff [<group>] | resume <run-id> | status | add <url>"
disable-model-invocation: false
user-invocable: true
allowed-tools: Agent, Bash, Read, Edit, Write
---

# Watch list

A shim. The list, the licence rules, the runs and what they produce are in
[CLAUDE/Watchlist.md](../../../CLAUDE/Watchlist.md); read it first. `watchlist <verb>` below
means `cd src-go && go run ./cmd/watchlist --root .. <verb>`. A command failure stops the skill:
report it, never continue past it.

## Dispatch

- **`sync`**: `watchlist sync`, then triage every `new` repository (Watchlist.md, step 2). Report
  what was new, unstarred, licence-changed or moved.
- **`baseline [<group>]`**, **`diff [<group>]`**: sync and triage first. Then
  `watchlist run new <mode>-<UTC yyyymmdd-hhmm> --mode <mode> [--group <group>]`, then study.
- **`resume <run-id>`**: `watchlist run status <run-id>`, then study the pending groups only.
- **`status`**: `watchlist run status` and the state and group counts in `CLAUDE/Watchlist.tsv`.
- **`add <url>`**: `watchlist add <url>`, then triage it.

## Study

Dispatch one `watchlist-scout` per pending group, all in parallel. Give each:

- the run id and the group;
- its rows, from `watchlist run rows <run-id> <group>`;
- the newest earlier report for the group under `CLAUDE/WatchlistRuns/`, or "none".

As each returns, `watchlist run mark <run-id> <group>`. If marking refuses, send the scout back
with the reasons it printed.

When every group is marked, write `SYNTHESIS.md` in the run directory and act on it (Watchlist.md,
"Act"). Commit and push the manifest, the reports and `CLAUDE/Watchlist.tsv` as the run goes, so
an interrupted run resumes from what is committed. Relay to the owner the issues and plans filed,
and where the synthesis is.
