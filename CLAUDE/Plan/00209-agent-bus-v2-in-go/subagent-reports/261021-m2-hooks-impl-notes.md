# M2 hooks implementation notes (Plan 00209 Task 3.1)

Each agent continuing the Go hooks appends a section: what it built, red/green per H group,
the consolidation list, open points, and where the next agent starts.

## Agent 1 (first section)

### State at hand-over

Branch feat/bus-v2-m2-hooks, package `src-go/internal/pingbus/hooks` (no `Run` yet, no
`hooks.go`; the golden placeholder's exported names are still to be written: `InputMax`,
`Spawner`, `Input`, `Run`). `go test -race ./internal/pingbus/hooks/` is green.
**golangci-lint is NOT clean yet** (the commit is a WIP hand-over made at the context budget):
fix these first, then run `./scripts/qa-changed.bash`:

- errcheck (unchecked type assertions): `membership.go` lines that still do `x.(string)` inside
  an expression (use the `text(...)` / `number(...)` helpers there), and `membership_test.go:86`
  (`errors.AsType` result unchecked: use `errors.As(err, new(*membershipError))`);
- cyclop (max 15): `pydump.go` `writeEscaped`, `pyFloatRepr`; tests
  `TestParseMemberAcceptsAGoodMember`, `TestReadDesiredAndApplied`, `TestToldRecords` (split);
- goconst (`team`, `generation`, `roles`, `handle`, `reason`, `NaN`), gofumpt (three files:
  run `gofumpt -w`), govet shadow (`err` in `parseDesired`, `parseApplied`, `parseTold`, the
  test), nilerr (`bundle.go` `refuseBesideElement`: the `Stat` failure returns nil on purpose,
  Python's `is_dir()` false; restructure so the linter sees a bool), nilnil
  (`readMembershipJSON` absent: return a sentinel or a found bool), perfsprint (`intNumber`),
  recvcheck (`member` mixes pointer and value receivers: make `stateDir`/`tokenPath` pointer
  receivers or `readRest`/`readLimits` free functions), gosec G304 in `files_test.go`
  `assertFile` (a test helper reading a temp path).

### Built (step 1, readers, partly)

| File                                  | What (future home)                                                                                                                                                                                |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pydump.go`                           | Python `json.dumps` encoder: default, compact, sorted, ensure_ascii False (content_size); float repr; `pyLen`, WTF-8 surrogates (bus/state, bus/proto)                                            |
| `files.go`                            | `readRegular`/`readFollowing` (O_NOFOLLOW, O_NONBLOCK, regular, limit), `pyJoin`, `parentOf`, `writeAtomic`, `writeJSONFile` (H88), `writePrivateJSON` (H83), `fsyncDir` (bus/state)              |
| `failure.go`                          | failure classes worst first (H9); error kinds `configError`, `stateError`, `untrustedError`, `membershipError`, `internalError`; `strerror`                                                       |
| `membership.go`                       | desired, applied, told, told-roles readers and writers, `activeOf`, `mountDir`; unhashable farewell reason is internal (v1 TypeError) (bus/state)                                                 |
| `bundle.go`                           | `resolveHome` (H16), `activeTeams` (H17), `world.loadActive`/`loadBundle`/`readToken`/`checkDistinct`, linked-bundle and Element checks, HB6 (bus/state)                                          |
| `member.go`, `teamrecord.go`, `ip.go` | copied from `internal/pingbus/statusline` with tests; `member` extended to all of config.Member (handle, base URL, admin, room, human_text, limits); `teamrecord.go` unchanged so far (bus/proto) |

Red/green: each test file was written before its source and failed to compile (undefined
names) before the source landed; green at the hand-over commit. H6 escapes (`é`, the
surrogate pair) are in the tests as raw-string escapes: the Write tool turns `\uXXXX` in a raw
string into the character, so edit those lines with a script, not the Write tool.

### Next, in the task's order

1. Fix the lint list above; commit.
2. `teamrecord.go`: return a full record (humans, roles, repos with branches, path_prefixes,
   org, exclude, forge_api) for the validator's Context; keep the status line's corpus test.
3. The validator (`protocol.validate_event` and all it reaches, read at `protocol.py:440-1245`):
   key details found: `content_size` is `pyDump(content, styleUnicode)` byte length, a lone
   surrogate there is `schema`; but `len(body.encode("utf-8"))` in `_human_outcome`,
   `_questions_outcome` and `validate_text_content` RAISES on a lone surrogate (uncaught:
   the hook's `internal`); in the questions room a list/object `sender` raises TypeError at
   `ctx.roles.get(sender)` when this member is coordinator (internal). `m.mentions` compared
   with Python dict equality; `mentions.get("room") is True` is JSON true only.
4. The inbox walk (`inbox.py:450-504`), cached record (`inbox.py:326-349`, bytes decode via
   `pyjson.DecodeBytes`), about/rooms (`inbox.py:543-611`, `read_json_file` 311-323: other OS
   errors propagate as internal), lock probe and claim (`inbox.py:932-1039`), seat views
   (`seat.py:245-281,784-813`), inject marks and key (H21, `inject.py`), socket (H20, notify.py).
5. Then steps 2 to 5 of the task file.

### Consolidation list (copies to merge when bus/state and bus/proto exist)

- statusline `member.go`, `teamrecord.go`, `ip.go` -> hooks copies (extended member).
- statusline `roles.go` resolveHome/loadBundle/readMember/checkToken/Element -> `bundle.go`.
- statusline `world.go` read/pyJoin -> `files.go`.

### Open points

- The status line's `limitBounds` lacks `report_per_hour` (limits.BOUNDS has it, 1 to 60): a
  bundle setting it is refused by the Go status line and accepted by v1. Not fixed here (00213's
  package must not be edited); the hooks copy has it, pinned by `TestParseMemberAcceptsAGoodMember`.

## Agent 2

### State at hand-over

Branch feat/bus-v2-m2-hooks. `go test -race ./internal/pingbus/hooks/` is green; still no
`Run` (hooks.go is the golden's placeholder; the `Placeholder` const and golden/pending_test.go
are still in place). `./scripts/qa-changed.bash` reports only findings about code the events
will use: `member.stateDir` unused/deadcode (the survey's state dir), `assertFile`'s mode
always 0600 and `parseTeamRecord`'s server/team always the test's (unparam: they go once the
cached-record reader calls it and a 0644 file is asserted). The commit is a WIP made at the
context budget for that reason.

### Built

- Agent 1's whole lint list fixed (errcheck, cyclop splits, goconst `key*` constants, shadow,
  nilerr via `isDirectory`, nilnil via `readMembershipJSON` returning `found`, perfsprint,
  recvcheck, thelper, gosec G304).
- `teamrecord.go`: `parseTeamRecord` returns the full `teamRecord` (humans set, roles, repos
  with branches (`[]string{}` is any branch), path_prefixes, org/hasOrg, exclude, forge_api)
  with v1's reason words in v1's check order (`TestParseTeamRecordReasonsAreThePythonWords`):
  the goodbye's loss of trust writes them into untrusted.json. Errors are `*untrustedError`.
- `validate.go`: `validateEvent(event, record, server, self, humanText, room) (outcome, error)`,
  all of validate_event: the ping path (content_size via `pyDump(styleUnicode)`: a lone
  surrogate there is `schema`), check_role, refs, branches, re and trust, the agent text, the
  human path, the questions room (edits, coordinator-post, an unhashable sender raises).
  `outcome{kind, reason, class (human|peer|ping), ts *big.Int}`; an `*internalError` where v1
  raises (UnicodeEncodeError on a lone-surrogate body or text, TypeError on a list or object
  sender in the questions room).
- Red/green: validate_test.go was written first and failed to compile (undefined
  validateEvent, roomMain); green after validate.go. `TestValidateEventAgreesWithThePython`
  runs v1's validate_event (testdata/validate_events.py, through execx) over a 120-case corpus
  and compares kind, reason, class and raising: all agree.

### Next, in the task's order

1. Readers left: the lock probe and `read_claim_at` (inbox.py:932-1039), the cached-record
   reader (inbox.py:326-349, `pyjson.DecodeBytes`, then this member's role), the inbox walk
   (inbox.py:450-504; sorted by (ts as big.Int, event id)), about and rooms (inbox.py:543-611;
   read_json_file 311-323 lets other OS errors through: internal), the seat views
   (seat.py:245-281,784-813), the inject marks and key (H21: env bytes that are not UTF-8 become
   U+DC80+b in WTF-8 before the SHA-256), the socket (H20, H44).
2. The events: hooks.py was read whole by agent 2 and the spec's H41-H79 match it.
   `writeToldRoles` ranges over a map, which is right only because writePrivateJSON sorts keys.
3. The goodbye: v1 writes `team.json` in the homeserver's key order, and `pyjson.Object` loses
   order, so `pyjson` needs an order-keeping decode (extend it, test first) for that write.
4. The main, the golden, the CPU table.
