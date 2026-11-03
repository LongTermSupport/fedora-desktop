# Task 8.8, slice S8 (seat key and remote take): handoff

Stopped at the context budget. One piece is done and committed (`matrix.CanonicalMemberBase`).
The seat work is written test first, seen red, and **half wired in the worktree, not committed**:
nothing in it is green yet, so by the rule "commit what is green" it is left as files. Pre-existing
seat tests pass; the 19 new ones fail until the four edits under "Left" are made.

## Done and committed

- `matrix.CanonicalMemberBase(raw)` (`matrix/client.go`) and `matrix/canonical_test.go`: X9's
  normal form (`memberBase`'s, judged with the CA the scheme needs). Red first (stub returning
  `raw`: `"HTTPS://example.org"` came back unchanged, `http://example.org` was not refused, 17
  lines); green now. `go test -race -shuffle=on ./internal/teamchat/matrix/`, golangci-lint plain,
  `busfailpoints`, `tuwunel`, vet in all three, gofmt, project-rules driver: all clean.

## Done, uncommitted in the worktree (compiles; `go build ./...`, vet and `sweep` tests pass)

- Tests, all written first: `seat/homeserver_test.go` (X8, X9: record, clash naming both, spelling,
  per team and per checkout, first take records, take refuses a disagreeing seat.json, corrupt
  record table, `checkout.json.new` leftover), `seat/reach_test.go` (X10: only a created seat is
  probed, probe failure, real probe against a stopped fakehs, reissue while down, take while down
  is `ErrUnreachable` and can be retried, TLS probe with the right and another CA, the probe sends
  one unauthenticated GET), `seat/remote_test.go` (X7 import and identifier scan; X11: no-roster
  take, parked seat found by its refused login, 401 login is a reissue, retired seat).
  Existing tests changed only for the new signatures: `Fill(ctx, is)` in `place_test.go` and
  `credential_test.go`, `Reach:` in `world_test.go` (plus `reopen`, `probe`), `tuwunel_test.go`,
  and `sweep/store_test.go`.
- Production, complete: `seat/homeserver.go` (`ErrHomeserverClash`, `checkout.json` v 1
  `{"v":1,"teams":{"<team>":{"server_name":…,"base_url":…}}}`, strict decode, `pinOf` judges a
  bundle, `pin.write` records it with `root.replace`), `seat/reach.go` (`ErrUnreachable`,
  `Reacher`, `VersionsReach`, `unreachable`, `Store.reachNew`), `Config.Reach` (required) and
  `Store.reacher` in `seat/seat.go`, `Holdings` skips `checkout.json` and `checkout.json.new`.
- Production, stubbed only: `StateNoRoster` is declared (`take.go`) but `checkState` does not
  handle it; `Claim.Fill(_ context.Context, is)` takes the context and ignores it.

## Left: four edits, then the checks

1. `seat/place.go` `Fill(ctx, is)`: after the existing kind, team, encode and handle checks and
   before `c.end()` (so a refusal leaves the claim abandonable): `p, err := s.pinOf(is.Seat)`; if
   `!exists(pool.Dir())` call `s.reachNew(ctx, is.Seat)`. After `swapIn` succeeds: `p.write()`.
   Put the checks in a helper; `gocognit` and `funlen` are on.
2. `seat/take.go`: `checkState` gets `case StateActive, StateNoRoster: return nil` (the
   `exhaustive` linter already flags it); `Take`, right after `poolRecord`, does
   `p, err := s.pinOf(bundle)` then `p.write()` (a refused clash makes no `held/`); `mint`, after
   the `ErrReissue` branch, returns `unreachable(b, err)` when `errors.Is(err, matrix.ErrUnreachable)`.
3. `seat/credential.go` `Mint`: a first login refused with `matrix.ErrAuthRefused` is `ErrReissue`
   too (today only `M_FORBIDDEN`), which is X11's named class.
4. Run, from `src-go`: `go test -race -shuffle=on ./internal/teamchat/seat/ ./internal/teamchat/sweep/ ./internal/teamchat/matrix/`; golangci-lint plain, `--build-tags busfailpoints`, `--build-tags tuwunel` over `./internal/teamchat/seat/ ./internal/teamchat/sweep/`; `go vet` plain and both
   tags; `go -C qa build -o <scratch>/driver ./driver` then `go vet -vettool=<scratch>/driver ./internal/teamchat/...`.
   Mutations to record: drop `reachNew` from the creation path and `TestPlacingASeatForAnUnreachableURL…`
   goes red; make the reissue path probe too and `TestReissuingASeatDoesNotNeedTheHomeserver` goes
   red; name `paths.ServerUnit` in a seat file and `TestASeatNeedsNoHomeserverUnit` goes red (it
   passes on the unchanged code on purpose: X7 says the code is already so and a test pins it).
   If `TestATakeWhileTheHomeserverIsDownIsUnreachableAndCanBeTakenAgain` fails at its second take
   (`ErrHeld` from the `held/` the failed mint leaves), that is a real finding about the existing
   mint-failure semantics: report it, do not paper over it.
   Then the report `261103-crossnet-seatkey.md` and a journal entry.

## Red evidence (the 19 failing seat tests, run before the wiring)

`checkout.json: no such file or directory` (the placement and the take record nothing),
`a bundle on another server name: <nil>`, `a probe that failed: <nil>`, `a creation for a homeserver that is down: <nil>`, `seat: unknown handle state 6` (three no-roster tests),
`a new seat asked [], want its bundle once`, `a login answered 401: … 401 M_USER_LOCKED …` (not a
reissue), `a take while the homeserver is down: … POST /login: no answer` (not `ErrUnreachable`),
`an entry key in capitals: a placement: <nil>, want ErrInvalid`. Raw files were in `untracked/scratch/`
(`s8-red-seat.txt`, `s8-red-matrix.txt`); they are not committed.

## Decisions taken (default; the owner may overturn)

- The pin is per checkout and per team id, in `pool/<checkout>/checkout.json`. The lead's
  summary of S8 says two homeservers with one team id "no longer collide"; SPEC X8 and code-map
  option (a) say a second URL for the same id is **refused naming both**. Built as the spec says.
- Written after the placement's swap-in, and by the first take for a seat that came by another
  route. A crash between the two is repaired by the next take.
- A reissue over an existing pool seat is not probed: the smallest reading of "creating".
- A URL with no port is not folded into `:443` (`memberBase` keeps the port as given).

## Owner questions

1. `DESIGN-v2-C-host.md` says `checkout.json` holds "the checkout's real path"; no code writes it
   and the store only has the key. S8 defines the file as the per-team homeserver pin. Add the path
   when the launcher exists (a v 2 of the record), or keep the pin elsewhere?
2. There is no way to move a team to a new URL on a checkout: a clash stays refused until the pin
   is removed by hand. A `seat forget-team` that refuses while any pool seat of the team exists?
3. X10, second half: with the password credential every take logs in, so a launch with the
   homeserver down cannot mint a token. `Take` now says `ErrUnreachable` naming the URL so the
   launcher can start without the seat and take again; whether it does is the launcher's.
4. X11 names `ErrAuthRefused`, but a wrong-password login on the pinned Tuwunel is
   `ErrForbidden`/`M_FORBIDDEN` (`matrix/tuwunel_test.go`, the wrong-password check), which `Mint`
   already maps to `ErrReissue`; `ErrAuthRefused` is a refused token. The 401 case is added to
   `Mint` for the spec's literal class. What a deactivated (retired) account's login answers is
   unrecorded, and `credential_test.go` pins `M_USER_DEACTIVATED` as not a reissue; left alone.
   Should a no-roster take treat it as a retired seat (and quarantine)?
5. The creation probe runs under the allocation lock, bounded by the client's 15 s per-try limit
   (the lock waits 30 s).
