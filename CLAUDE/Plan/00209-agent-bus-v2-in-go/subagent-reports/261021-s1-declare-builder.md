# Plan 00209 Task 2.2 stage 1: src-go/internal/bus/declare, builder notes

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Uncommitted in the worktree (branch feat/bus-v2-s1-declare); the coordinator commits.
Files: `src-go/internal/bus/declare/{errors,types,read,grammar,parse,value,queries,lxc}.go`, each
with its `_test.go`, plus `parity_test.go` (the real declare.py, test only) and
`testdata/parity/` (138 fixtures: 64 whole files, `refused/` 34, `lxc/` 40). The fixtures are
written by `untracked/scratch/261021-s1-declare-gen-fixtures.py` (now kept as this plan's
`s1-declare-gen-fixtures.py`; run from the worktree root;
it writes exact bytes: CRLF, lone CR, BOM, NEL, U+2028, invalid UTF-8). Placeholder values
only (192.0.2.x, 2001:db8::, example-org, example.com).

## Exported API

```go
type Kind int
const (KindCrash Kind = 1; KindUsage Kind = 64; KindRefused Kind = 78)
var ErrCrash, ErrUsage, ErrRefused, ErrVaulted error   // errors.Is targets
type Error struct{ Kind Kind; Vaulted bool; Message string } // Message has no "declare: "
func (e *Error) Error() string   // Message
func (e *Error) Code() int       // 1, 64 or 78
func (e *Error) Is(error) bool   // kind sentinel; ErrVaulted when Vaulted (a 78 refusal)

func Read(path string, readFile func(string) ([]byte, error)) (string, error) // V1
type HostVars string                                // the text Read returned
func (HostVars) Declared() ([]Team, error)          // V10, V11
func (HostVars) Limited() ([]Limit, error)          // V12
func (HostVars) Undescribed() ([]Undescribed, error) // V13
func (HostVars) TeamsFor(repo string) ([]string, error) // V15, V16
func (HostVars) Choices(repo string) ([]Choice, error)  // V17
func (HostVars) ElementTeams() ([]Profile, error)   // V18
func (HostVars) Address() (string, error)           // V19
func (HostVars) Settings(team string) ([]Setting, error) // V20
func (HostVars) NextSteps(team string) (string, error)   // V21
func GithubRemote(url string) string                // V22
func (HostVars) LxcMembers() ([]LxcMember, error)   // V23
```

Types with internal/declare's names, fields and meanings (so `type Team = busdeclare.Team`
etc. works): Team, DescriptionState (DescriptionVaulted, DescriptionInvalid), Limit,
Undescribed, Profile, Standing (StandingCovers, StandingWiden, StandingExcluded,
StandingUnreadable), Choice, Setting (Value json.RawMessage), SettingState (SettingVaulted,
SettingInvalid), LxcMember. The grammars (team, seat, host, OWNER/REPO, container, user, the
remote pattern, isdigit's other digits, normalise_repo, resolve_host, is_description) are in
`grammar.go`.

Mapping to internal/declare.Error: `&declare.Error{Code: e.Code(), Stderr: "declare: " + e.Message}`.

## Red / green

1. RED: every `_test.go` written first; `go test ./internal/bus/declare/` failed to build:
   `undefined: HostVars`, `undefined: Team`, `undefined: Error`, `undefined: KindUsage`, ...
2. GREEN, first unit run: 3 failures, each a wrong expectation of mine, checked against v1:
   `github_remote("https://github.com/example-org/proj.git.git")` is None (repo `proj.git`
   ends .git); `cutComment(" #c")` keeps the space its callers strip; `path_prefixes: 1.5` is
   the string "1.5" in v1 (only JSON gives floats), so it iterates. Then two more found when the
   `TestP*` tests ran: a dict path_prefixes is iterable (its keys); `   - team: b` (indent 3) is
   "not the shape", not "not indented alike". Fixed in the tests.
3. Parity, first run: all cases alike. Proven able to fail by mutation:
   - "no repository" -> "no repositor": 55 failure lines (next-steps, undescribed...).
   - the comment rule judging only ' ' as whitespace: NOT caught (a fixture gap). Added
     `comment-whitespace.yml` (tab, NBSP, U+3000, U+2003, U+001F, U+202F before '#'): 24
     failures with the mutation, green after reverting it.
4. Final: `go test -race -count=1 -cover ./internal/bus/declare/` ok, 99.0%, ~48 s (python
   runs in parallel); `golangci-lint run ./internal/bus/declare/...` 0 issues; vet, gofmt clean.
   No //nolint, no discarded error, no wall clock; every process via execx.OS{}.

## Parity cases (TestParityWithDeclarePy)

`env PYTHONPATH=<root> PYTHONDONTWRITEBYTECODE=1 python3 -P -m helpers.agent_bus.declare ...`
against the reader, compared as: v1 exit 0 -> the reader's values written back as v1's marker
lines equal v1's stdout byte for byte; 64/78 -> same code and `declare: <message>\n` stderr;
exit 1 with a traceback -> the reader's KindCrash.

- 64 whole fixtures x 24 queries (declared, limited, undescribed, element-teams, address,
  lxc-members, teams-for and choices x 4 repos incl. an invalid one, settings and next-steps x
  5 teams) = 1536
- 34 refused-whole fixtures x 12 = 408; 40 LXC fixtures x 2 = 80
- 8 file cases (missing, pathlib-normalised missing, a directory, a path through a file)
- 24 github-remote URLs
- Total 2056 (last full count on 2032 before comment-whitespace.yml: 937 answered alike, 1011
  refused alike, 84 tracebacks matched by the crash class).

TestOtherDigitsAreIsdigitLessIsdecimal: the isdigit-but-not-decimal table against python over
every code point both Unicode editions assign (284,278; 128 such digits).

## v1 behaviour not matched, and why

- B-S3: where v1 crashed (iterating an int/bool, `in` on an int, joining a non-string repo, an
  int over 4300 digits, a superscript port, printing a lone surrogate, an unreadable or
  non-UTF-8 file) the reader returns KindCrash with its own message; v1 had printed the rows
  before the crash, the reader returns none (internal/declare dropped stdout on error anyway).
- B-S4: a value with a tab is returned whole (the rendered line equals v1's bytes; the old
  marker parser refused it).
- B-S6: a port beyond int64 is refused 78 `line N: the port is not a number`; v1 printed it.
  Unit-tested only (TestPortBeyondAnIntIsRefused), not a parity fixture.
- Port 0: `Team.Port` is 0 for both "no port" and port 0, as in internal/declare's type; only
  the URL tells them apart, and with no address both read the same.
- Lone surrogates: the reader treats any in printed text as the crash (host python's strict
  stdout). The container's python 3.11 has `surrogateescape` on stdout and prints a low
  surrogate U+DC80-DCFF as a raw byte instead; fixtures use high surrogates.
- Settings with NaN/Infinity: v1 prints `NaN`/`Infinity` (json.dumps), which the reader returns
  as-is in `Setting.Value`; the old internal/declare.Settings refused such a line ("the value is
  not JSON"). After aliasing, a caller that json-marshals Value would fail. Coordinator's call
  whether internal/declare keeps its json.Valid check.
- JSON nesting: pycompat refuses past 990 levels; v1's real limit from inside declare.py is a
  little lower and version-dependent. Only "very deep is a crash" is proven.
- Unicode editions: Go 15.0 vs python 3.11's 14.0 (host 3.13: 15.1); a code point assigned in
  only one may answer differently (isspace, isdigit, Cc/Cf, lower).
- argv bytes that are not UTF-8 (python's surrogateescape) are not modelled: --repo, --team,
  --url are Go strings.
- `_checked_repo`'s second refusal ("not a repository the bus can name") is unreachable once
  is_owner_repo passes, so it is not implemented.

## pycompat

No defect found. Used: Loads, RawDecodeString, Dumps, PyStr, PyRepr, Truthy, Equal, Contains,
Iterate, Strip/LStrip/SplitLines/IsSpace/Lower/Len/CodePoints, IsDigitNd, ParseDigits,
IPAddress.

## QA

`./scripts/qa-changed.bash` (log: untracked/scratch/261021-s1-declare-qa-changed.log): this
package passed (ok, 99.0%). The run failed in other agents' work: `go test -race` failed in
internal/teams, teams/board, teams/choose, teams/hot, teams/login, teams/picker,
teams/restart, teams/seats, teams/shutdown, all on seat state ("agent-bus seat list could not
read every seat", "seat 3 ... does not show the seat free", "... is not a directory (a symlink
is refused)"), and suite test-ccy-teams-command.bash on the same seat rows (held/free). These
follow the uncommitted internal/agentbus and internal/bus/seat changes; nothing imports
internal/bus/declare yet. The suite is also routed to the new fixtures because it names files
called empty.yml, no-teams.yml and states.yml.
