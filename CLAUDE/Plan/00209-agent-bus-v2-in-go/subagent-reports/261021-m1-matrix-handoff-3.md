# Handoff 3: Plan 00209 M1, bus/matrix (client built; M44, report and review left)

Worktree `untracked/worktrees/bus-v2-m1-matrix`, branch `feat/bus-v2-m1-matrix`. Work only there.
The brief and the owner direction are unchanged (clean, general client; fakehs is the ground
truth; no `//nolint`, no discarded errors, no wall clock in tests; never push, never qa-all).

## Done

- `04a81650`: `src-go/internal/bus/matrix` (`client.go` transport, errors, base URLs, segments;
  `member.go` member endpoints; `sync.go`; `admin.go`), with `client_test.go`, `member_test.go`,
  `sync_test.go`, `admin_test.go` against fakehs over loopback HTTP. Green: `go test -race -shuffle=on` (98.7% coverage), golangci-lint 0 issues. Red: the tests did not compile
  (`undefined: Client`) before the source existed. Departures MB8-MB13 are in SPEC-matrix.md.
- qa-changed fails only on govulncheck GO-2026-6809 (go1.26.9; fixed in go1.26.10), as before;
  the commit body says so.

## API in one breath

`NewMember(Config)`, `NewAdmin(Config)` (`Admin` embeds `*Client`), `WithToken`, `Close`;
`Config{BaseURL, PlainHTTPHosts, Token, Clock, UserAgent}`; `Error{Method, Path, Status, Errcode, Detail}` with `Is` (kinds `ErrAuthRefused/Forbidden/NotFound/RateLimited/Unreachable/Refused/ Invalid`), `Transient`, `ExitCode`; `Token` (`Reveal`); `Session`; member: `Versions`, `Whoami`,
`Login`, `Send` (+`NewTxnID`), `PutState`, `GetState`, `GetStateEvent`, `RoomState`, `GetEvent`,
`Join`, `Leave`, `ReadReceipt`, `Invite`, `Kick`, `Membership`, `JoinedRooms`, `CreateRoom`,
`Messages`, `Sync` (`SyncRequest`, `RoomFilter`); admin: `Register`, `RegistrationMAC`,
`Account` (acct, found, err), `CreateAccount`, `ResetPassword`, `SetDeactivated`, `SetLocked`,
`MintToken`, `ServerAdmins`, `Devices`. In-package test knobs: `c.limit`, `syncCall`.

## Left to do

1. M44: `src-go/internal/bus/matrix/tuwunel_test.go` with `//go:build tuwunel`, skipping with its
   reason unless `FAKEHS_TUWUNEL_BIN` names a binary: start it (config, port, registration
   secret), then registration, the admin flow, a room, a send with a reused transaction ID,
   sync, messages and a receipt through this client. Without the tag it is not built, so the
   default gate does not see it; check `go vet -tags tuwunel ./internal/bus/matrix/` and
   `golangci-lint run --build-tags tuwunel` stay clean. How the installer configures Tuwunel
   (config keys, the registration token) is in the H4 fixtures' README or the v1 installer;
   read only the part needed.
2. The report `subagent-reports/261021-m1-matrix.md`: red and green evidence above, the
   departures, the govulncheck finding.
3. The adversarial review (Opus, limited to this worktree) over
   `git diff f460abb7...HEAD -- src-go .golangci.yml`: token leaks, redirect following,
   unbounded reads, retry and txn bugs, context and goroutine leaks, fakehs drift from the
   recorded exchanges, the `.golangci.yml` entries' scope. Verdict in
   `subagent-reports/261021-m1-matrix-review.md`; loop until MERGE, at most 3 rounds.

## Working notes

- The lint hook prints every finding of the package on each Go Edit/Write; write a file whole,
  then run `golangci-lint run ./internal/bus/matrix/...` and `golangci-lint fmt` yourself.
- The error-hiding hook refuses any `x, _ :=` in a Write; tests use fixture structs (`member`,
  `hostAdmin`, `roomFixture`) to avoid it. `wantErr` returns an `Error` value (not an error).
- `sed` and `| head` are refused; commit with `git commit -F untracked/scratch/<file>` inside
  the worktree.
