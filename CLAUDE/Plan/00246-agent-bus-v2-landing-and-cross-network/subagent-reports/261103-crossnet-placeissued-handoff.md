# Task 8.8, slice S9 (`seat place-issued`): handoff at the context budget

Branch `spike/v2-cross-network`, worktree `cross-network`. The dispatch (READ FIRST list, hard rules,
VERIFY list, COMMIT and REPORT shape) is the lead's message; its report file is
`261103-crossnet-placeissued.md` in this folder, not yet written.

## Done (committed with this handoff)

`paths.CheckoutKeyOf(realPath string) (CheckoutKey, error)` in `src-go/internal/teamchat/paths/ids.go`:
the first 32 hex of the SHA-256 of an absolute, clean, non-root, control-free path (C 3.7), refusing
without quoting. Tests first: `TestCheckoutKeyOfIsTheFirst32HexOfTheSHA256OfTheRealPath` (pinned vectors from
`sha256sum`) and `TestCheckoutKeyOfRefusesWhatIsNotARealPath` in `ids_test.go`; red recorded as
`undefined: CheckoutKeyOf` (build failure), then green. `separation_test.go` `notName` lists
`CheckoutKeyOf` (it is a maker of an id from a path, not a v2 name; without it `TestEveryExportedNameIsSampled`
went red). `go test -race -shuffle=on -count=2 ./internal/teamchat/paths/` ok; `gofmt -l` and `go vet` clean.

## Left to build (nothing else is written)

Everything below was read and decided; no file for it exists yet.

### Findings that shape the design (state them in the report)

1. `seat.Open` has no production caller yet; the launcher (Tasks 4.x) is unbuilt. `Config` needs a Clock
   (`Now`, `Sleep`; `member/cli/guest.go` `clock()` fits), `Credential`, `Sessions`, `Announcer`, `Reach`,
   `StateHome`, `RuntimeDir`, `Checkout`. place-issued never takes or releases, so `Sessions` and
   `Announcer` are adapters that fail if called.
2. **The pool credential kind is the store's, not the document's.** `Claim.checkIssued` refuses
   `is.Kind != s.credential.Kind()` ("a token, not this pool's password"); `Store.credentialPath` picks the
   file from the store's kind. The only production credential is `seat.Password`. A token pool credential is
   DESIGN-v2 7.1 (d1) or (d2), the owner's choice (INT6), "the build follows the default until the owner
   chooses"; `poolToken` in `seat/credential_test.go` is a test double. So place-issued opens the store with
   `Deps.Credential` (default `seat.Password`), and a token document is refused by `checkIssued` with the
   existing message. Do not invent a production token credential. Test the token placement path with an
   injected double (`Deps.Credential`). SPEC X23 says the kind is the owner's per-seat choice; the store makes
   it per-store. Owner question, options: (a) keep password-only until 7.1 is decided; (b) decide (d1) or
   (d2) and build the credential, then the launcher and place-issued open the store from one shared
   configuration. Admin already supports both kinds for any seat kind: `admin/verbs.go` `flagArg` accepts
   `password|token`, `admin/bundles.go` `seatKind` maps both, tests `TestMemberAddThroughTheVerb` (token via
   `addArgs("1","token")`), `TestMemberVerbsThroughTheVerbs` (return with token), `bundles_test.go`
   (token). No admin change is needed.
3. **The X21 pipe does not feed `DecodeIssued`.** `teamchat-admin member add|return|reissue` prints the
   answer `{"v":1,"changed":[...],"handle":"..","notes":[..]?,"bundle":{"seat":{..},"password"|"token":".."}}`
   (`admin/verbs.go` `memberWork`, `lineArray`; `wireMajor = 1` in `admin/child.go`), and `lxc.Joined.Bundle`
   is the `bundle` value alone. `DecodeIssued` wants exactly `{seat, one credential}`. Decision: `Read` accepts
   both shapes. A top-level `bundle` key means the answer: members must be within {v (int 1), changed (array of
   strings), handle (string), notes (array of strings), bundle (object)}, then `DecodeIssued(bundle.Compact())`.
   A top-level `seat` key means the bare issued document. Anything else: "not an issued seat". Say in the
   report that SPEC X22 says "issued document" and X21's pipe produces the answer; owner question (envelope
   only, bare only, both). Add a contract test in `admin` (or placeissued): the real `memberVerb` answer
   built with `SeatBundles` (`admin/bundles_test.go` `seatBundles(t)`; `world_test.go:271` sets
   `Bundles: w`, so build a world with `Bundles: seatBundles(t)`) read by `placeissued.Read`.
4. The code map's S9 line "push command through execx" is overridden by X21: build no ssh, no execx, no
   hostname anywhere. Say so.
5. The pool is per checkout: the verb needs `--checkout DIR` (absolute, clean, not `/`, `checkDir`/`valueDir`
   in `cli/verbs.go`): `filepath.EvalSymlinks`, then `paths.CheckoutKeyOf`. The launcher must call the same
   `CheckoutKeyOf`. `checkout.json` holds the per-team pin (S8), not the real path (S8's open question stands).
6. Re-placing the same document: S8's claim code replaces (RENAME_EXCHANGE), the old record goes to
   quarantine without its credential; both runs succeed, the pool ends identical. State "accepted, not
   refused" and test it. No new `ErrClaimEnded` sentinel is needed (see the flow).
7. Latent, not reachable through this verb while the store is password-only: `credentialName` is taken from
   the store's kind, so a record whose credential file is the other kind keeps that file when it goes to
   quarantine. Report as an observation, no fix (seat internals, owner's call).
8. Env from `Env.Getenv` (nil-safe): `HOME` (absolute) with `paths.StateHome(home, XDG_STATE_HOME)`,
   `XDG_RUNTIME_DIR` (absolute, else `exits.CodeRuntime`; `HOME` problems `exits.CodeHome`).
9. Existing tests that change: `member/cli/verbs_test.go` `bVerbs` (add `"seat place-issued"` after
   `"seat set-aside"`) and the usage table; `member/cli/parse_test.go:185` expects
   `"seat takes one of: disconnected, set-aside"` (becomes `..., set-aside, place-issued` if the row is added
   after set-aside); `TestEveryVerbIsBuiltOrNamesItsSlice` needs the verb in `runs` (built in cli).
   `release_test.go` (tag `teamchatrelease`) asserts `unbuilt` is empty; it is already red for `hook`,
   `status-line`, `report`, `ask`, `thread`, `questions` (n3e, n3f), so the new verb passes it by not being in
   `unbuilt`; run it and report the pre-existing failures exactly.

### Planned files

- `src-go/internal/teamchat/member/placeissued/` (new package; tests first in the same commit):
  - `Read(r io.Reader) (seat.Issued, error)`: cap (`seat.MaxBundle + seat.MaxSecret + 8 KiB`, read cap+1);
    empty; `proto.Parse` (`*proto.SyntaxError` with `Rule == "content after the value"` is "trailing", the
    rest "not strict JSON"); non-object; envelope-or-bare as above; `seat.DecodeIssued`.
  - `Place(ctx, Deps, io.Reader) (Placed, error)`; `Deps{StateHome, RuntimeDir, Checkout, Clock seat.Clock, Credential seat.Credential, Reach seat.Reacher}`; `OS(stateHome, runtimeDir, checkout)` = real clock,
    `seat.Password{Clock: matrix.SystemClock()}`, `seat.VersionsReach{Clock: matrix.SystemClock()}`;
    `Placed{Team paths.TeamID; Seat paths.SeatName}`.
  - Flow: `Read`, then `seat.Open` (Sessions/Announcer adapters that error), `store.Lock(ctx)`,
    `QuarantineLeftovers(a, s)` (the crash image: `<seat>.new-<id>/password` with no `seat.json`),
    `Claim`, `Fill(ctx, is)`; on a Fill error `c.Abandon()` (pre-write refusal leaves no `.new-` dir) and,
    when Abandon fails, `QuarantineLeftovers` again; unlock then close, errors joined.
  - Error mapping to `*exits.Error` with fixed templates (exit codes): empty/too big/not JSON/trailing/not a
    seat/envelope/invalid/clash/token: 78 `bundle`; `seat.ErrUnreachable`: 20 `unreachable` naming the URL
    (`exits.Name`); `seat.ErrBusy`, `seat.ErrHeld`: 75 `seat-held`; `seat.ErrConfig`: 78 `store`;
    `seat.ErrHomeserverClash`: 78 `bundle`. A `Failure{Object *exits.Error; Cause string}` carries the cause
    text, printed as one `teamchat: <sanitised>` line (controls to spaces) before the error object, as
    `exits.Write` does for an unknown error; the clash's two URLs and `ErrInvalid`'s fixed detail ("v is 1,
    this build reads only v 2", "record: no tls_ca", "base_url and tls_ca are not a member URL...") travel
    there. `Report(stderr, err) term.ExitCode`. Unmapped errors fall through to `exits.Write`.
- `src-go/internal/teamchat/member/cli/`: `verbs.go` row `seat place-issued`, flags `--checkout DIR`
  required (`checkDir`, `valueDir`), const `verbPlaceIssued`, `Does` with no trailing period; `run.go` `runs`
  entry; new `place.go` (`runPlaceIssued`, seam `var placeDeps = placeissued.OS`, env, `EvalSymlinks`,
  `CheckoutKeyOf`; stdout `placed seat <seat> of team <team>`, nothing else); tests `place_test.go`.
- Docs: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-B-member.md` (B 2.2.1 verb table, lists
  `seat set-aside`) and `SPEC-n3.md`: add the verb tersely; `git grep -n "seat set-aside"` finds the rows.
  `docs/` has no page that lists the member verbs.

### Refusals to table (dispatch), with what each asserts

Each: exit code, error object code, a message naming the problem, nothing written (no pool dir, no `.new-`),
and the secret (a distinctive random value in the document) in neither stdout nor stderr: empty stdin; stdin
over the cap; trailing bytes after the document; not JSON; unknown field (top level, inside `seat`);
v1 bundle (`v` 1 with `plain_http_hosts`); https URL with `tls_ca` absent and with `tls_ca` empty;
non-loopback `http`; team already on another URL, and on another server name (X8, both URLs on stderr);
allocation lock held by another open file description (fake clock, so the 30 s wait is instant; exit 75);
the seat store root or `pool/` not 0700 (exit 78 `store`); crash image (`<seat>.new-<id>/password` only):
the run succeeds, the leftover is in quarantine with no `password` file; a post-write failure (plant a
regular file named `released` in the store root, place twice: the second Fill fails after the exchange, the
error is reported, the new seat is in place; remove the file, run again: success, no `.new-` left); the same
document twice (both succeed, state it); a token document with the password store (refused naming the
kinds); a token document with an injected token double (placed in `token`, mode 0600, no `password`);
a new seat whose homeserver does not answer (fake `Reacher` error: exit 20 naming the URL, nothing written);
a held seat (`held/held.json` present: exit 75).
Also: pool files mode 0600 and dir 0700, `seat.json` equals `Encode()` of the bundle; one stdout line.

### Mutation checks to run after green (break each guard once, confirm the named test red, restore)

cap check; empty check; trailing/strict parse; envelope key allow-list; `DecodeIssued` skipped (read bare
bytes); `QuarantineLeftovers` skipped; `Abandon` on Fill error skipped; lock skipped (no `Lock`); credential
not from `Deps`; the `Failure` cause line dropped; mode of stdout line; `CheckoutKeyOf` of the unresolved path.

### Verify (from the dispatch, run from `src-go`; none run yet for the new packages)

`go build ./...`; `go vet ./internal/teamchat/...`; `go test -race -shuffle=on -count=2 -timeout=15m ./internal/teamchat/seat/... ./internal/teamchat/member/cli/... ./internal/teamchat/admin/... ./internal/teamchat/paths/... ./internal/teamchat/member/placeissued/...`; `golangci-lint run ./internal/teamchat/...` default,
`--build-tags busfailpoints`, `--build-tags tuwunel`; `go -C qa build -o /workspace/untracked/scratch/rules-driver-s9 ./driver`
then `go vet -vettool=... ./internal/teamchat/...` with no tag, `-tags busfailpoints`, `-tags tuwunel`;
`go test -tags teamchatrelease ./internal/teamchat/member/cli/`. Hook limits: write command output to files
under `untracked/scratch/` and `Read` them (no `| head`/`| tail`), no `sed`, wrap multi-statement Bash as
`bash -c 'set -uo pipefail; ...'`, `Grep` tool is absent (use `git grep`), avoid `--include=*` globs in Bash
(a secret-path guard blocks them).

### Commit

Messages start `Plan 00246 Task 8.8 S9:` and end `Co-Authored-By: Claude Sonnet 5.5 <noreply@anthropic.com>`;
write to `untracked/scratch/` and `git commit -F`. Commit the report with the code. No push, no amend, no
stash, no new branch. PLAN.md and the journal are the lead's.
