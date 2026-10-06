# Research: Tuwunel and the Matrix client-server API (Plan 00161, Task 1.1)

Scope: the homeserver facts the `agent-team` play needs, and the minimal client-server calls
`pingbus` and the warden need. Everything below was read from the upstream source at the
release tag, or from the Matrix spec source at its release tag. Each section gives sources.
"To verify" marks something that was not checked and needs an acceptance test.

Sources used throughout:

- Tuwunel repo, tag `v1.9.3`: https://github.com/matrix-construct/tuwunel/tree/v1.9.3
- Tuwunel example config (generated from `src/core/config/mod.rs`):
  https://github.com/matrix-construct/tuwunel/blob/v1.9.3/tuwunel-example.toml
- Matrix spec source, tag `v1.19` (latest, published 2026-07-08):
  https://github.com/matrix-org/matrix-spec/tree/v1.19 and rendered at
  https://spec.matrix.org/v1.19/client-server-api/

## 1. Release, image, pinning

| Fact                                | Value                                                                                                                                                                                     | Source                                                                  |
| ----------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| Current release                     | `v1.9.3`, published 2026-09-25 (v1.9.2 2026-09-21, v1.9.1 2026-09-12)                                                                                                                     | https://github.com/matrix-construct/tuwunel/releases/tag/v1.9.3         |
| Unreleased on `main`                | 134 commits ahead of `v1.9.3` (includes the new login rate limits, section 6)                                                                                                             | GitHub compare `v1.9.3...main`                                          |
| OCI image (GHCR)                    | `ghcr.io/matrix-construct/tuwunel:v1.9.3` (also `:latest`, `:preview`, `:main`)                                                                                                           | `docs/deploying/docker.md`, `.github/workflows/publish.yml`             |
| OCI image (Docker Hub mirror)       | `docker.io/jevolk/tuwunel:v1.9.3`                                                                                                                                                         | same                                                                    |
| Multi-arch index digest of `v1.9.3` | `sha256:678b7f5350e06a41614444497c587da9dddf66767e4068a27480402f3c1367d0` (same digest as `:latest` today)                                                                                | registry HEAD on `ghcr.io/v2/matrix-construct/tuwunel/manifests/v1.9.3` |
| amd64 (x86-64-v1) manifest digest   | `sha256:5c87cd5518b3ba2d3661d6840b8e7752d40f6153ea31d4883b61e41ed9464fa7`, also tagged `v1.9.3-release-all-x86_64-v1-linux-gnu`                                                           | same index; per-variant tag naming from `publish.yml`                   |
| Other index entries                 | arm64; amd64 variant v2 (`sha256:3ab60a17…`); amd64 variant v3 (`sha256:94b52148…`)                                                                                                       | same index                                                              |
| Signing                             | **None found.** No cosign signature (`sha256-<digest>.sig` tag: 404), no `.att` tag, no OCI referrers, no GitHub artifact attestations (API 404), no cosign/attest step in `publish.yml`. | registry probes; `.github/workflows/publish.yml`                        |

Implications for the play:

- Pin by digest: `Image=ghcr.io/matrix-construct/tuwunel@sha256:678b7f53…` (or the tag plus the
  digest). A tag alone is mutable and there is no signature to verify. A version bump means
  updating the digest, which suits `scripts/check-pinned-versions.bash` (the `update-versions`
  skill).
- The image is built from scratch with `tini`, CA certificates and the binary: **no shell**. So
  every health or exec command must use the JSON exec form.
- Data lives in `/var/lib/tuwunel` (the only directory it writes; `database_path` default).
- Release assets also include `.rpm`, `.deb`, static `.zst` binaries and `-oci.tar.zst`
  archives, but none carry checksums or signatures in the release listing.

## 2. Configuration: file versus environment

Source: `src/core/config/mod.rs` (`Config::load`, `file_paths`, `merge_environment`) at v1.9.3.

- Config files: `TUWUNEL_CONFIG` (also the legacy `CONDUIT_CONFIG`, `CONDUWUIT_CONFIG`) names a
  TOML file, and `-c/--config` adds more. Later files override earlier ones. A path that does not
  exist is a hard error. A file may use a `[global]` table or bare keys, but not both.
- Environment: `Env::prefixed(prefix).global().split("__")` for each of the prefixes `CONDUIT_`,
  `CONDUWUIT_`, `TUWUNEL_`, applied in that order. **Environment overrides files.** So the key
  `allow_federation` is `TUWUNEL_ALLOW_FEDERATION`. A nested key uses `__`, for example
  `[global.well_known] client` is `TUWUNEL_WELL_KNOWN__CLIENT`. Values are TOML-parsed, so arrays
  are written as `TUWUNEL_ADDRESS=["0.0.0.0"]` and `TUWUNEL_TRUSTED_SERVERS=[]` (upstream's
  `quadlet/tuwunel.env` uses this form).
- Command line: `-O key=value` (TOML syntax) overrides last. `--execute "<admin command>"` runs
  admin commands at startup (section 3).
- Many keys are marked `reloadable: yes` in the example config.
- Recommendation: one TOML file per team, mounted read-only, with secrets in separate files
  (`registration_token_file`, `registration_shared_secret_file`) rather than environment
  variables. Environment variables show up in `podman inspect`.

### The keys the issue lists (all checked in v1.9.3)

| Key                                             | Default                       | Notes                                                                                                                                                                                                                           | Where                                                                  |
| ----------------------------------------------- | ----------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `server_name`                                   | none                          | Cannot change without wiping the database.                                                                                                                                                                                      | example config line 38                                                 |
| `address`                                       | `["127.0.0.1", "::1"]`        | "An address set here must bind or the server refuses to start." In a container on NAT networking it must be `"0.0.0.0"` (the comment says so); host-side binding is then done by `PublishPort=<host-local-ip>:<hostport>:8008`. | line 67                                                                |
| `port`                                          | `8008`                        | "If you are using Docker, don't change this, you'll need to map an external port to this."                                                                                                                                      | line 79                                                                |
| `unix_socket_path`                              | unset                         | Alternative to TCP.                                                                                                                                                                                                             | line 89                                                                |
| `max_request_size`                              | `24 MiB` (`24 * 1024 * 1024`) | **Startup refuses below `10_000_000` bytes** (decimal 10 MB, not 10 MiB): `Err!(Config("max_request_size", "Max request size is less than 10MB…"))`. Accepts an integer or `"24 MiB"`.                                          | `src/core/config/check.rs` line 450; mod.rs `default_max_request_size` |
| `allow_registration`                            | `false`                       | If `true` with no token, startup fails unless `yes_i_am_very_very_sure_i_want_an_open_registration_server_prone_to_abuse = true`.                                                                                               | check.rs `check_registration`                                          |
| `registration_token`                            | unset                         | An empty string is a startup error.                                                                                                                                                                                             | line 775                                                               |
| `registration_token_file`                       | unset                         | Whitespace-separated tokens; must be readable and not empty, or startup fails.                                                                                                                                                  | line 786                                                               |
| `registration_shared_secret` / `_file`          | unset                         | Enables Synapse-style shared-secret registration (section 3). Read from the file on every use, so it can be rotated without a restart.                                                                                          | lines 788-799; `src/service/admin/register.rs`                         |
| `allow_federation`                              | `true`                        | Set `false`.                                                                                                                                                                                                                    | line 886                                                               |
| `trusted_servers`                               | `["matrix.org"]`              | Set `[]`.                                                                                                                                                                                                                       | line 1303; mod.rs `default_trusted_servers`                            |
| `new_user_displayname_suffix`                   | a heart emoji                 | **Appended to the display name at registration.** Set `""` so the display name is exactly the handle (issue section 2).                                                                                                         | line ~52                                                               |
| `client_sync_timeout_min` / `_default` / `_max` | `5000` / `30000` / `90000` ms | **A `/sync?timeout=0` is clamped up to 5 s** (see section 5.6). Set `client_sync_timeout_min = 0` for a non-blocking `recv`.                                                                                                    | lines 311-325; `src/api/client/sync/v3.rs` lines 296-305               |
| `auto_accept_invites`                           | `false`                       | Keep it off: workers must check the room creator before joining.                                                                                                                                                                | line 2917                                                              |
| `federate_admin_room`                           | `true`                        | Can only be set before the admin room is created; set `false` for tidiness.                                                                                                                                                     | line 3050                                                              |
| `admin_escape_commands`                         | `true`                        | Server admins can run `\!admin …` in **any** room, and the reply is public. Set `false`, or make sure no human or agent account is a server admin.                                                                              | line 2959                                                              |
| `allow_encryption`                              | `true`                        | Optional `false`: the bus is not E2EE, and the warden must read `m.mentions`.                                                                                                                                                   | line 866                                                               |
| `server_user_localpart`                         | `conduit`                     | The server bot `@conduit:<server_name>` posts in the admin room. The warden should ignore it.                                                                                                                                   | line 3020                                                              |
| `sentry`                                        | `false`                       | Off by default, as the issue says.                                                                                                                                                                                              | line 3056                                                              |
| `default_room_version`                          | `12` (`RoomVersionId::V12`)   | Matters for power levels (section 5.2).                                                                                                                                                                                         | mod.rs `default_default_room_version`                                  |

Startup and stop for the Quadlet (from `docs/deploying/docker.md` and `quadlet/tuwunel.container`):
`PodmanArgs=--stop-timeout=1800` plus `[Service] TimeoutStopSec=1830`, so a database migration
is not killed part-way. Upstream's own unit uses `Image=ghcr.io/matrix-construct/tuwunel:latest`
and `PublishPort=8008:8008`, which binds on all interfaces. **Do not copy that**: bind to the
host-local address only.

## 3. Admin and user creation

### First admin

- `grant_admin_to_first_user = true` (default): "technically the next user to register when the
  admin room is empty (or only contains the server-user) is granted". The grant joins the user
  to the admin room at power level 100 (`src/service/admin/grant.rs`, `make_user_admin`).
- This applies to `/register` **and** to the admin-console `create-user`
  (`src/admin/user/create_user.rs` passes `grant_first_user_admin: true`).

### Ways to create accounts (v1.9.3)

| Route                                                                         | Needs `allow_registration`?           | Auth                                                     | Notes                                                                                                                                                                                                                                                                                                    |
| ----------------------------------------------------------------------------- | ------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `POST /_matrix/client/v3/register` with `m.login.registration_token`          | yes                                   | registration token                                       | The issue's current plan.                                                                                                                                                                                                                                                                                |
| Admin room: `!admin users create-user <username> [password]` (alias `create`) | **no** (no check in `create_user.rs`) | admin room membership                                    | Replies with `Created user with user_id: … and password: \`…\``. **The password is written into the admin room's history** (stored in the database). With no password given, a 25-character password is generated. Rejects a localpart that fails `validate_strict()`unless`emergency_password\` is set. |
| Startup `--execute "users create-user <name>"` / `admin_execute = [...]`      | no                                    | the command line                                         | The output goes to the log, so the password ends up in the journal. Avoid this.                                                                                                                                                                                                                          |
| `GET` then `POST /_synapse/admin/v1/register` (shared secret)                 | **no**                                | HMAC-SHA1 over a nonce with `registration_shared_secret` | Unauthenticated nonce `GET`; the nonce lasts 60 s and is held in RAM. `admin: true` makes the account a server admin. Returns `access_token` and `device_id` unless `inhibit_login`. Usernames are lowercased and must pass `validate_strict`. Source: `src/api/client/admin/register.rs`.               |
| `PUT /_synapse/admin/v2/users/{user_id}`                                      | **no**                                | admin access token                                       | Creates or modifies an account: `password`, `displayname`, `admin`, `deactivated`, `locked`. Source: `src/api/client/admin/users/create_or_modify.rs`. Uses `users.create`, not `full_register`, so to verify: is the default push-rules or profile setup skipped, and does that matter for a bot?       |
| `POST /_synapse/admin/v1/users/{user_id}/login`                               | n/a                                   | admin access token                                       | **Mints an access token for another local user** with no password. Body `{"valid_until_ms": <optional>}` gives `{"access_token": "…"}`. Creates a device named `Admin login`, revoked by that user's logout. The admin cannot use it on itself. Source: `src/api/client/admin/users/login_as.rs`.        |

The list of Synapse admin endpoints Tuwunel serves (69 supported) is in
`docs/development/compliance/synapse-admin.md`. The doc also says: "Tuwunel has no per-user
request limiter to override".

### Recommendation (to put to DESIGN.md)

Registration can stay **closed from the first boot** (`allow_registration = false`; there is
then no `registration_token` at all):

1. Generate a high-entropy `registration_shared_secret_file` (mode 0600, mounted read-only).
2. Provisioning creates the team admin account with shared-secret register and `admin: true`.
   It keeps the returned admin `access_token` in a 0600 file. Treat the shared secret as
   equal to a server-admin token: the upstream comment says the same.
3. For each member: `PUT /_synapse/admin/v2/users/@<handle>:<server_name>` with a random
   password (discard it, or keep it 0600) and `"displayname": "<handle>"`, then
   `POST /_synapse/admin/v1/users/@<handle>:<server_name>/login` to mint the member's token.
   The member never needs a password login, which avoids the login rate limits coming in the
   next release (section 6).
4. `rotate-token` for a member: log out its device(s) through the admin API, then mint again.

This removes the issue's "first account is admin, register it before any member" race. No
password or token goes through a chat room or a log. It also avoids parsing the admin bot's
chat output. The issue's text ("allow_registration = true with a registration_token") still
works, but decision 4 in the issue (sign-ups closed) is then met only after provisioning, not
from the start.

The bundled shared-secret client in `synapse_admin_api` follows Synapse's HMAC:
`HMAC-SHA1(secret, nonce \0 username \0 password \0 ("admin"|"notadmin") [\0 user_type])`,
hex-encoded, in `mac`. Python standard library: `hmac.new(secret, msg, hashlib.sha1).hexdigest()`.

## 4. Health and readiness

- `GET /_tuwunel/server_version` returns 200 when serving (`src/main/health.rs`, which probes
  it). It needs no auth. `GET /_matrix/client/versions` (standard, no auth) also works.
- `tuwunel --health-check` reads the same configuration (environment and `TUWUNEL_CONFIG`, but
  **not** command-line arguments) and probes each listener, mapping `0.0.0.0` to `127.0.0.1`.
- Podman does not read the OCI image's healthcheck (containers/podman#25454, #18904), so the
  Quadlet must declare it: `HealthCmd=["/usr/bin/tuwunel", "--health-check"]`,
  `HealthInterval=30s`, `HealthTimeout=15s`, `HealthStartPeriod=1800s`, `HealthRetries=3`
  (upstream `quadlet/tuwunel.container`). The exec form is required because the image has no
  shell.
- The first boot after an upgrade may migrate the database before the listener opens. A
  readiness wait in the play should poll `/_tuwunel/server_version` with a bound, not assume a
  fixed delay.

## 5. Client-server API calls pingbus needs (spec v1.19)

Base: `http://<host-local-addr>:<port>/_matrix/client/v3`. Auth:
`Authorization: Bearer <access_token>`. The `?access_token=` query form also exists, but avoid it
because it puts the token in URLs and logs. All bodies are JSON.

### 5.1 Login (only if a password login is used)

Source: `data/api/client-server/login.yaml`.

`GET /login` returns `{"flows":[{"type":"m.login.password"},{"type":"m.login.token",…}]}`.

`POST /login`:

```json
{"type": "m.login.password",
 "identifier": {"type": "m.id.user", "user": "<localpart or full MXID>"},
 "password": "<pw>",
 "device_id": "<stable id, optional>",
 "initial_device_display_name": "pingbus <handle>"}
```

200 gives `{"user_id","access_token","device_id", "expires_in_ms"?, "refresh_token"?}`. Tokens do
not expire unless `refresh_token: true` was sent. Errors: 403 `M_FORBIDDEN` or
`M_USER_DEACTIVATED`, 429. `m.login.token` (`{"type":"m.login.token","token":…}`) is for short-lived
SSO-style tokens (`login_via_token` default true); `pingbus` has no use for it. With the admin
`login` mint (section 3), pingbus never calls `/login`. `GET /account/whoami` checks a token
(`{"user_id","device_id"}`).

### 5.2 createRoom with power levels and custom state

Source: `data/api/client-server/create_room.yaml`; room version 12:
https://spec.matrix.org/v1.19/rooms/v12/.

```json
POST /createRoom
{"preset": "private_chat",
 "visibility": "private",
 "name": "<work item>",
 "topic": "Commands: !halt, !halt all, !status, !sync <ref>, !fetch <ref>, !help",
 "creation_content": {"m.federate": false, "additional_creators": ["@<human2>:<sn>"]},
 "power_level_content_override": {
   "users": {"@<orchestrator>:<sn>": 50},
   "users_default": 0,
   "events_default": 0,
   "state_default": 100,
   "invite": 100, "kick": 100, "ban": 100, "redact": 100,
   "events": {"m.room.power_levels": 100, "m.room.tombstone": 150,
              "<ns>.role": 100, "<ns>.status": 0}
 },
 "initial_state": [
   {"type": "<ns>.role", "state_key": "@<orchestrator>:<sn>", "content": {"role": "orchestrator"}}
 ],
 "invite": ["@<worker>:<sn>"]}
```

200 gives `{"room_id": "!…"}`. Order of application: create, creator join, power_levels (with the
override), alias, preset, `initial_state`, name/topic, invites.

**Room version 12 is the default in Tuwunel, and it changes the issue's power-level model:**

- Creators are the `sender` of `m.room.create` plus `content.additional_creators`. They have
  infinite power and **must not appear in `power_levels.users`**: auth rule 10.4 rejects it. So
  "humans 100" becomes "humans are creators" (the creating human plus `additional_creators`), or
  extra humans get 100 in `users`.
- With v12 the spec says the `m.room.tombstone` level MUST be above `state_default` (for
  example 150).
- The issue says both "only humans create rooms" and "the CLI sets explicit power levels when
  creating a room". With v12 these fit together only if the CLI runs **as the human** (a human
  credential), or a human creates the room in Element and then the orchestrator or CLI cannot
  raise its own power. DESIGN.md has to choose. Element's own createRoom does not set a custom
  power-level override.
- The v12 room ID is a hash of the `m.room.create` event, so the create event is bound to the
  room ID cryptographically (Tuwunel always rejects a create event bound to another room for
  v12+; see `enforce_stripped_state_pdu_validation` in the example config).

### 5.3 Invite and join

Source: `inviting.yaml`, `joining.yaml`.

- `POST /rooms/{roomId}/invite` `{"user_id": "@w:<sn>", "reason": "…"?}` returns `{}`.
- `POST /rooms/{roomId}/join` `{}` (or `POST /join/{roomIdOrAlias}`) returns `{"room_id"}`.
- `POST /rooms/{roomId}/leave` `{}`.
- Invites appear in `/sync` under `rooms.invite.<roomId>.invite_state.events`: these are
  stripped state events (`sender`, `type`, `state_key`, `content` only). **Since v1.16
  `m.room.create` is required in `invite_state`**, so a worker can check the creator's
  `sender` before joining. It must still re-check after joining (stripped state is unsigned
  client data; see 5.7).

### 5.4 Sending a ping (custom event type)

Source: `room_send.yaml`; spec "Types of room events"; "Transaction identifiers".

`PUT /rooms/{roomId}/send/{eventType}/{txnId}` with body = event content, returns
`{"event_id": "$…"}`.

- Namespacing: the spec says new event types "SHOULD follow the Java package naming
  convention, e.g. `com.example.myapp.event`". A vendor-neutral name needs a reverse domain the
  project controls, for example one derived from the public repository's GitHub Pages host.
  Avoid `m.*` (reserved) and `org.matrix.*`.
- `txnId`: unique per device and per endpoint. A retry with the same `txnId` on the same path
  returns the original `event_id`. Tuwunel implements this (`src/api/client/send.rs`
  `check_existing_txnid`). Use a UUID4 or timestamp plus counter, and **reuse the same txnId
  when retrying after a timeout** so a send happens exactly once.
- Tuwunel only blocks `m.room.encrypted` (when encryption is disabled), redactions (when
  disabled) and call invites in public rooms. Custom types pass.
- State events (for example a worker's own status): `PUT /rooms/{roomId}/state/{type}/{stateKey}`
  returns `{"event_id"}`. These **cannot** use transaction IDs.
- To verify: Element shows unknown event types only with "show hidden events" turned on, so
  pings may not be visible to watching humans unless the warden or CLI also posts an
  `m.notice`.

### 5.5 m.mentions in m.room.message (what the warden reads)

Source: `content/client-server-api/modules/mentions.md` (changed in v1.7; body-text mention
push rules removed in v1.17).

```json
{"msgtype": "m.text",
 "body": "<display name> !halt",
 "format": "org.matrix.custom.html",
 "formatted_body": "<a href='https://matrix.to/#/@<handle>:<sn>'><display name></a> !halt",
 "m.mentions": {"user_ids": ["@<handle>:<sn>"]}}
```

`m.mentions` may also hold `"room": true` (`@room`). The spec recommends clients always include
`m.mentions`, possibly `{}`. The warden takes targets only from `content["m.mentions"]["user_ids"]`
and the command only from `body` after removing the pill text. "Event bodies are considered
untrusted data": validate the shape before use (spec "Room event format" warning). Edits arrive
as `m.room.message` with `m.relates_to.rel_type = "m.replace"` and `m.new_content`, and must be
rejected (issue section 5).

### 5.6 /sync with a filter and a since token

Sources: `sync.yaml`, `filter.yaml`, `definitions/sync_filter.yaml`, `room_event_filter.yaml`,
`event_filter.yaml`; Tuwunel `src/api/client/sync/v3.rs`.

Create a filter once: `POST /user/{userId}/filter` with the filter body returns
`{"filter_id": "…"}`. Or pass the JSON inline in `filter=` (the server tells them apart by a
leading `{`).

```json
{"presence": {"types": []},
 "account_data": {"types": []},
 "room": {
   "rooms": ["!room1", "!room2"],
   "ephemeral": {"types": []},
   "account_data": {"types": []},
   "state": {"types": ["m.room.create", "m.room.power_levels", "m.room.member", "<ns>.role"],
             "lazy_load_members": true},
   "timeline": {"types": ["<ns>.ping", "m.room.member", "m.room.power_levels"], "limit": 50}
 }}
```

(The warden's filter adds `m.room.message` to the timeline types. Filter fields: `types`,
`not_types` with `*` wildcards, `senders`, `not_senders`, `limit`, `rooms`, `not_rooms`.)

`GET /sync?filter=<id>&since=<next_batch>&timeout=<ms>&set_presence=offline`.

Response (the parts that matter):
`{"next_batch", "rooms": {"join": {"<id>": {"timeline": {"events": [...], "limited": bool, "prev_batch": "…"}, "state": {"events": [...]}}}, "invite": {"<id>": {"invite_state": {"events": [...]}}}, "leave": {...}}}`. `use_state_after=true` (v1.16) swaps `state` for
`state_after`; Tuwunel accepts it.

Tuwunel behaviour:

- `timeout` is clamped to `[client_sync_timeout_min, client_sync_timeout_max]` = `[5000, 90000]`
  ms by default. **`timeout=0` waits 5 s when nothing is pending.** For a non-blocking `recv`, set
  `client_sync_timeout_min = 0` in the team config (to verify that 0 is accepted). The 90 s
  maximum bounds each long poll inside `wait`, so `wait` loops.
- The timeline limit in the room loader read (`load_left_room`) is `filter.room.timeline.limit`,
  default 10, **capped at 100**. To verify that joined rooms use the same cap. Beyond it the
  sync is `limited`.
- An unparseable `since` is treated as `0` (an initial sync): `since.parse().unwrap_or(0)`. A
  `since` above the current position is clamped. Tuwunel's tokens are integers, but pingbus
  must treat them as opaque strings.
- `set_presence=offline` stops pings from marking agents online (optional).

### 5.7 Limited sync and gap fill with /messages

Sources: spec "Syncing" (CS API index); `message_pagination.yaml`; Tuwunel
`src/api/client/message.rs`.

When `timeline.limited` is true, the spec's procedure is
`GET /rooms/{roomId}/messages?from=<previous since>&to=<timeline.prev_batch>&dir=f&limit=<n>&filter=<RoomEventFilter JSON>`.
Repeat with `from=<end>` until `end` is absent or `to` is reached. The response is
`{"start", "end"?, "chunk": [...], "state": [...]?}`; "an empty `chunk` does not necessarily
imply that no more events are available… continue to paginate until no `end` property is
returned". Both sync tokens (`next_batch`, `prev_batch`) are valid for `from` and `to`.

Tuwunel: `limit` defaults to 10 with a maximum of 1000. `to` is honoured (it stops at `to`). The
route needs the user to be joined. Then de-duplicate the combined events by `event_id`
(issue section 5).

### 5.8 Room state for creator verification

Source: `rooms.yaml` (`getRoomStateWithKey`, `?format=` added in v1.16); Tuwunel
`src/api/client/state.rs` implements `?format=event|content`.

- `GET /rooms/{roomId}/state/m.room.create/?format=event` returns the full event, including
  `sender` (the creator) and `content.additional_creators` and `content.room_version`. Without
  `format=event` only the content comes back, and the content of a v12 create event has **no
  creator field**, so `format=event` is required.
- `GET /rooms/{roomId}/state/m.room.power_levels/` returns the levels to check against the
  expected roles.
- `GET /rooms/{roomId}/state` returns all current state as client events. `GET /rooms/{roomId}/joined_members` returns `{"joined": {"@u": {"display_name"}}}`.
- Check: creators are the create `sender` plus `additional_creators`; each must be a configured
  human; otherwise `POST /leave`.

### 5.9 Rate limits (M_LIMIT_EXCEEDED)

Spec (CS API "Rate limiting", `definitions/errors/rate_limited.yaml`): HTTP 429,
`{"errcode": "M_LIMIT_EXCEEDED", "error": "…", "retry_after_ms": <int, optional, deprecated>}`.
Since v1.10, servers SHOULD send a `Retry-After` header, and `retry_after_ms` is deprecated. The
client should honour `Retry-After` (seconds or HTTP-date), then `retry_after_ms`, then fall back
to a backoff with a bound.

Tuwunel v1.9.3 has **no general per-user or per-message rate limiter**. 429 comes only from
media creation (`media_rc_create_*`), threepid, OAuth, rendezvous and policy-server paths.
Pingbus's own rate limits (issue section 3) are therefore the only send throttle on a Tuwunel
team. Whether Tuwunel's 429 carries the `Retry-After` header was not checked (it is serialised by
ruma), so handle both.

## 6. Login rate limits coming in the next release

On `main`, after v1.9.3 (commits of 2026-09-30, for example `305f0b60d` "config: Nest the login
rate limits under [global.rate_limiting]"), Tuwunel adds Synapse-style login limits under
`[global.rate_limiting.login.failed]` (`per_second = 0.17`, `burst_count = 3`) and
`[global.rate_limiting.login.account]` (`per_second = 0.003`, `burst_count = 5`). Once the burst
is used up, even a correct password gets `M_LIMIT_EXCEEDED`. That is about one successful login
per account every 5.5 minutes. Source: `tuwunel-example.toml` on `main`.

So **pingbus must store its access token and never log in per call**. Minting member tokens
through the admin `login` endpoint (section 3) avoids `/login` entirely. Set `0` to disable a
limit if provisioning ever needs repeated logins.

## 7. Points DESIGN.md has to decide

1. Registration closed from the first boot using the shared secret plus Synapse admin API
   (section 3), instead of token registration and then closing it. This makes the "admin
   first" ordering unnecessary.
2. Room version 12: humans are creators (infinite power, not listed in `users`). Settle the
   "humans create rooms" against "the CLI sets power levels" conflict (5.2).
3. Set `new_user_displayname_suffix = ""`, `client_sync_timeout_min = 0`,
   `admin_escape_commands = false`, `federate_admin_room = false`, `allow_federation = false`,
   `trusted_servers = []`, `allow_registration = false`. Keep `max_request_size` at or above
   10,000,000 bytes (default 24 MiB). In the container: `address = ["0.0.0.0"]`, `port = 8008`,
   and publish on the host-local address only.
4. Pin the image by digest. It is not signed.
5. Event-type namespace string (5.4) and whether pings also need a visible `m.notice` for
   Element watchers (to verify).
