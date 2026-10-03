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
- [x] ✅ **Task 1.2**: Owner decision on the mechanism(s), from the survey's
  recommendation and security notes. **Owner chose A** (wl-clipboard over the mounted
  socket) for ccy, trial in this project's image first, then standard; and asked for LXC and
  other containers to be documented ([docs/features/container-clipboard.md](../../../docs/features/container-clipboard.md)).

### Phase 2: Prototype

- [x] ✅ **Task 2.1**: **Passed on the host**: inside a container built from the ccy
  image, `wl-paste -l` offered `image/png` (rc 0) and `wl-paste --type image/png` returned
  162,020 bytes (rc 0), over the read-only socket mount, SELinux Permissive. GNOME gave focus;
  option A works. Prototype the chosen mechanism for ccy, as a plan-local script or a
  ccy image change on a branch; the owner tries Ctrl+V once. Script ready:
  `prototype-ccy-wl-paste.bash` (HOST, once). It builds a throwaway image with
  `wl-clipboard`, runs `wl-paste` over the socket and removes the image. The owner reports the
  rc, the bytes and whether a window flashed. Blocked on the owner's run; Task 1.2 follows its result.
- [x] ✅ **Task 2.1a**: **Ctrl+V confirmed by the owner**: a pasted photo attached to the
  message in the rebuilt project ccy. Owner choice: trial in this project's ccy image first
  (`.claude/ccy/Dockerfile`), then make it standard in the shared image. Added
  `wl-clipboard` plus a `/usr/local/bin/wl-paste` wrapper that caps each call at 5 s, so a
  GNOME focus refusal fails with exit 124 instead of freezing Claude's Ctrl+V. Needs a ccy
  rebuild (automatic on next launch) and one Ctrl+V try. **Confirmed in the rebuilt ccy**:
  `wl-paste -l` lists `image/png` and `wl-paste --type image/png` returns the screenshot
  (89,846 bytes, viewed with Read), through the 5 s guard, no hang. **HOST next**: start ccy in this
  project (it rebuilds), copy an image, press Ctrl+V in Claude; the image attaches. Then
  Task 3.1 moves `wl-clipboard` and the guard into the shared claude-yolo image.
- [ ] ⬜ **Task 2.2**: Prototype for LXC (same mechanism if the survey says it carries).
  The manual recipe is documented (untested) in `docs/features/container-clipboard.md`:
  a read-only `lxc.mount.entry` for the socket with `optional`, `WAYLAND_DISPLAY` as an
  absolute path, a matching uid, wl-clipboard plus the guard inside, and a container restart
  after each login. Automating it needs LXC config under IaC, which this repo does not have.

### Phase 3: Deliver

- [ ] 🔄 **Task 3.1**: Implement in IaC (ccy Dockerfile/launcher with version bumps; the
  LXC play), docs updated. ccy done: `wl-clipboard` and the guard moved to the shared image
  (container 2.41, CCY 3.74.0, changelog), removed from this project's Dockerfile; docs
  `docs/features/container-clipboard.md` and the ccy.md security table. HOST: the next ccy
  launch in any project rebuilds the base image. LXC remains manual (Task 2.2).
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
