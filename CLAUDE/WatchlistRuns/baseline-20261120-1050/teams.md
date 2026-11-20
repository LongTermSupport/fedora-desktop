# Study: agent-team and agent-protocol repositories

Watch-list run `baseline-20261120-1050` (Plan 00258, Task 2.1), group "teams". Seven repositories, each compared against what this
repository builds: ccy-teams, the agent team bus (v1 `pingbus`, v2 `teamchat`), the team roles
(Plans 00196, 00207, 00225), the seat broker (00197) and seat status (00251).

Method: every claim below cites a path inside the clone, relative to its root under
`untracked/starred-repos/<licence>/<org>/<repo>/`. Nothing was built, installed or run. Commit
counts and dates come from `git log` on the clones as they stood on 2026-11-20; history may still
have been downloading, so the counts are lower bounds. Guesses are marked **(guess)**.

Reuse classes follow [Watchlist.md](../../Watchlist.md): MIT and Apache-2.0 are "lift" (keep the
notice); AGPL is "clean room" (ideas in our own words, no code, no transcribed comments or prompts).

What ours does, in one paragraph, for the comparisons: a team is a private Matrix homeserver with
one unencrypted room that the team's humans read in Element, on a desktop or a phone. Agents send
**pings**, a closed set of verbs (`fetch`, `sync`, `review`, `run-qa`, `halt`, `ack`, `nack`,
`done`, `blocked`) that act only on forge-verified artefacts at commits on trusted branches, and
free **text** that a receiving agent takes as a colleague's note, never an instruction
(`docs/agent-bus-protocol.md` §4-§7). A worker's message to a human copies the coordinator (00196).
Messages reach a ccy session through Claude Code's inbox socket, delivered by a supervised watcher
(`docs/agent-bus.md`, "How messages reach the session"). Each seat runs in a rootless Podman
container under an nftables network policy (`docs/ccy.md`, "Network policy"). Planned or in design:
minimum models per role and communication gates (00207), a liaison role for the human (00225), a
seat broker with owner approval (00197), and seat states for the coordinator (00251).

---

## mvschwarz/openrig

Studied: baseline at 07d8f1c6574ac31b3eeae802742f8bfda67f3295

Apache-2.0; the nearest neighbour.

### What it is, and who it is for

A local control plane that runs a team ("rig") of unmodified coding-agent CLIs (Claude Code, Codex,
Pi, Oh My Pi, a plain terminal, a scripted stub) in tmux, coordinated by a daemon with SQLite state.
People and agents drive it through the `rig` CLI, a TUI and an MCP server, all over local HTTP
(`ARCHITECTURE.md`, "What OpenRig is", "The request path"). It is aimed at one developer running a
persistent team on their own machine (`README.md`). Installed with npm; Node 22 or 24 and tmux
are required.

### Architecture

- **Team and roles.** A RigSpec YAML declares pods of members; each member names an agent spec, a
  runtime, a working folder, a model and a profile (`docs/reference/rig-spec.md`, "Minimal Valid
  Example", "Member"). Roles are not a fixed enum: they are agent specs plus **edges** between
  seats, `delegates_to`, `spawned_by`, `can_observe`, `collaborates_with` and `escalates_to`
  (`rig-spec.md`, "Edge Kinds"). `escalates_to` is the fallback route for queue escalation when a
  seat has no single delegation parent. A seat **pool** clones a member when its work waits and
  drains idle clones, but the block is "stored and validated; it has no runtime effect until the
  seat pool job is armed" (`rig-spec.md`, "Seat pool"). Shipped pod templates include orchestration,
  development, review and oversight teams (`skills/_canonical/pods/`).
- **Messaging and delivery.** `rig send` types a message into the seat's tmux pane inside a
  `From:`/`To:` envelope (`docs/reference/seat-delivery.md`, opening sections). A per-seat **typing
  guard** has seven modes, from `off` through `draft-aware` (paste only into a recognised empty
  composer, re-check before Enter), `openrig-urgent`, `urgent-only`, `sends-only`, `queue-only`, to
  `hold` (`seat-delivery.md`, the mode table). Held messages are retained, never replayed, one per
  sender per seat: a sender's newest held message coalesces its earlier ones, so one sender cannot
  fill the quota (`seat-delivery.md`, "Terminal delivery"). An opt-in **Claude hook bridge**
  replaces typing with Claude Code's `asyncRewake` hooks: a waiter armed at SessionStart and Stop
  polls the daemon, writes queued messages to its output and exits 2 to wake Claude at a turn
  boundary (`seat-delivery.md`, "Claude hook bridge"; `packages/daemon/assets/claude-hook-bridge.cjs`).
  The bridge prefixes each batch with one line saying the delivery adds no authority to the
  messages, which is the same stance as our PEER rule.
- **Task ownership and hand-off.** A durable SQLite **queue** separate from chat: `stream_items`
  (append-only intake), `queue_items` (owned work with eight states including `handed-off`),
  `queue_transitions` (append-only audit), `inbox_entries`, `outbox_entries`
  (`docs/as-built/architecture/coordination-primitive.md` §1). The **hot-potato closure contract**
  refuses `done` without a closure reason from a closed set (`handed_off_to`, `denied`, `canceled`,
  `no-follow-on`, `escalation`, `superseded`), and hand-off closes the source and creates the
  successor's row in one transaction (§2, §3; `packages/daemon/src/domain/hot-potato-enforcer.ts`).
  The doc is explicit about receipt honesty: a wake is not proof of a claim, a claim is not proof
  the body was read, and each has its own field (§2, "Which field answers 'did it reach them'").
  A stuck sweep turns unclaimed or undelivered work into findings routed to the orchestrator,
  and closes its own finding only on source evidence (§2).
- **Persistence.** SQLite in WAL mode under `~/.openrig`, about 96 migrations
  (`ARCHITECTURE.md`, "Key counts"); queue transitions are archived, never updated in place.
- **Humans.** Humans are participants with addresses (`<id>@external`), organisations and groups,
  bound to Slack or a local relay (`docs/reference/gateway-participants.md`). Decisions go to a
  durable queue, not chat: the `human-in-the-loop` skill classifies every closeout as
  auto-continue, human gate, or park, and "Mission Control" gives the human seven verbs (approve,
  deny, route, annotate, hold, drop, hand off) (`skills/_canonical/core/human-in-the-loop/SKILL.md`).
  Group policy text is read by agents and "OpenRig doesn't enforce it"
  (`gateway-participants.md`, opening paragraph).
- **Launch and supervision.** Runtime adapters launch each harness, project skills and files into
  it and check readiness (`ARCHITECTURE.md`, "A runtime adapter"). Seat **activity** comes from one
  arbiter over an evidence ladder: Claude's own session file, then lifecycle hooks, then visible
  needs-input chrome, then pane sampling as a fallback (`docs/reference/agent-state-taxonomy.md`,
  "The evidence ladder"). Three orthogonal axes (session, activity, resumability); `unknown` is a
  first-class value; needs-input is a count plus a reason, never a state; PARKED is derived at read
  time as idle-or-blocked times open obligations (same file, "Derived diagnoses"). Rejected
  oracles are kept with dated reasons (transcript quiescence, pane scraping as primary truth).
  Per-seat Linux memory limits and a "checked stop" (compare-and-swap on node and session id,
  `docs/reference/checked-seat-stop.md`) guard lifecycle actions.
- **Models and providers.** Per-member `runtime` and `model`, account and credential switching that
  keeps the conversation (`rig-spec.md`, "Switching credentials while keeping the conversation").
  Usage limits are read from Claude's typed `StopFailure`/`rate_limit` hook, never from message text,
  and hold that account's pool until the stated reset (`docs/reference/claude-status-line.md`,
  lines 92-137). No minimum-model rule per role was found.
- **Cross-machine.** A "network connector" puts one local seat on a remote hub over outbound MCP;
  marked "experimental and unsupported" (`docs/reference/network-connector.md`). "Hosted members"
  let a cloud assistant act as a seat through an MCP grant (`docs/reference/hosted-members.md`).

### Maturity and activity

5,616 commits from 2026-03-23 to 2026-11-20; 73 tags; release 0.7.19 (`ROADMAP.md`). Monthly
commits peak at 1,586 (October) and 897 so far in November. Top authors: one maintainer under three
identities plus a build bot, with a handful of outside contributors. Very active, fast-moving, with
heavy reference docs and an as-built tree that records the commit each page was verified against.

### Licence and reuse

Apache-2.0 (`LICENSE`), no `NOTICE` file, one SPDX header found (Apache-2.0); `skills/LICENSE` is
also Apache-2.0. Class: **lift**. The code is TypeScript, so little of it drops into our Go; the
reusable part is the documented design (adapt with attribution).

### Comparison with ours

| Area                 | openrig                                                                                                      | ours                                                                                 | Verdict                         |
| -------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------- |
| Human visibility     | Humans get a decision queue and Slack/relay; agent traffic lives in the daemon                               | Every message in one Matrix room humans read in Element, phone included              | **Ours ahead**                  |
| Trust of agent input | Delivery "adds no authority"; no artefact verification                                                       | Closed verbs, forge-verified refs at trusted commits, PEER text never an instruction | **Ours ahead**                  |
| Isolation            | Seats run as the user in tmux, broad default permissions (`README.md`, "What OpenRig changes")               | Rootless Podman per seat, nftables policy, no host home                              | **Ours ahead**                  |
| Cross-machine        | Experimental outbound MCP hub                                                                                | Team CA, WireGuard, per-seat Matrix accounts (`docs/agent-bus-cross-network.md`)     | **Ours ahead**                  |
| Role contracts       | Free-form agent specs plus edges; no model floor                                                             | Coordinator, worker, liaison, deployer; minimum models; communication gates (00207)  | Ours ahead on paper (planned)   |
| Durable owned work   | Queue with states, transactional hand-off, closure reasons, stuck sweep                                      | `done`/`blocked`/`ack` pings, no work object, no owner field                         | **openrig ahead**               |
| Seat state           | Three axes, evidence ladder, PARKED diagnosis, `unknown` honest                                              | Board shows liveness only; 00251 in design                                           | **openrig ahead**               |
| Delivery receipts    | wake / claim / read kept apart; held messages coalesced per sender                                           | Inbox socket write; hooks report failure to deliver                                  | openrig ahead                   |
| Runtimes             | Claude Code, Codex, Pi, OMP, terminal, stub                                                                  | Claude Code (ccy), opencode (`docs/ccy.md`, "opencode")                              | openrig ahead (not a goal here) |
| Testing              | Scenario library on a stub runtime, seeded-regression pairs, no model cost (`ARCHITECTURE.md`, "A scenario") | Unit tests through fakes; teamchat wiring e2e test                                   | openrig ahead                   |

### Ideas worth adopting

1. **A seat-state model with orthogonal axes, an evidence ladder and an honest `unknown`.** Lands
   in Plan 00251, which is waiting on a design choice. Directly answers its idle-lock: PARKED =
   idle (or blocked on input) while holding an open obligation, derived at read time. Also: a
   rate-limited state taken only from Claude's typed `StopFailure` reason, never from text; needs-input
   as a count plus reason; a successor never inheriting its predecessor's evidence. **Plan input**
   (feed into 00251's design decision, not a new plan).
2. **Owned work with a closure contract.** A `done` that must say what happens next (handed to whom,
   no follow-on, superseded, denied), so a baton cannot be dropped silently. In teamchat this could
   be a required closure field on `done` when it answers a request, and a "who holds what" view
   for the coordinator. **Needs discussion (plan)**: it changes the verb table, and teamchat v2 is
   the place for it.
3. **Receipt honesty in the vocabulary.** Separate "written to the inbox socket", "acknowledged by
   the agent" (`ack`) and "done"; never let a status line or board render "delivered" as "read".
   **Clear win (issue)** against teamchat's delivery reporting and the status line.
4. **Hold modes for a seat a human is typing in.** openrig's `queue-only`/`urgent-only` and
   orgtree's DND (below) both exist because automated messages interrupt a person. For ours the
   socket does not type over a draft (**guess**: the inbox socket queues rather than types; not
   verified here), so the value is an "urgent only" focus mode for a seat. **Needs discussion.**
5. **Checked stop.** Stop or remove a seat only if its node and session id still match what the
   caller saw, and report how far a failed stop got. Lands in `ccy-teams` seat actions and the
   seat broker (00197). **Clear win (issue).**
6. **Stub-runtime scenario tests with seeded regressions.** A scripted fake agent drives the real
   bus and launcher; each scenario is a pair that passes on healthy code and fails on a planted
   defect. Lands in `src-go/internal/teamchat` acceptance tests. **Needs discussion (plan)**; it
   is also a Defence Before Fix fit (the seeded run is the red proof).
7. **Docs that carry their own proof.** `ARCHITECTURE.md` prints every count beside the command
   that produced it, and as-built pages carry a `last-verified-against-source` commit. Lands in
   `docs/` and the docs QA. **Clear win (issue)** for the bus docs at least.
8. **Rejected-approach receipts.** A short, dated "we tried X, it failed because Y" list in the
   design doc so nobody re-derives it. Lands in plan DESIGN files (00251 especially). **Clear win**,
   but a convention, so a line in `CLAUDE/PlanWorkflow.md` rather than an issue.

### Code worth lifting

None directly: TypeScript. Text that may be adapted with attribution (Apache-2.0, keep notice):
`docs/reference/agent-state-taxonomy.md` (the axis and ladder vocabulary), and the hook-bridge
design in `docs/reference/seat-delivery.md` "Claude hook bridge" if we ever move off the inbox
socket. `packages/daemon/assets/claude-hook-bridge.cjs` is a 146-line reference for the
`asyncRewake` waiter shape, not for copying into Go.

### Risks and things to avoid

- **Typing into panes.** Most of `seat-delivery.md` exists to work around typing into a TUI: draft
  detection, numbered-menu refusal, paste labels, the tab-to-spaces quirk. Our socket route
  avoids the class; do not adopt pane typing.
- **Declared but inert configuration.** `pool` and `continuity_policy` are validated and stored
  but do nothing (`rig-spec.md`, "Seat pool", "Continuity Policy"); gateway policy text is not
  enforced. Same defect class as item 3 of the group section.
- **Complexity.** 87 top-level commands, 69 route mounts and seven typing-guard modes
  (`ARCHITECTURE.md`, "Key counts"); much of the reference prose is very dense. Borrow the models,
  not the surface.
- **Permissions.** The kernel runs with broad permissions and Codex with `danger-full-access`
  by default (`README.md`, "What OpenRig changes on your machine"), without container isolation.

---

## a2aproject/A2A

Studied: baseline at 5f931b8a25fe66789f987ec4a55fc16ee9fe41ee

Apache-2.0; the agent-to-agent protocol.

### What it is, and who it is for

A Linux Foundation protocol specification for one agent (client) to hand tasks to another (server)
across organisations: discovery by an **Agent Card**, a **Task** with a lifecycle, **Messages**
made of **Parts**, **Artifacts** as results, and three update routes: polling, streaming and
webhook push (`docs/specification.md` §2.2, §3.5). Three standard bindings: JSON-RPC, gRPC,
HTTP+JSON (§9-§11), with a canonical protobuf (`specification/a2a.proto`). For vendors and
platforms that want interoperable agents; SDKs in six languages, Go among them (`docs/roadmap.md`,
"SDKs").

### Architecture (the parts that matter for a bus)

- **Roles.** Only two: client and server (remote agent); a message's `role` is `user` or `agent`
  (§2.2). No teams, no coordinator, no human participant as such.
- **Messaging.** Request/response to one server endpoint. `contextId` groups related tasks and
  messages; `taskId` is server-generated and continues a task; `referenceTaskIds` points at
  related tasks (§3.4.1-§3.4.3).
- **Task ownership.** The server owns the task. States: submitted, working, completed, failed,
  canceled, input-required, rejected, auth-required (`a2a.proto`, `enum TaskState`).
  `input-required` and `auth-required` are interrupted states the client resolves by sending
  another message on the same task.
- **Human in the loop.** Indirect. In-task authorisation (§7.6) lets an agent hand an approval or a
  credential request up the chain by moving its task to `auth-required`, and the client may in turn
  move its own task there, forming a chain. The spec says the state transition alone authorises
  nothing (§7.6.4). Structured elicitation is roadmap work (`docs/roadmap.md`, "Elicitation &
  Multi-Turn Workflows").
- **Security.** Agent Card declares schemes (API key, HTTP auth, OAuth 2.0 with device code and
  PKCE, OIDC, mTLS) (§4.5); push-notification and extended-card security in §13.
- **Custom bindings** are allowed if they implement every core operation, keep the data model,
  map errors and declare themselves in the Agent Card (§12).

### Maturity and activity

665 commits from 2025-04-08 to 2026-11-18; tags v0.1.0 to v1.0.1 and v1.1.0-rc. 1.0.0 on
2026-03-12 with a long list of breaking changes, 1.0.1 on 2026-05-26 (`CHANGELOG.md`). 41 commits
since 2026-09-01. Stable 1.x, governed (`GOVERNANCE.md`, `MAINTAINERS.md`).

### Licence and reuse

Apache-2.0 for the spec, protobuf and docs (`LICENSE`; `README.md` line 131; `mkdocs.yml`
copyright). Class: **lift**.

### Comparison with ours

The two answer different questions. A2A is **point-to-point task delegation between strangers over
HTTP**: one client, one server, the server owns the task, nothing is visible to anyone else. The bus
is **a shared, human-readable room for a small trusted team**: every message is visible to the
team's humans, addressed by mention, and the trust model rests on forge-verified artefacts and on
who the sender is in the team record.

Where A2A is ahead: an explicit task object with a state, `input-required` and `auth-required` as
first-class interrupted states, discovery of what an agent can do (Agent Card skills), multi-turn
threading by ids, and interoperability with other vendors' agents.

Where ours is ahead: humans watching everything, closed verbs with role rules, the PEER rule
(agent text is never an instruction), artefacts proven at a commit on a trusted branch, and no
listening HTTP endpoint (federation off, a private homeserver, nftables on every seat).

### Could the agent bus speak A2A?

- **As a native binding over Matrix:** a custom binding must implement every core operation (send,
  stream, get, list, cancel, subscribe, four push-config operations, extended card) and keep the
  data model (§12.1). That is most of a second protocol inside teamchat, for no team member who
  needs it. **Not worth it.**
- **As a gateway seat:** an A2A server that fronts one seat (inbound: an outside agent hands a task,
  it arrives as an `external` text and its answer leaves as an artifact) or an outbound tool (a seat
  hands a task to an outside A2A agent). Cost: a listening HTTPS endpoint with its own auth and
  token store, which the bus has deliberately never had; an exemption in the ccy network policy;
  and a trust mapping in which everything inbound is at most PEER-level information, never a ping.
  Both openrig and stageflow chose exactly this edge-only shape: openrig ships recipes, not a
  command (`openrig/docs/reference/a2a-agents.md`, opening paragraph), and stageflow serves only
  published pipelines, with no streaming, cancel or push (`stageflow/docs/a2a.md`, "Status").
  openrig also records that two A2A stacks did not interoperate in its trial (same file, "Before
  either recipe"). **Needs discussion, no current demand.**
- **Borrow the task model, not the wire:** this is the cheap, useful part. Concretely: (a) a task
  state vocabulary for 00251 and for idea 2 above, with `input-required` (waiting on a human or the
  coordinator) and `auth-required` (waiting on a permission or credential) distinct from `blocked`;
  (b) `rejected` as distinct from `nack` (will not, versus cannot now); (c) a `contextId`-style
  thread id so a coordinator can list everything belonging to one piece of work; (d) the
  in-task authorisation chain as a model for the liaison (00225): the coordinator's question is
  "auth-required", the liaison passes it up, and the answer authorises only what it names (§7.6.4,
  which matches 00225's rule that the liaison's words are never authority). **Needs discussion
  (plan input to 00251 and the teamchat verb table).**

### Code worth lifting

`specification/a2a.proto` (Apache-2.0) if a gateway is ever built; otherwise use the official Go SDK
(not cloned here; its licence is unverified).

### Risks

Spec churn (a large breaking 1.0 only eight months ago); interoperability gaps between stacks in
practice (openrig's note above); a network endpoint is a new attack surface on a design that has
none.

---

## agent-substrate/substrate

Studied: baseline at dc747ef8560bc4b5bd76aa79078a9ffcd467e362

Apache-2.0; a sandbox runtime, not a team system.

### What it is, and who it is for

A Kubernetes runtime that runs millions of mostly idle, untrusted agents ("actors") in gVisor or
microVM sandboxes, snapshotting idle actors out of memory and resuming them onto a small warm pool
in under 500 ms (`README.md`, "What is Agent Substrate?"). For platform teams; Google's Agent
Executor and kagent build on it (`README.md`, "Ecosystem"). Written in Go.

### Architecture (relevant parts)

No team, role or messaging model: "the application layer" owns agent registries, lineage and
human authentication (`docs/security/security.md`, "Security Scope"). What overlaps is ccy's
container model:

- **Zero credentials in the sandbox.** An egress gateway terminates the actor's TLS for listed
  names, replaces a placeholder header with a credential fetched from a provider for that actor,
  and re-originates the request; the actor never holds the secret and cannot choose what leaves
  (`docs/guides/egress-credential-injection.md`, "How it works").
- **Default-deny networking**, DNS filtering, fail-closed egress policy (`security.md`, "Network").
- **Suspend and resume** of idle actors, with **request parking**: a request for a suspended actor
  waits a bounded time while it resumes instead of failing (`docs/concepts/request-parking.md`).
- **Threat model as data.** `docs/threats.json` lists each threat with priority, invariant and
  concrete mitigations; `security.md` tables give each principle a status of Met, In Progress,
  Future or Out of Scope.

### Maturity and activity

1,760 commits from 2026-05-19 to 2026-11-20; declares 1.0 with API stability (`README.md`, "Status
and compatibility"). Busiest month October (621). Backed by a weekly community meeting and CNCF
Slack channels (`README.md`, "Community").

### Licence and reuse

Apache-2.0 (`LICENSE`), per-file Apache headers in Go sources (for example
`internal/pidfd/doc.go`), third-party licences under `_LICENSES/`, a `vendor/` tree. Class: **lift**,
keeping the per-file header.

### Comparison with ours

Ours is a single-user desktop with rootless Podman and an nftables policy; substrate is a
multi-tenant cluster with VM-grade sandboxes. Not a competitor. The one place it is clearly ahead
of ccy on ccy's own terms: **ccy puts the Claude OAuth token and a GitHub token into the container
as environment variables** (`docs/ccy.md`, "What the container CAN reach"), and ccy's threat model
names credential exfiltration by prompt injection as a risk it does not remove (`docs/ccy.md`, "The
Security Model").

### Ideas worth adopting

1. **Keep the GitHub token out of the container.** A host-side egress proxy for a ccy session that
   injects the token into requests to the GitHub API and git over HTTPS, the container holding only
   a placeholder. Needs a CA trusted inside the container and a proxy per session; the Claude token
   is harder (Claude Code authenticates itself). Lands in `src-go/cmd/claude-yolo` and the ccy
   network policy. **Needs discussion (plan)**: it changes ccy's security model.
2. **Status column in the security model.** Give each row of ccy's "What the container CAN reach"
   and the residual risks a status (met, partial, future, out of scope). **Clear win (issue)**,
   docs only.
3. **Request parking for parked seats.** A message to a parked or suspended seat waits, bounded,
   while the seat is resumed, instead of being reported failed. Overlaps orgtree's "halted vs retired
   vs not a target" outcomes below. **Needs discussion.**

### Code worth lifting

Little, because we already have equivalents: process-exit watching via pidfd
(`src-go/internal/execx/start.go`, `src-go/internal/teamchat/member/keeper/osproc.go`) and a team
PKI (`src-go/internal/teamchat/pki/`). One candidate: `internal/localca/localca.go` (386 lines,
Apache-2.0 header) has a CA pool that a long-running signer re-reads when an administrator rotates
it, with no restart (`NewRefreshingPool`). Worth a look only if teamchat's CA gains rotation; keep
the header if lifted.

### Risks

Kubernetes, gVisor and Cloud Hypervisor are far outside this repository's scope; adopt ideas only.

---

## Maurdekye/orgtree

Studied: baseline at 1016ed38b1a2b45327ef963175b2089de2ad6e3e

MIT; an organisation of agents with mail, a docket and capacity.

### What it is, and who it is for

A desktop app (Windows first; a standalone engine runs on servers or in Docker) that runs a tree of
agents of several providers (Claude Code, Codex, Antigravity, OpenRouter, local models) as an
organisation: managers and specialists, a live canvas, mail, a work docket and a usage view
(`README.md`). Engine in Rust with PostgreSQL (`README.md`, "Built to keep running"). Sister projects:
a phone chat app and a self-hosted mail hub for cross-machine mail.

### Architecture

- **Team and roles.** A tree. Each agent has a **charter** (its standing role); a manager's team
  charter applies to everyone below it; org instructions apply to all (`docs/managing-agents.md`,
  "Choose a role and hire"). Agents hire agents.
- **Capacity.** **Credits**: each model tier has a seat cost; each agent has a grant to spend on
  its own team; retiring releases capacity (`managing-agents.md`, "Understand seats and credits").
  Separate from provider billing.
- **Messaging.** Typed mail between agents and the user, delivered at safe tool or turn
  boundaries, with a journal so confirmed batches are never replayed and pending ones pass to a
  successor session (`docs/mail-delivery-boundaries.md`). **Who may write to whom** is explicit:
  up and down the tree by default, siblings when the parent allows it, and directional
  **audiences** granted for anything else (`docs/collaborating.md`, "Understand who can message
  whom").
- **Unavailable targets** have three distinct sender outcomes that must never collapse into one:
  halted (stored, waits for unhalt), retired (stored, delivered only if someone rehires, which
  nothing schedules), and not a target (refused, nothing stored, and the refusal enumerates no
  other members) (`mail-delivery-boundaries.md`, "Unavailable targets").
- **Task ownership.** A docket of tickets with owner, requirements, status and progress
  (`README.md`, "A shared work docket"); retiring an agent does not complete its tickets
  (`managing-agents.md`, "Stop, pause or finish work").
- **Shared work primitives.** Project mail queues in round-robin or ACK mode with batch sizes,
  reclaim after a reminder budget, and a dead-letter line; **concurrency slots**, cooperative leases
  that are released automatically when the holder is frozen by a usage limit, halted, moved, or its
  turn crashes; one-shot and continuous **events**; **watchdogs** that wake an agent on a file
  change, a process stopping, a command's output, a usage threshold or an engine event, surviving
  engine restarts (`docs/projects-and-watchdogs.md`).
- **Halt is enforced.** Halt persists a "halting" phase, closes admission and mail delivery,
  terminates the provider process tree, and only then reports "halted"; it survives restart,
  retirement and rehire, and only an explicit unhalt releases it (`docs/agent-halt.md`).
- **Humans.** A "Needs attention" list gathers questions, urgent mail and flagged tickets; **DND**
  holds non-urgent mail for an agent's current turn while urgent mail still arrives
  (`managing-agents.md`, "Hold mail for focused work").
- **Models and accounts.** Several accounts per provider, a usage window, **account fallback** to
  another account at a limit, and auto-resume one minute after a reported reset
  (`docs/operating-orgtree.md`, "account-fallback" and line 87).

### Maturity and activity

11,721 commits from 2026-09-07 to 2026-11-20. Almost all under one author name; about forty other
committer names are agent seat names (for example `coordinator-opus`, `symmetry-kimi`), so the
project is largely built by its own agent organisation (`git log --format=%an`). Very high
velocity; release notes 2.0.0 onward in `docs/`.

### Licence and reuse

MIT (`LICENSE`, copyright 2026 Maurdekye); `THIRD_PARTY_NOTICES.md` for bundled components.
Class: **lift**. Code is Rust and TypeScript: ideas, not code, are what transfer.

### Comparison with ours

orgtree is ahead on **enforced lifecycle** (halt as a durable gate versus our `halt` ping, which
asks an agent to stop at its next safe point and trusts it), **capacity budgeting**, **shared-resource
leases**, **sender-visible delivery outcomes**, and **account fallback**. Ours is ahead on
isolation (orgtree's remote access is "like giving it SSH access", `README.md`, "From another
computer"), on the trust model for agent input, and on humans reading the whole conversation.
Its audiences are a working version of 00207's communication gates, from the other direction:
default routes from the tree, explicit directional grants for the rest.

### Ideas worth adopting

1. **Three sender outcomes for an unavailable seat** (parked, removed, not a member), each with its
   own wording, and no member enumeration on the refusal. Lands in teamchat `say`/`send` and
   `ccy-teams`. **Clear win (issue)** for v2.
2. **An enforced halt.** The coordinator's or a human's halt closes delivery and new turns for a
   seat, persists, and is released only explicitly; a seat cannot halt itself. Lands in teamchat and
   the ccy supervisor. **Needs discussion (plan)**: today `halt` is a verb with an ack, by design.
3. **Shared-resource leases.** A team-level lease (for example "one deploy at a time", "one
   full-QA run at a time"), first-come, released automatically when the holder's seat parks, hits a
   usage limit or dies. Fits the deployer role (00207) and the rule that only one full QA runs at a
   time. **Needs discussion (plan).**
4. **Directional audiences** as the model for 00207's "named allowances": a grant from A to reach
   B is not a grant from B to reach A. **Plan input** to 00207.
5. **Account fallback at a usage limit**, with auto-resume after the stated reset. Lands in ccy's
   token menu (`docs/ccy.md`, "No session on a token with no usage left") and 00251's rate-limited
   state. **Needs discussion.**
6. **State-change lines in the transcript** ("Idle for 19s", "Active for 32s") so time spent idle
   reads in place (`managing-agents.md`, end of "Stop, pause or finish work"). Cheap for the
   coordinator's view in 00251. **Plan input.**

### Code worth lifting

None (Rust/TypeScript; the ideas are the value).

### Risks

Single-maintainer project built at very high speed largely by agents; behaviour is described in
minute detail, but the docs read as change notes as often as reference (**guess** at stability).
Credits and charters assume agents hire agents freely, which is not how our teams are staffed
(the owner and the broker make seats, 00197).

---

## tejasghutukade/stageflow

Studied: baseline at 3f68656337c37870b0848ec0a311167d9204701c

MIT; pipelines of fresh agent sessions with verified hand-offs.

### What it is, and who it is for

A runtime for multi-stage agent workflows in YAML: a DAG of stages, each run in a fresh agent
session, handing typed **envelopes** and artifacts to the next, with human gates, CI mode and an
operator console (`README.md`, "Why Stageflow?", "Features"). The execution backend is Pi; Claude
and Cursor stage backends are experimental. For developers who want repeatable agent pipelines
locally and in CI.

### Architecture

- **No standing team.** Stages, not seats; each stage is a new session. A scheduler owns DAG
  readiness, fan-out, joins, loops, retries and resumable gates (`README.md`, "Architecture at a
  glance"; `docs/architecture.md`).
- **Hand-off contract.** A stage must call one tool exactly once to emit an envelope with
  `status`, `summary`, `artifacts` and a schema-checked `payload`, plus `fork_choice` or a
  feedback-loop decision where the stage declares them (`docs/envelopes.md`, "Contract").
- **Verified stage execution.** A stage's success is checked, not taken on its word: run a
  command, require an artifact, re-check the payload schema, require a real change in the
  checkout, require an operator gate; a checklist is recorded as an attestation and labelled as not
  independent verification (`docs/verified-stage-execution.md`, "Core check types"). On failure,
  repair with a bounded number of attempts or hand to the operator.
- **Humans.** `ask_operator` gates of four kinds (free text, confirm, several questions, review an
  artifact); an emit-phase gate stops a stage claiming success before the operator answered
  (`docs/hitl.md`). CI exits 2 when a run waits on a human (`README.md`, "Features").
- **A2A.** An inbound A2A server for explicitly published pipelines: caller tokens shown once and
  stored only as a SHA-256 digest, at-most-once run submission, no streaming, no cancel, no push
  (`docs/a2a.md`, "Status", "Manage A2A from the CLI or the API").

### Maturity and activity

1,147 commits from 2026-08-24 to 2026-11-18, essentially one author (three identities) with a few
agent commits. Published on npm and GHCR; release line 0.66 (`README.md`, "Docker").

### Licence and reuse

MIT (`LICENSE`, copyright 2026 Tejas G). Class: **lift**. TypeScript.

### Comparison with ours

Different shape: a pipeline engine rather than a standing team. The overlap is the hand-off. Our
`done` carries a forge reference that the receiver checks exists at a trusted commit, which is
stronger provenance than stageflow's artifact path; stageflow is stronger on **checking the claim
itself** (tests pass, files changed, schema holds).

### Ideas worth adopting

1. **Verify a `done` before believing it.** When a worker answers a coordinator's `review` or
   `run-qa` with `done`, the coordinator's tooling runs declared checks (the commit exists and is
   reachable, which we do; the named QA passed on that commit; the diff is non-empty) before the
   coordinator acts on it. Lands in teamchat or the coordinator skill. **Needs discussion (plan)**.
2. **Label attestations as attestations.** A self-reported checklist is shown as "attested", never
   as "verified". Cheap wording rule for reports and the questions room. **Clear win**, folded into
   idea 1 of openrig (receipt honesty) as one issue.
3. **A distinct exit code for "waiting on a human"** in non-interactive tools. Lands in
   `pingbus`/`teamchat` exit codes and plan scripts. **Needs discussion** (exit codes are stable,
   `docs/agent-bus-protocol.md` §14).
4. **Hashed, show-once caller tokens** if a gateway (A2A or otherwise) is ever built.

### Code worth lifting

None needed.

### Risks

Pi as the main backend; Claude support experimental. A pipeline model fits repeatable jobs, not an
open-ended standing team.

---

## rogeriochaves/open-managed-agents

Studied: baseline at e9a074356251f05005f67edb8e8ed1a1ab266974

AGPL-3.0; **CLEAN ROOM**.

This section describes behaviour only, in our own words. No code, pseudo-code, comments or prompts
from the repository are reproduced. File paths are cited as evidence.

### What it is, and who it is for

A self-hosted web platform that imitates a hosted "managed agents" product: an agent builder with
templates, sessions with a live event stream, many LLM providers, and organisation, team and
project governance with role-based access, budgets, per-team provider and MCP connector policies,
an audit log and encrypted credential vaults (`README.md`, "Features"). For platform teams that
cannot send data to a third-party agent service.

### Architecture

A Node server, a web UI and a CLI in a pnpm workspace (`packages/`), SQLite or Postgres, Docker
Compose and a Helm chart (`docker-compose.yml`, `helm/`). Behaviour is specified as Gherkin
feature files (`specs/`). No team or agent-to-agent messaging: each session is one agent loop with
a human sending messages. Governance can be loaded from a JSON file at boot
(`governance.example.json`).

### Maturity and activity

150 commits, all between 2026-04-09 and 2026-04-10, one author. **Dormant** since.

### Licence and reuse

AGPL-3.0 (`LICENSE`). Class: **clean room**. Nothing is proposed for lifting.

### Comparison with ours

Little overlap with teams or the bus. Two behaviours are worth noting as lessons, both found by
its author while dogfooding and written up in its specs:

- A request schema declared optional fields that the handler never read, so updates returned
  success and changed nothing; the fix was a test that fails when a declared field is not used by
  its handler (`specs/schema-handler-alignment-lint.feature`).
- A per-team connector policy was stored and editable but not consulted when a session started,
  so it had no effect until enforcement was added at session creation
  (`specs/mcp-policy-enforcement.feature`).

### Ideas worth adopting

Both lessons are one defect class: **configuration that is declared and accepted but enforced
nowhere**. openrig shows the same class in its own docs (`pool`, `continuity_policy`, gateway
policy text). See the group section, item 3. A cooperative stop that emits a declared terminal
state between iterations (`specs/engine-cooperative-cancellation.feature`) is ordinary practice
and nothing new for us.

### Risks

Licence (AGPL) and dormancy. Not worth watching.

---

## subgentic/coral

Studied: baseline at 20218bbf4fdbd8f50e5ac43698f32491955a4fd2

Apache-2.0 source (with a proprietary EULA on release builds, below); a multi-agent orchestrator
with a shared board, task queue and one human inbox. Formerly `cdknorow/coral`.

**This builds on the Plan 00193 audit** ([AUDIT-coral.md](../../Plan/Completed/00193-coral-adoption-audit/AUDIT-coral.md),
read at `e785da3`, verdict "leave it, borrow ideas"). That audit's architecture, security and
maintenance findings are not repeated here; this section records what changed in the 16,098 commits
since (`git log e785da3..HEAD`), re-checks the audit's deciding facts, and adds what matters for the
team bus and Plan 00251. Coral is already on the watch list (Plan 00200).

### What changed since the audit

- **Repositioned as "the inbox for your coding agents".** The headline is now **Needs you**: one
  list of every agent waiting on a permission, a question or a file tagged for review, answered in
  one tap, after which the agent resumes its turn (`README.md`, "The Inbox: Your Agents Report to
  You"). Files have versions with an inline compare and passage comments, and Approve, Approve with
  a note, or Request changes.
- **Goals with a single checkable "Done when" statement** group tasks and artifacts; a goal whose
  tasks are all complete shows as "looks done" rather than done, and closing one warns about files
  still in review (same README section).
- **Agent lifecycle is Awake, Asleep or Archived**; stopping archives an agent but keeps its seat
  and name, and a new agent cannot take an archived member's name without starting it or choosing
  another (`RELEASE_NOTES.md`, v2.1.1). The orchestrator cannot stop, sleep or remove itself or the
  last orchestrator (same entry).
- **A deterministic team health check for the orchestrator** (`coral-go/internal/teamhealth/`):
  only items that need its judgement, ranked, each with evidence and a suggested next action, then
  the questions waiting on the human (at most five, oldest first), then one line on what moved;
  capped at 4,096 bytes; sent to the orchestrator alone, never to the board; and "never silent",
  one line when there is nothing (`check.go`, top-of-file comment and `MaxCheck`). Each item has a
  stable key; once the orchestrator acknowledges it, it stays off the list until its state changes
  (`check.go`, `Item.Key`, `ItemID`). Overdue is judged against **the member's own history**:
  medians and the 90th percentile, never averages, said once at the threshold and again only at the
  next doubling (`deviation.go`, `UsualTaskTime`, `TaskOverdue`, `Level`).
- **A resource queue**: named resources with N slots, leases that end on release, on the holder's
  death or on expiry, priority plus ageing plus first come, and a human override that sticks
  (`coral-go/pkg/resourcequeue/resourcequeue.go`, package comment). This is orgtree's slots idea in Go.
- **Display rules for agent text shown to a person.** A permission card shows the request whole up
  to a size limit; anything longer, or holding a control, bidi or zero-width character, is said so
  in words and may only be rejected (`coral-go/internal/permreview/permreview.go`, package comment).
  One sanitiser for everything an agent wrote that coral shows (`internal/teamhealth/sanitize.go`).
- **Committers are now its agent seats**: the top names since the audit are `qa`, `dryrun`, a DevOps
  stack, "Backend Dev", "Frontend Dev"; a release is "a prerelease until the Operator promotes it"
  (`RELEASE_NOTES.md`, v2.1.1). About 8,900 commits in October and 7,600 so far in November.

### The audit's deciding facts, re-checked

| Audit finding (at `e785da3`)                   | Now                                                                                                                                                                    |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Listens on `0.0.0.0` by default                | **Fixed**: default `127.0.0.1`, widened only when remote access is turned on (`coral-go/internal/config/config.go:42`, `:184`)                                         |
| Loopback is fully trusted                      | **Still the default**: a direct loopback peer passes with no credentials (`internal/auth/middleware.go:22`, `:40`); a `require_auth_on_loopback` option exists (`:54`) |
| Agents run on the host as the user, no sandbox | Unchanged (no container launcher found; **guess** from the package list, `coral-go/internal/`, not re-audited line by line)                                            |
| Free text typed into other agents' terminals   | Unchanged in kind; the health check now goes "through the typing gate" (`teamhealth/check.go`, top comment)                                                            |
| Proprietary EULA in release builds             | **Still present**: "revocable" licence, codebase the "exclusive property" of subgentic LLC (`coral-go/pkg/license/terms.go:24`, `:41`); `LICENSE` is Apache-2.0        |
| Single maintainer, agent-written               | More so (above)                                                                                                                                                        |

The audit's verdict stands: do not adopt or fork. The isolation and trust mismatch is unchanged.

### Licence and reuse

`LICENSE` is Apache-2.0 and governs the source; no per-file headers were found in the Go packages
named below. The EULA in `pkg/license/terms.go` is shown by release builds and claims more than the
Apache grant allows a licensor to take back for already-published source. Class: **lift**, with a
caution: **ask the owner** before lifting, since the project's own text contradicts its licence
file, and keep the Apache notice plus a pointer to the upstream path in anything copied.

### Comparison with ours

Coral is ahead on **the human's view**: one ranked queue of everything waiting on a person, with
review of versioned files, and a deterministic, size-capped health report for the orchestrator.
That is the shape Plan 00225 (liaison) and Plan 00251 (seat status) are reaching for; 00251's own
research already notes the "Needs you" idea (`CLAUDE/Plan/00251-team-seat-status-for-the-coordinator/RESEARCH-seat-status.md`,
line 252). Ours stays ahead on isolation, on authenticated per-seat identity, on forge-verified
references, and on humans seeing the whole conversation.

### Ideas worth adopting

1. **The coordinator's health check as a deterministic, ranked, acknowledgeable, size-capped
   message**: rank, evidence, suggested action; acknowledged items stay quiet until their state
   changes; never silent. This is the "notify" half of Plan 00251. **Plan input** to 00251.
2. **Overdue against a seat's own history**, median and p90, falling back to the team's history
   below a minimum sample count, announced at the threshold and at each doubling only. Avoids both
   fixed timeouts and alert floods. **Plan input** to 00251.
3. **"Done when" on a goal, and "looks done" until a human closes it.** Pairs with stageflow's
   verified execution: completion of every task is evidence, not closure. **Needs discussion**,
   with takeaway 2 of the group section.
4. **Display rules for agent text shown to a human** (show whole or refuse; name control, bidi and
   zero-width characters in words). Our questions room and Element show agent text to humans.
   **Clear win (issue)** for teamchat: check what it renders against these rules.

### Code worth lifting

All Go, standard library only, Apache-2.0 source (owner's go-ahead first, see above):

- `coral-go/pkg/resourcequeue/` (`resourcequeue.go`, 959 lines; `spread.go`, 214): leased slots with
  injected clock and liveness, no goroutines, optional persister. The best ready-made base for the
  lease idea (orgtree takeaway 3) if a team needs "one deploy at a time".
- `coral-go/internal/teamhealth/deviation.go` (77 lines): median, percentile, own-or-team usual task
  time, the doubling levels. Small and self-contained.
- `coral-go/internal/permreview/permreview.go` (176 lines): the show-whole-or-reject rule.
  `internal/teamhealth/sanitize.go` depends on coral's `secrets` and `redact` packages, so it is
  not a clean lift.

### Risks

Everything in the 00193 audit's "Security concerns" and "Maintenance risk" still applies, with
churn now about 430 commits a day (16,098 commits from the audit's plan date, 2026-10-14, to 2026-11-20). The licence ambiguity above. Any lifted file is a snapshot:
upstream will not be tracked.

---

## Group section: the strongest takeaways, ranked

1. **Seat state is the gap, and openrig has already solved most of it (Plan 00251).** Three
   orthogonal axes (process present, activity, resumable), one arbiter over ranked evidence (Claude's
   own session file, then hooks, then visible prompts, then pane sampling), `unknown` as an honest
   answer, needs-input as a count plus a reason, a rate limit taken only from Claude's typed
   `StopFailure` reason, and **PARKED = idle or blocked while holding open work**, which is exactly
   00251's idle-lock. coral supplies the notify half: a deterministic, ranked, size-capped health
   check for the orchestrator, with evidence and a suggested action per item, items that stay quiet
   once acknowledged until their state changes, and "overdue" judged against each seat's own
   median and p90 (liftable Go, `coral-go/internal/teamhealth/deviation.go`). orgtree adds
   in-transcript state lines and enforced halt. Feed this into 00251's pending design decision
   rather than designing from scratch.
   Sources: `openrig/docs/reference/agent-state-taxonomy.md`,
   `openrig/docs/reference/claude-status-line.md`, `coral-go/internal/teamhealth/check.go`,
   `orgtree/docs/agent-halt.md`.
2. **Make work an object with an owner and a closure rule, not just a verb (teamchat v2).** openrig's
   queue with a closure contract (no `done` without saying where the work went), orgtree's docket and
   leases, coral's goals with a "Done when" check and its leased resource queue (liftable Go,
   `coral-go/pkg/resourcequeue/`), A2A's task states (`input-required`, `auth-required`,
   `rejected`) and stageflow's verified hand-off all point the same way: our `done`/`blocked`/`ack` pings carry no owner, no
   state and no successor, so a dropped baton is invisible. The cheapest first step is vocabulary:
   adopt A2A's state names for task state in 00251 and a required closure field on `done`; a full
   queue is a plan. Our forge-verified references stay the trust anchor.
3. **A new defect class for Defence Before Fix: declared but unenforced configuration.** Seen in
   open-managed-agents (a policy stored but never consulted; schema fields never read) and in
   openrig (validated, stored, inert `pool` and `continuity_policy`). Our exposure: team-record keys
   and role contracts (00207's communication gates, minimum models) that a reader accepts but no
   code path enforces. A detector in `src-go/qa/rules` could require that each team-record and
   member-config field has a consumer outside its parser and validator. **Plan** (DBF).
4. **Delivery honesty and sender outcomes.** Keep "written", "acknowledged", "done" and "verified"
   apart everywhere they are shown; tell a sender which of parked, removed or not-a-member it hit,
   without leaking the roster; label self-attested checklists as attested; show agent text to a
   human whole or not at all, naming control, bidi and zero-width characters (coral's
   `internal/permreview/permreview.go`). These are small, concrete changes to teamchat and the
   board. **Issues.** Sources:
   `openrig/docs/as-built/architecture/coordination-primitive.md` §2,
   `orgtree/docs/mail-delivery-boundaries.md` "Unavailable targets",
   `stageflow/docs/verified-stage-execution.md`.
5. **Do not speak A2A on the bus; keep it as an edge option.** A native Matrix binding would mean
   every core operation for no current member; a gateway seat would add the bus's first listening
   endpoint. The two nearest projects that support A2A kept it at the edge and incomplete. Borrow
   the task model (item 2) and the in-task authorisation chain as a model for the liaison (00225).
   Revisit only if an outside agent needs to hand work to a team.

Also worth a plan, outside the team bus: **keeping the GitHub token out of ccy containers** with a
host-side injecting proxy (substrate's egress credential injection), since ccy's own threat model
names token exfiltration as an unmitigated risk.

### Watch list

- **mvschwarz/openrig**: already on the watch list (`CLAUDE/Watchlist.tsv`, not yet reviewed);
  keep. Highest value in this group.
- **a2aproject/A2A**: add, low frequency; watch for v1.1 elicitation and bidirectional streaming.
- **Maurdekye/orgtree**: add; watch halt, delivery and lease semantics.
- **tejasghutukade/stageflow**: optional; watch verified stage execution only.
- **agent-substrate/substrate**: optional; watch egress credential injection.
- **subgentic/coral**: already on the watch list from Plan 00200 under its old name
  `cdknorow/coral`; update the entry to the new owner. Watch `internal/teamhealth` and the inbox,
  not the whole firehose.
- **rogeriochaves/open-managed-agents**: do not add (dormant, AGPL, little overlap).
