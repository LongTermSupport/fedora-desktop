# Plan 00211 Phase 1: complete in code, not deployed

Phase 1 (the member side, deploy 1) is written, tested and on the Phase 1 branch. Nothing of
it is on the host yet. Task 1.13 (merge to F44, the owner's run) is what is left.

## Every Phase 1 task and its commit

| Task                                                                                    | Commit(s)                                                  |
| --------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| 1.1 `protocol`: index, back-link, power levels, join rules, room-aware validation       | adb84a3b (RED), 418ae31c                                   |
| 1.2 `inbox`: room per item, `rooms.json`, `reports.json`                                | b3a1a873 (RED), fd4dbd61                                   |
| 1.3 `syncer`: side-room trust, reports join, invite rule, coordinator filter, questions | b3a1a873 (RED), fd4dbd61                                   |
| 1.4 coordinator `human_text: false` refused (`members.py`, `admin`); `status` ROOM line | fd4dbd61, 1306354e (RED), e3e7718d                         |
| 1.5 `limits`: `report_per_hour`                                                         | b3a1a873 (RED), fd4dbd61                                   |
| 1.6 `cli`: `report`, `ask-human`, `thread`, `questions`, `history --room`, HUMAN ROOM   | 55f64e19 (RED), a5486643                                   |
| 1.7 `envelope`: `room:`, `question:`, `revises:`                                        | 55f64e19 (RED), a5486643                                   |
| 1.8 hooks role lines; the pingbus skill                                                 | 1306354e (RED), e3e7718d, 393e7e7c                         |
| 1.9 container bump per image-touching commit (SPEC §1, §5.6 corrected)                  | fd4dbd61, then each commit after                           |
| 1.10 Go: `agent-bus list` skips ROOM lines                                              | a016e287                                                   |
| 1.11 `docs/agent-bus-protocol.md`, `docs/agent-bus.md`                                  | a016e287, a5486643                                         |
| 1.12 `acceptance.bash --stage 1` (+ `--fake`), `deploy.bash --stage 1`, pingbus 0.2.0   | the commit that adds this report                           |
| 1.13 merge to F44, queue, journal the run                                               | queued in that same commit; the merge and the run are left |

Phase 0's spike (Task 0.1) is a016e287. Versions now: pingbus 0.2.0, container 2.78.5, CCY
3.99.13.5.

## What is deployed by which entry

`CLAUDE/Plan/meta-deploy.list`, in this order, each with its description comment:

1. `00211-agent-bus-reports-and-questions-rooms/spike.bash` (Task 0.1). A throwaway Tuwunel
   on loopback with scratch accounts; changes no system state. It records whether the
   restricted join, the power-level refusals and a pending invite behave as the design needs.
2. `00211-agent-bus-reports-and-questions-rooms/deploy-stage-1.bash` (deploy 1). It runs
   `deploy.bash --stage 1`, whose legs fail fast:
   1. the whole `play-agent-bus.yml`: the host's pingbus 0.2.0 and every running LXC member's
      kit. No room is created, because the admin code is Phase 2 and not on F44;
   2. the whole `play-claude-yolo.yml`: the staged pingbus and skill, the launcher, and the
      ccy image rebuilt once (container 2.78.5);
   3. `acceptance.bash --stage 1`: S1 checks the host's `/usr/local/bin/pingbus` and the staged
      `/opt/claude-yolo/optional/agent-bus/pingbus`. S2 checks each container of a present
      `agent_bus_lxc_members` entry. Each must print this checkout's `pingbus version` line. A
      declared container that is down is refused and named.

The wrapper exists because meta-deploy passes no arguments to a unit script. A plan-folder
entry would run `deploy.bash` and `acceptance.bash` with no `--stage`, and both refuse that.
The `--fake` acceptance passed in the container (`./acceptance.bash --stage 1 --fake`).

## What the owner runs

1. Merge Phase 1 to F44 **before any Phase 2 code** (SPEC §5.3).
2. `./CLAUDE/Plan/meta-deploy.bash`. It needs sudo once. It runs the spike, then deploy 1.
3. Restart every team's agent sessions, ccy and LXC alike. A running session keeps the
   pingbus it started with. Deploy 2 waits for each coordinator to join the questions room,
   and only the new pingbus does that.
4. Remove both 00211 entries from `meta-deploy.list` once they have passed, and journal the
   results (Task 1.13).

meta-deploy does not stop the batch when a unit fails, so deploy 1 still runs after a failed
spike. That is safe: deploy 1 creates no room, and only Phase 2's design depends on the spike.
Before Phase 2 starts, read the spike's report.

## Known open items

- **The Phase 0 spike result is pending on the host.** Only `Syncer._reports_room` depends on
  the restricted join. If the spike says no, that method changes, and so does the fake's
  `_restricted_allows`.
- **D9 needs the owner's sign-off**: extend v1 Python now, or wait for Plan 00209's Go port.
  Phase 1 is built on "extend Python".
- Phase 2's `acceptance.bash --stage 2` and `--after-owner` will each need a wrapper like
  `deploy-stage-1.bash` to be queued in meta-deploy.
