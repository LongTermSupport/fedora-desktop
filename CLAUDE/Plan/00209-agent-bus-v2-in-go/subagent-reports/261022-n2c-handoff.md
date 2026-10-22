# N2 slice n2c: handoff 2

The second builder stopped at its context budget. Worktree
`/workspace/untracked/worktrees/bus-v2-n2c`, branch `feat/bus-v2-n2c`. Read the brief
(`untracked/scratch/n1-common.txt` in the main checkout, task name "n2c"), then
[SPEC-n2c.md](../SPEC-n2c.md), the slice's contract.

## Done and committed (tip cdfa1d27), each step green under -race and golangci-lint

The package `src-go/internal/teamchat/seat`, one file pair per step:

| Step                   | Files                                      | Spec                         |
| ---------------------- | ------------------------------------------ | ---------------------------- |
| records, issued bundle | `records.go`                               | SR1-SR3                      |
| directory-fd layer     | `fs.go`                                    | SW1-SW3                      |
| store, allocation lock | `seat.go` (package doc, Config), `lock.go` | SL1, n2c-D6                  |
| credential seam        | `credential.go` (`Credential`, `Password`) | SC1-SC3, n2c-D7              |
| take, L3 probe         | `take.go`, `lockprobe.go`                  | ST1-ST7, SX1's probe, n2c-D3 |
| release, notice inputs | `release.go` (`Release`, `Announce`)       | SX1-SX6, SD1-SD4, D4, D5     |
| placement              | `place.go` (`Claim`, `Fill`, leftovers)    | SP1, SP2                     |

Test helpers: `helpers_test.go` (ids, bundles, file helpers), `world_test.go` (fake clock, a
fakehs `homeserver` with `alive`/`stale`, fake `sessions` and `announcer`, the `world` with
`seed`, `take`, `release`, `keep` for a keeper's seat lock), plus `place_test.go`'s
`issued`/`place` and `take_test.go`'s `quarantined`. The lint config has the package's
`tagliatelle` snake override and a gosec exclusion for its tests.

Coverage at the tip: **89.6%, under the 90% floor**. The gaps are error paths: `fs.go`
(`replace`, `readRegular`, `checkEmptyOwned`, `closeQuietly`), `take.go` (`hold`,
`copyToSession`, `moveToQuarantine`, `newClaim`), `place.go`, `lockprobe.go` `probeOpen`.
Raise it first, in its own commit.

qa-changed is red only on govulncheck GO-2026-6809 (Task 1.6), the accepted red; it does not
check coverage, so measure with `go test -race -cover ./internal/teamchat/seat`.

## Left, in order, one commit each

1. Coverage to at least 90%.
2. `guest.go` (SP3, SP4): `PlaceGuest(layout paths.Layout, is Issued) error` (as the agent user:
   `Layout.SeatClaim(team, claim)` 0700 with `token` then `seat.json` 0600, swapped in as
   `Layout.Seat(team).Dir()`, the old directory removed by `os.RemoveAll`; a token bundle only),
   and `KeepGuestToken(base string, team, seat, Secret) error` (`paths.GuestTokenCopy` under
   `base`, written by `dir.replace`, its directory 0700). `openDir(root, true)` and the `dir`
   methods serve both; the guest root is `Layout.State()`.
3. `tuwunel_test.go` (`//go:build tuwunel`, ST6), modelled on `rooms/tuwunel_test.go` through
   `matrix/tuwunelhs`, server name `paths.ServerName(team)`: register the admin, create the seat's
   account, `place` it, keep a stale `LoginDevice` token, take, show the stale token dead,
   release, show the take's token dead.
4. The review loop (Opus, at most 3 rounds), `subagent-reports/261022-n2c-review.md` per round,
   the report `subagent-reports/261022-n2c.md`, Task 2.4's progress line in PLAN.md.

## Decisions taken while building (for the report)

- The L3 probe and every path open from the store root's fd with
  `RESOLVE_BENEATH|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS` (SW2), not from the fd of
  `sessions/<session>/` as C 3.7.3 words it: the same refusal of a link on every component.
- A free seat lock whose record is over 4 KiB names no run (SD1), as an undecodable one does.
- Release step 1 stops waiting when the lock is free or the session is not restorable; a lock
  still held by a session that is over gives no run id, so no notice is asked.
- A `held/` with no complete `held.json` met by a release (not only a take) goes to quarantine
  under the lock and the release is done. A release or `Announce` for a seat `held/` names
  another session for is `ErrHeld`.
- An `Outcome` neither posted nor with a one-line `why` is refused (`ErrInvalid`), not recorded.
- Release step 4 retires with the pool's `seat.json`, since `held/token` is written before
  `held/seat.json` and a take may die between them.
- `Store.Close` releases the root fd (not in the drafts).

Design-text fixes still to propose in the report: B's header for host `cc` against C 3.7.1
(n2c-D3), B 2.11's host `since` against `held.json`'s `since_ms`, `<seat>` collisions across
member types (n2c-D1), the release resume's run id (n2c-D5), and the probe's root-relative
`openat2` above.
