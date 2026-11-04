# Plan 00209 research: agent-bus HOST side, requirements and hard-won behaviour

Scope: what runs on the homeserver host (admin tool, root wrapper, installer, units, play,
seat commands, health, Element profile, host_vars editor). Member side (pingbus) only where
the host depends on it. Citations are `file:line` in this worktree; `SPEC-abi` = the Go-port
spec on branch `go-port/agent-bus-install`
(`CLAUDE/Plan/00192-.../SPEC-agent-bus-install.md`, its own line numbers). INFERENCE marks
anything not read directly in code.

Short paths: `cli` = helpers/agent_bus/cli.py, `admin` = helpers/agent_bus/admin.py,
`reg` = helpers/agent_bus/registry.py, `co` = helpers/agent_bus/checkout.py,
`tf` = helpers/agent_bus/teamfile.py, `rnd` = helpers/agent_bus/render.py,
`hl` = helpers/agent_bus/health.py, `inst` = files/usr/local/sbin/agent-bus-install,
`wrap` = files/usr/local/bin/agent-bus, `play` = playbooks/imports/play-agent-bus.yml,
`DES` = CLAUDE/Plan/00161-agent-team-bus-matrix/DESIGN.md, `gab` = src-go/internal/agentbus/agentbus.go.

---

## 1. Commands

### 1.1 Exit-code contract (shared by every caller)

- Admin tool: 0 ok, 64 usage, 69 homeserver unreachable, 70 refused (by HS or by state), 75 busy
  (seat held, allocation lock, `inbox.Busy`), 78 config (team file, team dir, secrets,
  registry, checkout) (`cli:14-16, 35-40, 411-433`).
- Wrapper adds 64 (bad `--out`, no sudo env) and 77 (admin cmd not as root / seat|health as
  root) (`wrap:29-38`).
- Installer: 0, 1 step failed, 64, 77, 78 (`inst:20-21`); BUT under `set -e` children leak their
  own codes (curl 77/78 collide with 77/78) - SPEC-abi bug B3.
- Go maps codes to kinds in one place (`gab:24-52`): 64 usage, 69 unreachable, 70 refused,
  75 busy, 77 not-root, 78 config, else failed. The play and systemd only test non-zero.

### 1.2 Admin tool `agent-bus` (zipapp `/usr/local/lib/agent-bus/agent-bus.pyz`)

Root-side commands run as root through `wrap`, which drops to user `agent-bus` with
`runuser -u agent-bus -- /usr/bin/python3 -I <pyz>` after `cd /` (`wrap:55-58`). Why: the
team's secrets (`/var/lib/agent-bus/<team>/`, 0700) belong to `agent-bus`, a user no agent runs
as (DES D8, `DES:400-416`); `load_team` refuses a team dir not owned by the euid or not 0700
(`admin:172-196`). Values are `--opt=value` only; nothing prompts (`cli:5-8`).

| Command                                                                                  | Args                         | stdout payload                                                                                                  | Callers                                                                                                                                                                         | Notes                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------- | ---------------------------- | --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `bootstrap TEAM`                                                                         | -                            | `CHANGED\t<what>` lines                                                                                         | installer `team` (`inst:831`) and `restore` (`inst:1096`)                                                                                                                       | idempotent; section 3 below                                                                                                                                                  |
| `add-member TEAM --repo= [--seat=] --host= --type= --role= --address= [--no-human-text]` | wrapper adds `--out=DIR\|-`  | bundle tar (refused on a tty, `cli:73-76`)                                                                      | `seat take` via `sudo [-n] /usr/local/bin/agent-bus add-member ... --out=-` (`co:244-249, 504-510`); LXC helper (`helpers/agent_bus_lxc/members.py:17`); human with `--out=DIR` | `--host` always required because sudo `env_reset` drops the role var (`DES:112-114`)                                                                                         |
| `park-member TEAM HANDLE`                                                                | -                            | CHANGED                                                                                                         | `seat remove` (`co:423`); ccy-teams `ParkMember` (`gab:188-190`); LXC helper                                                                                                    | revoke then mark parked (D51)                                                                                                                                                |
| `remove-member TEAM HANDLE`                                                              | -                            | CHANGED                                                                                                         | human only (no Go caller found)                                                                                                                                                 | retire for ever                                                                                                                                                              |
| `set-role TEAM HANDLE --role=`                                                           | -                            | CHANGED                                                                                                         | ccy-teams `SetRole` (`gab:194-199`, validates role before sudo)                                                                                                                 | one coordinator per team                                                                                                                                                     |
| `rotate-token TEAM HANDLE`                                                               | wrapper requires `--out=DIR` | one-file tar `token`                                                                                            | human only                                                                                                                                                                      | refuses parked                                                                                                                                                               |
| `list TEAM`                                                                              | -                            | `HUMAN\t<name>\t<membership\|->`, `MEMBER\t<handle>\t<role>\t<membership\|->\tactive\|parked` (`admin:853-864`) | ccy-teams `List` (`gab:175-185`); `seat choose` via `sudo -n` (`co:902-917`)                                                                                                    | NOPASSWD via sudoers                                                                                                                                                         |
| `human password TEAM NAME`                                                               | -                            | the password, one line, printed once                                                                            | ccy-teams login (`gab:217-227`)                                                                                                                                                 | never stored; logs out other sessions; stderr also prints sign-in limits (`cli:134-138`)                                                                                     |
| `human devices TEAM NAME`                                                                | -                            | `DEVICE\t<id>\t<last_seen_ms\|->` (`admin:901-917`)                                                             | ccy-teams (`gab:203-213`)                                                                                                                                                       | NOPASSWD; device ids regex-checked, client-chosen names never printed                                                                                                        |
| `human logout-all\|lock\|unlock TEAM NAME`                                               | -                            | nothing (stderr says)                                                                                           | human                                                                                                                                                                           | logout-all = password reset to a discarded one (`admin:889-893`)                                                                                                             |
| `render check [--previous=TEAM_JSON]`                                                    | team file on stdin           | canonical team.json                                                                                             | installer (`inst:775-779`)                                                                                                                                                      | stdin because agent-bus cannot read root's tmp copy (`DES:515-517`); `--previous` named but missing is an error, else the server_name guard would be skipped (`cli:163-170`) |
| `render toml`                                                                            | stdin                        | tuwunel.toml                                                                                                    | installer                                                                                                                                                                       |                                                                                                                                                                              |
| `render dropin --interface=ADDR=IFACE...`                                                | stdin                        | network.conf drop-in                                                                                            | installer                                                                                                                                                                       | iface facts are the installer's                                                                                                                                              |
| `render stamp --zipapp --tuwunel --installer-sha256`                                     | stdin                        | stamp JSON                                                                                                      | installer last step (`inst:838-841`)                                                                                                                                            |                                                                                                                                                                              |
| `render stamp-group --file --owner`                                                      | -                            | group name                                                                                                      | installer start of `team` (`inst:763`)                                                                                                                                          |                                                                                                                                                                              |
| `version`                                                                                | -                            | `agent-bus 0.1.0 protocol <v>`                                                                                  | -                                                                                                                                                                               |                                                                                                                                                                              |
| `seat check\|choose\|take\|list\|remove`                                                 | see 1.4                      |                                                                                                                 | desktop user, never root                                                                                                                                                        |                                                                                                                                                                              |
| `health status [TEAM...]` / `health check --team`                                        | see 1.5                      |                                                                                                                 |                                                                                                                                                                                 |                                                                                                                                                                              |

### 1.3 Installer `agent-bus-install` (root)

Commands, options, callers (`inst:107-135`, dispatch `inst:1162-1180`; SPEC-abi 1.1 table):

| Command                                    | Callers                                                                                                                                              | stdout                                         |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `software --source DIR [--bus-address IP]` | play every run (`play:182-189`)                                                                                                                      | CHANGED                                        |
| `team --team-file FILE`                    | play per present team (`play:320-331`)                                                                                                               | CHANGED (+ bootstrap's CHANGED passed through) |
| `remove --team T [--purge]`                | play per absent team (`play:260-270`); ccy-teams `RemoveTeam` (`gab:254-260`)                                                                        | CHANGED                                        |
| `backup-now --team T`                      | `agent-bus-backup@.service` daily (`files/etc/systemd/system/agent-bus-backup@.service`); play before a swap (`play:168-180`); ccy-teams `BackupNow` | CHANGED                                        |
| `backups --team T`                         | ccy-teams `Backups` (`gab:230-240`)                                                                                                                  | `BACKUP\t<id>\t<YYYY-MM-DD HH:MM UTC>`         |
| `restore --team T --backup ID`             | ccy-teams `Restore`                                                                                                                                  | CHANGED                                        |
| `check --team T`                           | human / other IaC, acceptance                                                                                                                        | `CHECK\t<fact>\tok\|FAIL\|info\t<value>`       |

Root is required because it writes /etc/systemd, firewalld, NetworkManager, /usr/local, other
users' dirs. ccy-teams reaches it only with the user's own password sudo (`sudo -v` then
`sudo -n`), NOT through the sudoers rule (SPEC-abi:11-17; the comment `src-go/internal/sudo/sudo.go:19`
"The two root-owned programs the sudoers rule covers" is wrong, bug B1).

### 1.4 Seat commands (run as the desktop user, in the checkout; wrapper refuses them as root, 77, `wrap:153-160`)

Why not root: "root never writes in a user's checkout" (`wrap:18-20`, D61). They reach root only
through `sudo agent-bus add-member|park-member|list`.

- `seat check LIST [--resolved]`: prints the canonical list; every team must be running here
  (waits for a starting HS, below). Caller: the ccy launcher, bash
  (`files/var/local/claude-yolo/lib/agent-bus-seats.bash:6-59`). `--resolved` forbids a bare
  team (no seat yet).
- `seat choose TEAM [--type=podman|host]`: `CHOICE\t<seat>\tfree|held|new\t<handle>\t<type words>`
  then `DEFAULT\t<seat>` (`co:920-943`). Caller ccy-teams `SeatChoose*` (`gab:128-166`). Reads
  members with `sudo -n agent-bus list` (NOPASSWD rule).
- `seat take LIST [--no-prompt] [--type] [--alloc-held]`: emits `CHANGED\tseat <seat>@<team> <handle>`
  per created seat, flushed per line (`cli:215-225`, `co:899`). Callers: ccy launcher, ccy-teams
  `SeatTake`/`SeatTakeAllocated` (`gab:141-149`). `--no-prompt` = `sudo -n` (headless launch).
- `seat list`: `SEAT\t...` lines (table on a tty); exit 78 if some seat unreadable but still
  prints the others (`cli:228-234`); Go treats 78 there as "partial" (`gab:107-124`).
- `seat remove LIST`: parks, deletes dir, prunes empty tree; may name several seats of one team.
  Caller ccy-teams `SeatRemove`.
- Every take/remove ends with `sudo -k` in a `finally` (security audit H1, `co:444-446, 513-524, 836-852, 973-981`): the next session on the terminal must not inherit the cached sudo.
  A failing `sudo -k` stops the launch.

### 1.5 Health

- `health check --team T`: root, from `agent-bus-hs-health@.timer` every 60 s (OnActiveSec=30s),
  run as `/usr/bin/python3 -I <pyz> health check --team %i` DIRECTLY, not via the wrapper
  (`files/etc/systemd/system/agent-bus-hs-health@.service`), sandboxed: ProtectSystem=strict,
  ReadWritePaths only `/var/lib/agent-bus-applied`, CapabilityBoundingSet
  `CAP_DAC_READ_SEARCH CAP_CHOWN` (read agent-bus's and root's 0700 dirs, chgrp the record),
  IPAddressAllow=localhost. Logs to stderr (journal). Failure fails the unit; stale record is
  the signal.
- `health status [TEAM...]`: desktop user via wrapper (refused as root, `wrap:155`), prints per
  team `homeserver <state>`, newest backup, `PROBLEM <what>` / `  do: <fix>`, sign-in limits;
  exit 1 on any problem (`hl:232-258, 411-424`). ccy-teams reads the JSON record itself
  (src-go/internal/teams/board/health.go).

### 1.6 Other helpers (Python, called by play or ccy-teams)

- `helpers/agent_bus/bridge.py --teams JSON` (play, root): `{"note","teams"}` with each team's
  allow_from + live `lxcbr0` IPv4 subnet (`bridge.py:46-86`). No bridge = note only; bridge
  without IPv4 = exit 1.
- `config_restart --state-root /var/lib/agent-bus TEAM...` (play, root, also in --check):
  `CONFIG-CHANGED <team>` when this checkout renders tuwunel.toml differently
  (`config_restart.py:200-236`).
- `timer_state` (play, root): `systemctl clean --what=state` on `stamp-agent-bus-*@<team>.timer`
  of teams with no applied stamp; `CHANGED` per cleared (`timer_state.py:135-162`).
- `element check|apply` (play-agent-bus-element, as the desktop user, JSON on stdin): Element
  Flatpak profile + launcher; exit 78 on refusal (`element.py:1-23`).
- `declare <sub>` (ccy-teams Go, desktop user, `python3 -P -m helpers.agent_bus.declare`,
  `src-go/internal/declare/declare.go:583`): ~25 subcommands that read/edit `agent_bus_teams`,
  `agent_bus_element_teams`, `agent_bus_lxc_members`, `agent_bus_address` in the untracked
  host_vars by line-parsing a YAML subset (`declare.py:1-140`), plus `status`/`overview`
  (applied-vs-declared verdict, D65). Exit 0/64/78/130.

---

## 2. On-disk files

| Path                                                                                                                                                                                       | Owner:group mode                                              | Format                                                                                                                                                                      | Written by (how)                                                                                                                                                                                        | Readers                                                                                                                                           |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/var/lib/agent-bus/`                                                                                                                                                                      | agent-bus 0700                                                | dir                                                                                                                                                                         | installer `software`/`team` (`inst:485, 800`)                                                                                                                                                           | homeserver (bind-mounted own team only)                                                                                                           |
| `/var/lib/agent-bus/<t>/` and `db/ backups/ secrets/`                                                                                                                                      | agent-bus 0700                                                | dirs                                                                                                                                                                        | installer `team` (`inst:801-805`)                                                                                                                                                                       | HS, admin tool (must be euid-owned 0700, `admin:177-187`)                                                                                         |
| `<t>/tuwunel.toml`                                                                                                                                                                         | root:agent-bus 0640                                           | TOML, bare top-level keys + `[rate_limiting.login.*]` tables, header comment (`rnd:140-149`)                                                                                | installer `put_file` (tmp+rename)                                                                                                                                                                       | HS (`TUWUNEL_CONFIG`), config_restart                                                                                                             |
| `<t>/team.json`                                                                                                                                                                            | root:agent-bus 0640                                           | canonical team file JSON, `indent=2, sort_keys` (`tf:388-390`)                                                                                                              | installer, restore                                                                                                                                                                                      | admin tool, installer (`jq .port/.listen`), health (`port`), other teams' port check                                                              |
| `<t>/registry.json`                                                                                                                                                                        | agent-bus 0600                                                | `{"v":2,"team","counters":{prefix:n},"members":{handle:role},"parked":[sorted]}` exact key set per version; v1 (no `parked`) loads as v2 (`reg:39-42, 232-239, 253-282`)    | admin tool `update_registry` only: flock on sibling `registry.lock` (0600, `O_CLOEXEC`), load, change, save only if different; save = mkstemp + fsync + chmod 0600 + rename + dir fsync (`reg:306-345`) | admin tool; backups                                                                                                                               |
| `<t>/registry.lock`                                                                                                                                                                        | agent-bus 0600                                                | empty flock target                                                                                                                                                          | admin tool                                                                                                                                                                                              |                                                                                                                                                   |
| `<t>/room_id`                                                                                                                                                                              | agent-bus 0600                                                | one printable line, room id                                                                                                                                                 | `bootstrap` once (`admin:579`) via `write_private` (mkstemp, fsync, `os.link` so never overwrites, dir fsync, `admin:199-219`)                                                                          | admin tool                                                                                                                                        |
| `<t>/secrets/registration_shared_secret`                                                                                                                                                   | agent-bus 0600                                                | 128 lowercase hex, NO newline                                                                                                                                               | installer once, never repaired (`inst:721-731`; B7)                                                                                                                                                     | HS (`registration_shared_secret_file`), admin `_register_admin`                                                                                   |
| `<t>/secrets/admin.token`                                                                                                                                                                  | agent-bus 0600                                                | one line                                                                                                                                                                    | bootstrap (`write_private`, link-not-replace)                                                                                                                                                           | admin `_session`                                                                                                                                  |
| `<t>/secrets/admin.password`                                                                                                                                                               | agent-bus 0600                                                | exists only between register and token stored                                                                                                                               | bootstrap                                                                                                                                                                                               | bootstrap recovery                                                                                                                                |
| `<t>/backups/meta/<n>`                                                                                                                                                                     | Tuwunel                                                       | numbered RocksDB backups, 7 kept                                                                                                                                            | HS on SIGUSR2                                                                                                                                                                                           | installer, health                                                                                                                                 |
| `/var/lib/agent-bus-install/` and `<t>/`                                                                                                                                                   | root 0700                                                     | root's own records, "kept out of the agent-bus user's directory so the homeserver cannot steer what root later removes" (`inst:85-87`)                                      | installer                                                                                                                                                                                               | installer, health (CAP_DAC_READ_SEARCH)                                                                                                           |
| `.../<t>/firewalld.rules`                                                                                                                                                                  | root 0600                                                     | `zone\trule` lines                                                                                                                                                          | `sync_firewall`, written as union BEFORE any firewall-cmd (`inst:596-608`)                                                                                                                              | `remove`, `check`                                                                                                                                 |
| `.../<t>/binary`, `.../<t>/unit-sha256`                                                                                                                                                    | root 0600                                                     | resolved binary path; sha256 of base unit                                                                                                                                   | `team` (`inst:817-824`)                                                                                                                                                                                 | restart triggers                                                                                                                                  |
| `.../<t>/backups/agent-bus-state-<n>.tar`                                                                                                                                                  | root 0600 (dir 0700)                                          | tar of `secrets/ team.json [registry.json] [room_id]`                                                                                                                       | `backup-now` (`inst:989-1007`), pruned with Tuwunel's                                                                                                                                                   | `backups`, `restore`, health                                                                                                                      |
| `/var/lib/agent-bus-applied/`                                                                                                                                                              | root:<stamp group> 0750                                       | dir                                                                                                                                                                         | installer `team` (`inst:764`)                                                                                                                                                                           | ccy-teams, `seat` (`co:482-487` lists teams built here), health                                                                                   |
| `/var/lib/agent-bus-applied/<t>.json` (applied stamp)                                                                                                                                      | root:<stamp group> 0640                                       | `{"v":1,"team","config":sha256,"software":sha256}` exact keys (`tf:399-466`)                                                                                                | installer LAST step, `put_file` (`inst:841`); removed by `remove` and `restore` (`inst:849-854`)                                                                                                        | ccy-teams (D65), declare `read_stamps`, health `_made` (mtime), timer_state                                                                       |
| `/var/lib/agent-bus-applied/health/<t>.json`                                                                                                                                               | root:<stamp group> 0640 in 0750                               | `{"v":1,"team","checked","expires"(=checked+300),"problems":[{what,fix}],"backup":{id,time}\|null,"backup_due","events":[{at,what}],"nrestarts"}` exact keys (`hl:210-229`) | health check: `.<t>.json.new` O_EXCL O_NOFOLLOW, fchown/fchmod, rename (`hl:365-385`)                                                                                                                   | `health status`, ccy-teams                                                                                                                        |
| `/etc/agent-bus/stamp-group`                                                                                                                                                               | root:root 0644                                                | one group name + newline                                                                                                                                                    | play only (`play:210-226`): desktop user's primary group by getent                                                                                                                                      | `render stamp-group` (must be regular, owned by root, not group/other writable, one existing group, no default, `tf:431-451`); health `_group_id` |
| `/etc/sudoers.d/agent-bus-read-only`                                                                                                                                                       | root 0440                                                     | sudoers, `visudo -cf` validated                                                                                                                                             | play (`play:231-239`)                                                                                                                                                                                   | sudo                                                                                                                                              |
| `/etc/systemd/system/agent-bus-{hs@,backup@,hs-health@}.{service,timer}`                                                                                                                   | root 0644                                                     | units                                                                                                                                                                       | installer `software` (put_file, daemon-reload on change, nothing enabled)                                                                                                                               | systemd                                                                                                                                           |
| `/etc/systemd/system/agent-bus-hs@<t>.service.d/network.conf`                                                                                                                              | root 0755/0644                                                | rendered drop-in                                                                                                                                                            | installer `team`                                                                                                                                                                                        | systemd                                                                                                                                           |
| `/usr/local/lib/agent-bus/{tuwunel-<v>, tuwunel-<v>.asset-sha256, tuwunel -> tuwunel-<v>, agent-bus.pyz, pingbus.pyz}`                                                                     | root 0755/0644                                                |                                                                                                                                                                             | installer `software`                                                                                                                                                                                    | units, wrapper                                                                                                                                    |
| `/usr/local/share/agent-bus/{tuwunel.pin, resolv.conf, kit/...}`                                                                                                                           | root 0644 (dirs 0755; `agent-bus-claude`, `kit/pingbus` 0755) |                                                                                                                                                                             | installer `software`                                                                                                                                                                                    |                                                                                                                                                   |
| `/usr/local/bin/{agent-bus (copy), pingbus -> ../lib/agent-bus/pingbus.pyz, agent-bus-claude -> kit}`; `/usr/local/sbin/agent-bus-install` (copy of itself)                                | root 0755                                                     |                                                                                                                                                                             | installer `software` (`inst:494-498`)                                                                                                                                                                   |                                                                                                                                                   |
| `/var/lib/systemd/timers/stamp-agent-bus-backup@<t>.timer`                                                                                                                                 | systemd                                                       | Persistent= state                                                                                                                                                           | systemd                                                                                                                                                                                                 | cleared by `remove` (`inst:869-873`) and `timer_state`                                                                                            |
| Checkout `<top>/.claude/ccy/pingbus/seats/<team>/<seat>/{token, README, member.json}`                                                                                                      | desktop user, dirs 0700, files 0600                           | bundle (member.json = protocol §12: `protocol, team, user_id, server_name, base_url, plain_http_hosts, token_file, admin, room, human_text`, `admin:688-702`)               | `seat take` as the user through opened dirs, `member.json` last (`co:298-411`)                                                                                                                          | pingbus in the container; `seat` commands (handle from member.json)                                                                               |
| `.../seats/<team>/<seat>/seat.lock`                                                                                                                                                        | user                                                          | flock + `seat <claim ms>`                                                                                                                                                   | pingbus `seat exec` (member side, D35/D53)                                                                                                                                                              | `seat take/remove/choose` (held = refused)                                                                                                        |
| `$XDG_RUNTIME_DIR/ccy/alloc/<sha256(realpath checkout)[:32]>.lock`                                                                                                                         | user 0600 (dir 0700)                                          | flock + holder line `<what> (pid N)`                                                                                                                                        | `seat take`, ccy-teams `session add` (same name, `co:752-790`; `src-go/internal/teams/hot/lock.go`)                                                                                                     | refused taker reads holder line                                                                                                                   |
| `$XDG_RUNTIME_DIR/ccy/membership/<session>/...` desired membership                                                                                                                         | user                                                          | ccy-teams                                                                                                                                                                   | `seat take/choose` (`co:793-824`)                                                                                                                                                                       |                                                                                                                                                   |
| `$XDG_RUNTIME_DIR/fedora-desktop-plays.lock` (play lock)                                                                                                                                   | user                                                          | flock + holder line                                                                                                                                                         | run.bash / ccy-teams (`src-go/internal/playlock/playlock.go:1-60`)                                                                                                                                      | serialises every play run of the user; the installer itself has NO lock (B16)                                                                     |
| Element: `~/.var/app/im.riot.Riot/config/Element-<t>/{config.json 0600, electron-config.json 0600 seeded once}` dir 0700; `~/.local/share/applications/agent-bus-<t>-element.desktop` 0644 | desktop user                                                  |                                                                                                                                                                             | element.py `apply` (tmp O_EXCL O_NOFOLLOW + rename, only on change, `element.py:255-296`)                                                                                                               | Element                                                                                                                                           |
| host_vars `localhost.yml` + `localhost.yml.backup.<UTC stamp>`                                                                                                                             | user, mode kept                                               | YAML subset                                                                                                                                                                 | declare.py `write` (backup O_EXCL, tmp, rename, `declare.py:691-716`)                                                                                                                                   | play, ccy-teams                                                                                                                                   |

Version fields: registry `v` 1|2; stamp `v` 1; health record `v` 1; member.json `protocol`;
team record `v` = protocol version. No version on team.json or tuwunel.toml (INFERENCE: the
canonical dump plus `error_on_unknown_config_opts` play that role).

---

## 3. Matrix / Tuwunel admin interaction

- Transport: `http://127.0.0.1:<port>` only; `check_base_url` refuses anything else; urllib
  with `ProxyHandler({})` (no proxy) and a no-redirect handler; token only in an
  `add_unredirected_header("Authorization")`; 30 s timeout; non-JSON answer is an error
  (`admin:247-300`). Tested "over real http ignores proxies" (`tests/.../test_admin.py:809`).
- Error messages never carry tokens: errcode regex-checked, error text ASCII-printable, cut to
  200 (`admin:318-323`).
- Endpoints used (`admin:77-78` prefixes `/_synapse/admin`, `/_matrix/client/v3`):
  - `GET/POST /_synapse/admin/v1/register` nonce + shared-secret HMAC-SHA1 (`nonce\0user\0pw\0admin|notadmin`) to create `@admin` (`admin:429-447`).
  - `POST /_matrix/client/v3/login` m.login.password (recovery path only, `admin:450-454`).
  - `GET /account/whoami` (checks token = `@admin:<sn>`; 401/403 = "rotating it is deferred").
  - `GET /_synapse/admin/v2/users?admins=true` (exactly one server admin, no pagination, `admin:505-513`).
  - `GET/PUT /_synapse/admin/v2/users/<id>` create (random discarded password, displayname, NO
    `admin` key), reset password with `logout_devices: true`, set `deactivated`/`locked`.
  - `POST /_synapse/admin/v1/users/<id>/login` mint a member token (`admin:376-382`).
  - `GET /_synapse/admin/v2/users/<id>/devices`.
  - `POST /createRoom` (preset private_chat, room_version protocol.ROOM_VERSION "12",
    visibility private, name = team, topic = description or fixed sentence,
    `power_level_content_override` = protocol.expected_power_levels(humans), initial_state
    `agent_bus.team` record) (`admin:559-581`).
  - `GET/PUT /rooms/<id>/state/<type>/<key>` (`?format=event` for m.room.create), `GET /rooms/<id>/state`, `/invite`, `/kick` (reason "removed from the team").
- Readiness/health endpoints: installer polls `/_tuwunel/server_version` (`inst:656`); health
  probes `/_matrix/client/versions` (`hl:313`).
- Room checks on rerun: m.room.create sender must be `@admin`, version 12, no
  `additional_creators` (`admin:561-568`).
- Team record (`agent_bus.team`, key ""): `{v, team, humans:[sorted user ids], roles:{user_id:role}, org?, exclude?, repos:[{repo,branches?}], path_prefixes?, forge_api}`; validated by `protocol.parse_team_record` before writing; never carries `description` (old readers) (`admin:530-547`; test `test_admin.py:248`).
- `_sync_team` writes power levels, record, topic only when canonical JSON differs (`admin:584-600`).
- `_empty_stray_status`: any `agent_bus.status` under a non-`@` state key is emptied each bootstrap (power 0 lets anyone write it; Matrix only binds `@`-keys to the sender) (`admin:603-615`).
- Bootstrap order (`admin:629-651`): admin session, single-admin check, ensure humans,
  ensure room, REMOVE humans named by the OLD record but not the file (before rewriting it,
  so a failed kick is retried next run), sync record, empty stray status, invite humans.
- Tuwunel quirks / probes (DES:1600-1605, `admin:17-20`, `rnd:144-146`, `DES:466-472`):
  - H4: `PUT v2/users` with `"admin": false` answers 500 - omit the key.
  - `+` accepted in localparts (handle separator).
  - `GET /login` still lists `m.login.token` with `login_via_token=false`; `get_token` 403.
  - SIGUSR2 does nothing without `admin_signal_execute`; with it, backup appears within ~1 s (H5).
  - `--restore-backup <id>` restores then serves (H5).
  - Unknown key with `error_on_unknown_config_opts=true` exits 1 - the readiness verdict is the
    unit state, never a journal scan (`inst:649-674`).
  - btrfs: `rocksdb_allow_fallocate=false`.
  - Config must be bare top-level keys (no `[global]`); a file with any bare key is read whole as global (`rnd:144-146`).
  - `max_request_size` floor 10_000_000 or Tuwunel refuses to start (`rnd:50-51`).
  - Password reset with `logout_devices` is the only recorded way to revoke tokens (no reactivation of a deactivated account recorded) -> park = password reset (`DES:787-792`).
  - `enable_set_displayname=false` stops only the member's own change; admin can still set displayname; per-room m.room.member displayname is still settable (`DES:396-398`).
  - Sign-in buckets are in memory, refusal logged only at debug (compiled out), no reset; a restart empties all buckets, so none offered (`hl:22-25`).
  - 1.9.4 has no per-user send rate limit; `rate_limiting` holds only login tables (`DES:395-397`).
  - Tuwunel is PID 1 of its own PID namespace (`PrivatePIDs`): an unhandled SIGUSR2 before its handler is installed is DROPPED -> `backup-now` must `wait_ready` before signalling (`inst:971-976`).
  - Tuwunel exits 0 on SIGTERM and on `server shutdown` -> `Restart=always` (`rnd:29-33`).
  - Type=notify: READY=1 once DB is open; watchdog ping every WatchdogSec/2; a stopped process holds SIGABRT pending, hence TimeoutAbortSec then SIGKILL (`files/etc/systemd/system/agent-bus-hs@.service` comment).
  - Server name can never change without wiping DB -> `render check --previous` refuses a change (`rnd:211-222`).
- tuwunel.toml key set: `rnd:39-48, 74-108` (DES 3.6 table). Notables: registration closed,
  federation off, trusted_servers [], encryption off, room creation off (only admin can create,
  so no DMs/side rooms), auto_accept_invites off, login_via_token/existing_session off,
  client_sync_timeout_min 0, log warn (no headers logged), login limits failed 3 then 1/5 min,
  account 5 then 1/5.5 min. Vendored key list fixture for the pinned version
  (`tests/helpers/agent_bus/fixtures/tuwunel-1.9.4-example-keys.txt`, `DES:349-352`).

---

## 4. Seats, roles, members

- Handle: `<repo>.<seat>+<host>.<type>` (`reg:1-16`; grammar owned by `helpers/pingbus/protocol.py`).
  repo = lowercase, `[^a-z0-9_-]`->`-`, strip leading `-_`, max 48; from origin's last path
  component minus `.git`, else the dir name (`reg:70-84`). host = role (`HOOKS_DAEMON_HOSTNAME`)
  normalised, or `--host`; never a hostname (public forge text) (`reg:87-95`, D19). For a ccy
  seat: the literal assigned in `ccy.env.local`, parsed never sourced (only
  `[export ]VAR=literal`, refuse any other form or a second assignment), else `local` (`co:95-127`, D49).
  types: podman, host, lxc, vm, docker. Seat = `[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}` (DES 5.5).
- Counters per prefix `<repo>+<host>.<type>`; never go down; a numbered `--seat` above the
  counter raises it (`reg:167-189`, D51). A failed account creation rolls back the member but
  NOT the counter (test `test_admin.py:529`).
- Roles: `coordinator` | `worker`; legacy `orchestrator` read as coordinator (`reg:126-131`,
  Plan 00196). At most one coordinator per team; a parked coordinator still holds the role;
  refusal names the holder (`reg:214-226`). The first in handle order wins for an old registry
  with two (`reg:216-217`, test `test_registry.py:456`). Seats are created as `worker`
  (`co:55`); role changes go through `set-role` (ccy-teams).
- Lifecycle (D40, D51, `admin:724-812`):
  - add: claim under the registry lock FIRST, then account (must not exist) + mint + invite +
    record; on failure undo the registry change (remove, or re-park for a return).
  - park: password reset (revoke) THEN mark parked, so a rerun completes it; parking again revokes again.
  - return: `add-member --seat` on a parked handle = unpark, same account/role/membership, new
    token, role from registry whatever `--role` says; refused if the account is gone/deactivated.
  - remove (retire): drop from registry, record, kick, deactivate; handle never returns
    (deactivated account blocks re-add); refuse a numbered handle never issued (`admin:789-795`).
  - rotate-token refuses parked.
  - "One place at a time": an active unparked handle is refused (second checkout of same repo
    on same host wanting the same seat name).
- Seat take discipline (`co:836-899`): parse+check teams (wait up to 180 s while HS unit is
  transitional, polling 2 s, progress every 20 s, `co:65-71, 669-694`; stopped-but-built vs
  unknown team get different messages naming `ccy-teams apply`/`create`); find checkout (git
  toplevel, user-owned, has `.claude/ccy/`); take allocation lock (bounded 15 s, poll 0.5 s,
  holder line in the refusal; no XDG_RUNTIME_DIR = refuse rather than run unlocked); check
  seat tree real user-owned dirs; `git check-ignore` the pingbus dir (P8: token must never be
  committable); observe; refuse held (75), refuse free-but-named by a running session's desired
  membership (75, H21; running = podman `ccy-session` label), refuse dir-without-bundle (78
  naming `seat remove`), refuse type mismatch; per missing seat: claim dir first (mkdir fresh;
  appeared meanwhile = refuse), `sudo agent-bus add-member ... --out=-`, place bundle by fd
  (O_NOFOLLOW|O_EXCL, 0600, fsync, member.json last), abandon empty dir on failure, emit CHANGED.
  Bundle read refuses anything but exactly three regular files of bounded size (`co:307-328`).
- Choose numbering: lowest number not used by any seat of this checkout and not held by an
  ACTIVE member of the same repo/host/type (a parked one returns); other-type seats count but
  are not offered; default = lowest free offered seat else new (`co:271-293`).
- Remove: take each present seat's lock (busy = held), park, rmtree, prune empties; a seat with
  no dir parks the handle the checkout's rule builds (lost-bundle recovery) (`co:414-424, 984-1014`).
- Parked members appear in `list` with `parked`; `seat choose` shows only this checkout's dirs.

---

## 5. Root / sudo boundary

- root: installer (everything system), wrapper entry, health check (sandboxed, caps).
- `agent-bus` user: every account/registry/secret operation and all rendering (`wrap:55-58`);
  homeserver process. No agent runs as it (D8). Caveat documented: an agent with passwordless
  sudo is trusted like a human (`DES:414-416`).
- desktop user: seat commands, health status, ccy-teams, declare, element (refused if the user
  holds pingbus bundles, because Element's session token is readable by every same-user
  process, D26; one exception: an Element-acceptance record only, `element.py:209-234`).
- Sudoers rule (`files/etc/sudoers.d/agent-bus-read-only.j2:7-8`), NOPASSWD, anchored regex
  (sudo >= 1.9.10): `/usr/local/bin/agent-bus ^list [a-z][a-z0-9-]*$` and
  `^human devices [a-z][a-z0-9-]* [a-z][a-z0-9_-]*$`. Argument: both read-only, print no token
  or password, run as agent-bus, arguments pinned so no option/extra arg/subcommand reaches
  the wrapper; grants no general root (reasoning in Plan 00177 UX-REVIEW "The sudo decision").
  Called by absolute path because sudo's secure_path may lack /usr/local (`gab:14-15`, D58).
- Bundle placement (D61, `wrap:65-151`, `co:296-411`): agent-bus side writes the bundle only as
  a tar on stdout (never to a tty); root unpacks named files into a private mktemp dir with
  `--no-same-owner --no-same-permissions`, extracting only the expected names. `--out=DIR`:
  absolute, must be via sudo (`SUDO_UID/GID` numeric); add-member refuses any existing path
  (symlink included); rotate-token needs an existing real dir owned by SUDO_UID with
  member.json; every write under `--out` done as the sudo user with
  `setpriv --reuid --regid --clear-groups` (root opens its copy as stdin, user only reads the fd).
  `--out=-` (add-member only): re-pack the three files as a tar (owner 0, mode 0600) on stdout.
  Why: a seat dir sits in a checkout every sibling session can rewrite (container root = host
  user); root's earlier `install -d -o $SUDO_UID` was racy (qa-reviewer finding, DES D61).
- Installer trust boundaries (SPEC-abi:127-143): root writes into agent-bus-owned dirs, so
  root-side records/tars live in root's dir; symlink refusals in put_file/ensure_dir (TOCTOU
  remains, PR2); state tar validated before anything stops; names regex-validated
  (`^[a-z][a-z0-9-]{0,23}$`, `^[1-9][0-9]*$`) because they become paths, unit names and
  `rm -rf` targets; pinned sha256 is the only supply-chain check (release unsigned); root runs
  code from the user-writable checkout (play runs the checkout's installer in place, `play:51`,
  and the bundler with cwd=source) - the existing repo-wide boundary.
- Homeserver confinement (unit file): per-team `TemporaryFileSystem=/var/lib/agent-bus:ro` +
  `BindPaths=` own dir + `PrivatePIDs=yes` (all teams share one uid, so without these a sibling's
  /proc/<pid>/root or ptrace reaches its secrets); IP filter deny-all base + drop-in allow;
  SocketBindDeny=any base; no resolver (tmpfs over /run/systemd/resolve with a 127.0.0.1 stub;
  /run/dbus inaccessible); UMask 0077.

---

## 6. Installer per team, and the play

`team` (`inst:755-842`; SPEC-abi 5.2): root, Fedora, readable file, software present, stamp
group read (fails without the play's record), applied dir 0750 root:group, firewalld and
NetworkManager running; `render check [--previous]` (its exit code passed through); state must
be present; port not another installed team's and (if our unit inactive) not listening; map each
listen address to exactly one interface (`ip -j addr`); render toml + dropin; dirs; put toml
(restart if changed), team.json (no restart), shared secret; `sync_firewall` (zone = zone of
the interface routing the CIDR, default zone if none; record written as union before mutating;
permanent + runtime; no --reload); drop-in (restart + daemon-reload if changed); record binary
path and base-unit sha (restart if changed, so a new Tuwunel or base unit restarts exactly once);
`converge_units` (start if inactive else restart if flagged; enable hs + backup timer + health
timer; timers started after the HS because a Persistent timer fires a missed run at once);
`wait_ready` (60 x 1 s, fail at once if unit not active, journal tail on failure); assert bound
addresses == 127.0.0.1 + listen exactly (`ss`); `bootstrap`; stamp LAST (so a stamp means fully
applied).

Network drop-in (`rnd:184-208`): `[Unit] StartLimitIntervalSec=0`, After/Wants on
`sys-subsystem-net-devices-<iface>.device` (dash escaped `\x2d`); `[Service] Restart=always, RestartSec=5s, RestartSteps=5, RestartMaxDelaySec=60s, IPAddressDeny=any, IPAddressAllow=<loopback v4+v6, each listen /32|/128, allow_from>, SocketBindDeny=any, SocketBindAllow=tcp:<port>`.
No start limit because a late interface or full disk would otherwise leave the unit failed
after the cause clears.

`software` (`inst:453-511`): packages (python3 firewalld NetworkManager zstd curl jq tcpdump
util-linux; tcpdump kept for acceptance P1/P2, util-linux for setpriv), user `agent-bus`
(system, nologin, home /var/lib/agent-bus), dirs, pin, resolver stub, Tuwunel (download only
when the asset record != pin; curl https-only TLS1.2; sha256 check before decompress; atomic
symlink swap), zipapps via `helpers.pingbus.bundle` (reproducible, `BUNDLE-CHANGED|UNCHANGED`
marker), wrapper, itself, kit, units (+daemon-reload), optional `agentbus0` NM dummy connection
with `<ip>/32|/128`, autoconnect. Converged rerun runs no curl/dnf/useradd/daemon-reload/nmcli
mutation and prints nothing (SPEC-abi:276-277).

`remove`: disable --now hs + backup timer + health timer, clear Persistent timer stamp, remove
recorded firewall rules only, drop-in, stamp + health record; `--purge` also both data dirs.
Nonexistent team = silent success (play relies on it, B18).

Backup: SIGUSR2 to main PID, wait 60 s for a higher `meta/<n>`, tar state, prune tars whose meta
is gone. Restore: validate tar (only team.json/registry.json/room_id regular files, secrets/
dir and files beneath, no setuid/setgid/sticky, team.json and secrets required) BEFORE stop;
stage, chmod, chown; stop HS; replace state; unstamp; transient
`systemd-run --unit=agent-bus-restore-<t>.service --collect` with the base unit's whole
[Service] sandbox read from the installed unit (minus ExecStart, `%i` substituted, other `%`
refused) + loopback-only IPAddressAllow + SocketBindAllow; `tuwunel --restore-backup <id>`;
wait ready; stop; start HS; wait; assert listening; bootstrap. Not atomic after the stop (B8).

Play (`play:1-389`): asserts shape (`agent_bus_teams` unique teams, state present|absent, purge
only on absent, `agent_bus_only_teams` subset), refuses dead `agent_bus_lxc_bridge`; computes
swap (checksums of pin + hs@ + backup@ units vs installed, or `CONFIG-CHANGED`) and backs up
every RUNNING present team before `software` (restore point minutes old; failure stops the run);
`software`; stamp-group record; sudoers; LXC absent members; `remove` absent; for present:
bridge subnet added, team files rendered into a root-only tempfile dir (always deleted),
`team` each; `systemctl reset-failed agent-bus-hs-health@*` (a removed team's failed run would
stay in --failed for ever); `timer_state`; LXC present members (stopped containers =
SKIPPED/DEFERRED, not failure). `changed_when` = `^CHANGED\t`. No `--check` support for mutating
steps. `agent_bus_only_teams` scopes steps 2-3 (ccy-teams applies one team at a time, D65, so
a bulk update can never take another team down). Element play is separate and optional.

Applied stamp and D65: `config` = sha256(canonical team.json); `software` =
sha256(sha256(zipapp) \\n tuwunel version without v \\n installer sha256 \\n) (`tf:404-419`).
`declare status` recomputes the same from the checkout (builds the zipapp in-process,
hashes the checkout's installer file, adds the live bridge subnet to allow_from) and reports
current / outdated(config|software|unstamped) / unbuilt / removed / decide(absent|undeclared) /
unreadable (`declare.py:461-521`).

---

## 7. Hard-won behaviours (each with its reason; keep in v2)

01. No `admin` key when creating an account (500 otherwise) (H4).
02. Admin password stored BEFORE registering and deleted after the token is stored: a run that
    died between register and store recovers by login; a token lost later is refused naming
    restore (no rotate-admin) (`admin:463-502`; tests `test_admin.py:318,329,337`).
03. Secrets written with link-not-replace so two runs cannot both create one (`admin:199-219`; test `:841`).
04. Exactly one server admin asserted every bootstrap (`admin:505-513`).
05. A deactivated human can never return; `removed_humans` in the team file makes that a
    validation rule shared by play and declare (Plan 00201 S-F14, `tf:209-223`).
06. Human removal happens before the record rewrite, so the old record still names them for a retry (`admin:638-644`; test `:416`).
07. Stray `agent_bus.status` under non-user keys emptied (`admin:603-615`).
08. `server_name` immutable; `--previous` must exist if named (`cli:163-170`, `rnd:211-222`).
09. Registry: lock across load+save; save only on change; atomic + fsync dir; v1 upgrade; strict exact keys; duplicate-key JSON refused (`teamfile.decode_strict_json`, also NaN refused).
10. Park = revoke then mark; return keeps role; rotate refuses parked; numbered seat raises counter (D51).
11. Firewall record written as union BEFORE any change so a partial failure is still cleanable; only recorded rules are ever removed (`inst:596-608`).
12. Restart only on toml/drop-in/binary-record/base-unit-record change; team.json alone does not restart; a converged run restarts nothing (`inst:799-828`).
13. Readiness verdict = unit state, not journal scan; journal tail printed only to explain (`inst:649-674`).
14. `backup-now` waits ready before SIGUSR2 (PID-1 namespace drops an early signal) (`inst:971-976`).
15. State tars in root's own dir; validated before stopping; extracted without owners/modes (DES 3.7).
16. `systemd-run --collect` so a failed restore leaves no failed unit blocking the name.
17. Persistent timer state cleared on remove and swept by the play, else a new team of the same name inherits a backup run (`timer_state.py`, `inst:869-873`); health treats a timer run before the stamp's mtime as not a run (`hl:166-186`; test `test_health.py:284`).
18. Stamp group recorded by the play and required by every `team` run, so a manual run keeps the play's group; no default (`play:191-226`, `tf:431-451`).
19. Stamp written last; removed on remove AND restore (restored team.json may differ).
20. Port uniqueness across installed teams + live `ss` check only when our unit is down.
21. Exactly-bound address assertion (P2) before bootstrap; wildcard listen refused (D4).
22. Restart policy: Restart=always, no start limit, growing delay, device deps (late WireGuard/dummy at boot).
23. Watchdog (Type=notify, WatchdogSec=30s, TimeoutAbortSec=30s) plus minutely client-API probe: 3 misses 10 s apart after a 60 s start grace, restart only if same InvocationID and still active (`hl:306-324`; tests `test_health.py:176-216`); NRestarts rises noted once and kept 24 h; a lower count = manual restart (`hl:190-200`).
24. `systemctl show` silently omits properties a unit type lacks: ask each unit only its own properties and refuse a missing one by name (`hl:71-75, 445-452`; test `:149,156`).
25. Health record has `expires` (checked + 300 s) so a dead check is visible (`hl:66-68`).
26. Newest usable backup = has both meta and state tar (what restore can use) (`hl:156-163`).
27. Seat start wait: boot restores sessions while system homeservers start; wait 180 s for transitional states (Plan 00203 gap 3, `co:63-71`).
28. `sudo -k` after every take/remove, failing loudly (security audit H1).
29. Allocation lock in the runtime dir, never in the checkout (sessions can write the checkout); refuse rather than run unlocked; same file name formula as Go (`co:752-790`; test `:1027`).
30. Desired-membership seats of RUNNING sessions are treated as held (H21) (`co:793-824`).
31. Seat dir claimed before sudo, so two launches racing for one new seat: one is refused before reaching the HS (D61).
32. member.json written last, so a half-placed seat has no bundle and `take` names `seat remove` (`co:298-300`).
33. `git check-ignore` before a token lands in the checkout (P8).
34. Wrapper `cd /` before runuser; python `-I`; health unit python `-I`.
35. `role_from_env_local` parses literals only and refuses shell-only forms (D58).
36. LXC bridge subnet read live (not fact cache) so a bridge created earlier in the same run is seen; one function shared by play and status so stamps read back current (`bridge.py:1-19`).
37. Element config overrides every outbound key explicitly (shallow merge over bundled config); spellcheck seeded off once (Chromium fetches dictionaries); keyring grant before first start (flathub issue 528) (`element.py:1-23`, element play).
38. Bundle README per type; checkout seat types omit the host-kit guide pointer (`admin:86-109, 705-714`).
39. Installer: `--team` regex anchored with true end-of-text (SPEC-abi:90-91); usage errors before the root check (SPEC-abi:92-95).

Known latent bugs (SPEC-abi:690-714, deviations in the Go port branch): B2 zone-id/IPv4-mapped
bus addresses accepted; B3 leaked child exit codes; B4 TMPDIR honoured for a stage holding
secrets; B5 validate-tar and extract-tar are different parsers; B6 backup tars a symlink planted
in secrets/; B7 shared secret mode never repaired; B8 restore not atomic; B9 temp files left on
failure; B10 malformed other team.json aborts with jq status; B12 `no zone` text vs exit; B13
nmcli terse IPv6 compare always differs (suspected); B16 no installer lock (play during a backup);
B17 bundler inherits Python env; B20 corrupted binary with matching record never detected;
B21 stdin-eating loop; B22 useradd on any getent failure; B25 curl reads ~/.curlrc and proxies.

---

## 8. What not to carry into v2 (with reasons)

01. Three languages for one subsystem: bash installer + bash wrapper + Python admin zipapp +
    Python helpers + Go ccy-teams, glued by marker lines and exit codes. The installer shells to
    the admin tool for `render` only because bash cannot validate (DES:150-152); a single Go
    binary removes the `render` CLI surface, `stamp-group`, `config_restart`, `bridge`,
    `timer_state` as separate processes. INFERENCE: these exist only to keep bash small.
02. `declare.py`'s hand-written YAML-subset line editor of the untracked host_vars
    (`declare.py:1-10, 230-442`): fragile, refuses many valid YAML shapes, and makes ccy-teams'
    source of truth a file Ansible also reads. Plan 00197 already moves seats out of declared
    config; v2 should hold team declarations in its own typed store (or a Go YAML node editor)
    rather than port this.
03. The software digest coupling: `declare` rebuilds the zipapp and hashes the checkout's
    installer to predict the stamp (`declare.py:461-470`); breaks the moment the installer is a
    binary (SPEC-abi 8.3). Replace with an embedded build/source digest.
04. Root running the user-writable checkout's installer in place (`play:51`) and the bundler
    with cwd=checkout (SPEC-abi PR4/PR5): v2 should install a root-owned binary and run that.
05. `AGENT_BUS_INSTALL_TEST_PREFIX` test hook in the production script (skips root, trusts PATH)
    (`inst:23-25, 63-77`, PR1): use fakes/build tags.
06. Duplicate unit-name/path helpers across bash, Python and Go (`hs_unit` in `inst:522`,
    `co:463`, `hl:94`; APPLIED_DIR in `co:60`, `timer_state.py:126`; STATE_ROOT in `rnd:26`,
    `cli:33`, `hl:51`).
07. Stale/contradictory docs to not inherit: `tf:395-398` says the stamp is world-readable (it
    is 0640 to the stamp group); DES 3.2 says `/usr/local/bin/agent-bus` is a symlink (it is a
    copy, `inst:495`); `sudo.go:19` claims the sudoers rule covers the installer (B1); SPEC-abi
    5.2 step 19 still says stamps 0644/dir 0755 (code is 0640/0750).
08. Two lock worlds with no installer lock (registry flock, allocation flock, seat flock, user
    play lock, nothing for root installer/backup/health): v2 needs one host-side serialisation
    for mutating root operations (B16, SPEC-abi D4).
09. The tar-on-stdout + root unpack + setpriv dance exists because the admin tool runs as
    agent-bus and cannot write user dirs. Keep the invariant (root never writes under a user's
    path; user places by fd), but a Go daemon/CLI can hand the bundle over a pipe/socket with a
    typed protocol rather than tar parsing in bash.
10. Per-team firewalld rich rules plus an exact-union record file: necessary (D5), but the
    record's union-before-mutate trick and B12/B13 parsing of firewall-cmd/nmcli text are where
    bugs live; use D-Bus APIs or well-tested parsers in v2 (INFERENCE).
11. Accidental surface: `human logout-all|lock|unlock`, `rotate-token`, `remove-member`,
    `check` have no programmatic caller (human/acceptance only) - keep only if a requirement
    names them. `version` reports `0.1.0` constant.
12. LXC members via `agent_bus_lxc_members` + a root play + lxc-attach scripts
    (`helpers/agent_bus_lxc/members.py`) are slated for retirement by Plan 00197's broker
    (owner-approved device-flow seat requests, audit log, broker never writes localhost.yml).
    v2 should be designed around that broker, not around host-sudo seat creation.
13. Legacy compatibility to drop if v2 resets the protocol: registry v1, role `orchestrator`,
    status-key sweeping (keep the check, it is a protocol rule), pre-seats ccy layout (already unread).
