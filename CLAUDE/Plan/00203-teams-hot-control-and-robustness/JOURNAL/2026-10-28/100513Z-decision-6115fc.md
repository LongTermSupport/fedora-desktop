## 10:05 · decision · —   — Decisions 5-24 decided (alternatives for 5 and 6); two LXC members accepted by seat 2

The owner answered the unblocking list of 2026-10-28 with "all recommended", typed directly in
the coordinator's session. For this plan:

- Part C item 6 (the coordinator's decisions, not overruled): Decisions 5-24 are decided. Every
  default stands except two, where the alternative is taken: Decision 5 (N2), a remove of a
  coordinator while its homeserver is down is refused without `--coordinator=demote`; Decision
  6 (N4), one definition of "live" in ccy-teams and agent-bus. New unticked build tasks: 3.21
  (Decision 5) and 3.22 (Decision 6). PLAN.md's "Open: for the owner" headings,
  DECISIONS-lifecycle.md and SPEC-team-add-and-restart.md section 5 now say decided.
- Item A2: two LXC members in one team (one coordinator, one worker) are accepted on the host by
  seat 2 with two throwaway members: new unticked Task 3.23. It sits here because this plan
  built lxc-add's Role step and holds its acceptance row (ACCEPTANCE.md M4 (e2)); Plan 00204's
  D6 is that step's design decision, still open there.
