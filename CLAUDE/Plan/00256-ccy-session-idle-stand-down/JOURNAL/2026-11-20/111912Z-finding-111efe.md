## 11:19 · finding · — — Watch-list run baseline-20261120-1050: stand-down inputs

The watch-list baseline run `baseline-20261120-1050` (Plan 00258) found inputs for this plan.
Reports are in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`; nothing was built or run.

- **"No background work" read from the screen:** aoe, herdr and cli-agent-orchestrator know "work
  still running behind an idle prompt"; our screen classifier does not, so an idle prompt is not
  proof of idle (`multiplexers.md` aoe idea 2, "Group findings, ranked" 1).
- **Cold returns cost more:** a study found that returning to a large history after the 1-hour
  cache TTL costs several times a warm or fresh start (small samples). Compact while warm before
  standing down, or start fresh with a handoff, instead of resuming cold.
- **Verify progress by a new transcript record**, not by a running process (clarp).
- Question raised: compact before stand-down, or hand off?

Sources: `multiplexers.md` aoe idea 2; `usage-observability.md` takeaway 3 and the clarp section;
`SYNTHESIS.md` section 2 items 05 and 09, section 4 row 00256.
