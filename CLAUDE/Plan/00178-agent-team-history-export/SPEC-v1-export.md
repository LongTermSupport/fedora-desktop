# SPEC: the v1 team history export (`teamchat-admin migrate export`)

The intended behaviour of the export of a v1 agent-team's room history, built in Go before v1
is retired (the owner's direction: export the v1 history before v1 goes). It is the export of
Plan 00209 DESIGN-v2-D-migration.md 5.6 (OD-D4: export, archive and link), built on its own
because the migration that would have run it (Plan 00209 Task 6.1) is not built.

The facts about v1 that this spec rests on (paths, owners, modes, the homeserver's address and
the event shapes) are read from v1's code, not assumed. Each expectation (E1-E21) gets a Go test,
written first and seen to fail.

## Where it lives

- `src-go/internal/teamchat/migrate/v1`: everything that names v1 (its paths, file names, event
  types). It is the one v2 package allowed to name v1 (D 4.1, the gate `teamchat-keeps-off-v1`).
- `teamchat-admin migrate export` and `teamchat-admin migrate export-verify`: two verbs of the
  `migrate` group (C 3.3), class root, wired in `internal/teamchat/admin`, which only parses
  flags and calls the package.

## Verbs

```text
teamchat-admin migrate export --team <v1-team> [--copy-to <dir> --copy-owner <user>]
teamchat-admin migrate export-verify --team <v1-team> [--export <stamp>]
```

## Expectations

### Arguments and privilege

- **E1** `--team` is required and is a v1 team name: the grammar v1's team file accepts, which is
  also the name of the team's directory. A missing, repeated or unknown flag, or a stray argument,
  is a usage error (exit 64) naming the problem. `--copy-to` and `--copy-owner` come together or
  not at all.
- **E2** The verbs run as root (the verb class). Every file below is opened by root.

### What it reads from v1, read-only

- **E3** The team's directory is `/var/lib/agent-bus/<team>/`. It must be a directory, not a
  symlink, owned by the v1 service user (`agent-bus`, resolved by name), mode 0700. Anything
  else is refused (exit 1) naming the path and what is wrong. A missing directory is refused as
  "no v1 team <team> on this host (never made, or purged)".
- **E4** `team.json` gives `port` (1024-65535, required) and `server_name` (optional, default
  `<team>.agent-bus.internal`). It is a regular file, opened with O_NOFOLLOW, owned by root,
  group the v1 user, mode 0640. A file that does not decode, or a missing or out-of-range port,
  is refused.
- **E5** The room ids are `room_id` (main, required), `room_id.reports` and `room_id.questions`
  (each optional: an absent side-room file means that room does not exist). Each is a regular
  file, O_NOFOLLOW, owned by the v1 user, mode at most 0600, holding one v12 room id
  (`!` and 43 characters of `[A-Za-z0-9_-]`) with nothing else. A malformed id is refused.
- **E6** The admin token is `secrets/admin.token`, with the same file checks as E5. It goes only
  into the `Authorization` header of a request. It never appears in argv, a file, stdout,
  stderr or an error message.
- **E7** Every request is a GET to `http://127.0.0.1:<port>`. The client uses no proxy, follows
  no redirect, and gives each request 30 s. The tool changes nothing in v1: no POST, PUT or
  DELETE, and no v1 file is written. A test holds the method of every request.

### Reaching the homeserver

- **E8** No answer from the homeserver (connection refused, or a timeout) is refused with exit 1,
  saying that the team's homeserver is not running, that a team declared `absent` is stopped and
  blocked by v1's play, and that the team must be declared `present` again (its archive opened)
  before it can be exported. The tool never starts, unblocks or reconfigures a v1 unit.
- **E9** `GET /_matrix/client/v3/account/whoami` must answer `@admin:<server_name>`. A 401 or 403
  is refused as "the admin token was rejected". Any other user id is refused, naming it.

### Paging each room

- **E10** For each room, the tool takes a starting token from one `/sync` with a filter that
  returns no timeline (`limit: 0`). It then pages `/rooms/<id>/messages` with `dir=b` and
  `limit=100`, and no event-type filter. It stops on an empty chunk, a missing `end`, or an
  `end` equal to the token it asked from. There is no page cap. The events are then put in
  chronological order. A room whose paging fails part-way fails the whole export: nothing is
  kept.
- **E11** For each room, the tool also reads the current state (`GET /rooms/<id>/state`).

### What it writes

- **E12** The record goes in a new directory,
  `/var/lib/teamchat-admin/_v1-export/<team>/<UTC stamp>/`. The leading underscore keeps it out
  of every walk of the state root that parses team ids (as `_carry` and `_lxc` are): `v1-export`
  is itself a valid v2 team id. The stamp is
  `YYYYMMDDTHHMMSSZ` from the injected clock. The directory is root's, mode 0700, and its files
  are mode 0600. It is built under a temporary name beside the final one and renamed into place
  only when every file is written and synced. A failure leaves no directory under the final
  name, and the temporary one is removed. An existing final directory is refused, never
  overwritten. `teamchat-backup` does not hold it (DESIGN-D 5.6).

- **E13** `events-<purpose>.jsonl` (purpose `main`, `reports` or `questions`) holds one event per
  line, in chronological order. Each line is the event's JSON exactly as the server sent it,
  compacted, so the export is lossless.

- **E14** `state-<purpose>.json` holds the state array of E11 as the server sent it.

- **E15** `history.md` is a readable rendering: one section per room, in chronological order.
  Each line starts with the UTC time and the sender's user id. Then:

  - a ping (`agent_bus.ping`): `PING <verb> <ref or -> [on <branch>] -> <to, comma-separated>`,
    with `re <event>` when it answers one;
  - agent text (`agent_bus.text`): `TEXT -> <to>` and the text, indented;
  - a report (`agent_bus.report`): `REPORT` and the body, indented;
  - a question root (`agent_bus.question`): `QUESTION Q<n>` and the text;
  - any other `m.room.message`: `MESSAGE` and the body, indented, with the reply fallback
    (leading `> ` lines) removed;
  - an edit (`m.relates_to.rel_type` `m.replace`): `EDIT of <event id>` and the new body;
  - a post in a thread (`rel_type` `m.thread`): its line is marked `in thread <root id>`;
  - a redacted event: `[redacted]`;
  - a reaction: `REACTION <key> to <event id>`;
  - a state event: `STATE <type> <state key>`, except `agent_bus.status`. Those are rewritten on
    every listen renewal and would bury the conversation, so they are counted per room at the
    section's end, not listed.

  An event the renderer cannot read (an unexpected shape) gets a line `UNREADABLE <event id> <type>`. It is never dropped, and the JSONL keeps it whole. Bodies are written as text, with
  no HTML. A body's control characters, other than newline, are shown escaped.

- **E16** `digest.json` holds `v` (1), the team, its server name, the export time (UTC, ms),
  each purpose's room id (or null), and for every other file in the directory its byte count
  and sha256. For each room it also holds the event count and the count per event type. It is
  written last.

### The human copy

- **E17** With `--copy-to <dir> --copy-owner <user>`, the tool copies the finished record to
  `<dir>/<team>-<stamp>/` once the record is renamed into place. The copy's directory is mode
  0700, its files 0600, all owned by `<user>`, which must be an existing account. `<dir>` must
  be an existing directory owned by that user. The tool verifies the copy against
  `digest.json` before it reports success. A failed copy is refused with exit 1, and the record
  stays.

### Verification

- **E18** `export-verify --team <team>` verifies the newest record for that team, or, with
  `--export <stamp>`, the one named. It re-reads `digest.json`, re-hashes every file it lists,
  and refuses a file that is missing, changed, or present but not listed. Exit 0 means every
  file matches; exit 1 lists each mismatch. It reads only the record and never touches v1.
  Plan 00209 DESIGN-D 7.2 offers a v1 purge only for a team whose export verifies.

### Output and exit codes

- **E19** On success stdout carries the record's path, then one line per room: purpose, room id,
  event count. Progress and diagnostics go to stderr.
- **E20** Exit 0 means success; 1 a refusal or failure, with the reason; 64 a usage error.
- **E21** Two exports of the same team (now, and again at retirement) give two records. Each
  verifies on its own.

## Departures from the earlier designs

- Plan 00178 planned `agent-bus export`, a v1 Python verb. Tooling is now Go, and v1 is being
  retired, so the export is a v2 verb reading v1 read-only, in the one package allowed to name
  v1.
- DESIGN-D 5.6 put the export in `/var/lib/teamchat-admin/<team>/migration/v1-export/`, one
  per team. Here it is keyed by the v1 team name and stamped, because a team is exported now
  and again at retirement (E21). A v1 team need not have a v2 team of the same name.
- The human copy (E17) is Plan 00178's home-directory archive. The owner chooses whether it is
  made by default; the build supports both.
- Exporting a stopped (`absent`) team: v1 offers no reader for a stopped server, and the tool
  starts nothing (E8). The team is declared `present` again through v1's play (DESIGN-D 5.6
  `archive open`), exported, verified, and declared `absent` again.
