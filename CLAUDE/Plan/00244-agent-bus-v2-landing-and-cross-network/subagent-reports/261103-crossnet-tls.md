# Task 8.8, slices S4 and S5: TLS pinned to the team's CA (interim report)

Branch `agent-a0ad1ccfc9fa10015-7897ae23`, after a fast-forward of `spike/v2-cross-network`.
**S4 and the X19 rule are done. S5 (matrix TLS, X16, X17, the TLS fake homeserver) is not
started**: the session hit its context budget, so a fresh agent takes S5 from the handoff.

## Commits

| Commit      | What                                                                                                                                                                                                                                                          |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `924a8a487` | X19, Defence Before Fix: `project_transport_home` in `src-go/qa/rules/transporthome.go`, its test and its own fixture root `src-go/qa/rules/testdata/transporthome`, the 23rd rule in `table.go`, the catalogue row and section in `CLAUDE/GoProjectRules.md` |
| `b073ed773` | The sweep fix: the rule found two sites on the real tree, `hostnet/probe.go:69` and `member/forge/forge.go:138`; both now ask `matrix.LoopbackClient` and `matrix.PublicTransport` (`src-go/internal/teamchat/matrix/transport.go`)                           |
| `7d9c8550a` | S4: `src-go/internal/teamchat/pki` (CA, leaf, names, PEM, fingerprint, pool); the package doc is the spec, P1 to P12, and the test names carry the numbers                                                                                                    |

## Red and green

- Rule: with the analyzer reporting nothing the test missed all 14 flagged fixture lines (the
  fixture covers literal, value, `new`, `var`, alias, `Clone`, elided slice type, struct field,
  both commands, a package below matrix); green once written. Real tree: two findings, fixed, then
  zero untagged, with `busfailpoints` and with `tuwunel`.
- pki: the whole test set failed to compile before the sources existed (no per-expectation
  mutation run was made). Green: `go test -race -shuffle=on` on the package, `golangci-lint run`
  on the package (0 issues), the project rules driver on the package, `gofmt -l` empty.
- Also green after the sweep fix: `go test -race -shuffle=on` on matrix, hostnet, forge; `go test ./rules/...`
  and `golangci-lint run ./rules/...` in `src-go/qa`.

## Not run

- S5 and everything in it. The full `./internal/teamchat/...` test run, and the `busfailpoints` and
  `tuwunel` tagged lint and test runs, were not made (only the rule driver was run under the tags).
- No mutation checks of the pki tests.

## Decisions the spec did not make (owner questions)

1. Go 1.26's `ecdsa.GenerateKey` ignores the reader it is given, so pki reads the P-256 scalar from the
   injected reader itself (rejection sampling). Signatures still use the library's own randomness.
2. Validity is backdated one hour (`NotBefore` = clock minus 1 h) so a slightly slow member clock does not
   see a fresh certificate as not yet valid; a leaf never outlives its CA, and a CA not valid at the clock's
   time signs nothing.
3. pki parsing is stricter than asked: a CA must be self-signed; a leaf must name the CA as issuer, be for
   server authentication alone, and hold only IP names and at most one lower-case DNS name.
4. `project_transport_home` exempts `matrix` and `pki` by package path; `matrix/fakehs` is not `matrix`, so
   the fake takes its `*tls.Config` from the test that starts it. `pki` builds neither type, so its
   exemption is slack the owner may remove. The terminator package (X18) will need adding to the owner list.
5. Fixing the two sweep findings put a constructor for each in `matrix` (`LoopbackClient`, `PublicTransport`),
   although the forge client is not a Matrix client; the alternative is a separate transport package that the
   rule also names as an owner.
6. For S5 (planned, not built): `matrix.Config.CAPEM` a string; an `http` URL with a CA is refused; loopback
   `http` needs no `plain_http_hosts` entry; certificate times follow an injectable wall clock.
