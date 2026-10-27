# Host results at cad6b5053: Plan 00232 acceptance and reclaim 2.0.4

Branch `plan/00232-00192-host-results`: `9de31f8f0` (00232), `bf397b0f8` (00192).

## Plan 00232

The acceptance rerun was ACCEPTED, with every check passing. Task 3.5 and the acceptance
success criterion are ticked. The served deploy, acceptance and changed-plays entries are gone
from `meta-deploy.list`.

- **F45, `--connect` refreshing other projects' sessions: intended.** The network connect
  covers only the project it runs in. `refresh` (`internal/netpolicy/app.go`, N9) walks every
  live session's state directory and recomputes each allow set from that session's own
  networks, so a session whose networks did not change gets the same set again. Written up in
  DESIGN.md section 10 and in docs/ccy.md under "Network policy".
- **F46, seated headless sessions not ending when their input closes: a real behaviour of a
  seated session, not the acceptance's driving.** np232c is driven the same way but has no
  seat, and it ended with exit 0. The seated two did not end in either run. Exit 143 comes
  from the teardown's SIGTERM. Their stream output shows no background task armed. The seated
  sessions carry the bus delivery path (inbox socket and watcher), and which part keeps them
  alive is not yet known. The teardown wording now says this is expected, and Task 3.6 is
  added to find the cause with a probe.
- **Left for 00232:** the final `qa-reviewer`, the owner's points, Task 3.6, and `qa-all` and
  a push from the coordinator.

## Plan 00192

reclaim 2.0.4's recheck passed and is recorded in PLAN.md and PORTS.md. In 2.0.5, a No to
either "Also remove all unused ... (-af)?" question now prints `Skipped <command>.`. Before,
it printed "Kept the unused images.". The tests failed first, then passed after the change.
SPEC E22 is updated. A changed-plays entry (play-cli-tools.yml) is queued for the option 6
recheck.
