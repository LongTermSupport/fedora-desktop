# Install path for agent bus v2 (Plan 00246, X30)

The question this answers: what must exist, and in what order, for a v2 team (`teamchat`) to be
installed on a homeserver host and joined over TLS by a member on a second machine (X30 of
[SPEC-cross-network.md](SPEC-cross-network.md), this plan's success criterion), starting from
what pull request 128 builds. It is a plan, not a build: nothing here is built yet unless it says
so. Paths are under `src-go/internal/teamchat/` unless given in full. Placeholders only: the
homeserver host, the member machine, the declared network, the undeclared network; addresses
from RFC 5737.

## Ordering

The coordinator's decision: three stages, in this order, then X30's proof.

| Stage | What                                                                | Runs where                                                                 | Depends on                                | Unblocks                                       |
| ----- | ------------------------------------------------------------------- | -------------------------------------------------------------------------- | ----------------------------------------- | ---------------------------------------------- |
| A     | Base units on the homeserver host, and the play that installs them  | container: Go, tests, play edit; host: the play through `meta-deploy.list` | pull request 128                          | `team apply` on a real host; X30 at seat level |
| B     | The v2 kit in the ccy image (Plan 00209 Task 4.1), CCY version bump | container: edit; host: the image rebuilt by the next ccy run               | A (the kit's build)                       | a ccy session that holds a v2 seat             |
| C     | The launcher (Plan 00209 Task 4.2)                                  | container: edit; host: a ccy run                                           | B                                         | a ccy member on the member machine             |
| X30   | Firewall zones, a specific route, the VM-lab scenario, the host run | lab or host; the member machine is another machine                         | A (lab: A only); B and C for a ccy member | the success criterion                          |

### The gates

- **Gate A is met.** Plan 00209's decision table records the irreversible choices (OD-A7, DC1,
  DC5, DC15, OD-D1, OD-D5, TID) as confirmed by the owner, "gate A met", on direction 6's
  condition, and its Task 0.6 check found that none of them blocks a remote homeserver. So the
  play tasks, units and image change that install a `teamchat` name may merge.
- **Gate B binds only the host run of the ccy hot reissue (Task 4.4).** Plan 00209 Task 3.1
  says so in its own words: "N3's LXC seats are token seats, so gate B binds only the ccy hot
  reissue's host run (Task 4.4)". Gate B is about the first deploy that gives a process a pool
  seat's password. X30's remote seat is issued with `--credential token` (X23), so it holds no
  password and is not gated. If the owner chose `--credential password` for it, gate B would
  bind that run; this plan assumes token.

### Who does what

- **The container** (any agent): edits Go, tests, plays and docs, and commits. It never runs a
  play.
- **The host** (the team's host agent, or the owner where there is none): every host run goes
  into `CLAUDE/Plan/meta-deploy.list` in the same commit that needs it, and the host agent is
  asked to run `./CLAUDE/Plan/meta-deploy.bash`. No loose commands.
- **Asked for separately**: the member machine (another machine: the owner sets it up and runs
  the play there), the ssh alias from the owner's session to it (the owner's own configuration,
  X21), and any reboot or login. A firewalld zone change on the homeserver host's main interface
  is the owner's decision before it is queued (X30, item 1).

### Out of scope

Element on the second machine and a verb that prints the team CA (owner questions 6 and 7 of the
spec) are out: the owner's answer is agents across networks first. X30's second machine has
working agent seats, not a working Element.

## Stage A: base units on the homeserver host

### What exists today

- `team apply` refuses at its render step when either base unit is missing: it reads
  `/etc/systemd/system/teamchat-server@.service` (`host/apply.go`, `render`) and
  `teamchat-tls@.service` (`host/tlsunit.go`, `tlsBaseInstalled`). No team can be applied on a
  real host until stage A installs both.
- **The base unit `teamchat-tls@` has no text anywhere in the repository.** No Go code and no
  play makes it; the `host/tlsunit.go` row of `unitSources` in `v1names/units_test.go` says so,
  and says the code that makes it must add it to `madeUnits`.
- The homeserver base unit `teamchat-server@` has its sandbox lines in Go (`host/sandbox.go`,
  `baseSandbox`, whose `IPAddressDeny=any` and `SocketBindDeny=any` belong to this unit), judged
  by `madeUnits` as "the base unit's sandbox"; no Go renders the whole unit text and no play
  installs it ("The base unit's template is to be rendered from it", says the file).
- The backup units are Go constants (`backup/units.go`: `ServiceUnit`, in `madeUnits`, and
  `TimerUnit`). Nothing installs them.
- The health units `teamchat-health.{service,timer}` exist only as design text (Plan 00209
  DESIGN-v2-C-host.md 3.11.1). No Go makes their text, on this branch or on the S12 and S13
  branches (checked on `origin/feat/crossnet-s13-int`).
- No play installs any v2 unit. `paths.PlayFile` names `playbooks/imports/play-teamchat.yml`,
  which does not exist; the design's play is DESIGN-v2-C-host.md 3.5.1, steps 01 to 13.
- The terminator's drop-in runs `/usr/local/bin/teamchat tls` (`paths.MemberBinaryPath`). On the
  homeserver host nothing puts a binary there: the only code that makes that link is the LXC
  guest kit (`host/lxc/kit.go`), and design step 03 installs `teamchat` under
  `/usr/local/lib/teamchat/kit/<digest>/` with a `current` link, which is not that path.
- v1, for comparison, ships its units as static files (`files/etc/systemd/system/agent-bus-hs@.service`,
  `agent-bus-hs-health@.service`, `agent-bus-backup@.service`) installed through
  `playbooks/imports/play-agent-bus.yml` and its installer. v2 does not copy that: a static file
  is a unit text no Go test judges.

### Deliverables

1. **Go renders every base unit text the play installs**, so the bytes on the host are the bytes
   the gate judges: `teamchat-server@.service` (from `baseSandbox` plus the per-unit lifecycle
   lines of C 3.2.3), `teamchat-tls@.service`, `teamchat-health.service` and `.timer`, and the
   existing backup units. A root-free `teamchat-admin` verb prints a named unit (its name is the
   builder's; the design has none), and the play writes its output. The resolver stub is a file,
   not a unit, and is installed the same way.
2. **`teamchat-tls@.service` carries X18a's lines itself**: no `ExecStart=` and no `User=`;
   `DynamicUser=yes`; `After=` and `Wants=` `teamchat-server@%i.service`, not bound to it;
   `Restart=always`; the homeserver's hardening lines; `RestrictAddressFamilies=AF_INET AF_INET6`;
   `IPAddressDeny=any`; `SocketBindDeny=any`; a descriptor limit above twice
   `tlsterm.DefaultMaxConns` (256, so above 512). The drop-ins keep their own denies as well: the
   B3 fix put `IPAddressDeny=any` and `SocketBindDeny=any` in both drop-ins, because systemd
   never applies one template's base text to another template's drop-in, and a drop-in is judged
   alone.
3. **The health unit is written with what S12 and S13 need**, which the design text lacks:
   - its `ReadWritePaths` include the teams' TLS directories. `paths.TLSDir` is under
     `/var/lib/teamchat-admin`, which the design's `ReadWritePaths=/var/lib/teamchat-status /run/teamchat-admin` does not cover, and S12's tick writes the renewed leaf where apply
     writes it;
   - its IP filter lets S13's probe reach the team's listen addresses. The design has
     `IPAddressDeny=any` with `IPAddressAllow=localhost`, and the probe dials each derived listen
     address (X32), which is not loopback, so the filter would drop it. The health unit is one
     per host and the listen addresses are per team: this needs a decision (below).
4. **`madeUnits` gains a row for every new text**: the full `teamchat-server@` unit, the
   `teamchat-tls@` unit, the health service, and anything else the verb prints. Each is then
   judged by `TestEveryUnitTextCarriesTheSixV1Paths` and `TestEveryUnitTextDeniesWhatItDoesNotAllow`,
   and the file that makes them takes a row in `unitSources`, or `TestEveryUnitTextSourceIsJudged`
   fails.
5. **The play `playbooks/imports/play-teamchat.yml`**, design steps 01 to 13 as far as X30 needs
   them: both binaries built as the login user, `declare check`, `teamchat-admin` and the kit
   installed with `/usr/local/bin/teamchat` pointing at the kit's `current` binary, the packages,
   the `teamchat-ctl` user and state roots, the pinned Tuwunel, the units with one
   `daemon_reload`, the bus address (`teamchat0`), `installed.json`, then `team remove` and
   `team apply` per team. Timers are enabled only while a team is present (C40), so the same play
   on a member-only machine installs the binaries and starts nothing. The fence unit is the
   migration's (area D) and stays out. The play is edited in the repository only; its host run
   goes into `meta-deploy.list`.

### Proven in the container

- The `madeUnits` rows above, with the two class tests and the source walk.
- A golden test of each rendered base unit (its text changes only when its source does).
- The play passes the project's Ansible gates (`qa-ansible`, the syntax check, the pause rule).

### Proven live

The host agent runs the play through `meta-deploy.bash`, then a plan-local acceptance script
(queued with it) that applies a throwaway team declaring the homeserver host's own networks
only: both units active, the terminator bound to the listen addresses and the homeserver to
loopback alone, `systemctl show -p IPAddressDeny,SocketBindDeny` of both instances, the health
tick clean, then the team removed. The same runs first on a VM-lab guest (X30, item 3).

### Defence Before Fix classes

- **A unit text no gate judges.** `TestEveryUnitTextSourceIsJudged` walks Go files only, so a
  unit written as a static file or a Jinja template under `files/` or `templates/` escapes it.
  Detector: extend the walk to every tracked file whose name is a `teamchat` unit, failing on one
  not made by Go; red on a fixture unit file.
- **Built and tested, reachable from no production entry point.** `deadcode -test` cannot see
  it: a unit whose `ExecStart=` names a path no play installs (the terminator's
  `/usr/local/bin/teamchat` today) passes every Go test. Detector: a test that every binary path
  a Go-made unit names is a destination the play installs (the play read as YAML). Whether the
  class can be detected more generally than for unit `ExecStart=` lines is not established;
  where it cannot, the rule's doc says so.

## Stage B: the v2 kit in the ccy image (Plan 00209 Task 4.1)

- **Deliverables**: the `teamchat` kit in the image at `/opt/claude-yolo/optional/teamchat`
  (area B; design step 03 says the play does not write it), built from the same source as stage
  A's kit, and the CCY version bump that any change under `files/var/local/claude-yolo/` needs
  ([ContainerRules.md](../../ContainerRules.md)). The image's files hold no `teamchat` reference
  today.
- **Proven in the container**: the image build's own tests, and the member CLI's release build
  test (`member/cli/release_test.go`, tag `teamchatrelease`, which no gate runs yet).
- **Proven live**: the image is rebuilt on the owner's next ccy launch after the bump; a ccy
  session runs `teamchat version`. A play run, if one is needed, goes through `meta-deploy.list`.
- **Depends on** A (one build of the kit). **Unblocks** C.
- **Defence Before Fix**: a kit in the image that differs from the host's. Detector: the digest
  the image records equals `installed.json`'s kit digest, checked by the launcher (C). Whether the
  CCY version bump has a mechanical gate is not established; `qa-reviewer` checks it.

## Stage C: the launcher (Plan 00209 Task 4.2)

- **Deliverables**: Task 4.2 as written there: the session directory mounts, refusing a checkout
  that holds the seat store (seam 53), the session record removed before the release (seams 54
  and 27), each team's system resolved with OD-D2's refusal (seam 55), and 00194's 2.0e switch.
- **Proven in the container**: Task 4.2's tests and the launcher's differential harness
  (`internal/ccylaunch/difftest`).
- **Proven live**: a ccy session on the member machine takes the seat X21 placed and posts. The
  member machine is another machine: asked of the owner.
- **Depends on** B. **Unblocks** a ccy member on the member machine, and Task 4.4.

## X30: what it needs beyond pull request 128, and how each is proven

X30's minimum is stage A on the homeserver host and the `teamchat` binary on the member machine
(stage A's play with no team declared). The spec says acceptance runs at the `seat` and
`teamchat` level, not through ccy; stages B and C are needed only for a ccy member.

### 1. Firewalld zones with a default reject

How the code places rules today: one rich rule per effective network (`rule family=... source address=<network> port port=<port> protocol="tcp" accept`; `host/records.go`, `Rule.Rich`), in
the zone of the interface that routes the network, or the default zone when no route covers it
or the interface has no zone (`hostnet/firewall.go`, `Zone`). The rule only adds an accept; it is
a wall only when the zone rejects what the rule does not accept. Two facts make that false today:

- the LXC bridge is in the `trusted` zone (`playbooks/imports/play-lxc-install-config.yml`),
  whose target accepts everything, so for an LXC member's network the firewall is no second wall;
- no play sets the homeserver host's zones or its default zone. Fedora Workstation's stock
  default zone opens TCP 1025 to 65535 to any source, which would admit the team port from
  anywhere. Which zone the owner's host uses is not established from the repository.

Required: the interface carrying each declared network is in a zone whose target is the default
reject (`default` or `%%REJECT%%`), with no port range or service covering the team port, so the
team's rich rule is the only accept for that port. `team apply` refuses, naming the zone, when
the zone it would place a rule in has target `ACCEPT` or already opens the port. Proven in the
container by `host` tests over a fake `Firewall` answering a permissive zone; live by the lab
scenario below. Defence Before Fix class: **an accept placed where everything is already
accepted**, detected by that refusal (a runtime check; no static detector can express it).

### 2. A specific route from the homeserver host to the member network

The route is not the bus's to declare (direction 7): it is the host's network configuration (a
connected interface, a static route or a VPN). `team apply` checks it (X3; `host/apply.go`,
`serve`) against the main route table (`/proc/net/route`, `hostnet/route.go`). The failures, as
printed: "network <cidr>: no route covers it", and "network <cidr>: only the default route covers
it; add a route (a VPN, a static route) so the homeserver is not reached by the default
interface". Proven live by `ip route get <member address>` on the homeserver host naming the
declared network's interface, and in the lab by the negative case (a declared network routed
only by the default route is refused, naming the CIDR).

### 3. The VM-lab scenario: three networks

The lab is Plan 00110's (`vars/vm-test-scenarios.yml`, the `vmtest` CLI in
`src-go/internal/vmtest`). Today every guest, a run's or a `member-up` one, is on its own passt
user-mode network with one ssh port forward (`passtNetwork`): two guests cannot reach each other
and no guest has two networks. **The scenario needs a lab capability that does not exist**:
isolated libvirt networks a guest can join, several per guest. That is a change to the lab (its
`SPEC-vmtest.md` and the manifest gate), with its own tests, before the scenario.

The scenario, in the lab's terms: three isolated networks, 192.0.2.0/24 (the declared member
network), 198.51.100.0/24 (a second network the homeserver sits on, also declared) and
203.0.113.0/24 (the undeclared network); a homeserver guest provisioned with stage A's play, on
the first two; a member guest on the declared member network only, provisioned with the same
play and no team; a prober guest on the undeclared network, with a route to the homeserver's
address through a lab router or the homeserver itself. Asserted:

- the member joins: the owner's pipe `teamchat-admin member add ... --credential token | ssh <member> teamchat seat place-issued ...` places the seat (X21, X22), and the member sends and
  receives a message over TLS with the CA pinned (X16);
- the prober's TCP connection to the team port is refused at the firewall (a reject, not a
  timeout), and the terminator's unit filter would refuse it too (read from `systemctl show` of
  the instance, as X29 reads the rendered text);
- a plain `http://` request to the team port is refused (the terminator speaks TLS only and the
  homeserver answers on loopback alone);
- the negative route case of item 2.

### 4. The two further checks the review asked for

- **A VPN routed through its own routing table.** X3's route reader reads the main table only.
  A VPN that puts its routes in another table, selected by an `ip rule` (policy routing), leaves
  the main table's default route as the only cover, so `team apply` refuses the network as
  default-routed, and the firewall's zone lookup falls to the default zone. Lab check: route the
  declared network through a second table with an `ip rule`; as built, the expected result is
  that refusal. Whether that is wanted is a decision (below).
- **The host's own /32.** The host's bus address is an effective network as a `/32`, served by
  the interface that holds it (`hostnet/route.go` reads the address, since the local table is not
  in `/proc/net/route`). Lab check: a member on the homeserver host reaches the team through the
  bus address; the prober, given a route to the bus address, is refused at the firewall and by
  the filter, because the `/32` admits the host alone as a source.

### Who runs X30

The lab scenario runs through the lab's bridge or by the host agent (`meta-deploy.list`). The
second-machine run needs the member machine, which the owner provides and provisions, and the
owner's ssh alias. The coordinator queues it once stage A is on the host.

## What was checked, and what was not

Checked in the repository: the spec, this plan, Plan 00209's Phase 4 and gate text, `host`
(`render.go`, `tlsunit.go`, `apply.go`, `sandbox.go`), `hostnet` (`firewall.go`, `route.go`),
`paths`, `backup/units.go`, `v1names/units_test.go`, the admin verb table, the v1 play, the LXC
play's zone task, the VM lab's manifest and guest networking, and the S13 branch for a health
unit text. Not established: the homeserver host's firewalld zones; how the terminator and
Tuwunel behave under a live systemd; whether the health unit's sandbox may restart the terminator
(S12) on a real host; the seat store's needs on a member machine without ccy, beyond
`seat place-issued` requiring `HOME` and `XDG_RUNTIME_DIR`; whether a CCY bump gate exists.

## Decisions for the coordinator

1. The health unit's IP filter for S13: a drop-in that `team apply` renders with every team's
   listen addresses, or the probe moved elsewhere. Recommendation: the drop-in, so the host-wide
   unit's text stays fixed and the allow is a function of the networks, as X4 holds elsewhere.
2. Firewalld zones: the play moves the declared networks' interfaces into a rejecting zone, or
   `team apply` refuses a permissive zone and the owner fixes it. Recommendation: the refusal in
   `team apply` always, and a zone set by the play only for interfaces the owner names.
3. A VPN in its own routing table: keep X3's refusal, or resolve routes as the kernel does,
   policy rules included. Recommendation: decide before the member machine's network is chosen,
   since a policy-routed VPN is a common way to reach it.
4. Whether X30 is met by a `teamchat run --plain` member on the member machine (stage A only) or
   needs a ccy member (stages B and C). Recommendation: the lab proves it at seat level, and the
   real second-machine run waits for stage C.
