# Plan 00185: the terminal draws the wrong emoji glyphs

**Status**: In Progress (triage.bash written and queued in meta-deploy; waiting on the host run)
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

## Facts (from the repo; the host facts come from triage.bash)

| #   | Fact                                                                                                                                                                                                                                                                                                    | Source                                         |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| F1  | No play installs, pins or configures an emoji font. The Noto Color Emoji on the host is whatever Fedora's default install and `dnf upgrade` put there.                                                                                                                                                  | grep for emoji/noto/fontconfig over playbooks/ |
| F2  | No fontconfig file is deployed by this repo: there is nothing under `files/` for fontconfig, and no play writes `~/.config/fontconfig/` or `/etc/fonts/`.                                                                                                                                               | `find files -ipath '*fontconfig*'`             |
| F3  | The only font plays are `play-ms-fonts.yml` (msttcore fonts via an upstream RPM, plus the `fontconfig` package) and `play-image-watermarking.yml` (`dejavu-sans-fonts`). Neither touches emoji or runs `fc-cache`.                                                                                      | the two plays                                  |
| F4  | `play-terminal-emulators.yml` installs Alacritty, Kitty, Ghostty (COPR) and Foot and writes kitty.conf UX settings, with no font settings. Ptyxis (VTE, the Fedora default) is only touched by `play-gsettings.yml` (tab middle-click). `play-vscode.yml` installs VS Code, whose terminal is xterm.js. | the plays                                      |
| F5  | Some substitutions are 0x60 code points apart (U+1F4C8 to U+1F528); the skin-tone hands fit no code point offset.                                                                                                                                                                                       | #60                                            |

## Hypotheses (each decided by a section of the triage report)

- **H1: the emoji font was replaced under a running terminal.** A `dnf upgrade` of
  `google-noto-color-emoji-fonts` swapped the font file while a terminal kept its old glyph
  table (glyph IDs from one version, outlines or bitmaps from the other). A glyph-ID shift
  fits F5: neighbouring glyph IDs are not neighbouring code points, and skin-tone variants sit
  together in the glyph order. *Confirms:* a `(deleted)` emoji font mapping in "emoji fonts
  mapped by running terminals", or an install time in "emoji font packages and files" later
  than the start time of the terminal showing the bug; and the Task 1.2 fresh window draws
  correctly. *Refutes:* no deleted mapping, font installed before the terminal started, and a
  fresh window still wrong.
- **H2: a stale fontconfig cache.** The cache records the old file's charset and index, so a
  lookup lands on the wrong face or glyph. *Confirms:* a cache file older than the emoji font
  file in "fontconfig cache ages", and the problem surviving a terminal restart but not an
  `fc-cache -r` (Task 1.2). *Refutes:* caches newer than the font.
- **H3: a second emoji font wins the fallback.** A user-installed or other packaged emoji font
  (Twemoji, an old Noto copy in `~/.local/share/fonts`) is first for these code points, or two
  files share the family name. *Confirms:* more than one file in `fc-list :charset=1f4c8`, or
  `fc-match emoji:charset=1f4c8` naming a file that is not the packaged Noto Color Emoji.
- **H4: the renderer's glyph atlas is corrupted (xterm.js#6014 shape).** Only if the terminal
  showing the bug is xterm.js-based (VS Code's terminal) or a GPU atlas terminal. *Confirms:*
  "this run's terminal" names VS Code or a GPU terminal, fontconfig and the mappings are
  clean, and a fresh window clears it without any font change.
- **H5: the terminal reads Noto's COLRv1 table wrongly.** Fedora's Noto Color Emoji is the
  COLRv1 build; a terminal with partial COLRv1 support could draw wrong layers. *Confirms:*
  the bug in one terminal and not in another on the same font, persisting across fresh
  windows and cache refreshes.

### Unverified premises

- That the terminal showing the bug is the one triage.bash runs in. The report records the
  run's own terminal and every running terminal, so either way the right one is covered, but
  the owner should start meta-deploy from the terminal that shows the bug.
- That the bug is still visible at triage time. H1 is only observable while the affected
  process is still running; if it has been restarted, the install time against the session
  start is the remaining evidence.

## Goals

- A named cause, with the evidence.
- A defence in the relevant play (font package, cache refresh, or terminal setting).

## Non-Goals

- Fixing a third-party terminal's renderer upstream (report it, link it, defend here).
- Changing which emoji the repo's own tools print.

## Tasks

### Phase 1: Triage

- [ ] 🔄 **Task 1.1**: `triage.bash`: the terminal emulator and version in use, the Noto Color
  Emoji package version and any other emoji fonts, `fc-match` for the affected code points,
  the fontconfig cache age, and whether GPU rendering is on. Written (with
  `triage-probe.bash`; it also records the VTE version, each terminal's mapped font files and
  the GPU) and queued in `meta-deploy.list`; open until the host report is read.
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
