Verdict: MERGE

# Plan 00209 N0 area A (protocol and rooms): adversarial review, round 3 (final)

Reviewed: `DESIGN-v2-A-protocol.md` (1767 lines) against the frame, the N0 common rules, the round-2
review and the scratch digests. Line numbers are those of the reviewed file.

Counts: round 2: 14 FIXED, 0 PARTLY, 0 NOT FIXED. Round 1 (PARTLY in round 2): 3 FIXED. New:
blocker 0, major 0, minor 5.

## Part A: earlier findings

| #            | Status | Checked at                                                   |
| ------------ | ------ | ------------------------------------------------------------ |
| B1           | FIXED  | 6.6 l.663-665; 20 l.1737-1738; HC-A1 l.1653                  |
| B2           | FIXED  | 10.5 l.1198-1199; 13.2 l.1397                                |
| B3           | FIXED  | 13.2 l.1407-1410; 13.6 point 1 l.1560-1562                   |
| B4           | FIXED  | 11.2 l.1258-1260                                             |
| B5           | FIXED  | 6.5 l.632-640; 13.2 l.1475-1476; 13.5 l.1547 (but see C1)    |
| B6           | FIXED  | 6.5 l.624-626                                                |
| B7           | FIXED  | 7.6 rule 09 l.912-915; 8.4 l.990 (a new edge, C5)            |
| B8           | FIXED  | 13.1 l.1356; 13.2 l.1479-1480; 13.3 l.1504-1508 (but see C2) |
| B9           | FIXED  | 7.6 rule 08 l.911; the corpus covers every reason, 14 l.1612 |
| B10          | FIXED  | 7.6 rule 05 l.899-903                                        |
| B11          | FIXED  | 13.2 l.1400; 13.1 l.1368-1370                                |
| B12          | FIXED  | 10.5 l.1196-1202                                             |
| B13          | FIXED  | AR6 l.53-56; 7.1 l.736; 10.1 l.1106-1109                     |
| B14          | FIXED  | 12.1 l.1289; 6.6 l.665; HC-A1 l.1653                         |
| R1 2 (`who`) | FIXED  | as B1                                                        |
| R1 3 (slots) | FIXED  | as B3, B8, B11                                               |
| R1 8 (bound) | FIXED  | as B6                                                        |

## Part B: new findings (all minor)

**C1. minor. 10.2 numbers questions by the raw maximum, which contradicts 6.5 (10.2 l.1137-1139 against 6.5 l.637-640).**
10.2 says `n` is "1 plus the highest `n` among the `<ns>.question` events". 6.5 counts only a
question whose `n` is exactly 1 plus the highest counted so far. A sender built from 10.2 takes
999999999 from one bad post and breaks `ask-human`, which is the B5 attack again. Neither section
says whether an uncounted question still takes part in attachment (10.4), reminders (10.5) or the
`question:` headline.
Fix: in 10.2, write "`n` is 1 plus the highest counted `n` of the question index (6.5)". In 6.5, add
"an uncounted question is not in the index: nothing attaches to it, it has no reminder and no
headline".

**C2. minor. The MEMBERSHIP form bound to 00203 H12 does not match H12 (13.3 l.1504-1508; 13.1 l.1356; 13.6 point 1 l.1560).**
In v1 and H12, a MEMBERSHIP notice is its own envelope with its own first line ("...: a membership
notice from ...: this session's teams changed"; digest v1-code l.475, inject-hooks l.181). 13.3
says "keep v1's form" but also counts MEMBERSHIP as `<M> membership` in the shared first line. H12's
lines also carry the team description (admin-written topic text with spaces and possibly non-ASCII;
H76 excludes only `<<` and `>>`). So "only slots are team names and handles" and 13.6 point 1's "ASCII
with no space" are both false for these lines.
Fix: choose one form. Recommended: MEMBERSHIP is always its own delivery with H12's first line, and
the `<M> membership` count is dropped from 13.1. In 13.3, list the description as a slot under P16
plus H76's `<<`/`>>` exclusion. Have 13.6 point 1 cite that rule for these lines, and note that the
admin writes the description (R38).

**C3. minor. A closed question still gets its reminder (10.5 l.1196-1199).**
The condition is "no attached human post" only. A question the coordinator closes without any human
answer (it resolved the matter itself) still produces a TIMEOUT that tells it to remind the humans.
Fix: "a question that is neither closed nor answered (10.4 state `open`) by ...".

**C4. minor. `humans` is a valid human name, so `silent: humans` and `<who-name>` are ambiguous (2.3 l.221; 13.2 l.1452, l.1462).**
`humans` matches `[a-z][a-z0-9_-]{0,31}`. A human named `humans` who is the silent addressee of a
ping renders the same `silent:` and `<who>` slot as a reminder.
Fix: add `humans` to the reserved localparts in 2.3, so C refuses it as a human name. The corpus
then pins that `<human>` never matches `humans`.

**C5. minor. Rule 09 records an answer that rule 10 may then drop (7.6 l.913-917; 9.3 l.1066-1069).**
A non-stale `nack`, `done` or `blocked` is recorded at rule 09, which stops its ping's TIMEOUT. If
rule 10 then drops it (`rate`, `unresolved` because the receiver's forge login failed or the forge
was unreachable, or `provenance`), the session gets no entry, no `answered:` line and no TIMEOUT. The
ping waits unseen for ever. The cause is also wider: "forge unreachable" and `forge-rate` are
permanent drops under judge-once, while 6.5 retries transient homeserver failures.
Fix: "a non-stale `nack`, `done` or `blocked` is recorded only when rule 10 delivers it. If rule 10
drops it, it is not recorded, so its TIMEOUT still falls due." Optionally, hold a ping whose forge
check failed transiently (`forge-rate`, unreachable, 5xx) and judge it again within its timeout,
instead of dropping it.

## Checked and sound

- Rule 09's "Then answers" agrees with 8.2, 8.4 and 8.5 for `ack` and for stale answers.
- The 10.5 reminder renders under 13.2: no `verb:`/`ref:` lines, `question: <q>`, `silent: humans`,
  `id:` is the question's, and the reminder's kind and authority sentences are pinned in 13.3.
- `who` labelling is the same in 6.6, 12.2 and 20, and HC-A1 records the presence timeout.
- The agent-account definition (6.5) keeps humans and the admin out of the question index.
- No install-specific names and no time estimates. Section 17 decisions were not re-raised.
