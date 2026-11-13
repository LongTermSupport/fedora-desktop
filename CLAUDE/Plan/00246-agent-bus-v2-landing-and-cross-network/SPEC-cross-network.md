# SPEC: cross-network membership (Plan 00246 Task 8.8)

Intended behaviour, not a replay of anything. Source: direction 7 of
[Plan 00209](../00209-agent-bus-v2-in-go/PLAN.md) and the code map in
[RESEARCH-cross-network-code-map.md](RESEARCH-cross-network-code-map.md). Paths are under
`src-go/internal/teamchat/`. Every expectation is a numbered, testable sentence; each gets a Go
test written first and seen to fail. Examples use RFC 5737 and RFC 3849 addresses.

**Every decision below is defaulted and the owner may overturn it** (the owner's "DO NOT GATE ON
ME"); the build follows the default meanwhile. The ones that need the owner's eye before the first
install are listed last, under "Owner questions".

## Terms

- **Homeserver host**: the machine that runs a team's homeserver. **Member machine**: a machine
  that runs a member. They may be the same machine.
- **Member network**: a CIDR a team's members may connect from. A team's **networks** are its
  declared list of them.
- **Effective networks**: the declared networks, plus the host's own bus address as a `/32`, plus
  the LXC bridge's subnet while the team has an LXC member (what is auto-added today).

## 1. A team declares its networks (the one source)

- **X1** A team declares `networks`: a list of 0 to 32 CIDRs in canonical masked form. `listen`
  and `allow_from` are no longer declared: `team.json` and `approved.json` go to `v` 2 without
  them (v2 is not deployed; nothing migrates). A `v` 1 file is refused, naming the version.
- **X2** `networks` is a gated field as `allow_from` is: a change needs the owner's approval, is
  compared as a set, and `--add` and `--remove` work on it.
- **X3** The **listen addresses** are derived at apply time, never declared: for each effective
  network, the address of the interface that routes it (the route table, as the firewall's zone
  lookup reads it), of the network's family; an interface with several addresses of that family
  gives the one inside the network if there is one, else its lowest. The set is deduplicated and
  sorted. A network whose route is the default route is refused with the CIDR named, unless it
  is the host's own bus address or the bridge; the owner adds a route (a VPN, a static route)
  instead. (Direction 6: "not the intention that the home server would be on the internet".)
- **X4** The homeserver, the TLS terminator (section 3), the unit's IP filter and the firewall
  rules are functions of the effective networks alone: the filter allows loopback, each listen
  address as a host prefix and each effective network; the firewall has one rich rule per
  effective network in the zone of the interface that routes it. A golden render over one
  declaration proves the config, the drop-ins and the rule set change only when the networks do.
- **X5** The LXC guest source check (R48) judges the guest's address against the effective
  networks, the same set the filter and the firewall use. (The code map found it reading the
  declared list only; a failing test comes first.)
- **X6** The team's `url` host must be one of the derived listen addresses when it is an IP
  literal, and may be a DNS name otherwise; anything else is refused at `declare check`. The
  port the `url` reaches, its explicit port or its scheme's default (443, 80), is the team's
  `port`, or `declare check` and `team apply` refuse it naming both ports
  (`declare.URLProblem`, X12's one function).

## 2. A seat is keyed on the homeserver URL

- **X7** A seat is taken from the pool by checkout and team id as today; it needs no local
  homeserver unit. (The code is already so; a test pins it.)
- **X8** The first bundle a checkout takes for a team records the team's `server_name` and
  canonical base URL in the checkout record. A later bundle with the same team id and another
  canonical URL or `server_name` is refused, naming both URLs. (Two homeserver hosts must not
  both declare one team id for the same member machine.)
- **X9** The canonical URL is `https`, the lower-cased host and the explicit port, as
  `matrix.memberBase` normalises it.
- **X10** Creating a seat for a URL the machine cannot reach fails fast and names the URL;
  launching into an existing seat starts, and its link catches up when the homeserver answers
  (C 3.7.6 item 3, the owner's rule: an offline party never blocks anything).
- **X11** A seat's take state on a member machine does not come from the homeserver host's status
  files: a parked or retired seat is found by its refused login (`ErrAuthRefused`) and is
  reissued.

## 3. TLS with the team's own CA pinned

- **X12** Plain HTTP is for a loopback IP literal only: an address of `127.0.0.0/8`, or `::1`,
  written in its canonical form (no zone, no name such as `localhost`, no IPv4-mapped IPv6).
  Plain `http` carries no CA, and an `https` base url needs one: a bundle with a CA and an `http`
  url, or an `https` url and no CA, is refused. Every other member URL is `https`, local ccy and
  LXC members included. One rule, one code path: `matrix.memberBase` holds it, and `declare check` and `team apply` judge a team's `url` by it (`declare.URLProblem`), so a non-loopback
  `http` url is refused at both, naming the field. `URLProblem` also refuses a url whose port,
  explicit or its scheme's default (443 for `https`, 80 for `http`), is not the team's port. A
  team that declares no `url` gets `https://<teamchat_address>:<port>`, whatever that address is.
  `plain_http_hosts` is deleted from the bundle, from `matrix.Config` and from the two places
  that derive it.
- **X13** A team has one CA, made on first apply: ECDSA P-256, a ten-year validity, generated by
  the standard library (no new module), its key readable by root only under the team's root
  state, and included in the team's backup so that a restore keeps every issued bundle valid.
  The backup stays root-only, as it is now; a copy of it taken off the host must be encrypted,
  because it holds the CA key. `teamchat/pki` makes it, with an injected clock.
  As built (review blocker B1 of pull request 128): the backup tar is version 2 of `backup.json`
  when the team has a CA, with `ca.crt` (0644) and `ca.key` (0600) after the six members of
  version 1; a team with no CA yet is backed up as version 1, and a version 1 backup still
  restores. A restore is refused at validation when the team has another CA (never replaced; the
  refusal says to remove both CA files to take the backup's), writes the backup's CA at its
  clean-up step when the team has none and says to run `team apply` for the leaf, and keeps the
  team's CA for a backup without one, saying so in its notes.
  `ca.key` must be root's alone, mode 0600 exactly (review S8 of pull request 128): a key of
  another mode or another owner is refused by every reader of the CA (the apply's tls step, the
  issuing of a bundle or LXC reach fact, the backup, a restore's validation and its write), each
  refusal naming the file, the mode or owner found, and the fix (`chmod 600`, `chown root`); the
  host never repairs the key itself. The owner and the mode are judged on the opened file
  (`state.ReadPrivateFile`). The certificate, 0644, is not held to it. The leaf key is held to
  the same rule, but the leaf is derived (X14): a leaf key of another mode or owner is not
  refused, the apply makes the leaf again with a new key and says why.
- **X14** The homeserver's leaf certificate is signed by the CA and has as subject alternative
  names every derived listen address (IP SANs) and the `url` host (a DNS SAN when it is a name).
  It is valid for one year and is made again at apply when the SAN set changes or fewer than 30
  days remain.
- **X15** Seat bundles go to `v` 2: `plain_http_hosts` is replaced by `tls_ca`, the CA
  certificate in PEM, required for an `https` URL and absent for a loopback URL. Every record
  that carries a bundle (the pool seat, the held seat, the issued document, the guest check
  input, `reach`) carries it.
- **X16** The member client trusts exactly the bundle's CA: its root pool is that one certificate
  and the system roots are not consulted; TLS 1.3 is the minimum; the server name is verified
  against the URL host. A certificate signed by another CA, one signed by a system root, and a
  certificate for another address all fail, with an error that names the pinned CA's
  fingerprint and the host.
- **X17** `matrix.memberBase` refuses `http` to anything but a loopback literal and refuses
  `https` without a CA.
- **X18** The homeserver is reached over TLS on the derived listen addresses by a small
  per-team terminator, `teamchat tls --team <t>`, a unit `teamchat-tls@<team>` that accepts TLS
  and relays the bytes to the homeserver on loopback. The homeserver binds loopback only and
  stays plain there (X12), so the admin client, the readiness probe and `ctl` are unchanged. The
  terminator unit has the IP filter and the socket bind of X4; the leaf and its key reach it as
  credentials; it holds no CA key. (Chosen because the repo does not say what Tuwunel's TLS
  keys accept; see the owner questions.)
  The terminator holds at most `tlsterm.DefaultMaxConns` (256) connections at once, and a
  relayed connection that moves no byte either way for `tlsterm.IdleLimit` (2 minutes) is
  closed, both ends, which frees its slot, so an idle host cannot hold every slot (review S6).
  Every byte read or written puts the deadline 2 minutes ahead again. The limit is above the
  member's matrix client's own waits, a sync long-poll of at most 30 s and a pooled connection
  dropped by the client after a minute idle, so a healthy member is never cut by it.
  Every remote member reaches Tuwunel from `127.0.0.1`, the terminator's side of the relay
  (review S5 of pull request 128, read from the source at the pinned v1.9.4,
  `files/usr/local/share/agent-bus/tuwunel.pin`). The login limits do not merge members: they
  are keyed on the account, not the client address (`src/service/login_ratelimit/mod.rs` lines 1
  to 9, `reserve_login_attempt(user_id)` at line 168, the key the lower-cased user id at line
  228, called from `src/api/client/session/password.rs` lines 73 to 75 before any account
  lookup), and the render sets both buckets. Tuwunel's limits keyed on the client address are the
  OIDC throttle (`oidc_rc_per_second`, `src/core/config/mod.rs` line 2120, off by default) and
  the MSC4388 rendezvous throttle (line 2196, 10 a second, burst 20), which no teamchat client
  calls; behind the terminator every member would share one such bucket. A loopback peer always
  bypasses `ip_source` (`src/api/router/client_ip.rs` lines 7 and 70 to 77), so Tuwunel takes a
  client's address from a proxy header the client sends, and a member can name any address in
  Tuwunel's logs. Nothing a member does changes, so the member documentation says nothing of it.
- **X18a** The terminator's unit is a base unit `teamchat-tls@.service`, installed with the other
  base units by the host play (Plan 00209 DESIGN C 3.2.3, step 07; no play installs any unit yet),
  and a per-team drop-in `teamchat-tls@<team>.service.d/team.conf` that `team apply` renders. The
  base unit has no `ExecStart=` and no `User=`: `DynamicUser=yes` alone gives the terminator its
  own uid, which is not the homeserver's (the homeserver fixes its name so a restore's transient
  unit shares its uid; the terminator has no state to share and must not be able to read the
  homeserver's). It is ordered `After=` and `Wants=` `teamchat-server@%i.service`, and is not
  bound to it, so a backup that stops the homeserver does not take the listeners down. It restarts
  always; it has the homeserver's hardening lines, `RestrictAddressFamilies=AF_INET AF_INET6` (no
  resolver, no `AF_UNIX`), `IPAddressDeny=any`, `SocketBindDeny=any`, and a descriptor limit above
  twice `tlsterm.DefaultMaxConns`.
- **X18b** The drop-in holds everything that is a function of the effective networks (X4): the
  device units of the interfaces carrying a listen address (`After=`, `Wants=`), `ExecStart=`
  naming `teamchat tls` with the team, the port and each listen address, a `LoadCredential=` for
  `leaf.crt` and for `leaf.key` (never the CA's key), `IPAddressDeny=any` with `IPAddressAllow=`
  (loopback, each listen address as a host prefix and each effective network) and
  `SocketBindDeny=any` with `SocketBindAllow=tcp:<port>`. systemd grants what no rule denies, so
  an allow restricts only beside its deny; the drop-in carries the deny itself, as the
  homeserver's drop-in does, and leans on no base unit for it (no Go code makes the base unit's
  text, and a drop-in is judged alone: `TestEveryUnitTextDeniesWhatItDoesNotAllow`). The
  homeserver's own drop-in and config no longer change when a network does: the config binds
  `127.0.0.1` alone, its filter allows loopback alone and it waits for no device; units.json's
  digest sets lose the device-unit digest with them.
- **X18c** `team apply` starts the terminator after the homeserver is ready and its bound
  addresses are loopback exactly, restarts it when its drop-in changed or its leaf was made again
  (a unit reads its credentials only at its start), enables it (its base unit's `[Install]`
  `WantedBy=multi-user.target` makes that start it at boot), and holds its bound addresses on
  the port to the listen set exactly. `serve` starts it after the homeserver and makes the same
  check from units.json's listen set; `team remove` stops and disables it before the homeserver
  and deletes its drop-in with the homeserver's. The team id `tls` is reserved, as `fence` is: it
  would make a derived name equal a fixed one. A stopped terminator cuts every member off while
  the homeserver reads as healthy on loopback, so the health tick reads the terminator unit's
  state each tick and puts a problem on the team's record when it is not active ("the TLS
  terminator unit is inactive: no member can reach the homeserver") or cannot be read; it
  restarts nothing, because the unit restarts always and one that is not active was stopped or
  failed past its start limit, and it judges none while a restore is recorded (HB3). A probe
  through the terminator, which also catches a unit that is active and not answering, is X32.
- **X19** A transport or TLS configuration under `teamchat/` is built only in `matrix` and the
  terminator; a project rule holds it (Defence Before Fix), with a red fixture.
- **X20** Element Desktop's profile trusts the team CA: decided by a recorded check of what
  Electron honours; until then a profile for an `https` team states in its launcher output that
  the CA must be trusted and where it is. Built so: `teamchat-admin element check` and `apply`
  say it on stderr, one line per `https` team (stdout stays the CHANGED lines), naming
  `paths.CACertFile` and that the Flatpak build's honouring of an added CA has not been checked.
  The check itself needs an Element Desktop on a real desktop, which no agent container has.

## 4. A remote seat's credential is issued over ssh

- **X21** Nothing ssh-es out of the bus server's host: its egress is DNS, NTP and web only, by
  design, because a bus host that can ssh into every member is a pivot. The issued document is
  still `teamchat-admin member add`'s stdout, and it reaches the member machine through an ssh
  run from the member machine or from the owner's machine, into the bus host, never out of it.
  The sudo is a password sudo read from the ssh's stdin (`sudo -S -p ''`), so there is no tty
  and no NOPASSWD rule; `member add` reads no stdin, so the password line is all the pipe
  carries in. The password is read once with `read -rs`, goes in through the `printf` builtin
  and so never in an argument list, and is unset afterwards. Two forms:

  - **Pull from the member:**
    `read -rs pw; printf '%s\n' "$pw" | ssh <bus> sudo -S -p '' teamchat-admin member add ... | teamchat seat place-issued ...; unset pw`
  - **Relay from the owner's machine:**
    `read -rs pw; printf '%s\n' "$pw" | ssh <bus> sudo -S -p '' teamchat-admin member add ... | ssh <member> teamchat seat place-issued ...; unset pw`

  No `ssh -t`: a pty would carry the document through a terminal's line discipline. Each ssh
  runs as the person issuing, with their own keys and agent; nothing is installed on the member
  machine for it, and no ssh runs as root. The aliases are that person's own ssh configuration;
  no hostname is in a declaration, a file of this repository or a handle. A sudoers rule for the
  verb is the bus host's own configuration to make or not; the play installs none.

- **X22** `teamchat seat place-issued` reads one issued document on stdin (strict, capped),
  checks it as `seat.DecodeIssued` does, applies X8, takes the allocation lock, and writes the pool
  seat: the password only in the pool, mode 0600, `seat.json` beside it. Nothing of the secret
  is in argv or in output.

- **X23** A remote seat is a pool seat, and a pool seat keeps a password (gate B, owner
  2026-10-30): it is issued with `--credential password`, and X22 refuses a bundle issued with
  `--credential token` (exit 78, naming the fix), which is for an LXC guest seat the lxc apply
  places itself (issue #248; rule token-bundle-into-place-issued).

- **X24** Removing a network and running `team apply` cuts that network's reach (the filter and
  the firewall); `member park` and `member retire` revoke a seat on the homeserver as today.

## 5. A handle never carries a real hostname

- **X25** A member's `site` is declared per machine (`teamchat_site`, default `local`); no other
  source feeds a handle. The read of `HOOKS_DAEMON_HOSTNAME` for an LXC member's site is removed.
- **X26** A declared site equal to the machine's hostname (normalised), to any dot-label of it, or
  to a DNS label of the team's `url` host is refused with a fixed message. This is best effort and
  says so; the guarantee is that no code path reads a hostname into a handle.
- **X27** `os.Hostname` and `net.LookupAddr` are not called under `teamchat/` except in the one
  file that reads the machine hostname for X26; a project rule holds it, red on a fixture.
  The same rule holds `syscall.Uname`, `unix.Uname` and every constant naming the hostname's
  variables (`HOOKS_DAEMON_HOSTNAME`, `HOSTNAME`) or files (`/etc/hostname`,
  `/proc/sys/kernel/hostname`), and is red on the tree before X25's fix (review S4).
- **X28** Handles that already embed a hostname are permanent identities; the owner retires and
  reissues them if wanted. (No v2 team is deployed, so there are none in the field.)

## 6. Keeping TLS alive and trusted (S12 to S15)

The owner's slices after S11 (comment on pull request 128): a team's TLS has to keep working
with no one at the keyboard, show when it does not, survive a lost CA key, and be trusted by
Element without a hand step. Each item below is test first, each test seen red, and a reading
the builder takes where this text is silent goes in its report.

- **X31** (S12) **Unattended leaf renewal.** The leaf is judged only by `team apply` (X14), so a
  team with no apply for a year goes dark when its leaf expires. The health tick makes the leaf
  again by X14's own rule (`pki.Leaf.NeedsRenewal`: the SAN set changed, or fewer than 30 days
  remain), writes it where apply writes it, and restarts the terminator, which reads its
  credentials only at its start (X18c). The make-and-restart is one step apply and the tick
  share, never a second copy; the tick reads time from the injected clock. When it cannot (the
  CA key unreadable, a write or the restart failing), it puts a problem on the team's record
  while the leaf is still valid, naming the expiry date and the cause, and tries again at every
  tick; a leaf past its expiry is a problem of its own. It judges none while a restore is
  recorded (HB3). Tests: 31 days left, nothing made; 29 days left, the leaf made, the terminator
  restarted once, the record clean, and the next tick does nothing; a changed SAN set renews at
  any age; the CA key unreadable at 29 days: a problem naming the date, no restart, and the same
  problem at the next tick; an expired leaf: its own problem; a restore recorded: nothing judged.
  **As built** (`subagent-reports/261106-crossnet-s12-s13-opus-report.md`): one leaf step,
  `host/leaf.go` `leafStep.ensure`, which apply and the tick (`host.LeafRenewer.Renew`) both
  call; the CA key is read only when a leaf must be made, so a tick with nothing to make never
  touches it; an active terminator that started before the leaf in force was made is
  restarted, which also retries a failed restart at every tick with no state kept between
  ticks; a failure is one problem with the expiry date and a fixed cause class (the file or
  unit that failed), never the error's own text, which the record refuses past 512 bytes (the
  project rule `project_problem_error_text` holds it); a held lock, `Refresh`, and a recorded
  restore all mean nothing is renewed this tick. The tick is `teamchat-admin health --all` run
  as root by `teamchat-health.service`. **That unit is not written yet, and its text must give
  the tick what X31 and X32 need:** `ReadWritePaths` must include `/var/lib/teamchat-admin` (or
  each team's `tls/` directory), or under `ProtectSystem=strict` every renewal fails with "the
  leaf cannot be written" and shows as a problem while the leaf is valid; and its
  `IPAddressAllow=localhost` must also allow the listen addresses (or the effective networks),
  as the terminator's drop-in has them, or X32's probe of a non-loopback address is refused. The
  code that writes that unit must add its text to `madeUnits` in
  `internal/teamchat/v1names/units_test.go`, which judges every unit text the Go code makes.
- **X32** (S13) **A probe through the terminator.** X18c's "not built" is built: the health tick
  makes a TLS request to each derived listen address with the team CA, as a member's client
  does (X16, the server name verified against that address), so that a terminator that shows as
  active but does not answer becomes a problem on the record. The probe is a client built in
  `matrix`, the one home of transport configuration (X19), never a second transport. It asks
  `/_matrix/client/versions`, which takes no credential, and is bounded by the injected clock; a
  slow address delays neither another address's probe nor the tick's other work. A failing
  address is a problem naming that address and the cause (refused, handshake failed with the
  pinned CA's fingerprint, no answer within the bound). It probes only while the unit is active:
  an inactive unit is X18c's one problem, not two. It judges none while a restore is recorded.
  Tests, against a TLS server on loopback with the leaf apply made: every address answers, no
  problem; one listener closed, a problem naming only it; a leaf signed by another CA, a
  problem naming the fingerprint; an address that hangs, a problem at the bound on the fake
  clock; an inactive unit, X18c's problem only and no probe made; a restore recorded, nothing.
  **As built** (`subagent-reports/261106-crossnet-s13-opus-report.md`): `matrix.Reach` is the
  probe (the pin of X16 with the listen address as the URL host, `matrix.ReachLimit`, 5 s, armed
  on the injected timers); `host.AppliedReach` gives the team CA and the listen set of units.json
  on the port of team.json; the tick starts one goroutine per address after the renewal and
  collects them at the end of its gather, so the view, the guests, the backups and the disk are
  read while they run. A failing address is one problem, `the TLS terminator does not answer on <addr>: <cause>`, fix `systemctl status <unit>`, the cause one of `the connection was refused`,
  the pin's fixed handshake text with the fingerprint, `no answer within 5s`, `it answered HTTP <code>`, `the connection failed`; a reach that cannot be read is one problem with apply as
  its fix. Not probed: a unit not active or not read, a restore recorded (also when seen under
  the lock), and a terminator the renewal restarted this tick (it is starting with its new leaf;
  the next tick probes it). `Refresh` probes too, as it changes nothing.
- **X33** (S14) **Rotating the CA.** A root verb makes a new team CA and a new leaf and
  reissues every bundle, for a lost or exposed CA key. A member trusts exactly its bundle's CA
  (X16), so a hard cut is what the protocol gives: the terminator, restarted with the new leaf,
  is unreachable to a member until its bundle carries the new CA, and the old CA is trusted by
  no member that holds the new bundle. The verb rewrites the bundles it can reach (the seats on
  the homeserver host) and prints, one per line on stdout, the handles of those it cannot (the
  seats placed over ssh, X21, X22): the owner re-places each of them with the same pipe, and the
  verb says so on stderr. The builder finds how the seat records tell the two apart and states
  it; if they do not, the distinction is added at issue time, and that is reported as a
  finding. All new material is made and checked before any file is replaced, so a failure leaves
  the old CA, leaf and bundles whole. The verb refuses while a restore is recorded. It logs the
  old and the new CA's fingerprints, never a key. A backup made before the rotation holds the
  old CA and leaf: restoring it undoes the rotation, and the verb's help says to rotate again
  after such a restore. The verb also says, on stderr, that the Element play (X34) must be run
  again: the record that play leaves holds the old CA, and `teamchat-admin element check` and
  `apply`, which cannot read the root-only CA, go on treating it as placed until the play's own
  check fails on it. Tests: the new CA's fingerprint differs; the new leaf verifies under the
  new CA and not the old; every reachable seat's `tls_ca` is the new CA; the remote seats are
  listed and no other; a client holding an old bundle refuses the new leaf with X16's message; a
  failure at each step leaves everything as it was; a restore recorded refuses; stdout holds
  only the handles; stderr names the Element play.
  **As built** (`subagent-reports/261106-crossnet-s14-opus-report.md`): `teamchat-admin team rotate-ca --team T`, a root verb beside `team apply`. The new CA and the leaf for the names
  the last apply made it for (`host.AppliedLeafNames`) are made by apply's own steps
  (`leafStep.make` is shared with X31, never a second copy) and checked before anything is
  replaced: the leaf verifies under the new CA and not the old, and the two fingerprints differ.
  Five writes follow, each file whole with apply's modes: the CA key, the CA certificate, the
  leaf key, the leaf certificate, and the LXC record. A failed write puts back every file
  written before it and the failed one itself, since a write can land and still report failure;
  a file that cannot be put back is named, and the error says the files are mixed and a team
  backup must be restored. An active terminator is then restarted; an inactive one is not
  started, and a failed restart leaves the rotation in place for X31's tick to retry. A restore
  recorded refuses the verb (exit 70). stdout is the handles of the enrolled agent seats the
  host does not place, one per line and nothing else; stderr has the two fingerprints, the
  reissue pipe for each handle, the LXC seats the next `lxc apply` reissues, the Element play
  to run again, and that a backup taken before the rotation undoes it.
  **Three readings the coordinator may overturn.** First, the premise "the seats on the
  homeserver host are reachable" does not hold: no pool seat is placed by root, on the
  homeserver host or off it, because the pool is the owner's and every such seat is placed by
  the owner's own `seat place-issued` pipe (X22), so a podman seat on the homeserver host is
  listed on stdout like a remote one. The seats root does place are the LXC seats, which `lxc apply` places through `guest place-bundle` and records, so that record is what tells the two
  apart and no issue-time flag is needed. Second, the verb does not reissue the LXC seats
  itself: a reissue revokes the old credential and cannot be undone if a later step fails, so
  the verb marks each LXC entry holding a handle `deferred` with `lxc.WhyCARotated`, as part of
  the same all-or-nothing replacement, and the next `lxc apply` reissues and places the new
  bundle. "Every reachable seat's `tls_ca` is the new CA" therefore holds after the next `lxc apply`, not when the verb returns, and is proven as a chain (the record is marked, a marked
  seat makes `joinOf` answer `member reissue`, and `host.CAPEM`, which bundles are issued from,
  is the new CA); there is no end-to-end test through `lxc apply`. Third, a crash between two
  writes (power loss, not an error) can leave the files mixed, the same window apply's two-file
  CA write has; `loadCA` refuses a key and certificate that do not match, by name, and the
  recovery is to restore a backup and rotate again. No journal was added.
  **Two gaps found, one since fixed.** `admin/wire` served `teamCANotMade` for `System.TeamCA`
  and for the bundles' CA, so in the production build no `https` bundle or LXC reach fact could
  be issued at all, before or after a rotation, and the LXC reissue X33 relies on would have
  failed at the bundle step. Review blocker B2 of pull request 128 fixed it by serving
  `host.CAPEM(root, team)` (S16, see Build order), so the reissue now has its CA. The other
  stands: the verb names the Element play by v2's name, `paths.ElementPlayFile`
  (`play-teamchat-element.yml`), because `teamchat-keeps-off-v1` refuses v1's file name, but
  that play does not exist: X34's tasks are in v1's `play-agent-bus-element.yml`, so until the
  Element step is ported the play the verb names is not the play that holds them. Not proven by
  a plant: `checkRotation`'s refusal of a new leaf that verifies under the old CA, which a leaf
  from a fresh CA cannot reach on its own. The old CA is read as every CA read is (X13, review
  S8): a `ca.key` that is not the owner's alone with mode 0600 refuses the rotation, naming the
  `chmod` or `chown` that fixes it, and nothing is replaced.
- **X34** (S15) **Element trusts the CA.** X20 says only that the CA must be trusted.
  The play now installs the team CA where the Flatpak build of Element Desktop reads it, so no
  hand step is needed, and fails loudly if the CA is not trusted afterwards: its last task
  makes a TLS handshake from inside Element's own sandbox to the team's address with the stores
  Element reads and fails the play, naming the CA file and the store, when it does not
  succeed. What Electron honours is a host check the coordinator queues; the play is written so
  that the check decides it (the mechanism is one variable of the play, and the check task is
  the same whichever is chosen), and the builder states which mechanism it chose from what it
  read and why. Element on the homeserver host itself uses loopback and needs no CA (owner
  answer 1). Where the CA's PEM comes from on a machine other than the homeserver host is read
  from the code (`element`, the bundle, `docs/agent-bus-cross-network.md`); a missing channel is
  a finding, never a second distribution path invented here. The play is edited in the
  container and run on the host only through `meta-deploy.bash`. `teamchat-admin element check`
  and `apply` stop saying the CA must be trusted by hand for a team whose CA the play placed.
  Tests: the Go side (what `element check` and `apply` say, with the CA placed and not
  placed); the play passes the project's Ansible gates, and its check task is proven to fail
  on a store without the CA by a fixture. **As built** (`subagent-reports/261106-crossnet-s15-opus-report.md`):
  the play imports `tasks/element-team-ca.yml` as its last step, for each team whose approved
  url is `https`; the variable `agent_bus_element_ca_store` is `nssdb` by default, Chromium's
  NSS database kept in Element's Flatpak data (`flatpak override --user --persist=.pki`), with
  `system` (the host's trust anchors) as the alternative, which makes every program on the host
  trust a CA that has no name constraints; `helpers/element_ca/nss.py` places and checks through
  NSS and is tested against a real TLS server and real NSS stores. Not proven: that Element's
  Chromium honours the store; the host check has to see Element sign in to the team with no
  certificate warning. The order to try if it fails is in the report. Not built: removing a
  team's CA and record when the team is undeclared, a fallback for a runtime with no `python3`,
  and a host run (no v2 `https` team is deployed on any host, so none is queued). The play is
  still v1's profile step (`helpers.agent_bus.element`); the new tasks key on v2's
  `approved.json` and need no change when that step is ported.

## Acceptance

- **X29** On one machine with two addresses: a team declares both networks, a member connects
  through each over TLS with the bundle's CA, and a connection from an address outside the
  declared networks is refused by the filter and by the firewall rule set the render produces.
  Held by `TestX29TwoDeclaredNetworksAreServedAndAnOutsideAddressIsRefused` (`host`): a real apply
  over two interfaces at the fake homeserver's port; the verb `teamchat tls` (`tlsverb.Serve`) run
  on the command line parsed from the rendered `ExecStart=` and a credentials directory made from
  the rendered `LoadCredential=` lines, its terminator `tlsterm` relaying to the fake homeserver
  on `127.0.0.1` at the team's port; and a member that is `matrix.NewMember` with the CA `CAPEM`
  gives the admin and an `https` URL to each derived address, making an authenticated call. Its
  limit: the documentation addresses cannot be bound, so the terminator binds a loopback stand-in
  for each listen address the verb was given and the member's dial (`matrix.Config.Dial`) to
  that address reaches the stand-in; the names on the leaf, the name the member checks, the port,
  the credentials and the trust are the real ones. A client for an address outside the networks
  fails the name check. The terminator's filter and the recorded rules are read for addresses
  inside and outside both networks. The filter is read as
  systemd applies it (an allow lets in, else a deny keeps out, else the address is let in), so
  its `IPAddressDeny=any` is what refuses an outside address, and the socket bind must be
  `SocketBindDeny=any` beside the port's allow; a drop-in without either deny fails X29. The
  refusal is of the unit text and the rules as rendered, not of a live systemd or firewall.
- **X30** The success criterion of Plan 00246: a member on a second machine joins over TLS, its
  credential issued over ssh by X21, and a connection from an undeclared network is refused. The
  second-machine run is a VM-lab or host check the owner's coordinator queues; the code is proven
  by X29 and by the unit and fake tests meanwhile. The run also checks two cases the review of
  pull request 128 asked for. A VPN routed through its own routing table, selected by an `ip rule`: X3 reads the main table only, so as built `team apply` refuses that network as covered
  by the default route alone, and whether that is wanted is a decision still open. And the host's
  own address as a `/32` network: a member on the homeserver host reaches the team through the
  bus address, and an outside prober given a route to it is refused at the firewall and by the
  unit's filter. Plan 00246's `INSTALL-PATH.md` has the lab scenario for both.

## Not in this task

- The v2 launcher and `ccy-teams` (Plan 00209 Tasks 4.1 to 4.4) are unbuilt, so acceptance runs
  at the `seat` and `teamchat` level, not through ccy.
- An overlay network. How a network is routed is not the bus's concern (direction 7).
- Tuwunel's own TLS. If the owner prefers it to the terminator, X18 changes after a recorded host
  check of the pinned binary's TLS keys; nothing else here depends on X18.

## Build order (each slice is test first, one commit or a few)

1. **S1** declare `networks` (X1, X2, X6 shape). 2. **S2** host and hostnet: derive and consume
   (X3, X4). 3. **S3** the LXC source check (X5). 4. **S4** `teamchat/pki` (X13, X14). 5. **S5**
   matrix TLS (X16, X17). 6. **S6** bundle v2 (X12, X15). 7. **S7** the terminator and its unit
   (X18, X19). 8. **S8** seat key and take (X7 to X11). 9. **S9** place-issued (X21 to X23).
2. **S10** site and hostname (X25 to X28). 11. **S11** Element, docs, acceptance (X20, X29).
   S1 before S2 and S3; S4 before S5, S6 and S7; S5 before S6; S6 before S8 and S9; S10 after S1.
   After S11, the owner's slices (section 6): 12. **S12** unattended leaf renewal (X31). 13.
   **S13** a probe through the terminator (X32). 14. **S14** rotating the CA (X33). 15. **S15**
   Element trusts the CA through the play (X34). Then X30, the second-machine run. S12 and S13
   both extend the health tick and S12 reuses apply's leaf step, so they are built together; S14
   follows them (it makes a leaf the way S12 does); S15 is independent of the Go slices.
   **S16** (review blocker B2 of pull request 128, built there): `admin/wire` serves `host.CAPEM`
   as the production `System.TeamCA` and as the seat bundles' CA, so an `https` bundle and an
   LXC reach fact carry the team's CA; a team with none is `ErrTeamCANotMade`
   (`host.ErrCANotMade`). S14's reissue depends on it.

## Owner answers

The coordinator answered all five for the owner (comment on pull request 128): every default
stands, and the owner can still overturn any of them. The owner then confirmed answers 1 and 2
directly, and said that every connection that leaves loopback is TLS.

1. Strict "plain HTTP only on loopback" (X12) puts every local ccy and LXC seat on TLS too, not
   only remote ones. Confirm that is intended; the looser reading (loopback, the bus address and
   the bridge stay plain) keeps `plain_http_hosts` and one more derivation.
   **Answer: confirmed.** Local ccy containers reach the bus address over a podman network that
   other sessions share, so a bearer token sent in plain HTTP there is exposed. One rule
   (`https` unless the host is a loopback literal) also means one tested code path, and
   `plain_http_hosts` and its derivations go away. Element on the host itself keeps using
   loopback, so it needs no CA.
2. TLS by a terminator unit (X18) rather than by Tuwunel: confirm, or ask for Tuwunel's own TLS
   after a recorded check on a host with the pinned binary.
   **Answer: the terminator unit, confirmed.** It keeps the homeserver on loopback only, so the
   admin client, the probe and `ctl` stay unchanged. The leaf key sits under its own uid, and
   renewal is a unit restart. Tuwunel's TLS settings in the pinned binary have not been
   checked. The health-tick problem stays in X18c; the probe through the terminator, left
   unbuilt here, is X32.
3. A network that routes only by the default route is refused (X3): confirm, or say how such a
   member should reach the homeserver.
   **Answer: confirmed.** The homeserver must never listen on the interface that faces the
   internet. A member on a remote network, such as a server on the internet, comes in over a
   VPN or a static route, which gives its network a specific route. The refusal message names
   the CIDR and says exactly that.
4. The CA key is in the team's backup (X13): confirm; leaving it out means a restore reissues
   every bundle.
   **Answer: confirmed.** Without it a restore means reissuing every bundle on every member
   machine, by hand over ssh. The backup already holds the homeserver database with the seat
   credentials, so it is already secret-grade. The condition is written beside X13: the backup
   stays root-only, and a copy taken off the host is encrypted.
5. Issuing is a pipe from the owner's own session (X21), not a root-run ssh: confirm.
   **Answer: confirmed.** A root-run ssh would give root on the homeserver host a credential
   that reaches other machines. With the pipe, sudo is needed only on the homeserver side, and
   the ssh uses the owner's own keys and agent. The secret travels on stdin and never in argv.
   **Revised on 2026-11-09 (X21):** the first real bus host's egress is DNS, NTP and web only, so
   the ssh runs into the bus host from the member machine or the owner's machine, never out of
   it, and its owner allows no standing NOPASSWD, so the sudo password goes in on the ssh's
   stdin (`sudo -S`).

## Owner questions

Found by the S15 builder; X34 is built as specified and neither is decided here.

6. **Which URL does Element use on the homeserver host?** Owner answer 1 says Element on the
   homeserver host uses loopback and needs no CA, but `element.Env.profile` writes the team's
   approved `url` as `base_url`, and for a cross-network team that is `https://<listen address>`, not loopback. Options: (a) the profile points at `http://127.0.0.1:<port>` on the
   homeserver host, so X34 is needed nowhere there; (b) the premise of answer 1 is wrong for
   Element and X34 is what makes its https profile work. **Recommendation: (a)**, since it
   matches the answer and keeps the homeserver host's Element off the CA store; X34 then serves
   the case of question 7 only.
7. **How does the human's Element on another machine get the team CA and a profile?** No
   Element profile exists on any machine but the homeserver host (`element` builds one only for
   a team with an `approved.json`, which only `team approve` writes there), and the CA's only
   path off that host is the seat bundle's `tls_ca`, which reaches the member's agent seat pool
   (X22), not the human. Options: (a) a verb on the homeserver host prints the team CA (a public
   certificate) and the profile's settings, and the human's machine places both with the X34
   play; (b) the CA and profile ride the X21 pipe to that machine; (c) out of scope, the human
   trusts the CA by hand. **Recommendation: (a)**: the CA is not a secret, so it adds no secret
   path, and it reuses X34's check. Until this is answered X30's second machine cannot have a
   working Element, only working agent seats.
