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

| Verb                                                                          | Does                                                                                                                                                                                   | Replaces in v1                         |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------- |
| `recv`                                                                        | prints every pending item and marks it delivered                                                                                                                                       | `recv`                                 |
| `wait [--timeout S]`                                                          | blocks until something is pending, then acts as `recv`; for sessions with no inbox socket                                                                                              | `wait`                                 |
| `inbox`                                                                       | lists what is pending without delivering it (no bodies)                                                                                                                                | `inbox`                                |
| `reply <event-id> [--verb V] [--ref R] [--to-coordinator]`                    | answers one item; text on stdin. The worker decides team, room, thread and addressee from the item. `--verb` answers a ping with a closed verb (with `--ref` where the verb takes one) | `say --re`, `send ack --re`, `thread`  |
| `say --to N[,N...]\|--to-coordinator [--team T]`                              | starts a conversation with named members; text on stdin                                                                                                                                | `say`                                  |
| `ping <verb> [<ref>] --to N[,N...]\|--to-coordinator [--team T] [--branch B]` | sends a closed-verb ping                                                                                                                                                               | `send`                                 |
| `report [--team T]`                                                           | a quiet report to the reports room; headline then body on stdin                                                                                                                        | `report` [Q 6]                         |
| `ask [--team T] [--asked-by N,...] [--re ID,...]`                             | the coordinator asks the team's humans a numbered question in the questions room; text on stdin                                                                                        | `ask-human` [Q 7]                      |
| `thread Q<n> [--ask] [--close] [--team T]`                                    | the coordinator posts in question `n`'s thread (quiet, or `--ask` to notify the humans, `--close` to close it)                                                                         | `thread` [Q 8]                         |
| `questions [--all] [--team T]`                                                | the coordinator's open questions and the human replies not yet answered                                                                                                                | `questions` [Q 8]                      |
| `history [--team T] [--room main\|questions] [--limit N]`                     | what this member received and sent, read from the room, newest first; consumes nothing                                                                                                 | `history`                              |
| `verify <event-id>`                                                           | re-fetches one event from the homeserver and prints its verified kind, sender, team, room and a SHA-256 of its body, so a consequential HUMAN request can be confirmed (B7)            | the skill's "check it in history" rule |
| `status [--team T]`                                                           | this session's teams, roles, members, side rooms, delivery state, the runtime's health and its own resident cost (B40)                                                                 | `status`, `config check`               |
| `version`                                                                     | the binary's source digest and the record version it prints                                                                                                                            | `version`                              |

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
