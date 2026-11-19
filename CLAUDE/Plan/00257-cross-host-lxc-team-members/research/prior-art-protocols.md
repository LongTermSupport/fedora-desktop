# Prior art: agent protocols, gateways and credential brokers

Seven public projects were read (READMEs, and for A2A and Key-Box the spec and docs). Each is
judged against the weaknesses of the v2 team bus:

- **W1**: one homeserver per team.
- **W2**: an LXC member works only on the bus host.
- **W3**: a seat is tied to the host it runs on.
- **W4**: a seat on another machine needs ssh + sudo + hand-piped credentials + a network CIDR
  - the team's TLS CA.
- **W5**: host suspend handling.

Licence note: Apache-2.0, MIT and ISC code may be copied into this repository, keeping the
original licence text, copyright lines and (for Apache-2.0) the NOTICE file and a note on
changed files. A repository with no open licence may not be copied at all; only its ideas may
be reused.

## a2aproject/A2A (Agent2Agent protocol)

- **Solves:** a vendor-neutral protocol for opaque agents to call each other: discovery,
  long-running tasks, streaming, push notifications. Spec plus SDKs (Go, Python, JS and more).
- **Identity/credentials:** none issued by the protocol. Each agent publishes `securitySchemes`
  (API key, HTTP bearer, OAuth2, OIDC, mTLS) in its Agent Card. An authenticated client can
  fetch an extended card. Cards may carry JWS signatures over a JCS-canonicalised (RFC 8785)
  body.
- **Transport:** JSON-RPC 2.0, gRPC or HTTP+JSON over HTTPS. SSE for streams, webhook POSTs for
  push.
- **Discovery:** `/.well-known/agent-card.json` per agent, or a registry.
- **Multi-host:** point-to-point HTTP between agents that can reach each other. There is no
  bus, no fan-out, no rooms and no buffering for an offline peer beyond push webhooks.
- **Answers:** none of W1-W5 directly. A per-agent HTTP server is worse than a Matrix room
  for agents behind NAT or asleep.
- **Verdict: crib ideas.**
  - A **signed, canonicalised member card** (JCS + JWS) that states a seat's identity, kind and
    team url. Any host can verify it against the team root key, with no ssh to the bus host.
  - Task states `input-required` / `auth-required` map well onto our ping rules.
- **Licence:** Apache-2.0, copy OK. **Maturity:** Linux Foundation project, about 26.6k stars,
  pushed in the last day.

## agentgateway/agentgateway

- **Solves:** a Rust data-plane proxy for agent traffic: LLM gateway, MCP gateway and A2A
  gateway, with one place for auth, RBAC, rate limits and telemetry. Runs standalone (YAML) or
  under Kubernetes Gateway API.
- **Identity/credentials:** JWT, API keys, OAuth and mTLS at the edge. CEL policy expressions
  for fine-grained RBAC.
- **Transport:** HTTP, SSE, streamable HTTP and stdio, all through the proxy.
- **Discovery:** from config (federated MCP tool lists, A2A cards passed through).
- **Multi-host:** one gateway that clients reach. It is a hub, like our homeserver.
- **Answers:** none of the five. It is the "credential-holding per-host proxy" (Option B in
  ARCHITECTURE.md) at industrial scale. It confirms that such a proxy is only safe with strong
  per-client authentication in front of it, which is the per-seat token we already have.
- **Verdict: not relevant** to adopt. At most, CEL as a model for a declarative "who may
  message whom" policy.
- **Licence:** Apache-2.0. **Maturity:** Linux Foundation project, about 6.3k stars, pushed
  today.

## agent-substrate/substrate

- **Solves:** a zero-trust Kubernetes runtime for millions of mostly-idle agents. Actors run in
  gVisor or microVM sandboxes, are snapshotted out when idle and restored onto any warm worker
  in under 500 ms.
- **Identity/credentials:**
  - every actor gets a short-lived **SPIFFE X.509** identity, plus per-actor JWTs from an
    OIDC-discoverable issuer that outside parties can federate with;
  - signing-key rotation is documented;
  - credentials never enter the sandbox: an egress gateway injects them on approved outbound
    requests.
- **Transport:** all ingress goes through a router (Envoy). No lateral actor-to-actor traffic.
- **Discovery:** Kubernetes API objects (Actor, ActorTemplate, WorkerPool, "atespace").
- **Multi-host:** native. **Identity belongs to the actor, not to the worker or node.** An
  actor "teleports" between workers, and the router **parks** requests while no worker holds
  it.
- **Answers:**
  - **W3:** identity is decoupled from placement.
  - **W5:** suspend is a first-class state, and requests wait for resume instead of failing.
  - Indirectly W4: a short-lived identity minted by an issuer that every node trusts.
- **Verdict: crib ideas.** The Kubernetes stack is far too heavy to adopt.
  - A seat identity that names the member, not the host.
  - Suspend/resume as a modelled member state, with messages held for resume.
  - A short-lived certificate in place of a long-lived token.
- **Licence:** Apache-2.0, copy OK. **Maturity:** 1.0 released, about 7.7k stars, pushed
  today.

## windtf/wireproxy

- **Solves:** a pure-userspace WireGuard client. It needs no root and no TUN device. It
  exposes SOCKS5/HTTP proxies and static TCP tunnels in both directions (client to peer, and
  peer to local service), plus a stdio tunnel for ssh `ProxyCommand`.
- **Identity/credentials:** WireGuard key pairs. Each peer is its public key, with an optional
  pre-shared key. Keys can come from environment variables.
- **Transport:** WireGuard over UDP. Inside it, TCP only (no UDP yet).
- **Discovery:** none, static config in wg-quick syntax.
- **Multi-host:** yes. Every participant gets a stable overlay address whichever host or NAT it
  sits behind.
- **Health:** `--info` serves `/metrics` (as `wg show`) and `/readyz`. `/readyz` returns 503
  when no pong has come back from a `CheckAlive` target within the interval plus 2 s.
- **Answers:**
  - **W4's network half:** a team could admit one overlay subnet instead of each host's CIDR.
  - **W2:** an unprivileged guest or podman container can join the overlay with no host
    networking change and no root.
  - **W5:** the pong-age readiness check is a cheap suspend/partition detector.
- **Verdict: crib ideas, possible adopt** as an optional member-side transport. It is Go, built
  on wireguard-go and gVisor netstack, so the library route fits `src-go/`. It still needs a
  WireGuard endpoint on the bus side and key distribution, which moves W4's problem rather than
  removing it.
- **Licence:** ISC (permissive, MIT-equivalent), copy OK. **Maturity:** about 5.9k stars,
  pushed in the last week. A single-maintainer style project.

## IT-Hjaelp-Nu/keybox (Key-Box)

- **Solves:** a small Rust credential broker for AI coding agents. Agents hold no standing
  access. They request one specific permission, which is checked against a signed-off policy.
  Higher tiers need a WebAuthn passkey approval, and the top tier adds a cancellable delay.
  The broker then hands out a short-lived GitHub App token or SSH certificate.
- **Identity/credentials:**
  - **one mTLS client certificate per agent**;
  - the agent API is `list/request/status/fetch/withdraw`, one JSON line each way;
  - the agent API can never approve anything;
  - **SSH: the agent generates its own key pair and sends only the public key; the broker
    signs a certificate.** The certificate is short-lived, has one principal, is bound to a
    source address and can carry a forced command;
  - a new passkey needs a one-time token from the server console.
- **Request states:** Pending, Delayed, Ready, Delivered, then Expired, Cancelled or Revoked.
  Unclaimed requests expire after 5 minutes. A policy change or emergency stop revokes
  everything.
- **Transport:** TLS 1.3, listening only on a private overlay network. Approval page over
  HTTPS.
- **Discovery:** none, a static address.
- **Multi-host:** clients anywhere on the overlay.
- **Answers W4 most directly:** the private key never travels. The remote host makes a key
  pair and sends a CSR or public key, and the issuer returns a short-lived certificate after
  owner approval (passkey or similar). That replaces ssh + sudo + piping a secret. The
  "fetch once" and expiry rules close the window in which an issued credential lies around.
  The policy files are proposals until the owner approves them; the same model fits a team's
  member list. The audit log is hash-chained.
- **Verdict: crib ideas only.**
- **Licence:** **none.** There is no LICENSE file and `Cargo.toml` says
  `LicenseRef-Proprietary`. **Do not copy code.** **Maturity:** 1 star, last push this month.
  A one-shop tool; docs mostly not in English.

## tonyweckstrom1987/holvi

- **Solves:** about 640 lines of Python, an MCP server that does narrow read-only GitHub and
  Gmail calls on an agent's behalf, so the agent never sees the tokens.
- **Identity/credentials:** clients are identified by **source IP**. The README itself says
  this is weak; nftables per-client ports are the real boundary. There is no TLS.
- **Transport:** MCP streamable HTTP.
- **Discovery:** static.
- **Multi-host:** LAN or VPN only.
- **Other features:** an audit log with no secrets or fetched data, alerts on blocked
  requests, and a separate process for the riskiest tool.
- **Answers:** none of the five. Its IP-as-identity model is what our per-seat token already
  improves on. Our CIDR admit is a network filter, not identity, which agrees with holvi's own
  warning.
- **Verdict: not relevant.** One small idea: an alert to the owner on every refused
  authentication.
- **Licence:** MIT, copy OK. **Maturity:** 1 star, v0.1.0, last push a month ago.

## maximhq/bifrost

- **Solves:** a high-performance Go LLM / MCP / agent gateway. It puts one OpenAI-compatible
  API in front of many model providers, with failover, load balancing, semantic cache, budgets
  and "virtual keys".
- **Identity/credentials:**
  - virtual keys mapped to real provider keys, with budgets per key, team or customer;
  - OIDC user provisioning;
  - clustering, the MCP gateway and guardrails are listed under enterprise deployments.
- **Transport:** HTTP.
- **Discovery:** config, UI or API.
- **Multi-host:** clustering is an enterprise feature.
- **Answers:** none of the five. It sits between agents and model providers, not between
  agents.
- **Verdict: not relevant.** "Virtual keys" (a revocable per-client alias for a shared secret)
  is the only transferable pattern, and our per-seat tokens already are that.
- **Licence:** Apache-2.0 (the enterprise features are not in this repository). **Maturity:**
  about 9.8k stars, pushed today.

## Best ideas to crib

1. **Key pair made on the member's host, certificate issued centrally (Key-Box, SPIFFE in
   Substrate).** A remote host generates the seat key and sends a CSR. The bus host signs a
   short-lived certificate after owner approval. Only the request and the public certificate
   cross machines, so nothing secret is piped over ssh (W4, and W2 when placing a seat).
2. **Approval as a state machine, not a shell session (Key-Box).**
   - States: request, then pending owner approval, then ready, then fetched once, then expired
     or revoked.
   - Delivery is pull-based over the team's own TLS endpoint, so the owner approves from
     anywhere and no sudo session is needed on the bus host.
3. **A signed member card (A2A JWS + JCS).** The team root signs a canonical JSON document
   naming the seat, kind, team url and CA fingerprint. Any host's placer verifies it offline.
   This lets placement run on the container's host (Option A) without trusting the delivery
   channel.
4. **The identity names the member, not the host (Substrate).** The seat key holds no host
   name, so a seat can move between machines with a reissue of its certificate and no new
   identity (W3).
5. **Suspend as a member state, with traffic parked (Substrate request parking, wireproxy
   `/readyz`).**
   - Liveness is a last-pong age with a stated tolerance.
   - A member past it is marked suspended, not failed. Matrix already queues for it.
   - Senders are told the member is "asleep", not "gone" (W5).
6. **An optional overlay network for members (wireproxy, ISC, Go).** Userspace WireGuard in
   the member client gives every seat a stable address with no root and no TUN. The team then
   admits one overlay CIDR instead of each host's network (W4's CIDR half). This is worth a
   spike only if the per-host CIDR admit keeps hurting.
7. **Short-lived credentials with rotation documented up front (Substrate, Key-Box).** Expiry
   bounds the damage a leaked seat can do and makes a "return seat" step mostly unnecessary.

None of these projects answers **W1** (one homeserver per team). They all assume one hub or
point-to-point links. Matrix federation remains the only multi-server option on the table.
