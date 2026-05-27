# Research 01: Claude Code "safe scroll" / fullscreen rendering — how the TUI handles wheel and PageUp

Scope: answers the six questions in the brief, with sources inline. Bottom line at the end.

## 1. Is this a built-in Claude Code feature, a CLI flag, an env var, or a wrapper command? What is it called?

**It is NOT a tmux wrapper.** Claude Code does not spawn tmux. The user's premise — that Claude Code "launches itself inside a tmux session" — is incorrect. The actual mechanism is an **in-process alternate-screen renderer**, marketed by Anthropic as:

- **"Fullscreen rendering"** — official name, documented at <https://code.claude.com/docs/en/fullscreen>
- Synonym: **"no flicker mode"** — preserved in the env-var name `CLAUDE_CODE_NO_FLICKER`
- It is opt-in (a "research preview"), requires Claude Code v2.1.89+, and is enabled via any one of:
  - `/tui fullscreen` slash command (persisted to `~/.claude/settings.json` under the `tui` key)
  - `CLAUDE_CODE_NO_FLICKER=1` env var (pre-v2.1.110 default mechanism, still honoured)
  - `settings.json` → `"env": { "CLAUDE_CODE_NO_FLICKER": "1" }`

Quote (docs): *"It draws the interface on the terminal's alternate screen buffer, like `vim` or `htop`, and only renders messages that are currently visible."* — <https://code.claude.com/docs/en/fullscreen>

The user's terminal sees this as the same `\e[?1049h` alt-screen sequence vim uses. There is no tmux invocation. The conversation lives in the alt-screen buffer, which is why the host terminal's native PageUp/PageDown/Cmd-F can't see it — exactly the same reason vim's buffer isn't in scrollback.

Confirmed locally: `/workspace/files/var/local/claude-yolo/entrypoint.sh:105-109` already sets `CLAUDE_CODE_NO_FLICKER=1` and `CLAUDE_CODE_DISABLE_MOUSE=1`. So in CCY we're explicitly in fullscreen mode with mouse capture off — see Q5 for why that combination is the source of the symptom.

## 2. How is tmux invoked?

It isn't. The "tmux as scrollback host" model is a community **workaround** for Claude Code (e.g. <https://hboon.com/using-tmux-with-claude-code/>, <https://codeongrass.com/blog/how-to-run-claude-code-with-tmux/>), not a feature Anthropic ships. Anthropic's docs only mention tmux to explain interop:

- <https://code.claude.com/docs/en/terminal-config#configure-tmux> recommends users add three lines to `~/.tmux.conf` (`allow-passthrough on`, `extended-keys on`, `terminal-features 'xterm*:extkeys'`) — but *the user* installs tmux and writes this config, not Claude Code.
- <https://code.claude.com/docs/en/fullscreen#use-with-tmux> warns that fullscreen mode is **incompatible with `tmux -CC`** (iTerm2 integration), and that wheel scrolling in tmux requires `set -g mouse on` in the user's own `~/.tmux.conf`.

So: no `tmux new-session` exec, no shipped `-f` config, no inline config. The host terminal owns the alt-screen; Claude Code just draws into it.

## 3. What tmux options does it set?

None — see Q2. The only tmux directives anywhere in Anthropic docs are the *recommendations* the user is told to write into their own `~/.tmux.conf`:

```
set -g allow-passthrough on
set -s extended-keys on
set -as terminal-features 'xterm*:extkeys'
set -g mouse on    # only needed for wheel forwarding in fullscreen mode
```

(Source: <https://code.claude.com/docs/en/terminal-config> and <https://code.claude.com/docs/en/fullscreen#use-with-tmux>.) No `bind-key WheelUpPane`, no `terminal-overrides`. Claude Code does not write to `~/.tmux.conf`.

## 4. Does Claude Code's TUI enable or disable mouse mode (DECSET 1000/1006)?

**It enables mouse capture by default whenever fullscreen rendering is active.** The docs are explicit: *"Fullscreen rendering captures mouse events and handles them inside Claude Code"* (<https://code.claude.com/docs/en/fullscreen#use-the-mouse>). That means Claude Code emits the `\e[?1000;1002;1006h` family of DECSET sequences and reads SGR-encoded mouse events from stdin.

When the user sets `CLAUDE_CODE_DISABLE_MOUSE=1` (as CCY does at `entrypoint.sh:109`), Claude Code stops sending those DECSET sequences. Without mouse capture, the terminal does **not** translate wheel events to `\e[<64;…M` SGR events — and in alt-screen mode many terminals fall back to emitting `\eOA` / `\eOB` (cursor up/down) for the wheel. That fallback is exactly what the user is observing: wheel arrives as arrow keys, which Claude Code's input box interprets as `history:previous` / `history:next` (defaults Up/Down — see Q5).

So Claude Code itself isn't disabling the mouse — *we* are, via CCY's entrypoint. The wheel-as-arrow-up behaviour is the terminal emulator's default for alt-screen with no mouse reporting.

## 5. Is there a knob to make wheel events behave as PageUp/PageDown?

Yes — three layers, in increasing order of cleanness.

**Layer A — re-enable mouse capture (most direct).** Drop `CLAUDE_CODE_DISABLE_MOUSE=1` from `entrypoint.sh`. With mouse capture on, fullscreen mode binds wheel directly to the `scroll:lineUp` / `scroll:lineDown` actions in its `Scroll` context — quote: *"Scroll with the mouse wheel to move through the conversation"* (<https://code.claude.com/docs/en/fullscreen#use-the-mouse>) and *"Mouse wheel scrolling triggers this action"* on `scroll:lineUp`/`scroll:lineDown` (<https://code.claude.com/docs/en/keybindings#scroll-actions>). The tradeoff (the reason CCY disabled it): native click-drag selection stops working — selection then lives inside Claude Code and copies via OSC 52.

- Tuning knob: `CLAUDE_CODE_SCROLL_SPEED=3` (range 1–20) multiplies wheel notches. Set via env or `/scroll-speed` slash command (persisted to `~/.claude/settings.json`).
- One-off native selection escape hatch: hold the terminal's bypass modifier (`Option` in iTerm2, `Shift` in most Linux/Windows terminals) while dragging.

**Layer B — keep mouse off, rebind PageUp explicitly.** Wheel-as-arrow-keys is a terminal-emulator fallback Claude Code can't reach. But `PgUp` / `PgDn` are already bound to `scroll:pageUp` / `scroll:pageDown` in the `Scroll` context (defaults documented at <https://code.claude.com/docs/en/keybindings#scroll-actions>). The user can drive scrolling from the keyboard with no extra config — this works in CCY *today* with the current `CLAUDE_CODE_DISABLE_MOUSE=1`. The wheel just won't drive it.

**Layer C — re-map the cursor-up/down keys to scroll, via `~/.claude/keybindings.json`.** Since the wheel arrives as cursor-up/down in the alt-screen with mouse off, in principle a keybinding entry in the `Scroll` context could bind `up` → `scroll:lineUp` and `down` → `scroll:lineDown`. **This is brittle**: the Scroll context only applies once the user has scrolled into the conversation (i.e. once the prompt loses focus), and at the prompt the same arrow keys are `history:previous`/`history:next` (<https://code.claude.com/docs/en/keybindings#history-actions>). So this Layer C trick can't cleanly disambiguate "wheel from prompt focus" vs "real arrow key from prompt focus" — both arrive on stdin identically. Don't go here.

There is **no** `--scrollback` / `--mouse` / `--no-mouse` CLI flag. The full env-var surface for this feature is just:

| Env var                                  | Effect                                                     |
| ---------------------------------------- | ---------------------------------------------------------- |
| `CLAUDE_CODE_NO_FLICKER=1`               | Enable fullscreen / alt-screen rendering                   |
| `CLAUDE_CODE_DISABLE_MOUSE=1`            | Suppress DECSET mouse reporting (keep native selection)    |
| `CLAUDE_CODE_SCROLL_SPEED=N`             | Wheel-notch multiplier, 1–20                               |
| `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1` | Force classic in-band renderer regardless of `tui` setting |

(Source: all four documented at <https://code.claude.com/docs/en/fullscreen>.)

## 6. Are there open or closed GitHub issues about this?

Yes — many, all variants of the same complaint, all funnel to the same canonical "wheel scrolls history" thread.

| Issue                                                            | Title (abbreviated)                                                                         | State                                                                                                        | Notes |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ----- |
| [#2301](https://github.com/anthropics/claude-code/issues/2301)   | (canonical) wheel scrolls history instead of conversation                                   | Open, **lead issue** — everything else dedupes here                                                          |       |
| [#5368](https://github.com/anthropics/claude-code/issues/5368)   | "Cannot scroll up to view conversation history — mouse wheel only scrolls within input box" | Auto-closed as dup of #2301                                                                                  |       |
| [#12953](https://github.com/anthropics/claude-code/issues/12953) | "Mousewheel scrolls through input history instead of chat history"                          | Open; comments report bug persists on v2.1.56, v2.1.71, v2.1.139 across macOS Terminal.app, Wave, Windows 11 |       |
| [#38810](https://github.com/anthropics/claude-code/issues/38810) | "Claude Code captures mouse events in tmux, making scrollback completely unusable"          | Auto-closed as dup of #23581                                                                                 |       |
| [#23581](https://github.com/anthropics/claude-code/issues/23581) | (canonical) mouse capture vs tmux scrollback                                                | Open                                                                                                         |       |
| [#58809](https://github.com/anthropics/claude-code/issues/58809) | "scroll wheel permanently broken for affected conversation"                                 | Auto-closed as dup of #58653                                                                                 |       |

Resolution status: **none of the canonical issues are fixed**. The fullscreen-rendering docs (Q1) are effectively Anthropic's response — *use fullscreen mode plus mouse capture, accept that native selection now requires the bypass modifier*. The dismissive_language_detector handler would flag "out of scope" framing, but the upstream pattern is genuinely "by design, here's the documented escape hatch." One useful breadcrumb from #2301 comments: Apple Terminal.app users report fixing wheel scrolling by **Settings → Profiles → Keyboard → uncheck "Scroll alternate screen"** — that flips the terminal's own wheel-in-alt-screen behaviour from "arrow keys" to "actual scroll wheel events," which Claude Code can then consume if mouse capture is on, or which the terminal can use for its own scrollback if mouse capture is off. Most modern terminals (iTerm2, Ghostty, Kitty) have an equivalent toggle.

## Bottom line

**Mechanism.** Claude Code's "safe scroll" is not tmux. It is an in-process alternate-screen renderer (`\e[?1049h`) called **fullscreen rendering**, gated by `CLAUDE_CODE_NO_FLICKER=1` or `/tui fullscreen`. In that mode, the TUI requests mouse reporting (DECSET 1000/1002/1006) and binds the wheel to `scroll:lineUp`/`scroll:lineDown` in its `Scroll` keybinding context. CCY then turns mouse reporting back off (`CLAUDE_CODE_DISABLE_MOUSE=1` in `entrypoint.sh:109`) so native click-drag selection works — and that is precisely what makes the host terminal fall back to "wheel emits arrow keys," which the prompt interprets as history navigation.

**Most promising single-knob intervention.** Remove `export CLAUDE_CODE_DISABLE_MOUSE=1` from `/workspace/files/var/local/claude-yolo/entrypoint.sh` (and optionally add `export CLAUDE_CODE_SCROLL_SPEED=3`). Wheel will then natively drive `scroll:lineUp`/`scroll:lineDown`. Cost: native terminal text selection stops working; users must hold `Shift` (Linux/Windows terminals) or `Option` (iTerm2) while dragging to bypass the capture. If preserving native selection is non-negotiable, the fallback is to **leave wheel-as-arrows alone and rely on the keyboard** — `PgUp`/`PgDn` already drive `scroll:pageUp`/`scroll:pageDown` in fullscreen mode with no further configuration.
