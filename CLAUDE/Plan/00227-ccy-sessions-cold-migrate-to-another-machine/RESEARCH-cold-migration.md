# Cold migration of ccy sessions and agent teams between two machines: research

Research only. Nothing in the repository was changed. Placeholders: `machine-A` (the overloaded
laptop), `machine-B` (the target), `<checkout>` (a project checkout's host path), `<repo>`,
`<team>`, `<seat>`, `<name>` (a ccy session name), `<id>` (a Claude Code session id).
Citations are `path:line` in this repo or URLs. **Verified** means checked here, by reading
code or running something. **Inferred** means read from documentation or reasoned from code,
not exercised end to end.

## Short answer

A **solo** ccy session is close to portable already. Inside the container the working
directory is always `/workspace`, and `/root/.claude` is a symlink to
`/workspace/.claude/ccy`. Everything Claude Code keeps for the session is therefore inside the
checkout, under a project directory that is called `-workspace` on every machine, whatever the
host path. Git worktrees also record only `/workspace/...` paths. So "copy the checkout, then
`ccy --resume <id>`" is the core of the migration.

What does not carry over is the **host-side** state:

- the session registry record and the tmux session;
- the session name, which comes from the host path;
- the tokens and the image;
- most of all, the **agent team bus**: in v1 a ccy seat refuses to start unless its team's
  homeserver runs on the same machine.

So there are two cases:

- **A solo session:** a small Go tool (`export`/`import`) built on code that already exists.
- **A whole team:** it moves only if the homeserver moves with it. Nothing builds that today,
  but `agent-bus-install backup-now`/`restore` already exist.
- **One member of a team that stays on machine-A:** this needs the unbuilt WireGuard overlay
  and v2 remote seats (Plans 00220, 00209). It is the largest option.

---

## 1. Claude Code's own session state

### Where it lives (documented)

Source: <https://code.claude.com/docs/en/claude-directory> ("Application data") and
<https://code.claude.com/docs/en/sessions> ("Where transcripts are stored").

| Path under `~/.claude/` (in ccy: `<checkout>/.claude/ccy/`)                                       | What it is                                                                                   | Needed to resume?                                 |
| ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `projects/<slug>/<id>.jsonl`                                                                      | the full transcript                                                                          | **yes**                                           |
| `projects/<slug>/<id>/subagents/`, `tool-results/`, `mcp-tasks/`                                  | sub-agent transcripts, spilled large tool output, MCP task state                             | yes; the transcript refers to them                |
| `projects/<slug>/memory/`                                                                         | auto memory                                                                                  | wanted for continuity                             |
| `file-history/<id>/`                                                                              | pre-edit snapshots for checkpoint rewind                                                     | only for `/rewind`                                |
| `tasks/`, `session-env/`, `plans/`, `paste-cache/`, `history.jsonl`                               | task lists, per-session env metadata, plan files, pastes, prompt history (with project path) | nice to have                                      |
| `sessions/<pid>.json`                                                                             | one file per **running** session, removed on exit                                            | **no; must not be carried** (see F5 below)        |
| `shell-snapshots/`                                                                                | removed on clean exit                                                                        | no                                                |
| `jobs/`, `daemon/`                                                                                | background-session (agent view) state                                                        | carry with the tree; no ccy use found             |
| `settings.json`, `plugins/`, `keybindings.json`, `skills/`                                        | user config; ccy's entrypoint rewrites parts of it and recopies `skills/`                    | carry                                             |
| `backups/`, `cache/`, `debug/`, `remote-settings.json`, `policy-limits.json*`, `stats-cache.json` | caches and logs                                                                              | no; regenerated                                   |
| `~/.claude.json`                                                                                  | app state: trust and onboarding per project path                                             | in ccy it lives in the container layer; see below |

The scratchpad and pasted images live under the temp directory (`/tmp/claude-<uid>/<slug>/<id>/`),
not under `~/.claude`. In ccy that is the container's `/tmp`, which is already lost when every
container stops, so a migration loses nothing new there.

**Slug rule (documented):** the working-directory path with every character that is not a
letter or digit replaced by `-`. A name longer than 200 characters is cut to 200 with a hash of
the full path appended. `CLAUDE_CODE_PROJECT_DIR_NAME` (together with `CLAUDE_CONFIG_DIR`) can
pin the slug (sessions doc, "Name the project directory yourself"). The repo encodes the same
rule for `cc` in `ccy_transcript_dir` (`files/var/local/claude-yolo/lib/session-registry.bash:1015-1028`).

**Plaintext:** transcripts, `history.jsonl` and `file-history/` are not encrypted at rest
(claude-directory doc, "Plaintext storage"; `docs/ccy.md:680-723`). Plan 00098 closed its gate
on live-state encryption, and no sealer exists (`CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/PLAN.md:196-208`).
A migration bundle is therefore a plaintext copy of everything Claude ever read, and it has to
be encrypted in transit.

### Resuming on another machine

**Documented:**

- `claude --resume <session-id>` works "from any directory". It looks first in the current
  project and its worktrees, then in "every other project on this machine". The cross-project
  search resolves only when exactly one project holds the id, so a hand-made duplicate gives
  not-found (sessions doc, "Resume by session ID or name").
- `--resume <absolute transcript path>` also resumes a session.
- A resume restores: history (from the last compaction), the model, the agent, the active goal,
  and session-scoped scheduled tasks that have not expired.
- It does not restore: background Bash/monitor tasks, background sub-agents or workflows (they
  appear as "didn't finish" notes), or flags such as `--settings`, `--mcp-config`, `--add-dir`.
- A session that ended in `bypassPermissions` resumes in the default mode unless the launch
  flag is passed again. ccy passes it on every launch.
- After a long break the prompt cache is cold, and a "Resume this conversation?" dialog may
  show the share of the 5-hour limit the first turn will cost.

**Verified here (Claude Code 2.1.310, in this container).** I copied one small transcript into
a fresh, empty `CLAUDE_CONFIG_DIR` (no `.claude.json`, no `sessions/`, no history, no
credentials) and ran `claude -p --setting-sources user --resume <id>` with no token at all:

- same slug (`-workspace`), cwd `/workspace`: the lookup **passed**, then the run stopped at
  "Not logged in";
- cwd somewhere else, transcript under `-workspace`: **passed** the lookup;
- transcript under an unrelated slug, cwd `/workspace`: **passed** the lookup;
- control, an id that is not there: `No conversation found with session ID: …`. This shows the
  lookup runs before the auth check.

So nothing machine-local (no index file, no `.claude.json` entry, no machine id) is needed for
Claude Code to **find** a copied transcript. That a copied conversation then **loads and
continues** with a valid login was not exercised (no credential was available to the probe).
That part is **inferred** from the docs. The scratch copy was deleted afterwards.

**Known issues:**

- anthropics/claude-code#18645 ("Session Export/Import for Cross-Machine Portability",
  <https://github.com/anthropics/claude-code/issues/18645>) reported that copied sessions
  stopped being recognised between 2.1.5 and 2.1.9, even with paths rewritten. The report
  guessed at machine validation. It was closed as a duplicate of #17682 (cross-environment
  history sync, closed as inactive). It is old, its cause was never confirmed, and the probe
  above contradicts it for 2.1.310 at the lookup level.
- That report relied on `sessions-index.json`. In the ccy store that file exists but was last
  written months ago and lists none of the current sessions, so 2.1.310 does not maintain it.
  It still embeds `fullPath` (`/root/.claude/projects/-workspace/...`) and
  `projectPath` (`/workspace`), which are container paths and identical on both machines.
- There is no official export/import command. `/export` writes readable text and cannot be
  resumed. The open request is anthropics/claude-code#11455.

**Absolute paths and machine identity inside Claude Code's state:**

- Every transcript line carries `cwd` (`/workspace` in ccy) and `gitBranch`.
- `history.jsonl` lines carry `project`.
- `sessions/<pid>.json` carries a PID from the container's PID namespace, a `pidDomain` with a
  per-machine hash, and a messaging socket path. This is why it must never be carried.
- The repo already found that `--continue` treats a stale record from another PID namespace as
  live for good. That is the F5 trap: `CLAUDE/Plan/00203-teams-hot-control-and-robustness/SPEC-team-add-and-restart.md:31-39`.
  `--resume <id>` is not subject to it.
- `~/.claude.json` holds `projects["/workspace"]` trust. In ccy it is in the container layer,
  recreated on every start, with trust for `/workspace` set unconditionally
  (`files/var/local/claude-yolo/entrypoint.sh:401-425`).
- For **ccy**, nothing in Claude Code's own state names the host path. For a host **cc**
  session the slug is the host path, so a different path on machine-B means renaming the
  project directory, and the `cwd` lines then disagree with it (inferred).
- Transcript lines record the writer's Claude Code `version`. There is no documented promise
  that an older Claude Code reads a newer transcript (inferred risk: refuse a downgrade).

---

## 2. What a ccy session depends on, beyond the transcript

| Item                                         | Where                                                                                                                                                                                                                                                                                                                            | Machine-specific?                                                                               | Migration verdict                                                                                                                                             |
| -------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Container                                    | `--rm`, ephemeral (`claude-yolo:2497`, `:3880-3881`). Mounts: `$PWD:/workspace`, a per-launch config import, tracked `.claude/ccy/mounts`, `ccy.env.local` read-only (`claude-yolo:2519-2543`)                                                                                                                                   | no state of its own                                                                             | nothing to move                                                                                                                                               |
| Claude Code state                            | `/root/.claude` -> `/workspace/.claude/ccy` (`entrypoint.sh:326-338`); transcripts always `<checkout>/.claude/ccy/projects/-workspace/` (`session-registry.bash:1018-1021`)                                                                                                                                                      | **no** (container paths only)                                                                   | **carry**; it is the payload                                                                                                                                  |
| The conversation id                          | not in the registry record (SPEC F8, same SPEC `:43-44`). It is in `<checkout>/.claude/ccy/pingbus/sessions/<name>/conversation.json`, rewritten on every SessionStart (`src-go/internal/teams/membership/report.go:70-72`; shape `src-go/internal/conversation/conversation.go:38-44`)                                          | keyed by `<name>`, which comes from the host path (below)                                       | carry; re-key if the name changes                                                                                                                             |
| Session name                                 | `ccy-<project>[-N]` (`lib/tmux-session.bash:75-84`); `<project>` = `<parent>-<dir>` of the **host** path (`lib/common-pure.bash:488-503`)                                                                                                                                                                                        | **yes**                                                                                         | the same path on machine-B gives the same name                                                                                                                |
| Session registry record                      | `${XDG_STATE_HOME:-~/.local/state}/ccy/sessions/<name>`: `name dir launcher prefix restore [compose] arg…` (`src-go/internal/registry/registry.go:96-128`, `:168-195`)                                                                                                                                                           | **yes**: `dir=` and `launcher=` are absolute host paths, `arg=--ssh-key <file>` a host key path | **snapshot, rewrite, write on machine-B**                                                                                                                     |
| Registry deleted on a clean `/exit`          | the trampoline removes the record when the launcher returns; the hard stop removes it too (`CLAUDE/Plan/00201-…/SPEC-shutdown.md:41-49`)                                                                                                                                                                                         | n/a                                                                                             | export must **snapshot before stopping** (the Task 3.17 pattern, §3)                                                                                          |
| Restore manifest                             | `<state>/ccy/last-restore`, tied to `boot_id` (`src-go/internal/restore/manifest.go:26`, `:131-141`)                                                                                                                                                                                                                             | yes                                                                                             | do not carry                                                                                                                                                  |
| tmux                                         | ccy's own server `tmux -L ccy` (`lib/tmux-session.bash:32,40`; `src-go/internal/tmuxccy/tmuxccy.go:20`); no team-specific layout found                                                                                                                                                                                           | yes                                                                                             | recreate (`tmuxccy.StartSpec`)                                                                                                                                |
| Claude OAuth token                           | `~/.claude-tokens/ccy/tokens/<name>.<YYYY-MM-DD>.token`, injected as `CLAUDE_CODE_OAUTH_TOKEN` (`claude-yolo:149`, `:3880-3894`)                                                                                                                                                                                                 | not bound to a machine                                                                          | existing `ccy --export-token NAME` prints a pasteable import script (`lib/token-management.bash:1804-1905`; `src-go/internal/ccylaunch/exporttoken.go:16-70`) |
| GH token, SSH keys                           | GH token derived per launch from machine-B's `gh` login (`playbooks/imports/play-github-cli-multi.yml:1214-1216`; `lib/ssh-handling.bash:1626-1666`); keys `~/.ssh/github_*`, deploy-key aliases (Plan 00116)                                                                                                                    | yes                                                                                             | provisioned on machine-B by the plays; never carried                                                                                                          |
| Images                                       | `claude-yolo:latest` built by `play-claude-yolo.yml`; `claude-yolo:<project>` built by the launcher from the tracked `.claude/ccy/Dockerfile` (`claude-yolo:2069-2100`)                                                                                                                                                          | yes (podman storage; tag comes from the host path)                                              | rebuilt on machine-B; never copied                                                                                                                            |
| `allowed-hostnames` (tracked)                | the launcher refuses a host whose name matches no line (`lib/common.bash:768-830`)                                                                                                                                                                                                                                               | **yes**                                                                                         | import must check it and refuse with the fix                                                                                                                  |
| `mounts` (tracked)                           | `<host-src>:<container-dst>`; a source that does not exist aborts the launch (`lib/common.bash:412-441`)                                                                                                                                                                                                                         | yes (host paths)                                                                                | import checks every source exists                                                                                                                             |
| `ccy.env.local` (untracked, Plan 00160)      | placed by IaC; sets `HOOKS_DAEMON_HOSTNAME`, the `<host>` part of a seat handle (`lib/common.bash:690-720`)                                                                                                                                                                                                                      | **yes** (the install's identity)                                                                | never carried; machine-B's own copy is used                                                                                                                   |
| `.last-launch.conf`                          | token name, `LAST_SSH_KEYS` host paths, network, teams, launcher hash (`claude-yolo:3448-3463`)                                                                                                                                                                                                                                  | yes                                                                                             | excluded; machine-B regenerates it                                                                                                                            |
| `state/restart-request.json`, restart budget | transient (`lib/restart-request.bash:12,34`); budget in `~/.cache/claude-yolo-restart-history` (`claude-yolo:162`)                                                                                                                                                                                                               | yes                                                                                             | refuse to export while a restart request is present                                                                                                           |
| Network, compose                             | `--network` replayed from the record; podman networks and compose services are per machine (`lib/network-management.bash`)                                                                                                                                                                                                       | yes                                                                                             | import checks the network exists on machine-B, or refuses                                                                                                     |
| hooks-daemon state                           | `<checkout>/.claude/hooks-daemon/untracked/`: venvs named after the path they were built for (some after machine-A's **host** checkout path), `daemon-<hash>.pid/.sock`, ledgers (goal ledger, cron records), `secret-meta.key.secret`                                                                                           | venvs and socket: yes; ledgers: per checkout                                                    | exclude venvs, pid, sock and locks (rebuilt on demand); carry ledgers                                                                                         |
| Git worktrees                                | `<checkout>/.claude/worktrees/agent-*` and `untracked/worktrees/`; every `gitdir` is absolute **`/workspace/...`**; container git 2.39 has no relative worktree paths                                                                                                                                                            | no (container paths)                                                                            | carry with the checkout; they work in the container on machine-B as on machine-A                                                                              |
| `untracked/` in the checkout                 | plan run logs, reference clones, scratch, agent reports; tens of GB on a busy checkout                                                                                                                                                                                                                                           | no                                                                                              | owner choice (Q6)                                                                                                                                             |
| Team membership                              | `$XDG_RUNTIME_DIR/ccy/membership/<name>/` (tmpfs) (`lib/agent-bus-membership.bash:9-16`)                                                                                                                                                                                                                                         | yes                                                                                             | rebuilt at launch                                                                                                                                             |
| Seat bundle and state                        | `<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>/`: `member.json` (`base_url`, `plain_http_hosts` = homeserver address), `token` (Matrix access token, 0600), `state/` (inbox, consumed, `sync.json`, outbox, cached team record, locks), `seat.lock`, `said-goodbye.json` (`docs/agent-bus-protocol.md:736-751`, `:911-925`) | **yes**: bound to a homeserver address and to a live homeserver database                        | valid on machine-B only if the homeserver moves with the same address (§4)                                                                                    |
| Homeserver                                   | `agent-bus-hs@<team>.service`; data in `/var/lib/agent-bus/<team>/` (db, backups, `secrets/registration_shared_secret`, `secrets/admin.token`, `team.json`, `registry.json`, room ids); root state in `/var/lib/agent-bus-install/<team>/` (`files/usr/local/sbin/agent-bus-install:88-91`, `:807-812`)                          | yes                                                                                             | moved only as a whole (§4)                                                                                                                                    |
| The local-homeserver rule                    | `ccy --teams` refuses a team whose homeserver unit is not active **on this host** (`helpers/agent_bus/checkout.py:697`; `CLAUDE/Plan/00161-agent-team-bus-matrix/DESIGN.md:877-883`; scope rule D46, `DESIGN.md:24-30`)                                                                                                          | n/a                                                                                             | **the blocker for moving one member**                                                                                                                         |
| Team declaration                             | untracked host_vars `agent_bus_teams` (port, `listen`, `allow_from`, humans, coordinator) and `agent_bus_lxc_members` (absolute `checkout:`) (`environment/localhost/host_vars/localhost.yml.dist:101-121`, `:159-168`); team name and `server_name` can never change (`docs/agent-bus.md:183-185`)                              | yes                                                                                             | carried by the owner's host_vars (vault/untracked), never by the bundle                                                                                       |
| LXC members                                  | reached only by `lxc-attach` into this machine's LXC (`docs/agent-bus-lxc-members.md:62-64`)                                                                                                                                                                                                                                     | yes                                                                                             | **refuse**                                                                                                                                                    |

### What decides "is this session running" today

- **tmux:** the session name is on the `ccy` tmux server (`tmuxccy.(*Tmux).List`,
  `src-go/internal/tmuxccy/tmuxccy.go:119-147`). Callers include `sessions/restore.go:107-116`
  and `teams/shutdown/stop.go:87-97`.
- **Pane state:** `restore.Judge` (`src-go/internal/restore/verdict.go:99-125`).
- **Container:** pane to container through `sessprobe.SessionContainers`
  (`src-go/internal/sessprobe/join.go:121-160`), and running containers labelled `ccy=true`
  (`collect.go:152-166`).
- **Seat keeper:** `hot.keeperAlive` (`src-go/internal/teams/hot/list.go:355-366`). A seat is
  held while the non-blocking `flock` on its `seat.lock` is held (`src-go/internal/bus/seat/lock.go:10-45`).
  The homeserver has no lease and no PID record (`docs/agent-bus-protocol.md:756`, `:814`).
- **Two meanings of "live":** these remain unreconciled (Plan 00203 Decision 6, `PLAN.md:308-320`).
  An export gate has to take the union of both.

---

## 3. Existing plans

| Plan                                         | Status                                                           | What it gives a migration                                                                                                                                                                                                                                                                                                                                                                                                          | What it lacks                                                                                                                                                                                                                                                                                                       |
| -------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 00135 sessions survive a reboot              | In Progress                                                      | The registry and its strict Go reader/writer (`registry.ReadFile`, `Store.Write`). Replay rules (`registry.ResumeArgs`, which strips one-shot flags and adds `--resume <id>`, `src-go/internal/registry/restore.go:94-125`). A detached start through `tmuxccy.StartSpec` with `CCY_SESSION_RESTORE=1` (`src-go/internal/sessions/restore.go:184-251`). A `--dry-run` precedent (`:232-234`). `set-going` for the cold first turn. | Same-machine only. `dir=`/`launcher=` are host paths, the manifest is per boot, and `os.Stat(rec.Dir)` fails when the path differs. No portability concept at all.                                                                                                                                                  |
| 00203 hot control and robustness             | In Progress                                                      | Owner ruling: resume is **by recorded id, never `--continue`** (Decision 10, `PLAN.md:342-350`; Task 3.11, `:232-238`). Decision 12: sessions on no team record their id too. `conversation.For` (`src-go/internal/conversation/conversation.go:59-95`) refuses an id with no transcript.                                                                                                                                          | Task 3.17, the **last-shutdown record** that snapshots every member's registry record, seats, role, conversation and LXC members before `/exit` deletes them, exists only on a side branch (`plan/00203-team-lifecycle`), is not built, and its Decisions 18, 19 and 21 are open. Export needs exactly this record. |
| 00201 many teams per session                 | (spec source)                                                    | SPEC-shutdown: `/exit` typed through tmux is the clean stop, and it deletes the registry record (`SPEC-shutdown.md:41-49`).                                                                                                                                                                                                                                                                                                        | n/a                                                                                                                                                                                                                                                                                                                 |
| 00154 freeze and thaw                        | not built                                                        | A "refuse if frozen" gate shape.                                                                                                                                                                                                                                                                                                                                                                                                   | n/a                                                                                                                                                                                                                                                                                                                 |
| 00158 take over                              | built                                                            | `ccy_tmux_other_terminals`: refuse while attached elsewhere.                                                                                                                                                                                                                                                                                                                                                                       | n/a                                                                                                                                                                                                                                                                                                                 |
| 00220 WireGuard overlay, remote team members | Not Started                                                      | The private network between the laptops. A `host`-type member on machine-B with a hand-carried bundle (Phase 2, `DESIGN.md:221-229`, `:320-337`).                                                                                                                                                                                                                                                                                  | Remote **ccy** seats are deferred to v2 (`PLAN.md:24-33`, `:110-115`). `play-vpn.yml` only installs `wireguard-tools`.                                                                                                                                                                                              |
| 00205 ssh team members                       | In Progress (research done, owner decisions open, nothing built) | ssh in place of `lxc-attach` for in-member steps.                                                                                                                                                                                                                                                                                                                                                                                  | It is about members, not about moving a launcher's sessions. It depends on WireGuard for other hosts and leaves token delivery to the broker.                                                                                                                                                                       |
| 00197 seat broker                            | Not Started                                                      | Approval-gated seat create/remove with an audit log; a natural future "move seat" authority.                                                                                                                                                                                                                                                                                                                                       | Designed for LXC bridge clients on one host.                                                                                                                                                                                                                                                                        |
| 00209 agent bus v2 (`teamchat`)              | In Progress (design)                                             | A host seat pool keyed by a hash of the checkout's real path, a fresh login on each take, a fixed device id, and a per-team `url` field for a team reached elsewhere.                                                                                                                                                                                                                                                              | Seat paths are one-host ("not on this host" stays `needs: reissue`, `DESIGN-v2-C-host.md:2190`). Its `migrate … fence … carry … commit … rollback` verbs are for v1 to v2, not between machines. A checkout-path hash key also means a different path on machine-B is a different pool.                             |
| 00207 D25 host-only seat store               | Not Started, decision open                                       | Moves cc coordinator seats out of the checkout.                                                                                                                                                                                                                                                                                                                                                                                    | If built, seats would **no longer travel inside the checkout**, so export would have to collect them.                                                                                                                                                                                                               |
| 00178 team history export                    | Not Started                                                      | Exports the team's Matrix room (`events.jsonl`, `history.md`).                                                                                                                                                                                                                                                                                                                                                                     | No Claude transcripts.                                                                                                                                                                                                                                                                                              |
| 00098 encrypted transcripts at rest          | In Progress; live encryption gate closed                         | Owner-only modes and permission repair.                                                                                                                                                                                                                                                                                                                                                                                            | No sealer and no keypair, so the bundle must bring its own encryption.                                                                                                                                                                                                                                              |

An earlier research report on cross-machine teams (kept untracked under `untracked/agent-reports/`)
reached the same verdict on remote seats. It recommends that remote ccy seats be built on v2
only, after the WireGuard overlay.

---

## 4. Proposed design

### Principles

- **Cold only, fail fast.** Refuse anything live and anything that cannot be carried exactly.
  Never half-migrate.
- **Resume by recorded id**, never `--continue` (Plan 00203 ruling).
- **One holder.** At no moment can both machines start the session or hold a seat: machine-A
  is fenced before machine-B is allowed to start.
- **The bundle carries checkout state, never machine state.** Machine-B's own plays provide the
  image, tokens, keys, gh login, `ccy.env.local` and team declaration. Import checks that they
  are there and refuses with the fix if not.
- **Go, not bash** (the CLAUDE.md tooling rule): verbs on the existing binaries, reusing
  `registry`, `conversation`, `sessprobe`, `tmuxccy`, `restore`.

### Unit of migration: the checkout

All sessions of one checkout share `projects/-workspace`, `history.jsonl` and the worktrees
(SPEC F4, `SPEC-team-add-and-restart.md:28-30`). The natural and safe unit is therefore **the
checkout and every ccy session recorded in it**, not one session. Exporting one session while
another session of the same checkout keeps running on machine-A would mean two writers on two
copies of one tree, so refuse it.

### `ccy-sessions export <checkout>` on machine-A

1. **Gate, refusing on any of these:**

   - the union of every liveness probe in §2, for every session whose record's `dir=` is this
     checkout;
   - any seat lock in `pingbus/seats/` held;
   - any member of any team those seats belong to live (§ whole team);
   - a pending `state/restart-request.json`;
   - a frozen session (00154 shape);
   - a `cc` session or an LXC member: refuse in v1 of the tool;
   - a session with no recorded conversation id (`--no-bus`, opencode; Decision 10 already
     names these as failures).

2. **Snapshot:** each session's registry record plus the id from `conversation.json`, validated
   by `conversation.For`. This is the Plan 00203 Task 3.17 last-shutdown record. Build export on
   it rather than invent a second format. If a session is still at a prompt, the owner stops
   it, or export performs the SPEC-shutdown `/exit` after taking the snapshot.

3. **Repository state:**

   - **Default:** carry the working tree **verbatim**. Uncommitted work, every worktree and git
     itself travel as files. The tool never commits on the owner's behalf.
   - **Safety net:** refuse unless every local branch with commits is pushed. GitHub is the
     backup (CLAUDE.md "Always Push"), so a lost bundle loses nothing committed.
   - **Alternative:** a mode that requires a clean tree and ships only `.claude/ccy`, with
     machine-B fetching from the remote. Smaller, but it cannot carry 100+ worktrees'
     uncommitted state (Q5).

4. **Build the bundle:**

   - an archive of the checkout with a fixed exclude list: hooks-daemon venvs, pid, sock and
     lock files; `.claude/ccy/sessions/*.json`, `shell-snapshots/`, `.last-launch.conf`,
     `ccy.env.local`, `cache/`, `debug/`, `backups/`; every `seat.lock` and `state/lock`;
   - the owner's chosen `untracked/` policy (Q6);
   - a manifest: source repo commit, ccy version, Claude Code version (the highest `version`
     in the transcripts), the host path, each session's rewritten record (args minus host-only
     flags) and id, and each seat's team/handle;
   - SHA-256 of every file.
   - The archive is encrypted (transport below). It is written owner-only, and never into the
     repository.

5. **Fence machine-A:**

   - remove the registry records, so `ccy-sessions restore` on machine-A cannot start them;
   - rename the checkout aside (`<checkout>.migrated-<stamp>`), so a manual `ccy` there cannot
     reopen the conversations;
   - for seats, the homeserver fence below.

   Renaming is reversible and needs no launcher change. A launcher tombstone check would be
   stronger but touches `files/var/local/claude-yolo/` and needs the CCY version bump (Q7).

### `ccy-sessions import <bundle>` on machine-B

1. Decrypt and verify the hashes. Refuse if the target path exists and is not empty, or if any
   session name in the manifest is already live on machine-B (restore today skips a live name
   silently; import must not).
2. **Path:**
   - **Default:** require the same absolute path as on machine-A. Both laptops are built by the
     same repo, so the user and layout normally match. Then the session name, the
     `conversation.json` key and the image tag are all unchanged.

   - **Different path:** this is a later option. It needs:

     - the session names re-derived and the `pingbus/sessions/<name>/` directories renamed
       (`conversation.For` checks `session == record name`, `conversation.go:80-83`);
     - the records' `dir=` rewritten;
     - for `cc`, the transcript project directory renamed to the new slug.

     ccy's transcripts and worktrees need nothing either way.
3. **Preflight, each failure naming the play to run:**
   - `allowed-hostnames` admits machine-B;
   - every `mounts` source exists;
   - every token named by `--token` exists and is valid (else point at
     `ccy --export-token NAME` on machine-A, already built);
   - the `gh` account and SSH keys the session used are present (`play-github-cli-multi.yml`);
   - the `--network` exists;
   - the ccy version is not older than the bundle's;
   - Claude Code is not older than the bundle's highest transcript `version`;
   - `claude-yolo:latest` exists (`play-claude-yolo.yml`). The project image builds on first
     launch.
4. Unpack with owner-only modes, write each rewritten registry record, and start each session
   detached through the restore path (`tmuxccy.StartSpec`, `CCY_SESSION_RESTORE=1`,
   `--resume <id>`). Optionally run `set-going` for the cold first turn, which may raise the
   "Resume this conversation?" cost dialog.
5. Print exactly what was started and what was refused.

### Transport and secrets

- **Bundle file (default):** an archive encrypted with `age`.
  - Either to a recipient key machine-B generates and publishes (only the public key leaves
    machine-B), or with a passphrase read from the terminal, never from argv or the environment.
  - The owner moves the file by any means. This needs no network service between the laptops.
  - `age` is not installed by any play today, and neither is `rsync` (no install task found
    for either), so this is a play edit: an IaC gap to fill, not to work around.
- **ssh/rsync pull from machine-B:** this needs sshd on machine-A. No desktop play enables it;
  only the LXC play touches sshd. It should run over the 00220 overlay rather than the LAN.
  Better as a second step.
- **What may travel inside the encrypted bundle:**
  - the transcripts (plaintext secrets of every file Claude read);
  - the seat `token` files (Matrix access tokens), and only when the homeserver moves with them.
- **Never in the bundle by default:** the Claude OAuth token (use the existing
  `--export-token` flow, or machine-B's own token), GH tokens, SSH keys, `ccy.env.local`, host
  vars, the vault.
- **Never in argv, never in the repo.** Seat tokens and homeserver secrets are written 0600 by
  the receiving side only.

### Whole-team migration

In v1 a team, its homeserver and every ccy/cc member live on one machine (D46, enforced by
`checkout.py:697`). So a team migrates **as a whole, homeserver included**, or not at all.

1. **Gate:** every member of the team is stopped. That covers every registry record holding
   `--teams …<team>`, every seat lock free, and LXC members (refuse if any exist). A member in
   another checkout is part of the team and is exported in the same run.
2. **Snapshot** each member (the Task 3.17 record), coordinator first in the order list.
3. **Homeserver, root-run and play-driven:**
   1. `agent-bus-install backup-now --team <team>` on machine-A
      (`files/usr/local/sbin/agent-bus-install:128`). A backup is the database backup plus the
      secrets/state tar of the same number, which is root-of-trust material
      (`docs/agent-bus.md:795-801`). Copying backups off the machine "is not built"
      (`docs/agent-bus.md:800`).
   2. The owner moves the `agent_bus_teams` entry to machine-B's host_vars, with the **same
      team name, `server_name`, port and bus address**. The bus address is a `/32` on a
      host-local dummy interface (`agentbus0`) that nothing routes, so both machines can carry
      the same address without conflict. That keeps every bundle's `base_url` valid, so **no
      seat has to be reissued**.
   3. Run `play-agent-bus.yml` on machine-B to install the team.
   4. Carry the backup.
   5. Run `agent-bus-install restore --team <team> --backup <ID>` (`agent-bus-install:1040-1101`;
      it requires the team directory and the backup files to exist on the target).
   6. Run `agent-bus-install remove --team <team>` on machine-A as the "never two homeservers"
      fence.
4. Import the member checkouts, **coordinator first**, then the workers, each by the solo
   procedure. The seat directories travel inside their checkouts and stay valid because the
   database moved with them.
5. **Fence alternative** if the homeserver stays on machine-A for any reason:
   `park-member`/`rotate-token` revokes the old token on the homeserver
   (`helpers/agent_bus/admin.py:1120-1130`; `helpers/agent_bus/cli.py:313`). Today this matters
   only for `host`/`vm` members, because a ccy seat cannot run away from its homeserver.

**Moving one member while its team stays on machine-A is not possible for a ccy seat.** It
needs the 00220 overlay plus v2 remote seats (00209), or the 00197 broker. The tool should
refuse it and name those plans.

### What breaks, and what must be refused

- **Refuse:**
  - anything live, by any of the probes in §2;
  - a partial checkout (some sessions running);
  - `cc` host sessions and LXC members (v1 of the tool);
  - sessions with no recorded id;
  - a target path in use;
  - a name already live on machine-B;
  - a Claude Code or ccy downgrade;
  - a hostname gate that does not admit machine-B;
  - missing mounts, tokens, keys or network;
  - one member of a team whose homeserver stays behind;
  - a team with LXC members.
- **Lost by design:**
  - background Bash/monitor tasks and background sub-agents (Claude Code docs);
  - the warm prompt cache (the first turn on machine-B re-sends the whole context);
  - `/tmp` scratchpads (already lost on every container stop);
  - compose services' runtime data, unless those volumes are in the checkout.
- **Unread team messages:** carried only if `state/sync.json`, `inbox/`, `consumed/` and
  `outbox.json` travel with the seat. They do, inside the checkout. Without them the first sync
  skips history (`docs/agent-bus-protocol.md:554-555`).
- **Seat handles** carry `<host>` from `HOOKS_DAEMON_HOSTNAME` in `ccy.env.local`
  (`docs/agent-bus-protocol.md:134-139`). It is unverified whether a carried seat whose handle
  names machine-A's role is refused, or merely mislabelled, when machine-B's `ccy.env.local`
  differs. Check `seat take` before relying on it. **Default:** refuse unless machine-B's
  `HOOKS_DAEMON_HOSTNAME` equals the one in the bundle's manifest.
- **Future drift:** Plan 00207 D25 would move cc coordinator seats out of the checkout, and v2
  keys its seat pool by a hash of the checkout's real path. Both would need export changes when
  they land.

### Open questions for the owner, each with a recommended default

| #   | Question                                                                                                   | Recommended default                                                                                                                   |
| --- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| Q1  | Must machine-B use the same checkout path?                                                                 | **Yes**, required in the first version; a different path is refused and becomes a later option.                                       |
| Q2  | Transport: bundle file or ssh/rsync?                                                                       | **An encrypted bundle file**; ssh pull only after the 00220 overlay exists.                                                           |
| Q3  | Encryption: `age` recipient key or passphrase?                                                             | **A recipient key generated on machine-B by a play**, public key only leaves it; a passphrase prompt as the fallback.                 |
| Q4  | Should the Claude OAuth token travel in the bundle?                                                        | **No**; the existing `ccy --export-token`, or machine-B's own token; import refuses a missing token with that command.                |
| Q5  | Uncommitted work: carry verbatim, WIP-commit, or refuse?                                                   | **Carry verbatim** and refuse unless every branch with commits is pushed; never commit on the owner's behalf.                         |
| Q6  | How much of `untracked/` and `.claude/worktrees/` travels?                                                 | **Worktrees: all.** `untracked/`: everything except reference clones and plan run logs, with a per-run override.                      |
| Q7  | Fence on machine-A: rename aside, delete, or a launcher tombstone?                                         | **Rename aside** plus registry records removed; delete stays a manual owner step later.                                               |
| Q8  | Teams: move the homeserver with the team, or wait for remote seats?                                        | **Move the homeserver** (backup, play on machine-B, restore, remove on machine-A), keeping name, `server_name`, port and bus address. |
| Q9  | One member while its team stays behind?                                                                    | **Refuse** until 00220 plus v2 remote seats.                                                                                          |
| Q10 | Should import start the sessions, and set them going?                                                      | **Start detached at the prompt, no `set-going`**: the owner looks first, since the first cold turn costs a full context.              |
| Q11 | Where do the verbs live?                                                                                   | **`ccy-sessions export/import`** for checkouts; **`ccy-teams migrate`** to drive a whole team (homeserver plus members in order).     |
| Q12 | Should the ccy repo commit be required to match on both machines?                                          | **Machine-B not older than machine-A** (ccy version and Claude Code version), refused otherwise.                                      |
| Q13 | Build export on the Plan 00203 Task 3.17 last-shutdown record (and settle its Decisions 18, 19, 21 first)? | **Yes**; one snapshot format for "shut down, then come back", wherever "back" is.                                                     |

---

## 5. Options, smallest workable first

Each option's effort and risk are relative; no time is estimated.

| #   | Option                                                                                                                                                                                      | Effort                                                                                                                                                           | Risk                                                                                                                                                                                                                                                            | Gives                                                                 |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| O0  | **No tool:** stop the session, copy the checkout to the same path, `ccy --export-token`, then `ccy --resume <id>` (id from `conversation.json`) on machine-B                                | none                                                                                                                                                             | High: no liveness gate, no fence (both machines can resume one conversation), plaintext copy over whatever channel is used, easy to carry `sessions/*.json` or `ccy.env.local` by mistake. Teams cannot be moved this way. Worth knowing; not a recommendation. | solo ccy sessions                                                     |
| O1  | **`ccy-sessions export`/`import`, same path, encrypted bundle file, solo sessions only**                                                                                                    | Small to medium: new verbs reusing `registry`, `conversation`, `sessprobe`, `tmuxccy`, `restore`; the Task 3.17 snapshot; a play edit adding `age` (and `rsync`) | Low to medium: the remaining unknown is the conversation load after a copy (lookup verified, load inferred). An acceptance test on a vmtest guest as machine-B would close it.                                                                                  | the owner's main ask for non-team sessions                            |
| O2  | O1 plus **teams by moving the homeserver** (`ccy-teams migrate`: gate the whole team, backup, host_vars on machine-B, play, restore, remove on machine-A, import members coordinator first) | Medium: root-run steps through a play or `agent-bus-install`; carrying the backup is new; the host_vars move is the owner's                                      | Medium: root-of-trust secrets in transit; "same bus address on both machines" must be confirmed in the installer; the seat-handle `<host>` question must be settled                                                                                             | whole teams, with no change to v1's one-machine rule                  |
| O3  | O1/O2 plus a **different checkout path** (re-key session names and `pingbus/sessions/`, rewrite `dir=`) and **`cc` sessions** (slug rename)                                                 | Medium                                                                                                                                                           | Medium: more rewriting means more ways to resume the wrong conversation; `cc` also touches host `~/.claude.json` trust and the D25 seat store                                                                                                                   | flexibility                                                           |
| O4  | O1/O2 with an **ssh/rsync pull over the WireGuard overlay** instead of a bundle file                                                                                                        | Medium, on top of Plan 00220 Phase 1 (overlay and sshd plays)                                                                                                    | Medium: a new network service on a laptop; firewalld zone design (00220)                                                                                                                                                                                        | fewer manual steps, with large `untracked/` trees moved incrementally |
| O5  | **Move one member** while its team and homeserver stay on machine-A                                                                                                                         | Large: 00220 overlay, v2 remote seats (00209) or the 00197 broker, and lifting D46                                                                               | High: cross-machine seat fencing (today only a local `flock`); plain-HTTP bus off-machine without the overlay                                                                                                                                                   | load spreading per member, not per team                               |

**Recommendation:**

1. Build **O1** first, on the Plan 00203 Task 3.17 snapshot, with Q1 to Q7 at their defaults,
   and prove it with a vmtest guest acting as machine-B.
2. Then build **O2**. It moves a whole team without touching the bus's one-machine design.
3. Leave **O5** to the 00220/00209 path that is already planned.
