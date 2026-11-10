# X30 on the VM lab: multi-network members (design, opus, 2026-11-09)

Research and design only; nothing here is built. Evidence is `file:line` in this worktree.
"Unverified here" marks what only libvirt/QEMU on a real host can prove. Placeholders only.

## 0. Premises of the brief that are wrong or need correcting (read first)

1. **The topology does not need libvirt networks at all.** INSTALL-PATH.md:422-424 says the
   lab needs "isolated libvirt networks a guest can join, several per guest". Every segment of
   the fixed topology has exactly **two** endpoints (INSTALL-PATH.md:429-433): N1 {bus,
   member}, N2 {bus, router}, N3 {router, prober}. A two-endpoint segment is a point-to-point
   link, which rootless QEMU makes with a UDP socket pair on loopback (`<interface type='udp'>`), with no libvirt network object, no bridge, no root and no host interface.
   This removes the owner's open privilege decision (00161 DESIGN-bridged-lab-guest.md:131-134)
   from X30 entirely. Plan 00161 needed a bridge because its guest had to reach the **host's**
   homeserver (DESIGN-bridged-lab-guest.md:35-39); X30 needs guest-to-guest only.
2. **The manifest gate needs no change.** INSTALL-PATH.md:423-424 says the capability is "a
   change to the lab (its SPEC-vmtest.md and the manifest gate)". The manifest describes
   single-guest `run` scenarios only (section 5); members are not manifest-driven (E56-E59). The
   SPEC changes; the gate does not.
3. **"Move to a second guest" cannot run on point-to-point links** (it needs three endpoints on
   N1 at once). The rebuild case (INSTALL-PATH.md:481-482, which the doc already puts first)
   works: the old bus guest is removed and the new one takes the same link ends.
4. **The host-move order as written mints a new CA.** INSTALL-PATH.md:475-477 says "run the play
   with the same declaration, then restore". The play runs `team approve` and `team apply`
   per team (play-teamchat.yml:419 and following), so on a fresh host the first apply makes a
   new CA *before* the restore. Whether the restore then replaces it, refuses, or leaves two is
   not established. Decision D6 below.
5. **The `/32` bus-address check (X30, INSTALL-PATH.md:496-500) cannot run on stage A**: the
   play refuses `teamchat_address` (play-teamchat.yml:97, header line 42). It waits for the
   bus-address work.
6. **Zones are neither set by the play nor judged by `team apply` yet.** play-teamchat.yml has
   no zone task (grep `zone`: none), and `hostnet/firewall.go:55-57` only *looks up* a zone; the
   permissive-zone refusal (INSTALL-PATH.md:378-381) is not built. So in the scenario the
   rejecting zone is host configuration that the scenario's bus fixture makes. The "team apply
   refuses a permissive zone" check is added to the scenario when that refusal is built.
7. **The lab's RAM ceiling is per guest, not for all guests together**, and the manifest's
   comment claims otherwise: vm-test-scenarios.yml:25-27 says "the lab refuses a run that would
   take committed guest RAM past the host's ceiling", but `ramFloor` compares one guest with 75%
   of MemTotal (retention.go:172-181), as E33 says (SPEC-vmtest.md:278-280). Five guests each
   pass alone. That is a defect, docs against code (section 4).
8. **Running play-teamchat.yml from this checkout against lab guests will write a false play
   ledger record.** `play_ledger` is enabled for every `ansible-playbook` run from the repo root
   (callback_plugins/play_ledger.py:3-5, ansible.cfg `callbacks_enabled`) and records "plays run
   on this host" (play_ledger.py:1, 49-51). A run against guests would mark play-teamchat as
   applied to the controller. Decision D5.
9. **ansible.cfg defaults that break a remote inventory**: `transport=local` and
   `[inventory] enable_plugins = yaml` (ansible.cfg). The scenario's inventory must be YAML (no
   INI) and set `ansible_connection: ssh` on every host, or the play runs locally on the
   controller. `fact_caching=jsonfile` in `./untracked/facts/` with `gathering = smart` keys
   facts by inventory name, so a rerun within the cache timeout reuses a destroyed guest's
   facts: the scenario runs with `--flush-cache` (or unique per-run host names).

## 1. How the isolated networks are made

| Option                                                                                | Root?                                                                   | Host interfaces                                           | Real source address                       | L2 isolation                                                                                                                                                                                                                                                                | How it fails                                                                                                                                   |
| ------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| (a1) `type=udp` pair on 127.0.0.1 (one per segment)                                   | no                                                                      | none (loopback only)                                      | yes, guests' own static addresses, no NAT | yes: two sockets, nothing else attached; another process of the same user could inject datagrams to the port                                                                                                                                                                | a taken port fails QEMU's bind, so virt-install fails and E57 tears down; UDP is connectionless, so either end may boot or reboot in any order |
| (a2) `type=mcast`                                                                     | no                                                                      | binds the multicast group on the host's default interface | yes                                       | **no**: QEMU's multicast socket sends on the host's real interface, so frames reach the LAN segment and a LAN host can inject (TTL not set by libvirt, unverified here); `lo` has no MULTICAST flag by default, so loopback-only needs root (`ip link set lo multicast on`) | fails with no default route (offline host); two labs on one LAN with the same group merge                                                      |
| (a3) `type=server`/`client` (TCP)                                                     | no                                                                      | loopback                                                  | yes                                       | yes (one peer)                                                                                                                                                                                                                                                              | ordering: the server end must be up first; a rebooted server end breaks the link until the client restarts, which the host move does           |
| (b) `qemu:///system` isolated network                                                 | **root libvirtd**, sudoers drop-in or `libvirt` group (root-equivalent) | a bridge per network                                      | yes                                       | yes                                                                                                                                                                                                                                                                         | needs the owner decision 00161 q1/q4; SELinux labelling of overlays under the user's tree (00161 q2)                                           |
| (c) a per-network hub (a small Go UDP hub on 127.0.0.1, guests as `type=udp` clients) | no                                                                      | loopback                                                  | yes                                       | yes                                                                                                                                                                                                                                                                         | a supervised long-lived process (systemd --user transient unit); more code                                                                     |
| QEMU `-netdev stream/dgram` on unix sockets                                           | no                                                                      | none                                                      | yes                                       | yes                                                                                                                                                                                                                                                                         | libvirt has no interface type for it (unverified here); only `--qemu-commandline` passthrough, which taints the domain                         |

**Recommendation: (a1), a UDP point-to-point link per segment.** It needs nothing from the
host beyond what the lab has (QEMU, libvirt session, passt: play-vm-test-lab.yml:85-105), and
nothing from `play-vm-test-lab.yml` except, optionally, the subnet variable (section 5). Its
header line "Rootless by design" (play-vm-test-lab.yml:10-13) stays true. Keep (c) as the
extension if a segment ever needs a third endpoint.

- **Source addresses**: guests address the link NICs statically; nothing rewrites them, and the
  router forwards without NAT, so the bus sees the prober's own `N3.20` on its N2 interface.
  The scenario proves it rather than assuming it (check C9).
- **Host reaches guests**: unchanged, over the passt forward (member.go:151-153, 167;
  E22). The host has no presence on N1 to N3; every probe runs inside a guest.
- **virt-install syntax, unverified here**: `--network type=udp,source.address=127.0.0.1,source.port=<peer>,source.local.address=127.0.0.1,source.local.port=<own>,model.type=virtio,mac.address=<mac>`.
  If this virt-install does not map those sub-options, `--xml` XPath edits do it. A host triage
  step proves it without booting (`virt-install --print-xml` with the option), and the first
  `member-up --link` proves the boot.
- **libvirt session accepting `type=udp`**: believed supported unprivileged; unverified here.

## 2. Guest addressing

- **Which NIC is which**: `virtInstall` puts the passt NIC first (member.go:167); link NICs
  follow it in the order given. Interface names depend on PCI slot order, so configuration
  matches by **MAC**, which `member-up` chooses and reports (E68).
- **How addressed**: over ssh after boot, with NetworkManager keyfiles: per NIC, `nmcli con add type ethernet con-name x30-<link> 802-3-ethernet.mac-address <mac> ipv4.method manual ipv4.addresses <addr>/<len> ipv4.never-default yes ipv6.method disabled` (plus `ipv4.routes`
  where the topology needs them). A cloud-init seed is not used: members boot an
  already-initialised base with no seed (`bootOverlay`, member.go:156-159), and whether cloud-init
  would apply a second network config is unverified. Keyfiles persist across a reboot. Fedora
  Cloud Base using NetworkManager, and NM's auto-default DHCP profile on the extra NICs being
  outranked by the explicit profile, are unverified here; the fixture asserts the result with
  `ip -j addr` and `ip -j route` and fails otherwise.
- **The default route stays on passt** (`never-default`). This matters for X3 (route.go:29-33):
  N1 and N2 are connected routes on link NICs, so they are not "only the default route"; an
  unrouted CIDR is covered only by the passt default route, which is exactly the negative case
  (check C11). **Pitfall**: passt gives eth0 the host's own upstream address and prefix by
  default (unverified here), so each guest also has a connected route to the host's LAN on the
  passt NIC. A declared network that overlaps the host's LAN would look specifically routed by
  eth0. The subnets must therefore be disjoint from every route on the controller (checked, D1).
- **Routes**: bus: `N3 via N2.2`. Prober: `N2 via N3.2` (a route to the network, so its traffic
  never takes passt to a real address). Router: `net.ipv4.ip_forward=1` in
  `/etc/sysctl.d/`, and no firewalld installed (assumed absent on Cloud Base, verified by the
  fixture). Reverse-path filtering: with the bus's N3 route present, the prober's packets pass
  `rp_filter` on N2; without it, strict filtering drops them, so the timing check goes red.

## 3. The lab verbs (new expectations, SPEC-vmtest.md style)

Members section, after E59; numbering continues from E66.

- **E67** `link-up <link>`: the name as E55. Chooses two distinct free UDP ports on 127.0.0.1
  (new seam `Env.FreeUDPPort`, as `FreePort` at app.go:44-45), writes `links/<link>/ports`
  (`a=<p>\nb=<q>\n`) through `fsx.WriteAtomic`, and prints `VMTEST-LINK-UP <link> a=<p> b=<q>`.
  An existing `links/<link>/` is refused with the `link-down` hint. No libvirt object, no root.
- **E68** `member-up <name> <key> [--ram-mib N] [--link <link>:<a|b>]...`: options before or
  after the two positionals are a usage error if repeated (`--ram-mib`), malformed, or naming a
  link that has no `ports` file (`no link <l>; make it with: vmtest link-up <l>`). An end already
  recorded by another member under `members/*/links` is refused naming that member. At most
  four links. Before the boot, `members/<name>/links` records `<link> <end> <mac>` per NIC (MAC
  `52:54:00:` + three bytes from `Env.Random`, refused if another member's record holds it).
  Each link adds one `--network type=udp,...` after the passt NIC, in the order given (E1-style
  argv test). After `VMTEST-MEMBER-UP`, stdout gets one
  `VMTEST-MEMBER-NIC <name> link=<l> end=<e> mac=<m>` per link, in order.
- **E69** `--ram-mib N`: a whole number from 768 to the base's `ram_mib`; it replaces the base
  figure in both `--memory` (member.go:165) and the RAM check. Below 768, `usage:`. (Floor:
  dnf on Fedora Cloud is unreliable below that; the figure is a starting value, unverified.)
- **E70** The RAM check of `member-up` and `run` counts every running lab guest: the sum of
  the memory of each listed `vmtest-*` domain (`virsh -c qemu:///session dominfo`, "Max memory")
  plus this guest may not exceed 75% of MemTotal:
  `the host cannot carry a member on <name>: guests <sum> MiB and this <n> MiB exceed the <ceiling> MiB ceiling of a <host> MiB host`.
  A failed `dominfo` is a failure, never "zero". This fixes premise 7 and the manifest comment.
- **E71** `link-down <link>`: refused while any member records the link, naming the members and
  the `member-down` hint; otherwise removes `links/<link>/`. Nothing there is no error. Prints
  `VMTEST-LINK-DOWN <link>`.
- **E72** `member-address <name>`: needs no lab load (as E58). Prints
  `VMTEST-MEMBER-ADDRESS <name> host=127.0.0.1 port=<p> user=fedora key=<lab-key>` and the
  `VMTEST-MEMBER-NIC` lines of its record, so an inventory is built without reading the lab's
  layout.
- **E73** `member-ssh [--as <user>] <name> <command...>`: `<user>` matches
  `^[a-z_][a-z0-9_-]{0,31}$`; default `fedora` (`guestHost`, guest.go:18-19). Everything else as
  E58.
- **E57 extended**: a member-up that does not finish also removes `members/<name>/links`, so a
  failed member never holds a link end.

Cleanup on every exit path is the scenario's (`plan_on_cleanup`, as 00161 acceptance.bash:206-211):
`member-down` each guest (E59, nothing there is no error), then `link-down` each link; the same
pair runs first to remove a previous run's leftovers.

**Go seams and tests (fakes only):** `execx.Fake` records virt-install and virsh argv
(member_test.go:146 `TestE56MemberUp` pattern); `Env.FreeUDPPort` new; `Env.Random` exists
(app.go:52-53); `Env.HostMiB` exists (retention.go:174). Tests first: `TestE67LinkUp`,
`TestE68MemberUpLinks` (argv order, MAC record, end taken, unknown link), `TestE69RamMiB`,
`TestE70RamCountsRunningGuests` (fake `dominfo`, failed `dominfo`), `TestE71LinkDown`,
`TestE72MemberAddress`, `TestE73MemberSSHAs`, `TestE57TeardownDropsLinks`.

**Files that change:** `CLAUDE/Plan/00192-.../SPEC-vmtest.md`; `src-go/internal/vmtest/member.go`,
`member_test.go`, new `link.go` + `link_test.go`, `app.go` (commands, usage, `Env.FreeUDPPort`),
`retention.go` + `retention_test.go` (E70), `run.go` (E70 call), `kit_test.go` (fake env);
`docs/vm-acceptance-testing.md` (Members section, lines 89-110); `vars/vm-test-scenarios.yml`
comment lines 25-27; optionally `play-vm-test-lab.yml` (D1).

## 4. RAM and disk

- **Ceiling**: 75% of MemTotal per guest (retention.go:26, 178), checked by `member-up`
  (member.go:226) and `run` (run.go:179). A member always takes the base's `ram_mib`
  (member.go:165, 226): server-fast is 4096 MiB (vm-test-scenarios.yml:39). Five such guests
  are 20 GiB, unchecked in total (premise 7).
- **What the scenario needs** (starting values, unverified): bus 3072 (Tuwunel's `MemoryHigh`
  is 512M by default, INSTALL-PATH.md:149-150; dnf for firewalld and zstd), member 1536 (the
  play installs zstd), router 768, prober 768: 6144 MiB in all, so about an 8.2 GiB host floor
  at 75%. The rebuilt bus guest replaces the old one, so the count never exceeds five.
- **Disk**: the guard wants 22 GiB free before each member (E64, prune.go:46-48), measured at
  that moment; it does not reserve for earlier members' overlays growing. Five members at their
  20 GiB worst case is 100 GiB, which nothing guards. Real growth is small (dnf metadata and
  packages, a Tuwunel download), well under 1 GiB per guest (unverified). Recommendation: leave
  E64 as it is and have the scenario's preflight ask for 22 GiB plus 2 GiB per further guest,
  saying why (D4).

## 5. The scenario's shape and the manifest gate

**What the manifest can express**: one base per scenario (scenarios.py:68-69 keys `base`,
`description`, `planned`, `max_skipped`; optional `run_env`, `host_only`,
`reboot_before_checks`, `audience`, `optional_plays`), one guest per run, `run.bash` fetched
inside that guest at a commit and run headless there (E35-E36), one checker inside the guest
(E31, E42). `qa-vmtest-manifest` checks the strict YAML against the Python validator, each runnable
scenario's `planned` against its checker's `PLANNED`, fixtures against a declared reboot, and
orphans, only for scripts directly under the guest directory
(src-go/qa/vmtestmanifest/guest.go:120-141, 175-311). There is no way to state several guests,
links, or a play run from the controller. The bridge offers `run-scenario`,
`list-scenarios`, `refresh-base` only (play-vm-test-lab.yml:39-44), not member verbs.

**So X30 is a plan-local script**, as 00161's U23 (`_acceptance-u23.inc.bash:390-404`):
`CLAUDE/Plan/00246-.../x30-lab.bash` on `_planlib.inc.bash` (plan-folder scripts are exempt
from the bash limits), with plan-local per-role fixtures (`x30-guest-bus.bash`,
`-member`, `-router`, `-prober`) that follow E40's convention (last line
`VMTEST-GUEST-PREPARE-DONE`, failures as `ERROR: prepare: <reason>`), fed with
`vmtest member-ssh <name> 'bash -s' < fixture`. Plan-local names keep them out of the manifest
gate's guest directory. Any parsing beyond one value goes in a plan-local Python checker with its
`test_*.py` (run by `qa-plan-tests`), as 00161's `acceptance_check.py`. Run on the host through
`meta-deploy.list` by the host agent.

**Where the play runs**: on the controller, which is the lab host and this checkout. Go is there
(play-vm-test-lab.yml:21 imports play-golang.yml). The play refuses a `src-go` that differs from
the commit (INSTALL-PATH.md:91-93), so the host checkout must be clean at the run. The
inventory is a per-run YAML file under the run's log directory, built from `member-address`
(E72):

```yaml
x30:
  hosts:
    x30-bus:    {ansible_host: 127.0.0.1, ansible_port: <p>, ansible_user: issuer}
    x30-member: {ansible_host: 127.0.0.1, ansible_port: <q>, ansible_user: issuer}
  vars:
    ansible_connection: ssh
    ansible_ssh_private_key_file: <lab-key>
    ansible_ssh_common_args: -o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
```

`ansible-playbook -i <inv> --flush-cache --become-password-file <0600 file> -e teamchat_hosts=x30-bus -e @<vars.json> playbooks/imports/play-teamchat.yml`, then again for the
member with no team. The vars file holds `teamchat_owner_group`, `teamchat_teams` (networks N1,
N2; a non-443 port; url `https://<name>.test:<port>` with the name in the member's
`/etc/hosts`) and `teamchat_approve_teams`. Tuwunel's download runs on the guest and leaves
through passt (INSTALL-PATH.md:126-128).

**Subnets are input** (00161 DESIGN-bridged-lab-guest.md:141-146): with UDP links the lab holds no
IP policy, so only the scenario needs them. It takes N1, N2, N3, plus N4 (the policy-routed VPN
case) and N5 (the unrouted case), with no defaults, and fails before anything boots when one is
missing, overlapping another, or overlapping any route of the controller (`ip -j route`;
premise in section 2). Where they live is D1.

**Evidence**: one `[PASS]`/`[FAIL]` line per check, then
`X30-LAB planned=<n> passed=<n> failed=<n> verdict=pass|fail|error` on stdout; exit 0 pass, 1 a
check failed, 2 could not establish (a guest, a play run or a precondition failed), as
00161 acceptance.bash:107 and 230-232. A total that differs from `planned` is `error`. The run
log is `plan_start_log`'s, under `untracked/plan-runs/`.

## 6. The checks

Guests: `bus` (N1 .10, N2 .10), `member` (N1 .20), `router` (N2 .2, N3 .2), `prober` (N3 .20).
Every "precondition" failure is verdict `error`, never a pass.

| #     | Assertion                                                                                                                                                      | Made by                                                                                                                                                                                                                              | Red when                                                                                                               |
| ----- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| P1    | links up, addresses and routes as designed                                                                                                                     | `ip -j addr/route` in each guest, `ip route get` on bus naming N2's NIC for the prober                                                                                                                                               | a NIC unaddressed, a route missing                                                                                     |
| P2    | `issuer` on bus has a password sudo and no NOPASSWD                                                                                                            | fixture: `useradd -G wheel issuer`, lab public key in its `authorized_keys`, password from the controller on stdin to `chpasswd` (never argv); then `member-ssh --as issuer bus 'sudo -n true'` must fail                            | a NOPASSWD rule reaches `issuer`                                                                                       |
| P3    | bus egress to TCP 22 is blocked                                                                                                                                | fixture: firewalld policy, ingress zone `HOST`, egress zone `ANY`, rich rule rejecting tcp 22; check: from bus, `timeout 10 bash -c 'exec 3<>/dev/tcp/<member N1>/22'` fails, while the same from router to member succeeds          | the rule absent, or it blocks nothing because sshd is down (the control)                                               |
| C1    | installed files equal `units print` byte for byte                                                                                                              | in bus: for each `teamchat-admin units list` line, `cmp <(sudo cat <path>) <(teamchat-admin units print <name>)`; list non-empty; a self-test runs the same comparator on a copy with one byte appended and must report a difference | the play's trim-plus-newline write wrong; a vacuous comparator                                                         |
| C2    | both units active; terminator bound to N1/N2 addresses, Tuwunel to loopback only                                                                               | `systemctl is-active`, `ss -Hltn`                                                                                                                                                                                                    | Tuwunel on a non-loopback address                                                                                      |
| C3    | `IPAddressDeny=any`, `SocketBindDeny=any` on both instances                                                                                                    | `systemctl show -p IPAddressDeny,SocketBindDeny`                                                                                                                                                                                     | a drop-in missing the deny (B3)                                                                                        |
| C4    | relay issue with the egress block in place                                                                                                                     | controller: \`printf '%s\\n' "$pw"                                                                                                                                                                                                   | vmtest member-ssh --as issuer bus "sudo -S -p '' teamchat-admin member add ... --credential token"                     |
| C5    | pull issue from the member over N1                                                                                                                             | a throwaway key made in member, authorised for `issuer` on bus; member runs the pull form against bus's N1 address                                                                                                                   | sshd not reachable over N1, zone lacking ssh                                                                           |
| C6    | member sends and receives over TLS with the CA pinned                                                                                                          | the member CLI verbs X29's acceptance uses, against a second seat local to bus; plus `curl --cacert <team CA>` succeeds and plain `curl` (system trust) fails                                                                        | a publicly trusted or unpinned leaf; no TLS                                                                            |
| C7    | plain `http://` to the team port gets no Matrix answer                                                                                                         | member: `curl -sS http://<bus N1>:<port>/_matrix/client/versions` fails or returns no `versions` JSON; `curl http://<bus N1>:<Tuwunel port>` refused                                                                                 | terminator speaking plain HTTP; Tuwunel on a routable address                                                          |
| C8    | prober refused at the firewall, fast                                                                                                                           | prober: `curl --connect-timeout 10 https://<bus N2>:<port>/`, exit 7 with "No route to host" (the `%%REJECT%%` ICMP), elapsed under 3 s by `date +%s%N`                                                                              | N2's NIC in a permissive zone (connects); N3 admitted (connects); no return route or strict rp drop (timeout, exit 28) |
| C9    | the same path is open for ssh, and the bus sees the prober's own address                                                                                       | prober holds `exec 3<>/dev/tcp/<bus N2>/22` open while bus runs `ss -Htn state established '( sport = :22 )'`, expecting peer `N3.20`                                                                                                | router NATs, or forwarding broken (then C8's pass would be meaningless: this is its control)                           |
| C10   | the filter is in the instance, not only the firewall                                                                                                           | C3's `systemctl show` reads the rendered allow list naming N1 and N2                                                                                                                                                                 | filter text missing a network                                                                                          |
| C11   | a team declaring N5 (no route but default) is refused naming N5                                                                                                | `team apply` with N1, N2, N5, expecting the X3 message (INSTALL-PATH.md:411-413)                                                                                                                                                     | the route reader treating default as specific                                                                          |
| C12   | a team declaring N4 routed only in table 100 by an `ip rule` is refused                                                                                        | bus: `ip route add <N4> via <router N2> table 100; ip rule add to <N4> lookup 100`; control: the same route in main is accepted                                                                                                      | the reader reading other tables (decision 3 changed silently)                                                          |
| M1-M4 | host move (rebuild): CA fingerprint equal before and after, leaf fingerprint different, member's seat files' sha256 unchanged, the member sends after the move | backup taken on bus, copied to the run directory through `member-ssh` stdout, `member-down bus`, `member-up bus` on the same link ends with N1 `.11`, fixture and play again, restore, member's `/etc/hosts` repointed               | a restore that mints a CA; a reissue needed (a finding, INSTALL-PATH.md:484-485)                                       |

The throwaway sudo password is made per run on the controller from `/dev/urandom`, held in a
0600 file under `$XDG_RUNTIME_DIR` (removed by cleanup) for `--become-password-file` and the
`printf` builtin, and never on a command line. The bus and member fixtures install and start
firewalld (the play asserts it running, play-teamchat.yml:107-125; whether Cloud Base lacks it
is unverified) and the bus fixture creates a zone with target `%%REJECT%%` and the `ssh` service
for the N1 and N2 NICs, so `team apply` places its rich rules there (firewall.go:55-57).

## 7. Order of work

1. SPEC-vmtest.md: E67-E73 and the E57 addition; fix the vm-test-scenarios.yml:25-27 comment.
2. Defence Before Fix for premise 7 (a check claimed in a comment that the code does not make):
   class named, `TestE70...` red on today's per-guest check, sweep the lab docs for the same
   claim; then E70.
3. Tests for E67, E68, E69, E71, E72, E73 (red), then `link.go`, then `member.go` (green), one
   verb per commit.
4. docs/vm-acceptance-testing.md Members section.
5. Plan-local host triage `x30-triage.bash`: `virt-install --print-xml` with the udp option,
   the libvirt and QEMU versions, the controller's routes against the chosen subnets. Boots
   nothing. Queued in meta-deploy.list.
6. The lab play edit for D1 if chosen, then a lab play run queued with the new vmtest.
7. The fixtures and their Python checker with tests.
8. `x30-lab.bash`: preconditions, then C1-C12, then M1-M4 last (it destroys the bus guest).
9. Queue it; the result goes into PLAN.md Task 8.8 and the journal.

**Only a host run proves**: libvirt session accepting `type=udp` and virt-install's syntax;
frames crossing the links; NetworkManager by MAC on Cloud Base and its persistence; the
`%%REJECT%%` answer seen as EHOSTUNREACH within the bound; firewalld on Cloud Base; passt's
copied address on eth0; Tuwunel and the terminator under real systemd; the RAM figures; the
restore order. The container proves only the Go verbs under fakes, the checker's parsing, and
the scripts' syntax and QA gates.

## 8. Decisions for the coordinator or owner

| #   | Decision                                        | Recommendation and why                                                                                                                                                                                                                                                                                                                                          |
| --- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | Where the subnets live                          | A host_vars mapping `vm_test_lab_networks` (no default), validated by play-vm-test-lab.yml (disjoint, off the host's routes) and written into `lab.env` as `VMTEST_NET_<NAME>=<cidr>` (E7 allows unknown keys); the scenario fails naming the variable when absent. Keeps q5's rule and IaC.                                                                    |
| D2  | Network mechanism                               | UDP point-to-point links (section 1). **No privilege change on the owner's host**; 00161's system-libvirt question stays open for 00161 only.                                                                                                                                                                                                                   |
| D3  | Aggregate RAM check (E70)                       | Yes, for runs and members: the manifest already promises it and five guests make it real.                                                                                                                                                                                                                                                                       |
| D4  | Disk for five members                           | Keep E64; the scenario preflights 22 GiB plus 2 GiB per further guest.                                                                                                                                                                                                                                                                                          |
| D5  | Play ledger on a run against guests             | Fix the ledger (Defence Before Fix: "a play run against a remote inventory recorded as run on the controller"): record only plays whose hosts include the local host. Until then the scenario sets `ANSIBLE_CALLBACKS_ENABLED=ansible.builtin.default`, logged as an issue per the dogfood rule. This also matters for any deploy route run from this checkout. |
| D6  | Host-move order                                 | Run the play with the team left out (`teamchat_only_teams: []`), restore, then the play with the team; and record what the documented order (play, then restore) does as a separate observation.                                                                                                                                                                |
| D7  | Password sudo in the lab                        | A separate `issuer` user with a password sudo; the cloud user keeps NOPASSWD as the lab's control channel. The play runs as `issuer` with `--become-password-file`, as a host without NOPASSWD would.                                                                                                                                                           |
| D8  | The `/32` check and the permissive-zone refusal | Deferred to the scenario revision that follows the bus address and the refusal; X30's lab verdict states that they were not run.                                                                                                                                                                                                                                |
