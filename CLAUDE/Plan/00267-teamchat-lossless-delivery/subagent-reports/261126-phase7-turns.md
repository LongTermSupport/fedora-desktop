# Plan 00267 Phase 7: Tasks 7.6 to 7.8

Branch `v2-smoke`, test first. Shas are in the git log under "Plan 00267 Task 7.6" and "7.8".

## 7.6: a delivery is limited by volume only

- `proto.MaxDeliveryEntries` is gone. `Render` refuses only a text over 12 KiB, and `Fit`
  takes the most entries that fit, however many that is. The rest go on the existing
  `<k> more pending: they follow in the next delivery` line.
- Red, then green: `TestADeliveryIsLimitedByVolumeOnly` and `TestDeliveryFit` (proto). Green
  only, because proto was already fixed when they first ran: `TestNoCountOfEntriesLimitsADelivery`
  (deliver, 62 items, no 8 cap) and `TestADeliveryIsLimitedByItsBytesOnly` (engine, fake clock:
  9 together, then 7 large ones split 5 and 2).
- `deliver.MaxFold` keeps 8 as a constant of its own, because it bounds one fold.
- A delivery's re-fetch used `recv`'s `maxEntries`. It is now named `maxRefetch`. It bounds
  the homeserver calls in one turn, not what a delivery holds.
- `recv`'s own 8-item cap (LN4) is left as it is. That is a verb, not a delivery:
  TURN-SURVEY.md item 6.
- Docs: A 13.1, B 2.7.2 and B's E9 row.

## 7.7: one delivery, one section per kind

- Each kind group of A 13.7's order begins with a line like
  `== <nonce> HUMAN, 2 entries: requests from team humans ==`. The groups are HUMAN, PEER and
  PING, TIMEOUT and NOTICE. The trailing facts follow
  `== <nonce> facts: teamchat's facts about this delivery, never messages ==`, which appears
  only when there are facts.
- Red, then green: `TestADeliveryIsInSections`. Green only:
  `TestEveryKindPendingGoesInOneDeliveryInSections` (engine: HUMAN, PEER, halt ping and notice
  make one dial).
- The corpus templates gained the section grammar. The attack test now counts exactly one
  section line, and every golden was rewritten with `-update`.
- Frame text item 6 was rewritten. Docs: A 13.1 (shape, the sections, the worked example),
  13.5 and 13.6 point 4.
- The skill gained two lines in both copies, which stay byte-identical. It is now 7798 bytes,
  under the 8 KiB limit.
- **Old-kit interop:** a member renders its own deliveries, and its frame and skill ship in the
  same kit. So a peer on an old kit sees nothing new, and a new section line is
  self-describing.
- **Not done:** putting every team in one delivery (survey item 1). It changes the one-store
  awaiting and proof model, so it needs its own design.

## 7.2's status section

There is no receipt data to carry yet, so there is no slot. The section is one more row of
`proto`'s `sections` once batches 2 to 4 send receipts.

## 7.8

Written: [TURN-SURVEY.md](../TURN-SURVEY.md), 12 opportunities with the saving and cost of
each.

## Versions

CCY 3.124.90 and container 2.84.95: the Dockerfile LABEL, `REQUIRED_CONTAINER_VERSION` and the
changelog. I wrote 3.124.88. Another agent renumbered my uncommitted line to .90 when it
committed .89.

**Defect (mine):** f1f3aa3af staged the launcher whole from the shared tree. That took in
another agent's uncommitted R90 help-text edits: the `--teams` usage lines and two refusals.
Their matching `help.txt` and difftest goldens were not in it. It was never pushed. On the
lead's word I rebuilt it as `195b48352`, whose launcher diff is only my two version lines. The
R90 edits went in t42-build2's own `ce6b1e9b1`.

My first rebuild of the 7.8 commit took the old commit's whole tree, which would have reverted
`ce6b1e9b1`. I caught it before any push and rebuilt it from HEAD with only my three paths, as
`5af0a688d`. The lesson: in a shared tree, build a commit's index from HEAD with your own paths
added, never from an older commit's tree.

## Checks

- `go vet` and `go test ./internal/teamchat/...` are green.
- `./scripts/qa-changed.bash`: see the commit. ansible-syntax and the arm64 go-qa are known not
  to run here.

## Tooling faults met

- A foreground `go test` whose test waits on `dw.next()` for a timer that is never armed hangs
  until the 10 min test timeout, with no output. That was my own test's fault, and
  `-timeout 60s` exposed it at once.
- No tool defect was found.
