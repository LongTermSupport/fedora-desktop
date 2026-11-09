# Task 8.8, slice S8 (seat key and remote take): finished

Branch `spike/v2-cross-network`, worktree `cross-network`. Code commit `e6c3353d4`. Not pushed.
Predecessor: `261103-crossnet-seatkey-handoff.md` (the half-wired state, committed as `d91e6cced`).

## What changed (6 files under `src-go/internal/teamchat/seat/`)

Production, the four edits the handoff named:

1. `place.go`: `Claim.Fill(ctx, is)` uses its context. The checks that come before any write moved
   into `checkIssued` (kind, team, encode, handle, then `pinOf`, then `reachNew` only when the
   seat's pool directory does not exist). They run before `c.end()`, so a refusal leaves the claim
   abandonable and empty. After `swapIn` succeeds, `p.write()` records the team's homeserver.
2. `take.go`: `checkState` takes `StateNoRoster` like `StateActive`. `Take` calls new `pinTaken`
   (pin check, and a write for a seat with no record) right after `poolRecord`, before `held/`.
   `mint` returns `unreachable(b, err)` when `errors.Is(err, matrix.ErrUnreachable)`, placed after
   the `ErrReissue` branch. `held/` is left as for any other failure (see finding 1).
3. `credential.go`: `Mint` treats a first login refused with `matrix.ErrAuthRefused` (401) as
   `ErrReissue`, through new `refusedLogin` (the old `M_FORBIDDEN` test is its second leg).

Tests corrected (three wrong once the code ran; the handoff said existing tests change only for
signatures, these are the exceptions):

- `reach_test.go` `TestOnlyACreatedSeatReachesItsHomeserver`: the reissue placed `otherPassword`
  but the fake account keeps `testPassword`, so the later take was a refused login. It places
  `testPassword` again.
- `homeserver_test.go` `TestTheFirstTakeRecordsAPoolSeatThatHasNoRecord`: the follow-up placement
  claimed a seat the take still holds (`ErrHeld` from `Claim`). The session is released first.
- `holdings_test.go` `TestHoldings`: wrote `{}` as `checkout.json`, which a take now reads and
  refuses (`no v`). It writes a real record (`writeRecord(fmt.Sprintf(recordFmt, ...))`).
  `TestHoldingsOfNone` keeps `{}` on purpose: `Holdings` never decodes the file.

## Red, then green

Red at the start (before my edits; 19 tests): `TestTheFirstPlacementRecordsTheTeamsHomeserver`,
`TestAnotherURLForTheTeamIsRefusedNamingBoth`, `TestAnotherServerNameForTheTeamIsRefusedNamingBoth`,
`TestTheSameHomeserverSpelledAnotherWayIsTheSame`, `TestEachTeamAndEachCheckoutKeepsItsOwnHomeserver`,
`TestTheFirstTakeRecordsAPoolSeatThatHasNoRecord`, `TestATakeRefusesASeatJSONThatDisagreesWithTheRecord`,
`TestACorruptCheckoutRecordIsRefused`, `TestTheRecordAndItsFreshCopyAreNotPoolSeats`,
`TestOnlyACreatedSeatReachesItsHomeserver`, `TestAProbeThatFailsRefusesTheCreation`,
`TestPlacingASeatForAnUnreachableURLFailsFastNamingIt`,
`TestATakeWhileTheHomeserverIsDownIsUnreachableAndCanBeTakenAgain`, `TestTheProbeOverTLSUsesTheBundlesCA`,
`TestASeatIsTakenOnAMachineThatHoldsOnlyTheSeatStore`, `TestATakeWithNoRosterTakesLikeAnActiveSeat`,
`TestATakeWithNoRosterFindsAParkedSeatByItsRefusedLogin`, `TestAFirstLoginAnsweredWithAnAuthRefusalIsAReissue`,
`TestATakeWithNoRosterFindsARetiredSeatByItsRefusedLogin`. After the four edits three more went red
(the test errors above); after correcting them, all green.

Green, from `src-go`:

- `go build ./...`, `go vet ./internal/teamchat/...` in the default, `-tags busfailpoints` and
  `-tags tuwunel` builds, and the same three with `-vettool` = the `qa` project-rules driver: clean.
- `golangci-lint run` over `./internal/teamchat/seat/... ./internal/teamchat/sweep/...`, default,
  `--build-tags busfailpoints`, `--build-tags tuwunel`: 0 issues each. `gofmt -l`: nothing. The
  `exhaustive` finding at `take.go` and the unused declarations in `homeserver.go` and `reach.go`
  are gone.
- `go test -race -shuffle=on -count=3 -timeout=300s ./internal/teamchat/seat/... ./internal/teamchat/sweep/...`
  ok (seat 20 s, sweep 1.4 s). `seat` also ok under `-tags tuwunel` and `-tags busfailpoints`
  (sweep ok under `busfailpoints`), `-count=1`.
- Importing packages, `-race -shuffle=on -count=1`: `member/...` and `matrix/...` ok (incl.
  `member/wiring`, 204 s). `admin` and `admin/wire` fail, see finding 2.

## Mutation checks (each applied to the green code, run, then restored)

| Mutation                                                 | Went red                                                                                                                                                                             |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| no `reachNew` on the creation path (`!found && false`)   | `TestOnlyACreatedSeatReachesItsHomeserver`, `TestAProbeThatFailsRefusesTheCreation`, `TestPlacingASeatForAnUnreachableURLFailsFastNamingIt`, `TestTheProbeOverTLSUsesTheBundlesCA`   |
| the reissue path probes too (`!found \|\| found`)        | `TestReissuingASeatDoesNotNeedTheHomeserver`, `TestOnlyACreatedSeatReachesItsHomeserver`                                                                                             |
| `paths.ServerUnit` named in `reach.go`                   | `TestASeatNeedsNoHomeserverUnit` (`reach.go:50:9 names paths.ServerUnit`)                                                                                                            |
| `pinTaken` does nothing                                  | `TestTheFirstTakeRecordsAPoolSeatThatHasNoRecord`, `TestATakeRefusesASeatJSONThatDisagreesWithTheRecord`, `TestACorruptCheckoutRecordIsRefused`                                      |
| `StateNoRoster` refused as `ErrReturn`                   | `TestASeatIsTakenOnAMachineThatHoldsOnlyTheSeatStore`, `TestATakeWithNoRosterTakesLikeAnActiveSeat`, `...FindsAParkedSeatByItsRefusedLogin`, `...FindsARetiredSeatByItsRefusedLogin` |
| the unreachable branch in `mint` returns `err` unwrapped | `TestATakeWhileTheHomeserverIsDownIsUnreachableAndCanBeTakenAgain`                                                                                                                   |
| `refusedLogin` without the `ErrAuthRefused` leg          | `TestAFirstLoginAnsweredWithAnAuthRefusalIsAReissue`                                                                                                                                 |
| `Fill` does not call `p.write()`                         | `TestTheFirstPlacementRecordsTheTeamsHomeserver` and five more (record and take tests)                                                                                               |

`git diff` after the last restore matched the committed code.

## Findings and what I left

1. **Mint-failure semantics, as the handoff asked to be reported.** The retry in
   `TestATakeWhileTheHomeserverIsDownIsUnreachableAndCanBeTakenAgain` works: the failed mint leaves
   `held/` naming session `s1`, and the second take clears it (`clearHeld`: a session that is over
   is released first). But I tried the same two takes with `sessions.set("s1", true)` (the session
   record restorable at the first failure, as a launcher that registers its record before taking
   would leave it) in a throwaway test, now deleted: the second take is `ErrHeld: by session s1`.
   That is the existing "any other failure leaves held/" rule meeting X10's "the launcher takes
   again". I did not change it (a refused login removes `held/`; making an unreachable login do the
   same is a one-line change in `mint`, but it changes the rule the file documents, so it is the
   owner's call). Question 1.
2. **Outside this slice, red on the branch:** `go test ./internal/teamchat/admin/...` fails
   `TestBackupAll`, `TestBackupAllGoesOnPastADownTeam` (`admin`) and
   `TestRestoreEngineReadsTheTarAndTheKey` (`admin/wire`) with `declaration: team.json.v is version 1, and only version 2 is read: it holds networks where version 1 held listen and allow_from`.
   The n2 backup and restore tests write `team.json` fixtures at `v` 1 and the cross-network
   declaration (X1 to X6) reads `v` 2 only. No seat code is on that path; I did not touch it. A
   fixture update in the slice that owns the declaration, or in `admin`, clears it.
3. No journal entry written: a hand-written entry is blocked and the dispatch did not ask for one;
   the S8 body the predecessor drafted is `untracked/scratch/s8-journal-body.md` (ignored), for the
   coordinator to stamp with `mkplan.bash --journal`. PLAN.md Task 8.8 status is the coordinator's.
4. No full-suite QA run (told not to).

## Owner questions

1. Should a take whose login finds the homeserver down (`ErrUnreachable`) remove `held/` as a
   refused login does, so a retry for a restorable session is not `ErrHeld` (finding 1)? Or does the
   launcher end the session record before it takes again?
2. The handoff's four questions stand unchanged: where `checkout.json` keeps the checkout's real
   path (DESIGN-v2-C says it does; S8 made it the per-team pin); a way to move a team to a new URL
   (`seat forget-team`); the launcher's behaviour with the homeserver down; and whether a no-roster
   take that gets `M_USER_DEACTIVATED` should quarantine the seat as retired (today it fails naming
   the user and leaves the pool record, pinned by `TestATakeWithNoRosterFindsARetiredSeatByItsRefusedLogin`).
3. The creation probe runs under the allocation lock, bounded by the client's 15 s per-try limit
   (the lock waits 30 s). Acceptable?
