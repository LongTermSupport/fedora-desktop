# Agent team bus protocol, version 1

This is the wire contract every `pingbus` and the warden enforce. Design and placement are
in [DESIGN.md](DESIGN.md). In build unit U02 this file moves to
`docs/agent-team-bus-protocol.md`, which then is the single source of truth, and a contract
test keeps its tables equal to the code's constants.

Notation: regular expressions are Python `re.fullmatch` patterns over the whole value.
"Drop" means: not shown to the session, written to `dropped.log` (event ID, sender user ID,
reason code, time; nothing else), counted in the batch's aggregate `DROPPED` line on stderr
(§14), and reflected in the exit code (§13).

**Trust root.** An agent acts only on a file under the team's `path_prefixes`, at a commit
reachable from a trusted branch of an allowlisted repository (§6, §10). Everything else a
ping can point at (an issue, a pull request's text, links out of the referenced file) is
data, never instructions. Any member can send raw events with its own token, so every
receiver re-checks everything, the forge included (§8).

## 1. Version and scope

- `PROTOCOL_VERSION = 1`. Every ping carries `"v": 1`. A receiver drops any other value
  (reason `version`).
- A change to any table in this file that makes a v1 receiver drop what a sender now sends,
  or accept what it used to drop, is a new version. Version 2 receivers may accept v1;
  v1 receivers never accept v2.
- `pingbus version` prints `pingbus <tool-version> protocol 1`.

## 2. Event types (namespace `io.github.longtermsupport.agentbus`)

Below, `<ns>` is the namespace.

| Type           | Kind  | `state_key`      | Content                                                      | Room    | Who may send (power)  |
| -------------- | ----- | ---------------- | ------------------------------------------------------------ | ------- | --------------------- |
| `<ns>.ping`    | room  | none             | §4                                                           | bus     | any member (0)        |
| `<ns>.room`    | state | `""`             | `{"v": 1, "control": <control room ID>}`: marks a bus room   | bus     | steward (creator)     |
| `<ns>.roles`   | state | `""`             | `{"v": 1, "roles": {<user ID>: "orchestrator" \| "worker"}}` | bus     | steward (creator)     |
| `<ns>.status`  | state | sender's user ID | `{"v": 1, "state": "listening", "until": <int ms>}`          | bus     | the member itself (0) |
| `<ns>.control` | state | `""`             | `{"v": 1, "bus_room": <bus room ID>}`: marks a control room  | control | steward (creator)     |

Roles are one map with an empty state key because a state key starting with `@` must equal
its sender; the status keeps its `@` key for exactly that reason, so the server enforces
that a member sets only its own. `roles` holds 1 to 64 entries, exactly one
`orchestrator`. `until` is a Unix time in milliseconds; a `listening` status whose `until`
has passed is shown as `stale`. No other event type is read by agents. `m.room.message` is
read only by the warden, and only in control rooms.

## 3. Identifiers

| Name               | Pattern                                                                                                                                               | Notes                                                                                 |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------- |
| agent handle       | `(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<n>[1-9][0-9]{0,5})\+(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.(?P<type>podman\|lxc\|docker\|vm\|host)` | Is the Matrix localpart and the display name. Example: `myrepo.1+workstation.podman`. |
| reserved localpart | `admin`, `steward`, `warden`, `conduit`                                                                                                               | Never a handle or a human.                                                            |
| human localpart    | `[a-z][a-z0-9_-]{0,31}`, not reserved                                                                                                                 | Contains no `+`, so it can never equal a handle.                                      |
| user ID            | `@<localpart>:<server_name>`, `<server_name>` equal to the member config's                                                                            | Any other server is dropped (`sender` / `target`).                                    |
| room ID            | `![A-Za-z0-9_-]{43}`                                                                                                                                  | Room version 12 only; other versions are refused (exit 10).                           |
| event ID           | `\$[A-Za-z0-9_-]{43}`                                                                                                                                 | Checked before it is used for anything, a file name included.                         |
| room pair name     | `[a-z0-9][a-z0-9-]{0,47}`                                                                                                                             | Shown to humans only (control room name).                                             |

**Building a handle** (done once, on the host, by `agent-team add-member`):

- `<repo>`: the last path component of the forge remote URL, `.git` removed; if there is no
  remote, the checkout directory name. Lowercase it; replace every character outside
  `[a-z0-9_-]` with `-`; strip leading `-`/`_`; truncate to 48. Empty after that is refused.
  The registry records which source was used.
- `<n>`: one more than the registry's counter for `<repo>+<host>.<type>`; never reused.
- `<host>`: the machine's short hostname (first label), lowercased; inside an LXC this is
  the LXC's name, which is where the session runs.
- `<type>`: the engine or place the session runs in. v1 provisions `podman` and `host` only.

## 4. Ping content

JSON object; keys outside this table are dropped (`schema`). The serialised content
(UTF-8, as received) is at most 2048 bytes (`size`).

| Key            | Type             | Required                              | Limits                                                                                                                                                               |
| -------------- | ---------------- | ------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `v`            | integer          | always                                | `1`                                                                                                                                                                  |
| `verb`         | string           | always                                | one of §5                                                                                                                                                            |
| `to`           | array of strings | always                                | 1 to 32 distinct user IDs, each an agent handle holding a role in the room, or the warden for `ack`, `nack`, `done` and `blocked` answering a warden ping (`target`) |
| `ref`          | string           | per §5                                | §6 grammar, a form §5 allows for the verb, ≤ 700 characters, inside the allowlists (§10)                                                                             |
| `re`           | string           | per §5                                | event ID of an earlier ping in the same room                                                                                                                         |
| `note`         | string           | never                                 | §7                                                                                                                                                                   |
| `on_behalf_of` | string           | exactly when the sender is the warden | user ID of a configured human                                                                                                                                        |

Booleans, floats, nulls and nested objects are never valid values (`schema`). The event
must have no `state_key`, must not be redacted, and must not carry `m.relates_to` or
`m.new_content` (an edit; `edit`).

## 5. Verbs (closed set)

| Verb      | Meaning to the receiver                                                                            | `ref` forms allowed              | `re`      | Ack expected | May be sent by           |
| --------- | -------------------------------------------------------------------------------------------------- | -------------------------------- | --------- | ------------ | ------------------------ |
| `fetch`   | Fetch the referenced repository state and read the artefact; no other action implied.              | required: `path`, `commit`       | forbidden | yes          | orchestrator, warden     |
| `sync`    | Re-read the referenced plan or spec and bring your work in line with it.                           | required: `path`, `commit`       | forbidden | yes          | orchestrator, warden     |
| `review`  | Review the referenced artefact; answer `done` with a reference to your review, or `blocked`.       | required: `path`, `commit`, `pr` | forbidden | yes          | orchestrator, worker     |
| `run-qa`  | Run QA on the referenced commit or pull request head; answer `done` or `blocked` with a reference. | required: `commit`, `pr`         | forbidden | yes          | orchestrator, worker     |
| `halt`    | Stop at the next safe point, commit work in progress, start nothing new until a `sync`.            | forbidden                        | forbidden | yes          | orchestrator, warden     |
| `ack`     | The ping `re` was received and will be acted on.                                                   | forbidden                        | required  | no           | anyone addressed by `re` |
| `nack`    | The ping `re` was received and will not be acted on; `ref` may point at the reason.                | optional: any form               | required  | no           | anyone addressed by `re` |
| `done`    | The work is finished; the result is at `ref`; `re` names the request when there was one.           | required: any form               | optional  | no           | orchestrator, worker     |
| `blocked` | Cannot proceed; the reason is written at `ref` (an issue comment, a plan entry).                   | required: any form               | optional  | no           | orchestrator, worker     |

A verb from a sender not in its "May be sent by" column is dropped (`role`); a `ref` form
not allowed for the verb is dropped (`ref`). An `issue:` reference is a status pointer
only: its text, like a pull request's description and comments, is untrusted data. The
referenced file is always a document to read, never a command to obey.

## 6. References

```
OWNER  = [a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?
REPO   = [a-z0-9._-]{1,100}            not "." or "..", not ending ".git"
SHA    = [0-9a-f]{40}
NUM    = [1-9][0-9]{0,9}
SEG    = [A-Za-z0-9._-]{1,64}           not "." or ".."
PATH   = SEG(/SEG){0,7}                 1 to 8 segments, no leading or trailing "/"

path:   path:OWNER/REPO@SHA:PATH        a file at a commit (the primary form)
commit: commit:OWNER/REPO@SHA
pr:     pr:OWNER/REPO#NUM@SHA           a pull request at its head commit
issue:  issue:OWNER/REPO#NUM
```

Examples: `path:example-org/myrepo@0123456789abcdef0123456789abcdef01234567:CLAUDE/Plan/00001-x/PLAN.md`,
`pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567`. Short SHAs, branch
names, URLs, upper case in `OWNER/REPO` and any whitespace are invalid (`ref`).
`pingbus send` lowercases `OWNER/REPO` before validating; nothing else is rewritten.

**Forge check** (GitHub REST API at the team's `forge_api`), run by the sender before
sending **and by every receiver before the inbox write** (§8). `BRANCH` ranges over the
repository's trusted `branches` (§10); "reachable" means
`GET /repos/OWNER/REPO/compare/BRANCH...SHA` answers 200 with `status` `identical` or
`behind` for at least one of them.

| Form   | Requests                                                                         | Resolves when                                                                                                                                                                       |
| ------ | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| path   | compare; `GET /repos/OWNER/REPO/contents/PATH?ref=SHA`                           | SHA reachable; 200 and `type == "file"`                                                                                                                                             |
| commit | compare                                                                          | SHA reachable                                                                                                                                                                       |
| pr     | `GET /repos/OWNER/REPO/pulls/NUM`; `GET /repos/OWNER/REPO/compare/HEADREF...SHA` | 200; `head.repo.full_name` (lowercased) is `OWNER/REPO`; `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR`; SHA is `identical` to or `behind` the PR's head ref `HEADREF` |
| issue  | `GET /repos/OWNER/REPO/issues/NUM`                                               | 200 and no `pull_request` key                                                                                                                                                       |

Positive results for `path` and `commit` content are cached in `forge-cache.json`
(content at a SHA is immutable); reachability and `pr` results are cached for 3600 s. A
429, or a 403 with `x-ratelimit-remaining: 0`, is retried per §9 and then reported as
`forge-rate`. Send-side refusals, on stderr: `not-found`, `wrong-kind`, `provenance`,
`forge-unreachable`, `forge-auth` (exit 5), `forge-rate` (exit 9). On receive, the same
failures drop the ping as `unresolved` (`not-found`, `wrong-kind`, `forge-unreachable`,
`forge-auth`, `forge-rate`) or `provenance`.

**Forge credential:** `PINGBUS_FORGE_TOKEN_FILE` (a 0600 file), else
`PINGBUS_FORGE_TOKEN`, else, only when `forge_api` is exactly `https://api.github.com`,
`GH_TOKEN` then `GITHUB_TOKEN`; else none (public repositories only). It is sent with
`add_unredirected_header`; a redirect to another host is refused.

## 7. Note

- Optional, at most 80 characters, pattern `[A-Za-z0-9 .,:_/#()=-]{1,80}`, no leading or
  trailing space, no two spaces in a row (`note`).
- A receiving member removes it before the inbox write, so no command, file or hook ever
  shows a note to an agent. Only the warden's control-room mirror shows it, to humans,
  marked "untrusted agent note". A session must not act on a note.

## 8. Validation

### On send (`pingbus send`, and the warden before it emits)

In order; the first failure refuses the send and nothing reaches the homeserver:

1. Member config valid (§11), else exit 78.
2. Content builds and passes every rule of §4–§7 and §10, else exit 4 with the reason code.
3. The room is a verified bus room (DESIGN.md §7), else exit 10.
4. The sender holds a role in the room permitting the verb (§5), and every `to` holds a
   role, else exit 4 (`role` / `target`).
5. Local rate limits (§9), else exit 9.
6. Forge check (§6), else exit 5 (or 9 for `forge-rate`).
7. `PUT /rooms/{room}/send/<ns>.ping/{txnId}`; a retry after a timeout reuses the txnId.

### On receive (`recv`, `wait`, the warden)

With no stored sync token, the first `/sync` uses `timeline.limit: 0` and only records
`next_batch`: history is never processed. Only events in a verified bus room (DESIGN.md §7)
are considered. For each ping-type timeline event, in order:

01. Event ID fails §3: drop (`schema`).
02. Already seen event ID: ignored silently.
03. Sender is self: ignored silently.
04. `state_key` present, redacted, or an edit: drop (`schema` / `edit`).
05. Sender is neither an agent handle holding a role in the room nor the configured warden:
    drop (`sender`).
06. Content fails §4–§7 or §10 (offline; same function as on send): drop with that reason.
07. Verb not permitted for the sender's role: drop (`role`).
08. `to` does not contain this member: ignored silently (addressed to someone else). The
    warden does not skip here: it mirrors every valid ping (DESIGN.md §8).
09. `origin_server_ts` older than the verb's ack timeout (§9; `ack_timeout_s` for verbs
    that expect none): drop (`stale`).
10. Sender over the receive flood limit (§9): drop (`rate`).
11. Forge check (§6): drop (`unresolved` / `provenance`).
12. Otherwise: the note is removed, the ping is written durably to the inbox, then the sync
    token is saved.

**Reading the inbox.** `recv`, `wait`, `inbox`, `show`, `tail` and the hooks re-run steps
1 and 4–7 on every stored file and ignore any that fail (an inbox file is a cache that code
in the checkout could have written). `recv` and `wait` also confirm each event with
`GET /rooms/{room}/event/{event}` before printing it when they hold the lock.

**Drop reason codes** (closed set): `version`, `schema`, `size`, `edit`, `sender`, `role`,
`target`, `verb`, `ref`, `allowlist`, `note`, `re`, `behalf`, `stale`, `rate`,
`unresolved`, `provenance`.

## 9. Limits

Defaults are built in. The member config's `limits` may change only the overridable ones,
and only within the bounds; a value outside, or any other key, is a config error (exit
78), never silently clamped.

| Limit                     | Default                                     | Bounds     |
| ------------------------- | ------------------------------------------- | ---------- |
| `send_per_minute`         | 20 (token bucket refill)                    | 1 – 60     |
| `send_burst`              | 10                                          | 1 – 30     |
| `recv_per_sender_minute`  | 60; excess dropped (`rate`)                 | 10 – 600   |
| `warden_per_human_minute` | 10; excess answered with a refusal          | 1 – 60     |
| `ack_timeout_s`           | 900 for `fetch`, `sync`, `review`, `run-qa` | 60 – 86400 |
| `halt_ack_timeout_s`      | 300                                         | 30 – 3600  |
| `wait_timeout_s`          | 1500 (`wait --timeout` default)             | 1 – 1790   |

Fixed, not configurable: the duplicate window is 60 s (same verb, ref, re and set of `to`
again is refused, exit 9); each `/sync` long-poll is 30 s; a server 429 is honoured
(`Retry-After`, then `retry_after_ms`, then 5 s), at most 3 tries, then exit 9.

An ack-expected ping with no `ack` or `nack` from a target by its deadline produces one
`TIMEOUT` line per silent target in the sender's next `recv` or `wait` (§14).

## 10. Allowlists

- `repos`: list of objects `{"repo": "OWNER/REPO", "branches": [BRANCH, …]}`. Every
  reference's repository must be listed (`allowlist`). `branches` (1 to 8 names, each
  `[A-Za-z0-9._/-]{1,100}`) are the branches whose history is trusted (§6); a fork is never
  listed.
- `path_prefixes`: list of prefixes; each is a `PATH` ending in `/`, or an exact file
  `PATH`. A `path:` reference's `PATH` must start with one of them (`allowlist`). Because
  `PATH` has no `.` or `..` segments, a plain string prefix test is exact.
- Both lists are required and non-empty. There is no default owner, repository, branch or
  prefix: a member config without them is refused (exit 78) for every command but
  `version`.

## 11. Member config

`member.json`, JSON, read-only to pingbus, rendered by `agent-team member-config` at each
start of a member. Resolution: `--config <file>`, else `PINGBUS_CONFIG`, else
`--team <team>` → `${XDG_CONFIG_HOME:-~/.config}/pingbus/<team>/member.json`, else the only
team directory if exactly one exists, else exit 78.

| Key             | Type             | Rule                                                                                          |
| --------------- | ---------------- | --------------------------------------------------------------------------------------------- |
| `protocol`      | integer          | `1`                                                                                           |
| `team`          | string           | `[a-z][a-z0-9-]{0,23}`                                                                        |
| `handle`        | string           | §3 handle, or `warden` for the warden; must equal `PINGBUS_HANDLE` when that is set           |
| `user_id`       | string           | `@<handle>:<server_name>`                                                                     |
| `server_name`   | string           | DNS name                                                                                      |
| `base_url`      | string           | `https://…`, or `http://` only when the host is a loopback literal or listed in `local_hosts` |
| `local_hosts`   | array of strings | IP literals this member may reach over plain HTTP (the homeserver's fixed team-network IP)    |
| `token_file`    | string           | absolute path; the file must be owned by the user and mode 0600 or stricter                   |
| `steward`       | string           | user ID of the steward (the only accepted room creator and inviter)                           |
| `humans`        | array of strings | user IDs of the configured humans (valid `on_behalf_of` values)                               |
| `warden`        | string           | user ID of the warden                                                                         |
| `repos`         | array of objects | §10                                                                                           |
| `path_prefixes` | array of strings | §10                                                                                           |
| `forge_api`     | string           | `https://` URL                                                                                |
| `limits`        | object           | optional; overridable keys of §9 only                                                         |

Unknown keys are a config error. The token is sent only in the `Authorization: Bearer`
header (unredirected, never through a proxy, no redirect followed) and never appears in
output, errors, URLs or logs.

## 12. CLI commands

Global options: `--config FILE`, `--team NAME`. Every command reads arguments only and
never prompts. Room creation is not a pingbus command: it is `agent-team room create`, on
the host (DESIGN.md §7).

| Command                                                                                                       | Does                                                                                                      | Network |
| ------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------- | ------- |
| `send VERB [REF] (--to HANDLE[,HANDLE…] \| --to-orchestrator) [--re EVENT_ID] [--note TEXT] [--room ROOM_ID]` | the only way to emit a ping; `--room` may be omitted when the member is in exactly one bus room           | yes     |
| `recv`                                                                                                        | sync once if the lock is free, then print and consume every pending ping and due `TIMEOUT`                | maybe   |
| `wait [--timeout S]`                                                                                          | hold the lock, long-poll until at least one ping or `TIMEOUT`, print and consume them, exit               | yes     |
| `inbox`                                                                                                       | print pending pings without consuming                                                                     | no      |
| `show EVENT_ID`                                                                                               | one ping's fields, when it arrived, and its ack state                                                     | no      |
| `status`                                                                                                      | team, handle, waiter freshness, pending count, overdue acks, drops since last `status`                    | no      |
| `peers [--room ROOM_ID]`                                                                                      | room members' user IDs, roles and status (stale marked)                                                   | yes     |
| `tail [--room ROOM_ID]`                                                                                       | follow the local inbox and outbox for a human                                                             | no      |
| `room list` / `room leave ROOM_ID`                                                                            | list joined bus rooms (IDs and own role) and rejected invites (IDs); leave a room                         | yes     |
| `validate VERB [REF] [--re EVENT_ID] [--note TEXT]` / `validate --event FILE`                                 | run the send-side (offline part) or receive-side (offline part) validator; prints `OK` or the reason code | no      |
| `config check`                                                                                                | validate the member config, token file mode and forge credential source                                   | no      |
| `hook stop` / `hook prompt` / `hook session-start`                                                            | Claude Code hook entry points: stdin hook JSON, stdout hook JSON; always exit 0; fixed templates only     | no      |
| `version`                                                                                                     | §1                                                                                                        | no      |

Invites are accepted by the syncer itself, after the checks in DESIGN.md §7.

## 13. Exit codes (stable)

| Code | Meaning                                                                              |
| ---- | ------------------------------------------------------------------------------------ |
| 0    | success; `recv`/`wait` printed at least one line (drops, if any, are on stderr)      |
| 1    | never assigned (an uncaught exception)                                               |
| 2    | never assigned (argparse's default is remapped to 64)                                |
| 3    | nothing: `recv` found nothing; `wait` reached its timeout                            |
| 4    | refused by the validator, or by role                                                 |
| 5    | the reference did not resolve at the forge, or failed the provenance check           |
| 6    | `recv` only: received pings were dropped and no valid line was printed               |
| 7    | homeserver unreachable                                                               |
| 8    | authentication refused (token rejected)                                              |
| 9    | rate limited (local limit, duplicate, server 429 after retries, or forge rate limit) |
| 10   | room refused: not a verified bus room, wrong room version, or not joined             |
| 64   | usage error                                                                          |
| 75   | busy: another process holds this account's sync lock (`wait`)                        |
| 78   | configuration refused (§11, §10, §9 bounds)                                          |

`wait` never exits because of drops alone: a batch that yields only drops is logged and
the long-poll continues.

## 14. Output format

One line per item, fields separated by a single tab, no field ever contains a tab or
newline, absent fields are `-`. Every field is printed only after it passed its §3–§6
grammar. Senders and targets are printed as localparts (all share the team's
`server_name`). Fields are only ever appended in later versions, never reordered.

| Line      | Stream | Fields                                                                             |
| --------- | ------ | ---------------------------------------------------------------------------------- |
| `PING`    | stdout | `PING`, `1`, event ID, room ID, sender, verb, ref, re, on-behalf-of                |
| `TIMEOUT` | stdout | `TIMEOUT`, `1`, event ID of the unanswered ping, room ID, silent target, verb, ref |
| `SENT`    | stdout | `SENT`, `1`, event ID, room ID (from `send`)                                       |
| `DROPPED` | stderr | `DROPPED`, `1`, count, `reason=count` pairs joined by `,` (one line per batch)     |

Example:
`PING	1	$AbC…	!XyZ…	orch.1+workstation.podman	review	pr:example-org/myrepo#12@0123…	-	-`

All other human-facing text goes to stderr, except for the report commands (`inbox`,
`show`, `status`, `peers`, `tail`, `room list`, `config check`) whose text is their
output. No command prints a room name, topic, invite reason, display name, note, unknown
status key or exception text taken from an event.

## 15. Human commands (warden)

Accepted only in a control room (one carrying `<ns>.control`), only from configured
humans, only in `m.room.message` with `msgtype` `m.text`, read from `body`:

```
TOKENS  = body split on runs of whitespace; one trailing "," or ":" is removed from each
COMMAND = "!halt" ["all"] | "!status" | "!help" | "!sync" REF | "!fetch" REF
TARGET  = a §3 agent handle, or its full user ID
BODY    = TARGET* COMMAND TARGET*          exactly one COMMAND; every other token a TARGET
```

- Targets are resolved against the paired bus room's `<ns>.roles`; one that holds no role
  gets a reply naming it, and nothing is sent.
- `!halt all` takes no targets. `!halt`, `!sync` and `!fetch` with no targets go to the bus
  room's orchestrator. `!status` and `!help` take no targets and are answered by the warden.
- If `content["m.mentions"]["user_ids"]` is present it must equal the targets' user IDs,
  else the command list is returned (fail closed).
- Each accepted command becomes one ping per DESIGN.md §8 with `on_behalf_of` set;
  everything else gets the fixed command list as an `m.notice`, and nothing is sent to any
  agent.
