# Research: prior art for hot-swapping the Claude token (ccy and cc)

Scope: desk research only; no code changed, no token read or printed. Companion to
[RESEARCH-token-hot-swap.md](RESEARCH-token-hot-swap.md) (binary internals, Claude Code 2.1.287).

**Source limits.** The official docs host (`code.claude.com`, and the `docs.anthropic.com`
redirect to it) could not be fetched from this container, so nothing below is quoted from the
official settings or authentication pages. "Documented" here means stated in a GitHub issue or
PR that cites the docs; each claim says which. Everything marked "to spike" is unverified and
must be proven on a throwaway container before it becomes a task.

## Verdict

1. A running Claude Code cannot be handed a new OAuth token. `apiKeyHelper` is the only
   live-refreshing credential hook, and it does not accept a `claude setup-token` token (issue
   97350, open, 2.1.281: 401). The earlier internals research found the same for the env var.
2. One mechanism swaps without any restart: point `ANTHROPIC_BASE_URL` at a tiny local
   forwarding proxy that attaches the real `Authorization: Bearer` from a file it re-reads, and
   give `claude` only a placeholder token. Several community tools already work this way. To
   spike: Claude Code must send its placeholder as Bearer to a custom base URL and accept the
   proxy's responses unchanged (the tools below say it does).
3. Fallback if the spike fails: the supervisor-driven respawn already in the plan (same session
   id via `--resume`, same container), which also keeps team seats if the respawn is done in
   the supervisor rather than by relaunching the container.

## 1. Can a running Claude Code pick up a new credential?

| Mechanism                                            | Re-read in a running session?                                                                                                                          | Takes a `setup-token` OAuth token?                                                                                                           | Evidence                                                   |
| ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `CLAUDE_CODE_OAUTH_TOKEN` env                        | No: read once, memoised; 401 path refuses to adopt another credential when it is set                                                                   | Yes (its purpose)                                                                                                                            | internals research, 2.1.287 (observed, one release)        |
| `apiKeyHelper` + `CLAUDE_CODE_API_KEY_HELPER_TTL_MS` | Yes: re-run on TTL and on every 401 retry (11 retries seen)                                                                                            | No: output is sent as an API key; an `sk-ant-oat01-` token gets `401 API key is invalid`; changing the TTL made no difference                | issue 97350, 2.1.281                                       |
| `settings.json` hot reload                           | Yes for the file (a session picks a change up "a moment after the write", about 3 s); `claude-swap` relies on it to install an `apiKeyHelper`          | Only API keys; helper output cached until the command string changes, so a rewritten key file alone left sessions on the old key for minutes | claude-swap PR 524 (open), 2.1.302 to 2.1.312              |
| `~/.claude/.credentials.json` re-read                | Mtime re-read exists but is switched off whenever the env var is set; refresh tokens are single-use and shared, so a second consumer revokes the first | Full OAuth credential, not a long-lived token                                                                                                | internals research; third-party `clauth` wiki; issue 54443 |
| `/login` in session                                  | Yes, in-process; deletes the env var and uses the stored login                                                                                         | Browser flow, not the pool                                                                                                                   | internals research                                         |
| `oauthTokenHelper` (a helper taking the OAuth slot)  | Does not exist; requested in issue 97350                                                                                                               | n/a                                                                                                                                          | issue 97350                                                |

A searched-for `CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR` and a "rotated env token" wait appear
only in the binary, not in any public doc found; they read the process's own state, so they do
not help from outside.

## 2. Prior art

- **claude-swap** (PR 524): registers its own `apiKeyHelper` in the profile's `settings.json`
  so running sessions follow an API-key switch. No OAuth-to-OAuth case; no use here.
- **claude-switch, claude-usage-panel, clauth**: swap the stored login or config dir. All need
  a restart or a fresh login; clauth documents the shared refresh-token race.
- **Forwarding proxies, no client restart**: TeamClaude (path-prefix per account on
  `ANTHROPIC_BASE_URL`, optional HTTPS forward-proxy mode), claude-rotate (client sends its own
  device key through `CLAUDE_CODE_OAUTH_TOKEN`; proxy swaps in the active account's setup
  token and forwards), LiteLLM proxy (token pool). Common design: the client holds a
  placeholder, the proxy holds the real tokens, a switch is a change inside the proxy. The
  better ones separate a quota 429 (switch account) from a burst throttle (wait).
- **cc-credentials-manager (PyPI)**: rotates among OAuth tokens by restarting around them.

Terms caveat from the claude-rotate write-up: automated pooling of consumer subscriptions is a
terms grey area; Anthropic does not endorse it. Switching one owner's own accounts by hand is
not pooling, but the owner should read the terms before any automatic rotation. This plan
needs only manual switching.

## 3. Fit for ccy and cc

**Proxy mechanism (to spike).**

- Run a ~50-line forwarder (Go, per the repo's tooling rule) that listens on loopback or a unix
  socket, strips the client's `Authorization`, adds `Authorization: Bearer <current token>`,
  forwards to `api.anthropic.com` over TLS with streaming responses passed through. It re-reads
  the current token from one 0600 file on every request, so a swap is an atomic file replace
  with no signal and no restart.
- `claude` is started with `ANTHROPIC_BASE_URL=http://127.0.0.1:<port>` and
  `CLAUDE_CODE_OAUTH_TOKEN=<fixed placeholder>`. The placeholder is not a secret, so it may sit
  in env, `/proc/1/environ` and the container label without harm. This also fixes the
  child-claude and `ccy-token` label problems of the earlier design: nothing real is in any
  environment.
- **ccy.** The forwarder runs inside the container (started by the entrypoint, before the
  supervisor), so no host socket is needed. The token file lives on container tmpfs, not under
  the project mount; the host writes it with `podman exec -i` on stdin (umask 077, write then
  rename), never argv or env. An alternative that keeps tokens off the container entirely is a
  host-side forwarder reached over a bind-mounted unix socket; that puts the token in a
  process the container cannot read, but needs a host service (Ansible-managed) and a
  socket mount in `podman run`.
- **cc (host).** Same forwarder as a user process or systemd user unit on loopback; `cc` sets
  the two variables. The token file sits in the existing mode-700 token directory.
- **Security.** Token never in argv or logs: the forwarder must not log headers. The proxy
  listens on loopback or a mode-0600 unix socket only. The placeholder env var is public by
  design.
- **Open questions for the spike, in order of risk:** (a) does Claude Code attach the OAuth
  Bearer and the `anthropic-beta` OAuth header when the base URL is custom (the community tools
  imply yes); (b) do usage/profile endpoints Claude Code calls (not only `/v1/messages`) work
  through the same forwarder, since the client may call other hosts; (c) does `/status` and
  the rate-limit display still make sense; (d) cold-cache cost on the new account is
  unchanged by any mechanism.

**No mechanism avoids** the per-account cold prompt cache on the first turn after a switch, and
a rate limit is per account, so the target token must belong to another account (the existing
`u` usage view shows headroom).

## 4. Recommendation

1. **Spike the forwarder first** (one task, throwaway container, one real request each way,
   then swap the file mid-session and watch the next request bill the other account via the
   rate-limit headers `token-management.bash` already reads). If it passes, build it: ccy
   in-container forwarder plus `ccy --switch-token` writing the tmpfs file, then cc with the
   same binary. No upstream supervisor change, no restart, no `--resume`, no team re-clicking.
2. **If the spike fails**, build the supervisor-driven respawn (wait idle, `/exit`,
   `claude --resume <id>` with the new token) as already planned, because it keeps the
   container, and the owner's team seats, which a container relaunch would not.
3. Keep `/login` as the documented emergency hatch only.
