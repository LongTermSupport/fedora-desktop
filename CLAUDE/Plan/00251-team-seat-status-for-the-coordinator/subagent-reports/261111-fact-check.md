# Fact check, Plan 00251 diff (related-plans paragraph)

## REFUTED / imprecise

1. "none of which models seat state": 00221's DESIGN.md (T3, T15, line ~295) keeps the board's existing STATE column
   (working / wants a look / not working, src-go/internal/teams/board/table.go). A board state exists today, derived
   in the host-local board. Plan 00251 should say none models the working/idle/blocked/gone state with duration for the
   coordinator, or say how 00251 relates to that column.

## Table

| #   | Claim                                                                              | Verdict                       | Evidence                                  |
| --- | ---------------------------------------------------------------------------------- | ----------------------------- | ----------------------------------------- |
| 1   | 00203 reads each ccy pane for idle moments, for idle shutdown                      | VERIFIED                      | 00203 PLAN.md lines 261, 328, 496         |
| 2   | 00194 wakes an idle seat to deliver                                                | VERIFIED                      | 00194 PLAN.md lines 20, 34                |
| 3   | 00224 is the coordinator's desktop notifications, possible carrier for notify half | VERIFIED (carrier is opinion) | 00224 Overview                            |
| 4   | 00221 is a board of team members                                                   | VERIFIED                      | 00221 title and Overview (tree board)     |
| 5   | 00209 and 00246 are v2, its landing and cross-network                              | VERIFIED                      | 00209 Overview; 00246 Status and Overview |
| 6   | none models seat state                                                             | REFUTED (see above)           | 00221 DESIGN.md T3/T15                    |
| 7   | all six plan folders exist in the live tree                                        | VERIFIED                      | ls of CLAUDE/Plan                         |
