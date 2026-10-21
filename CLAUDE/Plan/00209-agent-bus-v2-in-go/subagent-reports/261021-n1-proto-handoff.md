# N1 Task 1.3 `teamchat/proto`: handoff 2

Branch `feat/bus-v2-n1-proto`, worktree `untracked/worktrees/bus-v2-n1-proto`. The second builder
stopped at the context soft budget. Read `untracked/scratch/n1-common.txt` (main checkout) first;
its rules bind the next builder. x/text is APPROVED (lead): vendor only `unicode/norm` and what it
pulls in, add a `src-go/go-deps.allowlist` row in its style (what it is for, who uses it), keep a
test pinning `norm.Version == unicode.Version`, govulncheck clean, and commit go.mod, go.sum,
vendor and the row together, after the escaper test imports it (or tidy drops it).

## Done (committed, green: `go test -race`, golangci-lint, `qa-changed.bash`; 96.7% coverage)

| Commit     | Files                                                          | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ---------- | -------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `b97d35bc` | `doc.go`, `strictjson.go`                                      | A 2.1 strict decoder, `Value` tree, `Compact`/`Size`, builders, `FuzzParse`                                                                                                                                                                                                                                                                                                                                                                                                |
| `98834722` | `ids.go`                                                       | A 2.2-2.3 identifiers, handles, user-ID classes                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `abe91660` | `roster.go`, `limits.go`, `refs.go`, `verdict.go`              | `Roster` (`DecodeRoster`, `Validate`, `Encode`, `SameExceptRev`, `Sender` classes, `AddressableAgent`, `Coordinator`, `HumanText`, `IsHuman`), `NextRev`, `Forge` (`DecodeForge` for C's `trust`, `Validate`, `Class`, `ListedBranches`), `Limits` table, `SendGate` (dup window + integer bucket), `Window` (reports, receive flood, delivery budget), `RetryWait`; ref grammar, `LowercaseRefRepo`, `PathAllowed`, `ForgeOrigin`; the closed reason sets in `verdict.go` |
| `7a65b43b` | `verbs.go`                                                     | A 8.1 table as data, `AddressableFor`, `PingDeadlineMs`, `AnswerOnTime`, `TimeoutDue` (+30 s), `TimeoutKey`, `PingStale`, `TextStale`                                                                                                                                                                                                                                                                                                                                      |
| `6dea7c32` | `rooms.go`, `verify.go`, `filter.go`, `testdata/filter-*.json` | `Desired(server, roster, RoomIDs, description) []RoomSpec` in A 5.2's order (main first with no roster; side rooms with links; roster and `Invite` only once all three exist), `MembershipDiff`, `ValidDescription`; `VerifyMain`, `VerifySide`, `CheckPowerLevels` (I1-I4 named), `Invite` (accept, reject, pending); `SyncFilter` pinned per role                                                                                                                        |
| `c895635f` | `secret.go`, `secret_test.go`                                  | the twelve v1 shapes with `\s` = White_Space and a Unicode `\b` checked by hand; `SecretShape(text, token)`; own token = `own-token`                                                                                                                                                                                                                                                                                                                                       |

The field reader `fields` (`readFields`, `integer`, `str`, `boolean`, `object`, `optObject`,
`need`, `strs`, `has`, `get`) is in `strictjson.go`; every decoder uses it. Test helpers:
`mustParse`, `testRoster` (A 5.1's example plus a parked probe), `anchors(self)`, `roomState`.

## Decisions taken so far (go into the final report)

01. **Value tree, no struct tags** (handoff 1).
02. **Reserved service account**: `conduit` and `tuwunel` both reserved until HC-A6 (A 2.3 fix).
03. **Repo and site normalisation ASCII only**. 4. **`-0` accepted as 0**.
04. **C's `roster.json` stays C's**; proto gives `Roster`, its validator and `NextRev`.
05. **`exclude` entries are `OWNER/REPO`**, any owner, as A 5.1's example writes them (v1 had bare
    names under `org`). Design fix: say so in 5.1's table.
06. **An empty `exclude` or `path_prefixes` list is none** (the 5.1 example writes `[]`, 11.2 says
    1-32). `Encode` omits them. Design fix: 11.2 "0-32, empty is none" or fix the example.
07. **`repos` may be absent** (none); `name` and `rooms` are required; `forge` required.
08. **A probe-handle member must have role `probe`, and role `probe` only the probe handle**
    (2.3 "never reused for anything else"); a coordinator with `human_text: false` is invalid
    parked or not.
09. **"Any role" in 8.1 is coordinator or worker** (6.7: the probe sends `check` only).
    `check` may be addressed to the probe itself (A4's round trip).
10. **Duplicate key** absent values are empty strings; digest empty for pings.
11. **Desired gives `Members` (desired set) and `Invite`**; C diffs with `MembershipDiff`.
    Reports: humans invited, agents join by rule. Main and reports `Members` include parked members.
12. **Power-level absent keys take Matrix's defaults**; a non-integer level fails `power levels`.
13. **Invite to reports is rejected**; an admin invite to an unnamed room, or any admin invite
    before main verifies, is pending.
14. **Presence filter senders** = humans and every roster member, sorted.

## Left, in build order (each test first, commit each green concern)

1. **content.go + send.go** (A 7.1-7.4, 7.7, 10.1-10.3). Facts gathered:
   - typed key exactly one of the room's (main: ping, text, notice; reports: report; questions:
     question, thread); none, two or another room's: `schema`. Then the typed object's `v`
     (not an integer: `schema`; an integer other than 1, or `features` present: `version`),
     then size (content 16384; ping object 4096), then edits (`m.new_content` anywhere,
     `rel_type` `m.replace`: `edit`), `format` present: `schema`, `body` not a string: `schema`.
   - relations, exact shapes: none; reply `{"m.in_reply_to":{"event_id":X}}`; the answer form
     `{"rel_type":"m.thread","event_id":P,"is_falling_back":true,"m.in_reply_to":{"event_id":P}}`;
     questions' thread form (10.3, `is_falling_back` true or false). Decide: a ping takes none or
     the answer form only (a plain reply on a ping: `schema`); text none or reply.
   - v1 reasons (`protocol.py:600-735`): `to` count or repeat: `target`; not addressable:
     `target`; ref missing, forbidden, bad grammar or form: `ref`; `branch` only beside path or
     commit and `ValidBranch`, else `ref`; excluded: `excluded`; external for a verb with no
     external: `allowlist`; a path outside `path_prefixes` (listed or org only): `allowlist`.
     Receivers accept `to` in any order and sort it; senders sort.
   - text: 1-4096 bytes, `to` 1-16, role-holding non-parked members and listed humans; a worker
     writing to a human adds the coordinator (P21; v1 `with_coordinator_copy`). msgtype table 7.1.
   - notice: events `left disconnected back` (sender = subject; reason for left: `exit logout released other`), `added`/`role` (with `role`), `removed`, `parked`, `moved` (admin; `moved`
     has no subject, `to` 1-64 agents, no human); `to` 1-17, never the subject; unknown event:
     `schema`. Glosses table in 7.4 (for the envelope).
   - report 10.1: body headline 1-120 code points after trim, whole 1500 bytes, `body` refusal;
     question 10.2 (form, 8000, `Q<n> `, asked_by 0-16, re 0-16, remind_after_s 600-604800,
     duplicate `Question:` with whitespace collapsed names the open `Q<n>`); thread 10.3 (4000,
     close with empty text and body `Q<n> closed.`, no-`n` unattached reply form, `m.text` when
     asking again). carried 7.7 (kind ping text human timeout forwarded; verb and ref only with
     ping; `to` exactly one; text 4096 and secret-checked; admin only in main).
   - send side: build the content, run the receive decoder over it, then `SecretShape`, the body
     forms and the duplicate gate (`SendGate.Admit`, notices exempt).
2. **verdict.go** (A 7.6 rules 01-10 at 7.6, lines 985-1036; 7.5 human path at 951-983; 8.2, 8.4).
3. **questions.go** (A 6.5 at 666-678, 10.4-10.5 at 1380-1406).
4. **escape.go + envelope.go** (A 13 at 1589-1923; v1 `helpers/pingbus/envelope.py:95-346`), x/text.
5. **Corpus** (A 14 at 1924), then the review loop and `261021-n1-proto.md`, as the common rules say.

## Gotchas of this environment

- Write and Edit turn a backslash-u escape of a format or separator character into the raw
  character: spell such runes `string(rune(0x2028))` in Go.
- golangci-lint runs on every Go write (cyclop 15 even in tests, funcorder: unexported methods
  after exported ones, prealloc, gosec G101 on credential-like test literals and G304 on
  `os.ReadFile` of a joined path: use `fs.ReadFile(os.DirFS("testdata"), name)`), and
  `golangci-lint fmt ./internal/teamchat/proto/` fixes formatting.
- Hooks deny `sed`, any `, _ :=` (even a bool: use `fields.need`/`optObject` or a helper), and a
  multi-statement Bash without `&&`; write commit messages to `untracked/scratch/commit-msg.txt`
  in the worktree and `git commit -F` it.
- Test server `myteam.teamchat.internal` (`testServer`), team `myteam`, rooms `mainRoom`,
  `reportsRoom`, `questionsRoom`, members `coordID workerID lxcWorkerID parkedID probeID aliceID`.
