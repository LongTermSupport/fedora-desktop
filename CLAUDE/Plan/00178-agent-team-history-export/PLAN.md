# Plan 00178: agent team history export

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

## Overview

An agent team's room (Plan 00161) holds everything its agents and humans said: pings, human
messages and agent text. The owner wants to archive a team's history in a plain format,
JSON and Markdown, before taking a team down or simply for the record (2026-10-13: "not
urgent but we should do it at some point").

The bus already holds an admin login per team that can read the whole room, so the export
is a read of the room's events, written in two forms.

## Goals

- `agent-bus export <team>` (a root admin verb, like the others) pages the team room from
  its first event with the team's admin token (`/messages`, `dir=f`).
- It writes two files:
  - `events.jsonl`: the raw room events, lossless.
  - `history.md`: readable. Each line gives the time (UTC), the sender's display name and
    handle, the kind (a ping's verb and its ref, a human message, agent text, a system
    event) and the body.
- The human chooses where it lands. The default is
  `~/agent-team-archives/<team>-<UTC stamp>/`, offered and accepted on Enter.
- `ccy-teams export <team>`, and an "Export history" action in the picker.
- The files are owned by the human, mode 0600, and contain no token or password.
- Removing a team in `ccy-teams` offers an export first, beside the backup offer.

## Non-Goals

- Importing an export back into a team (a backup and restore already exist for that).
- Exporting other teams' rooms, or anything beyond the one team room.

## Tasks

### Phase 1: Build

- [ ] ⬜ **Task 1.1**: `agent-bus export` in `helpers/agent_bus` (paging, both writers),
  tested against a fake transport, including an empty room and a room longer than one page.
- [ ] ⬜ **Task 1.2**: `ccy-teams export` and the picker action, with the destination
  prompt (gum, home default); the removal flow offers it. Tests in
  `scripts/test-ccy-teams-command.bash`.
- [ ] ⬜ **Task 1.3**: `docs/agent-bus.md` documents it; the `qa-reviewer` agent on the branch.

### Phase 2: Host

- [ ] ⬜ **Task 2.1**: Host run through meta-deploy (the plays that install `agent-bus` and
  `ccy-teams`), then an export of a real team, read by the owner.

## Success Criteria

- [ ] On the host, `ccy-teams export <team>` writes both files where the owner chose. The
  Markdown reads as the room's conversation, and no secret is in either file.

## Delivery & Milestones

- Plan created; not started (low priority, after Plan 00161's D64 and Plan 00177).
