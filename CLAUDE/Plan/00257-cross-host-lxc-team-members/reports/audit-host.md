# Audit: HOST (host-side apply and its inputs)

## Scope

Read in full (production code): `src-go/internal/teamchat/host` (not `host/lxc`), `declare`,
`declare/hostvars`, `hostnet`, `units`, `unitfiles`, `teamlock`, `state`, `status`, `sweep`,
`element`, `notice`, and `playbooks/imports/play-teamchat.yml`. Read in part, only where a
finding needed it: `tlsterm/tlsterm.go`, `health/reach.go`, `backup/units.go`, `paths`,
Plan 00246 `PLAN.md` and `SPEC-cross-network.md`. Not read: the two `tasks/teamchat-*.yml`
files, the test files (beyond their names), and DESIGN-v2-C. Tests run:
`go test` on all eleven packages above, all `ok`. Nothing was run on a host, so every statement
about systemd, firewalld or kernel behaviour is from reading, not from observation. All paths
below are relative to `src-go/internal/teamchat/` unless they start with `playbooks/` or `CLAUDE/`.

Counts: 3 HIGH, 9 MEDIUM, 7 LOW, 4 unconfirmed.

## HIGH

### HOST-1 (HIGH) The health unit's IP filter refuses its own reach probe

- **Where:** `unitfiles/unitfiles.go:157-160` (`IPAddressDeny=any`, `IPAddressAllow=localhost`,
  and the comment "a drop-in may add IPAddressAllow= lines"); `health/reach.go:42-48`;
  `host/reach.go:22-44`.
- **What is wrong:** every health tick dials each team's listen addresses (the bridge address, a
  VPN address) through the terminator (X32). The unit allows loopback only. The drop-in that is
  meant to widen the filter is written by nothing: no Go code and no play task makes it (searched
  `src-go` and `playbooks/`, `tasks/`, `files/usr`). Plan 00246 Task 8.10 records it as not built
  (`CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/PLAN.md:183-186`).
- **Failure scenario:** on any installed host, each tick's probe to a non-loopback listen address
  is dropped by the unit's own filter, so every team carries a permanent team-level problem
  "the TLS terminator does not answer on <addr>". The status record is then never clean, the
  `status` verb never exits clean, and a real terminator outage looks the same as a healthy day.
  After a resume this is the one signal that would say "members cannot reach the bus", and it is
  already red.
- **Fix:** `team apply` and `team remove` render one health drop-in holding every applied team's
  listen addresses as host prefixes (the union of all `units.json` listen sets), reload, and a
  host test asserts the probe address set is inside the allow list.
- **W-number:** none. Known in Plan 00246 Task 8.10, missing from W1-W9.

### HOST-2 (HIGH) A changed terminator base unit or a new `teamchat` binary never reaches a running terminator

- **Where:** `host/render.go:90-99` (the digest sets hold the server base unit, the server
  ExecStart and the config only); `host/apply.go:503-509` (the TLS base unit is only checked to
  exist); `host/tlsunit.go:74` and `:93-100` (restart only when the TLS drop-in's bytes changed or
  the leaf was made); `playbooks/imports/play-teamchat.yml:606-615`.
- **What is wrong:** the play widens the run to every present team when `teamchat-tls@.service`
  changed (its regex names `tls` on line 614) and says the change is thereby applied. `team apply`
  then finds the TLS drop-in unchanged and does nothing to the terminator. The terminator's
  program is `/usr/local/bin/teamchat tls` (`host/tlsunit.go:43`), a link the play re-points on
  every build; the running process keeps the old binary, and the software digest goes only into
  the stamp (`host/apply.go:998`).
- **Failure scenario:** the fixes this audit leads to land in `tlsterm` or in the terminator's
  sandbox. The play runs green, every team shows "applied", and every terminator still runs the
  old code and the old sandbox until a reboot or a leaf renewal restarts it by chance.
- **Fix:** record the terminator's restart set in `units.json` (digest of the TLS base unit, the
  TLS drop-in and the software digest) and restart on a difference, as the server does. Add a test
  that a changed TLS base unit yields "tls unit restarted".
- **W-number:** none.

### HOST-3 (HIGH) A TLS drop-in written but not loaded or restarted is never repaired

- **Where:** `host/tlsunit.go:74-88`, against the rule the same package states at
  `host/apply.go:269-273` ("how the drop-in's bytes compare says nothing of it, as a reload can
  fail or a crash come after the write").
- **What is wrong:** the server path tracks `loaded` from `units.json`. The terminator path
  decides everything from a byte comparison of the drop-in on disk. If the write succeeds and the
  reload or restart does not happen (the play is interrupted, the D-Bus call fails, the process is
  killed), the next apply sees equal bytes: no reload, no restart.
- **Failure scenario A (listen set changed):** every later apply fails at `awaitBound` with
  "bound to X, not Y" for ever; nothing in the message says "restart the terminator".
- **Failure scenario B (only `networks` changed):** `awaitBound` passes, the apply is green, and
  the terminator keeps the old `IPAddressAllow`. A newly approved network is silently refused, or
  a removed one stays allowed at the unit (the firewall rule is gone, so this is the loss of the
  second layer, not an open door).
- **Fix:** same record as HOST-2: compare against what `units.json` says was started, not the file.
- **W-number:** none.

## MEDIUM

### HOST-4 (MEDIUM) Nothing in the host layer handles suspend and resume; a moved address is not re-derived

- **Where:** no `sleep.target`, inhibitor or resume hook in `host`, `unitfiles`, `health` or the
  play (searched). Listen addresses are literals in the terminator's `ExecStart`
  (`host/tlsunit.go:41-48`), in the leaf's names (`host/leaf.go:147-157`), and in `units.json`.
- **What is wrong:** confirms W6, and adds: if the bus host comes back with another address on
  the serving interface (a DHCP lease, a re-made VPN interface), the terminator stays bound to the
  old literal, the leaf names the old literal, the bundles carry the old url, and only a new
  `team apply` re-derives them. The health tick does not re-derive; it probes the recorded
  addresses (`host/reach.go:22-44`) and, per HOST-1, that probe is already failing.
- **What does hold across a resume (read, not run):** lock waits and readiness waits use Go's
  monotonic reading (`teamlock/teamlock.go:66`, `host/apply.go:889-916`); the health timer is
  monotonic (`unitfiles/unitfiles.go:168-169`) so a tick follows within 60 s of resume; the backup
  timer is `Persistent=true` (`backup/units.go:14-16`); a status record written before the suspend
  is shown as expired, never as current (`status/status.go:500-504`); a leaf that expired during
  the suspend is renewed and the terminator restarted by the first tick (`host/leaf.go:244-278`).
- **Fix:** a resume-time unit (`After=suspend.target`, and a boot-time equivalent) that runs the
  health tick at once; the tick compares live interface addresses with `units.json` and reports
  "the host's address moved: run team apply" as its own problem. Require a DNS name or a static
  address for a team's url on a host that suspends.
- **W-number:** W4, W6 (extended).

### HOST-5 (MEDIUM) The `disconnected` scan stops on the homeserver's clock

- **Where:** `notice/notice.go:142-166`, the early return at `:154-155`.
- **What is wrong:** the scan reads main backwards and stops at the first event of any sender
  whose `origin_server_ts` is older than `SinceMS`. `origin_server_ts` is the bus host's wall
  clock; `SinceMS` is the member machine's.
- **Failure scenario:** the bus VM resumes with its wall clock behind (see U-2). Events it stamps
  after resume carry old times. A member's orderly `left` posted then is stamped before
  `SinceMS`, the scan stops without seeing it, and the sweep posts a false `disconnected` after a
  clean goodbye.
- **Fix:** do not stop on a timestamp from another machine's clock; bound the scan by pages only
  and match the run id, or carry the seat's hold as an event id rather than a time.
- **W-number:** none.

### HOST-6 (MEDIUM) A stale terminator is detected by comparing two wall-clock times

- **Where:** `host/leaf.go:208-212` (`servesOldLeaf`), fed by `units/units.go:646-655`
  (`ActiveEnterTimestamp`, realtime).
- **Failure scenario:** the terminator starts while the host clock is ahead (a wrong RTC at boot
  or resume), the clock is then stepped back, and a leaf is renewed. `Since` is after `madeAt`, so
  the terminator is judged current and is never restarted; it serves the old leaf until that
  expires and every member's TLS fails.
- **Fix:** record the served leaf's fingerprint or serial when the terminator is (re)started and
  compare that; or let the reach probe compare the certificate it is shown with the one on disk.
- **W-number:** none.

### HOST-7 (MEDIUM) A network is judged by its first address only

- **Where:** `hostnet/route.go:15-29`; `hostnet/firewall.go:57-58`; used at `host/apply.go:374-406`
  and `:656-666`.
- **Failure scenario:** `networks: [192.0.2.0/23]` on a host with `192.0.2.0/24` on one interface
  and `192.0.3.0/24` routed over a VPN. The route, the listen address and the firewall zone are all
  taken from `192.0.2.0`. Members in the second half arrive on the VPN interface, whose zone has
  no rule. The apply is green.
- **Fix:** refuse a declared network that no single non-default route covers whole.
- **W-number:** none.

### HOST-8 (MEDIUM) A team with a DNS url, no networks and no LXC member passes the refusals and crash-loops its terminator

- **Where:** `host/apply.go:358-369` (listen stays empty), `:428-431` (only an IP literal is held
  to the listen set); `host/tlsunit.go:41-48` (ExecStart with no `--listen`);
  `tlsterm/tlsterm.go:182-183` ("no listen address").
- **Failure scenario:** the homeserver starts and is enabled, then the terminator exits at once
  and `Restart=always` restarts it for ever; the apply fails 60 s later with "not ready".
- **Fix:** refuse an empty listen set at step 2, before any change.
- **W-number:** none.

### HOST-9 (MEDIUM) The approval gate does not cover `service`, `port` or `site`

- **Where:** `declare/gate.go:19` (the gated fields), `:39-45` and `:98-110` (an LXC entry is
  container, checkout, seat, user, human_text).
- **What is wrong:** flipping an LXC member to `service: true` makes root write a session unit
  into the guest that starts an agent session at boot; that is not a gated change. Neither is the
  team's `port` (which moves the firewall rule and the SELinux label) or a member's `site`.
- **Fix:** add `service` to the LXC entry the gate compares, and gate `port`; or record in the
  design why each is exempt.
- **W-number:** none (near W8).

### HOST-10 (MEDIUM) `teamchat_approve_teams` makes the gate self-service

- **Where:** `playbooks/imports/play-teamchat.yml:48-54`, `:670-679`.
- **What is wrong:** the approval list is an inventory variable beside the declaration it
  approves. Whoever can edit host_vars (W8: any agent running as the owner) can change a team's
  networks, url or trust and name the team in the list in the same edit.
- **Fix:** take the list only from the command line of a run the owner starts (`-e`), and refuse
  it when it comes from inventory; or drop it and keep `team approve` under password sudo.
- **W-number:** W8 (the concrete route).

### HOST-11 (MEDIUM) The play does not check `state`, and treats it as optional where Go requires it

- **Where:** `playbooks/imports/play-teamchat.yml:101-102`, `:131-160`; `declare/declare.go:226`.
- **What is wrong:** `tc_present` takes entries with no `state`; Go refuses them ("state is
  required"), but only at the first step that decodes, after the SELinux labels and `team approve`
  have run. An entry with `state: abesnt` is in neither list: when it is the only team, or no
  other team is present, the play is green and does nothing, the class the play itself cites as
  filter-list-unchecked (line 175-177).
- **Fix:** assert in the input check that every entry has `state` of `present` or `absent`, and
  delete the `rejectattr('state', 'defined')` branch.
- **W-number:** none.

### HOST-12 (MEDIUM) A CA rotation is not safe against a crash

- **Where:** `host/rotate.go:156-173` (`commit`), called from `:68`.
- **What is wrong:** four files are replaced one after another. A failed write is rolled back; a
  crash or power loss between the CA key and the CA certificate leaves a pair that does not match,
  which every later apply refuses, with advice to remove both files (which makes a third CA).
- **Fix:** write the new set into a sibling directory and swap one name, or write an intent
  record first that the next run finishes or undoes.
- **W-number:** none.

## LOW

- **HOST-13** `unitfiles/unitfiles.go:131-132` says the approvals stay read-only to the health
  unit. They live in the status tree (`paths/paths.go:338`), which line 146 makes writable. The
  comment is false. `declare/gate.go:566-567` also reads `approved.json` with `ReadFile`, not
  `ReadOwnedFile`; use the owned read as the trust rule says.
- **HOST-14** `team remove` of a team already removed but not purged reloads systemd on every
  play run (`host/remove.go:79-93` still finds `root/<team>/`, `:189-197` reloads unconditionally)
  and reports no change.
- **HOST-15** SELinux port labels are added for present teams and never removed for an absent
  team or an old port (`playbooks/imports/play-teamchat.yml:657-665`).
- **HOST-16** Every deploy leaves a digest-named `teamchat-admin-<tree>` and a kit directory
  behind; nothing prunes them (`playbooks/imports/play-teamchat.yml:383-390`, `:407-423`).
- **HOST-17** The health and backup units lack the cheap hardening lines the team units have
  (`RestrictNamespaces`, `LockPersonality`, `RestrictSUIDSGID`, `SystemCallArchitectures`,
  `ProtectClock`, `ProtectHostname`, `UMask`): `unitfiles/unitfiles.go:137-161`,
  `backup/units.go:28-48`. Both run as root with D-Bus access, so this is depth, not a boundary.
- **HOST-18** `state.WriteFile` leaves its `.name.<random>.tmp` file after a crash and nothing
  sweeps them (`state/state.go:41-52`); one can sit beside a CA key with the key's bytes.
- **HOST-19** Stale comments that contradict the code: `host/render.go:28` (the homeserver binds
  loopback alone), `host/sandbox.go:10-11` and `backup/units.go:8-9` ("to be rendered", "nothing
  installs it": `unitfiles` and the play do), and
  `CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/SPEC-cross-network.md:141` ("no play
  installs any unit yet").

## Unconfirmed

- **U-1 (would be HIGH)** `WatchdogSec=30s` on the homeserver (`unitfiles/unitfiles.go:44`).
  If the VM's monotonic clock jumps forward on resume (a guest left running while its hypervisor
  host sleeps, as opposed to a suspend inside the guest), systemd's watchdog deadline has passed
  before Tuwunel can ping, and systemd may abort every homeserver at once. Not tested; depends on
  how the VM is suspended. Needs a lab run of each suspend mode.
- **U-2 (would be MEDIUM)** After a hypervisor-side pause the guest's wall clock may stay behind
  until time sync steps it, and a default chrony steps only at start. HOST-5 and HOST-6 depend on
  this. The play neither checks nor configures time sync. Not verified on the real VM.
- **U-3** The same monotonic jump would end a running health tick at `TimeoutStartSec=120`
  (`unitfiles/unitfiles.go:140`) mid-work. The lock is a flock and the writes are atomic, so I
  expect no damage; not tested.
- **U-4** Whether the first health tick after resume restarts a homeserver that is only slow.
  That logic is in `health`, outside this area; not read.

## Bloat and debloat candidates

- `teamchat_address` is refused by the play (`play-teamchat.yml:147`) and never passed, so these
  are dead in production: `Input.Address` and the own-host-prefix branch
  (`declare/networks.go:34-41`), the default url (`declare/declare.go:323-328`, `:342-348`), the
  host-prefix branch of `Route` (`hostnet/route.go:16-28`) and `route.Default && !own` for it.
- Three near-identical unit state readers: `units/units.go:599-613`, `:615-657`,
  `units/state.go:31-70`. One reader with the superset of properties would do.
- Three hand-written strict JSON readers: `host/records.go:225-334` (`record`),
  `declare/node.go` (`node`), and `status` (encoding/json plus a re-encode check,
  `status/status.go:168-212`).
- `host.Serve` and `host.ServeLoopback` share all but two lines (`host/serve.go:17-61`).
- `host/apply.go` is 1,023 lines holding the refusals, the network derivation, the firewall, the
  backup start and the records; the firewall (`:599-714`) and the network derivation
  (`:344-467`) are separable files with their own tests.
