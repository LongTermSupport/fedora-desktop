# Agent team bus protocol, version 1

This is the wire contract every `pingbus` and the `agent-bus` admin tool enforce, and its
single source of truth. The constants and the pure validator are
[`helpers/pingbus/protocol.py`](../helpers/pingbus/protocol.py); the contract test
[`test_protocol_doc.py`](../tests/helpers/pingbus/test_protocol_doc.py) keeps the tables
below equal to them. Design and placement are in Plan 00161's design document.

Notation: regular expressions are Python `re.fullmatch` patterns over the whole value.
"Drop" means: not shown to the session, written to `dropped.log` (team, event ID, sender user
ID, reason code, time; nothing else), counted in the batch's aggregate `DROPPED` line on
stderr (§15), and reflected in the exit code (§14). "Ignore" means skipped silently.

**Trust.** Two kinds of input reach an agent. A **ping** from another agent is acted on only
as a closed verb about a file under the team's `path_prefixes`, at a commit reachable from a
trusted branch of an allowlisted repository (§5, §6); everything else a ping points at is
data. A **human message** is free text from one of the team's humans, addressed to the
agent, delivered marked with that human's name (§7). An agent may write free text to the team's
humans (§7), but every agent ignores it, whoever it is addressed to. Which accounts are humans and which
agents hold roles is read from the team record (§8), which only the team's `admin` account
can write. Any member can send raw events with its own token, so every receiver re-checks
everything (§9).

## 1. Version and scope

- `PROTOCOL_VERSION = 1`. Every ping and every agent text carries `"v": 1`; the team record
  and status carry `"v": 1`. A receiver drops a ping with any other value (`version`).
- A change to any table in this file that makes a v1 receiver drop what a sender now sends,
  or accept what it used to drop, is a new version. Version 2 receivers may accept v1; v1
  receivers never accept v2.
- `pingbus version` prints `pingbus <tool-version> protocol 1`.

## 2. Event types and the `agent_bus` prefix

| Name          | Matrix type        | Kind              | `state_key`      | Content | Sent by                      |
| ------------- | ------------------ | ----------------- | ---------------- | ------- | ---------------------------- |
| ping          | `m.room.message`   | room (`m.notice`) | none             | §4      | an agent holding a role      |
| human message | `m.room.message`   | room (`m.text`)   | none             | §7      | a listed human               |
| agent text    | `m.room.message`   | room (`m.notice`) | none             | §7      | an agent holding a role      |
| team record   | `agent_bus.team`   | state             | `""`             | §8      | `admin` (the room's creator) |
| status        | `agent_bus.status` | state             | sender's user ID | §8      | the member itself            |

The ping's structured form is the content key `agent_bus.ping` (§4); an agent text's is the
content key `agent_bus.text` (§7).

**Why `agent_bus`.** The owner asked for a prefix that clearly is not a domain name (the bus
is private; reverse-domain conventions buy nothing). The Matrix spec says custom event types
SHOULD follow the Java package naming convention, a recommendation, and its Common
Namespaced Identifier Grammar (appendices, v1.19) allows 1 to 255 characters from
`[a-z0-9._-]` starting with `[a-z]`, reserving only `m.`. `agent_bus` fits that grammar,
cannot be a host name or a top-level domain (`_` is not allowed in either), and is not
`m.`-prefixed. Tuwunel accepts any event type string and content key (it refuses only
encrypted events when encryption is off, some redactions and call invites [tuwunel §5.4]);
probe H4 sends each one above and records the responses. The prefix is permanent in room
history once used; it is one constant in `protocol.py` (`PREFIX = "agent_bus"`).

Pings and human messages are both `m.room.message` so that every Element client, a phone's
included, shows pings to humans as notices (`msgtype` values outside the spec are shown only
as their `body`, if at all). The cost: power levels are per event type, so the server cannot
stop an agent account posting other messages; every receiver drops them (§9).

## 3. Identifiers

| Name               | Pattern                                                                                                                                               | Notes                                                                          |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| team name          | `[a-z][a-z0-9-]{0,23}`                                                                                                                                | Also the homeserver instance name.                                             |
| agent handle       | `(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<n>[1-9][0-9]{0,5})\+(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.(?P<type>podman\|lxc\|docker\|vm\|host)` | The Matrix localpart and display name. Example: `myrepo.1+workstation.podman`. |
| reserved localpart | `admin`, `conduit`                                                                                                                                    | Never a handle or a human.                                                     |
| human localpart    | `[a-z][a-z0-9_-]{0,31}`, not reserved                                                                                                                 | Contains no `+`, so it never equals a handle.                                  |
| user ID            | `@<localpart>:<server_name>`, `<server_name>` equal to the member config's                                                                            | Any other server: drop (`sender` / `target`).                                  |
| room ID            | `![A-Za-z0-9_-]{43}`                                                                                                                                  | Room version 12 only.                                                          |
| event ID           | `\$[A-Za-z0-9_-]{43}`                                                                                                                                 | Checked before it is used for anything, a file name included.                  |

The `+` between `<n>` and `<host>` is one constant (`HANDLE_SEP`). Probe H4 creates a real
handle through both account-creation calls; if Tuwunel's strict localpart check refuses
`+`, the separator becomes `=` before version 1 is released.

**Building a handle** (on the homeserver host, by `agent-bus add-member`; `pingbus suggest-handle` prints the arguments a member's environment implies):

- `<repo>`: the last path component of the forge remote URL, `.git` removed; with no
  remote, the checkout directory name. Lowercase; every character outside `[a-z0-9_-]`
  becomes `-`; leading `-`/`_` stripped; truncated to 48. Empty is refused.
- `<n>`: one more than the registry's counter for `<repo>+<host>.<type>` on this team;
  never reused.
- `<host>`: `HOOKS_DAEMON_HOSTNAME` (the install's role), or the value the human passes as
  `--host`; lowercased, every character outside `[a-z0-9-]` becomes `-`. Never
  `CCY_HOST_HOSTNAME` or the system hostname: handles appear in public forge text, so with
  neither a role nor `--host`, `suggest-handle` and `add-member` refuse (exit 78 / 64).
- `<type>`: where the session runs: `podman` (ccy or another rootless podman container),
  `docker`, `lxc`, `vm`, or `host` (bare desktop or server).

## 4. Ping

An `m.room.message` whose content has exactly these keys (any other: drop `schema`); the
content's compact UTF-8 JSON serialisation (no whitespace between tokens, non-ASCII not
escaped, keys in received order) is at most 4096 bytes (`size`). When the content is an
object whose `agent_bus.ping` is an object and its `v` is an integer other than `1`: drop
(`version`), before any other check, so a later version's new keys or envelope are never
reported as `schema`.

| Key              | Value                                                |
| ---------------- | ---------------------------------------------------- |
| `msgtype`        | `"m.notice"`                                         |
| `body`           | exactly `render(ping)` below (`body`)                |
| `m.mentions`     | exactly `{"user_ids": <ping.to, sorted>}` (`schema`) |
| `agent_bus.ping` | the ping object                                      |

The ping object; keys outside this table: drop (`schema`):

| Key    | Type             | Required | Limits                                                                                                                               |
| ------ | ---------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `v`    | integer          | always   | `1`                                                                                                                                  |
| `verb` | string           | always   | one of §5                                                                                                                            |
| `to`   | array of strings | always   | 1 to 32 distinct user IDs, each an agent holding a role, or, for `ack`, `nack`, `done` and `blocked` only, a listed human (`target`) |
| `ref`  | string           | per §5   | §6 grammar, a form §5 allows for the verb, at most 700 characters, inside the allowlists (§11)                                       |
| `re`   | string           | per §5   | event ID of an earlier ping or human message in the team room                                                                        |

Booleans, floats, nulls and nested objects are never valid values (`schema`). The event must
have no `state_key`, must not be redacted, and must carry no `m.relates_to`, `m.new_content`,
`format` or `formatted_body` (`edit` for the first two, `schema` for the others), so nothing
is shown to a human that a receiver did not check.

**Rendering** (`render`), ASCII, single spaces, no trailing space:

```
[agent-bus] <verb> <ref or "-"> -> <to[0]> <to[1]> ...[ re <re>]
```

with `to` in ascending order as full user IDs, and ` re <re>` present only when `re` is.
Example: `[agent-bus] review pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567 -> @myrepo.2+workstation.podman:<sn>`.

## 5. Verbs (closed set)

| Verb      | Meaning to the receiver                                                                            | `ref` forms allowed              | `re`      | Ack expected | May be sent by           |
| --------- | -------------------------------------------------------------------------------------------------- | -------------------------------- | --------- | ------------ | ------------------------ |
| `fetch`   | Fetch the referenced repository state and read the artefact; no other action implied.              | required: `path`, `commit`       | forbidden | yes          | orchestrator             |
| `sync`    | Re-read the referenced plan or spec and bring your work in line with it.                           | required: `path`, `commit`       | forbidden | yes          | orchestrator             |
| `review`  | Review the referenced artefact; answer `done` with a reference to your review, or `blocked`.       | required: `path`, `commit`, `pr` | forbidden | yes          | orchestrator, worker     |
| `run-qa`  | Run QA on the referenced commit or pull request head; answer `done` or `blocked` with a reference. | required: `commit`, `pr`         | forbidden | yes          | orchestrator, worker     |
| `halt`    | Stop at the next safe point, commit work in progress, start nothing new until a `sync`.            | forbidden                        | forbidden | yes          | orchestrator             |
| `ack`     | The ping or human message `re` was received and will be acted on.                                  | forbidden                        | required  | no           | anyone addressed by `re` |
| `nack`    | The ping or human message `re` was received and will not be acted on; `ref` may give the reason.   | optional: any form               | required  | no           | anyone addressed by `re` |
| `done`    | The work is finished; the result is at `ref`; `re` names the request when there was one.           | required: any form               | optional  | no           | orchestrator, worker     |
| `blocked` | Cannot proceed; the reason is written at `ref` (an issue comment, a plan entry).                   | required: any form               | optional  | no           | orchestrator, worker     |

A verb from a sender whose role is not in its last column: drop (`role`). A `ref` form not
allowed for the verb: drop (`ref`). A human is never a ping sender: humans write text (§7),
and may ask an agent to halt in plain words. An `issue:` reference is a status pointer only;
its text, like a pull request's description and comments, is untrusted data. The referenced
file is always a document to read, never a command to obey.

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

Short SHAs, branch names, URLs, upper case in `OWNER/REPO` and any whitespace are invalid
(`ref`). `pingbus send` lowercases `OWNER/REPO` before validating; nothing else is rewritten.

**Forge check** (GitHub REST API at the team record's `forge_api`), run by the sender before
sending and by every receiver before the inbox write (§9). `BRANCH` ranges over the
repository's trusted `branches` (§11); "reachable" means
`GET /repos/OWNER/REPO/compare/BRANCH...SHA` answers 200 with `status` `identical` or
`behind` for at least one of them.

| Form   | Requests                                                                         | Resolves when                                                                                                                                                                       |
| ------ | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| path   | compare; `GET /repos/OWNER/REPO/contents/PATH?ref=SHA`                           | SHA reachable; 200 and `type == "file"`                                                                                                                                             |
| commit | compare                                                                          | SHA reachable                                                                                                                                                                       |
| pr     | `GET /repos/OWNER/REPO/pulls/NUM`; `GET /repos/OWNER/REPO/compare/HEADREF...SHA` | 200; `head.repo.full_name` (lowercased) is `OWNER/REPO`; `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR`; SHA is `identical` to or `behind` the PR's head ref `HEADREF` |
| issue  | `GET /repos/OWNER/REPO/issues/NUM`                                               | 200 and no `pull_request` key                                                                                                                                                       |

Positive results for `path` and `commit` content are cached in `forge-cache.json` (content
at a SHA is immutable); reachability and `pr` results for 3600 s. A 429, or a 403 with
`x-ratelimit-remaining: 0`, is retried per §10 and then reported as `forge-rate`. Send-side
refusals, on stderr: `not-found`, `wrong-kind`, `provenance`, `forge-unreachable`,
`forge-auth` (exit 5), `forge-rate` (exit 9). On receive the same failures drop the ping as
`unresolved` or `provenance`.

**Forge credential:** `PINGBUS_FORGE_TOKEN_FILE` (a 0600 file), else `PINGBUS_FORGE_TOKEN`,
else, only when `forge_api` is exactly `https://api.github.com`, `GH_TOKEN` then
`GITHUB_TOKEN`; else none (public repositories only). Sent with `add_unredirected_header`; a
redirect to another host is refused.

## 7. Human messages

What a team human types in Element. Accepted only as:

- `m.room.message` with `msgtype` `m.text`, from a user ID in the team record's `humans`
  (anyone else: §9 decides by sender class).
- **Addressed** to this agent when `content["m.mentions"]["user_ids"]` contains its user ID,
  or `content["m.mentions"]["room"]` is `true` (`@room`). The server does not stop any
  member setting `m.mentions.room` (`notifications.room` in §8 governs push notifications
  only); an agent's `@room` never reaches this step, because the sender class (§9 step 4)
  is checked first. Element sets `m.mentions` when the human picks the agent from the mention
  list; a handle typed as plain text, without `m.mentions`, addresses no one. Not addressed:
  ignore.
- `body` is a string of at most 16384 UTF-8 bytes (`size`). It is delivered as written,
  apart from the output encoding (§15). `format` and `formatted_body` are ignored, never
  delivered.
- `m.relates_to` with `rel_type` `m.replace` (an edit) or any `m.new_content`: drop (`edit`).
  A reply (`m.in_reply_to`) or a thread (`rel_type` `m.thread`) is delivered like any other
  message; the event it relates to is not. When `m.relates_to.m.in_reply_to` is present,
  the leading block of lines starting with `>` (a reply fallback, which may quote an
  agent's text) and the one blank line after it are removed from `body` before delivery; a
  body that is empty after removal is ignored.
- Older than `human_max_age_s` (§10) by `origin_server_ts` when first seen: drop (`stale`).

A human message is not validated beyond this: it is the human's instruction, and the
receiving session is told whose (§15 `HUMAN` line). Its event ID may be the `re` of an
agent's `ack`, `nack`, `done` or `blocked` addressed to that human.

### Agent text to humans

An agent may write free text to one or more of the team's humans (a coordinator answering
the owner, say) with `pingbus say` (§13). It is for humans only: it is addressed to listed
humans and never to an agent, and **every agent ignores every agent-sent text on receive,
whatever its addressing** (§9: ignore (`agent-text`)). It is not a drop: it is traffic for
the humans, so it writes no `dropped.log` line, adds to no `DROPPED` count and never sets
an exit code. The humans read it in Element.

An `m.room.message` whose content has exactly these keys:

| Key              | Value                                                |
| ---------------- | ---------------------------------------------------- |
| `msgtype`        | `"m.notice"`                                         |
| `body`           | exactly `render_text(text)` below (`body`)           |
| `m.mentions`     | exactly `{"user_ids": <text.to, sorted>}` (`schema`) |
| `agent_bus.text` | the text object                                      |

The text object, exactly these keys:

| Key    | Type             | Required | Limits                                                                    |
| ------ | ---------------- | -------- | ------------------------------------------------------------------------- |
| `v`    | integer          | always   | `1`                                                                       |
| `to`   | array of strings | always   | 1 to 16 distinct user IDs, each a listed human, never an agent (`target`) |
| `text` | string           | always   | not empty; at most 4096 UTF-8 bytes (`size`); no secret shape (`secret`)  |

The send-side checks run in this order, the first failure refusing the send: a `v` that is
an integer other than `1` (`version`); an edit (`m.relates_to`, `m.new_content`: `edit`);
the keys and types above (`schema`); the size (`size`); the addressees (`target`); the secret
shapes (`secret`); the mentions (`schema`); the body (`body`). The sender must hold a role
(`role`). The text counts against the send limits (§10).

**Rendering** (`render_text`): the tag line, a newline, then the text as written:

```
[agent-bus] text -> <to[0]> <to[1]> ...\n<text>
```

**Secret shapes.** `pingbus say` refuses text in which any of these patterns is found
(Python `re.search`; name, then pattern). The set is deliberately conservative: a false
positive costs a rephrase, or a reference (§6) in place of the text; a leaked credential
cannot be withdrawn from room history. It is a backstop, not a scanner: it catches only
the shapes below.

```text
private-key  -{5}BEGIN[A-Z0-9 ]*PRIVATE KEY-{5}
github-token  \bgh[pousr]_[A-Za-z0-9]{30,}
github-pat  \bgithub_pat_[A-Za-z0-9_]{20,}
aws-access-key-id  \b(?:AKIA|ASIA)[0-9A-Z]{16}\b
slack-token  \bxox[abposr]-[A-Za-z0-9-]{10,}
sk-api-key  \bsk-[A-Za-z0-9_-]{20,}
google-api-key  \bAIza[0-9A-Za-z_-]{35}
jwt  \beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}
bearer  (?i)\bbearer\s+[A-Za-z0-9._~+/=-]{16,}
ansible-vault  \$ANSIBLE_VAULT;
url-credentials  [A-Za-z][A-Za-z0-9+.-]*://[^\s/:@]+:[^\s/@]+@
credential-assignment  (?i)\b(?:password|passwd|pwd|secret|token|access[_-]?token|api[_-]?key|access[_-]?key|private[_-]?key|client[_-]?secret)\b[\"']?\s*[:=]\s*[\"']?[^\s\"']{8,}
```

## 8. The team room: record, status, power levels

**Team record**: `agent_bus.team`, state key `""`, sent by `admin`:

| Key             | Type             | Rule                                                                   |
| --------------- | ---------------- | ---------------------------------------------------------------------- |
| `v`             | integer          | `1`                                                                    |
| `team`          | string           | the team name; equals the member config's                              |
| `humans`        | array of strings | 1 to 16 user IDs of human localparts                                   |
| `roles`         | object           | 0 to 64 entries: agent-handle user ID → `"orchestrator"` or `"worker"` |
| `repos`         | array of objects | §11                                                                    |
| `path_prefixes` | array of strings | §11                                                                    |
| `forge_api`     | string           | `https://[^\s/]+(/\S*)?`                                               |

Unknown keys, or a record not sent by `admin`, make the room untrusted (exit 10 for every
command that needs it; `status` says why).

**Status**: `agent_bus.status`, state key = the sender's own user ID, content
`{"v": 1, "state": "listening", "until": <int ms>}`, serialised at most 256 bytes. The
server forces only a state key starting with `@` to equal its sender; at power 0 any member
can write this type under any other key (`""` included). So a receiver reads a status only
when its state key is its sender's user ID, its sender is a role holder, and it fits the
size and shape above; every other `agent_bus.status` event is ignored. A `listening` status
whose `until` has passed is shown as `stale`.

**Power levels** (`m.room.power_levels`), set by `admin` at creation and whenever `humans`
changes; a receiver treats the room as trusted only when they are exactly:

```json
{"users": {"<each human user ID>": 50},
 "users_default": 0, "events_default": 0, "state_default": 100,
 "invite": 100, "kick": 100, "ban": 100, "redact": 100,
 "notifications": {"room": 50},
 "events": {"m.room.power_levels": 100, "m.room.tombstone": 150,
            "m.room.redaction": 100, "m.reaction": 50, "m.sticker": 100,
            "agent_bus.status": 0}}
```

`admin`, as the room's creator in version 12, holds creator power and is not listed.

**Trusted room** (checked on join and on every change to these events): the room ID equals
the member config's `room`; `m.room.create` (read with `?format=event`) has sender `admin`
and room version 12, and no `additional_creators`; the power levels are exactly the above;
the team record is valid, sent by `admin`, and lists this agent with a role. An invite is
accepted only when its sender and its stripped `m.room.create` sender are `admin` and its
room ID is `room`; every other invite is rejected and logged by room ID only.

## 9. Validation

### On send (`pingbus send`)

In order; the first failure refuses the send and nothing reaches the homeserver:

1. Member config valid (§12), else exit 78.
2. The team room is trusted (§8), else exit 10.
3. The ping builds and passes every rule of §4-§6 and §11, else exit 4 with the reason code.
4. The sender's role permits the verb (§5) and every `to` is allowed (§4), else exit 4
   (`role` / `target`).
5. Local rate limits (§10), else exit 9.
6. Forge check (§6), else exit 5 (or 9 for `forge-rate`).
7. `PUT /rooms/{room}/send/m.room.message/{txnId}` with the content of §4; a retry after a
   timeout reuses the txnId.

`pingbus say` runs steps 1, 2 and 5 as above; in place of 3, 4 and 6, the text builds and
passes §7's agent-text rules and the sender holds a role (exit 4 with the reason code,
`secret` included); then step 7 with the content of §7.

### On receive (`recv`, `wait`, `watch`)

With no stored sync token, the first `/sync` uses `timeline.limit: 0` and only records
`next_batch`: history is never processed. Only the trusted team room is considered. Timeline
events of any other type are ignored, before anything else of them (their event ID included)
is read. For each `m.room.message` timeline event, in order:

1. Event ID fails §3: drop (`schema`).
2. Already seen: ignore. Sender is self: ignore.
3. `state_key` present, redacted, `content` not an object, or `origin_server_ts` not a
   non-negative integer: drop (`schema`).
4. **Sender class** from the team record: a listed human → the human path; an agent with a
   role → the ping path; anyone else (a non-team account, `admin`, the server user `conduit`,
   a removed member): drop (`sender`).

**Human path:** 04h. The bundle has `"human_text": false`: drop (`sender`). 05h. `msgtype`
not `m.text`: drop (`schema`). 06h. Edit: drop (`edit`). 07h. Body over the limit: drop
(`size`). 08h. Not addressed to this agent (§7): ignore. 09h. Reply fallback removed (§7);
empty: ignore. 10h. Too old: drop (`stale`). 11h. Sender over the receive flood limit: drop
(`rate`). 12h. Write to the inbox. Steps 04h-09h are the pure validator's; 10h and 11h need
a clock and are the caller's, so a reply that is empty after removal is ignored and never
counts as `stale` or towards the flood limit.

**Ping path:** 04p. Content carries the key `agent_bus.text` (an agent's text to humans, §7,
well formed or not, whoever it addresses): ignore it (`agent-text`): no `dropped.log`
line, no `DROPPED` count, no effect on the exit code. The one exception is content that
also carries the ping key: that event is neither, so it is dropped as `schema`. 05p. Content fails §4-§6 or §11
offline (the same function as on send): drop
with that reason; agent free text, a notice without the ping key, or a `body` that is not the
rendering all end here (`schema` / `body`). 06p. Verb not permitted for the sender's role:
drop (`role`). 07p. `to` does not contain this agent: ignore. 08p. `origin_server_ts` older
than the verb's ack timeout (§10; `ack_timeout_s` for verbs that expect none): drop
(`stale`). 09p. Sender over the receive flood limit: drop (`rate`). 10p. Forge check (§6):
drop (`unresolved` / `provenance`). 11p. Write to the inbox.

After the batch's inbox writes are durable, the sync token is saved.

**Reading the inbox.** `recv`, `wait`, `inbox` and the hooks re-run the
offline steps on every stored file and ignore any that fail (an inbox file is a cache that
code in the checkout could have written). Before printing an item, `recv` and `wait` fetch it
with `GET /rooms/{room}/event/{event}` and print from the fetched copy.

**Drop reason codes** (closed set): `version`, `schema`, `size`, `edit`, `sender`, `role`,
`target`, `verb`, `ref`, `allowlist`, `re`, `body`, `stale`, `rate`, `unresolved`,
`provenance`.

**Send-only refusals**: `secret` (§7). It refuses a send and never names a receive drop.

## 10. Limits

Defaults are built in. The member config's `limits` may change only the overridable ones,
within the bounds; a value outside, or any other key, is a config error (exit 78), never
silently clamped.

| Limit                    | Default                                      | Bounds        |
| ------------------------ | -------------------------------------------- | ------------- |
| `send_per_minute`        | 20 (token bucket refill)                     | 1 - 60        |
| `send_burst`             | 10                                           | 1 - 30        |
| `recv_per_sender_minute` | 60, humans included; excess dropped (`rate`) | 10 - 600      |
| `ack_timeout_s`          | 900 for `fetch`, `sync`, `review`, `run-qa`  | 60 - 86400    |
| `halt_ack_timeout_s`     | 300                                          | 30 - 3600     |
| `human_max_age_s`        | 86400                                        | 3600 - 604800 |
| `wait_timeout_s`         | 1500 (`wait --timeout` default)              | 1 - 1790      |

`send` and `say` draw on the same `send_per_minute` / `send_burst` bucket.

Fixed: the duplicate window is 60 s (same verb, ref, re and set of `to` again is refused,
exit 9); each `/sync` long-poll is 30 s; a server 429 is honoured (`Retry-After`, then
`retry_after_ms`, then 5 s), at most 3 tries, then exit 9; a ping's content at most 4096
bytes; a human message's body at most 16384 bytes; an agent text's `text` at most 4096
bytes.

The send limits are checked at §9 send step 5, so a send refused after step 5 still counts:
it has spent a token and is in the duplicate window. The receive flood count is held by the
process that receives, so each `recv` without a running watcher starts from zero.

An ack-expected ping with no `ack` or `nack` from a target by its deadline produces one
`TIMEOUT` line per silent target in the sender's next `recv` or `wait` (§15).

## 11. Allowlists

From the team record (§8), never from the member config:

- `repos`: 1 to 32 objects `{"repo": "OWNER/REPO", "branches": [BRANCH, …]}`. Every
  reference's repository must be listed (`allowlist`). `branches` (1 to 8 names, each
  `[A-Za-z0-9._/-]{1,100}`) are the branches whose history is trusted (§6); a fork is never
  listed.
- `path_prefixes`: 1 to 32 entries, each a `PATH` ending in `/` or an exact file `PATH`. A
  `path:` reference's `PATH` must start with one of them (`allowlist`). `PATH` has no `.` or
  `..` segments, so a plain prefix test is exact.

## 12. Member bundle and config

A member's bundle for one team is a directory `PINGBUS_HOME/<team>/` holding `member.json`
and `token` (one line, no trailing newline, owned by the user, mode 0600 or stricter, else
exit 78), written by `agent-bus add-member`. Pingbus keeps its state in
`PINGBUS_HOME/<team>/state/`: `inbox/` (one JSON file per event ID), `consumed/`,
`outbox.json`, `sync.json`, `team.json` (the last verified team record, for `status` and
the hooks), `forge-cache.json`, `dropped.log`, `lock`. `lock` is held with `flock` by the
one process syncing this team, which writes its kind into it: `watch` (a watcher), `wait`
(a waiter), or `recv` (a `recv` syncing once, for that sync only). A waker is live exactly
when a non-blocking `flock` on it fails and the kind it holds is `watch` or `wait`; a
`recv` holder is not a waker. No PID is
ever recorded: `/workspace` is shared across container namespaces, where a PID means
nothing.

- `PINGBUS_HOME`: default `${XDG_CONFIG_HOME:-~/.config}/pingbus`; a ccy session uses
  `/workspace/.claude/ccy/pingbus` (the entrypoint sets it).
- `PINGBUS_TEAMS`: comma-separated team names, each with a bundle; the **active** teams.
  Unset or empty: pingbus refuses every command but `version`, `validate` and
  `suggest-handle` (exit 78). A listed team with no valid bundle: exit 78.
- `--team NAME` selects one active team. `send` needs it when more than one team is active
  (exit 64 otherwise). `recv`, `wait`, `watch`, `inbox` and `status` cover every active
  team unless `--team` is given; `wait` and `watch` long-poll every team at once, one
  thread per team, and a team whose lock another process holds is reported busy on its own
  (exit 75 only when every team is busy).

`member.json`, read-only to pingbus; unknown keys are a config error:

| Key                | Type             | Rule                                                                                       |
| ------------------ | ---------------- | ------------------------------------------------------------------------------------------ |
| `protocol`         | integer          | `1`                                                                                        |
| `team`             | string           | team name; equals the directory name                                                       |
| `user_id`          | string           | `@<handle>:<server_name>`                                                                  |
| `server_name`      | string           | DNS name                                                                                   |
| `base_url`         | string           | `https://…`, or `http://` only when its host is an IP literal listed in `plain_http_hosts` |
| `plain_http_hosts` | array of strings | IP literals the homeserver listens on                                                      |
| `token_file`       | string           | `"token"`, resolved inside the bundle directory                                            |
| `admin`            | string           | user ID of the team's `admin` (the only trusted room creator, inviter and record sender)   |
| `room`             | string           | the team room's ID                                                                         |
| `human_text`       | boolean          | optional, default `true`; `false` drops every human message (§9 step 04h), pings only      |
| `limits`           | object           | optional; overridable keys of §10 only                                                     |

The token is sent only in the `Authorization: Bearer` header (unredirected, never through a
proxy, no redirect followed) and never appears in output, errors, URLs or logs.

## 13. CLI commands

Global options: `--team NAME`. Every command reads arguments only and never prompts.
Account and room administration is not a pingbus command: it is `agent-bus` on the
homeserver host.

| Command                                                                        | Does                                                                                                                                                                                                                                                                                                            | Network |
| ------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `send VERB [REF] (--to HANDLE[,HANDLE…] \| --to-orchestrator) [--re EVENT_ID]` | the only way to emit a ping; `--to` takes handles, human localparts, or full user IDs                                                                                                                                                                                                                           | yes     |
| `say --to HUMAN[,HUMAN…]`                                                      | the only way to emit an agent text (§7); the text is read from stdin; `--to` takes human localparts or full user IDs, never a handle                                                                                                                                                                            | yes     |
| `recv`                                                                         | sync once if the lock is free, then print and consume every pending item and due `TIMEOUT`                                                                                                                                                                                                                      | yes     |
| `wait [--timeout S]`                                                           | hold the locks, long-poll until at least one item or `TIMEOUT`, print and consume them, exit                                                                                                                                                                                                                    | yes     |
| `watch`                                                                        | started by the SessionStart hook: hold the locks, sync, fill the inbox, notify the session socket (counts and a rising notice number)                                                                                                                                                                           | yes     |
| `inbox`                                                                        | print pending items without consuming                                                                                                                                                                                                                                                                           | no      |
| `status`                                                                       | per team: handle, room trust, wake path (from the lock: watcher, waiter, none), pending count, overdue acks, drops, unexpected members; members' user IDs with their role or `human`, and their status (stale marked), from the last verified team record                                                       | no      |
| `validate VERB [REF] [--re EVENT_ID]` / `validate --event FILE`                | run the offline send-side or receive-side validator against the active team's last verified record (`--event` needs one; with no active team, `VERB` is checked for grammar only); prints `OK`, the reason code (exit 4), or `ignore <reason>` for an event a receiver skips silently (`agent-text` among them) | no      |
| `config check`                                                                 | validate every active team's bundle and the forge credential source                                                                                                                                                                                                                                             | no      |
| `suggest-handle`                                                               | print the `agent-bus add-member` arguments this environment implies (§3)                                                                                                                                                                                                                                        | no      |
| `hook session-start` / `hook prompt` / `hook stop` / `hook session-end`        | Claude Code hook entry points: stdin hook JSON, stdout hook JSON; always exit 0; fixed templates only                                                                                                                                                                                                           | no      |
| `version`                                                                      | §1                                                                                                                                                                                                                                                                                                              | no      |

Invites are accepted by the syncer itself, after the checks in §8. `show`, `peers` and
`tail` are deferred: no success criterion needs them, and `status` lists the members.

## 14. Exit codes (stable)

| Code | Meaning                                                                              |
| ---- | ------------------------------------------------------------------------------------ |
| 0    | success; `recv`/`wait` printed at least one line (drops, if any, are on stderr)      |
| 1    | never assigned (an uncaught exception)                                               |
| 2    | never assigned (argparse's default is remapped to 64)                                |
| 3    | nothing: `recv` found nothing; `wait` reached its timeout                            |
| 4    | refused by the validator (`secret` included), or by role                             |
| 5    | the reference did not resolve at the forge, or failed the provenance check           |
| 6    | `recv` only: received items were dropped and no valid line was printed               |
| 7    | homeserver unreachable                                                               |
| 8    | authentication refused (token rejected)                                              |
| 9    | rate limited (local limit, duplicate, server 429 after retries, or forge rate limit) |
| 10   | the team room is not trusted (§8), or not joined                                     |
| 64   | usage error                                                                          |
| 75   | busy: another process holds this account's sync lock (`wait`, `watch`)               |
| 78   | configuration refused (§12, §10 bounds, Python older than 3.11)                      |

`wait` never exits because of drops alone.

## 15. Output format

One line per item, fields separated by a single tab; no field contains a tab or newline;
absent fields are `-`. Every field is printed only after it passed its grammar. Senders and
targets are printed as localparts (a team's accounts share its `server_name`). Fields are
only ever appended in later versions, never reordered.

| Line      | Stream | Fields                                                                               |
| --------- | ------ | ------------------------------------------------------------------------------------ |
| `PING`    | stdout | `PING`, `1`, team, event ID, sender, verb, ref, re                                   |
| `HUMAN`   | stdout | `HUMAN`, `1`, team, event ID, sender, `origin_server_ts` (ms), text as a JSON string |
| `TIMEOUT` | stdout | `TIMEOUT`, `1`, team, event ID of the unanswered ping, silent target, verb, ref      |
| `SENT`    | stdout | `SENT`, `1`, team, event ID (from `send` or `say`)                                   |
| `DROPPED` | stderr | `DROPPED`, `1`, count, `reason=count` pairs joined by `,` (one line per batch)       |

The `HUMAN` text field is `json.dumps(body, ensure_ascii=True)`, so newlines, tabs and
control characters arrive escaped, and the line stays one line. Examples:

```
PING	1	<team>	$AbC…	orch.1+workstation.podman	review	pr:example-org/myrepo#12@0123…	-
HUMAN	1	<team>	$DeF…	alice	1791234567890	"please halt and commit what you have"
```

All other human-facing text goes to stderr, except for the report commands (`inbox`,
`status`, `config check`, `suggest-handle`) whose text is their output. No command prints a
room name, topic, invite reason, display name, `formatted_body`, unknown status key or
exception text taken from an event. The socket notification and every hook output are
fixed templates carrying counts only; the socket notification also carries the watcher's
monotonic notice number, because the socket drops a message identical to an earlier one:

```
agent-bus: <N> pending (<H> from humans, <P> pings), notice <S>. Run `pingbus recv`.
```
