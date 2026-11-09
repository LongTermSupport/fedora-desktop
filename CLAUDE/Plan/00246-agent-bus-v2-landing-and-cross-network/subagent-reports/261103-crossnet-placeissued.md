# Task 8.8, slice S9: `teamchat seat place-issued` (X21 to X23)

Written by the lead after two agents stopped (the first at its context budget, the second on the
account's weekly usage limit, which no further agent can run under). The first agent's design is
`261103-crossnet-placeissued-handoff.md`; this report is what was built from it and what was run.

## Built

- `paths.CheckoutKeyOf` (first S9 agent, `68c6714d3`): the pool key of a real path.
- `member/placeissued` (new package):
  - `Read`, with `FailureError` and `Report` (S9a agent): strict JSON within a cap, either the
    issued document or the answer of `teamchat-admin member add|return|reissue` around it, then
    `seat.DecodeIssued`.
  - `Place` (lead): `Read`, `seat.Open`, the allocation lock, `QuarantineLeftovers`, `Claim`,
    `Fill`, `Abandon` when `Fill` refused before writing, the lock released and the store closed
    however it ends. `OS` is the real machine (wall clock over `clock.System`, `seat.Password`,
    `seat.VersionsReach`). The store's session and announcer seams answer an error if asked:
    place-issued never takes or releases a seat.
  - Failure mapping (`failure`): `seat.ErrInvalid` 78 bundle; `seat.ErrHomeserverClash` 78 bundle
    (the cause line names both homeservers); `seat.ErrUnreachable` 20 naming the URL;
    `seat.ErrBusy` and `seat.ErrHeld` 75; anything else the store reports 78 store.
- `member/cli`: the verb `seat place-issued --checkout DIR` (table row, `runs` entry, `place.go`).
  It reads `HOME`, `XDG_RUNTIME_DIR` (absolute, else `home` / `runtime` errors) and
  `XDG_STATE_HOME`; resolves `--checkout` with `filepath.EvalSymlinks`, keys the pool on the
  result with `paths.CheckoutKeyOf`; stdout is one line, `placed seat <seat> of team <team>`.
- `admin`: `TestTheAnswerOfAMemberVerbIsWhatPlaceIssuedReads`, the contract test `read.go` names:
  the real `member add` answer, built with the real `SeatBundles`, for a password and a token
  seat, is read by `placeissued.Read`.
- Docs: the verb's row in `DESIGN-v2-B-member.md`.
- No ssh and no `execx` code: X21 (the owner pipes, as themself) overrides the code map's S9 line
  "the push command through execx".

## Red and green

The S9a agent's tests for `Place` were committed red (they named `Place`, `Deps`, `Placed`). Writing
`Place` made all but one green; the one was a fixture fault, not the code: the lock-held test holds
the allocation lock itself and its helper then asserted the lock free. The world now releases its
own hold first. The CLI tests were written first (`undefined: placeDeps`, then green on the first
run). The contract test was red first because the test team's derived URL (`http://` on a
non-loopback address) is refused by the real bundle maker; it now applies a team that declares an
https `url`.

## Mutation checks (each broken once, the named test red, then restored)

| Guard broken                                    | Red                                                                                                                                                                             |
| ----------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| leftover sweep skipped                          | `TestPlaceMovesACrashImageToQuarantine`, `TestPlaceReportsAFailureAfterTheSwapAndTheNextRunMendsIt`                                                                             |
| claim never abandoned after a refusal           | `TestPlaceRefusesATokenForThePasswordStoreNamingTheKinds`, `...ANewSeatWhoseHomeserverDoesNotAnswer`, `...ATeamOnAnotherURLNamingBoth`, `...ATeamOnAnotherServerNameNamingBoth` |
| `ErrUnreachable` reported as a store fault      | `TestPlaceRefusesANewSeatWhoseHomeserverDoesNotAnswer`                                                                                                                          |
| pool keyed on the unresolved `--checkout`       | `TestPlaceIssuedKeysThePoolOnTheResolvedCheckout`                                                                                                                               |
| a relative `XDG_RUNTIME_DIR` accepted           | `TestPlaceIssuedNeedsAnAbsoluteHomeAndRuntimeDirectory`                                                                                                                         |
| `answerVersion` drifted from the admin's answer | `TestTheAnswerOfAMemberVerbIsWhatPlaceIssuedReads`                                                                                                                              |

## What was run

From `src-go`, output read from files:

- `go build ./...` ok. `go vet` and `go test -race -shuffle=on -count=2` over `placeissued`, `cli`,
  `admin`, `seat`, `paths`: all ok.
- `golangci-lint run ./internal/teamchat/...` in the default, `busfailpoints` and `tuwunel`
  builds: 0 issues each.
- The project rules driver over `./internal/teamchat/...` in the same three builds: it found one
  instance in the S9a agent's test helper (`holding` returned map-ordered paths unsorted); sorted,
  then clean in all three.
- `placeissued` and `cli` as compiled test binaries run as uid 65534: PASS (CI runs the Go tests
  unprivileged, and the first CI run failed on tests that only pass as root).
- `go test -tags teamchatrelease ./internal/teamchat/member/cli/`: red, for exactly the six verbs
  slices n3e and n3f still owe (`hook`, `status-line`, `report`, `ask`, `thread`, `questions`);
  `seat place-issued` is not among them.

## Not verified

`admin` and `seat` as uid 65534; a real homeserver for the reach check (a fake answers); a real ssh
pipe (X30's second-machine run is a host or VM-lab check); `qa-changed` and `qa-all` on this
branch (the coordinator's).

## Findings and owner questions

1. **The credential kind is the store's, not the document's (X23).** `Claim.checkIssued` refuses
   a kind other than the store's, and the only production credential is `seat.Password`; a token
   pool credential is DESIGN-v2 7.1 (d1) or (d2), which the owner has not chosen. So place-issued
   refuses a token document with the store's own message, naming both kinds. The admin already
   issues either kind. The token placement path is tested with an injected credential. Options:
   (a) password only until 7.1 is decided; (b) decide (d1) or (d2), build the credential, and let
   the launcher and place-issued open the store from one shared configuration.
2. **X21's pipe produces the answer of `member add`, X22 says "issued document".** `Read` takes
   both. Options: both (built), the answer only, or the bare document only (then the owner must
   extract `.bundle` first).
3. **`--checkout DIR` is required.** The pool is per checkout, and the launcher must key it with
   `paths.CheckoutKeyOf` of the same resolved path. Where `checkout.json` keeps the checkout's real
   path is still S8's open question.
4. **A team that declares `networks` and no `url` applies cleanly, and `member add` then fails at
   the bundle step.** The admin test team (networks, no `url`) derives `http://` on a non-loopback
   address; the real bundle maker refuses it (X12), with `seat: invalid seat.json: base_url and tls_ca are not a member URL and the CA certificate pinned to it`. Observed in the contract
   test; I did not look for the check at apply time. Option: refuse such a team at `team approve`
   or `team apply`, naming `url`.
5. **Re-placing the same document is accepted, not refused**: the second run replaces the record
   with an identical one and quarantines the first, credential unlinked (tested).
6. **Observed by the first S9 agent, not re-checked:** the store takes the credential file name
   from its own kind, so a record whose credential file is the other kind keeps that file when
   it goes to quarantine. Not reachable through this verb while the store is password-only.
