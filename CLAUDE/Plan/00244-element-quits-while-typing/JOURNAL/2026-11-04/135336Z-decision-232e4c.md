## 13:53 · decision · —   — Task 3.2: the owner chose the shared task file

The owner chose the design for Task 3.2 on 2026-11-04, asked as a question with two options: the shared
task file (what was built) over the stricter move (the agent-bus play only checks that Element is there, and
`ccy-teams` runs the clients play first, a Go change to `ops.go`).

Task 3.2 is ticked. What is built and pushed: `tasks/element-desktop.yml`, imported by both
`play-matrix-clients.yml` and `play-agent-bus-element.yml`; `docs/playbooks.md` and the queue entry for the
clients play say so. The host run is the owner's, through `meta-deploy.bash`; nothing was run here.

The plan stays On Hold: the quit investigation (Tasks 1.3, 1.4, 2.1, 2.2) is parked on the owner's word, and
Phase 3 is the only part of it that was ever separate work.
