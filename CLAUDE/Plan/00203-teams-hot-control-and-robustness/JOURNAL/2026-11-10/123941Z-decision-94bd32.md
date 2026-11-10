## 12:39 · decision · —   — Stale seat takeover (A16)

Owner decision (2026-11-10): a stale seat named only by a stopped, restore-after-reboot record is
taken over by Add a running session to this seat, not refused. Built as SPEC A16: the add has a
third step, a confirm (default No) naming the old session; yes turns its restore off through
`seats.SetRestore`, the code the seat menu's Stop starting its session again after a reboot now
shares, then runs `session add`. A failed add turns the restore on again. A live-held seat stays
refused (A15). H22 needs no change: it counts only restored records
(`TestAddTakesASeatOnceTheRecordThatRestoresItIsTurnedOff`). Tests red then green in
`src-go/internal/teams/picker/addrunning_test.go`; picker coverage 90.1%. Docs: SPEC A15-A16,
`docs/agent-bus.md`, the picker help. Report:
[subagent-reports/261110-stale-seat-takeover.md](../../subagent-reports/261110-stale-seat-takeover.md).
