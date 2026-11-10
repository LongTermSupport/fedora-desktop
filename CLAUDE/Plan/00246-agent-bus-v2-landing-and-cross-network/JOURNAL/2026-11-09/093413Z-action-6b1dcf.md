## 09:34 · action · —   — Install-path plan: the coordinator's review folded in, and the remote bus server host

Folded the coordinator's review of the install-path plan into INSTALL-PATH.md, plus the point the
owner added: the first real bus server host is a remote machine that another repository manages,
and its deploy route runs stage A's play.

What changed in the plan:
- The play runs against a remote inventory host (`hosts` from a variable, default `desktop`). The
  design's steps 01 to 03 build as the login user on the target; that cannot hold there. The
  binaries are built once per architecture on the controller (CGO off, vendored, cross-built;
  both commands build for amd64 and arm64, checked), copied to a root-only staging directory,
  compared by sha256, and `declare check` runs there as root. The design text and
  `build-go-command.yml` are the conflict; the existing task keeps serving the desktop plays.
- No architecture lock: one table keyed by `ansible_architecture` (GOARCH, Tuwunel asset, pin
  line), the pin read from v1's existing pin file, a missing architecture fails at the first
  task. The VM lab is x86_64 only (its image fetch names it) and the plan says so.
- The LXC bridge leaves `trusted` for a rejecting zone in the LXC play, which must accept DHCP and
  DNS from the bridge and the team port through the team's rich rule. How firewalld forwards
  container traffic out of a non-trusted zone beside lxc-net's rules is not established; the lab
  proves it before the move is queued.
- One prober topology: five guests on three isolated networks, the prober reaches the bus server
  through a router guest and lands in the zone of the interface it arrives on, so the reject
  comes back and is observable.
- Decision 3 closed: X3's refusal stays; a member carries no policy route.
- Nits: the report no longer names a checkout path, the ssh alias is outside this repository by
  design.

Not changed: DESIGN-v2-C-host.md 3.5.1 still reads steps 01 to 03 and 05 as built on the
target; the amended text goes in with the commit that builds the play, so the design and the
play do not disagree for long.
