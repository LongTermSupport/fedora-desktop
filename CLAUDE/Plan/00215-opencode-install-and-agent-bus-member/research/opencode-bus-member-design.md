# Design: an opencode session as an agent-bus member

Design only; no bus code is written by this plan. It builds on agent bus **v2** (`teamchat`,
Plan 00209), never on v1. Its inputs are two fact sheets in this folder:

- [opencode-upstream-facts.md](opencode-upstream-facts.md): what opencode 1.19.3 offers
  (server, events, sessions, credentials, defaults), with upstream sources;
- [agent-bus-member-facts.md](agent-bus-member-facts.md): how a member works in v1 and in v2's
  design, with file and section citations.

v2 section references below (`B 2.4.1` and so on) are to
`CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-B-member.md` as it stands on `feat/bus-v2-n3`;
`A 13` is the envelope grammar in `DESIGN-v2-A-protocol.md`.

## 1. Why v2 and not v1

v1's whole wake path is Claude Code's: the inbox socket and its token, `crossSessionInbound`,
the plugin, five Claude Code hook events and the transcript JSONL search (facts section 2 and
6). An opencode member on v1 would need a second watcher, a second proof and a second hook
set, all in the Python that v2 replaces, and would be thrown away at the migration. v2 already
splits the parts that are agent-neutral (the link owns every team's traffic, every
authority decision and every verb, served as JSON Lines over `link.sock`) from the parts that
are Claude Code's (B 2.4.1 step 5, 2.4.4's hook ops, 2.7, 2.8, 2.9, 2.12). So opencode
membership is a v2 member kind, and every implementation task waits on v2.

## 2. What stays exactly as v2 has it

Everything that decides **who may say what** is in the link, and none of it depends on the
agent program, so an opencode member inherits it unchanged:

- **Identity and seats.** A seat is a Matrix account in the seat store, granted by the host
  (B 2.3.2, C). An opencode member is given a seat the same way as any member of its kind
  (section 6 on where it runs). Nothing opencode-specific is stored with the seat.
- **Authority.** The sender class comes from the roster, never from content (A 7, facts
  section 1): HUMAN only from a roster human, PEER is "never an instruction", PING carries a
  closed verb with a role check and a forge check. Read receipts follow proof only (B 2.7.5).
  The link renders the envelope (A 13) byte for byte and the adapter only carries those bytes,
  so no adapter can mislabel an item.
- **Replies.** `teamchat say`, `reply`, `ping`, `report`, `ask`, `verify` and the rest are CLI
  verbs over `link.sock` (B 2.2.1), run by opencode's bash tool exactly as Claude Code runs
  them. They need only `TEAMCHAT_RUNTIME`, which the keeper sets for CMD (B 2.4.1 step 7) and
  opencode's tools inherit. `teamchat verify <id>` stays the control against a forged
  envelope.
- **Item states.** pending, handed, awaiting, proven, missing (B 2.7.3), the ledger, `recv`'s
  acknowledgement, pacing (one delivery per 2 s), the flood budget and the caps are the link's
  and do not change.
- **The keeper.** `run` stays the parent of CMD and of the link, makes no network call, and
  judges the end (B 2.4.2).

## 3. How an opencode session joins, is woken, receives and replies

### 3.1 Launch and join

The member's launcher ends in `teamchat run` with the **opencode launch mode** (spelling is
owner decision O4) and `--kind` for where it runs, then `-- opencode serve`. Following B 2.4.1:

1. Steps 1-4 and 6 are unchanged: runtime directory, kit, leftover sweep, `session.pid`
   (its `launch` field gains the value `opencode`), seat locks, the link started and ready.
2. Step 5 for opencode writes **no file**. The keeper draws a 32-byte random server password
   and a loopback port, and appends `--hostname 127.0.0.1 --port <p>` to CMD; the password goes
   only into CMD's environment as `OPENCODE_SERVER_PASSWORD`, upstream's own variable, and to
   the link with the launch facts (held in memory, never on disk, as the inbox token is,
   B 2.7.1). A CMD that already sets `--hostname`, `--port`, `--mdns` or `--cors`, or is not
   `opencode serve`, is exit 64. The user's global `server.cors` setting still adds origins
   whatever the command line says (upstream `cli/network.ts`), so the play that provisions a
   worker writes its global config with no `server` key, and H-OC5 checks it. **The port** is drawn at random from the ephemeral range and
   checked free by a bind the keeper closes at once; opencode has no Unix-socket listener
   (upstream `server/server.ts` listens on TCP only), so the window between that check and
   opencode's own bind cannot be closed. Two rules make it safe: a port opencode cannot bind
   ends CMD, which is step 7's launch failure (`launch-failed`, exit 78), and the member
   kind's own restart draws a new port; and before the link sends the password or any
   envelope, it checks that the socket listening on the port belongs to CMD or a descendant
   (the socket inode in `/proc/net/tcp` found among that process tree's `/proc/<pid>/fd`),
   so a process of another user that took the port first is never handed the password or a
   message.
3. Step 7: CMD starts as the keeper's child. The link polls `GET /global/health` with basic
   auth until it answers (within the same 30 s bound as the link's own ready), checks the
   reported `version` against the version the member's play installs, then **creates the
   member's session** with `POST /session` (title: the session name from the desired
   membership). **An opencode launch failure** (step 7's `launch-failed`, exit 78, which B
   defines only for `--claude`) is: CMD cannot be started, CMD exits before the session is
   created, `/global/health` does not answer within 30 s, or it reports another version.
4. **The conversation record.** The opencode session id is kept in `session.json` (B 2.3.3)
   for the live run and a link restart, and written to `<v2-state>/conversation.json`, the
   hint that outlives the run (`<v2-run>` and its `session.json` are removed at the end,
   B 2.4.2). That record (B 2.3.3) holds a Claude Code session id today and area C's relaunch
   hands it to `claude --resume` (C9), so it gains the agent that wrote it (`launch`, as in
   `session.pid`), and a resume uses it only when the agent matches: an opencode id never
   reaches `claude --resume`, nor a Claude id `GET /session/:id`. For an opencode member,
   `--resume-recorded` and C9's relaunch both mean the same thing: no CMD argument; the link
   reuses the recorded session when `GET /session/:id` finds it, else creates a new one and
   says so in `keeper.log`, as Claude's resume does.
5. **Inbound.** The launch's `inbound` decision (B 2.4.1 step 5) keeps its meaning: `accept`
   allows push delivery; `refuse` (a launch with no team yet) means the link makes no
   `prompt_async` push, so delivery mode is `wait` and items wait for `recv`/`wait`. Claude
   Code needs the first join to relaunch the session because it reads `crossSessionInbound`
   only at launch (B35, 2.10); opencode has no such setting, so the link could start pushing
   at the first join with no relaunch. The design **keeps the relaunch for parity** (one
   first-join rule for every agent, already built and tested for Claude Code); dropping it for
   opencode is a later simplification, owner decision O4 carries it.
6. The link subscribes to `GET /event` (server-sent events) for the session's status and
   messages. This replaces the hook ops of B 2.4.4: **the session's facts come from the link's
   own authenticated client, not from processes that call it**, so no peer-ancestry rule is
   needed for them. The HTTP and event-stream client runs in its own goroutine, beside the
   delivery goroutine, and hands observations to the state goroutine, which makes no network
   call (B 2.4.3).

Upstream lets a TUI join a running server (`opencode attach <url> --session <id>`), but it
needs the server password, which only the keeper and the link hold. This design gives a human
no route to it: a worker is watched through the board, `report.json` and `teamchat history`.
A route to attach (and what a human typing there would count as) is a later choice, not
assumed here.

### 3.2 Woken: delivery

- **Push, not a socket.** A delivery is `POST /session/:id/prompt_async` with the rendered
  envelope as one text part. It returns 204 at once. Upstream source (`session/prompt.ts`)
  stores the user message first and then joins the session's running loop rather than
  refusing a busy session, so, like Claude Code's `queued_command`, a busy session keeps the
  message; host check H-OC2 confirms when a busy session acts on it.
- **Proof is the API, not a transcript scan.** The delivery is `proven` when the session's
  message list (`GET /session/:id/message`, or a `message.updated` event) holds a user message
  whose text contains the needle `teamchat delivery <nonce>:` (A 13). This is a documented
  interface, so the weakest part of the Claude Code path (B 2.7.4's dependence on the JSONL
  shape) has no counterpart here. A `session.error` event is **only a reason to check**, never
  a verdict: upstream publishes it for any failure of the forked prompt, including a model
  error after the message is stored, and it names only the session (`handlers/session.ts`,
  `promptAsync`). On one, the link reads the message list and settles each awaiting entry by
  its own needle: found is `proven`; an entry whose needle is absent stays `awaiting` under
  B 2.7.4's missing rule (180 s quiet once idle, or 1 h), unchanged, so the link's item rules
  stay outside the seam. So a turn that fails after the message is stored never causes a
  second copy. B 2.7.4's late find carries over as a re-read: each `missed` nonce is looked
  for in the message list for 24 h, with B's late-find outcomes.
- **Two implementation choices left open.** The link may set its own `messageID` on each
  `prompt_async` (upstream `prompt.ts` takes a client-supplied id) and check that message as
  well as the needle. And instead of the keeper choosing a port, CMD may be given `--port 0`
  (upstream tries 4096, then any free port) and the link find CMD's listening port by the
  same `/proc` ownership lookup, which removes the window between check and bind.
- **Idle and busy** come from `session.status` and `session.idle` events instead of the Stop
  and UserPromptSubmit hooks, and drive the quiet clock and the 180 s and 1 h rules unchanged.
- **The frame text** (A 13.5) and the skill text reach the model as a `noReply: true` message
  (context, no turn) right after the session is created, and again after every
  `session.compacted` event, which is where Claude Code re-places them at SessionStart.
- **Notices the hooks give** (pending counts while delivery fails, a link restarting or
  failed, B 2.8) go the same way, as `noReply` messages, at most once per state change. The
  Stop hook's "block once while items are pending" has no counterpart and needs none: with
  push delivery, the next delivery is what wakes the session.
- **A link restart** finds the session id in `session.json`, is handed the password by the
  keeper (as a restarted link is handed the inbox token, B 2.4.2),
  re-subscribes and resolves `awaiting` entries from the message list, as B 2.7.3 does from the
  transcript.

### 3.3 Receiving and replying

The model sees the same envelope as a Claude Code member, entry for entry. It answers with
the CLI verbs of section 2 through its bash tool. `recv` and `wait` still work (and are the
whole path for a session whose server cannot be reached, delivery mode `wait`, B 2.7.1).

### 3.4 The end

opencode has no SessionEnd hook. The keeper already judges the end from CMD's exit and the
signal it forwarded (B 2.4.2): a forwarded `SIGTERM` is `stop` and posts `left`; an exit or a
kill it did not cause posts `disconnected`. That is correct for opencode without an end
reason; the one loss is that a server that exits by itself (a crash, or a clean exit nobody
asked the keeper for) reads as `disconnected`, never `left`, which is the safe side.

## 4. What differs from a Claude Code member

| Concern              | Claude Code member (v2)                                           | opencode member (this design)                                               |
| -------------------- | ----------------------------------------------------------------- | --------------------------------------------------------------------------- |
| CMD                  | `claude ...`, or a wrapper with one `--`                          | `opencode serve`, the keeper appends host and port                          |
| Per-launch config    | `<v2-run>/settings.json`, `--plugin-dir`, `--settings`            | none written; the server password in CMD's environment only                 |
| Delivery channel     | inbox socket, token supplied by a hook                            | `prompt_async` over loopback HTTP, basic auth with the keeper's password    |
| Who supplies facts   | hook ops from descendants of the session (B 2.4.4)                | the link's own client reads them from the server                            |
| Proof                | needle search of the transcript JSONL (undocumented shape)        | needle in the session's message list (documented API)                       |
| Idle / busy          | Stop and UserPromptSubmit hooks                                   | `session.status` / `session.idle` events                                    |
| Frame text and skill | SessionStart hook and the `teamchat:teamchat` plugin skill        | `noReply` messages at creation and after compaction                         |
| Status line          | the kit's status line (B 2.9)                                     | none; `report.json` and the board only                                      |
| Resume               | `--resume <session_id>` from `conversation.json`                  | reuse the opencode session id recorded in `conversation.json`               |
| End reason           | SessionEnd hook records it                                        | none; the keeper's own judgement                                            |
| Guard rails          | Claude Code permissions plus the hooks daemon's PreToolUse guards | opencode's own `permission` rules only (defaults allow nearly everything)   |
| Reads `CLAUDE.md`    | yes                                                               | yes by default, and `.claude/skills/` too (`OPENCODE_DISABLE_CLAUDE_CODE*`) |
| Forging a delivery   | anything holding the inbox token (the session's own tools can)    | anything holding the server password, including the session's own bash tool |

The last row is the same exposure in both: the session can forge deliveries to itself, which
only deceives itself, and `teamchat verify` is the control (facts section 1). Host check
H-OC4 establishes whether opencode passes `OPENCODE_SERVER_PASSWORD` into its tools'
environment; if it does, a `shell.env` plugin hook can remove it, but that is a plugin the kit
would have to ship, so it is listed as open, not assumed.

The guard-rail row is the difference that matters most: an opencode worker in this repository
runs **without** the hooks daemon (no destructive-git, secret-read, sed or plan guards).
Section 6 and decisions O2 and O3 follow from it.

## 5. The member-adapter seam in v2

### 5.1 Should v2 define one?

Yes, narrowly, and **now**, because the Claude Code delivery slice (n3e: delivery into Claude
Code, the hooks, the status line, the skill) is not built yet. Writing it as the first
implementation of a small internal interface costs little; carving the seam out of a finished
n3e costs a refactor of the most delicate code in the member. `--plain` already is a second
launch mode with its own behaviour (no delivery), so v2 has two cases today and opencode would
be the third. The seam is **in-process Go only**: no external adapter executables, no plugin
loading, no new wire. An adapter carries bytes the link rendered; it decides nothing about
authority.

### 5.2 The shape

A package inside the member, e.g. `internal/teamchat/member/agent` (a Go package path is not
an owner-visible name; final placement is v2's call). The link calls it; the keeper consults
it only at launch.

```go
// Agent is the agent program a launch runs: session.pid's `launch` value. Exactly one
// per run (B 2.4.1 step 5). Not the member kind, which stays --kind ccy|lxc|host|other.
type Agent string // "claude", "opencode", "plain"

// Launch is what an adapter may add to CMD before the keeper starts it.
type Launch struct {
	Args  []string          // appended after CMD's last argument
	Env   map[string]string // added to CMD's environment only
	Files []RunFile         // written under <v2-run>, 0600, O_EXCL (Claude's settings.json)
}

// Prepare validates CMD for this agent (exit 64 on a CMD it cannot drive) and returns
// what the keeper adds. secrets is the keeper's per-run random material.
type Preparer interface {
	Prepare(cmd []string, inbound Inbound, secrets RunSecrets) (Launch, error)
}

// Channel is one live session as the link sees it. The link owns every state
// transition; a Channel only moves bytes and reports what it observed.
type Channel interface {
	// Ready blocks until the session can take a delivery, or the deadline.
	Ready(ctx context.Context) (SessionFacts, error)
	// Deliver writes one rendered envelope. Written reports how far it got,
	// for B 2.7.3's "unknown" and "yes".
	Deliver(ctx context.Context, envelope []byte) (Written, error)
	// Context adds text without starting a turn (frame text, notices).
	// ErrUnsupported where the agent has no such route (Claude uses hooks).
	Context(ctx context.Context, text []byte) error
	// Observations streams what the link needs to judge proof and the quiet
	// clock: proof of a nonce, idle, busy, compacted, delivery error, gone.
	Observations() <-chan Observation
	Close() error
}

type Capabilities struct {
	Delivery   DeliveryMode // push | wait | none   (B 2.7.1's modes)
	Proof      ProofKind    // transcript | api | none
	Facts      FactSource   // hooks | client      (who may set session facts)
	StatusLine bool
	EndReason  bool         // whether the agent reports one (Claude's SessionEnd)
}
```

A `Channel` runs in its own goroutines and only sends `Observation`s to the state goroutine,
which keeps every transition and makes no network call (B 2.4.3). `Delivery` is the agent's
best mode; the launch's `inbound: refuse` lowers it to `wait` for that run.

How each agent fills it:

- **claude**: `Prepare` is B 2.4.1 step 5 as written (settings file, `--plugin-dir`,
  `--settings`, the `claude` argument rule). Facts arrive from hook ops (B 2.4.4); the
  `Channel` is built once a hook has supplied the socket and transcript, `Deliver` is the
  inbox-socket write, `Observations` come from the transcript watcher and the hook ops,
  `Context` is `ErrUnsupported` (the hooks answer with context themselves).
  Capabilities: push, transcript, hooks, status line, end reason.
- **opencode**: section 3. Capabilities: push, api, client, no status line, no end reason.
- **plain**: `Prepare` adds nothing; no `Channel`. Capabilities: none, none. Exactly today's
  `--plain`.

What stays outside the seam, deliberately: envelope rendering (A 13), item states and the
ledger (B 2.7.3), receipts, the flood budget, the end and its notices (B 2.11), seats and
locks. An adapter cannot reach them, so a new agent cannot weaken them. The hook ops of
B 2.4.4 remain Claude-only and keep their trust rule; an agent whose `Facts` is `client` never
accepts them (a hook op on an opencode run is refused).

### 5.3 What the seam needs from v2's design text, and Gate A

- B 2.4.1 step 5 and B 2.7 are re-worded as "the Claude Code adapter"; the behaviour of a
  Claude Code launch does not change by a byte, which the n3e tests prove.
- `session.pid`'s `launch` field and `status`'s `delivery` value take the agent's values, and
  `conversation.json` (B 2.3.3) gains the same `launch` field, which every resume checks
  (section 3.1 step 4).
- A host check per pinned opencode version, as Claude Code has (H-OC1 to H-OC5, section 7).

**Gate A is untouched.** The seam and the opencode kind add no binary, unit, path, plugin,
skill, environment variable or profile with the `teamchat` name: per-run material lives in
the existing private `<v2-run>` and in memory, the only environment variable is upstream's
`OPENCODE_SERVER_PASSWORD`, and the frame and skill text are sent over the API rather than
placed as files. The one visible addition is the launch-mode argument of `teamchat run`
(O4), an argument spelling rather than one of Gate A's names, put to the owner with v2's
other names in any case.

## 6. Where an opencode worker runs

The play installs opencode for the login user on the host, for the owner's own use. A
**worker** is different: unattended, permissive by default, and outside the hooks daemon. Run
directly as the login user on the host, it can read and change everything that user can (the
vault password file, other checkouts, the owner's GitHub token). So the recommendation is to
run workers where v2 already isolates members:

- **an LXC guest** (v2 N3's LXC member kind, Plan 00209 Task 3.3, B 2.13), with opencode
  installed in the guest by the guest's own play; or
- **the ccy container**, which would put opencode into the ccy image: a ccy image change with a
  CCY version bump, after v2's kit is in the image (Plan 00209 N4, Task 4.1), which is itself
  held by Gate A.

A host member is v2's seam 56 (Plan 00209 Task 5.1, N5), refused until defined and held by
**Gate B** (a host member's seat is a pool seat). This is decision O2.

## 7. Host checks an implementation must pass (per pinned opencode version)

- **H-OC1** `opencode serve --hostname 127.0.0.1 --port <p>` with `OPENCODE_SERVER_PASSWORD`
  refuses every request without the password and answers `/global/health` with it; a `<p>`
  already in use ends `opencode serve` with a non-zero exit (the launch-failure rule needs it).
- **H-OC2** a `prompt_async` to a busy session stores the message at once and the session acts
  on it in the running turn or the next one; to an idle session, starts a turn.
- **H-OC3** the needle written in a delivery appears verbatim in `GET /session/:id/message`; a
  turn that fails after the message is stored (a provider error) emits `session.error` and the
  delivery is still proven, never redelivered.
- **H-OC4** whether `OPENCODE_SERVER_PASSWORD` reaches the bash tool's environment.
- **H-OC5** the managed `/etc/opencode/opencode.json` and a per-run permission policy are
  applied to a `serve` session (permission rules hold without a TUI to answer `ask`), and the
  worker's global config carries no `server` key, so no CORS origin is allowed.

## 8. Open decisions for the owner

Collected in PLAN.md under **Owner decisions** (O1-O7), each with the default this design
assumes.
