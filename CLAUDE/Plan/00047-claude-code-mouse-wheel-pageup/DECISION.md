# Plan 00047 — Decision Gate

**Status**: 👁️ Awaiting user input
**Date**: 2026-05-27
**Inputs**: `research-01-claude-code-safe-scroll.md`, `research-02-terminal-emulators.md`, `research-03-tmux-mouse-bindings.md`, `research-04-prior-art-recipes.md`

## What the research actually showed

The user's framing — "safe scroll mode which basically uses tmux" — turns out to be partly off, and that matters for the fix.

1. Claude Code's "safe scroll" is **not tmux**. It is an in-process feature called **fullscreen rendering** (`/tui fullscreen`, env `CLAUDE_CODE_NO_FLICKER=1`) that draws to the terminal's alternate screen (`\e[?1049h`). Docs: <https://code.claude.com/docs/en/fullscreen>. (research-01)
2. In fullscreen mode, Claude Code by default DOES emit DECSET 1000/1003/1006 and binds wheel to `scroll:lineUp`/`scroll:lineDown` in its `Scroll` keybinding context. (research-01, research-03)
3. **CCY's own entrypoint suppresses that** with `CLAUDE_CODE_DISABLE_MOUSE=1` at `files/var/local/claude-yolo/entrypoint.sh:109` so that native click-drag selection (and copy-from-tmux-pane) keeps working. The comment on line 106 even names tmux as the motivating use case.
4. With Claude Code's mouse capture suppressed, the terminal emulator's **DECSET 1007 (alternate-scroll-mode)** fallback kicks in: on the alt screen, the wheel emits `\e[A` / `\e[B` — i.e. cursor-up / cursor-down. (research-02, research-04)
5. The Claude Code prompt's `History` keybinding context interprets bare `up` / `down` as `history:previous` / `history:next`, which is the user's reported symptom. (research-01)
6. There is no Claude Code setting that disambiguates "real arrow key" from "wheel-translated arrow key" — they are bit-identical on stdin. (research-01 Layer C, ruled out)
7. The user might or might not actually be running tmux around CCY. CCY itself does **not** launch tmux (`grep tmux files/var/local/claude-yolo/` finds nothing but the comment above). If the user has their own tmux wrapper that is a separate habit not modelled in this repo.

## Two clean fixes — both work, with different blast radius

### Path A — Remove `CLAUDE_CODE_DISABLE_MOUSE=1` from CCY entrypoint

**Mechanism**: Let Claude Code keep its DECSET-1000 mouse capture turned on. It then natively handles wheel events as `scroll:lineUp`/`scroll:lineDown`. The terminal-emulator `?1007` fallback never triggers because the app is consuming the events directly.

**Diff**: one line removed from `files/var/local/claude-yolo/entrypoint.sh`. Bump `CCY_VERSION` (patch).

**Cost**:

- Native click-drag text selection in the terminal stops working in fullscreen mode. To select text the user must hold `Shift` (Linux/Windows terminals) or `Option` (macOS / iTerm2). This is documented Claude Code behaviour, not a bug. (research-01)
- That is exactly the trade-off the original commit was avoiding — the comment on entrypoint.sh:106 explicitly cites SSH/tmux selection as the reason.

**Scope**: CCY only. Host `cc` (via Plan 00036 parity work) would need a matching change, or be left alone.

**Effort**: ~5 minutes plus host-side test.

### Path B — Tmux `WheelUpPane` binding that sends `PageUp`/`PageDown`

**Mechanism**: Add a tmux binding that, when the inner pane is on the alternate screen, sends literal `PageUp` / `PageDown` keystrokes for wheel events; when the inner pane is on the primary screen and has requested mouse, forward raw events (`send -M`). The canonical snippet (from `research-03` Bottom line):

```tmux
set -g mouse on

bind -n WheelUpPane {
    if -F '#{pane_in_mode}' {
        send -M
    } {
        if -F '#{alternate_on}' {
            send-keys -N 3 PageUp
        } {
            if -F '#{mouse_any_flag}' { send -M } { copy-mode -e }
        }
    }
}
bind -n WheelDownPane {
    if -F '#{pane_in_mode}' {
        send -M
    } {
        if -F '#{alternate_on}' {
            send-keys -N 3 PageDown
        } {
            if -F '#{mouse_any_flag}' { send -M } { send-keys -N 3 Down }
        }
    }
}
```

The clever bit is gating on `alternate_on` *before* `mouse_any_flag` — that catches Claude Code's fullscreen mode whether or not it has mouse capture on, while leaving primary-screen mouse apps (btop, mc) untouched. (research-03)

**Cost**:

- Requires the user to actually run Claude Code inside tmux. If they currently aren't, this is a workflow change. (Need to confirm — see "Open question" below.)
- Native click-drag selection inside Claude Code requires holding Shift to bypass tmux's mouse mode (same shift-drag pattern as Path A).
- `PageUp` / `PageDown` are inert at Claude Code's prompt today; they only do something useful once the user has scrolled into transcript context. So this fix is **slightly worse than Path A** for active scrolling — but it does cleanly stop the history-recall clobber, which is the user's actual complaint.

**Scope**: Host `~/.tmux.conf`. No CCY change. Works regardless of whether CC is running in CCY, ccdt, or bare on the host.

**Effort**: ~20 minutes: write `play-tmux-claude-bindings.yml` (or extend `play-tmux.yml` if one exists — `grep -l tmux playbooks/imports/`) with a `blockinfile`, QA, deploy on host.

### Path C — Both

These fixes are not mutually exclusive. Path A is the strict improvement for CCY users when they aren't on tmux; Path B is the universal safety net that catches stray cases (host `cc`, ad-hoc terminals, SSH sessions where tmux is the natural workflow).

If the user runs both, Path B's `alternate_on` guard fires before Path A's mouse capture matters, so the wheel ends up sending `PageUp` (Path B wins). That is the LESS preferred UX (PageUp is inert at the prompt) but it never clobbers history, which is the actual bug.

If the user wants the BEST UX (wheel = smooth scroll inside the transcript) and is willing to accept Shift-drag for selection, **Path A alone** delivers it.

## Open question for the user (single most important)

**Are you actually running Claude Code inside tmux today, or is "safe scroll" doing the alt-screen-rendering work on its own?**

You can answer this in one line:

```bash
echo "$TMUX"
```

- If that prints a socket path → you're in tmux → **Path B fixes you directly without touching CCY.**
- If that prints nothing → you're not in tmux → "safe scroll" is Claude Code's fullscreen rendering, and **Path A is the right fix** (or set up tmux first and then use Path B).

This single answer collapses the decision space.

## Path C — Remap the wheel at the terminal-emulator layer (PRESERVES SELECTION)

**Triggered by user follow-up**: "can we fix the wheel and keep the selection? is there a third option?"

Yes — IF the user runs kitty, alacritty, ghostty, or wezterm as their daily-driver terminal. The trick is to have the EMULATOR translate the wheel into a `PageUp` / `PageDown` keystroke *before* it ever reaches the alt-screen DECSET-1007 fallback. Claude Code's fullscreen mode binds `PageUp` / `PageDown` to `scroll:pageUp` / `scroll:pageDown` in its `Scroll` context unconditionally, so the wheel ends up scrolling the transcript with no clobber, **and `CLAUDE_CODE_DISABLE_MOUSE=1` stays in place so click-drag selection keeps working.** (research-02, research-04)

### Crucial: the remap is SCOPED to alt-screen + ungrabbed-mouse, not blanket

User flagged: "in normal terminal apps we want scroll to just be scroll". Yes — the binding syntax in every supported emulator gates on the inner pane's mode, so the rule is:

| Inner app state                        | Example apps                                                     | Wheel behaviour with Path C                                                                                              |
| -------------------------------------- | ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Primary screen (no alt-screen)         | bash prompt, `cat`, `ls`, `git diff`                             | **Emulator scrolls its own backlog** (default — unchanged)                                                               |
| Alt screen + app has NOT enabled mouse | Claude Code (with `DISABLE_MOUSE=1`), vim/`less`/`man` w/o mouse | Emulator emits `PageUp`/`PageDown` keystroke — CC scrolls transcript, vim/less/man scroll by page (their normal binding) |
| Alt screen + app HAS enabled mouse     | `htop`, `btop`, `lazygit`, `tig`, `mc`                           | Raw mouse event forwarded to the app — its own wheel handling continues to work (unchanged)                              |

Concretely, the gating modifier in each emulator:

- **kitty**: `mouse_map scroll_up ungrabbed,alternate_screen send_text all "\x1b[5~"` — `ungrabbed` excludes mouse-grabbing apps, `alternate_screen` excludes the bash prompt.
- **alacritty**: `mode = "Alt"` clause confines the binding to alt-screen mode; non-alt is left at the emulator default.
- **ghostty**: `mode = alt` equivalent.
- **wezterm**: Lua predicate on `pane:get_user_vars()` / alt-screen state.

So the user does NOT lose "scroll = scroll" in normal terminal usage. The binding only fires inside fullscreen TUIs that haven't grabbed the mouse. That is the entire universe of "apps where the wheel currently maps to up-arrow via DECSET 1007", which is the bug we're trying to fix.

**Emulator support** (ranked by ease):

- **kitty** — `mouse_map wheel_up press ungrabbed send_key Page_Up` (and `wheel_down` → `Page_Down`). One block in `~/.config/kitty/kitty.conf`, marker-managed by Ansible. Per-line and well documented. (research-02)
- **ghostty** — **NOT POSSIBLE as of v1.x.** Ghostty has *no* mouse-binding config — only keyboard `keybind`s. Open discussion [#4169](https://github.com/ghostty-org/ghostty/discussions/4169) "Bind actions to mouse wheel scroll" + #11874 are unshipped feature requests. **Verified via direct ghostty.org/docs/config/reference fetch on 2026-05-27**: zero `mouse_scroll`/`wheel`/`scroll_up`/`scroll_down` directives. Was originally named in this document as "the recommendation" — that was a synthesis error; see correction below. (research-02)
- **alacritty** — `[[mouse.bindings]] mouse = "WheelUp"; action = "ReceiveChar"; chars = "[5~"; mode = "Alt"`. The `mode = "Alt"` clause restricts it to alt-screen apps, so non-fullscreen scrolling still works as before. (research-02)
- **wezterm** — `SendKey { key = "PageUp" }` in `mouse_bindings`. Cleanest API. (research-02)
- **gnome-terminal / Ptyxis (VTE)** — **NOT POSSIBLE.** VTE has zero mouse-binding config and additionally resets DECSET state on every `reset` (Debian #921537). If this is the daily-driver terminal, Path C is unavailable; either accept Path A's Shift-drag, or run Claude Code inside one of the four supported emulators above. (research-02, research-04)
- **foot** — only allows *disabling* the wheel fallback, no `send_key` action. Half-measure — kills the clobber but the wheel does nothing useful afterwards. Inferior to switching emulators. (research-02)

**Cost**:

- Solution is per-emulator, not portable. If the user has multiple terminals in rotation (e.g. Ptyxis on laptop + kitty over SSH), the bug returns wherever the config isn't deployed.
- Slight smell of "fix Claude Code's UX with terminal config" — but the alternative for selection-preservers is *worse* (no fix at all on VTE).

**Path C vs Path A summary**:

|                         | Path A (drop env var)        | Path C (emulator remap)                  |
| ----------------------- | ---------------------------- | ---------------------------------------- |
| Selection works         | Needs Shift-drag             | **Yes, native click-drag**               |
| Wheel scrolls           | Yes — `scroll:lineUp` smooth | Yes — `scroll:pageUp` page-at-a-time     |
| CCY change required     | Yes (entrypoint + version)   | No                                       |
| Host config required    | No                           | Yes (one playbook per emulator in use)   |
| Works on gnome-terminal | Yes                          | **No — VTE has no knob**                 |
| Blast radius            | All CCY users                | Per-emulator, per-user, easy to back out |

## Open question for Path C

**Which terminal emulator do you actually run day-to-day for Claude Code?**

- kitty / ghostty / alacritty / wezterm → **Path C is viable and is the best of all worlds** (selection + wheel scroll)
- gnome-terminal / Ptyxis / any VTE-based one → Path C is OFF the table; fall back to Path A (Shift-drag for selection) or live with PageUp/PageDown on the keyboard

## Separate, related issue you raised — multi-line copy gets double newlines

This is orthogonal to the wheel question. It's almost certainly **soft-wrap unwrapping** on copy: VTE / many emulators treat each visually-wrapped row as a logical line, then Claude Code's renderer may emit explicit hard newlines at paragraph boundaries that combine with VTE's per-row newlines to produce `\n\n` on copy. Not in scope for this plan — should be its own investigation (likely Plan 00048). Capture it before we forget.

## Recommendation (subject to the open-question answer)

- **If `$TMUX` is set**: implement **Path B** — Ansible-deploys a focused `~/.tmux.conf` block, zero CCY churn, low blast radius, fixes the bug for every CC invocation in tmux including host `cc`.
- **If `$TMUX` is empty**: implement **Path A** — delete one env var in CCY entrypoint, accept Shift-drag for selection. Optionally also do Path B as a defence-in-depth in case the user later starts using tmux.

Either way the implementation surface is small and Ansible-deployable. Path B is on the safer side because it doesn't change Claude Code's mouse-capture behaviour for anyone who doesn't run tmux.

## Outstanding non-fix observations worth noting

- Anthropic issues #2301, #9902, #12953, #23581, #38810 all track this. None resolved. The fullscreen-rendering docs are Anthropic's effective response. (research-01, research-04)
- VTE (gnome-terminal, Ptyxis) has **no per-user "wheel-on-alt-screen" knob**, so any emulator-side fix is impossible on the user's likely daily-driver setup. tmux or Claude Code's own env vars are the only intervention points. (research-02, research-04)
- The user is *not* on xterm (which DOES have `*alternateScroll: false`), so the easiest emulator-level fix is out of reach.

## Chosen direction: Path C+ — auto-configure good emulators, warn loudly on bad ones

User decision after reviewing A/B/C: deploy Path C config to all supported emulators AND add a runtime pre-flight check inside `claude-yolo` that detects the current terminal and either silently proceeds (supported emulator → Path C config already deployed) or prints a red abort banner (VTE-based → Path A fallback or switch terminals).

### Recommended "best" emulator for Claude Code on Fedora GNOME

**Correction (2026-05-27)**: This document originally named **ghostty** as the recommendation. That was wrong — research-02 already flagged Ghostty as unable to remap wheel events (no `mouse-binding` syntax shipped in v1.x), and a direct fetch of `ghostty.org/docs/config/reference` on 2026-05-27 confirmed: zero wheel/mouse-binding directives, only keyboard `keybind`s. The original synthesis confused Ghostty's keyboard `keybind` syntax with a hypothetical `mouse_scroll_up` directive that does not exist. Apologies for the noise.

**Actual recommendation: kitty**, for these reasons:

- Mature and stable; mouse-binding syntax (`mouse_map …`) has been first-class for years.
- Supports the Kitty graphics protocol (its own creator, so this is the reference implementation — matters if Anthropic ever ships inline images in CC).
- Wayland-native, GPU-accelerated.
- Per-line, well-documented config in `~/.config/kitty/kitty.conf`.
- Already installed by `playbooks/imports/play-terminal-emulators.yml`.

**Runner-up: alacritty** — simpler TOML config, also works for wheel remap, but no graphics protocol (caps future CC inline-image features). Already installed.

**Tertiary: wezterm** — cleanest API (`SendKey { key = 'PageUp' }`) but not installed by the standard playbook, and Lua config is more surface area than this case needs. Supported by detection but not deployed by default.

**Avoid for CC** (wheel-remap not possible at emulator level): **ghostty** (no `mouse-binding` config), gnome-terminal / Ptyxis / any VTE (no binding syntax at all), konsole (no wheel token in key-bindings), foot (no `send_key` action — half-measure only).

### Pre-flight check in `claude-yolo` (host wrapper)

Lives in `files/var/local/claude-yolo/claude-yolo` (host side, before the container starts). Runs detection on the user's actual terminal, not inside the container.

**Detection chain** (most reliable → least):

1. `$TERM` matches `xterm-kitty*` / `alacritty*` / `xterm-ghostty*` / `ghostty*` / `wezterm*` → set `current_term`
2. `$KITTY_PID` / `$GHOSTTY_*` / `$ALACRITTY_LOG` / `$WEZTERM_PANE` env vars → set `current_term`
3. `ps -o comm= -p "$PPID"` → parent process name (`kitty` / `ghostty` / `alacritty` / `wezterm-gui` / `gnome-terminal-` / `ptyxis-agent` / `tilix` / `konsole` / `foot` / `xterm`)
4. Fallback → `unknown`

**Supported set** (wheel-remap works at emulator level): `kitty alacritty wezterm`. **Unsupported but detectable**: `ghostty foot konsole gnome-terminal ptyxis tilix xterm`. (Ghostty moved out of the supported set after upstream-docs verification on 2026-05-27 — see correction above.)

**Behaviour matrix**:

| Detected                                          | Action                                                                                   |
| ------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| kitty / alacritty / wezterm                       | Silent proceed — Ansible-deployed config handles the wheel                               |
| ghostty / gnome-terminal / Ptyxis / konsole / VTE | Red banner + table of installed vs supported + abort prompt `[y/N]`; default `N` (abort) |
| foot                                              | Same as above — half-measure only (can disable 1007 but no PageUp send-key)              |
| unknown                                           | Yellow banner explaining detection failed; show table; prompt the same                   |

Override env var: `CCY_ACCEPT_UNSUPPORTED_TERM=1` (persistent "I know what I'm doing" — adds it to the user's `.bashrc` only on explicit request, never automatically).

### Banner UX sketch

```
┌──────────────────────────────────────────────────────────────────────────────┐
│ ⚠  CCY is running in an UNSUPPORTED terminal emulator: gnome-terminal       │
│                                                                              │
│  Why this matters: Claude Code's mouse-wheel scroll is broken in this        │
│  terminal — wheel events clobber your prompt input with command history     │
│  recall. This is a VTE/GNOME limitation we cannot work around at the         │
│  emulator level.                                                             │
│                                                                              │
│  Supported emulators on this system:                                         │
│                                                                              │
│    Emulator       Installed   Supported   Recommended                        │
│    ────────────   ─────────   ─────────   ───────────                        │
│    kitty              ✓           ✓        ★ ← best for CC                  │
│    alacritty          ✓           ✓                                          │
│    wezterm            ✗           ✓                                          │
│    ghostty            ✓           ✗        no mouse-binding config           │
│    foot               ✓           —        half-measure only                 │
│    gnome-terminal     ✓           ✗        ← you are here                    │
│    ptyxis             ✗           ✗                                          │
│                                                                              │
│  Recommended: close this window and re-launch CCY in kitty.                  │
│                                                                              │
│  Set CCY_ACCEPT_UNSUPPORTED_TERM=1 to skip this check permanently.           │
└──────────────────────────────────────────────────────────────────────────────┘

Continue anyway? [y/N]
```

Default to N. Enter or N aborts. Only explicit `y` continues. The abort branch tells the user to open kitty (or alacritty), cd to the project root, and run `ccy` — NOT `kitty -e ccy`, which inherits the launching shell's cwd (breaks ccy's git-repo-root requirement) and closes the window the moment ccy exits.

### Phase 3 task breakdown

- [ ] ⬜ **Task 3.1**: Add wheel→PageUp config to each supported emulator (single playbook: `playbooks/imports/play-terminal-emulators.yml`, marker `ANSIBLE MANAGED: CC wheel→PageUp`)
  - [ ] ⬜ 3.1.1 — kitty (`~/.config/kitty/kitty.conf`) — `mouse_map wheel_up press ungrabbed send_key Page_Up` + `_down`. The `ungrabbed` clause means primary-screen scrolling AND mouse-grabbing apps (htop/btop) are unaffected.
  - [ ] ⬜ 3.1.2 — alacritty (`~/.config/alacritty/alacritty.toml`) — `[[mouse.bindings]] mouse = "WheelUp"; chars = "[5~"; mode = "Alt"` + `_down` → `[6~`. The `mode = "Alt"` clause scopes to alt-screen.
  - [ ] ⬜ 3.1.3 — wezterm — conditional on binary existing (not installed by the standard playbook). `SendKey { key = 'PageUp' }` with `alt_screen = true`.
  - [ ] ⬜ 3.1.4 — ghostty: **SKIPPED** (no mouse-binding config in v1.x; detection redirects user to kitty/alacritty).
- [ ] ⬜ **Task 3.2**: Verify each emulator's directive against upstream docs at deploy time — kitty + alacritty verified in research-02; ghostty re-verified 2026-05-27 and confirmed impossible; wezterm verified in research-02.
- [ ] ⬜ **Task 3.3**: Add `detect_terminal()` function to `claude-yolo`
  - [ ] ⬜ 3.3.1 — TDD-style test fixture? (host bash script, can be unit-tested with mocked `$TERM`/`$PPID` env)
  - [ ] ⬜ 3.3.2 — Implement the detection chain
  - [ ] ⬜ 3.3.3 — Implement the banner-and-prompt + table renderer (consider `gum` if already a dependency, else plain bash printf)
  - [ ] ⬜ 3.3.4 — Honour `CCY_ACCEPT_UNSUPPORTED_TERM=1` override
- [ ] ⬜ **Task 3.4**: Bump `CCY_VERSION` (minor — new feature) with explanatory comment
- [ ] ⬜ **Task 3.5**: QA: `./scripts/qa-all.bash` (bash + shellcheck) and `./scripts/qa-ctrl-z-patch.bash` not applicable (no patch changes)
- [ ] ⬜ **Task 3.6**: Deploy on HOST (user runs the playbooks) and end-to-end test in each installed supported emulator
- [ ] ⬜ **Task 3.7**: Documentation — update CLAUDE/AnsibleStyle.md or a new docs page noting the supported-terminal pre-flight

### Resolved implementation questions

1. **Where do we keep the supported-terminals list?** Hardcoded in `claude-yolo` (KISS). Confirmed.
2. **UI**: `gum` for the prompt + table. User confirmed gum is already a repo dependency; it's used elsewhere. No new install burden.
3. **Should host `cc` (Plan 00036 parity) get the same check?** **NO — out of scope for this plan.** Verified: Plan 00036 is plan-only, no code shipped (`grep -rn 'CLAUDE_CODE_NO_FLICKER' files/` returns nothing; no `cc()` function deployed). Host `cc` does not currently enter fullscreen rendering, so it does not have the wheel bug. When Plan 00036 ships, it should pick up the pre-flight by reusing `claude-yolo`'s `detect_terminal()` (factor it into a sourceable helper at that point — not now).
4. **Should the playbook also flip the user's `default-terminal-emulator` GNOME setting?** No — too invasive. Recommend ghostty in the banner; let user choose.
5. **What about ssh sessions?** If the user runs CCY over ssh, `$TERM` is inherited from their local terminal. Detection still works. No special handling needed.
6. **Ptyxis on F43**: Ptyxis is GNOME's new container-aware terminal and is VTE-based. Same fate as gnome-terminal — unsupported. Separate enumeration entry in the banner.

### Cross-reference to Plan 00036

When Plan 00036 (host `cc` parity) is implemented, it will start setting `CLAUDE_CODE_NO_FLICKER=1` + `CLAUDE_CODE_DISABLE_MOUSE=1` on the host, at which point host `cc` inherits this exact wheel-clobbers-history bug. Plan 00036's PLAN.md needs a note added: *"Phase X: source `claude-yolo`'s `detect_terminal()` helper and gate `cc` on the same supported-terminal pre-flight."* That note should go into Plan 00036 in the same commit that ships Plan 00036's actual implementation — not as a speculative edit now.

### Why this is the right shape

- Aligns with **fail-fast** (project HARD RULE) — explicit error on bad terminal, no silent degradation.
- Aligns with **IaC** — supported terminals are configured by playbook, not manually.
- Aligns with **YAGNI** — only the four emulators that are realistically GPU/Wayland-modern; no shotgun support.
- Aligns with **"work WITH GNOME, not against it"** (Plan 00044 principle) — we don't try to patch VTE or replace gnome-terminal globally; we just steer the user to a better tool for this specific job.

## Outstanding (separate plans, captured here so we don't forget)

- **Plan 00048 candidate**: double-newline-on-copy when selecting multi-line text — likely soft-wrap unwrapping at the VTE/CC renderer boundary. Orthogonal to wheel.
- **Plan 00036 follow-up**: extend the terminal pre-flight to host `cc` for parity.

## Old next-steps (superseded by Phase 3 above)

1. ~~User answers the `$TMUX` question.~~ Resolved: not in tmux; Path A territory.
2. ~~Path A or Path B.~~ Resolved: Path C+ chosen.
3. Mark Phase 2 ✅ in `PLAN.md` and replace Phase 3 with the task list above.
4. Verify end-to-end on host after deploy.
