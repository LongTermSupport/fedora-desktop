# n3c W handoff (Plan 00209 N3, slice n3c, step W)

Branch `feat/bus-v2-n3c-w`, worktree `untracked/worktrees/bus-v2-n3c-w`. The first builder
stopped at its context budget. Each commit below passed `qa-changed`. golangci is clean with
and without `--build-tags tuwunel`.

## Done (commits)

1. `a708051da`: the wiring.
   - New package `src-go/internal/teamchat/member/wiring`:
     - `Verbs() map[string]cli.RunFunc`: `run` is `keeper.Main`; `link` is
       `link.Main(link.Wiring{NewTeam, NewEnds})`; the six client verbs are `client.Main(verb)`;
       `seat disconnected` and `reach`.
     - `NewTeam(engine.TeamConfig) (team.Team, error)` is `teamsync.New`, wrapped so that the seat
       directory is watched (`teamsync.WatchSeat`) only while `Run` runs. A team made only for an
       owed leave holds no inotify descriptor. A watch failure stops the team.
     - `NewEnds(engine.EndsConfig) team.Ends` is `ends.New`.
   - `cmd/teamchat` sets `verbs = wiring.Verbs()`.
   - cli:
     - `Outside()` lists the verbs a binary passes in.
     - n3c's verbs are out of `unbuilt`.
     - An outside verb the binary did not pass is 1 `internal`, "not wired into this binary".
   - engine:
     - `Config.NewEnds` is now `func(EndsConfig) team.Ends`, with
       `EndsConfig{Engine, Run, Clock, Log}`. The old `func(team.Engine)` could not give the ends
       their run, as 8a P6 requires.
     - `TeamConfig` gains `Run`, which teamsync needs. The engine and the link's `owed` set it.
   - `notice` uses `proto.StateEvents`; its copy is deleted (8a P5 trust).
2. `766eaeb17`, **a defect found by wiring.** A keeper started with `--state` held its seats
   under that root. The link it started worked out its layout from `--run` alone, so it looked
   for the seats under the home state root and could adopt none.
   - The fix: the keeper's production `linkCmd` passes `--state DIR` on, the `link` verb takes
     an optional `--state`, and `link.layoutFor` applies `WithState`.
   - This is a departure from B's `teamchat link --run <dir>`, and needs a line in SPEC-n3 at
     the merge (decision W-D1).
3. `e2873d54c`: the end-to-end tests over fakehs, in `wiring/e2e_*_test.go`.
   - How they run:
     - `TestMain` dispatches on `argv[0]`: `teamchat` is `cli.Run` over `Verbs()` with
       digest `e2e0e2e0e2e0e2e0`, and `agent` is the CMD.
     - The CMD prints `agent <pid> <TEAMCHAT_RUNTIME>` on the stdout it shares with the keeper,
       then drains stdin.
     - The kit is `<tmp>/k/<digest>/bin/teamchat`, a copy of the test binary, with `agent`
       hard-linked to it.
     - The launch is `run --plain --kind other --state S --seat alpha/2 --kit K -- agent`.
   - The tests, green with `-race -count=5`:
     - `TestE2EItems`: a human's item and a peer's reach recv, once each, and are acked. A
       second recv and inbox are empty.
     - `TestE2EKeeperTerm`: SIGTERM to the keeper gives `left other`.
     - `TestE2ECmdKilled`: SIGKILL of CMD gives `disconnected`.
     - `TestE2EKeeperKilled`: SIGKILL of the keeper makes the link post `disconnected` within
       5 s, measured on `clock.System().Boot()`.
   - Cleanup kills CMD and the keeper, then waits until each link pid named in keeper.log is
     gone, counting a zombie as gone. Without that wait, the link was still writing the store
     while `t.TempDir`'s RemoveAll ran.
   - Coverage: `wiring` 100%, cli 99.1%, engine 94.2%, link 90.9%, keeper 91.0%, notice 92.5%.

## Left

1. **Hot add and hot remove through desired.json.** The test failed and was taken out before the
   commit:

   ```go
   w.desire(1, []state.DesiredSeat{}, []state.Farewell{})
   s := w.launch("--desired", w.desired, "--state", w.state)
   w.desire(2, []state.DesiredSeat{{Team: alphaID, Seat: mySeat}}, []state.Farewell{})
   s.recvUntil(w.say("after the hot add"))
   w.desire(3, []state.DesiredSeat{}, []state.Farewell{{Team: alphaID, Seat: mySeat, Reason: state.FarewellReleased}})
   w.awaitNotice("left")
   ```

   - `desire` writes `state.Desired{Session: "e2e", Generation, WrittenMS: clock.System().WallMS(), Seats, Farewell, Inbound: accept}.Encode()`
     to `<d>/desired.json`, by rename.
   - What happened: the session started, and keeper.log shows `the link is ready` and
     `CMD started`. After generation 2 there is **no keeper.log line at all**: no hot add, and
     no refusal.
   - Not yet known: whether the keeper's desired watch fires on a rename into `--desired DIR`,
     whether its `trustedRead` refuses a test-made directory without logging (which would be a
     silent failure to fix), or whether generation 1 at launch is read.
   - Start in `keeper/prepare.go` `readLaunchDesired` and `keeper/seats.go` (around lines
     190-202).

2. **The Tuwunel e2e** (`//go:build tuwunel`, `FAKEHS_TUWUNEL_BIN`): a `homeserver` from
   `tuwunelhs.Start`, built as in `teamsync/tuwunel_test.go` `tuwunelWorld`, then
   `TestE2EItems` and `TestE2EKeeperTerm` over it. `newWorld` already takes any `homeserver`.

3. **Failpoints under `busfailpoints`.** Each n3c failpoint gets an e2e row, with the checks
   from P1-P11 that apply before n3e (P3, P4, P5, P6, P7, P11). Not started; grep SPEC-n3 for
   the P table.

4. The owner's offline rule end to end: a stopped fakehs (`srv.Stop()`) under a running
   session. It must be shown in `status`, and `recv` must not block.

5. The PLAN.md Task 3.2 progress line. The common rules say builders do not edit PLAN.md, so this
   is left for the lead.

6. The final report, `subagent-reports/261022-n3c-w.md`.
