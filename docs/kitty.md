# Kitty Terminal — Configuration & Usage

What the `play-terminal-emulators.yml` playbook deploys to `~/.config/kitty/kitty.conf`, and how to use it day-to-day.

This is a reference, not a tutorial — see [the kitty manual](https://sw.kovidgoyal.net/kitty/) for the full feature set. Everything below is just *what's switched on in this repo* and *what you actually press*.

## Source of truth

All kitty settings deployed by this repo live inside a single Ansible-managed block in `playbooks/imports/play-terminal-emulators.yml`. The block is delimited in `kitty.conf` by:

```
# BEGIN ANSIBLE MANAGED: kitty UX defaults (Plan 00047)
…
# END ANSIBLE MANAGED: kitty UX defaults (Plan 00047)
```

Anything *outside* those markers is your personal config and the playbook will not touch it. Re-running the playbook is idempotent — it replaces the block in-place.

To re-deploy after edits:

```bash
ansible-playbook ~/Projects/fedora-desktop/playbooks/imports/play-terminal-emulators.yml --tags kitty
```

## What's configured

### Shell integration

```
shell_integration enabled
```

OSC 7 cwd reporting on every prompt so new tabs/windows inherit the current directory. Default in recent kitty, set explicitly here for deterministic behaviour across upgrades.

### Tab / window creation with cwd inheritance

| Keybind            | Action                                      |
| ------------------ | ------------------------------------------- |
| `Ctrl+Shift+T`     | New tab in the **current** working dir      |
| `Ctrl+Shift+Enter` | New window split in the current working dir |

Default kitty `Ctrl+Shift+T` opens a tab in your home dir — these bindings make it open where you actually are. Relies on `shell_integration enabled` above.

### Scrollback

```
scrollback_lines               10000
scrollback_pager_history_size  10
```

- **Live buffer**: 10 000 lines (default is 2 000) — wide enough to wheel back through a long Claude Code conversation without losing the top.
- **Pager history**: 10 MB of extra history kept on disk; the pager (`Ctrl+Shift+H`) can search further back than the on-screen buffer.

### Tab bar

```
tab_bar_edge      top
tab_bar_style     slant
tab_bar_min_tabs  1
tab_title_template "{index}: {title}"
```

- Tabs at the top with slanted edges, so they read as GUI tabs rather than a status strip.
- Bar visible even with one tab (default hides it until you open a second).
- Each tab labelled `1: <title>`, `2: <title>` — index makes tabs identifiable at a glance.

### Window / bell behaviour

```
confirm_os_window_close 0
enable_audio_bell       no
window_padding_width    4
```

- `Ctrl+Shift+Q` (and the window close button) close immediately with no "are you sure?" prompt, even with several tabs open.
- Silent bell — Claude Code rings the terminal bell when a long task finishes; visual bell symbol on the tab and window-alert stay on (kitty defaults), so you still see which background tab just finished without an audible nag.
- 4-pixel padding around the text grid.

### URL handling

```
mouse_map right press ungrabbed show_window_context_menu
map       ctrl+shift+u           kitten hints --type url --program @
```

Two ways to copy or open URLs without precise drag-selection:

**Right-click context menu** (kitty 0.42+): right-clicking pops a familiar GUI menu with Copy / Paste / New Window / New Tab / Open Link etc. When you right-click on a hovered URL, the menu surfaces URL-specific actions (Open / Copy).

**Keyboard URL hints** (`Ctrl+Shift+U`): kitty overlays a one- or two-letter label on every URL currently visible on screen — press the letter and the URL is copied to the system clipboard. Workflow:

1. Press `Ctrl+Shift+U`
2. Every URL on screen is decorated with a coloured letter
3. Press the letter → URL is on your clipboard
4. Paste with `Ctrl+Shift+V` (or wherever)

Works on URLs that wrap across lines, which mouse selection can't easily handle. The default `Ctrl+Shift+P > U` chord (which *opens* URLs in the browser instead of copying) is still active alongside this.

## Useful kitty defaults (not changed by this repo)

Still bound to their kitty defaults but worth knowing:

| Keybind                 | Action                                  |
| ----------------------- | --------------------------------------- |
| `Ctrl+Shift+C`          | Copy selection to clipboard             |
| `Ctrl+Shift+V`          | Paste from clipboard                    |
| `Ctrl+Shift+F`          | Search the scrollback                   |
| `Ctrl+Shift+H`          | Open scrollback in pager (`less`)       |
| `Ctrl+Shift+Up / Down`  | Scroll one line                         |
| `Ctrl+Shift+Page Up/Dn` | Scroll one page                         |
| `Ctrl+Shift+Home / End` | Scroll to top / bottom of scrollback    |
| `Ctrl+Shift+\\`         | Vertical split (new window in same tab) |
| `Ctrl+Shift+L`          | Cycle window layout                     |
| `Ctrl+Shift+W`          | Close current window/split              |
| `Ctrl+Shift+]` / `[`    | Next / previous window in tab           |
| `Ctrl+Shift+Right/Left` | Next / previous tab                     |
| `Ctrl+Shift+P > U`      | Open URL hint (opens in browser)        |
| `Ctrl+Shift+E`          | Open URL under cursor in browser        |
| `F11`                   | Toggle full-screen                      |

## Adding your own settings

Edit `~/.config/kitty/kitty.conf` **outside** the Ansible-managed block markers. Lines outside those markers are preserved across playbook runs.

```
# BEGIN ANSIBLE MANAGED: kitty UX defaults (Plan 00047)
# … managed block — do not edit, will be overwritten …
# END ANSIBLE MANAGED: kitty UX defaults (Plan 00047)

# Your own settings go here — safe across re-deploys.
font_family       JetBrainsMono Nerd Font
font_size         13.0
background_opacity 0.95
```

Reload kitty config without restarting: `Ctrl+Shift+F5`.

## See also

- Plan 00047 — `CLAUDE/Plan/00047-claude-code-mouse-wheel-pageup/` — why these specific settings, including the wheel→PageUp pivot history.
- Kitty manual — [https://sw.kovidgoyal.net/kitty/conf/](https://sw.kovidgoyal.net/kitty/conf/)
- Hints kitten reference — [https://sw.kovidgoyal.net/kitty/kittens/hints/](https://sw.kovidgoyal.net/kitty/kittens/hints/)
