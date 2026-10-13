# Plan 00185: the terminal draws the wrong emoji glyphs

**Status**: Not Started
**Created**: 2026-10-13
**Owner**: joseph
**Priority**: Low

Addresses #60.

## Overview

The terminal draws some emoji as other emoji (#60). U+1F4C8 📈 shows as a hammer, U+1F4CA 📊
as a kitchen knife, and others as hands with assorted skin tones. The text underneath is
right: copying it gives the intended characters, and the bytes were decoded and checked. It
hits every program in the terminal and reproduces with a plain
`printf '\U0001F4C8 \U0001F9F5 \U0001F4E6\n'`. Some substitutions are exactly 0x60 code
points away, but others fit no offset, which points at glyph lookup or rendering rather than
at the characters.

The issue's leading suspects are a stale or corrupted glyph cache: the renderer's GPU texture
atlas (xterm.js has a known bug of this shape, xtermjs/xterm.js#6014), or a stale fontconfig
cache or mismatched Noto Color Emoji in a native terminal. Which terminal it was is not
recorded, so the cause is unknown and this plan triages first. The fix, once known, is a
defence in the plays so a provisioned desktop cannot get into this state again.

## Goals

- A named cause, with the evidence.
- A defence in the relevant play (font package, cache refresh, or terminal setting).

## Non-Goals

- Fixing a third-party terminal's renderer upstream (report it, link it, defend here).
- Changing which emoji the repo's own tools print.

## Tasks

### Phase 1: Triage

- [ ] ⬜ **Task 1.1**: `triage.bash`: the terminal emulator and version in use, the Noto Color
  Emoji package version and any other emoji fonts, `fc-match` for the affected code points,
  the fontconfig cache age, and whether GPU rendering is on.
- [ ] ⬜ **Task 1.2**: Reproduce with the issue's `printf` in a fresh window and after a
  font cache refresh and terminal restart (the owner at the screen; NOT ESTABLISHABLE by
  script, so listed for the owner).

### Phase 2: Defence

- [ ] ⬜ **Task 2.1**: From the cause, the defence in the owning play, with a check that
  would have caught it.
- [ ] ⬜ **Task 2.2**: `deploy.bash`, `acceptance.bash`, an entry in `meta-deploy.bash`;
  `qa-all.bash`; the `qa-reviewer` agent.

## Success Criteria

- [ ] The issue's `printf` draws a line graph, a spool of thread and a package.
- [ ] The cause is named in the journal and the defence is in a play.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00185-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #60.
