## 19:40 · action · 3.1 — Task 3.1 built

Task 3.1 built. Rate and budget hold an agent item to the next free slot of their window
(proto.Window.Reserve) as Pending.not_before; the delivery loop waits for it; recv hands it at
once. A held item past pending's agent share bounces rate via the Phase 2 drop ledger and owed
receipts. Decision: recv does not wait on not_before, since the session asked for the items and
recv's own caps bound it. Decision: no HeldBack is counted now, so the held back: line is never
written; Phase 5 words it with Phase 4's held receipts. Red: TestAHeldItemIsDeliveredAtItsSlot
(1 dial before the slot) and TestAHeldItemPastTheAgentShareBouncesRate (pending full) on HEAD's
engine; the proto and teamsync tests were edited to the new verdicts with the code, so their red
was a compile failure only.
