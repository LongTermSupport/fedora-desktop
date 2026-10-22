# N2 slice n2b: handoff 1

Worktree `/workspace/untracked/worktrees/bus-v2-n2b`, branch `feat/bus-v2-n2b` (stacked on
`feat/bus-v2-n2a` at f3aa1bb0). The builder stopped at its context budget; the lint hook's
whole-file echoes were the cost. Read `SPEC-n2b.md` first: it is the contract, numbered, with
decisions n2b-D1 to n2b-D11.

## Done (committed)

- 5e3c772b: `SPEC-n2b.md`.
- c09c379e:
  - `src-go/internal/teamchat/state` (ST1, ST2): `WriteFile(path, data, mode, gid)` atomic,
    `ReadFile(path, limit)` capped, no links, regular files only. 86.7%; the chown-failure test
    skips as root (the container runs as uid 0). A `.golangci.yml` G304 exclusion for
    `state.go`, with its reason.
  - `src-go/internal/teamchat/declare` (DR1-DR8), 94.4%:
    - `node.go`: the strict typed reader over `proto.Value`, and `FieldError{Field, Problem}`,
      which never echoes a value.
    - `values.go`: the value grammars, and `trust` as A's forge section through
      `proto.DecodeForge`.
    - `declare.go`: `Decode`, `FromValue` (the YAML reader's way in), and the `Team`,
      `LXCMember` and `ElementTeam` types. `Team.URL` is the resolved URL.
    - `input.go`: `Input` (one team, its LXC entries, the address), `Declaration.Inputs`,
      `Input.Encode`, `DecodeInput`, `Team.Canonical` (`team.json`) and `DecodeTeamFile`.

`qa-changed` is red only on govulncheck GO-2026-6809. That stops go-qa's runners before the
tests, so run `go test -race -cover` on the packages yourself.

## Left, in order

1. **The gate, AG1-AG7**, in `declare` as `gate.go` (test first, `gate_test.go`):

   - `Gated` (sorted lists) and `Input.Gated()`;
   - `LXCEntry` `<container>/<user>,human_text=<bool>` and its parser;
   - `Difference{Field, Old, New}`, `Compare(prev *Gated, in)`, the `SECURITY` line, and
     `Check(decl, loader)`;
   - `Change{Field, Op, Value}` and `Approve(prev, in, *Change)`, with the planted-entry test
     (a human planted beside the owner's `--add`, refused 70), and `RefusedError`;
   - the `approved.json` codec, and `FileApprovals{Path func(TeamID) string, GID int}` over
     `state`.

2. **The YAML reader, HV1-HV3**: `declare/hostvars`. Vendor `go.yaml.in/yaml/v3` v3.0.5, the
   version `src-go/qa` already uses, which is in the module cache: run `go get`, then
   `go mod tidy`, then `go mod vendor`. Add the allowlist `module` row. Convert `yaml.Node` to
   `proto.Value` and call `declare.FromValue`.

3. **The go-deps keepout rule, GD1-GD3**, in `src-go/qa/godeps`:

   - a 4-field `keepout` row in `allowlist.go`;
   - `go list -deps -f '{{with .Module}}{{.Path}}{{end}}' <pkg>` run through `goRun` in
     `deps()`;
   - tests with the fake runner;
   - the row `keepout go.yaml.in/yaml/v3 ./cmd/teamchat-admin`.

   The spec of record is `CLAUDE/Plan/00204-go-tui-polish/SPEC-qa-go-deps.md`: append the
   kind there too.

4. **Apply, AP1-AP12**: `internal/teamchat/apply`.

   - The roster file codec and `paths.RosterFile` and `paths.RoomsFile` (add them to `paths`,
     with its tests).
   - The rooms record as `rooms.Recorder`.
   - `DeriveRoster` and `Run`: the gate, then `ServerAdmins`, then humans
     (`CreateAccount`, `SetLocked`, `Account`), then `rooms.Converge`, then roles.
   - End to end on fakehs; `rooms/rooms_test.go`'s `world` is the pattern.
   - Map `*apply.Error{Drift}` in `admin.ExitCodeOf`.
   - A tagged Tuwunel test only if cheap: copy `rooms/tuwunel_test.go`'s `realAdmin`.

5. The review loop (Opus, at most 3 rounds), then `subagent-reports/261022-n2b.md`, then the
   PLAN.md Tasks 2.3 and 2.4 progress lines.

## Gotchas

- The pre-commit scanner rejects `/home/<word>` in any staged file: use `/srv/...` in fixtures.
- The secret guard blocks any identifier matching `*.secret*` in Write/Edit content: do not
  name a test field `secret`.
- `sed` is denied. Use Edit, or `python3 -I -c` for an escape-sensitive replace. Chain Bash
  with `&&`.
- golangci: `cyclop` max 15 (tests included), `recvcheck` (no mixed pointer and value
  receivers), errcheck on unchecked type assertions in tests (use the `obj`/`arr` helpers in
  `declare_test.go`), ST1018 on literal format characters (write `‎`).
