# Fact check: REGISTER.md R79 and the 00209 design diff (26 Nov)

Scope note: the 00265 diff holds only R79. It holds no R73 rewrite and no seat-choice row
citing chooser/teamchat.go:92-114, so those were not checked. The private-term list was only
counted and grouped by its source column; no term was printed.

## REFUTED (most consequential first)

1. **R79 Cause (checked): "learns its terms from the agent bus status and the machine's own names
   ... holds 3 terms. The other team's name is not among them: a session on one team never learns
   the names of the teams beside it."** The 3 terms are all source `host name`
   (`awk -F'\t' '{print $1}' .git/private-terms | sort | uniq -c` gives `3 host name`). The list
   holds NO `agent-bus team` or `agent-bus member` term, not even this session's own team. So
   the gap seen in this container is that no bus terms were learned at all, not that a
   neighbouring team is missing. `src-go/qa/privatenames/main.go:95-98` says a session on no
   team (pingbus status exit 78) gets only host names, which fits. The "never learns teams
   beside it" story is unproven; the Cause must be reworded, and the fix's red fixture should
   cover a list with no bus terms too.

## Table

| #   | Claim                                                                                    | Verdict                            | Evidence                                                                                                                                                                             |
| --- | ---------------------------------------------------------------------------------------- | ---------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | `src-go/qa/privatenames` exists and learns terms from bus status and the machine's names | VERIFIED                           | `terms.go:1-6`, `Derive` reads TEAM/MEMBER/SEAT lines and Hostnames                                                                                                                  |
| 2   | Terms kept in `<git dir>/private-terms`                                                  | VERIFIED                           | `main.go:37` `ListName`; `.git/private-terms` exists                                                                                                                                 |
| 3   | "In this container that list holds 3 terms"                                              | VERIFIED                           | `grep -c ''` gives 3                                                                                                                                                                 |
| 4   | The other team's name is not among them                                                  | UNVERIFIABLE-HERE                  | would need the name, which must not be printed; moot, see REFUTED 1                                                                                                                  |
| 5   | A session on one team never learns the names of teams beside it on the same bus host     | REFUTED (not shown) / UNVERIFIABLE | learning is from `pingbus status` output (`main.go:196-236`); whether that lists other teams needs a run of pingbus on a multi-team host; this container's list shows zero bus terms |
| 6   | Plan 00245 redacted the name                                                             | VERIFIED (weakly)                  | `00245-valuable-work-never-untracked/PLAN.md:100` Task 3.5 "journals redacted"                                                                                                       |
| 7   | The name returned through batch 3 and batch 23, the batch 23 qa-reviewer saw it          | UNVERIFIABLE-HERE                  | would need a search for the name, not allowed                                                                                                                                        |
| 8   | No gate blocked it                                                                       | UNVERIFIABLE-HERE                  | follows from 5/7                                                                                                                                                                     |
| 9   | Fix: the owner can pin a known private name in the git-dir list                          | UNVERIFIED as new                  | a git-dir list exists; `private-terms-export-skip` (`denylist.go:93`) is for the opposite use                                                                                        |

## 00209 diff (design docs, Plan 00267 Tasks 7.2 and 7.3)

| #   | Claim                                                                                                                             | Verdict           | Evidence                                                                                                                              |
| --- | --------------------------------------------------------------------------------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| 10  | A 13.2 is the entry-lines grammar                                                                                                 | VERIFIED          | `DESIGN-v2-A-protocol.md:1908`                                                                                                        |
| 11  | B 2.7.2 is where folding sits; 2.7.3 states                                                                                       | VERIFIED          | `DESIGN-v2-B-member.md:1511`, `:1548`; new text falls between them                                                                    |
| 12  | 7.4's table has events `left`, `added`, `role`, `moved` (moved takes no subject), and `reason:` values exit/logout/released/other | VERIFIED          | A 7.4 table lines about 984-991                                                                                                       |
| 13  | 7.7 gives `sent_ms` for the `carried:` time                                                                                       | VERIFIED          | grep in 7.7 finds `sent_ms` (2 hits)                                                                                                  |
| 14  | A 7.8 `teamchat.receipt` lives in the reports room; a main-room one is dropped `schema`                                           | VERIFIED          | A 7.8 lines 12, 37, 43                                                                                                                |
| 15  | "at most 8 entries ... (E9)" delivery cap                                                                                         | VERIFIED          | `at most 8` appears in B (3 hits)                                                                                                     |
| 16  | Plan 00267 Task 7.3 means folding from one sender about one subject, up to 8                                                      | PARTLY            | Task 7.3 reads "one kind about one member in one window"; the design is stricter, so the task text does not match the design it cites |
| 17  | Task 7.2: receipts never an entry                                                                                                 | VERIFIED          | 00267 PLAN.md:126-129                                                                                                                 |
| 18  | Task 7.2 bounce text "rides in next delivery as one trailing line per bounced message ... (Plan 00267 batches 2-4)"               | PARTLY            | Task 7.2 says the same; "batches 2-4" not checked                                                                                     |
| 19  | B44, E6, E9, P77, H4 cross-references                                                                                             | UNVERIFIABLE-HERE | not searched                                                                                                                          |

Result: 19 rows, 11 verified, 1 refuted, 7 unverifiable or partial.
