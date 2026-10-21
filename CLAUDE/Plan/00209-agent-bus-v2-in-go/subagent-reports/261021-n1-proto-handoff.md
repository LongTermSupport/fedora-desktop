# N1 Task 1.3 `teamchat/proto`: handoff 3

Branch `feat/bus-v2-n1-proto`, worktree `untracked/worktrees/bus-v2-n1-proto`. The third builder
stopped at the context soft budget. Read `untracked/scratch/n1-common.txt` (main checkout) first;
its rules bind the next builder. x/text is APPROVED (lead): vendor only `unicode/norm` and what it
pulls in, add a `src-go/go-deps.allowlist` row in its style (what it is for, who uses it), keep a
test pinning `norm.Version == unicode.Version`, govulncheck clean for this package (it does not
reach net/http; if GO-2026-6809 is reported, check that and say why), and commit go.mod, go.sum,
vendor and the row together, after the escaper test imports it (or tidy drops it). x/text
v0.31.0 is in the module cache.

## Done (committed, green: `go test -race`, golangci-lint, `qa-changed.bash`; 97.4% coverage)

| Commit     | Files                                                          | What                                                                                                                                                                                                                                                                                                                                                       |
| ---------- | -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `b97d35bc` | `doc.go`, `strictjson.go`                                      | A 2.1 strict decoder, `Value` tree, `Compact`/`Size`, builders, `FuzzParse`                                                                                                                                                                                                                                                                                |
| `98834722` | `ids.go`                                                       | A 2.2-2.3 identifiers, handles, user-ID classes                                                                                                                                                                                                                                                                                                            |
| `abe91660` | `roster.go`, `limits.go`, `refs.go`, `verdict.go`              | `Roster`, `NextRev`, `Forge`, `Limits`, `SendGate`, `Window`, `RetryWait`; ref grammar; the closed reason sets                                                                                                                                                                                                                                             |
| `7a65b43b` | `verbs.go`                                                     | A 8.1 table, `AddressableFor`, deadlines, `TimeoutDue`, `TimeoutKey`, `PingStale`, `TextStale`                                                                                                                                                                                                                                                             |
| `6dea7c32` | `rooms.go`, `verify.go`, `filter.go`, `testdata/filter-*.json` | room specs, `VerifyMain`/`VerifySide`, `CheckPowerLevels`, `Invite`, `SyncFilter`                                                                                                                                                                                                                                                                          |
| `c895635f` | `secret.go`                                                    | `SecretShape(text, token)`                                                                                                                                                                                                                                                                                                                                 |
| `92351aec` | `content.go`                                                   | `DecodeContent(content, keys)` (keys `MainKeys`, `AdminMainKeys`, `ReportKeys`, `QuestionKeys`): typed key, version first, sizes, edits, `format`, `body`, exact relations, each typed object; `Relation` (`ReplyTo`, `AnswerTo`, `InThread`, `IsAnswer`, `Encode`); `DecodeHuman` (reply fallback, threads, edits); `need` takes objects only             |
| `db9874d9` | `verdict.go`                                                   | `Judge{Server, Self, RoomID, Purpose, Roster, NowMs, Lookup, Flood}.Judge(raw) Verdict`: rules 01-10; outcomes `Deliver Ignore Drop Record Index Redact Fault`; `Verdict.Forge` (the check B runs), `Verdict.Answer`; `Roster.checkAgent`/`checkPing`/`checkRef`/`checkNoticeTo`/`checkAdminNotice`, `checkCarried`, `checkTo` (shared with the send side) |

Test helpers: `mustParse`, `testRoster`, `anchors`, `roomState`; in `verdict_test.go` `judge(t, self, purpose)` (probe unparked), `event(sender, ageMs, content)`, `pingContent`, `textContent`,
`noticeContent`, `humanContent`, `carriedContent`, `fakeLookup`/`testLookup`, `testNow`;
`content_test.go` has `mustDecode`, `mustHuman`, `pingEvent replyEvent otherEvent`.

## Decisions taken so far (go into the final report)

01-15: as handoff 2 (git show cde625c6 for its text): value tree; `conduit` and `tuwunel` reserved;
ASCII normalisation; `-0` is 0; C owns `roster.json`; `exclude` as `OWNER/REPO`; empty lists are
none (11.2 says 1-32); `repos` optional; the probe handle and role go together, a coordinator
needs `human_text`; "any role" is coordinator or worker; duplicate-key empties; `Desired` gives
`Members` and `Invite`; power-level defaults; invite rules; presence filter senders.

16. **Agent relations are exact shapes** (no extra key); a human's relation and mentions are read
    leniently, as v1's `_addressed` (a malformed part is none).
17. **Relations per key**: text none or a reply; notice, report, question and carried none; a
    thread post with `n` the thread form, without `n` exactly a reply and no `close`; a ping by its
    verb, an optional relation being none or the answer form (a plain reply is `schema`).
18. **Notice `to` on receipt**: listed humans and the non-parked coordinator, never the subject;
    `moved` to addressable agents. An admin notice's subject is a roster member, except `removed`.
19. **Carried**: `from` a human or agent user ID of the server (else `schema`); a carried ping's
    unknown verb is `verb`, its reference grammar per verb `ref`; `text` optional, 0-4096 bytes.
20. **The admin is exempt from the receive flood and staleness**, so a migration's carried items
    are never lost to `rate`. Design fix: say so in 11.1 (`recv_per_sender_minute`) and 7.6 rule 10.
21. **A question-room post that fails its shape** in rule 03's index path is dropped with that
    reason, not judged further.
22. **Rule 05's "a member that reads reports is a fault"** is the outcome `Fault`.
23. **`human_text: false` is checked before a human message is decoded** (drop `sender` first).
24. **P21's coordinator copy is the sender's only**; receivers do not require it.
25. **A malformed edit** (bad original ID, no `m.new_content`, not `m.text`) is `edit`; a body that is
    blank after trimming is ignored `empty`.
26. **A redaction's target is `content.redacts`** (room version 12).
27. **The forge check is `Verdict.Forge`**, run by B; `provenance`, `unresolved` and `forge-rate`
    are B's drops after it.
28. **`check` has exactly one addressee** (else `target`), and the probe is addressable for an
    answer whose root is a known `check` from the probe (`Lookup.Event`).

## Left, in build order (each test first, commit each green concern)

1. **send.go** (A 7.1-7.4, 7.7, 10.1-10.3, 11.2). Plan: a `Composer{Server, Self, Token, Roster, Lookup, Gate *SendGate, Reports *Window, Index *QuestionIndex, NowMs}` with `Ping(verb, to, ref, branch, answers)`, `Text(to, text, replyTo)`, `Notice(Notice, display)`, `Report(body)`,
   `Question(body, askedBy, re, remindAfterS)`, `Thread(n, body, close, ask, inReplyTo, fallingBack)`, `UnattachedReply(post, body)`, `Carried(...)`, each returning the content `Value`
   (with `msgtype` by 7.1's table, `body` display text, `m.mentions` sorted addressees) or a
   refusal. Order: a worker's text or ping to a human adds the coordinator (P21), senders sort
   `to`; build; run `DecodeContent` and `Roster.checkAgent` (or `checkAdminNotice`/`checkCarried`
   for the admin) as a receiver would; `SecretShape` over free text (`secret`); the body forms
   (`body`: report headline 1-120 code points after trim and 1500 bytes; question form of P54 as
   v1 `protocol.py:1360-1420`: headline, `Context:` and `Question:` required, `Options:` lines
   `1. ` to `9. ` in order, a section twice is `body`, 8000 bytes; thread 4000, empty only when
   closing with body `Q<n> closed.`); `duplicate` for an open question with the same collapsed
   `Question:` section; the report window (`report_per_hour`, `Count` first, record after the
   gate); then `SendGate.Admit` with `DuplicateKey` (notices exempt). The forge check is returned
   for B to run.
2. **questions.go** (A 6.5 at 666-678, 10.4-10.5 at 1380-1406): the index fed from `Verdict`s of
   outcome `Index` (counting from `question_floor`, an agent's `n` only at highest+1, an admin's
   whatever its `n`), attachment rules 1-5, states and unanswered counts, `Next`, the duplicate
   check, reminders keyed `<question>~humans`.
3. **escape.go + envelope.go** (A 13 at 1589-1923; v1 `helpers/pingbus/envelope.py:95-400`, whose
   `_PS_ALLOWED`, `_ESCAPED`, emoji and keycap rules and `cap_body` binary search carry over),
   x/text for NFKC. The exhaustive property test over every scalar value; ASCII-only headline mode.
4. **Corpus** (A 14 at 1924), then the review loop and `261021-n1-proto.md`, as the common rules say.

## Gotchas of this environment

- Write and Edit turn a backslash-u escape of a format or separator character into the raw
  character: spell such runes `string(rune(0x2028))` in Go.
- golangci-lint runs on every Go write (cyclop 15 even in tests: split long tests; exhaustive on
  switches over the package's int enums: use `switch {` with conditions; funcorder; prealloc;
  wastedassign; gosec G101 and G304), and `golangci-lint fmt ./internal/teamchat/proto/` formats.
- Hooks deny `sed`, any `, _ :=` or `, _ =` (use a helper such as `reasonOf(_ Ref, r string)`),
  a multi-statement Bash without `&&`, and `| tail`/`| head` (pipe to
  `/workspace/.claude/hooks-daemon/bin/echd-capture 30`); write commit messages to
  `untracked/scratch/commit-msg.txt` in the worktree and `git commit -F` it.
- deadcode in `qa-changed` fails an exported function no test reaches.
- Test server `myteam.teamchat.internal` (`testServer`), team `myteam`, rooms `mainRoom`,
  `reportsRoom`, `questionsRoom`, members `coordID workerID lxcWorkerID parkedID probeID aliceID`
  (the probe is parked in `testRoster`).
