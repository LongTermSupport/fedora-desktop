# Research: prior art for hot-swapping the Claude token (ccy and cc)

Scope: desk research only; no code changed, no token read or printed. Companion to
[RESEARCH-token-hot-swap.md](RESEARCH-token-hot-swap.md) (binary internals, Claude Code 2.1.287).

**Source limits (first pass).** The official docs host (`code.claude.com`, and the
`docs.anthropic.com` redirect to it) could not be fetched in the first pass, so sections 1 to 4
quote no official page. "Documented" there means stated in a GitHub issue or PR that cites the
docs. The refresh below did fetch the official pages, and it corrects sections 1 to 4 where
they differ. Everything marked "to spike" is still unverified.

## Refresh (November 2026): claims re-checked against the official docs

The verdict holds and is now better supported: the official docs describe the forwarder's
configuration as a supported one. Claim by claim:

| First-pass claim                                                                  | Now                                                                                                                                                                                                                                                                                                                                                                                                                                                            | Source                                    |
| --------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- |
| `CLAUDE_CODE_OAUTH_TOKEN` is read once (internals only)                           | **Holds, now documented:** "Unless you run `/login`, Claude Code uses the token you set for the whole session. To replace an expired token, generate a new one and restart." It is also "Ignored in project and local settings".                                                                                                                                                                                                                               | env-vars reference                        |
| `apiKeyHelper` cannot carry a `setup-token` token                                 | **Holds.** The docs still describe it as returning "an API key". Issue 97350 is still open, with no comments. No `oauthTokenHelper` exists.                                                                                                                                                                                                                                                                                                                    | authentication page; GitHub issue 97350   |
| `settings.json` hot reload applies only to API keys                               | **Corrected:** settings `env` values are re-applied to a running session when the file changes (startup-only readers excepted). That does not help: `CLAUDE_CODE_OAUTH_TOKEN` is ignored in project/local settings, is fixed for the session, and writing it into a settings file would put a real token on disk.                                                                                                                                              | env-vars reference ("Precedence")         |
| `/login` swaps in-process                                                         | **Holds, documented:** "If you run `/login` while the variable is set, Claude Code switches the current session to the new login."                                                                                                                                                                                                                                                                                                                             | authentication page, precedence item 5    |
| Precedence (not stated)                                                           | **New:** cloud provider, then `ANTHROPIC_AUTH_TOKEN` (sent as `Authorization: Bearer`), then `ANTHROPIC_API_KEY`, then `apiKeyHelper`, then `CLAUDE_CODE_OAUTH_TOKEN`, then profiles, then `/login`. A placeholder must therefore be the only credential set.                                                                                                                                                                                                  | authentication page                       |
| Spike question (a): is the OAuth Bearer and `anthropic-beta` sent to a custom URL | **Answered by the docs, still to observe:** the gateway guide says the claude.ai-login case "is possible when `ANTHROPIC_BASE_URL` is set without a gateway credential variable", and that `anthropic-beta` "also carries an OAuth capability that the upstream requires, and stripping it fails those requests with `401`". `CLAUDE_CODE_OAUTH_TOKEN` is a claude.ai credential, not a gateway credential variable.                                           | gateway compatibility guide               |
| Spike question (b): does Claude Code call other hosts                             | **Partly answered:** inference is `POST /v1/messages?beta=true`, with `/v1/messages/count_tokens` (optional) and a best-effort `HEAD /api/hello`. The fast-mode availability check and the WebFetch domain preflight go **directly** to `api.anthropic.com`, not through the base URL. Both have documented skips (`CLAUDE_CODE_SKIP_FAST_MODE_ORG_CHECK`, `skipWebFetchPreflight`). `/usage`, `/status` and auto-mode classifier calls are not listed: spike. | gateway guide; fast-mode page; data-usage |
| Spike question (c): rate-limit display                                            | **Answered by the docs:** a gateway must forward `anthropic-ratelimit-unified-*` "unchanged on every response". Claude Code reads them to show plan usage and to tell a plan limit from a throttle on a `429`.                                                                                                                                                                                                                                                 | gateway guide, response headers           |
| Streaming                                                                         | **New requirements:** never buffer; pass the SSE `ping` events (the client has a streaming idle watchdog); forward error bodies unmodified (the client's capability-rejection retry matches their wording); forward the `system` array unchanged (the attribution block is stripped by position).                                                                                                                                                              | gateway guide                             |
| Remote Control                                                                    | Disabled when the base URL is not `api.anthropic.com` (v2.1.196). A `setup-token` token cannot run Remote Control anyway, so nothing is lost.                                                                                                                                                                                                                                                                                                                  | env-vars; authentication page             |

**Community proxies, re-checked:**

| Tool                       | Licence | Stack                    | Client side                                                                    | Token attach                                                                                                         | Notes                                                                                                                                                         |
| -------------------------- | ------- | ------------------------ | ------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| claude-rotate (doxaras)    | MIT     | Python, FastAPI, httpx   | `ANTHROPIC_BASE_URL` + a per-device key in `CLAUDE_CODE_OAUTH_TOKEN`           | Replaces only `Authorization` with the account's `setup-token` token; client `anthropic-beta` (OAuth) passes through | Byte-identical SSE relay. Reads `anthropic-ratelimit-unified-*`, tells a quota 429 (rotate) from a burst 429 (pace). **Closest to this plan's design.**       |
| CC-Router (VictorMinemu)   | MIT     | TypeScript, Node 20/22   | `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN=proxy-managed` in `settings.json` | Injects `Authorization: Bearer`; takes tokens from the keychain or `.credentials.json` and refreshes them            | Uses refresh tokens, which are single-use and shared, so it races the client (section 1). Not a model for this plan.                                          |
| claude-proxy (asyncdargen) | MIT     | Kotlin/Ktor + Go gateway | `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` (proxy token)                    | Strips client auth, adds the account credential; mechanism not stated                                                | Go SSE relay, mid-stream failover, rate-limit headers tracked. Says subscription accounts need "the Claude Code identity headers and system prompt" injected. |

The `ANTHROPIC_AUTH_TOKEN` style makes Claude Code treat the proxy as a gateway credential. The
client then sends no OAuth beta (the proxy must add it), treats fast mode as disabled, and does
not show plan usage. The `CLAUDE_CODE_OAUTH_TOKEN` placeholder (claude-rotate's style) keeps
the client in its subscription mode, so this plan uses it. TeamClaude, LiteLLM, claude-swap and
cc-credentials-manager were not re-checked.

**Terms, as written (quoted, not interpreted).** Claude Code's legal page:

- "OAuth authentication is intended exclusively for purchasers of Claude Free, Pro, Max,
  Ultra, Team, and Enterprise subscription plans and is designed to support ordinary use of
  Claude Code and other native Anthropic applications."
- "Anthropic does not permit third-party developers to offer Claude.ai login into their own
  applications, or to route requests through Free, Pro, Max, or Ultra plan credentials on
  behalf of their users. Moreover, developers may not collect, store, or intermediate
  Claude.ai credentials or session tokens — sign-in to a Claude account must complete through
  Anthropic's own flow."
- "Advertised usage limits for Pro, Max, and Ultra plans assume ordinary, individual usage of
  Claude Code and the Agent SDK."
- "Anthropic reserves the right to take measures to enforce these restrictions and may do so
  without prior notice."

The proxy READMEs add: "a gray area under Anthropic's consumer terms" and "do not use consumer
subscriptions to back a shared/commercial service" (claude-rotate); "Using multiple Max
subscriptions to increase throughput may violate the ToS" and "Anthropic has been known to ban
accounts for unusual OAuth usage patterns" (CC-Router). Press coverage of a February 2026
Consumer Terms update reports that subscription OAuth tokens may be used only in Claude Code
and Claude.ai. That was read secondhand; the Consumer Terms page itself was not fetched. In
this design the client stays the unmodified Claude Code binary and the tokens are minted by
`claude setup-token` through Anthropic's own flow. The owner is the only user. The owner
decides (PLAN.md decision 3).

**Sources (refresh):** <https://code.claude.com/docs/en/authentication>,
<https://code.claude.com/docs/en/env-vars>, <https://code.claude.com/docs/en/llm-gateway>,
<https://code.claude.com/docs/en/llm-gateway-protocol>,
<https://code.claude.com/docs/en/fast-mode>, <https://code.claude.com/docs/en/data-usage>,
<https://code.claude.com/docs/en/legal-and-compliance>,
<https://github.com/anthropics/claude-code/issues/97350>,
<https://github.com/doxaras/claude-rotate>, <https://github.com/VictorMinemu/CC-Router>,
<https://github.com/asyncdargen/claude-proxy>,
<https://www.theregister.com/2026/02/20/anthropic_clarifies_ban_third_party_claude_access>.

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

## 5. Forwarder design detail (the plan's design)

This section supersedes section 3's ccy placement. Section 3 put the forwarder inside the
container; the plan's recommendation (PLAN.md decision 2) is the host-side alternative it
mentioned.

### 5.1 Placement and how the container reaches it

- **Why not a host TCP port.** The ccy network policy (`src-go/internal/netpolicy`, N1) refuses
  the private ranges, every global address of the host and `host.containers.internal`. N2
  always opens loopback, the container's nameservers, the agent-bus address and project
  networks. A host listener on TCP would need a policy hole, and the agent-bus address is not
  for this.
- **Unix socket mount.** A unix socket is not an IP destination, so the policy does not
  govern it. Precedent: ccy's one-key ssh agent (`ccy_agent_filter_start`,
  `lib/ssh-handling.bash`, Plan 00163) is a per-session host process. It is created under
  `mktemp -d "$XDG_RUNTIME_DIR/ccy-agent.XXXXXX"`, watches the launcher's pid, and its
  socket is mounted into the container. The forwarder copies that lifecycle and its SELinux
  mount handling.
- **In-container relay.** `ANTHROPIC_BASE_URL` takes an HTTP URL, and no unix-socket form is
  documented. So `ccy-token-proxy relay` listens on `127.0.0.1:<fixed port>` in the
  container and splices each connection to `/run/ccy/token-proxy/api.sock`. It parses no
  HTTP and holds no secret. When the socket dial fails it reads the request head and answers
  `503` with an Anthropic-shaped error body naming the cause ("ccy token forwarder not
  reachable; relaunch the session with --resume"), so Claude Code shows a readable message.
- **Rejected alternative (decision 2b).** A forwarder in the container with the token in
  tmpfs, delivered by `podman exec -i` on stdin. It is simpler (no mount, no host process),
  but the real token stays where the agent can read it, as `/proc/1/environ` exposes it
  today. A switch would also need a container exec.

### 5.2 Token custody

- The forwarder takes `--pool <dir>` (the existing mode-700 ccy token directory),
  `--current <file>`, `--listen <socket>` and `--parent-pid`: paths, never values. Its
  environment holds no token.
- `current` holds a token **name**. The name resolves as the launcher resolves `--token NAME`: the first `NAME.*.token`, expiry from the filename, `sk-ant-oat01-` prefix, length
  90 to 120. The forwarder caches the token keyed on `current`'s and the pool file's
  (mtime, size), and re-checks both per request. A bad name, a missing file or an expired
  token gets a named error response. It never falls back to another token.
- No copy of the token is written anywhere. `current` and the optional usage file (5.4) are
  0600 in the 0700 directory, and neither holds a token.
- The placeholder is `sk-ant-oat01-` plus random characters to the pool tokens' length (format
  checks in the client are a spike item), generated per session. The forwarder compares it in
  constant time and refuses any other value with `401`. In ccy only the container can reach
  the socket, so the check is defence in depth. For host `cc` on loopback TCP, which any local
  process can reach, the check is the access control.

### 5.3 Request handling

- `net/http/httputil.ReverseProxy` to `https://api.anthropic.com` with `FlushInterval: -1`
  (flush every write, so SSE `ping` events reach the idle watchdog). It honours the host's
  `HTTPS_PROXY` through `http.ProxyFromEnvironment`.
- Request: drop `Authorization` and `x-api-key`, set `Authorization: Bearer <current>`, and
  forward every other header and the body unchanged (an open list, as the gateway guide
  requires). Response: status, headers and body unchanged, error bodies included.
- One log line per request: time, method, path, status, token **name**, duration. Headers and
  bodies are never logged. A test checks the log for token bytes.
- A switch changes the token for the next request. A request already in flight finishes on
  the token it started with.

### 5.4 Token usage (Plan 00101)

The `u` view in the token chooser (`token-management.bash`, `ccylaunch/tokenusage.go`)
probes each token with a tiny request and reads `anthropic-ratelimit-unified-*`. Nothing
about that changes. Optionally (decision 5) the forwarder writes the last unified-\* values
it saw per token name to `usage.json`. The switch menu then shows live figures for the
current account without spending a probe, and Claude Code's own plan-usage display keeps
working because the headers pass through.

### 5.5 Failure modes

| Failure                                 | Behaviour                                                                                     |
| --------------------------------------- | --------------------------------------------------------------------------------------------- |
| Forwarder does not start at launch      | Launch stops with the forwarder's stderr (fail fast, as `ccy_agent_filter_start` does)        |
| Forwarder dies mid-session              | Relay answers each request `503` naming it; `ccy --switch-token` and `ccy-sessions` report it |
| `current` names a missing/expired token | Named error response; the switch command validates first, so this means the pool changed      |
| Upstream 401/429/5xx                    | Passed through unchanged; Claude Code's own handling applies                                  |
| Wrong placeholder                       | `401` from the forwarder, named                                                               |
| Relay cannot listen at container start  | Entrypoint fails the start                                                                    |

### 5.6 Tests (Go, `httptest` upstream fake)

Header swap; client `Authorization`/`x-api-key` dropped; `anthropic-beta` and
`anthropic-version` verbatim; SSE: the test reads the first event before the fake upstream
sends the second; response headers (unified-\*, `x-should-retry`) and error bodies
byte-identical; switch between two requests; in-flight stream keeps its token; bad `current`
gives a named error and no upstream call; wrong placeholder gives `401` and no upstream call;
log has no token bytes; relay splice round trip; relay on a dead socket gives `503` with the
body. `src-go/internal/leakcheck` may serve the log assertion.

### 5.7 Phase 0 spike steps (`spike.bash`, host)

1. Build `ccy-token-proxy` from the branch. Start `serve` in a fresh 0700 directory with the
   pool directory and the owner's two token **names**, A and B, of different accounts.
2. Non-interactive: run `claude -p 'reply with ok'` in a throwaway ccy container with the
   socket mounted, the relay running, `ANTHROPIC_BASE_URL` and the placeholder set, and no
   other credential (checks P1, P4 via `claude -p '/status'` if it answers there, otherwise
   interactively).
3. Interactive: start a session the same way and send a prompt. Run `spike.bash switch B`,
   send another prompt, then `spike.bash report`. The report prints the forwarder log (names,
   paths, statuses), each request's unified 5h utilisation by token name, and a scan of the
   forwarder log, all argv and the container's `/proc/*/environ` for either token value. It
   prints found or not found, never the value (P2, P3).
4. In the same session, try `/status`, `/usage`, `/fast` and one WebFetch, and record what each
   did. That settles decision 8 and lists every path the client called.
5. Pass and fail criteria: PLAN.md Task 0.3. Record the outcome in the JOURNAL.
