# D: what a running v1 team holds (migration input digest)

Read-only research for Plan 00209 area D. Placeholders only: `<team>`, `<handle>`, `<seat>`,
`<checkout>`, `<sn>` (server_name). Citation keys (all paths relative to the repo):

- `RH` = CLAUDE/Plan/00209-agent-bus-v2-in-go/RESEARCH-host.md (its own `admin:`/`inst:` etc. are
  helpers/agent_bus/admin.py, files/usr/local/sbin/agent-bus-install ...)
- `RP` = RESEARCH-pingbus.md, `RL` = RESEARCH-lxc-robustness.md (same folder)
- `PROTO` = docs/agent-bus-protocol.md, `DOC` = docs/agent-bus.md
- `S211` = CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/SPEC.md
- `SPEC-state S<n>`, `SPEC-matrix M<n>` = specs on branches feat/bus-v2-m1-state / feat/bus-v2-m1-matrix
- `TUW` = CLAUDE/Plan/00161-agent-team-bus-matrix/subagent-reports/261006-research-tuwunel-opus-5-5.md
- `FRAME` = DESIGN-v2-FRAME.md (v2 is a separate system, installs beside v1, one team at a time,
  v1 team stays intact until the migration commits)

Marks: FACT = read in code or doc (cited). INFERENCE = reasoned. UNVERIFIED = no evidence in the
repo; needs a probe before the design relies on it.

---

## 1. Per-team host state

### 1.1 The homeserver instance

| Item                   | v1 fact                                                                                                                                                                                                                                                                        | Cite                                                |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------- |
| Software               | One Tuwunel process per team, pinned `v1.9.4`, static build, sha256-checked; `/usr/local/lib/agent-bus/tuwunel-<v>` + symlink `tuwunel`                                                                                                                                        | `files/usr/local/share/agent-bus/tuwunel.pin`; RH 2 |
| Unit                   | `agent-bus-hs@<team>.service` (template), `Type=notify`, `WatchdogSec=30s`, `Restart=always`, no start limit; per-team drop-in `network.conf`; `PrivatePIDs`, `TemporaryFileSystem=/var/lib/agent-bus:ro` + `BindPaths=` own dir, all teams share ONE uid `agent-bus`          | RH 5, 6, 7.22-23                                    |
| Data dir               | `/var/lib/agent-bus/<team>/` (agent-bus:0700): `db/` (RocksDB), `backups/`, `secrets/`, `tuwunel.toml` (root:agent-bus 0640), `team.json` (canonical team file), `registry.json` (0600), `registry.lock`, `room_id`                                                            | RH 2                                                |
| Root-only records      | `/var/lib/agent-bus-install/<team>/` (root 0700): `firewalld.rules`, `binary`, `unit-sha256`, `backups/agent-bus-state-<n>.tar` (tar of `secrets/ team.json registry.json room_id`)                                                                                            | RH 2                                                |
| Applied stamp / health | `/var/lib/agent-bus-applied/<team>.json` (`{v,team,config,software}`), `.../health/<team>.json`, `.../lxc/<team>.json`                                                                                                                                                         | RH 2; RL 1.8                                        |
| Listen                 | `127.0.0.1` + the team's `listen` addresses (bus address on dummy iface `agentbus0`), one TCP `port`; no TLS, plain http; `allow_from` enforced by firewalld rich rules AND unit IP filter; LXC bridge subnet added live by the play                                           | RH 6; DOC "Where a team's homeserver runs"          |
| Port / name uniqueness | port not another installed team's; team name `^[a-z][a-z0-9-]{0,23}$` is the unit, folder, timer and firewall-record name                                                                                                                                                      | RH 6, 7.20; RH 5                                    |
| `server_name`          | default `<team>.agent-bus.internal`; NEVER changes for a team's life (`render check --previous` refuses a change; Tuwunel cannot rename without wiping the DB)                                                                                                                 | RH 7.08; `helpers/agent_bus/render.py:211-222`      |
| Config                 | bare top-level keys only (no `[global]`); registration closed; federation off; encryption off; room creation off for non-admins; `login_via_token`/`existing_session` off; login rate buckets in memory; `error_on_unknown_config_opts=true`; `max_request_size` >= 10_000_000 | RH 3, 7                                             |
| Timers                 | `agent-bus-backup@<team>.timer` (daily), `agent-bus-hs-health@<team>.timer` (60 s); a Persistent timer's state file `/var/lib/systemd/timers/stamp-agent-bus-backup@<t>.timer` outlives a removed team and must be cleared                                                     | RH 2, 7.17                                          |

### 1.2 Accounts

| Account                                                           | Created how                                                                                                                                                                                      | Credential and where it lives                                                                                                                                                                                                                                                                                                                | Cite                              |
| ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- |
| `@admin:<sn>` (exactly one server admin, asserted each bootstrap) | `registration_shared_secret` HMAC-SHA1 register (`GET/POST /_synapse/admin/v1/register`) with `admin`; registration stays CLOSED from first boot                                                 | shared secret: `secrets/registration_shared_secret` (128 hex, no newline, 0600, written once by the installer, never repaired). Admin password: `secrets/admin.password` exists ONLY between register and token stored. Admin token: `secrets/admin.token` (0600, link-not-replace). No rotate-admin: a lost token means restore from backup | RH 2, 3, 7.02-04; TUW 3           |
| Agent members `@<handle>:<sn>`                                    | `PUT /_synapse/admin/v2/users/<id>` with a random DISCARDED password, displayname, NO `admin` key (H4: `"admin": false` answers 500); token minted by `POST /_synapse/admin/v1/users/<id>/login` | token only in the member's bundle (`token`, 0600) in a seat dir (ccy), `~<agent>/.config/pingbus/<team>/` (LXC/host), no password stored anywhere. Revoke = password reset with `logout_devices:true` (the only revoke Tuwunel has)                                                                                                          | RH 3, 4; SPEC-matrix M29-M30      |
| Humans `@<name>:<sn>`                                             | same PUT (random discarded password); never admin; power 50 in the room; password login only                                                                                                     | NEVER stored. `agent-bus human password <team> <name>` sets a fresh 32-char password, logs out other sessions, prints it once. `human lock/unlock/logout-all/devices` exist                                                                                                                                                                  | RH 1.2; DOC "The human's account" |
| Deactivated/removed humans                                        | `removed_humans` in the team file; a deactivated account can never come back (name is burnt)                                                                                                     | team file                                                                                                                                                                                                                                                                                                                                    | RH 7.05                           |

Handle grammar `<repo>.<seat>+<host>.<type>`, `type` in podman, host, lxc, vm, docker; `<host>` is the
install's ROLE name, never a machine hostname (handles reach public forge text) (RH 4; RP 5.21).
Seat = `[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}`. The user-ID localpart therefore contains `+`
(Tuwunel accepts it, RH 3).

### 1.3 Registry (host's source of truth for membership)

`<t>/registry.json` v2: `{"v":2,"team","counters":{prefix:n},"members":{handle:role},"parked":[...]}`.
Counters per `<repo>+<host>.<type>` never go down (a numbered seat above the counter raises it);
roles `coordinator` | `worker` (legacy `orchestrator` reads as coordinator); at most one
coordinator, a parked coordinator keeps the role. Lifecycle: add (claim in registry first, then
account, mint, invite, record; undo on failure), park (revoke THEN mark, so a rerun completes it),
return (unpark keeps account, role; new token), remove (retire for ever: deactivated, kicked,
handle burnt). RH 4, 7.09-10; `helpers/agent_bus/registry.py:167-345`.

### 1.4 Rooms

| Room                      | Created by                                                                                                                                                                                                           | Id stored                                                                                                                             | Trust shape (what v1 members check)                                                                                                                                                                                                                                                                                                                        | Cite                      |
| ------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------- |
| Main room                 | `admin` `POST /createRoom`, preset private_chat, room version 12, name = team, topic = description or fixed sentence, `power_level_content_override`, initial_state `agent_bus.team`                                 | `<t>/room_id` (written once, link-not-replace); also in every bundle's `member.json` as `room`                                        | create sender `admin`, version 12, no `additional_creators`, event id == `$`+room[1:]; power levels EXACTLY (humans 50, users_default 0, events_default 0, state_default 100, invite/kick/ban/redact 100, notifications.room 50, `m.room.tombstone` 150, `m.reaction` 50, `agent_bus.status` 0); `agent_bus.team` record from admin lists self with a role | RH 3; PROTO 8 (l.455-480) |
| Team record               | `agent_bus.team`, state key `""`, admin only: `{v,team,humans[],roles{uid:role},org?,exclude?,repos[],path_prefixes?,forge_api}`; never carries `description` (an unknown key makes an old member distrust the room) | in room state                                                                                                                         |                                                                                                                                                                                                                                                                                                                                                            | RH 3; S211 4              |
| Reports room (Plan 00211) | admin; name `<team> · reports`; restricted join to main; humans power 0, `events_default` 10 (nobody posts but role holders)                                                                                         | `<t>/room_id.reports`; index `agent_bus.rooms` `{v,reports,questions}` in the MAIN room; back-link `agent_bus.room` in each side room | side room trusted only if index names it AND back-link names main                                                                                                                                                                                                                                                                                          | PROTO 8.1; S211 4-5       |
| Questions room            | admin; invite-only; coordinators invited; `Q<n>` numbering = highest `Q<n>` among coordinators' posts in room history; threads                                                                                       | `<t>/room_id.questions`                                                                                                               | open/closed power levels depend on whether a coordinator exists                                                                                                                                                                                                                                                                                            | PROTO 8.1; S211 8         |
| Aliases                   | NONE. No room aliases anywhere; members find side rooms by ID through the index (S211 line 106). Room creation off for everyone but admin                                                                            |                                                                                                                                       |                                                                                                                                                                                                                                                                                                                                                            | S211 l.106                |
| Admin room                | Tuwunel's own (`federate_admin_room=false`, `admin_escape_commands=false`); grant_admin_to_first_user default; the first registrant joins it at 100                                                                  | internal                                                                                                                              |                                                                                                                                                                                                                                                                                                                                                            | TUW 3; DESIGN 1.x table   |

Seat/role model (D40, D51): a seat is a durable role identity; account, handle, role and bundle
persist across sessions and reboots; any later session claiming it continues as that member
(DESIGN D40, in 00161 DESIGN `D40` row). One seat per team per session (D48).

### 1.5 Declaration (host_vars, untracked, desktop)

- `agent_bus_teams` (list): one team file per entry: required `team, port, listen[], allow_from[], humans[], forge_api`; optional `state` (present|absent), `server_name`, `org`, `exclude`, `repos`
  (`{repo, branches?}`), `path_prefixes`, `description`, `removed_humans`. Source:
  `helpers/agent_bus/teamfile.py:42-50`.
- `agent_bus_element_teams`: `{team, base_url, server_name?}` per human Element profile
  (`helpers/agent_bus/element.py:20-23`).
- `agent_bus_lxc_members`: per (container,user,team) `{team, container, user, host?, checkout?, seat?, role?, human_text?, repo?, state?, service?}` (RL 1.1).
- `agent_bus_address` (bus IP on `agentbus0`), `agent_bus_only_teams` (scope a run to teams),
  `agent_bus_lxc_bridge`. Edited by `ccy-teams` through `declare` (a hand-written YAML-subset line
  editor, RH 8.02), which writes `localhost.yml.backup.<UTC>` first.
- Applied by `playbooks/imports/play-agent-bus.yml` -> `agent-bus-install team --team-file F` per
  present team; `ccy-teams` applies one team at a time with `agent_bus_only_teams`.

---

## 2. Per-member state

### 2.1 Where it lives

| Member kind           | Bundle + state home                                                                                                                                                                                                                                                                                                                           | Notes                                                                                                                                             |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| ccy (podman) seat     | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/{token, README, member.json, state/, seat.lock, said-goodbye.json}`; session home (container `/tmp/pingbus-home`): symlink `<team>` -> seat dir, `keeper.lock`, `keeper.log`, `applied.json`, `active`, `settings.json`, `statusline-inner`, `socket.json {v,socket,token}`               | seat dir is in the checkout, so it survives container death; reachable by sibling sessions (hence locks live in `$XDG_RUNTIME_DIR`, RH 2, RL 3.2) |
| host (`cc`)           | `~/.config/pingbus/<team>/...` (PINGBUS_HOME default) on the desktop user                                                                                                                                                                                                                                                                     | refused if the user has an Element profile (token readable by same-user processes), unless accepted once (RP 2a)                                  |
| LXC / docker / vm     | `~<agent>/.config/pingbus/<team>/...` + `~<agent>/.config/pingbus/env` (`PINGBUS_TEAMS=csv`, `HOOKS_DAEMON_HOSTNAME=<role>`, foreign lines kept)                                                                                                                                                                                              | LXC claims no seat; busy rule = the team sync lock (RL 1.9)                                                                                       |
| Host-side per session | `$XDG_RUNTIME_DIR/ccy/membership/<session>/membership.json` (desired, host-written, mounted RO into the container), `.../alloc/<sha32>.lock`, `.../change/<session>.lock`, `<checkout>/.claude/ccy/pingbus/sessions/<session>/{applied.json,conversation.json}`, `~/.local/state/ccy/sessions/<name>` (launch args incl. canonical `--teams`) | RL 3.2, 5                                                                                                                                         |

`member.json` (v1 protocol 1): `protocol, team, user_id, server_name, base_url, plain_http_hosts, token_file:"token", admin:"@admin:<sn>", room, human_text?, limits?`; all keys exact, duplicates
refused (RP 2a; `helpers/pingbus/config.py:44-55,490-546`). `base_url`, `user_id`, `room` are fixed
into the bundle: changing server means a NEW bundle.

### 2.2 `<team>/state/` (SPEC-state S25-S35, RP 2b)

| File                                                                   | Content                                                                                                                                                                                           | Role in migration                                                                                            |
| ---------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `inbox/<eid>.json`                                                     | accepted events not yet delivered (projection `type,event_id,sender,origin_server_ts,content`; questions-room items carry `room:"questions"`,`question`)                                          | PENDING = queued, undelivered. Re-validated on every read; printed only from a server re-fetch (`GET event`) |
| `consumed/<eid>.json`                                                  | delivered/consumed items                                                                                                                                                                          | with inbox = the SEEN/dedupe set (known ids)                                                                 |
| `sync.json`                                                            | `{"v":1,"next_batch":T}` written only AFTER inbox writes are durable; first sync (no token) uses `timeline.limit 0` so history is NEVER processed; `seat retire` deletes it to force a first sync | tokens are per-server; unusable on a new server                                                              |
| `outbox.json`                                                          | `{v:1,pings:[{event_id,verb,ref,to,sent_ms,answered,reported}],gate:{bucket,recent}}`; RMW under flock on the state dir                                                                           | the only "await" state (below)                                                                               |
| `reports.json`                                                         | `{v:1,sent:[ms...]}` per-hour report gate                                                                                                                                                         |                                                                                                              |
| `team.json`, `about.json`, `room.json`, `rooms.json`, `untrusted.json` | verified team record, description, room members/statuses, side-room join states, distrust reason                                                                                                  | all re-derived by the syncer on the new server                                                               |
| `forge-cache.json`                                                     | positive GitHub checks, TTL                                                                                                                                                                       | server independent; may be kept                                                                              |
| `dropped.log`, `ignored.log`                                           | one line per drop/ignore, never text                                                                                                                                                              |                                                                                                              |
| `lock`                                                                 | flock; content `watch`/`wait`/`recv`                                                                                                                                                              | sync lock, one syncer per team                                                                               |

Session home extras: `stop-guard.json`, `goodbye.json`, `watch.log`, `inject/<key>.{transcript,stalled,missing,watch,spawned}`,
`relay/lock` (+ spool dirs, relay mode only), `down/<team>`, `told.json`, `told-roles.json`.
All state files are 0600/0700, `O_NOFOLLOW`, size-capped, strict key sets (an unknown key is a
StateError -> exit 78, never silently ignored), written tmp+fsync+rename+dir fsync (RP 2).

### 2.3 What "in-flight" means in v1

There is NO outbound send queue. `send`/`say`/`report`/`ask-human` are synchronous: charge the send
gate, `PUT /rooms/{r}/send/{type}/{txn}` (txn `pb`+32 hex, retried 3x, 1 s apart, same txn on
Unreachable), print `SENT` (RP 1 `send` detail; RP 3). A failure exits 7/8/9/10; nothing is stored for
retry. So in-flight, by place:

1. **Sent, not yet synced by the receiver**: an event on the v1 server (in the DB), past the
   receiver's stored `next_batch`. Survives only if the v1 server and its DB are kept.
2. **Synced, not yet delivered**: file in `inbox/` (PENDING). Survives a restart; re-fetched from
   the server when printed, so a v2 cut-over that deletes the v1 room makes the item undeliverable
   (print refuses a missing event; see RP 5.05, `cli.py:1120-1133`).
3. **Fetched by the watcher but the seat was removed**: consumed UNDELIVERED on purpose (H23a)
   so nothing posted while out is delivered on re-add (RP 4, 5.25).
4. **Delivered into a session**: written whole to Claude Code's inbox socket = "sent", even if the
   close timed out (E11); file moves to `consumed/`; transcript later scanned for
   `agent-bus delivery <nonce>:`; absent after 180 s quiet / 3600 s -> `inject/<key>.missing`.
   A delivery whose nonce never appears is the only case v1 itself flags as possibly lost (RP 4).
5. **Ack-expected ping awaiting reply**: an `outbox.json` ping entry (verbs with an ack rule:
   `fetch, sync, review, run-qa`, `ack_timeout_s` 900; `halt` 300). Each silent target yields ONE
   `TIMEOUT` item (key `<eid>~<target localpart>`) in the sender's next recv/wait/watch after the
   deadline; `answered`/`reported` lists track targets. An ack counts by the answer's
   `origin_server_ts` vs deadline, even when read late. Settled pings drop on save. (PROTO l.688-692,
   755-758; SPEC-state S27; RP 5.14-15). A v2 migration that moves the member loses the ack:
   the target's `ack` would be a v1 event.
6. **Free-text and HUMAN messages** have no timeout state; a human message in the main room reaches
   an agent ONLY if it mentions it (or `@room`); otherwise only `ignored.log` (DOC l.216-246).
7. **Questions** (`Q<n>`): their state is the room history itself (numbering and open/closed read
   from history, S211 l.76,483-499). A v2 room with no history restarts numbering unless imported.
8. **Presence**: `agent_bus.status` `listening` + `until` (600 s TTL, renewed every 300 s by the
   watcher; goes stale, never cleared). Pure soft state.

### 2.4 Delivery into the Claude session and hooks

- Default = SOCKET delivery: watcher connects `$CLAUDE_CODE_MESSAGING_SOCKET`, sends NDJSON `auth`
  - `user` lines; message appears under Claude Code's "Another Claude session" label; needs
    `crossSessionInbound: accept`, decided ONCE at launch (settings read once, RL 6.11). Two
    messages \<2 s apart or identical within 30 s are dropped by the socket, hence >=2 s pacing and a
    per-delivery nonce (RP 4).
- Processes: hooks (short-lived `pingbus hook <event>`), supervisor `watch --supervise` (restarts
  child 1->60 s backoff; stops on socket gone / exit 0 / 8,10,64,78), watcher child (one thread per
  team, 30 s long-poll, transient retry 1->30 s from the stored token), seat keeper (`seat keep`:
  the only seat-lock holder, polls desired membership every 0.5 s). Members are loaded ONCE at
  watch start (`helpers/pingbus/cli.py:2174`); the token is re-read from file on every use
  (`config.py:354`), but `base_url`/`user_id`/`room` are held from the process start (INFERENCE:
  changing them needs a watcher restart).
- Hooks (kit `hooks.json`, guarded by `$PINGBUS_MEMBERSHIP`/`active`): SessionStart (survey,
  start supervisor, seat lines, role lines, pending counts), UserPromptSubmit (revive watcher,
  membership notices, pending), Stop (blocks once per distinct pending set when no waker),
  SessionEnd (goodbye to humans+coordinators; bounded by
  `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000`), PostToolUse (relay only). Status line `pingbus status-line` every 25 s (was 1 s). RP 4.
- Read receipts: public `m.read` for each HUMAN item only after the nonce is found in the
  transcript (so humans see who got it).
- Goodbye signals: SessionEnd goodbye; host `seat farewell` (remove); host `seat disconnected`
  (container gone without a goodbye, keyed on `seat.lock` claim vs `said-goodbye.json`). Not
  covered: host reboot, launcher SIGKILL (RP 4).

---

## 3. Humans' Element logins

- Per team: one Element Desktop profile (Flatpak `im.riot.Riot`, `--profile <team>`), config
  `~/.var/app/im.riot.Riot/config/Element-<team>/{config.json 0600, electron-config.json}`, launcher
  `~/.local/share/applications/agent-bus-<team>-element.desktop`. `config.json` overrides EVERY
  outbound key explicitly (default homeserver = the team's `base_url`, no identity server, no
  telemetry, spellcheck off). Written by `element.py apply`, run by the separate
  `play-agent-bus-element` play from `agent_bus_element_teams`. A team = its own Matrix server =
  its own Element window; one account per window (RL 2; `element.py:1-23`; RH 1.6, 6 row Element).
- Address: `http://<bus-address-or-127.0.0.1>:<team port>`; a phone reaches it over WireGuard (DOC
  "The human's account"). No TLS.
- Credentials: a 32-char password shown once by `agent-bus human password`; stored only in the
  human's password manager / Element session. The session token is in the Element profile dir.
- Loss if the server changes (v2 = new instance, new account store):
  1. the Matrix account itself: user ID, password and every device/session (profile is bound to the
     old `base_url`/`server_name`); humans need a NEW password and a new/updated Element profile;
  2. room history in Element (E2EE is off, so history lives only on the server; no client-side
     copy beyond Element's cache);
  3. read markers / receipts, notification settings, room mute (they muted main to follow the
     reports and questions rooms, DOC l.260-262), pins;
  4. the profile's Chromium state if the profile dir is renamed (INFERENCE).
     A removed human's name is burnt (`removed_humans`), so a v2 account for a still-listed human must
     be new, not "returned".
- Throttle: 3 failed logins then 1 per 5 min; 5 logins then 1 per 6 min, in memory only, emptied by
  a homeserver restart (RH 7, `health.py:22-25`).

---

## 4. Tuwunel facts for migration

| Question                   | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     | Status                                                                                                                                                                                                                         |
| -------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Two instances side by side | v1 already runs one unit per team, own `db/`, port, unit, secrets, firewall record; teams isolated by `TemporaryFileSystem` + `PrivatePIDs`; port and name uniqueness checked across installed teams (RH 5, 6, 7.20). A v2 instance with its own name/dir/port is the same pattern.                                                                                                                                                                                                                                          | FACT (pattern); a SECOND instance for the same team name is not possible in v1 (unit, folder, firewall record are keyed by team name) so v2 needs its own name/unit/dir. Shared uid `agent-bus` is v1's; v2 may choose another |
| `server_name` immutable    | Yes, and enforced in v1 (`render check --previous`). New instance may reuse the same `<sn>` string (separate DB) but Element, tokens and every bundle pin `base_url`+`server_name`, so a clash serves nothing; recommend a distinct `<sn>`                                                                                                                                                                                                                                                                                   | FACT / INFERENCE                                                                                                                                                                                                               |
| Import/backfill history    | No admin command or endpoint for importing room events is recorded anywhere in the repo. Tuwunel serves 69 of the Synapse admin endpoints (`docs/development/compliance/synapse-admin.md`, TUW 3); none documented there is an event importer. Appservice `?ts=` timestamp massaging: appservices are "always allowed to create rooms" (00161 DESIGN l.1222-1226) but no appservice or `?ts=` use exists in v1 and Tuwunel's support is not evidenced.                                                                       | UNVERIFIED. Treat history as NOT importable unless a probe proves `?ts=` or an import route. Fallback evidence: export (below)                                                                                                 |
| History export (lossless)  | Plan 00178 (Not Started): `agent-bus export <team>` pages `/messages dir=f` with the admin token into `events.jsonl` + `history.md`. Not built. `/messages` is used already by `history` (1-500, \<=50 pages) and gap fill                                                                                                                                                                                                                                                                                                   | FACT (plan only)                                                                                                                                                                                                               |
| Read-only room             | Power levels `events_default: 100` (and keep `users_default`) would stop everyone below admin posting; admin as v12 creator keeps unlimited power. BUT v1 members treat the room as untrusted unless the power levels are EXACTLY the v1 shape (PROTO l.455-495; `syncer.py:295-313`), so changing them makes every v1 member exit 10 and drop the team: a hard fence, not a soft freeze                                                                                                                                     | FACT; side effect to design for. `agent_bus.status` at 0 also lets any member write status                                                                                                                                     |
| Tombstone / predecessor    | Room v12 (creator power, no `additional_creators`); `m.room.tombstone` power 150 (nobody holds it; `/upgrade` needs tombstone power no member holds; the admin as creator can). Whether Tuwunel 1.9.4's `/upgrade` or a hand-sent `m.room.tombstone` works, and how Element renders `predecessor` across different SERVERS, is not evidenced. A tombstone's `replacement_room` is a room on the SAME server in practice; cross-server it only helps if Element follows it, which it does only for rooms its account can join | UNVERIFIED. A tombstone cannot move accounts or history to another instance                                                                                                                                                    |
| Backup                     | Daily + before restarting plays: `SIGUSR2` to the HS main PID (needs `admin_signal_execute`; `wait_ready` first or the signal is DROPPED in the PID-1 namespace), RocksDB backups numbered under `<t>/backups/meta/<n>` (7 kept) + root-only state tar with the same number; `restore --backup <n>` stops, restores, restarts, re-bootstraps; not atomic after the stop (B8)                                                                                                                                                 | FACT. Usable as the rollback point; restore also unstamps                                                                                                                                                                      |
| Config quirks that bite    | no `[global]`; unknown key -> exit 1; `max_request_size` floor; btrfs needs `rocksdb_allow_fallocate=false`; `PUT v2/users admin:false` = 500; `GET /login` lists `m.login.token` while `login_via_token=false`; no per-user send rate limit in 1.9.4; password reset+logout is the only revoke; Tuwunel exits 0 on SIGTERM (so `Restart=always`); sync filter state types are fixed (`create, power_levels, member, agent_bus.team, agent_bus.status, topic`, + side-room types)                                            | FACT (RH 7; SPEC-matrix M40)                                                                                                                                                                                                   |
| Admin-room commands        | `!admin users create-user` writes the password into admin-room history and `--execute` puts it in the journal; v1 deliberately avoids both and uses the shared-secret and `PUT v2/users` routes                                                                                                                                                                                                                                                                                                                              | FACT (TUW 3). No other admin command is relied on                                                                                                                                                                              |
| Account export             | None. Tokens cannot be copied to another instance; accounts are re-created (random discarded password) and tokens re-minted                                                                                                                                                                                                                                                                                                                                                                                                  | FACT/INFERENCE                                                                                                                                                                                                                 |

---

## 5. Starting and restarting members, kit pick-up, supervisor

- **ccy (podman)**: the launcher (`ccy --teams <seat>@<team>[,...]`) runs `agent-bus seat take`
  (sudo; waits up to 180 s for a homeserver still starting; allocation lock per checkout; places the
  bundle by fd), then the container entrypoint execs `pingbus seat exec -- claude ...` (spawns the
  keeper, waits \<=30 s READY, writes session `settings.json`, sets `PINGBUS_HOME`/`PINGBUS_TEAMS`).
  The pingbus zipapp + plugin/skill are BAKED into the ccy image (`COPY optional/`); every
  pingbus change needs a CCY and container version bump (CLAUDE/ContainerRules.md; RL 6.17; S211 l.380-385).
  `--teams` list is stored in the session registry so restart/restore replay it.
- **Picking up a new kit**: NOT live. Plugin hooks and `settings.json` (incl.
  `crossSessionInbound`) are fixed at launch; a running container keeps its old pingbus until its
  session restarts (S211 l.383; RL 6.11). A session launched on no team gets
  `crossSessionInbound: refuse`, so its FIRST join relaunches it once via the supervisor restart path
  (RL 3.3, H22/H34). Host `cc` picks up `/usr/local/bin/pingbus` on relaunch.
- **Supervisor (ccy)**: the ccy tmux supervisor restarts a session in place when `relaunch.json {"why":"restart"}` appears in the membership dir (`ccy-teams restart <team>`: at the next idle
  moment, `--resume <recorded id>` never `--continue`, 300 s/180 s bounds; stopped records are
  started again; `cc` left running; LXC left out) (RL 3.5; 00203 SPEC-team-add-and-restart B1-B11).
  `ccy-teams shutdown <team>` types `/exit` into panes then hard-kills after 60/30 s and waits for
  each seat lock to free. `session add|remove|move|role|repair` hot-control membership with no
  restart (RL 3.3). Inside a container: `watch --supervise` restarts the watcher child, keeper dies
  with its parent via `PR_SET_PDEATHSIG`.
- **LXC members**: no seat, no hot control (TR A10). Applied by the Ansible play
  (`helpers/agent_bus_lxc/members.py`, as root from the host): checkout read from the host as root
  by walking the container's rootfs (never runs anything in it); pre-checks per container; kit
  synced (tar of `/usr/local/share/agent-bus/kit`, root-owned, `python3 -I` in the guest, Python >= 3.11 needed, links
  `/usr/local/bin/{pingbus,agent-bus-claude}`); join each member (all demotions before promotions);
  merge env file; `pingbus config check` as the agent; optional `agent-bus-claude-<user>.service`
  (`Restart=always`, tmux). `agent-bus-claude` execs `claude --plugin-dir <kit>/plugin/pingbus --settings <kit>/settings.json`. After a kit change the human must restart the session
  (lxc-add tells them; it looks for a session older than the add) (RL 1.3-1.9).
- **Stopped containers** (owner requirement: the play must not need LXC running): not RUNNING =
  SKIPPED (known active handle) or DEFERRED (no known handle yet) with a one-line why and one
  `LXC-SUMMARY`; never fails (missing container still fails); a skipped member's handle is claimed,
  a deferred one switches `unheld_ok` for its team so new members take the next number; absent +
  stopped parks the handle host-side immediately and leaves bundle/env/unit until the first run
  with it up. Applied record `/var/lib/agent-bus-applied/lxc/<team>.json`
  `{version:1,team,members:[{container,user,state,status:joined|stopped|deferred|left,handle|null,checkout,why,since}]}`
  (RL 1.5, 1.8). The member record duplicates the registry only because the host cannot read a stopped
  container's bundle (RL 7.04).
- **Members on other machines** (remote, WireGuard): kit/bundle managed by their own IaC; v1
  remote members "may run v1 for a long time" (DESIGN 10.1). A v2 migration cannot reach them from
  here (INFERENCE).

---

## 6. ccy-teams and meta-deploy today

`ccy-teams` (Go, `src-go/cmd/ccy-teams`, `src-go/internal/teams/*`; usage text
`src-go/internal/teams/usage.txt`). It "holds no team logic of its own": it acts through
`agent-bus`, `agent-bus-install`, `ccy` and the session registry.

| Area           | Commands                                                                                                                                                                                                                           | Reads / writes                                                                                                                                                                                                                                                                                                                                             |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| View           | picker (default), `-l/--list`                                                                                                                                                                                                      | `agent-bus list <team>` + `human devices` (NOPASSWD sudo rule `/etc/sudoers.d/agent-bus-read-only`, anchored regex), `agent-bus health` records `/var/lib/agent-bus-applied/health/<t>.json`, applied stamps, `agent-bus-hs@` unit state, host_vars via `declare overview` (APPLIED verdict), session registry `~/.local/state/ccy/sessions/`, `lxcrecord` |
| Team life      | `create` (writes `agent_bus_teams` + `agent_bus_element_teams`, then applies that team alone), `apply`, `remove`, `backup`/`backups`/`restore` (via `agent-bus-install`, needs user's own sudo password), edit settings, `lxc-add` | host_vars `localhost.yml` with `.backup.<UTC>`; plays through run.bash single-play mode, under the user play lock `$XDG_RUNTIME_DIR/fedora-desktop-plays.lock`                                                                                                                                                                                             |
| Seats/sessions | `session list/add/remove/move/role/repair`, `choose-teams`, `restart`, `shutdown`, `wind-down`, login (`human password`), set-role, park                                                                                           | desired membership dir, `agent-bus seat take/remove`, tmux `-L ccy`                                                                                                                                                                                                                                                                                        |

Where it reads team state: host_vars (declared), `/var/lib/agent-bus-applied/*` (stamps, health,
lxc), `sudo agent-bus list`, session registry, `$XDG_RUNTIME_DIR/ccy/*`. Starts sessions: `ccy --teams ...` or `ccy-teams session add`.

`CLAUDE/Plan/meta-deploy.bash` runs the queue in `CLAUDE/Plan/meta-deploy.list`: one entry per
line; three kinds: `<plan folder>` (its triage.bash, deploy.bash, acceptance.bash in order),
`playbooks/<path>.yml` (a play no plan owns), `<plan folder>/<script>.bash` (one plan script);
`#` lines describe the next entry; blank lines ignored; anything else is fatal; HOST ONLY; `--list`
shows without running; plan names validated before anything executes; summary under
`untracked/plan-runs/_meta-deploy/<newest>/summary.txt`. A migration run is added as a plan-folder
entry with an explicit `#` description; the migration itself must be a plan script or a Go
subcommand, not a hand-run command list (CLAUDE.md "Host runs go through meta-deploy.bash").

---

## 7. Known incidents and lessons relevant to migration

| #   | What happened                                                                                                                                                                                       | Lesson for migration                                                                                                                                                                               | Cite                                          |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| 1   | SessionEnd hooks bounded to 1.5 s: no goodbye was EVER sent until `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000`                                                                                    | any "farewell/move" notice from a member's session must fit the default budget or set the env                                                                                                      | RP 5.01                                       |
| 2   | `podman stop`/closed terminal kills supervisor with no SessionEnd; only host `seat disconnected` covers it; host reboot and launcher SIGKILL are not covered                                        | a migration must not rely on member goodbyes to know a member is gone; it must read the seat lock and desired-membership state                                                                     | RP 4; RL 4                                    |
| 3   | Watcher died on a homeserver restart (refused connection treated as permanent); fixed E25 (retry from stored token; status-line `(down)`)                                                           | stopping v1's server while members run is survivable for ccy members that retry, but a 401/403/untrusted ends a team's thread permanently (exit 8/10) and the supervisor does not restart on those | RP 5.10; RL 6.12                              |
| 4   | v1 `first sync` uses `limit 0`; a removed team's in-flight poll is consumed undelivered; `seat retire` deletes the sync token (re-add = first sync)                                                 | nothing posted while out is ever delivered: a cut-over necessarily drops "not yet synced" events unless drained first                                                                              | RP 3, 5.25                                    |
| 5   | Stale/lost tokens: no rotate-admin; a lost admin token refuses and names restore; lost bundle = add/park cycle (absent then present) for the same handle; a numbered handle never issued is refused | the v2 admin identity must be created fresh; no v1 token is reusable                                                                                                                               | RH 7.02; RL 1.6                               |
| 6   | Stopped LXC container failed the whole play (exit 78) and blocked every team change; fixed with SKIPPED/DEFERRED; "stopped path not yet proven live on the host"                                    | the migration must treat stopped members as ordinary (deferred, completed when started), not as a blocker; the live-stopped path is unproven                                                       | RL 6.01                                       |
| 7   | Inherited umask 077 via `lxc-attach` made the kit dir 0700 (`Permission denied` exit 126); guest scripts now set umask first                                                                        | guest-side writes must set umask and name every leaf mode                                                                                                                                          | RL 6.02                                       |
| 8   | Live role change on a declared LXC member reverted by the next apply (entry vs live)                                                                                                                | when moving a role, change the declaration and the live state under one lock hold                                                                                                                  | RL 6.06                                       |
| 9   | Race on container names, reused tmux names inheriting stale membership dir, change lock inside the RO mount held for ever                                                                           | locks in `$XDG_RUNTIME_DIR`, never in the checkout/mount; v2 locks need the same reach rule                                                                                                        | RL 6.07-6.10                                  |
| 10  | Kit replaced IN PLACE under running sessions; Python >= 3.11 required in every container; Python startup ~1 s per hook                                                                              | v2 kits should be a directory per version; a session keeps its kit until restart                                                                                                                   | RL 7; DESIGN 12                               |
| 11  | `rate limited: duplicate` refused two legitimate `disconnected` notices (same text within the window)                                                                                               | an identical migration notice to several targets must differ per recipient or be exempt                                                                                                            | 00194 JOURNAL 26-10-19 (Task 2.0i)            |
| 12  | A human's `@room` typed as plain text mentioned nobody: logged but undelivered                                                                                                                      | migration notices to humans/agents must use real mentions                                                                                                                                          | 00194 JOURNAL 26-10-19 (Task 2.0f); DOC l.216 |
| 13  | Restore is not atomic after the stop (B8); no installer lock vs play/backup (B16); `backup-now` before `wait_ready` drops the signal                                                                | any migration step that stops/starts a v1 homeserver must take a backup first and treat restore as a manual last resort                                                                            | RH 7 latent bugs, 7.14                        |
| 14  | Persistent timer stamp of a removed team survives and a new team of the same name inherits a backup run                                                                                             | do not reuse a v1 team NAME for the v2 instance                                                                                                                                                    | RH 7.17                                       |
| 15  | Old pingbus reads an unknown team-record key or a named seat as "untrusted"; deploys of protocol additions were staged member-first for this reason                                                 | a v1 member in a room with changed power levels/record drops the team (exit 10); any in-place room change is a fence for v1 members                                                                | S211 l.183-241; PROTO l.455                   |

### What the migration must account for (checklist derived from above)

1. Drain: for every member with state, run a sync until `inbox/` is empty and `outbox.json` has no
   unreported pings; members not running (parked, stopped LXC, other machines) hold unsynced events
   on the v1 server that only an export (Plan 00178, not built) preserves.
2. Accounts and tokens are not portable: every member gets a new bundle (new handle string allowed
   by the v2 design), every human a new password and Element profile.
3. History cannot be assumed importable (section 4); v2 needs a decision between "archive only"
   and a probed import route.
4. Running sessions keep their old kit/settings until restarted; ccy can restart in place
   (`relaunch.json`), LXC members and remote members cannot be restarted by ccy-teams.
5. The v1 team must stay intact until commit: backup first (`backup-now`), keep the v1 unit
   running or stopped-but-built, a v1 name is not reused.
6. The only read-only fence on v1's room that members respect is breaking the exact power-level
   check, which also stops members (exit 10); consider a team-record or status-based notice plus
   stop instead.
