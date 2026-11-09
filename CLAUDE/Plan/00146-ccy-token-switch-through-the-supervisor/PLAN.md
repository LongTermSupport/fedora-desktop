# Plan 00146: ccy token switch through the supervisor

**Status**: Not Started (decision gate: the owner decisions below, then the Phase 0 spike)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

The owner needs to hot-swap the Claude token a running session uses. Today the only way is
to end the session and relaunch with another token: Claude Code reads
`CLAUDE_CODE_OAUTH_TOKEN` once and keeps it for the whole session (now stated in the
official env-vars reference), and a container's environment is fixed at `podman run`.

**Design: a local token forwarder.** `claude` is started with `ANTHROPIC_BASE_URL` pointing at
a small Go forwarder and a per-session placeholder in `CLAUDE_CODE_OAUTH_TOKEN`. The forwarder
replaces the `Authorization` header with the current real token and passes everything else,
SSE streams and rate-limit headers included, unchanged to `api.anthropic.com`. The current
token is named in a small file the forwarder re-checks on every request, so a switch is an
atomic file write: no restart, no `--resume`, no lost turn, MCP server or sub-agent. The
official gateway guide documents this exact configuration (a claude.ai credential with a
custom base URL; the gateway must forward `anthropic-beta`, whose OAuth value it needs), and
three MIT-licensed community proxies run it. Evidence, sources and the design detail:
[RESEARCH-hot-swap-prior-art.md](RESEARCH-hot-swap-prior-art.md) (refresh section and section
5).

**Placement for ccy (recommended, decision 2):** the forwarder runs **on the host**, one per
session, started by the launcher like ccy's one-key ssh agent (Plan 00163). It listens on a
unix socket in a mode-0700 directory under `$XDG_RUNTIME_DIR`, which is bind-mounted into
the container. The container network policy refuses the host's own addresses, so the
container cannot reach a host TCP port. A unix socket is not covered by that policy. Claude
Code takes only an HTTP URL, so a tiny in-container relay (`127.0.0.1` TCP to the mounted
socket, no HTTP parsing, no token) sits in between. The real token then never enters the
container. Host `cc` uses the same forwarder on loopback TCP (Phase 3).

The supervisor respawn, the plan's earlier design, is now the fallback only:
[FALLBACK-supervisor-respawn.md](FALLBACK-supervisor-respawn.md). The folder name is
historical. Earlier evidence: [RESEARCH-token-hot-swap.md](RESEARCH-token-hot-swap.md).

## Goals

- From a running ccy session, switch it to another token from the pool. The very next API
  request uses that token. The session, its container, ssh-agent and tmux pane keep running.
- No real token in argv, env, a log, the project tree, the container, or any new file. The
  forwarder reads the existing 0600 pool file; the "current" file holds a token name only.
- Child claudes (`ccy-claude`), the session registry, the next launch and podfreeze all
  follow the switch.
- Host `cc` gets the same switch with the same binary.

## Non-Goals

- Automatic rotation on a 429 or a usage threshold (see Risks: terms). Switching is a
  manual command.
- Changing the credential inside a running `claude` process. `/login` stays a documented
  emergency escape hatch only.
- The tmux F12 menu entry (Plan 00147); it will call the switch command.
- Editing `.claude/ccy/claude-supervise.py` (daemon-owned).

## Owner decisions (open)

Each one can be answered in a line. My recommendation follows each.

1. **Forwarder as the primary design, spike first?** Recommend yes. The respawn becomes the
   fallback.
2. **Where the ccy forwarder runs:** (a) on the host, per session, over a mounted unix socket
   plus an in-container relay; (b) inside the container, with the token in container tmpfs,
   delivered by `podman exec -i`. Recommend (a): the real token leaves the container
   entirely, and a switch is a host-only write.
3. **Terms:** do you accept, on your own reading of the Consumer Terms and the Claude Code
   legal page, that manually switching between your own accounts through your own local
   forwarder is ordinary use? (Quotes are in the research file.) Recommend: manual only, no
   auto-rotation, and no other person's traffic.
4. **Host `cc` in this plan** (Phase 3, after ccy) or in a follow-up? Recommend in this plan.
   It is the same binary, and Plan 00048 already gave `cc` the named-token pool.
5. **Live usage from the forwarder:** it records the last `anthropic-ratelimit-unified-*`
   values per token name (no token) in its directory, and the switch menu shows them beside
   the `u` probe (Plan 00101). Recommend yes.
6. **Forwarder death mid-session:** fail loudly (the relay answers every request with a
   named 503, and `ccy --switch-token` and `ccy-sessions` report it) without auto-restart, or
   have the launcher restart it. Recommend fail loudly; relaunch with `--resume` to recover.
7. **Stale `ccy-token` label:** the forwarder's `current` file becomes the source of truth.
   The session registry is rewritten on a switch, and podfreeze and `ccy-sessions` read
   `current` for a live session. The label is documented as "token at launch". Recommend yes.
8. **Features that call `api.anthropic.com` directly** (fast-mode availability check,
   WebFetch domain preflight) carry the placeholder. If the spike shows them failing, accept
   the documented skip settings (`CLAUDE_CODE_SKIP_FAST_MODE_ORG_CHECK`, `skipWebFetchPreflight`)
   or treat it as a spike failure? Recommend accept, if nothing else breaks.

## Tasks

### Phase 0: Forwarder core and spike (decides the rest)

- [ ] ⬜ **Task 0.1**: TDD the forwarder in Go: `src-go/internal/tokenproxy` and
  `src-go/cmd/ccy-token-proxy` (`serve`, `relay`). Unit tests against an `httptest`
  upstream, per section 5.6 of the research: header swap, client `Authorization` and
  `x-api-key` dropped, `anthropic-*` passed verbatim, SSE flushed event by event,
  response headers and error bodies unchanged, placeholder checked in constant time, a
  switch between requests honoured, an in-flight stream kept on its token, a bad `current`
  answered with a named error and never another token, no token bytes in the log. Relay:
  byte splice; a dead socket answered with a named 503.
- [ ] ⬜ **Task 0.2**: Plan-local `spike.bash` (host run through `meta-deploy.list`, built on
  `_planlib.inc.bash`): builds the binary, starts `serve` on two pool token **names** given
  by the owner, runs a throwaway ccy container wired to it, and has `switch` and `report`
  verbs for the interactive half. The steps are in research section 5.7.
- [ ] 🚫 **Task 0.3**: **HOST**: the owner runs the spike. **Pass** means all of these hold:
  (P1) `claude -p` through the forwarder succeeds on token A; (P2) in one interactive session
  a turn on A, then `spike.bash switch B`, then the next turn succeeds and the forwarder log
  names B for it; (P3) no token value in the forwarder log, any argv, or any
  `/proc/*/environ` in the container; (P4) `/status` shows a subscription login, not an API
  key. **Fail** means P1 or P2 fails: the plan switches to the fallback. The spike also lists
  every path Claude Code called and how `/usage`, `/fast` and WebFetch behaved, which settles
  decision 8. Blocked on decisions 1 to 3.

### Phase 1: ccy wiring

- [ ] ⬜ **Task 1.1**: Launcher: start `serve` before `podman run`, in
  `$XDG_RUNTIME_DIR/ccy-token-proxy/<container>/` (0700). Its arguments are paths and names
  only, and it watches the launcher's pid. Wait for the socket and fail the launch if it does
  not come up. Mount the directory read-only at `/run/ccy/token-proxy`. Export the
  placeholder and `ANTHROPIC_BASE_URL` instead of the real token. Stop the forwarder in the
  launcher's cleanup.
- [ ] ⬜ **Task 1.2**: Image: the relay binary built static by `play-claude-yolo.yml` (like
  `pingbus-status-line`). The entrypoint starts it before the supervisor and fails the start
  if it cannot listen. Bump the container version and `REQUIRED_CONTAINER_VERSION`.
- [ ] ⬜ **Task 1.3**: `ccy-claude`: confirm children get the placeholder and
  `ANTHROPIC_BASE_URL` (it reads `/proc/1/environ` today), so they follow a switch.

### Phase 2: The switch command

- [ ] ⬜ **Task 2.1**: `ccy --switch-token [NAME] [--session <name>]`, answered by the Go
  front (`src-go/internal/ccylaunch`). It resolves the session's forwarder directory, lists
  the pool with the current token marked and usage (existing `listtokens`/`tokenusage`, plus
  decision 5), validates the choice live, and writes `current` atomically (0600, temp then
  rename). It refuses a dead forwarder by name. It follows `CLAUDE/InteractiveScripts.md`
  (bounded re-prompt, clean exit on EOF).
- [ ] ⬜ **Task 2.2**: After a confirmed switch, rewrite the session-registry record and
  `.last-launch.conf` to the new name. podfreeze and `ccy-sessions` read `current` for live
  sessions (decision 7).
- [ ] ⬜ **Task 2.3**: `CCY_VERSION` bump, `docs/ccy-changelog.md`, `docs/ccy.md` Tokens
  section ("switching account mid-session", the cold-cache cost, the `/login` hatch), help
  text.

### Phase 3: Host `cc` (decision 4)

- [ ] ⬜ **Task 3.1**: `cc-claude` starts `serve` on `127.0.0.1:<random port>`. On loopback
  the placeholder is a real capability: a random secret per session, checked by the
  forwarder. Add a `cc` switch verb sharing Task 2.1's code. Update `docs/` for `cc`.

### Phase 4: Verification

- [ ] ⬜ **Task 4.1**: `deploy.bash` and `acceptance.bash` in this folder. The acceptance
  run does a live switch in a real ccy session and checks: same session, the next request on
  the new token (forwarder log), no token in argv, env or log, and the registry updated.
- [ ] ⬜ **Task 4.2**: `./scripts/qa-all.bash` green; `qa-reviewer` agent over the full diff.
- [ ] 🚫 **Task 4.3**: **HOST**: run `deploy.bash` then `acceptance.bash` through
  `meta-deploy.bash`. Blocked: Ansible never runs in the ccy container.

## Risks

- **Terms.** The Claude Code legal page reserves OAuth for "ordinary use of Claude Code" by
  subscribers. It bars third-party developers from routing requests through Free, Pro, Max
  or Ultra credentials on behalf of their users, and from collecting or intermediating
  Claude.ai credentials. It says advertised limits "assume ordinary, individual usage".
  Community proxy READMEs call multi-account rotation a grey area and report bans for
  unusual OAuth patterns. This plan keeps the official binary unmodified, carries only the
  owner's own traffic and tokens, and switches only by hand. Whether that is acceptable is
  decision 3.
- **The first turn on the new account re-reads the whole context** with a cold prompt cache.
  No mechanism avoids it, and a rate limit is per account, so the target must be another
  account.
- **Upstream drift:** a release could stop honouring `ANTHROPIC_BASE_URL` for subscription
  credentials, or add direct calls. The forwarder passes headers through as an open list
  (the gateway guide's advice), and the acceptance run re-checks this after updates.
- **Forwarder down = session down.** That is deliberate: fail loudly, never fall back to a
  token in the environment.

## Success Criteria

- [ ] A switch in a live ccy session takes effect on the next request with no restart, and
  the conversation, container and ssh-agent carry on.
- [ ] No real token in the container, argv, env, logs, the project tree or a new file.
- [ ] `ccy-claude`, the registry, the next launch and podfreeze use the new token name.
- [ ] Every failure (no forwarder, bad token name, expired token, dead socket) stops with a
  named error and never sends another token.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00146-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- (none yet)
