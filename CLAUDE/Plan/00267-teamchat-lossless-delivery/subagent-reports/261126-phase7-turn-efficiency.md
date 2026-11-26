# Plan 00267 Phase 7: turn efficiency, first batch

Branch `v2-smoke`. Built test first in `src-go/`.

## The Phase 7 rewrite (the owner's 09:10Z words)

Commit **dabfcab9c**, on its own. It covers the revised 7.2 (an unfetched receipt rides in the
next delivery's status section and never gets a delivery of its own), 7.6 (no 8-entry cap, a
limit by volume only), 7.7 (one delivery carries every kind, each in its own section) and 7.8
(the turn-saving survey, `TURN-SURVEY.md`). Tasks 7.3 and 7.4 are ticked in it.

## Done

| Task                                                                       | Commit               | Red, then green                                                                                                                                                                                                                                           |
| -------------------------------------------------------------------------- | -------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 7.4 the skill line, in both copies, section 2 after the reply paragraph    | d4ac8a82d            | `TestTheSkillAsksForOneReplyPerSender` red (no line), green. `TestTheImageKitFilesAreTheKitAssembledForTheImage` keeps the two copies byte-identical.                                                                                                     |
| 7.2 a receipt is never an item (the half the owner's revision keeps)       | 5ad4357b8            | `TestAReceiptIsNeverAnItem` is a guard that passed when first run: the reports room's `faults` path has no route to an item. proto's "receipt in main" row drops one sent in main as `schema`.                                                            |
| 7.3 notices fold into one entry: one sender, one subject, `earlier:` lines | ab143e152            | `TestEntryFoldedNotices`, `TestNoticesAboutOneMemberFoldIntoOneEntry` and `TestAFoldIsBounded` were red because the build failed, then green. Golden: `deliver/testdata/notice-folded.txt`. A 13.2 has the `earlier:` row in both tables and a paragraph. |
| 7.1 and 7.5 the coalesce window and the human settle                       | see the final commit | `TestDueAt` red with `dueAt` stubbed to 0. The fake-clock tests (burst, agent ceiling, human settle, human ceiling, halt) timed out red because no timer was armed. All green after.                                                                      |

The 7.1 and 7.5 design is in `member/engine/coalesce.go`, and B 2.7.3 states it:

- **Agent items** wait 2 s after the newest one, and at most 10 s after the oldest. 2 s is the
  old `paceWait`, the gap that used to split a burst.
- **HUMAN items** skip the agent window and get their own settle: 3 s after the newest HUMAN
  item, and at most 10 s after the oldest. Anything else pending goes with them. Every HUMAN
  item restarts the settle, whoever sent it. The delivery goes once every human is quiet. For
  one human this is the same as the PLAN's "same human" rule. For several humans it is the more
  conservative choice, and the 10 s cap still bounds it.
- **A halt ping** skips both windows.
- **Times** are when the link first saw each item pending (`deliveryState.seen`, boot clock).
  A held team's trailing facts wait with its items.
- **The engine test world** sets the windows to zero (`buildWorld`), so the roughly 40 existing
  pacing tests keep their own timelines. `coalesce_test.go` uses the production windows. This
  runs on a fake clock, so it is not a real-time bound that a test shortens.

## Left

- **7.2 status section:** waits on batches 2-4, which send the receipts.
- **7.6, 7.7 and 7.8:** not started.
- **ccy and container version bump:** not done. The skill text and the teamchat Go code are both
  image inputs, and so is batch 1's proto change. The launcher
  (`files/var/local/claude-yolo/claude-yolo`) had another agent's uncommitted edits in the tree,
  so I did not touch it. The lead or that agent must bump `CCY_VERSION`, the Dockerfile `LABEL`
  and `REQUIRED_CONTAINER_VERSION`, and add a `docs/ccy-changelog.md` entry.
- **`MaxFold`** is still `proto.MaxDeliveryEntries` (8). Task 7.6 should give it its own
  constant when the entry cap goes.
- **Full Go gate:** it cannot run on this arm64 machine (#426). I ran `go test -race` on
  `./internal/teamchat/proto/` and `./internal/teamchat/member/...`, and `golangci-lint` on the
  touched packages.
