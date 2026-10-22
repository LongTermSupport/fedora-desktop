# SPEC: N2 slice n2f, `teamchat-admin`'s production wiring (Plan 00209 Tasks 2.3, 2.4)

Built on n2a to n2e (`SPEC-n2a.md` to `SPEC-n2e.md`). Sources: area C's 3.2.2 (files and
modes), 3.2.5 (network and firewall), 3.3 (the child), 3.4 (locks), 3.5.2 (steps 1, 8, 9), 3.6.2
step 1 (the admin account), 3.7.2 (the bundle), 3.10 (Element), 3.11 (health, the status
record), DC12 and DC17. The research digest behind the gaps is `untracked/scratch/n2f-research.md`
of the slice's worktree (not tracked). Each expectation is numbered and has a Go test written
first and seen to fail.

**What this slice is.** The N2 packages are reached only through seams, and
`cmd/teamchat-admin` wires none. This slice builds the production side of every seam, wires the
verbs whose packages had no caller (`health`, `element`, the announcer), and makes `main` build
the real `System`, so the binary works on a host once installed.

**What it is not (owner gates).** Gate A: no play, task, unit file, tmpfiles entry,
meta-deploy entry or anything that installs a `teamchat` name or path. Gate B: nothing gives a
process a pool seat's password; the code that would do so at run time is the bundle of CV3, and
it reaches a host only with the install. v1 is untouched; ccy-teams' side (host_vars, `seats sweep`) is not this slice.

**Trust rule for every adapter.** Root never decides from a file a member or the owner can
write. Every file root or the child reads is opened `O_RDONLY|O_NOFOLLOW|O_NONBLOCK|O_CLOEXEC`
(so a FIFO or a device never blocks the open), `fstat`ed, and refused unless it is a regular
file; its size is capped (`state.ReadFile` is the one reader). Every external program runs
through `execx`, with no secret in argv.

The packages:

- `internal/execx`: a credential and a replaced environment (EX).
- `internal/teamchat/admin`: the child's start (CS), its two new operations (CO), the bundle
  (BU), the owner group, the pin and the Fedora check (HF), the `health` and `element` verbs
  (VB), and `Env.Wire` (WR).
- `internal/teamchat/ctl`: the child's own files: the admin token, bootstrap step 1 and the
  `Opener` (OP).
- `internal/teamchat/units`: systemd over D-Bus with `github.com/godbus/dbus/v5` (SD).
- `internal/teamchat/hostnet`: the network, firewalld and the loopback probes (NW, FW, RD).
- `internal/teamchat/health`: `Refresh` under a held lock and the production adapters (HR).
- `internal/teamchat/admin/wire`: the production `System`, built from the adapters (WR).

## 1. `execx`: a credential and a replaced environment (EX)

- **EX1.** `Cmd.Credential` (`*syscall.Credential`, nil for none) runs the child as that uid,
  gid and exact supplementary group list (`NoSetGroups` false, so an empty list clears them).
- **EX2.** `Cmd.CleanEnv` true makes `Env` the child's whole environment: nothing of the
  parent's is inherited, `Unset` is refused beside it (a contradiction), and an empty `Env` is
  an empty environment.
- **EX3.** The `Fake` records both, so a test asserts the uid, gid, groups and environment of a
  child without starting one. Neither changes any existing caller's command.

## 2. The child's start (CS, C 3.3, n2a-D6)

`admin.ExecChild{Runner, Self, Lookup}` is the production `Child`.

- **CS1.** Each `Ask` looks up `paths.CtlUser`; no such account is a config failure (78) naming
  the install, and a root uid or gid is refused (`NewChildSpec`). It runs `Self ctl-child` with
  the spec's credential (EX1, no groups), `/` as its directory and `PATH=/usr/bin:/bin` alone
  (EX2), the request on stdin and nothing else inherited.
- **CS2.** `Self` is the running binary, `/proc/self/exe` resolved once at start (the versioned
  file the link swap of C 3.4 names), absolute and clean, so the child is the same build.
- **CS3.** The answer is read from stdout through a writer that stops at `maxAnswer`+1 bytes: a
  larger answer is refused without reading the rest into memory. Stderr is capped the same way
  and shown only as an explanation.
- **CS4.** The child's exit code is never passed through (CC6). Exit 0 with an `ok` answer, or
  exit 1 with a failed answer, is the answer; anything else (a signal, another code, an answer
  that does not decode, exit 0 with a failed answer or 1 with an `ok` one) is `ErrChild`
  wrapped with what happened, which the verbs report as 1 and the health tick as
  `health.ErrViewChild`.
- **CS5.** A context that ends stops the child (execx's TERM, then KILL after the grace) and is
  the context's error.

## 3. The child's new operations (CO, C 3.11.2)

- **CO1. `team.view`**, args `{team_file}`: the child loads its roster file and rooms record,
  runs `view.Read` as the admin, and answers `ok` with a `view` object
  `{"members": [...], "humans": [...], "problems": [...]}` in the status record's own
  encoding (C 3.11.3). A view's file failure is class `config`, the homeserver's is
  `unreachable` for `matrix.ErrUnreachable` and `ErrRateLimited`, else `failed`.
- **CO2. `member.disconnected`**, args `{team_file, handle, run, since_ms}`: the child reads the
  guest token copy `paths.GuestTokenCopy` (CS's trust rule; missing is a `notice` with
  `posted` false and why "the member has no token copy"), its rooms record's main room, and runs
  `notice.Disconnected` with a member client on `http://127.0.0.1:<port>` and that token. It
  answers `ok` with `notice` `{"posted": bool, "why": "<line>"}`; a transient failure (NT2) is
  class `unreachable`.
- **CO3. Root decodes both strictly** (CC6's rules): `view` and `notice` only with `ok`, only
  for their op, exact keys, every string one line; a `view` decodes into `status.Member`,
  `status.Human` and `status.Problem` and must make a record that `status.Encode` accepts (SR1's
  value rules, so a membership word, a kind or a role the record cannot hold is refused before
  anything is written). An answer with a `view` or `notice` for another op is refused.
- **CO4. Root's adapters.** `admin.ChildViewer` (health.Viewer) asks `team.view` with the
  applied `team.json` root reads; a missing or unreadable `team.json` wraps
  `health.ErrViewFiles`, a child failure or a refused answer `health.ErrViewChild`, a `config`
  answer `ErrViewFiles`, an `unreachable` one `matrix.ErrUnreachable`.
  `admin.ChildAnnouncer` (health.Announcer) asks `member.disconnected`; an `unreachable`
  answer, or a child that failed, is the error the tick retries, and `posted` false is a
  `health.Notice` with its why.

## 4. The child's own files and the Opener (OP, C 3.2.2, 3.6.2 step 1)

`ctl.Open(ctx, ctl.Config, team, port, regKey)` is the production `admin.Opener`; `Config` holds
the state root (`paths.CtlStateRoot` under a test root), the clock and the random source.

- **OP1. The client.** The admin client is `matrix.NewAdmin` on `http://127.0.0.1:<port>`, the
  port from the request's `team.json` (the child never reads root's files). `Opened` carries
  `apply.Deps` (`RosterPath`, `RoomsPath`, `GuestDir` under `ctl/<team>/`, the clock) and a
  member-client maker for CO2.
- **OP2. The token.** `admin.token` is read by the trust rule: at most 4096 printable ASCII
  bytes without a space, else refused (78) without quoting it. For every op but `team.apply`,
  a missing token is 78: "team <t> has no admin token: run team apply".
- **OP3. Bootstrap step 1, only with a registration key (`team.apply`).** Token present:
  `whoami` must answer `@admin:<server_name>`, else refused (70). Absent and `admin.password`
  present: log in as `admin` with it on the device `TCADMIN`, store the token, delete the
  password. Both absent: write a fresh password (32 random bytes as 64 hex), register `@admin`
  as a server admin with the registration key (M27's nonce and HMAC), log in, store the token,
  delete the password. A register answered "user in use" with no password file is refused
  (70): the token is lost while the account exists, so the way back is `restore`.
- **OP4. Link, not replace.** The password and the token are each written to a fresh temporary
  file in `ctl/<team>/` (0600, `O_EXCL|O_NOFOLLOW`), synced, linked to their name (an existing
  name fails, never replaced), the temporary unlinked and the directory synced. `ctl/<team>/`
  must be a directory the child's euid owns, mode 0700, opened without following a link.
- **OP5. Nothing secret escapes.** No error, CHANGED line or log holds the password, the token
  or the registration key; the step answers `admin registered` or `admin token stored` lines
  through `apply.Result` only by the bootstrap's own lines (OP3 prints none of its own).
- **OP6.** The `Opener` signature gains the port: `Opener(ctx, team, port, regKey)`, and
  returns `admin.Opened`. The view and the announcer use the same `Opener`.

## 5. The bundle, the owner group, the pin and the Fedora check (BU, HF)

- **BU1. The main room travels in the answer.** An `ok` answer that carries a credential also
  carries `main_room` (`apply.Result.Rooms.Main`), a valid room ID; root refuses an answer with
  one and no credential, or a credential and none (CV3's check). Root never reads `ctl/`.
- **BU2. `admin.SeatBundles`** (the production `Bundles`) builds `seat.Issued`: `seat.json`'s
  fields from the applied `team.json` (`team`, `server_name`, `base_url` = `Team.URL`,
  `plain_http_hosts` = the URL's host when it is an IP literal, else none), the handle, the
  admin id, the main room, `device_id` `TCSEAT`, `issued_ms` from the clock and `issuer` the
  build's digest; and the credential. It is encoded by `seat.Issued.Encode`, which refuses a
  bundle `seat.DecodeBundle` would refuse.
- **HF1. The owner group.** `paths.OwnerGroupFile` is read by the trust rule and must be owned
  by root and not writable by group or other; its content is one group name and at most one
  newline; the group is looked up by name. Missing, wrong or unknown is 78.
- **HF2. The pin.** `paths.ServerPinFile` (root-owned, not group or other writable) is strict
  JSON `{"v": 1, "version": "<x.y.z>", "memory_high": "<size>", "cache_capacity_modifier": "<decimal>"}`; it is `host.Pin`, and render's own checks judge its values. Missing or wrong is
  78 (n2f-D3).
- **HF3. Fedora.** `team apply` refuses (78) unless `/etc/os-release` (under the test root) has
  `ID=fedora` (quotes allowed) (C 3.5.2 step 1).

## 6. systemd over D-Bus (SD, DC17)

`units.Manager` holds one system-bus connection, opened on first use (so a verb that needs no
systemd never connects) and closed by `Close`. `Manager.Host()` is `host.Systemd` and
`Manager.Health()` is `health.Units`.

- **SD1. Jobs.** `Start`, `Restart` and `Stop` call the Manager's `StartUnit`, `RestartUnit` and
  `StopUnit` with mode `replace`, having subscribed (`Subscribe`) and matched `JobRemoved` before
  the call, and return when the job's `JobRemoved` arrives: `done` is success; `failed`,
  `timeout`, `canceled`, `dependency`, `skipped` or any other result is an error naming the
  unit, the verb and the result (n2e-D13). A context that ends first is its error.
- **SD2. Reload.** `DaemonReload` is `Reload`, which systemd answers when it is done.
- **SD3. Enable and disable.** `EnableUnitFiles([unit], false, false)` and
  `DisableUnitFiles([unit], false)`, then `Reload`.
- **SD4. State (host).** `LoadUnit` then the unit's `ActiveState` and `UnitFileState`; enabled
  is `enabled` or `enabled-runtime`. A unit that is not loaded is `inactive`, not enabled.
- **SD5. State (health).** `ActiveState`, `SubState`, `InvocationID` (16 bytes, as 32 hex),
  `NRestarts` (the Service interface; a service has it, H 7.24) and `ActiveEnterTimestamp`
  (microseconds; 0 is the zero time). A property of the wrong D-Bus type is an error, never a
  zero.
- **SD6. Clean and journal.** `CleanState` and `JournalTail` are `systemctl clean --what=state`
  and `journalctl -u <unit> -n 20 --no-pager -o cat` through `execx` (the tail is only shown).
- **SD7. Error mapping.** A D-Bus error is one line naming the call, the unit and the error's
  class: `NoSuchUnit`, `AccessDenied` and `InteractiveAuthorizationRequired` ("not permitted"),
  a bus that cannot be reached or a lost connection ("systemd is not reachable on the system
  bus", and the connection is dropped so the next call dials again), `UnitMasked`, and any other
  error by its D-Bus name. A string the bus returned is printed only when it is one line of at
  most 256 bytes; a body is never printed whole.
- **SD8. Unit names.** Every call's unit is a `teamchat-server@<team>.service` name built by
  `paths`, checked again here against systemd's unit-name characters before any call.
- **SD9. Tests.** Unit tests over a fake connection cover every call, signal order, result and
  error class; a real test starts a scratch `dbus-daemon` (installed in the container, never
  installed by this slice) and drives the adapter against a fake `org.freedesktop.systemd1`
  exported on it, so the method names, signatures, signal match and error names are the wire's.
  godbus is vendored with its go-deps allowlist row; the YAML `keepout` row stays valid.

## 7. The network, firewalld and the loopback probes (NW, FW, RD)

- **NW1. Interfaces** are `net.Interfaces` and their addresses, each as a `netip.Addr` without
  zone.
- **NW2. Listeners** are `/proc/net/tcp` and `/proc/net/tcp6` read in-process (C 3.2.5): state
  `0A`, the address and port from the fixed hex columns, the socket inode; a line that does not
  parse fails the read.
- **NW3. A listener's unit** is found from the inode: the `/proc/<pid>/fd/*` link that reads
  `socket:[<inode>]`, then that process's `/proc/<pid>/cgroup` v2 line `0::<path>`, whose last
  element is the unit when it ends `.service`; none found is `""`. A process that ends during
  the walk is skipped.
- **NW4. The bridge** is `lxcbr0`'s first IPv4 address and prefix, masked; no such interface or
  no IPv4 address is an error naming it (render fails).
- **FW1. The zone of a source** is the zone of the interface whose route covers the source's
  address, from `/proc/net/route` and `/proc/net/ipv6_route` (longest prefix, then lowest
  metric), asked as `firewall-cmd --get-zone-of-interface=<dev>`: exit 0 is its one-word zone,
  exit 2 (firewalld's no zone) or no route is `firewall-cmd --get-default-zone`. Any other exit
  is an error naming the code. A zone name is `^[A-Za-z0-9_-]{1,32}$`.
- **FW2. Query, add, remove** are `firewall-cmd [--permanent] --zone=<z> --query-rich-rule=<rule>`, `--add-rich-rule=` and `--remove-rich-rule=`, with the rule as one
  argument; query 0 is present, 1 absent, anything else an error naming the code (C 3.2.5).
- **RD1. Readiness** (host step 8) is `GET http://127.0.0.1:<port>/_tuwunel/server_version`
  answering 200 within 5 s; **RD2. the probe** (health step 2) is
  `GET http://127.0.0.1:<port>/_matrix/client/versions` answering 200 within 5 s, the port from
  the applied `team.json`. Neither follows a redirect or uses a proxy, and each reads at most
  64 KiB of the body.

## 8. Health in production (HR, C 3.11)

- **HR1. `Tick.Refresh(ctx, team)`** is `host.Status.Refresh`: steps 1 to 5 for one stamped
  team with the caller already holding its team lock, so it neither takes the lock nor restarts
  the unit, and writes the record only if no other writer wrote since it read it. A team with
  no stamp is an error.
- **HR2. `health.Stamps`** (health.Teams) lists the teams under `paths.StatusRoot/teams/` with a
  stamp (host's stamp decoder), the name from `team.json`, the origin `migration` when
  `paths.MigrationDir(t)/state.json` exists, else `v2`.
- **HR3. `host.Status.Remove`** deletes `status.json` and `lxc-online.json`; a record whose
  origin is `migration` is refused (D's migrate verbs keep its pointer and are not built).
- **HR4. Backups** (C 3.12, not built) answer no backup and the last run `none`, so the record
  says the backup has not run 36 h after the apply (n2f-D6).
- **HR5. Guests** (C 3.9.2, not built) fail every probe with "the LXC guest probe is not built
  yet", which the tick records for that member (n2f-D7).

## 9. The verbs (VB)

- **VB1. `health --all`** (the timer's) runs `RunAll` and prints one line per team on stderr:
  `<team> written`, `<team> dropped: <why>` or `<team> failed: <error>`. It exits 0 when no
  team failed, else 1; a team dropped because its lock was busy is not a failure (C 3.4).
  **VB2. `health <team>`** ticks that one team the same way; neither form and anything else is
  64\.
- **VB3. `element check|apply`** run `element.Check` and `element.Apply` for the real uid, its
  home from the account database (never `$HOME`), the approvals from `approved.json` and the
  names from the status records. **VB4.** `element apply --consent` records the owner's consent
  (`element.WriteConsent`) first; the play asks the owner and passes the flag (n2f-D8).
  Root is refused (EL5). A failure is 1, a refused input 64.
- **VB5. `team apply`** checks HF3 first.

## 10. The wiring (WR)

- **WR1. `Env.Wire`** builds the `System` when `Env.Sys` is nil, at the first verb that needs
  one: `version` and a verb of the wrong class never build it. A wiring that fails is 78.
- **WR2. `wire.New(digest)`** is the production wiring: host paths (`Root` ""), `LockDir`
  `paths.AdminRunDir`, the system clock, `crypto/rand`, the pin (HF2), `Software` the digest,
  `teamchat-ctl`'s ids, `FileApprovals`, `units.Manager`, `hostnet`, the health tick for
  `Status`, the backup refused (C 3.12 not built: step 6 fails `backup` for a team whose restart
  set changes and that has a database), `ExecChild`, `SeatBundles`, the owner group (HF1) and
  `ctl.Open`; for the child, `ctl.Open` alone.
- **WR3. `main`** reads the environment, calls `run` with `wire.New`, and exits with its code;
  `run` is tested, `main` is not.
- **WR4.** The YAML module stays out of `teamchat-admin` (the keepout row), and godbus is in it.

## 11. Decisions where the design is silent

| ID      | Decision                                                                                                                                                          |
| ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| n2f-D1  | The child's answer carries `main_room` beside a credential (BU1): root parses only the answer, never a `ctl/` file.                                               |
| n2f-D2  | The admin device is `TCADMIN`, beside the seats' `TCSEAT`.                                                                                                        |
| n2f-D3  | The pin file is strict JSON written by the play (format silent in C); its values are render's to judge.                                                           |
| n2f-D4  | `CleanState` and `JournalTail` stay `systemctl` and `journalctl` through `execx`; every unit job and state is D-Bus.                                              |
| n2f-D5  | firewalld's no zone is exit 2 of `--get-zone-of-interface`; H-C6 confirms it on a host. The route is read from `/proc/net/route` and `ipv6_route`, not from `ip`. |
| n2f-D6  | Backups answer none until C 3.12 is built, and `team apply`'s step 6 refuses a backup it would need.                                                              |
| n2f-D7  | The LXC guest probe fails until C 3.9.2 is built.                                                                                                                 |
| n2f-D8  | Consent is recorded by `element apply --consent`.                                                                                                                 |
| n2f-D9  | `health` takes `--all` or one team; a failed team is exit 1, a busy one is not.                                                                                   |
| n2f-D10 | The child's ops are named `team.view` and `member.disconnected`.                                                                                                  |
