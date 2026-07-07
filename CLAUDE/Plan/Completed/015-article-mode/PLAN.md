# Plan 015: Article Mode for Speech-to-Text

**Status**: Complete
**Owner**: joseph
**Priority**: Medium

Delivered: `files/home/.local/bin/wsi-article` + `wsi-article-window`, the
`Shift+Insert` binding, and the two-pane article window are all shipped in the
repo (see the Key Reference Files / Verification sections below).

## Context

The speech-to-text system currently supports single-session recording (up to 120 seconds) with optional Claude post-processing (corporate/natural styles). This plan adds **article mode**: an indefinite looped recording mode where every 120 seconds the accumulated transcription is flushed and Claude re-polishes the entire raw article. A dedicated two-pane GTK window serves as the UI.

**Keybinding**: `Shift+Insert` (follows pattern: `Insert`, `Ctrl+Insert`, `Alt+Insert`, `Shift+Insert`)

---

## Architecture

The extension (thin layer) launches `wsi-article-window`, which:

1. Opens the two-pane GTK4 window
2. Spawns `wsi-article` as a subprocess (the looping recorder)
3. Monitors article files for changes and drives Claude polishing

`wsi-article` emits standard DBus signals (`RECORDING`/`IDLE`) so the panel indicator updates correctly. The extension shows an elapsed-time label (`📝 REC 0m`, `📝 REC 1m`...) instead of a countdown during article mode.

---

## Files to Create

### 1. `files/home/.local/bin/wsi-article`

Python script: looped RealtimeSTT recording in 120-second chunks.

**Behaviour:**

- Accepts args: `--debug`, `--language`, `--no-notify`, `--model`
- Creates RealtimeSTT recorder once (model loaded once, reused across chunks)
- Inner loop:
  - Accumulates `recorder.text()` sentence callbacks into `chunk_texts[]`
  - Every 120 seconds after the current sentence completes: flush chunk
  - Flush: join `chunk_texts`, append to `~/.cache/speech-to-text/article-raw.txt` with a newline
  - Write current timestamp to `~/.cache/speech-to-text/article-chunk.trigger` (signals window)
  - Reset `chunk_texts = []`, reset chunk timer
- Real-time partial updates: write current partial to `~/.cache/speech-to-text/article-partial.txt`
- DBus signals: `PREPARING` (model loading), `RECORDING` (recording), `IDLE` (stopped)
- Uses same PID_FILE as wsi-stream (`/dev/shm/stt-recording-{user}.pid`) → abort-recording (Escape) works automatically
- Exits cleanly on SIGTERM/SIGINT

### 2. `files/home/.local/bin/wsi-article-window`

Python + GTK4 two-pane window.

**Window layout:**

```
┌─────────────────────────────────────────────────┐
│  [Stop Recording]    Status: Recording chunk 1… │
├──────────────────────┬──────────────────────────┤
│  Raw Transcript      │  Polished Article         │
│  (read-only,         │  (read-only,              │
│  real-time updates)  │  auto-refreshed per chunk)│
│                      │                           │
│                      ├──────────────────────────┤
│                      │  Prompt tweak (optional): │
│                      │  [text entry____________] │
│                      │  [Re-polish]  [Copy]      │
└──────────────────────┴──────────────────────────┘
```

**Behaviour:**

- Accepts args: `--model sonnet|opus|haiku`, `--debug`, `--language`, `--no-notify`
- Spawns `wsi-article` as subprocess on startup with matching args
- Uses `GLib.timeout_add(500, poll_files)` to poll:
  - `article-partial.txt` → update left pane in real-time (current partial chunk)
  - `article-chunk.trigger` → when mtime changes: update left pane with full raw, auto-run Claude polish → update right pane
- Claude polishing: calls `wsi-claude-process --style article --model {model} [--tweak {tweak}] "{full_raw_text}"`
- Stop button: sends SIGTERM to wsi-article subprocess
- Re-polish button: runs Claude on full raw with current tweak text on demand
- Copy button: copies polished article to clipboard via `wl-copy`
- When wsi-article exits: updates status to "Recording stopped", Stop button becomes inactive, window stays open for copy/review

### 3. `files/home/.config/speech-to-text/claude-prompt-article.txt`

```
Transform this raw speech-to-text article into a well-structured, polished piece of writing. Output ONLY the formatted article with no preamble or explanation.

Guidelines:
- Fix grammar, spelling, and punctuation errors
- Organise into logical paragraphs with clear structure
- Remove filler words and verbal tics (um, uh, like, you know)
- Maintain the speaker's voice and intent
- Use appropriate headings if the content warrants it
- Do NOT add information that wasn't in the original transcription
{TWEAK}

<transcription>
{TRANSCRIPTION}
</transcription>

Output the polished article:
```

Note: `{TWEAK}` is replaced with `\nAdditional instruction: {tweak_text}` when present, or removed when absent.

---

## Files to Modify

### 4. `files/home/.local/bin/wsi-claude-process`

Add `--tweak TEXT` argument:

- When `--tweak` is provided and non-empty, replace `{TWEAK}` placeholder in prompt with `\nAdditional instruction: {tweak_text}`
- When absent, replace `{TWEAK}` with empty string
- Backwards-compatible: existing prompts (corporate, natural) don't contain `{TWEAK}`, so the substitution is a no-op

### 5. `extensions/speech-to-text@fedora-desktop/schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml`

Add new key:

```xml
<key name="toggle-recording-article" type="as">
  <default><![CDATA[['<Shift>Insert']]]></default>
  <summary>Toggle Article Mode Recording</summary>
  <description>Keybinding to start article mode: indefinite looped recording with two-pane polishing UI</description>
</key>
```

### 6. `extensions/speech-to-text@fedora-desktop/extension.js`

Changes:

- Add `this._isArticleMode = false` and `this._elapsedSeconds = 0` to constructor
- Register `toggle-recording-article` keybinding → `_launchArticleMode()`
- Add `_launchArticleMode()`: validates state (stops if active), sets `_isArticleMode = true`, launches `wsi-article-window` with model/debug/language flags, calls `_startElapsedTimer()`
- Add `_startElapsedTimer()`: shows `📝 REC 0m` green label (mirrors `_startCountdown` structure but counts UP in minutes, no auto-stop)
- When DBus IDLE signal received and `_isArticleMode`: call `_stopElapsedTimer()` and reset `_isArticleMode = false`
- Unregister `toggle-recording-article` keybinding in `disable()`

### 7. `playbooks/imports/optional/common/play-speech-to-text.yml`

- Add `wsi-article` and `wsi-article-window` to the "Copy User Scripts" task loop (mode `0755`)
- Add `claude-prompt-article.txt` to the "Copy Claude Prompt Templates" task loop

---

## Cache Files Used

| File                                            | Purpose                                                                |
| ----------------------------------------------- | ---------------------------------------------------------------------- |
| `~/.cache/speech-to-text/article-raw.txt`       | Full accumulated raw transcription (all completed chunks)              |
| `~/.cache/speech-to-text/article-partial.txt`   | Current chunk's partial real-time transcription                        |
| `~/.cache/speech-to-text/article-chunk.trigger` | Timestamp written after each chunk flush (signals window to re-polish) |

---

## Key Reference Files

- `files/home/.local/bin/wsi-stream:594` — streaming recorder pattern to follow in wsi-article
- `files/home/.local/bin/wsi-claude-process` — to modify for `--tweak` support
- `files/home/.config/speech-to-text/claude-prompt-corporate.txt` — prompt format reference
- `extensions/speech-to-text@fedora-desktop/extension.js:599` — `_startCountdown()` pattern for elapsed timer
- `extensions/speech-to-text@fedora-desktop/extension.js:750` — `_launchWSI()` launch function pattern
- `extensions/speech-to-text@fedora-desktop/extension.js:815` — `_launchWSIClaude()` launch function pattern

---

## Verification

1. Run `./scripts/qa-all.bash` (Python syntax check for wsi-article and wsi-article-window)
2. Run ESLint: `cd /workspace/extensions && node_modules/.bin/eslint speech-to-text@fedora-desktop/extension.js`
3. Deploy: `ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml`
4. **Log out and log back in** (Wayland extension reload requirement)
5. Test:
   - Press `Shift+Insert` → article window opens, recording starts
   - Panel shows `📝 REC 0m` in green, increments each minute
   - Speak 120+ seconds → left pane fills with raw text, right pane shows polished article after each chunk
   - Add tweak text → click Re-polish → right pane updates with tweaked polishing
   - Click Stop → recording ends, window stays open for review/copy
   - Press `Escape` during recording → also stops via PID file mechanism
