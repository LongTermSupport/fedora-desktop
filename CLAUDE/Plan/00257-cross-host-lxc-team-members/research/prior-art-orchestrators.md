# Prior art: other agent-team orchestrators

What six public projects do about the problems teamchat has: one homeserver per team, LXC members
only on the bus host, seats tied to the host they run on, joining a seat from another machine
(ssh, sudo, credentials piped by hand, a network CIDR, a TLS CA), and host suspend. Sources: each
project's README and the design docs named below, read at their default branch in Nov 2026.

| Repo                               | Licence (SPDX)                        | Stars | Last push | Verdict                     |
| ---------------------------------- | ------------------------------------- | ----- | --------- | --------------------------- |
| multica-ai/multica                 | NOASSERTION (Apache-2.0 + conditions) | ~57k  | Nov 2026  | Crib ideas                  |
| mvschwarz/openrig                  | Apache-2.0                            | ~8.9k | Nov 2026  | Crib ideas (best match)     |
| awslabs/cli-agent-orchestrator     | Apache-2.0                            | ~1.6k | Nov 2026  | Crib ideas (delivery)       |
| OrchestratorInc/agent-orchestrator | Apache-2.0                            | ~15k  | Nov 2026  | Crib one idea               |
| Maurdekye/orgtree (+ mailhub)      | MIT                                   | ~230  | Nov 2026  | Crib ideas (closest design) |
| rogeriochaves/open-managed-agents  | AGPL-3.0-or-later                     | ~120  | Jun 2026  | Not relevant                |

## multica-ai/multica

**Solves:** an issue board (Go backend, PostgreSQL, web/desktop/mobile) where agents are assignees.
A per-machine daemon picks up issues, runs a local agent CLI (Claude Code and about 30 others) in a
git worktree, and posts progress back as comments; humans review.

- **Transport:** star topology. Each machine's daemon holds one outbound WebSocket to the server.
  The server pushes a *wake signal* when work waits; the daemon then *claims* in one batch. A 30 s
  poll is the catch-up path if a wake is missed or the socket is down, so a missed push only delays.
  Agents talk to each other through issues, comments and mentions, not a chat bus.
- **Other machines:** "Add runtime" in the UI shows two commands to paste on the new machine
  (install, then `multica login --token <pat>` and `daemon start`). No inbound port, no ssh, no sudo.
- **Identity:** a personal access token per user (OAuth or `--token`, 90-day); the daemon
  registers a *runtime* per detected agent CLI per workspace; daemon id and device name default to
  the hostname. Roles owner/admin/member, with per-member scopes for which agents they can run.
- **Offline/suspend:** 15 s heartbeats; a daemon that stops beating is offline, and its runs stay
  *queued unclaimed* until it returns. Failed runs retry. `daemon autostart enable` installs a
  systemd user unit so a reboot or login brings the runtime back.
- **Answers:** joining another machine (pull-based, outbound only, token login); host suspend (work
  is not pushed to a seat, it waits to be claimed).
- **Licence:** GitHub reports "Other". The Multica License is Apache-2.0 text plus conditions: no
  hosted service or commercial embedding without a commercial licence, branding must stay, and
  `server/ee/` is under a separate commercial licence. Do not copy code into this repo; ideas only.

## mvschwarz/openrig

**Solves:** a local daemon (TypeScript, Hono, SQLite) that boots a team of Claude Code/Codex seats
in tmux from a YAML *RigSpec*, with snapshot and restore after a reboot, a work queue, and a TUI.

- **Transport:** CLI and MCP tools against the local daemon (`rig send`, `rig broadcast`,
  `rig chatroom`); typed-in delivery to tmux panes, with a per-seat *typing guard* that holds
  automatic messages while a human types. Work is a *queue row* (claim, update, hand off, blocked on
  a person or another row), separate from chat.
- **Seat addressing:** `role@rig`, a stable address the conversation behind it can change under.
- **Other machines:** two experimental routes. (1) *Hosted members*: `rig mcp serve --http` behind
  the operator's own TLS proxy, one bearer token per member (`rig mcp token add <name> --seat x@rig`), scoped to one seat and one rig, revocable. (2) *Network connector*: a daemon on another
  machine runs `rig network enroll <one-time code>`, trades it for an installation key kept in
  `network-secrets/` and never printed, then `rig network connect <seat> --as <name>`. Every
  connection is outward; the hub never dials the member. `--replace` re-enrols after a lost key.
- **Identity:** each daemon has a `hostSelfId`; rosters name seat plus host id. A bearer per
  participant; the installation key is per daemon, not per seat.
- **Offline/suspend:** queue rows outlive the conversation; nothing promises to wake a member, so
  it re-lists its owned rows. Optional signed webhook "ring" (Standard Webhooks HMAC) to wake one.
- **Answers:** the cleanest model for "add a seat on another machine": one-time code, outward-only,
  per-seat scoped revocable bearer. Also seat identity separate from the host it runs on.

## awslabs/cli-agent-orchestrator (CAO)

**Solves:** a local `cao-server` (Python, FastAPI, SQLite) that starts agent CLIs in tmux and gives
a supervisor MCP tools (`handoff`, `assign`, `send_message`) to drive workers.

- **Transport:** messages persist in a SQLite inbox and are pasted into the receiver's tmux pane.
  The *Inbox Delivery* doc is the valuable part: deliver only when the provider TUI is IDLE or
  COMPLETED; after paste and Enter, wait for evidence the receiver started *this* delivery, and if
  not, recover (bare Enter if the composer still holds the text, re-send only if it can be read as
  empty). Never deliver into an open choice dialog, where the paste would answer a human's question.
  Two paths: immediate on send, plus a 5 s watchdog and a reconcile sweep.
- **Other machines:** none locally (server is unauthenticated, loopback). The EKS example adds a
  *broker* pod as a security boundary: workers hold a per-lease release token, and the broker
  forwards only inbox delivery and memory calls; the supervisor's port is unreachable to workers.
- **Answers:** none of the cross-host weaknesses; it answers *delivery correctness* into a
  Claude Code TUI, which teamchat's wake path also faces.

## OrchestratorInc/agent-orchestrator (Orchestrator.inc)

**Solves:** a desktop app plus local daemon (Go, chi, SQLite) that runs one agent per task in its
own worktree and follows the PR through CI and review; mobile app and hosted cloud workers.

- **Transport:** loopback HTTP and WebSocket to the local daemon; no agent-to-agent bus beyond the
  orchestrator agent driving workers. Messages arriving while a session has no controller go to a
  *durable outbox* with a stable idempotency key, and are delivered once a controller owns it.
- **Other machines:** only the phone. An opt-in second LAN listener with a rotating bearer, a QR
  pairing code, and an optional managed cloudflared tunnel for HTTPS. Workers on other hosts are
  their paid cloud, not self-hosted.
- **Answers:** little. The durable outbox through a handover gap is the one idea for suspend.

## Maurdekye/orgtree and orgtree-mailhub

**Solves:** a desktop app (Rust engine, PostgreSQL) for a tree of managers and specialist agents
with persistent mailboxes; the *mail hub* links organisations, people and plain Claude Code or
Codex sessions on different machines. Same problem shape as teamchat.

- **Transport:** each client *dials out* and long-polls one multiplexed `POST /api/poll`; the hub
  keeps a queue per address. `send` is idempotent on a client-minted id; `ack` is custody transfer
  *after* the client persisted the mail (at-least-once; the client collapses duplicates). Hub clock
  orders. Presence = a parked poll or any call in the last 90 s, plus busy/dnd flags.
- **Other machines:** a one-line installer drops a stdlib-only Python MCP server (`hubtool.py`) on
  any machine, registers it with Claude Code/Codex and pre-approves only its tools. The session then
  calls `hub_register` with a name of its own. No port forwarding, no admin rights. A Claude Code
  session can stream mail with `hubtool.py listen <name>` under its Monitor tool.
- **Identity:** each address self-issues a secret at creation; the hub stores only its sha256
  fingerprint, first registration wins the slug, and the secret travels in one auth header. Device
  enrolment by one-time code (`/api/link/put`, `/take`). Joining is open: *reachability is the
  authorisation*, so the hub must sit on a closed network; it sees everything in plaintext.
- **TLS:** not built in; a separate relay-only port is the only one ever exposed, behind a proxy
  with a real certificate. Explicit rule: never ship self-signed certificates to clients.
- **Offline/suspend:** mail queues on the hub until acked, so a suspended member just catches up on
  its next poll; on engine restart, interrupted agents get a continuation message.
- **Answers:** seat portability (an address is a secret, not a host); joining from another machine
  (no ssh or sudo); suspend (pull plus ack-after-persist). Its open-join trust model is weaker than
  teamchat's issued seats and should not be copied.

## rogeriochaves/open-managed-agents

**Solves:** a self-hosted clone of Claude Managed Agents: a server-side agent loop over any LLM API,
with org/team RBAC, budgets, MCP policies and encrypted credential vaults.

- **Transport:** REST plus SSE per session; agents are API-loop agents, not CLI sessions; no
  agent-to-agent bus and no notion of members on other machines.
- **Answers:** none. **AGPL-3.0-or-later:** copying code into this public repo would put the copied
  part under AGPL and its network-use clause; read it for ideas at most. Not relevant.

## Best ideas to crib

1. **Outward-only members, pull with a wake** (multica, orgtree, openrig connector). A member
   dials the server; the server only *signals* that work waits; the member claims. A periodic poll
   is the fallback, so a lost push or a suspended host costs latency, never a message. Matrix
   `/sync` long-polling already has this shape: keep LXC and remote seats strictly as clients that
   reach out, and drop anything that needs the bus host to reach into a member.
2. **One-time join code instead of ssh, sudo and credential piping** (openrig `network enroll`,
   orgtree device link, multica "paste two commands"). The root admin tool prints a short-lived,
   single-use code bound to one seat; the new machine trades it over HTTPS for its own credential
   and stores it locally with mode 0600, never echoed. `--replace` re-enrols after a lost key.
3. **A seat is an address plus a secret, not a host** (openrig `role@rig`, orgtree slug and
   fingerprint). Keep host only as an attribute (`hostSelfId`) so a seat can move machines by
   re-enrolling, and the roster shows where each seat lives now.
4. **Per-seat scoped, revocable bearer** (openrig `mcp token add --seat`): a token reaches one
   seat's rooms and work only; revoke on disconnect; the server never stores the secret, only a hash.
5. **Ack after persist, idempotent send** (orgtree mailhub): a client-minted message id makes
   retries safe across suspend and reconnect; delivery is at-least-once and the member collapses
   duplicates. Pairs with Matrix transaction ids.
6. **Presence from liveness, plus busy and dnd** (orgtree 90 s rule, multica 15 s heartbeat):
   a suspended host shows offline by itself, and work waits for it instead of being pushed.
7. **Delivery into a Claude Code TUI** (CAO inbox doc): deliver only when idle, confirm the
   receiver began this delivery, recover by reading the composer, never paste into an open choice
   dialog. Worth checking against teamchat's own wake/inject path.
8. **TLS without a private CA on members** (orgtree): expose one relay-only port behind a proxy
   with a publicly trusted certificate (or a tailnet name); never make members trust a self-signed
   CA. Would remove the "install the team CA" step for a seat on another machine.
9. **Autostart the member as a systemd user unit** (multica `daemon autostart enable`), so a
   reboot or login brings the seat back without a human.

Not worth cribbing: orgtree's open join (reachability as authorisation), multica's single central
server for everything (teamchat keeps one homeserver per team by design), any code from multica
(licence conditions) or open-managed-agents (AGPL).
