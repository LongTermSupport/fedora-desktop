# Task 8.8, slice S5: the member client trusts exactly the team's CA

Branch `agent-abb7daebad59a88ff-19cec7d5`, after a no-fast-forward merge of the pki and
transport-home branch (no conflict). S5 (X16, X17, the TLS fake homeserver) is done; S6 and later
are not started.

## Commits

| Commit      | What                                                                                                                                                                                                 |
| ----------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `dd4fe7a0e` | `fakehs.Server.StartTLS(cfg)`: the listener wrapped in the caller's `*tls.Config`, `URL()` says https, `Restart` keeps it; `fakehs/tls_test.go`                                                      |
| `300d2dadd` | `matrix.Config.CAPEM` and `Wall`; `matrix/transport.go` (pin, handshake classes, `newHTTPClient(pin)`); `memberBase(raw, hasCA)`; `NewAdmin` refuses a CA; `matrix/tls_test.go`; the fixture updates |

## What the member client does now

- With `CAPEM`: `RootCAs` is `pki.Pool(caPEM)` (one certificate, built from an empty pool),
  `MinVersion` TLS 1.3, `ServerName` the URL host without brackets, `Time` from `Config.Wall`
  (nil is the system clock). No other field is set, so nothing skips or replaces verification.
- A handshake failure is an `ErrUnreachable`, not transient, with a fixed detail: `TLS verification of <host:port> failed against the pinned CA sha256:<64 hex>: <class>`, class one of `unknown authority`, `name mismatch`, `expired or not yet valid`, `certificate not valid for this use`,
  `certificate rejected`, `not a TLS server`, `handshake refused (TLS 1.3 is the minimum)`. A
  cancelled context or this client's own time limit is classed first, as before.
- `memberBase`: `http` only to a loopback IP literal (127.0.0.0/8, `::1`), listed or not; `http`
  with a CA refused; `https` without a CA refused. Unusable CA text (junk, two certificates, a leaf,
  a key, trailing bytes) is refused at construction with a fixed detail that never echoes it.
- Admin: still `http://127.0.0.1:<port>` only; a CA is refused. `PlainHTTPHosts` stays and is inert
  for `NewMember` (S6 removes it); `listed()` is deleted.

## Red evidence (files under `untracked/scratch/`, not committed)

- fakehs, before `StartTLS` existed (`s5-red-fakehs.txt`): `tls_test.go:78:17: tm.s.StartTLS undefined (type *Server has no field or method StartTLS)`.
- matrix, compile red first (`s5-red-matrix-compile.txt`): `unknown field CAPEM in struct literal of type Config`, `cannot use hasCA (variable of type bool) as []string value in argument to memberBase`.
- matrix, behavioural red with the new surface stubbed to the old behaviour (`s5-red-matrix.txt`,
  15 tests failed), one line per expectation:
  - right CA connects: `versions over TLS: ... no answer`
  - another CA: `detail = "no answer", want "TLS verification of 127.0.0.1:<port> failed against the pinned CA sha256:<hex>: unknown authority"`; the same shape for name mismatch, expired, not yet
    valid, TLS 1.2 and a plain server under an https URL
  - https with no CA: `err = <nil>, want a *matrix.Error`; unusable CA text: the same
  - http with a CA: `detail = "plain http is only for an IP literal in plain_http_hosts"`
  - plain http to a listed non-loopback address: `err = <nil>, want a *matrix.Error`
  - the table: `memberBase("https://example.org", false) = "https://example.org", want a refusal`
  - admin takes no CA: `err = <nil>, want a *matrix.Error`
- Mutations of the finished code, each seen red and restored: `RootCAs` nil (system roots) fails
  `TestTLSSystemRootsAreNotConsulted` with `err = <nil>, want a *matrix.Error` (`s5-mutation-rootcas-nil-alone.txt`);
  `MinVersion` TLS 1.2 fails `TestTLSOneDotTwoServerFails` (`err = <nil>`); the real clock in place of
  `Wall` fails the expired and not-yet-valid tests; a wrong `ServerName` fails 5 tests.

## Checks (from `src-go`, all green on the final tree)

- `gofmt -l` on the touched packages: empty. `go build ./...`, `go vet ./...`, `go vet -tags busfailpoints`
  and `-tags tuwunel` over `./internal/teamchat/...`: clean.
- `go test -race -shuffle=on ./internal/teamchat/...`: all packages ok. Then `-count=2 -tags busfailpoints`
  over matrix, pki, seat, admin and guest (the failpoint lane runs every test twice): ok.
- golangci-lint 2.14.0 over `./internal/teamchat/...`, plain, `--build-tags busfailpoints` and
  `--build-tags tuwunel`: 0 issues, with no suppression and no config change (gosec G402 passes: every
  test `tls.Config` sets `MinVersion`). Run with `--allow-parallel-runners` because another agent held the lock.
- Project rules driver (`go -C qa build ./driver`, then `go vet -vettool=...`) over `./internal/teamchat/...`,
  plain and both tags: nothing. `go -C qa test ./rules/...`: ok.
- Not run, as told: `qa-all.bash`, `qa-changed.bash`, and the `tuwunel` tests with a real binary
  (`FAKEHS_TUWUNEL_BIN` unset; their URLs are loopback http, which still passes).

## Fixtures changed on purpose (X12), and why

The stricter rule broke tests in three packages that used a non-loopback http URL or an https URL
with no CA. They were changed, not loosened: admin `bundles_test.go` (its default team URL was a
non-loopback address, so every refusal row there would have passed for the wrong reason; it is now
loopback, and the old rows become loopback v4 and v6 bundles plus two refusals), seat
`records_test.go` ("http not listed" now decodes: new `TestBundleLoopbackNeedsNoList`; a listed
non-loopback http URL and https with no CA are refusals), guest `check_test.go` (the refusal is
asserted by the rule's message, so it is not a connection that failed). matrix `client_test.go` is
the table by `hasCA`.

## Owner questions (add to the colleague's six)

07. Between S5 and S6 a v1 seat bundle cannot carry an https team: the seat decode builds a client with
    no CA, so it refuses `https`. Intended (S6's v2 `tls_ca` fixes it); the spike branch must not be
    released between the two.
08. The SPEC says loopback literal `127.0.0.1`, `::1`; the handoff and this slice accept all of 127.0.0.0/8.
    Say if only the two are wanted.
09. A certificate failure is not transient (a caller does not retry it). An expired leaf that the host
    re-issues is then seen by the next call, not by a retry of the same one.
10. `Config.Wall` is new API (the clock certificate validity follows). Without it, tests that make
    certificates on a fake clock would break when real time passes.
11. `TestTLSSystemRootsAreNotConsulted` proves exclusivity end to end by giving the process a system
    pool (`SSL_CERT_FILE`) that trusts the signer. Go loads that pool once per process, so it must be
    the only test of the package that needs system roots; it fails loudly (not silently) if another does,
    and its signer has a fixed key so `-count=2` works.

## Left

S6 (bundle v2: `tls_ca` in place of `plain_http_hosts`, in the pool seat, held seat, issued document,
guest check input and `reach`; delete `Config.PlainHTTPHosts`, `admin.plainHTTPHosts` and the guest
`plain_http_hosts` key), then S7 onward. The terminator's package will need adding to the
`project_transport_home` owner list (colleague's question 4).
