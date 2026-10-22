# Agent-bus membership: facts for a non-Claude-Code member

Facts only, no design. v1 = the running Python bus (`pingbus`, `agent-bus`, Plan 00161).
v2 = `teamchat` (Plan 00209), read from branch `feat/bus-v2-n3` (cited `n3:<path>:<line>`;
paths under `CLAUDE/Plan/00209-agent-bus-v2-in-go/` shortened to the file name). Other cites
are this worktree (off F44). Placeholders: `<team>`, `<seat>`, `<repo>`, `<site>`.

## 1. Joining: seat, identity, authority

**Identity is a Matrix account per team, minted by the team admin on the homeserver host.**

- The team runs one Tuwunel homeserver; its secrets belong to the `agent-bus` system user,
  and admin commands run through `sudo agent-bus` (`files/usr/local/bin/agent-bus:2-10,55-58`).
- `agent-bus add-member` creates the handle's account, mints its token, invites it, writes
  its role into the team record, and returns a bundle tar (`helpers/agent_bus/admin.py:992-1045`).
  `park-member` revokes the token and keeps the account (`admin.py:1048-1058`).
- Bundle = `PINGBUS_HOME/<team>/member.json` + `token` (mode 0600 or stricter, else 78)
  (`docs/agent-bus-protocol.md:725-727`, the keys at `:896-910`; mode check
  `helpers/pingbus/config.py:291-295`). The token goes only in `Authorization: Bearer`
  (`protocol.md:920-921`, `helpers/pingbus/matrix.py:194`).
- Handle = `<repo>.<seat>+<host>.<type>`, `type` in `podman|lxc|docker|vm|host`
  (`protocol.md:104`, built at `:116-137`). The type enum has no "agent kind" in it.
- Who grants: a human with sudo on the homeserver host. A non-ccy member gets a bundle by
  `sudo agent-bus add-member ... --out=DIR` and copies it in by hand or by IaC
  (Plan 00161 `DESIGN.md:539-562`, table `:566-573`).
- ccy: `ccy --teams <seat>@<team>`; the launcher runs `agent-bus seat take`, which issues
  one `sudo agent-bus add-member` for a new seat (`files/var/local/claude-yolo/lib/agent-bus-seats.bash:7-8,85-87`;
  `files/opt/claude-yolo/optional/agent-bus/README.podman:14-37`). The bundle lives at
  `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/` (`protocol.md:798-805`).
  Desired seats come from `PINGBUS_MEMBERSHIP` (`/run/ccy/membership/membership.json`,
  read-only), and `pingbus seat exec` / `seat keep` hold the seat locks and build
  `/tmp/pingbus-home` (`protocol.md:805-816,847-858`). The entrypoint refuses any
  `PINGBUS_*` value set from the checkout (`files/var/local/claude-yolo/entrypoint.sh:562-599`).
- Non-ccy (`host`/`lxc`/`docker`/`vm`): `agent-bus-claude` reads `~/.config/pingbus/env`
  (`PINGBUS_TEAMS`, optionally `PINGBUS_HOME`), refuses a user that has an Element profile,
  runs `pingbus config check`, then execs `claude --plugin-dir <kit>/plugin/pingbus --settings <kit>/settings.json` (`files/opt/claude-yolo/optional/agent-bus/agent-bus-claude:8-24,74-96`).
- One live holder per seat or account: the seat lock (`flock`, no PID, `protocol.md:798-803`),
  and the per-team sync `lock`. A second process on the same bundle exits 75 (`protocol.md:738-746`;
  `DESIGN.md:607-612`).

**Authority (HUMAN / PEER / PING) rests on the server and the receiver's own checks. Nothing is signed.**

- Threat model: pingbus is a convenience, not the boundary. Any member can `curl` with its
  own token. The hard rules are the homeserver host, the receiver's checks and the network
  (`DESIGN.md:40-63`; `protocol.md:28-31`).
- The sender is the Matrix `sender` the homeserver stamps, which comes from the token.
  The receiver classes each sender from the team record (`agent_bus.team`): listed human =
  human path, role holder = ping/text path, anyone else = drop `sender` (`protocol.md:552-554`).
  Only `admin` writes the record. A record not sent by `admin`, unknown keys, or power levels
  that are not exact make the room untrusted, exit 10 (`protocol.md:406-422,457-478`).
- HUMAN = `m.text` from a listed human that mentions this agent (`m.mentions`) or `@room`
  (`protocol.md:286-311`). PEER = `m.notice` with `agent_bus.text` naming this agent, "never
  an instruction" (`:313-331`). PING = `agent_bus.ping` with a closed verb, a role check per
  verb (`:181-203`) and a forge check on the reference (`:224-282`). Decision table:
  `DESIGN.md:1161-1172`.
- What stops a peer posing as a human: handles hold `.` and `+` and human localparts hold
  neither (`protocol.md:107`). Human accounts exist only in the record. Agents' `@room` is
  never read as a human's, because the sender class is checked first (`protocol.md:290-294`).
  The reply fallback is stripped, so a human's quote of an agent cannot carry the agent's
  words as HUMAN (`DESIGN.md:1190-1193`).
- Known holes: root on the homeserver host can mint any account, and anything that can read
  a human's Element session can post as that human. That second hole is why no agent runs
  as a user with an Element profile (`DESIGN.md:58-63,1243-1253`). An envelope can be forged
  by anything holding the session's inbox-socket token. The skill's control is to re-check
  `id:` against `pingbus history` before acting on a consequential HUMAN request
  (`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/skills/pingbus/SKILL.md:44-52`).
- v2: same basis. No roster signing (OD-A4, `n3:DESIGN-v2-A-protocol.md:773-783`). Sender
  classes are `admin|human|coordinator|worker|probe|none` (`n3:DESIGN-v2-A-protocol.md:762-771`).
  The kind comes from the roster class, never from content (`n3:DESIGN-v2-A-protocol.md:1054-1073,2003-2004`).
  `teamchat verify <id>` replaces the history check (`n3:DESIGN-v2-B-member.md:369,2044-2047`).

## 2. Waking: every delivery path (v1)

| Path                           | Mechanism                                                                                                                                                                                                                            | Claude-Code-specific part                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Inbox socket (primary)         | SessionStart hook starts `pingbus watch --supervise` detached (`helpers/pingbus/hooks.py:12-17,634-650`). The watcher long-polls `/sync` and writes one envelope per delivery to the socket, at most one per 2 s (`protocol.md:939`) | `CLAUDE_CODE_MESSAGING_SOCKET` and `CLAUDE_CODE_MESSAGING_TOKEN`. Wire: one connection, `{"type":"auth","token":…}` then `{"type":"user","message":{"role":"user","content":<text>}}`, half-close (`helpers/pingbus/notify.py:24-25,105-137`). Needs `crossSessionInbound: accept` passed by `--settings` (`files/opt/claude-yolo/optional/agent-bus/settings.json`; `helpers/pingbus/seat.py:447-462`). `watch` refuses (78) without the socket (`protocol.md:939`). Claude Code labels the message "Another Claude session sent a message" (`SKILL.md:96-99`) |
| Delivery proof                 | the watcher searches the session transcript for `agent-bus delivery <nonce>:`. Not found after 180 s quiet or 1 h = `missing` (`helpers/pingbus/inject.py:1-21,173-228`)                                                             | the transcript path comes from the hook payload's `transcript_path` (`hooks.py:946-957`). The search depends on how Claude Code writes the JSONL (a `user` entry, or a `queued_command` attachment). Read receipts are sent only after the message is found (`protocol.md:772-779`)                                                                                                                                                                                                                                                                             |
| `pingbus wait` (fallback)      | the agent runs `pingbus wait` with `run_in_background`. It exits on the first item and the agent re-arms it (`SKILL.md:136-139`; `DESIGN.md:1108-1112`)                                                                              | depends on Claude Code's background-Bash completion re-invoking the agent. A scripted driver can run `wait` between turns: "a headless agent driven by a script (`claude -p` in a loop…) needs no socket" (`DESIGN.md:714-716`)                                                                                                                                                                                                                                                                                                                                 |
| Hooks (guard and context)      | UserPromptSubmit adds the pending count. Stop blocks once per pending set. SessionStart adds wake path, roles and seats. SessionEnd posts the goodbye (`hooks.py:1-45`; registrations in `plugin/pingbus/hooks/hooks.json:3-54`)     | Claude Code hook names and JSON shapes (`hookSpecificOutput`, `decision`, `stop_hook_active`, `systemMessage`). Each hook is gated on `$PINGBUS_HOME/active`. SessionEnd needs `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000` (`protocol.md:821-826`)                                                                                                                                                                                                                                                                                                           |
| Membership notice              | the watcher writes a `MEMBERSHIP` envelope when the owner changes teams on the host. Fallback: the next UserPromptSubmit (`protocol.md:1133-1146`)                                                                                   | same socket, same hook                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Relay mode (supervisor typing) | `PINGBUS_DELIVERY=relay` makes `watch` spool lines for a supervisor plugin to type (`helpers/pingbus/relay.py:1-61`; `protocol.md:748-770`)                                                                                          | **not deployed**: inert until a launch sets the variable. Deferred to v2 until hooks-daemon #119 ships (Plan 00194 `PLAN.md:3,36-37,59-62`; Plan 00209 D4)                                                                                                                                                                                                                                                                                                                                                                                                      |
| ccy supervisor                 | ctrl+z guard and the `ccy_lifecycle` plugin: max-age restart, deadline, first-join relaunch (`files/var/local/claude-yolo/supervisor-plugins/ccy_lifecycle.py:8-30`; `entrypoint.sh:604-626,749-756`)                                | delivers no bus message today. The first-join relaunch exists because `crossSessionInbound` is read only at launch (`seat.py:448-454`)                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Status line                    | `pingbus-status-line` wraps the session's own status line and adds `👥 team (role)`, plus `(down)` (`src-go/internal/pingbus/statusline/statusline.go:38,366-367`; `protocol.md:872-888`)                                            | not a wake path and shows no pending count. Reads the payload's `session_id` and Claude settings files (section 6)                                                                                                                                                                                                                                                                                                                                                                                                                                              |

## 3. Delivery format and ack (v1)

- `recv`/`wait` print tab-separated lines: `PING|HUMAN|PEER|REPLY|TIMEOUT` (`protocol.md:991-1066`;
  `SKILL.md:18-30`). Every `HUMAN`/`PEER` line is followed by a `REPLY` line carrying a
  ready-to-run `pingbus --team T say --to NAME --re '$id'` (`protocol.md:1030-1039`).
- The socket envelope has a first line `agent-bus delivery <nonce>: N messages (…)`. Each
  entry sits between `<<agent-bus NONCE i/n>>` and `<<end …>>`, with lines `kind`, `from`,
  `team`, `to`, `sent`, `id`, a fixed `authority:` sentence, `reply:` and `body:` (one JSON
  string, at most 2048 bytes). At most 8 entries or 12 KiB per delivery (`protocol.md:1104-1131`;
  example `SKILL.md:101-114`; authority strings `helpers/pingbus/envelope.py:68-83`).
- Consumption: a delivery consumes its items once the socket write completes, and `recv`
  consumes what it prints (`SKILL.md:130`; `protocol.md:772-775,937`). There is no per-message
  ack from the agent to pingbus. A ping's protocol ack is the agent's own `pingbus send ack --re ID` (`protocol.md:190,246`). An unanswered ping comes back to its sender as `TIMEOUT`.
  The only "delivery ack" is the transcript search (section 2).

## 4. Replying (v1)

- `pingbus send VERB [REF] --to …|--to-coordinator [--re ID] [--branch B]` sends a ping;
  `pingbus say --to …` sends free text read from stdin; `report`, `ask-human`, `thread` and
  `questions` serve the side rooms (`protocol.md:931-936`; `SKILL.md:224-298`).
- Auth: the CLI reads the bundle token under `PINGBUS_HOME/<team>/` and sends a Bearer header
  (`config.py:12,93,107`; `matrix.py:194`). Forge checks need `gh auth token`; without it,
  78 (`protocol.md:276-282`). Send order: config → room trust → validator → role → rate →
  forge → `PUT /send` (`protocol.md:522-539`). Whichever process holds the env and token can
  send. Nothing ties the send to the agent's process.
- Exit codes: `protocol.md:970-989`.

## 5. v2 (`teamchat`, Plan 00209)

- **Shape.** One static Go binary. `teamchat run` is the **keeper**: Claude Code's parent,
  makes no network calls. `teamchat link` owns all state and traffic and serves every verb
  over `<v2-run>/link.sock`. Hooks, the status line and agent verbs are short RPC clients
  (`n3:DESIGN-v2.md:38-77`; `n3:DESIGN-v2-B-member.md:275-337`, sections 2.1, 2.4).
- **Member model.** A seat is a Matrix account. The bundle is `seat.json` + `token`, placed
  by the host into a seat store outside every checkout (`~/.local/state/teamchat/seats/`).
  A ccy session sees only `sessions/<session>/` at `/run/teamchat/seats` (DC5,
  `n3:DESIGN-v2.md:138,194`; `n3:DESIGN-v2-D-migration.md:257-270`). Host and LXC members use
  `~/.config/teamchat/seats/<team>/` and `~/.config/teamchat/membership/desired.json`.
  There is no env file: `run` takes every fact as an argument (`n3:DESIGN-v2-D-migration.md:266`).
  The team id is stable and the name is display-only (TID, `n3:DESIGN-v2.md:79-92`).
- **Wire.** Typed `teamchat.*` content in `m.room.message`, wire major 1, strict JSON. The
  roster is an admin state event. Rooms are main, reports and questions (`n3:DESIGN-v2.md:63-65`;
  A sections 2-7). Envelope grammar: A section 13 (`n3:DESIGN-v2-A-protocol.md:1695-2055`).
  Its first line and the transcript needle say `teamchat delivery <nonce>:` (`n3:DESIGN-v2-B-member.md:1565`).
- **Agent CLI.** JSON Lines on stdout, an error object last on stderr (BD3, B 2.2.2
  `n3:DESIGN-v2-B-member.md:407-466`). Verbs: `recv`, `wait`, `inbox`, `reply <id>`, `say`,
  `ping <verb>`, `report`, `ask`, `thread`, `questions`, `history`, `verify`, `status`
  (`n3:DESIGN-v2-B-member.md:354-372`). `recv`/`wait` end with a CLI → link ack: an item is
  marked delivered only once stdout has taken it (`n3:DESIGN-v2-B-member.md:1092-1106`).
- **Delivery.** v2 still uses Claude Code's inbox socket, with transcript proof and read
  receipts only after proof (B 2.7, `n3:DESIGN-v2-B-member.md:1415-1612`). The hooks are
  SessionStart, UserPromptSubmit, Stop and SessionEnd (B 2.8, `:1645-1694`). A session with
  no inbox socket gets delivery mode `wait` (`:1439-1442`). Relay mode is out (D4, PLAN `:58,72`).
- **Gate A (owner-visible names and paths).** The irreversible decisions OD-A7 (handle
  grammar, `host`→`site`), DC1 (one homeserver per team), DC5 (seat store outside checkouts),
  DC15 (only the pool bundle travels between sessions), OD-D1 (product name `teamchat`),
  OD-D5 (no reverse move after commit) and TID. No host install that writes a `teamchat` name
  or path the owner sees, and no team migration, until the owner confirms. Go code may merge;
  play tasks, units and the ccy image change may not (`PLAN.md:77,131-140`;
  `n3:DESIGN-v2.md:300,307,330,334,344,351,355`). Name table: D 3.2-3.6
  (`n3:DESIGN-v2-D-migration.md:113-273`). It fixes binary `teamchat` /
  `/usr/local/bin/teamchat`, `/usr/local/sbin/teamchat-admin`, plugin `teamchat` and skill
  `teamchat:teamchat`, `TEAMCHAT_*` env, `teamchat-server@<team>`,
  `teamchat-session-<user>.service`, `/var/lib/teamchat-status/`, the kit at
  `/opt/claude-yolo/optional/teamchat/` (ccy) and `/usr/local/lib/teamchat/kit/<digest>/`,
  and Element profile `teamchat-<team>` (`n3:DESIGN-v2.md:189-198`).
  Gate B: no process gets a pool seat's password before the owner confirms INT6 (`PLAN.md:78,141-148`).
- **Milestones** (`n3:PLAN.md`): N0 design done (`:108-148`). N1 foundations: Tasks 1.1-1.4a
  done, 1.5-1.7 open (`:150-199`). N2 host side: 2.1-2.4 in progress (`:204-257`). N3 member
  client: Task 3.1 spec done (`SPEC-n3.md`), Task 3.2 build open, slices n3a-n3f
  (`:341-370`). n3e is "delivery into Claude Code, the hooks, the status line, the skill"
  (`n3:SPEC-n3.md:31`). N4 ccy members and board, N5 other member kinds (host `cc`,
  docker/VM), N6 migration: all open (`:381-416`). **Current: N2 and N3 are being built
  together.** n3a has code on `feat/bus-v2-n3a*` (`src-go/cmd/teamchat`, `internal/teamchat/member/{cli,exits,homefs,kit,lock,records}`).
  Resume note: `n3:subagent-reports/261022-resume.md:22-63`.
- **Existing member-kind / launch-kind notions.**
  - `run --claude|--plain --kind ccy|lxc|host|other` (`n3:DESIGN-v2-B-member.md:378,785-846`).
    `--kind` only picks the recovery text for a failed link (`:787`).
  - `--plain -- CMD` runs any command with no settings, no plugin, no hooks and inbound
    `refuse`. Its teams show `"delivery":"none"`, and items wait for `recv` (`:815-818,843-846`).
  - `--plain` is already used for a shell (`:322-326`), and for the acceptance probe
    `run --plain --kind host --state DIR --seat <team>/probe` (`n3:DESIGN-v2-C-host.md:458`).
  - In code: `LaunchClaude`/`LaunchPlain`, `KindOther = "other"`
    (`feat/bus-v2-n3a:src-go/internal/teamchat/member/cli/launch.go:17-33`). Under `--claude`
    the Claude argument is the first argv entry whose base name is `claude` (`launch.go:182`).
  - The handle `<type>` is still the encapsulation (`podman|lxc|docker|vm|host`), not the
    agent program (`n3:DESIGN-v2-A-protocol.md:205-212`).
  - No design text names an "adapter" for agent programs. "adapter" in D means only
    `migrate/v1` (`n3:DESIGN-v2-D-migration.md:874`).
- **Where a generic seam would touch the design** (locations, not a proposal):
  - B 2.4.1 step 5: per-launch settings and the plugin, which `--claude` alone does.
  - B 2.4.4: the hook ops and their trust rule (peer pid descends from the session, socket
    path in Claude Code's form, `n3:DESIGN-v2-B-member.md:1108-1120`).
  - B 2.7.1-2.7.5: socket write, envelope, transcript proof, receipts.
  - B 2.8 hooks, B 2.9 status line, B 2.12 plugin and skill.
  - B 2.11: the end and goodbye, decided by the keeper after CMD exits.
  - D 3.4: plugin and skill names. Seam 56 / N5: other member kinds (`n3:DESIGN-v2.md:175`).

## 6. Assumptions that a member is Claude Code

v1:

- The plugin format (`.claude-plugin/plugin.json`, `hooks/hooks.json`), `--plugin-dir`,
  `--settings` and `crossSessionInbound` (`agent-bus-claude:21-24,96`;
  `entrypoint.sh:587-592`, which refuses unless the command is `claude`).
- Hook events SessionStart, UserPromptSubmit, PostToolUse (matcher `Bash`), Stop and
  SessionEnd, with their stdin/stdout JSON (`hooks.json:3-54`; `protocol.md:946`).
- `CLAUDE_CODE_MESSAGING_SOCKET`/`_TOKEN` and the socket's wire (`notify.py:24-25,105-110`),
  and `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS` (`settings.json`).
- Transcript JSONL search via the hook's `transcript_path` (`inject.py:15-20`).
- Status line: the `statusLine` setting. It finds the inner line in
  `.claude/settings.local.json`, `.claude/settings.json`, then `$CLAUDE_CONFIG_DIR` or
  `~/.claude/settings.json` (`src-go/internal/pingbus/statusline/settings.go:65-70`). It
  files records under the payload's `session_id` (`statusline.go:345-347`).
- Resume by conversation id: `conversation.json` from SessionStart, resolved under
  `~/.claude` to a transcript, `--resume <id>` (`src-go/internal/conversation/conversation.go:2-6,51-62,87`;
  `protocol.md:946`).
- The skill is a Claude Code skill (`plugin/pingbus/skills/pingbus/SKILL.md`). Its wake
  advice assumes `run_in_background` (`SKILL.md:136-139`).
- The envelope text names the label "Another Claude session" (`hooks.py:171-172`).

v2 keeps every one of these under `--claude`:

- the inbox socket form `$XDG_RUNTIME_DIR/cc-socks/<pid>.sock` (`n3:DESIGN-v2-B-member.md:1116-1118`);
- the transcript needle (`:1560-1595`);
- `--plugin-dir <kit>/plugin/teamchat --settings <v2-run>/settings.json` appended after CMD (`:819-835`);
- the four hook events (`:1645-1685`);
- `--resume <session_id>` from `conversation.json` (`:836-842`);
- the skill `teamchat:teamchat` (`:2022-2068`).

`--plain` is the only launch mode not tied to Claude Code, and it gets no wake path
(`:843-846`). The pure-CLI surface does not depend on Claude Code, except `TEAMCHAT_RUNTIME`
pointing at a live link: `recv`, `wait`, `reply`, `say`, `ping`, `verify`, `status`, all
as JSON Lines over `link.sock` (`:354-372,1060-1090`).
