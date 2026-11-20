# Study: gateways and credential brokers

Plan 00258, Task 2.1, group "gateways". Nine repositories read from the clones under
`untracked/starred-repos/`, read only: nothing was built, installed or run. File paths below are
relative to each clone. Where a statement is my inference rather than something a file says, it
is marked as a guess.

Read first, for comparison: [docs/ccy.md](../../../docs/ccy.md) (Security Model, Tokens,
Network policy), `src-go/cmd/ccy-network-policy`, `src-go/cmd/ccy-token-usable`,
`src-go/cmd/wsi-claude-token`, Plans [00252](../../Plan/00252-ccy-egress-proxy-per-request-policy-and-credential-injection/PLAN.md)
(and its devvm research), [00146](../../Plan/00146-ccy-token-switch-through-the-supervisor/PLAN.md)
(and its prior-art research), 00116, 00181, 00232, 00220.

## Where ccy stands (the baseline every repo is compared with)

- The container holds **live** `CLAUDE_CODE_OAUTH_TOKEN` (a `claude setup-token` token from the
  named pool under `~/.claude-tokens/ccy/`) and a live `GH_TOKEN`. docs/ccy.md says so plainly
  under "What this means in practice".
- Plan 00232 (accepted on the host) holds an nftables policy in the container's namespace: LAN,
  private ranges and the host refused, **public internet open**. It is L3/L4 only.
- Plan 00146 (Not Started) designs a per-session host forwarder behind `ANTHROPIC_BASE_URL`,
  reached over a mounted unix socket and an in-container byte relay; the container gets a
  placeholder; a switch is an atomic write of a token *name*. No TLS interception, no CA.
- Plan 00252 (Not Started, owner decisions open) asks whether that forwarder should grow into a
  host L7 egress proxy with a per-container CA, per-request rules, and GitHub narrowing.
- Plan 00116 gives a project's deploy key over SSH; there is no HTTPS-API narrowing of `GH_TOKEN`.

## Summary table

| Repo                         | Licence (verified)                                                                                                                      | Reuse class                                      | Language           | Activity (to 2026-11-20)                                                     | Closest to                                                                                        |
| ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ | ------------------ | ---------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| maximhq/bifrost              | Apache-2.0 (root `LICENSE`)                                                                                                             | lift                                             | Go                 | 9384 commits since 2025-03; ~1000 in the last 30 days; 238 authors           | An org LLM/MCP gateway; "relays" = credential injection to fixed APIs                             |
| agentgateway/agentgateway    | Apache-2.0 (root); `crates/pool/LICENSE` is MIT (Sean McArthur)                                                                         | lift (Rust, so ideas or a component in practice) | Rust               | 3306 commits since 2025-03; 226 in 30 days; 302 authors; LF Projects charter | **Plan 00252 as a product**: CONNECT egress proxy, per-host interception, placeholder replacement |
| ThinkWatchProject/ThinkWatch | BUSL-1.1 (root; usage-threshold commercial terms in `LICENSING.md`); `crates/gateway/src/proxy/web_tools/LICENSE.core-web-tools` is MIT | clean room (except that one MIT directory)       | Rust + React       | 914 commits since 2026-04, one author; 4 in 30 days                          | Org gateway: guards, budgets, per-user MCP identity                                               |
| tonyweckstrom1987/holvi      | MIT                                                                                                                                     | lift                                             | Python             | 1 commit (squashed public release, 2026-10-17)                               | A tiny MCP credential broker (read-only tools)                                                    |
| IT-Hjaelp-Nu/keybox          | No licence file; `Cargo.toml` says `LicenseRef-Proprietary`, `publish = false`                                                          | clean room                                       | Rust               | 1 commit (squashed, 2026-11-01)                                              | **Per-repo, short-lived GitHub tokens** with passkey approval                                     |
| starbaser/ccproxy            | AGPL-3.0 with a commercial dual licence (root `LICENSE`)                                                                                | clean room                                       | Python (mitmproxy) | 720 commits; **none since 2026-08-09**                                       | Transparent MITM of a jailed process via rootless WireGuard namespace                             |
| liaohch3/claude-tap          | MIT                                                                                                                                     | lift                                             | Python             | 404 commits since 2026-02; 2 in 30 days; 142 tags                            | Trace capture and viewer for agent API traffic                                                    |
| KarpelesLab/teamclaude       | MIT                                                                                                                                     | lift (JS, so logic rewritten in Go)              | Node, no deps      | 596 commits since 2026-03; 102 in 30 days; 60 authors                        | **Plan 00146 grown up**: pooled subscriptions, rotation on quota, MITM for hardcoded hosts        |
| windtf/wireproxy             | ISC                                                                                                                                     | lift (with one caveat, below)                    | Go                 | 186 commits since 2022; 1 in 30 days                                         | Userspace WireGuard client exposing SOCKS5/HTTP/SNI proxies                                       |

---

## maximhq/bifrost

Studied: baseline at df44ee4ce01888263c2dad1d0eb5dea04b3e7f2e

Licence and reuse class: Apache-2.0, lift.

**What and for whom.** A high-throughput LLM gateway for organisations: one OpenAI-compatible
and Anthropic-compatible API over 20+ providers, with virtual keys (`sk-bf-*`), budgets, rate
limits, routing, failover, an MCP gateway, guardrails, a web UI and a CLI (`README.md`,
`core/`, `framework/`, `plugins/governance/`, `transports/bifrost-http/`). Enterprise builds
(the `ent-v*` tags, `ui/app/_fallbacks/enterprise/`) add SSO, clustering and the "Edge"
endpoint agent; the enterprise code does not appear to be in this repository (guess, from the
fallback UI stubs).

**Where it sits / how Claude Code reaches it.** Base URL only: `ANTHROPIC_BASE_URL=<gateway>/anthropic`
(`docs/cli-agents/claude-code.mdx`, tested on Claude Code 2.1.128 to 2.1.212). Five ways to
authenticate: a CLI launcher, a static virtual key in `ANTHROPIC_AUTH_TOKEN`, **a Claude
subscription passthrough** (Claude Code keeps its own `sk-ant-oat…` login, the virtual key goes in
`ANTHROPIC_CUSTOM_HEADERS: x-bf-vk: …` and Bifrost forwards the caller's own Authorization to
Anthropic), `apiKeyHelper`, and a `/login` against the gateway. The subscription path is detected
by a `Bearer sk-ant-oat` prefix (`transports/bifrost-http/integrations/utils.go`, around line 455)
and the passthrough headers are applied only when the provider is Anthropic, because the same
request handlers serve other providers and would otherwise leak the caller's credential to them
(`core/providers/utils/utils.go`, `setPassthroughHeaders`). No HTTPS proxy and no MITM in the
open-source gateway; "Edge" (enterprise, a menu-bar agent, `docs/edge/`) routes at machine level
and its mechanism is not documented in the repo.

**Credential injection.** Two kinds. (a) Provider keys held by the gateway, chosen per request
(`core/keyselectors/weightedrandom.go`), with *session affinity* that keeps a session on the key
that served it before, keyed under the caller's identity so two callers cannot share state by
picking the same session id (`core/sessionaffinity.go`). (b) **Relays** (`docs/relays/overview.mdx`,
`core/relay/`): any non-LLM HTTP API registered with a base URL and a stored secret. A call to
`/relay/<name>/<path>` goes only to that base URL, has caller credential headers (and a credential
query parameter, decoded by name so `api%5Fkey` is caught; `;` separators refused) stripped
and the stored one added (`core/relay/auth.go`), and must match an allow-listed route
(`METHOD /path`) unless "any path" is on. This is the devvm/00252 GitHub-narrowing idea as a
product feature: per-route allowlist plus injected secret plus a log row per call.

**Failover and usage limits.** `core/failureclass.go` is the most reusable piece of thinking:
it separates failures that should retry the same key (500/502/503/504 and Anthropic's 529,
"rotating would burn every key on a condition none of them can avoid"), failures that should
rotate to another key (rate-limit, quota/credit markers), and failures nothing helps. It reads
the provider's own error code or type first, the message second, the status last.

**Workload identity (new to us).** `core/providers/anthropic/workloadidentity.go` exchanges a
workload's OIDC token at Anthropic's `/v1/oauth/token` (jwt-bearer grant, an
`oidc-federation-2026-04-01` beta) for a short-lived `sk-ant-oat01` token, cached single-flight
and re-minted before expiry. That is for API organisations, not consumer subscriptions, but it is
the first evidence I have seen of Anthropic offering short-lived minted inference tokens.

**Audit.** Every LLM, MCP and relay call is a log row (content logging switchable); OTel and
Prometheus plugins.

**MCP gateway.** Per-virtual-key tool filtering, per-user OAuth to upstream MCP servers, a
"vouch" mark only core can set so plugins can tell a provider-injected tool from a caller's
(`core/mcpvouch.go`). No human approval flow for tool calls that I could find in the OSS tree.

**Maturity.** Very active, commercial backing (Maxim), large surface. Many commits are by a
release bot.

**Against ccy / Plan 00252.** Bifrost is a server for many users, with a database, UI and virtual
keys. ccy needs one owner's credentials kept out of a container. Adopting Bifrost as the 00252
component is out of proportion. Its subscription-passthrough mode *leaves the real token in the
client*, which is the opposite of what 00252 wants. What it changes for 00252: the relay model is a
ready-made description of "GitHub narrowing by route" (Q6) and of the logging it needs.

**Ideas.**

| Idea                                                                                                                          | Lands in                                                       | Class                    |
| ----------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------- | ------------------------ |
| Classify upstream failures as retry-same / rotate / give-up, provider code first, status last; never rotate on 529            | `src-go/internal/tokenproxy` (Plan 00146 Task 0.1, decision 5) | issue, once 00146 starts |
| Gate credential-bearing passthrough on the destination, not the call site, so one handler shared by two upstreams cannot leak | 00146 forwarder and any 00252 proxy                            | design note in 00252     |
| Strip caller credential headers *and* query parameters (name-decoded) before injecting the stored one                         | 00252 design                                                   | design note              |
| A "relay" per external API: fixed base URL, route allowlist, injected secret, one log row per call                            | 00252 GitHub narrowing (Q6)                                    | plan discussion          |
| Session affinity keyed under the caller's identity, not the session id alone                                                  | 00146 Phase 3 (host `cc`), any pooled forwarder                | design note              |

**Code worth lifting.** Small, and only with adaptation, since the files depend on Bifrost's
`schemas` types. No per-file licence headers; the root Apache-2.0 `LICENSE` applies, there is no
`NOTICE` file. Candidates: the response-header deny list `providerResponseFilterHeaders` and the
hop-by-hop handling in `core/providers/utils/utils.go`; the Accept-Encoding narrowing in
`setPassthroughHeaders` (never widen what upstream may send; pin `identity` when the filter
leaves nothing, so an SSE parser never sees an undecodable coding); `stripCallerQueryCredential`
in `core/relay/auth.go`. Keep the Apache notice and mark changes.

**Risks.** Huge, fast-moving dependency (about a thousand commits a month). Enterprise/OSS split
means features in the docs may not be in the code. Its "Claude subscription through Bifrost" mode
is documented by Bifrost, and Anthropic's gateway guide documents the same configuration; I state
no view beyond that.

---

## agentgateway/agentgateway

Studied: baseline at cec2c4eebf5d19e49df668f850c1446652b0bb8f

Licence and reuse class: Apache-2.0, lift; in practice ideas or a component.

**What and for whom.** A Linux Foundation project (`CHARTER.md`, LF Projects) and a Kubernetes
Gateway API implementation: an L7 proxy for LLM, MCP and A2A traffic with CEL policy, JWT/API-key/
OAuth auth, rate limits, guardrails and OTel (`README.md`, `crates/`, `architecture/`). Also runs
standalone from one YAML file.

**Where it sits / interception.** All three ways:

- Reverse proxy / LLM gateway behind a base URL; it recognises Claude Code specifically:
  session detection from `x-claude-code-session-id` (`crates/agentgateway/src/http/session.rs`),
  an "agent harness" field in CEL (`crates/agentgateway/src/cel/types.rs`), and handling of Claude
  Code's gzip-compressed request bodies and `oauth-*` betas (`crates/agentgateway/src/llm/mod.rs`,
  `crates/llm/src/conversion/bedrock.rs`).
- **HTTPS forward proxy (CONNECT)** with three modes in `examples/traffic-egress-proxy/`:
  `config.yaml` is an SNI hostname allowlist that never decrypts; `config-conditional.yaml`
  decrypts only named hostnames and passes the rest through; `config-dynamic-tls.yaml` terminates
  everything with per-host certificates from a configured CA (`tls.mode: dynamicCa`,
  `crates/agentgateway/src/types/dynamic_ca_cert.rs`).
- **Per-actor egress** for sandboxed agents ("Substrate actor egress",
  `crates/agentgateway/src/http/substrate/README.md`, `egress.rs`, code from 2026-08 to
  2026-11). The outer CONNECT is authenticated and resolved to an actor through a control API;
  the actor's policy is fetched per connection and per request and decides, per host and port,
  **intercept or pass through**. Requests inside an intercepted tunnel are authorised one by one;
  WebSocket upgrades and nested CONNECT are refused with fixed 403 bodies; missing or bad SNI,
  unknown protocols and failed policy lookups close the connection; an unmatched host is
  intercepted precisely so it can be refused with a 403 before any upstream is dialled. A
  `replace_headers` effect fetches a secret from a credential provider (gRPC, cached 5 minutes,
  4-second fetch timeout) and **replaces a header only if the actor's request already carries it,
  overwriting the placeholder value**.

**Credential injection.** `backendAuth` kinds (`crates/agentgateway/src/http/auth/mod.rs`):
passthrough, a configured key (or a value computed by CEL), GCP, AWS SigV4, Azure, GitHub
Copilot, per-request JWT signing, OAuth token exchange, cross-app access. Plus the per-actor
`replace_headers` above.

**Per-request policy.** CEL over the request, JWT claims, source, destination and MCP fields;
e.g. `examples/mcp-authorization/config.yaml` allows a tool by name and caller claim. Keyed and
global rate limits (`examples/llm-keyed-rate-limit`, `examples/mcp-keyed-rate-limit`).

**Account pooling / failover.** Load balancing, outlier detection, retries and budgets across
backends; nothing specific to subscription quota windows.

**Audit.** OTel logs, metrics, traces; access-log fields can be CEL expressions (the substrate
README shows recording the CONNECT authority beside the actor name).

**MCP gateway.** Federation of many MCP servers, stdio/SSE/streamable HTTP, OpenAPI-to-MCP,
OAuth for MCP, tool-level authorisation. No human approval step that I found.

**Maturity.** High: about 200 to 250 commits a month, 300 authors, 110 release tags (latest
v1.7.0), fuzzing (`fuzz/`), `cargo deny` and `osv-scanner` configs. Its `examples/openshell`
integrates NVIDIA OpenShell sandboxes.

**Against ccy / Plan 00252.** This is the closest thing in the whole star set to what 00252
describes, and it already answers several of 00252's open design points the way devvm did:
per-host choice between passthrough and interception; placeholder-only replacement; per-sandbox
identity on the outer CONNECT; fail closed on lookup errors. Two ways to use it:

1. **As the data plane.** Ship a pinned agentgateway release binary on the host, generate its
   YAML per session from the launcher, and point the container's `HTTPS_PROXY` at it. The static
   modes (SNI allowlist, conditional termination, `backendAuth` key injection) are enough for an
   Anthropic-and-GitHub binding without writing a proxy. The per-actor mode would need us to
   implement its control and credential-provider gRPC services (protos under `crates/protos`),
   which is a real piece of Go work but small compared with a proxy.
2. **As the reference design** for a smaller Go proxy of our own.

Costs of (1): a large Rust binary on the host that is not in Fedora's repositories, a YAML schema
that moves quickly, and a feature surface hundreds of times what we need (an attack and
maintenance surface on the most valuable process on the host, which is 00252 Q3's worry). It
would not replace Plan 00146's forwarder for the *switch* (the switch is a pool-and-name concept
agentgateway has no notion of), though `replace_headers` with a credential provider we write could
serve it.

**Ideas.**

| Idea                                                                                                                                                    | Lands in                          | Class                   |
| ------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------- | ----------------------- |
| Per host: SNI allowlist without decryption by default; terminate only hosts with a bound credential or a path rule                                      | 00252 design (Q2)                 | plan                    |
| Identity of the container on the outer CONNECT (Proxy-Authorization), not by source address                                                             | 00252 design                      | plan                    |
| Intercept an unmatched host so the refusal is an HTTP 403 with a reason, not a reset (better for the agent, matches 00232's "fail at once and say why") | 00252 design                      | plan                    |
| Refuse WebSocket upgrade and nested CONNECT inside an intercepted tunnel unless a rule allows them                                                      | 00252 design                      | plan                    |
| Replace a credential header only when the client already sent that header (with the placeholder)                                                        | 00146 Task 0.1 and 00252          | issue when 00146 starts |
| Read Claude Code's `x-claude-code-session-id` for attribution in the forwarder log                                                                      | 00146 forwarder log               | issue                   |
| Option: agentgateway as the 00252 data plane, ccy generating its config                                                                                 | 00252 Phase 2 (Task 2.2 names it) | owner decision          |

**Code worth lifting.** None in practice: Rust, and our tooling is Go. `crates/pool` carries its
own MIT licence (Sean McArthur), so a per-directory exception exists if it ever matters.

**Risks.** Adopting a component with this release cadence means an update discipline (like Plan
00169's for the ccy image). Its config examples ship a development CA under
`examples/mcp-tls/certs/`; never reuse example keys.

---

## ThinkWatchProject/ThinkWatch

Studied: baseline at 299f68d7d43265c045d7b6a64db7befb5fb287df

Licence and reuse class: BUSL-1.1, CLEAN ROOM.

Described in my own words only. No code, comments or prompts copied. The one MIT directory,
`crates/gateway/src/proxy/web_tools/` (`LICENSE.core-web-tools`), was not needed.

**What and for whom.** A self-hosted AI API and MCP gateway for organisations, with an admin
console, OIDC sign-in, roles, PostgreSQL, Redis and ClickHouse (`README.md`, `crates/`, `web/`,
`deploy/`). `LICENSING.md`: free in production up to monthly token and MCP-call thresholds,
commercial above; each version converts to GPL-2.0-or-later after four years. The README points
individuals at a separate MIT desktop app, ThinkWatch Lite, which is **not** in our clone set and
may be worth a look as a permissive alternative (not checked).

**Where it sits.** A base-URL gateway on one port for clients (Claude Code included), a console
on another. Users get gateway virtual keys; provider keys stay in the gateway.

**What is distinctive (behaviour, from `README.md` and module docs in
`crates/gateway/src/guards.rs`, `redaction.rs`, `tool_inspection.rs`):**

- **Outbound secret and personal-data redaction with restoration.** Before a request leaves for
  the model provider, every text position in it (system prompt, all messages, tool arguments, tool
  results, tool descriptions) is scanned; matches are swapped for numbered stand-ins, and the
  stand-ins are swapped back in the answer, streamed answers included. Protocol fields that must
  stay exact (ids, roles, tool names) are skipped.
- **Dangerous-tool-call guard on the response side.** The model's tool calls are inspected as they
  stream back; a matching call is cut off or recorded. Two built-in rules flag a credential sent to
  an unfamiliar host and a local file uploaded to an external host.
- **Monitor before enforce.** Each guard is on, monitoring or off; a new install starts in
  monitoring, so hits go to the audit log before anything is blocked.
- **One policy snapshot per request**, swapped whole on change, so a request is never redacted
  with old rules and restored with new ones.
- **The audit log carries no request text**: events name the rule and count matches, with a few
  masked characters at most, because the audit log is read more widely than request bodies.
- **MCP tools run as the real user**: each user links their own upstream account (OAuth or a
  personal token); an unlinked user sees the tools, and a call returns a JSON-RPC error carrying
  the authorisation URL. Per-user responses are never shared from cache.
- **Limits fail open by default** if Redis or the database is down; a setting makes them fail
  closed.

**Maturity.** 908 of 914 commits by one author in about seven months, then a sharp slowdown (4
commits in the last 30 days). Guess: largely agent-written. Treat claims as claims.

**Against ccy / 00252.** Not a component for us (licence, size, multi-tenant). Two ideas bear on
ccy's actual threat model ("a misled agent exfiltrates what it holds", docs/ccy.md):

| Idea                                                                                                      | Lands in                                                                | Class              |
| --------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------- | ------------------ |
| Monitor-then-enforce mode for any new egress rule set (log what would be refused before refusing)         | 00252 rollout; 00232 already has `off`, this would be a "log only" tier | plan discussion    |
| Audit events that name the rule and never carry the request text                                          | 00252 proxy log                                                         | design note        |
| A response-side check for tool calls that send a credential-shaped string to a host outside the allowlist | would need TLS interception of the Anthropic stream; 00252 extension    | plan, low priority |
| Request-side redaction of secrets the agent read from the workspace before they reach the model provider  | not ccy's threat model today; note only                                 | none               |

Fail-open limits contradict this repository's fail-fast rule; do not copy that default.

---

## tonyweckstrom1987/holvi

Studied: baseline at 0930ee58f19014ab7ba7c07b3f6f765ae5ca7517

Licence and reuse class: MIT, lift.

**What and for whom.** A home-lab credential broker as a small MCP server (about 640 lines of
Python, Finnish docs with an English summary, `README.md`). Agents get narrow **read-only tools**
(list own repos, recent commits, read a file; Gmail search and read) and never see the tokens.

**Architecture.** Two processes on two ports (`src/holvi/palvelin.py` for GitHub,
`src/holvi/posti.py` for mail), deliberately separate because the mail-reading agent is the most
exposed to prompt injection and must never see the GitHub tools. Not a proxy: the broker makes the
upstream call itself and returns only a trimmed result. Layers (README "Turvamalli"): firewall
per client port (`esimerkit/nftables.example.conf`) as the real boundary; tool permissions by
source IP from a rules file, fail closed when the file is missing or broken; read-only by scope
(Gmail `readonly`) and by method (GitHub GET only, fine-grained read token recommended); strict
input character classes and no `..` (`src/holvi/github.py` lines 17 to 24); a deny list of
sensitive file names (`.env`, private keys, `.npmrc`, credentials files); every returned body
wrapped with a "this is external data" warning field; hard size and count caps; 30 requests per
minute per IP; an audit log of who, which tool, parameters and outcome, never keys or fetched
content, mode 640; Telegram alerts on refusals and on expired or revoked credentials.

**Maturity.** One squashed commit; derived from a private predecessor; 101 tests against a fake
network only (README "Testit"). Not battle-tested in this form, by its own account.

**Against ccy / 00252.** A different shape from a proxy: *tools instead of access*. For ccy the
relevant point is the split by exposure: a session that reads untrusted input should not hold
write credentials. 00252's per-container bindings get the same effect if bindings differ per
session.

| Idea                                                                  | Lands in                                                           | Class           |
| --------------------------------------------------------------------- | ------------------------------------------------------------------ | --------------- |
| Return fetched third-party content marked as external data            | not applicable to a proxy; note for any future MCP broker          | none            |
| Refuse to serve sensitive file names through a credentialed read path | any future GitHub relay rule set                                   | note            |
| Alert the owner on refusals and on an expired upstream credential     | 00252 proxy; ccy already has desktop notifications planned (00224) | plan discussion |

**Code worth lifting.** Only the sensitive-file-name regex in `src/holvi/github.py`
(`ARKAT_TIEDOSTOT`, around line 24), MIT, keep the notice. Small enough that rewriting is as easy.

**Risks.** Identity by source IP is weak, which the README states itself. No TLS (README says to
put it behind a VPN or a TLS proxy).

---

## IT-Hjaelp-Nu/keybox

Studied: baseline at a56a19a1c94d7e6735e908220c55cf7b7b424b6d

Licence and reuse class: proprietary, CLEAN ROOM.

No licence file at the root or in any directory; `Cargo.toml` declares `LicenseRef-Proprietary`
and `publish = false`. Nothing may be copied. Described in my own words.

**What and for whom.** One company's credential broker for its own AI agents, written in Rust,
docs in Danish with an English summary (`README.md`, `SECURITY.md`, `docs/`). Agents hold **no
standing access**. An agent asks for one specific grant, described by customer, system, action,
target, commit and reason. The request is checked against a policy file the owner has signed off.
Higher-risk requests need the owner's passkey (WebAuthn) approval on a phone, pushed as an
encrypted notification; the riskiest tier adds a cancellable delay. Then the broker hands out a
narrow credential that expires by itself, once.

**Architecture.**

- One daemon, two interfaces, both listening only on a private overlay network (Tailscale) and
  only for listed addresses. The agent interface is mutual TLS with a client certificate per agent
  and a one-line JSON protocol (list, request, status, fetch once, withdraw). It cannot approve
  anything. The approval page is HTTPS with passkeys; refusing needs no passkey, approving does.
- A pure core crate with no I/O decides everything security-relevant: a request state machine
  (pending, delayed, ready, delivered, expired after five idle minutes, cancelled, revoked on
  emergency stop or a policy change that no longer allows it) (`crates/keybox-core`).
- Policy files hold the *names* of secrets, never secrets; they live in a private repository; a
  policy change is only a proposal until the owner approves it with a passkey, and the daemon can
  print each rule in plain words for review (`crates/keybox-policy`, `policy.example.toml`).
- Long-lived secrets (the GitHub App's private key, SSH CA keys) stay in a password manager
  (Vaultwarden) and are fetched by the daemon only.
- An append-only log with a hash chain (`crates/keybox-log`).
- `unsafe` forbidden workspace-wide; `cargo audit` and `cargo deny`; locked toolchain.

**GitHub narrowing, the important part (`docs/GITHUB-APP.md`).** A GitHub App installed on the
organisation, on selected repositories only, whose repository permissions are the *ceiling*. For
each grant the broker asks GitHub for an **installation token restricted to exactly one repository
and exactly the permissions the policy names**, valid one hour. It then **reads GitHub's answer,
and if the token covers more repositories or more permissions than asked, it revokes the token at
GitHub and refuses to hand it out**. The docs add that a GitHub token cannot be limited to a
branch, so branch protection on `main` is what stops a writer from pushing there.

**SSH (`docs/SSH.md`).** The agent makes its own key pair; the broker signs a short-lived SSH
certificate for it: one user name, only from the agent machine's address, optionally one forced
command, from a per-customer CA, so a certificate for one customer never works at another.

**Maturity.** One squashed commit; a single-company tool; browser tests exist (`tests/browser`).

**Against ccy / 00252.** This is the strongest answer in the group to 00252 Q6 ("path rules on
one token, or per-repo tokens?") and to the residual risk docs/ccy.md names ("anything your `gh`
token can reach on GitHub"). A per-session GitHub App installation token scoped to the project's
own repository needs **no proxy, no CA and no TLS interception**: the host mints it at launch, the
container gets a token that can only touch one repository, and it dies within the hour. It
composes with 00116 (deploy key for git over SSH) and with 00252 (the proxy could hold and refresh
it, so the container sees only a placeholder). The approval flow is more than ccy needs today, but
"the agent asks, the owner approves on a phone" fits the agent-team work (00197's seat broker
already has an approval step).

| Idea                                                                                                                        | Lands in                                                                                       | Class                                                              |
| --------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ |
| **Per-session GitHub App installation token, one repo, minimum permissions, verified after minting and revoked if broader** | ccy launcher (`src-go/internal/ghlogin` / `ccylaunch`), replaces the full `GH_TOKEN`; 00252 Q6 | plan (needs a GitHub App per owner, vault storage of its key, IaC) |
| Branch protection as the stated boundary for any write-capable agent token                                                  | docs/ccy.md Security Model                                                                     | issue (docs)                                                       |
| Short-lived SSH certificates from a host CA instead of mounting a key (one principal, source address, forced command)       | 00116 follow-up; for LXC/remote team members (00220)                                           | plan discussion                                                    |
| Policy holds secret names only; a policy change takes effect only after the owner approves                                  | 00252 bindings file                                                                            | design note                                                        |
| Request state machine in a pure, I/O-free package                                                                           | any Go broker we write                                                                         | design note                                                        |

**Risks.** An installation token expires in an hour, so a long ccy session needs a refresh path
(the host forwarder, or a credential helper calling the host). GitHub App setup is per owner and
manual, so it is an opt-in mode, not the default.

---

## starbaser/ccproxy

Studied: baseline at f2c47695b0835da023257aee0ac2a3dffd9fe570

Licence and reuse class: AGPL-3.0 plus commercial, CLEAN ROOM.

Described in my own words only.

**What and for whom.** A transparent interceptor for LLM CLIs, started for Claude Code, built on
mitmproxy (`README.md`, `docs/inspect.md`). Two entry points: a reverse proxy behind a base URL,
and a **namespace jail**: it runs the target command in a rootless user and network namespace
whose only route is a WireGuard tunnel into mitmproxy's WireGuard listener, so all traffic is
captured with no proxy variables or base URL in the client. It needs `slirp4netns`, `wg`, `ip`,
`iptables` and `unshare`, fails before creating anything if one is missing, and works on Linux and
WSL2 only (README "Platform support").

**Pipeline.** A fixed chain of mitmproxy add-ons feeding a DAG of hooks (`src/ccproxy/pipeline/`,
`src/ccproxy/hooks/`): cross-provider translation, cache breakpoint placement, session id
extraction, credential injection, MCP notification bridging.

**Credential injection.** Credential sources per provider (`docs/configuration.md`, "auth"): a
shell command, a file, or OAuth sources that refresh tokens in-process and write them back
atomically. A **sentinel key** of a fixed shape in the client's environment triggers substitution
of the real token for a named provider, the same placeholder idea as Plan 00146.

**What makes it a ToS concern (facts).** "Request shaping" (`docs/shaping.md`) replays a captured
"compliance envelope" from the official SDK or CLI (beta headers, client identity headers, user
agent, the first system-prompt block, the metadata identity) around the content of a request from
a *different* client. "Fingerprint capture" (`docs/fingerprint.md`) records the original client's
TLS ClientHello fingerprint and replays requests through an impersonating sidecar. Combined with
OAuth sources that read Claude Code's own credentials file, the stated purpose is to let other
clients and providers be driven through subscription logins. The Claude Code legal page quoted in
Plan 00146's research bars routing third-party clients through subscription credentials. ccy
keeps the official client unmodified, so none of this applies to us; it is a pattern to keep out.

**Maturity.** Dormant: no commits since 2026-08-09; four authors; three tags.

**Against ccy.** The namespace jail is the one transferable idea: a rootless way to force a
process's traffic through an intercepting proxy without trusting the process to honour
`HTTPS_PROXY`. ccy already gets the same guarantee more simply, because Plan 00232 holds an
nftables table in the container's namespace from the host. If 00252 needs a transparent mode
(devvm's layer 3), redirecting the container's port 443 to the host proxy from that same nftables
table is the ccy-shaped equivalent; no WireGuard needed.

| Idea                                                                          | Lands in                                           | Class |
| ----------------------------------------------------------------------------- | -------------------------------------------------- | ----- |
| Transparent capture by owning the namespace's routing, not by proxy variables | 00252 Task 2.1 (spike), via 00232's nftables table | plan  |
| Hard-fail before any setup when a prerequisite tool is missing                | already our rule                                   | none  |

Nothing to lift (AGPL).

---

## liaohch3/claude-tap

Studied: baseline at b21d17157b674a0562e0ed08f4c4c607e8e5a56f

Licence and reuse class: MIT, lift.

**What and for whom.** A local proxy and trace viewer for coding-agent traffic: run Claude Code
(and about fifteen other CLIs) through it, then inspect system prompts, tool schemas, tool calls,
reconstructed SSE streams and token usage in a self-contained HTML viewer, with diffs between
adjacent requests (`README.md`, `claude_tap/`). About 19k lines of Python.

**Interception.** Reverse mode sets the client's base URL (`claude_tap/cli_clients.py`, around
line 388). Forward mode sets `HTTPS_PROXY`, empties `NO_PROXY`, and points
`NODE_EXTRA_CA_CERTS` (and `SSL_CERT_FILE` for non-Node clients) at a CA it generates
(`claude_tap/certs.py`, `claude_tap/forward_proxy.py`, `cli_clients.py` around line 708 to
736). A "global" mode edits `~/.claude/settings.json` to set the base URL and restores it
byte for byte from a backup (`claude_tap/global_inject.py`).

**Credentials.** Pass-through only: the client's own credential goes upstream; in the trace,
sensitive headers are replaced by `***` with no prefix kept (`claude_tap/proxy.py`,
`SENSITIVE_HEADER_KEYS` and `filter_headers`, around lines 40 to 80). Request and response
**bodies are kept in full**, so a trace holds everything the agent read.

**Maturity.** 142 tags (v0.1.146), 36 contributors, but only 14 commits in the last 90 days.

**Against ccy.** Not a credential tool. Two uses for us:

| Idea                                                                                                                                                                                          | Lands in                                                                           | Class                               |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ----------------------------------- |
| A per-Claude-Code-version capture of every host and path the client calls, as a regression oracle for the 00146 forwarder (its Risks name "upstream drift: a release could add direct calls") | 00146 Phase 0 spike and its acceptance re-check; 00169 (image keeps tools current) | issue when 00146's spike is written |
| A session trace for debugging what a ccy agent actually sent (opt-in, host side, bodies are sensitive)                                                                                        | a 00252 debug mode                                                                 | plan discussion, low priority       |

**Code worth lifting.** The self-contained `claude_tap/viewer.html` (MIT) could be reused whole
for a trace viewer if we ever build one; the header redaction list is trivial. Python, so any
proxy logic would be rewritten in Go anyway.

**Risks.** Traces contain secrets the agent read; the forward-mode CA key sits in `~/.claude-tap`
for five years (`certs.py`), the opposite of devvm's 30-day per-VM CA.

---

## KarpelesLab/teamclaude

Studied: baseline at 4c044d3295af4975ce06b1206ce409f0daec572c

Licence and reuse class: MIT, lift.

**What and for whom.** A local, dependency-free Node proxy that pools one person's Claude Max,
Codex, API-key and third-party accounts and rotates on quota, so a fleet of agent sessions keeps
running (`README.md`, `docs/`). Explicitly scoped to "one person, their own accounts, the
provider's own client" (`docs/compliance.md`). Very active (102 commits in 30 days), issue
numbers in the docs show a large user base. Being renamed TeamRouter.

**Where it sits / interception.** Two modes (`docs/proxy-modes.md`):

- `--no-mitm`: `ANTHROPIC_BASE_URL` to the proxy.
- **MITM (the default for `teamclaude run`)**: `HTTPS_PROXY` plus `NODE_EXTRA_CA_CERTS`, a CA
  trusted only by the launched `claude` process, never the system store; the CA private key is
  never written to disk (so each renewal brings a new CA); the leaf key is 0600; upstream
  certificates are still verified. **Why MITM is the default: some Claude Code features use a
  hardcoded `https://api.anthropic.com` and ignore `ANTHROPIC_BASE_URL`** (the docs name the
  Claude Design MCP).
- Inside the MITM tunnel only model traffic is pooled. Client token refresh
  (`/v1/oauth/token`), trusted-device enrolment, Remote Control (`/v1/code/*` and the bridge
  paths), bootstrap (`/api/claude_cli/*`), policy limits, feature evaluation, the WebFetch domain
  check (`/api/web/domain_info`) and the MCP registry go out **on the client's own login**.

**Credential injection.** The client holds its own login (or a proxy key); the proxy injects the
chosen account's token, drops any client `x-api-key`, and **rewrites the `account_uuid` inside
`metadata.user_id` in the request body** so the body names the account whose token was injected
(`src/account-uuid-rewrite.js`, a streaming, same-length byte patch). A pin per session is passed
as the proxy URL's user name in MITM mode (`TC_ACCT`, removed from the environment before
`claude` starts), or set at run time per `x-claude-code-session-id` (`teamclaude switch <acct> --session`). Per-client keys (`proxy.clientKeys`) tell one owner's machines apart and can be
revoked singly.

**Quota and failover (`docs/routing.md`, `docs/quota.md`), the most valuable content here:**

- Quota is learned from `anthropic-ratelimit-unified-*` response headers (5h and 7d windows,
  per-model weekly buckets).
- **Two kinds of 429.** A quota rejection (`unified-*-status: rejected`) rotates; a per-minute
  rate-limit 429 does not rotate as a policy (it would move the burst and lose the prompt cache)
  but pauses the account and absorbs short `retry-after`s; a 429 with no rate-limit headers is a
  refusal of that request; `x-should-retry: false` is passed to the client as is. One failover
  hop only on a rate limit or 5xx: if the second account is limited too, the limit is probably on
  the egress IP.
- Billing refusals, `oauth_not_allowed_for_organization`, account holds and
  `oauth_scope_insufficient` (a setup-token token has `user:inference` only and cannot read
  profile or usage endpoints) each get their own handling.
- **Storm control**: after a switch, concurrency on the new account ramps from 1, so fifty agents
  failing over at once do not throttle it.
- **Conversation pinning** keyed by session id plus a digest of the conversation's opening, so
  sub-agents of one session can spread while forked sub-agents stay with the cache they inherit.
- **Prompt cache and thinking blocks**: the cache is per organisation, so a switch between
  personal subscriptions costs one cold turn; thinking blocks are bound to the organisation that
  produced them (docs cite Anthropic's preserved-thinking docs). `compactBeforeSwitch` compacts on
  the old account before moving.
- **Egress pinning** (`src/egress-guard.js`, opt-in): hold requests if the exit IP is not the
  pinned one, because a request from an unexpected region gets a 403 that Claude Code reads as a
  dead session.
- Long-lived login (`docs/accounts.md`): the same request `claude setup-token` makes, one year,
  `user:inference` only, never refreshed; the docs list exactly what such a token cannot do.

**Audit.** Optional per-request log files with size caps and retention; request correlation via
`x-request-id`; usage history in SQLite by consumer, model and account; a TUI and dashboard.

**MCP.** An opt-in MCP endpoint that lets Claude Code read the fleet's quota and, at `full`,
switch accounts or change settings from inside a session.

**ToS facts (no judgement).** The maintainer's own write-up (`docs/compliance.md`, issue #107)
argues that automated switching between one person's own subscriptions, through the genuine
client, is consistent with Claude Code's terms, and says Anthropic has not explicitly blessed
automated pooling. Features beyond plain rotation that the owner should know exist:
the `account_uuid` body rewrite; **keep-warm**, which sends real one-word `claude -p` messages to
start idle accounts' five-hour windows early (`docs/quota.md`); automatic redemption of banked
usage-limit resets; an optional paid extra-usage fallback; and an **sx.org residential proxy mode
that retries on a fresh egress IP when a 429 is scoped to the IP** (`src/sx.js`,
`docs/proxy-modes.md`, with a referral link). Plan 00146's terms quotes (advertised limits
"assume ordinary, individual usage"; Anthropic "may take measures … without prior notice") apply
to these. Plan 00146 already decided on manual switching only.

**Against ccy / Plans 00146 and 00252.** teamclaude is what 00146 becomes if every non-goal is
dropped. Three findings change or sharpen our plans:

1. **`ANTHROPIC_BASE_URL` alone does not see all of Claude Code's Anthropic traffic.** 00146's
   decision 8 lists "features that call `api.anthropic.com` directly" carrying the placeholder;
   teamclaude's path list is a concrete inventory of them (bootstrap, policy limits, feature
   evaluation, WebFetch domain info, MCP registry, Remote Control, trusted devices, and the Claude
   Design MCP). With a placeholder-only container, every one of those fails unless the proxy also
   intercepts `api.anthropic.com` (00252 Q2) or the skip settings cover them. This is evidence for
   the 00146 spike, which should check each path.
2. **`metadata.user_id` carries an `account_uuid`.** Under a switch the body names one account
   and the token another. teamclaude patches it; 00146 does not mention it. Whether a mismatch
   matters is unknown (guess: it at least affects Anthropic's attribution). With setup-token
   tokens, which cannot read the profile, it is not even clear what Claude Code puts there under a
   placeholder; the spike should record it. Rewriting it is an owner decision with a terms angle.
3. The 429 taxonomy and the scope-denial list are what 00146's decision 5 ("live usage from the
   forwarder") needs to label a token correctly, and what Plan 00251's `rate-limited` trigger
   should be keyed on (quota rejection, not every 429).

| Idea                                                                                                           | Lands in                                     | Class                   |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------- | ----------------------- |
| Inventory Claude Code's direct `api.anthropic.com` paths in the 00146 spike; decide intercept vs skip per path | 00146 Task 0.2/0.3 and decision 8; 00252 Q2  | issue (spike checklist) |
| Record and decide on `metadata.user_id.account_uuid` under a switch                                            | 00146 decisions                              | owner decision          |
| Tell quota rejection from rate-limit 429 from request refusal; record per token name                           | 00146 decision 5; 00251 trigger              | issue when 00146 starts |
| Proxy CA: trusted by the launched process only, key never on disk, short life                                  | 00252 Q2/Q3 (if TLS interception is chosen)  | design note             |
| Pin per session carried on the proxy credential (`user@` of the proxy URL)                                     | 00252: the per-container identity on CONNECT | design note             |
| Egress-IP check before spending a request                                                                      | ccy hosts behind a VPN; low priority         | note                    |
| Session switch by `x-claude-code-session-id` from outside the session                                          | 00146 Task 2.1 (`--switch-token --session`)  | design note             |
| Never let the agent switch its own credentials (teamclaude's MCP `full` mode does)                             | 00146/00252 principle                        | design note             |

**Code worth lifting.** MIT, but Node; nothing to copy into Go verbatim. The value is the
documented behaviour, which is free to reuse. `src/account-uuid-rewrite.js` is a good model for a
streaming same-length patch if we ever need one.

**Risks.** Plaintext token store in `~/.config/teamclaude.json`; an optional third-party token
sync service (callback.net); the features listed under ToS. Its rotation is automatic by design,
which Plan 00146 rules out.

---

## windtf/wireproxy

Studied: baseline at e00074911d2e8b23bac6b74703fa1a2b522cff2b

Licence and reuse class: ISC, lift; light pass.

**What.** A userspace WireGuard client (wireguard-go plus a userspace network stack) that exposes
SOCKS5, HTTP CONNECT and a transparent SNI proxy, with no root and no kernel interface
(`README.md`, `wireguard.go`, `http.go`, `sni.go`). Since 2026-08 it can tunnel only hosts that
match a domain allowlist and dial the rest directly, logging each decision so a user can learn
what to list (`router.go`).

**Relevance.** Egress through WireGuard without touching host networking: a ccy container (or a
host proxy) could leave through a remote WireGuard peer as a rootless process, e.g. to give a
session a fixed exit address, or to reach a remote team's network (Plan 00220 builds a kernel
WireGuard overlay through IaC instead, which is the right choice for a hub). No credential or
policy features.

**Lift, with a caveat.** `sni.go` (96 lines, Go) peeks the ClientHello for SNI without consuming
it, which a 00252 transparent mode needs. Its header says the approach is "based on" Andrew Ayer's
sniproxy from a blog post, whose licence is not stated in the file. ISC covers wireproxy's
contribution; the origin is unclear, so write our own (Go's `crypto/tls` with a read-only
connection is a standard technique) rather than copy. Do not copy the sample key in the README.

**Maturity.** Maintained, slow (one commit in 30 days); 51 contributors.

---

## Group section

### The five strongest takeaways, ranked

1. **Per-repository GitHub tokens beat path rules (keybox).** Mint a GitHub App installation
   token for the project's one repository and minimum permissions at launch, verify the answer,
   revoke if broader. It closes the docs/ccy.md "anything your `gh` token can reach" risk with no
   proxy, no CA and no interception, and it answers 00252 Q6. It is independent of whether 00252
   proceeds, so it can be its own plan. Clean room: work from this report, not the source.
2. **`ANTHROPIC_BASE_URL` does not cover all of Claude Code's Anthropic traffic (teamclaude).**
   A placeholder-only container will break every hardcoded `api.anthropic.com` call. Either the
   proxy intercepts `api.anthropic.com` too (00252's TLS route), or the 00146 spike proves each
   such feature can be switched off. This decides 00252 Q2 more than any other fact here, and it
   should be the first thing the 00146 spike measures, along with what lands in
   `metadata.user_id`.
3. **The proxy design 00252 sketches already exists in a well-maintained Apache-2.0 project
   (agentgateway).** SNI passthrough by default; interception only for hosts with a binding;
   per-container identity on CONNECT; placeholder-only header replacement; fail closed on lookup
   errors; refuse nested CONNECT and WebSocket in intercepted tunnels; a 403 with a reason for an
   unknown host. Use it as the reference, and keep it as an option for the data plane.
4. **Classify upstream failures before acting on them (teamclaude, bifrost).** Quota rejection,
   rate-limit throttle, request refusal, billing refusal, scope denial and transient 5xx/529 each
   need a different response. Plan 00146's usage readout and Plan 00251's trigger should key on
   "quota rejected", not on any 429.
5. **Keep the audit log free of request text, and roll rules out in monitor mode first
   (ThinkWatch, holvi).** Whatever proxy 00252 builds logs rule, host, method, path, binding name
   and outcome, never bodies or credential values, and new rule sets can run log-only before they
   refuse.

### Recommendation for Plan 00252

**Build our own, small, in Go; borrow specific parts; do not adopt any of these as the
component now.**

- Start from Plan 00146's forwarder exactly as designed, and let its spike answer takeaway 2. If
  the spike shows the hardcoded calls matter, extend the same host process into a CONNECT proxy
  that passes everything through by SNI and terminates only `api.anthropic.com` (and later the
  GitHub hosts), with agentgateway's behaviours from takeaway 3 as the specification and a CA
  trusted only by the container, key kept in memory, short-lived (teamclaude's and devvm's
  practice).
- Take GitHub narrowing **out** of the proxy question: per-session GitHub App installation tokens
  (takeaway 1), with path rules in a proxy only as a second line later, as devvm itself advises.
- Keep agentgateway as the named fallback if our proxy grows past a few hundred lines of policy
  code: generate its YAML from the launcher and run a pinned release on the host. Bifrost and
  ThinkWatch are multi-tenant servers and wrong-sized; ccproxy's techniques are a terms risk;
  teamclaude's automatic rotation is outside 00146's decided scope.
- Code to lift is minor (bifrost's header deny list and Accept-Encoding rule, Apache-2.0 with
  notice). The rest is design, which is free to reuse for the permissive repos and goes through
  this report for keybox, ThinkWatch and ccproxy.

### Watch list

Already on the watch list (rows in CLAUDE/Watchlist.md): all nine except wireproxy. Suggest
keeping teamclaude (behaviour of Claude Code under proxies, changes often) and agentgateway (the
egress and per-actor work); bifrost could drop to low priority; ccproxy is dormant. ThinkWatch
Lite (MIT, separate repository) was not studied and may be worth one look.
