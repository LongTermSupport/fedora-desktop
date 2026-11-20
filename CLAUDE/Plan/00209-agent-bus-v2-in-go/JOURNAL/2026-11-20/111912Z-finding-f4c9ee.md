## 11:19 · finding · — — Watch-list run baseline-20261120-1050: bus work objects for the v2 verb table

The watch-list baseline run `baseline-20261120-1050` (Plan 00258) proposed a new plan for "bus
work objects". The dedupe check found that the v2 verb table lives here
(`DESIGN-v2-A-protocol.md` section 8.1), not in Plan 00246, so it is recorded here instead of a new
plan. Reports are in `CLAUDE/WatchlistRuns/baseline-20261120-1050/`; nothing was built or run.

- **The gap:** the verb table's `done`, `blocked` and `ack` carry no owner, state or successor, so
  a dropped baton is invisible.
- **Converging inputs:** a completion message with an outcome and the request it closes, and a
  closure contract (no `done` without saying where the work went; openrig); durable needs or gates
  that block named work and wake the seat when a human answers (intentic, Orca); leader-only wake
  with de-duplication (Multica, clean room: report only); enforced halt and shared-resource leases
  (orgtree; coral's `pkg/resourcequeue/`, Apache-2.0 with an EULA caution); A2A's task state names;
  stageflow's verified hand-off.
- **Delivery honesty:** keep "written", "acknowledged", "done" and "verified" apart wherever they
  are shown; tell a sender which of parked, removed or not-a-member it hit, without leaking the
  roster.
- Cheapest first step, per the report: vocabulary (A2A state names, a required closure field on
  `done`); a full queue is a design question for this plan.
- The run advises against speaking A2A on the bus; borrow its task model only.

Sources: `teams.md` "Group section: the strongest takeaways, ranked" 2, 4 and 5; `fleets.md`
takeaway 4; `SYNTHESIS.md` section 2 item 06, section 4 "New plans" 3.
