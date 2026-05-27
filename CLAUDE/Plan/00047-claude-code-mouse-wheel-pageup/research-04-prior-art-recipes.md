# Research 04 — Prior Art & Community Recipes for Wheel→Page in TUI/tmux

Survey of how the broader Linux/Unix TUI ecosystem solves "mouse wheel in a TUI on the
alternate screen, especially through tmux, must scroll something useful". Findings are
quoted from upstream docs and community sources, with citations at the bottom.

## 1. The Underlying Mechanism: `DECSET 1007` (Alternate Scroll Mode)

This is the linchpin of the whole topic. Per xterm's ctlseqs reference:

> `Ps = 1 0 0 7  ⇒  Enable Alternate Scroll Mode, xterm.  This corresponds to the alternateScroll resource.`

When an application that does **not** enable mouse reporting (`?1000h`/`?1002h`/`?1003h`)
runs on the alternate screen, an `?1007h`-honouring emulator translates the wheel into
cursor-up/down (`\e[A` / `\e[B`). That is exactly what is hitting Claude Code's prompt and
producing the readline history recall. From the xterm FAQ summarised by xterm.js #1007:

> "When wheel scroll events occur, they can either emulate up arrow/down arrow key sequences
> (`^[[A`, `^[[B`) or, if mouse-reporting is enabled (via sequences like `^[[?1000h`, `1002h`
> and `1003h`), send wheel events as button presses in the `^[[M$b$x$y` format. By default,
> arrow-key translations only occur in the Alternate Screen Buffer (which most full-screen
> apps like less use)…"

`\e[?1007h` turns the translation on; `\e[?1007l` turns it off (then wheel events are
*either* swallowed by the emulator's own scrollback, or — under tmux — eaten by tmux).

## 2. Per-Emulator Support for `?1007`

| Emulator                                            | Honours `?1007h`/`?1007l`?                                                                          | User-facing config knob                                                                                                                              |
| --------------------------------------------------- | --------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| xterm                                               | Yes (origin)                                                                                        | `*alternateScroll: true/false` Xresource                                                                                                             |
| kitty                                               | Yes                                                                                                 | **No** explicit `alternate_scroll` knob in `kitty.conf`; only `wheel_scroll_multiplier`/`wheel_scroll_min_lines`. Behaviour is hard-wired on.        |
| alacritty                                           | Yes                                                                                                 | Was `scrolling.faux_multiplier` (deprecated). New behaviour: respect the `?1007` DECSET sent by the app; `scrolling.multiplier` controls line count. |
| ghostty                                             | Yes                                                                                                 | No `alternate_scroll` directive, only `mouse-scroll-multiplier = precision:…,discrete:…`                                                             |
| VTE (gnome-terminal, xfce4-terminal, tilix, Ptyxis) | Yes — but **resets to "on" on every terminal reset**, ignoring an earlier `?1007l`. Debian #921537. |                                                                                                                                                      |
| Konsole                                             | Yes (Phabricator D12140 added it)                                                                   |                                                                                                                                                      |
| iTerm2, WezTerm, Windows Terminal                   | Yes                                                                                                 |                                                                                                                                                      |

Key takeaway: **almost every modern emulator honours `?1007`**, but only xterm exposes a
durable user-facing setting (`alternateScroll`). On VTE, even `printf '\e[?1007l'` is
clobbered after any reset (the bug the user is hitting on Fedora's gnome-terminal /
Ptyxis):

> "With VTE-based terminals … the mouse wheel behavior is reset to the hardcoded default
> after a terminal reset, unlike xterm which preserves the setting." (Debian #921537)

There is **no per-user kitty/ghostty/VTE config flag** that says "don't synthesise
arrow-keys on altscreen". The mechanism is purely via the `?1007l` escape sequence sent
by the application — or by something *between* the emulator and the application.

## 3. tmux is That "Something In Between"

When tmux is in the loop it presents its own virtual terminal to the inner application
and decides what to do with wheel events itself. The official upstream **tmux Recipes
wiki page** gives the canonical recipe (titled "Send `Up` and `Down` keys for the mouse
wheel"). The advanced form is the one that's relevant here:

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

Trivially adaptable: replace `Up`/`Down` with `PageUp`/`PageDown`, or replace the inner
branch with `copy-mode -e` so the wheel enters tmux's scrollback regardless of altscreen
state. Issue tmux/tmux#4952 asks upstream to make a variant of this the default; it was
**closed without merge** — tmux's stance is "configure it yourself, the recipe is in the
wiki".

The popular "sane scrolling" snippet (from the Stuck-in-Tmux-Scroll-Up writeup) is a
shorter cousin:

```tmux
bind -n WheelUpPane if-shell -F -t = "#{mouse_any_flag}" \
    "send-keys -M" \
    "if -Ft= '#{pane_in_mode}' 'send-keys -M' 'copy-mode -e; send-keys -M'"
```

That one drops to copy-mode unconditionally on altscreen — which is **option (a)** in the
user's problem statement (scrollback in tmux instead of PageUp/PageDown to the app).

## 4. How Each Class of TUI Behaves

- **vim / neovim**: enables full mouse mode with `:set mouse=a`. Once that is set the
  app receives proper SGR mouse events (`\e[<…M`) — no `?1007` involvement, no history
  destruction. The well-known recipe is `set ttymouse=sgr` + tmux `set -g mouse on`.
  Claude Code does **not** enable mouse mode, which is why it falls through to `?1007`'s
  arrow-key fallback.
- **less / man / git-pager**: `less` does **not** enable its own mouse mode by default.
  `less --mouse` (or `LESS=--mouse` env var, available since less 569) makes `less`
  enable mouse tracking and own the wheel. Without it, scrolling works only because
  `?1007h` synthesises arrow keys.
- **htop / btop / lazygit / k9s / gh-dash**: all enable mouse mode explicitly (ncurses
  `mousemask(BUTTON4_PRESSED|BUTTON5_PRESSED,...)` or bracketed mouse via crossterm/
  bubbletea). They receive raw wheel events and bind them to scroll actions internally.
  This is the class Claude Code is **not** in.
- **Claude Code (the TUI in question)**: Ink-based. Ink does not enable mouse reporting,
  so `?1007h` translation kicks in, the arrows go to the prompt's history nav, and the
  user loses input.

## 5. Claude Code-Specific Discussion (Community)

Three open issues track this exact symptom:

- **anthropics/claude-code#12953** — "Mousewheel scrolls through input history instead of
  chat history". Reporter notes it began after a `watch` command emitted escape sequences
  inside Bash tool output. No fix yet; labels `area:tui`, `bug`, `has repro`.
- **anthropics/claude-code#9902** — "Mouse scroll in tmux scrolls input box instead of
  output". Closed as duplicate, no resolution.
- **anthropics/claude-code#38810** — "Claude Code captures mouse events in tmux, making
  scrollback completely unusable". Proposes three fixes by the reporter (not staff):
  detect tmux & route wheel to viewport; add `--no-mouse` / `disableMouseCapture`; add
  keyboard PageUp/Down.

Claude Code docs (code.claude.com/docs/en/fullscreen) acknowledge: *"Claude Code detects
mouse wheel and trackpad scrolling issues at runtime and mitigates them automatically,
with the best scroll experience available by upgrading to 2025.3 or later."* — i.e. there
is an in-app mitigation but it has not landed reliably for tmux users.

The hboon.com "Using tmux with Claude Code" writeup recommends only `set -g mouse on` and
notes the author falls back to tmux copy-mode rather than the wheel.

No third-party dotfile/recipe surfaced that solves it for Claude Code specifically; the
problem is recent enough that the standard tmux "altscreen wheel" recipe is the de-facto
answer.

## 6. General-Purpose Translators

There is an old `mouseterm` (macOS-only, Terminal.app injector — not relevant here).
On Linux there is no equivalent userland tool: the `?1007` translation happens inside
the emulator's event loop and is not interceptable from outside. tmux is effectively
*the* general-purpose translator because every wheel event passes through it.

## 7. Bottom Line — Ranked Options

Comparing every approach against (1) likelihood-of-working, (2) Ansible-deployability,
(3) blast radius.

| #   | Approach                                                                                                             | Works?                                                                                                                                                                                                                           | Ansible-deployable?                                                | Blast radius                                                                                                                                                                                                                              |
| --- | -------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **tmux `WheelUpPane`/`WheelDownPane` binding sending `PageUp`/`PageDown`** (variant of the upstream Recipes snippet) | **High** — tmux is in the loop, the binding intercepts every wheel event before the inner app sees it. Works on any emulator (kitty, ghostty, Ptyxis, gnome-terminal, alacritty), because tmux owns the translation.             | **Trivial** — one `blockinfile` on `~/.tmux.conf`, marker-managed. | **Low/medium** — affects every TUI inside tmux. Most TUIs that want raw wheel events set `#{mouse_any_flag}` via `?1000h`, which the recipe already short-circuits with `send -M`. vim, htop, lazygit, less --mouse all continue to work. |
| 2   | **tmux `WheelUpPane` → `copy-mode -e`** (the "scrollback in tmux" variant)                                           | **High** — same mechanism.                                                                                                                                                                                                       | **Trivial** — one blockinfile.                                     | **Low** — same exclusion via `mouse_any_flag`. Differs from #1 only in that the user gets tmux scrollback instead of in-app paging.                                                                                                       |
| 3   | **Emit `\e[?1007l` once per shell** (e.g. from `.bashrc`/`.zshenv` via `printf`)                                     | **Low** — under tmux this never reaches the outer emulator's `?1007` state because tmux is doing the translation, not the emulator. Outside tmux it works on xterm/kitty/alacritty/ghostty but is **wiped by any reset on VTE**. | **Trivial**, but mostly useless inside tmux.                       | **Low** — affects only the shell that emits it.                                                                                                                                                                                           |
| 4   | **Switch / configure emulator (`*alternateScroll: false` on xterm; nothing equivalent on kitty/ghostty/VTE)**        | **Low** — xterm-only knob exists; user is on Fedora desktop, likely Ptyxis/VTE, where no such knob exists and the bug-prone reset behaviour bites.                                                                               | Possible for xterm via Xresources; otherwise N/A.                  | Low.                                                                                                                                                                                                                                      |
| 5   | **Patch Claude Code to enable mouse reporting (`?1000h`) or to ignore bare arrow keys at prompt**                    | Would fix it definitively, but requires upstream change; in-flight via issues #9902 / #12953 / #38810.                                                                                                                           | Not us.                                                            | Not our call.                                                                                                                                                                                                                             |
| 6   | **`less --mouse`-style env-var fix in Claude Code**                                                                  | N/A — no such flag exposed.                                                                                                                                                                                                      | N/A.                                                               | N/A.                                                                                                                                                                                                                                      |

**Recommendation**: option #1 — a tmux `WheelUpPane`/`WheelDownPane` binding that sends
`PageUp`/`PageDown` on the alternate screen (and falls through to `send -M` whenever the
inner app has actually requested mouse events) is the unique recipe that (a) is known to
work, (b) is upstream-blessed, (c) deploys as one Ansible `blockinfile`, and (d) leaves
every other TUI untouched. Option #2 is an equally cheap fallback if the user prefers
tmux scrollback to in-app PageUp.

## Sources

- xterm ctlseqs — DECSET 1007 — https://invisible-island.net/xterm/ctlseqs/ctlseqs.html
- xterm.js #1007 (background on `?1007`) — https://github.com/xtermjs/xterm.js/issues/1007
- tmux Recipes wiki ("Send Up and Down keys for the mouse wheel") — https://github.com/tmux/tmux/wiki/Recipes
- tmux/tmux#4952 (request to make it default; closed) — https://github.com/tmux/tmux/issues/4952
- Debian bug #921537 (VTE resets `?1007`) — https://bugs.debian.org/cgi-bin/bugreport.cgi?bug=921537
- Launchpad #106995 (gnome-terminal unconditional wheel interpretation) — https://bugs.launchpad.net/bugs/106995
- Konsole D12140 (KDE adds alternate scroll mode) — https://phabricator.kde.org/D12140
- alacritty/alacritty#4583 (`alternate_scroll` use) — https://github.com/alacritty/alacritty/issues/4583
- alacritty PR #946 (faux scrolling implementation) — https://github.com/alacritty/alacritty/pull/946
- kitty kitty.conf reference — https://sw.kovidgoyal.net/kitty/conf/
- ghostty option reference — https://ghostty.org/docs/config/reference
- ghostty discussion #4617 (tmux scrollback on macOS) — https://github.com/ghostty-org/ghostty/discussions/4617
- anthropics/claude-code#12953 — https://github.com/anthropics/claude-code/issues/12953
- anthropics/claude-code#9902 — https://github.com/anthropics/claude-code/issues/9902
- anthropics/claude-code#38810 — https://github.com/anthropics/claude-code/issues/38810
- Claude Code fullscreen docs — https://code.claude.com/docs/en/fullscreen
- hboon.com "Using tmux with Claude Code" — https://hboon.com/using-tmux-with-claude-code/
- freeCodeCamp "tmux in practice: scrollback buffer" — https://www.freecodecamp.org/news/tmux-in-practice-scrollback-buffer-47d5ffa71c93/
- dandavison/delta#58 (less + `?1007h`) — https://github.com/dandavison/delta/issues/58
