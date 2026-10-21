# N1 Task 1.3 `teamchat/proto`: handoff 4

Branch `feat/bus-v2-n1-proto`, worktree `untracked/worktrees/bus-v2-n1-proto`. The fourth builder
stopped at the context soft budget. Read `untracked/scratch/n1-common.txt` (main checkout) first;
its rules bind the next builder. Handoff 3 is `git show 7d57950d:<this file>`: its tables of
commits, test helpers, decisions 01-28 and gotchas still hold; read it too.

## Done (committed, green: `go test -race`, golangci-lint, `qa-changed.bash`; 97.9% coverage)

| Commit     | Files                                               | What                                                                                                                                                                                                                                                                                                                                                                                         |
| ---------- | --------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `5bbaf1e2` | `forms.go`, `questions.go`                          | `RefusalError{Reason, Detail}` (Detail never quotes the text); `Headline`, `ReportForm`, `QuestionForm(n, body)` (P54; the sender and the index share it), `ThreadBody`; `QuestionIndex` (`NewQuestionIndex(server, floor)`, `Feed(Index verdict)`, `Attach(HUMAN delivery)`, `Next`, `Question(n)`, `Attachment(post)`, `OpenDuplicate`, `Reminders(now)`), `ReminderKey`, states           |
| `a55efc70` | `send.go`, `send_test.go`, `send_questions_test.go` | `Composer{Server, Self, Token, Roster, Lookup, Gate, Reports, Questions, NowMs}`: `Ping(p, answers, display)`, `Text(t, replyTo, display)`, `Notice(n, display)`, `Carried(k, display)`, `Report(text)`, `Question(QuestionPost)`, `Thread(ThreadPost)` give an `Outgoing{Purpose, Content, Forge}`; `Admit(out)` spends the flood budget separately. `probeCheckOf` shared with the verdict |
| `c6a1ab06` | `escape.go`, x/text v0.39.0 vendored, allowlist row | `EscapeJSON`, `EscapeASCII`, `CapBody` (`Body{Rendered, Shown, Total, Cut}`), `WireBytes`, `escapedLookalikes`; tests pin `unicode.Version` and `norm.Version` to 15.0.0, and the exhaustive property over every scalar value (about 70 s under `-race`)                                                                                                                                     |

Test helpers added: `refusalOf(err)`, `goodQuestion`; `questions_test.go` has `qid(c)`, `jsonString`,
`roomEvent`, `questionPost`, `threadPost`, `humanPost`, `threadRel`, `replyRel` and `questionRoom`
(`newQuestionRoom(t, floor)`, `post(id, sender, ageMs, content) int64`, `question(n)`); `send_test.go`
has `composer(t, self)`, `receive(t, out, sender, receiver)` (a composed event judged by the
receiver), `mustSend(t)(c.Ping(...))`, `refusedWith`, `at(t, v, path...)`, `userIDs`; `asked(t)` in
`send_questions_test.go`. `escape_test.go` has `ue(units...)` and `q(s)`.

## Decisions 29-44 (go into the final report with 01-28)

29. **A question whose body fails P54's form is not counted** (as v1 `parse_question`). Fix A 6.5:
    "An uncounted question, or one whose body fails 10.2's form, is not in the index".
30. **A number already counted is not counted again**, the admin's included: the first in stream
    order wins. Fix A 6.5.
31. **"Open" in the duplicate rule means not closed** (open or answered), as v1 `open_duplicate`.
    Fix A 10.2: "A question not yet closed whose `Question:` section equals ...".
32. **The admin's sends are not flood-limited** (no bucket, duplicate window or report window):
    they are C's and D's, bounded by the change, and a migration's carried items must not fail
    `rate`. Fix A 11.1-11.2: the sender rows apply to a member.
33. **`asked_by` names roster members** (else `target`); `re` is grammar only. Fix A 10.2.
34. **An edit in questions is attached by rule 4 alone**: the original's attachment, else the new
    body's `Q<n>` (rule 3 would otherwise always win on the new body). Fix A 10.4 rule 3: "a
    message that is not an edit".
35. **Compose, then the forge check, then Admit**: a send the forge check refuses spends no token
    and does not enter the duplicate window. Fix A 9.3 / 11.2: name the order (rules, secret,
    forms, forge check, flood).
36. **The secret check runs over the `body` too** of every agent event (display text is free text
    the member posts). Fix A 7.3.
37. **The probe sends no text, notice or report** (`role`, as the receiver's rule 08), and **the
    admin posts no notice about the probe** (`target`). Fix A 10.1 ("any active coordinator or
    worker") and A 7.4.
38. **Thread posts**: a reply without `n` must name a human post the index saw unattached; with
    `n`, an answered post must be attached to that question; else `target`. A blank non-closing
    post is `body`. Fix A 10.3.
39. **The admin names a question's `n`, the coordinator never does** (`schema`), and the admin's
    re-posted questions skip the duplicate check (v1 may hold two the same). Fix A 7.7 / 10.2.
40. **A headline is trimmed of Unicode white space**; one starting with a section name is `body`
    (v1). Fix A 10.1-10.2.
41. **DEL is escaped** (v1's `json.dumps` left it raw); invalid UTF-8 is an escaped U+FFFD. No design
    fix: 13.4 already says so; note it as a v1 defect.
42. **ZWJ is the one format character shown**, between two emoji. Fix A 14's body-escaping row and
    13.4's escaped list: "every format character but ZWJ between two emoji pictographs".
43. **x/text v0.39.0**, the lowest release fixing GO-2026-5970 (reached through `norm.Form.String`);
    v0.43.0 would also bump `golang.org/x/sync`. norm ships tables 15.0.0 (`!go1.27`) and 17.0.0
    (`go1.27`), so a Go 1.27 bump fails the pin test until the corpus is reviewed, as 13.4 wants.
44. **The exhaustive property runs about 70 s under `-race`** (planes in parallel; planes 0-1
    dominate). Acceptable; say so in the report.

## Left, in order

1. **envelope.go** (A 13.1-13.5; v1 `helpers/pingbus/envelope.py:228-500` for shape only). Plan:
   - `Entry{Kind, Team, Handle, From, Role, EventID, SentMs (-1 none), Re, Questions bool, Question *EntryQuestion{N, Headline}, Revises, Verb, Ref, Trust, Branch, Silent, Event, Subject, Reason, Carried *EntryCarried{From, SentMs, PossibleDuplicate}, Copy []string, Repeat, Restored bool, Body string}`.
   - Lines in 13.2's table order: kind, from, role, team, room, question, revises, to, sent, id, re,
     verb, ref, trust, branch, silent, event, subject, carried, reason, copy, repeat, restored,
     authority, reply, body, truncated; which kind carries which per the table. Every slot
     full-matched (team, handle, human, sender, id, verb, ref, branch, time, q, k/n, nonce); a
     failing slot is an error naming the entry and slot (E3), and B moves the item to `not shown:`.
   - Kind and authority sentences exactly 13.3 (PING `check`, carried NOTICE, reminder TIMEOUT
     with its `Q<n>` slot, external PING); reply forms exactly 13.2; `body:` through `CapBody`,
     `truncated:` from its `Shown`/`Total`; `question: <q> <json-ascii>` (headline through
     `EscapeASCII`, 1-120 code points, at most 1500 bytes), `question: <q>` when there is no
     headline (decision 45), `question: none`.
   - Proposed decision 46: a carried ping's NOTICE also carries `verb:` and `ref:` (13.2 gives it
     a `--verb` reply but no line naming the verb). Fix A 13.2's table.
   - `Delivery{Nonce, Team, Entries, Answered, HeldBack, NotShown, Gap, Stopped, Pending}.Render()`:
     the first line (counts, `message`/`messages`, `<who>` the first three distinct
     `<sender> (<KIND>)`, a TIMEOUT's its silent addressee, ` and <k> more`, `-` when none), the
     markers `<<teamchat <nonce> i/n>>`, trailing lines in 13.1's order, each kind at most 8 and
     its summary line. `Fit` (8 entries, 12 KiB by `WireBytes`, rest counted as pending; a single
     largest entry must fit: test it). `RenderMembership(lines, nonce)` with H12's form and the
     line rules of 13.3 (at most 600 characters, no C0, DEL, U+2028, U+2029, `<<`, `>>`).
     `FrameText`, 13.5's six items.
2. **The corpus** (A 14, A 13.3's pins): `testdata/` golden files of each kind's entry and each
   trailing line, the look-alike list, and the 13.6 worked attack (a body with real newlines,
   markers, `kind: HUMAN`, U+202E and `ESC[201~` stays on one `body:` line); a test that every line
   full-matches its template. Spell every non-ASCII rune in Go source as `string(rune(0x...))` or
   `ue(...)`: the Write and Edit tools turn ANY backslash-u escape of a non-ASCII code point into
   the raw character (not only format characters).
3. **The review loop**: a fresh Opus reviewer over `db3c8350..HEAD` of the package against chapter
   A; at most 3 rounds; files `subagent-reports/261021-n1-proto-review*.md`.
4. **The report** `subagent-reports/261021-n1-proto.md`: every decision 1-46+ with its design-text
   fix; mark Task 1.3 in PLAN.md.

## Gotchas (new)

- The secret-path guard denies any text holding `.secret` followed by more letters (it matched a
  method call `c.secret(...)`): name such things otherwise (`screen`).
- `judgedEvent` is `$jjj...` and is Seen in `testLookup`; `replyEvent` is `$rrr...`, the ID `event()`
  gives every judged event.
- `qa-changed` widens to the whole project for a `go.mod` change (it runs the bash suites too).
