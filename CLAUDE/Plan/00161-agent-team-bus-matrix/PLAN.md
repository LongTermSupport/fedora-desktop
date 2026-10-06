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
homeserver per team on the desktop, a standard-library CLI (`pingbus`) through which a
session sends and receives **pings** (a closed verb plus a reference to a committed
artefact, never free text), a deterministic **warden** that turns human `!` commands into
pings, hooks that wake an idle session, an opt-in ccy setting to join a team, and an
optional desktop client play.

The issue's hard rules hold throughout: the bus carries pointers, never content; pings are
validated on send and on receive; human text never reaches an agent; nothing team-specific
is committed to this repository; opt-in per machine and per team.

## Goals

- A team can be created on the desktop with one command: its homeserver runs rootless,
  bound to host-local addresses only, federation off, sign-ups closed.
- A ccy session that opts in can `pingbus send` and `pingbus wait`/`recv`, and an idle
  session is woken by a ping.
- Humans watch and command through Element, and only `!` commands reach agents, as pings.
- A versioned protocol specification lives in this repository and the CLI enforces it.
- The privacy acceptance checks in the issue are each proven by a check, not assumed.

## Non-Goals

- Federation, or any homeserver reachable off the machine by default (a remote homeserver
  is "just a URL" and must be TLS; supported by the CLI, not provisioned here).
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
- [ ] 🔄 **Task 1.3**: OWNER: the questions in DESIGN.md "Owner questions". Answered
  (journal 26-10-06): teams are themed around a project and span repositories, hosts and
  encapsulations; an agent can be in several teams; docker, LXC and VM members are
  first-class in v1; humans may send free text aimed at specific agents. Still open: where
  a team's homeserver runs and over which private network, and the event namespace.
- [ ] ⬜ **Task 1.4**: Revise DESIGN.md and PROTOCOL.md for those answers, then rebuild
  the unit list.

### Phase 2: Build, by milestone (units U00–U30 in DESIGN.md §12)

- [ ] ⬜ **M0 probes**: U00 (host, through `meta-deploy.bash`), U01 (container).
- [ ] ⬜ **M1 host-to-host ping**: U02–U11, U13–U18.
- [ ] ⬜ **M2 ccy-to-ccy ping, the idle session woken**: U12, U19–U23.
- [ ] ⬜ **M3 warden and control room**: U24–U26.
- [ ] ⬜ **M4 desktop and acceptance (privacy checks P1–P7)**: U27–U30.

## Success Criteria

- [ ] Two ccy sessions in different projects exchange a `review` ping and an `ack`; the
  idle one is woken.
- [ ] A human `@<agent> !halt` in Element reaches that agent as a `halt` ping; any other
  human text reaches no agent.
- [ ] The homeserver makes no outbound connection and is unreachable from another machine
  (checked, per the issue's privacy list).

## Delivery & Milestones

- <!-- delivery commit hashes -->
