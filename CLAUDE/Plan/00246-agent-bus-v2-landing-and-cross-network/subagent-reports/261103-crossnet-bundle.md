# Task 8.8, slice S6: seat bundles go to v 2, `tls_ca` replaces `plain_http_hosts`

One code commit (`a9465bd7d`) after a no-fast-forward merge of the cross-network branch (no
conflict). X12 and X15 are done; S7 onward is not started. v2 is not deployed, so the formats break
freely (the commit says so).

## What changed

- `seat.Bundle` is `v` 2 (`seat.BundleVersion`). `tls_ca` is the team CA certificate in PEM: required for an
  `https` base URL, empty for a loopback `http` one. The key is always present (the decoder requires every
  key, as it did for `plain_http_hosts`), so "absent" is the empty string. `HeldBundle`, `MemberBundle` and
  `Issued` embed or hold it and follow. A present value with `http` is refused (the member client's rule from S5).
  The other records (held, released, notice) carry no bundle and stay `v` 1.
- A v 1 or v 3 seat.json is refused naming the version (`seat.json: v is 1, this build reads only v 2`),
  before the key check, so an old bundle is told its version and not that `tls_ca` is missing.
- `Bundle.MemberConfig(token)` is the one place a bundle becomes a `matrix.Config` (URL and `CAPEM`). The four
  builds (`seat/credential.go`, `member/client/seat.go`, `member/client/reach.go`, `member/teamsync/teamsync.go`)
  use it. `member/guest/check.go` has no bundle; it passes `CAPEM` from its stdin.
- `guest check` stdin and `reach` facts: `v` 2, `team`, `base_url`, `tls_ca` (the old key is refused).
- `matrix.Config.PlainHTTPHosts`, its check in `NewAdmin`, and both derivations (`admin/bundles.go`,
  `admin/lxc.go`) are deleted; `ctl` and the tests lost the loopback list. `memberBase(raw, hasCA)` is S5's.
- `admin.TeamCA` (`func(paths.TeamID) (string, error)`) is a field of `SeatBundles` (`CA`) and of `System`
  (`TeamCA`, for the LXC reach facts). It is asked only for an `https` team, never for loopback `http`.

## S7a must know

`admin/wire` wires `teamCANotMade` into both fields; it returns `ErrTeamCANotMade` ("the team CA is not made
yet"). Until S7a replaces it, `member add` and the LXC reach facts fail for an `https` team and work for a
loopback one. S7a replaces one function and one test (`TestTeamCAIsNotMadeYet`).

## Red evidence (`untracked/scratch/`, not committed)

- seat (`s6-red-seat.txt`, new field and `MemberConfig` stubbed to the old behaviour, 8 tests red), for example:
  `v 1` bundles: `err = <nil>, want an ErrInvalid` for pool, held, member and issued; `v 3`: `"seat: invalid seat.json: v is not 1" does not name the version ("v is 3")`; `https with the CA: Decode err = ... unknown field "tls_ca"`; `loopback v4 with a CA: Encode err = <nil>, want ok = false`; `MemberConfig = {BaseURL:... CAPEM: ...}`;
  the https round trips: `base_url is not a member URL its plain_http_hosts allow`.
- admin (`s6-red-admin.txt`, `s6-red-admin-reach.txt`): `https: ... base_url and tls_ca are not a member URL`
  (no CA was added), `not made: ... does not carry the provider's error`, and Reach still answers `v:1` with
  `plain_http_hosts` and answers an https team with no CA.
- wire (`s6-red-wire.txt`): `System seams left nil: [TeamCA]` and a nil-pointer panic in `TestTeamCAIsNotMadeYet`.
- matrix (`s6-red-matrix.txt`): `matrix.Config still has PlainHTTPHosts`.
- guest: no red run exists. The seat change broke the package's build until the guest code was changed in the
  same sitting. Evidence instead: the new tests exercise the code through TLS (right CA reaches, another CA makes
  no request, https with no CA, junk and a key refused before any request).
- Mutation (`s6-mutation-memberconfig.txt`): `MemberConfig` without `CAPEM` fails 11 seat tests and
  `TestReachOverTLSUsesTheSeatsCA`; restored.

## Tests that prove the expectations

- `seat/tls_test.go`: v 1 / v 3 refused naming the version (4 decoders); https needs `tls_ca`; loopback http (v4,
  v6) takes none and refuses one; not-one-CERTIFICATE-block refused (junk, two certs, a leaf, a key, trailing and
  leading bytes, truncated, white space) with no text echoed; pool, held, member and issued round trips; one CA PEM
  is under 1 KiB and every record is under a quarter of the 16 KiB cap; a `tls_ca` over the cap is refused; an
  issued bundle for an https team logs in to `fakehs.StartTLS` (`Password.Mint`) with the right CA and fails with
  `unknown authority` with another.
- `member/guest/check_tls_test.go`, `member/client/reach_test.go`: the same over `guest check` and `reach`.
- `admin/bundles_test.go`, `lxc_test.go`, `wire/wire_test.go`: provider asked only for https; provider failure,
  junk, two blocks and no provider issue nothing and never quote the secret; Reach `v` 2.

## Checks (from `src-go`, final tree)

`gofmt -l` empty; `go build ./...`, `go vet ./...`, `go vet -tags busfailpoints|tuwunel ./internal/teamchat/...`
clean; `go test -race -shuffle=on ./internal/teamchat/...` all ok (the one run's `member/client` build failure was
my own mid-run edit; it passes alone with `-race`); golangci-lint 2.14.0 plain, `busfailpoints`, `tuwunel`: 0
issues, no suppression, no config change; project rules driver plain and both tags: nothing.
Not run: `qa-all.bash`, `qa-changed.bash`, `tuwunel` tests with a real binary.

## Owner questions

1. `declare`'s default `url` is `http://<address>:<port>`, which X12 now refuses for every non-loopback address.
   The default must become `https` (`declare.defaultURL`, outside this slice). Until then a team that declares no
   `url` cannot issue a bundle.
2. `tls_ca` is always present and empty for loopback (not omitted). Say if omitted is wanted.
3. No rule yet holds "a member client built from a bundle goes through `Bundle.MemberConfig`" (Defence Before
   Fix). The class is small (five sites); a rule is available on request.

## Left

- Docs and comments still naming `plain_http_hosts` for v2: `internal/teamchat/host/lxc/lxc.go:108` (comment; not
  touched, `host/` is another agent's), and the v2 specs/designs under the 00209 plan (SPEC-matrix, SPEC-n3,
  SPEC-n2c, SPEC-n2f, SPEC-view, DESIGN-v2-B and C, research-remote-seat). The Python v1 bus keeps its key.
- S7a: the real `TeamCA` provider. S7: the terminator. S9: `place-issued` reads the v 2 issued document.
