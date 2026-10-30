# Plan 00240: team sessions report tooling issues to fedora desktop

**Status**: Not Started
**Created**: 2026-10-30
**Owner**: joseph
**Priority**: High

## Overview

ccy, ccy-teams and pingbus now run in sessions working on other repositories: ccy
containers, LXC members, and other hosts' sessions. Defects in that tooling are found there,
but those sessions do not know the tooling comes from LongTermSupport/fedora-desktop, where
to report a defect, or how to report one without leaking what runs on the machine (the
repository is public, and an issue cannot be retracted). A recent example is a member that
stays stale through a cold boot (issue #115).

The hooks daemon solves the same problem for its own repository with `issue-report`: a
procedure, a generator that collects a controlled set of fields and never gathers hostnames,
remotes, paths or logs, then the filing. This plan gives fedora-desktop's tooling the same,
for every session that runs it.

Where it lands: every team member, whatever runs it (ccy, LXC, docker, vm, host), starts
Claude through `agent-bus-claude`, which loads the kit's pingbus plugin
(`files/opt/claude-yolo/optional/agent-bus/plugin/pingbus/`). A second skill in that plugin
reaches all of them without any new injection route. A ccy session outside any team reaches
it through the ccy image. This is the proposal for the owner to confirm (Q1).

## Goals

- Any session running fedora-desktop tooling can answer "where does this tool come from, and
  how do I report a defect in it", from a skill the tooling itself ships.
- A report is built by a generator from a fixed set of fields (tool, version, the command
  that failed, its exit code and message, with paths, hosts, team and project names replaced
  by placeholders). A hand-written body is refused, as the hooks daemon refuses one.
- Filing is `gh issue create` against LongTermSupport/fedora-desktop where gh is logged in;
  elsewhere the session hands the generated file to its coordinator or a human.

## Non-Goals

- Fixing #115 itself: that is its own fix.
- Reporting defects in the session's own project: this covers fedora-desktop tooling only.

## Questions for the owner

- **Q1**: should the skill ship in the pingbus plugin (it reaches every team member now), or
  be a separate "ccy-teams" plugin that the plugin dir also loads?
- **Q2**: the generator is new tooling, so it is Go (CLAUDE.md). Is it a new `fd-issue-report`
  binary in the kit, or a subcommand of an existing Go binary the kit already carries?
- **Q3**: may a session file directly with gh, or should every report pass through the
  coordinator, or a human, first?

## Tasks

### Phase 1: Design

- [ ] ⬜ **Task 1.1**: Read the hooks daemon's issue-report procedure and generator, and list
  which parts carry over.
- [ ] ⬜ **Task 1.2**: Settle Q1 to Q3 with the owner.
- [ ] ⬜ **Task 1.3**: Fix the field set and the placeholder rules, with tests on real (anonymised)
  captures from #115.

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: The generator in Go, unit tested: it refuses unknown fields and anything
  matching the scrub rules.
- [ ] ⬜ **Task 2.2**: The skill: what the tooling is, where it comes from, when to report, and
  the steps.
- [ ] ⬜ **Task 2.3**: Ship both through the kit (ccy image, LXC kit, host).

### Phase 3: Verify

- [ ] ⬜ **Task 3.1**: In a ccy member and an LXC member, the skill is listed and generates a
  clean body for a scripted failure.
- [ ] ⬜ **Task 3.2**: qa-reviewer.

## Success Criteria

- [ ] A session in another repository, in a ccy container and in an LXC container, finds the
  skill and produces a body that names no install-specific detail.
- [ ] A hand-written or over-full body is refused.

## Delivery & Milestones

- Plan filed.
