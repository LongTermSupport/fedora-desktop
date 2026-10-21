# Handoff: Plan 00209 M1, `bus/state` (member-side on-disk state)

Written by the first m1-state agent at its context budget, after the reading and the spec,
before any Go. The next agent continues in the same worktree and branch.

## Where things are

- Worktree `untracked/worktrees/bus-v2-m1-state`, branch `feat/bus-v2-m1-state`.
- F44 was merged into the branch first (`git merge --no-ff F44`; one conflict in
  `CLAUDE/Plan/README.md`, resolved by keeping both lines). The branch base lacked
  `src-go/internal/pingbus/{pyjson,statusline}`, Plan 00211's SPEC and the current
  `helpers/pingbus`. The lead was told; sibling branches (m1-proto) likely have the same gap.
- `CLAUDE/Plan/00209-agent-bus-v2-in-go/SPEC-state.md` is written: S1-S45, L1-L12, the error
  classes, the write styles table, the corpus format, departures SB1-SB8 and findings F1-F8,
  each with its Python `file:line`. It is the source of truth; read it first.
- No Go, no corpus, no Python test yet. Nothing else changed.

## What is left (the task as the lead gave it)

1. **pyjson extension** (`src-go/internal/pingbus/pyjson`, new file plus tests): an encoder
   (`ensure_ascii` escaping; the four styles of SPEC S17: compact, sorted, spaced, sorted-bare;
   ints via big.Int; floats refused) and an ordered decode option (objects keep key order, for
   `outbox.json`'s `gate.recent`). Do not change existing behaviour; statusline uses the decoder.
2. **`src-go/internal/bus/state`** (test file first: the TDD hook blocks a new source file with
   no test): `Error`/`Class`; the strict JSON object helpers over `pyjson.DecodeUTF8` with
   `NoDuplicateKeys`; the regular-file reader (S4); the atomic writer with v1's temporary name
   (S15; do not use `fsx.WriteAtomic`, finding F5); the layout (S6-S14); every kind of sections
   5-8; the locks L1-L11 (use `golang.org/x/sys/unix`, already vendored via execx); the
   `TODO(bus/proto)` validators of section 10, listed in the report.
   - member.json: port the reading in `internal/pingbus/statusline/member.go` and `ip.go`
     (urlsplit and ipaddress emulation, already parity-tested) into bus/state, adding
     `report_per_hour` (finding F1). Do not refactor statusline.
   - Desired membership: write bus/state's own strict decoder; `internal/teams/membership` is
     the host writer (encoding/json, finding F6) and stays as it is.
3. **Corpus** `tests/fixtures/agent-bus-state/<kind>/{valid,invalid}/` and `cases.json` per SPEC
   section 11, cases from the formats and findings F2-F8 (e.g. `v: true` in `sync.json` is v1
   accept / v2 `type`, SB6; a list as a farewell `reason` is v1 `crash`, F4).
4. **Python walk** `tests/helpers/pingbus/test_state_corpus.py` (stdlib unittest, `REPO_ROOT`
   inserted into `sys.path` as `tests/helpers/pingbus/test_seat.py` does), one v1 reader adapter
   per kind, and the v1 writer round trip for `roundtrip` cases.
5. **Go corpus walk** and **lock proofs**: a second real process (the test binary re-run as a
   child; `execx.OS{}.Run` with the test binary path, a ready file and a release file, no
   wall-clock reads in tests: `project_testclock`), and a real v1 process
   (`python3 -I <testdata script> <checkout> ...` as `statusline/writers_test.go` runs its
   writers) for L1, L7, L8 and L9 in both directions.
6. Red then green recorded in the report `subagent-reports/261021-m1-state.md`; 90% coverage,
   `-race`, no `//nolint`, no discarded errors; `./scripts/qa-changed.bash` before each commit
   (never qa-all, never push).
7. Adversarial review by an Opus sub-agent into `subagent-reports/261021-m1-state-review.md`,
   fixed until MERGE; final message to the lead under 1500 characters.

## Facts learnt that the spec does not spell out

- `hooks.py` grep or `class X` lookups are blocked by the LSP hook; use `awk` line ranges.
  `sed`, `| head` and `| tail` are blocked; use `awk 'NR>=a && NR<=b'`.
- `src-go/qa/internal/cmdmaintest` is in the nested `qa` module; `src-go` packages cannot import
  it. Spawn the child through `internal/execx` in a test file (test files are exempt from
  `project_seam_commands`).
- Python 3.11.2 and Go 1.26 are on this machine.
- `tests/helpers/pingbus/` already holds the v1 unit tests; the new corpus test goes beside them.
