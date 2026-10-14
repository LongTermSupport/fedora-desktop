# Plan 00194: Agent bus messages delivered live into a ccy session

**Status**: In Progress
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

A ccy session launched into a team (`ccy --teams`) is told its seat at session start by the
pingbus Claude Code plugin, and `pingbus watch`, started detached by that plugin, is meant to
wake an idle session when a ping or a team message arrives. What reaches the agent is a
count ("agent-bus: N pending"), by design (Plan 00161: hook output carries fixed templates
and counts only); the agent then has to run `pingbus` to read the messages.

The owner wants messages to arrive in the session in real time, with the agent doing
nothing: something running beside the session delivers them. The hooks daemon's ccy
supervisor already takes ccy-specific plugins (`ccy_lifecycle.py`, shipped in the image) and
injects at idle points, which is what that mechanism was built for. This plan designs and
builds a ccy supervisor plugin that delivers the team's traffic into the session as it
arrives, without weakening the bus's trust model: a human's message in the room is an
instruction, another agent's text is a colleague's note to weigh, never one to obey.

## Goals

- A team message or ping reaches an idle ccy session within seconds, as text the agent reads
  without running anything, and a busy session gets it at its next idle point.
- Human text and agent text stay distinguishable to the receiving agent, as the protocol
  requires; nothing an agent sends gains the authority of the human at the keyboard.
- One delivery path: the plugin replaces, or is built on, `pingbus watch`'s wake, never a
  second watcher beside it.

## Non-Goals

- cc sessions on the host (not supervised by the ccy supervisor); they keep the current wake.
- Changing the bus protocol's message types.

## Tasks

### Phase 1: Design

- [ ] 🔄 **Task 1.1**: Establish what works today on a real ccy team session (seat told at
  start; does `pingbus watch` run and wake an idle session; how a human's Element message
  reaches the agent) and write `DESIGN.md`: the supervisor plugin API (as the installed
  hooks daemon defines it), how the plugin gets the session's messages (the inbox, the
  watcher, or the bundle directly), what it injects and how it is framed per sender kind,
  rate and size limits, the failure modes, and how it is tested without a live homeserver.
- [ ] ⬜ **Task 1.2**: The owner agrees the design.

### Phase 2: Build

- [ ] ⬜ **Task 2.1**: The plugin, test first, shipped in the image beside `ccy_lifecycle.py`,
  enabled for a session launched with `--teams` (CCY version bump).
- [ ] ⬜ **Task 2.2**: Fable review; qa-reviewer; meta-deploy; the owner's test from Element.

## Success Criteria

- [ ] On the host, a message the owner posts in Element mentioning a seat appears in that
  ccy session within seconds, with the agent running nothing, framed as the human's.
- [ ] Another agent's message appears framed as a colleague's note.

## Delivery & Milestones

- Design: pending.
