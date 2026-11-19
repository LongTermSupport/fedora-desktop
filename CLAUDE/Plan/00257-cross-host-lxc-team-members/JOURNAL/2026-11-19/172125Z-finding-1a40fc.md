## 17:21 · finding · —

The B.5 review's fixes are done (`subagent-reports/b5-fix-report.md`). ADMIN-1's peer check now
takes the homeserver unit's exact system cgroup. The flaky silent-client test was a fake-clock
due shared by the handshake and first-request limits; `project_shared_due` was widened to every
package's shared limits (12 findings fixed with `AwaitDueFrom`). The terminator's per-source cap
is the team's `connections_per_source` (default 256, coordinator ruling), and a source at its cap
has its dead connections reclaimed by a short keepalive before a refusal. The request gate now
refuses a loose chunk size and HTTP/1.0 with a transfer encoding. HOST-4 is Task B.2d
(coordinator ruling). HOST-7's new refusal is warned of in meta-deploy.list and the user docs.
Open for the coordinator: the ruled default of 256 equals the terminator's slots, so SEC-4's
per-source bound holds only where a team declares a lower cap.
