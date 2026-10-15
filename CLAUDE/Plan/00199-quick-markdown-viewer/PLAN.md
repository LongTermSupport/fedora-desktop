# Plan 00199: quick markdown viewer

**Status**: In Progress (md-viewer M0 to M12 merged in its own project; install play written, host run pending; owner's decisions pending)
**Created**: 2026-10-15
**Owner**: joseph
**Priority**: Medium

## Overview

The owner wants a desktop GUI that renders a Markdown file nicely and opens quickly: a
double-click on a `.md` file in Nautilus, or `<tool> file.md` in a terminal, shows it rendered in
a window. The need is reading, not editing. Repositories here are full of Markdown (plans,
journals, docs), so it is read often.

First find out what exists for Fedora on GNOME (Wayland): viewers and editors with a view-only
mode, how each is packaged, how fast it starts, and how well it renders GitHub-flavoured
Markdown. Nothing fitted, so the owner chose to build one: md-viewer, its own repository, in Go
on Gio with goldmark (Task 1.2).

## Goals

- A `.md` file opens rendered, in a window, quickly, from Nautilus and from a terminal.
- It renders GitHub-flavoured Markdown well: tables, fenced code with highlighting, task lists,
  local images and relative links between files.
- It follows GNOME's light or dark scheme.
- It is installed by IaC and set as the default handler for `text/markdown`.

## Non-Goals

- An editor. Editing stays in the owner's editor.
- Rendering remote content or running scripts from a document.

## Tasks

### Phase 1: Research

- [x] ✅ **Task 1.1**: Survey existing viewers (research agent):
  [RESEARCH-markdown-viewers.md](RESEARCH-markdown-viewers.md). Nothing fast, view-only and
  GNOME-native renders well: Glow (dnf) is terminal only; Marka (Flathub, GTK4 and WebKitGTK)
  is the nearest but is an editor with thin maintenance; the rest are editors, Qt, or Electron.
  Startup times there are estimates, not measured.
- [x] ✅ **Task 1.2**: Owner's decision (2026-10-15): build it, as its own public repository
  `LongTermSupport/md-viewer` (MIT), in Go. Linux first, with cross-platform left open.
  Launch-to-render as fast as possible. GitHub-style rendering with scroll, zoom and word wrap.
  Nothing else: no editing, no syntax highlighting, no tabs. Gio for the window and rendering (no
  web engine), goldmark for parsing, go-qa-ci as its QA. Opus writes the plan and spec in that
  repository; Haiku develops it milestone by milestone.

### Phase 2: Adopt or build

- [x] ✅ **Task 2.1a**: md-viewer's spec, architecture and 12 milestones (M0 to M11), written by
  Opus and pushed to its `main`
  ([report](subagent-reports/261015-md-viewer-plan-opus.md)): `docs/SPEC.md`,
  `docs/ARCHITECTURE.md`, `docs/MILESTONES.md`, its own `CLAUDE.md` and
  `.claude/ccy/Dockerfile` (Go plus Gio's libraries, so it builds and tests in its own
  container).

- [ ] 🔄 **Task 2.1b**: The owner's decisions before the build. These are listed at the end of
  `docs/MILESTONES.md`. The two that matter for reading plan files are emoji (the embedded Go
  fonts draw ✅ ⬜ 🔄 as boxes) and dark mode (a `--dark` flag, or the desktop setting read
  over D-Bus).

- [x] ✅ **Task 2.1c**: md-viewer is its own ccy project, with its own hooks daemon, on one
  team with this session. All its milestones, M0 to M12, are merged on its `main` (M10: the
  binary opens a Gio window; M11: speed fixtures, benchmarks and measurement; M12: non-regular
  files never block the viewer, and the tracked-files-only label test). The pin is md-viewer's
  `main` tip at M12, `bafafc8e4ff9`.

- [ ] 🔄 **Task 2.2**: A play installs md-viewer on the host (with Gio's runtime libraries) and
  makes it the default for `text/markdown`. Startup is measured on the host with `--time`.
  The play is written:
  [play-md-viewer.yml](../../../playbooks/imports/optional/common/play-md-viewer.yml). It
  builds md-viewer with `go install` at the commit pinned in `md_viewer_version` (through
  `tasks/build-go-command.yml`, which now takes an external `go_command_module`), after
  upgrading golang and asserting it meets md-viewer's `go.mod`. It checks `--version` names
  that commit, installs a desktop file and sets the `text/markdown` (and `text/x-markdown`)
  default in `~/.config/mimeapps.list`. It is queued in `meta-deploy.list`; the host run and the
  `--time` measurement remain.

- [ ] ⬜ **Task 2.3**: Host run through `meta-deploy.bash`; the owner opens a plan's `PLAN.md`
  from Nautilus and confirms it renders and opens quickly. qa-reviewer over the plan.

## Success Criteria

- [ ] A double-click on a `.md` file opens it rendered, quickly.
- [ ] Tables, code, task lists, images and relative links render correctly.
- [ ] Installed by a play and set as the default for `text/markdown`.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00199-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
