# Cross-host LXC members: per-seat placement or a per-host proxy

The owner's ruling on Q44 (relayed by the team coordinator): an LXC container on any host must
be able to join any v2 team through a per-container step. The team's homeserver may be on
another host. The owner also asked whether a per-host "teamchat proxy" would be simpler. That
would be one binary on each host, through which anything on the host (LXC guests, podman, the
host itself) reaches any teamchat server.

## What exists on F44

- **Cross-network membership is built for ccy pool seats** (Plan 00246 Task 8.8):

  - teams declare member `networks` as CIDRs;
  - a seat is keyed on the team's url;
  - TLS uses the team's own CA, pinned in the bundle;
  - a seat issued on the bus host reaches another machine through
    `ssh <bus> sudo teamchat-admin member add ... | teamchat seat place-issued`.

- **An LXC member works only on the bus host** (Plan 00209 DESIGN-v2-C-host.md 3.9). `lxc apply`
  does two jobs in one root process:

  - it *issues* the seat, through the local homeserver's child;
  - it *places* it: the kit and the session unit go into the guest through the container's root
    fd (step 04), and the bundle is written by `teamchat guest <op>` run as the agent user.

  X5 (C 3.5.1) ties the team url to the local LXC bridge. No stated rationale restricts LXC to
  the bus host; the restriction falls out of doing both jobs in one place.

- **v1 has no cross-host LXC step either.** `ccy-teams lxc-add` lists the bus host's own
  containers (`src-go/internal/teams/lxcadd/adder.go:1-12`).

## Option A: per-seat placement on the container's host

Split `lxc apply`'s two jobs across the two hosts:

- **Issuance stays on the bus host**, unchanged. The owner's ssh with password sudo runs
  `teamchat-admin member add|return|reissue --kind lxc`. This is the same credential channel the
  ccy pool seats already use (direction 7: "a remote seat's credential is issued over ssh").

- **Placement runs as root on the container's host.** A new verb takes the issued document on
  stdin and does steps 02-05 of the present phase against a local container:

  - the kit through the root fd;
  - the session unit;
  - the guest op that writes the bundle.

  The verb is `teamchat-admin lxc place --member C/K`, with the name to be fixed by the spec.
  The container's host gets `teamchat-admin` and the kit from `play-teamchat.yml` with no team
  declared, which already installs the programs and starts nothing.

## Option B: a per-host teamchat proxy

A daemon on each host listens locally, on the bridge and loopback, and forwards to remote
homeservers. There are two forms.

- **Transport-only** (TCP or TLS passthrough). It holds no credential. It buys almost nothing:

  - a guest on the default NAT bridge already reaches a routed homeserver as its host's address;
  - TLS with the pinned CA already protects the path.

  Every remaining gap is still open: issuance, placing the bundle, the kit, the session unit. So
  B-transport is Option A plus a daemon.

- **Credential-holding** (it authenticates members itself). Anything that can reach the proxy can
  act as every member the proxy serves: any process on the host, and any guest on the bridge,
  including guests in no team. That breaks the per-seat token, which is the boundary between
  members (DESIGN-v2-C-host.md K16).

## Comparison

| Concern     | A: per-seat placement                                                                                                                     | B: per-host proxy                                                                                                               |
| ----------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Credentials | One token per seat, issued over the owner's ssh, written only into that guest user's home. As today on the bus host.                      | Passthrough: as A. Credential-holding: one process holds every seat's secret, and any local client can impersonate any of them. |
| Network     | The team admits each container host's routed network (CIDRs, direction 7). There are no per-seat admits: NAT guests appear as their host. | The same admit per host. The proxy adds a listener on every host.                                                               |
| Kit model   | Unchanged: root-fd writes, now done by the container's host.                                                                              | Unchanged, and still needed: a proxy does not put the binary or the session unit in a guest.                                    |
| X5          | Applies only to a team whose homeserver is local. A placed seat's url is checked against the guest's routes instead.                      | The url becomes the proxy's local address: a second url per team to approve and keep in sync.                                   |
| Build       | A placement verb that reuses `host/lxc` steps 02-05 without the child, plus the issuer side's `--kind lxc` remote form.                   | All of A, plus a long-running daemon, its unit, TLS client config per team, health checks and its own security review.          |

## Recommendation: A

Option B does not remove any of A's work: a guest still needs the kit, the unit and a bundle
placed for it. A transport proxy duplicates what routing and TLS already do. A credential proxy
weakens the token boundary. Option A reuses the issuance path that ccy pool seats have already
proven across networks, and the root-fd placement that `lxc apply` already has. It moves
placement to the host that owns the container.

## Common work, built first whichever is chosen

1. The kit and the session unit placed into a guest whose team's homeserver is not local. This
   is `lxc apply` steps 02-05 without the child, taking the url and CA from the issued document.
2. A guest seat keyed on the team url. Pool seats already have this (S-items of Plan 00246
   Task 8.8); the LXC guest's seat root has to take the same key, not a local unit.
