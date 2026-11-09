## 17:22 · correction · 26-11-05/23:26   — PR 165: reading (b) only for a missed entry declared once due

PR 165 (issue 156, PT1 P2 readings), rounds 1 to 3. The entry at 26-11-05/23:26 describes the
first design, which review rejected: it read any delivery in flight before the recv as reading
(b), the harness, and named its test after that. The design as it stands:

- The mark's record keeps awaiting entries and missed entries apart, each missed one with its
  `Missed.AtMS` (the boot clock), and the run records the boot clock before its world starts.
- Reading (b), the harness, is given only for a delivery that was a missed entry before the recv
  AND was declared once it could be due: `declareMissing` needs 180 s of quiet after the write
  (`engine/proof.go` `quietAfter`), so an entry declared less than 180 s after the run's start
  reads (a), production, "declared missing before due". No seed run lasts 180 s, so in a seed run
  (b) cannot print; only the forced timelines reach it.
- A delivery awaiting before the recv reads (a), naming P11's question (a recv hands out only an
  item handed to nobody). A delivery both missed and awaiting reads neither: the store
  contradicts itself.
- The verdict is unchanged (`TestPT1P2VerdictIsTheRecordsNot`).

Tests: `TestPT1P2ReadsAMissedEntryDeclaredBeforeDueAsProduction` and
`TestPT1P2ReadsADeliveryBothMissedAndAwaitingAsAmbiguous` were red against the round-2
classifier (both read the harness) and are green now; `TestPT1P2ReadsAMissedEntryFoundLateAsTheHarness`
and `TestPT1P2ReadsAnAwaitingEntryBeforeTheRecvAsProduction` keep round 1's results.
Reports: `subagent-reports/261106-pr165-readings-opus.md` (round 2) and
`subagent-reports/261107-pr165-r3-opus.md` (round 3).
