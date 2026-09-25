# Plan 00140: agent browser sessions leak

**Status**: In Progress
**Created**: 2026-09-25
**Owner**: joseph
**Priority**: High

## Overview

Agents in ccy containers start browser session after browser session and leave the old
ones running, so Chromium after Chromium piles up (on the desktop too, in headed mode).
The browsing skill already says, in bold, to close what you open. Agents ignore it, so
more wording will not fix this.

This plan measures what actually spawns each browser, then adds an enforcement to the
browser commands themselves. It does not depend on the agent remembering anything.

## Goals

- Establish by measurement in a ccy container what makes a new browser process appear.
- Bound the number of live browsers per browser command without relying on the agent.
- Keep every existing use working: reusing a session, `close`, `session list`, `skills`,
  and a deliberate multi-session flow when a project opts in.
- A permanent, test-first QA gate for the enforcement, wired into `qa-all.bash`.

## Non-Goals

- Changing upstream agent-browser, or pinning a different version.
- A Claude Code Stop/PreToolUse hook (evaluated and rejected; see Decision 2).
- Changing the idle reaper's five-minute value.

## Context & Background

Root cause, measured in-container (full evidence in the journal, 26-09-25):

- **Every distinct session name is its own daemon and its own browser.** Four session
  names gave four daemons and four Chromium roots, about 14 processes each. Reusing
  one name reuses the browser.
- **The skill we ship sends agents to upstream's core skill first, and that skill tells
  them to start their own named session** (`export AGENT_BROWSER_SESSION="$(agent-browser session id --scope worktree --prefix task)"`). In Claude Code that `export` is gone by
  the next Bash call, so later calls land on `default` and start a second browser. The
  derived id also changes with the cwd and with the prefix, so every task gets a new one.
- **Bare `close` closes one session only.** The others stay up.
- **The reaper is per daemon, and only an idle one.** `AGENT_BROWSER_IDLE_TIMEOUT_MS`
  (five minutes in the image) works, and upstream source at v0.38.1 applies an
  explicit value to headed browsers as well. But every new session starts its own
  timer. So a busy agent has one browser per session opened in the last five minutes.

## Tasks

### Phase 1: Measure

- [x] ✅ **Task 1.1**: Count daemons and browser roots from `/proc` for the patterns
  agents use: repeated `open`, `--session`, `AGENT_BROWSER_SESSION`, bare `close`,
  `close --all`, idle reaping (headless and lite only).
- [x] ✅ **Task 1.2**: Check upstream v0.38.1 source for whether an explicit idle
  timeout covers headed browsers.
- [x] ✅ **Task 1.3**: Check how ccy containers get Claude settings, before deciding on
  a hook.

### Phase 2: Enforce (test first)

- [x] ✅ **Task 2.1**: `scripts/test-agent-browser-session-guard.bash` against a fake
  agent-browser binary: the RED run, before the guard exists.
- [x] ✅ **Task 2.2**: `files/var/local/claude-yolo/agent-browser-session-guard`. It
  refuses a command that would start a new session while this browser command already
  has `CCY_BROWSER_MAX_SESSIONS` (default 1) live, names the live session(s) and says
  how to reuse or close them. It waits out the ~160 ms a just-closed session stays
  listed, and fails loudly if it cannot read the session list.
- [x] ✅ **Task 2.3**: Route the three wrappers through the guard (Dockerfile), stage the
  guard into the build context (`play-claude-yolo.yml`), bump the container version
  and `CCY_VERSION`, add a changelog entry.
- [x] ✅ **Task 2.4**: Wire the suite into `qa-all.bash`.
- [x] ✅ **Task 2.5**: Run the checked-out guard against the real binary in this
  container, repeating the leaking patterns from Task 1.1 through it:
  `./acceptance.bash --checkout`.

### Phase 3: Docs and review

- [x] ✅ **Task 3.1**: Update the browsing skill: one session per command, why
  upstream's "always use your own session" advice does not apply here, what a refusal
  means. Update `docs/ccy.md`, `CCY-GUIDE.txt` and `docs/playbooks.md`.
- [x] ✅ **Task 3.2**: `./scripts/qa-all.bash`; `qa-reviewer` over the branch diff;
  resolve findings. Round 1 was FIX-BEFORE-MERGE (fixed, Decision 3); round 2 was
  PASS WITH NITS (the nits were fixed).
- [ ] ⬜ **Task 3.3**: On the HOST, `./deploy.bash` (`play-claude-yolo.yml`, image
  rebuild). Then, inside a NEW ccy session, `./acceptance.bash`. On the host it reports
  COULD NOT ESTABLISH (exit 2), because the browsers exist only in the image.

## Technical Decisions

### Decision 1: cap live sessions in the wrapper, and refuse the next one

**Context**: something has to stop a new browser from starting while old ones run.
Measurement shows that happens at exactly one point: a command on a session name that is
not live yet.
**Options considered**:

- A: pin every command to one session by rewriting `--session`. Simple, but it silently
  changes what the caller asked for, and it turns a leak into silent page sharing
  between parallel agents.
- B: cap live sessions per command and refuse a new one past the cap, naming the live
  session. This is loud, the agent sees it the moment it matters, and it corrects itself:
  an agent that lost its `export` is told which session to reuse.
- C: close sessions that look unused. That needs a "last used" signal the CLI does not
  expose, and guessing wrong closes another agent's browser mid-task.
  **Decision**: B, default cap 1 per command. So at most one headed Chromium, one headless
  Chromium and one Lightpanda. A project that genuinely needs two at once (a two-user
  flow) sets `CCY_BROWSER_MAX_SESSIONS` in its `ccy.env`.

### Decision 2: no Claude Code hook

**Context**: a Stop hook could list live sessions and block the end of a turn.
**Why not**: ccy's `/root/.claude` is the project's own `.claude/ccy/`, so a hook would
have to be merged into every project's settings by `entrypoint.sh`, alongside whatever
Stop handling the project already has. It would also block a headed window the user
asked to keep open between turns. The wrapper guard covers every caller (main agent,
subagents, scripts) with no Claude configuration. Once the count is capped, the idle
reaper handles the one browser per command left open. Revisit only if that proves
not to be enough.

### Decision 3: refuse a caller's copy of a wrapper-owned flag; agents name their session

**Context**: the first review measured that the CLI is last-wins for a repeated flag, and
this plan then measured `--headed` too. A caller's `--namespace` escaped the count, a
repeated `--session` fooled reuse, and `agent-browser-headless --headed true` would open
a desktop window. The Dockerfile's "first-wins" comment was wrong.
**Decision**: the guard refuses (exit 2) any flag its own wrapper sets, derived from the
wrapper's argv rather than listed. It forwards every `--session` and a caller's
`--config` to its probes, so they resolve what the command will. The skill has agents pass
`--session <name>` on every command and close by name. On `default`, parallel agents
would share one browser without noticing, and `close --all` closes other agents' sessions.

## Success Criteria

- [x] Through the guard, the leaking patterns from Task 1.1 leave at most one browser
  root per browser command (real binary, in-container; checkout mode, before deploy).
- [x] Reuse, `close`, `close --all`, `session list`, `skills` and `--version` are never
  refused.
- [x] A caller cannot override a wrapper-owned flag (`--namespace`, `--headed`, lite's
  `--config`), and a repeated `--session` resolves as the CLI resolves it.
- [x] `scripts/test-agent-browser-session-guard.bash` passes and runs in `qa-all.bash`.
- [ ] After the host deploy, `./acceptance.bash` (installed mode) in a fresh ccy
  container: ACCEPTED with full coverage.
- [x] QA passes (`./scripts/qa-all.bash`), apart from stages that fail for a documented
  worktree-only reason; `qa-reviewer` findings resolved.

## Risks & Mitigations

| Risk                                                                                | Impact | Probability | Mitigation                                                                                    |
| ----------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------- |
| Two parallel agents both start a first session at once and both pass the check      | L      | L           | Bound exceeded by the racers only; each still reuses its own. No lock, deliberately (YAGNI).  |
| A refused agent closes a parallel agent's browser                                   | M      | L           | Refusal and skill say close `--session <yours>`, never `close --all`, never one not yours.    |
| A non-launching command outside the pass list (`auth list`, `dashboard`) is refused | L      | L           | Loud, not a leak; widen the list if it happens.                                               |
| Command-word detection misreads an unusual flag layout                              | L      | L           | Mostly guarded; an unlisted value flag whose value is a pass word (`-s close open`) slips by. |
| Upstream changes `session list --json` output                                       | M      | L           | The guard fails loudly on unparseable output; the suite pins the format it expects.           |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00140-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Root cause measured (Phase 1)
- `0e0ab5f8` guard, suite, image wiring, CCY 3.67.5 / container 2.38
- `416f81d4` skill and docs
- `707b2bef` review round 1: wrapper-owned flags refused, named sessions
- Waiting on the host deploy (Task 3.3)
