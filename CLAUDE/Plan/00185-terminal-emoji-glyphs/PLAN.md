# Plan 00185: the terminal draws the wrong emoji glyphs

**Status**: In Progress (the fallback defence is built, Tasks 2.1 and 2.2, and waits on its host run through meta-deploy; the screen check of #60's `printf` is left for Plan 00219's desktop check)
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

- [x] ✅ **Task 1.1**: `triage.bash`: the terminal emulator and version in use, the Noto Color
  Emoji package version and any other emoji fonts, `fc-match` for the affected code points,
  the fontconfig cache age, and whether GPU rendering is on. Host report read; the analysis
  and ranked diagnosis are in [triage-analysis.md](triage-analysis.md). H2 and the strong form
  of H3 are refuted; H1 leads but cannot be confirmed from the report (rebooted since).
- [ ] ⬜ **Task 1.2**: Open item for Plan 00219's desktop check (coordinator default,
  changeable by the owner; it no longer gates Phase 2). At the screen, NOT ESTABLISHABLE by
  a plain script: the four checks
  in triage-analysis.md ("Still unknown"), starting with the issue's `printf` in a new Ptyxis
  window after today's reboot, and whether the wrong glyphs are in colour. They decide between
  H1 and H4/H5. A cache refresh is no longer part of the check: H2 is refuted.

### Phase 2: Defence

**Decision (coordinator default, changeable by the owner):** build the fallback fix the
analysis calls certain and independent of the screen check, plus a "restart terminals"
notice, without waiting for Task 1.2. The dnf5 actions hook for font upgrades (analysis,
part 2) is not built: the docs line in `docs/playbooks.md` is the minimum the analysis names,
and the hook stays the owner's call.

- [ ] 🔄 **Task 2.1**: The defence in the owning play. `play-ms-fonts.yml` is renamed
  `play-fonts.yml` (retired-plays map updated) and deploys
  `files/etc/fonts/conf.d/69-colour-emoji-fallback.conf`: Noto Color Emoji appended weakly
  after `monospace`, `sans-serif` and `serif`, and "en" recorded for it at font scan, with
  `fc-cache -f` and a reopen-terminals notice when the file changes. The append alone is not
  enough: fontconfig's single best match ranks a font supporting the locale's language
  (Symbola, which has Latin letters) above any weak family entry, so the scan edit is what
  makes `fc-match monospace:charset=1f4c8` name the colour font (the ordered list Pango walks
  already follows the append). The check is `scripts/test-colour-emoji-fallback.bash`, a
  `qa-gates.tsv` suite running fontconfig against BDF fixture fonts: red without the file and
  with any one of its four rules removed (each mutant shown red in review round 2), green
  with the file. The same was reproduced in the container with the real fonts (Source Code
  Pro, Symbola, Noto Emoji, Noto Color Emoji, DejaVu) under the stock conf.d: without the
  file Symbola and Noto Emoji win, with it Noto Color Emoji, for Source Code Pro too. The
  known cost: a text symbol no font in the family list has, but an unlisted text font has,
  now comes from the colour font (documented in the file and `docs/playbooks.md`). Open: the
  host run.
- [ ] 🔄 **Task 2.2**: `deploy.bash` (play-fonts.yml, then `acceptance.bash`) and
  `acceptance.bash` (17 checks: the deployed file, the colour font and its scanned "en", the
  single best match for #60's emoji under each generic family and the named monospace text
  font, digits, `#`, `*` and the space kept on the text font, the ordered list, and the same
  for the font Ptyxis is set to, read from its settings, with check marks and the sun kept
  on a text font there). A red check 17 is not a deploy fault but the known cost reaching
  the terminal font, and hands the owner a decision: accept it, or add a text font with
  those symbols to the sans-serif list. Queued in `meta-deploy.list`. Review: round 1
  CHANGES, round 2 MERGE (its should-fix items applied: F44 merged, fontconfig declared in
  the project image and CI), round 3 MERGE (nits applied). Open: the meta-deploy run, `qa-all.bash` and the
  `qa-reviewer` agent.
- [ ] 🔄 **Task 2.3**: The first host run failed 6 of 17 checks (DBF remediation; report in
  [subagent-reports/261023-umask-dbf-report.md](subagent-reports/261023-umask-dbf-report.md)).
  The run started from a `cc` session, whose umask is 077, and Ansible's root `fc-cache -f`
  inherited it, so the system font cache came out 0600 and fontconfig read the desktop
  user's stale cache. Defence: the rule `ansible-umask-pinned` (CLAUDE/QA.md). Fix: the
  `umask_pin` callback, enabled in `ansible.cfg`, runs every Ansible run under 022, and
  `play-fonts.yml` rebuilds the system cache and the user's own when that file changes or
  when other users cannot read a system cache file, which repairs the damaged files on its
  next run. `acceptance.bash` adds check 18 (every system cache file is readable) and triage
  lists unreadable cache files. Open: the meta-deploy run.

## Success Criteria

- [ ] The issue's `printf` draws a line graph, a spool of thread and a package (open item
  for Plan 00219's desktop check, not the owner's).
- [ ] The cause is named in the journal and the defence is in a play.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00185-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #60.
