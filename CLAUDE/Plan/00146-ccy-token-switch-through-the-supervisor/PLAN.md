# Plan 00146: ccy token switch through the supervisor

**Status**: Not Started (transport and front ends decided by the owner; decisions 3, 5-8 open, then the Phase 0 spike)
**Created**: 2026-10-02
**Owner**: joseph
**Priority**: Medium

## Overview

The owner needs to hot-swap the Claude token a running session uses. Today the only way is
to end the session and relaunch with another token: Claude Code reads
`CLAUDE_CODE_OAUTH_TOKEN` once and keeps it for the whole session (now stated in the
official env-vars reference), and a container's environment is fixed at `podman run`.

**Design: a placeholder-swapping HTTPS proxy (owner decision 9).** `claude` runs with
`HTTPS_PROXY` pointing at a small Go proxy, a per-session CA in `NODE_EXTRA_CA_CERTS`, and a
per-session placeholder in `CLAUDE_CODE_OAUTH_TOKEN`. The proxy tunnels every host untouched,
except Anthropic's API hosts: it terminates TLS for those alone, with a name-constrained CA,
and replaces the placeholder in `Authorization` with the current real token. `ANTHROPIC_BASE_URL`
stays unset. Setting it caps 1M-context models at 200K, and the owner will not accept losing
the context window. With it unset, the calls Claude Code makes to `api.anthropic.com` by name
go through the swap too. The current token is named in a small file the proxy re-reads on every
request, so a switch is an atomic file write: no restart, no `--resume`, no lost turn, MCP
server or sub-agent. Design, spike questions and liftable code:
[RESEARCH-placeholder-proxy.md](RESEARCH-placeholder-proxy.md). The earlier base-URL
forwarder and its evidence: [RESEARCH-hot-swap-prior-art.md](RESEARCH-hot-swap-prior-art.md).

**One mechanism, several front ends (owner direction).** A switch is always the same
operation: validate the token name against the pool, then write the session's `current` file
atomically. It lives in one Go package that every front end calls, so the front ends add only
how a switch is asked for:

- `ccy-sessions` (the proof of concept): switch one session, or bulk-switch every session on
  a token to another;
- the session itself: the status line shows the current token, and a Claude Code mod offers
  the switch from inside the session;
- `ccy-teams`: automatic failover to the next token in a set the team defines, when a
  session reaches its usage limit.

**Placement for ccy (decision 2):** the proxy runs **on the host**, one per session, started by
the launcher like ccy's one-key ssh agent (Plan 00163). It listens on a unix socket in a
mode-0700 directory under `$XDG_RUNTIME_DIR`, which is bind-mounted into the container. The
container network policy refuses the host's own addresses, so the container cannot reach a
host TCP port, but a unix socket is not covered by that policy. A tiny in-container relay
(`127.0.0.1` TCP to the mounted socket, a byte splice, no token) is what `HTTPS_PROXY` names.
The real token never enters the container. Host `cc` uses the same proxy on loopback TCP
(Phase 3).

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
- The 1M context window is kept: `ANTHROPIC_BASE_URL` is never set.
- One switch mechanism, with no front end re-implementing any part of it: `ccy-sessions`
  (single and bulk), the status line and a Claude Code mod, and `ccy-teams` failover.

## Non-Goals

- Changing the credential inside a running `claude` process. `/login` stays a documented
  emergency escape hatch only.
- The tmux F12 menu entry (Plan 00147); it will call the switch command.
- Editing `.claude/ccy/claude-supervise.py` (daemon-owned).

## Owner decisions

Decided:

- **9. Transport: the HTTPS proxy, never `ANTHROPIC_BASE_URL`.** "We cannot tolerate losing
  the context window size."
- **10. Front ends: several, over one DRY mechanism.** Switching from the session (status
  line or a Claude Code mod), from `ccy-sessions` (including bulk-switching many sessions to
  a new token), and automatic failover across a team's defined token set in `ccy-teams`. The
  proof of concept is the simplest: `ccy-sessions`.
- **1. Proxy first, spike first.** Taken as answered by 9 and 10. The respawn is the fallback.

Open (each can be answered in a line; my recommendation follows each):

2. **Where the ccy proxy runs:** (a) on the host, per session, over a mounted unix socket
   plus an in-container relay; (b) inside the container, with the token in container tmpfs,
   delivered by `podman exec -i`. Recommend (a): the real token leaves the container
   entirely, and a switch is a host-only write.
3. **Terms:** do you accept, on your own reading of the Consumer Terms and the Claude Code
   legal page, that switching between your own accounts through your own local proxy is
   ordinary use? (Quotes are in the research file.) Decision 10 asks for automatic failover in
   `ccy-teams`. That is the use the community proxies report bans for, so it is built last
   (Phase 2, Task 2.4), stays off unless a team turns it on, and needs your explicit yes here
   first. Manual switching does not wait on it. Never anyone else's traffic.
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
8. **Hosts that carry the token:** with the HTTPS proxy, calls Claude Code makes to
   `api.anthropic.com` by name are swapped too, so the base-URL bypass is gone. The spike
   lists every host that carries the placeholder (research Q3, Q4). If one is not an
   Anthropic host, is that a spike failure? Recommend yes.

## Tasks

### Phase 0: Forwarder core and spike (decides the rest)

- [ ] ⬜ **Task 0.1**: TDD the proxy in Go: `src-go/internal/tokenproxy` and
  `src-go/cmd/ccy-token-proxy` (`serve`, `relay`). It answers CONNECT, splices every host
  untouched except the Anthropic API hosts, and terminates TLS for those alone with a
  per-session CA name-constrained to them. Unit tests against an `httptest` TLS upstream:
  other hosts tunnelled byte for byte and never decrypted; the placeholder swapped in
  `Authorization` and checked in constant time; client `x-api-key` dropped; `anthropic-*`
  passed verbatim; SSE flushed event by event; response headers and error bodies unchanged; a
  switch between requests honoured; an in-flight stream kept on its token; a bad `current`
  answered with a named error and never another token; no token bytes in the log. Relay: byte
  splice; a dead socket answered with a named 503. Lift list: research, "Code we may lift".
- [ ] ⬜ **Task 0.1b**: TDD the switch core, `src-go/internal/tokenswitch`, the one place a
  switch happens (decision 10): resolve a session's proxy directory, list the pool with the
  current token and the last usage seen (decision 5), validate a target name, and write
  `current` atomically (0600, temp then rename). Front ends call it and nothing else writes
  `current`.
- [ ] ⬜ **Task 0.2**: Plan-local `spike.bash` (host run through `meta-deploy.list`, built on
  `_planlib.inc.bash`): builds the binary, starts `serve` on two pool token **names** given
  by the owner, runs a throwaway ccy container wired to it, and has `switch` and `report`
  verbs for the interactive half. The steps are in research section 5.7.
- [ ] 🚫 **Task 0.3**: **HOST**: the owner runs the spike. It answers research questions Q1 to
  Q8 ("What the spike must measure"). **Pass** means: a turn on token A, `spike.bash switch B`,
  and the next turn succeeds on B in the same session; the status line reads a 1,000,000-token
  context window for a native-1M model; no token value in the proxy log, any argv, or any
  `/proc/*/environ` in the container; `/status` shows a subscription login. **Fail** on the
  switch or on the context window means the plan stops for the owner. Blocked on decisions 2
  and 3.

### Phase 1: ccy wiring

- [ ] ⬜ **Task 1.1**: Launcher: start `serve` before `podman run`, in
  `$XDG_RUNTIME_DIR/ccy-token-proxy/<container>/` (0700). Its arguments are paths and names
  only, and it watches the launcher's pid. Wait for the socket and fail the launch if it does
  not come up. Mount the directory read-only at `/run/ccy/token-proxy`. Export the
  placeholder, `HTTPS_PROXY` (the relay) and `NODE_EXTRA_CA_CERTS` (the session CA's public
  certificate) instead of the real token; never `ANTHROPIC_BASE_URL`. Stop the proxy in the
  launcher's cleanup.
- [ ] ⬜ **Task 1.2**: Image: the relay binary built static by `play-claude-yolo.yml` (as
  `play-agent-bus.yml` builds `pingbus-status-line`). The entrypoint starts it before the supervisor and fails the start
  if it cannot listen. Bump the container version and `REQUIRED_CONTAINER_VERSION`.
- [ ] ⬜ **Task 1.3**: `ccy-claude`: confirm children follow a switch. They inherit
  `HTTPS_PROXY` and `NODE_EXTRA_CA_CERTS`; `ccy-claude` recovers only `CLAUDE_CODE_OAUTH_TOKEN`
  from `/proc/1/environ` when it is unset, which then yields the placeholder.

### Phase 2: The front ends (each a thin caller of `tokenswitch`)

- [ ] ⬜ **Task 2.1**: **Proof of concept:** `ccy-sessions switch-token NAME SESSION...` and
  `ccy-sessions switch-token NAME --from OLD` (every live session on token OLD). Each session
  is reported by name: switched, or refused with the reason (dead proxy, unknown token, not a
  proxied session). One refusal does not stop the others, and the exit status says whether any
  was refused. Usage text and tests.
- [ ] ⬜ **Task 2.2**: After a confirmed switch, `tokenswitch` rewrites the session-registry
  record and `.last-launch.conf` to the new name. podfreeze and `ccy-sessions` read `current`
  for live sessions (decision 7).
- [ ] ⬜ **Task 2.3**: In the session: the status line shows the current token name and its
  last usage, and a Claude Code mod offers the switch from inside the session (a slash command
  that lists the pool and calls `tokenswitch` on the host). How the mod reaches the host
  without giving the container a way to read tokens is designed first, in this folder.
- [ ] ⬜ **Task 2.4**: `ccy-teams` failover: a team names an ordered token set. When a seat's
  proxy sees a usage-limit response, the team switches that seat to the next token in the set
  with headroom and reports it in the team room. Off unless the team turns it on, and blocked
  on decision 3.
- [ ] ⬜ **Task 2.5**: `CCY_VERSION` bump, `docs/ccy-changelog.md`, `docs/ccy.md` Tokens
  section ("switching account mid-session", the cold-cache cost, the `/login` hatch), help
  text.

### Phase 3: Host `cc` (decision 4)

- [ ] ⬜ **Task 3.1**: `cc-claude` starts `serve` on `127.0.0.1:<random port>`. On loopback
  the placeholder is a real capability: a random secret per session, checked by the
  proxy. Add a `cc` switch verb that calls `tokenswitch`. Update `docs/` for `cc`.

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
- **Upstream drift:** a release could pin certificates for Anthropic's hosts or stop honouring
  `HTTPS_PROXY`/`NODE_EXTRA_CA_CERTS`, which are documented in the network-config guide. The
  proxy passes headers through as an open list, and the acceptance run re-checks the switch
  and the 1M window after updates.
- **The session CA is a credential.** Its private key stays on the host, 0600, one per
  session, name-constrained to the Anthropic hosts, and deleted with the session.
- **Automatic failover (Task 2.4) is the riskiest use under the terms** (decision 3).
- **Proxy down = session down.** That is deliberate: fail loudly, never fall back to a
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
