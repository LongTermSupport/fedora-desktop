# Plan 00161: agent team bus (Matrix)

**Status**: In Progress
**Created**: 2026-10-06
**Owner**: joseph
**Priority**: High

Implements [fedora-desktop#59](https://github.com/LongTermSupport/fedora-desktop/issues/59).

## Overview

Agent sessions in separate containers have no way to signal each other, so cross-repository
work needs one orchestrator session that carries every repository's toolchain and
credentials. The owner asked to deliver the bus that issue #59 designs: a private Matrix
homeserver per team, installed by a play or a standalone installer on any Fedora desktop or
server, a standard-library CLI (`pingbus`) through which an agent sends and receives
**pings** (a closed verb plus a reference to a committed artefact), free text from the
team's humans aimed at specific agents, hooks that wake an idle session, an opt-in setting
to join a team, and an optional desktop client play.

The owner's answers (Task 1.3) change two of the issue's rules: humans may send free text to
agents, and a team spans hosts and encapsulations. The rest hold: agent-to-agent traffic is
pings only, validated on send and on receive; nothing team-specific is committed to this
repository; opt-in per machine and per team.

## Goals

- A team's homeserver is installed on a Fedora desktop or server with one command (a play,
  or the standalone installer): federation off, sign-ups closed, listening only where the
  team's members can route to it.
- An agent that opts in (ccy session, the bare desktop, LXC, docker or a VM) can
  `pingbus send` and `pingbus wait`/`recv`, and an idle session is woken by a ping. An agent
  can be in several teams.
- Humans watch through Element, from a desktop or a phone, and can address free text to a
  specific agent.
- A versioned protocol specification lives in this repository and the CLI enforces it.
- The privacy acceptance checks in the issue are each proven by a check, not assumed.

## Non-Goals

- Federation, or a homeserver on the public internet.
- Homeservers on anything but Fedora (for now).
- Per-agent forge identities (issue open question 5: revisit later).
- An MCP server.

## Tasks

### Phase 1: Understand and design

- [x] ✅ **Task 1.1**: Six research reports (`subagent-reports/261006-research-*.md`): ccy
  integration, rootless podman services, Python conventions, Tuwunel and the Matrix API,
  Element and terminal clients, waking sessions.
- [x] ✅ **Task 1.2**: [`DESIGN.md`](DESIGN.md) and [`PROTOCOL.md`](PROTOCOL.md), reviewed
  through three lenses (security, feasibility, scope: 9 blockers between them) and revised
  ([revision](subagent-reports/261006-design-revision-opus-5-5.md)). Main changes: room
  roles that Matrix accepts, probes before building, and a host-to-host ping as the first
  milestone. Its control room was superseded by the owner's answers (Task 1.3).
- [x] ✅ **Task 1.3**: OWNER: the questions in DESIGN.md "Owner questions", all answered
  (journal 26-10-06, two entries): teams are themed around a project and span repositories,
  hosts and encapsulations; an agent can be in several teams; docker, LXC and VM members are
  first-class in v1; humans may send free text aimed at specific agents. The homeserver
  placement is mixed: a play and a standalone installer for any Fedora desktop or server,
  installed on every desktop, used where the team's agents are. Reaching it needs only a
  routable address (a private network such as WireGuard is the access control). The event
  prefix must clearly not be a domain name.
- [x] ✅ **Task 1.4**: DESIGN.md and PROTOCOL.md revised for those answers
  ([revision](subagent-reports/261006-design-revision-2-opus-5-5.md)), reviewed again
  through security, feasibility and scope, and every finding applied (none rejected). Main
  changes: one Tuwunel homeserver per team, installed by `agent-bus-install` as a hardened
  systemd service; members of every encapsulation share one per-team bundle; humans'
  addressed text reaches the named agent; the warden is gone; event prefix `agent_bus.`;
  wake through the session inbox socket. Units rebuilt as U00–U28; the wave-1 branches'
  code is reused, not merged.
- [x] ✅ **Task 1.5**: OWNER, answered: (1) an agent may send free text to a human, which
  every agent ignores (not a drop: DESIGN.md D30), and pingbus refuses secret-shaped text; (2) a homeserver may
  listen on any address it is given, and v1 has no TLS (DESIGN.md §3.3 says what that
  costs); (3) Plan 00160 Task 3.3 is done (ccy 3.84.0). Folded into DESIGN.md.

### Phase 2: Build, by milestone (units U00–U28 in DESIGN.md §12)

- [ ] 🔄 **M0 probes**: U00 (host, through `meta-deploy.bash`), U01 (a logged-in child
  `claude`). U00's `triage.bash` is built and in `meta-deploy.bash`; its H4 and H5 already ran
  against a real Tuwunel in the container (journal 26-10-06, U00 finding), and its
  corrections are folded into DESIGN.md §3.6, §3.7, §4 and P7; the review's fixes are in.
  U01 is built as `u01_probe.py` legs of the same `triage.bash` (`--claude-only` runs just
  them), authenticated with the ccy token. Host run 26-10-07 (journal): U01 complete
  (Stop fires per turn; a bypass session needs `crossSessionInbound: accept`; dedupe window
  20-33 s; back-to-back notices are separate turns), and H1's dummy leg reached the bus
  address from every podman network, which settles DESIGN.md §5.3; H2's docker and LXC legs
  reached it too (third run). Open: H6 (Element Desktop is not installed yet, U27) and the
  libvirt guest owner step.
- [ ] 🔄 **M1 host-to-host ping, through the installer**: U02–U11, U13–U17. Built and
  integrated (wave 1): U02 (spec `docs/agent-bus-protocol.md`, `protocol.py`), U03
  (`limits.py`), U04 (`config.py`, its `limits` parsed by U03) and U14 (`teamfile.py`,
  `registry.py`, their grammars imported from `protocol.py`). Wave 2: U05 (`cli.py`, the
  offline commands), U06 (`inbox.py`), U07 (`forge.py`) and U08 (the fake client and
  admin APIs, from recorded Tuwunel fixtures). Wave 3: U09 (`matrix.py`, the client),
  U13 (`bundle.py`, the zipapp builder) and U15 (`helpers/agent_bus/` admin tool, renders
  and the `agent-bus` wrapper). Wave 4: U10 (`syncer.py`, the sync engine and room trust)
  and U16 (`agent-bus-install`, its units, `tuwunel.pin`, the resolver stub), both
  reviewed and fixed; U16 is tested in the container (`scripts/test-agent-bus-install.bash`).
  U16's host run is this plan's `deploy.bash`, through `meta-deploy.bash` (it needs
  `agent_bus_address` in the untracked host_vars, or `--bus-address=`): `software` (which
  stays installed, with `agentbus0`), a throwaway team, both run twice with no `CHANGED`,
  `check`, `remove --purge`, then `triage.bash --reach-only` for H1/H2's dummy leg. Host run
  26-10-07: every installer leg passed, and the play legs too (M3); the third run, with the
  pinned busybox pulled first, passed `deploy.bash` end to end.
  Wave 5: U11 (`cli.py` `send`, `say`, `recv`, `wait`, one long-poll thread per team;
  `test_cli.py` against the fake homeserver) built, reviewed and fixed. Wave 7: U17
  (`acceptance.bash`, M1.1–M1.3, its checks in `acceptance_check.py`, the bus address
  shared with `deploy.bash` through `_bus-address.inc.bash`) built, reviewed and fixed.
  `meta-deploy.bash` runs it after `deploy.bash`; it needs GitHub's API reachable without a
  credential and a fetched upstream branch. First host run (26-10-07) stopped at the
  members' bundle extraction (`tar` under the members' seccomp filter); they now extract
  with Python's `tarfile`. The next run joined both members and carried the first real
  ping (a's `review` to b's `wait`), then lost the ack's event ID to `systemd-run`'s `$`
  expansion, now off. Re-run pending.
- [ ] 🔄 **M2 ccy members, the idle session woken**: U12, U18–U20. U12 built:
  `hooks.py` (the four hooks, offline; the Stop guard), `notify.py` (the inbox socket
  client), `cli.py` `watch`, `hook`, `inbox`, `status`, and the room view (`room.json`)
  the syncer keeps for `status`; `wait` and `watch` publish `listening`. U18 built:
  `files/opt/claude-yolo/optional/agent-bus/` (the plugin with its three hooks and the
  `pingbus` skill, `settings.json`, the `agent-bus-claude` launcher), copied into the kit
  by `agent-bus-install software`; `test_plugin_contract.py`; reviewed and fixed. U01's host
  run confirmed Stop firing and notices under real turns; U20 must confirm the plugin loads
  from `--plugin-dir <kit>/plugin/pingbus`. Wave 7: U19 (the ccy image joins
  teams listed in `PINGBUS_TEAMS` from `ccy.env.local`; container 2.45, CCY 3.85.0;
  `scripts/test-ccy-agent-bus.bash`) built, reviewed and fixed. Its host run, the
  `play-claude-yolo.yml` image rebuild, is the last step of `deploy.bash`; then a ccy
  session with a real bundle. The ccy address (DESIGN.md §5.3) is settled as the bus
  address, as the podman README and `docs/ccy.md` already say (H1's dummy leg, 26-10-07).
  `admin.py` does not accept the address pasta gives `host.containers.internal`.
- [ ] 🔄 **M3 other encapsulations and hosts, the play**: U21–U24. U22 built:
  `play-agent-bus.yml` (imported by `playbook-main.yml`) runs `agent-bus-install software`
  on every desktop, then `remove` for each `agent_bus_teams` entry with `state: absent` and
  `team` for each present one, changed only on the installer's `CHANGED` lines; the
  `localhost.yml.dist` placeholder and four drift pairs in `qa-deployed-drift.bash`.
  `--syntax-check` passes. Its host run is a leg of this plan's `deploy.bash`, through
  `meta-deploy.bash`: after U16's legs, the play with extra vars declaring the throwaway
  team present (the recap counts a change), again (it counts none), then absent with
  `purge: true` (a change). All three passed on the host run of 26-10-07. Wave 7: U21 (`docs/agent-bus.md`, the member kit's
  per-encapsulation READMEs, `test_member_docs.py`) built, reviewed and fixed; its podman
  README was checked against U19's entrypoint as built: it matches, the address included
  (settled by H1's dummy leg, M2). No host run of its own.
- [ ] 🔄 **M4 Element, deploy and acceptance (privacy checks P1–P8)**: U25–U28 (U26, TLS,
  only if probe H7 needs it). Wave 7: U25 (`element.py`, `play-agent-bus-element.yml`,
  `agent_bus_element_teams` in `localhost.yml.dist`) built, reviewed and fixed. Its host
  run is pending: the owner sets `agent_bus_element_teams`, then U27's `meta-deploy.bash`
  entry runs the play and the H6 checks.

## Success Criteria

- [ ] Two ccy sessions in different projects exchange a `review` ping and an `ack`; the
  idle one is woken.
- [ ] A human's message addressed to one agent in Element reaches that agent, marked as
  from that human; no other agent's `pingbus` delivers it, and no agent's free text
  reaches another agent.
- [ ] An agent in a different encapsulation on another host (LXC or VM) is in the same team
  and exchanges a ping.
- [ ] The homeserver makes no outbound connection and answers only on the addresses the
  team's members use (checked, per the issue's privacy list).

## Delivery & Milestones

- <!-- delivery commit hashes -->
