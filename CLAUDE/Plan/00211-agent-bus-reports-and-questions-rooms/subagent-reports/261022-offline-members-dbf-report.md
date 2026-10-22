# Defence Before Fix report: offline-member-blocks

Branch `fix/offline-members-never-block` (worktree of F44 c159e6626). Defence red `2e0c8aa6e`;
fixes `978d02f45`, `a51a29e17`, `a14af5445`, `f84831b0e`, the Go merge `1e00176bd` (branch
`fix/offline-members-go`, `48959ab50`); plan notes `b37ea0269`, journal `2f3dd9e3a`. After the
conformance review (`261022-offline-members-conformance-round1.md`): the rule widened, red
again on its own, `1d9951f0e`; its fix `96a8fdb6c`. After the adversarial review round 1
(`261022-offline-members-review-round1.md`, over-corrections the rule cannot see): `521e97108`
(check-rooms by live status), `0be44b47f` (wind-down and shutdown by the live container list),
`49256e214` (SPEC/PLAN/owner's steps), `1959e982d` (NOT JUDGED verdicts, R9 by declared
members, the offline prefix, and the rule counting `plan_lxc_online_named`, red first in its
fixture), `617b48bc4` (roles wording by the container list, restart's cleanup hint).

## Specification followed

Method specification 1.1.0, detector specification 1.0.0, toolchain specification 0.2.0, all
read from the cached copies the refresh script listed (`/root/.cache/defence-before-fix/spec/`).

## Toolchain

No manifest declaration of DBF exists in this repository (no `package.json`/`composer.json`
entry; none was added). The project's toolchain is its own QA gates (`scripts/qa-all.bash`,
`scripts/qa-changed.bash`, CLAUDE/QA.md); none is graded in the register. Project rules of this
kind live as stdlib `unittest` detectors under `tests/helpers/`, run by
`scripts/qa-helper-tests.bash`, documented by identifier in CLAUDE/QA.md. Listing: the rule
sections of CLAUDE/QA.md. Identifier resolution: the printed message names
`CLAUDE/QA.md "offline-member-blocks"`. Single-rule harness:
`python3 -m unittest tests.helpers.agent_bus_lxc.test_offline_member_rule`. Calibration assumed:
the owner's rule as recorded in CLAUDE/AgentNotes.md, no exceptions recorded.

## Defect and class

Originating instance: 00211 stage 1's `s2_check` refused a stopped declared LXC container and
stopped deploy 2 ("start it, then run meta-deploy again"). Class: code that treats a declared
team member that is offline (container stopped, session not running, coordinator with no live
status) as a failure or a precondition. Hazard: normal lifecycle blocks a play, deploy,
acceptance or tool, and the owner is told to start a member to go on.

## Rule

`offline-member-blocks`, a reading detector in Python (`tests/helpers/agent_bus_lxc/ test_offline_member_rule.py`), documented in CLAUDE/QA.md and AgentNotes.md. Three detectors:
A, member-reading bash with its own LXC state read or an unguarded `lxc-attach` (no
`plan_lxc_online` or `plan_lxc_online_named` in its plan folder); B, team-member code (Python, Go, bash) whose message
makes starting a member the way on; C, a teams Go flow failing on a non-empty LXC row list.
Excludes comments, completed plans and non-member code. Next wider rule considered: any
`lxc-attach`/`lxc-info` anywhere, or any "not running" text; not built because the lab, VM and
container-management tools enter containers that are not team members and carry no hazard.

Why a test module and not a semgrep rule or a Go analyzer: the class spans bash, Python and Go,
and two of its detectors need more than one file at once (A's plan-folder exemption reads every
bash file of the folder; C joins the list a package fills in one file with the exit it takes in
another). semgrep matches one file at a time and a `go/analysis` rule sees Go only; the
project's established form for such rules is a stdlib `unittest` detector under
`tests/helpers/` with its fixtures in the same file.

Narrowing, recorded. Detector A's exemption: a file in a plan folder that enters a container is
not a finding when another bash file of the same folder calls `plan_lxc_online` on a code line,
because a plan's scripts are sourced together, so the entry is of a container that file listed.
It was added in `978d02f45` together with fixes, not in a commit of its own; that is recorded
here, it is now tightened to code lines of bash files (`1d9951f0e`, a comment or a document
naming the helper no longer counts, with a fixture), and it is written in CLAUDE/QA.md. The
search confirms it: every plan folder it exempts (00211, 00213, 00203) was read, and each
container entry there is of a container from `plan_lxc_online`.

## Proof

Fixtures in the test file carry `# ruleid` marks and each detector is asserted to fire exactly
on them. The sweep test failed at `2e0c8aa6e` with 19 findings in 10 files; that commit holds
the red on its own.

## Independent search

`261022-offline-members-independent-search.md`, dispatched before the rule, without its pattern.
It found 11 instances. Text search found the "start it" / "Start container" messages; reading
alone found shutdown's every-LXC-row-unreached (no offline wording at all) and 00211's
unguarded `lxc-attach` in `lxc_sessions`, which led to detectors C and the attach half of A.
The rule first covered every instance but `restart/jobs.go:265`, which the practitioner judged
not an instance. The conformance review refused that call as outside the practitioner's
authority (the search wins). Detector B was widened (`1d9951f0e`: "then run <tool> again to
start it", with a fixture), went red on it alone, and the instance was fixed (`96a8fdb6c`).

## Sweep and count

Scope: first-party bash, Python and Go: active plan folders, `files/`, `scripts/`, `helpers/`,
`src-go/internal/teams` and the member tools; vendored Go and completed plans excluded. Other
Go under `src-go/internal` (freeze, vmtest, lab and camera tools) and Python outside the
agent-bus and pingbus helpers handle no team member, so they cannot carry the hazard. 19
findings in 10 files at the first red, 1 more at the second. Final: 0.

## Fixes

Each examined individually. 00211 stage 1 and 2 and 00213 C3: `plan_lxc_online`
and `plan_lxc_online_named` (`CLAUDE/Plan/_members.inc.bash`, tested in
`scripts/test-planlib.bash`): offline named on a fixed prefix, not judged; ABORTING and an
unreadable container fail. A check that judged no member prints `ACCEPTED, <check> NOT JUDGED`
(00211 S2, the test question, A3's inbox half, A4, A5; 00213 C3; 00203 R9). `admin.py
check_rooms`: a coordinator not joined is judged by its live `agent_bus.status` in the team
room: none is an `OFFLINE` line, exit 0; a listening one not joined, `leave`, `ban` or any
other membership is a `PROBLEM` (tests in `test_admin_side_rooms.py`, `test_cli.py`); CCY
3.99.14.14, container 2.78.17. `members.py` deferred and summary wording,
`derive.py` unmounted-store wording. Go (sub-agent, `261022-offline-members-go-report.md`):
roles judges an entry by its record handle with no checkout read, and names an unreadable
entry offline only when the container list says stopped; wind-down and shutdown list offline
rows (a record `stopped` is checked against the live container list) and never exit 1 for
them, a running unreachable member still fails. Plan 00203 R9 judges the online containers of
the declared `service: true` entries only, each unit by name. `restart`: a ccy session with an open tmux session and a
stopped container is left as it is, named offline, not a failure, with the cleanup hint
(ccy-sessions ends the leftover tmux session) and no restart asked for. No suppression; no instance
satisfied by rewording alone where it still failed the run. No fix was applied by pattern: the
20 findings are in 13 files of three languages with different shapes, and each was read.

## Decisions referred to the owner

1. `ccy-teams` role change with no LXC member record at all: when a declared LXC entry's
   checkout cannot be read (its container stopped) and no member record exists for the team
   (the play has not run since the record was introduced), the change still stops, because
   nothing else can say whether the handle is that entry's, and passing it over could leave
   the declaration's role wrong. It now advises `ccy-teams apply <team>`, never starting the
   container. Count: 1 path left, every other fixed. Fixing it would take a way to name an
   LXC entry's handle without its checkout or its record (the entry's own `host`/`seat`/`repo`,
   which the play already honours). The rule does not flag it; recorded in PLAN.md Task 2.8,
   and the follow-up that decides an entry giving `host`, `seat` and `repo` by its fields is
   PLAN.md Task 2.9.
2. The board's record-based "container stopped at the last apply" is unchanged because Plan
   00209's acceptance-s1 compares `ccy-teams --list` bytes; recorded as Plan 00209 Task 7.4.

## Permanence

The detector is a test module under `tests/helpers/`, so `scripts/qa-helper-tests.bash` runs it
on every `qa-all.bash` (the push gate); it fails, never warns. `qa-changed.bash` runs it only
when the module itself changes: its routing maps a Python file to its mirrored test module, so
a change to a Go or bash file the rule scans does not rerun it before the commit. Green through
the module run and `./scripts/qa-changed.bash --base F44` (rc 0). `qa-all.bash` is not run by
sub-agents here; the coordinator's full run is the entry-point green.

## Toolchain and detector gaps

No DBF manifest declaration and no register-graded toolchain. The project's tests-as-rules
pattern gives a rule in the project, a single-rule run and a stable identifier in every
message; it lacks a resolver command and a listing command (the identifier names its CLAUDE/QA.md
section; the listing is that file's rule sections). Recorded here as the toolchain owner's gap
under clause 3.2, with the `qa-changed` routing gap above. Detector B bans start-to-go-on
advice on every path, failing or not; review round 1 suggested limiting it to failure paths.
Not done: no instance needs it (the restart hint is cleanup advice and does not match).

## The original defect

Fixed in `978d02f45` alongside the other plan-script instances, not after them, which departs
from the method's order; recorded here. 00211 stage 1 `s2_check` names a stopped container
offline and passes; `acceptance.bash --stage 1 --fake` has the offline legs ("a stopped
container is offline, named and not judged", "every container stopped: not judged"), which
pass, and the running-but-wrong legs are still rejected.
