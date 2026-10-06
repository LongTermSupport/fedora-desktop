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
  ([revision](subagent-reports/261006-design-revision-opus-5-5.md)). Main changes: a
  separate control room so human text never reaches an agent account, room roles that
  Matrix accepts, probes before building, and a host-to-host ping as the first milestone.
- [x] ✅ **Task 1.3**: OWNER: the questions in DESIGN.md "Owner questions", all answered
  (journal 26-10-06, two entries): teams are themed around a project and span repositories,
  hosts and encapsulations; an agent can be in several teams; docker, LXC and VM members are
  first-class in v1; humans may send free text aimed at specific agents. The homeserver
  placement is mixed: a play and a standalone installer for any Fedora desktop or server,
  installed on every desktop, used where the team's agents are. Reaching it needs only a
  routable address (a private network such as WireGuard is the access control). The event
  prefix must clearly not be a domain name.
- [ ] 🔄 **Task 1.4**: Revise DESIGN.md and PROTOCOL.md for those answers, re-review, then
  rebuild the unit list. Wave-1 branches kept for reuse: U00 (test only), U01, U02.

### Phase 2: Build, by milestone (units U00–U30 in DESIGN.md §12)

- [ ] ⬜ **M0 probes**: U00 (host, through `meta-deploy.bash`), U01 (container).
- [ ] ⬜ **M1 host-to-host ping**: U02–U11, U13–U18.
- [ ] ⬜ **M2 ccy-to-ccy ping, the idle session woken**: U12, U19–U23.
- [ ] ⬜ **M3 warden and control room**: U24–U26.
- [ ] ⬜ **M4 desktop and acceptance (privacy checks P1–P7)**: U27–U30.

## Success Criteria

- [ ] Two ccy sessions in different projects exchange a `review` ping and an `ack`; the
  idle one is woken.
- [ ] A human's message addressed to one agent in Element reaches that agent, marked as
  from that human; it reaches no other agent, and no agent can send free text.
- [ ] An agent in a different encapsulation on another host (LXC or VM) is in the same team
  and exchanges a ping.
- [ ] The homeserver makes no outbound connection and answers only on the addresses the
  team's members use (checked, per the issue's privacy list).

## Delivery & Milestones

- <!-- delivery commit hashes -->
