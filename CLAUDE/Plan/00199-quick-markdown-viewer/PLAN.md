# Plan 00199: quick markdown viewer

**Status**: In Progress (research under way)
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

- [ ] 🔄 **Task 2.1**: md-viewer: the spec, architecture and milestones (Opus), then the build,
  milestone by milestone, tests first (Haiku), in its own repository. The window needs Gio's
  system libraries, which this container lacks, so md-viewer's CI builds and proves the binary.
  Its go-qa-ci step fails until go-qa-ci is public.
- [ ] ⬜ **Task 2.2**: A play installs md-viewer on the host (with Gio's runtime libraries) and
  makes it the default for `text/markdown`. Startup is measured on the host with `--time`.
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
