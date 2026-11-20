## 11:19 · finding · — — Watch-list run baseline-20261120-1050: install and wake inputs

The watch-list baseline run `baseline-20261120-1050` (Plan 00258) found inputs for this plan.
Reports are in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`; nothing was built or run.

- **Versioned install directories with an atomic `current` link** (Emdash): an install or upgrade
  on a member machine lands beside the old one and switches in one rename, so a half-landed
  install never runs.
- **Event-driven wake on an issue reply** (claude-code-tools idea 2) instead of polling.
- **Tombstone-and-reconcile** for a member machine that cannot be reached (Emdash), shared with
  Plan 00227.
- The run's "bus work objects" proposal (completion with outcome, closure, durable needs, leases)
  was checked against this plan: it holds no v2 verb table, so it went to Plan 00209, the plan of
  record for v2's design, as a journal entry.

Sources: `fleets.md` emdash ideas 2 and 4; `tooling.md` claude-code-tools idea 2; `SYNTHESIS.md`
section 4 row 00227/00246.
