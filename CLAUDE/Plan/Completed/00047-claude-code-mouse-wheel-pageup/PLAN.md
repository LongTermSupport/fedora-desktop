# Plan 00047: Claude Code Safe Scroll — Mouse Wheel → PageUp/PageDown

**Status**: Complete (2026-07-13)
**Created**: 2026-05-27
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code's terminal UI now runs in a "safe scroll" mode that uses tmux to provide a real scrollback buffer (scroll up/down via PageUp/PageDown keys to view history). This works correctly when the user hits PageUp/PageDown on the keyboard.

**The problem**: The mouse scroll wheel in the host terminal emulator emits arrow-up / arrow-down key events, NOT PageUp / PageDown. Claude Code's interactive prompt interprets arrow-up as "previous message history" (readline-style up-arrow recall), which then:

1. Replaces whatever the user is typing
2. Loses in-progress input
3. Causes a frustrating interruption every time the user instinctively scrolls

We want the mouse scroll wheel to send PageUp / PageDown into Claude Code so that it engages the tmux scrollback (which is what the user actually wants) instead of clobbering the prompt buffer with history navigation.

## Goals

- Mouse wheel scroll up → PageUp into Claude Code (engages tmux scrollback)
- Mouse wheel scroll down → PageDown into Claude Code (engages tmux scrollback)
- No regression in normal text input (arrow keys on the keyboard still work for prompt history)
- Solution deployable via Ansible (IaC) — no manual config edits
- Works across the user's terminal emulators of choice (need to enumerate which ones are in use)

## Non-Goals

- Re-implementing scrollback inside Claude Code itself
- Disabling Claude Code's "safe scroll mode" (we want to keep tmux scrollback)
- Patching Claude Code internals to ignore arrow-up while at the prompt
- Cross-platform support (Linux/Wayland/GNOME-Terminal-or-similar is the target)

## Context & Background

- Claude Code "safe scroll" mode runs the TUI inside a tmux session so that the host terminal's scrollback shows the live TUI, while tmux owns the historical buffer.
- The interaction is: host terminal emits scroll-wheel events → host terminal converts those to escape sequences → tmux receives them → tmux passes them to the inner process (Claude Code).
- The exact translation depends on:
  1. Terminal emulator type (gnome-terminal / kitty / alacritty / ghostty / wezterm / foot)
  2. Whether the application has enabled "mouse mode" via `\e[?1000h` family escape sequences
  3. tmux's `mouse on` setting + any custom `bind-key -T copy-mode-vi WheelUpPane …` bindings
  4. Whether Claude Code itself enables mouse mode (it might explicitly disable mouse, hence the fallback to arrow keys)
- Known relevant env var from prior CCY/CC work: `CLAUDE_CODE_DISABLE_MOUSE` (Plan 00036). This may be why the TUI does not receive raw mouse events and the terminal falls back to emitting arrow keys for wheel events.

The research phase below must clarify these mechanics before we pick an implementation.

## Tasks

### Phase 1: Research (complete)

- [x] ✅ **Task 1.1**: Documented Claude Code's "safe scroll" mode — confirmed it is NOT tmux; it is in-process fullscreen rendering using `\e[?1049h` (alt screen). Mouse capture controlled by `CLAUDE_CODE_DISABLE_MOUSE=1`. → `research-01-claude-code-safe-scroll.md`
- [x] ✅ **Task 1.2**: Documented terminal-emulator wheel behaviour: DECSET 1007 is the root cause; per-emulator remap viable for kitty/alacritty/wezterm; impossible for VTE/ghostty/konsole; half-measure for foot. → `research-02-terminal-emulators.md`
- [x] ✅ **Task 1.3**: Documented tmux `WheelUpPane` binding with `alternate_on` gate. (Path B — not chosen, but preserved for the case where the user adopts tmux later.) → `research-03-tmux-mouse-bindings.md`
- [x] ✅ **Task 1.4**: Documented community recipes — DECSET 1007 is the universal mechanism; tmux interception and per-emulator remap are the two viable fix surfaces. → `research-04-prior-art-recipes.md`

### Phase 2: Decision Gate (complete)

- [x] ✅ Synthesised findings into `DECISION.md`
- [x] ✅ Implementation surface chosen: **Path C+** — per-emulator wheel→PageUp config (kitty + alacritty deployed; wezterm supported but not in default install) + runtime `terminal_preflight_check` in `claude-yolo` host wrapper to gate launch on supported emulator
- [x] ✅ User approval received: "execute"
- [x] ✅ Ghostty recommendation corrected (was "best", now NOT supported — see DECISION.md correction block)

### Phase 3: Implementation

- [x] ✅ **Task 3.1**: Add wheel→PageUp config to each supported emulator
  - [x] ✅ 3.1.1 — kitty: `mouse_map wheel_up press ungrabbed send_key Page_Up` (+ `_down`) in `~/.config/kitty/kitty.conf` via blockinfile
  - [x] ✅ 3.1.2 — alacritty: `[[mouse.bindings]] mouse = "WheelUp"; chars = "[5~"; mode = "Alt"` (+ `_down`) in `~/.config/alacritty/alacritty.toml` via blockinfile
  - [x] ✅ 3.1.3 — wezterm: skipped (not installed by `play-terminal-emulators.yml`)
  - [x] ✅ 3.1.4 — ghostty: skipped (verified: no mouse-binding config in v1.x; upstream discussion #4169 open)
- [x] ✅ **Task 3.2**: Verify each emulator's directive against upstream docs — kitty + alacritty syntax verified; ghostty verified UNSUPPORTED via `ghostty.org/docs/config/reference` fetch
- [x] ✅ **Task 3.3**: Add `detect_terminal()` + `terminal_preflight_check()` library
  - [x] ✅ 3.3.1 — Created `files/var/local/claude-yolo/lib/terminal-detection.bash` (165 lines, shellcheck-clean)
  - [x] ✅ 3.3.2 — Detection chain: env-var → `$TERM` → `ps -o comm= -p $PPID` → unknown
  - [x] ✅ 3.3.3 — Gum-rendered banner with fallback to plain printf
  - [x] ✅ 3.3.4 — `CCY_ACCEPT_UNSUPPORTED_TERM=1` override honoured
- [x] ✅ **Task 3.4**: Bump `CCY_VERSION` 3.14.0 → 3.15.0 with explanatory comment
- [x] ✅ **Task 3.5**: Source new lib in `claude-yolo` and call `terminal_preflight_check` after `check_allowed_hostname`
- [x] ✅ **Task 3.6**: Deploy via `play-claude-yolo.yml` (add to lib loop + install `gum` as host dependency)
- [x] ❌ **Task 3.7**: QA of Path C+ — superseded by Path E (the Path C+ artifacts never shipped; QA runs against the Path E change instead)
- [x] ❌ **Task 3.8**: Commit Path C+ — superseded by Path E (committed under the Path E change)
- [x] ❌ **Task 3.9**: HOST deployment of Path C+ configs — superseded by Path E (no `play-terminal-emulators.yml` wheel remap to deploy)
- [x] ❌ **Task 3.10**: Live test of Path C+ banner/PageUp — superseded by Path E (verified fullscreen wheel scroll on GNOME-Terminal instead)

### Path E: fullscreen + mouse capture (the actual resolution)

The Phase 3 tasks above chase Path C+ (per-emulator wheel→PageUp remap + a
preflight gate), which was proven empirically dead, after which the plan pivoted
to Path D (force the classic renderer via `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1`).
Both paths shared one buried assumption — that CCY keeps mouse tracking **off**
(`CLAUDE_CODE_DISABLE_MOUSE=1`, kept for native click-drag selection).

Dropping that assumption solved it: with `CLAUDE_CODE_DISABLE_MOUSE` unset,
Claude Code captures the mouse itself and handles the wheel **inside** the
alt-screen, so fullscreen scroll works natively on GNOME-Terminal/VTE — no
emulator remap, no DECSET-1007 arrow-key fallback. With the wheel fixed, the
Path D kill switch had no reason to exist and was removed too, so fullscreen is
a normal opt-in again (`/tui fullscreen`, or per-repo via `.claude/ccy/ccy.env`).

- [x] ✅ **Task E.1**: Drop `CLAUDE_CODE_DISABLE_MOUSE=1` from `entrypoint.sh` — verified: wheel scrolls in a forced-fullscreen (agent-view / `claude attach`) session on GNOME-Terminal
- [x] ✅ **Task E.2**: Drop `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1` (Path D kill switch) so fullscreen is opt-in again; per-repo override via `ccy.env` documented in `entrypoint.sh`
- [x] ✅ **Task E.3**: Container rebuild forced (`REQUIRED_CONTAINER_VERSION` 2.20 → 2.22; `CCY_VERSION` → 3.27.0); QA green (`./scripts/qa-all.bash`)

## Open Questions (for research to answer)

1. Is Claude Code's safe scroll mode something Claude Code itself launches (e.g. it execs `tmux new-session …` internally), or is it a wrapper script the user invokes?
2. Which terminal emulator is the user actually on? (Affects which side of the chain we tweak.)
3. Does tmux's `mouse on` get us what we want, or does that swallow the wheel events server-side without forwarding PageUp/Down to the inner process?
4. Is there a Claude Code config flag for "interpret wheel as PageUp/PageDown"?
5. Are arrow keys the *fallback* the terminal uses when mouse mode is off, or is some intermediate layer (libreadline-style) doing the translation?

## Dependencies

- Related: Plan 00036 (CC/CCY parity — `CLAUDE_CODE_DISABLE_MOUSE` env var)

## Success Criteria

- [x] Mouse wheel up in Claude Code TUI scrolls the conversation, does NOT touch the prompt buffer (via CC's own alt-screen mouse capture, not PageUp/tmux)
- [x] Mouse wheel down scrolls the conversation / returns to live view (same mechanism)
- [x] Keyboard arrow keys still work normally for prompt history
- [x] Configuration is deployed via Ansible (`entrypoint.sh` via `play-claude-yolo.yml`; no manual steps)
- [x] All QA checks pass
- [x] Tested on the user's actual daily-driver terminal (GNOME-Terminal) + Claude Code

## Notes & Updates

### 2026-05-27

- Plan created. Research phase kicked off with 4 parallel sub-agents writing directly to plan folder.
- Research phase complete; DECISION.md written with Path C+ chosen (per-emulator config + runtime pre-flight).
- User approved execution.
- **Ghostty correction**: original DECISION.md named ghostty as "best for CC" — verified incorrect via direct upstream fetch (ghostty has no mouse-binding config in v1.x; open feature request #4169). Recommendation pivoted to **kitty**. Ghostty moved to detected-but-unsupported bucket alongside VTE/foot/konsole.
- Phase 3 implementation: new lib `files/var/local/claude-yolo/lib/terminal-detection.bash`, `CCY_VERSION` bumped 3.14.0 → 3.15.0, kitty + alacritty wheel→PageUp blocks added to `play-terminal-emulators.yml`, gum added as a host dependency in `play-claude-yolo.yml`.
- Remaining: QA, commit, host deployment, live verification.
- **PIVOT** (same day, after host testing): Path C+ proven empirically dead. `kitty --debug-input` showed kitty bypasses `mouse_map` entirely in `NO_TRACKING` mode (the mode CCY's `CLAUDE_CODE_DISABLE_MOUSE=1` enforces). Smooth scroll-axis events on Wayland never synthesise into `b4`/`b5` button presses; they go to kitty's own scrollback handler. DECSET 1007's wheel→arrow translation (what CC was seeing) also bypasses `mouse_map`. No emulator-side fix is reachable while CCY keeps mouse tracking off and fullscreen mode on.
- **New direction (Path D)**: drop `CLAUDE_CODE_NO_FLICKER=1` from `entrypoint.sh` — CC reverts to classic in-band rendering, kitty's native scrollback IS the conversation, wheel/selection/search all work natively. The thing fullscreen mode was buying (flat memory, no flicker) turns out not to be worth its cost on a GPU-accelerated emulator. See `DECISION.md` → "FINAL ANSWER".
- All today's work has been moved to PR branch `plan/00047-wheel-pageup-rework`; F43 will be reset to before these commits and the PR merged once Path D is live-verified.

### 2026-07-13 — Resolved via Path E (fullscreen + mouse capture)

- While fixing an unrelated CCY symptom (the mouse wheel typing arrow keys in
  agent-view / `claude attach` background sessions, which force fullscreen and
  ignore the Path D kill switch), we found the assumption underpinning both
  dead paths: they all kept `CLAUDE_CODE_DISABLE_MOUSE=1`. Removing it lets
  Claude Code capture the mouse and scroll the conversation natively inside the
  alt-screen — the exact thing Plan 00047 concluded was impossible on VTE.
- Shipped as **Path E**: `entrypoint.sh` now sets **neither**
  `CLAUDE_CODE_DISABLE_MOUSE` nor `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN`. CC's
  own defaults apply (classic renderer by default; `/tui fullscreen` opts in,
  per-repo via the symlinked `.claude/ccy/settings.json` or an
  `export CLAUDE_CODE_NO_FLICKER=1` in `.claude/ccy/ccy.env`).
- Container rebuild forced: `REQUIRED_CONTAINER_VERSION` 2.20 → 2.22 (an interim
  2.21 image was built during dogfooding and is skipped in committed history);
  `CCY_VERSION` → 3.27.0. QA green. Verified on GNOME-Terminal: wheel scrolls
  the conversation in fullscreen; classic sessions keep native selection/search.
- Path C+ artifacts (`lib/terminal-detection.bash`, preflight gate, per-emulator
  wheel remap) were already reverted before this — nothing to unwind.
- Ref: LongTermSupport/fedora-desktop#31.
