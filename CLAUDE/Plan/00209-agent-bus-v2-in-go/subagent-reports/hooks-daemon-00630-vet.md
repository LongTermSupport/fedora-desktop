# Vet: hooks-daemon Plans 00630-00635 and 00523 against fedora-desktop teamchat v2

Read-only review. Plans read at hooks-daemon `main` 2a6fe84eb (paths below are relative to
`CLAUDE/Plan/` there). Truth read in fedora-desktop branch `plan-xhost-lxc-members` at
93fd6761a (paths relative to the repository root). `whoami` and `duty` are judged against the
approved spec (`CLAUDE/Plan/00209-agent-bus-v2-in-go/PROPOSAL-seat-kinds.md`, "PROPOSAL" below),
not against merged code. Nothing was built or run.

Short names used below: DESIGN = `00630-.../DESIGN.md`; HOSTCC =
`CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-host-cc-v2.md`; QADESIGN =
`CLAUDE/Plan/00263-machine-qa-queue/DESIGN.md`; TC = `src-go/internal/teamchat`.

## Blockers

### B-1. The probe reads the wrong variable: `TEAMCHAT_MEMBERSHIP` never reaches the session

- Plan: DESIGN:92, :106, :129, :164-166, :266; 00631 PLAN.md:30-31 and Task 2.2 (:54-55).
- Truth: `TEAMCHAT_MEMBERSHIP` is read by `ccy-start-claude` and unset for the session
  (`CLAUDE/Plan/00209-agent-bus-v2-in-go/LAUNCH-ccy.md:37-41`;
  `src-go/internal/ccystart/ccystart.go:17`, "TEAMCHAT_MEMBERSHIP does not reach the session").
  A host `cc` session never has it: it passes `--desired` as an argument (HOSTCC:103). It
  also points at `desired.json`, which holds seats, not roles (PROPOSAL:48-50). What the
  session (and so the hook process) does carry is `TEAMCHAT_RUNTIME`, the run directory, plus
  `TEAMCHAT_KIT` and a `PATH` with the kit's `bin` first. The keeper sets them on the child
  (TC/member/keeper/loop.go:469, :161-166). `whoami` finds `status.json` from
  `TEAMCHAT_RUNTIME` (PROPOSAL:147).
- Effect as written: every v2 session resolves as "absent", so no behaviour ever switches on.
- Fix: stamp `TEAMCHAT_RUNTIME` (and `TEAMCHAT_KIT`, to find the binary) onto the payload.
  "Absent" means no `TEAMCHAT_RUNTIME`, or `whoami` exits 69 (PROPOSAL:151-153). Delete
  `TEAMCHAT_MEMBERSHIP` everywhere, including the absent fixture (DESIGN:266).

### B-2. The seat model contradicts the approved spec (role, seat kind, `+local.host`)

- Plan: DESIGN:23-27 ("seat kind host|container; a host seat can be a worker or the
  coordinator; two independent axes"), DESIGN:121-125 (`seat_kind: host | container`),
  DESIGN:311-314 and :437 (B7 keyed on the `+local.host` handle kind); 00631 PLAN.md:15, :26;
  00635 PLAN.md:24-26, :56.
- Truth: the approved field is `duty`, with values `coordinator`, `worker` and `host`.
  `reviewer` and `observer` come later (PROPOSAL:73-85, :191, :202-203). A host member is never
  coordinator (OD1, PROPOSAL:198) and host seats are not workers (PROPOSAL:10, :14). The wire
  `role` stays `coordinator|worker|probe|null` (TC/member/state/status.go:79). The transport
  `kind` is a third, separate thing: `podman|lxc|host` (PROPOSAL:19-20, :67-69). `duty` is
  carried on each `status.json` team row and given out by `teamchat whoami --json` as
  `{"v":1,"teams":[{"team","member","role","duty"}]}` (PROPOSAL:143-150). Parsing a handle for
  `+local.host` is a v1 user-id shape (DESIGN:103) and has no place in v2.
- Fix: the model per team is `role` (wire; `null` → unknown) plus `duty` (open set; `null` →
  unknown). B7 is `duty == host`. The coordinator behaviours use `role == coordinator`, which
  equals `duty == coordinator`. Drop `seat_kind`, `container` and `+local.host`, and drop the
  "a host can be coordinator or worker" text. Settle Q1 as option C (`whoami`) now, as
  approved upstream, and drop the option A stop-gap (DESIGN:184, :188-190; 00631 Task 1.1).
  `status.json` decodes strictly and is versioned, so a second decoder in Python would drift
  (PROPOSAL:65-66, :139-140).

## Major

### M-1. Secrets: the v2 credential is not a `token` file

- Plan: DESIGN:230-231 (invariant 5), :279; 00631 PLAN.md:37, :63; 00634 PLAN.md:18.
- Truth: v2 keeps the seat credential in a `password` file in the seat directory
  (TC/paths/member.go:18, :40; pool seat TC/paths/paths.go:744-745). The seat store holds the
  credential, so a probe must never walk it (PROPOSAL:61-64). The store is mode 0700 and its
  files 0600 (TC/member/state/writer.go:23, TC/member/state/strict.go:36). `status.json` holds
  no secret (PROPOSAL:58-59).
- Fix: restate invariant 5. The daemon opens nothing under the seat store or state root
  (`<store>/sessions/<session>`, `/run/teamchat/seats`, the pool) and nothing under the run
  directory. It only runs `teamchat whoami|history|verify`. Test it as "no file opened under
  those roots", not "no file named `token`".

### M-2. B2 (00634) names a verb that cannot do the check; `verify` can, but only for the main room

- Plan: DESIGN:290-292, :459-465 ("the `pingbus history` equivalent"); 00634 PLAN.md:15-17.
- Truth: v2 has `teamchat verify <event-id>`. It re-fetches one event and returns kind, sender,
  team, room, text and SHA-256 (TC/member/cli/verbs.go:247; record shape
  TC/member/records/records.go:330-339). But it fetches from the main room only
  (TC/member/teamsync/verbs.go:153, sets `Room: proto.PurposeMain` at :166). Human answers live
  in the questions room (TC/proto/rooms.go:17; `questions` verb, verbs.go:241).
  `history --room questions` is a bounded list (verbs.go:243-245). It cannot look up one id.
  Every agent verb needs the session's `TEAMCHAT_RUNTIME` and its live link.
- Fix: in 00634, use `teamchat verify --team <T> <id>` and check that the kind is `human`
  (TC/member/records/records.go:25). File a fedora-desktop request so `verify` covers the
  questions room. Until that lands, say plainly that a questions-room cite cannot be verified.
  The guard then denies; it does not fall back to `history`. Run the subprocess with the
  stamped `TEAMCHAT_RUNTIME` and the kit's binary.

### M-3. Some behaviours still target v1 verbs and v1 files

- Plan: B8 targets `pingbus thread --re <id>` (DESIGN:208, :478-484). B5 reads `pingbus status`
  (DESIGN:205). Skills go to the pingbus kit (DESIGN:260-262). The absent fixture uses "no
  `pingbus` on PATH" (DESIGN:266-267). Terms define ccy-teams as "the pingbus agent-team bus"
  (DESIGN:9). The whole v1 signal table stays in the design (DESIGN:96-106).
- Truth: in v2, `thread` takes only `Q<n>` plus `--ask`/`--close`. It has no `--re`. The
  `--re` flag is on `ask` (TC/member/cli/verbs.go:231-239). A v2 session sets
  `PINGBUS_MEMBERSHIP=off` (TC/../teams/teamchatlaunch/launch.go:189).
- Fix: re-check B8 against the v2 verbs. A top-level reply cannot be made with `thread`, so B8
  may be void; drop it unless a v2 case is shown. Point B5 at `teamchat status` or fedora-desktop
  Plan 00251. Use v2-only fixtures, including one with `PINGBUS_MEMBERSHIP=off` set, to prove
  no v1 path. Remove the v1 table, or move it to an appendix marked "not a contract".

### M-4. Two guards for the host lane: say who owns which layer

- Plan: 00635 Task 2.2 (PLAN.md:110-111, "optional deny-rule second layer"); DESIGN:441-442.
- Truth: QADESIGN:253-256 recommends A: a library handler in hooks-daemon, switched on by
  config. B, the cc deny rules, is a cheap second layer that fedora-desktop owns. Task 5.1
  already passes `--extra-settings <tmp>/cc-deny.json` to a host launch (HOSTCC:182-186, :195).
  PROPOSAL:93-96 and :169-171 have `cc-run` write that file from the seat's duty.
- Fix: 00635 owns only the handler. It drops Task 2.2 and points at fedora-desktop's
  `cc-deny.json` as the static layer. fedora-desktop builds no project handler of its own.
  fedora-desktop should update QADESIGN:258-263 ("UNKNOWN: host-seat signal") to say: the
  signal is `whoami` duty, and the handler is hooks-daemon 00635. Then there is one guard per
  layer.

### M-5. B7 is gated "per team the act concerns", but a QA command concerns no team

- Plan: 00635 PLAN.md:85-86; DESIGN:139-146.
- Truth: the approved question is "is any team of this session `duty == host`"
  (PROPOSAL:155-156). A host-kind member defaults to duty `host` (PROPOSAL:120-121).
- Fix: B7 fires when any held team has duty `host`. Keep per-team gating for acts that name a
  team (B2 cite, B8). Two kinds of "unknown" must stay apart: no answer from the probe (the
  handler says so in its output, PROPOSAL:157-161) and no team at all (silent).

### M-6. Gap: the probe gets no repo or checkout, so "the team the act concerns" for push or merge cannot be found

- Plan: DESIGN:143-145 ("a repo-to-team map from the membership"); 00633 PLAN.md:177-178;
  00634 PLAN.md:16-17.
- Truth: `whoami` gives team, member, role and duty only (PROPOSAL:144). Seats are placed by
  checkout (HOSTCC:48-49), so one session can hold several teams on the same checkout.
- Fix: do not build a repo-to-team map. For checkout-level acts (push, merge) the B2 cite names
  its team (`verify --team T`), and the gate requires `role == coordinator` in that team.
  Profile switches (00633) apply while the seat is coordinator in any team, or are keyed on an
  explicit team argument. Never infer the team from the handle.

### M-7. Gap: the probe must run where the session's run directory exists

- Plan: DESIGN:154-168 (the daemon resolves file paths itself).
- Truth: a ccy run directory is `/tmp/teamchat/<run>` inside the container; a host one is
  `$XDG_RUNTIME_DIR/teamchat/run/<run>` (HOSTCC:75). The `teamchat` binary is on the session's
  `PATH` through the kit (keeper loop.go:161-166). The daemon's `PATH` may not have it.
- Fix: in 00631 Task 1.1, prove that the daemon runs in the session's mount namespace, or run
  `whoami` from the hook side. Call the binary as `$TEAMCHAT_KIT/bin/teamchat`, never the bare
  name from the daemon's `PATH`.

### M-8. 00634 conflicts with 00632/00633 on unattended pushes

- Plan: DESIGN:364-366 (readiness authorises "push; merging a peer branch after its gate");
  00633 PLAN.md:20 (push only if green); 00634 PLAN.md:14-15 (every push needs a cited human
  id).
- Fix: 00634 must accept the verified readiness GO as the cite for the classes that GO
  authorised, until the window expires. Otherwise an unattended coordinator cannot push at
  all. Put this in 00634 and fixture it in 00632.

### M-9. Gap: 00632's readiness command has no source for its seat-liveness and "answerer armed" checks

- Plan: DESIGN:391 ("seat liveness from the seat resolver"); 00632 PLAN.md:15; readiness step 2
  checks the 00523 answerer is armed (DESIGN:357-361).
- Truth: `whoami` carries no liveness (PROPOSAL:144). `status.json` covers only this session's
  own teams (down, stalled; TC/member/state/status.go:42-53). 00523 defines no "armed" signal
  (00523 PLAN.md:30-35, :71-73).
- Fix: take other seats' liveness from `teamchat status` or fedora-desktop 00251. Give 00523 a
  readable armed state (a config key plus a supervisor status line) as a task.

## Minor

- m-1. Today's role set is `coordinator|worker|probe|null`, not two roles (DESIGN:17-18;
  TC/member/state/status.go:79). `null` means "not yet known" and maps to unknown, not to
  absent. A `probe` session switches nothing on.
- m-2. `deployer` (Plan 00207) is replaced by duty `host`. Remove it as a B7 key (00635
  PLAN.md:25; DESIGN:26, :313; 00631 PLAN.md:21).
- m-3. Role changes are owner-approved roster changes, because `coordinator` is a gated field
  (PROPOSAL:37-38). Duty is static for a run (PROPOSAL:162-163). A 60 s TTL is fine. A stale
  `written_ms` is no reason to drop duty.
- m-4. `whoami` exit codes: 0 ok (the list may be empty), 69 no session. The code for an
  unreadable or invalid file is still to be confirmed (65 is the spec's inference;
  PROPOSAL:151-154). 00631's degradation fixtures should use these codes. An empty `teams` list
  counts as absent.
- m-5. A host `cc` session with no team has no duty, so 00635 does nothing there. If the owner
  wants every host session covered, that is fedora-desktop's static layer. fedora-desktop has its
  own gap here: QADESIGN:254-255 says "on every cc launch", but Task 5.1 adds the file on the
  teams path only (HOSTCC:195).
- m-6. 00630 Non-Goals says "no usage ceiling" (PLAN.md:42-43). 00523 Task 2b.2 keeps a
  "ceiling pause" `/compact` (00523 PLAN.md:87-90). Say whether the ruling retires the 00479
  ceiling or only means readiness sets none.
- m-7. 00630 says fedora-desktop 00263 D3 was not read (DESIGN:47-50). Its content matches
  QADESIGN section 3, except that the host-seat signal there is still UNKNOWN (QADESIGN:258).
  Approved `whoami`/`duty` now answers it.

## Overlaps between the children

- 00632 GO gate and 00634 share one verifier. That is fine: the dependency is declared. Add
  M-8's rule there.
- B3 (umbrella) and B12 (00633) are one procedure. Keep it in 00633 only.
- 00633 "standing authorisations" overlaps the daemon's existing `standing_authorisations`
  handler and 00632's recorded authorisation list. Keep one store.

## 00523

No teamchat or v1 assumption. It is independent of teams. Gaps only: an armed signal for
readiness (M-9) and the ceiling wording (m-6).
