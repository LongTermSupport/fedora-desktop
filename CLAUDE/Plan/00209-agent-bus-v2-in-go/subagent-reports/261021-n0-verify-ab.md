# Plan 00209 N0 fix pass, verification of scope "ab" (A protocol, B member, their seam rows)

Verifier, not author. Read at worktree tip 31a1d598: DESIGN-v2.md (sections 3 and 7 in full), A in
full, B 1-2.13 and 3, the delivery and member dispositions, the merge report, the four reviews, A's
round-3 review (for the five late minors), and the D and C passages that seam rows 43, 44 and 28 and
BD13 cite (D 5.6, 6.1 A7, 6.2; C 3.3 acceptance probe, 3.7.5, 3.9.2 absent phase).

## Verdict: FIX-FIRST

Every blocker and major in scope is fixed in the text or fixed on the A and B side, with two
PARTIAL. The new mechanisms bring four new majors, none of them wording: the store-less resume can
start arbitrarily far back and can skip owed items (N1); the ccy chain puts an unspecified supervisor
between the keeper and Claude Code, which every stop and end rule ignores (N2); acceptance A7's probe
cannot send the ping it is specified to send without being a roster member asking the coordinator
for real work (N3, seam row 44); an LXC agent can stop its own removal by rewriting the desired file
B 2.10 lets it own (N4, BD13). Then nine minors.

Counts: 0 new blockers, 4 new majors, 9 new minors.

## Blockers and majors in scope

| Review and number     | State          | Evidence                                                                                                                                                                                                                                                                                                                                                   |
| --------------------- | -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| quality 1             | VERIFIED-FIXED | A 13 is the one grammar (13.1 worked example, 13.2 templates in B's form, `verify` in `truncated:`, `not shown:` and 13.5 item 4, humans first 13.7, every SessionStart 13.5); B 2.7.2 restates none; B 2.12 test parses every command in templates, trailing lines, authority sentences and frame text. Two template leftovers the test would trip on: N9 |
| quality 2             | VERIFIED-FIXED | B13, B45, 2.1, 2.4.3: the link always runs, binds `link.sock` itself under `link.lock`, no keeper server, no hand-over; one writer per file in 2.3.1 (keeper.json keeper-only, session.json and status.json link-only)                                                                                                                                     |
| quality 3             | VERIFIED-FIXED | 2.4.5 "There is no repair or adopt path"; BD17; no `repair` or `keeper` verb in 2.2.1                                                                                                                                                                                                                                                                      |
| quality 4             | VERIFIED-FIXED | the process is the link (2.1 last paragraphs); exit 69 codes `no-session`, `no-link`, `link-failed`; `"link"` in the runtime record; status line and hook text say `teamchat`                                                                                                                                                                              |
| quality 6             | VERIFIED-FIXED | A 4.2 (admin redacts one event, reason `secret`), A 7.6 rule 01, A 12.4, OD-A10; B 2.7.3 "A redacted item". The rule depends on redactions reaching the link, and no chapter defines the sync filter that would carry them: N8                                                                                                                             |
| quality 7             | VERIFIED-FIXED | A 13.1 "one team", B 2.3.2 `awaiting` has no `teams`, B 2.7.3 "One team per delivery" and the team choice by oldest HUMAN item                                                                                                                                                                                                                             |
| coherence 3           | VERIFIED-FIXED | as quality 1; flag rule decided in B 2.2.1 (flags after the verb, a flag before it is 64, no global flags); E21 bound restated in A 13.2 `sent:`                                                                                                                                                                                                           |
| coherence 4           | VERIFIED-FIXED | A 11.1 `deliver_per_sender`, A 11.4 applied at admission, surplus not kept; B 2.7.3 flood budget; B 2.3.2 reserves 256 `pending` entries for HUMAN and TIMEOUT; NOTICE inclusion aligned (member-sent notices count, admin items never held)                                                                                                               |
| coherence 6           | VERIFIED-FIXED | A 7.4 `moved` (admin, no subject, to every non-parked agent, pinned), its gloss, 7.6 rule 06 delivers it, A 11.2 `to` 1-64; D 5.6 and 6.2 use it; carried kind renamed `forwarded` (A 7.7)                                                                                                                                                                 |
| coherence 12          | PARTIAL        | `status` now prints `outbox` records (B 2.2.1, 2.2.2) and D 6.1 A7 reads them, so the readable half is fixed. The trigger, as the merge resolved row 44, does not work as written: N3                                                                                                                                                                      |
| coherence 13          | VERIFIED-FIXED | A 13.2 `reply:` is `teamchat reply '<id>'` in questions; A 10.3 unattached reply (a `teamchat.thread` with no `n`); why `thread --re` is dropped is stated                                                                                                                                                                                                 |
| coherence 1 (B side)  | VERIFIED-FIXED | B 2.10 last paragraph and 2.13 `membership` as the agent user, C 3.9.2 runs it. The mechanism it chose opens N4                                                                                                                                                                                                                                            |
| coherence 2 (B side)  | VERIFIED-FIXED | B 2.13: no root guest op, no `install-self`, no `unit`; kit and unit host-side through the root fd; one collection rule by `session.pid`                                                                                                                                                                                                                   |
| coherence 8           | VERIFIED-FIXED | B 2.2 `internal/teamchat/member`; B 2.15 `internal/teamchat/matrix/fakehs`                                                                                                                                                                                                                                                                                 |
| coherence 10          | VERIFIED-FIXED | no `teamchat-claude`; every launcher ends in `teamchat run --kind` (B 2.4.1)                                                                                                                                                                                                                                                                               |
| operations 1          | PARTIAL        | `run` is the keeper and Claude Code's parent (2.4.1, 2.1), the link ignores SIGTERM/SIGINT/SIGHUP in its own session with `/dev/null` stdin and log stdio (2.4.3, B47), per-kind trace and launcher contract (2.11: 20 s container stop, 30 s unit, default KillMode), VM-lab rows (2.15). Host `cc` and LXC traces hold. The ccy trace does not: N2       |
| operations 2          | VERIFIED-FIXED | B29, 2.11 host rule by seat lock and room, called by the ccy launcher at podman exit and by the health tick for every kind; `back` added (A 7.4). Two txn-id defects in the doubles guard: N6                                                                                                                                                              |
| operations 3 (B)      | VERIFIED-FIXED | A 5.1 `restore_epoch`; B 2.6 step 08 reads the roster by a direct state request after every outage and resumes store-less; B 2.3.2 store field. A start from a stored position after a restore that happened while the session was down is not covered by step 01 (a): N7                                                                                  |
| operations 6 (B)      | VERIFIED-FIXED | as coherence 2, and `membership` has a writer                                                                                                                                                                                                                                                                                                              |
| security 1            | VERIFIED-FIXED | B49, B44, 2.6 step 01 (b): start at the earlier of the receipt and `released_ms` from the host-held `seat.json`; items before the release marked `restored`; C's DC16 now logs a released token out. The bound does what security 1 asked. The receipt it takes as the other bound is the wrong fact, which N1 is about                                    |
| security 5            | VERIFIED-FIXED | A 7.7: nothing from v1's questions room is carried (reported, question re-posted); inbox and inject entries are candidates carried only after the v1 server shows the addressing                                                                                                                                                                           |
| ops 15, qual 16 (min) | VERIFIED-FIXED | B48 and 2.4.2: 1, 2, 4, 8, 16 s, `failed` on the 6th exit with no 10-minute run or at once on 78, seat locks closed on `failed`, recovery per kind; hooks write no file (2.8)                                                                                                                                                                              |

Rejected finding: quality 9 (`ping` read as a liveness check). The rejection is sound: `request` is
the envelope's word for a human's authority, the verb is required and comes first, and a bare `ping`
exits 64 with a pointer to `status` (B 2.2.1).

A's five late minors (round-3 C1-C5), each read in the merged text: C1 fixed (A 10.2 counts from the
index and `question_floor`; A 6.5 "an uncounted question is not in the index"); C2 fixed (A 13.1,
13.3, 13.6 point 1: MEMBERSHIP its own delivery, description slot named); C3 fixed (A 10.5 "still
`open`"); C4 fixed (`humans` reserved, A 2.3); C5 fixed as recommended (A 7.6 rule 09). C5's optional
second half was not taken: a ping whose receiver-side forge check fails only transiently
(`unresolved` for an unreachable forge, `forge-rate`) is dropped for good under judge-once. Its
sender still gets a TIMEOUT, so it is not silent; noted, not a finding.

Seam rows 43 and 44 as the merge resolved them: row 43 holds (A 7.4, D 5.6, 6.2 and C 3.6.3 agree
on `moved`, admin-posted once, pinned, to every non-parked agent; the relaunch is the second signal).
Row 44 does not hold: N3.

The A 7.7 decision (no feature name for `teamchat.carried`) is sound: every v2 release knows the
key, so a feature could never protect a receiver, and the end path (rule 06 "anything else" is
ignored) needs no new major. Its condition for dropping the key is slightly too weak: N10 below.

## New findings

### N1. major: the store-less resume starts from a receipt that B sets for HUMAN items only (B 2.6 step 01 (b), B49, B44, 2.7.5)

What is wrong. Step 01 (b) starts each room at the earlier of the member's own latest read receipt
(or its join) and `released_ms`. B 2.7.5 sends `m.read` only for HUMAN items, after proof. So the
receipt is not "everything before here was handled"; it is "the newest proven HUMAN item". Two
failures follow.

1. **Owed agent items are skipped silently.** Humans go first (B24, A 13.7), and deliveries are
   proven independently. Scenario: a PING P arrives, then a HUMAN H. The delivery holding P is
   declared missing (2.7.4), so P returns to `pending`, held from the socket for `recv`; the next
   delivery holds H and is proven, and the receipt moves to H, after P. The session is released
   before the agent runs `recv`. DC15 quarantines the store. The next session starts at
   min(receipt H, `released_ms`) = H, and P is never fetched. No `gap:` line, no count. The same
   holds for any PEER, PING or member NOTICE still pending, `handed` or awaiting proof at release
   that is older than a proven HUMAN item. This is exactly the "nothing posted for it is skipped"
   B49 and INT1 claim.
2. **The start can be months back.** A seat whose sessions handle only agent traffic has no
   receipt, so the start is its join. Every new ccy session on that seat (every launch, since the
   store is always quarantined) then re-reads the room from the join under step 03's bound of 20
   pages of 100. On a busy team that bound is hit on every start, so every session start carries a
   `gap:` line, and which events fall in the gap (the oldest or the newest) is not said: step 01
   says only "fetched with `/messages` under step 03's page bound". If the newest fall in it, the
   items sent while no session held the seat, and a migration's carried items for a member stopped at
   the move (D 6.2 relies on this resume), are the ones skipped. Below the bound, the last 24 h of
   agent text the previous session already handled is redelivered as `restored` (8.5 staleness
   stops only older text), costing turns and held-back lines.

Fix. Resume from a position that means "nothing before here is owed": the room's floor, which B
already keeps (`floor_ms`, 2.3.2), written to the server as the member's own marker in the same
step that persists it (room account data such as `m.fully_read`, or a `teamchat.floor` account-data
key under A's namespace). Keep `m.read` for Element's display. Step 01 (b) then starts at
min(floor marker, `released_ms`), which is bounded by the host's release time exactly as B49 needs
(the marker is set with the token, like the receipt). State which end of the range the page bound
gives up (the oldest, so the newest is never in a gap), and add a crash-point test for scenario 1.

### N2. major: the ccy chain puts the ccy supervisor between the keeper and Claude Code, and no rule covers it (B 2.11 ccy rows, 2.4.1 steps 5 and 7, 2.4.2 "The end", B 6 C3; seam row 53)

What is wrong. B 2.11's ccy row and B 6 C3 say `entrypoint.sh` execs `teamchat run --kind ccy` with
"the ccy supervisor inside it as CMD", and the supervisor runs `claude`. Every keeper rule is
written as if CMD were Claude Code:

- a stop is "forwarded once to CMD" (2.4.2), and the ccy row says the keeper "forwards it to Claude
  Code". It reaches the supervisor. Nothing requires the supervisor to forward `SIGTERM` or `SIGHUP`
  to `claude`. A shell supervisor waiting on a foreground child defers its trap until the child
  exits and does not pass the signal on. Then `podman stop`: the keeper forwards to the supervisor,
  `claude` runs on, podman kills everything at 20 s, no `left` is posted, and the launcher posts
  `disconnected` for an orderly stop: operations 1's regression, back on the main ccy path;
- `end exit|stop|killed` is judged from CMD's status, the supervisor's, not Claude Code's;
- if the supervisor restarts `claude` inside the same keeper (v1's first-join relaunch and
  `ccy-teams restart` are both "a relaunch request the ccy supervisor acts on", D 6.2), the new
  `claude` reads the same `<v2-run>/settings.json`, written once with `O_EXCL` and the launch's
  `inbound`, and the link keeps the launch value (B 2.10). A session launched on no team then stays
  in `wait` mode after its relaunch for ever. The old `claude`'s SessionEnd reason also stays in
  `session.json`, so a later crash of the new one is announced as `left`, not `disconnected`. And a
  fresh `claude` with source `startup` and a new session id cannot change the transcript path
  (2.4.4 accepts a new id only on `clear` or `resume`), so every delivery is declared missing.

Fix. In B 2.4.1 and 2.11, state the contract CMD must meet when it is a wrapper: it forwards
`SIGTERM` and `SIGHUP` to Claude Code, exits with Claude Code's status (128+n for a signal), and never
starts a second Claude Code; any relaunch ends the keeper (a new container or a new `run`). Or have
the keeper signal CMD's process group, with CMD started as its own group leader and the terminal's
job control kept right. Clear `end_reason` at every SessionStart. Put the real supervisor, not a
stand-in, in the 2.15 VM-lab row for `podman stop`.

### N3. major: acceptance A7's probe cannot send the ping it is specified to send (seam row 44; D 6.1 A7, C 3.3 acceptance probe; A 6.7, 7.6 rule 04, 8.1, 7.4)

What is wrong. A7 has a temporary probe account send "one closed-verb `teamchat ping` whose verb
expects an answer" to the coordinator. By A, an account that is not a non-parked role holder in the
roster is sender class none, and its ping is dropped `sender` (6.7, 7.6 rule 04). So the probe must
be added to the roster with a role, which neither D 6.1 nor C 3.3 says. As a worker, its only verbs
that expect an answer are `review` and `run-qa` (8.1: `fetch`, `sync` and `halt` are
coordinator-only, and a second coordinator makes the roster invalid), and both require a `ref` that
passes the forge check at the sender (the owner's `gh` login on the host) and at the receiver. So
the acceptance asks the live coordinator to review or run QA on a real artefact, which is real work,
not the "one short turn" D claims. Adding and removing the probe also posts A 7.4's `added` and
`removed` notices to the humans and the coordinator (A 7.4 makes them every admin post, C 3.6.3
posts them), writes the roster twice, and burns a handle and a seat number for good (AR8, R9: never
reused).

Fix. Choose and state one, in D 6.1 and C 3.3, with A's support where needed: (a) a closed verb for
exactly this, with no `ref`, answer expected, sendable by a worker, whose authority sentence says
"a check that the team is reachable: answer with `ack`"; or (b) accept a real `review` of a named,
harmless artefact and say so; or (c) drop the closed-verb requirement and have A7 accept the
coordinator's proven delivery, made visible by a receipt on a `say` (which needs B to receipt PEER
items for the probe case). For any of them, say that the probe is a roster member with a role, that
its `added` and `removed` notices are posted or are suppressed for a probe (and how), and that its
handle is a reserved probe seat rather than a fresh counter value.

### N4. major: an LXC agent can stop its own removal, because it owns the desired file the absent phase relies on (B 2.10 last paragraph, BD13, B31; C 3.9.2 absent phase steps 1-4)

What is wrong. B 2.10 makes the LXC desired file the agent's own (written by `guest membership` as
the agent user) and argues that a rewrite "can only change which of the seats placed in its home
are active", undone by the next apply. C's absent phase rewrites the file without the seat (step
1), waits at most 60 s for the seat lock to be free (step 2), and fails the phase otherwise; only
after that come `guest remove` (step 3) and `member park` (step 4), which is what revokes the token.
An agent that watches its own file and puts the seat back (its keeper re-takes the lock within 2 s,
B32) makes step 2 fail on every apply. The member is never parked, its token stays valid, and it
keeps reading and posting in the team the owner removed it from. B31 ("the member never grants
itself a team") is false for LXC.

Fix. In C 3.9.2's absent phase, park first (revoke the token, as the stopped-container branch
already does), then rewrite the membership, wait, and `guest remove`; a timeout then leaves only
files with a dead token, not a live member. In B 2.10, restate the LXC residual: an agent can keep a
placed seat active until the host revokes its token, and nothing it does can delay that.

### N5. minor: `back` is posted only when a store survives (B 2.11 "Closing the previous run"; A 7.4)

A 7.4 says the link posts `back` "when a run starts on a seat whose latest own lifecycle notice in
main is `disconnected`". B puts that step in the list for a store that survived its session, so a
new ccy session (store quarantined, DC15) on a seat the host announced `disconnected` posts no
`back`, and a coordinator told that the member went is never told it returned. Fix: make the `back`
check part of every team goroutine start in B 2.6 step 01, (a) and (b) alike.

### N6. minor: the doubles guard for `disconnected` uses two different seeds, and one is read from the wrong file (B 2.11, 2.5 "Content")

- For a ccy seat the host's calls use the claim id (`tc-disc-<claim>`), while the next link of a
  restored session uses the ended run's id (`tc-disc-<run id>`). Each judges from the room first,
  but a tick that posts while a restored session's link is starting gives two `disconnected` for one
  claim, against A 7.4's "exactly one per claim". Fix: the link uses `seat.json`'s `claim` for a ccy
  seat (seam row 24 puts `claim` there).
- For LXC the link reads "the ended run's id from the seat lock file", but the keeper writes its new
  run id into that file when it takes the lock (2.5 "Content"), before the link starts. The link then
  derives the new run's id, which the tick never uses. Fix: take the ended run's id from the
  surviving store's `run.id`.

### N7. minor: a stored position is not checked against `restore_epoch` at start (B 2.6 step 01 (a), step 08)

Step 08 compares the epoch "after every outage". An LXC or host member whose session was not running
during the restore starts at step 01 (a), "verify the room, then sync from the stored position", and
no step says the epoch is compared there; the room verification reads the roster but is not said to
act on it. B 2.3.2's `restore_epoch` bullet is general enough to cover it, but step 01 (a) is what
an implementer follows. Fix: step 01 (a) compares the roster's epoch read during verification with
the store's before syncing.

### N8. minor: B syncs "with A's filter", and A defines no filter (B 2.6 step 02, step 10; A 6.5, 7.6 rule 01, 12.4)

The redaction rule (7.6 rule 01) needs `m.room.redaction` in the timeline; the re-verification
triggers (6.5) need the five trust state types and the member's own `m.room.member`; presence (6.6)
needs the presence section. No chapter lists the filter, and coherence 9 found SPEC-matrix's filter
in v1's shape. A filter that keeps the timeline to `m.room.message` would silently disable admin
redaction of a pending item. Fix: A 6 states the filter's event types per room and role, and A 14's
corpus pins it.

### N9. minor: two agent-facing texts are not commands the 2.12 test can parse (A 13.3 reminder authority; A 13.2 `reply:`)

The reminder's authority sentence says "remind the humans with teamchat thread and --ask", which is
not `thread Q<n> --ask` and fails B's parser, which B 2.12 applies to authority sentences. The PING
`reply:` template ends `--verb <VERB>`, a literal the agent replaces, which no corpus slot fills.
Fix: write the reminder as `teamchat thread <q> --ask` with the reminder's `Q<n>` as a slot, and give
the 2.12 test a stated rule for `<VERB>` (parsed with each answer verb).

### N10. minor: the condition for dropping `teamchat.carried` is "has run", not "has delivered" (A 7.7 last bullet)

A release may stop reading the key once every seat of every moved team "has run on v2 since its
team's move". A member that ran but still holds a carried item in `pending` (or past a gap, N1)
re-fetches it after the upgrade, rule 06 then ignores it, and the item is lost silently. Fix: the
condition is that no store and no server-side position still owes a carried item; the board or the
migration report can show it per seat.

### N11. minor: the skill tells agents that NOTICE is about this session (B 2.12 item 1; A 13.3)

Item 1 says "NOTICE is teamchat's own fact about this session". A 13.3 says NOTICE is a fact about
the team from the admin or a member about itself; MEMBERSHIP is teamchat's fact about this session.
The frame text (A 13.5 item 2) has it right, so the agent gets two meanings. Fix: item 1 follows A
13.3, and names TIMEOUT and MEMBERSHIP too.

### N12. minor: B49's wording overclaims (B49, INT1 row)

B49 and INT1 say a store-less seat "never starts after a message that arrived while no session held
it". With step 03's page bound that is true only when the range fits in 20 pages; otherwise the
`gap:` line says it. Fix: "never starts after ... except past BD15's bound, which it says".

### N13. minor: the restart count and the removal kill (B 2.5 "A removal that times out", 2.4.2)

A removal that times out kills the link, and "the kill counts as one of the link's exits, B48". Five
timed-out removals inside ten minutes (an unreachable homeserver during a series of hot leaves, which
2.10 says costs "only the notice's delay") then mark the link `failed` for the session's life, ending
every other team. Fix: a kill the keeper made on purpose is not a crash and does not count toward
B48's bound.

## Checked and found sound

- The process model: two resident processes in every kind; the seat lock as one shared file
  description held by keeper and link, closed by the keeper only at the end and on `failed`, so a
  free lock with no notice means exactly what the host's rule says (B 2.5, 2.11); the link learns of
  the keeper's death by the control socket and a pidfd checked against `getppid()`; no
  `PR_SET_PDEATHSIG`.
- Exit 69 at once: the link alone binds `link.sock` after `ready`; a refused connect reads
  `keeper.json` and probes `keeper.lock` to choose `no-link` or `link-failed`; only `wait` retries,
  with its deadline; requests past their deadline never run (B45, 2.4.4).
- The restart bound: 1, 2, 4, 8, 16 s, failed at the 6th exit with no 10-minute run, at once on 78;
  E26's codes mapped; per-team failures never end the link (2.4.2). B48, BD18 and 2.4.5 agree.
- The goodbye in LXC (link in its own session, ignoring SIGTERM, SIGINT, SIGHUP; keeper in the pane
  forwarding; `TimeoutStopSec=30`; `After=network-online.target`) and on the host (SIGHUP on a
  closed terminal, no writes to it). The end table in 2.11 is complete for these kinds.
- One team per delivery, its team choice by the oldest HUMAN item, the per-store transitions, the
  ledger invariant, `handed` persisted before records are sent (2.3.2, 2.7.3).
- The envelope verbs: every template command in A 13 exists in B 2.2.1 with its flags after the verb
  (`reply`, `verify`, `status --team`, `history --team`); `status` prints the `outbox` records the
  `answered:` summary points at.
- The redact verb as A and B see it: admin only, one event, reason `secret`, by password sudo; a
  pending or `handed` item removed in one store write; a restored item dropped by rule 02 at its
  re-fetch; a delivered copy said to stay (A 12.4, B 2.7.3), subject to N8.
- `restore_epoch` read by a direct state request, not by sync, after an outage (B 2.6 step 08),
  apart from N7.
- The `back` belief rule (subject equals sender, at most once per run, only after a `disconnected`).
- BD1-BD18 rows in DESIGN-v2 section 7 match B 3; BD14 in B 3 is settled as OD-D2 says.

## Outside this scope, briefly

- C 3.9.2's absent phase is the other half of N4 and needs the reorder there.
- C 3.3's probe and D 6.1 A7 carry N3.
- D 6.2 relies on the store-less resume from the join for a moved member's carried items, which N1
  makes unreliable on a busy team.
