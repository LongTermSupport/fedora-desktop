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
Markdown. If nothing fits, build a small one: per the tooling rule it would be Go, with GTK4 and
WebKitGTK rendering HTML made by a GFM-capable Markdown library.

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
- [ ] 🔄 **Task 1.2**: Recommend: build a small viewer. GTK4 and libadwaita with WebKitGTK 6.0,
  a GFM Markdown library, a file monitor for live reload, the GNOME colour scheme followed. The
  tooling rule makes it Go (gotk4 and its WebKitGTK bindings, goldmark as `qa-docs` uses); the
  research sketched it in Python. The owner decides: build in Go, or try Marka first.

### Phase 2: Adopt or build

- [ ] ⬜ **Task 2.1**: Adopt: install by a play (dnf or Flathub, as the repo installs others),
  and make it the default for `text/markdown`. Or build: a spec of expectations first, then the
  tool in `src-go/`, tests first, built and installed by a play.
- [ ] ⬜ **Task 2.2**: Host run through `meta-deploy.bash`; the owner opens a plan's `PLAN.md`
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
