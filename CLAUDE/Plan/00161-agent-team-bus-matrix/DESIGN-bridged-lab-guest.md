# Plan 00161: a bridged lab guest (proposal, for owner decision)

Status: proposal. No playbook or code is written. It serves two open items: P2's VM leg
(DESIGN.md section 10: a connection from a source outside `allow_from` is refused) and
README.vm's bridge route (a bridged guest, its subnet in `allow_from`, proven by acceptance).
Owner, 2026-10-12: IaC-provisioned like U23's member; never an owner step, never
SKIPPED-NEEDS-OWNER; the leg FAILS when the guest is missing.

Read: DESIGN.md sections 3.3, 5.2, 10, U23, D28; `files/opt/claude-yolo/optional/agent-bus/README.vm`;
`playbooks/imports/optional/common/play-vm-test-lab.yml`; `files/home/.local/bin/vmtest`
(`member-up`, `member-down`, `member_domain_remove`); `_acceptance-u23.inc.bash`;
`CLAUDE/ContainerEngines.md`, `AnsibleStyle.md`, `InfrastructureAsCode.md`, `ExampleValues.md`;
`docs/vm-acceptance-testing.md`.

## Why the existing member cannot do it

The U23 VM member is a rootless `qemu:///session` guest on passt (`vmtest member-up`). passt
connects from the host's own namespace, so the homeserver always sees `<bus_ip>`, which it
always allows. It has no source of its own, so it can prove neither leg.

## 1. The libvirt network

One network in `qemu:///system`, defined by IaC: name `vmtest-bus`, bridge `virbr-vmtbus`,
subnet `198.51.100.0/24` (RFC 5737, ExampleValues.md; unlikely to collide with a LAN, and
overridable by a variable), host at `.1`, DHCP range `.10-.50` with a static lease for the
guest's MAC.

- **Isolated vs NAT.** Choose an isolated, host-only network: no
  forward element at all. The guest needs only the host's `<bus_ip>`; it does not
  need the internet at run time (the kit is copied in by SSH and gh is the only outward need,
  see open question 3). No forward means no masquerade, so the source the homeserver sees is
  the guest's own address, which is the thing under test. NAT would rewrite it to the host's
  address when leaving the bridge, but a connection to the host's own address never leaves,
  so NAT is harmless to the test and needed only if the guest must reach GitHub (gh login).
- **Reachability of `<bus_ip>`.** A guest on `virbr-vmtbus` reaches the host's addresses
  directly; the connection arrives with source `198.51.100.x` on the bridge interface. The
  libvirt firewalld zone (`libvirt`) admits it to the host (INPUT); the homeserver's own rich
  rules then decide by `allow_from`. This matches DESIGN.md 3.3: "their bridge subnet goes
  in `allow_from`".

## 2. The guest image and how it is built

Reuse the lab's `server-fast` base (Fedora Cloud, checksum-verified, freshness-gated by
`vmtest ensure-base`): no second image pipeline. The guest is a qcow2 overlay on that base,
as `vmtest member-up` makes, differing only in `--connect qemu:///system` and
`--network network=vmtest-bus,mac=<fixed>,model=virtio`. Bases are today under the user's
`~/.local/share/vmtest`; the system-scope qemu user (SELinux, `svirt`) must be able to read
the base and write the overlay, so the overlay and base are opened by path with the label
libvirt applies, or the overlay is created under `/var/lib/libvirt/images/vmtest-bus/`
(open question 2). The lab SSH key is already baked into the base; SSH reaches the guest at
its static lease from the host (the host is on the bridge, so no port forward). In-guest
fixtures are the same as U23's (git-core via dnf, the kit copied in, gh logged in by the
harness per D66); the new part is only the network.

## 3. Lifecycle

Per run, as U23's member: the acceptance slice creates the guest (`vmtest member-up` extended
with a network option, or a sibling verb) and removes it in an `EXIT` trap, and at start if a
failed run left one (`member_domain_remove` already makes absent no error, so it is
idempotent). The libvirt NETWORK is persistent infrastructure, owned by the play: defined,
autostarted and started if absent, left alone if present with the same XML, and failed (not
rewritten) if present with different XML (Fail Fast). The guest is never kept: a kept guest
ages out of its freshness gate and holds RAM and a lease.

## 4. Which play owns it

**Edit `play-vm-test-lab.yml`, do not add a play.** The network is part of the lab (it exists
to give lab guests a source address, uses its base, key and CLI) and `play-agent-bus.yml`
runs on every desktop while the lab is opt-in; putting a libvirt network in the bus play
would couple a homeserver install to the lab stack. Two consequences the owner must accept:

1. The play's header says "Rootless by design" and installs only user-scope tooling. Adding
   a system-scope network breaks that statement for this one object. ContainerEngines.md
   already carves out rootful engines "for compatibility" (Docker, LXC); the justification
   here is that a source address on a bridge cannot be had rootlessly (a session bridge needs
   a setuid `qemu-bridge-helper` and an `/etc/qemu/bridge.conf` ACL, which is equally
   root-owned state). The header and docs/vm-acceptance-testing.md must be updated in the
   same change.
2. Needs `libvirtd`/`virtqemud` system sockets enabled and `libvirt-daemon-config-network`
   (already installed). The user does NOT join the `libvirt` group (root-equivalent, in the
   spirit of ContainerEngines.md); the guest is driven through `sudo virsh -c qemu:///system`
   from a narrowly scoped sudoers drop-in the play installs, limited to the verbs the verb
   set needs on the one domain-name prefix (open question 1).

The acceptance script reads `$VMTEST_...` facts the play writes into `lab.env`; the U23 VM leg
already FAILS naming the play when the lab is absent, and the same applies to the network.

## 5. How acceptance uses it

One guest, one persistent network, `allow_from` toggled, not two guests. A second source
address inside the same /24 cannot stand in for "outside `allow_from`" without toggling;
and a source address OUTSIDE the /24 on the bridge would be spoofed, and the host's reply
would take the default route, so the connection fails from unreachability, which would look
like a refusal but proves nothing. So:

1. Leg (a), P2: with the acceptance team's `allow_from` NOT containing `198.51.100.0/24`,
   the guest connects to each host address on `<port>`: every attempt fails, and the
   homeserver's log (or the firewalld counter) records the drop from `198.51.100.<n>`,
   which distinguishes refusal from "no route". A host-side positive control (a route and a
   plain TCP listener on the bridge address) proves the guest can reach the host at all, so
   a broken bridge FAILS rather than passing the refusal.
2. Leg (b), README.vm: the team file is re-rendered with `allow_from` containing the subnet
   (`agent-bus-install team`, the supported path); the guest, following README.vm steps 1-6
   exactly as U23 does, joins and exchanges a ping, with `--address=<bus_ip>` and a recorded
   source of `198.51.100.<n>`.
3. The toggle is restored (the original team file) in an `EXIT` trap, so a failed run does
   not leave the subnet allowed. Order a-then-b: the guest is refused before the subnet is
   allowed, and the final state is the allowed one only after (b), then restored.

README.vm's "not yet exercised by any acceptance run" is edited to point at this leg when
it lands.

## 6. Security notes

- The bridge has no forward element: the guest cannot reach the LAN or the internet, and
  nothing on the LAN can reach the guest. If NAT is wanted for gh, it is limited to the
  GitHub API hosts by an nftables rule owned by the play, otherwise gh's login happens by
  the harness copying a token in (as U23) and needs no egress at all except pingbus's forge
  call (D66), which does (open question 3).
- The subnet is in `allow_from` only for the duration of leg (b) and is removed in a trap;
  it is never in the committed team file. A stale allow after a crash is checked at the start
  of the next run and FAILS the run, rather than being silently reset.
- The guest holds the desktop user's gh token for the run (D66), as U23's members do; it is
  destroyed, with its overlay, after the run.
- The sudoers drop-in (if used) names exact commands and the `vmtest-bus` network and
  `vmtest-bus-*` domain prefix; no wildcard to `virsh` in general.
- Public repo: the subnet is an RFC 5737 example; no names, MACs or paths of any install.

## 7. Open questions for the owner

1. System libvirt or a rootless helper. Recommended: `qemu:///system` network and guest with a
   scoped sudoers drop-in. Alternative: keep the guest in `qemu:///session` and attach it to
   the system bridge via `qemu-bridge-helper` (setuid, `/etc/qemu/bridge.conf` listing the
   bridge): the guest stays rootless but the network still needs root libvirtd. Which?
2. Where the overlay lives (`/var/lib/libvirt/images/...` for SELinux, simple) versus
   the user's lab tree (relabelling).
3. Does the guest need internet (gh login check, dnf for git-core)? If yes, NAT is added and
   limited, or the base is pre-provisioned with git-core and gh; which?
4. Is it acceptable that `play-vm-test-lab.yml` stops being rootless-only, with the header and
   docs updated?
5. Subnet default `198.51.100.0/24` and bridge name `virbr-vmtbus`: overridable by a
   variable; fine?
