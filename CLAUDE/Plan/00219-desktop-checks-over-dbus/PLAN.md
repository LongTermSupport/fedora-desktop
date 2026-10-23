# Plan 00219: desktop checks over D-Bus

**Status**: Not Started (design reviewed READY in round 2; decisions D1-D8 in DESIGN.md carry recommended defaults)
**Created**: 2026-10-23
**Owner**: joseph
**Priority**: Medium

## Overview

On-screen acceptance checks cannot be done by the host agent. Plan 00198 waits on the owner to
confirm that a file opened as Nautilus does opens its editor window on the active workspace;
Plan 00185 waits on the owner to look at emoji glyphs in a fresh terminal. GNOME Shell 50
refuses `Shell.Eval`, `Introspect.GetWindows` and the Screenshot API to any sender not on its
allowlist, the screenshot portal asks the owner on their screen, and `wmctrl`/`xdotool` cannot
see native Wayland windows.

This plan gives agents two things. A read-only D-Bus object in the repo's own `fedora-desktop`
panel extension, `org.fedoradesktop.DesktopFacts.GetWindows` at `/org/fedoradesktop/DesktopFacts`
on `org.gnome.Shell`, which returns the active workspace and each window's class, app id,
workspace, monitor and focus as one JSON document, with no titles. And a Go client,
`desktop-check`, which reads that object and runs a throwaway headless GNOME Shell on a private
bus in unsafe mode, where a check can open windows, switch workspace and take a window
screenshot without touching the owner's screen. The extension gets no mutating method. The client's
two mutating subcommands refuse unless the bus is one it created and the shell says it is
headless and in unsafe mode, and behind that the owner's shell refuses those calls by GNOME's
own sender check, so no owner switch is needed.

The full design (interface, JSON schema, test shell steps, security table, use by 00198 and
00185, decisions) is [DESIGN.md](DESIGN.md).

## Goals

- An agent reads window placement and focus from the owner's real session, read-only.
- An agent proves 00198's fix red and green in a test shell, with no owner and no change to
  the owner's screen.
- An agent takes a window screenshot of a fresh terminal for 00185 in the test shell.
- Nothing this plan adds can switch the owner's workspace or capture the owner's screen.

## Non-Goals

- Screenshots or workspace changes on the owner's real session.
- Window titles, contents or geometry.
- Replacing checks only the owner can make (a long-running terminal's glyphs, a real
  double-click); 00185 and 00198 decide what stays theirs.

## Unverified premises (Task 1.1 settles them)

- `gnome-shell` 50 accepts `--headless`, `--unsafe-mode`, `--virtual-monitor WxH`,
  `--wayland-display NAME` and `--no-x11`, and starts on a private bus with no seat.
- `dbus-daemon --session` is installed (Fedora's session bus is dbus-broker), or `dbus-run-session`
  is; whichever is present is named in the play.
- `busctl --user` or `gdbus` honours `DBUS_SESSION_BUS_ADDRESS` for the private bus.
- A test-shell run leaves the owner's `~/.config/dconf/user` unchanged.
- An app launched with the test shell's environment, directly or by D-Bus activation, opens its
  window there and only there.
- A new window in a headless shell takes focus with no input device (`screenshot-window` shoots
  the focused window).
- The Meta/Shell calls in DESIGN.md section 3 exist in 50.5 (read from the extracted source).

## Tasks

### Phase 1: Facts

- [ ] ⬜ **Task 1.1**: `triage.bash` (host, read-only to the owner's session): `gnome-shell --help-all`
  and the version; a start of a headless shell with the flags above on a private bus, the bus
  daemon itself in the full private environment of DESIGN.md section 4 step 2; the start-up
  time; the shell's stderr with no session manager present; Eval `global.context.unsafe_mode`
  and `global.backend.is_headless()`; `org.gnome.Shell.Introspect.GetWindows` answered there.
  Then two apps: one launched directly (`gtk4-demo` or another installed GTK app) and one
  **D-Bus-activated** (gnome-text-editor through `gio open` with the system desktop file). For
  each: its window is listed by the test shell, it has focus, and its `/proc/<pid>/environ`
  carries the test shell's `WAYLAND_DISPLAY` and no `DISPLAY` (so it cannot be on the owner's
  screen; this needs no facts object). Also: every bus name activated on the private bus, a diff
  of `$XDG_RUNTIME_DIR` before and after, the owner's dconf database checksum before and after,
  the default `text/plain` handler with an empty private config, and which bus daemon and client
  tools exist. Analysis in `triage-analysis.md`.

### Phase 2: The facts object (extension)

- [ ] ⬜ **Task 2.1**: Verify every Meta/Shell call in DESIGN.md section 3 against the 50.5 source.
  `tests/extensions/test-panel-desktop-facts.mjs` (red first): the JSON for a fixed window set,
  the eligibility filter, `null` workspace for all-workspace windows, ids as strings, sorted
  ids, no `title` key, `schema` 1. Add it to `scripts/test-panel-sections.bash`.
- [ ] ⬜ **Task 2.2**: `windowFacts.js` (pure) and `desktopFacts.js` (export/unexport), wired into
  `extension.js` `enable()`/`disable()`; header paragraph. ESLint clean
  (`extensions/node_modules/.bin/eslint .`).

### Phase 3: The client and test shell (Go)

- [ ] ⬜ **Task 3.1**: `SPEC-desktop-check.md` (numbered requirements), then
  `src-go/cmd/desktop-check` and its package, unit tests first through execx fakes: `windows`
  (schema check, the not-loaded-yet message), `wait-window` (bounded, `--timeout`), `shell`
  (the nine steps of DESIGN.md section 4: the bus daemon started in the private environment,
  the stale-run sweep, the shell log dumped on a readiness timeout, teardown on every exit
  path), `activate-workspace` and `screenshot-window` (refused unless the bus is one the tool
  created **and** Eval says headless and unsafe; tests prove each refusal alone, including the
  real-session answer `(false, '')`; a relative screenshot path refused).
- [ ] ⬜ **Task 3.2**: `play-fedora-desktop-panel.yml`: import `play-golang.yml`, build and install
  `desktop-check` with `tasks/build-go-command.yml`, install the bus daemon package Task 1.1
  names. The extension files already deploy with the whole-directory copy. The play header
  and the extension header mention the facts object.

### Phase 4: Deploy and accept

- [ ] ⬜ **Task 4.1**: `deploy.bash` (the panel play) and `acceptance.bash`: in the test shell, the
  facts object answers with `headless: true`; a GTK app window appears on workspace 0 and is
  focused; `activate-workspace 1` moves `active_workspace` to 1; `screenshot-window` writes a
  non-empty PNG; the owner's dconf database unchanged. On the real session,
  `activate-workspace` and `screenshot-window` refuse with exit 3, and `windows` answers with
  `headless: false` and no `title` key (after the owner's login; before it, not yet established).
  Entry in `meta-deploy.list`. The owner's one separate ask: log out and in, for the real-session leg.
- [ ] ⬜ **Task 4.2**: Hand 00198 and 00185 their usage (DESIGN.md sections 7 and 8) as a task in
  each plan; a line for the facts object and `desktop-check` in the panel's user doc under
  `docs/`; `qa-reviewer` over this plan.

## Success Criteria

- [ ] `desktop-check windows` on the real session reports the active workspace and windows, no titles.
- [ ] `desktop-check shell` runs a check in a headless shell and leaves the owner's settings and screen untouched.
- [ ] `activate-workspace` and `screenshot-window` are refused on the real session.
- [ ] 00198's red and green test-shell runs are possible with this tool alone.

## Review

Fable review rounds are recorded in this plan's journal.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00219-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan and design filed.
