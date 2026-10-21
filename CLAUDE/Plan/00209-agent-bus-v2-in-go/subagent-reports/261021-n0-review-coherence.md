# Plan 00209 N0 integrated review: coherence and completeness

Reviewer concern: coherence and completeness (seam contract honoured, no chapter disagreements,
R1-R50, Plan 00211, Plan 00194 E1-E26, the stable team id, buildability with fakes, the N1-N6
order). Reviewed at worktree HEAD 0b498397 (INT3's short-id fix included). Sub-agent digests, used
as leads and re-checked against the text before any finding below was written:
`untracked/scratch/261021-n0-coh-digest-{E,211,R}.md`; SPEC-matrix taken from
`feat/bus-v2-m1-matrix` into `untracked/scratch/261021-n0-coh-spec-matrix.md`.

## Verdict: FIX-FIRST

1 blocker, 12 major, 8 minor. The chapters are individually strong, and most seam rows hold. But
several rows marked **Fixed** in DESIGN-v2.md section 3 are not fixed in the owning chapter's text
(rows 10, 17, 20, 28, 43, 44), and the LXC member path, which N3 builds first, cannot work as
written.

## Findings

### 1. Blocker: an LXC member never becomes active, and its removal always fails

- **Where:** C 3.9.2 (present and absent phases); B 2.10 (reconcile; "LXC members get the same hot
  control"); B 2.13 (`guest membership`, `guest remove`).
- **What:** B makes a team active only when it is both seated and desired (B31, B 2.10, C 3.7.5),
  and the guest's keeper reads `~/.config/teamchat/membership/desired.json`, which only
  `teamchat guest membership` writes. C's present phase (steps 1-9) places the bundle (step 7) and
  the unit (step 8) but never runs `guest membership`. In the whole of C, `guest membership` appears
  only in the section 7 assumption list.
- **Failure:** `lxc apply --phase present` succeeds, the session starts, and the keeper finds no
  desired seat, so no worker starts. The member is offline for ever, while the board shows it
  `joined` (C 3.9.3 record).
- **The removal fails as well.** The absent phase runs `guest remove`. That takes the seat lock and
  exits 75 if a session holds it (B 2.13). A `service: true` member's supervised session always holds
  it, so the absent phase fails for every running supervised member. Nothing first writes a desired
  file with a `farewell` to make the keeper let the seat go, and no `left released` is posted.
- **Fix:**
  - Present phase, after step 7: `guest membership --user U` with the user's full seat list, under
    the team lock.
  - Absent phase: rewrite the membership without the seat and with a `farewell` (reason `released`
    or `removed`). Then wait for the seat lock, probed by L3 from the host and bounded (B's 30 s
    notice bound plus margin). Then `guest remove`, then `member park`. On timeout, fail naming the
    session.
  - Seam row 28 then becomes true.

### 2. Major: two different mechanisms for the LXC kit and for the supervised unit

- **Where:** C 3.9.2 step 4, C 3.9.4; B 2.13 (`install-self`, `unit`); seam row 28 ("Fixed").
- **The kit.** C writes the kit directory from the host side, swaps `current`, and collects old
  digests by listing each guest process's `/proc/<pid>/exe`. B streams the binary through
  `lxc-attach`, checks it with the guest's `sha256sum`, and has `guest install-self` swap `current`
  and sweep by `session.pid` runtime records. B says plainly: "The host cannot list
  container-private or guest runtimes, so for LXC this sweep is the garbage collection".
- **The unit.** C 3.9.4 says the unit is written from the host side, started over the guest's
  private systemd socket, "with no guest program run", and "running the guest's `systemctl` as root
  is not an option". B's `guest unit` op runs as root in the guest, writes the unit and restarts it.
- **Failure:** N3 Task 3.3 has two incompatible specs. Built either way, one chapter's tests and
  host checks (H-C9, B's install-self sweep) do not match the code. Two garbage collectors can
  delete a digest the other still counts as in use.
- **Fix:** choose one owner for each, and delete the other chapter's text:
  - kit: B 2.13's streamed binary, `sha256sum` and `install-self`, with one stated GC rule;
  - unit: C 3.9.4's host-side write and D-Bus start; B's `unit` op reduced to rendering text, or
    removed.

### 3. Major: B's envelope disagrees with A 13, which seam row 10 says it follows exactly

- **Where:** A 13.1, 13.2, 13.5, 13.7; B 2.7.2, 2.7.3; seam row 10 ("Envelopes exactly to A 13",
  Fixed).
- **What disagrees:**
  - **`truncated:`.** A says `... bytes shown; the whole text is in teamchat --team <team> history (id '<id>')`. B says `truncated: <shown> of <total> bytes; the whole text: teamchat verify '$<43>'`.
    Both the text and the command differ.
  - **`held back:`.** A says `read them with teamchat --team <team> history`. B says `teamchat recv shows them now`. A's budget also counts member-sent NOTICE entries; B's counts PEER and PING
    only.
  - **The consequential-request check.** A 13.5 item 4 says to confirm with `history`. B made
    `verify` for exactly this (B7, B 2.2.1).
  - **Entry order.** A 13.7 orders by `origin_server_ts` across teams, then TIMEOUTs. B orders HUMAN
    first, then PEER and PING, then TIMEOUT, then NOTICE.
  - **The first line.** A's grammar has five literal count slots and `message(s)`. B's example is
    `2 messages (1 from humans, 1 from agents)`.
  - **Line order.** B's example entry puts `team:` before `from:` and has no `re:` line. A's order is
    `kind`, `from`, `role`, `team`, ..., with `re:` on HUMAN, PEER and PING.
  - **Where `--team` goes.** Every command in A has it before the verb (`teamchat --team <team> thread <q>`, `... status`, `... history`). B's CLI defines it per verb (`history [--team T]`, `thread Q<n> [--team T]`) and never says that a flag before the verb is accepted.
- **Failure:** N1 builds A's corpus golden files (A 14 "exact bytes for each kind and trailing
  line"), and B's renderer fails them. Worse, an agent that runs a pointer exactly as printed gets
  exit 64 if `--team` is per-verb only. Those pointers are the `reply:` line for every questions
  entry and the history pointers.
- **Fix:**
  - Make A 13 the one grammar, and replace B 2.7.2's templates with a citation of it.
  - Decide `verify` against `history` once: `verify '<id>'` is the better pointer for `truncated:`
    and for item 4.
  - Decide the global-flag rule in B 2.2, either way, and make A 13's templates match it.
  - Restate E21's bound (0..253402300799999) in A 13.2.

### 4. Major: the flood budget is not a bound, and one flooding peer can stall human delivery

- **Where:** B 2.7.3 "Flood budget"; B 2.3.2 caps; A 11.1 `deliver_per_sender`, A 11.4.
- **What:** A 11.4 bounds what one compromised sender costs each session: 60 events a minute
  admitted, then 10 entries per 600 s, "the rest counted as held back", as v1 (SPEC-inject T6, which
  consumes the surplus). B instead keeps the surplus `pending`, delivers it as the budget refills,
  and says "recv shows them now".
- **Failure:** a compromised member posts 60 events a minute.
  - Each receiver's `pending` grows by about 590 every 10 minutes and reaches its cap of 2048 in
    about 35 minutes.
  - At the cap, B stops that team's sync (B 2.3.2: "A full `pending` stops only that team's sync").
    From then on, no HUMAN message on that team reaches the member until someone runs `recv`.
  - Before the cap, the backlog drains at 10 per 10 minutes for hours, each delivery costing a model
    turn.
  - A's worst-case statement is therefore false for v2.
- **Fix:** choose one rule and put it in A 11, with B citing it:
  - either consume the surplus, as v1 did, keeping the counted `held back:` lines and a `verify` or
    `history` pointer;
  - or keep a small per-sender held cap (for example `deliver_per_sender` times 3) above which the
    rest are counted and dropped.
  - Either way, HUMAN and TIMEOUT items must never wait behind a full `pending`: give them their own
    allowance under the cap.
  - Align the NOTICE inclusion between A and B.

### 5. Major: C says the admin posts only `parked` and `removed`; A and D need four more admin posts

- **Where:** C 3.6.3 ("These two are the only notices the admin's account posts"); A 7.4 (admin
  posts `added`, `removed`, `parked`, `role`); seam row 20 (Fixed).
- **What:** C's `member add` steps are claim, create, mint and invite, with no `added` notice. C's
  `role apply` (3.6.4) posts no `role` notice. D's carried items and the welcome notice (M4) are
  also admin posts, so the "only notices" sentence is wrong twice over.
- **Failure:** humans and the coordinator never see A's `added` and `role` notices. A's tests
  (A 14, every 7.4 event) have no producer.
- **Fix:** in C 3.6.3, add "post A's `added` notice" as the last step of `add` and `return`. In
  C 3.6.4, have each demote or promote post `role`. Replace the "only notices" sentence with the
  full list of admin posts: `added`, `removed`, `parked`, `role`, carried items, and the welcome
  notice (finding 6).

### 6. Major: D's welcome notice has no wire form, so A's rules ignore it

- **Where:** D 5.6 ("an admin message in the v2 main room, pinned"), D 6.2, D 12; A 7.6 rule 06;
  seam row 43 (Fixed).
- **What:** A 7.6 rule 06 delivers an admin event only as a `teamchat.notice` (other than `left` and
  `disconnected`) or as `teamchat.carried`. Anything else is "ignore `admin`". A 7.4's notice events
  have no `welcome` or `moved` event, and a notice with an unknown event is dropped `schema`. A 7.7's
  carried `moved` addresses one sender of an open ping (`to` is exactly one user ID), not every
  member.
- **Failure:** after M6, every moved session's worker ignores the welcome notice. "It reaches the
  session on its first v2 start as a NOTICE" (D 6.2) is false. The agent is never told that its v1
  sends will fail, and acceptance A3/A9 pass on a notice that no agent received.
- **Fix:**
  - Either add a `teamchat.notice` event `moved` to A 7.4 (admin, to every member and human, with
    its own authority sentence in A 13.3 and a corpus case),
  - or have D post one carried `moved` item per member.
  - Then make seam row 43 cite the A section.

### 7. Major: a parked coordinator stays in an open questions room, and C never says when it writes A's roster

- **Where:** A 4.3, A 5.2; C 3.6.1, C 3.6.3 (park); seam row 17 (C is the only roster writer,
  `rev` + 1, refuses an invalid roster; cites C 3.6.1, marked Met).
- **What:**
  - A requires that a parked coordinator is kicked from questions, that questions switches to its
    closed form, and that `parked: true` is set in the roster. C's `member park` is "revoke; post
    `parked`; mark", with no roster-event rewrite and no rooms step.
  - C 3.6.1 describes C's own `ctl/<team>/roster.json`, which has no `rev` field. Apart from a
    rename, nothing in C says when the `teamchat.roster` state event is derived from it, how
    `rev` + 1 is persisted (the converge-by-diff engine of 3.6.2 has nowhere to keep the last
    `rev`), or that an invalid roster is refused before the write.
- **Failure:** after a park, members still see the coordinator as active. Questions stays open.
  Human answers go to a revoked token and wait unread. Rooms converge only on the next play run.
- **Fix:**
  - C 3.6.3 `park`, and every member or role verb: rewrite the roster event (`rev` + 1), then run
    the rooms step of 3.6.2 for the team.
  - C 3.6.1: say that the event is `proto`'s `Roster(roster.json, declaration)` with `rev` taken
    from the current event plus one, validated by A's decoder before the write.

### 8. Major: B and C place v2 Go code outside the separation gate's scope

- **Where:** B 2.2 (`cmd/teamchat/main.go` thin over `internal/bus/member/...`), B 2.15
  (`bus/matrix/fakehs`), C 3.6.1 (`bus/state`), C section 7 (`bus/seat`); D 3.3
  (`src-go/internal/teamchat/...` for "All v2 Go packages"); D 4.1 (the gate's scope is
  `src-go/internal/teamchat/`).
- **Failure:** the member client and the matrix client are built under `internal/bus/`. The
  `teamchat-keeps-off-v1` gate never scans them, so a v1 path in the worker passes QA, which is the
  one property D 4 exists to prove. It also blurs the line from the v1-era `internal/bus` work that
  PLAN Task 7.2 means to supersede.
- **Fix:** in B and C, replace every `bus/...` package name with its `internal/teamchat/...` name
  (`member`, `matrix`, `matrix/fakehs`, `state`, `seat`), to match D 3.3 and PLAN Task 1.5.

### 9. Major: `fakehs` does not model what v2 needs, and Task 1.5 does not list the difference

- **Where:** B 2.6 step 09, B 2.15; A 6.6, A 14 (presence under HC-A1); SPEC-matrix M21, M22, M37
  (on `feat/bus-v2-m1-matrix`, not in this plan folder); PLAN Task 1.5.

- **What:**

  - SPEC-matrix's `fakehs` has no presence at all: no `set_presence`, no `PUT /presence/.../status`,
    no server-side timeout. Its filter renders v1's shape, with presence empty (M21).
  - Its transaction ids are made with prefix `pb` (M22); DESIGN-v2 section 4 names `tc`.

- **Failure:** B's presence rules (the long-poll as the only online-marking request, offline at a
  clean end) and the `member.present` field cannot be tested through fakes, which B41 requires. The
  first test of them is the real-Tuwunel suite.

- **Fix:** Task 1.5 names the v2 changes to the matrix client and `fakehs`:

  - presence routes and timeout;
  - the v2 filter;
  - the `tc` prefix;
  - `m.room.pinned_events` as plain state.

  Also carry SPEC-matrix into this plan folder, or say in PLAN which branch's copy is the N1 spec.

### 10. Major: `teamchat-claude`, the non-ccy launcher, is specified by no chapter

- **Where:** D 3.3 and D 3.5 name `teamchat-claude`, on PATH and in the kit (host and LXC), and
  `~/.config/teamchat/env` for "the launcher's settings" (D 3.6). B never mentions either. C 3.9.4
  and B 2.13 (`guest unit`) run "the launcher".
- **Failure:** N3's LXC-first member has no spec for the program its supervised unit runs: its
  arguments, its env file grammar (strictly decoded or not), how it builds `teamchat run --claude --desired ~/.config/teamchat/membership -- claude ...`, and what it refuses.
- **Fix:** add a B section for `teamchat-claude`, with its tests, and list it in N3 Task 3.2/3.3.

### 11. Major: the N1-N6 order puts `teamchat-coexist` in N2, but the scenario needs N3 and N4

- **Where:** PLAN Task 2.5 (N2 acceptance: "VM-lab scenario `teamchat-coexist` (P1)"); D 4.3 step 4
  ("join a seat and an LXC member, exchange messages").
- **Failure:** N2 cannot reach its acceptance until the member client (N3) and the ccy launcher and
  kit (N4) exist. A strict reading blocks N2's MERGE; a loose one ships N2 with the no-touch proof
  unrun.
- **Fix:**
  - At N2: run D 4.3 steps 1-3, plus a host-only team cycle (create, backup, restore, purge).
  - At N3: add the LXC member to the scenario.
  - At N4 Task 4.4: run the full scenario. Say this in D 4.3 and in PLAN.

### 12. Major: acceptance A7 reads an outbox that `status` does not print

- **Where:** D 6.1 A7 ("read back from the sender's outbox in `teamchat status` (B 2.2.1, 2.3.2)");
  seam row 44 (Fixed); B 2.2.1 `status`; B 2.2.2 record kinds.
- **What:** `status` prints `team`, `member`, `room` and `runtime` records. It has no outbox or ping
  record and no answered state. A7 also does not say how ccy-teams makes a live agent send the ping.
  ccy-teams is not a team human, and it cannot run a verb inside a running session.
- **Fix:**
  - Add an `outbox` record to `status` (ping id, targets, answered, due), or have A7 read the ack
    from the room through `teamchat-admin`.
  - Specify the trigger. For example, the owner runs `teamchat run --plain -- teamchat ping ...` as a
    probe seat, which removes the need to disturb a live agent at all.

### 13. Major: an unattached human post in questions has no valid `reply:` line

- **Where:** A 10.4 rule 5 (delivered with `question: none`), A 13.2 (the questions reply is
  `teamchat --team <team> thread <q>`); B 2.2.1 (`thread Q<n>`, no `--re`; `reply` "replaces ...
  `thread`"); 00211 SPEC 8.3 (`thread --re 'EVENT_ID'`, dropped with no reason given).
- **Failure:** for a `question: none` entry, `<q>` has no value. By A's own rule ("a slot that fails
  its grammar is an error and the entry is not rendered") and B 2.7.2, the HUMAN entry is dropped
  with reason `render`. The coordinator receives only "could not be shown", for every human answer
  that is not threaded or prefixed. That is a common Element habit.
- **Fix:** in questions, the reply line is `teamchat reply '<id>'` for every entry: B's `reply`
  already resolves the room and thread from the item. `thread Q<n>` stays a verb for posting into a
  question. State in A 10 why `thread --re` is dropped.

### 14. Minor: exit 12 `no-room` is dead

- **Where:** B 2.2.4 (exit 12), the B 2.2.2 error example; A 4.1 ("all three rooms exist from
  creation", the refusal deleted, A 19).
- **Fix:** remove exit 12, or redefine it for a side room that fails trust. That case is exit 22 by
  A 6.5, so removal is cleaner. Fix the example message.

### 15. Minor: `human_text` has two sources, the bundle and the roster

- **Where:** A 5.1 ("Why `human_text` and `limits` move from the bundle to the roster"); C 3.7.2
  (`seat.json` still carries `human_text`); B 2.6 step 10 and C 3.6.4 (both decide from "the
  bundle's" value).
- **Failure:** the owner flips a member's `human_text`. The roster changes, but no bundle is
  re-issued (C 3.7.2 says so), so B's invite rule and A's verdict read different values.
- **Fix:** drop `human_text` from `seat.json` and read the roster everywhere.

### 16. Minor: C says the keeper posts the goodbye

- **Where:** C 3.7.5 "Leave" and C section 7, assumption on B, item 4 ("B's keeper ... posts the
  goodbye as that member, drains its outbox"). This contradicts B14 and B 2.10: the keeper makes no
  network call, and the worker posts `left released`. B also does not "drain the outbox" on a
  remove.
- **Fix:** C's wording follows B 2.10.

### 17. Minor: C's `migrate` verb list does not match D's steps

- **Where:** C 3.3 (stage, fence, unfence, fence-status, carry, commit, archive, archive-check,
  footprint); D 5.4 (M0 preflight, M3 capture, M6 move, M7 accept, rollback 5.7).
- **Fix:** list every root step, or say which steps run as the user and through which verb.

### 18. Minor: section 8 trace defects

- R3's "no TLS (documented)" is dropped silently. Add a C 6.1 row or a C 3.2.5 sentence.
- R14's note says C 3.7.5; the 180 s wait is in C 3.7.4 (line 1014).
- R43 and R44 cite B11 and AR21, which do not state the own-token refusal, the 10-minute credential
  hold or the drop after a 401. The rules are in A 7.3 and B 2.6 step 11; cite those, or add a B
  requirement.
- R41 has no requirement ID.
- R19 and R31 cite only part: add B 2.6 steps 03 and 08 to R19, and B33 to R31.
- R10's "refused naming the holder" became a declaration-time single field (C30); say so.

### 19. Minor: Plan 00211 items carried only in part

- The skill and session-start text: 00211 10.1-10.2 (`role_questions`, `role_reports`, "ask the
  coordinator, never a human", "reports are not for answering"). B 2.12's outline also puts `report`
  under "for a coordinator", although any member may report.
- `ask`'s stdout record is not bound to the `question` kind. `--asked-by` name resolution, and the
  check that `--re` is a main-room event (its exit code), are unstated.
- Health: 00211 5.4's "no worker in questions" and "the coordinator joined" checks are not itemised
  in C 3.11.2.
- C 3.6.4 never names the questions room's switch between open and closed forms, or the re-invite of
  a coordinator that left. It relies on `Desired()`, which is acceptable only once finding 7's
  ordering is stated.

### 20. Minor: the bootstrap order of the roster and the side rooms is unstated

- **Where:** A 5.1 (a valid roster needs both `rooms`), A 5.2, A 5.3 (the side-room back-link needs
  main's ID); C 3.6.2 (a generic engine, specs per room).
- **What:** the order must be: main is created with no roster; the side rooms are created with
  their back-links; the roster is written with `rooms`; then invites go out. Neither A 5.2 nor C
  3.6.2 says it, and `Desired(team, roster)` cannot name room IDs that do not yet exist.
- **Fix:** one sentence in A 5.2, and C 3.6.2's engine passes created IDs back into `Desired`.

### 21. Minor: B 1 cites only some E-numbers in its requirements

B 1 cites E1-E13, E16, E17, E19, E20 and E25. E14, E15, E18, E21, E23, E24 and E26 appear only in
B 2.7.6's one-line table. E26's list of codes that are never restarted (8, 10, 64, 78) became 78
alone, and the others are not mapped.

## What was checked and found sound

- **The seam contract, spot-checked against the owning text (22 rows).**
  - Sound: 2, 3, 4, 11, 15, 24 (apart from finding 15), 25, 27, 31, 32, 33, 40, 42, 45, 46 (apart
    from finding 17) and 50.
  - Not as stated: 10, 17, 20, 28, 43 and 44 (findings 3, 7, 5, 2, 6 and 12).
  - Rows 13, 18, 23, 49 and 52-56 are honestly marked Later, and each is named in PLAN.
- **The stable team id.** It holds everywhere checked. Unit, dynamic user, every path,
  `server_name`, bundle, roster `team`, Element profile and launcher file, and every status record
  use the id. The display name appears only in room names, the roster `name`, the launcher's
  `Name=`, `status` and the board (A 2.2, 4.1; C 3.5.2, 3.10, 3.11.3; D 3.5, 3.7). INT3's short id
  for a long v1 name is consistent across D 3.5, 3.9, 5.1, 5.4 M0, 5.10 and C 3.8, and resolves
  unambiguously (23 or 24 characters can never be an id).
- **R1-R50.** Every cited ID exists. 36 of the 50 are carried outright, and the partial ones are
  covered in the chapter bodies, apart from R3's TLS clause (finding 18). R47's drop is sound.
- **E1-E26.** 21 are met, including the strong transcript proof (E16), exactly-once delivery
  (E10-E12) and failure handling (E15, E25). E22 is dropped with a reason. The partly-met ones are
  findings 3, 4 and 21.
- **Plan 00211.** The verbs exist with consistent names in A and B (`report`, `ask`, `thread`,
  `questions`, `history --room`). The rooms and power levels, side-room trust (A 5.3, 6.3-6.4), the
  question index and numbering with `question_floor` and admin-carried questions (A 6.5), reminders
  (A 10.5) and edits (`revises:`) are fully specified. The gaps are findings 7, 13, 14, 19 and 20.
- **Elsewhere.** The `disconnected` and `left` split (A 7.4, B 2.10-2.11, C 3.7.5) is coherent
  apart from C's keeper wording. The desired-file schema matches between B 2.10 and C 3.7.5. The
  `seat disconnected` flags match. The guest op names match between C and B 2.13. Fakes are named
  for each external system in B 2.15 and C 3.14 (`fakeinbox`, `faketranscript`, a fake systemd
  D-Bus, `execx.Fake`), apart from finding 9.

## Outside this concern (brief)

- PLAN Task 0.4 asks the N0 review to challenge BD9, BD2 and BD11. I did not judge them.
- D M2(b) and M8 run v1 verbs (`agent-bus-install backup-now`, `agent-bus human lock`) "through the
  adapter". Check that D 5.5's fixed list holds each one.
- D 3.9's "one Go function shared by ccy-teams and the launcher": the ccy launcher is bash, so how
  it calls that function needs a sentence.
