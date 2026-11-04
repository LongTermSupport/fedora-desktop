Agent team bus: private local Matrix homeserver per team so agent sessions in separate containers can ping each other
## Summary

A private, local **agent team bus**: a Matrix homeserver per team, a small CLI that lets Claude Code sessions in separate containers **ping** each other, desktop clients so a human can watch, and hooks that wake an idle session when a ping arrives. A machine can host several independent teams.

The motivating case is cross-repository work: a large change touches several repositories, and each repository's agent runs in its own container with its own toolchain and credentials. Today the only way to coordinate is to run one orchestrator session that accumulates every repository's toolchains, credentials and configuration. That container gets heavier and riskier with each project it touches. A bus lets each container keep only what its own repository needs, while an orchestrator session hands out work and collects results.

## Hard rules (the bus carries pointers, never content)

- **A bus, not a chat room.** The record of the work is committed files; the bus only signals "go and look at this". Nothing that defines the work lives only in chat history.
- **Pings only.** A message names a verb and points at a durable artefact. The primary form is a committed file at a commit (rich context lives in committed plan or spec files; issues carry terse, human-friendly status). Issue, pull request and commit references are also allowed. All context lives in the artefact, never on the bus. Verbs are a closed set (for example `fetch`, `sync`, `review`, `run-qa`, `halt`, `ack`, `nack`, `done`, `blocked`). There is an optional note of at most 80 characters with a restricted character set.
- **Validated at both ends.** The CLI refuses to send a ping whose reference does not resolve (checked against the forge API on send), and re-validates every received ping before showing it to a session. A ping that fails validation is dropped and reported, never shown.
- **Not a prompt-injection channel.** There is no free text an agent acts on, from agents or from humans. Human text never reaches an agent at all (see section 4, the warden). The receiving session re-reads the referenced artefact from its source of truth and acts on that.
- **CLI, not MCP.** A small CLI with no dependencies beyond the Python standard library, documented by a skill, so it costs no context until it is used.

## Proposed design

### 1. Homeserver per team

- **Tuwunel** (Rust, single image, embedded RocksDB, no database server) run rootless under podman and managed as a `systemd --user` unit (Quadlet), one instance per team. Each team has its own `server_name`, port, data volume and registration token.
- Settings checked against Tuwunel v1.9.3's source and example config, and proven in a working deployment:
  - `allow_federation = false`; federation endpoints then answer 403 `M_FORBIDDEN`.
  - `trusted_servers = []`, so no key-server queries. The default is `matrix.org`.
  - `allow_registration = true` with a `registration_token`, so registration is token-only and no open registration is possible. An empty register request returns 401 offering only the token flow.
  - Keep the container's default port 8008 and map a host port to it.
  - Do not set `max_request_size` below 10 MB: Tuwunel refuses to start below that.
  - No update check, announcement fetch or telemetry exists in the source, and Sentry is off by default.
- **First account is admin.** Tuwunel makes the first account registered the server admin, so provisioning must register a dedicated admin account before any member.
- **One homeserver per team** (decided): strong isolation, and a team can be removed without touching the others.
- **Default placement is the desktop** (decided): `agent-team create` runs the team's homeserver on the host, as above. It listens only on the host-local bridge addresses its members use, never on every interface, and must be unreachable from anything else (see Privacy).
- **Elsewhere is just a URL.** A team may instead run its homeserver in its own infrastructure, or on a remote server. To the CLI and clients, a team is a URL plus a token, so nothing else changes. A homeserver off the machine changes the privacy model, though: tokens and pings cross a network on every request. So a non-local homeserver requires TLS, and must be reachable only over a protected path (VPN or firewall allow-list). The CLI should refuse a plain-HTTP URL that is not a host-local address.

### 2. Team definitions and provisioning

- A local, untracked team registry (for example `~/.config/agent-teams/<team>.toml`) with the team name, `server_name`, bind address and port, member agents and human members.
- A command (for example `agent-team create | add-member | remove-member | list | rotate-token`) that:
  - brings up the homeserver;
  - registers the admin account first;
  - registers each member;
  - writes each member's credentials (config file plus a mode-0600 token file).
- **Agent naming** (decided): one handle per agent session, `<repo>.<n>+<host>.<container-type>`, for example `myrepo.1+workstation.podman`.
  - `<repo>`: the repository the session works in.
  - `<n>`: auto-incremented per repo, host and container type. Allocated by provisioning, recorded in the team registry and never reused, so old messages are never attributed to a new session. A resumed session keeps its number: its credential lives in that project's ccy state.
  - `<host>`: the machine's short hostname. `<container-type>` is one of `podman`, `lxc`, `docker`, `vm` or `host`.
  - The handle is lowercase and is exactly the Matrix user ID's localpart, so there is no mapping: `myrepo.1+workstation.podman` is `@myrepo.1+workstation.podman:<server_name>`, and the display name is the handle. `+` is used because a localpart may only contain `a-z 0-9 . _ = - / +`: `@` would clash with mentions, and `:` is Matrix's own separator between localpart and server. Repository and host names are lowercased when the handle is made.
  - Sign-ups stay closed. Provisioning creates each account through the team's admin account: Tuwunel's admin `create-user` command does not depend on `allow_registration`.

### 3. The CLI (`pingbus`, name to be decided)

- Commands:
  - `send <verb> <ref> [--note]` is the only way to emit a ping;
  - `recv` drains the inbox without blocking;
  - `wait` long-polls until something arrives;
  - `inbox`, `show`, `status`, `peers`, `tail` (human view in a terminal), `room` (one room per piece of cross-repository work), `validate` and `config check`.
- Stable exit codes and a one-line-per-ping output format, so hooks and scripts can rely on it.
- Multi-team: config and token per team (`~/.config/pingbus/<team>/`), chosen with `--team` or a per-project default.
- Rate limits and acknowledgement timeouts have built-in defaults that config can override.
- Built test-first. The validator does no I/O, so it can be tested exhaustively.
- `path` references are limited to a configurable path-prefix allowlist (for example a plans directory), and a referenced file is a document to read, not a command to obey.
- No default forge owner or repository: a team that has not configured its allowlists gets a refusal. Event-type names use a vendor-neutral namespace.

### 4. Humans: command-only, through a warden

- Humans in a room cannot send free text to agents. Every human message must visibly do something, so it is either a command or it is answered with a refusal.
- Element has no custom slash commands: an unknown `/command` opens an "Unknown Command … Send as message" dialog (checked in Element Web's source). In-room bot commands are proposed in MSC4332, which is still open. So commands use the usual Matrix bot prefix `!`: `!halt`, `!halt all`, `!status`, `!sync <ref>`, `!fetch <ref>`, `!help`.
- A small, deterministic **warden** process (no LLM, standard library only) runs beside each team's homeserver as a companion service. It is the only reader of human text. It answers every human message:
  - a valid command is turned into a structured ping and confirmed (for example "halt sent to 3 workers");
  - anything else gets a fixed reply listing the commands, and the text goes nowhere.
- **Targeting with @-mentions.** Typing `@` in Element suggests room members and inserts a mention pill; the event carries the targets in its structured `m.mentions.user_ids` field (Matrix spec; used by Element Web). The pill shows the display name, which is the full handle; the leading `@` below is Element's mention trigger, not part of the handle, and `user_ids` holds the user ID, for example `@myrepo.1+workstation.podman:<server_name>` (section 2). The warden reads `m.mentions`, never the text, to decide who a command goes to:
  - `@myrepo.1+workstation.podman !halt`: halts that one agent;
  - `@myrepo.1+workstation.podman @otherrepo.2+workstation.lxc !sync <ref>`: syncs those two;
  - `!halt all`: every worker in the room;
  - a command with no mention and no `all` (for example `!sync <ref>`) goes to the room's orchestrator, which decides who acts; `!status` and `!help` are answered by the warden itself;
  - a mention with no valid command (`@myrepo.1+workstation.podman hello`) gets the command list back, and nothing reaches the agent;
  - mentioning an account that is not a worker in the room is refused with a reply naming the problem.
- Agents therefore only ever receive structured pings. The room topic lists the commands.
- A substantive instruction is committed to a plan file and then pinged with `!sync <ref>`.

### 5. Room trust and Matrix mechanics (from a design review)

- Only humans create rooms and assign the orchestrator. A worker joins only a room whose creator is a configured human. Room state it cannot see in an invite (stripped state) is verified after joining; on failure it leaves.
- The CLI sets explicit power levels when creating a room (Matrix defaults would stop workers posting their own status events and let any member invite). Humans 100, orchestrator 50, workers 0; the roles and power-level events need 100.
- Sync continuity: on a limited sync, fill the gap from `/messages`; de-duplicate by event ID; one syncer per account at a time; persist the sync token only after the inbox write is durable. The orchestrator's acknowledgement timeout is the real delivery guarantee.
- A background `wait` must fit the agent harness's background-command limit (30 minutes by default), so its default timeout is shorter than that, and a stale "listening" status is shown as stale.
- Human free text, edits (`m.replace`) and any message that is not a valid ping are rejected on receive.

### 6. Waking sessions

- An idle session runs a background `pingbus wait` (or the Monitor tool) and is woken when a ping arrives.
- A `Stop` hook checks the inbox at the end of each turn, so a ping that arrives mid-turn is not missed. An optional `UserPromptSubmit` hook shows pending pings.
- Integrate with the existing ccy supervisor seam where it fits (#31).

### 7. ccy integration

- An opt-in ccy setting to join a team: mount that member's credential read-only into the container, put the CLI on `PATH`, and install the skill and hooks.
- A ccy container on the host already reaches services on host-local container networks, so no new networking should be needed. Confirm this per network type.

### 8. Members that ccy does not manage

Some agents run in containers that ccy does not manage (for example containers provisioned by other tooling). fedora-desktop should:

- publish the CLI as an installable artefact (a single file or wheel);
- document the member contract: the account, where the token file lives and its mode, the config format, and the network reachability needed.

The consuming project then provisions its own containers against that contract. Team-specific values never live in this repository.

### 9. Desktop viewing (optional play)

- Install **Element Desktop** (Flatpak) and a **terminal Matrix client**.
- One Element profile per team: Element Desktop supports `--profile <name>`, and each profile has separate data and its own `config.json`. A desktop launcher per team. A user's other Matrix accounts are untouched.
- Each team profile's `config.json`:
  - pins the homeserver (`default_server_config`, `disable_custom_urls`);
  - switches off every third-party service: identity server, integration manager (`integrations_*`), Jitsi and Element Call, map tiles (`map_style_url`), analytics (`posthog`, `sentry`), rageshake (`bug_report_endpoint_url`), and URL previews.
- To verify: where Element's `config.json` resolves inside the Flatpak sandbox for a named profile.
- The terminal client should be one that talks only to its homeserver. Verify its network behaviour before choosing.

## Privacy: acceptance checks

"Private" means nothing leaves the machine, and nothing outside the machine can reach in. Each point needs a check, not an assumption:

- **No outbound traffic from the homeserver.** Prove it by firewall deny logs or a capture over a session of normal use.
- **Not reachable from the network.** The published port must not be reachable from another machine. Check IP forwarding and the firewall zone of every bridge it is published on.
- **Names are not published.** A team's hostnames must not appear in public DNS.
- **No outbound traffic from the Element team profile** to anything except the team's homeserver.
- **Tokens never reach logs.** Deploy logs and CLI output are checked for each token's value.

## Open questions

1. ~~One homeserver per team, or one with a space per team?~~ **Decided: one per team.**
2. ~~Default placement?~~ **Decided: the desktop by default. Elsewhere is a URL, and TLS is required off the machine** (see section 1).
3. ~~TLS on the host-local default?~~ **Decided: plain HTTP for a host-local homeserver**, bound to host-only bridge addresses. TLS is required off the machine (section 1).
4. ~~Close registration after provisioning?~~ **Decided: closed.** Accounts are created through the admin account (section 2), and the registration token is not a standing key to join.
5. ~~How are agents named?~~ **Decided: `<repo>.<n>+<host>.<container-type>`** (section 2). This identifies the sender of every ping. Still open: forge authorship. If every agent pushes to the forge as the same account, a receiver cannot tell from the artefact itself which agent wrote it. The proposal for the first version is to rely on the bus identity of the ping, humans-only room creation and the path-prefix allowlist (section 5), and revisit per-agent forge identities if that proves too weak.
6. ~~Can humans send free text?~~ **Decided: no. A bus, not a chat room.** Humans send `!` commands through the warden (section 4); anything else is answered with the command list and never reaches an agent.

## Constraints

- Nothing team-specific is committed here: team names, addresses, member lists and tokens stay in local untracked config, or in the consuming project.
- Opt-in per machine and per team. Nothing changes for a user who does not enable it.
- The protocol specification (verbs, payload schema, validation rules, rate limits) is a versioned document in this repository, so every team's CLI enforces the same rules.

