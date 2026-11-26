# Plan 00209: the long task bodies, in full

Moved out of `PLAN.md` so the plan stays within its size limit; the text is unchanged.
`PLAN.md` keeps each task's line, its status and a short statement of what is open, and links to
the section here.

## Task 2.2

The homeserver instance per team (C 3.2): the pinned Tuwunel, the hardened
`teamchat-server@<team>` unit carrying D's `InaccessiblePaths=`, checked by D's gate (seam 49).
Done, as inert code: each team's `config.toml` and drop-in, rendered by `host/render.go` and
written by `team apply`. Not done: the pinned Tuwunel's install and the
`teamchat-server@<team>` unit template (play tasks, gate A, Task 2.5), and the health and
seats unit texts (S7b). `InaccessiblePaths=` is in every unit text Go makes (`v1names`, S7a).

## Task 2.3

Install and apply (C 3.5), the rooms engine from A's specs (C 3.6), the
roster, members, humans and roles; the service-account localparts `conduit` (HC-A6) and
`tuwunel` reserved (A 2.3) (seam 18).
Slices n2a (the rooms engine), n2b (the approval gate, `team apply` step 10), n2d (the member
operations, `role apply`, `team apply` steps 1-9 and 11, `team remove`) and n2f (the
production side of every seam, the `health` and `element` verbs) are built as inert code; what
each covers and leaves out: [BUILT-n2.md](BUILT-n2.md). Owner residual: n2f-D12 ([DECISIONS.md](DECISIONS.md)).
Not built: the play's install tasks and units (gate A), and the member verbs' `-tags tuwunel`
round trip.

## Task 2.4

Seats and bundles in the seat store (C 3.7), the declarations (C 3.8),
Element profiles (C 3.10), health and the status records (C 3.11), backups and restore (C 3.12).
Slices n2b (the declarations of C 3.8), n2c (the seat store), n2e (status records, the health
tick, the disconnected notice, the seat sweep, Element profiles) and n2g (backups and the
restore engine) are built as inert code, with real-server evidence under `-tags tuwunel`;
what each covers and leaves out: [BUILT-n2.md](BUILT-n2.md). Not built: the host_vars writer
(ccy-teams', Task 4.3), the units and timers (gate A), and `restore` in production, which stays
refused (`restore.ErrUnproven`, n2g-D8, until H-C5 and H-C15). The verbs no slice
specified (`status`, `declare check`, `check`, `human *`, `redact`): [SPEC-n2h.md](SPEC-n2h.md), a draft.
The sealed carry of a team to an empty guest (`team seal`, `export`, `import`, `carry-check`,
issue #324; outside DC8, `restore` unchanged): [SPEC-carry.md](SPEC-carry.md). Its build is
complete in code on branch `sealed-carry`, in its section 13's order, with no open question:

- items 01-09: names, archive, records, guards, host exports, the child's carry ops, the seal
  and `team seal`, the sealed export and `team export`, the drill export and
  `team export --drill`; SC1 refuses an entry the export would refuse (sc-D20);
- item 10: `Import` and `team import` (validation, staging, placement, `--resume`,
  `--discard`, a kill at every call; sc-D11, sc-D12);
- item 11: `Check` and `team carry-check` (sc-D13), its drop-in removed after the stop (sc-D14);
- item 12: SG6's `carried_from` marker (sc-D15); item 12b: SG2, a drill import never joins a
  real team (sc-D16);
- item 13: SA1-SA3, `host.Apply` over an imported tree (sc-D17);
- item 14: SH-T1 to SH-T3, the tick and `backup --all` (tests only);
- item 15: the wiring, and a carry through the four verbs over fakehs and, under
  `-tags tuwunel`, over a real Tuwunel (sc-D18); the dispatcher's carry gate judges root's
  verbs only (sc-D21);
- item 16: host check SH1's clause for the drill, its import half pinned by a test (sc-D19).

Pending:

- the play's run on each host that serves teams: it now writes the archive cap file
  (`teamchat_carry_max_archive_bytes`, default 1 GiB, checked at play time), and export
  and import exit 78 on a host until it has run there;
- the `-tags tuwunel` carry test has not been run against a real Tuwunel (its harness now
  sets umask 077 as the host's unit does, so SX3 sees the host's modes; unproven until run);
- the drill, with SH1, has not been run on the scratch guest (the driver's);
- the lab scenario of Q5 (`TeamStateRoots` absent after a purge) is not built: it needs a
  manifest entry and a guest script that applies and purges a team, and its checker must not
  copy the Go list.

Its slice S5 is built in part, for issue #296 (a declared human could not sign in): `human password`, `logout` and `devices` (HU1-HU5, HU13) on the defaults of section 8. `human lock`
and `unlock` wait on n2h-D12 (a roster state `locked`, the owner's) and the real-Tuwunel
probe n2h-D20. HU15's fault rows are built for the account lookup, both roster writes and the
reset (`apply/human_fault_test.go`: each then run again, no second valid password, no password
in any file of the tree). Not done for S5: HU15's injection into the rooms step's calls, its
scan of `ErrChild`'s text and the `execx.Fake` transcript (the verb's child path in
`internal/teamchat/admin`), the `-tags tuwunel` row of HU15, and the rooms step's `changed`
lines in a failed answer.
Of six N2 packages with no caller, n2f gave four one and `ccy-teams seats sweep` gave `sweep`
one (Task 4.2); `declare/hostvars` waits on ccy-teams (Task 4.3):
[BUILT-n2.md](BUILT-n2.md#the-six-n2-packages-and-their-callers).

## Task 2.5

Acceptance: C 3.14's tests; the M44 real-Tuwunel test, and Tuwunel's answers
for a locked and a deactivated account recorded into `fakehs` (host or VM lab); VM-lab scenario
`teamchat-coexist` (P1) to its N2 stage, a host-only team cycle (D 4.3); the install on the host with the footprint before and after (P2); host
checks H-C1 to H-C6 and H-C9 to H-C12 (H-C7 and H-C8 are Task 4.4's); H-C13 to H-C15 (DC16,
DC8) in the VM lab, or on the host once gate B is cleared, except H-C15's clause that a held
ccy session works again after ccy-teams' hot reissue, which needs N3 and N4 and is Task
4.4's; restore stays refused (DC8) until that clause passes too.

- **Gate A** (Task 0.5): the play tasks and units of Tasks 2.2 to 2.4 (and of any later task,
  such as 3.3) merge to F44, and the install on the host runs, only after the owner has
  confirmed OD-A7, DC1, DC5, DC15, OD-D1, OD-D5 and TID. The VM-lab parts of this task do not
  wait.
- **Gate B** (Task 0.5): no pool seat is added on the host (H-C13 to H-C15, less H-C15's
  held-ccy-session clause, where they run on the host) until the owner has confirmed INT6 or
  chosen [DESIGN-v2.md section 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password)
  in its place. The VM lab is not a deploy and does not wait.

## Task 3.1

The N3 spec from area B. It says that net/http may resend a dropped `/sync`
(a GET retried on some connection errors), and that this is safe because a sync with a since
token is idempotent within one database, and across a restore because no answer is persisted
before B 2.6 step 08's epoch check (H-C14); and it carries 00194's 2.0e and 2.0g
([DECISIONS.md](DECISIONS.md#requirements-carried-from-other-plans)).
Done: [SPEC-n3.md](SPEC-n3.md), three Opus review rounds (`subagent-reports/261022-n3-spec-review*.md`,
fix logs `-fixes-1.md`, `-fixes-2.md`); round 3's one major is applied as the reviewer wrote it
(report `subagent-reports/261022-n3-spec.md`). The slices, in order (n3b and n3c may be built at
once after n3a):

- **n3a**: `cmd/teamchat`, the CLI, the member files' codecs, the locks, the kit's layout and
  the guest operations: an LXC guest's side;
- **n3b**: `teamchat-admin lxc apply` and `lxc restart` (C 3.9), the session unit as code, the
  L3 probe and the LXC disconnected notice's child op: an LXC member placed by `lxc apply`;
- **n3c**: `run`, the keeper, the link, the store, the sync engine (SY7 is the resent `/sync`
  and its restore tests on fakehs), receiving, the ends and hot control;
- **n3d**: the sends, the outbox, TIMEOUTs and the forge check: a whole LXC exchange with
  `--plain`;
- **n3e**: delivery into Claude Code, the hooks, the status line, the skill: an LXC Claude Code
  member end to end;
- **n3f**: the side rooms of Plan 00211, from a SPEC-n3 addendum reviewed before it starts.

2.0e: the switch (RS2) is CMD exit 75 alone and is turned on with its host half (Task 4.2);
2.0g: `empty` counts as unheard (UH1). Gate A binds the play, kit and unit placement; N3's LXC
seats are token seats, so gate B binds only the ccy hot reissue's host run (Task 4.4).

## Task 3.2

The `teamchat` binary: CLI, member state, the keeper (`run`) and the link, locks, sync,
delivery, hooks, status line, plugin and skill (B 2.1-2.12). Built, fakes only, none installed:
n3a-n3d; n3e in part (hooks wired, four `--claude` e2e rows, PT1 slices 1-4). Open: n3e's
rest (the OF1 row, PT1's unreached points, seed 57 in issue 156), n3f
(n3f-a to n3f-c2a merged to F44 with pull request 178; n3f-a built: G6 and G7, G4 and G5 (the records
and the filter), G1 whole (the question index: fold and caps with its two post lists, OW5 (b),
the strict codec and load, the build record, `RedactedSlot`, the property tests) and G2 (the
store's seven keys, `MaxStore` 4 MiB); n3f-b built (the reports room): the team goroutine's
half (RP1, RP2, RP4's
verification) on `feat/bus-v2-n3f-b`, and on `feat/bus-v2-n3f-b2` the `report` verb end to end
(RP3, RP5, RP6), the sender's post to the reports room with its own verification (RP4), the
`status` `room` record and the wiring, so the release test fails on `ask`, `thread` and
`questions` only; on `feat/bus-v2-n3f-b3` the recovery of a tried report under a reissued
token (n3f-D16) and the end-to-end rows (OF1-n3f's `report` row, the n3f-D1 stdin cases
through the real binary, RP5's two-member check, a refused join in `status`); n3f-c1 (the
questions room's sync) built: the engine's write of the `questions` key and a demotion's
drop, and in `teamsync` QS1 to QS10 with QS7, QS8's tests, the per-room floor markers,
`Refetch` of a questions item and `status`'s questions record (n3f-c1b, c1c and c1d, pull
requests 168, 171 and 175); n3f-c2a built (QX1's index build and catch-up, pull request 178;
its kill-and-resume and QS7 end-to-end tests are not written); n3f-c2b, n3f-c2c, n3f-d and
n3f-e open, see BUILT-n3.md).

- n3a (CLI, codecs, locks, kit layout, guest ops), n3c (the keeper, the link, receiving, the
  ends) and n3d (the sends, the outbox, TIMEOUTs, the forge check) are built, reviewed and
  merged into the branch; their coverage and reviews are in [BUILT-n3.md](BUILT-n3.md).
- Issue #160: a served op whose state-goroutine step outlives its deadline (CPU starvation)
  answers 4 `timed-out`, never 1 `internal`; the engine's one failure sink holds it
  (`fix/status-internal-160`, class in GoProjectRules.md).
- `restore.OS.ReadDir` lists by name, as `homefs.Root.Entries` does; `project_listing_order`
  now reports a production function that returns a listing unsorted
  (`fix/restore-readdir-sorted`).
- n3d's caps are measured as the files are written (`fix/store-size-as-written`, PR #164,
  reviewed by the coordinator 2026-11-06; [BUILT-n3.md](BUILT-n3.md)). Open, the owner's:
  whether `awaiting` refuses a transcript path heavy in `<>&` (it measures as escaped) or
  `sizeAwaiting` is raised.
- The sender's stale stop (issue #159): a 401 or 403 stops a team's sends only for the token
  the PUT was made under; built on `fix/recover-stale-401-159` ([BUILT-n3.md](BUILT-n3.md)).
- The retry hold (`fix/retry-hold-lift`): a newer sync already lifts it, now pinned; the E2E's
  slow delivery is the team's up-mark at its first long-poll, left to the coordinator ([BUILT-n3.md](BUILT-n3.md)).
- Issue #149: `teamsync` `TestARestartedServerBetweenPolls` restarts fakehs on the test's
  goroutine after its post is answered; rule `project_fake_restart` holds the class
  (`fix/teamsync-restart-149`).

## Task 3.3

LXC members: guest operations (B 2.13), LXC apply from the host (C 3.9),
one `teamchat-session-<...>.service` per seat (n3b-D2: a member is (team, container, checkout,
seat), any number per container); the container's own config stays with its own play (seam 52).
**Gate A** (Task 0.5): its play tasks and units merge to F44 only after the owner's
confirmation.

- The guest side of B 2.13 (GU1-GU5, SA1) is built in n3a; the host side (C 3.9) is n3b's.
- **n3b built** and merged as inert code: `teamchat-admin lxc apply` and `lxc restart`, the
  session unit as code, the L3 probe. Coverage, reviews, the install build's CL2 gate and the
  tagged analyzer stages: [BUILT-n3.md](BUILT-n3.md).
- **The play side** (gate A met): `play-teamchat.yml` reads and shape-checks
  `teamchat_lxc_members`, hands each team its own entries in `team approve` and `team apply`,
  and runs `lxc apply --phase absent` before the removals and `--phase present` after the
  applies, the approval first (DESIGN-v2-C-host.md 3.5.1, as-built note). Go gap closed
  with it: the absent phase passes by a team never applied (no stamp, no record), which a
  team's first play meets (`health.ErrNotStamped`). `teamchat_address` (the `teamchat0`
  address) stays refused: an LXC seat is given the team's address on the bridge, which it is
  served on (Plan 00257 Task B.1 freed the team `url` itself). Held by `installplay/lxc_test.go`; not yet run on a host or in the VM lab. Report:
  `subagent-reports/261117-lxc-play-report.md`.
- **Review fixes**: a team declared absent has every seat its LXC record holds parked and
  removed by the absent phase before `team remove`, which refuses a team whose record holds
  a seat not `left` (one run, as D 5.7 step 5 needs); a present team redeclared unstamped is
  passed by with its leaving seats `DEFERRED`. A team with a present LXC member must name an
  address inside the bridge's subnet in its `url` (`team apply` and `lxc apply` refuse
  otherwise; superseded by Plan 00257 B1: LXC seats are given the bridge address and the
  `url` is free, see DESIGN-v2-C-host.md 3.5.1); the present phase fails on a `reach` line, and the held-back lines show in a task
  of their own. The play's LXC Jinja is rendered by ansible in the gate
  `test-teamchat-play-jinja.bash`. Report: `subagent-reports/261117-lxc-play-fix-report.md`.

## Task 3.5

Delivery debouncing (issue #129): incoming items are held for a quiet
window, 30 s by default with a ceiling, and delivered as one envelope. A halt ping and a
human's direct message bypass the hold. Specified against delivery into a session (B18-B24)
before it is built on the keeper's delivery path; plan only until Plan 00246's Task 8.8 is done.

## Task 4.2

The launcher: the session directory mounts, refusing a checkout that contains
the seat store (seam 53); removing the session record before the release, which runs `teamchat seat disconnected` (seam 54, 27); resolving each team's system, OD-D2's refusal (seam 55).
For 00194's 2.0e (SPEC-n3 RS3), the launcher skips its `seat disconnected` for the run it
relaunches and records it in `held/notice.json`, and the same change turns RS2's switch on.
Built: the end of `entrypoint.sh` ported to Go (`ccy-start-claude`, v1 byte for byte by a
parity test), which starts a v2 member under `teamchat run` (CCY 3.115.0, container 2.82.0).
Built since: `ccy-teams system` (seam 55, L3) and the launch take, mount guard and seat
adapters as Go (`teams/teamchatlaunch`), then wired (CCY 3.116.0): the `ccy-teams internal`
verbs `launch-args`, `seat-check`, `seat-take` and `bus-disconnected`, the exit release, RS3
with RS2's switch on. Then the owner's seat sweep, `ccy-teams seats sweep`, and its
`teamchat-seats.{service,timer}` user units, installed by `play-claude-yolo.yml` (C 3.7.5,
DC21). A real launch on a v2 team is Task 4.4's. Decisions L1-L6: [LAUNCH-ccy.md](LAUNCH-ccy.md).

## Task 4.3

ccy-teams on both systems: the board from the status records (C 3.13, D 3.9),
the flows (C45), creating a team and renaming it (00201 Task 2.16). Built so far:
`choose-teams` for a ccy (podman) launch lists this checkout's teamchat teams (those with a
pool seat here) first, and a teamchat team, listed or named alone, gets its one pool seat or
the one chosen of several; no seat here is refused (78) and teams on both systems are refused
(64) until (a) of Task 4.5. A host (`cc`) launch does the same since Task 5.1 step 6.

## Task 4.4

Acceptance: H-C7 and H-C8, and H-C15's held-ccy-session clause (Task 2.5); the v2-native pilot `teamchat-pilot` with one ccy
session and one LXC member, its cost measured (P3); `teamchat-coexist` run whole, with the ccy
seat (D 4.3). **Gate B** (Task 0.5): the pilot's ccy seat and H-C8's are pool seats, so
neither is added on the host until the owner has confirmed INT6 or chosen
[DESIGN-v2.md section 7.1](DESIGN-v2.md#71-int6s-narrower-alternative-a-pool-seat-with-no-password).
The first real ccy launch on a v2 team, cross-network, was seen outside this repository
(the journal, 2026-11-26); this task's own acceptance (the pilot, `teamchat-coexist`, H-C7,
H-C8) is still to run here.

## Task 4.5

(owner, 2026-11-17: "BUILD IT"): one ccy session that holds a v1 seat and a v2
seat at once, so the coordinator of a v1 team can dispatch to v2 workers without moving the whole
team. This reverses L3's refusal of a session on both systems for a mixed list, never for one
name on both. Design first (DESIGN-dual-bus.md), then build, test first:

- **(a) Launch:** `ccy-teams system` judges each name on its own. A mixed list is a dual
  launch, not exit 64; one name on both systems stays exit 64. The seat check and take run
  per system: the v1 seats through `agent-bus`, the v2 seats through `teamchatlaunch`. A list
  with no v1 team no longer needs `agent-bus` (built ahead of the rest, for the v2 cutover:
  CCY 3.122.22; an agent-bus list on a host without it is refused with 78, naming the play).
- **(b) Start:** `ccy-start-claude` runs the v1 kit step AND starts the session under
  `teamchat run`, one owner of the claude start. The pingbus watcher stays a separate child
  feeding the same inbox socket. L1 changes: `TEAMCHAT_MEMBERSHIP` beside a live
  `PINGBUS_MEMBERSHIP`, not `off`. The stop hooks of both systems run, each judging its own
  pending messages.
- **(c) Exit and restart:** RS3 and seam 54 release or keep the v2 seats, and v1's exit
  handling releases its own, without either judging the other's seats.
- **(d) Restore:** whether the session's conversation survives a relaunch into dual mode
  (ccy's session restore with the seats kept, L5). Proven on a scratch session before the
  coordinator moves.
- **Tests:** the mixed-list decision; the take of all seats or none across both systems; both
  deliveries reaching one fake session; and each system's exit.
  Built after Task 4.4 (the first real v2 launch), whose pilot proves the v2 half this reuses,
  unless the design finds a shared change that has to land with it.

## Task 4.7

The coordinator-retrospective backlog,
[BACKLOG-coordinator-retro.md](BACKLOG-coordinator-retro.md) (R0-R12, v2 only): an owner's
answer delivered to the seat that asked, a needs-owner digest, reply routing, a required
"Why a human:" field, seat capabilities, a one-seat-per-team session lock, team wind-down,
host load visible in the container, and the size and questions-room verify issues. R12 (owner,
Q67) is resumes that can never clash. Its first step, refusing `--continue` with teams, ships
with the ccy caps; then a per-conversation lock for every resume; then `--continue` is banned.

- R0, the questions verbs ([DESIGN-questions-v2.md](DESIGN-questions-v2.md) section 5): all
  8 steps built and merged into v2 (sizes, attachment, the forms; QX3, QX4, QX7 and the
  `uncounted` hook line; the verbs end to end with the link-back; `verify --room`; step 7's
  delivery to the asking seat at 520a0c367: questions membership and power levels, the
  coordinator's relay, the worker's fetch; step 8's skill text). The owner's Lightning check
  HC-Q1 (the design's section 9) is not yet run. DESIGN-v2-B-member.md section 2.2.1's verb
  table matches the built verbs. [Build report](subagent-reports/r0-build-report.md).
  Step 7's [review](subagent-reports/r0-step7-review.md) is fixed on `v2-relay-fix`: a relayed
  item keeps its account in the store and outlives a sync restart, a relay is not
  rate-limited or stale, its `reply:` line is `say --to-coordinator`; the parked seat is a
  known gap (design section 5).

## Task 6.4

(owner, 2026-11-17: "close the door on v1"): the cut-over without
migration, a team at a time, while Tasks 6.1 and 6.2 are unbuilt. A v1 team moves to a fresh
v2 team (no history carried, OD-D4), in this order, each step with its check:

1. The v2 team exists and is approved, with a roster entry for each seat that moves.
2. Each moving machine has the member program (`play-teamchat.yml --tags member`), reaches the
   bus by name over TLS with the team CA, and has the bus in each checkout's network policy.
3. Each seat is issued and placed in its checkout's pool; `ccy-teams system <team>` there says
   `teamchat`.
4. A pilot session per machine: a round trip with every other seat of the team, both
   directions, read in `teamchat history` on each side; a human posts from Element or
   Lightning and a seat sees it.
5. The v1 team's open questions and threads are answered or closed (v2 has no questions
   verbs yet), then every session on it is relaunched on the v2 team only.
6. Only then the v1 team is `state: absent`, purged or not as the owner decides.
   A v1 team with no v2 destination (LXC members, which v2 cannot deploy until Phase 5) stays on
   v1. A host `cc` seat moves like a ccy one (Task 5.1): a pool seat issued with
   `--prefix <repo>+<site>.host`, placed at the cc checkout. No v1 team is retired before its replacement passes step 4.

## Task 6.5

removing v1, D 7.2 steps 4 to 7, once every `agent_bus_teams` entry is
absent: the v1 software and host objects (`agent-bus*`, its sudoers file, network, user and
state roots), its Element profiles and launchers, the v1 kit out of the ccy image (CCY bump)
and the v1 code out of the repository, then the declaration check refusing `agent_bus_*` keys.
Built test first as plays that refuse to run while any v1 team is present. Needs: a teamchat
launch on a host without `agent-bus` (Task 4.5(a), built: CCY 3.122.22), and issue #299 (an
Element profile left behind by a dropped item) fixed first.

## Task 7.5

(follow-up from the landing review, not blocking v2): Defence Before Fix
rules for the three classes fixed at landing without one: "a value the store's check bounds,
produced by code that does not apply that bound" (the floor ids; a second instance is issue
#380, next_batch), "an artefact judged current by its presence, not its content" (the
host kit), and "a teamsync test that reads the team's live state on a batch alone" (the fake
engine hands a batch over from inside Persist, before settle makes its fold live;
`TestQX3APostWhoseNumberIsTakenIsNotPostable` lost that race on CI, fixed by
`running.settled`). Each a project rule in `src-go/qa/rules`, or a test-world change that
removes the race for every test (a batch handed over only after settle), red first, swept,
blocking; or a stated reason it cannot be expressed.
