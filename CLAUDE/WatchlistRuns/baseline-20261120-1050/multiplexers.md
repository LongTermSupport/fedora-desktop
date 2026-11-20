# Plan 00258 research: multiplexers and session managers

Six repositories that run coding agents in terminals, keep terminal sessions alive, or watch
worktrees for conflicts. Compared against ccy, `ccy-sessions`, `ccy-teams` and the agent bus,
and the worktree rules.

Method: each clone under `untracked/starred-repos/` was read only (no build, install or run).
Clone heads are given per repository. Paths are relative to that repository's root unless they
start with `src-go/`, `docs/`, `files/` or `CLAUDE/`, which are ours. Guesses are marked
**(guess)**. Where a claim about our own code matters, it was checked against this checkout.

| Repository                     | Licence (from its own file)                       | Reuse class | Language   | Commits / since   | Activity                     |
| ------------------------------ | ------------------------------------------------- | ----------- | ---------- | ----------------- | ---------------------------- |
| herdrdev/herdr                 | Apache-2.0 (no NOTICE)                            | lift        | Rust       | 1884 / 2026-03-27 | very active                  |
| agent-of-empires               | MIT, with Apache-2.0 parts (see its licence note) | lift        | Rust       | 3349 / 2026-01-09 | very active (522 last month) |
| awslabs/cli-agent-orchestrator | Apache-2.0, NOTICE (Amazon)                       | lift        | Python     | 788 / 2025-07-29  | active, accelerating         |
| standardagents/dmux            | MIT (two asset folders excluded)                  | lift        | TypeScript | 747 / 2025-08-19  | slowing (last 2026-08-16)    |
| clash-sh/clash                 | MIT                                               | lift        | Rust       | 47 / 2026-02-03   | dormant since 2026-02-24     |
| shell-pool/shpool              | Apache-2.0, per-file Google headers               | lift        | Rust       | 438 / 2022-09-16  | steady, one main maintainer  |

All six are permissive, so code may be lifted with its notice. Ours is Go, so a lift here means
a port: credit it in a header comment naming the source repository, path, copyright holder and
licence, and say what changed (Apache-2.0 §4b). The most liftable things are **data**: screen
patterns and captured-screen fixtures.

---

## herdrdev/herdr

Studied: baseline at e8d7d9705a7a2c89cef4dd42079379f05685af87

Clone head dated 2026-11-20. The watch list reviewed it at `ee5bfb5`
(`CLAUDE/Plan/00200-repo-watch-list/subagent-reports/scan-20261111T0947Z.md`), and Plan 00251
lifted its state vocabulary and screen manifests (`RESEARCH-seat-status.md` §5). This builds on
both.

**What it is.** One Rust binary that is a terminal multiplexer plus a background server for
people running many CLI agents (Claude Code, Codex, Cursor, OpenCode, Gemini and about twenty
more), locally or over SSH, on Linux, macOS and Windows.

**Since the watch-list scan.** Four fix commits, none changing that scan's verdicts:
latest-frame-wins backpressure for TUI clients that stop reading (`4898729`,
`src/server/client_transport.rs`), an EINTR retry in the live handoff (`src/server/handoff.rs`),
and shells reaped through a pidfd so 500+ pane sessions do not starve the blocking pool
(`src/pty/backend/linux.rs`).

**Architecture.**

- **Its own pty server, not tmux.** Vendored `portable-pty` and `libghostty-vt` as the
  emulator (`vendor/`, both MIT), thin TUI clients, several clients at once, live binary handoff,
  remote machines in one window (`src/remote/`).
- **Status detection fuses three signals** (`src/detect/mod.rs`, `src/pane/agent_detection.rs`):
  1. pty output activity is the normal "working" signal;
  2. per-agent TOML **manifests** (`src/detect/manifests/*.toml`, also published under
     `distribution/agent-detection/`). A rule has an id, a state (working / blocked / idle /
     unknown), a priority, a screen **region** (`osc_title`, `osc_progress`,
     `bottom_non_empty_lines(N)`, `after_last_horizontal_rule`, `prompt_box_body`,
     `last_non_empty_above_prompt_box`, `whole_recent`), and matchers (`contains`, `regex`,
     `line_regex`, nested `any`/`all`/`not`). Highest priority wins; matcher counts and lengths
     are capped (`src/detect/manifest.rs`);
  3. the terminal **title and OSC 9;4 progress**: Claude's title starts with a spinner glyph
     while working and with `✳ ` when idle (rules `osc_title_working`, `osc_title_idle`,
     `osc_progress_idle` in `claude.toml`).
- **Debounce.** A working-to-idle change is held for three rechecks at 100 ms (capped at 700 ms)
  unless the screen shows idle or blocker chrome (`PendingIdleConfirmation`,
  `src/pane/agent_detection.rs`); a 3 s start-up grace; an unchanged bottom buffer is not
  rescanned.
- **`skip_state_update` rules** freeze the state while Claude's transcript viewer (ctrl+o) or model
  picker is open, instead of misreading it.
- **`agent.explain`** (API and CLI) returns the matched rule, its region and an evidence
  preview: a debugger for the classifier.
- **"Done"** is idle and not yet seen by the user (`src/app/api_helpers.rs`).
- **Hooks are for session identity only, for Claude.** This is the main new finding.
  `src/integration/claude_settings.rs` installs only a `SessionStart` hook (reporting
  `session_id` and `transcript_path`), and its `HOOK_REMOVALS` table actively deletes the
  `PreToolUse`, `PostToolUse`, `Stop`, `PermissionRequest` and `UserPromptSubmit` state hooks
  it used to install. `CHANGELOG.md` (around line 712) says Claude, Codex, Copilot, Droid, Kimi
  and Qoder "report session identity only", with state from screen detection. The reasons it
  gives elsewhere in the changelog: hooks miss a cancelled permission and a user interrupt,
  subagent and recap events revive an idle pane, and a stale `blocked` sticks. Hooks keep full
  authority only for agents whose plugin API covers the whole lifecycle (pi, OpenCode and a few
  others).
- **Remote manifest updates** (`manifest_update.rs`) fetch an index from the project's website,
  with a version compare and complexity caps but **no signature**.
- **API**: newline-delimited JSON over a unix socket, about 128 methods and events, a JSON schema
  (`docs/next/api/herdr-api.schema.json`), wait primitives (`agent.wait --until blocked`,
  `pane.wait_for_output`), and an agent skill teaching agents to start and wait on peers
  (`skills/herdr/SKILL.md`).
- **Persistence**: layout and cwd in `session.json`; restore types a validated bare resume
  command into the pane shell (`src/agent_resume.rs`). Processes are not kept across a reboot.
- **Worktrees**: an API to create and remove a git worktree per pane (`src/app/api/worktrees.rs`).
- **Notifications**: OSC 9 / OSC 99 through the host terminal (`src/terminal_notify.rs`), sounds,
  toasts.

**Maturity.** 1884 commits since 2026-03-27, about 100 authors, but the lead has well over half
(under two names). Releases v0.1.0 to v0.9.5 plus dated previews, roughly weekly. About 4000
`#[test]`s. **No captured-screen fixtures for Claude**: manifest tests use synthetic strings.
CONTRIBUTING.md: no unsolicited pull requests.

**Licence.** `LICENSE` Apache-2.0 (root, `Cargo.toml`, `crates/ghostty-vt`); no NOTICE file.
Exceptions: `vendor/libghostty-vt` and `vendor/portable-pty` (MIT), `packaging/windows/licenses`
(Microsoft). The manifest TOMLs carry no per-file header, so the repository licence covers them.

**Against ours.**

- Ahead: a blocked-on-dialog state (permission, elicitation, form, question), the title signal,
  debounce, the explain tool, many agents, a wait-until-blocked API, scale to hundreds of panes.
- Behind: no isolation (agents run as the user), no freeze/thaw, no cold migration, no team bus,
  nothing that survives a reboot beyond re-typing a resume command.
- Different: the screen is authoritative and hooks give identity only. Plan 00251's seat state
  machine is hook-led (UserPromptSubmit, Stop, a new Notification hook for dialogs).

**Ideas.**

1. **Hooks alone are lossy for Claude's blocked/working state** (plan: fold into Plan 00251's
   research). herdr ran Claude state hooks for months and removed them, for named reasons: a
   cancelled permission leaves `blocked` stuck, an interrupt sends no event, subagent and recap
   events revive an idle pane. Plan 00251's `blocked: dialog` from a Notification hook needs a
   screen confirmation or an expiry, not trust in the hook alone. agent-of-empires reached the
   same conclusion from the other side (see agent-of-empires below).
2. **The pane title as a cheap working/idle signal** (issue, after a check). tmux holds each
   pane's title as `#{pane_title}`; reading it costs no `capture-pane`. Nothing in `src-go/`
   reads `pane_title` today. **(guess)** tmux takes Claude's OSC 0/2 title into `pane_title` by
   default; our `files/etc/tmux.conf` sets `set-titles` for the outer terminal, which is a
   separate thing. Verify inside a ccy session first.
3. **Debounce plus "viewer open, keep the last state"** in `src-go/internal/screen` (issue). Stops
   flapping in `ccy-sessions` and protects Plan 00256's idle stand-down from a pane read as idle
   mid-turn.
4. **An explain mode** for our classifier (issue): print the rule, region and evidence for a
   session. It makes the next Claude Code UI change quick to diagnose.
5. **Rules as data with a minimum engine version** (plan, low priority). YAGNI while we read only
   Claude. Do not copy the unsigned remote fetch.

**Code worth lifting.** `src/detect/manifests/claude.toml` (242 lines) is the best lift, as data.
Its regexes use `\x{...}`, `(?m)`, `(?i)` and `\z`, all valid in Go's RE2, and no lookarounds.
`PendingIdleConfirmation` in `src/pane/agent_detection.rs` is about 40 lines of logic to
reimplement with credit. The region slicers in `src/detect/manifest.rs` only where we need them
(`after_last_horizontal_rule`, `prompt_box_body`).

**Avoid.** The unsigned remote manifest updates. The hook scripts in `assets/claude/`, which
swallow every error. The pty server and process-tree identity, irrelevant under tmux and a
container. Upstreaming is not an option (no outside pull requests): issues only.

---

## agent-of-empires/agent-of-empires

Studied: baseline at 4fce915b435129734b5dbb0cbc41d7c5c17ad1a0

Called aoe below. Clone head dated 2026-11-20.

**What it is.** A Rust session manager for many coding agents (Claude, Codex, OpenCode, Gemini,
Cursor, Copilot, Kiro, Qwen and more) on Linux and macOS. A session is a tmux session plus an
optional git worktree, multi-repo workspace or container sandbox. Surfaces: a ratatui TUI, a
CLI, an HTTP API, a web dashboard installable on a phone, an MCP server. `CONTEXT.md` is a
glossary of its terms (session vs tmux session vs agent conversation; trash vs purge; agent hook
vs status hook vs repo hook), a good model of a domain glossary.

**Architecture.**

- **tmux**: every call goes through one constructor (`src/tmux/mod.rs`). Release builds use
  **tmux's default socket** unless `tmux.socket_name` is set; only test builds get their own.
  Each session runs in a transient `systemd-run --user --scope` (`src/process/systemd/mod.rs`),
  documented as "best effort, may fall back to unscoped". We always use a dedicated socket and
  fail when the scope cannot be had.
- **Status detection, in three layers:**
  1. **Agent hooks** write a status token to a file: `/tmp/aoe-hooks-<euid>/<instance>/status`,
     plus `session_id` and an `attention.json` with an expiry. The reader is fd-anchored,
     size-capped and symlink-safe (`src/hooks/status_file.rs`, `dir_guard.rs`). Claude's map is
     `CLAUDE_HOOK_EVENTS` in `src/agents.rs` (line 452): PreToolUse / UserPromptSubmit /
     ElicitationResult → running; PreToolUse of `AskUserQuestion` → waiting until PostToolUse;
     Stop and StopFailure → idle; Notification `permission_prompt` / `elicitation_dialog` /
     `agent_needs_input` → waiting; Notification `idle_prompt` / `agent_completed` → idle. The
     file's mtime says how long the value has stood, and a stale `running` loses authority.
  2. **Screen rules**: TOML manifests per agent in `src/tmux/detect/manifests/` (14 agents plus
     `shared.toml` templates), the same region / priority / matcher model as herdr's, with the
     hook file as one more rule (`hook_waiting`, priority 300). Claude signals worth having:
     the title spinner glyph set; the `(4s · ↓ 88 tokens)` counter; **"N background agents /
     monitors / MCP tasks still running"** behind an idle prompt = still running; the
     `✻ Cooked for 1m 58s` completion line = idle; "what should Claude do instead" = idle after
     Esc; an approval = "do you want to" plus a `❯ 1.` option line; the folder-trust prompt.
  3. **Reconciliation** (`src/session/instance/status_update.rs`): skip the capture when tmux's
     `#{window_activity}` has not moved and no hook fired; a dwell before publishing; a check
     for a bare shell left in the pane ("agent exited").
- **ACP view**: a separate structured mode runs the agent under a worker (`acp-worker/`) through
  ACP adapters and shows approvals as cards. Not screen scraping.
- **Sandbox** (`docker/`, `src/containers/`): Docker by default, Podman and Apple Container
  optional. The image **runs as root** ("IS_SANDBOX=1 allows --dangerously-skip-permissions",
  `docker/Dockerfile`) and installs agents with `curl | bash`. The project is mounted read-write,
  and one Claude `.credentials.json` is **shared read-write by every sandbox**
  (`docs/guides/sandbox.md`). Network is none or a custom network; no egress policy, no token
  pool; `privileged`, `cap_add` and `security_opt` pass straight through.
- **Resume** (`docs/guides/session-resume.md`): a matrix per agent of how its conversation id is
  captured; resume is refused unless the store, cwd and agent are attested, and the id is never
  inferred from recency (the same rule as our restore, which never uses `--continue`).
  `conversation_carry.rs` copies a transcript when a session moves to another account's config
  directory, so `--resume <id>` still finds it.
- **Notifications**: Web Push (already mined by Plan 00251). Beyond that: approval and question
  pushes bypass suppression; keyboard input in the TUI suppresses pushes for 30 s on every
  device; an answered approval is withdrawn from the other devices.
- **Other**: CC0 sounds per state (`bundled_sounds/`); themes with explicit waiting / fresh-idle
  / idle colours; idle reaping (`src/server/idle_reap.rs`); a sleep inhibitor while sessions work
  (`sleep_inhibit.rs`); a "smart rename" by one `claude -p` call at the first turn end
  (`smart_rename.rs`); snooze, pin, trash-then-purge; remote access by Cloudflare or Tailscale
  tunnel with passkeys (`src/server/{tunnel,passkeys,auth}.rs`); plugins as any-language NDJSON
  JSON-RPC workers (`aoe-plugin-api/`).

**Maturity.** 3349 commits from 2026-01-09 to 2026-11-20, 300 to 500 a month, 142 authors (three
carry most). 135 tags (v0.0.1 to v1.25.0, plus nightlies) by release-plz. About 664k lines of
Rust in `src/`, about 6800 `#[test]`s, a tmux-driven e2e suite (`tests/e2e`), 14 CI workflows
including CodeQL. Fast-moving: its detection rules churn with every Claude Code release.

### Licence: MIT, but its Claude rules are partly herdr's

`LICENSE` is MIT (© 2026 Nathan Brake). `THIRD_PARTY_NOTICES.md` names one Apache-2.0 exception,
`src/tui/hyperlink.rs` (derived from herdr). `bundled_sounds/` are CC0. No per-file headers in
`src/tmux/detect`, `src/hooks` or `themes/`.

**But the rule table is not purely MIT.** The commit that introduced it, `faab441c` (2026-08-31,
"decide pane status from a rule table, not a chain", #3600), says: "The manifest model, the region
vocabulary, the title priority asymmetry and the confirmation hold follow herdr (Apache-2.0)…
its viewer rules are ported directly", and a later commit says two background-work rule shapes
"come from herdr's claude manifest (Apache-2.0)". `THIRD_PARTY_NOTICES.md` does not list these.
So lift Claude screen rules from **herdr's `claude.toml` under Apache-2.0** where they overlap,
and treat aoe's `claude.toml` as MIT plus Apache-2.0 content, carrying both notices.

**Against ours.**

- Ahead: hook-fed status with a screen fallback; an approval-waiting state; awareness of
  background work behind an idle prompt; a phone dashboard with push; many agents; worktree
  lifecycle bound to a session; trash and snooze.
- Behind: containment (root image, shared writable credentials, `curl | bash`, Docker default,
  no egress policy); best-effort scope; the user's default tmux socket; no freeze/thaw, no
  migration, no token pool, no team bus (its multi-user design is a separate ADR).
- Different: we reach humans through Matrix and Element; aoe runs its own web server and tunnels.

**Ideas.**

1. **An approval/question-waiting state in `src-go/internal/screen`** (issue). Today the package
   knows ready, busy, typed and not-drawn, plus launcher prompts and the conversation picker;
   nothing says "Claude is waiting on a permission dialog or AskUserQuestion". Signals: the
   approval chrome above, the AskUserQuestion footer, the folder-trust prompt. Same target as
   herdr idea 3; one issue.
2. **"Still running behind an idle prompt"** (issue). The background agents / monitors / MCP
   tasks lines mean the session is not idle. Plan 00256 already requires "no background task or
   subagent running" for idleness; this is the evidence to read it from. Wind-down and close
   (`src-go/internal/sessclose`, `src-go/internal/teams/winddown`) should not treat such a pane as
   ready either.
3. **A Notification-hook token file with mtime ageing** (plan: Plan 00251). aoe's event map is a
   tested starting point for 00251's new kit hooks, including `AskUserQuestion` as waiting
   between PreToolUse and PostToolUse and StopFailure as idle. Combined with herdr's lesson,
   hooks and screen are both needed.
4. **Transcript carry on a token switch** (needs a check). If ccy's token pool can put a resumed
   session on another account's config directory, the transcript must be where `--resume` looks.
   Check whether ccy already shares the store before filing.
5. **Sleep inhibit while any session is busy** (issue, owner's yes first): `systemd-inhibit` held
   by whatever watches sessions on a laptop.
6. **A glossary like `CONTEXT.md`** for ccy and team terms (discussion; docs).

**Code worth lifting.** `src/tmux/detect/manifests/claude.toml` and `shared.toml` (data; see the
licence note above); the `CLAUDE_HOOK_EVENTS` table in `src/agents.rs` (data); the region
definitions in `src/tmux/detect/region.rs`; the Claude screen captures embedded in the tests in
`src/tmux/status_detection.rs`, to mine for our `testdata/`. Not `src/tui/hyperlink.rs`.

**Avoid.** The sandbox model wholesale (root, shared writable credentials, `curl | bash`,
privileged pass-through). Best-effort scopes and skip-and-warn fallbacks (against our fail-fast
rule). Sharing the user's default tmux socket. Exposing a web server through public tunnels.
Hook-only state (it misses Esc and API errors; aoe backs it with StopFailure and the screen).

---

## awslabs/cli-agent-orchestrator

Studied: baseline at a4a08604dea20e29b6e3096a3203e2a15d32ca5e

Called CAO below. Clone head dated 2026-11-20.

**What it is.** A Python (FastAPI and FastMCP) server on loopback that runs coding-agent CLIs in
tmux panes and lets one agent, the supervisor, drive others, the workers, through MCP tools. For
developers running a team of CLIs on one workstation or cloud box; AWS and Kiro first, but about
15 providers. It has grown into a platform: a web UI (`web/`), a Rust TUI (`tui/`), herdr as an
alternative backend (`backends/herdr_*.py`), scheduled flows, a memory wiki, OTel, OAuth.

**Architecture.**

- **Terminal model**: a CAO session is a tmux session; each agent is a "terminal" with a
  server-made id stamped on the pane as the option `@cao_terminal_id` and exported as
  `CAO_TERMINAL_ID`, so the MCP server inside that agent knows who calls (`clients/tmux.py`).
  State in SQLite (`clients/database.py`).
- **Orchestration verbs** (`mcp_server/server.py`): `handoff` (start a worker from a profile and
  block until it finishes, 1 to 3600 s), `assign` (start and return; the worker replies later),
  `send_message` (to an inbox; `expect_reply` forwards the receiver's last response if it goes
  idle without replying), `complete_assignment`, `answer_user_prompt` (answer a receiver's open
  dialog explicitly), `list_siblings`, remote workers through a broker. The supervisor and worker
  protocols are taught as skills (`cao-supervisor-protocols`, `cao-worker-protocols`).
- **Typing into a pane** (`clients/tmux.py` `send_keys`, around lines 1877-2107): text goes into a
  named tmux buffer (`load-buffer`), is pasted (`paste-buffer`), then after a per-provider delay
  (2.0 s for Claude Code) Enter is sent; the buffer is deleted in `finally`. Three details:
  - it **leaves copy mode** (`send-keys -X cancel`) before the paste and again before every
    Enter, because a pane the human scrolled with the mouse is in copy mode and eats the keys
    (their issue #654);
  - tmux 3.7 sanitises pasted buffers, so it uses `paste-buffer -p` (real bracketed-paste markers
    only if the pane asked for them) and never `-S`; on older tmux it wraps the markers by hand;
  - if `#{pane_current_command}` is a bare shell, the TUI has exited, and no bracketing is used.
- **Status**: `pipe-pane -o` into a per-terminal FIFO through a hardened writer
  (`utils/fifo_writer.py`), fed into a `pyte` screen emulator, read after 0.2 s of quiet by the
  provider's `get_status_from_screen`; `capture-pane` as a fallback. Every status carries an
  **output generation**, so "processing" is tied to a moment, not just a value.
- **Inbox delivery** (`services/inbox_service.py`, `docs/inbox-delivery.md`): one message per
  terminal per ready cycle, oldest first, under a per-terminal lock; an immediate path, a 5 s
  watchdog and a 30 s orphan sweep. **Submit confirmation**: after a paste, wait up to 8 s for a
  status whose generation is newer than the dispatch; else recover at most twice: a bare Enter if
  the composer still holds the text, a full re-send only if the composer is readable and empty,
  otherwise only log. A delivered message is never re-queued (it would paste twice), and the
  bare-Enter recovery refuses when the receiver is waiting on a dialog (it would pick an option
  for the human, their #896).
- **Providers** (`providers/*.py`): each owns its launch command, start-up dialog handling,
  status regexes, composer and response extraction. Claude Code's regexes are
  `providers/claude_code.py` lines 58-530: the spinner glyph set, the completion line, the idle
  prompt, the dialog footer ("↑/↓ to navigate", "Enter to confirm ·", "Esc to cancel · Tab to
  amend"), the trust and bypass prompts, the new input box, background waits, a reduced-motion
  spinner. A ReDoS guard test runs over every provider regex.
- **Worktrees** (`services/worktree_service.py`): `use_worktree=True` gives a worker its own
  worktree and branch; teardown deletes the branch only if fully merged.
- **Security**: loopback API with **auth off by default** (`security/auth.py`: any local process
  is trusted), agents with permissions bypassed on the host, no sandbox. Strong supply-chain
  hygiene in CI (gitleaks, CodeQL, zizmor, cargo-deny).

**Maturity.** 788 commits since 2025-07-29, 100 contributors, heavy dependabot; 179 commits in
2026-10 and 150 so far in 2026-11. Tags v1.0.0 (2026-01-23) to v2.5.4. About 165k lines of
source and about 395k lines of tests, with live CI per provider. Very mature on driving tmux;
sprawling elsewhere.

**Licence.** `LICENSE` Apache-2.0; `NOTICE` "Copyright Amazon.com, Inc. or its affiliates". No
per-file headers in `src/`. `vendor/` and `patches/` hold two MIT npm packages for the web UI,
irrelevant to us.

**Against ours.**

- Ahead: delivery confirmed by output newer than the send; copy-mode handling; tmux 3.7 paste
  semantics; an emulated screen rather than a raw capture; **21 captured Claude Code screens** as
  fixtures (`test/providers/fixtures/claude_code_*.txt`: permission prompts, turn ended,
  stop-hook spinner, API retry, queued rows mid-turn, the resume trust dialog, composers) where
  `src-go/internal/screen/testdata` has 6, all of the conversation picker.
- Behind: no isolation, a trust-everyone local API that can type into any agent; no survival of
  terminal death or reboot to match ours; no human chat; orchestration is a strict tree where
  ours is a peer bus with seats.
- Different: their coordinator is an agent calling blocking MCP tools; ours is message-driven.
  They paste; our `tmuxccy.Submit` types with `send-keys -l` and re-presses Enter until the input
  box empties (`src-go/internal/tmuxccy/submit.go`), a box check where theirs is a generation
  check.

**Ideas.**

1. **Leave copy mode before typing and before every Enter** (issue, clear win; Defence Before
   Fix). Checked here: `src-go/internal/tmuxccy/panes.go` `SendKeys`, `SendEnter` and the Escape
   sender call `send-keys` with no `-X cancel`, and `files/etc/tmux.conf` sets `mouse on`, so a
   wheel scroll puts a pane in copy mode. `Submit` is used by `sessclose` (close and `ccy --stop`),
   `teams/winddown` and `sessions/setgoing`. **(guess, untested)** In copy mode, `send-keys -l`
   letters are read as copy-mode commands and Enter copies a selection, so a close or a delivery
   to a pane someone scrolled can fail or misfire. The class: "keys sent to a pane that may be in
   a mode". A detector rule: every `send-keys` in `src-go` other than `-X cancel` goes through
   one helper that cancels first.
2. **Port the captured Claude Code screens** into `src-go/internal/screen/testdata` with
   attribution, and assert the state on each (issue). Pairs with the aoe/herdr waiting state.
3. **Never answer an open dialog with a recovery Enter** (Plan 00251). `Submit` re-presses Enter
   until the box empties; on a dialog that Enter chooses an option. Check `Submit` refuses when
   the screen shows a dialog.
4. **A started-after-this-send proof** (Plan 00251, discussion): count pane output since the
   dispatch (`#{history_size}` or a `pipe-pane` counter), so a "busy" earned before the delivery
   does not count as pickup.
5. **Bracketed paste for multi-line pings** (`load-buffer`, `paste-buffer -p`, never `-S`)
   rather than `send-keys -l` (plan-level, small; how our injector handles newlines today was not
   checked).
6. ReDoS guard: not needed, Go's RE2 is linear. Noted so nobody ports it.

**Code worth lifting.** Data: `providers/claude_code.py` lines 58-530 (the regexes and their
comments, which are the evidence for each) and `test/providers/fixtures/claude_code_*.txt`.
Logic to reimplement, not copy: the paste / cancel / Enter sequence in `clients/tmux.py` and
`_wait_for_pickup` / `_confirm_submitted` in `services/inbox_service.py`, with the recovery table
in `docs/inbox-delivery.md`. Attribution: source path, "Copyright Amazon.com, Inc. or its
affiliates", Apache-2.0, and a NOTICE line for ported fixtures.

**Avoid.** The posture (bypass on the host, unauthenticated local API that types into panes).
Fixed sleeps as the main synchronisation. The platform sprawl. Fixture names encode their issue
numbers, not Claude Code versions **(guess from names)**: record the Claude Code version with
each fixture we port.

---

## standardagents/dmux

Studied: baseline at 8cb3d926631a9349ab67f7ece41d218427ac7e24

Clone head is v5.11.1, dated 2026-08-16.

**What it is.** An npm CLI for one developer running many agents side by side in one tmux
session: each task is a pane plus a git worktree plus a branch, driven from an Ink (React) TUI in
a sidebar pane (`src/DmuxApp.tsx`). Press `n`, type a prompt, pick agents; later choose Merge or
Create PR. macOS and Linux, no sandbox.

**Architecture.**

- **Panes and layout** computed by dmux, not tmux (`src/layout/`, a long design note in
  `context/LAYOUT.md`); several repositories can share one session.
- **Worktrees** under `<repo>/.dmux/worktrees/<slug>` (`src/utils/paneCreation.ts`), with a base
  branch and branch name per pane.
- **Merge** (`context/MERGE.md`, `src/utils/mergeExecution.ts`): a read-only **`git merge-tree`
  conflict pre-check** (`mergeValidation.ts`), then main into the worktree, then the worktree into
  main with plain `git merge --no-edit` (so a fast-forward can happen), then `git worktree remove --force` and `git branch -d`. On a conflict: AI-assisted (a new agent pane in the same worktree
  with a canned prompt, and `conflictMonitor.ts` polling every 2 s for no unmerged files and no
  `MERGE_HEAD`), manual, or cancel. Cleanup failure paths swallow errors (`try { branch -D } catch {}` in `useWorktreeActions.ts`).
- **Agents**: a registry (`src/utils/agentLaunch.ts`) for about a dozen CLIs, each with
  permission-mode flags and a resume template; commands typed with send-keys.
- **Naming** (`src/utils/slug.ts`): an LLM asked for a 1-2 word slug, with a deterministic
  fallback (stop words, token scoring, at most 4 words / 48 characters, else a date).
- **Status, three layers** (`src/workers/PaneWorker.ts`, `StatusDetector.ts`): a Claude **Stop
  hook** written into the worktree's `.claude/settings.local.json` that writes an event file
  (session id, last assistant message, `stop_hook_active`); screen regexes such as `esc to interrupt` and "changed in 3 of 5 captures"; and an **LLM only when the screen is static** and no
  hook settled it (`PaneAnalyzer.ts`), which classifies option dialog / open prompt / in progress,
  extracts the options and flags "potential harm", at most once per 5 s.
- **Notifications**: macOS only, through a Swift helper that checks window focus, so it does not
  alert when you are already looking (`native/macos/dmux-helper.swift`).
- **User hooks** by event name (`before_pane_create` … `post_merge`, `run_test`), searched in a
  committed `.dmux-hooks/`, then local, then global (`src/utils/hooks.ts`), errors tolerated. A
  committed `AGENTS.md` in the hooks folder tells an agent how to write them.
- **Persistence**: `<repo>/.dmux/dmux.config.json` written atomically; the exact Claude session id
  found by reading `/proc/<pid>/fd` links to an open `~/.claude/projects/**/<uuid>.jsonl`
  (`src/utils/paneAgentTracking.ts`). No systemd scope: the server dies with the user session.
- A localhost HTTP API and Vue dashboard were removed on 2026-02-20, but `frontend/` and
  `context/API.md` remain.

**Maturity.** 747 commits from 2025-08-19 to 2026-08-16; 18 authors but one has 620. Monthly
commits fell from over 100 to 0 in 2026-06, 4 in 2026-07 and 5 in 2026-08. **Slowing; looks like
maintenance mode (guess).** 104 vitest files, but CI is only a publish workflow.

**Licence.** `LICENSE` MIT (© 2025 Justin Schroeder); no per-file headers. Do not take
`arnis-tile-cache/` (88 map tiles referenced nowhere, **(guess)** map-data licensed, not MIT) or
`native/macos/sounds/*.caf` (no stated source).

**Against ours.** Ahead on worktree-per-task creation and one-action merge, focus-aware
notifications and lifecycle hooks. Behind on everything about survival and containment, and its
merges can fast-forward and its cleanups swallow failures (both against `CLAUDE/Worktree.md` and
our fail-fast rule). Different: many visible panes with a sidebar, where we use one full-window
session and pickers.

**Ideas.**

1. **A `git merge-tree` pre-check before a work branch merges** (discussion; see clash-sh/clash below, which
   is the same idea done properly).
2. **Deterministic slug fallback** (`src/utils/slug.ts`) for naming sessions or branches without
   an LLM (issue, small; only if we gain a "name from a prompt" feature, otherwise YAGNI).
3. **Focus-aware attention**: do not notify about a session whose pane is attached and active
   (`#{client_active}`, `#{pane_active}`); discussion, with the teams notifications.
4. Session id from `/proc/<pid>/fd`: **not needed**. Our restore already records the
   conversation id from the bus hook at start and after every `/clear`, resume or compaction
   (`docs/ccy.md`, "Sessions Survive a Reboot"), which is exact; the fd scan depends on Claude
   holding the file open.

**Code worth lifting.** `src/utils/slug.ts` (`generateLocalSlugFromPrompt`, the stop-word list),
credit "derived from dmux, © 2025 Justin Schroeder, MIT". The working-indicator regexes in
`src/utils/paneAttentionHeuristics.ts` only to compare with herdr's and CAO's.

**Avoid.** Shell strings built from branch names (`execSync(`git merge "${branch}"`)`), prompts
interpolated into LLM prompts, error swallowing, writing into the project's
`.claude/settings.local.json` (we own settings through IaC), LLM classification as anything
but a last resort.

---

## clash-sh/clash

Studied: baseline at 2ac931c50d2bea96ad63e517ba603fab2a433a33

Clone head dated 2026-02-24 (v0.2.0). 2113 lines of Rust; the README, `CLAUDE.md`, the plugin,
`src/check.rs`, `src/worktree/conflict.rs` and the discovery in `src/worktree/manager.rs` were
read in full, the TUI only by name.

**What it is.** A small Rust CLI that predicts merge conflicts **between every pair of git
worktrees** of a repository, so agents working in parallel learn of an overlap before they
finish. Commands: `clash check <file>` (or hook mode reading Claude Code's PreToolUse JSON from
stdin), `clash status` (a conflict matrix, `--json`), `clash watch` (a ratatui TUI refreshed on
file change). Shipped as a Claude Code plugin whose only content is a PreToolUse hook on
`Write|Edit|MultiEdit` running `clash check` (`plugins/clash/hooks/hooks.json`).

**How it works.**

- Worktree discovery through `gix` (`src/worktree/manager.rs`): the main worktree plus each linked
  one; a worktree whose path cannot be reached gets a placeholder label rather than an error.
- Pairwise check (`src/worktree/conflict.rs`): for two worktrees, take each **HEAD commit**, find
  the merge base, and run an in-memory three-way tree merge (`repo.merge_trees`, git's
  `merge-tree`); the conflicting paths are the result. Read-only by design (its `CLAUDE.md`).
- `check` (`src/check.rs`) adds "active changes": the file in the other worktree differs from that
  worktree's HEAD (uncommitted work). A hit in hook mode answers PreToolUse with
  `permissionDecision: "ask"` and the reason, so the human is asked before the edit; exit codes
  0 clear / 2 conflicts / 1 error.

**Limits found in the code.**

- Merge conflicts are computed between **committed** HEADs only; uncommitted work counts only as
  "this same file is being changed", not as a conflict test.
- Several failures are read as "fine": `file_has_active_changes` returns false when the other
  worktree cannot be opened; a dirty check that errors is reported as Clean
  (`manager.rs` around line 117); an unreachable worktree becomes a label. Against our fail-fast
  rule.
- It compares worktrees with each other, not each worktree with the protected branch it will
  merge into, unless that branch is checked out in some worktree.
- No tests directory in the tree, although its `CLAUDE.md` says tests go in `tests/`.
- Last commit 2026-02-24: dormant.

**Licence.** `LICENSE` MIT, "Copyright (c) 2026 Clash Contributors". No per-file headers.

**Against ours.** We have nothing like it: no `merge-tree` use anywhere in `src-go/`, `scripts/`
or `.claude/hooks/` (checked). Our worktree rules (`CLAUDE/Worktree.md`) cap open branches and
delete merged ones, but nothing tells an agent that another seat is editing the same lines.
Its discovery would also trip on our layout: the host and ccy containers share one `.git` but see
worktrees at different paths, so from the host the container-made worktrees are unreachable and
clash would quietly label them and move on.

**Ideas.**

1. **Overlap warning across this repository's live work branches** (plan, discussion). Run the
   three-way merge on **branch tips** rather than worktree paths, which sidesteps the two path
   worlds entirely: every open work branch against its protected base and against each other,
   with `git merge-tree --write-tree --name-only` (git 2.38+; the ccy image's git version must
   be checked, Worktree.md already notes it is older than the host's). Consumers: a QA or
   pre-merge check, the coordinator of a team (which seat to steer), and possibly an advisory
   PreToolUse hook. Uncommitted work stays invisible by this route; that is a choice the plan
   must make.
2. **A PreToolUse "ask" on a conflicting edit** (discussion only). In ccy, permissions are
   bypassed and nobody is there to answer an "ask"; an advisory (additional context) suits an
   unattended seat better. If adopted, it belongs in the hooks daemon's project handlers, not a
   third-party plugin.

**Code worth lifting.** Little: the algorithm is a dozen lines around `merge_trees`, and Go would
call `git merge-tree` directly. The JSON shapes in `src/check.rs` (`CheckOutput`,
`FileConflict`) are a reasonable output contract to imitate, with credit if copied.

**Avoid.** The fail-open error handling; the `curl | sh` installer; an "ask" decision for
unattended sessions.

---

## shell-pool/shpool

Studied: baseline at 238b9fb5836b96c4d2797e5abd44c5af3679afbf

Clone head dated 2026-11-18.

**What it is.** "Think tmux… then aim lower" (`debian/control`): a per-user daemon that owns named
shell sessions so that an SSH drop does not kill them. **No multiplexing**: no panes, windows,
status bar or copy mode. Raw pty bytes go to the local terminal, so scrollback, selection and
terminal features behave natively **(guess for OSC 52, hyperlinks and the kitty keyboard
protocol: they pass through because nothing re-renders, but no test in the tree covers them)**.

**Architecture.**

- Crates `shpool` (binary), `libshpool` (logic), `shpool-protocol` (wire types: 4-byte length plus
  msgpack headers; framed data, heartbeat, exit status chunks).
- Daemon by systemd user socket activation (`systemd/shpool.socket`, `SocketMode=0600`;
  `KillMode=mixed`, `SendSIGHUP=yes`) or started on the first attach.
- **Screen restore on reattach** (`libshpool/src/session_restore.rs`): all pty output, attached or
  not, feeds an in-memory terminal (their fork `shpool_vt100`, or an experimental engine). Modes:
  one screenful (default), N lines, or a bare SIGWINCH. The restore replays the screen **and the
  input modes** (bracketed paste, application cursor, mouse), so a paste after reattach is still
  bracketed. Redraw trick: resize the pty one row and column larger, then back after a delay
  (`daemon/shell.rs`), so full-screen programs notice.
- **One client per session**: a second attach gets "busy"; `attach -f` steals by sending a
  detach on a new connection and retrying (`attach.rs`), the same shape as our take-over.
  Heartbeats detect dead clients; `shpool detach <name>` frees a session held by a wedged SSH
  connection.
- A per-session stable `SSH_AUTH_SOCK` symlink re-pointed on every reattach, so agent forwarding
  survives reconnects (`server.rs` `link_ssh_auth_sock`).
- An event socket (`EVENTS.md`): JSON lines with **no payload** (created, attached, detached,
  removed, window title changed); bounded queue per subscriber, slow subscribers dropped, no
  replay; the consumer re-lists after each event.
- Security: socket 0600 and an SO_PEERCRED uid check (`daemon/peer.rs`).

**Maturity.** 438 commits since 2022-09, about 21 authors but one has 336 (Google copyright on
the files). 60 tags, v0.11.7 on 2026-11-13, releases every few weeks by release-plz. About 220
tests, mostly integration. Bus factor about one; protocol crate still 0.x.

**Licence.** `LICENSE` Apache-2.0; all three crates say Apache-2.0; most `.rs` files carry an
Apache header "Copyright … Google LLC". No NOTICE file; `debian/` has no copyright file.

**Could it replace or complement our tmux layer?** No. What ccy uses tmux for, against shpool:

| ccy uses tmux for                                    | shpool                                                         |
| ---------------------------------------------------- | -------------------------------------------------------------- |
| surviving terminal death (scope plus own server)     | yes (daemon, systemd unit)                                     |
| one attached terminal, take-over                     | yes (busy, `attach -f`)                                        |
| reading the screen (`capture-pane`) for `screen`     | **no API**; the daemon holds the screen but exposes only title |
| typing into a pane (closes, wind-down, bus delivery) | **no**; no write request, and a second client is refused       |
| the F12 menu (`display-menu`)                        | no, only a detach key                                          |
| listing sessions and state                           | yes (`list --json`, events)                                    |
| native scrollback and mouse                          | better than tmux                                               |

It lacks the two primitives our automation stands on (read the screen, inject input). Nesting it
inside tmux would add a second attach layer for nothing. Native scrollback is the one thing tmux
structurally cannot give, and is worth knowing as the price we pay.

**Ideas.**

1. **Replay input modes and nudge the size after a thaw or restore** (issue only if we see stale
   or unbracketed screens after `podfreeze` thaw or a take-over; otherwise note). The resize
   jiggle is cheap with `tmux resize-window`.
2. **Payload-less events, consumer re-lists** (discussion): tmux hooks (`client-attached`,
   `pane-exited`, `pane-title-changed` **(guess at availability by tmux version)**) feeding a
   watcher instead of polling `ccy-sessions` state.
3. **Bounded per-subscriber queues, drop the slow subscriber**: a pattern for any agent-bus
   fan-out (note only).
4. SO_PEERCRED: **already done here.** The teamchat link checks it on every connection
   (`src-go/internal/teamchat/member/link/server.go`, `wire/control.go`). Nothing to file.
5. A stable `SSH_AUTH_SOCK` per session: not applicable, ccy mounts keys rather than forwarding
   an agent (`docs/ccy.md`, "SSH and GitHub").

**Code worth lifting.** None needed. `daemon/peer.rs` is about twenty lines we already have; the
restore logic lives in an external crate; Go would use a library, not a port.

**Avoid.** The startup prompt injection into the user's shell (a sentinel wait of up to 90 s),
the `curl` plus `sed -i` install in its README, owner-maintained forks of core crates.

---

## Group findings, ranked

1. **Our screen classifier is a generation behind, and three permissive projects have solved
   it.** herdr, agent-of-empires and CAO each read Claude Code's screen with a priority-ordered
   rule table over named screen regions, and each knows states we do not: waiting on a
   permission or question dialog, work still running behind an idle prompt, viewers that must
   freeze the state, the title spinner. CAO adds 21 captured screens; we have 6, all of the
   conversation picker. **Action:** one issue to add a `waiting` (dialog) state and a
   background-work check to `src-go/internal/screen`, porting rules from herdr's `claude.toml`
   (Apache-2.0) and fixtures from CAO (Apache-2.0, Amazon NOTICE), recording the Claude Code
   version of each fixture. It feeds Plan 00251 (blocked seats), Plan 00256 (idle needs "no
   background task") and `ccy-sessions`. Mind the licence trap: aoe's `claude.toml` is partly
   herdr's Apache-2.0 work although its notices say MIT (see its licence note).
2. **Hooks alone are not enough for state; fuse them with the screen.** herdr removed its Claude
   state hooks for named failures (a cancelled permission stuck as blocked, interrupts unseen,
   subagents reviving idle panes). aoe keeps hooks but ages them by file mtime and lets the
   screen overrule. **Action:** a section in Plan 00251's research before its spec is written:
   a hook-reported `blocked` needs screen confirmation or an expiry; take aoe's event map
   (AskUserQuestion between PreToolUse and PostToolUse, StopFailure) as the starting table.
3. **Typing into a scrolled pane.** CAO leaves copy mode before every paste and every Enter after
   it bit them. Our `tmuxccy` never does, and our tmux config turns the mouse on, so a pane
   someone scrolled is in copy mode when a close, a wind-down or a delivery types into it.
   **Action:** an issue, worked by Defence Before Fix (class: keys sent to a pane that may be in
   a mode; one sending helper that cancels first). Reproduce before claiming the failure mode,
   which is reasoned, not observed. While there, check `Submit`'s Enter re-press never lands on
   an open dialog (CAO #896).
4. **Cross-branch overlap warning (clash's idea, done on branch tips).** Nothing here tells one
   seat that another is changing the same lines until a merge fails. A `git merge-tree` check of
   the open work branches against their protected base and each other avoids the host/container
   path split. **Action:** a plan (needs discussion: consumers, cost, what about uncommitted work,
   the ccy image's git version).
5. **Cheap signals and debounce.** Read `#{pane_title}` before paying for a capture; hold a
   working-to-idle change briefly; skip the capture when `#{window_activity}` has not moved; add
   an explain mode that shows which rule matched. **Action:** fold into the issue in point 1, after
   checking that tmux carries Claude's title into `pane_title` in a ccy session.

Where we are ahead of all six: containment (rootless Podman, network policy, token pool, no
shared writable credentials), survival (systemd scope, own socket, boot restore by exact
conversation id, freeze/thaw, cold migration) and a human-readable team bus. None of these
projects is worth adopting as a component. herdr and agent-of-empires are already on the watch
list (herdr) or worth adding (agent-of-empires, for its Claude rule churn); CAO is worth adding
for its tmux-driving fixes. dmux, clash and shpool are not worth watching.
