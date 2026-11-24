# Plan 00178: agent team history export

**Status**: In Progress
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: High (it is the step before v1 is retired, Plan 00209 Task 6.5)

## Overview

An agent team's room (Plan 00161) holds everything its agents and humans said: pings, human
messages and agent text. The owner wants a team's history archived in plain formats, JSON and
Markdown. Its first reason was a team being taken down, or simply the record (2026-10-13). Its
present reason is that the history must be exported before v1 is retired: v2 does not import
it (Plan 00209 OD-D4: an export, the v1 archive and a link).

Each v1 team has its own homeserver and an admin login that can read every room. The export
therefore reads each room's events and writes them in two forms. It has a digest that proves
the record whole before any v1 purge.

The behaviour is [SPEC-v1-export.md](SPEC-v1-export.md) (E1-E21). The v1 facts it rests on were
read from v1's code: the team directory, files, owners and modes, the loopback address and port,
the admin token, and the event shapes.

## Goals

- `teamchat-admin migrate export --team <v1-team>` writes a lossless record to a root-only,
  stamped directory. The record holds each room's events as JSONL, its state, a readable
  `history.md` and a `digest.json`. It reads v1 only, over GET on loopback.
- `teamchat-admin migrate export-verify --team <v1-team>` re-hashes a record against its
  digest. Plan 00209 DESIGN-D 7.2 offers a v1 purge only for a team whose export verifies.
- `--copy-to <dir> --copy-owner <user>` also gives the human a copy they own.
- A team is exported now, and again when it is retired. Each record verifies on its own.

## Non-Goals

- Importing the history into v2 (OD-D4).
- Starting or unblocking a stopped v1 homeserver. A team declared `absent` is declared
  `present` again through v1's play before it is exported (SPEC departures).
- The `ccy-teams export` picker action of the first draft of this plan. The export is a root
  verb, run on the host through a plan script.

## Tasks

### Phase 1: Build

- [x] ✅ **Task 1.1**: The spec, SPEC-v1-export.md (E1-E21), written from v1's code.
- [x] ✅ **Task 1.2**: `src-go/internal/teamchat/migrate/v1`, test first. It covers the file
  checks, the loopback client (GET only, no proxy, no redirect), paging to the first event with
  no cap, the JSONL and state writers, the `history.md` renderer over every v1 event shape, the
  digest, the atomic record directory and the verify step, each against a fake homeserver and
  a temporary tree.
- [x] ✅ **Task 1.3**: The two `teamchat-admin migrate` verbs, with the human copy. The
  `teamchat-keeps-off-v1` gate stays green: v1 names appear only in `migrate/v1`. The carry gate
  does not judge them: their `--team` names a v1 team, not a v2 one.
- [x] ✅ **Task 1.4**: A plan script, `export.bash`, run through meta-deploy. It exports and
  verifies every v1 team on the host that is declared present, and names the absent ones. Then
  the `qa-reviewer` agent on the branch. The script is queued in `meta-deploy.list` after
  `play-teamchat.yml`; the review's fixes, and the rule `project_defer_assign_lost`, are in the
  journal.

### Phase 2: Host

- [ ] ⬜ **Task 2.1**: Only one host still holds v1 team data; the others hold none, checked
  read-only. On that host: `play-teamchat.yml` installs the new `teamchat-admin`, then
  `export.bash` runs. Each absent but unpurged team is opened, exported, verified and closed
  again. The owner reads a `history.md`.

## Success Criteria

- [ ] Every v1 team still holding data has a record that `export-verify` passes. Its
  `history.md` reads as the room's conversation. No token or password is in any file.
- [ ] Plan 00209 Task 6.5 can then purge v1 against verified exports.

## Delivery & Milestones

- Re-scoped on 2026-11-24 from a v1 Python verb to a v2 Go verb, ahead of v1's retirement.
