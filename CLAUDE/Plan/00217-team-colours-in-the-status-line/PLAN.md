# Plan 00217: team colours in the status line

**Status**: In Progress
**Created**: 2026-10-22
**Owner**: joseph
**Priority**: Medium

## Overview

The status line's `👥` rows show every agent team in the same cyan, so a session's teams cannot
be told apart at a glance. This plan gives each team its own colour: an automatic one, derived
from the team's name into a palette of ten colours that read well on a dark terminal, which the
owner can override per team with `colour:` in the team's `agent_bus_teams` entry.

The automatic colour depends on the name alone, so it is the same in every session and on every
machine. The override has to reach every member's status line: ccy containers, the host, LXC
members and members on other machines. The team record is not the way, because a member that
does not know a record key stops trusting the team; the colour therefore travels as its own
admin-written room state event, as the description travels as the room topic, and an older
member simply ignores it. [SPEC-team-colours.md](SPEC-team-colours.md) states every
expectation (C1 to C14).

Both status lines change together: the Go one the kit runs and the Python one that is its parity
oracle ([Plan 00213](../00213-agent-bus-performance-go-carve-out/PLAN.md)), and the oracle is
re-recorded from the Python.

## Goals

- Each team on the status line is drawn in its own 24-bit colour, stable across renders,
  sessions and machines, with at least WCAG AA contrast (4.5:1) on typical dark backgrounds and
  no red, orange, yellow or green (hues the status line already uses for states).
- The owner can set a team's colour by hand (`colour: <name>` or `colour: "#rrggbb"`), refused
  with a clear message when it is unknown, malformed or too dark.
- The Go and Python status lines give the same bytes; the parity oracle is re-recorded through
  its own recorder.

## Non-Goals

- A light-background palette. Future work only (SPEC section 4).
- A 256-colour fallback: the status line has never told terminal colour depths apart (C14).
- A colour field on the `ccy-teams` settings screen. The owner edits the host_vars entry by hand.
- Plan 00209 (v2): its follow-up is recorded below; its files are not changed here.

## Tasks

### Phase 1: Spec and colour module

- [x] ✅ **Task 1.1**: SPEC-team-colours.md, the palette chosen and measured (C1 to C5).
- [x] ✅ **Task 1.2**: `helpers/pingbus/colours.py`, test first: the palette, the contrast,
  hue band and distance tests, the FNV-1a auto colour and the override grammar (C1 to C7).
- [x] ✅ **Task 1.3**: The owner's second requirement, this member's short name on each team piece
  (`alpha: fedora-desktop.2 (c)`), specified as C15 to C18; built with Phase 2.

### Phase 2: The status line, both implementations

- [x] ✅ **Task 2.1**: Python `statusline.py`: a colour span per team, `colour.json` read, the
  `(colour?)` mark and stderr line (C10 to C12), and the member name piece (C15 to C18), test
  first.
- [x] ✅ **Task 2.2**: Go `statusline`: the palette copy with its Python parity and contrast tests,
  the same rendering, the cache fingerprinting `colour.json` (C6, C10 to C13), test first.
- [x] ✅ **Task 2.3**: The oracle: new scenarios in `scenarios.json` (auto colours, an override, each
  refusal, a cache change of the colour), re-recorded with `-update` from the Python, and the Go
  passing it; `writers_test.go` reads a `colour.json` written by the real writer.

### Phase 3: The override's route

- [x] ✅ **Task 3.1**: `teamfile.py` accepts and validates `colour` (C7); `localhost.yml.dist`
  documents it.
- [x] ✅ **Task 3.2**: The admin keeps the `agent_bus.colour` state event (C8); the protocol spec
  names it.
- [x] ✅ **Task 3.3**: The syncer reads it and `inbox.TeamState` keeps `colour.json` (C9).

### Phase 4: Deploy and review

- [x] ✅ **Task 4.1**: Versions the pre-commit hook asks for; the meta-deploy entry
  (play-agent-bus.yml installs the kit's status line and pingbus and applies each team, so its
  admin writes the colour event; the ccy image takes the new pingbus at its next build).
- [ ] ⬜ **Task 4.2**: Adversarial Opus review of the diff until MERGE, then the qa-reviewer agent.
- [ ] ⬜ **Task 4.3**: On the host, after the deploy: every team's row is in its own colour in a
  ccy session, a host session and an LXC member, and a `colour:` set on one team and the play run
  again changes that team's colour in each without a restart.

## Owner Decisions

- **D1, long repository names** (C15, C17). Default taken: the repository part of the member name
  is never shortened; a long one makes a long piece, which wraps onto a row of its own whole.
  The alternative is to cut it past a length (for example 20 characters, with `~` where it was
  cut, ASCII so S27's width measure stays exact) and keep the seat; the cost is that two
  repositories with the same first 20 characters would look alike. Say if you want the cut.
- **D2, a hand-set hex must be legible** (C7). Default taken: `colour: "#rrggbb"` below 4.5:1
  contrast on any reference dark background is refused, as the palette is held to. Say if you
  want any hex accepted.

## Follow-ups

- **Plan 00209 (v2)**: v2's `status.json` `teams[]` gains `colour`, v2's link reads the team's
  colour as v1's syncer does (C9), and `teamchat_teams[]` takes the same `colour` grammar (C7);
  v2's status line keeps C10 to C12. To pick up when 00209 builds its status line (its stage 6b).
- **A light-background palette** (SPEC section 4).

## Success Criteria

- [ ] Two teams on one session show in two different colours, each with C2's contrast.
- [ ] A `colour:` override is drawn by every member's status line after the play, and a bad one
  is refused by the play and by `ccy-teams` with the C7 message.
- [ ] The Go and Python status lines pass the re-recorded oracle; `qa-changed.bash` passes.

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
