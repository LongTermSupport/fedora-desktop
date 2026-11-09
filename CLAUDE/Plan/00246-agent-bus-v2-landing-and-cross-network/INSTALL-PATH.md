# Install path for agent bus v2 (Plan 00246, X30)

The question this answers: what must exist, and in what order, for a v2 team (`teamchat`) to be
installed on a bus server host and joined over TLS by a member on a second machine (X30 of
[SPEC-cross-network.md](SPEC-cross-network.md), this plan's success criterion), starting from
what pull request 128 builds. It is a plan, not a build: nothing here is built yet unless it says
so. Paths are under `src-go/internal/teamchat/` unless given in full. Placeholders only: the bus
server host, the member machine, the declared network, the undeclared network; addresses from
RFC 5737.

**Terms.** The bus server is the Matrix server this bus runs (Tuwunel behind the TLS terminator),
and the bus server host is the machine it runs on: a server in a data centre, a VM, or a
desktop. It is not the owner's home; the owner's home holds members only. Code names
(`teamchat-server@`, `seat/homeserver.go`) keep their names.

## Ordering

Three stages, in this order, then X30's proof. Stage A is first for a real reason: the first real
bus server host is a remote machine that another repository manages and has not built yet, and
its own deploy route runs stage A's play.

| Stage | What                                                                | Runs where                                                                                                        | Depends on                                | Unblocks                                       |
| ----- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------- | ---------------------------------------------- |
| A     | Base units on the bus server host, and the play that installs them  | container: Go, tests, play edit; controller: the build; target: the play, through its own deploy route or the lab | pull request 128                          | `team apply` on a real host; X30 at seat level |
| B     | The v2 kit in the ccy image (Plan 00209 Task 4.1), CCY version bump | container: edit; host: the image rebuilt by the next ccy run                                                      | A (the kit's build)                       | a ccy session that holds a v2 seat             |
| C     | The launcher (Plan 00209 Task 4.2)                                  | container: edit; host: a ccy run                                                                                  | B                                         | a ccy member on the member machine             |
| X30   | Firewall zones, a specific route, the VM-lab scenario, the host run | lab or host; the member machine is another machine                                                                | A (lab: A only); B and C for a ccy member | the success criterion                          |

### The gates

- **Gate A is met.** Plan 00209's decision table records the irreversible choices (OD-A7, DC1,
  DC5, DC15, OD-D1, OD-D5, TID) as confirmed by the owner, "gate A met", on direction 6's
  condition, and its Task 0.6 check found that none of them blocks a remote bus server. So the
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
- **This checkout's host** (the team's host agent, or the owner where there is none): every host
  run goes into `CLAUDE/Plan/meta-deploy.list` in the same commit that needs it, and the host
  agent is asked to run `./CLAUDE/Plan/meta-deploy.bash`. No loose commands.
- **A remote bus server host** is deployed by the route of the repository that manages it, not by
  `meta-deploy.list`, which queues runs for this checkout's own host. That route runs stage A's
  play against the host as an inventory host (next section). What this repository ships for it is
  the play, the verb, the units and the plan-local acceptance script; the route runs them.
- **Asked for separately**: the member machine (another machine: the owner sets it up and runs
  the play there), the ssh alias from the owner's session to it (the owner's own configuration,
  outside this repository by design, X21), and any reboot or login. A firewalld zone change on the bus server host's main interface
  is a decision of whoever manages that host before it is queued (X30, item 1).

### Out of scope

Element on the second machine and a verb that prints the team CA (owner questions 6 and 7 of the
spec) are out: the owner's answer is agents across networks first. X30's second machine has
working agent seats, not a working Element.

## Where the play runs, and for which architecture

Two properties of stage A's play are fixed by the first real host, and each differs from what the
design assumed.

### The play runs against a remote inventory host

Every play in this repository today is `hosts: desktop`: a local connection, with the checkout on
the machine it changes and a login user whose home holds the build cache. Design steps 01 to 03
(DESIGN-v2-C-host.md 3.5.1) build as the login user from the checkout, on that machine.
`tasks/build-go-command.yml` does the same: it builds in `/home/<login user>/.cache` from
`root_dir/src-go` on the target. None of that holds for a remote host: the checkout is on the
controller, the target may have no Go toolchain, no desktop user, and no checkout.

So stage A's play makes these changes to the design, and says so:

1. **Hosts.** `hosts: "{{ teamchat_hosts | default('desktop') }}"`, so a deploy route names its
   inventory group, and a checkout's own run keeps working. The play uses nothing from the
   `desktop` group's variables (`user_login`, `install_audience`, `root_dir` as a path on the
   target): `root_dir` is read from the controller's `ansible.cfg` and used only in tasks
   delegated to `localhost`.
2. **Build on the controller, copy the binaries over.** Steps 01 to 03 become one task file that
   runs `delegate_to: localhost`, `run_once`, `become: false`: for each distinct
   `ansible_architecture` among the play's hosts it builds `teamchat-admin` and `teamchat` with
   `CGO_ENABLED=0 GOOS=linux GOARCH=<from the table>`, `-trimpath -buildvcs=false`,
   `GOFLAGS=-mod=vendor`, into a controller-side temporary directory removed at the end of the
   run. (Both commands cross-build for `amd64` and `arm64` from one host, checked in the
   container.) The source digest baked into both (`-X main.SourceDigest`) is the git tree id of
   `src-go` at the checkout's commit, and the play refuses a `src-go` that differs from that
   commit, so a digest always names the bytes built. (The closure-based digest of 3.13 does not
   exist yet; it replaces this when the board is built, and the kit and program names change with
   it.) The files are copied to the target as root straight to their digest-named homes
   (`/usr/local/lib/teamchat/teamchat-admin-<digest>` and `kit/<digest>/bin/teamchat`, 0755
   root; nothing links to them yet), and the play compares the target's sha256 of each with the
   controller's before it links anything.
3. **`declare check` is not built, so this play does not run it.** The verb is a row of the
   admin table with no code (`teamchat-admin declare check` answers "not built yet"; only the
   library `declare.Check` exists), and nothing yet turns a host's declaration into the input
   `team apply` reads. The play hands each team's entry to `team apply` as `{"team": <entry>, "lxc_members": []}`, which the Go reader judges strictly and fills the defaults of, and
   `team apply` itself refuses a security field that has no matching approval (AG4, exit 70), so
   the gate still holds. What is missing is the cross-team checks `declare check` makes (a port
   another team declares, the v1-name rule) and the `SECURITY` listing before a run. It is built
   next, as its own piece (A1b); it is a caller class `ClassOther` (refused to root and to
   `teamchat-ctl`), so the play will run it as `nobody` through `setpriv` with the owner group as
   the one supplementary group, not as the login user. Until then a host that declares one team
   is covered; a host that declares several is not.
   **The approval** is the owner's gate (DC18) and the owner is not on a remote host. The play
   approves exactly the teams `teamchat_approve_teams` names, as declared, with `team approve`
   before `team apply`: listing a team is the operator's act, so the review of the inventory
   change that adds it is the approval. A new or changed team that is not listed is refused by
   `team apply`. The copy's integrity check replaces step 03's `O_NOFOLLOW` hand-over, which
   guarded a root copy out of a user-writable directory; the transfer now lands in a root-owned
   one. K9's boundary moves from "the owner's checkout" to "the controller's checkout", and stays
   stated, not solved.
4. **No login user on the target.** Where the design reads one (`/etc/teamchat/owner-group`, the
   owner-readable group of `/var/lib/teamchat-status`, step 05; the user units `teamchat-seats.*`,
   step 07), the play takes the group from a variable, `teamchat_owner_group`, with no default
   and a failure that names it, and installs the user units only where a member user is declared
   (`teamchat_member_user`). A bus server host with no member on it installs neither.
5. **Packages and downloads are the target's own, and no firewall is started.** `zstd` is
   installed on the target. Step 06's `get_url` of the pinned Tuwunel asset runs on the target
   and needs its egress; a target without it is not established here and would move the
   download to the controller. Design step 04 installed `firewalld`; this play only checks that
   it is running and refuses otherwise, because the first start of a firewall can cut off the
   ssh that runs the play (the LXC play guards the same hazard).
6. **Not in this play yet, and refused by name when set:** the `teamchat0` bus address
   (`teamchat_address`), the LXC members (`teamchat_lxc_members`), and `installed.json` (no
   program reads it yet; the board that does is later). The Element team list has its own play.

This conflicts with design step 01 ("built as the login user") and with `build-go-command.yml`.
The conflict is the design's, and it is resolved here: the existing task keeps serving the
desktop plays that build on their own machine; stage A adds `tasks/teamchat-build-go-commands.yml`
for the controller build (a v2-owned name, since the `teamchat-keeps-off-v1` gate lets a v2 play
name only v2 files and the shared build task). Whether the two merge into one with a
`go_command_goarch` input is for review. DESIGN-v2-C-host.md 3.5.1 steps 01 to 03 and 05 get the
amended text in the commit that lands the verb.

The Tuwunel pin is v2's own file, `files/usr/local/share/teamchat/tuwunel.pin` (the gate also
forbids reading v1's), in the same `var: value` form and with the same values, listed in
`vars/version-pins.yml` so `update-versions` finds it. Until v1 is retired a bump changes both
files. The server pin file `teamchat-admin` reads (`ServerPinFile`) is written from it with
`memory_high` 512M and `cache_capacity_modifier` 0.5 as defaults: starting values that H-C1 has
not measured on a real host, overridable per host.

### No architecture lock

The design says "the pinned Tuwunel" as if there were one asset. There are two, and the Go builds
do not care:

| `ansible_architecture` | `GOARCH` | Tuwunel asset (`<version>-release-all-<this>-linux-gnu-tuwunel.zst`) | Pin line                 |
| ---------------------- | -------- | -------------------------------------------------------------------- | ------------------------ |
| `x86_64`               | `amd64`  | `x86_64-v1`                                                          | `tuwunel_sha256_x86_64`  |
| `aarch64`              | `arm64`  | `aarch64-v8`                                                         | `tuwunel_sha256_aarch64` |

- **One table, in one place**, read by the build and by the download. The version and the sha256
  of each asset are already pinned together in `files/usr/local/share/agent-bus/tuwunel.pin`
  (read by v1's installer and tracked by `update-versions` through `vars/version-pins.yml`); v2's
  play reads the same file, so one pin serves both and a bump changes both. The file is YAML, so
  the play reads it with `from_yaml` on the controller.
- **An architecture the table lacks fails the play at its first task, naming it.** There is no
  default.
- **Go builds** are for the host's own architecture, by the table above.
- **Stage B** builds the kit for the image's architecture (the image build passes it), with no
  `x86_64` literal.
- **The VM lab is x86_64 only for now**, and says so: its image fetch, kickstart tree URL and
  checksum names carry `x86_64` (`vmtest/fetch.go`), so a lab guest of the other architecture
  needs lab work (images, firmware, and an aarch64 host or emulation) that is not part of this
  stack. The scenario therefore proves the play on x86_64; the aarch64 path is proven by the
  detector below and by the cross-build, not by a run.
- **Defence Before Fix class: an architecture handled in one place and missing in another** (an
  arch lock by omission). Detector: a test that the play's table, the pin file's
  `tuwunel_sha256_*` keys and the kit builds name the same architectures, and that both commands
  cross-build for each (`GOOS=linux GOARCH=arm64 go build`, as a QA step). Red on a table with an
  architecture the pin file lacks.

## Stage A: base units on the bus server host

### What exists today

- `team apply` refuses at its render step when either base unit is missing: it reads
  `/etc/systemd/system/teamchat-server@.service` (`host/apply.go`, `render`) and
  `teamchat-tls@.service` (`host/tlsunit.go`, `tlsBaseInstalled`). No team can be applied on a
  real host until stage A installs both.
- **The base unit `teamchat-tls@` has no text anywhere in the repository.** No Go code and no
  play makes it; the `host/tlsunit.go` row of `unitSources` in `v1names/units_test.go` says so,
  and says the code that makes it must add it to `madeUnits`.
- The bus server base unit `teamchat-server@` has its sandbox lines in Go (`host/sandbox.go`,
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
  bus server host nothing puts a binary there: the only code that makes that link is the LXC
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
   existing backup units. A root-free `teamchat-admin` verb (any caller; the design has none)
   prints a named unit: `units print <name>` writes the text and `units list` writes one line
   per unit, its name and its install path, so the play holds neither a text nor a path of its
   own and the Go `paths` stay the one source. The resolver stub is a file, not a unit, and is
   listed and printed the same way. The verb runs on the target from the copied binary.
2. **`teamchat-tls@.service` carries X18a's lines itself**: no `ExecStart=` and no `User=`;
   `DynamicUser=yes`; `After=` and `Wants=` `teamchat-server@%i.service`, not bound to it;
   `Restart=always`; the bus server's hardening lines; `RestrictAddressFamilies=AF_INET AF_INET6`;
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
     per host and the listen addresses are per team; decision 1 below settles it: a drop-in that
     `team apply` renders with every team's listen addresses.
4. **`madeUnits` gains a row for every new text**: the full `teamchat-server@` unit, the
   `teamchat-tls@` unit, the health service, and anything else the verb prints. Each is then
   judged by `TestEveryUnitTextCarriesTheSixV1Paths` and `TestEveryUnitTextDeniesWhatItDoesNotAllow`,
   and the file that makes them takes a row in `unitSources`, or `TestEveryUnitTextSourceIsJudged`
   fails.
5. **The play `playbooks/imports/play-teamchat.yml`**, design steps 01 to 13 as far as X30 needs
   them, as amended above: both binaries built on the controller for the target's architecture
   and copied over, `teamchat-admin` and the kit installed with `/usr/local/bin/teamchat`
   pointing at the kit's `current` binary, the owner group, the `teamchat-ctl` account and state
   roots, the pinned Tuwunel for the target's architecture and the server pin, the units and the
   resolver stub as `units print` gives them with one `daemon_reload`, then `team approve` (the
   teams the operator names), `team remove` and `team apply` per team. Built: all of that.
   Not built: `declare check` (the verb does not exist), the bus address, the LXC phases,
   `installed.json` (item 3 above). Timers are enabled only while a team is present
   (C40), so the same play on a member-only machine installs the binaries and starts nothing.
   The fence unit is the migration's (area D) and stays out. The play is edited in the repository
   only; a host run of this checkout's own host goes into `meta-deploy.list`, and a remote host's
   run belongs to its deploy route.

### Proven in the container

- The `madeUnits` rows above, with the two class tests and the source walk.
- A golden test of each rendered base unit (its text changes only when its source does).
- The play passes the project's Ansible gates (`qa-ansible`, the syntax check, the pause rule),
  and the architecture-table test above.
- Both commands cross-build for `amd64` and `arm64` with `CGO_ENABLED=0`.

### Proven live

The route that deploys the host runs the play, then a plan-local acceptance script (shipped with
it) that applies a throwaway team declaring the bus server host's own networks only: both units
active, the terminator bound to the listen addresses and the bus server to loopback alone,
`systemctl show -p IPAddressDeny,SocketBindDeny` of both instances, the health tick clean, then
the team removed. The same runs first on a VM-lab guest (X30, item 3).

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
- **A play that works only where its checkout is.** A task that reads `user_login`, a home
  directory or `root_dir` as a path on the target runs fine against `desktop` and fails against
  a remote host. Detector: a test over `play-teamchat.yml` and the task files it imports that
  flags those references outside tasks delegated to `localhost`; red on a fixture task. Whether
  it can be general (any play meant for remote hosts) is not established.
- **An architecture handled in one place and missing in another.** See above.

## Stage B: the v2 kit in the ccy image (Plan 00209 Task 4.1)

- **Deliverables**: the `teamchat` kit in the image at `/opt/claude-yolo/optional/teamchat`
  (area B; design step 03 says the play does not write it), built from the same source as stage
  A's kit for the image's architecture, and the CCY version bump that any change under
  `files/var/local/claude-yolo/` needs ([ContainerRules.md](../../ContainerRules.md)). The
  image's files hold no `teamchat` reference today.
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

X30's minimum is stage A on the bus server host and the `teamchat` binary on the member machine
(stage A's play with no team declared). The spec says acceptance runs at the `seat` and
`teamchat` level, not through ccy; stages B and C are needed only for a ccy member.

The first real bus server host is a Fedora x86_64 VM that another repository manages. Members
reach it over a site-to-site tunnel (a /24) and over an admin VPN; a member carries no policy
route, because the member side reaches the tunnel through a dedicated client guest. Its team
declares a non-443 `port` and an IP `url`, both of which the declaration accepts; S3 requires the
url to reach the team's port, so an IP url names that port explicitly (`https://<address>:<port>`)
and one that leaves it to the scheme's 443 is refused. So the real run's network facts are the
plain case: a declared network with a specific route on the host.

### 1. Firewalld zones with a default reject

How the code places rules today: one rich rule per effective network (`rule family=... source address=<network> port port=<port> protocol="tcp" accept`; `host/records.go`, `Rule.Rich`), in
the zone of the interface that routes the network, or the default zone when no route covers it
or the interface has no zone (`hostnet/firewall.go`, `Zone`). The rule only adds an accept; it is
a wall only when the zone rejects what the rule does not accept. Two facts make that false today:

- the LXC bridge is in the `trusted` zone (`playbooks/imports/play-lxc-install-config.yml`),
  whose target accepts everything, so for an LXC member's network the firewall is no second wall;
- no play sets the bus server host's zones or its default zone. Fedora Workstation's stock
  default zone opens TCP 1025 to 65535 to any source, which would admit the team port from
  anywhere. Which zone a real host uses is not established from the repository; the repository
  that manages the first real one reports it.

Required: the interface carrying each declared network is in a zone whose target is the default
reject (`default` or `%%REJECT%%`), with no port range or service covering the team port, so the
team's rich rule is the only accept for that port. `team apply` refuses, naming the zone, when
the zone it would place a rule in has target `ACCEPT` or already opens the port. Proven in the
container by `host` tests over a fake `Firewall` answering a permissive zone; live by the lab
scenario below. Defence Before Fix class: **an accept placed where everything is already
accepted**, detected by that refusal (a runtime check; no static detector can express it).

**What happens to the LXC bridge.** The refusal would stop every LXC member network while
`lxcbr0` is in `trusted`, so the LXC play moves it, as the coordinator prefers, to a rejecting
zone (the play creates it; its target is `%%REJECT%%`). The zone must accept, from the bridge:

- DHCP and DNS to the host's address on the bridge (the `dhcp` and `dns` services), which
  `lxc-net`'s dnsmasq answers; and
- the team's port from the LXC network, which the team's rich rule adds and the zone does not.

Nothing else is accepted for the host itself. Container traffic out to the network is forwarding,
not host input: `lxc-net`'s own FORWARD and MASQUERADE rules carry it today, and on a host that
runs Docker the DOCKER-USER rules do. **Not established here:** how firewalld's own forwarding
treats traffic from a non-trusted zone alongside those rules. So the move is lab-proven before it
is queued: on an LXC-enabled guest, a container gets its lease, resolves a name, downloads a
file, and reaches the team's port, and a port the zone does not name is refused. The
zone move is an edit of the LXC play and goes to this checkout's host through `meta-deploy.list`;
`team apply`'s refusal merges with it, not before, so no host sits with a refusal it cannot
satisfy. The alternative, exempting the bridge from the refusal, is not taken: it would leave the
LXC members, the most numerous kind, with no firewall wall at all.

A zone set by the play for other interfaces exists only for interfaces the owner names (a
variable of interface names); the play never moves an interface it was not told to.

### 2. A specific route from the bus server host to the member network

The route is not the bus's to declare (direction 7): it is the host's network configuration (a
connected interface, a static route or a VPN). `team apply` checks it (X3; `host/apply.go`,
`serve`) against the main route table (`/proc/net/route`, `hostnet/route.go`). The failures, as
printed: "network <cidr>: no route covers it", and "network <cidr>: only the default route covers
it; add a route (a VPN, a static route) so the bus server is not reached by the default
interface". Proven live by `ip route get <member address>` on the bus server host naming the
declared network's interface, and in the lab by the negative case (a declared network routed
only by the default route is refused, naming the CIDR).

### 3. The VM-lab scenario: three networks

The lab is Plan 00110's (`vars/vm-test-scenarios.yml`, the `vmtest` CLI in
`src-go/internal/vmtest`). Today every guest, a run's or a `member-up` one, is on its own passt
user-mode network with one ssh port forward (`passtNetwork`): two guests cannot reach each other
and no guest has two networks. **The scenario needs a lab capability that does not exist**:
isolated libvirt networks a guest can join, several per guest. That is a change to the lab (its
`SPEC-vmtest.md` and the manifest gate), with its own tests, before the scenario. The lab is
x86_64 only (see above).

**One topology.** Three isolated networks and five guests:

| Network                                    | Guests (addresses)                                |
| ------------------------------------------ | ------------------------------------------------- |
| N1 `192.0.2.0/24`, declared member network | bus server `192.0.2.10`, member `192.0.2.20`      |
| N2 `198.51.100.0/24`, declared, second     | bus server `198.51.100.10`, router `198.51.100.2` |
| N3 `203.0.113.0/24`, undeclared            | router `203.0.113.2`, prober `203.0.113.20`       |

The bus server guest is on N1 and N2 only and is provisioned with stage A's play, with a team
declaring N1 and N2. The member guest is on N1 only and is provisioned with the same play and no
team. The router guest forwards between N2 and N3 and is on no other network; the prober guest is
on N3 only, with a route to `198.51.100.10` through the router. The bus server has a route to N3
through the router: a route is not a declaration, so N3 stays undeclared, and the route is what
lets the refusal come back to the prober.

**Where the prober's packets land.** firewalld picks a zone by source binding first, then by the
interface a packet arrives on; it does not use the lookup `team apply` makes to place a rule. The
prober's packets arrive on the bus server's N2 interface, so they land in the zone of that
interface: the rejecting zone the play was told to put N1's and N2's interfaces in. That zone
holds an accept for source N1 and one for source N2 and none for N3, so its target answers the
prober with a reject. The reply returns through the route to N3, so "a reject, not a timeout" is
observable: the prober's connection fails at once with the reject, not after its connect timeout.

Asserted:

- the member joins: the owner's pipe `teamchat-admin member add ... --credential token | ssh <member> teamchat seat place-issued ...` places the seat (X21, X22), and the member sends and
  receives a message over TLS with the CA pinned (X16);
- the prober's TCP connection to the team port is refused at the firewall, with the time it took
  bounded well below the connect timeout. The terminator's unit filter is not what refuses it
  (the firewall answers first); the filter is proven from `systemctl show` of the instance, as
  X29 reads the rendered text;
- a plain `http://` request to the team port is refused (the terminator speaks TLS only and the
  bus server answers on loopback alone);
- the negative route case of item 2, on the same bus server guest: a team that also declares a
  network no specific route covers (only the default route does) is refused, naming that CIDR.

### 4. The further checks the review asked for

- **A VPN routed through its own routing table.** Resolved: X3's refusal is kept. X3's route
  reader reads the main table only, so a VPN that puts its routes in another table, selected by
  an `ip rule`, is refused as default-routed, and the firewall's zone lookup falls to the default
  zone. The first real host does not need otherwise: a member carries no policy route, and the
  tunnel is a connected network on the host. Lab check, kept because it pins the behaviour: route
  the declared network through a second table with an `ip rule`; the expected result is that
  refusal.
- **The host's own /32.** The host's bus address is an effective network as a `/32`, served by
  the interface that holds it (`hostnet/route.go` reads the address, since the local table is not
  in `/proc/net/route`). Lab check: a member on the bus server host reaches the team through the
  bus address; the prober, given a route to the bus address, is refused at the firewall and by
  the filter, because the `/32` admits the host alone as a source.

### Who runs X30

The lab scenario runs through the lab's bridge or by the host agent (`meta-deploy.list`). The
second-machine run needs the member machine, which the owner provides and provisions, and the
owner's ssh alias, and a bus server host with stage A on it. The coordinator queues it once stage
A is on that host.

## What was checked, and what was not

Checked in the repository: the spec, this plan, Plan 00209's Phase 4 and gate text, `host`
(`render.go`, `tlsunit.go`, `apply.go`, `sandbox.go`), `hostnet` (`firewall.go`, `route.go`),
`paths`, `backup/units.go`, `v1names/units_test.go`, the admin verb table, the v1 play, v1's
Tuwunel pin file, `tasks/build-go-command.yml`, the LXC play's zone task, the VM lab's manifest,
guest networking and image fetch, and the S13 branch for a health unit text. A cross-build of
both commands for `linux/amd64` and `linux/arm64` with `CGO_ENABLED=0` passed in the container.
Not established: the bus server host's firewalld zones; whether its egress reaches the Tuwunel
release; how the terminator and Tuwunel behave under a live systemd; whether the health unit's
sandbox may restart the terminator (S12) on a real host; how firewalld forwards container traffic
out of a non-trusted zone beside `lxc-net`'s rules; the seat store's needs on a member machine
without ccy, beyond `seat place-issued` requiring `HOME` and `XDG_RUNTIME_DIR`; whether a CCY
bump gate exists.

## Decisions

Answered by the coordinator on #193 (the recommendation taken, except where noted).

1. **The health unit's IP filter for S13: a drop-in** that `team apply` renders with every
   team's listen addresses, so the host-wide unit's text stays fixed and the allow is a function
   of the networks, as X4 holds elsewhere.
2. **Firewalld zones: `team apply` refuses a permissive zone, always; the play sets a zone only
   for interfaces the owner names.** Noted exception: the LXC bridge moves to a rejecting zone in
   the LXC play (item 1 above).
3. **A VPN in its own routing table: X3's refusal is kept.** A policy-routed VPN is not on the
   critical path.
4. **X30 at seat level** (a `teamchat run --plain` member on the member machine, stage A only);
   the lab proves it at seat level, and the real second-machine run for a ccy member waits for
   stage C.

Raised while building the play, for the coordinator:

5. **`declare check` is not built** (a verb-table row with no code), and nothing turns a host's
   declaration into the cross-team checks it is meant to make. Recommendation: build it as the
   next piece (A1b) before any host declares two teams; `team apply` already holds the
   approval gate, so one team on one host is safe without it.
6. **The source digest** in stage A is the git tree id of `src-go` at a clean commit, not the
   closure-based digest of 3.13 (which nothing computes yet). Recommendation: accept it for stage
   A; the closure digest replaces it with the board.
7. **Approval by IaC**: `teamchat_approve_teams` approves a listed team exactly as declared. The
   owner's gate (DC18) assumes the owner is at the host; for a host managed from another
   repository the reviewed change is the approval. Recommendation: accept, and say so in the
   play's header (done).
8. **A second Tuwunel pin file** (v2's own) beside v1's, with the same values, because the
   separation gate forbids v2 reading a v1 file. Recommendation: accept until v1 is retired.
9. **MemoryHigh 512M and cache modifier 0.5** are starting values, not measured (H-C1).
   Recommendation: leave them overridable and measure on the first real host.

Open for review: whether the controller build task merges into `build-go-command.yml` (a
`go_command_goarch` input) or stays its own file.
