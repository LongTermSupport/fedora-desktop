# Adversarial review, round 1: fix/offline-members-never-block

Diff reviewed: `git diff F44...HEAD` (13 commits, 52 files). Read-only review.

Runs, all green:

- `python3 -m unittest` over test_offline_member_rule, test_admin_side_rooms, test_cli,
  test_members and test_derive (312 tests, OK)
- `go test ./internal/teams/...` (ok)
- 00211 `acceptance.bash --stage 1 --fake` and `--stage 2 --fake` (both FAKE ACCEPTED)
- `scripts/test-planlib.bash` (rc 0)

The direction is right, and most of the blocking sites are fixed well. `plan_lxc_online` is
small, tested and fails on a name lxc-info cannot read. Stage 1 S2 judges running containers in
full. Shutdown and wind-down still fail on a member in a running container that they cannot
reach.

The weak side is over-correction. Several paths now label a member "offline" because of a
guess drawn from indirect evidence, and pass it, when the member may be running and wrong. The
owner's rule forbids exactly this. The design documents of 00211 were also not brought along.

## Findings

### 1. major: check-rooms passes a running coordinator that never takes questions

`helpers/agent_bus/admin.py:960-962`, docstring `:944-950`, and `docs/agent-bus.md:619-621`.

`asked == "invite"` is printed as a fact: "offline (... its session not running)". A pending
questions invite does not mean the session is down:

- **A running coordinator whose bundle has `human_text: false` leaves the invite pending on
  purpose.** See `syncer.invite_action` returning `INVITE_PENDING` and `_questions_room`
  returning `refused`.
- SPEC §4.5 (SPEC.md:264-269) names this as "the one case left, and it is loud: ... the
  deploy-2 wait leg and **health name it as not joined**". With this branch, both are silent:
  check-rooms says OFFLINE, exit 0, and the wait leg calls a host-member coordinator offline
  (finding 4).
- A running 0.2.0 coordinator whose questions join keeps failing (a `MatrixError` in
  `_join_questions`, which leaves it `pending`) also reads as offline.

This is the "running member that is wrong now passes" case the owner's rule forbids.

**Fix:** decide online or offline from liveness, not from the membership. Members already
publish `agent_bus.status` (`state: listening`, `until`, Unix ms; protocol.py:38, :1109-1126)
in the main room. `check_rooms` reads as admin, so it can read that state:

- invited, with `until` in the future: a PROBLEM ("running but has not taken the questions
  invite: `pingbus status` in its session names why, e.g. human_text false")
- invited, with no live status: OFFLINE

Word the OFFLINE line from what is actually known. Add both cases to test_admin_side_rooms.py.

### 2. major: check-rooms drops a `ban` (or any other membership) on questions without a word

`helpers/agent_bus/admin.py:955-967`.

The old code flagged every `!= "join"`. The new code handles only `leave`, `None`, `invite`
and `join`. A coordinator that is `ban` (or `knock`) in questions now gives neither a PROBLEM
nor an OFFLINE line. The same is true of anything in reports while questions is `invite`.

**Fix:** turn the chain into an exhaustive match. Every membership other than `join`, or
`invite` judged offline per finding 1, is a PROBLEM that names the membership. Add a `ban`
test.

### 3. major: wind-down and shutdown call a member offline from the last apply's record, never from the live container list

`src-go/internal/teams/winddown/winddown.go:369-370` (`byRecord`) and
`src-go/internal/teams/shutdown/lxc.go:45-46`.

A `StatusStopped` item is a member that was joined and was skipped because its container was
stopped *at the last apply*. It still has its handle and its seat. If the container has started
since, its service unit's agent-bus-claude session is running, but:

- wind-down puts it in `idle` ("offline ... nothing to wind down") and exits 0, so a running
  member is never wound down;
- shutdown says "offline, nothing to shut down", exit 0, while its agent runs.

Before this branch, both said "held back" and failed, which was loud. Now it is silent.

**Fix:** send `StatusStopped` items through the same live check as `StatusJoined`, since both
have a container and a handle:

- wind-down: running means reach it; stopped means offline;
- shutdown: running means unreached (exit 1); stopped means offline.

Only `StatusDeferred` with no handle can be offline from the record alone. Add a test with a
record of `stopped` whose container is in the running list.

### 4. major: the 00211 stage-2 acceptance calls every coordinator it cannot drive "offline"

`_acceptance-stage2.inc.bash:183` (find_session) and `:255-262` (rooms_joined, via
`not_joined offline`).

"Offline" here means only "no ccy seat and no online LXC entry". A host member coordinator
(the comment itself says "a host member is not driven from here") may be running. Its questions
join is then not judged at all, and with findings 1 and 2 nothing else judges it. The text
"is offline (no running session this host can drive)" is wrong for it.

**Fix:**

- Word it as "not driven from here (offline, or a host member)" and say the test question is
  NOT JUDGED for that reason.
- For the join, judge every coordinator's ROOM membership the same way regardless of
  drivability. Leave out only one that is offline by liveness, once finding 1 exists. Wait the
  180 s for any coordinator whose status says listening.

### 5. major: the 00211 SPEC, PLAN and owner's steps still describe the refusals that were removed

These now contradict the code:

- `SPEC.md:340` ("It refuses a declared container that is down")
- `SPEC.md:343-344` (pre-flight: "a container that is down is refused, named")
- `SPEC.md:350-355` (the wait's "restart that session, then run meta-deploy again")
- `SPEC.md:268-269` (§4.5, "health name it as not joined")
- `SPEC.md:370-378` (§5.4, "each coordinator is joined to questions, and to reports"; repair
  "restart the coordinator's session")
- `SPEC.md:742-744` (§11.2, "one in no such session, is REJECTED ... it is never skipped")
- `ACCEPTANCE-owner.md:6-7` ("the coordinator's session is not running, the acceptance is
  REJECTED")
- `PLAN.md:192-194` (Task 2.5, "one in no session the host can drive, is REJECTED")
- `PLAN.md:197-198` (Task 2.6 pre-flight "for LXC containers up and new")

Task 2.8's note does not stand in for these.

**Fix:** rewrite each to the new behaviour (offline named, NOT JUDGED, check-rooms OFFLINE
lines), and state in §11.2 that an offline coordinator leaves the test question and A5 NOT
JUDGED until a later run. That §11.2 change is a scope change to Task 2.6's done condition, so
record it.

### 6. major: Plan 00213's acceptance prints "installed everywhere" when C3 judged nothing

`CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/acceptance.bash:110` with
`_acceptance.inc.bash` c3_lxc.

When every declared container is offline, c3_lxc returns 0. The leg is then PASS, and the
verdict reads "ACCEPTED: the Go status line is installed everywhere". This breaks the
convention this branch writes down itself: NOT JUDGED, never PASS (AgentNotes, and the 00211
header). The same holds for 00203's R9 (`_acceptance-robust.inc.bash:268-270`), where the
"not judged" note sits inside a passing leg.

**Fix:** have c3_lxc set a flag, and print `ACCEPTED, C3 NOT JUDGED (no declared LXC member online)` instead of "everywhere". Do the same for R9.

### 7. minor: the wrong repair when questions is joined but reports is not

`helpers/agent_bus/admin.py:966-967`.

The message blames "its pingbus is older than 0.2.0: update it". A pingbus older than 0.2.0
cannot have joined questions (SPEC §4.4: it rejects the invite). The likely cause is a reports
join the syncer failed or that was refused.

**Fix:** "has joined questions but not reports (X): its reports join failed; `pingbus status`
in its session names why, then run the agent-bus play".

### 8. minor: roles calls a failed checkout read "offline" without knowing whether the container runs

`src-go/internal/teams/roles/roles.go:394-397`.

With a record present and the entry not in it, any read error is called "offline", including
a timeout or a broken checkout path in a *running* container. The entry is then passed over,
and when it was the handle, the role is changed live only and the next apply puts it back.
That is low risk (an entry the last apply never saw is rarely the handle), but the wording is
a guess.

**Fix:** say "its checkout could not be read (...)", and keep "offline" for when the container
list says stopped.

### 9. minor: wording that drops the repair

- `_acceptance-stage2.inc.bash:272` now ends with "its session is out of date", which gives no
  action. Say what brings it in line, without making a start the way on. For example: "a
  session started before deploy 1 runs the old pingbus; it joins once relaunched".
- `restart/jobs.go:265-266`: the old hint (end the half-open tmux session in ccy-sessions) was
  removed only to satisfy detector B, on a path that no longer fails. Give the cleanup hint
  back without "then run restart again to start it". Detector B is too broad when it bans
  advice on a non-failing path; consider limiting it to messages on a failure path.

### 10. minor: 00203 R9 lists every LXC container on the host, not the declared members

`_acceptance-robust.inc.bash:240-247`.

`lxc-ls -1` takes in lab or other non-member containers. Each stopped one is then printed as
"offline, so its member unit is not judged", and any one that lxc-info cannot read fails R9.

**Fix:** list the containers of the present `agent_bus_lxc_members` entries, or keep
`lxc-ls --running` and name the stopped declared ones from the host_vars.

### 11. nit: any stderr from lxc-info is reported as an offline container

`_acceptance-stage1.inc.bash` online_containers (and the copy in 00213 c3_lxc).

Every stderr line of a *successful* plan_lxc_online is written to the report as
"<line>: not judged". A sudo or lxc warning would show up as a phantom offline container.
`plan_lxc_online` could print the offline names on a separate fd, or with a fixed prefix the
caller filters on.

### 12. nit: lxc-info states other than STOPPED are all called offline

`_members.inc.bash` treats every state but RUNNING as offline. FROZEN, STARTING, STOPPING and
ABORTING are mid-lifecycle or broken, not offline. Consider naming the raw state (it already
prints `${state,,}`) and treating ABORTING as a failure.

## Checked and fine

- **Version bumps:** CCY 3.99.14.12 to 3.99.14.13, and container 2.78.15 to 2.78.16 in both the
  Dockerfile LABEL and REQUIRED_CONTAINER_VERSION. There is a changelog entry. F44 is at
  3.99.14.12, so there is no collision.
- **meta-deploy.list:** the text matches what the scripts now do (the RUNNING precondition is
  gone, versions updated). Finding 4's wording carries into "an offline coordinator is named,
  not waited for".
- **Streams and errors:** stderr and stdout are kept apart in the new bash (`find_session`'s
  tee goes to stderr, so it does not pollute the capture), and failures still propagate.
- **Public repo:** no install-specific names in the diff.
- **Go:** no error hiding found in the Go diff.
- **Detector:** tests/helpers/agent_bus_lxc/test_offline_member_rule.py fires on its fixtures,
  and the sweep is green. It cannot see findings 1-4, which are over-corrections, not blocks.

## The DBF report's referred decision 1 (roles with no member record)

I agree that the general case cannot be passed over safely now. With no record and an
unreadable checkout, nothing says whether the handle is that entry's. Passing it over would
set the role live only, and the next apply would silently put the old role back. Stopping with
"ccy-teams apply <team>" (never a start) is the right fail-fast.

I do not agree that it "cannot be fixed" at all. The safe subset can be fixed now: an entry
that gives `host`, `seat` and `repo` determines its handle with no read (`members.py:713-717`
already honours it). `declare.LxcMember` lacks `Repo`, so this means adding the field to the
Go declaration read and deciding such an entry by its fields before the record or the
checkout. Record it as a follow-up task in 00211 PLAN.md (not a blocker for this merge). The
remaining case is rare: it needs no record, because the play has not run since the record was
introduced.

VERDICT: FIX-FIRST
