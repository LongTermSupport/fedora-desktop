# Why `--continue` no longer reaches the coordinator session

Investigator: Opus, read-only. No code changed, nothing committed. Claude Code 2.1.303 in the
container. No transcript message content was read or quoted: the evidence below is file
names, sizes, mtimes, field names and ids. The Claude Code logic was read from the strings in
the shipped binary (`/usr/local/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`),
dumped to `untracked/scratch/cc-strings/all.txt`.

## Short answer

On 2026-10-09 at 08:37:02Z the coordinator conversation was sent to the background with
Claude Code's `/background` command. That forked it into a daemon-hosted background session,
`d2dae64d-90a7-46a0-bebd-7089855013e5`. The container ended while that background session was
stuck on the weekly limit. Claude Code's record of it as a *live background session*
(`~/.claude/sessions/1360335.json`, `kind: "bg"`) was left on disk.

Each ccy container gets a new PID namespace. Claude Code cannot prove a record from another
PID namespace is dead, so it treats the record as live, and only a process in that same
namespace may sweep it. That namespace is gone, so the record never expires. While it exists,
`--continue` skips `d2dae64d` as "running in the background". It then reaches the
pre-background conversation, sees its `continued-in → d2dae64d` marker, and refuses.

Run_in_background sub-agents did not cause this. The cause is the `/background` /
agent-view feature. It is controlled by the `disableAgentView` setting
(or `CLAUDE_CODE_DISABLE_AGENT_VIEW=1`).

## 1. How `--continue` picks a conversation (proven from the binary)

`claude --help`: `-c, --continue  Continue the most recent conversation in the current directory`. The picker is function `Mlo` (called through `ixe`):

1. It lists every live session record (`listAllLiveSessions`, read from `~/.claude/sessions/*.json`).
   For every record whose `kind` is not `"interactive"`, the `sessionId` goes into a set `h`
   ("held by a background session"). If that record's `pidDomain` differs from this
   process's own, the id also goes into `y` ("liveness unprovable").
2. In `listAllLiveSessions` (`Aee`), a record with a foreign `pidDomain` is counted as
   **live without any check**. A dead record is only swept when its `pidDomain` equals
   the caller's (`$6n(w,s)` and the pid is dead).
3. It walks the transcripts newest first:
   - if a transcript's own session id is in `h`, it is skipped;
   - if a transcript carries a `{"type":"continued-in","continuedInSessionId":X}` line and X
     is in `h`, the walk **stops**. The error comes from `Odt`. Where X's liveness is
     unprovable, the text is:
     "Your most recent conversation moved to a background session (X) registered from another
     machine or container, so this one can't tell whether it is still running. Use `claude --resume` to pick a session, or add --fork-session to branch off a copy."
     (The provable variant says "is running in the background … Use `claude agents`".)
4. `--fork-session` bypasses the check (`if(n)return!0`), but it starts a *new* session id.

## 2. The evidence in this project's state

`<home>/.claude` resolves to `<repo>/.claude/ccy`, which is the project's own ccy state on the host.

Transcripts in `<home>/.claude/projects/-workspace/`, newest first (top-level `.jsonl` only):

| File (session id) | Size    | mtime (local)    | Fields of note                                                           |
| ----------------- | ------- | ---------------- | ------------------------------------------------------------------------ |
| `d2dae64d-…`      | 137 MB  | 2026-10-19 10:59 | `entrypoint: cli`, `sessionKind: "bg"`; **the coordinator now**          |
| 16 files `…`      | ~600 KB | 10-15 to 10-16   | `entrypoint: sdk-cli` (headless/SDK runs), first line `queue-operation`  |
| `dc2673b8-…`      | 36 MB   | 2026-10-09 10:23 | `entrypoint: cli`; one `continued-in` line → `d2dae64d` at 08:37:02.982Z |
| `76e6d7f5-…`      | 8 KB    | 2026-10-09 09:37 | `sessionKind: "bg"`, a stub from the same dispatch minute                |
| older `cli` ones  |         | ≤ 10-03          | earlier conversations                                                    |

No transcript carries `isSidechain` lines. Sub-agent transcripts live in per-session
subdirectories and play no part.

Session registry, `~/.claude/sessions/*.json` (fields only):

| Record         | kind          | sessionId  | pidDomain (prefix) | notes                                                   |
| -------------- | ------------- | ---------- | ------------------ | ------------------------------------------------------- |
| `1360335.json` | `bg`          | `d2dae64d` | `04ab7982…`        | `jobId: d2dae64d`, started 2026-10-09 08:37Z, **stale** |
| `103.json`     | `interactive` | `d2dae64d` | `751d1d56…`        | 2026-10-16 container                                    |
| `104.json`     | `interactive` | `d2dae64d` | `d3d2392a…`        | **this container** (pid 104), started 2026-10-19 08:16Z |
| `72.json`      | `interactive` | `dc2673b8` | `34fb7081…`        | the pre-background coordinator, 2026-10-05 container    |

Background job state, `~/.claude/jobs/d2dae64d/state.json`: `template: "bg"`,
`backend: "daemon"`, `interactiveLineage: true`, `state: "blocked"`, `needs: "rate limited … weekly limit"`, `createdAt 2026-10-09T08:37:02.623Z`, `updatedAt 09:23:34.644Z`.
`~/.claude/daemon/roster.json` still lists worker `d2dae64d` with `dispatch.source: "slash"`,
`launch.mode: "resume"`, `fork: true`, `seed.intent` (a short typed phrase). The
`daemon.status.json` supervisor pid (1364203) belongs to a container that no longer exists.
The job timeline shows the background session working from 08:37Z until it hit the weekly
limit at 09:23:34Z. Nothing was written after that.

`claude agents --json` (read-only) now reports two entries for the one conversation:
`{id: d2dae64d, kind: "background", state: "blocked"}` and `{kind: "interactive", pid: 104, status: "busy"}`. Claude Code still believes the background copy is alive.

This session was launched as `claude … --dangerously-skip-permissions --resume` (bare
`--resume`, the picker): `/proc/104/cmdline`, `ps` pid 1.

## 3. Which conversation `--continue` picks now, and why it is not this one

The coordinator's transcript is `d2dae64d`, the newest and largest, and the walk takes it
first. Its id is in `h` because of the stale `1360335.json` `bg` record, so it is skipped.
The `sdk-cli` transcripts are passed over. This is inferred from the behaviour: had they been
eligible, `--continue` would silently open one of them, not refuse. The walk then reaches
`dc2673b8`, whose `continued-in` names `d2dae64d`, which is in `h` and in `y` (foreign
`pidDomain`), so it stops with the "registered from another machine or container" refusal.
**`--continue` picks nothing.** It refuses, which matches what the owner sees.

### Proven vs inferred

- Proven: the stale `bg` record, its foreign `pidDomain`, the `continued-in` marker, the
  selection code that skips and stops on them, the sweep rule that only same-domain processes
  may remove a record, `claude agents` still listing the job as live, and the dispatch from a
  slash command at 08:37:02Z.
- Inferred: that the owner typed `/background`. The roster says `source: "slash"`, and a model
  cannot type slash commands. Also inferred: that `sdk-cli` transcripts are excluded from the
  `--continue` list. Not determined: exactly how the container ended at about 09:23Z.

## 4. Fix options, ranked

### A. Make this conversation `--continue`-able again (no new session)

The trap is the one stale file `~/.claude/sessions/1360335.json`. While it exists, every future
container refuses `--continue`. Once it is gone, `h` no longer holds `d2dae64d`, and the newest
transcript (`d2dae64d`, with no `continued-in` of its own) is picked.

- Supported CLI route: `claude rm d2dae64d` ("Delete a background session … works on
  already-exited sessions"; per `claude stop`'s own text, rm removes "worktree and job
  state", not the conversation). **Untested and probably refused.** `rm` first asks the daemon
  to kill the worker (`TAe` with `evict`), and with no daemon for that namespace it falls back
  to a liveness check that faces the same unprovable foreign-domain record. It would then
  print "couldn't remove d2dae64d — …" and change nothing. Nobody has tried it. `claude stop`
  goes the same way.
- The certain route is removing the stale record. That file is Claude Code's own runtime
  state, not system configuration, but the project's IaC rule still applies to how it is done.
  The durable fix is in the launcher (C below), so the owner gets it with a deploy and not a
  hand edit.

### B. Have ccy resume by explicit id (strongly recommended, independent of A)

What the launcher does today:

- Supervisor restarts (`lib/restart-request.bash`, `ccy_restart_relaunch_args`) already strip
  `-c/--continue/-r/--resume/--session-id` and relaunch with `--resume <session_id>`, the id
  taken from the supervisor's `restart-request.json`. That path is immune to this trap.
- Reboot restore (`lib/session-registry.bash` `ccy_registry_restore_args`, and its Go
  twin `src-go/internal/registry/restore.go` `RestoreArgs`) appends **`--continue`** unless the
  recorded args already continue or resume. The registry record (`ccy_registry_write`: name, dir,
  launcher, prefix, restore, compose, arg=…) holds **no conversation id**. The restore manifest's
  `resume` field documents `--continue` as "the newest transcript in the project", which is
  exactly the assumption this trap breaks. With the stale record present, a reboot restore of
  this coordinator would fail to come back.

The fix: record the live conversation id in the session's registry record and restore with
`--resume <id>`. The id changes over a session's life (`/clear`, `/background`, fork: here
`dc2673b8` became `d2dae64d`), so it must be kept current, not captured once at launch. Two
places know it: the `sessionId` in `~/.claude/sessions/<claude-pid>.json` for the claude
process ccy started, and the SessionStart hook's `session_id`, which fires on startup,
resume, clear and compact. The supervisor plugin already gets it for restart requests.
`--continue` should remain only as the fallback for a record with no id. It has a second
hazard of its own: once these exclusions change, a newer headless/`sdk-cli` transcript or
another session in the same project directory could be the "most recent".

### C. Stop the trap recurring: disable agent view in ccy, and sweep stale records

- `/background`, `--bg`, `claude agents` and the on-demand daemon are all switched off by the
  setting **`disableAgentView: true`** (binary's schema: "Disable agent view (`claude agents`,
  `--bg`, /background, the on-demand daemon) … Equivalent to CLAUDE_CODE_DISABLE_AGENT_VIEW=1").
  Neither is set anywhere in the repo or in `<home>/.claude/settings.json`. In ccy this
  feature cannot work properly: its daemon dies with the container, and its records cannot
  outlive the container's PID namespace. So disabling it in the ccy-managed settings or
  environment (Dockerfile / entrypoint, with the usual CCY version bump) is justified and
  costs nothing ccy uses. No repo code calls `--bg`, `claude agents` or `attach`.
- **Do not** disable run_in_background sub-agents or background shell tasks
  (`disableBackgroundTasks` / `CLAUDE_CODE_DISABLE_BACKGROUND_TASKS`). They are in-session tasks,
  they write no `~/.claude/sessions` `bg` record, and they had no part in this. The coordinator
  workflow depends on them.
- Not proven: whether `disableAgentView` also stops `--continue` from honouring an existing
  stale record. The `Mlo` code reads the live list either way. So disabling prevents new traps
  but does not clear this one.
- Optional hardening: before starting the container, the host launcher could remove
  `kind != "interactive"` records under the project's `.claude/ccy/sessions/` when no container
  for that config dir is running. Every such record is then dead by definition. This touches
  Claude Code's internal file layout, so B and C (the setting) are the better primary fixes.
  Caveat: only safe when no other container shares that `.claude/ccy`.

### D. End the session and start fresh (last resort, not needed)

Not needed. `--resume d2dae64d-90a7-46a0-bebd-7089855013e5` (or the picker) opens this
conversation today without trouble. A new session would still leave the stale record in
place. Its own transcript would then be newest, and `--continue` would work for it only
while nothing else in the project is newer.

## 5. Implications for a "restart the whole team" command

Yes, it should always use `--resume <id>`, never `--continue`:

- Several ccy sessions in one project directory share one transcript directory, so "most
  recent in the current directory" picks the wrong member or refuses.
- One stale background record (from this incident or a future one) turns `--continue`
  into a refusal for the affected lineage.
- Headless `sdk-cli` transcripts accumulate beside the interactive ones.

The command needs each member's *current* conversation id, kept up to date in the registry
(as in B). The supervisor's restart path already shows the pattern: strip any
continue/resume flag and append one `--resume <id>`. A member with no recorded id should
make the command refuse, naming that member, and not fall back to `--continue`. That
follows fail fast.

## Recommended fix

1. Now: keep launching this coordinator with `ccy --resume d2dae64d-90a7-46a0-bebd-7089855013e5`
   (explicit id, no picker). This works today.
2. IaC, a single CCY change with a version bump: set `disableAgentView: true` (or
   `CLAUDE_CODE_DISABLE_AGENT_VIEW=1`) in the container's Claude settings/environment, so
   `/background` can no longer create these records inside ccy.
3. IaC, the registry: record and maintain each session's conversation id, and make restore
   (bash `ccy_registry_restore_args` and Go `registry.RestoreArgs`) and any team-restart
   command use `--resume <id>`. Fix the manifest doc that equates `--continue` with "the
   newest transcript".
4. To make `--continue` itself work again for this conversation, the stale
   `.claude/ccy/sessions/1360335.json` (and its `jobs/d2dae64d/` and roster entry) must go.
   Either try the supported `claude rm d2dae64d`, which is expected to be refused (see 4A), or
   have the launcher's stale-record sweep (4C, optional hardening) remove it on the next launch.
