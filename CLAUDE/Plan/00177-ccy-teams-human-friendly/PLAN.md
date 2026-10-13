# Plan 00177: ccy-teams, human friendly

**Status**: In Progress (Phase 4: the owner's first-use findings)
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: High

## Overview

`ccy-teams` (Plan 00161) is the owner's manager for agent teams. Its first real use on the
host went badly. It asked for a sudo password with no context. After three failed password
attempts it carried on and said "No agent teams on this machine", which is a false statement
built on a failure it had hidden. With no teams it only said that `create` exists, instead of
offering to run it. Its output has no colour or structure. In `create`, the prompt "Humans of
the team, comma-separated (their Matrix account names)" left the owner asking what it meant.
It does not say these are the people who read and write in the team room through Element,
that the installer makes their accounts, or what a sensible answer looks like. The repos and
subnets prompts have the same problem.

The owner asked (2026-10-13): make it much more human friendly, with colour and formatting;
offer to create a team when there is none; think about the UX; do a full critical pass.

## Goals

- Every failure is reported as what it is. A refused sudo never turns into "no teams".
- No teams: offer to create one there and then, and walk through it.
- Colour and layout that make teams, members, seats, sessions and restore marks easy to scan,
  in line with the other ccy pickers and with the project's interactive-script rules.
- Ask for sudo only when it is needed, saying why first, or remove the need where that is
  safe (for example a narrowly scoped, read-only sudoers rule installed by IaC). Record the
  decision and its security reasoning.
- Every verb and key gives plain-language guidance on what happens next. For example, after
  `create`: what is declared, and that a meta-deploy run installs it.

## Non-Goals

- Changing the bus itself (pingbus, agent-bus, homeserver), except a read-only listing
  path if the sudo decision needs one.

## Tasks

### Phase 1: Critical UX review

- [x] ✅ **Task 1.1**: A full critical pass over `ccy-teams` (every verb, key, prompt, message
  and failure path; first run, no teams, sudo refused or cancelled, no TTY, a team with no
  members). Write the findings into `UX-REVIEW.md` in this folder, each with its fix.

### Phase 2: Build

- [x] ✅ **Task 2.1**: Fix every finding from Task 1.1, with tests in
  `scripts/test-ccy-teams-command.bash`. Update `docs/agent-bus.md` "Getting started" where
  the flow changes. The no-teams offer and the new create flow came first, by the owner's
  order. Then sudo (a scoped read-only sudoers rule in play-agent-bus.yml), the people and
  their passwords, add-repo, colour (CCY 3.94.2: the shared picker passes `--ansi`) and
  help. UX-REVIEW.md's deferred D1 and D2 stay open.
- [x] ✅ **Task 2.2**: The `qa-reviewer` agent on the branch; fix its findings.

### Phase 3: Host

- [x] ✅ **Task 3.1**: Host run through meta-deploy (`play-claude-yolo.yml` for gum, CCY
  3.94.2 and ccy-teams; `play-agent-bus.yml` for the sudoers rule
  `/etc/sudoers.d/agent-bus-read-only`), then the owner's first real `ccy-teams` use. Host
  run passed 26-10-13 (both plays), and again with Plan 00161's D64/D65. The owner created
  a real team with `ccy-teams create`; their findings are Phase 4.

### Phase 4: The owner's first-use findings

- [x] ✅ **Task 4.1**: The create wizard was a wall of text. It now shows numbered steps
  (`1/4  GitHub owner`), a one-line hint each, a ✓ per answer and a short summary without
  `forge_api` or `path_prefixes`. gum's grey key-help line is hidden (gum 0.17 cannot
  recolour it).
- [ ] **Task 4.2**: Create sets up Element itself (the `agent_bus_element_teams` entry
  and its play) and offers to open it. No message tells the owner to edit `localhost.yml`.
- [ ] **Task 4.3**: Offer sign-in straight after create.
- [ ] **Task 4.4**: Run from a repository root, `ccy-teams` offers to start a ccy session
  there as a team member.
- [ ] **Task 4.5**: Seats default to the next free number, and members are always shown as
  `<repo>.<N>`, counting from 1.
- [ ] **Task 4.6**: ccy's offer to re-attach to a detached session keeps `--teams`: it
  attaches only to a session holding the requested seats, and otherwise says why it
  starts a new one (CCY version bump).
- [ ] **Task 4.7**: `path_prefixes` no longer limits paths in an org-wide team.

## Success Criteria

- [ ] On the host, `ccy-teams` with no teams offers to create one, and a refused or cancelled
  sudo is reported as such, never as "no teams".
- [ ] The owner finds it clear and pleasant to use (their judgement).

## Delivery & Milestones

- Plan created; work handed to a sub-agent in a worktree.
