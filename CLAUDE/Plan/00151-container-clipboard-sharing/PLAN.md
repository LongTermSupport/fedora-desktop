# Plan 00151: container clipboard sharing

**Status**: In Progress
**Created**: 2026-10-03
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code pastes an image with Ctrl+V by running `wl-paste` (Wayland) or `xclip` (X11)
to read the clipboard. The owner runs Claude almost entirely inside containers: ccy
(rootless Podman) and LXC. Inside them Ctrl+V finds no clipboard tool and, even with one
installed, may not reach the host compositor. Plan 00150's `imgpaste` works around this by
pasting an image as text. This plan finds out whether the clipboard itself can be shared,
safely, and builds that if it can.

The ccy launcher already bind-mounts the host Wayland socket read-only and sets
`WAYLAND_DISPLAY` and `XDG_RUNTIME_DIR`, so the headed browser can open windows
(`files/var/local/claude-yolo/claude-yolo`, "Detect Wayland or X11"). The ccy image does
not ship `wl-clipboard`. So for ccy the first question is whether adding `wl-clipboard`
is enough. GNOME's mutter does not implement the `wlr-data-control` protocol, so
`wl-paste` has to map a surface to get focus before it may read; whether that works from a
container, and how it looks to the user, is not known.

LXC containers (`play-lxc-install-config.yml`) are full systems with their own users and
no display socket today, so they may need a different route. A route that serves both is
preferred.

## Goals

- Know, with evidence, whether Ctrl+V image paste can work inside a ccy container and
  inside an LXC container, and by what mechanism.
- If it can: deliver it through IaC (ccy image and launcher; the LXC play), with the
  security trade-off stated and chosen by the owner.
- If it cannot (or the cost is too high): record why, and keep `imgpaste` as the route.

## Non-Goals

- Sharing the clipboard in the other direction (container to host) unless the chosen
  mechanism gives it for free.
- X11-only desktops beyond what the survey shows is free.
- Remote (SSH) servers: there is no local clipboard there; `imgpaste` covers them.

## Tasks

### Phase 1: Survey

- [x] ✅ **Task 1.1**: Done, [RESEARCH-survey.md](RESEARCH-survey.md). Claude Code runs
  `xclip … || wl-paste` with no timeout; the Wayland socket ccy already mounts is usable from
  inside (probe: `probe-wayland-globals.py`); GNOME offers no data-control protocol, so
  `wl-paste` needs focus (flash, or hang). Recommends A for ccy (adds no new exposure) and D
  (shim plus host spool) for LXC, whose containers are not configured by IaC. Side finding:
  GNOME here offers no virtual-keyboard protocol, so `clean-paste`'s `wtype` keystroke likely
  fails. Survey agent: how Claude Code reads the clipboard on Linux (which
  commands, which env vars, image MIME types), what the ccy launcher already passes into
  the container, how LXC containers here are configured, and the candidate mechanisms:
  - A: `wl-clipboard` in the container over the already-mounted Wayland socket.
  - B: the same for LXC, by bind-mounting the user's Wayland socket into the container.
  - C: a host-side bridge that writes the clipboard image to a file the container can
    read (works for both, exposes no socket).
  - D: anything else the survey finds (e.g. a shim `wl-paste` in the container that
    talks to a host helper).
    For each: does it work under GNOME/mutter, what it exposes, what it costs. Report in
    `RESEARCH-survey.md`.
- [ ] ⬜ **Task 1.2**: Owner decision on the mechanism(s), from the survey's
  recommendation and security notes.

### Phase 2: Prototype

- [ ] 🚫 **Task 2.1**: Prototype the chosen mechanism for ccy, as a plan-local script or a
  ccy image change on a branch; the owner tries Ctrl+V once. Script ready:
  `prototype-ccy-wl-paste.bash` (HOST, once). It builds a throwaway image with
  `wl-clipboard`, runs `wl-paste` over the socket and removes the image. The owner reports the
  rc, the bytes and whether a window flashed. Blocked on the owner's run; Task 1.2 follows its result.
- [ ] ⬜ **Task 2.2**: Prototype for LXC (same mechanism if the survey says it carries).

### Phase 3: Deliver

- [ ] ⬜ **Task 3.1**: Implement in IaC (ccy Dockerfile/launcher with version bumps; the
  LXC play), docs updated.
- [ ] ⬜ **Task 3.2**: `deploy.bash` running `acceptance.bash` as its last leg.
- [ ] ⬜ **Task 3.3**: `./scripts/qa-all.bash` and the `qa-reviewer` agent clean.

## Success Criteria

- [ ] The survey states, per container type, whether clipboard image paste is possible
  and the recommended mechanism, with evidence.
- [ ] Either Ctrl+V pastes an image into Claude inside ccy and LXC, deployed by IaC and
  confirmed on the host, or the plan records why not and is closed.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00151-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created.
