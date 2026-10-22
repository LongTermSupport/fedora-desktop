# n2f part hostnet (SPEC-n2f section 7: NW1-NW4, FW1-FW2, RD1-RD2)

Package `src-go/internal/teamchat/hostnet`: `network.go` (Network), `firewall.go` (Firewall),
`probe.go` (Readiness, Prober, TeamPort). Tests written first, over fake `/proc` trees in
`t.TempDir()`, the `execx` Fake and `httptest`.

QA for this part: `go test -race -shuffle=on -cover` 95.8%, `golangci-lint run` with and without
`--build-tags tuwunel` 0 issues, `go vet` clean. No qa-changed, no qa-all (as instructed).

## API as built

- `hostnet.Network{Proc, BridgeName string}` implements `host.Network` (value receiver). Interfaces
  come from `net.Interfaces`; an unexported `links` field lets tests supply them.
- `hostnet.Firewall{Runner execx.Runner; Proc string}` implements `host.Firewall`.
- `hostnet.Readiness{}` implements `host.Readiness`; `hostnet.Prober{Port func(paths.TeamID) (int, error)}`
  implements `health.Prober`; `hostnet.TeamPort(root)` is the port func.

## Decisions

- **n2f-N1. The bridge field is `BridgeName`, not `Bridge`.** A Go struct cannot hold a field and a
  method of the same name, and `host.Network` requires the method `Bridge()`. Callers write
  `hostnet.Network{Proc: "/proc", BridgeName: "lxcbr0"}`. Design-text fix: the wiring text should
  name the field `BridgeName`.
- **n2f-N2. No injectable HTTP base URL or client.** `httptest` listens on 127.0.0.1 on a random
  port, which the production URL form reaches as it is, so the tests need no seam. Each probe
  builds its own client (no proxy, `ErrUseLastResponse`, 5 s timeout, keep-alives off); a test
  pins the transport's `Proxy == nil` and the timeout.
- **n2f-N3. A missing `tcp6` / `ipv6_route` is "none"; a missing `tcp` / `route` is an error.** A
  host with IPv6 disabled has no IPv6 tables; IPv4 tables always exist, so their absence is a
  broken `/proc`.
- **n2f-N4. Table lines.** The first line of `tcp`, `tcp6` and `route` is the header and is
  skipped; every other non-blank line must parse in full (state, inode, address, port; for routes
  destination, flags, metric, mask) even when it is not a listener or a usable route. A route
  mask that is not a contiguous prefix fails the read.
- **n2f-N5. Which routes count.** A route must be up (`RTF_UP`) and not `RTF_REJECT`; best is the
  longest prefix then the lowest metric, the first of equals wins. The default route counts as
  covering a source.
- **n2f-N6. Unit attribution (NW3).** One walk of `/proc/<pid>/fd` for the whole listener set. A
  process whose fd directory, a link or its cgroup file is gone (`ErrNotExist`, `ESRCH`) is
  skipped; any other error (including permission) fails the read. The cgroup file is read once
  per process, only when it holds a wanted socket. When several processes hold one socket, a
  `.service` unit wins over none; the cgroup line is `0::<path>` and only its last element
  counts. IPv6 addresses in `tcp6` are not unmapped.
- **n2f-N7. `firewall-cmd` exit handling.** `--get-zone-of-interface` exit 2 is "no zone"; the
  interface's zone and the default zone must match `^[A-Za-z0-9_-]{1,32}$`. Query: 0 present, 1
  absent, other codes an error naming the code. A rule's zone is validated before any program
  runs. Add and Remove return the runner's `*execx.ExitError` wrapped, so the code stays
  reachable with `errors.As`. The rule is always one argument (`--add-rich-rule=<rule>`).
- **n2f-N8. Probes.** 200 exactly (a 3xx is not followed and is an error); the body is read to
  64 KiB and no further; a port outside 1-65535 is refused before any connection.
- **n2f-N9. `TeamPort`.** `state.ReadFile` capped at 64 KiB, then `declare.DecodeTeamFile`; the
  file must declare the team asked for, and a port outside 1-65535 is an error.
- **n2f-N10. Interface addresses** are unmapped, zone-less prefixes; a 16-byte mask over an IPv4
  address is reduced to its IPv4 length; an address kind or mask that is not a prefix fails.

## Notes for the wiring part

- `Network.Listeners` needs `Proc` set (production `/proc`) and root-level access to
  `/proc/<pid>/fd`; a non-root caller fails on the first foreign process (N6).
- `Firewall.Proc` is read only for the route tables.
