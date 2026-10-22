Verdict: MERGE

# Plan 00209 stage 1: seat-read-failure branch, review round 2 (cea276e4..03189e40)

## What I ran

- `go test ./internal/teams/... ./internal/bus/...` in src-go: all green. `go vet ./internal/teams/...`: clean.
- Mutations with `go test -overlay`, so no tracked file was edited:
  - M1: makeSeat ignores the SeatList error after seat take. TestAddSaysWhenTheSeatListCannotBeReadAfterTheSeatIsMade goes red.
  - M2: waitHeld drops heldErr from `why`. TestAddOfATeamAlreadyInSaysWhenItsSeatCannotBeShownHeld goes red.
  - M3: seatHeld ignores its SeatList error (round 1's surviving mutation). Both of the tests above go red.
  - M4: the `r.CrashSeat` line deleted from the after-seat-take test. That test goes red.
  - M5: the `t.Cleanup(refuseInProcessReads)` line deleted from teamtest.New. Every test stays green (finding 1).
  - M6: inProcessRead's seat-list branch disabled. TestOnRefusesTheSeatList and TestAnInProcessReadRunAsAProcessFailsTheTest go red.
- `bash -n` and `shellcheck -x` on acceptance-s1.bash, \_acceptance-s1.inc.bash, \_s1-shim.bash, meta-deploy.bash and test-meta-deploy.bash: clean. `bash scripts/test-meta-deploy.bash`: 19 passed.
- `scripts/qa-bash-limits.bash` with base cea276e4: OK, so the lowered allowlist record for meta-deploy.bash (162/0/181) is exact.
- A meta-deploy harness on scratch fixtures for the paths the suite does not run. A failing deploy with an acceptance gives FAIL deploy + SKIP acceptance and no extra row. A failing deploy-only plan gives one FAIL row. A passing deploy-only plan gives one PASS row and exit 0. A `<plan>/deploy.bash` entry exiting 2 gives UNSET, and the same deploy run through its plan folder gives FAIL (finding 2).
- An acceptance harness: a scratch repo tree whose `_planlib.inc.bash` sources the real library with no container markers and `PLAN_SUDO_CMD=(true)`. It runs a copy of acceptance-s1.bash with only the not-root check removed, and the real \_acceptance-s1.inc.bash and \_s1-shim.bash. Fake `go` writes old or new fake ccy-teams by whether the tree has internal/bus; git history is real (before / stage 1); a session record and a report_per_hour member.json exist. Outcomes:
  - no pty: S3 "no terminal", exit 2;
  - under `script` (pty): ACCEPTED, exit 0, with old/new order alternating and the load average in each line;
  - new open asks `gum confirm` first: S3 NOT JUDGED, exit 2;
  - --list differs in every round: REJECTED, exit 1;
  - --list differs in one round only: a note, ACCEPTED.
- The same tree through a copy of meta-deploy.bash: no pty gives `UNSET 00209-x-acceptance-s1.bash (exit 2 — COULD NOT ESTABLISH …)` and exit 1; with a pty it gives PASS and exit 0. So the open's /dev/tty path works through meta-deploy's `| tee` pipeline.
- I grepped the added lines for install-specific names (home paths, usernames, hostnames, real IPs): none. The only addresses are 192.0.2.x and 2001:db8::.

## Round-1 findings: each verified

01. **Fixed.** The OnFunc now does what the rig's take does (`r.m.exists[...]`) before CrashSeat. M4 (no crash) turns the test red, so the crash is what the test depends on. The suggested no-crash control case was not added, but M4 proves the same thing.
02. **Fixed.** makeSeat names the read error (add.go:288-291), and waitHeld carries heldErr (add.go:397-400). Both tests assert the reason ("the seat list cannot be read: agent-bus seat list in …"). Round 1's surviving mutation (M3) is now red.
03. **Fixed.** `judge` records a 2 in NOT_JUDGED, not as a failed leg. The script exits 2 when nothing failed but something was not judged, and REJECTED still names the not-judged legs. The header, the usage text and the list entry all say exit 2 / COULD NOT ESTABLISH, and say that an out-of-date team blocks S3 until play-agent-bus.yml has applied it. Shown end to end in the harness.
04. **Fixed.** A --list difference rejects only when it happens in every round; a difference in some rounds only is a reported note. Shown both ways in the harness. The list entry and the header say so.
05. **Fixed.** The play-claude-yolo.yml entry now says it rebuilds ~/.local/bin/ccy-teams with stage 1.
06. **Fixed.** `run_order` alternates old and new between rounds. `/proc/loadavg` is printed beside every open line and every median.
07. **Fixed.** `scriptable` guards World.On/OnErr/OnDeclare/OnDeclareEdit for the seat list and the declare reads. A cleanup in New fails the test on any such read that ran as a process, however it was scripted. Its registration is untested (finding 1).
08. **Fixed.** Every write and capture inside the checks has `|| return 1`. A member.json that cannot be read is named and fails the leg. sha256sum is captured and checked before say_report. Left unchecked: the `$(git ls-tree …)` inside `[[ -n … ]]` at inc:113 and the `$(run_order …)` word lists, both harmless (the rev was verified one line earlier).
09. **Fixed.** The tty probe's error is captured (`ttyErr`) and printed in the stderr message (inc:251-253).
10. **Fixed.** The header S1 says "old Go against new Go over the current Python" and that a stage which changes Python cannot reuse it as is.

## Confirmed sound

- **meta-deploy.bash.** `unit` and `status` are set at the top of every loop pass, so no `set -u` hazard. `deployOk` and `triageArgs` are only used in the branch that sets them. The `*:` case arm swallows a unit-less plan, so a deploy failure counted in place is not counted twice. Playbook (`*.yml`) exit 2 stays FAIL. The plan-folder flow (triage before, deploy, triage after, acceptance, skipped acceptance counted bad) behaves as before. The acceptance UNSET row now comes from the shared case rather than an inline one, with the same text.
- **plan_gather_leg runs each check with `set -e` off.** Every check returns an explicit status. `judge` passes a 2 through as a recorded not-judged and returns 0, and passes any other non-zero through. check_open's early return on a question is `bad ? 1 : 2`, so an earlier failure is not hidden by a later not-judged. `set -u` stays on in the legs. Every expansion is defaulted or set, and the empty associative array in check_report_per_hour is fine on bash ≥ 4.4.
- **Exit-2 path in acceptance-s1.bash.** The EXIT trap still drains the run log, and the report and run log are named. An `exit` in the script itself does not touch R14, which governs the library.
- **Session records.** registry.go writes `dir=` with a trailing newline, so the `read` loop in S4 sees every line.

## What only the host can prove

- The installed ccy-teams is byte-for-byte this checkout's build (S0), and the old revision builds offline from the vendor tree (S1).
- On live teams, --list prints the same bytes, and how often a live change touches a pair (age-word granularity is not checked here).
- The real timings under the host's load.
- The real open reaches fzf first, with no description or update question, and starts exactly one `declare status` and no seat list. The old binary starts a seat list and at least two declare.py runs, which shows the shims see them.
- A live member.json with report_per_hour exists and is listed.
- sudo priming and reading the member rosters as the desktop user.

## Findings

1. **nit: the cleanup that refuses an in-process read run as a process is never shown to be registered.** src-go/internal/teams/teamtest/world.go:96.
   Evidence: M5 deletes `t.Cleanup(func() { refuseInProcessReads(t, w.Fake) })` from New, and every test in ./internal/teams/... stays green. TestAnInProcessReadRunAsAProcessFailsTheTest calls refuseInProcessReads directly, then swaps `w.Fake` so that the real cleanup cannot fire. The commit's claim "however it was scripted" rests on a line no test holds.
   Fix: take the registration out into a function over a small interface (`Cleanup(func())` plus `errorer`), e.g. `refuseOnCleanup(tb, w)`. Call it from New, and test it with a fake TB that collects cleanups, runs them, and expects the Errorf.
2. **nit: exit 2 is reported as COULD NOT ESTABLISH for every `<plan>/<script>.bash` entry, deploy scripts included.** CLAUDE/Plan/meta-deploy.bash:241 and meta-deploy.list:16-19.
   Evidence: in the harness, a `<plan>/deploy.bash` entry exiting 2 is listed as `UNSET … nothing failed, but some check had no evidence`, and the same script run through its plan folder is `FAIL … (exit 2)`. The current queue has two deploy-shaped script entries (00213's deploy.bash and 00211's deploy-stage-2.bash). Both end through plan_deploy_leg / plan_finish (exit 1), so nothing is mislabelled today, and the batch still exits 1. But a deploy script whose last command is ansible-playbook would pass the play's 2 on, and that would read as "nothing failed".
   Fix: apply `2:` only to units whose script name matches `*acceptance*.bash` (or `*triage*`/`*acceptance*`). Or state in PlanScriptStandards R9 that a plan script's exit 2 means "not established" only, so a deploy script never exits 2.
3. **nit: the closing line counts an UNSET unit as "failed or were skipped".** CLAUDE/Plan/meta-deploy.bash:256.
   Evidence: the harness summary for the not-judged run reads `1 of 1 unit(s) failed or were skipped.` above a row that says nothing failed. This was already true for an acceptance UNSET, and it now covers every script entry.
   Fix: "… failed, were skipped or could not establish."
4. **nit: when the seat list cannot be read, waitHeld keeps polling for the whole Waits.Apply and drops whyNot.** src-go/internal/teams/hot/add.go:388-401.
   Evidence: the error is carried into `why` (round-1 finding 2 is fixed), but a reader failure that will not go away still costs the full apply wait. When both reads fail, the report error from whyNot is replaced rather than kept beside it.
   Fix (optional): append rather than replace (`why + "; the seat list cannot be read: " + …`). Or return at once when heldErr repeats on consecutive polls.
5. **nit: the report prints "first screen fzf ," with a stray space.** \_s1-shim.bash:20 together with \_acceptance-s1.inc.bash:267/272.
   Evidence: stamp_screen writes `"%s %s %s"` with an empty third field for fzf, so `cut -f2-` gives "fzf ", as the harness output shows.
   Fix: in stamp_screen, write `${1:+ $1}` instead of ` ${1:-}`.
