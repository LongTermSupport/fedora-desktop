# Decisions and requirements carried from other plans

Moved out of `PLAN.md` so the plan stays within its size limit; the text is unchanged.

## Decisions

Every decision is in [DESIGN-v2.md section 7](DESIGN-v2.md#7-the-owner-decisions), each
**defaulted; the owner may overturn it**, and the build follows the default until then. The
coordinator hears of each one flagged there (IRREVERSIBLE, CONTRADICTS or VISIBLE) before the
milestone that acts on it.

| ID           | Decision                                                                                                                                                | State                                                                                                                                                                                                              |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| D4           | no relay mode in v2; specified on v2 when the plugin API ships                                                                                          | accepted by the coordinator                                                                                                                                                                                        |
| D15          | a keeper (`teamchat run`) and a link (DESIGN.md called it the worker) in every member, the resident cost measured (B 2.14)                              | accepted by the coordinator                                                                                                                                                                                        |
| TID          | a team has a stable id (at most 22 characters) fixed at creation; its name is display only; `server_name` and handles carry the id and survive a rename | accepted with gate A (Task 0.5): built on; to the owner in the coordinator's digest                                                                                                                                |
| INT3         | a v1 name over 22 characters, or one of A 2.2's reserved ids, migrates under a short id declared with the move, and stays its display name              | defaulted; to the coordinator (not among the Task 0.5 answers)                                                                                                                                                     |
| VISIBLE      | OD-A10, BD9, BD15, BD16, DC4, DC16, DC17, DC18, DC19, DC21, OD-D6, INT4, INT5, INT7, and DC8                                                            | accepted (Task 0.5); DC17 with its go-deps allowlist rows and govulncheck clean; DC8 comes back to the coordinator if H-C5 or H-C15 fails; DC16's and DC19's stated cost is INT6's, below                          |
| IRREVERSIBLE | OD-A7, DC1, DC5, DC15, OD-D1 (`teamchat`), OD-D5, and TID above                                                                                         | confirmed by the owner 2026-10-30 (gate A met), on direction 6's condition: Task 0.6 checks none of them prevents a remote homeserver or members on other machines                                                 |
| INT6         | owner-uid processes holding a pool seat's password have power over that account (DC16, DC19)                                                            | the owner chose the default 2026-10-30 (gate B met): owner-uid processes may hold a pool seat's password; [DESIGN-v2.md 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password) is not built |
| n2f-D12      | the admin token goes to whatever holds the team's loopback port: only `team apply` checks the holder ([SPEC-n2f.md](SPEC-n2f.md) section 11)            | owner only, before the first install (gate A): accepted as residual in the inert code; the alternative is the admin client over a unix socket only the server and `teamchat-ctl` reach, which removes the race     |
| others       | OD-A1-A6, OD-A8, OD-A9, OD-A11, BD1-BD8, BD10-BD14, BD17, BD18, DC2, DC3, DC6, DC7, DC9-DC14, DC20, OD-D2-D4, OD-D7, INT1, INT2                         | defaulted, not flagged                                                                                                                                                                                             |

DESIGN.md's D1-D20 belonged to the in-place design and are superseded with it.

## Requirements carried from other plans

| From                                      | What                                                                                                                                                                       | Carried by                                                                                                               | Milestone |
| ----------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ | --------- |
| Plan 00201 Task 2.16                      | rename a team end to end                                                                                                                                                   | the stable team id (TID): a rename edits `name` and runs the play; nothing restarts, nothing is re-issued (C44, C 3.5.2) | N4        |
| Plan 00211                                | reports, questions, threads and asking a human, the side rooms and their drift check (its [SPEC.md](../00211-agent-bus-reports-and-questions-rooms/SPEC.md), as behaviour) | A 4.3, 6.3, 10; B6                                                                                                       | N1-N3     |
| Plan 00194 Task 2.0e                      | a scheduled restart posts no goodbye                                                                                                                                       | closed: RS2 on and RS3 built (Task 4.2, CCY 3.116.0); in images from the next container bump                             | N3        |
| Plan 00194 Task 2.0g                      | an addressed reply that is only the quote fallback is ignored as `empty` and logged by no one                                                                              | A 7.5 (`empty`, the unheard count) and B18-B24: the N3 spec decides whether `empty` counts as unheard                    | N3        |
| Plan 00194 Tasks 2.1, 2.1b, 2.2           | the supervisor-plugin delivery path                                                                                                                                        | specified on v2 when #119 ships (D4)                                                                                     | later     |
| Plan 00194 SPEC-inject E1-E26, Plan 00203 | delivery into a session; hot control                                                                                                                                       | B18-B24, B31-B35                                                                                                         | N3        |
| Plan 00213                                | the status line's own TTL cache, the 25 s refresh, stderr shape, empty stays empty                                                                                         | B27, B 2.9                                                                                                               | N3        |

Tasks M0 folded in here from other plans, and what the clean break does to each:

- **00213 Task 3.1** (the v1 hooks in Go): not built. v1's hooks stay Python until v1 retires;
  v2's are `teamchat hook` (B 2.8). `feat/bus-v2-m2-hooks` is superseded.
- **00213 Task 3.2** (what stays in Python): answered by [RESEARCH-python-scope.md](RESEARCH-python-scope.md).
- **00201 Task 2.8** (the relay spool's `REPLY` line): superseded, relay mode is not carried (D4).
