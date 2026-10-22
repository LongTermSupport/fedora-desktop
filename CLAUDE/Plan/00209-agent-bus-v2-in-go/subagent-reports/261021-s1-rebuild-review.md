Verdict: MERGE

# Plan 00209 stage 1 rebuilt on F44: adversarial review, round 1

Scope: `git diff b18e91f5...HEAD -- src-go scripts` in feat/bus-v2-s1-rebuilt (HEAD 9834696c). Compared
against F44 b18e91f5 (detached at `untracked/scratch/f44`) and the original stage 1 branch
(442143f5..d5ff5e4e). No blocker or should-fix item found; the notes below are for the record.

## What was checked, and held

- `git merge-base --is-ancestor 35acceab HEAD`: exit 1 (not an ancestor).
- `go test ./internal/...` (worktree): all ok. `go test -race` over board, agentbus, teamtest, picker,
  declare, hot, winddown: all ok.
- `bash scripts/test-ccy-teams-command.bash`: `passed: 383  failed: 0` on the branch and on F44; the
  two runs' output is identical line for line (sorted diff empty).
- Parity against the CURRENT (F44) Python, run here: seat `compared 36 fixture checkouts (16 crashes)
  and 175 seat cases`; declare `2056 cases ... 951 answered alike, 1021 refused alike, 84 v1
  tracebacks matched`. `limitCases` (member_test.go) feed the seat parity, so `report_per_hour`
  (in bounds and 61) is judged against v1's `limits.BOUNDS`, which F44 added; `limitBounds`
  (bus/seat/member.go:40-52) equals `BOUNDS` (limits.py:38-47) in keys, bounds and order.
- Live v1 inputs: `declare.py`, `teamfile.py`, `bridge.py`, `config.py` are byte-identical
  442143f5..b18e91f5; `seat.py`'s change is the status-line guard and refresh only; `inbox.probe_lock_at`
  (flock LOCK_SH|LOCK_NB, O_NOFOLLOW, regular-file check) is unchanged and matches bus/seat/lock.go;
  `seat_lines` passes `home=None`, so v1 always printed `-` for owner and dropping `Seat.Self` loses
  nothing (no F44 non-test code read `.Self`). F44's membership/strict.go, desired.go, report.go and
  winddown/*.go are untouched by the branch.
- V40 key vs what `declare status` reads today (declare.py:524-533): host_vars, every `*.json` stamp,
  PIN_FILE, INSTALLER_FILE, `bundle.build` over helpers/pingbus and helpers/agent_bus (bundle.py itself,
  whose SHEBANG F44 changed, is in the helpers/pingbus tree walk), helpers/agent_bus_lxc (imported at
  declare.py:140), the bridge subnet. The only `helpers.*` imports across the three trees are those three
  trees. `teamfile`'s pwd/grp use is `read_stamp_group`, not on the status path. Covered.
- Lost F44 behaviour: board/fetch.go is F44's but for three comments; the seat read still runs in the
  pool through `Client.WithStderr` and the tape (fetch.go:101). `WithStderr` copies the struct, so
  `ListSeats`/`uid` travel with it. `systemPython` (`/usr/bin/python3`) is kept for every remaining run
  and every test line names it. Picker header keeps F44's wind-down action beside stage 1's undo.
  `ops.New(h, b.Statuses, b.Changed)` still forgets the devices (`Changed` = `ForgetDevices("")` +
  `ForgetTeams`). The only `declare.Client` built in production is host.go:203, which `board.Wired`
  wires, so no edit path bypasses `Edited`. SeatList's callers are the same set on F44 as on the stage 1
  base (one moved collect.go -> fetch.go), so B-S2's "every caller names a checkout top" premise is no
  more exposed than when it was reviewed.
- Tests: test functions deleted relative to F44 are exactly the 17 stage 1 deleted relative to its base
  (`comm` of `func Test*` names: identical lists). Added: stage 1's 159 plus three new
  (`TestARemovedSeatIsNotListed`, `TestListOnATerminalIsTheTable`,
  `TestRootReadsFailOnAFailedRunAndOnMalformedLines`). Per file, the +/- lines of each changed file's
  delta equal stage 1's delta except in agentbus_test, verdict_test (bare `python3` -> `systemPython`,
  which makes the failed-run fake actually consulted), fetch_test, chooser/wizard (`/usr/bin/python3` in
  removed lines), hot/list_test (new test), picker_test (wind-down in header), teamtest/world_test (new
  test) and winddown_test. F44's fetch claims are all still asserted for the in-process read: once per
  checkout under two names, four calls held in flight together with peak <= bound (a serialised seat read
  could not reach `together`; the gate keys on `seats|`), bound 2 respected, order kept when the first
  seat read finishes last; plus `noSeatListProcess` in each. winddown's "nothing goes on the bus" allow
  list lost `agent-bus seat list`, so it is stricter.
- V42: re-ran the open/redraw on F44 with a throwaway probe (deleted after):
  `draw 0: map[agent-bus (user) seat list:1 agent-bus human devices:3 agent-bus list:1 declare.py
  limited:1 declare.py overview:3 declare.py undescribed:1 git:3 systemctl:3 tmux:1]` (17) and
  `redraw: map[agent-bus (user) seat list:1 agent-bus list:1 declare.py overview:1 systemctl:1 tmux:1]`
  (5). The report's F44 columns are exact. picker/opencost_test.go pins the branch columns as whole maps
  (`maps.Equal`), so any returning Python read or seat list process fails it: the pin is not vacuous.
- Timing: re-ran `untracked/scratch/261021-s1-rebuild-timing.bash b18e91f5 0d2afc77`: F44 open 706-807
  ms (avg 744), list 121-145; rebuilt open 147-165 (avg 154), list 121-140. Consistent with the report's
  675-759 / 141-173 and its "--list unchanged" claim; both are the fake world, as the report says.

## Findings

1. note: src-go/internal/teams/create/wizard_test.go:542 and :884. F44 pinned that `next-steps` runs
   after the Element step and `teams-for` before the session offer
   (`steps < elem` in TestYesWithEveryValueCreatesInstallsAndPrintsTheNextSteps; two order lines in
   TestWizardRunsItsStepsInTheDocumentedOrder). Both are now in-process and unobservable as calls, so the
   order of the next-steps text against the Element step is no longer asserted (teams-for's place is
   still implied: the offer only appears when it covers). Inherited unchanged from stage 1 (reviewed
   twice). Fix if wanted: record `len(h.err())` at the `gum confirm ... Start a ccy session` call in
   wzScript and assert "is up. Later" lies before it and after the Element output.
2. note: src-go/internal/teams/board/fetch_test.go:270. F44's `failing` made the second seat list fail
   as a command (exit 69, error path); it is now an unreadable seat (`BreakSeat`, the `unreadable` path).
   The in-process error path (crash class, collect.go:423 `reportErr`) is covered by
   TestListWithAnUnreadableSeatFails and TestSeatListCrashIsAnError, but not under the out-of-order gate.
   Its report order is the sequential `seatsRead` loop, so no ordering risk; noting the changed path.
3. note: src-go/internal/teams/picker/helpers_test.go:85. Every picker test now starts with alpha's and
   beta's description offer answered "never" (F44's fake answered `undescribed` with nothing, which no
   host_vars can do). Consequence: the row header test now pins the header WITH "be asked to describe
   alpha again", and the header without a decline record is pinned nowhere in picker_test. Behaviour is
   equivalent for the offer tests (9 call `askAboutAlphasDescription`). Fix if wanted: one header case on
   a rig that removes the record.
4. note: src-go/internal/teams/create/wizard_test.go:1093. TestAFlagValueMayLookLikeAFlag's expected
   exit moved 0 -> 64. Not live drift: F44's 0 came from a fake answering `next-steps --team --yes`; the
   real declare.py would refuse that argv too, and no host_vars can declare a team `--yes`. The assertion
   that the value reaches `create` whole is kept.
5. note: src-go/internal/declare/verdict.go:144-175. The bridge enters the key through Go's
   `net.InterfaceByName`, while v1's status asks `ip -j -4 addr show dev lxcbr0` (bridge.py). A host where
   `ip` is missing or fails while the interface reads fine keeps one key while v1's run would start
   failing; the first status run of the session still reports it, and a changed bridge still changes the
   key. Same class as B-S5 (the interpreter); acceptable, worth one line beside B-S5 in SPEC-view.
