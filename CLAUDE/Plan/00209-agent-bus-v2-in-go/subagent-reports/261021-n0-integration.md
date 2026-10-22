# Plan 00209 N0 integration: report

Branch `feat/bus-v2-n0`. Not pushed, not merged. qa-changed passed before each commit; qa-all was
not run (the coordinator's gate).

## Commits

| Commit   | What                                                                                    |
| -------- | --------------------------------------------------------------------------------------- |
| f183e567 | the four area designs edited at their seams                                             |
| 1f1452d3 | DESIGN-v2.md, the integrated design                                                     |
| db63e0b5 | DESIGN.md marked superseded                                                             |
| d67182c2 | the first integrator's handoff                                                          |
| 0b498397 | INT3: a v1 name over 22 characters migrates under a short id                            |
| 524f4c3c | src-go/CLAUDE.md "How a port is built": the oracle optional, none for a separate system |
| acf26259 | PLAN.md re-planned to N0-N6; DESIGN.md and BUILD-BRIEF.md contract wording removed      |
| 5dee3ead | journal: D4 and D15 answers, TID and INT3, the integration                              |

The tip is the commit that adds this report.

## GAPs fixed

The "Fixed" rows of [DESIGN-v2.md section 3](../DESIGN-v2.md#3-the-seam-contract), 25 of 56. Each
was a GAP or a contradiction between chapters, closed by an edit in the owning chapter:

- A and B: 2 (the events B sends, which are trust-bearing), 3 (reply threading, question
  numbering), 4 (handle grammar), 10 (envelopes use B's verbs), 11 (presence set by the worker).
- A and C: 15 (the main room as an anchor in `seat.json`), 19 (the team-id length), 20 (who posts
  which notice).
- B and C: 24 (the bundle), 25 (`desired.json` with `inbound`), 27 (the release runs
  `teamchat seat disconnected`), 28 (B's guest verb names), 29 (no farewell marker), 31 (the
  conversation record), 32 (a new seat resumes from the member's read receipt).
- D and the others: 37 (every name and path), 38 (`teamchat.carried`), 40 (`question_floor`), 42
  (carried items on a first start), 43 (the pinned welcome notice), 44 (`teamchat ping` and the
  outbox for acceptance A7), 45 (a session on both systems refused), 46 (migration verbs,
  `origin`), 48 (the port check reads v1's declaration in `migrate/v1`), 50 (the status record's
  `origin`, `set`, `migration`).

The "Later" rows (13, 18, 23, 49, 52-56) are now tasks in PLAN.md's milestones N1-N5; 23 (the
send-level raise) waits for the first feature (YAGNI).

## Contradictions resolved

The 17 of [DESIGN-v2.md section 5](../DESIGN-v2.md#5-contradictions-found-and-how-each-was-settled):
no stable team id; the id's length; messages to a seat with no session; carried items with no
kind; presence; who posts `disconnected`; the farewell; reply commands; guest operation names; the
bundle's main room; desired membership; the seat store's location; timers, sudoers and records;
the port check; `teamchat` as a human name; the welcome and the acceptance check; R47 and R36 with
no home. Item 02 is amended by INT3 (below).

## INT3: why M0 refused a long v1 name, and the fix

**Why it refused.** The team id is separate from its name (TID), but the first integration kept
one shortcut from before TID: a migrated team's id **was** its v1 name, so that every
`<seat>@<team>` string in the session registry kept working unchanged (D 5.1). v1 allows names of
24 characters (P4); v2's id allows 22, so that the homeserver's dynamic user `teamchat-<team>` fits
systemd's 31-character limit (C 3.2.3). A v1 name of 23 or 24 characters could not be its own id,
and M0 refused it. The limit came only from deriving the id from the name.

**The fix (0b498397).** A long v1 name migrates:

- the move's declaration in `teamchat_migrations` gains `id`, written only for a name over 22
  characters: ccy-teams proposes the name cut to 22 characters with trailing hyphens removed, the
  owner may change it, and ccy-teams refuses an id that fails the grammar or names another v1 team
  or a v2 team (D 5.10); M0 checks the same again (D 5.4);
- the v1 name stays the team's display name (D 5.1, C 3.8);
- the set record names each team as `{"v1_name", "id"}` (D 5.3);
- the one resolving function of ccy-teams and the launcher maps a v1 name with no status record of
  its own to its pair's id once the set is `committed` or later, so old `<seat>@<v1 name>` strings
  keep working (D 3.9). A 23- or 24-character name can never be a v2 id, so no string resolves two
  ways; the function's unit test covers both kinds of team.

Also edited: D 3.5 and 12, and DESIGN-v2.md sections 2, 5 (item 02), 7 (INT3 row, now VISIBLE
rather than CONTRADICTS) and 9 (the N0 review checks the fix). Unreviewed.

## Decisions for the coordinator

From DESIGN-v2.md section 7, each defaulted:

| ID    | Default                                                                                                                           | Flag                                                        |
| ----- | --------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| TID   | a stable id (at most 22 characters), the name display only; `server_name` and handles carry the id                                | IRREVERSIBLE; agreed, for the owner's next digest           |
| OD-A7 | v1's handle grammar, `host` renamed `site`                                                                                        | IRREVERSIBLE                                                |
| BD15  | a gap over 20 pages of 100: move on, one `gap` NOTICE naming `history`, counted in `status`                                       | VISIBLE: a said, not silent, skip                           |
| DC1   | one homeserver per team                                                                                                           | IRREVERSIBLE once teams run                                 |
| DC4   | status records each minute and after each change, no sudoers file; board facts up to about 70 s old, a bounded poll at sign-in    | VISIBLE                                                     |
| DC5   | a seat store outside every checkout, one session directory mounted per session (a ccy change)                                     | IRREVERSIBLE once teams run                                 |
| DC15  | only the host-held pool bundle travels between sessions; session state quarantined                                                | IRREVERSIBLE once teams run                                 |
| DC16  | the pool token reused across sessions; a hostile released session keeps a valid token until `member reissue` or `park` (risk K13) | VISIBLE: security                                           |
| DC17  | `godbus/dbus/v5` and `go.yaml.in/yaml/v3` in the production module, with go-deps allowlist rows                                   | VISIBLE: new dependencies                                   |
| OD-D1 | the product name `teamchat`                                                                                                       | IRREVERSIBLE once deployed                                  |
| OD-D5 | no reverse move after commit; fix forward                                                                                         | IRREVERSIBLE                                                |
| OD-D6 | the v1 archive stopped at M8, started on demand by `ccy-teams archive open`                                                       | VISIBLE                                                     |
| INT3  | a long v1 name migrates under a short id (above)                                                                                  | VISIBLE: that team's `<seat>@<id>` differs from its v1 name |

Also for the coordinator, from this re-plan:

- **00213 Task 3.1, folded into 00209 at M0** (the v1 hooks in Go), now builds nothing: v1's hooks
  stay Python until v1 retires, and v2's hooks are `teamchat hook`. PLAN.md says so and calls
  `feat/bus-v2-m2-hooks` superseded. 00213's own file still points at 00209 for it; the coordinator
  owns that edit.
- `feat/bus-v2-m1-proto*` and `m1-state*` decode v1's formats. They are useful to stage 1 at most;
  Task 7.2 rebuilds or drops each.
- The build notes' challenges to area B's defaults are in the N0 review's scope (PLAN.md Task
  0.4): BD9 (`disconnected` posted on the next start, so a session that never restarts may never
  be reported gone), BD2 (exit 69 when the worker is down, and the error a human on an LXC member
  sees), BD11 (prove the Go goodbye fits the default 1.5 s SessionEnd bound).

## BUILD-BRIEF.md edits

Only wording the clean break made false:

1. Scope: "every stage of 00209's design ... the reader" became v2 as a separate system beside v1
   (DESIGN-v2.md), with the host parts v2 has (no reader: DC4); the link to PLAN.md's removed
   "Owner direction" now points to "The owner's directions"; "that means" became "v2 replaces".
2. The plans list: DESIGN.md as the design of record became DESIGN-v2.md, which supersedes it;
   "decisions D1-D20" became DESIGN-v2.md section 7's.
3. Plan 00213's oracle pattern: "reuse it for every piece, over the compatibility contract" became
   reuse only where Go replaces a v1 piece in place (stage 1); v2 has no oracle against the Python.
4. Plan 00211: carry its behaviour, not pingbus 0.2.0's verb names or formats.
5. "Spec first": the byte-compatibility of every on-disk and wire format between Python and Go
   became "v2 shares no format, lock or wire with v1; it installs beside v1, touches nothing of it,
   teams move one at a time"; old and new versions of each system still run side by side; LXC
   first kept.
6. Definition of done: "every pingbus verb runs as Go" became v2 does in Go what each does; the
   zipapp retired once every team has moved and the owner agrees (D 7.2); "the parity oracles
   pass" became the no-touch proof, each migration's acceptance, and v2's own mixed-version window.

Not edited, though the coordinator may want to: the session-side list still names "the relay"
among the pieces to rewrite (D4 defers it), and the budget paragraph's dates.

## Other edits

- **DESIGN.md**: the intro's "only the compatibility contract binds" sentence and section 11's
  "a parity oracle checks the compatibility contract only" removed; the supersession note kept and
  the body otherwise unchanged.
- **src-go/CLAUDE.md** (Task 4): kept as general guidance, with three corrections: the old tool is
  "treated as" a prototype (not every old tool is one); the live-tree run before cut-over applies to
  in-place replacements; an oracle is optional, checks only what both share while they run side by
  side, and a separate system that shares nothing has none.
- **Journal**, through `mkplan.bash --journal`: owner direction 2 and its clarification; a
  correction of the 16:51 "only the contract binds" entry; the re-plan to N0-N6; D4 and D15
  accepted; TID and INT3; the integration. The direction and re-plan entries went in with PLAN.md's
  commit, as the relocation the size-drop check asks for.

## Still open

- The N0 review (PLAN.md Task 0.4) over DESIGN-v2.md section 9, INT3's fix included. Nothing of
  this integration has been reviewed.
- The coordinator's answers on the flagged decisions (Task 0.5).
- The 00213 file edit for its folded Task 3.1 (above).
- GO-2026-6809's toolchain bump (Task 1.6); whether F44's Go 1.26.10 (67253b0d) already covers it
  was not checked here.
- Plan 00194 Task 2.0e and 2.0g are carried into the N3 spec as open questions, not designed:
  DESIGN-v2 does not yet say how a scheduled restart avoids its `left` notice, nor whether `empty`
  counts as unheard.
