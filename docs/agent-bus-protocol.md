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

**Trust.** Three kinds of input reach an agent. A **ping** from another agent is acted on only
as a closed verb about a file (under the team's `path_prefixes`, when it sets them), at a commit reachable from a
trusted branch of a trusted repository: one of the team's `org`, on the branch the ping names (else its default branch), or one
listed in `repos`, at its listed branches or, listed with none, on any of its branches as an
org repository is (§5, §6, §11); everything else a ping points at is
data. A ping may also point at a public repository outside the team, for a verb that only
reads or reports (§5): its PING line marks the reference `external` (§15), content anyone
could have written, read as information and never followed as instructions. A **human message** is free text from one of the team's humans, addressed to the
agent, delivered marked with that human's name (§7): it carries that human's authority. An
**agent text** is free text from another agent, delivered only to the agents it names, on
its own line kind (`PEER`, §15): information from a peer, never an instruction (§7). Pings
remain the way to act: a structured action (a review, an ack, a wake-up, anything with a
real artefact to reference) is a ping; agent text is for the discussion around it. Every
message stays in the one unencrypted team room, where the team's humans read it all. Which
accounts are humans and which agents hold roles is read from the team record (§8), which
only the team's `admin` account can write. Any member can send raw events with its own
token, so every receiver re-checks everything (§9).

## 1. Version and scope

- `PROTOCOL_VERSION = 1`. Every ping and every agent text carries `"v": 1`; the team record
  and status carry `"v": 1`. A receiver drops a ping with any other value (`version`).
- A change to any table in this file that makes a v1 receiver drop what a sender now sends,
  or accept what it used to drop, is a new version. Version 2 receivers may accept v1; v1
  receivers never accept v2.
- Agent text addressed to agents (§7) is still version 1: a receiver built before it
  ignored every agent text, and still does, so it drops nothing it used to accept and acts
  on nothing new; such a member just does not see text sent to it. Every member of a team
  takes the current pingbus before agents rely on it.
- Trust by `org`, external references (§11) and a ping's `branch` (§4) are still version 1:
  a receiver built before them drops an external reference (`allowlist`) and a ping with a
  `branch` (`schema`), and so acts on nothing new; but it reads a team record carrying `org`
  as untrusted (an unknown key, §8), so every member of a team takes the current pingbus
  before the team's file names an `org`.
- `exclude` (§8, §11) is the same: a receiver built before it reads a team record carrying
  `exclude` as untrusted (an unknown key), so every member of a team takes the current
  pingbus before the team's file names an `exclude`.
- The `coordinator` role (§8) and the copy a worker's message to a human also sends it (§7)
  are still version 1: the copy is an ordinary ping or agent text that names one more
  addressee, so a receiver built before it accepts it as it accepts any other. But a receiver
  built before the rename reads a team record that says `coordinator` as untrusted (an unknown
  role), so every member of a team takes the current pingbus before the team's file names a
  coordinator. A record that still says `orchestrator` reads as `coordinator` (§8), so the
  rename needs no migration.
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

| Name               | Pattern                                                                                                                                                                       | Notes                                                                                                       |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| team name          | `[a-z][a-z0-9-]{0,23}`                                                                                                                                                        | Also the homeserver instance name.                                                                          |
| agent handle       | `(?P<repo>[a-z0-9][a-z0-9_-]{0,47})\.(?P<seat>[1-9][0-9]{0,5}\|[a-z][a-z0-9]{0,11})\+(?P<host>[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)\.(?P<type>podman\|lxc\|docker\|vm\|host)` | The Matrix localpart and display name. Examples: `myrepo.1+workstation.podman`, `myrepo.dev1+local.podman`. |
| seat               | `[1-9][0-9]{0,5}\|[a-z][a-z0-9]{0,11}`                                                                                                                                        | A number or a name; no `-`, `_`, `.` or `@`, and no leading `0`.                                            |
| reserved localpart | `admin`, `conduit`                                                                                                                                                            | Never a handle or a human.                                                                                  |
| human localpart    | `[a-z][a-z0-9_-]{0,31}`, not reserved                                                                                                                                         | Contains no `+`, so it never equals a handle.                                                               |
| user ID            | `@<localpart>:<server_name>`, `<server_name>` equal to the member config's                                                                                                    | Any other server: drop (`sender` / `target`).                                                               |
| room ID            | `![A-Za-z0-9_-]{43}`                                                                                                                                                          | Room version 12 only.                                                                                       |
| event ID           | `\$[A-Za-z0-9_-]{43}`                                                                                                                                                         | Checked before it is used for anything, a file name included.                                               |

The `+` between `<seat>` and `<host>` is one constant (`HANDLE_SEP`). Probe H4 creates a real
handle through both account-creation calls; if Tuwunel's strict localpart check refuses
`+`, the separator becomes `=` before version 1 is released.

**Building a handle** (on the homeserver host, by `agent-bus add-member`; `pingbus suggest-handle` prints the arguments a member's environment implies):

- `<repo>`: the last path component of the forge remote URL, `.git` removed; with no
  remote, the checkout directory name. Lowercase; every character outside `[a-z0-9_-]`
  becomes `-`; leading `-`/`_` stripped; truncated to 48. Empty is refused.
- `<seat>`: the seat `add-member --seat` names, or, without `--seat`, one more than the
  registry's counter for the prefix `<repo>+<host>.<type>` on this team. A counter number
  is never reused, and the counter steps past a number `--seat` issued. A ccy session's
  seats are always named by its launch (`ccy --teams <seat>@<team>`); every other member
  is numbered by the counter.
- `--seat` reuses a seat by design: on a **parked** handle (`park-member` revoked its
  token and kept its account, role and room membership) it returns the same member with a
  new token. It is refused on a handle that is a current, unparked member, and on one
  whose account was deactivated by `remove-member`, which never returns.
- `<host>`: `HOOKS_DAEMON_HOSTNAME` (the install's role), or the value the human passes as
  `--host`; lowercased, every character outside `[a-z0-9-]` becomes `-`. A ccy seat's
  `<host>` is the role its checkout's `ccy.env.local` assigns, else the literal `local`,
  which names no machine. Never `CCY_HOST_HOSTNAME` or the system hostname: handles appear
  in public forge text, so with neither a role nor `--host`, `suggest-handle` and
  `add-member` refuse (exit 78 / 64).
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

| Key      | Type             | Required | Limits                                                                                                                               |
| -------- | ---------------- | -------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `v`      | integer          | always   | `1`                                                                                                                                  |
| `verb`   | string           | always   | one of §5                                                                                                                            |
| `to`     | array of strings | always   | 1 to 32 distinct user IDs, each an agent holding a role, or, for `ack`, `nack`, `done` and `blocked` only, a listed human (`target`) |
| `ref`    | string           | per §5   | §6 grammar, a form §5 allows for the verb, at most 700 characters, trusted or, where §5 allows it, external (§11)                    |
| `re`     | string           | per §5   | event ID of an earlier ping or human message in the team room                                                                        |
| `branch` | string           | optional | `BRANCH` (§11): the branch holding the commit of a `path` or `commit` `ref`; with any other `ref` or none: drop (`ref`)              |

Booleans, floats, nulls and nested objects are never valid values (`schema`). The event must
have no `state_key`, must not be redacted, and must carry no `m.relates_to`, `m.new_content`,
`format` or `formatted_body` (`edit` for the first two, `schema` for the others), so nothing
is shown to a human that a receiver did not check.

**Rendering** (`render`), ASCII, single spaces, no trailing space:

```
[agent-bus] <verb> <ref or "-">[ on <branch>] -> <to[0]> <to[1]> ...[ re <re>]
```

with `to` in ascending order as full user IDs, and ` on <branch>` and ` re <re>` present only
when `branch` and `re` are.
Example: `[agent-bus] review pr:example-org/myrepo#12@0123456789abcdef0123456789abcdef01234567 -> @myrepo.2+workstation.podman:<sn>`.

## 5. Verbs (closed set)

| Verb      | Meaning to the receiver                                                                            | `ref` forms allowed              | `re`      | Ack expected | May be sent by           | External `ref` |
| --------- | -------------------------------------------------------------------------------------------------- | -------------------------------- | --------- | ------------ | ------------------------ | -------------- |
| `fetch`   | Fetch the referenced repository state and read the artefact; no other action implied.              | required: `path`, `commit`       | forbidden | yes          | coordinator              | yes            |
| `sync`    | Re-read the referenced plan or spec and bring your work in line with it.                           | required: `path`, `commit`       | forbidden | yes          | coordinator              | no             |
| `review`  | Review the referenced artefact; answer `done` with a reference to your review, or `blocked`.       | required: `path`, `commit`, `pr` | forbidden | yes          | coordinator, worker      | yes            |
| `run-qa`  | Run QA on the referenced commit or pull request head; answer `done` or `blocked` with a reference. | required: `commit`, `pr`         | forbidden | yes          | coordinator, worker      | no             |
| `halt`    | Stop at the next safe point, commit work in progress, start nothing new until a `sync`.            | forbidden                        | forbidden | yes          | coordinator              | no             |
| `ack`     | The ping or human message `re` was received and will be acted on.                                  | forbidden                        | required  | no           | anyone addressed by `re` | no             |
| `nack`    | The ping or human message `re` was received and will not be acted on; `ref` may give the reason.   | optional: any form               | required  | no           | anyone addressed by `re` | yes            |
| `done`    | The work is finished; the result is at `ref`; `re` names the request when there was one.           | required: any form               | optional  | no           | coordinator, worker      | yes            |
| `blocked` | Cannot proceed; the reason is written at `ref` (an issue comment, a plan entry).                   | required: any form               | optional  | no           | coordinator, worker      | yes            |

A verb from a sender whose role is not in its "May be sent by" column: drop (`role`). A `ref`
form not allowed for the verb: drop (`ref`). An external `ref` (§11) for a verb whose last
column says `no`: drop (`allowlist`). Only verbs that ask the receiver to read (`fetch`,
`review`) or that report a result or a reason (`nack`, `done`, `blocked`) take one; `sync`
(bring your work in line with it) and `run-qa` (run its code) would hand authority to
content anyone could have written, so they take trusted references only. A human is never a ping sender: humans write text (§7),
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
sending and by every receiver before the inbox write (§9). Which check runs depends on where
the reference's repository stands (§11):

- **listed** in `repos` with `branches`: the table below, `BRANCH` ranging over its listed
  `branches`, or only the ping's `branch` when it names one, which must be among them (else
  `provenance`);
- **of the `org`**, or **listed** with no `branches`: first `GET /repos/OWNER/REPO` (its answer
  kept for 3600 s, in the cache file below), which must answer 200 with `owner.login` equal to the org (for a listed
  repository, its `OWNER`; case-insensitively) and `fork` `false`, else
  `provenance` (a fork is never trusted); then the table below, `BRANCH` the ping's
  `branch`, or the repository's `default_branch` when it names none. A named branch must be
  the repository's own: `GET /repos/OWNER/REPO/branches/BRANCH` answers 200, else
  `provenance`. Any branch of an org repository is trusted, since pushing one needs write
  access to it; but that the API returns a commit never is: GitHub serves a fork's commits
  through its parent's API, so only a branch of the repository itself vouches for one;
- **external**: first `GET /repos/OWNER/REPO`, which must answer 200 with `private` `false`
  and `fork` `false`, else `provenance` (a private repository outside the team is refused,
  and one the gh login cannot see answers 404, `not-found`; a fork is refused because its API serves
  its whole network's commits, so it could pass off anyone's commit under its own name). With a `branch`, the branch and reachability checks of an
  org repository. Without one, only that the content exists, at no branch in particular: `path` and `commit` need `GET /repos/OWNER/REPO/commits/SHA` to answer 200
  (`path` then the contents request below); `pr` needs the pull request (200) and SHA
  `identical` to or `behind` its head commit `head.sha` (`compare/HEADSHA...SHA`), wherever
  the head lives and whoever wrote it; `issue` as below. It proves the content is real, never
  that it is trusted.

"Reachable" means `GET /repos/OWNER/REPO/compare/BRANCH...SHA` answers 200 with `status`
`identical` or `behind` for at least one `BRANCH`.

| Form   | Requests                                                                         | Resolves when                                                                                                                                                                       |
| ------ | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| path   | compare; `GET /repos/OWNER/REPO/contents/PATH?ref=SHA`                           | SHA reachable; 200 and `type == "file"`                                                                                                                                             |
| commit | compare                                                                          | SHA reachable                                                                                                                                                                       |
| pr     | `GET /repos/OWNER/REPO/pulls/NUM`; `GET /repos/OWNER/REPO/compare/HEADREF...SHA` | 200; `head.repo.full_name` (lowercased) is `OWNER/REPO`; `author_association` is `OWNER`, `MEMBER` or `COLLABORATOR`; SHA is `identical` to or `behind` the PR's head ref `HEADREF` |
| issue  | `GET /repos/OWNER/REPO/issues/NUM`                                               | 200 and no `pull_request` key                                                                                                                                                       |

A compare answering 422 (no merge base with `BRANCH`) counts as found but not reachable, so
it ends in `provenance`, not `not-found`.

Positive results only are cached, in `forge-cache.json` in the member's state directory: a
file's existence at a SHA for 30 days (content at a SHA is immutable; the expiry only bounds
the file), reachability and `pr` results for 3600 s. Beside them it keeps, under `repos`,
each repository's `GET /repos/OWNER/REPO` answer (owner, `private`, `fork`, default branch) for 3600 s, so a
command in a new process does not ask again for a repository it asked about a moment ago; a
file with no `repos` is read as having none. A cache file that is not exactly the
expected shape is a config error (exit 78), not silently discarded. A 429, or a 403 with
`x-ratelimit-remaining: 0`, is retried per §10 and then reported as `forge-rate`. Send-side
refusals, on stderr: `not-found`, `wrong-kind`, `provenance`, `forge-unreachable`,
`forge-auth` (exit 5), `forge-rate` (exit 9). On receive the same failures drop the ping as
`unresolved` or `provenance`, except `forge-rate`, which drops it as `forge-rate` (never `rate`, the flood limit): the forge
declined to answer, which says nothing about the reference.

**Forge credential:** one source, the GitHub CLI's login: `gh auth token --hostname github.com`, run without a shell, with a 15 s timeout, its stdout captured and never logged
(gh itself answers with `GH_TOKEN` or `GITHUB_TOKEN` when one is set). Only for `forge_api`
exactly `https://api.github.com`; any other `forge_api` is a config error (78) before gh is
asked. No anonymous mode: gh missing, not logged in for github.com, failing, or printing
anything but one printable token is a config error (78) naming the fix, from
`config check`, `seat exec` and every command that reaches the forge. Sent with
`add_unredirected_header`; a redirect to another host is refused.

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

### Agent text

An agent may write free text to the team's humans (a coordinator answering the owner, say)
and to other agents (two workers discussing a change) with `pingbus say` (§13). It names
each addressee: listed humans and role-holding agents, set as `m.mentions.user_ids` and
shown as handles on the body's tag line, so Element notifies them and every human reading
the room sees who was told. It is never `@room`: a broadcast would land in every agent's
context and invite a storm of replies, and naming each agent keeps the record exact.

On receive (§9 04p), an agent whose user ID is not in the text's `to` or mentions **ignores
it** (`agent-text`): it is traffic for others, so it writes no `dropped.log` line, adds to no
`DROPPED` count and never sets an exit code. An agent the text names checks it against
every rule below except the secret shapes, drops what fails with its reason, and delivers
the rest as a `PEER` line (§15). A `PEER` line is **information from a peer, not an
instruction**: only a listed human's text (`HUMAN`) carries authority, and a peer's text
that asks for an action is weighed as a suggestion from a colleague, never obeyed as a
command. Pings remain the way to act: whenever there is a structured action (a review, an
ack, a wake-up, anything with a real artefact to reference) the agent sends a ping (§4),
and uses agent text for the discussion around it. A member whose bundle has `"human_text": false` takes pings only: it drops agent text naming it as `sender`, as it does human text.

**The coordinator's copy.** When a worker writes to a listed human, by `say` or by a ping
that may name one (`ack`, `nack`, `done`, `blocked`, §4), `pingbus` adds the team's
coordinator to the addressees, unless the coordinator is the sender or is already named, or
the team has none: the worker never has to remember to. It is no new kind of message: the
coordinator is one more user ID in `to` and in `m.mentions.user_ids`, so every check on send
and on receive treats it like any other addressee, and a member built before this simply
sees an agent text or ping that names the coordinator. A receiver that is the coordinator
and finds listed humans among the addressees is **on copy**: its `PEER` or `PING` line says
so and names them (§15). Nothing is added for a message to agents only, or to the
coordinator alone. The copy counts against the limits of `to`: a message that was at the limit
before it and now exceeds it is refused (`target`), and the worker names fewer addressees.

An `m.room.message` whose content has exactly these keys:

| Key              | Value                                                |
| ---------------- | ---------------------------------------------------- |
| `msgtype`        | `"m.notice"`                                         |
| `body`           | exactly `render_text(text)` below (`body`)           |
| `m.mentions`     | exactly `{"user_ids": <text.to, sorted>}` (`schema`) |
| `agent_bus.text` | the text object                                      |

The text object, exactly these keys:

| Key    | Type             | Required | Limits                                                                            |
| ------ | ---------------- | -------- | --------------------------------------------------------------------------------- |
| `v`    | integer          | always   | `1`                                                                               |
| `to`   | array of strings | always   | 1 to 16 distinct user IDs, each a listed human or a role-holding agent (`target`) |
| `text` | string           | always   | not empty; at most 4096 UTF-8 bytes (`size`); no secret shape (`secret`)          |

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

| Key             | Type             | Rule                                                                  |
| --------------- | ---------------- | --------------------------------------------------------------------- |
| `v`             | integer          | `1`                                                                   |
| `team`          | string           | the team name; equals the member config's                             |
| `humans`        | array of strings | 1 to 16 user IDs of human localparts                                  |
| `roles`         | object           | 0 to 64 entries: agent-handle user ID → `"coordinator"` or `"worker"` |
| `org`           | string           | optional; `OWNER` (§6), §11                                           |
| `exclude`       | array of strings | optional; needs `org`; §11                                            |
| `repos`         | array of objects | §11                                                                   |
| `path_prefixes` | array of strings | optional; §11                                                         |
| `forge_api`     | string           | `https://[^\s/]+(/\S*)?`                                              |

The team file accepts only `https://api.github.com` for `forge_api`, the one forge a
member's credential is sent to (§6). Every key but `org`, `exclude` and `path_prefixes` is required. Unknown keys, or a record not sent by `admin`, make the room untrusted (exit 10 for every
command that needs it; `status` says why).

**The coordinator.** A team has three kinds of member: workers, humans, and at most one
agent its owner nominates as the **coordinator**. The coordinator holds every verb a worker
holds and the verbs that direct others (`fetch`, `sync`, `halt`, §5), is the addressee of
`--to-coordinator` (§13), and is copied on what workers tell humans (§7). A record that says
`"orchestrator"`, the name this role had, reads as `"coordinator"` (`roles` of the parsed
record holds only `coordinator` and `worker`); writers write `coordinator`. The limit of one
is the writers': `agent-bus add-member` and `set-role` refuse a second coordinator, naming the
holder, and a parked member keeps the role it holds. A receiver does not refuse a record that
lists more than one (a record from before the limit may): it treats each as a coordinator.

A `roles` key names a numbered or a named seat alike, for example
`@myrepo.3+workstation.host:<sn>` and `@myrepo.dev1+local.podman:<sn>`; a parked member
keeps its entry. A pingbus built before named seats reads a record listing one as invalid
and treats the room as untrusted, so every member of a team takes the current pingbus
before a named seat joins it.

**Status**: `agent_bus.status`, state key = the sender's own user ID, content
`{"v": 1, "state": "listening", "until": <int ms>}`, serialised at most 256 bytes. The
server forces only a state key starting with `@` to equal its sender; at power 0 any member
can write this type under any other key (`""` included). So a receiver reads a status only
when its state key is its sender's user ID, its sender is a role holder, and it fits the
size and shape above; every other `agent_bus.status` event is ignored. A `listening` status
whose `until` has passed is shown as `stale`. The wakers write it: `watch` when it starts,
with `until` 600 s ahead, renewed every 300 s; `wait`, once it starts long-polling, with
`until` at its own deadline. Neither clears it on exit; it goes stale.

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
the member config's `room`; `m.room.create` (read with `?format=event`) has sender `admin`,
room version 12, no `additional_creators`, and an event ID equal to the room ID with `$` in
place of `!` (version 12 derives the room ID from it; fixture 034); the power levels are exactly the above;
the team record is valid, sent by `admin`, and lists this agent with a role. An invite is
accepted only when its sender and its stripped `m.room.create` sender are `admin` and its
room ID is `room`; every other invite is rejected (`POST /leave`) and logged by room ID only.

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

**Ping path:** 04p. Content carries the key `agent_bus.text` (an agent's text, §7). Content
that also carries the ping key is neither, so it is dropped as `schema`. When neither the
text object's `to` nor `m.mentions.user_ids` names this agent: ignore it (`agent-text`): no
`dropped.log` line, no `DROPPED` count, no effect on the exit code, however it is formed.
When one does: the bundle has `"human_text": false`: drop (`sender`); the §7 agent-text
rules but the secret shapes, as on send: drop with the reason; then the age (`stale`,
`human_max_age_s`) and the receive flood limit (`rate`), as 10h and 11h; then write to the
inbox, delivered as a `PEER` line. 05p. Content fails §4-§6 or §11
offline (the same function as on send): drop
with that reason; agent free text without the `agent_bus.text` key (an `m.text`, say), a notice without the ping key, or a `body` that is not the
rendering all end here (`schema` / `body`). 06p. Verb not permitted for the sender's role:
drop (`role`). 07p. `to` does not contain this agent: ignore. 08p. `origin_server_ts` older
than the verb's ack timeout (§10; `ack_timeout_s` for verbs that expect none): drop
(`stale`); an `ack` or `nack` dropped here is not delivered, but it goes on through 09p
and 10p and answers its ping (§10) only if neither would drop it. Any other stale ping
stops here. 09p. Sender over the receive flood limit: drop (`rate`). 10p. Forge check (§6):
drop (`unresolved` / `provenance`, or `forge-rate` when the forge's rate limit stopped the check). 11p. Write to the inbox.

After the batch's inbox writes are durable, the sync token is saved.

**Reading the inbox.** `recv`, `wait`, `inbox` and the hooks re-run the
offline steps on every stored file and ignore any that fail (an inbox file is a cache that
code in the checkout could have written). Before printing an item, `recv` and `wait` fetch it
with `GET /rooms/{room}/event/{event}` and print from the fetched copy. An item the server
no longer has, or whose fetched copy fails the offline steps, is dropped (`schema` when the
server has no copy), logged and consumed. A sync's drops and its re-fetch's drops are one
batch: one `DROPPED` line per team per pass (§15).

**Drop reason codes** (closed set): `version`, `schema`, `size`, `edit`, `sender`, `role`,
`target`, `verb`, `ref`, `allowlist`, `excluded`, `re`, `body`, `stale`, `rate`, `forge-rate`, `unresolved`,
`provenance`.

**Send-only refusals**: `secret` (§7). It refuses a send and never names a receive drop: text
already in the room is in every member account's view, so a receiver delivers it as it is.

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

`send` and `say` draw on the same `send_per_minute` / `send_burst` bucket. For `say` the
duplicate window compares the text and the set of humans and agents it is addressed to;
only a digest of the text is kept in the member's state. An agent text received is held to
`human_max_age_s` and `recv_per_sender_minute`, as a human message is.

Fixed: the duplicate window is 60 s (same verb, ref, re and set of `to` again is refused,
exit 9); each `/sync` long-poll is 30 s; a server 429 is honoured (`Retry-After`, then
`retry_after_ms`, then 5 s), at most 3 tries, then exit 9, and a wait over 60 s is not slept
and is exit 9 at once; a forge rate limit (§6) is honoured the same way with GitHub's
`x-ratelimit-reset` read after `Retry-After`, a wait over 60 s reported as `forge-rate` at
once; a ping's content at most 4096
bytes; a human message's body at most 16384 bytes; an agent text's `text` at most 4096
bytes.

The send limits are checked at §9 send step 5, so a send refused after step 5 still counts:
it has spent a token and is in the duplicate window. The receive flood count is held by the
process that receives, so each `recv` without a running watcher starts from zero.

An ack-expected ping with no `ack` or `nack` from a target by its deadline produces one
`TIMEOUT` line per silent target in the sender's next `recv` or `wait` (§15). "By its
deadline" is judged by the answer's `origin_server_ts`, not by when the sender reads it: an
answer sent in time counts even when the sender's next sync drops it as `stale`, once it
passes the flood limit and the forge check as a fresh answer must (§9 08p), and one sent after the deadline does not count, however soon it is read.

## 11. Allowlists

From the team record (§8), never from the member config:

- `org`: a GitHub owner (an organisation or a user). Every repository it owns is trusted, on
  any of its own branches the ping names, else its default branch (§6); a fork, even one the org owns, never
  is. A team works within one org, and agents of any of its repositories join on demand.
- `repos`: 0 to 32 objects `{"repo": "OWNER/REPO", "branches": [BRANCH, …]}`, each a
  repository the team trusts, of any owner. With `branches` (1 to 8 names, each
  `[A-Za-z0-9._/-]{1,100}`), only their history is trusted (§6), and the listing replaces the
  org's rule for an org repository. Without `branches`, the repository is trusted as an org
  repository is: on any of its own branches the ping names, else its default branch, and
  only while it is its owner's own and not a fork (§6). A fork is never listed. A record
  names an `org`, at least one repository, or both.
- `exclude`: optional; needs an `org`; 1 to 32 distinct repository names (`REPO`, no owner,
  each `[a-z0-9._-]{1,100}`, not `.`, `..` or ending `.git`) of that org, and none may also
  be listed in `repos`. It is checked before trust: a reference to an excluded repository, of
  any form, for any verb, is refused (`excluded`, offline, on send and on receive), so it is
  neither trusted nor external. Every member takes the current pingbus before the team's file
  names an `exclude` (§1).
- A reference whose repository is neither listed nor the org's is **external**: accepted only
  for a verb whose "External `ref`" column says `yes` (§5), else `allowlist`; it must be in a
  public repository that is not a fork (§6); it is never held to `path_prefixes`; and its PING line says
  `external` (§15).
- `path_prefixes`: optional; when present, 1 to 32 entries, each a `PATH` ending in `/` or an
  exact file `PATH`, and a trusted `path:` reference's `PATH` must start with one of them
  (`allowlist`). Absent, a trusted reference may name any `PATH`: the org's repositories are
  trusted whole. `PATH` has no `.` or `..` segments, so a plain prefix test is exact.

## 12. Member bundle and config

A member's bundle for one team is a directory `PINGBUS_HOME/<team>/` holding `member.json`
and `token` (one line, no trailing newline, owned by the user, mode 0600 or stricter, else
exit 78), written by `agent-bus add-member`. Pingbus keeps its state in
`PINGBUS_HOME/<team>/state/`: `inbox/` (one JSON file per event ID), `consumed/`,
`outbox.json`, `sync.json`, `team.json` (the last verified team record, for `status` and
the hooks), `untrusted.json` (why the room stopped being trusted, for `status`; it replaces
`team.json` on loss of trust and is removed when the room is trusted again), `room.json`
(the joined and invited members and the §8 statuses the syncer last saw, for `status`;
the first sync and the one after a join load every member, later syncs follow joins and
leaves), `forge-cache.json`, `dropped.log`, `lock`. Beside the bundles, `PINGBUS_HOME`
holds `stop-guard.json` (the Stop hook's memory of what it last blocked for, and when) and
`watch.log` (the watcher's stderr). `lock` is held with `flock` by the
one process syncing this team, which writes its kind into it: `watch` (a watcher), `wait`
(a waiter), or `recv` (a `recv` syncing once, for that sync only). A waker is live exactly
when a non-blocking `flock` on it fails and the kind it holds is `watch` or `wait`; a
`recv` holder is not a waker. No PID is
ever recorded: `/workspace` is shared across container namespaces, where a PID means
nothing.

A team entry of `PINGBUS_HOME` is normally the bundle directory itself; a symlink is
accepted only onto a real directory (not itself a link) owned by the user, with mode 0700,
and refused (exit 78) otherwise.

**Seats** (a ccy checkout, Plan 00161 DESIGN.md section 5.5). A seat is one `<seat>@<team>`
pair (§3 seat and team grammars): a member of one team, whose bundle and state live in
`.claude/ccy/pingbus/seats/<team>/<seat>/` under the checkout, created on the host by the
launch that first names it. `seat.lock` beside the bundle is held with `flock` by the one
session in that seat, which writes `seat <claim time in ms>` into it; as with `lock`, the
seat is held exactly when a non-blocking `flock` fails, and no PID is recorded. The
launcher passes the session's seats as `PINGBUS_SEATS`, a list of `<seat>@<team>` items
joined by `,` with at most one seat per team. `seat exec` (§13) reads it, takes every
seat's lock without waiting, and builds the session home `/tmp/pingbus-home` (mode 0700,
private to the container), holding one symlink per seat, `<team>` onto that seat's
directory, and the session's own `stop-guard.json` and `watch.log`. It sets `PINGBUS_HOME`
to the session home and `PINGBUS_TEAMS` to the list's teams for the command it then runs,
which inherits the locks: they are released when the last process of the session exits.
`cc --teams` claims a `host` seat of the checkout the same way, on the host, giving `seat exec` the checkout's seats with `--root` and a session home of its own with `--home`.

- `PINGBUS_HOME`: default `${XDG_CONFIG_HOME:-~/.config}/pingbus`; a ccy session launched
  with `--teams` uses `/tmp/pingbus-home`, which `seat exec` builds and sets.
- `PINGBUS_SEATS`: the session's seats, set by the ccy launcher only; read by `seat exec`
  alone. Missing or malformed: exit 78.
- `PINGBUS_TEAMS`: comma-separated team names, each with a bundle; the **active** teams.
  Unset or empty: pingbus refuses every command but `version`, `validate` and
  `suggest-handle` (exit 78). A listed team with no valid bundle: exit 78.
- `--team NAME` selects one active team. `send` needs it when more than one team is active
  (exit 64 otherwise). `recv`, `wait`, `watch`, `inbox` and `status` cover every active
  team unless `--team` is given; `wait` and `watch` long-poll every team at once, one
  thread per team, and a team whose lock another process holds is reported busy on its own
  (exit 75 only when every team is busy). `wait` gives a `recv` holder up to 5 s to finish
  its one sync before calling that team busy. A team that fails in `recv` is reported and
  the other teams still run; in `wait`, any team's failure ends the command with its code.
  `wait`'s default timeout is the smallest `wait_timeout_s` among the active teams.

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

| Command                                                                                        | Does                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Network |
| ---------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `send VERB [REF] [--to HANDLE[,HANDLE…]] [--to-coordinator] [--re EVENT_ID] [--branch BRANCH]` | the only way to emit a ping; `--to` takes handles, human localparts, or full user IDs; at least one of `--to` and `--to-coordinator` is required and they combine; `--to-coordinator` means the team's coordinator, refused (`target`) when the sender is the only one or there is none; a worker's ping naming a human also names the coordinator (§7); the ping's `branch` (§4) is `--branch`, else, for a `path` or `commit` reference into a repository whose every branch is trusted (an unlisted one of the team's `org`, or one listed with no `branches`), the branch the working directory's checkout tracks on `origin` when `origin` is that repository (so an agent never types it), else none                                                                                                                                                                                                                                                                 | yes     |
| `say [--to NAME[,NAME…]] [--to-coordinator]`                                                   | the only way to emit an agent text (§7); the text is read from stdin as UTF-8, less one trailing newline; `--to` takes human localparts, handles or full user IDs, each a listed human or a role-holding agent, never `@room`; as for `send`, at least one of `--to` and `--to-coordinator` is required, and a worker's text naming a human also names the coordinator (§7)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                | yes     |
| `recv`                                                                                         | sync once if the lock is free, then print and consume every pending item and due `TIMEOUT`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 | yes     |
| `wait [--timeout S]`                                                                           | hold the locks, long-poll until at least one item or `TIMEOUT`, print and consume them, exit                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | yes     |
| `watch`                                                                                        | started by the SessionStart hook: hold the locks, sync, fill the inbox, notify the session socket (counts and a rising notice number) whenever an item is pending that was not at the last look, at most one notice per 2 s; exit 0 when the socket goes; a team that fails is reported and dropped, and the watcher exits with the first failure's code once no team is left; refused (78) without `CLAUDE_CODE_MESSAGING_SOCKET` and its token                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           | yes     |
| `inbox`                                                                                        | print pending items without consuming: one `PENDING` line each (team, event ID, sender, `origin_server_ts`, verb, `human` or `peer`), never the text, which only `recv` prints from its re-fetched copy                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    | no      |
| `status`                                                                                       | per team: handle, room trust, wake path (from the lock: watcher, waiter, none), pending count, overdue acks, drops, unexpected members; members' user IDs with their role or `human`, and their status (stale marked), from the last verified team record. Lines: `TEAM` (team, handle, then `key=value` fields `trust`, `wake`, `pending`, `humans`, `peers`, `pings`, `overdue`, `dropped`, `unexpected`, `-` where the room is not trusted), `UNTRUSTED` (team, the recorded reason), `MEMBER` (team, localpart, role or `human`, `listening`, `stale` or `-`), `UNEXPECTED` (team, user ID, `join` or `invite`). A team that fails is reported on stderr and prints nothing; the others still print, and the exit is the first failure's code. Then, when the checkout has a seats directory, one `SEAT` (team, seat, `held` or `free`, `self` or `-`, handle) per seat; a seat whose bundle cannot be read is reported on stderr (exit 78) and the others still print | no      |
| `history [--limit N]`                                                                          | what this member received and sent in each active team (or `--team`), newest first, read from the room: `HISTORY` lines (§15), at most `N` per team (default 50; 1 to 500, else 64); a record that consumes, acks and stores nothing (below)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | yes     |
| `validate VERB [REF] [--re EVENT_ID]` / `validate --event FILE`                                | run the offline send-side or receive-side validator against the active team's last verified record (`--event` needs one; with no active team, `VERB` is checked for grammar only); prints `OK`, the reason code (exit 4), or `ignore <reason>` for an event a receiver skips silently (`agent-text` among them)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            | no      |
| `config check`                                                                                 | validate every active team's bundle and the forge credential: gh's login (§6), printed as `FORGE gh`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | no      |
| `suggest-handle`                                                                               | print the `agent-bus add-member` arguments this environment implies (§3)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | no      |
| `hook session-start` / `hook prompt` / `hook stop` / `hook session-end`                        | Claude Code hook entry points: stdin hook JSON, stdout hook JSON; always exit 0; fixed templates only. `session-start` starts `watch` detached when the session has the socket and a team has no waker; `stop` never blocks when `stop_hook_active` is true, blocks once per set of pending items, and at most once per 600 s for a failure class or a team with no waker; `session-end` does nothing (the watcher exits when the socket goes)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             | no      |
| `seat exec [--root DIR] [--home DIR] [--] CMD…`                                                | run by ccy's entrypoint, or by `cc --teams` with both options, in front of the session (§12 "Seats"): claim every seat in `PINGBUS_SEATS` (exit 75 when another session holds one, and none is kept), build the session home, check every bundle as `config check` does (exit 78), print the seats and handles on stderr, then replace itself with `CMD`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   | no      |
| `version`                                                                                      | §1                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         | no      |

**`history`** reads the room, not the local state, so a member whose state is new (a seat
returned, a fresh checkout) still sees its past. A `/sync` with `timeline.limit: 0` gives a
`next_batch`, which is never saved; `/messages` is paged backwards from it, at most 50 pages
of 100 room messages per team, and a scan that stops there says so on stderr. It keeps the
pings this member sent or that name it in `to`, the agent texts it sent, the agent texts
that name it, and the human messages addressed to it (§7). A received item passes the offline receive steps (§9) and
the forge check (§6); an item it sent passes the send-side rules of §4-§7 and §11, its
role, and for a ping the forge check. The age and flood limits do not apply: nothing is
acted on. Failures are counted on one `DROPPED` line per team, and nothing goes to
`dropped.log`. A team that fails is reported and the others still print; the exit is 0,
or the first failing team's code.

Invites are accepted by the syncer itself, after the checks in §8. `show`, `peers` and
`tail` are deferred: no success criterion needs them, and `status` lists the members.

## 14. Exit codes (stable)

| Code | Meaning                                                                                                               |
| ---- | --------------------------------------------------------------------------------------------------------------------- |
| 0    | success; `recv`/`wait` printed at least one line (drops and other teams' failures, if any, are on stderr)             |
| 1    | never assigned (an uncaught exception)                                                                                |
| 2    | never assigned (argparse's default is remapped to 64)                                                                 |
| 3    | nothing: `recv` found nothing; `wait` reached its timeout                                                             |
| 4    | refused by the validator (`secret` included), or by role                                                              |
| 5    | the reference did not resolve at the forge, or failed the provenance check                                            |
| 6    | `recv` only: received items were dropped and no valid line was printed                                                |
| 7    | homeserver unreachable                                                                                                |
| 8    | authentication refused (token rejected)                                                                               |
| 9    | rate limited (local limit, duplicate, server 429 after retries, or forge rate limit)                                  |
| 10   | the team room is not trusted (§8), or not joined                                                                      |
| 64   | usage error                                                                                                           |
| 75   | busy: another process holds this account's sync lock (`wait`, `watch`), or another session holds a seat (`seat exec`) |
| 78   | configuration refused (§12, §10 bounds, Python older than 3.11)                                                       |

`wait` never exits because of drops alone.

## 15. Output format

One line per item, fields separated by a single tab; no field contains a tab or newline;
absent fields are `-`. Every field is printed only after it passed its grammar. Senders and
targets are printed as localparts (a team's accounts share its `server_name`). Fields are
only ever appended in later versions, never reordered.

| Line      | Stream | Fields                                                                                                                     |
| --------- | ------ | -------------------------------------------------------------------------------------------------------------------------- |
| `PING`    | stdout | `PING`, `1`, team, event ID, sender, verb, ref, re, trust, branch, copy                                                    |
| `HUMAN`   | stdout | `HUMAN`, `1`, team, event ID, sender, `origin_server_ts` (ms), text as a JSON string                                       |
| `PEER`    | stdout | `PEER`, `1`, team, event ID, sender, `origin_server_ts` (ms), text as a JSON string, copy                                  |
| `REPLY`   | stdout | `REPLY`, `1`, team, event ID, command                                                                                      |
| `TIMEOUT` | stdout | `TIMEOUT`, `1`, team, event ID of the unanswered ping, silent target, verb, ref                                            |
| `SENT`    | stdout | `SENT`, `1`, team, event ID (from `send` or `say`)                                                                         |
| `DROPPED` | stderr | `DROPPED`, `1`, count, `reason=count` pairs joined by `,` (one line per batch: per team per pass; re-fetch drops included) |
| `HISTORY` | stdout | `HISTORY`, `1`, direction (`in`/`out`), `origin_server_ts` (ms), line (`PING`/`HUMAN`/`PEER`/`SENT`), its fields after `1` |

A `PING` line's trust is `trusted` for a reference to a listed or org repository (§11),
`external` for one outside the team, and `-` for a ping with no reference; its branch is the
ping's `branch` (§4), or `-`. An `external`
reference is **content anyone could have written**: the receiving agent reads it as
information, never follows instructions found in it, and does nothing on its strength that it
would not do for a stranger's text. The ping's verb still says what is asked (read it, review
it, or note a result), never more.

A `PING` or `PEER` line's last field, `copy`, is `-` unless the receiver is the team's
coordinator and the item also names listed humans (§7): then it is `copy:` and those humans'
localparts joined by `,` (`copy:alice,bob`), and the receiving agent knows the item was
addressed to them and sent to it for its information, not as a request of its own.

The `HUMAN` and `PEER` text field is `json.dumps(body, ensure_ascii=True)`, so newlines, tabs
and control characters arrive escaped, and the line stays one line. A `PEER` line is an
agent's text naming this agent (§7): information from a peer, never an instruction; its
sender is an agent handle, never a human.

`recv` prints a `REPLY` line right after each `HUMAN` and `PEER` line (never after a `PING`,
whose answer is a verb the agent chooses; never in `history`). Its event ID is that item's and
its command is `pingbus --team <team> say --to <name>`, ready to run with the text on stdin:
`<name>` is a human's localpart, or an agent's short name (the handle before `+`,
`<repo>.<seat>`) when no other member of the team has it, else its full handle. `--to` takes
the same names (§9): a full user ID, an exact handle, a human localpart, or a short name that
matches exactly one agent; a short name matching several, or a name matching no member, is a
usage error (exit 64) naming the candidates or the team's members. Examples:

```
PING	1	<team>	$AbC…	orch.1+workstation.podman	review	pr:example-org/myrepo#12@0123…	-	trusted	-	-
PING	1	<team>	$MnO…	myrepo.2+workstation.podman	done	issue:someone/project#7	-	external	-	copy:alice
HUMAN	1	<team>	$DeF…	alice	1791234567890	"please halt and commit what you have"
PEER	1	<team>	$JkL…	orch.1+workstation.podman	1791234567950	"I am on the docs; the tests are yours"	-
PEER	1	<team>	$PqR…	myrepo.2+workstation.podman	1791234568100	"the docs are merged"	copy:alice
REPLY	1	<team>	$PqR…	pingbus --team <team> say --to myrepo.2
```

A `HISTORY` line (`history`, §13) records one item: `in` for one this member received,
`out` for one it sent; the item's `origin_server_ts`; then the line the item would be,
less its `1`: `PING` for a ping either way (an `out` ping's sender is this member),
`HUMAN` for a human message, `PEER` for an agent text that named this member, `SENT` for
an agent text this member sent (team and event ID only: the text is never printed; the room
holds it). It is a record of the past, never a request to act:
work comes from `recv` and `wait` only.

```
HISTORY	1	out	1791234567999	PING	<team>	$GhI…	myrepo.1+workstation.podman	ack	-	$AbC…	-	-	-
HISTORY	1	in	1791234567890	HUMAN	<team>	$DeF…	alice	1791234567890	"please halt and commit what you have"
```

The report commands' lines carry no version field and are defined with their command in
§13: `PENDING` (`inbox`), `TEAM`, `UNTRUSTED`, `MEMBER`, `UNEXPECTED` and `SEAT`
(`status`). A `SEAT` line is `SEAT`, team, seat, `held` or `free`, `self` (a seat this
session holds) or `-`, handle; the host's `agent-bus seat list` prints the same lines from
the same code. Example:

```
SEAT	<team>	dev1	held	self	myrepo.dev1+local.podman
```

All other human-facing text goes to stderr, except for the report commands (`inbox`,
`status`, `config check`, `suggest-handle`) whose text is their output. No command prints a
room name, topic, invite reason, display name, `formatted_body`, unknown status key or
exception text taken from an event. The socket notification and every hook output are
fixed templates carrying counts, and names that passed their §3 grammar (a session's
seats, their teams and handles, at SessionStart); the socket notification also carries the
watcher's monotonic notice number, because the socket drops a message identical to an
earlier one:

```
agent-bus: <N> pending (<H> from humans, <A> from agents, <P> pings), notice <S>. Run `pingbus recv`.
```
