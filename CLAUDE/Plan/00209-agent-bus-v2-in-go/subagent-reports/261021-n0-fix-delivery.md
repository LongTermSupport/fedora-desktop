# Plan 00209 N0 fix pass: theme delivery (envelope, frame text, CLI verbs and exit codes)

Fixer for theme "delivery". Owns A (all), B 2.2 (2.2.1-2.2.4) and B 2.7 (2.7.1-2.7.6). By the
lead's assignment it also edited B 2.12 items 5-6 and the skill-versus-code test paragraph, and the
B 3 rows BD3, BD4 and BD7, which decide the sections it owns. Branch
`feat/bus-v2-n0-f1-delivery`.

## Dispositions

| Review, finding              | Disposition                                                        | Change                                                                                                                                                                                                                                                                                                                              |
| ---------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| quality 1                    | fixed, A 13.1-13.8, B 2.7.2                                        | A 13 is the one grammar; B 2.7.2 renders it and restates none. `truncated:` and frame item 4 use `verify`; `held back:` names `history`; every command in B's form (flags after the verb, B 2.2.1); the questions reply is `reply '<id>'`; humans first (13.7); the frame at every SessionStart; a worked example moved into A 13.1 |
| quality 6                    | fixed, A 12.4, 7.6 rule 01, 7.3, 10.2, OD-A10; B 2.7.3             | admin-only `teamchat-admin redact` through ccy-teams with the owner's password; members drop a pending item a redaction names; delivered copies, backups and the export are said to keep it. C's verb is a cross-fixer request                                                                                                      |
| quality 7                    | fixed, A 13.1, 13.7; B 2.7.3                                       | one team per delivery; the next delivery is for the team with the oldest pending HUMAN item, else the oldest item; the "several teams" crash rules deleted. B 2.3.2's `teams` field is a cross-fixer request                                                                                                                        |
| quality 8                    | fixed, B 2.2.2, 2.2.4                                              | exit 12 and `no-room` deleted; a missing or failing side room is 22 `untrusted`; the error example is now exit 11 `role`                                                                                                                                                                                                            |
| quality 9                    | rejected with reason, B 2.2.1, BD4                                 | `request` is the envelope's word for a human's authority, the line the envelope keeps; `ping` is also the kind, wire key and Element line. A bare `ping` exits 64 with the verb list and a pointer to `status`                                                                                                                      |
| quality 10                   | fixed, B 2.2.2, BD3                                                | records start at `"v": 1`                                                                                                                                                                                                                                                                                                           |
| quality 11                   | fixed, B 2.2.1, 2.2.4                                              | `recv` and `inbox` with nothing pending: no records, exit 0; exit 3 deleted                                                                                                                                                                                                                                                         |
| quality 12                   | fixed, B 2.2.4, 2.12 item 5                                        | four exit classes (fix, wait, read, tell a human) with a Class column; the skill teaches the classes; the full table is `teamchat help exit-codes` (new `help` verb)                                                                                                                                                                |
| quality 13                   | fixed, A 7.1, 7.2, 8.1, 12.1, AR22, OD-A11                         | a ping's answers are replies in the ping's thread (the answer form); `pr:` and `commit:` references also as GitHub URLs in the Element `body`                                                                                                                                                                                       |
| quality 14                   | fixed differently, A 7.7                                           | no feature name: carried items are posted only by a migration, so a release can stop reading the key once no v1 team remains (it then falls to 7.6 rule 06, ignored) with no new major. The "drain before the fence" idea is D's: cross-fixer note                                                                                  |
| quality 15                   | fixed, A 3.1, 3.2, AR4, 5.1, 14, 18, 20                            | any `features` key refused; the registry, its test and `proto.send` are built with the first feature or second major                                                                                                                                                                                                                |
| quality 16                   | not mine                                                           | the hook-side file is B 2.8 (member-process fixer)                                                                                                                                                                                                                                                                                  |
| coherence 3                  | fixed, A 13, B 2.7.2, B 2.2.1                                      | one grammar (A owns, B renders); `verify` decided for `truncated:` and frame item 4; the flag rule decided in B 2.2.1 (flags after the verb, no global flags) and A's templates match; first-line form fixed (`message`/`messages`, five counts, `on <team>`); line order is A's; E21's bound restated in A 13.2                    |
| coherence 4                  | fixed, A 11.1, 11.4; B 2.7.3; BD7                                  | the budget is applied at admission and the surplus is not kept (v1's consumption, with the `history` pointer); two alternatives weighed in A 11.4; NOTICE inclusion aligned to A (member-sent notices count). HUMAN and TIMEOUT room in `pending`: cross-fixer request to B 2.3.2                                                   |
| coherence 5                  | fixed in A 7.4, A 20; cross-fixer to C                             | A 7.4 lists every admin post; C 3.6.3-3.6.4 must post `added` (add, return) and `role` (demote, promote), and replace the "only notices" sentence                                                                                                                                                                                   |
| coherence 6                  | fixed, A 7.4, 11.2, 13.2-13.3, A 20; seam row 43; cross-fixer to D | new notice event `moved` (admin, no subject, to every agent member, pinned), with fixed glosses on the `event:` line; carried kind `moved` renamed `forwarded`                                                                                                                                                                      |
| coherence 7                  | fixed in A 5.2 and A 20; cross-fixer to C                          | a park is a roster write plus the rooms step; no change is complete until both are done. C 3.6.1 and 3.6.3 must say when and how the roster event is written                                                                                                                                                                        |
| coherence 12                 | fixed, B 2.2.1, 2.2.2; seam row 44; cross-fixer to D               | `status` prints `outbox` records (targets, deadline, answers, timed out). The A7 trigger is D's                                                                                                                                                                                                                                     |
| coherence 13                 | fixed, A 10.3, 10.6, 13.2; B 2.2.1                                 | every questions entry's reply line is `teamchat reply '<id>'`; an unattached post gets a plain reply (`teamchat.thread` with no `n`); why `thread --re` is dropped is stated in A 10.3                                                                                                                                              |
| coherence 14                 | fixed, B 2.2.2, 2.2.4                                              | as quality 8                                                                                                                                                                                                                                                                                                                        |
| coherence 15                 | A already single-sourced (A 5.1); cross-fixer to C and B           | drop `human_text` from `seat.json` (C 3.7.2); B 2.6 step 10 and C 3.6.4 read the roster                                                                                                                                                                                                                                             |
| coherence 19 (part)          | fixed, B 2.2.1, 2.12 item 6                                        | `ask` prints a `question` record; `--asked-by` resolution and the `--re` main-room check (10 `re`) stated; `report` listed for any member. The rest (skill text from 00211 10.1-10.2, C 3.11.2 health checks) is not mine                                                                                                           |
| coherence 20                 | fixed, A 5.2; cross-fixer to C                                     | the bootstrap order stated; C 3.6.2 must pass created room IDs back into `Desired`                                                                                                                                                                                                                                                  |
| coherence 21                 | cross-fixer to member-process fixer                                | E26's never-restarted codes belong to B 2.4.2's supervision; the 2.7.6 row is unchanged                                                                                                                                                                                                                                             |
| security 5, part 2           | fixed, A 7.7, A 20; cross-fixer to D                               | nothing from v1's questions room is carried (reported instead; the question is re-posted open)                                                                                                                                                                                                                                      |
| security 5, part 1           | rule fixed in A 7.7; cross-fixer to D                              | inbox and inject entries are candidates only; carried only if the v1 server shows them addressed to that member                                                                                                                                                                                                                     |
| security 14                  | fixed in A 6.9; cross-fixer to C                                   | a threat row: the network is not a boundary between teams; tokens and login rate limits are. C 3.2.5 owns the sentence and the bridge-subnet scoping                                                                                                                                                                                |
| operations 15                | not mine, except the table                                         | the exit-69 recovery text and crash loop are B 2.4.2 and 2.8 (member-process fixer); 2.2.4's 69 row is unchanged                                                                                                                                                                                                                    |
| lead: skill-versus-code test | fixed, B 2.12, A 14                                                | the test also parses every `teamchat` command in A 13's templates, trailing lines, authority sentences, frame text and the hook texts of 2.8 with the CLI's own parser                                                                                                                                                              |

## Rows changed in DESIGN-v2.md

- Seam rows 5 (now Fixed), 10, 20, 43, 44.
- Decisions rows BD3 (`"v": 1`), BD4 (`ping` kept), BD7 (surplus not kept), and two new rows
  OD-A10 (admin redaction, VISIBLE) and OD-A11 (answers in the ping's thread).

## Cross-fixer requests

To the member-process fixer (B outside 2.2 and 2.7):

1. B 2.3.2: delete the awaiting entry's `teams` field (the example at the `awaiting` JSON, the
   sentence about `teams` sorted, and `teams` in the awaiting cap); keep room in `pending` for
   HUMAN and TIMEOUT items however many agent items it holds (A 11.4), so a full `pending` never
   stops human delivery.
2. B 2.15: delete the "several teams (2.7.3)" crash test row; add one for "a delivery holds one
   team's items" and for the admission-time flood budget.
3. B 2.4.4: the socket example's error object (`"v":2`, exit 12 `no-room`) becomes `"v":1` and a
   live code.
4. B 5: "JSON Lines with `"v": 2`" becomes `"v": 1`.
5. B 2.6 step 03: the `gap` item is A 13.2's `gap:` trailing line (cite it); BD15 says "a `gap:`
   line" instead of "a `gap` NOTICE". The `stopped:` line covers "a team that stopped on a
   permanent failure"; name where 2.6 decides that.
6. B 2.6 step 10 reads `human_text` from the roster, never the bundle (coherence 15).
7. The 2.2.4 paragraph under the exit table (keeper serves the socket, `no-worker`) and the 69 row's
   code names follow whatever process model and process name you settle (quality 2 and 4).
8. B 2.8's hook texts: name commands in B's form, since the 2.12 test now parses them.
9. Coherence 21: map E26's never-restarted codes in B 2.4.2.

To the host fixer (C):

1. C 3.3: add `teamchat-admin redact --team T --event ID --reason secret`, reached only through
   ccy-teams with the owner's password, never automatic (A 12.4, OD-A10); its output says backups
   and the export keep the text.
2. C 3.6.3: post A's `added` notice as the last step of `add` and `return`; replace "these two are
   the only notices the admin's account posts" with A 7.4's full list.
3. C 3.6.4: each demote and promote posts A's `role` notice.
4. C 3.6.1 and 3.6.3: every member or role change, park included, is one roster write (`rev` from
   the current event plus one, validated by A's decoder before the write) followed by the rooms
   step in A 5.2's order.
5. C 3.6.2: create in A 5.2's bootstrap order and pass created room IDs back into `Desired`.
6. C 3.7.2: drop `human_text` from `seat.json`.
7. C 3.2.5: say the network is not a boundary between teams (A 6.9), and add the bridge subnet only
   to teams that declare an LXC member.
8. C writes no `proto` and no `features` key in the roster (A 3.1-3.2).

To the migration fixer (D):

1. D 5.6 and D 6.2: the welcome notice is A 7.4's `moved` notice (admin, no subject, to every agent
   member, pinned).
2. D 5.2 and OD-D7: the carried kind `moved` is renamed `forwarded` (A 7.7).
3. D 5.2 row 1: nothing from v1's questions room is carried; such items go to the migration report
   (A 7.7, security 5 part 2).
4. D 5.2 rows 2 and 4: inbox and inject entries are candidates; carry one only after the v1 server
   shows it addressed to that member (A 7.7, security 5 part 1).
5. D 6.1 A7: read the answer from `status`'s `outbox` records (B 2.2.2), say how the ping is
   triggered, and word it as "one closed-verb ping answered", not "one acknowledged ping" (quality
   9's liveness reading).
6. Quality 14's "drain before the fence" idea, for the owner digest if D wants it.
