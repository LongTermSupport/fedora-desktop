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
