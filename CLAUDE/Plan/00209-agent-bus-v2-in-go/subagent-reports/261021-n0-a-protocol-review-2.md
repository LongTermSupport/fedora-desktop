Verdict: FIX-FIRST

# Plan 00209 N0 area A (protocol and rooms): adversarial review, round 2

Reviewed: `DESIGN-v2-A-protocol.md` (1737 lines) against the frame, the N0 common rules, the round-1
review, and the scratch digests. Line numbers are those of the reviewed file.

Counts: round 1: 17 FIXED, 3 PARTLY, 0 NOT FIXED. New findings: blocker 0, major 2, minor 12.

The round-1 fixes are real and mostly clean. The two majors below are contradictions that the fixes
themselves introduced, and each is a one-line fix.

---

## Part A: round-1 findings

| #   | Status | Checked at                                                               | Note                                                                                                       |
| --- | ------ | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------- |
| 1   | FIXED  | 6.5 l.629-633; 7.6 rule 03 l.883-886; 10.2 l.1121-1123; 10.5 l.1180-1182 | Own posts reach the index before `self`. The trust edge of the index is new finding B5                     |
| 2   | PARTLY | 6.6 l.644-672; 3.2 l.267-268; OD-A1 l.1640; HC-A1 l.1624; 20 l.1706-1708 | Seat lock named as truth, `set_presence` rule, versions from C. But `who` contradicts itself (B1)          |
| 3   | PARTLY | 13.2 l.1384-1454; 13.5 l.1517-1520; 13.6 l.1532-1541                     | Templates and slots added, headline ASCII-only and attributed. "No slot holds a space" is still false (B3) |
| 4   | FIXED  | 7.1 l.734-745; 7.2 l.757; 7.4 l.808; 10.1-10.3                           | One table; the examples agree                                                                              |
| 5   | FIXED  | 7.3 l.794-799; 10.1 l.1085; 10.2 l.1118-1120; 10.3 l.1156; 14 l.1583     |                                                                                                            |
| 6   | FIXED  | 4.2 l.330-334, l.381-392; 6.4 l.585-593; 6.9 l.707; 12.2 l.1300-1310     | The Matrix facts are now right (own-redaction via I4, msgtypes open, self-leave)                           |
| 7   | FIXED  | 4.2 l.396-398; 5.2 l.512                                                 |                                                                                                            |
| 8   | PARTLY | 6.5 l.619-627                                                            | Accepted and named, but the stated bound is wrong (B6)                                                     |
| 9   | FIXED  | 9.3 l.1052-1053; 13.2 l.1374, l.1425                                     |                                                                                                            |
| 10  | FIXED  | 2.2 l.179-180; 4.1 l.300; 7.4 l.832-833; 12.2 l.1300                     | No new broken cross-references found                                                                       |
| 11  | FIXED  | AR6 l.50-55; 7.5 l.859-861                                               |                                                                                                            |
| 12  | FIXED  | AR23 l.115; 4.3 l.413-416; 5.1 l.476; 5.2 l.513-515; 7.3 l.792           |                                                                                                            |
| 13  | FIXED  | 11.2 l.1234, l.1238-1240                                                 | Fixed for agent text only; reports, questions and threads regress (B4)                                     |
| 14  | FIXED  | 5.1 l.479, l.499-503; 9.3 l.1029-1031; 6.9 l.708                         |                                                                                                            |
| 15  | FIXED  | 7.5 l.848; HC-A3 l.1626                                                  |                                                                                                            |
| 16  | FIXED  | 10.3 l.1144, l.1150-1155                                                 |                                                                                                            |
| 17  | FIXED  | 8.3 l.962-966; 8.4 l.976; 13.2 l.1439                                    | The 30 s grace matches v1's fixed 30 s long-poll. Reminders lack the same treatment (B12)                  |
| 18  | FIXED  | 7.5 l.866-867; 7.6 l.889, l.908                                          |                                                                                                            |
| 19  | FIXED  | 12.3 l.1318-1321                                                         |                                                                                                            |
| 20  | FIXED  | 13.2 l.1456-1462                                                         |                                                                                                            |

---

## Part B: new findings

### Major

**B1. major. `who` reads presence in 6.6 and the seat lock in section 20 (6.6 l.656 against 20 l.1708; 12.2 l.1296; HC-A1 l.1624).**
6.6 says "`who` (a member's view) and Element's online dot show it [presence]. The board shows the
seat lock." Section 20 tells B: "Liveness for the board and `who` is the seat lock (R27), never
presence." 6.6 also says that other members cannot see a container's locks, so B cannot build what
section 20 asks. HC-A1's failure branch ("`who` shows the roster only") agrees with 6.6. B will build
one of two different `who`s.
Fix: in section 20, write "Liveness for the board is the seat lock (R27). `who` shows presence,
labelled as display (6.6), or the roster only if HC-A1 fails." Have `who` print the presence column
with a fixed label such as `presence (display, not liveness)`, so no agent reads it as R27's fact.

**B2. major. The reminder TIMEOUT cannot be rendered under the binding grammar (10.5 l.1181; 13.2 l.1374, l.1396, l.1423, l.1453).**
10.5 gives the reminder `verb: ask-human`. 13.2 renders a TIMEOUT's `verb:` as `verb: <verb>`, and
`<verb>` is "the closed verb set (8.1)", which has no `ask-human`. 13.2 also says "A slot that fails
its grammar is an error and the entry is not rendered". Built as written, every AR24 reminder is
refused at render time. 10.5 also does not say what `ref:` holds for a reminder.
Fix: drop `verb:` and `ref:` from the reminder. Its kind sentence, `room: questions`, `question: <q>`
and `silent: humans` already say everything. In the 13.2 kind table, split TIMEOUT into "ping" (with
`verb:` and `ref:`) and "reminder" (without them). Alternatively, add the template
`verb: ask-human`, valid only on a reminder TIMEOUT.

### Minor

**B3. minor. "No slot can hold a space" is still false (13.2 l.1384-1385; 13.6 point 1 l.1532-1533).**
The slot table includes `<json>` and `<json-ascii>`, and both hold spaces. `<json>` also holds
non-ASCII. 13.6 point 1 ("every slot ... passed a full-match ASCII grammar with no space") repeats
the claim. A builder who adds a generic "no space in any slot" assertion refuses every body.
Fix: write "every slot except `<json>` and `<json-ascii>` is space-free ASCII. Those two are JSON
strings from the 13.4 escaper, and each is the last token on its line". Restate 13.6 point 1 the
same way.

**B4. minor. The duplicate digest is empty for reports, questions and thread posts (11.2 l.1238-1240; 10.1 l.1079; 10.2 l.1105-1111; 10.3 l.1146).**
The digest is taken over "the typed object's free text". `<ns>.report` is `{"v":1}`, and the
question and thread objects carry no text either, because their text is the `body`. So two different
reports, or two quick thread follow-ups in one question, get the same key within 60 s and the second
is refused `duplicate`. That is round-1 finding 13 again, for these three kinds.
Fix: "the SHA-256 of the free text: `<ns>.text`'s `text`, else the event's `body` for report,
question and thread post".

**B5. minor. The question index trusts an undefined "agent account" and any `n` (6.5 l.629-633; 7.6 rule 03 l.883-886; 10.2 l.1121-1123).**
"Agent account" is not one of 6.7's classes. A human (power 50 in questions) can send an
`m.room.message` that carries `<ns>.thread {"n":3,"close":true}` from Element's devtools and close a
question in the index. A coordinator that bypasses its client (6.9 row 2) can post
`<ns>.question` with `n` 999999999. History is permanent, so every later `n` (1 plus the highest) is
then 1000000000, which fails `Q[1-9][0-9]{0,8}`, and `ask-human` is broken for the team for ever.
13.5 item 3 and 13.2 l.1450 also say the headline is "the coordinator's" words. After a handover it
is a former coordinator's.
Fix: define "agent account" as a sender whose localpart parses as a handle (2.3), so never a human
and never the admin. For numbering, count only questions whose `n` is exactly 1 plus the highest seen
so far, and ignore the rest for numbering. Write "the words of the coordinator that asked it" in
13.2 and 13.5.

**B6. minor. The fail-open bound is stated wrongly (6.5 l.622-623).**
"At most the sync latency's worth" holds only for a member that is syncing. A member whose worker is
down while it is promoted, or switched to `human_text: true`, judges on restart everything not yet
judged: human messages up to `human_max_age_s` (up to 604800 s) and pings up to their timeout.
Fix: name the bound as "the member's offline span, capped by `human_max_age_s` for human text and by
the verb's timeout for pings (8.5)".

**B7. minor. Rule 10 of 7.6 drops stale answers that 8.5 says still count, and never says where answers are recorded (7.6 l.902-903; 8.4 l.974; 8.5 l.985-986).**
7.6 has no step that records an answer, and rule 10 ends in "Then deliver". 8.5 says a stale answer
still counts and 8.4 says an `ack` is not delivered. Followed literally, 7.6 drops a stale `done`
before it is recorded, and delivers every `ack`. In the same area, 8.4 says an `ack` that arrives
after its TIMEOUT is shown only as an `answered:` line in "the next delivery for another reason". If
no other delivery comes, the session keeps acting on a TIMEOUT that no longer holds.
Fix: add a step between rules 09 and 10: "an answer (8.2) is recorded against its ping here,
whatever its age; an `ack` then ends `answered` and is not delivered". Then add "an `answered: ... (after its TIMEOUT)` line is reason enough for a trailing-only delivery, at most once a minute", as
13.1 already allows for held-back lines.

**B8. minor. MEMBERSHIP has no templates, although A claims to fix them (13.3 l.1478-1480; 13.2 l.1361-1382, l.1453; 13.1 l.1335; 13.6 l.1532).**
The 13.2 tables have no MEMBERSHIP column. 13.3 says the form is `from: <v2-bin>` and
`to: this session`, and both break 13.2's `from:` and `to:` templates. Its other lines are left as
"fixed template lines with only grammar-checked names". 13.6 point 1 ("every line is one of 13.2's
templates") does not cover them. The 13.1 first line has no MEMBERSHIP count. B cannot produce these
"exactly to section 13" (20 l.1705).
Fix: add a MEMBERSHIP column and its line templates, taken from 00203 H12, with `from: <v2-bin>`
and `to: this session` as named alternatives. Add `<M> membership` to the first line. Or state that
00203 H12 is binding for these lines and cite it in 13.6.

**B9. minor. The drop reasons do not match the rules (7.6 l.908; rule 08 l.898-900).**
`target` is in the closed set and is used nowhere. An agent `<ns>.notice` that is not `left`, or
whose `subject` is not its `sender` (rule 08), has no named reason.
Fix: use `target` for that notice case (or name it `schema`), or remove `target` from the set. Add a
corpus case.

**B10. minor. Whether rule 10's checks run in questions is unclear (7.6 rule 05 l.890-891).**
Rule 05 sends a questions-room human message down "the human path ... kind HUMAN" without saying
whether age, flood and budget (rule 10) still apply. If `stale` applies, a coordinator that restarts
after more than `human_max_age_s` loses human answers that the index counts as attached.
Fix: state the choice. Recommended: in questions, apply `rate` but not `stale`, because the index
already tracks open and answered questions.

**B11. minor. Envelope slots are left undefined in two places (13.2 l.1377, l.1430; 7.4 l.825; 13.1 l.1347; 13.2 l.1437).**
The `added` notice carries `role` (7.4), but the `reason:` line is listed only for `left` and
`role`, so the new member's role never reaches the session. `<who>` is
`<sender> (<KIND>, <team>)`, and a TIMEOUT has no sender.
Fix: list `reason:` for `added` too. For a TIMEOUT in `<who>`, use the silent addressee (or
`humans`).

**B12. minor. Reminders repeat the clock-skew gap that 8.3 closed for pings (10.5 l.1180-1182).**
The bound is in server time, and the firing clock and attachment timing are not stated. There is no
grace, and nothing covers a human answer, sent on time, that arrives after the reminder.
Fix: apply 8.3's 30 s grace, and add the `(after its TIMEOUT)` treatment, or state that a late
reminder is harmless and accepted.

**B13. minor. Questions and thread bodies are read by receivers, against AR6 and 7.1 (AR6 l.53-55; 7.1 l.727; 10.2 l.1115-1126; 13.2 l.1450-1451).**
A replacement coordinator reads other coordinators' question bodies for the headline, for the
`Question:` duplicate check, and for the form. 10.1 states the exemption for reports. 10.2 and
10.3 do not, so AR6 says those bodies are never read.
Fix: extend the 10.1 sentence to questions and thread posts, and make AR6 and 7.1 say "except
reports, questions and thread posts, whose body is the content (10.1-10.3)".

**B14. minor. Presence overclaims in Element (12.1 l.1268; 6.6 l.653-654).**
"An online dot while its seat is held": a member that dies stays online until the server's presence
timeout. Conduit-family servers default to minutes, not seconds.
Fix: write "an online dot (display; it can lag a dead seat by the server's presence timeout,
HC-A1)". Have HC-A1 record that timeout.

---

## Checked and sound

- I1-I4 still hold for every form in 4.2. The reports level 10 against `top` 10 is safe, because
  `m.room.message` is exempt from I3. The closed questions form passes.
- The `msgtype` table, the examples, AR26 and 12.1 agree. A `m.notice` with mentions stays quiet.
- R15-R44: every requirement in A's scope is cited or carried. R20, R24, R31, R32 and R34-R36 are
  member or host requirements (owned by B and C), and R40 is C's. None vanished.
- There are no install-specific names and no time estimates.
