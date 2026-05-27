# Research 02 — Terminal-Emulator Wheel Behaviour When Mouse Mode Is OFF

Plan: `CLAUDE/Plan/00047-claude-code-mouse-wheel-pageup/`. Task 1.2.

This file documents what each common Linux terminal emulator does with the mouse
scroll-wheel when the foreground application has **not** enabled mouse-reporting
(no DECSET `?1000/1002/1003/1006` in effect). The key concept is the **xterm
"alternate scroll" mode** (DECSET `1007`): on the alternate screen, the wheel is
translated to arrow Up/Down keystrokes and sent to the application.

## The Core Mechanism — DECSET 1007 (`alternateScroll`)

xterm originated this. *"By default, wheel mouse events are translated to
scroll-back and scroll-forward actions that normally scroll the whole window …
However if Alternate Scroll mode is set, then cursor up/down controls are sent
when the terminal is displaying the alternate screen."* Disable with
`printf '\e[?1007l'` (`XTERM_ALTBUF_SCROLL`).

Source: <https://invisible-island.net/xterm/ctlseqs/ctlseqs.html>,
[movementarian.org](https://movementarian.org/blog/posts/2023-11-04-scroll-wheel-in-vim/).

This is the root cause of the user's symptom: Claude Code runs on the alternate
screen, so the emulator helpfully turns the wheel into Up/Down arrows, which the
prompt's readline-style binding consumes as history-up / history-down.

---

## gnome-terminal / VTE

**Default**: emits **Up/Down arrows** on the altscreen (alternate-scroll mode is
on). Hardcoded "translate wheel to arrow keys" loop in `vte_terminal_scroll()`
since VTE ~0.40.

**User-configurable?** *Effectively no*, in two senses:

- There is **no `mouse_bindings` config** at all; VTE has no key/mouse binding
  syntax for end users.
- The "send keystrokes for altscreen scrolling" toggle that used to live in the
  gnome-terminal Profile UI (GNOME bug #538195) was **removed** in newer
  gnome-terminal. The setting `scroll-on-keystroke` and the per-profile
  `alternate-screen-scroll` boolean were dropped or are no longer surfaced.

**The only knob the user has** is the DECSET 1007 escape sequence —
`printf '\e[?1007l'` — but this is set by the *application*, not by VTE, and is
reset on every screen-mode toggle. Some shells / wrappers send it on each prompt.

**Inside tmux**: tmux on the host's altscreen *also* honours alternate-scroll
mode; with `set -g mouse on` tmux intercepts the raw wheel buttons (b64/b65)
itself and they never become arrow keys. With `mouse off`, behaviour follows
VTE.

Sources:
[gitlab.gnome.org/GNOME/gnome-terminal#27](https://gitlab.gnome.org/GNOME/gnome-terminal/-/issues/27),
[bugzilla.gnome.org #538195](https://bugzilla.gnome.org/show_bug.cgi?id=538195),
[movementarian.org](https://movementarian.org/blog/posts/2023-11-04-scroll-wheel-in-vim/).

---

## kitty

**Default**: scroll wheel scrolls kitty's *own* scrollback on the main screen;
on the altscreen the events are **passed to the app** as Up/Down arrows (same
1007 convention).

**Fully user-configurable**. From `kitty.conf` reference:

```conf
mouse_map shift+wheel_up   press ungrabbed send_key shift+Page_Up
mouse_map shift+wheel_down press ungrabbed send_key shift+Page_Down
mouse_map shift+wheel_up   press grabbed   send_key shift+Page_Up
mouse_map shift+wheel_down press grabbed   send_key shift+Page_Down
```

- `grabbed` = application has enabled mouse reporting; `ungrabbed` = it has not.
- For our use case (Claude disables mouse → ungrabbed):

```conf
mouse_map wheel_up   press ungrabbed send_key Page_Up
mouse_map wheel_down press ungrabbed send_key Page_Down
```

Source: <https://sw.kovidgoyal.net/kitty/conf/>, see also
[issue #2819](https://github.com/kovidgoyal/kitty/issues/2819).

---

## alacritty

**Default**: scrolls Alacritty's own scrollback on the main screen; on the
altscreen sends Up/Down arrows (1007 mode, default-on).

**Fully user-configurable** via `mouse.bindings` in TOML:

```toml
[[mouse.bindings]]
mouse = "WheelUp"
chars = "[5~"        # PageUp escape sequence

[[mouse.bindings]]
mouse = "WheelDown"
chars = "[6~"        # PageDown escape sequence
```

`chars` sends a raw byte sequence directly to the PTY; `[5~` and
`[6~` are the standard PageUp / PageDown CSI sequences. `action = "ScrollPageUp"` (no chars) scrolls Alacritty's own buffer, not the app.

Source: <https://alacritty.org/config-alacritty-bindings.html>,
[issue #7588](https://github.com/alacritty/alacritty/issues/7588).

---

## wezterm

**Default**: same as everyone else (Up/Down arrows on altscreen). Tunable per
alt-screen with `alt_screen = true|false`.

**Fully user-configurable** with first-class `SendKey`:

```lua
config.mouse_bindings = {
  {
    event = { Down = { streak = 1, button = { WheelUp = 1 } } },
    mods = 'NONE',
    alt_screen = true,
    action = wezterm.action.SendKey { key = 'PageUp' },
  },
  {
    event = { Down = { streak = 1, button = { WheelDown = 1 } } },
    mods = 'NONE',
    alt_screen = true,
    action = wezterm.action.SendKey { key = 'PageDown' },
  },
}
```

`alt_screen = true` scopes the binding to TUI apps; on the main screen the
default scrollback behaviour is kept. There's also a dedicated
`alternate_buffer_wheel_scroll_speed` setting.

Source: <https://wezterm.org/config/mouse.html>.

---

## foot

**Default**: alt-screen mode honoured (`alternate-scroll-mode=yes` is default),
sending **fake Up/Down KeyDOWN events** to the client app — quote from
[foot.ini(5)](https://man.archlinux.org/man/foot.ini.5.en):

> "Alt screen: send fake _KeyUP_ events to the client application, if alternate scroll mode is enabled."

**Configurable** via the `[mouse-bindings]` section, but the action vocabulary
is limited. Wheel events appear as `BTN_WHEEL_BACK` / `BTN_WHEEL_FORWARD` and
can be bound to any of foot's actions:

```ini
[mouse-bindings]
scrollback-up-mouse=BTN_WHEEL_BACK
scrollback-down-mouse=BTN_WHEEL_FORWARD
```

**Caveat**: foot's actions are foot-internal (e.g. `scrollback-up-page`); it
has *no* `send-key` action that injects an arbitrary keystroke into the PTY, so
**foot cannot natively remap wheel→PageUp at the emulator level**. Best the
user can do is set `alternate-scroll-mode=no` to suppress the arrow-key
fallback altogether (wheel does nothing in altscreen).

Sources: <https://man.archlinux.org/man/foot.ini.5.en>,
<https://www.mankier.com/7/foot-ctlseqs>.

---

## ghostty

**Default**: same altscreen-→-arrows behaviour.

**User-configurable?** *Not yet*. Per
[discussion #4169](https://github.com/ghostty-org/ghostty/discussions/4169) and
[#11874](https://github.com/ghostty-org/ghostty/discussions/11874), Ghostty has
no `mouse-binding` syntax approved as of v1.x; only keyboard `keybind`s are
supported. Wheel-to-key remapping is a requested-but-not-shipped feature.

Source: <https://ghostty.org/docs/config/reference>.

---

## xterm (the original)

**Default**: alternate-scroll mode **off by default**. Enable with the X
resource:

```
xterm*alternateScroll: true
```

or `xterm -xrm '*alternateScroll: true'`. Once on, wheel→Up/Down on altscreen.
The DECSET `1007` sequence is the standard runtime toggle that other emulators
re-implement.

Source: <https://invisible-island.net/xterm/ctlseqs/ctlseqs.html>,
[ArchWiki: xterm](https://wiki.archlinux.org/title/Xterm).

---

## konsole (KDE)

**Default**: supports DECSET 1007 since 2018
([Phabricator D12140](https://phabricator.kde.org/D12140)) — altscreen wheel
emits Up/Down arrows when the app sets it.

**User-configurable?** Only via the per-profile **Key Bindings** editor
(Settings → Edit Current Profile → Keyboard). The binding language is
keyboard-focused; **there is no `wheel_up`/`wheel_down` token**. So Konsole is
in the same bucket as VTE/foot: it has 1007 mode but no native emulator-level
remap to PageUp.

Source: [Konsole Handbook ch.5](https://docs.kde.org/trunk_kf6/en/konsole/konsole/key-bindings.html).

---

## tmux as a Special Case

When the user runs Claude Code inside tmux **with `set -g mouse on`**, tmux
itself enables DEC mouse reporting upstream of Claude. The emulator now sends
*raw* mouse-button escape sequences, not arrow keys, regardless of altscreen
state. Tmux then decides what to do (defaults: enter copy-mode on wheel-up in
the active pane). The user's wheel-clobbers-prompt symptom therefore implies
**no tmux, or `mouse off`** — tmux is not currently rewriting wheel events.

A typical fix-at-the-tmux-layer if the user wanted one:

```tmux
set -g mouse on
bind -T root WheelUpPane   send-keys -t = PageUp
bind -T root WheelDownPane send-keys -t = PageDown
```

Source: [tmux/tmux #1302](https://github.com/tmux/tmux/issues/1302).

---

## `CLAUDE_CODE_DISABLE_MOUSE=1`

Per `CLAUDE/Plan/00036-cc-ccy-parity/PLAN.md` (lines mentioning the variable),
this env-var instructs Claude Code itself to **not enable** terminal mouse mode
(`?1000h` family). It does **not** touch the terminal emulator. Effect: the
emulator's *fallback* behaviour (altscreen 1007 → Up/Down arrows) is exactly
what the user sees. Without the var, Claude would grab raw mouse events and
silently swallow the wheel; with it set, the wheel becomes arrow keys via 1007.

So: **`CLAUDE_CODE_DISABLE_MOUSE=1` does not cause the problem — it
*reveals* it**, by leaving the emulator in charge of wheel translation.

---

## Detection — Which Emulator Is the User On?

This repo (`playbooks/imports/play-terminal-emulators.yml`) installs **all of
alacritty, kitty, ghostty, foot** in one go. Fedora-GNOME ships
**gnome-terminal/VTE** as the system default. The user has not told us which
they actually launch Claude Code from, so probe at runtime:

```bash
# Best signal: the emulator self-identifies in env
echo "TERM=$TERM TERM_PROGRAM=$TERM_PROGRAM COLORTERM=$COLORTERM"
echo "VTE_VERSION=$VTE_VERSION KITTY_WINDOW_ID=$KITTY_WINDOW_ID"
echo "ALACRITTY_LOG=$ALACRITTY_LOG WEZTERM_PANE=$WEZTERM_PANE"
echo "GHOSTTY_RESOURCES_DIR=$GHOSTTY_RESOURCES_DIR FOOT_VERSION=$FOOT_VERSION"

# Fall-back: parent process name
ps -o comm= -p "$(ps -o ppid= -p $$ | tr -d ' ')"
```

Strong indicators:

| Var set                  | Emulator                  |
| ------------------------ | ------------------------- |
| `$VTE_VERSION`           | gnome-terminal / any VTE  |
| `$KITTY_WINDOW_ID`       | kitty                     |
| `$ALACRITTY_LOG`         | alacritty                 |
| `$WEZTERM_PANE`          | wezterm                   |
| `$GHOSTTY_RESOURCES_DIR` | ghostty                   |
| `$FOOT_VERSION`          | foot                      |
| `$TERM_PROGRAM=tmux`     | inside tmux (check outer) |

`$TERM=xterm-256color` is uninformative — almost everyone sets it.

---

## Bottom Line — Ranked Remap Ease

| Rank | Emulator      | Remap wheel → PageUp at emulator config?   | Effort         |
| ---- | ------------- | ------------------------------------------ | -------------- |
| 1    | **wezterm**   | Yes, first-class `SendKey { key='PageUp'}` | Trivial        |
| 2    | **kitty**     | Yes, `mouse_map … send_key Page_Up`        | Trivial        |
| 3    | **alacritty** | Yes, `chars = "[5~"`                                            | Trivial        |
| 4    | **xterm**     | No native remap, but `alternateScroll` off | Indirect       |
| 5    | **foot**      | No emulator-level send-key; can disable    | Workaround     |
| 6    | **konsole**   | No wheel token in key bindings             | Not feasible   |
| 7    | **gnome/VTE** | No mouse bindings at all                   | **Impossible** |
| 8    | **ghostty**   | Feature not shipped                        | **Impossible** |

**Which emulator is the user on?**

The repo installs *four* third-party terminals but the user is on Fedora GNOME
where **gnome-terminal/VTE is the default** launched by the GNOME shell,
keyboard shortcut Ctrl-Alt-T, and the Files "Open Terminal" action. Unless the
user has switched their default to one of the installed alternatives (no
gsettings playbook in this repo flips `org.gnome.desktop.default-applications.terminal`),
**the most likely emulator is gnome-terminal/VTE** — which is the *worst case*
for emulator-side remap.

This pushes the fix away from the emulator and toward either:

1. **Tmux interception** — `bind -T root WheelUpPane send-keys -t = PageUp`
   (works under any emulator, requires running inside tmux).
2. **Convince the user to switch** to one of the already-installed kitty /
   alacritty / wezterm and apply the per-emulator config above.
3. **Claude Code prompt-binding change** — make ↑/↓ at column 0 of an
   already-typed line *not* trigger history navigation. This would be the
   universal fix and is independent of the emulator. (Out of scope for this
   research file; flagged for Plan 00047 design phase.)

---

### Sources

- [xterm ctlseqs (canonical)](https://invisible-island.net/xterm/ctlseqs/ctlseqs.html)
- [movementarian.org — wheel-in-vim](https://movementarian.org/blog/posts/2023-11-04-scroll-wheel-in-vim/)
- [gnome-terminal issue #27](https://gitlab.gnome.org/GNOME/gnome-terminal/-/issues/27)
- [GNOME bz #538195](https://bugzilla.gnome.org/show_bug.cgi?id=538195)
- [kitty.conf reference](https://sw.kovidgoyal.net/kitty/conf/)
- [alacritty bindings](https://alacritty.org/config-alacritty-bindings.html)
- [wezterm mouse bindings](https://wezterm.org/config/mouse.html)
- [foot.ini(5)](https://man.archlinux.org/man/foot.ini.5.en)
- [ghostty mouse-binding discussion #4169](https://github.com/ghostty-org/ghostty/discussions/4169)
- [Konsole D12140 (1007 support)](https://phabricator.kde.org/D12140)
- [tmux altscreen wheel #1302](https://github.com/tmux/tmux/issues/1302)
