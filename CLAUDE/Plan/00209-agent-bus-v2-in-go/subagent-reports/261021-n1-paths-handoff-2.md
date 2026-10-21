# Plan 00209 N1 Tasks 1.1-1.2 (task name "paths"): handoff 2

The third builder stopped at its context budget during the round-2 fixes. Worktree
`/workspace/untracked/worktrees/bus-v2-n1-paths`, branch `feat/bus-v2-n1-paths`. The rules that
bind the next agent are in `/workspace/untracked/scratch/n1-common.txt`. The lead's task also
asks for this: the gate allows the matrix replay fixtures by an exact allowlist, with red and
green proof. Read the first handoff, `261021-n1-paths-handoff.md`, only for its history.

## Done (committed)

- `5cacffd2`: round-1 findings 01-03, 05 and 09-12 in paths (100% coverage).
- `516cf722`: round-1 findings 04, 06 (K7) and 07 (K5, one row per line); `ElementPlayFile`
  moved to `playbooks/imports/optional/common/`, where v1's Element play is. The three
  matrix replay rows are proven in `run_test.go`
  (`TestK5TheMatrixReplayRowsAllowExactlyItsThreeReferences`).
- The real tree was proven through the wrapper with the matrix branch's real
  `fakehs/replay_test.go` as an untracked probe:
  - with no rows: exit 1, 3 findings (lines 26, 28, 205);
  - with the three rows: exit 0;
  - with an extra `/var/lib/agent-bus` line: exit 1, 1 finding.
    The probe was then removed, and the tree is clean.
- Review round 2: `261021-n1-paths-review-2.md`, FIX-FIRST (1 major, 9 minor). It is
  committed with this handoff.

## Work in progress (the WIP commit with this handoff; does NOT pass QA yet)

- Round-2 01 (major): every id in `ids.go` is now an opaque struct (`TeamID{s}` and the
  rest), with `String`, `MarshalText` (which refuses the zero value) and `UnmarshalText`
  (through Parse). `Kit` is opaque too. `HostKitCurrent()` and `CCYKitDir()` are now
  functions. `paths.go` compiles.
- Round-2 06: `ParseDigest` is lowercase hex, 16-64 characters.
- New tests in `ids_test.go`: `TestIDsDecodeOnlyThroughTheirParse`, `TestAZeroIDIsNotEncoded`
  and `TestParseDigestIsLowercaseHex`. `paths_test.go` and `separation_test.go` were rewritten
  by `gofmt -r` (`string(x)` became `x.String()`, and the kit constants became calls).

Left to make it green, in `src-go/internal/teamchat/paths`:

1. revive: give each `MarshalText` and `UnmarshalText` in `ids.go` a doc comment, or group
   them. gofumpt: run `golangci-lint fmt ./internal/teamchat/paths/`.
2. `ids_test.go`: fix the `err` shadow in `TestIDsDecodeOnlyThroughTheirParse`. Take
   `"Digest"` out of the segment-parser map in
   `TestSegmentIdentifiersRefuseWhatCouldLeaveTheirDirectory`, since digests are hex now.
3. `separation_test.go`:
   - `sampleDigest` must parse 16 hex.
   - `notName` must also skip `*.String`, `*.MarshalText` and `*.UnmarshalText`, and the
     `Kit.String` method.
   - `HostKitCurrent` and `CCYKitDir` stay in `constNames` (as `.String()`) so the
     reserved-tails test still sees them. `exportedIdents` now lists them as funcs, which is
     fine.
   - Check what `gofmt -r` did to the file: it may have rewritten conversions such as
     `string(data)` (one is already fixed back).
4. Run `go test -race -cover` (target 100%) and `golangci-lint run`, then
   `./scripts/qa-changed.bash`, then commit.

## Left: the round-2 minors (exact fixes are in the review file)

- 02: in the `reference` regex in `scope.go`, add `['"]/` to the prefix alternation, for
  `root_dir ~ '/files/...'` and `+ '/files/...'`. Add both repository lines as cases, and a
  K7 red case.
- 03: in `judgeFile`, run `path.Clean` on each reference first. A result that starts with
  `../` or leaves `tasks/` or `files/` (or `vars/`) is a finding. Add a traversal test.
- 04: bring `vars/` into scope (ScanPaths, InScope below a `teamchat*` component, the regex,
  and the routing glob `vars/*teamchat*`). Update the QA.md row and the wrapper.
- 05: accept `play-teamchat*.yaml` (InScope, routing glob, and move the scope_test case to
  `in`).
- 07: derive `perTeamRoots` from the team-derived samples of `ops` (the prefix before `/ops`
  followed by `/` or the end). Sample `Layout.Seat` in both layouts.
- 08: on this branch, the rows are not in `allowed.tsv` (a row for a file that is not in the
  tree is stale). `feat/bus-v2-n1` already holds the matrix `replay_test.go`, which is
  identical to its branch. A trial merge of it was gate-green with the three rows, but
  qa-changed was red on govulncheck GO-2026-6809 (crypto/x509 through
  `matrix/fakehs/http.go:62`). This container runs go1.26.9 and needs go1.26.10 (the ccy
  image is rebuilt on the host; F44 has the image pin 8459fc36d, and the go.mod bump
  73e518c76 is not merged). So the merge was aborted, and the lead was told. Record this in
  the PLAN.md Task 1.2 note and in the report: the merge into `feat/bus-v2-n1` adds the three
  rows below to `allowed.tsv` (tab-separated, verbatim from the test's `replayRows`), and that
  merge is gated on `qa-teamchat-separate` exit 0.
  - `src-go/internal/teamchat/matrix/fakehs/replay_test.go` TAB
    `tests/helpers/pingbus/fixtures/tuwunel` TAB `the recorded real-Tuwunel captures the replay reads, which v1's tests own`
  - same path TAB `probe.agent-bus.internal` TAB `the server name the capture was recorded under; the replay must match it byte for byte`
  - same path TAB `"agent_bus.status"` TAB `the v1 event type the capture recorded, which the replay picks out of the recorded sync`
- 09: bring the wrapper header in `scripts/qa-teamchat-separate.bash` (scope and exit-1
  wording) into line with the CLAUDE/QA.md row.
- 10: the design-text fixes go in the final report, each with its line:
  - C 3.9:1736 `_lxc`;
  - A 2.2 and D 3.5: the reserved ids, and the rule that no new fixed `teamchat-<x>` may take
    a declared team id;
  - D 4.1:383-386: `/ccy/membership`;
  - D 4.1:379: plays in subdirectories, `.yaml` and `vars/`;
  - D 3.7:273: the Element play directory.

Then do review round 3 (a fresh opus reviewer writes `261021-n1-paths-review-3.md`; it is the
last round allowed). Commit each review file. Mark PLAN.md Tasks 1.1 and 1.2 at MERGE. Write
`261021-n1-paths.md` with what was built, coverage, the decisions, and the design-text fixes;
the first handoff's last paragraph lists the earlier ones. The decisions taken in this round:

- opaque ids;
- hex digests;
- K7 and the SharedFiles list;
- one row per line;
- `/ccy/membership`;
- the Element play in `optional/common`;
- the rows recorded for the integration merge rather than landed here.
  No version bump has been demanded.
