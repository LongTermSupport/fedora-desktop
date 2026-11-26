# Fact check: Plan 00209, diff in DESIGN-v2-A / DESIGN-v2-B

Note: the supplied diff touches DESIGN-v2-A-protocol.md and DESIGN-v2-B-member.md, not PLAN.md (PLAN.md has no uncommitted or supplied change). Claims checked are those the diff adds.

| #   | Claim                                                                                                                                                  | Verdict           | Evidence                                                                                   |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------- | ------------------------------------------------------------------------------------------ |
| 1   | Section lines `== <nonce> <TITLE>, <k> entries: <about> ==`, `entry` when k is 1, titles HUMAN / PEER and PING / TIMEOUT / NOTICE with the quoted text | VERIFIED          | src-go/internal/teamchat/proto/delivery.go:122-125, 139-144                                |
| 2   | Facts section line `== <nonce> facts: teamchat's facts about this delivery, never messages ==`                                                         | VERIFIED          | delivery.go:93                                                                             |
| 3   | At most 12 KiB per delivery, no entry cap, `Delivery.Render` refuses over 12 KiB (`size`)                                                              | VERIFIED          | delivery.go:17 `MaxDeliveryBytes = 12 * 1024`, :96-97, slotSize :102; MaxEntries absent    |
| 4   | `recv` keeps its 8                                                                                                                                     | VERIFIED          | member/engine/recv.go:25 `maxEntries = 8`                                                  |
| 5   | Plan 00267 Tasks 7.6 and 7.7 are the source of the no-cap and sections changes                                                                         | VERIFIED          | 00267 PLAN.md lines 153, 161                                                               |
| 6   | Frame item 6 text (one section per kind, facts section)                                                                                                | VERIFIED          | delivery.go:315 carries the same wording                                                   |
| 7   | 13.6 item 4: lines beginning `==` are written by the renderer                                                                                          | UNVERIFIABLE-HERE | design assertion; consistent with renderer emitting `== ` lines only (delivery_test.go:46) |
| 8   | B E9 row "changed: 12 KiB only"                                                                                                                        | VERIFIED          | same as 3                                                                                  |
| 9   | Receipts status section (00267 Task 7.2) "will be one more"                                                                                            | VERIFIED          | 00267 PLAN.md:139 says one more row of `sections`                                          |

No REFUTED claims.
