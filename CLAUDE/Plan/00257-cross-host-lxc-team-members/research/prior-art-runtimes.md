# Prior art: agent runtimes and multi-machine agent tooling

Survey of ten public projects against teamchat's known weaknesses: **W1** one homeserver per
team; **W2** LXC members only on the bus host; **W3** seats tied to the host they run on;
**W4** a seat on another machine needs ssh + sudo + hand-piped credentials + network CIDR +
TLS CA; **W5** host suspend. Sources: each repo's README and, where named, its docs tree.
Stars and last push read from GitHub at the time of writing.

None of the ten is a peer-to-peer agent bus. The closest is Orca (paired servers carrying
agent mail); the rest are session managers or account tools. Nothing here replaces Matrix;
the value is in enrolment, identity and reconnect patterns.

## KarpelesLab/teamclaude

- **Solves:** local proxy pooling several Claude/Codex subscriptions for one person's agent
  fleet, rotating on quota. Node, no dependencies.
- **Messaging / multi-machine:** no agent messaging. Cross-machine *credential* sync through
  an external store: each install signs in by a poll-token (device-code) OAuth flow that
  works over SSH or in a container with no listening port; one row per account; refresh goes
  through a per-row advisory lock so one install renews and the rest adopt; removal travels
  as a tombstone row.
- **Offline:** unreachable store stops renewals rather than racing; can hold a request open
  until quota resets.
- **Answers:** W4 (enrolment without piping secrets), and the refresh-token-rotation race any
  multi-seat Claude login has.
- **Verdict:** crib ideas. **Licence:** MIT. **Maturity:** pushed 2026-11-17, 512 stars.

## standardagents/dmux

- **Solves:** tmux pane per task, each in its own git worktree/branch; merge or PR from a
  menu; lifecycle hooks on worktree create/merge.
- **Messaging / multi-machine:** none. Local tmux only.
- **Offline:** resumes the agent conversation when a pane is recreated.
- **Answers:** none of W1-W5.
- **Verdict:** not relevant. **Licence:** MIT. **Maturity:** pushed 2026-08-16, 1.8k stars.

## herdrdev/herdr

- **Solves:** a terminal multiplexer that owns agent terminals in a background server (Rust,
  single binary); marks each pane working/blocked/idle.
- **Messaging:** agents drive it over a CLI and socket API: spawn panes, prompt each other,
  wait until another agent is genuinely blocked. Scoped to one server; IDs and agent names
  are per server.
- **Multi-machine:** "saved machines": `herdr machine add <ssh-target>` installs or updates
  the remote binary after confirmation, starts the remote server, saves a profile holding
  only id, label, SSH target, session name, enabled flag. Identity and auth are plain
  OpenSSH (shared ControlMaster, `ControlPersist 600`); no secrets stored. Client and server
  negotiate capabilities, versions need not match.
- **Offline / suspend:** each machine reconnects independently with backoff to two minutes;
  a connection must stay healthy a minute before backoff resets; a lost machine shows cached
  state dimmed and input-disabled; background reconnects never prompt or install, they flag
  "Attention" (`! auth`, `! error`) for a human. Idle bridge closes after a minute, and sleep
  counts toward that deadline, checked on wake.
- **Answers:** W5 (reconnect state machine), W3/W4 partly (SSH as the only trust root).
- **Verdict:** crib ideas. **Licence:** Apache-2.0. **Maturity:** pushed 2026-11-07, 51.8k stars.

## generalaction/emdash

- **Solves:** desktop app running agents in parallel, one worktree each; issue-tracker
  intake; diff review and merge.
- **Multi-machine:** remote projects over SSH/SFTP (agent, key, password, OpenSSH
  certificates in the agent; credentials in the OS keychain). Installs marker-tagged hooks in
  the agent's user config that do nothing outside an Emdash session.
- **Messaging / offline:** none described.
- **Answers:** marginal for W4 (OpenSSH certificates as the remote credential).
- **Verdict:** not relevant (hook-marker trick aside). **Licence:** Apache-2.0.
  **Maturity:** pushed 2026-11-19, 6.3k stars.

## Maxteabag/hotseat

- **Solves:** Go TUI/CLI showing quota of each Claude/Codex account, which session uses
  which, and which conversations a usage limit stopped, with resume.
- **Messaging / multi-machine:** none (local credential files only); `nudge` writes into a
  running Codex session.
- **Offline:** "Work" list of sessions stopped at a limit; resume only after a quota check.
- **Answers:** none of W1-W5; adjacent to usage-pause recovery.
- **Verdict:** not relevant to the bus. **Licence:** MIT. **Maturity:** pushed 2026-11-18, 8 stars.

## stablyai/orca

- **Solves:** desktop/mobile app running a fleet of agents in worktrees, with an
  orchestration layer (coordinator dispatches workers, typed mail, gates).
- **Messaging:** a per-runtime mailbox with typed messages (`status`, `handoff`, `question`,
  `dispatch`, `merge_ready`, plus Dispatch-scoped `escalation`, `heartbeat`, `worker_done`)
  and group addresses (`@all`, `@idle`). Agents end their turn and Orca wakes them on mail.
- **Multi-machine:** "Remote Orca Server". Server owner runs `orca pair --address <addr>`
  (or a UI button) to mint a one-time access link; the client pastes it and receives its own
  **revocable per-client grant**; revoke disconnects at once. Only the server's own user can
  mint pairings. Transport: their runtime protocol over a private path (Tailscale/LAN/SSH
  tunnel); refuses public source addresses unless opted in. `send --on <server>` carries
  mail to an agent on a paired server; the sender's own runtime keeps the conversation and
  replies arrive in the local mailbox, sender shown as `<agent> on <server>`. Too-old peers
  answer `server_mail_unsupported`. Cloned images: `ORCA_INSTANCE_ID` forces a fresh
  identity per copy.
- **Offline / suspend:** agents live on the server, so a client laptop sleeping is harmless;
  the server itself must stay awake. Per-terminal systemd scopes with memory caps.
- **Answers:** W1 (federation by pairing runtimes, mailbox stays home), W3, W4 (pairing link
  replaces credential piping), partly W2.
- **Verdict:** crib ideas (strongest match). **Licence:** MIT. **Maturity:** pushed
  2026-11-19, 129k stars; remote servers marked beta.

## Ark0N/Codeman

- **Solves:** self-hosted web "mission control" for agents in tmux, mobile UI, respawn and
  auto-resume on usage-limit reset, Claude Code native Agent Teams visualised.
- **Messaging:** no bus of its own; watches `~/.claude/teams/`; HTTP API for prompting a
  session (exactly-once with client id + sequence, "send and block until turn done").
- **Multi-machine:** remote SSH sessions in a dedicated remote tmux; bounded-backoff
  auto-reconnect; discover and attach sessions already on a host. Remote reach via Tailscale
  serve or Cloudflare tunnel; QR login with short-lived single-use tokens.
- **Offline / suspend:** tmux survives drops and client sleep; ghost-session discovery on
  start; auto-resume after limit reset.
- **Answers:** W5 partly; exactly-once delivery idea for the member client.
- **Verdict:** crib ideas (small). **Licence:** MIT. **Maturity:** pushed 2026-11-15, 843 stars.

## agent-of-empires/agent-of-empires

- **Solves:** Rust TUI + web dashboard managing agents in tmux sessions, optional worktree
  and Docker/Podman sandboxing, HTTP API.
- **Messaging / multi-machine:** none beyond "run it over SSH"; sessions persist in tmux.
- **Offline:** tmux persistence only.
- **Answers:** none of W1-W5 (podman sandbox is already what ccy does).
- **Verdict:** not relevant. **Licence:** MIT. **Maturity:** pushed 2026-11-19, 4.7k stars.

## Maxteabag/clarp

- **Solves:** self-hosted server + PWA/iOS app to talk to Claude Code by voice from a phone.
- **Messaging:** `/send` with `force_session` injects a prompt into another agent's tmux;
  scheduled prompts via systemd timers with `Persistent=true`.
- **Multi-machine / identity:** one-time pairing code (10 min, single use) exchanged for a
  per-device bearer token with a scope (`limited`/`full`); `pair revoke DEVICE_ID`; headless
  JSON pairing for scripts. Transport: Tailscale, a managed relay, or direct LAN with
  automatic fallback.
- **Offline:** runtime service separate from the HTTP service, so redeploys do not kill turns.
- **Answers:** W4 (scoped pairing for headless clients).
- **Verdict:** crib ideas only; do not adopt or copy code. **Licence:** PolyForm-Shield-1.0.0
  (source-available, non-OSI). **Maturity:** pushed 2026-11-19, 3 stars.

## shell-pool/shpool

- **Solves:** persistent named shell sessions without tmux's window management, so native
  scrollback works; systemd user socket activation.
- **Messaging / multi-machine:** none; used on the far side of an SSH login.
- **Offline:** sessions survive dropped connections; `--ttl` per session.
- **Answers:** none of W1-W5 directly.
- **Verdict:** not relevant (possible tmux alternative for seat terminals, nothing more).
  **Licence:** Apache-2.0. **Maturity:** pushed 2026-11-18, 2.0k stars.

## Best ideas to crib

1. **Pairing link instead of credential piping (Orca, Clarp)** - W4. The root admin tool
   mints a one-time, short-lived join token bound to a seat name; the new machine's member
   client redeems it over TLS and receives its own long-lived device credential. The admin
   never pipes a password; each machine's grant is listed and revocable on its own. Only the
   bus host's own user can mint.
2. **Device-code enrolment (teamclaude)** - W4 for containers and LXC: a poll-token flow needs
   no listening port on the joiner and works over SSH or inside a container.
3. **Mail stays home, peers are addressed by `name on server` (Orca)** - W1. Instead of one
   homeserver shared by all machines, each host keeps its own runtime and mailbox and pairs
   with others; cross-host messages carry a server qualifier and a capability check
   (`unsupported` reply from an old peer). Matrix federation is the native form of this;
   Orca shows the UX of qualified names and that replies need no flags.
4. **Reconnect state machine (herdr)** - W5. Per-peer independent reconnect with capped
   backoff; reset only after a minute healthy; cached state shown stale, never as live;
   background reconnect never prompts or installs, it raises a typed attention flag
   (`auth`, `error`) for a human; sleep counts toward idle deadlines, evaluated on wake.
5. **SSH as the only remote trust root, profile stores no secrets (herdr)** - W2/W3. A saved
   remote member is id + label + SSH target + enabled flag; install/upgrade of the remote
   side runs only interactively after confirmation; capability negotiation, not version
   equality.
6. **Instance identity for cloned images (Orca `ORCA_INSTANCE_ID`)** - W3. A cloned LXC or
   VM image must not reuse a seat's identity; record an instance id with the credential and
   force re-pairing when it changes.
7. **Exactly-once prompt delivery (Codeman)** - client id + sequence on each injected
   message, so a reconnect after suspend neither drops nor double-delivers.
8. **Refresh-token advisory lock (teamclaude)** - if seats on several machines ever share one
   Claude login, a single renewer with a lock avoids the rotation race that logs others out.
