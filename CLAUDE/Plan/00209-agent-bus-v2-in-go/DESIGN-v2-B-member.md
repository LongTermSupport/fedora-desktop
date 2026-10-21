# Plan 00209 N0, area B: the v2 member client

This is area B of the v2 design (see `DESIGN-v2-FRAME.md`, which binds it). It covers everything
that runs in a member session: a ccy container, `cc` on the host, an LXC guest, docker or a VM. It
is designed from the requirements, not from v1. v1's Python is read here only for what it had to
do and what went wrong.

**Placeholders.** Area D owns every name and path. Until its names are merged this file uses:

| Placeholder    | What it is                                                                         | What it must be (for D)                                                                      |
| -------------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `<v2-bin>`     | the one static member binary                                                       | a command name no v1 kit puts on `PATH`                                                      |
| `<V2>_`        | the prefix of every environment variable the binary reads                          | distinct from `PINGBUS_`, so a v1 hook never sees a v2 session's variables, or the reverse   |
| `<v2-plugin>`  | the Claude Code plugin (hooks and skill)                                           | a plugin and skill name distinct from v1's `pingbus` plugin                                  |
| `<v2-state>`   | the persistent per-seat state root                                                 | survives session and container restarts; private to the member's uid; never committed        |
| `<v2-run>`     | the per-session runtime directory                                                  | private to one session (container-private `/tmp` in ccy, under `$XDG_RUNTIME_DIR` elsewhere) |
| `<v2-bundle>`  | where area C places a seat's bundle                                                | area C decides; B reads it                                                                   |
| `<v2-desired>` | the read-only directory holding the session's desired membership (written by host) | area C decides; mounted read-only into the member                                            |
| `<v2-unit>`    | the systemd unit of a supervised LXC session                                       | distinct from v1's `agent-bus-claude-<user>.service`                                         |

**Source keys** used in brackets: `R<n>` = DESIGN.md section 2; `P<n>` = SPEC-proto; `S<n>`, `L<n>`,
`F<n>` = SPEC-state; `M<n>` = SPEC-matrix; `H<n>`, `HB<n>` = SPEC-pingbus-hooks; `E<n>` = Plan
00194 SPEC-inject; `Q` = Plan 00211 SPEC; `HC` = Plan 00203 SPEC-hot-control; `MT` = Plan 00201
SPEC-many-teams; `RL x.y` = RESEARCH-lxc-robustness; `RP x.y` = RESEARCH-pingbus; `ST` = Plan
00213's status-line spec. "New" marks a requirement v1 did not have, with what it fixes.

---

## 1. Requirements

Numbered `B1`... Each says where it comes from. Where v1 did something in a way v2 drops, the
requirement states the need and section 5 states the drop.

### 1.1 One binary, one contract

- **B1.** One static binary, `CGO_ENABLED=0`, no interpreter and no shared library beyond the
  kernel's system calls, the same file in every member kind. It needs nothing in the guest but
  coreutils for its own installation \[DESIGN 8.4; RL 7.01; the coordinator: EL9 guests have
  Python 3.9 at `/usr/bin/python3`\].
- **B2.** Every verb an agent uses prints a versioned, typed record per line on stdout, a message
  body only ever as a JSON string, and exits with a code that has exactly one meaning. New: v1 had
  two line families (versioned and not) and exit codes shared by several meanings (3, 4, 7, 9,
  10\) [CLI digest E1-E6].
- **B3.** Every verb is argument-only and never prompts; text arrives on stdin [protocol §13].
- **B4.** A reply needs no addressing by the agent: the event it answers decides the team, room,
  thread and addressee. New: v1 made agents copy a `REPLY` command with a quoted event id, and
  retyping names was the known failure [CLI digest E9, E10].
- **B5.** Several teams in one session: no verb guesses a team. A verb names its team, or it acts
  on an event whose team is known, or exactly one team is active; otherwise usage error listing
  the teams with their descriptions [R20; RP 5.26; MT S-C8/C9].
- **B6.** The carried 00211 behaviour: quiet reports to the reports room (rate-limited, never
  waking anyone), human questions in the questions room as numbered threads (coordinator only),
  thread replies, the question list, history per room [Q 6-8].
- **B7.** An agent can check where a consequential human request came from with one verb that
  re-fetches the event from the homeserver. New: v1 asked the agent to scan `history` and compare
  decoded bodies by hand [E1 skill text].

### 1.2 State

- **B8.** One writer for every piece of member state. New: v1 had seven locks because six kinds of
  process wrote the same files [S, L1-L12].
- **B9.** The sync position advances only together with the items it brought, in one atomic
  write; a crash at any point loses nothing and repeats at most the delivery that was in flight
  [S25; RP 5.18; v1 needed an fsync ordering across files].
- **B10.** Strict decoders: exact key sets, version checked, integers never booleans or floats,
  duplicate keys refused, an error never quotes a value read from a file [S1-S5; SB1, SB6; F6].
- **B11.** The token is read by one process only (the worker), checked on every read (owner,
  mode 0600 or tighter, one printable line), never logged, never in argv. The Claude Code inbox
  socket token is never written to disk. New: v1 stored the socket token at rest in `socket.json`
  [R43; S41].
- **B12.** State that must survive a restart (sync position, outbox, delivery ledger, run record)
  lives in `<v2-state>`; state that belongs to one session lives in `<v2-run>` and dies with it.

### 1.3 Processes

- **B13.** Exactly two resident processes per session, in every member kind: the keeper and the
  worker. The frame decides this; the cost is measured and reported (B40) [frame; D15].
- **B14.** The keeper makes no network calls, holds the seat locks, follows the desired
  membership, supervises the worker and dies with its session. The worker holds everything that
  talks to the homeserver and to Claude Code [DESIGN 8.2; RP 5.29].
- **B15.** Death with the session is detected without PIDs that mean nothing across namespaces
  and without Go's thread hazard on `PR_SET_PDEATHSIG` [RP 4; P5.29; new: the Go hazard].
- **B16.** One supervised receiver, no chain of fallbacks. The owner rejected v1's chain
  (supervisor, hook revive, waiter) and asked for the root cause [RL 6.12; RP 5.10].
- **B17.** The receiver survives a homeserver restart, including at its first sync; it says so
  once at the start of an outage and once at its end; only a permanent failure (401, 403, lost
  trust, broken bundle) stops a team, and only that team [R24; E25; RP 5.10].

### 1.4 Delivery into Claude Code

- **B18.** Delivery through Claude Code's per-session inbox socket wakes an idle session and is
  queued by a busy one, with no keypress and no agent action [E; RP 4; U01 probe facts].
- **B19.** Kind and sender come from the trust rules applied to the event as the homeserver
  served it, never from text or from a file; a body can never change a header line \[R21; E1-E3;
  P74\].
- **B20.** At most one copy of an item reaches the agent through the socket in normal operation;
  after a crash a repeat is possible only for the one delivery in flight, carries the same item
  id, and is marked as a repeat. `recv` and the delivery loop never show one item twice \[R22;
  E10-E12\]. New: the repeat is marked, and a crash between write and record is resolved by the
  transcript instead of always repeating.
- **B21.** Proof of delivery by the transcript; a delivery not found is reported loudly and its
  items return to `recv`. New: v1 left a missing delivery consumed, reachable only through
  `history` [E16; delivery digest O2].
- **B22.** Read receipts for a human's message only after its delivery is proven; pending
  receipts survive a worker restart. New: v1 kept them in memory [R23; E16].
- **B23.** Pacing and caps keep each delivery its own entry under the socket's duplicate filter
  and rate bucket: at most one delivery every 2 s, a nonce in every delivery, at most 8 entries
  and 12 KiB per delivery, body cap 2048 rendered bytes, per-sender flood budget, humans never
  held back [E4, E8, E9, E13, E19].
- **B24.** Humans first: within a delivery, HUMAN entries come before PEER and PING entries. New:
  v1 ordered strictly by time, so a human waited behind agent chatter [delivery digest O6].

### 1.5 Hooks, status line, notices

- **B25.** Every hook answers one valid JSON line and exits 0, fast, from fixed templates filled
  only with counts and grammar-checked names; nothing from a file, an event or an error text
  reaches the model [R25; H6, H7, H14].
- **B26.** The Stop hook blocks only when `stop_hook_active` is literally false, once per distinct
  pending set, never while socket delivery is live, never when it cannot remember that it already
  blocked [R25; H59-H73; E17].
- **B27.** The status line always exits 0, wraps the session's own status line under v1's rules
  (process group, 10 s timeout, cancel, never runs itself), refreshes every 25 s, inside the
  hooks-daemon supervisor's 30 s sidecar freshness, and costs one exec and one file read for the
  bus segment, which has its own cache separate from the hooks-daemon line [R26; ST; Plan 00213].
- **B28.** Goodbye once per real session end, never on `/clear` or `/resume`, to humans and
  coordinators only, and not lost when Claude Code's SessionEnd budget is short [R28; RP 5.01].
- **B29.** Disconnected once per session that ended without a goodbye, in every member kind. New:
  v1 had it only for ccy seats, posted from the host's launcher [R29; RL 4].
- **B30.** Membership and role notices once per change [R30; HC H12-H13].

### 1.6 Teams, hot control, guests

- **B31.** Active teams = seats held ∩ seats desired; the desired membership is host-written and
  read-only to the member; the member never grants itself a team [R31; HC H4, H8, D2, D3].
- **B32.** Reconcile within 2 s of a change to the desired membership [R31; HC H9].
- **B33.** A removed team stops at once; nothing posted while it was out is delivered when it
  returns; a re-added team starts from a fresh sync that skips history [HC H23a; RP 5.25].
- **B34.** Hot control works the same way for LXC members as for ccy. New: v1 LXC membership was a
  container-writable env file applied by a play [RL 7.07; HC H-n].
- **B35.** A session launched on no team keeps inbound socket messages refused until its first
  join, which relaunches it once [R32; HC H22, H34].
- **B36.** Guest operations need only coreutils, util-linux and systemd in the guest, set their own
  umask and every leaf mode, open with no-follow and place with `O_EXCL`, and never run
  container-controlled code on the host [R33, R48; RL 6.02, 1.2].
- **B37.** A frozen and thawed member resumes as a reconnect, not as a failure. New: no source
  says what v1 did on thaw [multi-team digest 4].
- **B38.** One kit version per session from launch to exit [R49; D18].

### 1.7 Cost and tests

- **B39.** No polling loop where the kernel can wake the process; an idle member wakes only for
  the homeserver's long-poll, its presence lease and the inbox socket \[new; v1 ticked at 0.5 s and
  1 s\].
- **B40.** The resident cost per member (CPU, RSS, wakeups, file descriptors, connections) is
  measured by an acceptance and reported live by `status`. New: v1 never recorded RSS or wakeups
  [multi-team digest 5; Plan 00213 Task 1.1].
- **B41.** Every rule above is an expectation with a test through fakes: a fake homeserver, a fake
  Claude Code inbox socket and transcript, an injected clock, real processes for the locks and the
  supervision, crash points, a real Tuwunel behind a build tag and the VM lab for LXC \[DESIGN 11;
  M37-M44; src-go/CLAUDE.md\].

---

## 2. Design

### 2.1 The shape: who holds what

```text
  launcher (ccy entrypoint, cc --teams, LXC <v2-unit>, docker/VM wrapper)
    └─ <v2-bin> run [--desired DIR] -- claude <args>         (forks the keeper, then execs claude)
         │
         ├─ claude  (same pid as `run`; Claude Code; creates the inbox socket)
         │    ├─ hooks:        <v2-bin> hook <event>      ── RPC ──┐
         │    ├─ status line:  <v2-bin> status-line       ── reads <v2-run>/status.json
         │    └─ Bash tool:    <v2-bin> recv|reply|say|…  ── RPC ──┤
         │                                                         ▼
         └─ keeper  (<v2-bin> keeper; watches claude by pidfd)   worker.sock (0600, in <v2-run>)
              │  holds: keeper.lock, every seat lock               ▲
              │  reads: <v2-desired>/membership.json (inotify)     │
              │  keeps in memory: the inbox socket token           │
              └─ worker  (<v2-bin> worker; the keeper's child) ────┘
                   holds: worker.lock, every per-seat store, the bundle tokens
                   one goroutine per active team: /sync long-poll, sends, presence lease
                   one delivery goroutine: inbox socket, transcript proof, receipts
```

The keeper and the worker are the only resident processes. Everything an agent, a hook or the
status line runs is a short process that asks the worker over one Unix socket, or reads one file
the worker writes.

**Two designs were weighed for who owns the member state.**

1. *Shared files with cross-process locks* (v1, and DESIGN.md 8.2): `recv`, `wait`, `send`, the
   hooks and the receiver each read and write the per-team files, coordinated by seven `flock`s
   with an order to keep. It works without a running receiver. Its cost is everything SPEC-state
   records: seven locks, a delivery lock held across socket writes, an inbox that every reader
   must re-validate because any process could have written it, the recv/watcher race that needed
   a re-check under a lock (E12), lock-free readers that must tolerate half-written states (SB4),
   and the hooks re-parsing every pending file on every prompt (H28, P7).
2. *One owner* (chosen): the worker is the only process that reads a token, talks to the
   homeserver, writes member state or writes to the inbox socket. Every other verb is a typed
   request to it. There is no delivery lock and no outbox lock because there is one goroutine
   that owns the queue; there is no untrusted inbox cache because items arrive in the worker's
   memory straight from the homeserver; hooks cost one round trip, not a survey.

What (2) gives up is working with no worker. That case is a session whose worker is down, which
the keeper restarts at once (2.4), or a human with no session, who runs `<v2-bin> run -- bash`
to get the same runtime around a shell. There is no second code path that reads the store
directly: a verb with no worker to ask exits 69 and says why (2.2.4). That is the "no fallbacks"
rule applied to the member.

### 2.2 The binary and its command line

One binary, `<v2-bin>`, `CGO_ENABLED=0`, built by `tasks/build-go-command.yml` (which gains a
`CGO_ENABLED=0` input for it; today nothing sets it), `cmd/<v2-bin>/main.go` thin over
`internal/bus/member/...`. `--version` prints the source digest (Plan 00206), so a kit's version
is checkable from the binary itself.

#### 2.2.1 The verbs

Agent-facing (all go through the worker):

| Verb                                                                          | Does                                                                                                                                                                                                             | Replaces in v1                         |
| ----------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------- |
| `recv`                                                                        | prints every pending item and marks it delivered                                                                                                                                                                 | `recv`                                 |
| `wait [--timeout S]`                                                          | blocks until something is pending, then acts as `recv`; for sessions with no inbox socket                                                                                                                        | `wait`                                 |
| `inbox`                                                                       | lists what is pending without delivering it (no bodies)                                                                                                                                                          | `inbox`                                |
| `reply <event-id> [--verb V] [--ref R] [--to-coordinator]`                    | answers one item; text on stdin. The worker decides team, room, thread and addressee from the item. `--verb` answers a ping with a closed verb (with `--ref` where the verb takes one)                           | `say --re`, `send ack --re`, `thread`  |
| `say --to N[,N...]\|--to-coordinator [--team T]`                              | starts a conversation with named members; text on stdin                                                                                                                                                          | `say`                                  |
| `ping <verb> [<ref>] --to N[,N...]\|--to-coordinator [--team T] [--branch B]` | sends a closed-verb ping                                                                                                                                                                                         | `send`                                 |
| `report [--team T]`                                                           | a quiet report to the reports room; headline then body on stdin                                                                                                                                                  | `report` [Q 6]                         |
| `ask [--team T] [--asked-by N,...] [--re ID,...]`                             | the coordinator asks the team's humans a numbered question in the questions room; text on stdin                                                                                                                  | `ask-human` [Q 7]                      |
| `thread Q<n> [--ask] [--close] [--team T]`                                    | the coordinator posts in question `n`'s thread (quiet, or `--ask` to notify the humans, `--close` to close it)                                                                                                   | `thread` [Q 8]                         |
| `questions [--all] [--team T]`                                                | the coordinator's open questions and the human replies not yet answered                                                                                                                                          | `questions` [Q 8]                      |
| `history [--team T] [--room main\|questions] [--limit N]`                     | what this member received and sent, read from the room, newest first; consumes nothing                                                                                                                           | `history`                              |
| `verify <event-id>`                                                           | re-fetches one event from the homeserver and prints its verified kind, sender, team, room, full text and a SHA-256 of it, so a consequential HUMAN request can be confirmed (B7) and a truncated body read whole | the skill's "check it in history" rule |
| `status [--team T]`                                                           | this session's teams, roles, members, side rooms, delivery state, the runtime's health and its own resident cost (B40)                                                                                           | `status`, `config check`               |
| `version`                                                                     | the binary's source digest and the record version it prints                                                                                                                                                      | `version`                              |

Lifecycle (not for agents; the skill does not list them):

| Verb                                         | Run by                                       | Does                                                                                                                        |
| -------------------------------------------- | -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `run [--desired DIR] [--seat T/S...] -- CMD` | the launcher                                 | makes `<v2-run>`, forks the keeper, waits for its READY, sets `<V2>_RUNTIME`, execs CMD (2.4.1)                             |
| `keeper`                                     | `run` only                                   | the keeper (2.4.2); refuses to start unless its parent is the `run` it was forked by                                        |
| `worker`                                     | the keeper only                              | the worker (2.4.3); refuses unless fd 3 is the keeper's control socket                                                      |
| `hook <event>`                               | Claude Code, from the plugin's `hooks.json`  | one hook (2.8)                                                                                                              |
| `status-line`                                | Claude Code, from the session's settings     | the status line (2.9)                                                                                                       |
| `repair`                                     | the owner, or `ccy-teams session repair`     | starts a keeper for a session whose keeper died (2.4.5); never run automatically                                            |
| `reach --team T`                             | ccy-teams hot add, inside the member         | an unauthenticated `GET /_matrix/client/versions` to the team's homeserver (3 s), so a route is proven from the member side |
| `seat close-ended --seat T/S`                | the host launcher's cleanup; the next keeper | posts "disconnected" for a run of this seat that ended without a goodbye, once (2.11)                                       |
| `guest <op>`                                 | the host, through `lxc-attach`               | one guest operation (2.13)                                                                                                  |

`reply` is the change that matters most to agents. In v1 the agent had to copy a `REPLY` line or an
envelope's `reply:` line exactly, with the `$` event id single-quoted so the shell did not expand it,
and with the right addressee; retyping it was the failure the skill warned about [RP 5.27]. In v2 the
worker remembers every item it delivered (the ledger, 2.3.2), so `reply '$id'` is enough; the quotes
stay only because the shell needs them. An id the worker does not know (older than the ledger, or
from another session) is looked up on the homeserver; one it cannot find exits 23.

Why the verbs are renamed rather than kept: `send` versus `say` was the commonest confusion in the
skill text (one sends a closed verb, the other text); `ping` says what it sends. `ask-human` becomes
`ask` because only humans are asked in the questions room. The v1 names stay v1's; nothing aliases
them, since v2 is a separate system with its own skill.

#### 2.2.2 The output contract

Every agent-facing verb prints **JSON Lines** on stdout: one object per record, each with `"v": 2`
and a `"kind"`, keys only ever added within a version, text only as JSON strings. Two designs
were weighed: v1's tab-separated lines with a JSON-quoted text field, and JSON Lines. The tab
form is shorter to read but needs a parser per kind, cannot nest (questions, members), and
already had two families with and without a version field. JSON Lines is one parser, is what a
model reads most reliably, and makes a body that contains tabs, newlines or a forged marker
harmless by construction. The `kind` is the first key so a reader skimming the output sees it
first.

Records (all also carry `"v": 2`; `team` is always present, B5):

```json
{"kind":"human","team":"<team>","id":"$<43>","from":"alice","from_id":"@alice:<server>","sent_ms":1760000000000,"room":"main","text":"Please push the branch.","question":null}
{"kind":"peer","team":"<team>","id":"$<43>","from":"<repo>.2+<host>.podman","sent_ms":1760000000000,"text":"QA passed on my side.","copy_of":null}
{"kind":"ping","team":"<team>","id":"$<43>","from":"<repo>.2+<host>.podman","sent_ms":1760000000000,"verb":"review","ref":"pr:<owner>/<repo>#12@<sha40>","trust":"trusted","branch":"feat/x","re":null}
{"kind":"timeout","team":"<team>","id":"$<43>","silent":"<repo>.3+<host>.lxc","verb":"review","ref":"pr:<owner>/<repo>#12@<sha40>"}
{"kind":"sent","team":"<team>","id":"$<43>","room":"main"}
{"kind":"pending","team":"<team>","id":"$<43>","from":"alice","item":"human","sent_ms":1760000000000}
{"kind":"question","team":"<team>","n":4,"state":"open","id":"$<43>","asked_by":["<repo>.2+<host>.podman"],"unanswered":["$<43>"]}
{"kind":"history","team":"<team>","dir":"in","item":{"kind":"human","...":"..."}}
{"kind":"verified","team":"<team>","id":"$<43>","item":"human","from":"alice","room":"main","body_sha256":"<64 hex>","sent_ms":1760000000000}
{"kind":"team","team":"<team>","handle":"<repo>.2+<host>.podman","role":"worker","about":"Release work for the desktop","trust":"trusted","delivery":"socket","pending":0,"down":false}
{"kind":"member","team":"<team>","handle":"<repo>.1+<host>.podman","role":"coordinator","present":true}
{"kind":"room","team":"<team>","purpose":"questions","state":"joined"}
{"kind":"runtime","keeper":{"rss_kib":7400,"cpu_ms":120,"wakeups":31,"fds":9,"uptime_s":3600},"worker":{"rss_kib":14100,"cpu_ms":910,"wakeups":402,"fds":14,"conns":2,"restarts":0,"uptime_s":3590}}
```

`from` is the short form an agent can pass to `--to`: a human's localpart, or an agent's handle
when its short name is not unique in the team (the worker decides, P69). `from_id` is the full
Matrix user id. Area A owns which fields exist for each kind and what makes each kind trusted;
this table is the rendering, and area A's merge may add fields.

**Errors.** A failing verb prints nothing for the failed part on stdout and writes one JSON object
as the last line of stderr, after any human-readable diagnostics:

```json
{"v":2,"kind":"error","exit":12,"code":"no-room","team":"<team>","message":"team <team> has no questions room yet; it is created by the host when the team is applied"}
```

`code` is from a closed set per exit code (the table below), so an agent can branch on it without
reading prose. `message` is a fixed template filled with grammar-checked names only.

#### 2.2.3 Multi-team results

`recv`, `wait`, `inbox`, `history`, `status` and `questions` (without `--team`) run over every active
team. A team that fails produces one record on stdout, `{"v":2,"kind":"team-failed","team":"<t>", "exit":20,"code":"unreachable"}`, the other teams' records still print, and the verb exits 5. So an
agent never mistakes a partial answer for a whole one, which v1's "exit 0 if anything printed"
allowed [CLI digest E6].

#### 2.2.4 Exit codes

Each code has one meaning. v1's conflations (3 for both "nothing" and "timed out"; 4 for both a
refusal and a room that does not exist yet; 7 for both unreachable and not found; 9 for four kinds
of limit; 10 for three kinds of trust failure) are split.

| Exit | Meaning                                                             | `code` values                                                              | What the skill tells the agent to do                              |
| ---- | ------------------------------------------------------------------- | -------------------------------------------------------------------------- | ----------------------------------------------------------------- |
| 0    | done                                                                | -                                                                          | -                                                                 |
| 1    | internal error (a bug; never returned on purpose)                   | `internal`                                                                 | tell a human                                                      |
| 3    | nothing pending (`recv`, `inbox`)                                   | `nothing`                                                                  | carry on                                                          |
| 4    | `wait` timed out with nothing                                       | `timed-out`                                                                | run `wait` again if you still wait                                |
| 5    | a multi-team verb succeeded for some teams only                     | `partial`                                                                  | read the `team-failed` records                                    |
| 10   | refused by the protocol rules                                       | `size`, `schema`, `secret`, `target`, `verb`, `ref`, `re`, `body`, `empty` | fix the message; do not retry it unchanged                        |
| 11   | refused by role                                                     | `role`                                                                     | ask the coordinator instead                                       |
| 12   | the side room does not exist yet                                    | `no-room`                                                                  | use the main room; do not retry; do not tell a human              |
| 13   | a reference did not resolve at the forge, or its provenance failed  | `forge`, `provenance`                                                      | push first, or reference what exists                              |
| 14   | rate limited                                                        | `rate`, `server-rate`, `report-rate`                                       | wait; do not loop                                                 |
| 15   | the same message again within the duplicate window                  | `duplicate`                                                                | it was already sent; do not resend                                |
| 20   | the homeserver did not answer                                       | `unreachable`                                                              | tell a human if it persists; the worker keeps retrying on its own |
| 21   | the member's token was refused                                      | `token`                                                                    | tell a human                                                      |
| 22   | the room is not trusted, or the homeserver forbade the request      | `untrusted`, `forbidden`                                                   | run `status`; tell a human                                        |
| 23   | the event or question named does not exist                          | `not-found`                                                                | check the id                                                      |
| 64   | usage error                                                         | `usage`, `ambiguous-team`, `unknown-member`, `ambiguous-member`            | read the message, which lists the choices                         |
| 69   | no runtime: not in a member session, or its worker is not running   | `no-session`, `no-worker`                                                  | tell a human; do not start anything yourself                      |
| 75   | busy: the seat is held by another session, or a keeper already runs | `seat-held`, `keeper-running`                                              | (lifecycle verbs only)                                            |
| 78   | configuration refused (bundle, desired membership, store)           | `bundle`, `membership`, `store`, `forge-login`                             | run `status`; tell a human                                        |

`hook` and `status-line` always exit 0 (2.8, 2.9), except the status line's 128+signal on cancel,
which Claude Code expects.

### 2.3 Member state on disk

#### 2.3.1 Truth, cache and secret

| What                                        | Where                                                                              | Truth or cache                                                         | Written by             | Lifetime                  |
| ------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------- | ------------------------- |
| the seat's bundle (identity, token, server) | `<v2-bundle>` (area C)                                                             | truth, host-owned                                                      | area C's placement     | until the seat is removed |
| the desired membership                      | `<v2-desired>/membership.json` (area C), read-only to the member                   | truth, host-owned                                                      | the host               | the session               |
| the seat store                              | `<v2-state>/<team>/<seat>/store.json`                                              | truth for the sync position, pending ids, outbox, ledger, run record   | the worker only        | survives restarts         |
| the seat report                             | `<v2-state>/<team>/<seat>/report.json`                                             | a report for the host's views; never read back as authority            | the keeper             | rewritten on each change  |
| the session state                           | `<v2-run>/session.json`                                                            | truth for this session's stop guard, told generations, transcript path | the worker             | the session               |
| the status row                              | `<v2-run>/status.json`                                                             | a cache of the worker's state for the status line                      | the worker             | the session               |
| the worker socket, the locks                | `<v2-run>/worker.sock`, `keeper.lock`, `worker.lock`                               | -                                                                      | keeper, worker         | the session               |
| the worker's log                            | `<v2-run>/worker.log` (copied to `<v2-state>/<team>/<seat>/last.log` at clean end) | diagnostics                                                            | keeper (worker stderr) | the session               |
| the inbox socket token                      | the keeper's memory, passed to each worker on its control socket                   | secret                                                                 | -                      | never on disk             |

`<v2-state>` must survive a container restart, so for a ccy seat it sits beside the seat's bundle
in the checkout (area C places the bundle and proves the directory cannot be committed, R12), and
for LXC, `cc` and other members under the user's home. `<v2-run>` must be private to the session:
container-private `/tmp` in ccy, `$XDG_RUNTIME_DIR/<v2>/<run id>/` on a host or guest. Both are
0700, files 0600, owned by the member's uid; the worker refuses to start otherwise (exit 78).

**What sharing `<v2-state>` with sibling containers means.** In ccy, every container of one checkout
mounts the same checkout and runs as the same uid, so a sibling can read a seat's token and write
its store. v1 treated the shared inbox as an untrusted cache for this reason, but the token beside
it was readable just the same (SPEC-inject R4). v2 does not pretend otherwise: the store holds event
ids and positions, never content that is rendered; an item restored from the store after a crash is
re-fetched from the homeserver before it is delivered (2.7.3), so a planted id can only name a real
event, which the trust rules then judge. A sibling that edits the store can make a member skip or
repeat; it cannot make it deliver text the homeserver did not serve. Isolating sibling containers
from each other's tokens is area C's (separate uids or bundle mounts per container); B states the
need in its assumptions.

#### 2.3.2 The seat store

Two designs were weighed against a third:

1. *Files per item, as v1*: `inbox/<id>.json`, `consumed/`, `sync.json`, `outbox.json`, logs. The
   sync position and the items it brought are separate files, so crash safety depends on an fsync
   order across them (S25), and every reader lists a directory.
2. *An embedded database* (bbolt is pure Go and would vendor; SQLite is either CGO, which breaks
   B1, or modernc's large port): real transactions, but a new dependency on the allowlist, a file
   that cannot be read with `jq` when diagnosing, and transactions that one writer does not need.
3. *One snapshot document per seat, one writer* (chosen): the worker holds the seat's state in
   memory and persists the whole document with one atomic replace (temp file `O_EXCL|O_NOFOLLOW`
   in the same directory, write, `fsync`, `rename`, `fsync` of the directory) on every change that
   must survive a crash. With one writer, the atomic replace is the transaction: the sync position
   and the items it brought land together or not at all (B9).

The document is small by construction: ids, not bodies; capped lists. Writes happen per sync batch
that brought something, per delivery and per send: a few a minute at most. Its schema:

```json
{
  "v": 1,
  "team": "<team>",
  "seat": "<seat>",
  "user_id": "@<repo>.2+<host>.podman:<server>",
  "sync": {"next_batch": "<token>", "fresh": false},
  "pending": [
    {"id": "$<43>", "room": "main", "item": "human", "sent_ms": 1760000000000, "from": "@alice:<server>", "repeat": false}
  ],
  "inflight": null,
  "ledger": [
    {"id": "$<43>", "room": "main", "item": "human", "from": "@alice:<server>", "thread": null, "nonce": "3f9c2a71", "via": "socket", "at_ms": 1760000000500, "proof": "found"}
  ],
  "receipts": [{"id": "$<43>", "room": "main"}],
  "outbox": {
    "pings": [{"id": "$<43>", "verb": "review", "ref": "pr:<owner>/<repo>#12@<sha40>", "to": ["@<repo>.3+<host>.lxc:<server>"], "sent_ms": 1760000000000, "due_ms": 1760003600000, "answered": [], "reported": []}],
    "sending": [{"txn": "pb<32 hex>", "room": "main", "kind": "text", "since_ms": 1760000000000}],
    "gate": {"credit": 9.5, "at_ms": 1760000000000, "recent": [{"key": "<sha256>", "at_ms": 1760000000000}]}
  },
  "told": {"generation": 3, "role": "worker"},
  "run": {"id": "<16 hex>", "started_ms": 1760000000000, "ended": null}
}
```

- `inflight` is `{"nonce": "<8 hex>", "ids": [...], "transcript": {"path": "...", "offset": N}, "at_ms": N}`
  while a delivery is being written (2.7.3).
- `ledger` holds the last 2048 delivered items, oldest dropped first. It serves `reply` (which item
  is this id), de-duplication of a gap fill that overlaps what was already seen, and proof state.
- `sending` holds a send whose transaction id is chosen but whose answer has not arrived; a worker
  that restarts re-sends it with the same transaction id, which the homeserver stores once
  (M22; the durability of that across a homeserver restart is a risk, 4).
- `run.ended` is `null` while a session holds the seat; at the end it becomes `{"reason": "exit|logout|other|killed", "goodbye": "sent|attempted|none", "at_ms": N}` (2.11).
- Caps: 1 MiB for the file, 4096 pending ids, 256 outbox pings, 64 sends in flight; a cap reached is
  a loud failure of that team (exit 78 on the next verb, a status line note), never a silent drop.

The decoder is strict (B10). A store that does not decode stops that team with exit 78 and the
message names the file; nothing deletes or repairs it automatically, since a store the worker cannot
read may hold owed TIMEOUTs and pending items. `status` shows the failure; the owner's repair is to
move the file aside, which makes the next start a fresh sync (history skipped, B33).

#### 2.3.3 The session files

`session.json` (worker-written, tmpfs, strict):

```json
{"v": 1, "run": "<16 hex>", "session_id": "<claude session id>", "source": "startup", "transcript": "/<abs>/<id>.jsonl",
 "stop_guard": {"pending_digest": "<64 hex>|null", "failure_ms": null, "no_waker_ms": null},
 "told_generation": 3, "frame_told": true}
```

`status.json` is section 2.9's. `report.json` (keeper-written, for area C's views, never authority):

```json
{"v": 1, "run": "<16 hex>", "started_ms": 1760000000000, "seat": "<team>/<seat>", "handle": "<repo>.2+<host>.podman",
 "worker": "running|restarting|failed", "last_sync_ok_ms": 1760000000000, "pending": 0, "delivery": "socket|wait|none"}
```

### 2.4 The process model

#### 2.4.1 `run`: how a session starts

The launcher of every member kind ends in `<v2-bin> run --desired <v2-desired> -- claude <args>`.
`run`:

1. refuses an environment that carries v1's bus variables (`PINGBUS_HOME`, `PINGBUS_MEMBERSHIP`,
   `PINGBUS_TEAMS`), exit 78, so no session is on both systems (frame: no team mixed across v1 and
   v2);
2. draws a run id (16 hex from `crypto/rand`) and creates `<v2-run>` with `mkdir` 0700, refusing an
   existing path;
3. locks its main goroutine to the main OS thread (`runtime.LockOSThread` in `init`) and starts
   `<v2-bin> keeper --parent <own pid> --run <dir> --desired <dir>` through `execx`, in a new
   process group, with one extra file: the write end of a pipe for READY;
4. waits up to 30 s for `READY\n` on the pipe. Before writing it, the keeper has taken its lock,
   read the desired membership, taken every desired seat's lock, and started a worker that is
   listening on its socket. A desired seat held by another session fails the launch (exit 75,
   naming the seat), because launching into a held seat is the launcher's mistake; once the session
   runs, a refused seat is reported and the others carry on (2.10);
5. sets `<V2>_RUNTIME=<v2-run>` and execs CMD. Claude Code then has `run`'s pid.

The keeper learns of the session's end through a **pidfd** on that pid (`pidfd_open`, Linux 5.3, in
every kernel a member runs on: EL9's 5.14 and later), not through `PR_SET_PDEATHSIG`. Go starts
processes from whichever OS thread the goroutine runs on, and `PR_SET_PDEATHSIG` fires when that
*thread* ends, which in Go can happen while the parent process lives (golang/go#27505). A pidfd
names the process, survives the `exec`, is unaffected by threads, and is polled like any file
descriptor. The keeper opens it first thing and then checks that `getppid()` still equals
`--parent`, which closes the window in which the parent could have died before the pidfd existed.
The one pid written to a file is the session's, in `<v2-run>/session.pid`, for `repair` (2.4.5);
`<v2-run>` is private to the session's own namespace, and no pid is ever compared across one.

#### 2.4.2 The keeper

The keeper is a small event loop over five sources, each a goroutine blocked in the kernel, so an
idle keeper does not wake:

| Source                      | On an event                                                                                              |
| --------------------------- | -------------------------------------------------------------------------------------------------------- |
| pidfd of the session        | the session ended: end the worker (below), release, remove `<v2-run>`, exit 0                            |
| inotify on `<v2-desired>`   | re-read the desired membership; reconcile (2.10)                                                         |
| pidfd of the worker         | the worker exited: restart it, or mark it failed                                                         |
| the worker's control socket | the worker's state (for `report.json`), its requests for session facts                                   |
| signals                     | `SIGTERM`: as a session end; `SIGINT` and `SIGHUP`: ignored (a terminal's Ctrl-C belongs to Claude Code) |

It holds `keeper.lock` and the seat locks, writes `report.json` for each held seat, keeps the
inbox socket's path and token in memory (handed to it by the worker after a hook supplied them, and
handed back to each new worker at start), and makes no network call. Its only run-time failures
are reads of the desired membership, which it reports and does not act on.

**Supervising the worker.** The worker is the keeper's child, started with a `socketpair` as fd 3 for
control. On exit it is restarted after 1 s, doubling to 60 s, the backoff reset after 10 minutes
of running. An exit of 78 (the runtime's own configuration is refused: `<v2-run>` or `<v2-state>`
has the wrong owner or mode, a store does not decode) is final: no restart, `report.json` and
`status.json` say `failed`, the hooks say it once. Per-team failures never end the worker (2.6).

**When the session ends** (the pidfd fires, or `SIGTERM`): the keeper tells the worker `end` on the
control socket and waits up to 5 s for it to exit. In those 5 s the worker finishes any goodbye
(2.11), sets each lease to expired, persists every store, and exits. After 5 s the keeper kills it.
The keeper then records `run.ended` for a seat whose worker could not (`"reason": "killed"`),
releases the seat locks, removes `<v2-run>` and exits. Because this happens after Claude Code has
gone, it is outside Claude Code's SessionEnd hook budget (2.8).

#### 2.4.3 The worker

The worker runs, for each active team, one goroutine that owns that team's homeserver traffic (2.6),
and, for the session, one delivery goroutine (2.7), one RPC server (2.4.4), one transcript watcher
(2.7.4) and the control link to the keeper. All member state is owned by one **state goroutine**:
team goroutines hand it sync batches and send results, RPC handlers hand it requests, the delivery
goroutine asks it for the next delivery and reports writes. The state goroutine persists a store
before it answers anything that depends on the change. It never makes a network call, so a slow
homeserver cannot stall a hook; every network call is made by a team goroutine with the request's
own deadline.

#### 2.4.4 The worker socket

`<v2-run>/worker.sock`, a Unix stream socket, 0600 in the 0700 run directory; the worker checks the
peer's uid with `SO_PEERCRED` and closes any other. One request per connection:

```json
{"v":1,"op":"reply","kit":"<source digest>","args":{"id":"$<43>","verb":null},"stdin":"Done, pushed as <sha>."}
```

The worker answers with zero or more record lines, exactly as the verb prints them, then one final
line:

```json
{"done":{"exit":0}}
{"done":{"exit":12,"error":{"v":2,"kind":"error","exit":12,"code":"no-room","team":"<team>","message":"..."}}}
```

The CLI is a pass-through: it copies records to stdout, the error to stderr, and exits with `exit`.
So every verb has one implementation, inside the worker, tested in-process through the same request
type; the CLI process only frames the request. A request is at most 64 KiB; the worker refuses
anything larger with exit 10. A `kit` digest that differs from the worker's is refused with exit 78
(`kit changed under the session`), which B38's versioned kits make impossible but which would
otherwise be a silent mixed-version session.

Deadlines, set by the worker per operation: hook operations 300 ms; local reads (`recv`, `inbox`,
`status`) 2 s; sends 30 s (three tries with the same transaction id, M22); `history` and `questions`
60 s; `wait` its own timeout. A client that cannot connect exits 69 (`no-worker`); one with no
`<V2>_RUNTIME` exits 69 (`no-session`).

#### 2.4.5 Failure modes

| What fails                                 | What happens                                                                                                                                                                                                                                   |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| the worker crashes                         | the keeper restarts it with backoff; the new worker loads every store, resolves `inflight` by the transcript (2.7.3), re-sends `sending` with the same transaction ids, takes the inbox socket facts from the keeper; nothing waits for a hook |
| the keeper crashes                         | the kernel releases the seat locks; the worker sees the keeper's pidfd fire, persists, sets leases expired and exits; the session runs on without the bus; hooks say so once per window and the status line shows it; the owner runs `repair`  |
| Claude Code is killed (`SIGKILL`)          | as a session end, without a SessionEnd hook: no goodbye reason is known, so `run.ended.reason` is `killed` and the next start of the seat posts "disconnected" (2.11)                                                                          |
| the container or guest is stopped          | everything dies at once; the next start of each seat posts "disconnected"; for ccy the host's launcher cleanup runs `seat close-ended` sooner (2.11)                                                                                           |
| the homeserver restarts or is unreachable  | the team goroutine retries (2.6); the team is marked down after 15 s; nothing else is affected                                                                                                                                                 |
| a freeze and thaw, or a suspend and resume | the team goroutine sees its request deadline long overdue, or a gap between `CLOCK_BOOTTIME` and `CLOCK_MONOTONIC`: it drops the connection, re-verifies the room, renews its lease at once and logs one line with the pause's length (B37)    |
| the disk is full                           | a store write fails: that team stops taking new items (its sync position does not advance, so nothing is lost), the error is reported once, and the team goroutine retries the write with the transient backoff                                |
| `<v2-run>` is removed under a live session | the RPC server's listener fails: the worker exits 78 and the keeper reports it failed; a missing runtime is a bug or an operator's mistake, never repaired silently                                                                            |

`repair` (run by the owner, or by ccy-teams' `session repair`, inside the member's namespace): it
reads `<V2>_RUNTIME` and the session pid the keeper wrote into `<v2-run>/session.pid` at start, takes
`keeper.lock` (75 if a keeper holds it), and starts a keeper on that pid as `run` would. It is the
one deliberate path back; nothing calls it on its own.

### 2.5 Locks

Three kinds, all `flock(2)` on files opened `O_NOFOLLOW|O_CLOEXEC`, each acquisition on its own file
description (L11), released by `LOCK_UN` then `close` (L2), never a PID:

| Lock          | Path                                 | Held by                       | Guards                                                                                              |
| ------------- | ------------------------------------ | ----------------------------- | --------------------------------------------------------------------------------------------------- |
| `keeper.lock` | `<v2-run>/keeper.lock`               | the keeper                    | one keeper per session runtime                                                                      |
| seat lock     | `<v2-state>/<team>/<seat>/seat.lock` | the keeper, one per held seat | one session per seat, so one worker per store; presence for the host's views                        |
| `worker.lock` | `<v2-run>/worker.lock`               | the worker                    | one worker per runtime (guards against a stray process); probed by the status line for `(bus down)` |

What v1 needed seven locks for is now one goroutine's ordering: the sync lock (one syncer per team),
the delivery lock (an item shown by one of the watcher and `recv`), the outbox lock (the outbox's
read-modify-write), the watch lock (one deliverer per socket) and the told lock (a notice once) are
all properties of the worker owning its state alone.

- **Acquisition.** Every acquire is non-blocking (`LOCK_EX|LOCK_NB`), tried three times 50 ms apart
  where a probe may hold a momentary shared lock (L1, L3; `worker.lock` only, since only the status
  line probes). A held lock is a typed `Busy` error; nothing waits on a lock indefinitely.
- **Order, enforced by the API.** A `SeatLock` can only be obtained from a held `KeeperLock`
  (`func (k *KeeperLock) TakeSeat(team, seat string) (*SeatLock, error)`), so no code path holds a
  seat without being the session's one keeper. Since every acquire is non-blocking, two keepers
  reaching for overlapping seats cannot deadlock: one of them is refused.
- **The store has no lock.** Its single-writer rule rests on the seat lock: only the worker of the
  session whose keeper holds the seat writes it. The only other writer is `seat close-ended`, which
  takes the seat lock first, so it runs only when no session holds the seat.
- **Content.** After taking a seat lock the keeper writes `{"v":1,"run":"<id>","started_ms":N}` into
  it, for a human diagnosing a held seat. The lock, not the content, is the truth.
- **Proof.** Each rule is a test against a second real process (the test binary re-run through
  `cmdmaintest`), including a `SIGKILL`ed holder.

The host's own locks (the allocation and change locks, the play lock) are area C's and stay out of
any container's reach (R14; RL 6.09).

### 2.6 The sync engine (one goroutine per team)

Area A owns which events exist, what makes a room trusted, and every validation rule. This section is
how the worker applies them.

01. **Start.** Load the store. With no store (a new seat, or a team re-added after removal): verify
    the room (A's trust check), then sync with a timeline limit of 0, persist the position with
    `"fresh": false`, and skip history (B33; P5.18). With a store: verify the room, then sync from
    the stored position.
02. **Long-poll.** `GET /sync?since=<pos>&timeout=30000` with A's filter; the request deadline is
    45 s (M6). One kept-alive connection per team.
03. **A limited timeline** is filled forward from `prev_batch` with `/messages` (100 per page, at most
    20 pages); events already in the ledger are skipped. A gap larger than that is not an error that
    stops the team: the position moves on, and one `gap` item is queued for the agent ("N messages on
    `<team>` could not be fetched; read them with `<v2-bin> history --team <team>`"), so the loss is
    said, not hidden.
04. **Trust** is re-verified at start, after a join, on a limited timeline and on any event of a type A
    marks as trust-bearing. A failed verification stops the team with exit 22 for its verbs and one
    hook line; the worker does not drop its store, so the owner's fix resumes where it stopped.
05. **Each event** gets A's verdict: accept (queued as an item), ignore (counted in `status`), or drop
    (counted, one line in `worker.log` with the reason). An answer to one of the member's pings
    updates the outbox by its `origin_server_ts` against the deadline, even when read late (R42).
06. **Persist, then announce.** The state goroutine appends the batch's accepted items to `pending`
    and writes `next_batch` in the same store write, then wakes the delivery goroutine. A crash before
    the write re-fetches the batch from the old position; ids already in `pending` or the ledger are
    skipped.
07. **Due TIMEOUTs** come from the outbox on the state goroutine's timer (the earliest `due_ms`), not
    from a tick.
08. **Failures.** Unreachable, timeout, 5xx and 429 past its retries are transient: retry from the
    stored position after 1 s, doubling to 30 s, with jitter. The team is marked `down` once a failure
    has lasted 15 s, until a sync succeeds; `worker.log` gets one line at each end of the outage. A
    401, a 403 or lost trust is permanent for that team: it stops, its verbs exit 21 or 22, the hooks
    say so once, and the worker retries it only when it is next started. No team's failure touches
    another's (B17).
09. **Presence lease.** At start and every 5 minutes the team goroutine publishes A's presence state
    with a 10-minute expiry; at a clean end it publishes it expired.
10. **Side rooms.** A coordinator's filter includes the questions room; nobody syncs the reports room,
    which is send-only and verified at each send (Q 6).
11. **The forge.** A ping's reference is checked with the member's own `gh` login (R16): the worker
    runs `gh auth token --hostname github.com` through `execx` when a check needs it, holds the
    credential in memory for at most 10 minutes and drops it at once after a 401 (R44), and keeps
    positive results for 3600 s in memory. A missing or logged-out `gh` is a configuration failure of
    the forge only (exit 78, `forge-login`, on a verb that needs it).

### 2.7 Delivery into Claude Code

#### 2.7.1 The inbox socket

Claude Code exposes a per-session Unix socket to its hooks as `CLAUDE_CODE_MESSAGING_SOCKET`, with
`CLAUDE_CODE_MESSAGING_TOKEN` (U01). A message written to it starts a turn in an idle session and is
queued (`queued_command`) in a busy one. Every hook passes both values to the worker in its request
(2.8); the worker hands them to the keeper, which hands them to any later worker. They are never
written to disk (B11). The wire stays what Claude Code accepts: one connection per delivery, two
compact ASCII JSON lines (`{"type":"auth","token":...}` then `{"type":"user","message":{"role": "user","content":<envelope>}}`), half-close, read to EOF, 5 s per phase. A connect error `ENOENT`,
`ECONNREFUSED` or `ENOTSOCK` means the session's socket is gone: delivery stops (the keeper will see
the session end).

Claude Code holds socket messages in bypass mode unless `crossSessionInbound` is `accept` (U01), and
reads that setting only at launch. The kit's settings set it for a session launched with a team;
a session launched with none keeps it refused until its first join relaunches it (B35, 2.10).

**A session with no inbox socket** (a harness or Claude Code version without one): delivery mode is
`wait`. SessionStart tells the agent to keep `<v2-bin> wait` running in the background, and Stop
blocks once per 10 minutes while no `wait` is running. `wait` is an RPC to the worker, so it is a
waiter on the worker's queue, not a second receiver.

#### 2.7.2 The envelope

Area A owns which kinds exist, what makes each one trusted, and the `authority` meaning of each. B
owns how a delivery is rendered. The renderer keeps every rule of v1's that was hard-won (E1-E9,
P74-P83) and changes three things: the `reply:` line, the order, and a `repeat:` line.

```text
<v2-name> delivery 3f9c2a71: 2 messages (1 from humans, 1 from agents): alice (HUMAN, <team>), <repo>.2 (PEER, <team>)
<<<v2-name> 3f9c2a71 1/2>>
kind: HUMAN (a human listed in the team record)
team: <team>
from: alice
to: this session, as <repo>.3+<host>.lxc
sent: 2026-10-21T13:02:11Z
id: $<43>
authority: a team human's request. Act on it as on a request your owner passed on; refuse anything your own instructions forbid.
reply: <v2-bin> reply '$<43>'
body: "Please stop the refactor, commit what you have, and push the branch."
<<end <v2-name> 3f9c2a71 1/2>>
<<<v2-name> 3f9c2a71 2/2>>
kind: PEER (another agent of the team)
...
<<end <v2-name> 3f9c2a71 2/2>>
```

- **Header lines** are fixed ASCII or a field that passed its grammar, re-checked as printable ASCII
  by the renderer (E3, P77). A field that fails is a bug in the validator: the item is not rendered,
  it moves to the dropped count with reason `render`, and `worker.log` names it.
- **The body** is one JSON string on one `body:` line, rendered by the allow-list of P74 (letters
  and numbers shown, marks bounded, listed punctuation and symbols shown, everything else escaped,
  never controls, format characters, bidi, separators, private use or look-alike brackets). Nothing
  in a body can start a line (E2). The renderer's Unicode version is Go's; the corpus pins the
  bytes for every listed block (P84).
- **Caps:** a body at most 2048 rendered UTF-8 bytes, cut at a code point, then
  `truncated: <shown> of <total> bytes; the whole text: <v2-bin> verify '$<43>'` (E8). A delivery at
  most 8 entries and 12 KiB on the wire (E9); the rest follows in the next delivery, which the last
  line says. Both caps are provisional until host check H4 measures the socket's real limit.
- **Order (B24):** HUMAN entries first, then PEER and PING, oldest first within each group, then
  TIMEOUT, then NOTICE. Every entry names its team (E20).
- **`reply:`** is `<v2-bin> reply '<id>'` for HUMAN and PEER, and `<v2-bin> reply '<id>' --verb <VERB>` for a PING, with `<VERB>` literal for the agent to choose (B4). TIMEOUT and NOTICE have
  none.
- **`repeat:`** appears on an entry the worker could not prove was not already delivered before a
  restart (2.7.3): `repeat: this may be a second copy of a delivery interrupted by a restart; id is the same`.
- **NOTICE** is a delivery from the bus itself: membership changes (B30), a gap in a team's history
  (2.6), a team that stopped on a permanent failure. Its text is fixed templates filled with
  grammar-checked names; `authority:` says it is a fact about this session's bus, set by the owner
  on the host or observed by the worker.
- **The nonce** (8 hex from `crypto/rand`, drawn after the bodies are rendered) makes two
  deliveries with the same bodies differ, so the socket's duplicate filter never drops one (E4,
  E13), and is the transcript needle (2.7.4). It is not a secret.

#### 2.7.3 Delivered once: the item's states

```text
            sync batch (persisted with next_batch)
                    │
                    ▼
   ┌────────────► pending ──────────────── recv / wait ──────────► delivered(via recv)
   │                │ chosen for a delivery (persist inflight{nonce, ids, transcript offset})
   │                ▼
   │            inflight ── socket write fails (nothing written) ──► pending (retry in 10 s)
   │                │ full write (persist: ids to ledger, inflight cleared)
   │                ▼
   │            delivered(via socket, proof: waiting)
   │                │ needle found in the transcript          │ not found (180 s quiet, or 1 h)
   │                ▼                                          ▼
   │            proven ──► receipts (HUMAN)              missing ──► pending, held from the socket
   └──────────── worker restart with inflight set:                    (only recv / wait take it)
                 needle found → proven; not found → pending with repeat
```

- One goroutine owns every transition, so `recv` and the delivery loop can never both take an item:
  `recv` takes `pending` only; an item in flight is not pending.
- **Sent** means the payload was written in full; a slow or failed close after that still counts
  (E11), so a lost answer never causes a second copy with a new nonce.
- **The crash window** v1 could not close (written but not recorded, R3) is closed by the
  transcript: `inflight` records the transcript's size before the write, and a restarted worker
  scans from there for the nonce. Found: the delivery happened. Not found within the missing rule:
  the items go back to `pending` marked `repeat`, and the agent is told so in the entry.
- **Content.** An item that arrived over `/sync` in this worker's life is rendered from memory, as
  the homeserver served it. An item restored from the store after a restart has only its id there,
  so it is re-fetched (`GET /rooms/{room}/event/{id}`) and judged by A's rules again before it is
  rendered; one that no longer passes is dropped with its reason. That is the only re-fetch.
- **Pacing:** at most one delivery every 2 s; items that arrive meanwhile join the next (E13).
- **Stall:** if every write has failed for 120 s, the team rows show `(stalled)`, the hooks bring
  back pending counts and Stop blocks once per pending set (E15).
- **Flood budget:** an agent sender (PEER, PING) gets at most 10 entries per team per rolling 10
  minutes (E19). v2 does not consume the surplus: it stays `pending`, the delivery says
  `held back: <n> more from <sender> on <team> (flood limit); <v2-bin> recv shows them now`, and
  the items go out when the sender's budget refills. Humans and TIMEOUTs are never held. Nothing is
  consumed undelivered by a limit.
- **A removed team's items** are dropped from `pending` when the team is removed (B33), with one
  log line; they are never delivered later.

#### 2.7.4 Transcript proof

The SessionStart and UserPromptSubmit hooks pass `transcript_path`; it changes on `/clear`, and the
worker follows it. The transcript watcher holds an inotify watch on the transcript file and its
directory (for a new path or a rotation) and, on growth, scans only the new bytes, plus the needle's
length less one of overlap, for the needle `<v2-name> delivery <nonce>:` of every delivery waiting
for proof. The needle is ASCII with no character JSON escapes, so a byte search of the JSONL is
exact whichever way Claude Code records the message (a `user` entry with `origin.kind` `peer` when
idle; a `queued_command` attachment when busy; U01).

- Found: the delivery is `proven`; its HUMAN items get their read receipts.
- Not found when the transcript has been quiet for 180 s since the write, or 1 h has passed while
  it kept growing: `missing`. The items return to `pending`, held from the socket (pushing them again
  into a session that holds socket messages would only be held again), and the hooks say `a delivery did not reach the conversation; <n> items are pending again: run <v2-bin> recv` even while socket
  delivery otherwise works. `status` and the status line show `(missing)` until a later delivery is
  proven.
- A transcript that shrinks or moves restarts the scan at 0 of the new file.
- No socket delivery is made before a hook has supplied both the socket and a transcript path, so
  "no transcript" is not a case the rule has to guess at.

The scan depends on how Claude Code records socket messages, which is not a documented interface.
That is the price of the only proof there is (Claude Code acknowledges nothing on the socket). It is
pinned by a host check per Claude Code version (4, risks) and by the fake transcript's fixtures.

#### 2.7.5 Read receipts

When a delivery is proven, the state goroutine adds each HUMAN item's `{id, room}` to the store's
`receipts`, and the team goroutine sends `m.read` receipts for them, removing each once the
homeserver answers; transient failures retry with the team's backoff (E16, B22). Element then shows
the agent's avatar under a message that reached the model, and none under one that did not.
PEER, PING, TIMEOUT and NOTICE items get no receipt.

#### 2.7.6 The 00194 expectations, carried

| E   | v2                                                                                                |
| --- | ------------------------------------------------------------------------------------------------- |
| E1  | kept: kind and sender from A's verdict on the event as served (2.7.2, 2.7.3)                      |
| E2  | kept: one escaped body line, the P74 allow-list                                                   |
| E3  | kept: header grammar re-checked by the renderer                                                   |
| E4  | kept: nonce markers, drawn after the bodies; renamed to `<v2-name>`                               |
| E5  | kept: one fixed authority sentence per kind (A owns the meaning)                                  |
| E6  | changed: `reply:` is `<v2-bin> reply '<id>'` (B4)                                                 |
| E7  | kept: a short first line with counts and up to three senders                                      |
| E8  | kept: 2048-byte body cap; the pointer is `verify` instead of `history`                            |
| E9  | kept: 8 entries and 12 KiB, provisional until H4                                                  |
| E10 | replaced: one owner of the queue; re-fetch only for items restored after a restart                |
| E11 | kept: a full write is sent                                                                        |
| E12 | replaced: `recv` and delivery are transitions of one goroutine; no lock                           |
| E13 | kept: 2 s pacing, the nonce against the duplicate filter                                          |
| E14 | kept: the frame text at every SessionStart (2.8)                                                  |
| E15 | kept: 120 s stall, loud                                                                           |
| E16 | kept and improved: inotify instead of polling; missing items return to `recv`; receipts persisted |
| E17 | kept: hooks quiet while socket delivery works                                                     |
| E18 | replaced: per-session files live in `<v2-run>`, not a shared directory keyed by a hash            |
| E19 | changed: held-back items stay pending and go out later; nothing consumed by the limit             |
| E20 | kept, with B24: humans first, then by time; every entry names its team                            |
| E21 | kept: a `sent:` time out of range renders `-`                                                     |
| E22 | dropped: no relay mode (frame decision)                                                           |
| E23 | kept: one socket text per delivery, no count notice                                               |
| E24 | kept: the full set of verbs remains, as v2 verbs (2.2.1)                                          |
| E25 | kept: a homeserver restart drops nothing (2.6)                                                    |
| E26 | replaced: the keeper supervises the worker; no hook revives anything (B16)                        |

### 2.8 Hooks

The plugin's `hooks.json` runs `<v2-bin> hook <event>` directly for SessionStart, UserPromptSubmit,
Stop and SessionEnd. There is no `sh` guard: a hook with no `<V2>_RUNTIME` answers `{}` and exits 0
in a few milliseconds, which is what v1's guard bought by not starting Python. No PostToolUse hook
(it existed for relay mode).

**Every hook** reads stdin (at most 1 MiB; strict UTF-8; a JSON object, else treated as no payload),
sends one `hook` request to the worker with the event, the payload fields it uses (`session_id`,
`transcript_path`, `source`, `stop_hook_active`, `reason`) and the inbox socket variables, and prints
the worker's answer: one JSON line, `{}` or Claude Code's `hookSpecificOutput`, `decision`/`reason` or
`systemMessage` shapes (H6). The answer is built by the worker from fixed templates filled with
counts and grammar-checked names only (H14). The hook exits 0 always; a bad event argument is 64.

**When the worker cannot be reached** within 300 ms, the hook answers from its own fixed template
("the bus runtime is not running; this session is not receiving messages; tell a human") as
additional context on SessionStart and UserPromptSubmit, at most once per 10 minutes, remembered in
`<v2-run>/no-worker.json`, the one file a hook writes, and only in this case. Stop never blocks on
it: an agent cannot repair the runtime, and blocking would trap it.

| Event            | What the worker does and answers                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| SessionStart     | records `session_id`, `source` and the transcript path; takes the socket; answers the frame text (how deliveries look, the kinds and their authority, `reply`, `verify` before a consequential HUMAN request, never carry one team's messages to another), each team's seat, role and description, the delivery mode, and any pending or failure line. On every source (startup, resume, clear, compact), since after a clear or compaction the earlier frame may be gone from context |
| UserPromptSubmit | updates the socket and transcript path; answers membership lines only if the MEMBERSHIP notice could not be written to the socket (B30's fallback, told once by generation), pending counts only when delivery is not live or items are held as missing, and a failure line once per 10 minutes                                                                                                                                                                                        |
| Stop             | B26: nothing unless `stop_hook_active` is literally `false`. Blocks once per distinct pending set when some pending items will not be pushed (delivery not live, or held as missing), with `<n> pending (...). Run <v2-bin> recv before stopping.`; once per 10 minutes for a team failure; once per 10 minutes for a socketless session with no `wait` running. If `session.json` cannot be written, it does not block (H59-H73)                                                      |
| SessionEnd       | `reason` `clear` or `resume`: `{}`, nothing else (the session goes on). Otherwise records the reason (`exit`, `logout`, `other`) as the run's end and answers `{}` at once. The goodbye is posted by the worker after the hook has returned, inside the keeper's 5 s end (2.4.2, 2.11)                                                                                                                                                                                                 |

The SessionEnd hook therefore does almost nothing within Claude Code's 1.5 s SessionEnd bound, and
the `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS=8000` lift v1 needed (P5.01) is not set by v2's kit.

### 2.9 The status line

The kit's settings give `{"statusLine": {"type": "command", "command": "<v2-bin> status-line", "refreshInterval": 25}}`. 25 s keeps the session's own status line, which `<v2-bin>` wraps, inside
the hooks-daemon supervisor's 30 s sidecar freshness (Plan 00213 Task 1.3); a test pins the kit's
value at 25 and below 30.

1. **The inner line.** As v1 (R26, ST): the session's own status-line command (looked up in the
   project's `settings.local.json`, then `settings.json`, then the user's, skipping `<v2-bin> status-line` itself) runs through `/bin/sh -c` with the payload on stdin, in its own process
   group, `<V2>_STATUS_LINE_INNER=1` set so it can never run `<v2-bin>` again, killed whole after
   10 s or when the status line is cancelled (`SIGTERM`, `SIGINT`, `SIGHUP`: exit 128+signal, nothing
   on stdout, even if the signal lands before the child's pid is known). It is never cached: it
   writes the supervisor's context sidecar.
2. **The bus segment.** One read of `<v2-run>/status.json` (at most 64 KiB, strict) and one probe of
   `worker.lock`. Rows `👥 <team> (<role>)` with `(down)`, `(stalled)`, `(missing)` or `(failed)`
   marks, wrapped at `COLUMNS` after a comma, as v1. A free `worker.lock` replaces the rows with a red
   `bus runtime not running`. No teams: no segment (empty stays empty).
3. **Exit 0** always, except the cancel above.

`status.json` is written by the worker's state goroutine whenever anything it shows changes, at
most once a second:

```json
{"v": 1, "run": "<16 hex>", "written_ms": 1760000000000, "worker": "running",
 "teams": [{"team": "<team>", "role": "coordinator", "down": false, "failed": null, "delivery": "socket", "pending": 0, "missing": 0}]}
```

This file is the bus segment's cache, separate from the hooks-daemon line, as Plan 00213 requires;
it needs no TTL and no fingerprints because it is pushed on change instead of recomputed on render.
The bus part of a render is one exec of a static binary, one 1-2 KiB read and one `flock` probe. Its
CPU per render is measured by the acceptance of B40 and reported beside the v1 figure (0.32 CPU-s
per render).
