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

- [ ] 🔄 **Task 1.1**: Map what the bus builds on: ccy networking, mounts and `ccy.env`
  opt-in; how plays run rootless podman services under `systemd --user` (Quadlet); how
  hooks reach a ccy session; Python CLI conventions and tests here; Tuwunel's image and
  config; Element Desktop's per-profile config inside the Flatpak.
- [ ] ⬜ **Task 1.2**: `DESIGN.md` (components, file layout, interfaces, what each play
  installs) and `PROTOCOL.md` (versioned: verbs, event schema, reference forms, validation
  rules, rate limits, exit codes), reviewed before any code.

### Phase 2: Build (tests first, each component on its own branch)

- [ ] ⬜ **Task 2.1**: The protocol validator and `pingbus` CLI (standard library only).
- [ ] ⬜ **Task 2.2**: `agent-team` provisioning and the homeserver play (Tuwunel under
  Quadlet, one per team).
- [ ] ⬜ **Task 2.3**: The warden.
- [ ] ⬜ **Task 2.4**: Waking sessions (skill, Stop and UserPromptSubmit hooks) and the ccy
  opt-in.
- [ ] ⬜ **Task 2.5**: Desktop viewing play (Element Desktop profile per team, terminal
  client).

### Phase 3: Review and deploy

- [ ] ⬜ **Task 3.1**: `qa-all.bash` green; `qa-reviewer` over the full diff; findings fixed.
- [ ] ⬜ **Task 3.2**: `deploy.bash` and `acceptance.bash` (including the privacy checks),
  in `meta-deploy.bash`.
- [ ] ⬜ **Task 3.3**: HOST: create a team, join two ccy sessions, ping between them, and
  command one from Element.

## Success Criteria

- [ ] Two ccy sessions in different projects exchange a `review` ping and an `ack`; the
  idle one is woken.
- [ ] A human `@<agent> !halt` in Element reaches that agent as a `halt` ping; any other
  human text reaches no agent.
- [ ] The homeserver makes no outbound connection and is unreachable from another machine
  (checked, per the issue's privacy list).

## Delivery & Milestones

- <!-- delivery commit hashes -->
