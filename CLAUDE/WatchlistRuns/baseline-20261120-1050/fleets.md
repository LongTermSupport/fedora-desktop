# Fleet orchestrators: Orca, Multica, Agent Orchestrator, Emdash, intentic, Codeman

Plan 00258 (Task 2.1), group "fleets", written for the watch-list baseline run. A study of six open-source products that run many coding
agents at once, compared against ccy, ccy-teams, the agent bus (pingbus / teamchat) and the hooks
daemon.

Method: each clone under `untracked/starred-repos/` was read, never built or run. Paths below are
relative to the repository named in each section's heading. Activity figures come from `git log`
on the clone on the day of writing; two clones (Orca, intentic) were still shallow (one commit),
so their history could not be measured. Statements marked **(guess)** are inferences, not read
from the source.

Licence classes follow [Watchlist.md](../../Watchlist.md): permissive = may lift with notice;
Multica = clean room.

## Contents

1. [Common ground: what all six are](#common-ground)
2. [Codeman](#ark0ncodeman)
3. [Agent Orchestrator](#orchestratorincagent-orchestrator)
4. [Orca](#stablyaiorca)
5. [intentic](#intenticintentic)
6. [Emdash](#generalactionemdash)
7. [Multica (clean room)](#multica-aimultica)
8. [Group takeaways, ranked](#group-takeaways-ranked)
9. [Proposed issues and plans](#proposed-issues-and-plans)

## Common ground

All six converge on the same product shape, which is itself a finding:

- **One worktree (or directory) per agent task**, a board of sessions sorted by "needs you", and a
  review step before work lands. ccy's unit is a container on one checkout; none of these
  isolates by container by default (intentic and Codeman's Docker cases are the exceptions).
- **Many agent CLIs behind one registry** (Codeman 11, Emdash 37, Multica 31, Agent Orchestrator
  ~45). ccy runs Claude Code only. This is a deliberate difference, not a gap, unless the owner
  wants other CLIs in ccy.
- **Agent status from hooks first, screen-reading second.** Every product installs agent hooks
  posting to a local server; Emdash refuses to infer status from the terminal at all, Codeman
  reads the screen heavily as a fallback, Orca adds the OSC 7501 terminal status protocol.
- **A phone surface** (Codeman PWA, AO and Orca native apps, intentic and Multica web/mobile).
  Our equivalent is Element / Lightning on the team bus.
- **Agents drive the tool through its own CLI or MCP server** (`codeman agent`, `ao`, `orca orchestration`, `multica`, intentic's in-sandbox `capabilities`/`needs`). pingbus is our
  equivalent.
- **None has what the agent bus has**: a multi-machine, multi-human team room on a self-hosted
  Matrix server, with seats and a coordinator, readable by people in an ordinary chat client.
  Multica (issue board + squads) and Orca (Runs/Tasks/Dispatches) are the closest.
- **None has a hooks daemon equivalent** for safety policy on the agent's own tool calls; their
  hooks are for status only. intentic's command judge and host guards are the nearest
  (section 4).

---

## Ark0N/Codeman

Studied: baseline at 5548c17c4aa300f4f86be6ef90f5ac5ab853a048

**What it is.** A self-hosted "mission control" web server (Node/TypeScript, Fastify, node-pty,
xterm.js) that spawns agent CLIs inside persistent tmux sessions and streams the real terminal to
any browser, phone included (`README.md`, `CLAUDE.md`). For a single developer or small trusted
team who wants long unattended runs and phone access.

**Architecture.**

- Process model: one Node server; each session is a tmux pane on a private tmux socket
  (`tmux -L codeman`), so agents survive server restarts (`src/tmux-manager.ts`,
  `src/config/instance.ts`). A pluggable CLI registry (`src/config/cli-registry/`) describes each
  CLI's launch argv, env prefixes and screen-detection patterns as data.
- Isolation: none by default (agents run as the user, on the host). Optional "Docker cases": one
  hardened container per case with `--cap-drop ALL`, `no-new-privileges`, non-root, pids and
  memory caps, never the socket; a "sealed" profile adds `network:none` and no host credentials
  (`docs/docker-cases.md:185`). Credentials are copied in, never shared read-write.
- Supervision: a respawn controller detects idle (completion text, an AI check, output silence,
  token stability) and cycles continue / `/clear` / `/init`, with a circuit breaker and health
  score (`src/respawn-controller.ts`, `docs/respawn-state-machine.md`). A PTY-exit breaker stops
  restart loops (`src/session-pty-exit-breaker.ts`).
- **Usage limits**: opt-in auto-resume parses Claude Code's limit banner (every observed wording,
  1.0.x to 2.1.x, with IANA time zone), waits until reset plus two minutes, dismisses the dialog
  and sends `continue`; respawn and auto-compact are held while paused so `/clear` cannot wipe the
  waiting conversation (`src/usage-limit-patterns.ts`, `src/session-auto-ops.ts`).
- **Prompt-cache keep-warm**: one tiny nudge turn five minutes before an idle session's prompt
  cache would expire, gated by a long list of guards (empty composer, no dialog, no limit pause,
  inside a cap), using the statusline's `prompt_cache` object (`src/prompt-cache-keepwarm.ts`).
- Input safety: a submit verifier re-presses Enter only while the composer verifiably still holds
  what was sent (Claude Code ignores Enter for 30-50 s after paint, per their measurement)
  (`src/session-submit-verifier.ts`); trust-dialog auto-accept is screen-matched with attempt
  caps (`src/session-trust-dialog.ts`); process-tree walks are bounded by depth and node count
  (`src/proc-tree.ts`).
- Multi-agent: `codeman agent` verbs and an MCP server let one agent spawn workers, send
  exactly-once prompts, block on `wait` until a turn's `stop`, read the last response from the
  transcript, and keep a per-session inbox (`src/cli-agent.ts`, `src/mcp-server.ts`,
  `src/web/agent-inbox.ts`). An "orchestrator loop" state machine plans phases, waits for human
  approval, verifies each phase (`docs/orchestrator-loop-architecture.md`). It also watches
  Claude Code's native agent teams (`~/.claude/teams/`) so idle detection does not fire while
  teammates work.
- Human in the loop: tab colours (yellow idle, red blocked), an Approvals Inbox answering Claude
  permission prompts from the browser or a push notification, re-captured from the pane before
  any answer is typed (`src/web/approval-inbox.ts`); a two-way Discord bridge, one thread per tab
  (`src/discord-bridge.ts`).
- Credentials and accounts: `CLAUDE_CONFIG_DIR` per session for separate accounts; plan usage
  (5-hour and weekly) in the header from a statusline exporter injected by `--settings`, never
  written to the user's files.
- State: JSON files under `~/.codeman` with timestamped config backups.

**Maturity.** 4,091 commits since 2026-01-18; 2,241 in the last 90 days; 49 authors (40 in 180
days, one "maintainer" identity dominant, likely agent commits **(guess)**); 212 release tags,
latest `codeman@1.51.1` on 2026-11-20. Very fast-moving, single-maintainer-led.

**Licence.** MIT (`LICENSE`, "Codeman Contributors"). Sub-licences: `packages/xterm-zerolag-input/LICENSE`
(MIT), Nerd Fonts licence under `src/web/public/fonts/`. Reuse class: **lift**.

**Compared with us.**

- Ahead: auto-resume at a usage-limit reset (we show limits, `ccy --token-usage`, but do not act
  on a limit); prompt-cache keep-warm; a phone-first terminal; a mature submit/Enter verifier;
  approvals answerable remotely.
- Behind: no container by default, no network policy, credentials copied wholesale into Docker
  cases, no team bus between machines and humans, no safety policy on tool calls. Its
  screen-scraping (the Claude spinner, the composer glyph) is fragile by its own account
  (`CLAUDE.md`, "A ❯ sighting is NOT the end of a turn").
- Different: Codeman is a dashboard over tmux; ccy is a launcher plus container. Its agent-to-agent
  verbs are local HTTP calls, where ours are Matrix messages across machines.

**Ideas worth adopting.**

1. **Hold the session at a usage limit and resume at reset.** Parse the reset time (or better,
   use the statusline's `rate_limits.*.resets_at`, as Plan 00251 found), hold every automated
   input meanwhile, then resume. Lands in the hooks daemon's usage pause / the ccy supervisor and
   in Plan 00251's `rate-limited until` state; pairs with Plan 00146's token switch (switch first,
   wait only when every token is spent). **Plan** (decision: wait vs switch token). Already noted
   as a non-goal in 00251's research, line 253; this is the follow-up.
2. **Keep-warm nudge for idle seats.** A seat waiting on the coordinator for more than the cache
   TTL pays a full re-cache on its next turn. Lands in the ccy supervisor. **Plan** (cost vs
   value, needs measurement; easy to waste usage).
3. **Bounded process-tree walk** as a rule, with named caps and a truncation report. We walk
   process trees in ccy-sessions / stop paths **(guess: verify)**. **Issue** if we have an
   unbounded walk; a Go project rule is a candidate.
4. **Re-capture before answering**: any automated answer to a permission dialog must re-read the
   pane and match the options shown now. Relevant only if we ever answer dialogs from Element.
   Note for Plan 00251.

**Code worth lifting (MIT, keep notice).**

- `src/usage-limit-patterns.ts` (210 lines, pure): the catalogue of Claude Code limit-banner
  wordings and the reset-time parser. Port to Go as test fixtures and a parser, if we parse the
  screen at all. The wordings list in its file header is the valuable part.
- `src/proc-tree.ts` (120 lines): the bounded descendant walk, small enough to port.

**Risks / avoid.** Its sheer surface (local models, gesture control, Discord) is the opposite of
our scope. The `curl | bash` installer and in-app self-update are patterns we deliberately do not
use. Its agent-facing `CLAUDE.md` is enormous (incident history inline): an example of what our
docs rules prevent.

---

## OrchestratorInc/agent-orchestrator

Studied: baseline at cab1f8d1570ea64eab9aeb8daa1ffd992a2b2425

**What it is.** "Orchestrator.inc": an Electron desktop app plus a Go daemon (`ao`) that runs a
fleet of coding agents, each in its own worktree and branch, and routes CI failures, review
comments and merge conflicts back to the agent that owns the branch (`README.md`). Mobile app,
optional cloud workers. For developers running many PRs in parallel.

**Architecture** (`docs/architecture.md`, `CONTEXT.md`).

- Process model: Go daemon on `127.0.0.1` (REST, SSE, terminal WebSocket), Electron and Expo
  clients, `ao` CLI and an MCP server (`backend/cmd/ao`, `backend/cmd/ao-mcp`). Ports-and-adapters
  layout (`backend/internal/ports`, `backend/internal/adapters`).
- **Durable facts, derived status**: the database stores only facts (`activity_state`:
  active/idle/waiting_input/blocked/exited, `is_terminated`, PR facts); the board status
  (working, needs_input, ci_failed, mergeable…) is computed at read time with a fixed precedence
  (`docs/architecture.md:49-65, 824-861`). A lifecycle manager is the one write path for facts.
- **Change data capture**: SQLite triggers append to a `change_log` table; a poller tails it with a
  watermark and fans out to SSE (`docs/architecture.md:236-248, 804-820`).
- Observers separated from actors: an SCM observer polls GitHub/GitLab; a runtime reaper probes
  panes. **"Failed probes are not proof of death"**: a session is terminated only when runtime and
  process are both clearly dead and no recent activity contradicts it (`docs/architecture.md:962-985`).
  Processes are identified by pid plus start time, and a force-stop captures the whole process
  tree before teardown (`docs/architecture.md:205-234`).
- Runtimes: native PTY, ConPTY, tmux; Git worktree or standalone directory. No container or
  sandbox (`grep` for podman/bubblewrap finds only a "docker agent" adapter).
- **Input guard**: every write into a live pane goes through one guard that re-reads the session
  under a lease and refuses when the session is blocked on a permission dialog, because the
  appended Enter would answer it (`backend/internal/sessionguard/guard.go:1-12`).
- Chat mode queues messages in SQLite while a turn runs; "steer" folds queued guidance into the
  active turn (`docs/architecture.md:691-720`).
- **Accounts**: several Claude accounts as separate `CLAUDE_CONFIG_DIR` folders that symlink the
  shared history (`projects`, `sessions`, `todos`, `plans`…) so `--resume` works across accounts,
  drop login-selecting keys from copied settings, and strip credential env vars
  (`backend/internal/adapters/agentaccount/claude.go:22-40`). Usage limits are read by sending
  Claude Code's own `get_usage` control request over stream-json, so the CLI reads its own
  limits with its own login (`claude.go:128-186`). Provider API keys never appear in arguments;
  tmux sessions get them through a 0600 file the pane's shell reads and deletes
  (`backend/internal/providerkeys/store.go:1-5`).
- Human in the loop: phone pairing over an authenticated LAN listener with a rotating password,
  per-source lockout, daemon-control routes loopback-only (`CONTEXT.md`).
- Single-instance data dir lock (`backend/internal/datadirlock/`).

**Maturity.** 4,339 commits since 2026-02-13; 2,007 in 90 days; 165 authors (134 in 180 days);
524 tags, nightly builds daily (latest `v0.15.4-nightly.202611192143`). Large, active,
company-backed. Already on our watch list (`CLAUDE/Watchlist.tsv`, never scanned) and cited by
Plan 00251's research (`RESEARCH-seat-status.md:249`).

**Licence.** Apache-2.0 (`LICENSE`). Per-file: agent icon licences under
`frontend/src/renderer/assets/agents/LICENSE-*.txt` (artwork only). No `NOTICE` file. Reuse
class: **lift** (keep the licence and state changes).

**Compared with us.**

- Ahead: the derived-status model; the CDC event feed; feedback routing from CI/review to the
  owning agent; multi-account with shared history; `get_usage` for limits without an API call.
- Behind: no isolation, no network policy, no multi-human team room, no tool-call safety policy.
- Different: AO's coordinator is the product; ours is an agent seat on a bus. AO's
  "orchestrator" agent and our coordinator seat play the same role.

**Ideas worth adopting.**

1. **Read usage limits with Claude Code's `get_usage` control request** instead of a probe API
   call (ccy docs say the figures "are only available as rate-limit headers on a real API
   response", `docs/ccy.md:921`). This contradicts that and is cheaper and quieter. Lands in
   `ccy --token-usage` and Plan 00251's rate-limited state. **Issue** to verify on our pinned
   Claude Code version first (it is an undocumented control subtype **(guess: not in the public
   SDK docs)**).
2. **Account folders that share history**: one `CLAUDE_CONFIG_DIR` per token with the
   conversation directories symlinked to a shared tree, so `--resume` crosses token switches.
   Relevant to Plan 00146 (token switch) and the token pool. **Plan** input (00146 may make it
   moot with its forwarder).
3. **Pane-write guard**: one tested choke point that refuses automated input into a seat blocked
   on a dialog. Our deliveries into ccy sessions (pingbus wake, supervisor compaction) should go
   through such a guard. **Issue** (check how the supervisor and pingbus deliver into the pane).
4. **"Failed probes are not proof of death"** with pid + start-time identity. Applies to
   ccy-sessions' orphan detection and Plan 00256's idle stand-down. **Issue**, or a project rule
   for Go (`os.FindProcess` without start-time identity) under Defence Before Fix.
5. **Durable facts, derived status** for Plan 00251: publish facts from the seat, derive
   idle-locked / gone in the reader. Already adopted in 00251's design; keep it.

**Code worth lifting (Apache-2.0).**

- `backend/internal/processalive/process_unix.go`: liveness with EPERM-as-alive and zombie
  detection. Small; our Go could use the same rules.
- `backend/internal/adapters/agentaccount/claude.go:22-40` (the shared-directories and
  dropped-settings lists) and `:128-186` (the `get_usage` exchange): reference for 00146.
- `backend/internal/datadirlock/` (flock wrapper, Unix/Windows): we already have flock in Plan
  00237; reference only.

**Risks / avoid.** The daemon's loopback listener is unauthenticated by design ("safe only because
nothing off-box can reach it", `CONTEXT.md`); a ccy container sharing the host network namespace
would break that assumption, so do not copy the pattern into anything a container can reach.
Nightly-tag churn makes the watch-list scan noisy; scan `main` by date, not tags.

---

## stablyai/orca

Studied: baseline at 2d268efee716f69a21503d7d50ec81bc74b0dc32

**What it is.** An Electron "AI orchestrator" IDE: any coding agent in parallel worktrees, chat or
raw terminal, built-in browser, diffs, Git/Linear/Jira integrations, mobile app, remote servers and
per-worktree cloud VMs, and a CLI agents use to orchestrate other agents (`README.md`). Very
large: 38,428 tracked files.

**Architecture** (sampled).

- Process model: Electron main (`src/main/`, about 130 subsystems), a separate daemon `orcad`
  (`src/main/orcad/`: instance lock, host runtime, relay, credentials), CLI (`src/cli/`), mobile
  app (`mobile/`). Remote "Orca servers", SSH hosts and ephemeral VMs
  (`src/main/ephemeral-vm/`: suspend, resume, lost-contact, cleanup).
- **Agent status**: two channels into one status store
  (`src/main/agent-hooks/README.md`): managed per-agent hooks posting to a loopback hook server,
  and **OSC 7501, the "Program Status Protocol"**, where the agent writes its own state
  (working/idle/blocked with kind permission/question/auth, a message, child records) into the
  terminal stream after the terminal answers a probe. Their matrix records Claude Code supporting
  it since 2.1.295, with all five states, all three blocked kinds and subagents as children;
  hooks take precedence, OSC 7501 fills gaps (no hooks installed, a fresh SSH host, an agent run
  by hand). Rule in `server/program-status/hook-precedence.ts`.
- **Orchestration** (`docs/site/content/docs/cli/orchestration.mdx`,
  `skill-guides/orchestration/`): a **Run** (namespace and coordinator inbox), **Tasks** with
  dependencies (a DAG; `pending/ready/dispatched/completed/failed/blocked`), a **Dispatch** (one
  attempt of a task on one terminal, which alone may report `worker_done`), **messages** typed
  (`status`, `worker_done` with `--outcome succeeded|failed`, `escalation`, `question`,
  `heartbeat`), and **decision gates** (a coordinator-owned question that blocks a task). The
  coordinator ends its turn after starting workers and is woken when a worker reports, asks or
  stops. A worker stuck on a permission prompt is escalated; only a person may allow it unless the
  user opts in to let coordinators approve, and then the first answer wins. Every command prints
  a short receipt ending with the next step; `--json` gives one line.
- Accounts and limits: multiple Claude, Codex, Cursor, Grok accounts (`src/main/claude-accounts/`,
  `codex-accounts/` …); a rate-limit service polls each provider
  (`src/main/rate-limits/`), for Claude via an OAuth usage endpoint
  (`src/main/rate-limits/providers/claude/claude-oauth-usage-request.ts:22`, an internal
  endpoint, undocumented **(guess)**). Usage reports parsed from transcripts
  (`src/main/claude-usage/`).
- Isolation: worktrees and optional cloud VMs; no local container sandbox seen in the sample.

**Maturity.** Clone was shallow (one commit, 2026-11-20); history not measured. Tag-less clone.
Large codebase with a docs site and published apps suggests a funded product **(guess)**.

**Licence.** MIT (`LICENSE`, Lovecast Inc.). Bundled third-party: `resources/licenses/`
(ripgrep, shadcn), `mobile/packages/expo-two-way-audio/LICENSE`, theme plugins under
`resources/plugins/launch/*/LICENSE`, `docs/site/THIRD_PARTY_NOTICES.md`. Reuse class: **lift**
(check the file is not one of the bundled third-party parts).

**Compared with us.**

- Ahead: the orchestration vocabulary (Run/Task/Dispatch/gate) is a sharper version of what our
  coordinator does with free text and pings; the OSC 7501 status channel; outcome-carrying
  completion messages; receipts that end with the next command.
- Behind: no container isolation or network policy locally; no human team room.
- Different: Orca orchestrates within one owner's machines (paired servers); our bus spans
  machines and people.

**Ideas worth adopting.**

1. **OSC 7501 as a seat-status source for Plan 00251.** If Claude Code really emits program status
   when the terminal answers the probe, the ccy supervisor (which owns the pane through tmux) can
   read working/idle/blocked(kind) without hooks or screen-scraping, and it works for a session
   whose hooks are off. Lands in Plan 00251 Task 1.4 probes. **Plan** input: verify on our pinned
   version, and check tmux passes the probe and the reply (tmux may swallow unknown OSC;
   **(guess)**). Read the spec named in the README before designing.
2. **Typed completion messages with an outcome.** A pingbus verb for "done" that carries
   `succeeded|failed`, files touched, and the task/request id it closes, so the coordinator's
   view (00251) closes requests deterministically. Lands in the agent bus protocol
   (`docs/agent-bus-protocol.md`). **Plan** (protocol change).
3. **Decision gates**: a coordinator-owned question that blocks named work until a human or the
   coordinator answers, visible as a state, not as chat scrollback. Our questions room is the
   transport; the missing piece is the blocking state. **Plan** (with 00251).
4. **Receipts that end with the next step** for pingbus and ccy-teams command output. Fits our
   CLI style rules. **Issue**.
5. **Escalation rule**: a worker blocked on a permission prompt is reported to the coordinator,
   who works on, and only a person allows it unless the owner opts in. Our ccy seats run with
   bypass permissions, so this mainly matters for host `cc` seats (Plan 00255/00226). Note.

**Code worth lifting (MIT).** `src/shared/program-status/` (the OSC 7501 parser and state
mapping) if idea 1 proves out, ported to Go. Nothing else is worth porting from a TypeScript
Electron app.

**Risks / avoid.** The OAuth usage endpoint is internal and could break or be against terms
**(guess)**; prefer `get_usage` (AO) or statusline data. The product's breadth is a warning about
scope.

---

## intentic/intentic

Studied: baseline at 4e61e599edfacd623cb0fdba997ccc079a2c2838

**What it is.** An open-source workspace for coding agents where the whole agent daemon runs in a
**Docker sandbox on a machine you control**, reached from browser, desktop or phone over an
outbound tunnel; agents plan first, work in worktrees, and land only after review (`README.md`).
Also a hosted platform (sign-in, relay) and an OS image. Large monorepo (13,731 files).

**Architecture** (`ARCHITECTURE.md`, `docs/architecture/*.md`, `_sandbox/README.md`).

- Process model: per project one container. `intentic-netd` (Rust) is PID 1 inside it: owns every
  port and the tunnel, supervises the Node daemon in its own process group, restarts it with
  backoff, SIGKILLs one that misses a 5-minute ping, logs every kill (`_sandbox/netd/README.md`,
  `docs/architecture/sandbox.md`). The daemon listens only on a Unix socket, so its restart drops
  no connection. A Rust host CLI `ic` runs, updates and repairs sandboxes.
- Agents: one actor per conversation decides whether a message starts a turn, steers the running
  one or queues; a person can "send later" until an account's allowance reopens
  (`_sandbox/sandbox/src/agent/run/turn/holds/allowance-gate.ts`). Several accounts per provider;
  a spent account's queued words are booked for its reopen or moved to another account
  (`.../providers/accounts/switch-account.ts`).
- **Needs** (`docs/architecture/needs.md`): when an agent cannot finish without something only a
  person can give (a connection, a secret's value, wider access, a tool in the image, a standing
  automation, an instruction for `AGENTS.md`), it raises a *need* with one CLI call. The need is
  stored on its own, survives the turn and a restart, shows as a card, and **wakes the
  conversation when met**, however late. The CLI waits up to 90 s and exits 0 met, 1 refused, 3
  still open ("carry on; the answer will arrive"), 2 for no verdict.
- **Credentials** (`docs/architecture/capabilities.md:46-62`): a vault outside the file API; a
  **credential gateway** on `127.0.0.1:8790` where each brokered connector's credential is
  attached; the agent's environment holds an inert placeholder plus a per-conversation signed
  gateway address; per-card method/path rules (`broker.rules`, first match, `ask` holds the
  request on a permission card); upstream taken from the connector's declaration, never the
  request; redirects returned, not followed; git rewritten onto it through `GIT_CONFIG_*`. They
  **chose this over a TLS-intercepting egress proxy** "since the gateway's addresses need no
  trust-store changes and the upstream is never the client's to choose". They state plainly it is
  a hard boundary only once the agent cannot read the vault, which is not yet true (root shell in
  the same container). An **ssh agent** of their own that only lists and signs, per conversation
  socket, with approvers per key. **Host guards** per secret: a use passes unasked only when the
  command is a readable `curl`/`wget`/`git` to a listed host. Secret values are masked in tool
  output as `{{secret:name}}`.
- **Convergence** (`docs/architecture/convergence.md`): six rules for leftovers on unwatched
  machines: everything carries its owner; ownership is a lease; deletion is a record (retire only
  on a positive "gone", after a trash window); converge on a level, not an event; every store has
  a retention; destroy only on positive evidence. Written after finding stale sync sessions,
  orphan volumes and a 40 GB forgotten backup.
- Output cleaners: agent Bash output trimmed with the raw text retrievable, savings ledgered
  (`_sandbox/output-cleaners/`).
- CI gate: `intentic-gate` lets a pipeline wait on a release gate or run an agent turn and exit on
  the verdict (`_sandbox/gate/README.md`).

**Maturity.** Shallow clone (one commit, 2026-11-20); history not measured. Dated design notes
in the docs run through 2026-11 and show active, careful iteration.

**Licence.** MIT (`LICENSE`, 2026 Artur Kurowski). Exceptions: `_extensions/onlyoffice/editor/`
is **AGPL-3.0** (clean room for that directory); `_sandbox/castd/LICENSE` is MIT (Tembo);
`_sandbox/fileq/NOTICE` declares Apache-2.0 material adapted from SurfSense (keep that notice if
lifting fileq); `_tools/oxlint/anti-slop/LICENSE` separate. Reuse class: **lift**, except
`_extensions/onlyoffice/`.

**Compared with us.** The closest in spirit to ccy: the agent lives in a container, the owner's
machine is the host, and the design documents say honestly where the boundary is and is not.

- Ahead: the credential gateway is a working design of exactly Plan 00252; the needs model is a
  better human-in-the-loop than a chat message; the ssh agent that signs per conversation beats
  mounting the key read-only (ccy mounts SSH keys into the container, `docs/ccy.md`); the
  convergence rules; per-account allowance booking.
- Behind: a single container shares the vault with the agent (they say so); no multi-machine team
  of agents and humans; a hosted relay sees traffic in memory when used.
- Different: one long-lived container per project with the daemon inside, versus ccy's
  disposable container per session.

**Ideas worth adopting.**

1. **Plan 00252: adopt the gateway shape, not the TLS proxy.** Placeholder in the env, a
   per-session signed local address per upstream, rules per method and path, upstream fixed by
   config, redirects not followed, git pointed at it with `GIT_CONFIG_*` url rewrites. For GitHub
   via `gh` and `git`, this avoids a CA in the container. **Plan** input to 00252's owner
   decisions (it directly answers the proxy-vs-gateway choice); the gateway must live on the host
   side of the container to be a real boundary, which ccy can do and intentic cannot.
2. **An ssh agent that only signs**, one socket per ccy session, instead of mounting key files.
   Lands in claude-yolo's SSH key handling. **Plan** (security improvement, moderate work; Go has
   `golang.org/x/crypto/ssh/agent`).
3. **Needs as durable asks**: a pingbus/teamchat verb for "I need X from a human" that persists,
   shows in the questions room, and wakes the seat when answered, with exit codes met / refused /
   still open. Lands in the agent bus protocol and ccy-teams. **Plan**.
4. **The six convergence rules** as a checklist for ccy's leftovers (images, containers,
   worktrees, branches, LXC members): Plans 00235, 00250, 00256 already apply parts. Adopt the
   wording "destroy only on positive evidence" and "deletion is a record". **Issue**
   (docs: add to AgentNotes or ContainerRules as a rule set).
5. **Mask secret values in tool output** before the model reads them. A hooks-daemon PostToolUse
   handler could do this for known token values (ccy tokens, GitHub token). **Plan** (hooks
   daemon is upstream; may be an upstream feature request).
6. **Supervisor kills a daemon that misses a liveness ping** and logs every kill; relevant to the
   hooks daemon and the bus link (teamchat). **Issue** if our supervisors only watch exit.

**Code worth lifting (MIT; check each file is outside `_extensions/onlyoffice/`).**

- `_sandbox/sandbox/src/capabilities/broker/` (about 2,900 lines TypeScript): read as the
  reference design for 00252; port ideas and rule semantics to Go rather than code.
- `_sandbox/sandbox/src/capabilities/broker/ssh-agent.ts`: note the OpenSSH 8.9+
  `session-bind` agent-extension pitfall recorded in `capabilities.md:61` (unanswered messages must be
  skipped by length or replies go one late). Valuable even if we use Go's agent package.
- `_sandbox/output-cleaners/src/` (MIT): output trimming heuristics, if we ever want them.

**Risks / avoid.** A same-container vault is not a boundary (they say so). Their docs carry dated
incident notes inline, which our docs rules forbid; take the rules, not the format. The hosted
relay decrypts traffic in memory; nothing comparable belongs in ccy.

---

## generalaction/emdash

Studied: baseline at ae7df31237e3d615a357469800ff6d158dfd11cd

**What it is.** "Emdash Classic": an Electron desktop app for running coding agents in parallel,
each in its own Git worktree and branch, locally or on remote machines over SSH; diffs, PRs, CI
checks; issues pulled from ten trackers (`README.md`). 37 providers, 23 over ACP
(`agents/integrations/providers.md`).

**Architecture.**

- Electron main process with a typed RPC router (`agents/architecture/overview.md`); SQLite
  local state; PTY orchestration (`agents/risky-areas/pty.md`).
- **Remote workspace server** (`apps/workspace-server/`, `agents/architecture/workspace-server.md`):
  a Node daemon on the remote host, reached over an SSH-forwarded Unix socket; installed into
  immutable version directories with an atomic `current` symlink, verified by SHA-256; one
  connection supervisor per host owns retries (ADR 0008).
- **Status from hooks only**: "Emdash does not infer agent status from terminal output. If a
  provider has no hook/plugin integration for an event, the renderer should not show or notify an
  inferred status." Hooks are installed into the provider's **user-global** config with a version
  marker, checked before and under a per-root write lock, written only when missing or stale,
  never into a task worktree, and they exit successfully when the Emdash environment is absent
  (`agents/integrations/providers.md`).
- Decisions recorded as ADRs (`docs/adr/`): the registry is the **single writer** of the
  workspaces table, enforced by lint (0002); **tombstone-and-reconcile deletion** replaces a
  durable command queue for deletes on unreachable hosts (0006); cancellation is best-effort
  across planes (0004, retired).

**Maturity.** 10,661 commits since 2025-09-12; 1,031 in 90 days; 157 authors (60 in 180 days);
206 tags, latest `v1.2.13` 2026-11-19 with `v1.2.14` merged since. Mature, company-backed; the
README now calls it "Classic", pointing at a hosted product, so the open app may slow down
**(guess)**. Already on our watch list, never scanned.

**Licence.** Apache-2.0 (`LICENSE.md`, General Action, Inc.). No per-directory exceptions found.
Reuse class: **lift**.

**Compared with us.** No container isolation; SSH remote is its distribution story. Its strongest
contribution is discipline: hooks-only status, single-writer stores, ADRs.

**Ideas worth adopting.**

1. **Hooks installed with a version marker, written only when stale, checked under a lock, and
   inert outside the product's sessions.** This is the pattern for our hooks-daemon and pingbus
   plugin installs into `~/.claude` on hosts and LXC members. **Issue** to compare against how
   ccy/LXC members get their hook config.
2. **Tombstone-and-reconcile for operations on unreachable machines**: for Plan 00246's
   cross-network members and Plan 00227's cold migration, record the intent on the local row and
   let a reconcile pass complete it when the remote is reachable. **Plan** input to 00227.
3. **Single writer per store, enforced by a lint rule.** Our host_vars edits by `ccy-teams`
   (backing up `localhost.yml`) and the team registry are candidates; a Go analyzer can ban
   direct writes outside one package. **Issue** (Defence Before Fix style rule, if we find more
   than one writer).
4. **Immutable versioned install directories with an atomic `current` symlink** for anything we
   install onto a remote machine (cross-network team members). Note for Plan 00246.

**Code worth lifting.** None needed; the value is in the ADRs (`docs/adr/0002`, `0006`, `0008`)
as design references.

**Risks / avoid.** Global hooks in the user's agent config are touched on every session start;
fine for them, but our hooks are managed by Ansible and must stay there (IaC rule).

---

## multica-ai/multica

Studied: baseline at a6671705899edfc23982f8d965445fc205fc13e2

**CLEAN ROOM. Licence first.** `LICENSE` is the "Multica License": the Apache-2.0 text plus additional
conditions (no hosted or embedded commercial service without a commercial licence; Multica UI
branding must stay; attribution for non-UI use; contributions may be relicensed by the producer).
`server/ee/` and `packages/ee/` are under a separate commercial licence. Reuse class: **clean
room**. This section describes behaviour in our own words from the README and user docs; no
code, prompts or comments are reproduced. File paths are cited only so a reader can check a
claim.

**What it is.** A source-available, self-hostable workspace where AI agents are assigned issues
like teammates, work on a runtime the user controls, comment progress, raise blockers and hand
work back for review (`README.md`, `VISION.md`). Go backend with PostgreSQL, Next.js web,
Electron desktop, Expo mobile, and a Go daemon on each machine that runs agents
(`server/internal/daemon/`). 31 agent CLIs.

**Behaviour, in our words.**

- **Daemon and runtimes** (`CLI_AND_DAEMON.md`, `apps/docs/content/docs/daemon-runtimes.mdx`): a
  daemon on each machine detects installed agent CLIs and registers one runtime per CLI per
  workspace; the server pushes a wake over a WebSocket when work waits, with a slow poll as
  catch-up; heartbeats every 15 s; a runtime is "recently lost" then "offline". Queued runs wait
  for their runtime rather than moving machines, and expire only when both the runtime has been
  silent and the run has waited past a grace period. The daemon re-execs itself when its binary
  is replaced, but only between tasks. Boot autostart is opt-in, never overwrites a unit it did
  not create, and snapshots only `PATH`.
- **Security model, stated honestly** (`.../security-model.mdx`): runs execute with the full
  rights of the daemon's OS user, Claude Code in bypass mode; the docs tell users to put the
  daemon in a dedicated user, container or VM. They removed a partial Linux sandbox because it
  broke host CLIs and never stopped credential exfiltration. Run-scoped API tokens bind a run to
  one agent so it cannot act as the user.
- **Squads** (`.../squads.mdx`): assigning work to a squad wakes only its leader agent; the leader
  is given a fixed operating protocol, the roster with exact mention syntax, and squad
  instructions; it delegates by mentioning members, records why it decided, then stops; it is
  re-woken by most later comments, never by its own, with de-duplication while it already has a
  queued run; it moves the parent to review only when the whole goal is met and leaves "done" to
  a human.
- **Wakeups** (`.../issue-wakeups.mdx`): a rule stored on an issue wakes an agent at a time, on a
  schedule with an end date, when someone replies, when a field changes, when another run ends,
  when sub-issues close, or when a linked PR's CI finishes; the woken run acts for the person who
  made the rule and that person's right to run the agent is re-checked first.
- **Runs, failures and retries** (`.../tasks.mdx`): a closed list of failure reasons with one
  suggested action each; transient faults retry automatically a bounded number of times;
  credential, quota and configuration failures never auto-retry; a manual retry reuses the
  session unless the error "poisoned" it (context overflow, invalid request), in which case a new
  session starts on the same directory.
- **Steering**: a comment can be added to an agent's running turn instead of queueing a follow-up.
- **Environments**: a repository can declare dev servers the daemon supervises per run, with a
  block of ports each so parallel runs never collide, a TTL, and crash excerpts redacted before
  upload.
- **Workspace garbage collection** with separate TTLs for finished tasks, orphans, artefacts
  (`node_modules`) and repo caches (`CLI_AND_DAEMON.md:395-406`).

**Maturity.** 6,590 commits since 2026-01-28; 1,659 in 90 days; 437 authors (341 in 180 days);
185 tags, latest `v0.11.1` on 2026-11-19. The most contributors in this group.

**Compared with us.** Multica is the closest to ccy-teams in *intent* (people and agents as one
team, coordination visible to humans), built on an issue board where we use a chat room. It is
ahead on structured work state (issue, run, status, retry, wakeup) and behind on isolation (none,
by its own docs).

**Ideas worth adopting (to be implemented from this description only).**

1. **Leader-only wake for a team**: work addressed to the team wakes the coordinator alone; the
   coordinator dispatches and stops; it is re-woken by members' reports, never its own messages,
   and not twice while a wake is pending. Matches 00251's "notification costs a turn only when
   actionable". **Plan** input to 00251 / the bus protocol.
2. **Wakeup rules** for seats: "wake me at T", "when a member replies", "when CI on PR N
   finishes", stored outside the agent and re-checked before firing. Our hooks-daemon crons and
   pingbus wakes cover part. **Plan**.
3. **A closed failure-reason list with one next action each**, and "poisoned session" handling
   (start fresh on the same worktree after context overflow). Fits ccy-sessions' restore and our
   fail-fast messaging. **Issue** for ccy-sessions' failure reporting.
4. **Retry only transient faults; never auto-retry quota or credential failures.** A rule for any
   automated restart we add (Plan 00256 wake, 00181 restart after age). Note.
5. **Per-run port blocks** for parallel dev servers. ccy uses container networking, so collisions
   are rarer; note only.

**Code worth lifting.** None (clean room).

**Risks / avoid.** Do not read `server/ee/` or `packages/ee/`. Do not copy prompts, protocol text
or UI copy. The anonymous telemetry default is something we would not ship.

---

## Group takeaways, ranked

1. **Build Plan 00252 as a credential gateway, not a TLS-intercepting proxy** (intentic, section
   4). intentic made and documented the same choice for the same reasons, has a working rule
   model (method and path rules, upstream fixed by config, redirects returned, placeholders in
   the env, git url rewrites), an ssh agent that only signs, and an honest statement that it only
   becomes a boundary once the agent cannot read the store, which ccy can achieve by putting the
   gateway on the host side. This is the single most valuable finding for in-flight work.
2. **Seat status without screen-scraping: OSC 7501 and `get_usage`** (Orca, AO; sections 2-3).
   Claude Code reportedly emits its own working/idle/blocked state (with the block's kind and its
   subagents) to a terminal that answers a probe, and answers a `get_usage` control request with
   its 5-hour and weekly windows and reset times. Both feed Plan 00251 directly and would replace
   the probe API call behind `ccy --token-usage`. Both need a probe on our pinned version first.
3. **Do something at a usage limit** (Codeman, intentic, AO). Every product either resumes at
   reset, books queued work for the reopen, or switches account; ccy only reports limits. With
   Plan 00146's token switch: switch while any token has room, otherwise hold all automated input
   and resume at the reset, and tell the coordinator the seat is rate-limited until T.
4. **Give the team bus structured work state** (Orca's Run/Task/Dispatch/gate, Multica's squads
   and wakeups, intentic's needs). Today a coordinator tracks work as free text. Three small
   protocol additions would close most of the gap: a completion message with an outcome and the
   request it closes; a durable "need" or gate that blocks named work and wakes the seat when a
   human answers; leader-only wake with de-duplication.
5. **Adopt the convergence and liveness rules as written rules** (intentic's six rules, AO's
   "failed probes are not proof of death" with pid + start-time identity, Emdash's
   tombstone-and-reconcile). Several active plans (00235, 00250, 00256, 00227, 00246) are
   instances of these classes; a stated rule set, and where possible a Go analyzer, would stop
   the next instance.

## Proposed issues and plans

For Task 3.1; each needs the dedupe check against existing plans before filing.

| Kind  | Title (draft)                                                                 | Lands in                          | Source                  |
| ----- | ----------------------------------------------------------------------------- | --------------------------------- | ----------------------- |
| Plan  | 00252 input: credential gateway with per-route rules and an ssh signing agent | Plan 00252 owner decisions        | intentic                |
| Issue | Probe OSC 7501 program status from Claude Code through tmux in a ccy session  | Plan 00251 Task 1.4               | Orca                    |
| Issue | Read token usage with Claude Code's `get_usage` control request               | `ccy --token-usage`, Plan 00251   | Agent Orchestrator      |
| Plan  | Usage-limit hold and resume for ccy seats (switch token first, else wait)     | ccy supervisor, Plans 00146/00251 | Codeman, intentic       |
| Plan  | Bus protocol: completion with outcome, durable needs/gates, leader-only wake  | agent-bus protocol, ccy-teams     | Orca, Multica, intentic |
| Issue | One guard for automated writes into a seat's pane (refuse while blocked)      | ccy supervisor, pingbus delivery  | Agent Orchestrator      |
| Issue | Convergence and liveness rules written down (owner, lease, positive evidence) | AgentNotes / ContainerRules       | intentic, AO, Emdash    |
| Issue | Failure reasons with one next action each in ccy-sessions restore             | ccy-sessions                      | Multica (clean room)    |
| Plan  | Mask known token values in tool output (hooks-daemon PostToolUse)             | hooks daemon (upstream)           | intentic                |
| Watch | Add Orca, intentic and Multica to the watch list; scan AO, Emdash, Codeman    | `CLAUDE/Watchlist.tsv`            | all                     |
