# Plan 00064: open command universal file opener

**Status**: Dormant (blocked on Phase 4: HOST deploy plus live desktop and terminal checks of the `open` command)
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: Medium

## Overview

Fedora has no single "just open this" command that works everywhere. `xdg-open`
opens the registered default and nothing else — with no default registered it
fails, frequently silently (exit 3, no message), and it never offers a choice.
`gio open` behaves the same. Over SSH both are worse than useless: they try to
launch a graphical app that has nowhere to render.

This plan adds `~/.local/bin/open` — a thin wrapper that keeps the "just works"
path (registered default → launch it) and adds the two things that were missing:
**session awareness** (no display ⇒ only terminal viewers that are actually
installed are offered) and **a chooser when it is not sure** (no default, or an
unrecognised file type ⇒ fzf, or a numbered menu when fzf is absent).

The wrapper delegates rather than reimplements: `mimetype`/`xdg-mime`/`file` for
type resolution, `xdg-mime query default` + `gio mime` for the registered app
list, `gio launch` to start a `.desktop` app, and `mimeopen --ask-default` as the
documented way to register a new default.

## Prior Art Reviewed (Task 1.1)

| Tool                            | Does it fit?                                                                                                                                                                                                                                     |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `xdg-open` (xdg-utils)          | No chooser. Fails silently (exit 3) with no registered default. Session-blind.                                                                                                                                                                   |
| `gio open` (glib2)              | Same as above, different binary.                                                                                                                                                                                                                 |
| `mimeopen` (perl-File-MimeInfo) | **Closest prior art** — does prompt with a numbered menu when no default exists (`-a` always asks, `-d` sets a default). Gap: knows GUI apps ONLY, so over SSH it offers apps that cannot render. Retained as the delegate for setting defaults. |
| `run-mailcap` / `see` (mailcap) | mailcap-driven, ignores the freedesktop desktop-entry database the rest of the system registers against.                                                                                                                                         |
| `handlr` / `handlr-regex`       | Good fit functionally, but not in Fedora's official repos (COPR/cargo only) — not an IaC-clean dependency for this repo.                                                                                                                         |
| `rifle` (ranger)                | Rule-based and terminal-aware, but it is ranger's internal opener and needs ranger's config to be useful standalone.                                                                                                                             |

**Conclusion**: nothing existing covers "default app when known, chooser when
not, terminal-aware when headless". Build the wrapper; delegate everything else.

## Goals

- A single `open` command that handles files, directories, and URLs.
- Registered default is used without prompting when one exists.
- A chooser (fzf, else a numbered menu) when there is no default, or the file
  type is unrecognised, or `-a` is given.
- Never offer a GUI app when there is no graphical session — the same command is
  useful over SSH and on a headless server (`scope: general`).
- Only offer handlers that are actually installed on the box.
- Deployed entirely through Ansible; no manual steps.

## Non-Goals

- Replacing `xdg-open` system-wide or changing any system MIME default at
  install time. `open -d` sets a default only when the user explicitly asks.
- Reimplementing MIME resolution or the desktop-entry database.
- Installing a full media stack (mpv, ffmpeg, ranger, bat, exiftool, 7z…). The
  script probes with `command -v`, so those are picked up automatically if the
  user installs them for other reasons.
- A GUI "open with" dialog — this is a CLI tool.

## Context & Background

- The repo provisions **both** a desktop and a headless server from one tree
  (Plan 00061), so the play is `scope: general` and the script must degrade to
  terminal handlers rather than assume a display.
- Interactive behaviour follows `CLAUDE/InteractiveScripts.md`: strict
  validation with a bounded (3-attempt) re-prompt loop, clean EOF/`q` exit, and
  a hard fail-fast when a chooser is needed but no terminal is attached.
- Output follows `CLAUDE/StderrHygiene.md`: the menu, prompts, and the
  `→ file — handler` status line are on stderr; the chooser's selected index and
  `--dry-run`'s command line are the stdout payload.
- Fedora ships no `/usr/bin/open`, and `~/.local/bin` precedes `/usr/bin` in the
  user's PATH, so taking the name `open` masks nothing.

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Survey prior art (xdg-open, gio open, mimeopen, mailcap,
  handlr, rifle) and record why a wrapper is still needed — see the table above.

### Phase 2: Implementation

- [x] ✅ **Task 2.1**: Write `files/home/.local/bin/open`
  - [x] ✅ MIME resolution chain: `mimetype` → `xdg-mime` → `file`, with a
    fail-fast message naming the packages when all three are absent
  - [x] ✅ GUI candidates: `xdg-mime query default` first, then `gio mime`,
    resolved to `.desktop` paths across `XDG_DATA_HOME`/`XDG_DATA_DIRS`
  - [x] ✅ Terminal candidate table by MIME type, every entry gated on
    `command -v` so only installed handlers are ever offered
  - [x] ✅ Confidence rule: GUI default, or a known type with at least one real
    installed handler ⇒ run it; otherwise ask
  - [x] ✅ Chooser: fzf when present, else a numbered menu with bounded retries
    (`MAX_TRIES=3`), `q`/EOF cancel, prompts on stderr, read from `/dev/tty`
  - [x] ✅ Flags: `-a/--ask`, `-t/--terminal`, `-d/--set-default`, `-l/--list`,
    `-n/--dry-run`, `-h/--help`, `--`, unknown-option fail-fast
  - [x] ✅ Targets passed to handlers as `argv`, never interpolated into the
    command template (filenames with spaces/quotes are safe)
- [x] ✅ **Task 2.2**: Write `playbooks/imports/optional/common/play-open-command.yml`
  (`scope: general`, no guard; resolution deps + terminal viewers; deploy script
  0755; usage summary on completion)
- [x] ✅ **Task 2.3**: Container-side verification (no Ansible in CCY)
  - [x] ✅ `bash -n` + shellcheck (only advisory SC2016 notes, from the
    deliberately-unexpanded handler templates)
  - [x] ✅ Behavioural smoke tests: text/JSON/dir/URL resolve confidently;
    unknown type and known-type-with-no-installed-handler both ask; the no-TTY
    case fails fast with guidance instead of hanging; odd filenames survive
  - [x] ✅ `./scripts/qa-all.bash` green (413 files)

### Phase 3: Documentation

- [x] ✅ **Task 3.1**: Add the play to `docs/playbooks.md` (Common Optional Features)
- [x] ✅ **Task 3.2**: Plan + README index row committed with the code

### Phase 4: HOST deployment and live verification

- [ ] ⬜ **Task 4.1**: (HOST) Run
  `ansible-playbook playbooks/imports/optional/common/play-open-command.yml`
- [ ] ⬜ **Task 4.2**: (HOST) Desktop checks — `open <pdf>` uses the registered
  viewer; `open .` opens the file manager; `open -a <image>` shows the chooser;
  `open -d <image>` registers the default and a subsequent `open` honours it
- [ ] ⬜ **Task 4.3**: (HOST) Terminal checks — over SSH or with `-t`:
  `open <text>` opens `$EDITOR`; `open .` lists the directory; `open <image>`
  renders via chafa; `open <pdf>` via pdftotext; an unknown type shows the chooser

## Dependencies

- Depends on: Plan 00061 (Complete) — `provisioning_profile` / `scope` taxonomy.

## Technical Decisions

### Decision 1: Wrap the existing stack rather than adopt handlr or reimplement

**Context**: Something must resolve MIME types, find registered apps, and launch
`.desktop` entries.
**Options considered**: (A) adopt `handlr` — good UX, but not in Fedora's
official repos, so it needs COPR/cargo and violates the repo's dependency
hygiene; (B) reimplement desktop-entry parsing — large, duplicative, and drifts
from the freedesktop spec; (C) wrap `mimetype`/`xdg-mime`/`gio`, all packaged in
Fedora.
**Decision**: C. Every hard part is delegated to a packaged tool.
**Date**: 2026-07-29

### Decision 2: "Not sure" means ask — and an uninstalled handler is not sure

**Context**: A known MIME type whose handlers are all absent (an `.mkv` on a box
with no mpv) would otherwise fall through to a trivial handler (`file`) and run
it silently, looking broken.
**Options considered**: (A) always run the top candidate; (B) count a known type
as confident only when the type's own branch contributed at least one installed
handler.
**Decision**: B — the candidate count is measured before and after the type's
case branch; a zero delta demotes the type to "unknown", which routes to the
chooser. Caught by the smoke tests, not by review.
**Date**: 2026-07-29

### Decision 3: Require a real terminal before prompting

**Context**: The first smoke run hung: fzf reads `/dev/tty`, which existed in a
non-interactive harness, so the chooser waited forever for input nobody could
give.
**Decision**: The chooser requires `[ -t 2 ]` **and** a readable `/dev/tty`;
otherwise it fails fast naming `--list`, `--ask`, and `mimeopen --ask-default`.
This is `InteractiveScripts.md` rule 11 (non-interactive escape hatch, never
hang on an unanswerable prompt).
**Date**: 2026-07-29

## Success Criteria

- [x] `open` deployed by an Ansible play, no manual steps
- [x] Registered default is used without prompting; `-a` forces the chooser
- [x] Unknown type or no default ⇒ chooser (fzf or numbered menu)
- [x] Headless/SSH never offers a GUI app; only installed handlers are offered
- [x] Non-interactive invocation fails fast instead of hanging
- [x] QA passes (`./scripts/qa-all.bash`)
- [ ] HOST deploy + live desktop and terminal verification (Phase 4)

## Risks & Mitigations

| Risk                                                      | Impact | Probability | Mitigation                                                                                                |
| --------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------------- |
| The name `open` masks another command                     | M      | L           | Fedora ships no `/usr/bin/open`; documented in the play header and docs. `openvt` remains callable.       |
| `gio mime` output format changes                          | L      | L           | Only the `*.desktop` IDs are parsed, not the localised headings; the `xdg-mime` default path is separate. |
| A handler is offered but unusable in the current terminal | L      | M           | Every handler is `command -v` gated; `--list` and `--dry-run` let the user inspect before anything runs.  |

## Delivery & Milestones

- Implementation, playbook, docs, and plan committed together (Phases 1–3)
