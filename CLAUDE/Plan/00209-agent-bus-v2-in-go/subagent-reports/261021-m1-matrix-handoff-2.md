# Handoff 2: Plan 00209 M1, bus/matrix and bus/matrix/fakehs

Worktree `untracked/worktrees/bus-v2-m1-matrix`, branch `feat/bus-v2-m1-matrix`. Work only there.
The brief is unchanged from handoff 1 (spec first, test first, 90% coverage, `-race`, no
`//nolint`, no discarded errors, no wall clock in tests, the Opus review to MERGE in
`subagent-reports/261021-m1-matrix-review.md`, the report in `subagent-reports/261021-m1-matrix.md`,
a final message under 1500 characters). Read `SPEC-matrix.md`; do not re-read DESIGN.md or the
research files whole.

## Done

- The branch is merged with F44 at `f460abb7` (merge commit `32e71c95`; the only conflict was
  `CLAUDE/Plan/README.md`, both entries kept).
- `src-go/internal/bus/matrix/fakehs` is committed (`555ca390`) and green: `-race -shuffle=on`,
  coverage 92.1%, golangci clean, the 73-exchange replay matches. Red evidence: the tests did not
  compile (`undefined: Server`) before the source existed.
- `.golangci.yml`: a tagliatelle `case.overrides` entry sets `json: snake` for
  `internal/bus/matrix` and `internal/bus/matrix/fakehs` (the way to satisfy tagliatelle without
  an exclusion; the client's json tags will be checked as snake_case), and one gosec exclusion
  (`G401|G505`) for `internal/bus/matrix/(admin|fakehs/admin|fakehs/replay_test)\.go`, the
  HMAC-SHA1 registration MAC. The client's MAC must therefore live in `internal/bus/matrix/admin.go`.

## The fakehs API for the client tests

- `New(Options{ServerName, Now, Quirks})`, `TuwunelQuirks()`, `SetQuirks`.
- `AddUser(localpart, password, admin) string`, `MintToken(uid) (string, error)`,
  `RegistrationKey()`.
- `Start() error`, `URL() string`, `Stop() error`, `Restart() error`, `Close() error` (HTTP on
  127.0.0.1; Restart keeps the address and the state, drops open connections, empties the
  login-failure counts).
- `Requests() []Request{Method, Path (as on the wire), Query, Header}`; no body is kept.
- `Inject(Fault{Method, Path (substring), Status, Body, Header, Drop, DropAfter, Redirect, Times})`.
  `Redirect` answers 307 (or `Status`) with `Location`; `Drop` closes before handling;
  `DropAfter` handles, then closes (the lost-send case for M22).
- `Receipts()`, `CheckModelled()`; `Handle(ctx, method, path, query, body, token)` in process.
- A body over `maxHTTPBody` (16 MiB) is 413 over HTTP; `Quirks.MaxRequestBytes` gives 413
  `M_TOO_LARGE` at any size.

## Left to do

1. `src-go/internal/bus/matrix`: the tests first, then the code, for SPEC M1-M36 (`Client`,
   `NewMember`, `NewAdmin`, `Error` with kinds, sentinels and `ExitCode`, `Clock`, the 429 rule,
   the send retry with one transaction ID, `Sync` with `RoomFilter`, the member and admin
   endpoints, `quote(safe="")` path segments). Run them against fakehs over loopback HTTP. The
   TDD hook needs each file's `_test.go` before the file.
2. The `tuwunel` build-tagged test (M44), skipping without `FAKEHS_TUWUNEL_BIN`.
3. The report with red and green evidence, then the Opus adversarial review loop to MERGE.

## Open: the govulncheck finding blocks qa-changed

qa-changed fails only on govulncheck: GO-2026-6809 (crypto/x509 Verify, fixed in go1.26.10),
reached through net/http in `fakehs/http.go:62`. The container's Go is go1.26.9 (Fedora's
golang). Every other go-qa stage passes. Any HTTP client or server reaches it, so the matrix
client will too. It is a toolchain bump (IaC: the golang package the plays install), the lead's
or the owner's call. `555ca390` says so in its body.

## Working notes

- The lint hook runs golangci-lint over the whole package on every Go Edit/Write and prints every
  finding, about 2-3k tokens each time. Write a file whole once, then fix lint in few edits, one at
  a time (parallel edits each print the full list). `golangci-lint fmt <pkg>` formats with no hook.
- strict lint traps met: `errcheck` checks type assertions (use a `text(t, v)` helper in tests);
  `containedctx` (the fake's request keeps `done <-chan struct{}`, not the context); `funcorder`
  (exported methods first); `goconst` at 3 uses; `modernize` wants `new(expr)` and `maps.Copy`;
  go-qa's `gqaci_map_range_order` flags appending from a map range without a sort.
- Commit messages go through `git commit -F` with the file under this worktree's
  `untracked/scratch/` (writes to the main checkout are refused).
