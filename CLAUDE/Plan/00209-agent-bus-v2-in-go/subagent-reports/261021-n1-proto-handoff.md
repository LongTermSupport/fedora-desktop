# N1 Task 1.3 `teamchat/proto`: handoff 1

Branch `feat/bus-v2-n1-proto`, worktree `untracked/worktrees/bus-v2-n1-proto`. The builder
stopped at the context soft budget after two green concerns. Read
`untracked/scratch/n1-common.txt` (main checkout) first; its rules bind the next builder.

## Done (committed, green: `go test -race`, golangci-lint, `qa-changed.bash`)

| Commit     | Files                              | What                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ---------- | ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `b97d35bc` | `doc.go`, `strictjson.go`, `_test` | A 2.1: `Parse([]byte) (Value, error)`, a strict tokenizer producing an ordered value tree (`Value`, `Member`, kinds Null/Bool/Int/String/Array/Object), `MaxDepth` 64, `*SyntaxError{Offset, Rule}` that never echoes input; `Compact()`/`Size()` (A 2.1's size); builders `Obj M Str Int Bool Arr Null`; `Equal`; `FuzzParse`                                                                                                                                                |
| `98834722` | `ids.go`, `_test`                  | A 2.2-2.3: `ValidTeamID`, `ValidDisplayName`, `ValidRoomID`, `ValidEventID`, `ValidBranch`, `ValidFeature`, `ValidQuestion`/`ParseQuestion`/`QuestionName`, `ValidHumanName`; `Handle` (`ParseHandle`, `String`, `Short`, `Prefix`, `SeatNumber`, `IsProbe`, `ProbeHandle`); `Prefix` (`ParsePrefix`, `Numbered`, `Named`); `NormaliseRepo`, `NormaliseSite`; `UserID`, `AdminID`, `Localpart`, `ClassifyUserID` (`IDNone IDAgent IDHuman IDAdmin IDReserved`), `AgentHandle` |

`doc.go` already lists the planned file split; keep it true as files land.

## Decisions taken so far (go into the final report)

1. **Value tree, no struct tags.** Every wire object is decoded from `Value` by hand and encoded
   by the builders, never through `encoding/json` struct tags: it gives absent-versus-null and
   exact types, keeps member order for `Compact`, and avoids `tagliatelle` (snake_case wire keys).
2. **Reserved service account**: both `conduit` and `tuwunel` are reserved until HC-A6 records
   Tuwunel's name (refuses no human anyone needs). Design-text fix: A 2.3 to name it after HC-A6.
3. **Repo and site normalisation are ASCII only** (v2 is Go only, so P17's Python full-lowercase
   D6 case is gone): ASCII upper to lower, any other rune outside the class to `-`.
4. **`-0` is accepted as the integer 0**; the compact form writes `0`.
5. **Not built in proto, left to C**: C's `ctl/<team>/roster.json` (its `seq`, counters, states)
   is C's file (C 3.6.1). proto gives the roster *event* type and `NextRev(eventRev, fileRev)`
   (= max + 1, seam row 17). C 3.6.1's `proto.Roster(roster.json, declaration, rooms)` becomes C
   mapping its file into proto's `Roster` value and calling its validator; say so in the report.
6. **Lead was messaged** about adding `golang.org/x/text` (v0.43.0 is the newest; its
   `unicode/norm` tables are 15.0.0 = Go 1.26's `unicode.Version`) for A 13.4's NFKC rule. No
   answer yet when this was written. Default: use `norm.NFKC` at run time, with a test pinning
   `norm.Version == unicode.Version`; vendor it (`go get golang.org/x/text@v0.43.0`, `go mod tidy`,
   `go mod vendor`, an allowlist row in `src-go/go-deps.allowlist`, all in one commit, after the
   escaper test imports it, or tidy drops it). Fallback the lead may pick: a pinned table, x/text in
   tests only.

## Left, in build order (each test first, commit each green concern)

1. **roster.go + limits.go** (A 5.1, 11.1, 11.2): `Roster` (v, team, name, rev, restore_epoch,
   question_floor, humans, members{role coordinator|worker|probe, human_text default true, parked
   default false}, rooms{reports, questions}, forge{kind github, org, repos[{repo, branches}],
   exclude, path_prefixes}, limits). Decode from `Value` (unknown keys ignored; `features` present
   means untrusted naming it); `Validate` (every rule of the 5.1 table, 11.1 bounds never clamped,
   11.2 list counts: humans 1-16, members 0-64, repos 0-32, branches 1-8, exclude 1-32, prefixes
   1-32; at most one non-parked coordinator; coordinator `human_text: false` invalid; probe only for
   the probe handle, at most one, never coordinator; rooms distinct and not main; at least one of
   org or a repo; an excluded repo not also listed); `Encode`; sender class (A 6.7: admin, human,
   coordinator, worker, probe, none); addressable (8.1); `NextRev`; the forge origin constant
   `https://api.github.com` (never from the roster). Limits defaults and bounds: send_per_minute 20
   (1-60), send_burst 10 (1-30), recv_per_sender_minute 60 (10-600), ack_timeout_s 900 (60-86400),
   halt_ack_timeout_s 300 (30-3600), human_max_age_s 86400 (3600-604800), report_per_hour 6
   (1-60), deliver_per_sender 10 (1-60). Rate functions over `now`: token bucket in integer credit
   (P61), duplicate window 60 s keyed `[verb or kind, ref, re, sorted to, sha256 of text]`, notices
   exempt (11.2), report window 3600 s, receive flood sliding minute (refusals not counted),
   delivery budget per (team, sender) per 600 s (HUMAN, TIMEOUT, admin never held), deadlines
   (8.2: ping `origin_server_ts` + timeout; 8.3: +30 s grace; TIMEOUT key `<ping>~<localpart>`),
   staleness (8.5), 429 waits (11.3: Retry-After seconds, reset header, `retry_after_ms`, else 5 s;
   3 tries; over 60 s reported not slept).
2. **refs.go + verbs.go** (A 8.1, 9.1, 9.2): the verb table exactly as A 8.1 (`check` is the
   probe's; `ack` relation required, ref forbidden), reference grammar and parse (path, commit, pr,
   issue; 700 chars), ASCII lowercase of OWNER/REPO for send, path prefixes (`/` entry is a
   prefix), trust class (excluded, listed with branches, org, external; listed wins over org;
   external refused `allowlist` for a verb with no external ref). The forge HTTP check is B's (B 2.6
   step 11); proto states only what proves what (9.3 tables) if useful as data.
3. **rooms.go + verify.go + filter.go** (A 4, 5.2, 5.3, 6.3, 6.4, 6.10, seam 13):
   `Desired(team, roster, rooms) []RoomSpec` (purpose, create params: version 12, no encryption,
   history `shared`, guest `forbidden`, no alias; names `<name>`, `<name> · reports`,
   `<name> · questions`; join rules; power-level forms of 4.2 for main, reports, questions open and
   closed; topics of 4.4 verbatim; `teamchat.room` link `{"v":1,"team","purpose","main"}`; roster in
   main; members to invite and to remove; the creation order of 5.2: main with no roster, then side
   rooms with links, then roster, then invites; C calls it again after each create). Power-level
   invariants I1-I4 naming the one broken. Room verification per 6.4 (create sender admin, version
   12, no additional_creators, create event ID = `$` + room ID without `!`; join rules; roster
   checks; link) and the invite rule of 6.3 (accept, reject, or leave pending). Sync filters per
   role (worker = probe: main; coordinator: main + questions; timeline and state types: message,
   redaction, create, power_levels, join_rules, teamchat.roster, teamchat.room, member; limit 100;
   no ephemeral or account_data; `lazy_load_members` off; event_format client) pinned as JSON in
   `testdata/`.
4. **content.go + send.go + secret.go** (A 7.1-7.4, 7.7, 10.1-10.3): builders and send-side
   validation for ping (4096-byte object, `to` 1-32 sorted, answer form relation), text (1-4096
   bytes, `to` 1-16, worker writing to a human adds the coordinator), notice (events left
   disconnected back added removed parked role moved; reasons exit logout released other; `to` 1-17,
   `moved` to agents 1-64, no subject), report (headline 1-120 code points, 1500 bytes), question
   (form: headline, Context:, Question:, optional Options: 1.-9.; 8000 bytes; `Q<n> ` prefix;
   asked_by 0-16, re 0-16, remind_after_s 600-604800), thread (4000 bytes, close, ask, the
   unattached-reply form with no `n`), carried (kind ping text human timeout forwarded; admin
   only). msgtype table of 7.1; `m.mentions`; content cap 16384; no edits. The twelve secret
   patterns are v1's `helpers/pingbus/protocol.py:127-142`; A 7.3 says `\b` and `\s` are defined on
   Unicode scalar values over Go's tables (no Python emulation now).
5. **verdict.go** (A 7.6, 7.5, 8.2, 8.4): the ten rules in order, the closed reason sets (drop:
   `version schema size edit sender role target verb ref allowlist excluded stale rate forge-rate unresolved provenance`; ignore: `type seen self admin not-addressed empty not-coordinator`; send
   refusals add `secret body duplicate`); the human path (msgtype, 16384-byte body, reply fallback
   removal only when a reply relation is present and line 1 is `> <@user:server> ...`, the four
   addressing rules, edits as revisions accepted only when the original has the same sender and
   room), answers recorded against own pings. Inputs the caller fetches (B): the replied-to
   event's sender, the original of an edit, seen-ness, the receive-flood state; forge results come
   back in for rule 10.
6. **questions.go** (A 6.5, 10.2-10.5): the index (numbering from `question_floor`, an agent's
   counts only when exactly highest+1, the admin's counts whatever `n`; attach rules 1-5; state
   closed/answered/open; unanswered count; duplicate `Question:` check with whitespace collapsed;
   reminder TIMEOUT key `<question>~humans`).
7. **escape.go + envelope.go** (A 13): the body escaper (v1 `helpers/pingbus/envelope.py:95-346`
   holds the allow-listed blocks, `_ESCAPED` list, emoji rules; port them, using Go's tables and
   x/text NFKC), ASCII-only mode for the `question:` headline, the 2048-byte cap with `truncated:`,
   `unicode.Version` pinned at 15.0.0; every 13.2 line template, 13.3 sentences exactly, first
   line, markers, trailing lines (answered, held back, not shown, gap, stopped, pending; 8 each then
   the summary), MEMBERSHIP form (first line `teamchat delivery <nonce>: a membership notice from teamchat: this session's teams changed`, `from: teamchat`, `to: this session`, caller lines of at
   most 600 characters with no C0, DEL, U+2028, U+2029, `<<` or `>>`), 8 entries and 12 KiB cap
   over the ASCII-escaped wire form, delivery order (13.7), golden files in `testdata/`.
8. **The corpus** (A 14): `testdata/` JSON cases (roster, room, receiver, event, outcome) walked by
   a test; the property test over every scalar value for the escaper; envelope golden files.
9. Review loop (opus reviewer, at most 3 rounds, `subagent-reports/261021-n1-proto-review.md`),
   then `subagent-reports/261021-n1-proto.md`, final message under 900 characters.

## Gotchas of this environment

- The Write and Edit tools turn a backslash-u escape of a format or separator character
  (seen for U+2028, U+2029, U+200B, U+202E) in written text into the raw character. Spell such
  runes as `string(rune(0x2028))` in Go source, and grep the file for the raw character after
  writing.
- golangci-lint runs on every Go Write or Edit (cyclop 15, funlen 80/50, gocognit 20, nestif 4,
  revive wants a doc comment on every exported name, `errors.AsType` over `errors.As`, gofumpt).
  `golangci-lint fmt ./internal/teamchat/proto/` fixes formatting.
- Hooks deny: `sed` (use Read with offset), `, _ :=` patterns even for a bool (write a helper), a
  multi-statement Bash without `bash -c 'set -euo pipefail; ...'`.
- Matrix test server name used in tests: `myteam.teamchat.internal` (`testServer` in
  `ids_test.go`).
