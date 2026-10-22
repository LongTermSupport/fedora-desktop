# Research digest: pingbus (agent-bus member client, Python v1) for the Go v2 rebuild

Plan 00209. Read-only study. Sources: `helpers/pingbus/*.py`, `docs/agent-bus-protocol.md`
(abbrev. **P§n**), `files/opt/claude-yolo/optional/agent-bus/` (**kit**), Plan 00194
(`DESIGN.md`, `SPEC-inject.md` **E1-E26**, JOURNAL). Citations are `file:line`; paths under
`helpers/pingbus/` are given by basename. "(inference)" marks anything not read directly.

---

## 0. Shape in one paragraph

pingbus is a stdlib-only Python 3.11+ CLI shipped as a reproducible zipapp (`bundle.py:1-19`,
`config.py:33`). Every invocation is a fresh process. It is one Matrix account per team ("bundle"),
a closed-verb "ping" protocol plus free text on `m.room.message`, a local file-based inbox that is
treated as an untrusted cache and re-validated on every read, and a long-lived per-session
**watcher** (`watch --supervise` → `watch --child`) that long-polls every active team and writes
"delivery envelopes" into Claude Code's per-session inbox socket. Claude Code hooks (SessionStart,
UserPromptSubmit, Stop, SessionEnd, PostToolUse) are each a `pingbus hook <event>` process. Seats
(ccy) add a **seat keeper** process (`seat keep`) that is the only holder of seat flocks and
follows a host-written desired-membership file. Host-side commands (`seat retire|farewell|disconnected`,
`reach`) run on the host for lifecycle notices.

---

## 1. Commands

Global: `--team NAME` (also accepted after the subcommand, `cli.py:2706-2721`). argparse errors →
exit 64 not 2 (`cli.py:2692-2701`). Python < 3.11 → 78 (`config.py:114-119`, checked in `main`
`cli.py:2851` and `load_active` `config.py:396`). Unknown exception → 1 (never assigned,
`cli.py:72`). Every failure class maps through `failure()` (`cli.py:803-828`).

Exit codes (P§14, `cli.py:56-86`): 0 ok/printed; 3 nothing; 4 refused (validator/role/secret);
5 forge unresolved/provenance; 6 recv: only drops; 7 homeserver unreachable/404; 8 auth 401;
9 rate (local bucket, duplicate, server 429, forge rate); 10 untrusted/not joined/403;
64 usage; 75 busy (lock held); 78 config.

Output lines (P§15, `cli.py:104-114`): tab-separated, `KIND\t1\t…`, `-` for absent, every field
grammar-checked before printing, no tab/newline/control in fields (`format_line`
`cli.py:147-159`). Fields only ever appended, never reordered (P§15 l.870).
`PING team eid sender verb ref re trust branch copy` · `HUMAN team eid sender ts json-text` ·
`PEER team eid sender ts json-text copy` · `REPLY team eid command` · `ABOUT team json-desc` ·
`TIMEOUT team eid target verb ref` · `SENT team eid` · `DROPPED count reason=n,…` (stderr) ·
`HISTORY dir ts KIND fields…`. Report lines without version: `PENDING`, `TEAM`, `UNTRUSTED`,
`MEMBER`, `UNEXPECTED`, `SEAT`, `OK`, `FORGE`.

| Command                                                       | Purpose / inputs                                                                                                                                                                                                                                                   | Outputs                                                                                                                                         | Exits                                                     | Caller                                                                                                                         | Cite                                                                                      |
| ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| `version`                                                     | tool+protocol version                                                                                                                                                                                                                                              | `pingbus 0.1.0 protocol 1`                                                                                                                      | 0                                                         | human                                                                                                                          | `cli.py:373-375`, `TOOL_VERSION` `cli.py:52`                                              |
| `validate VERB [REF] [--re]`                                  | offline send-side check against cached record; without active team, grammar only (stderr note)                                                                                                                                                                     | `OK` or reason code on **stdout**                                                                                                               | 0/4                                                       | human/agent/tests                                                                                                              | `cli.py:391-407`                                                                          |
| `validate --event FILE`                                       | offline receive-side check of a JSON event (needs active team; file ≤64 KiB, regular, O_NONBLOCK)                                                                                                                                                                  | `OK`, reason, or `ignore <reason>`                                                                                                              | 0/4/78                                                    | tests/human                                                                                                                    | `cli.py:410-456`                                                                          |
| `config check`                                                | validates every active bundle + `gh auth token`                                                                                                                                                                                                                    | `OK\tteam\thandle\thuman_text=…`, `FORGE\tgh`                                                                                                   | 0/78                                                      | `agent-bus-claude` before exec (kit `agent-bus-claude:91-93`), human                                                           | `cli.py:469-476`                                                                          |
| `suggest-handle`                                              | `--repo= --host= --type=` from `HOOKS_DAEMON_HOSTNAME`, git origin, container detection                                                                                                                                                                            | one line                                                                                                                                        | 0/78                                                      | human on a new member                                                                                                          | `cli.py:604-620`, type detection `cli.py:571-601`                                         |
| `send VERB [REF] --to … [--to-coordinator] [--re] [--branch]` | the only ping emitter; P§9 send steps 1-7                                                                                                                                                                                                                          | `SENT team eid`                                                                                                                                 | 0/4/5/7/8/9/10/64/78                                      | agent                                                                                                                          | `cli.py:1005-1030`                                                                        |
| `say --to … [--to-coordinator] [--re]`                        | free text from stdin (≤4096 B +1 newline stripped)                                                                                                                                                                                                                 | `SENT`                                                                                                                                          | same                                                      | agent                                                                                                                          | `cli.py:1060-1095`                                                                        |
| `recv`                                                        | per team: sync once if lock free (else read inbox), then print+consume each pending item from its **re-fetched** copy, then due TIMEOUTs                                                                                                                           | PING/HUMAN/PEER (+REPLY after HUMAN/PEER), TIMEOUT, DROPPED                                                                                     | 0 if any printed, else first failure, else 6 (drops) or 3 | agent; hooks tell agent to run it                                                                                              | `cli.py:1241-1287`                                                                        |
| `wait [--timeout S]`                                          | hold every team's sync lock as `wait`, one long-poll thread per team, exit on first printed item or timeout; publishes `listening` status with `until`=deadline                                                                                                    | same as recv                                                                                                                                    | 0/3/75 (all busy)/first failure                           | agent in background (sessions without socket)                                                                                  | `cli.py:1399-1479`                                                                        |
| \`watch \[--supervise                                         | --child\]\`                                                                                                                                                                                                                                                        | socket mode: watcher delivering envelopes; relay mode: spooler                                                                                  | stderr only (`watch.log`)                                 | 0 socket gone/no team; 75; first permanent failure                                                                             | SessionStart/prompt/stop hooks (`--supervise`), seat keeper `_wake`; plugin in relay mode |
| `history [--limit N]`                                         | read room backwards via `/messages`, 1-500 (default 50), ≤50 pages×100                                                                                                                                                                                             | `HISTORY` lines, `DROPPED`                                                                                                                      | 0/first failure/64                                        | agent (envelope `truncated:` & held-back lines point here)                                                                     | `cli.py:2179-2205`, `syncer.py:517-598`                                                   |
| `inbox`                                                       | offline list of pending, never the text                                                                                                                                                                                                                            | \`PENDING team eid sender ts verb                                                                                                               | human                                                     | peer\`                                                                                                                         | 0/first failure                                                                           |
| `status`                                                      | offline per-team report + `SEAT` lines                                                                                                                                                                                                                             | `TEAM … trust= wake= pending= humans= peers= pings= overdue= dropped= unexpected= relay=`, `ABOUT`, `UNTRUSTED`, `MEMBER`, `UNEXPECTED`, `SEAT` | 0/first failure/78                                        | agent/human, named by every hook failure template                                                                              | `cli.py:2306-2376`                                                                        |
| `hook EVENT`                                                  | \`session-start                                                                                                                                                                                                                                                    | prompt                                                                                                                                          | stop                                                      | session-end                                                                                                                    | post-tool                                                                                 |
| `status-line`                                                 | wraps the session's own statusLine, adds `👥 team (role) (down)` rows                                                                                                                                                                                              | text                                                                                                                                            | **always 0**                                              | Claude Code status line every 1 s                                                                                              | `cli.py:2224-2228`, `statusline.py:413-445`; fast path skips cli import `bundle.py:57-68` |
| `seat exec [--root] [--home] -- CMD…`                         | spawn `seat keep --launch`, wait ≤30 s for `READY`, write session `settings.json`+`statusline-inner`, set `PINGBUS_HOME`/`PINGBUS_TEAMS`, `execvpe` CMD                                                                                                            | stderr `✓ agent team bus: …`                                                                                                                    | 75/78/keeper's code                                       | ccy entrypoint (`files/var/local/claude-yolo/entrypoint.sh:592`), \`cc --teams                                                 | --bus\`                                                                                   |
| `seat keep [--root] [--home] [--parent PID] [--launch]`       | the seat keeper daemon                                                                                                                                                                                                                                             | `READY\n` on stdout once (launch), then stdout→/dev/null, stderr→`keeper.log`                                                                   | 0/75/78                                                   | `seat exec`; run alone = repair                                                                                                | `cli.py:2477-2498`, `seat.py:490-765`                                                     |
| `seat retire SEAT@TEAM`                                       | host: hold seat lock, wait ≤40 s for sync lock, delete `sync.json`, publish expired status                                                                                                                                                                         | stderr ✓ lines                                                                                                                                  | 0/75/78                                                   | host after hot remove                                                                                                          | `cli.py:2530-2579`                                                                        |
| `seat farewell SEAT@TEAM`                                     | host: post `<handle> left the team: removed by the owner`                                                                                                                                                                                                          | stderr                                                                                                                                          | 0/75                                                      | host after hot remove                                                                                                          | `cli.py:2583-2595`                                                                        |
| `seat disconnected --root --session --since MS`               | host after container gone: for each seat in `../sessions/<name>/applied.json`, wait ≤15 s for lock free, if `seat.lock` still says `seat <claim>` ≥ since and no `said-goodbye.json` for that claim, post `<handle> disconnected: session ended without a goodbye` | stderr, written only after all seats told                                                                                                       | first failure                                             | ccy launcher cleanup (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:71-92`, under `timeout` 60 s, SIGINT ignored) | `cli.py:2599-2688`                                                                        |
| `reach --team T [--root]`                                     | unauthenticated `GET /_matrix/client/versions`, 3 s                                                                                                                                                                                                                | stderr                                                                                                                                          | 0/7                                                       | host before hot add                                                                                                            | `cli.py:2505-2527`                                                                        |

`send` detail (`cli.py:1005-1030`): pick team (`_sending_member` `cli.py:979-1002`: `--team`, sole
team, team whose inbox/consumed holds `--re`, team where every `--to` resolves; else 64 listing
teams+descriptions) → `verify_room()` (network, every send) → resolve `--to` (`_user_id`
`cli.py:866-885`: full ID, exact handle, human localpart, reserved, unique short name `repo.seat`;
ambiguous/unknown → 64 naming members) → add coordinator copy when a worker addresses a human
(`protocol.py:565-572`) → lowercase OWNER/REPO in ref (`protocol.py:530-536`) → auto `branch` from
the checkout's upstream when the repo trusts any branch (`cli.py:514-557`, git subprocesses) →
validate → role → `admit_send` (bucket + 60 s duplicate window, persisted, charged even if later
refused, `inbox.py:580-588`) → forge check → `PUT send` → record ack-expected ping in outbox →
(relay) append `sent.jsonl` → `SENT`. `target` refusals are re-raised with a list of who can be
addressed (`cli.py:888-921`).

`say` additionally refuses text containing the member's own token as `secret`
(`cli.py:1089-1090`) beyond the regex secret shapes (`protocol.py:127-142`). Duplicate key is
`("text", sha256(text), None, to)` so the text is never stored (`cli.py:632-635,1092-1094`).

---

## 2. On-disk files

All state files are 0600 in 0700 dirs, opened `O_NOFOLLOW`/`O_NONBLOCK`, size-capped, and checked
to be regular files; writes are temp-file + fsync + rename + dir fsync (`inbox.py:179-235`).
Unknown keys/wrong `v` → refused (StateError → 78) rather than silently discarded, except the Stop
guard (replaced) and `forge-cache.json` (78 with "remove it").

### 2a. Bundle `PINGBUS_HOME/<team>/` (written by host `agent-bus add-member`; read-only to pingbus)

- `member.json` (≤64 KiB, duplicate keys refused, unknown keys refused): `protocol`(=1), `team`
  (=dir name), `user_id`, `server_name`, `base_url` (https, or http only to an IP in
  `plain_http_hosts`; no path/query/creds), `plain_http_hosts` (canonical IP literals),
  `token_file` (must be `"token"`), `admin` (must be `@admin:<sn>`), `room`, optional
  `human_text` (bool), `limits` (P§10 keys only, bounds enforced) — `config.py:44-55,490-546`.
- `token`: one printable ASCII line, no newline, owner uid, mode ≤0600, re-read and re-checked on
  every use, never held on `Member` (`config.py:327-375`). Two bundles sharing user_id or token →
  78 (`config.py:406-416`).
- A team entry may be a symlink only to a real 0700 dir owned by the user (`config.py:305-324`).
- `host`-type member refused if the running user has an Element profile (flatpak dir or
  `~/.config/Element*`), unless reached via a session-home link and the user accepted it once in
  `~/.config/pingbus/element-accepted` (`config.py:249-296,423-449`); the kit launcher duplicates
  this check in bash (`agent-bus-claude:48-69`).

### 2b. Team state `PINGBUS_HOME/<team>/state/` (`inbox.py:11-30`, P§12)

| File                                      | Format                                                                                                                                                                                        | Writer                                                                            | Readers                                                                                                    |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `inbox/<eid>.json`, `consumed/<eid>.json` | projection `type,event_id,sender,origin_server_ts,content`, ASCII JSON ≤384 KiB                                                                                                               | syncer `commit_batch` (`inbox.py:327-353`); consume = rename (`inbox.py:424-434`) | recv/wait/watch/inbox/status/hooks — re-validated on every read (`inbox.py:377-418`); known IDs = seen set |
| `sync.json`                               | `{"v":1,"next_batch":tok}` saved only after inbox writes durable                                                                                                                              | syncer                                                                            | syncer; `seat retire` deletes                                                                              |
| `outbox.json`                             | `{"v":1,"pings":[{event_id,verb,ref,to,sent_ms,answered,reported}],"gate":{"bucket":{credit,at_ms},"recent":{key:ms}}}`; RMW under `flock` on the state **directory** fd (`inbox.py:562-578`) | send/say/syncer (answers)/deliverers (reported)                                   | status (lock-free read)                                                                                    |
| `team.json`                               | last verified `agent_bus.team` content                                                                                                                                                        | syncer `verify_room`                                                              | everything offline (`load_cached_record` re-parses and requires own role, `inbox.py:253-276`)              |
| `untrusted.json`                          | `{"v":1,"reason":…}` (replaces team.json)                                                                                                                                                     | syncer `_lose_trust`                                                              | status                                                                                                     |
| `about.json`                              | `{"v":1,"description":…}` from admin's room topic                                                                                                                                             | syncer                                                                            | hooks, status, team-choice message                                                                         |
| `room.json`                               | \`{"v":1,"members":{uid:join                                                                                                                                                                  | invite},"statuses":{uid:until_ms}}\`                                              | syncer                                                                                                     |
| `forge-cache.json`                        | `{"v":1,"entries":{key:exp_s},"repos":{key:{exp,owner,private,fork,default_branch}}}` ≤16 MiB, positive results only, key prefixed with forge API (`forge.py:194-287`)                        | forge                                                                             | forge (each process)                                                                                       |
| `dropped.log`                             | append, \`team\\teid                                                                                                                                                                          | -\\tsender                                                                        | -\\treason\\tms` (`inbox.py:514-541\`)                                                                     |
| `lock`                                    | flock; content = holder kind \`watch                                                                                                                                                          | wait                                                                              | recv\`                                                                                                     |

### 2c. Under `PINGBUS_HOME` (per user on hosts; per container in ccy)

- `stop-guard.json` `{"v":1,"pending":sha256|null,"no_waker_ms","failure_ms"}` (`hooks.py:630-693`).
- `goodbye.json` `{"v":1,"session":<session_id>}` written **before** the goodbye attempt
  (`hooks.py:1059-1078`).
- `watch.log` (watcher + hooks stderr, append, `hooks.py:612-624`).
- `inject/<key>.{transcript,stalled,missing,watch,spawned}`, key = first 16 hex of
  sha256(socket path) (`inject.py:5-13,44-141`). `.watch` is the per-session watch lock, never
  deleted.
- `relay/lock` (spool lock, used in **both** modes, `relay.py:126-140`), `relay/spool/<team>/<key>.line`,
  `relay/relayed/<team>/<key>`, `relay/stalled`, `relay/sent-offset` `{"v":1,"offset":n}`.
- `sent.jsonl` (relay mode only) append-only, fsynced (`sentlog.py:86-96`).
- `down/<team>` mtime-refreshed mark while homeserver unreachable, honoured for 300 s
  (`statusline.py:84-88,276-299`).
- `told.json` / `told.lock` (membership notices, `membership.py:60-64,440-457`),
  `told-roles.json` (sessions outside membership, `membership.py:67-72`).
- Session home only (ccy/cc, `/tmp/pingbus-home` 0700, `seat.py:71`): `<team>` symlinks to seat
  dirs, `keeper.lock` (kind `keeper`), `keeper.log`, `applied.json`, `active` flag (present iff a
  seat held), `settings.json`, `statusline-inner`, `socket.json` `{"v":1,"socket","token"}`
  (`notify.py:50-90`) — **socket token at rest**, 0600.

### 2d. Elsewhere

- `$PINGBUS_SESSIONS_DIR` or `/tmp/pingbus-sessions-<uid>/<session_id>`: comma list of teams for
  the status line; dir must be 0700 owned (`hooks.py:242-262`, `statusline.py:108-134`).
- Seats: `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/` (bundle + `state/` + `seat.lock`
  content `seat <claim_ms>` + `said-goodbye.json` `{"v":1,"claimed_ms":N}`) (`seat.py:73-79,262-272`).
  Explicit release empties `seat.lock`; a dead holder leaves its claim (`inbox.py:764-775`).
- `<checkout>/.claude/ccy/pingbus/sessions/<session>/applied.json` (keeper's report for host) and
  `conversation.json` `{v,session,session_id,source,written_ms}` (`hooks.py:213-239`, `seat.py:693-710`).
- Desired membership `$PINGBUS_MEMBERSHIP/membership.json` (host-written, RO in container):
  `{v,session,kind:ccy|cc,checkout,generation≥1,written_ms,seats[{team,seat}],farewell[{team,seat,reason}]}`
  (`membership.py:213-241`). Applied: `{v,generation,seats[{team,seat,handle,state}],refused[{team,seat,why,holder}],told?,inbound?}`
  (`membership.py:258-284`). Active teams = held in applied ∩ named in desired (H4/H8,
  `membership.py:484-490`) — applied alone never grants a team.

---

## 3. Matrix interaction

Client: `matrix.py`. urllib, empty proxy map, **no redirects** (3xx → error), token only as
`add_unredirected_header` Bearer (`matrix.py:95-105,193-194`); request timeout 15 s, responses
≤32 MiB, must be a JSON object; errors carry method+path+status+errcode only (no query, no server
`error` text) (`matrix.py:46-93,197-232`). Status map: 401→8, 403→10, 404→NotFound(7), 429→retry,
5xx/conn errors→`transient` (`matrix.py:63,156`).

Endpoints used (`matrix.py:236-321`): `GET /_matrix/client/versions` (no auth), `GET account/whoami`
(no pingbus caller; agent_bus uses it, inference from grep), `GET sync`, `POST rooms/{r}/join`,
`POST rooms/{r}/leave` (reject invite), `PUT rooms/{r}/send/{type}/{txn}` (txn `pb`+32 hex; retried
3× on Unreachable with **same txn**, 1 s apart, `matrix.py:266-283`), `PUT rooms/{r}/state/{type}/{key}`,
`GET rooms/{r}/state/{type}/{key}[?format=event]`, `POST rooms/{r}/receipt/m.read/{eid}`,
`GET rooms/{r}/event/{eid}`, `GET rooms/{r}/messages`.

429 (P§10, `limits.py:47-53,115-129`): wait = `Retry-After`, then `retry_after_ms`, then 5 s; ≤3
tries; wait >60 s → fail at once (exit 9). Forge same plus `x-ratelimit-reset`, and 403 with
`x-ratelimit-remaining: 0` counts (`forge.py:580-592`).

Sync loop (`syncer.py:327-480`):

- Filter inline JSON per request: only the team room; presence/account_data/ephemeral empty;
  state types `create, power_levels, member, agent_bus.team, agent_bus.status, topic`; timeline
  same + `m.room.message`, limit 50; `lazy_load_members` true except first sync / after join
  (`syncer.py:42-53,119-134`).
- **First sync** (no token): `timeline.limit 0`, timeout 0, record `next_batch` only — history is
  never processed (`syncer.py:354-365`).
- Invites: accept only `room == member.room` with inviter and stripped create sender == admin;
  others `leave` and logged by room ID (`syncer.py:137-160,367-379`). After join: verify, then a
  `limit 0` sync to skip history (`syncer.py:381-391`).
- Re-verify the room on start, after join, on `limited`, or any trust-type event in the batch
  (`syncer.py:345-346`). Verify = `GET create?format=event` (sender admin, `event_id == "$"+room[1:]`,
  room_version "12", no `additional_creators`), `GET agent_bus.team?format=event` (sender admin,
  state_key ""), exact power levels (canonical JSON compare incl. types), own role present
  (`syncer.py:295-313`, `protocol.py:999-1020`). Failure → `untrusted.json`, team.json removed,
  statuses cleared, exit 10; 404/403 count as untrusted (`syncer.py:253-323`).
- Limited timeline gap filled forward with `/messages` (`m.room.message` only, 100/page, ≤20
  pages, else Unreachable) (`syncer.py:393-411`).
- Topic change → `about.json` refresh; description accepted only from admin, grammar 1-120 chars
  no Cc/Cf, not the fixed default sentence (`syncer.py:273-293`, `protocol.py:420-424`).
- Receive: per event `validate_event` → local checks (stale, flood, forge) → drop log / accept;
  `ack`/`nack` answers recorded in outbox **even when stale** if sent before the deadline
  (`syncer.py:448-513`); then `commit_batch` (inbox writes durable, then token).
- Long-poll 30 s (`limits.py:48`), socket timeout = poll + 15 s (`matrix.py:244-257`).

Status (P§8): `agent_bus.status` state key = own user ID, `{"v":1,"state":"listening","until":ms}`
≤256 B (`syncer.py:602-611`). `watch` publishes `until = now+600 s` every 300 s
(`cli.py:1485-1486,1547-1550`); `wait` publishes `until = deadline` (`cli.py:1439-1441`);
`seat retire` publishes `until = now` (expired). Never cleared; goes stale. Read only from role
holders under their own key (`protocol.py:978-996`).

Read receipts: public `m.read` for each HUMAN item, only after the delivery's nonce is found in
the transcript (`cli.py:1809-1829`).

Protocol (P§1-§11, `protocol.py`): version 1; prefix `agent_bus` (permanent); custom state
types `agent_bus.team` (state_key "", admin only), `agent_bus.status`; content keys
`agent_bus.ping` and `agent_bus.text` inside `m.room.message` `m.notice`; human messages are
`m.text`. Ping content keys exactly `msgtype, body, m.mentions, agent_bus.ping`; body must equal
`render()`; mentions must equal sorted `to`; ≤4096 B compact JSON; v≠1 → `version` before any other
check (`protocol.py:597-736`). Verbs table (`protocol.py:211-221`). Refs grammar
`path:|commit:|pr:|issue:` (`protocol.py:88-103,238-243`). Agent text: `to` 1-16, text ≤4096 B,
optional `m.relates_to` = exactly `{"m.in_reply_to":{"event_id"}}` (`protocol.py:1039-1129`).
Human text ≤16 KiB, addressed via `m.mentions.user_ids` or `room:true`, reply fallback stripped
(`protocol.py:751-799`). Drop reasons closed set (`protocol.py:163-166`); ignores (`type`, `seen`,
`self`, `not-addressed`, `empty`, `agent-text`) are never logged.

Forge (P§6, `forge.py`): credential = `gh auth token --hostname github.com` subprocess, 15 s
timeout, only for `forge_api == https://api.github.com` (`forge.py:112-145`). Same-origin
redirects only; token never forwarded (`forge.py:162-177`). Checks: listed+branches → compare
against listed branches (or the ping's branch, which must be listed); org/listed-no-branches →
`GET /repos/O/R` (owner == org, not fork) + named branch exists + compare; external → public,
not fork, commit/contents/PR-head compare; pr → head repo == ref repo, author association
OWNER/MEMBER/COLLABORATOR; issue → no `pull_request` key (`forge.py:323-520,595-620`). Repo metadata
TTL 3600 s even in a long-running watcher ("a repository moved out of the org must stop being
trusted", `forge.py:388-389`).

---

## 4. Concurrency, processes, delivery

### Locks (all `flock`, never PIDs: "/workspace is shared across container namespaces, where a PID

means nothing", P§12 l.636-639, `inbox.py:740-891`)

| Lock                                                                                                | Path                      | Kinds / content         | Held by                                                                                                                                          |
| --------------------------------------------------------------------------------------------------- | ------------------------- | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| sync lock                                                                                           | `state/lock`              | `watch`, `wait`, `recv` | one syncer per team; `recv` only for one sync; `wait` waits ≤5 s for a `recv` holder (`cli.py:1290-1300`)                                        |
| seat lock                                                                                           | `seats/<t>/<s>/seat.lock` | `seat <claim_ms>`       | only the keeper (close-on-exec; detached session processes can't keep a seat, `seat.py:12-22`)                                                   |
| keeper lock                                                                                         | `<home>/keeper.lock`      | `keeper`                | keeper; hooks probe it → "keeper stopped" line (`hooks.py:752-764`)                                                                              |
| watch lock                                                                                          | `inject/<key>.watch`      | `watch`                 | supervisor, inherited by child (`cli.py:1879-1900`)                                                                                              |
| spool lock                                                                                          | `relay/lock`              | —                       | watcher check+write+consume, recv/wait print+consume; held across socket write (≤~15 s), never across homeserver calls (`relay.py:126-140`, E12) |
| outbox                                                                                              | flock on `state/` dir fd  | —                       | every outbox RMW; lock order spool → outbox (journal 26-10-14 17:30)                                                                             |
| told lock                                                                                           | `<home>/told.lock`        | —                       | non-blocking; second teller skips (`membership.py:440-457`)                                                                                      |
| Lock acquire tries 3× 50 ms so a probe's momentary shared lock doesn't look like a holder           |                           |                         |                                                                                                                                                  |
| (`inbox.py:88-91,815-824`). `Lock.release` truncates then explicit `LOCK_UN` (frees all dup'd fds). |                           |                         |                                                                                                                                                  |

### Processes

- **Hooks**: one short-lived Python process per hook event; all offline except SessionEnd goodbye.
- **Supervisor** `watch --supervise` (detached `start_new_session`, stderr → `watch.log`,
  `hooks.py:612-624`): runs `watch --child` (via `pingbus` found on `PATH`, `hooks.py:604-609`),
  restarts with 1→60 s backoff (reset if child ran ≥60 s); stops on socket gone, child exit 0,
  or exit 8/10/64/78 (`cli.py:1873-1919`).
- **Watcher child**: main thread ticks every 1 s; one daemon thread per team doing
  first-sync/long-poll with transient retry 1→30 s from stored token (E25, `cli.py:1520-1579`);
  permanent failure (401/403/untrusted/config/state) drops only that team (`cli.py:1489-1495,2048-2062`).
  Under `PINGBUS_MEMBERSHIP` follows active teams each tick; removed team's thread stops after the
  in-flight poll and its fetched items are **consumed undelivered** (H23a, `cli.py:1327-1335,2072-2143`).
  Delivery-pass exceptions are contained and retried next tick (`cli.py:2039-2046`).
- **Seat keeper**: polls desired file stamp (inode,mtime,size) and `told.json` stamp every 0.5 s,
  applies diffs, dies with parent via `PR_SET_PDEATHSIG` (`cli.py:735-744`, `seat.py:536-556,755-765`);
  starts the watcher on a later join using `socket.json` (`seat.py:672-691`).
- **Wait threads**: as watcher but no retry; any team's failure ends `wait` (`cli.py:1338-1361`).

### Socket delivery (default; SPEC-inject E1-E26)

Wire (`notify.py:1-137`, probe U01): connect `$CLAUDE_CODE_MESSAGING_SOCKET`, send two NDJSON
lines `{"type":"auth","token":…}` and `{"type":"user","message":{"role":"user","content":text}}`,
half-close, wait ≤5 s for close. ENOENT/ECONNREFUSED/ENOTSOCK → `SocketGone` → watcher exits 0.
Socket drops a body identical to the previous within 30 s and rate-limits at 0.5/s → ≥2 s pacing
and a per-delivery nonce (E13). Written in full = sent, even if close times out or resets (E11).
Pass (`cli.py:1672-1857`): candidates = pending items across teams by `origin_server_ts`, then due
TIMEOUTs → re-fetch each item via `GET event` **outside** the lock → flood budget per agent sender
(10 per team per 10 min; humans/TIMEOUTs exempt; excess consumed + counted on a `held back:` line;
held-only deliveries ≤1/min) → fit ≤8 entries / 12 KiB wire → under spool lock re-check still in
inbox / still due, render, send, consume, spend budget. Write error → nothing consumed, retry in
10 s; 120 s of waiting with no send → `<key>.stalled` (`relay.py:217-234`). After send, transcript
scanned for `agent-bus delivery <nonce>:`; not found after 180 s quiet or 3600 s → `<key>.missing`
(`inject.py:173-228`).
Envelope (`envelope.py`): first line `agent-bus delivery <nonce>: N message(s) (H from humans, A from agents, P pings, T timeouts): who…`; entries between `<<agent-bus NONCE i/n>>` markers;
fixed `kind:`/`authority:` lines per kind; `reply:` command; body one JSON string ≤2048 B rendered,
allow-list renderer (letters/numbers any script shown; P/S only from listed blocks; quote/angle
look-alikes, Cf, separators, variation selectors escaped; ZWJ only inside emoji sequences)
(`envelope.py:90-337`), `truncated:` pointer to `history`. Nonce drawn after bodies; not secret.
SessionStart adds the fixed `inject_frame` explanation of the socket's "Another Claude session"
label (`hooks.py:160-172`).

### Relay delivery (`PINGBUS_DELIVERY=relay`; Plan 00194 Task 2.1 target, waits on hooks-daemon API 1.1 / #119)

Spool line = §15 line + `cut` field (pre-escape 2048 B cap, `relay.py:42-72`); plugin writes
`relayed/` marker; watcher consumes once (`cli.py:1207-1230`); `sent.jsonl` + PostToolUse/Stop
`SENT` echo as `systemMessage` (`sentlog.py`). Any other `PINGBUS_DELIVERY` value → 78
(`relay.py:53-61`).

### Hooks behaviour (`hooks.py`)

- All guarded in `hooks.json` by `[ -z "$PINGBUS_MEMBERSHIP" ] || [ -e "$PINGBUS_HOME/active" ] || …`
  so a seatless session spawns nothing (SessionStart runs `hook conversation` instead) (kit
  `hooks.json:8-49`). PostToolUse only execs Python when relay.
- Output is fixed templates filled only with counts and grammar-checked names (`hooks.py:97-178,269-276`).
- `session-start`: record conversation id (membership), survey, record socket+told baseline,
  keeper-stopped line, write session teams record, start supervisor if socket and some team has no
  waker else `no_socket`, socket frame line, record transcript path, seat lines (own/siblings/history
  pointer), team descriptions, role lines (coordinator/worker + coordinator handles), pending
  counts unless delivery active, worst failure class (`hooks.py:779-798`).
- `prompt`: record transcript, revive watcher (lock free and not started within 30 s grace; died
  again within 300 s → `watcher_failed` instead of loop), membership/role notices not yet told,
  pending + failure (`hooks.py:885-913`).
- `stop`: never blocks if `stop_hook_active` true; blocks once per distinct pending set (digest),
  once/600 s per failure class, once/600 s for no waker (socketless only) or a watcher that keeps
  dying; unwritable guard file → never block (would block every turn) (`hooks.py:961-1016`).
- `session-end`: remove teams record; skip on `clear`/`resume`; forget inject marks; goodbye once
  per session (record-before-attempt), parallel threads, 4 s total, SIGINT/SIGHUP ignored, to
  humans + coordinators only (≤16), text `<handle> left the team: session ended (exit|logout|other)`;
  records `said-goodbye.json` per seat (`hooks.py:1018-1116`, `cli.py:1098-1117`).
- Failure classes worst-first `internal, config, state, untrusted, relay, inject, missing`
  (`hooks.py:180`); any exception → `internal` template, hook still answers (`hooks.py:1150-1159`).

### Presence / goodbye / disconnected semantics

Presence = seat lock alone; nothing runs at release (P§12 l.709-713). Signals posted: goodbye
(SessionEnd), farewell (host remove), disconnected (launcher after container gone, only for a claim
still in `seat.lock` with no `said-goodbye.json`, then empties it — so each claim is told once),
plus the stale-able `listening` status. Not covered: host reboot, launcher SIGKILL, hook killed
between post and record (both sent) (journal 26-10-19 09:16). Scheduled lifecycle restart sends a
goodbye (no reliable reason; upstream hooks-daemon #127 pending).

### Membership notice (Plan 00203 H12/H13)

Watcher sends a `MEMBERSHIP` envelope (joined/left/role changed/teams now); if write fails the next
prompt hook adds the same lines; `told.json` ensures once per change (`hooks.py:364-421`,
`cli.py:2146-2173`, `envelope.py:434-449`). Non-membership sessions get role-change notices via
`told-roles.json` (`hooks.py:433-475`).

---

## 5. Hard-won behaviours (keep these)

01. **SessionEnd hooks are bounded to 1.5 s in total**; a plugin hook's `timeout` does not count;
    only `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` (set to 8000 in kit `settings.json:8-10` and
    session settings `seat.py:80-84,443-469`) lifts it. Before this **no goodbye was ever sent**
    (journal 26-10-19 09:16). Python startup alone was ~1 s.
02. Hooks run in their own session; Ctrl-C mashing reaches Claude Code, not the hook; still ignore
    SIGINT/SIGHUP while posting (`hooks.py:699-719`).
03. `podman stop`/closed terminal kills the supervisor without SessionEnd → launcher-side
    `seat disconnected` is the only fallback; it must write nothing to stderr until every seat is
    told, and tolerate EIO/EBADF/EPIPE on a dead tty (`cli.py:2640-2653`, DisconnectedDeadStderrTest).
04. Goodbye recorded **before** the attempt so a double-fired hook or a retry sends nothing more
    (`hooks.py:1059-1078`). Goodbye to humans+coordinators only: "each agent addressed costs a model
    turn" (`cli.py:1098-1103`; GoodbyePostTest was a fix).
05. Inbox is a cache any checkout code could write: re-validate on every read, and print only from a
    homeserver re-fetch (`cli.py:11-15,1120-1133`, P§9 "Reading the inbox").
06. Delivered-once: re-fetch outside lock, re-check `in_inbox` under spool lock (RecvRelayRaceTest:
    the deterministic race where recv runs inside the watcher's re-fetch).
07. Socket write success = full `sendall`; close-path errors still count as sent, otherwise a
    second copy goes out with a new nonce (E11, journal 26-10-15 18:25 M2).
08. Re-fetch failures must still count waiting items so the stall mark fires (`cli.py:1840-1850`, M1).
09. A starting watcher clears its old stalled/missing marks; SessionEnd removes them so a reused
    socket path doesn't inherit `missing` (`cli.py:1993-1995`, `hooks.py:1048-1057`).
10. Watcher must survive homeserver restarts, including at its very first sync (E25; previously
    any error dropped the team; journal 26-10-15 23:55). Status-line `(down)` mark (H26).
11. Supervisor + per-session watch lock + hook revive with 30 s grace / 300 s no-loop (E26).
12. Stop guard: block once per pending set; never block when it can't remember (`hooks.py:630-635`);
    respect `stop_hook_active`; window kept on disk so it can't loop against other Stop handlers.
13. Hooks must not announce pending counts or block Stop while the watcher delivers — otherwise the
    agent `recv`s and sees the item twice (E17, `hooks.py:517-524,929-940`).
14. Separate TIMEOUT per silent target; key `<eid>~<localpart>` (SilentTargetsTest).
15. Ack answers count by `origin_server_ts` vs deadline, even if read late / dropped as stale
    (`syncer.py:482-501`, `inbox.py:678-690`).
16. Send gate charged before network so a failed send still spent its token (P§10, `inbox.py:580-588`).
17. Matrix send retries reuse the txn ID (`matrix.py:266-283`).
18. First sync and post-join sync skip history (`limit 0`).
19. Status-line must always exit 0 (non-zero blanks the line), kill the inner command's process
    group on timeout or on cancel signal including the Popen-start race (`statusline.py:187-224,429-436`);
    team rows go on their own lines, wrapped at `COLUMNS` (`statusline.py:13-18`); never call itself
    (`INNER_VAR`, `statusline.py:58-59`).
20. Session settings decide `crossSessionInbound` once at launch (Claude Code reads `--settings`
    once); without `accept` a bypass-mode session holds socket messages instead of starting a turn
    (kit `agent-bus-claude:18-20`, `seat.py:443-469`).
21. Handles never carry the machine hostname (public forge text) — `suggest-handle` refuses
    without `HOOKS_DAEMON_HOSTNAME` (`cli.py:604-611`).
22. Status `listening` for a removed role holder is dropped on re-verify (`syncer.py:266-270`).
23. Forge: GitHub serves fork commits through the parent API, so "API returns the commit" never
    proves provenance; branch must be the repo's own (`forge.py:340-359`, P§6). 422 compare =
    found but unreachable → `provenance` not `not-found`.
24. Envelope renderer allow-list replaced a deny-list after review found 11 missed look-alikes (E2).
25. Removed team: items fetched by the in-flight poll are consumed undelivered so nothing posted
    after a remove reaches the session on re-add; `seat retire` deletes the sync token so a re-add
    starts as a first sync (H23).
26. Several active teams: `send`/`say` infer team from `--re` or unique `--to` fit, else refuse with
    a list — "guessing would put a message in the wrong room" (`cli.py:979-1002`, InferTeamTest).
27. `REPLY`/`reply:` commands single-quote the event ID (a shell expands `$`), and use the short name
    when unique (ReplyLineTest, ShortNameTest).
28. Ignore (not drop) agent text not addressed to me: no log, no count, no exit-code effect (P§7).
29. Keeper is the only seat-lock holder (close-on-exec) so a detached session process can't pin a
    seat (audit F5, `seat.py:18-20`); `PR_SET_PDEATHSIG` for cc sessions.

---

## 6. Things a rebuild should question or not carry

- **Two delivery modes.** Relay mode (spool, `relayed/` markers, `sent.jsonl`, `sent-offset`,
  PostToolUse echo, relay stall, `relay=` field) exists for a plugin API that has not shipped
  (journal 26-10-15 09:28). The spool lock is reused as socket-mode's delivery lock, which is the
  only reason `relay/` exists in socket mode (`cli.py:1233-1238`). Decide one path; name the lock
  for what it is. (Inference: relay mode is not used in production today.)
- **Two body caps** (`relay.cap_text` pre-escape bytes vs `envelope.cap_body` post-render bytes;
  E22 admits the misalignment).
- **Two goodbye records**: `goodbye.json` (session home, by session_id) and `said-goodbye.json`
  (seat dir, by claim). One per-claim record suffices (inference).
- **Duplicated constants/logic for startup cost**: `statusline.py` re-implements membership
  active-team logic and team-name regex "copies a test keeps equal" (`statusline.py:36-39,259-273`);
  bash launcher duplicates the Element check (`agent-bus-claude:48-69`). A Go binary with ms
  startup removes the reason.
- `hooks.py` defines `SESSION_ID_RE` twice (64 at l.87, 128 at l.202; the second wins) — accidental.
- `syncer.py:47` has a comment glued onto `TRUST_TYPES` line (`)#: The filter probe…`) — cosmetic accident.
- `TOOL_VERSION = "0.1.0"` never moves while plugin is 1.4.0 (`cli.py:52`, `plugin.json:3`).
- No caller in `helpers/`: `Spool.ids`, `membership.entries`, `Desired.reason_for`,
  `Desired.identity` (grep; tests may use them).
- `send`/`say` call `verify_room()` (3 GETs) on every send; hooks/offline commands use the cached
  record. Consider a daemon owning room state and answering local clients.
- Every CLI process re-runs `gh auth token` (subprocess, ≤15 s) when it needs the forge
  (`syncer.py:163-174`); cache file shared across processes as a workaround.
- Outbox RMW locks the whole state dir; `status` reads it lock-free relying on atomic rename.
- Inbox as one file per event + full re-validation of every file on every hook = O(pending) JSON
  parses per prompt/stop; fine at small N but a design consequence of "no daemon".
- `wait` vs `watch` vs `recv` = three receive drivers with overlapping lock rules (`recv` holder
  waited 5 s, etc.). With a single session daemon, `wait` survives only for socketless sessions.
- Stop "no waker → run `pingbus wait` in background" remains for socketless sessions only.
- Protocol carries back-compat shims: `orchestrator` → `coordinator` (`protocol.py:53`), `HANDLE_SEP`
  probe note (`protocol.py:62`). v2 may drop if the wire version changes (P§1 rules).
- `history` cannot see more than 5000 messages and re-runs the forge check for every past ping
  (network-heavy) (`syncer.py:56-61,553-598`).

---

## 7. Startup cost / frequency

- Measured: `pingbus --help` ~1 s in a ccy container (journal 26-10-19 09:16) — every hook, every
  CLI call pays Python start + importing the whole package (cli imports all 17 modules,
  `cli.py:45-49`).
- `status-line`: every 1 s (`refreshInterval: 1`, kit `settings.json:3-7`) → zipapp `__main__`
  fast path imports only `statusline` (`bundle.py:54-68`); `read_roles` lazily imports
  config/inbox only when teams exist (`statusline.py:302-308`); plus it spawns the inner status
  line via `/bin/sh` with 10 s timeout.
- Hooks: SessionStart once per start/clear/resume/compact; UserPromptSubmit every user prompt;
  Stop every turn end; PostToolUse every Bash call (shell guard avoids Python unless relay);
  SessionEnd once. Each is a Python process doing offline surveys (bundle validation incl. token
  file re-read, `team.json` parse, inbox re-validation, lock probes).
- Watcher: one long-lived process + supervisor per session; 1 s main tick; 30 s long-poll per team;
  status republish every 300 s; delivery ≥2 s apart; transcript scan up to 16 MiB per poll
  (`inject.py:40`).
- Keeper: 0.5 s poll of two stat()s.
- `send`/`say`: ~3 GETs verify + forge (gh subprocess + 1-4 GitHub calls unless cached) + PUT.
- `seat exec`: waits ≤30 s for keeper READY; `gh` check when seats desired.
