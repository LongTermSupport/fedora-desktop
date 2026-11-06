I've added `ccy-teams --graph`, which prints the declared teams as a Mermaid `flowchart` on stdout. It's one commit, `e66808893` on `feat/teams-graph-111` in `<worktree>`, not pushed or merged. `./scripts/qa-changed.bash` passed (go-qa on both modules, docs, and the suites test-ccy-teams-command, test-ccy-container-name, test-meta-deploy and test-qa-changed). It has not run on the host yet; that check is the open Task 15.2.

**What it does**
- **Spelling:** it's a flag beside `-l|--list`, since both print the board; the other modes are verbs.
- **Data:** it reuses the same read as `--list`, plus each team's LXC member record, which the snapshot now keeps so LXC members can be grouped by container. `board.Mermaid` in `src-go/internal/teams/board/graph.go` is a pure formatter over that data.
- **Failures:** it fails the way `--list` does, through shared code. When members need sudo or something can't be read, it still prints the graph, says why on stderr and exits 1. It never runs `sudo -v`.
- **Ids and labels:** ids are numbered by position and use only `[A-Za-z0-9_]`, so names can't collide and `end` is never an id. Labels are quoted and escaped with Mermaid entity codes.
- **Docs:** usage/help, the package doc and `docs/agent-bus.md` (one short example) are updated. Plan 00177 has a new Phase 15 (15.1 ✅, 15.2 ⬜ host check) and a journal entry.

**Differences from the brief**
- **Live state:** ccy-teams can't see pingbus's listening status from the host, because `pingbus status` reads each member's own state inside that member. So the states come from what `--list` already knows:
  - **online:** a seat whose session is running, or that a live process holds.
  - **offline:** a seat whose session has stopped, a free seat, a parked member, or an LXC member whose container was stopped or deferred at the last apply.
  - **unknown, with the reason:** a joined LXC member (checking whether its container runs needs root, which `--graph` never asks for), or a member on another machine.
- **No "stale":** I didn't add a "stale" state because there's nothing to base it on.
- **No rooms:** the team code doesn't know the reports/questions rooms, so they aren't drawn.
- **Shared constants:** I added `kindLxc` as an untyped constant rather than a fourth `board.Kind` value. A typed one made go-qa's exhaustive check demand a new case in `picker/menu.go`.

**Tests, red then green**
- With a stub formatter, the 9 new tests in `board/graph_test.go` failed:
  - a golden test with spaces, quotes, `&`, `<>` and `#` in the names, plus collision-proof ids;
  - a joined LXC member shown as unknown, and unreadable members shown with the reason;
  - the full run on a fake machine: normal output, the sudo case (exit 1, no `sudo -v`), no teams, a read failure and a write failure.
- 3 app tests failed before the wiring: `--graph x` as a usage error, dispatch, and help.
- All pass now, and golangci-lint is clean.

**Example output (abridged, placeholder names)**
```
flowchart TB
  classDef online fill:#dff5e1,stroke:#2e7d32,color:#000
  subgraph t0_team_a["team team-a: what it is for<br/>homeserver up, applied: up to date<br/>port 6167"]
    direction TB
    t0_h0["person person-a<br/>in room, 1 signed in"]
    t0_m0["repo.dev+local.podman (coordinator)<br/>kind ccy, seat dev, role coordinator<br/>checkout ~/repo<br/>online: session ccy-repo running<br/>membership active"]:::online
    subgraph t0_c0["LXC container box-1"]
      direction TB
      t0_m1["site.1+local.lxc<br/>kind lxc, seat 1, role worker<br/>user agent, checkout /srv/site in the container<br/>offline: container box-1 stopped at the last apply<br/>membership active"]:::offline
    end
    t0_m0 -->|dispatches to| t0_m1
  end
```

**Deploy entry**
There's no CCY version bump: ccy-teams is built into `~/.local/bin` by `play-claude-yolo.yml`, not shipped through `files/var/local/claude-yolo/`. I added this to `CLAUDE/Plan/meta-deploy.list`:
```
# Rebuild ccy-teams with --graph (Plan 00177 Task 15.1): expected to pick up play-claude-yolo.yml,
# whose Go build of src-go/cmd/ccy-teams changed.
changed-plays
```