# PR 128 review, the code items: tlsverb read error, tlsterm log race, S6, S8

Branch `fix/crossnet-review-rest`. Nothing was pushed or merged. One commit per item, each with
its qa-changed result in the message.

| Item                             | Commit      |
| -------------------------------- | ----------- |
| tlsverb dropped read error (nit) | `fbd8af113` |
| tlsterm.logf data race (nit)     | `25e1ef2ef` |
| S6 idle deadline                 | `0ba2ec9ca` |
| S8 CA key root's alone, 0600     | `309255e01` |

## 1. tlsverb: the dropped read error

`credential()` turned every failed read of a credential file into "missing or empty", and the
read's error was lost. Now a file that is there but cannot be read gets its own refusal, "cannot
be read" (exit 78, runtime). The read's reason goes to stderr first: the path and the system
error, never the file's bytes.

- Red: `TestV3aAnUnreadableCredentialIsRefusedWithItsReason` (a directory in the key's place,
  and a key over the limit) failed with
  `the refusal does not carry the read's reason "not a regular file" for leaf.key: {..."message":"the unit's file leaf.key for team ops is missing or empty; tell a human"}`.
- Green: the test passes. V3b (stderr refuses writes, the refusal still stands) and V2a (Main
  refuses when no credentials directory is set) were also added. go-qa's coverage floor had
  failed the first qa-changed run (87.0%, `Main` reached by no test). The package is now at 91.3%.

## 2. tlsterm.logf: the data race

`s.cfg.Log` was read outside `logMu` for the nil check, and written to nil under it. Now a
`logFailed` field, read and written under `logMu` only, records a failed log. The config is
never written after Start.

- Red under `-race`: `TestT4AFailingLogIsDroppedWithoutARace` failed with
  `WARNING: DATA RACE  Read at ... logf() tlsterm.go:336 / Previous write ... logf() tlsterm.go:342`
  and `race detected during execution of test`.
- Green under `-race`, run 3 and then 10 times.
- Coverage: `TestT6RunEndsWithItsContext` and `TestT6AStopOnItsOwnIsTheFailure` cover `Run`,
  `Err` and `stop`. No test reached them before, so a diff touching tlsterm would have failed
  the floor.
- Defence Before Fix class: a logger, shared writer or callback field read and written from
  several goroutines without one lock.
  - Sweep over `internal/teamchat`: I read every `logf`, and grepped for field writes on log,
    writer and callback names and for every `Mu` field.
  - This was the only instance. `ends`, `engine`, `link` and the link server hold their log
    under one lock throughout. The keeper logs from its loop alone (its reader goroutine posts
    through `k.send`). guestbus's `refused` and `out` fields are each locked.
- Detector: the race detector catches the class when a test exercises the path. I added no
  static rule. One would have to prove that a field is written after construction and also
  reached from several goroutines, which go/analysis cannot see reliably. I don't think a rule
  is worth it, but that is for the coordinator to decide.

## 3. S6: the terminator's idle deadline

- `tlsterm.IdleLimit = 2 * time.Minute`, checked against the client:
  - `maxSyncTimeout` is 30 s (`matrix/sync.go:12`).
  - `IdleConnTimeout` is `time.Minute` (`matrix/transport.go:62` and `:165`).
- Each relay has an `idleTimer` on the config's Clock. Each read or write that moved a byte
  disarms the previous timer and arms a new one, through `touchReader` and `touchWriter` inside
  `io.Copy`. The connections are not wrapped, so `CloseWrite` still passes the half-close on.
- A firing that a later touch overtook carries an old generation and does nothing. When the
  timer fires it closes both ends, the copies fail, the relay ends and its slot is free.
- Red: `TestT8AnIdleRelayIsClosedAndItsSlotFreed` failed with
  `panic: test timed out after 30s` at `tlsterm_test.go:362`, its `AwaitDue(IdleLimit)`. No idle
  timer was ever armed. The 30 s was the `-timeout` of that one run.
- Green, run 10 and 20 times under `-race`:
  - idle relay closed, and with MaxConns 1 the next client is served (`leakcheck.Poll` on
    `len(srv.slots)`, no sleep)
  - an active relay kept open across five times IdleLimit less a second
  - the timer's end and generation rules
  - IdleLimit is above both client waits
- Coverage is 94.3%.
- SPEC X18 states the deadline as the contract. The member doc is unchanged: a healthy member
  sees no difference.
- Defence Before Fix class: a connection or resource limit with no release path under
  idleness. I found no detector that can express it, so it was fixed conventionally.
- Not mine, and left alone: `TestT5MaxConnsBoundsTheRelays` has an unbounded `for {}` retry
  loop (lines 307 to 319). Worth an issue.

## 4. S8: the CA key must be root's alone, mode 0600

- New `state.ReadPrivateFile(path, limit, uid)` (ST4): `ReadOwnedFile`'s rules plus mode 0600
  exactly. It is checked by `readOpen` on the opened file, so there is no check-then-open race.
  Refusals are typed: `*state.OwnerError` (same text as before) and `*state.ModeError`.
- `loadCA`, `CAPEM`, `CABackupFiles`, `RestoreCA` and `CheckRestoreCA` take an owner uid.
- `host.Deps` gains `StateOwnerUID`. `admin/wire` passes `stateOwnerUID` (0) to `CAPEM` and to
  `host.Deps`. backup and restore pass their own `StateOwnerUID`. The test worlds pass the
  test's own uid, so nothing needs root.
- The refusal names the file, what was found, and the fix:
  - `chmod 600 <file>`
  - `chown root <file>`, or the uid when the owner is not 0
- Nothing is repaired. The certificate is not judged.
- Red: `TestTheCAKeyMustBeRootsAlone` was run with the signatures plumbed but the old read.
  Every reader answered `<nil>` for 0644, 0640 and 0620 and for another owner, for example
  `the apply: <nil>, want a drift refusal at tls`. The state tests' red was a build failure
  (`undefined: ReadPrivateFile`).
- Green: the test passes, and holds that nothing is repaired or changed.
  `TestTheCAKeyFixNamesRoot` covers owner 0.
- What the refusal does:
  - apply stops at tls as drift (70)
  - issuing a bundle or an LXC reach fact fails
  - a backup fails at the team CA
  - a restore over a team with a bad key is refused at validation before any change, and the
    engine's write refuses it too
  - a team with no CA is unaffected
- Sweep, same class: the apply's read of the leaf key in `leafVerdict` was a second instance.
  It now uses `ReadPrivateFile` too. The leaf is derived, so a loose leaf key is made again with
  a new key (never a chmod) and the apply says why. Red: `TestALooseLeafKeyIsMadeAgain` had no
  line, kept the mode and kept the key. Green now. The terminator reads systemd's credential
  copy, so it stays `ReadFile`.
- I added no detector. A rule would need to know which reads are of keys; the coordinator
  decides.
- SPEC X13 and `docs/agent-bus-cross-network.md` say it.

### What S14 (`feat/crossnet-s14-int`) will need when it takes this

- `host/rotate.go:83`: `loadCA(r.Root, team)` becomes `loadCA(r.Root, team, owner)`.
  `CARotation` needs an owner field (say `StateOwnerUID uint32`), set to 0 by its wiring and to
  the test's uid by its tests.
- `host/leaf.go:248`: the same change in `LeafRenewer`'s `sign`, with a field on `LeafRenewer`.
- If S14 reads the leaf key anywhere else (a renewal verdict), it should use
  `state.ReadPrivateFile` and remake the leaf as `leafVerdict` now does.
- Every call to `CAPEM`, `CABackupFiles`, `RestoreCA` or `CheckRestoreCA` that S14 adds or keeps
  takes the owner, and any `host.Deps` its tests build needs `StateOwnerUID`.
- A rotation over a team whose CA key is loose is refused with the chmod or chown fix. That is
  the intended behaviour.

## Checks, whole tree, before the last commit

All exit 0, from `src-go`:

- `go build ./...`
- `go vet ./...`
- `golangci-lint run ./internal/teamchat/...`
- `golangci-lint run --build-tags busfailpoints ./internal/teamchat/...`
- `go test -race -count=1 ./internal/teamchat/... ./cmd/teamchat/...`
- `gofmt -l internal/teamchat cmd/teamchat`, output empty

qa-changed passed before each of the four commits.

## Not proven

- Every run was as root in the container, so I did not see a non-root run. The owner checks
  run through the injected uid, and the tests pass the test's own uid, so they should hold in
  CI.
- No real host run of the idle deadline against a live homeserver and member.
- qa-all was not run (by instruction).
