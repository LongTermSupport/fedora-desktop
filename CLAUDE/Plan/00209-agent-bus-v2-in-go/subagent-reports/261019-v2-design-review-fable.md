# Plan 00209 Task 1.3: adversarial review of the v2 design (Fable)

Reviewed at `feat/agent-bus-v2-design` 07625931: `DESIGN.md`, `PLAN.md`, the four
`RESEARCH-*.md`, the designer's report, against v1 (`helpers/agent_bus`, `helpers/pingbus`,
`helpers/agent_bus_lxc`, the plays, the kit, `docs/agent-bus*.md`, Plans 00161/00177/00194/
00196/00197/00201/00203). Nothing changed. Where a claim below is checked against code the
file is named; the rest is judged from the digests, which I found accurate wherever I probed.

## Verdict

**REWORK** (one round). The requirements catalogue and the research are strong and the staged
coexistence is sound in outline, but three things need redoing before the owner decides:
D1 is not framed neutrally (the strongest no-daemon option is missing and the stated
disqualifier of option (c) is false), the member-side lock simplification in sections 7 and
8.2 contradicts the "recv/send/say stay" promise and would reintroduce the double-delivery
race v1 fixed, and the daemon's socket exposure widens who can reach the holder of every
admin token (R38). Several hard-won behaviours are also missing from R1-R39, and stage
ordering ships the two owner-visible wins later than it needs to.

## Summary

The design is a genuine re-architecture of v1's *process model* (in-process reads, one
resident keeper+worker per session, a daemon owning the admin session) while keeping v1's
*data model, CLI contract and wire protocol* intact for coexistence. That is the right shape for
a side-by-side migration and the requirement catalogue (R1-R39) is the best single statement
of what the bus does that the repo has. But the daemon is justified more by the seat broker
(deferred to its own plan, D11) and by "one writer" (which the registry flock already gives,
and which the daemon itself breaks during stage 5) than by the latency target, which the
research's own path reaches without it; D1 omits `sudo -u agent-bus` with a Go reader, which
removes root from the read path with no daemon, no new unit and no reachable parser. The
pingbus v2 section drops cross-process locks that separate processes (`recv`, `send`, `say`,
hooks) still need, which is exactly the class of bug v1 found most of. The security section
should treat a world-connectable socket into a process holding every team's admin token as a
widening, not a narrowing, and fix it with file modes before peer checks. The requirements miss
outbox/ack/TIMEOUT semantics, member-side token handling, conversation recording, the Element
profile play, LXC `service: true` units, and the ccy image bump that stages 6-7 imply. With those
fixed, and stage 6a (hooks and status line) and a stage-3-lite pulled forward, this is ready
for the owner.

## Findings, ranked

### 1. D1 is not neutral: the no-daemon option that removes root is missing, and (c) is misdescribed — REWORK

- D1(c) is dismissed as "keeps a NOPASSWD rule and root on the path". Root is on v1's read
  path only because the bash wrapper needs `runuser` to drop to `agent-bus`
  (`files/usr/local/bin/agent-bus`, RESEARCH-host 1.2). A sudoers line
  `<owner> ALL=(agent-bus) NOPASSWD: /usr/local/bin/agent-bus view` runs a Go binary **as
  `agent-bus` directly**: no root at any point, one argv shape, parallel HTTP to every
  homeserver (RESEARCH-go-callers 5 step 3: about 0.1 s for T+H reads), and the 1 s target is
  met (the research says so itself, step 5: "one batched sudo call is probably enough").
  This option (call it (c')) must be in the table.
- The design's three reasons for the daemon (3.3) do not hold up as stated: (1) "live read
  model": nothing on the board needs sub-second freshness; a redraw already re-reads. (2) "one
  writer": v1 serialises every registry change under `registry.lock` (RESEARCH-host 7.09); a
  flock across CLI processes *is* single-writer discipline, and section 7 admits the daemon
  shares that lock with the Python tool through stage 5, so it is not the single writer during
  the very stages it is introduced. (3) "the seat broker's home": D11 defers the broker to its
  own plan; building its host now is speculative (YAGNI).
- The operational cost of a new long-running service is not weighed in D1: a new unit and
  sandbox to maintain, a new failure mode in which the board cannot show members at all (B6,
  `--list` exits non-zero), freshness semantics (`observed_at`, stale marks) the board must
  render, a version skew check (13), inotify on the registry during coexistence, an audit log,
  two sockets, `/etc/agent-bus/owner`, and the security surface of finding 3. Each is small;
  together they are the bulk of stage 4-5's new code, for a read that (c') does in ~0.1 s.
- Recommendation to put before the owner: (c') as stage 4, daemon deferred to the broker plan
  (where its approval flow is the reason to exist). If the owner still wants the daemon,
  D1's "why" must be the broker, said plainly, not latency or single-writer.

### 2. Sections 7 and 8.2 drop cross-process locks that cross-process callers still need — REWORK

8.2 says v2 "replaces the spool, watch, keeper, told and outbox locks with ownership inside one
process tree: the keeper owns the session's state and the worker is its only child that writes
it". 8.1 and 8.2 also say `recv`, `wait`, `history`, `send`, `say` and the hooks **stay as
commands** — each a separate process started by the agent or by Claude Code, not a child of the
keeper. In v1:

- `recv` and `wait` consume inbox items (rename to `consumed/`) and share the spool lock with the
  watcher so an item is shown by exactly one of them (`helpers/pingbus/cli.py:1233-1238`,
  SPEC-inject E12; RecvRelayRaceTest is the deterministic race). Dropping that lock while
  `recv` remains a separate process reintroduces double delivery (breaks R22).
- `send`/`say` read-modify-write `outbox.json` under a flock on the state directory while the
  worker records answers and deliverers record `reported` (`helpers/pingbus/inbox.py:18,564-571`).
- Hooks (`prompt`) and the worker both emit membership notices gated by `told.json` under
  `told.lock` (RESEARCH-pingbus 4).

Either (a) `recv`/`send`/`say`/hooks become thin clients of the keeper's socket (then 8.2 must
say so, and a session with no keeper, such as a human running `pingbus recv` by hand on an LXC
member, needs the lock path anyway), or (b) the delivery, outbox and told locks are kept as
cross-process flocks named for what they are (P 6's actual recommendation: "name the lock for
what it is"). Also: for LXC/host members `PINGBUS_HOME` is `~/.config/pingbus`, shared by every
session of that user, so a v1 and a v2 session of one user coexist on one `state/` during
stage 6-7; the locks must stay v1-compatible there. Section 7's claim "seven locks → two" is
wrong as written; this is the defect class the design itself names as v1's most frequent (15).

### 3. The daemon widens who can reach the admin tokens; fix with modes, not only peer checks — REWORK

- Today the admin token is touched only by a short-lived process reached through sudo's
  argv filter. v2 puts **every team's** admin token in one resident process with two sockets
  the design makes `0666` in a `0755` directory (5.3). Any local uid, including the ccy
  `cc` session's agent (owner uid) and any other account on the host, can connect and send
  arbitrary bytes to a JSON parser in that process before the peer check refuses it. 6.3
  argues "a bug in the parser runs as `agent-bus`, never as root" — but `agent-bus` holding
  all admin tokens *is* the capability R38 names ("can instruct every agent"). That is a
  widening of the reachable surface, and the design presents it as a narrowing.
- Cheap fix: `admin.sock` `0600 agent-bus:agent-bus` (root bypasses DAC, so `sudo agent-bus`
  connects and nobody else can); `view.sock` `0660` with group = the owner's group, or in a
  `0750` directory with an ACL for the owner uid; peer check kept as defence in depth. Then
  nothing but root and the owner can deliver a byte. Also: the view answer carries
  `m.room.member` content (display names, membership) that *members* control; the daemon is the
  first admin-token holder that reads room events at all, so `bus/view` must grammar-check
  every field it forwards and the design should say so (R39 extends to room events).
- Sandbox parity: the daemon needs the homeserver's `PrivatePIDs`/`ProtectProc`/no-new-privs
  posture (15 says "as the homeserver's"; make it a requirement, since the homeservers share
  its uid and the per-team `TemporaryFileSystem` exists precisely so one uid cannot reach a
  sibling's secrets — the daemon is a sibling that holds all of them).
- Correct and worth keeping: `SO_PEERCRED` reports uid 0 for a `sudo`-started CLI (H3 is the
  right check); `/etc/agent-bus/owner` beside `stamp-group`; the password remains the presence
  check for changes (D3); the bundle still placed by the user by fd (R12).
- A compromised member container: unchanged from v1 (it holds its token and can write its
  checkout, including sibling seats' `state/` and `member.json`). Note that `bus/seat`'s
  in-process read of `member.json` + `seat.lock` feeds the board's seat rows from
  container-writable data; v1 has the same exposure, but the view request gives v2 an
  authoritative roster to cross-check handles against — say that the board does so.

### 4. Requirements R1-R39 miss hard-won behaviours the stage specs would then miss — REWORK

Each below is in the research and in v1 but has no R-number; a stage spec written from
section 2 would not know to carry it:

1. **Outbox, acks and TIMEOUT**: a ping expecting an answer gets one TIMEOUT per silent target
   (`<eid>~<localpart>`, P 5.14); ack/nack counted by `origin_server_ts` against the deadline
   even if read late or dropped as stale (P 5.15); the send gate charged before the network
   call and the 60 s duplicate window persisted (P 5.16). R21 only names TIMEOUT as a kind.
2. **Member-side token handling**: token re-read and re-checked (owner uid, mode ≤0600, one
   printable line) on every use, never held on the member object; two bundles sharing a
   user_id or token refused (78); `say` refuses text containing the member's own token; the
   session's `socket.json` holds the inbox socket token at rest 0600 (P 2a, 2c). R11 covers the
   host side only.
3. **Conversation recording**: the SessionStart hook writes `conversation.json`
   (`{v,session,session_id,source,written_ms}`) that `ccy-teams restart` resumes by
   (`--resume <recorded id>`, never `--continue`, RESEARCH-lxc 3.5); `relaunch.json` is the
   supervisor restart request. 5.2 lists `conversation.json` as a kept file but no requirement
   says who writes it or why; `relaunch.json` is absent from 5.2.
4. **Element**: `play-agent-bus-element.yml` runs `helpers.agent_bus.element check|apply`
   (profile keys, keyring grant, spellcheck seed, H 7.37); the `host`-type refusal when the user
   has an Element profile and the `element-accepted` consent (D26, P 2a). Stage 8 deletes
   `helpers/agent_bus` and takes `element.py` with it; nothing in 3.1 replaces it. R37 states the
   invariant but not the mechanism.
5. **LXC** (R33 is thin): `service: true` supervised session unit, its attach command and
   restart-only-when-text-changed rule (L 1.7); pre-checks 4-5 (gh logged in as the agent user,
   source address on a bridge inside `allow_from`); absent phase runs before team removal;
   `agent_bus_only_teams` scoping of the record sweep; the member record's `since` semantics.
6. **Admin bootstrap rules** cited only as "[H 3]": exactly one server admin asserted; admin
   password stored before register and deleted after the token is stored (recovery path);
   `removed_humans` can never return; human removal before the record rewrite; stray
   `agent_bus.status` emptied; `description` reaches members via `m.room.topic`, never a record
   key (an unknown key makes older members distrust the room, MT S-A8 — this one is a protocol
   invariant and must be an R).
7. **Status line** (R26): kill the inner command's process group on timeout or cancel including
   the Popen-start race; never call itself (`INNER_VAR`); wrap at `COLUMNS`; `(role?)` mark;
   the `down/<team>` mark honoured 300 s (P 5.19, `statusline.py:187-224`).
8. **Forge in a long-running worker**: `gh auth token` is a subprocess per process today; the
   worker must decide how often it re-reads the credential and keep the 3600 s repo-metadata
   TTL ("a repository moved out of the org must stop being trusted", P 3). Not mentioned in 9.
9. **Hooks**: SIGINT/SIGHUP ignored while posting the goodbye; the 4 s parallel post budget;
   `stop_hook_active` respected; the fixed `inject_frame` explanation at SessionStart (P 4).

Present and correctly stated (checked): goodbye/disconnected (R28-29, never both, once per
claim, dead-tty tolerance), envelope nonce and flood budget (8.3 by reference to E1-E26),
seats and derivation (R9, R33), one coordinator with demote-before-promote (R10), stopped LXC
tolerance (R33), play lock (R34), membership notices (R30), inbox as untrusted cache (R18).

### 5. The ccy image bump and the kit are not in the migration — REWORK (small)

The pingbus zipapp is baked into the ccy image (`files/var/local/claude-yolo/Dockerfile:398-407`)
and `helpers/agent_bus` rides along (`helpers/agent_bus_lxc/applied_record.py:6-7`). Stage 6
therefore needs a Dockerfile change and the mandatory CCY version bump, and "a running session
keeps v1 until it restarts" is really "until its container is rebuilt from the new image".
Stage 7 changes the LXC kit sync (Python strings → `pingbus guest`) and the host kit under
`/usr/local/share/agent-bus/kit`. Neither is in 10.2, 10.3 or the risks.

### 6. Stage ordering ships the owner's two pains late — RECOMMEND

- The open's cost is pain one; the per-session Python every second and the goodbye budget are
  pain two (section 1 says "every running session pays it too"). Hooks and status line in Go
  reading v1 files (stage 6's first half) depend on nothing host-side and could follow stage 3
  directly as **stage 6a**; today it sits behind the daemon and the admin CLI.
- Stage 3 depends on stage 2 only for the stamp's software digest (B4). A **stage 3-lite** that
  keeps exactly one `declare overview` Python call for the APPLIED verdict and moves the other
  six declare calls and the C seat lists in-process saves ~6 s of the ~6.5 s with no
  dependency on the installer merge (which is the riskiest stage: root, restarts homeservers,
  awaiting a host test). The digest change then lands with stage 2 whenever it is accepted.
- Stage 1 (proto/state packages plus corpora) ships no value and is large; split it so each
  later stage pulls in only the grammars it needs, or accept it as the price of the corpora.

### 7. Latency claims: consistent with the research, but the headline is unmeasured — ACCEPT WITH NOTE

- Stage 3 "about 6 s": 7 declare starts (~3.7 s incl. the 183 ms zipapp rebuild ×3) + 6 seat
  lists (~2.8 s) = ~6.5 s. Stage 4 "about 1 s": removes `sudo list` ×3 (~2 s) and `human devices`
  ×6 (~3.5 s) from a ~12 s sum. Both follow from RESEARCH-go-callers 5; the per-start figure is
  measured (0.36-0.42 s in a container, best of 5), the counts are read from code, the host
  figure is inferred (zipimport writes no `.pyc`). The 9-23 s headline is nowhere recorded
  (G 4), so H1 must run **before stage 3** or the claim cannot be checked; PLAN.md Task 4.1 does
  say "H1 before", good. The redraw figure (10 starts, ~4.5 s) becomes ~2 s after stage 3 and
  ~0 after stage 4; say so in 10.2's "what the owner sees".
- With (c') from finding 1 the same ~1 s is reached: sudo+PAM ~50-100 ms, parallel HTTP ~0.1 s.

### 8. The status line fix is right in kind, incomplete in scope — ACCEPT WITH CHANGES

- Replacing a Python start per second (plus a lazy import of config/inbox when teams exist)
  with a Go binary that reads the keeper's state file is right and sufficient; a per-second
  Go `exec` is ~5 ms. Having the worker write the rendered row so the status line is a file
  read plus the inner command is simpler still, and avoids the status line re-deriving
  "active teams" (which v1 duplicated to save import time, P 6).
- The dominant remaining cost is the **inner** status line it wraps (`/bin/sh -c <command>`,
  10 s timeout, own process group); unchanged by v2 and not the bus's to fix, but the design
  should say the per-second budget it is aiming at and that `refreshInterval: 1` is the kit's
  own choice (`files/opt/claude-yolo/optional/agent-bus/settings.json`) that could be 2-5 s.
- Keep the whole of P 5.19 as expectations (finding 4.7). Keep the fast path requirement
  (status line must not load the member/sync code) even in Go: a Go binary that opens the
  inbox to count pending every second is the same mistake in a faster language.

### 9. Migration claim "formats and protocol kept" is true, but two coexistence cases are untested — RECOMMEND

- True: every v1 file keeps path, owner, mode, schema and version (5.2); the writer writes the
  oldest live reader's version (5.1); protocol 1 unchanged (D5). The shared corpus (5.4) is a
  good mechanism and the right rule ("never by recording one implementation's output").
- Missing tests: (a) stage 4-5: the Python admin tool and the daemon on one registry under one
  flock, with the daemon's inotify re-read (a second real process, as 11 requires for locks);
  (b) stage 6: a **host** v2 `pingbus seat disconnected` reading a **v1** session's `seat.lock`
  claim and `said-goodbye.json`, and the reverse; (c) stage 4: the admin token used by two
  clients at once (daemon `/sync` and the Python tool's calls) — Tuwunel keeps sync tokens
  per user, so this should be fine, but H4 should confirm nothing in the Python tool breaks.
- Rollback per stage is sound. Stage 4's "sudoers rule stays until accepted" is right. Stage 5
  rollback relies on the registry version staying 2 — it is in 5.1; make H-check of it explicit.
- Legacy forms (D9): "read until stage 8, never written" conflicts with "a parked `orchestrator`
  role" possibly present in a live registry — the one-off migration must run before the Go
  decoder refuses the form, and the sequencing should be stated.

### 10. Redesign or port? — ASSESSMENT

It is a new process model on v1's data model. New: in-process reads, keeper+worker replacing
six process kinds, no root on the read path, a typed bundle hand-over instead of tar in bash,
one lock for the installer, static binary in members, one transport. Carried unchanged, and
rightly for coexistence: every file format, the CLI contract, the protocol, the file-per-event
inbox with full re-validation (P 6 notes this is a consequence of "no daemon"; a resident worker
could hold the inbox in memory but coexistence forbids changing the files), three receive
drivers (`recv`/`wait`/`watch`→worker), the host_vars as the declaration store (D6), the
play-driven LXC model (D7). Package layout mirrors v1 modules one-to-one (proto=protocol.py,
state=inbox.py, matrix=matrix.py, forge, admin, seat=checkout.py, declare, lxc=members.py);
fine as an inventory, but the stage specs must be written from the requirements (11 says so).
Say plainly in section 1 that v2 optimises the process model and privilege model and leaves
the data model for after stage 8; the owner asked for "properly optimise" and should know where
the optimisation stops and why.

### 11. Decisions decided silently that belong to the owner — RECOMMEND

- A new system service with sockets reachable by every local process (finding 3) — fold into
  D1 with the cost stated.
- B6: `ccy-teams --list` exits non-zero and the board shows "members unknown" when the daemon
  is down. Today the board degrades to asking for sudo. New failure mode for the owner's main
  view; the owner should accept it (or (c') makes it moot).
- A keeper process in **every** member (LXC, host, docker, VM via `agent-bus-claude`), not only
  seated ccy sessions (8.2). New resident process on other machines; owner-visible.
- `refreshInterval: 1` kept (finding 8).
- Stage 6's ccy image bump and its timing (finding 5).
- Whether `human devices` keeps printing device IDs for a human (B2) — fine, but it is the one
  place the design *reduces* what the owner can see; note it in D1's row.
- D6's dependency (`go.yaml.in/yaml/v3` allowlist row) is right to surface; add that comment
  and `!vault` preservation are expectations with fixtures (15 has it; D6 should point at it).

### 12. Smaller corrections

- 3.1 table: `pingbus` "also the host for `seat disconnected`, `farewell`, `retire`, `reach`" —
  correct, but then `pingbus` is also a *host* binary the play deploys and `qa/deployeddrift`
  tracks; add it to the deployed-tools list (Plan 00206).
- 4.1: "systemd unit state: one `systemctl show` for every unit, or D-Bus" — H 7.24 (ask each
  unit type only its own properties, refuse a missing one by name) must survive the
  consolidation; it was a real bug.
- 6.1 "health check … run as `agent-bus-install health-check`": the unit's sandbox
  (`CAP_DAC_READ_SEARCH CAP_CHOWN`, `ReadWritePaths` only the applied dir) is a requirement
  (R7 should name it); a Go binary in the same unit keeps it, but the unit file changes when the
  ExecStart does — that is a `software` CHANGED and a `daemon-reload`, worth a line in stage 2.
- 8.3 Unicode: Go's `unicode` tables (15.0 in current Go) are newer than Python 3.11's
  `unicodedata` (14.0); "v2 escapes any character its own table does not know" is the right
  direction, but the corpus must then include characters *Python* knows and Go does not (none
  today) and the reverse (Unicode 15 additions), so the two renderers' disagreement is pinned,
  not discovered.
- 12 row "Two goodbye records → one": keep `said-goodbye.json` (per claim, read by the host's
  `seat disconnected`), drop `goodbye.json` (per session home). Say which.
- PLAN.md: no task owns the Element play's future (finding 4.4) or the image bump (5); Task 9.2
  runs qa-reviewer only at the end — each stage's acceptance should include it (CLAUDE.md
  requires it per plan, and these stages are each a deploy).
- RESEARCH-host 8.12 recommends designing around the 00197 broker; DESIGN D7/D11 chose otherwise
  (port the play-driven model). That is a defensible call but is the one place the design
  contradicts its own research without saying so; add a sentence in D7.

## What is right and should not be re-opened

The requirement catalogue as a form; the research digests (accurate where probed); keeping
protocol 1 (D5); keeping the password as the presence check (D3); dropping relay mode (D4);
adopting the 00192 installer (D10); the install lock; strict decoders and the shared corpora;
"no fallbacks between v1 and v2 at run time"; the user placing the bundle by fd; the
`PR_SET_PDEATHSIG` keeper with the worker as its only child; the Tuwunel quirk list as `fakehs`
switches; the H1-H10 host checks (add the coexistence checks of finding 9).
