# Plan 00199: quick markdown viewer

**Status**: In Progress. md-viewer is installed on the host by its play (at the current pin
22a55582735d, passed at F44 114dfa25) and is the `text/markdown` default; first frame 122 ms
at the first pin. The emoji and dark-mode decisions are taken (Task 2.1b, coordinator
defaults, changeable by the owner). Left for workers: md-viewer M13 (Task 2.1d) in its own
repository, then the pin move and play entry, and qa-reviewer over the plan. Left for the host
agent: Task 2.3's open from Nautilus, judged by screenshot. Nothing waits on the owner.
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

- [x] ✅ **Task 2.1b**: The owner's decisions before the build. These are listed at the end of
  md-viewer's `docs/MILESTONES.md`. The two that matter for reading plan files are emoji (the
  embedded Go fonts draw ✅ ⬜ 🔄 as boxes) and dark mode (a `--dark` flag, or the desktop
  setting read over D-Bus). Decided as **coordinator defaults, changeable by the owner** (the
  owner's standing "do not gate on me"):

  - Emoji: embed an emoji font in md-viewer. It is the only option that needs no scan of the
    system's fonts at start, so it keeps Task 1.2's "launch-to-render as fast as possible",
    and still draws the status marks plans are full of.
  - Dark mode: read `org.freedesktop.appearance` `color-scheme` once at start through the XDG
    settings portal over D-Bus, with `--dark` and `--light` overriding it. A flag alone does
    not meet this plan's goal "It follows GNOME's light or dark scheme".
  - Any other item listed there: md-viewer's own recommendation for it, recorded in its
    `docs/MILESTONES.md` by the worker of Task 2.1d.

- [ ] ⬜ **Task 2.1d**: md-viewer milestone M13, in md-viewer's repository by its own method:
  the embedded emoji font and the portal colour scheme of Task 2.1b, tests first. The pin
  22a55582735d already carries "dark mode and font size settings" (commit 64f2b6894); the
  worker checks what that does against Task 2.1b's default and builds only the gap. Then move
  `md_viewer_version` in `play-md-viewer.yml` to the merged tip and queue the play in
  `meta-deploy.list` (host agent: `md-viewer --version` names the new pin, `--time` again).

- [x] ✅ **Task 2.1c**: md-viewer is its own ccy project, with its own hooks daemon, on one
  team with this session. All its milestones, M0 to M12, are merged on its `main` (M10: the
  binary opens a Gio window; M11: speed fixtures, benchmarks and measurement; M12: non-regular
  files never block the viewer, and the tracked-files-only label test). The pin was md-viewer's
  `main` tip at M12, `bafafc8e4ff9`, and is now its later tip `22a55582735d` (text on its
  baseline, dark mode and font size settings; commit 64f2b6894).

- [x] ✅ **Task 2.2**: A play installs md-viewer on the host (with Gio's runtime libraries) and
  makes it the default for `text/markdown`. Startup is measured on the host with `--time`.
  The play is written:
  [play-md-viewer.yml](../../../playbooks/imports/optional/common/play-md-viewer.yml). It
  builds md-viewer with `go install` at the commit pinned in `md_viewer_version` (through
  `tasks/build-go-command.yml`, which now takes an external `go_command_module`), after
  upgrading golang and asserting it meets md-viewer's `go.mod`. It checks `--version` names
  that commit, installs a desktop file and sets the `text/markdown` (and `text/x-markdown`)
  default in `~/.config/mimeapps.list`. Host run done (journal 2026-10-15 17:45, meta-deploy at
  F44 158855fb): the play passed, `md-viewer --version` named the pin, `xdg-mime query default` gives md-viewer.desktop for `text/markdown` and `text/x-markdown`, and `--time` on a
  PLAN.md gave a first frame at 121.8 ms. The later pins ran too: `bafafc8e4ff9` (commit
  3811949b2) and the current `22a55582735d` at F44 114dfa25 (commit 6be8de0bc).

- [ ] ⬜ **Task 2.3**: The host agent opens a plan's `PLAN.md` from Nautilus (or `gio open`),
  takes a screenshot of the window, and records whether it renders and opens quickly. The
  owner's taste in rendering is a product preference, not a gate. qa-reviewer over the plan
  (a worker).

## Success Criteria

- [ ] A double-click on a `.md` file opens it rendered, quickly.
- [ ] Tables, code, task lists, images and relative links render correctly.
- [x] Installed by a play and set as the default for `text/markdown` (Task 2.2).

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00199-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- <!-- milestone or delivery commit hash -->
