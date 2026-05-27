# Research 03 — tmux mouse-wheel → PageUp / scrollback binding for Claude Code

Scope: how to configure tmux so the host terminal's mouse wheel engages tmux's own scrollback (or sends PageUp/PageDown into the inner process) instead of Claude Code receiving raw arrow-key escape sequences that clobber the prompt history.

## 1. What `set -g mouse on` actually does

`set -g mouse on` is the single switch introduced in **tmux 2.1** (Oct 2015) that replaced the old `mode-mouse`, `mouse-select-pane`, `mouse-select-window`, `mouse-resize-pane`, and `mouse-utf8` quartet. The 2.1 CHANGES entry is blunt:

> "Mouse-mode has been rewritten. There is no longer options for mouse-this and mouse-that, instead mouse events may be bound as keys and there is one option 'mouse' that turns on mouse support entirely." ([tmux/CHANGES](https://github.com/tmux/tmux/blob/master/CHANGES))

With `mouse on`:

- tmux requests SGR mouse tracking from the host terminal (`\e[?1000;1002;1006h`-style sequences). The host stops translating wheel-to-arrow-keys and starts sending raw SGR mouse events to tmux.
- tmux dispatches each event through its key tables (`root`, `prefix`, `copy-mode`, …) using synthetic key names: `WheelUpPane`, `WheelDownPane`, `MouseDown1Pane`, `MouseDrag1Pane`, etc.
- The **default** root-table binding for `WheelUpPane` is roughly `if -Ft= '#{mouse_any_flag}' 'send -M' 'select-pane -t=; copy-mode -e; send -M'` — i.e. if the inner app has itself enabled mouse tracking, forward the SGR event raw (`send -M`); otherwise auto-enter copy-mode and start scrolling tmux's scrollback. This is the behaviour confirmed in [tmux#3705](https://github.com/tmux/tmux/issues/3705): "mouse wheel up enters copy mode and shows scrollback buffer".
- It does **not** "suppress" the terminal's wheel-to-arrow fallback as a separate action — that fallback only fires when no mouse tracking is requested, and turning mouse on requests it, so the fallback never gets to run.

`#{mouse_any_flag}` is true when the inner pane has issued any of `?1000h / ?1002h / ?1003h`. `#{alternate_on}` is true when the inner pane has switched to the alternate screen (`?1049h`). `#{pane_in_mode}` is true when the pane is already in copy-mode or another tmux mode.

## 2. Default behaviour for a TUI on alternate screen that has NOT enabled mouse mode

For a pane on the alternate screen (`alternate_on=1`) with no mouse tracking (`mouse_any_flag=0`), the default `WheelUpPane` binding **enters copy-mode and starts scrolling tmux's scrollback**. That is already very close to what we want for Claude Code — *if* Claude Code did not enable mouse tracking.

Caveat (and the heart of this whole problem): **Claude Code DOES enable mouse tracking.** Per [claude-code#23581](https://github.com/anthropics/claude-code/issues/23581) and [#38810](https://github.com/anthropics/claude-code/issues/38810): "Claude Code enables mouse tracking escape sequences (`\e[?1000h, \e[?1003h, \e[?1006h`)". With these set, `mouse_any_flag=1`, the default binding falls into the `send -M` branch, raw SGR mouse bytes hit Claude Code, and Claude Code interprets wheel-up as Up-arrow (history recall). That is exactly the user-visible bug.

## 3. The canonical tmux wiki recipe (verbatim)

From [tmux wiki → Recipes → "Send Up and Down keys for the mouse wheel"](https://github.com/tmux/tmux/wiki/Recipes):

**Variant 1 — minimal:**

```tmux
bind -n WheelUpPane   if -Ft= "#{mouse_any_flag}" "send -M" "send Up"
bind -n WheelDownPane if -Ft= "#{mouse_any_flag}" "send -M" "send Down"
```

Line-by-line:

- `bind -n WheelUpPane` — bind in the *root* table (`-n` ≡ `-T root`), no prefix required.
- `if -Ft= "#{mouse_any_flag}" …` — `-F` evaluates the format expression as a boolean; `-t=` targets the pane currently under the mouse.
- True branch `send -M` — pass the raw mouse event through to the inner app (it asked for mouse, give it mouse).
- False branch `send Up` — synthesise an Up-arrow keystroke into the pane.

The wiki notes "the `mouse` option must also be `on`".

**Variant 2 — alternate-screen aware, recommended:**

```tmux
bind -n WheelUpPane {
    if -F '#{||:#{pane_in_mode},#{mouse_any_flag}}' {
        send -M
    } {
        if -F '#{alternate_on}' { send-keys -N 3 Up } { copy-mode -e }
    }
}
bind -n WheelDownPane {
    if -F '#{||:#{pane_in_mode},#{mouse_any_flag}}' {
        send -M
    } {
        if -F '#{alternate_on}' { send-keys -N 3 Down }
    }
}
```

Line-by-line:

- Outer guard `#{||:#{pane_in_mode},#{mouse_any_flag}}` — if either tmux is already in copy-mode OR the app captured mouse, forward raw (`send -M`). This preserves normal scroll inside copy-mode and inside btop/vim-with-mouse.
- Else branch: if `alternate_on`, synthesise three Up/Down keystrokes (`-N 3` = repeat 3 times) — the conventional "one wheel notch ≈ 3 lines" feel.
- Else (primary screen, no mouse, not in copy-mode): `copy-mode -e` enters copy-mode in scrolling mode where one more wheel-down exits automatically.
- WheelDownPane intentionally omits the else-branch — once on primary screen there is no "down" scrollback to recover.

## 4. Cleaner modern form

The Variant 2 block syntax (`bind -n WheelUpPane { … }`) requires **tmux ≥ 2.6** (control-flow blocks landed in 2.6, Oct 2017). `if -F` (format predicate, no shell fork) is **tmux ≥ 2.5**. `send-keys -N` is **tmux ≥ 2.4**. All of these are present in every distro-shipped tmux for the last seven years; treat tmux ≥ 3.0 as the safe target.

The pre-2.6 equivalent uses the older `if-shell -F` two-string form:

```tmux
bind-key -T root WheelUpPane \
    if-shell -F -t = '#{mouse_any_flag}' \
        'send-keys -M' \
        'if-shell -F -t = "#{alternate_on}" "send-keys -t = Up Up Up" "select-pane -t =; copy-mode -e"'
```

For Claude Code specifically the only realistic target is "modern tmux"; use Variant 2.

## 5. Failure modes

- **Inner app legitimately wants mouse (btop, htop, mc, vim with `set mouse=a`, nested tmux).** The `#{mouse_any_flag}` guard catches this and forwards raw — those apps keep working. Good.
- **Claude Code is *also* in the `mouse_any_flag=1` camp.** Variant 2 will therefore *still* forward wheel events raw to Claude Code, and the bug stays unfixed. This is the critical finding for this plan: the wiki recipe is not enough on its own for Claude Code. We need a *stronger* predicate that overrides the `mouse_any_flag` check — see Bottom Line below.
- **Copy-mode and text selection.** `MouseDrag1Pane` is bound separately (`copy-mode -M`), and `pane_in_mode` short-circuits the wheel binding once selection has started. Drag-to-select is unaffected.
- **Nested tmux.** Inner tmux requests mouse → outer tmux sees `mouse_any_flag=1` → forwards raw → inner tmux processes it. Works.
- **tmux 2.1 → 2.4 era.** Block syntax not available; use the `if-shell -F` form. Also: tmux 2.1.0 had a regression where wheel scrolling stopped working entirely until users set `mouse on` — see the [Arch forum thread](https://bbs.archlinux.org/viewtopic.php?id=204091). Irrelevant on modern installs.
- **Terminals that natively scroll the alternate screen** (Kitty, WezTerm, iTerm2 with "scroll wheel sends arrow keys"). With tmux mouse on, tmux owns the wheel — the terminal's native behaviour is bypassed. That is the intended trade-off.

## 6. Claude Code-specific references

- [anthropics/claude-code#9902 — "Mouse scroll in tmux scrolls input box instead of output"](https://github.com/anthropics/claude-code/issues/9902) — the canonical bug report. Closed as duplicate; discussion thin.
- [anthropics/claude-code#38810 — "Claude Code captures mouse events in tmux, making scrollback completely unusable"](https://github.com/anthropics/claude-code/issues/38810) — confirms Claude Code emits `?1000h ?1003h ?1006h`.
- [anthropics/claude-code#23581 — "Add option to disable mouse tracking in TUI"](https://github.com/anthropics/claude-code/issues/23581) — open feature request for `--no-mouse` / `CLAUDE_CODE_NO_MOUSE=1` / `"mouseTracking": false`.
- [anthropics/claude-code#27995 — "Add flag to disable mouse/scroll capture in interactive prompts"](https://github.com/anthropics/claude-code/issues/27995) — same theme.
- [anthropics/claude-code#58364 — "iTerm2 + tmux: mouse wheel hijacked to input history"](https://github.com/anthropics/claude-code/issues/58364) — Feb 2026 regression.
- [Yeachan-Heo/oh-my-claudecode#890](https://github.com/Yeachan-Heo/oh-my-claudecode/issues/890) — same problem, third-party wrapper.
- [Hwee-Boon Yar — "Using tmux with Claude Code"](https://hboon.com/using-tmux-with-claude-code/) — recommends `set -g mouse on` plus `set -g allow-passthrough on`, `set -s extended-keys on`, `set -as terminal-features 'xterm*:extkeys'`. Does not solve the wheel→arrow problem; relies on `ctrl-w [` for scrollback.
- [tmux#4952 — "Request: Send up/down by default for mousewheel in alternate screen"](https://github.com/tmux/tmux/issues/4952) — upstream request that the wiki Variant 1 behaviour become default; not yet merged.

None of these sources publishes a Claude-Code-specific binding because Claude Code's mouse-tracking-on default defeats the standard `mouse_any_flag` guard. The fix has to invert the guard.

## Bottom line — the snippet to add

Because Claude Code sets `mouse_any_flag=1`, we cannot use the wiki recipe verbatim. Instead, gate on `alternate_on` *first* (Claude Code's fullscreen TUI is on the alternate screen) and only fall through to `send -M` when the pane is on the primary screen and an app is actively grabbing mouse:

```tmux
# Mouse on, but route the wheel into tmux scrollback / PageUp for fullscreen
# TUIs (Claude Code, less, man, vim) that sit on the alternate screen --
# even when they have requested SGR mouse tracking. Primary-screen apps that
# want mouse (btop, mc) still get raw events.
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

Line by line:

1. `set -g mouse on` — required; without it none of the `Wheel*Pane` bindings ever fire.
2. `bind -n WheelUpPane { … }` — root-table binding, block syntax (tmux ≥ 2.6).
3. `if -F '#{pane_in_mode}' { send -M }` — if the user is already in copy-mode, let copy-mode's own wheel handler do its thing (it scrolls the scrollback).
4. Else, `if -F '#{alternate_on}' { send-keys -N 3 PageUp }` — **the key clause for Claude Code.** Any fullscreen TUI on the alternate screen gets PageUp (×3 per notch), regardless of whether it has requested mouse tracking. Claude Code's prompt input does not bind PageUp, so the keystrokes are inert there; its scrollback (if any) advances. For `less`/`man`/`vim`-without-mouse, PageUp does the right thing.
5. Else (primary screen): preserve the wiki default — apps that asked for mouse get raw events, otherwise enter tmux copy-mode and start scrolling.
6. `WheelDownPane` is symmetric, except the final fallback is `send Down` rather than copy-mode entry (there is nothing below the live cursor to scroll into).

Required tmux version: **≥ 2.6** for block `{ … }` syntax; **≥ 3.0** is the sensible floor. Works with tmux 3.5 (current stable) confirmed via [tmux#4952](https://github.com/tmux/tmux/issues/4952) environment notes.

If Claude Code's alternate-screen TUI eventually responds to PageUp by scrolling its own transcript, the binding becomes "wheel scrolls Claude's transcript directly". Until then, PageUp is harmless and the user falls back to `prefix [` / `Ctrl-w [` to enter tmux copy-mode for scrollback — which is the documented Claude Code workflow per the Hwee-Boon Yar article.

## Sources

- [tmux/tmux Wiki — Recipes](https://github.com/tmux/tmux/wiki/Recipes)
- [tmux/CHANGES (2.1 mouse rewrite)](https://github.com/tmux/tmux/blob/master/CHANGES)
- [tmux#3705 — copy-mode entry on wheel-up with alternate screen](https://github.com/tmux/tmux/issues/3705)
- [tmux#4952 — default mousewheel→arrow request](https://github.com/tmux/tmux/issues/4952)
- [anthropics/claude-code#9902, #23581, #27995, #38810, #58364](https://github.com/anthropics/claude-code/issues/38810)
- [Hwee-Boon Yar — Using tmux with Claude Code](https://hboon.com/using-tmux-with-claude-code/)
- [Arch Linux Forum — tmux 2.1 mouse config](https://bbs.archlinux.org/viewtopic.php?id=204091)
- [tmux(1) man page](https://man.openbsd.org/tmux)
