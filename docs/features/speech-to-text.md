# Speech-to-Text GNOME Extension

**GPU-accelerated voice typing for your entire desktop**

Transform speech into text anywhere on your system with a single keystroke. This GNOME Shell extension provides real-time, GPU-accelerated speech-to-text transcription using OpenAI's Whisper model, with optional AI enhancement via Claude Code.

---

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation](#installation)
- [Configuration](#configuration)
- [Usage](#usage)
- [Claude Code Post-Processing](#claude-code-post-processing)
- [Icon Reference](#icon-reference)
- [Troubleshooting](#troubleshooting)
- [Architecture](#architecture)

---

## Overview

The Speech-to-Text extension adds system-wide voice input to Fedora. Press **Insert** to start recording, speak naturally, and your words appear as text automatically.

**What makes this special:**

- **GPU Acceleration**: Uses CUDA for fast transcription (falls back to CPU if needed)
- **Real-time Streaming**: Optional instant transcription while you speak
- **AI Enhancement**: Optional Claude Code post-processing for professional formatting
- **Works Everywhere**: Any application, any text field
- **Privacy-Focused**: All processing happens locally on your machine

---

## Features

### Core Capabilities

- ⚡ **GPU-accelerated transcription** with faster-whisper (NVIDIA CUDA)
- 🎯 **Two transcription modes**:
  - **Batch mode** (default): Fast, accurate transcription after you stop speaking
  - **Streaming mode**: Real-time transcription while you speak (experimental)
- 🎤 **Configurable models**: tiny, base, small, medium, large-v3 (trade speed for accuracy)
- 🌍 **Language-specific transcription**: Force language or auto-detect
- 🤖 **Claude Code integration**: Optional AI post-processing for professional text
- ⌨️ **Auto-paste**: Text types automatically at cursor position
- 🔔 **Visual feedback**: Status icons and desktop notifications

### Processing Modes

1. **Raw transcription** (default): Direct Whisper output
2. **Corporate mode** 🤖: Professional formatting via Claude Code
3. **Natural mode** 💬: Casual cleanup via Claude Code

---

## Prerequisites

### Required

- **Fedora 44** (this branch)
- **Active internet** for initial model downloads (cached afterwards)

### Recommended

- **NVIDIA GPU** with CUDA support (GTX 10-series or newer), with drivers installed via
  `play-nvidia.yml`

A GPU is not strictly required: `faster-whisper-transcribe.py` catches the CUDA
initialisation failure and falls back to CPU (`int8`) automatically. Transcription is
considerably slower that way, but it works.

### Hardware Recommendations

- **Minimum**: GTX 1050 Ti (2GB VRAM) - use tiny/base models
- **Recommended**: RTX 2060 (6GB VRAM) - use small/medium models
- **Optimal**: RTX 3060+ (12GB VRAM) - use large models

### Disk Space

- Model cache: ~41MB (`tiny.en`) to ~3GB (`large-v2` / `large-v3`)
- RealtimeSTT dependencies: ~2GB on first install (PyTorch, etc.)
- Temporary audio files: ~10MB per recording

---

## Installation

### Step 1: Install NVIDIA Drivers (if not already done)

```bash
cd ~/Projects/fedora-desktop
./playbooks/imports/optional/hardware-specific/play-nvidia.yml
```

Reboot after driver installation to ensure CUDA is available.

### Step 2: Install Speech-to-Text Extension

```bash
cd ~/Projects/fedora-desktop
./playbooks/imports/optional/common/play-speech-to-text.yml
```

**Installation time**: 5-15 minutes on first run

- System packages: ~1 minute
- faster-whisper + CUDA libraries: ~2 minutes
- RealtimeSTT (streaming mode): **5-15 minutes** (large PyTorch download)
- Extension deployment: ~10 seconds

**What gets installed:**

- System packages: sox, ydotool, wl-clipboard, zenity, wev
- Python packages: faster-whisper, RealtimeSTT, nvidia-cublas-cu12, nvidia-cudnn-cu12
- GNOME extension: `~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/`
- Scripts: `wsi`, `wsi-stream`, `wsi-claude-process` in `~/.local/bin/`
- Prompt templates: `~/.config/speech-to-text/claude-prompt-*.txt`

### Step 3: Enable Extension

The extension is enabled automatically during installation. If you need to manually enable it:

```bash
gnome-extensions enable speech-to-text@fedora-desktop
```

Verify it's running:

```bash
gnome-extensions list --enabled | grep speech-to-text
```

---

## Configuration

### Model Size Selection

Choose the model in **Settings... → Transcription → Whisper Model** (only downloaded
models are listed, meaning a cache snapshot holds the weights, `model.bin`; **Manage
Whisper Models...** downloads more). The default, `auto`, is decided at each recording
by `~/.local/bin/wsi-resolve-model`, and `play-speech-to-text.yml` downloads the model
it picks, resuming a download that was cut short:

| Machine      | English                         | Other languages or detection    |
| ------------ | ------------------------------- | ------------------------------- |
| NVIDIA GPU   | `distil-large-v3.5`             | `large-v3-turbo`                |
| No GPU (CPU) | `small` batch, `base` streaming | `small` batch, `base` streaming |

The first recording with a model not yet downloaded fetches it (about 1.5 GB for either
GPU choice). On a machine with an NVIDIA GPU that CUDA cannot use (a broken CUDA or
cuDNN install), `auto` stops with an error rather than quietly using the CPU model. An
English-only model with another language set is refused with an error,
not transcribed as English. `distil-large-v3.5` is handed to faster-whisper as its
Hugging Face repo, `distil-whisper/distil-large-v3.5-ct2`: faster-whisper 1.2.1 knows
the short name, but 1.1.1 (pinned by older RealtimeSTT releases) does not. Article mode
uses the warm server's model, like streaming server mode.

`stt_model` and `stt_language` in the host variables are only the defaults of
`faster-whisper-transcribe` when you run that script by hand; the recorders always pass
the model and language from Settings:

```yaml
# File: environment/localhost/host_vars/localhost.yml
stt_model: small  # Default
# Language: 'en' (English), 'es' (Spanish), etc. or '' for auto-detect
stt_language: en  # Default
```

**Model comparison:**

The authoritative list is `_whisperModels` in
`extensions/speech-to-text@fedora-desktop/extension.js` — this table is generated from it.
**Multilingual:**

| Model            | Size   | Notes                      |
| ---------------- | ------ | -------------------------- |
| `auto`           | varies | See the table above        |
| `tiny`           | ~75MB  | Fastest, basic accuracy    |
| `base`           | ~142MB | Fast, good accuracy        |
| `small`          | ~466MB | Balanced — **default**     |
| `medium`         | ~1.5GB | Slow, great accuracy       |
| `large-v2`       | ~3GB   | Very high accuracy         |
| `large-v3`       | ~3GB   | Best quality               |
| `large-v3-turbo` | ~1.6GB | Distilled, fast + accurate |

**English-only** (smaller and faster, no multilingual capability):

| Model               | Size   | Notes                                         |
| ------------------- | ------ | --------------------------------------------- |
| `tiny.en`           | ~41MB  | Fastest, English only                         |
| `base.en`           | ~77MB  | Fast, good accuracy, English only             |
| `small.en`          | ~252MB | Balanced, English only                        |
| `medium.en`         | ~789MB | Great accuracy, English only                  |
| `distil-large-v3.5` | ~1.5GB | Fewer errors than Turbo, faster, English only |

A model change applies from the next recording; in Server mode, from the next server
start. Models are cached in `~/.cache/huggingface/hub/` and shared between batch and streaming modes.

### Language Configuration

Choose the language in **Settings... → Transcription → Language**: "System default"
uses your desktop locale (`en_GB.UTF-8` gives `en`), or "English". For another code, or
`""` for language detection (slower and less accurate), set the key directly:

```bash
gsettings --schemadir ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas \
    set org.gnome.shell.extensions.speech-to-text language 'de'
```

`wsi` run by hand without `--language` uses the same setting.

### Streaming Mode Setup

Enable real-time transcription in extension settings:

1. Click the extension icon in the top bar → **Settings...**
2. Under **Streaming Mode**, enable the **"Streaming mode"** switch

**First-time streaming setup:**

- Downloads large dependencies (~2GB PyTorch)
- May take 5-15 minutes
- Subsequent uses are instant

### Keeping the Server Warm

In streaming **Server mode** a background server (`wsi-stream-server`) keeps the model
loaded, so a recording starts at once. The first Insert after the server has stopped
waits for it to start and load the model. Two settings under **Settings... → Streaming
Mode** control that:

- **Server idle timeout (minutes)** (`server-idle-timeout-minutes`, default 20, 0 =
  never): the server shuts down after this long without a recording. It is passed when
  the server starts, so a change applies from the next start.
- **Start the server at login** (`server-start-at-login`, default off): the user unit
  `wsi-stream-server-at-login.service` starts the server when you log in, so the first
  recording of the session is warm. It acts only when streaming mode is on with Startup
  mode "Server"; otherwise it starts nothing. The playbook enables the unit on every
  host, so the switch needs no playbook run; it applies from the next login.

At most one server runs at a time. A server holds an exclusive lock on its PID file
(`$XDG_RUNTIME_DIR/wsi-stream-server.pid`) for as long as it lives, and a second server
that cannot take the lock exits at once; a file left behind without the lock does not
block a start. An Insert while a server is still loading waits for it (up to 45 s), and
the login unit starts nothing if a server already holds the lock. The unit never restarts the server: after
the idle timeout, or after a playbook run that updated the scripts (which stops the
server), the next Insert starts it again. With both settings on (0 and at login) the
server stays warm until logout. What the unit did at login:

```bash
journalctl --user -u wsi-stream-server-at-login.service -b --no-pager | cat
```

### Recording Limits

Each mode stops itself; the panel shows the elapsed time (`1:42`).

| Mode                                 | Stops at                                                                     |
| ------------------------------------ | ---------------------------------------------------------------------------- |
| Batch (`wsi`)                        | 30 s                                                                         |
| Streaming: standard, pre-buffer      | 120 s (`wsi-stream --timeout`)                                               |
| Streaming: server, continuous off    | 120 s, the same cap                                                          |
| Streaming: server, continuous **on** | Insert; or **Stop after silence** / **Maximum length** (Settings), see below |
| Article mode (**Create Article...**) | its window's Stop, or the same **Stop after silence** / **Maximum length**   |

None of these auto-stops on a pause in speech, except continuous dictation's no-speech
stop.

### Continuous Dictation

For dictating at length. In streaming **Server mode**, turn on **Settings... → Continuous
Dictation → Continuous dictation** (`continuous-dictation`, default off while it is new;
read at each recording, no logout). The server cuts your speech at natural pauses into
segments of at most 28 s, transcribes each with the selected model while you keep
talking, and pastes the whole text once when you press Insert. The panel shows the
elapsed time and, after a dot, how many segments are still waiting (`3:42 ·2`); amber
means transcription is slower than your speech.

To see the text arrive as you speak, set **Paste while dictating, every (seconds)**
(`dictation-paste-interval-seconds`, default 0 = once at stop), for example to 120.
Each paste then holds the phrases finished since the last one, and Enter follows only
the last paste, at stop. Nothing is lost by pasting early: each phrase is transcribed
once, when you pause, and never revised. Claude post-processing still pastes once at
stop, because it needs the whole text.

### Where The Text Is Pasted

While a dictation can still paste, the focused window has a red outline: that is where
the next paste goes. In streaming mode the paste key is chosen at each paste, for the
window focused at that moment (batch mode still uses the window focused at Insert, and
does not save after pasting):

- an app in **Apps using Ctrl+V** (`paste-ctrl-v-apps`) gets Ctrl+V;
- a terminal gets Ctrl+Shift+V. A terminal is an app whose desktop entry lists the
  `TerminalEmulator` category, which kitty, Ptyxis and GNOME Terminal all do;
- any other app gets **Paste shortcut for other apps** (`paste-default-mode`, Ctrl+V by
  default, which is what GTK, Qt, Electron and browsers bind).

**Apps to save after pasting** (`paste-save-apps`, empty by default) gets Ctrl+S after
each paste, for example `org.gnome.TextEditor`. It is never sent to a terminal, where
Ctrl+S freezes the screen until Ctrl+Q. With **Debug logging** on, each paste's line in
`debug.log` names the window class to put in either list.

The recording also stops by itself, transcribes and pastes, and a notification that
stays until dismissed says why:

- **Stop after silence** (`silence-autostop-seconds`, default 120 s, 0 = never);
- **Maximum length** (`max-recording-minutes`, default 60); the panel counts down its
  last minute.

With continuous dictation off, server mode records the whole clip (up to the 120 s cap)
and transcribes it once at stop with the selected model; no speech detection runs, and
reaching the cap shows the usual short notification. If the speech detector continuous
dictation needs cannot be used with the installed faster-whisper, a continuous recording
refuses to start and says why; turning the setting off still records.

If anything fails (a segment cannot be transcribed, the microphone disappears,
transcription falls more than 120 s behind, the final segments do not finish in time),
the dictation stops at once and **nothing is pasted**: the text so far is put on the
clipboard (Ctrl+V), the audio not yet transcribed is kept as WAV files, and a
notification that stays until dismissed names the folder. While a dictation runs, its
text is also written line by line to `$XDG_RUNTIME_DIR/wsi-dictation/session-*/journal.jsonl`
(cleared at logout).

If `wsi-stream` itself dies, its keepalives stop and the server closes the microphone
15 s later and finishes the transcription, but nobody is left to paste it. The server
keeps that dictation's journal, stays up (even past its idle timeout) and, at the next
Insert, the text is put on the clipboard (Ctrl+V) and a notification that stays names
the journal. The new recording then carries on as usual; its own result replaces the
clipboard when it is pasted, so paste the old text first. If the server stops before
the next Insert (logout, a playbook run), the text is still in the journal until logout:

```bash
jq -r .text "$XDG_RUNTIME_DIR"/wsi-dictation/session-*/journal.jsonl | cat
```

### Stop Grace

After the first stop press, every recorder (batch, and streaming in standard,
pre-buffer and server mode) keeps recording for `stop-grace-seconds` (default 3,
range 0-30), then stops. A second press during the grace stops at once, Escape
discards at once, and 0 turns the grace off. In pre-buffer mode a stop pressed while
the model is still loading closes the microphone on the same schedule; what was
recorded is transcribed once the model has loaded. There is no Settings control yet; set
it with `gsettings` (takes effect on the next recording, no logout):

```bash
gsettings --schemadir ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas \
    set org.gnome.shell.extensions.speech-to-text stop-grace-seconds 5
```

The recorders read it through `~/.local/bin/wsi-stop-grace` and refuse to record if
it cannot be read; re-run `play-speech-to-text.yml` to fix that. Why the last words
were lost, per mode: `CLAUDE/Plan/00148-stt-unlimited-dictation-loop-and-buffer/RESEARCH-stop-path.md`.

---

## Usage

### Basic Workflow

1. **Start Recording**: Press **Insert** key

   - 🎤 Red microphone icon appears
   - Desktop notification: "Recording..."
   - The panel shows the elapsed time; each mode stops itself at its limit (batch 30 s,
     see [Recording Limits](#recording-limits))

2. **Speak Clearly**: Say what you want to type

   - Speak at normal pace
   - Minimize background noise
   - Pause briefly between sentences

3. **Stop Recording**: Press **Insert** again

   - The icon turns to the orange "…" at once, so you can see the press was taken, but
     recording carries on for a short grace (3 seconds by default) so the words you
     were still saying are kept; a notification says "Stopping in 3s" (only when
     notifications are on)
   - Press **Insert** once more to stop at once; **Escape** still discards at once
   - The icon stays "…" while the text is transcribed
   - Desktop notification: "Transcribing..."

   See [Stop Grace](#stop-grace) to change or turn off the grace.

4. **Text Appears**: Automatically typed at cursor

   - Notification shows preview
   - Press **Enter** sent automatically
   - Text also saved to `~/.cache/speech-to-text/last-transcription.txt`

### Keyboard Shortcuts

| Shortcut        | Action                           | Mode                        |
| --------------- | -------------------------------- | --------------------------- |
| **Insert**      | Start/stop recording             | Default (raw transcription) |
| **Ctrl+Insert** | Record with corporate processing | 🤖 Claude corporate mode    |
| **Alt+Insert**  | Record with natural processing   | 💬 Claude natural mode      |

### Extension Menu

Click the extension icon in the top bar to access:

- **Auto-paste at cursor** and **Debug Logging** — quick toggles
- **Copy Last Transcription**, **View Debug Log...**, **Create Article...**
- **Manage Whisper Models...**, **Server Manager...**
- **Settings...** — opens the extension's own Preferences window (language, model,
  streaming mode, Claude Code post-processing)

### Batch Mode (Default)

**Best for**: Most use cases, accurate transcription

```
You: Press Insert → Speak "Hello world, this is a test" → Press Insert
System: [2-3 seconds processing]
Output: Hello world, this is a test.
```

**Characteristics:**

- Fast processing (2-5 seconds typical)
- High accuracy
- Complete sentence transcription
- GPU acceleration (or CPU fallback)

### Streaming Mode (Real-Time)

**Best for**: Long dictation, seeing words as you speak

Enable via the extension menu: **Settings... → Streaming Mode → Streaming mode**

```
You: Press Insert → Start speaking "The quick brown fox..."
System: [Words appear in real-time as you speak]
Output: The quick brown fox jumps over the lazy dog.
```

**Characteristics:**

- Words appear instantly while speaking (standard and pre-buffer mode preview)
- Stops at 120 s; for longer dictation use [Continuous Dictation](#continuous-dictation)
- Higher GPU load
- Experimental feature
- Standard mode pastes the selected model's final transcription. If that does not
  finish, nothing is pasted: the live preview text (lower quality) goes to the clipboard
  and an error notification says so
- Server mode transcribes with the selected model in the warm server (no preview text)

### Claude Code Post-Processing

Enhance transcriptions with AI-powered formatting:

#### Corporate Mode (🤖 Ctrl+Insert)

**Use for**: Emails, documentation, professional communication

```
Raw: "um so basically what I'm trying to say is we need to like schedule a meeting"
Corporate: "We need to schedule a meeting."
```

**What it does:**

- Removes filler words (um, uh, like, you know)
- Fixes grammar and punctuation
- Professional but approachable tone
- Organizes into paragraphs
- Preserves core meaning

#### Natural Mode (💬 Alt+Insert)

**Use for**: Chat messages, personal notes, casual communication

```
Raw: "hey can you um grab some milk at the store"
Natural: "Hey, can you grab some milk at the store?"
```

**What it does:**

- Removes filler words
- Fixes punctuation and capitalization
- Keeps contractions and casual tone
- Preserves informal style

### Command-Line Usage (Advanced)

The backend script can be called directly:

```bash
# Basic usage
wsi

# Debug mode (verbose output)
wsi -d

# Clipboard mode (Ctrl+V to paste)
wsi -c

# Auto-paste without Enter key
wsi -a --no-auto-enter

# Force language
wsi -l en

# Claude processing
wsi --claude-process --claude-model sonnet --claude-style corporate
```

---

## Claude Code Post-Processing

### How It Works

```
┌──────────────┐      ┌──────────────┐      ┌──────────────┐
│  Whisper     │ -->  │  Claude Code │ -->  │  Final Text  │
│  Raw Output  │      │  AI Polish   │      │  (Enhanced)  │
└──────────────┘      └──────────────┘      └──────────────┘
```

1. Whisper transcribes your speech (raw text)
2. Text logged to `~/.local/share/speech-to-text/debug.log`
3. Claude Code processes text with style-specific prompt
4. Enhanced text replaces raw transcription
5. Result auto-pasted at cursor

### Customizing Prompts

Prompt templates are stored in `~/.config/speech-to-text/`:

```bash
# Corporate style prompt
~/.config/speech-to-text/claude-prompt-corporate.txt

# Natural style prompt
~/.config/speech-to-text/claude-prompt-natural.txt
```

**Customization workflow:**

1. Edit prompt template:

   ```bash
   vim ~/.config/speech-to-text/claude-prompt-corporate.txt
   ```

2. Keep `{TRANSCRIPTION}` placeholder intact:

   ```
   Transform this transcription: {TRANSCRIPTION}

   Your custom instructions here...
   ```

3. Test with Ctrl+Insert or Alt+Insert

**Backup protection**: The playbook automatically backs up your custom prompts to `.bak` files before updating system defaults. Your customizations are preserved across playbook re-runs.

### Claude Model Selection

Configure in the extension's Preferences window, or per-invocation on the command line.
There is no Ansible host variable for this setting:

1. Click the extension icon in the top bar → **Settings...**
2. Under **Claude Code Post-Processing**, set **Claude model** (`sonnet` is the default;
   `opus` and `haiku` are also available)

```bash
# Command-line (temporary)
wsi --claude-process --claude-model opus --claude-style natural
```

**Model trade-offs:**

- **haiku**: Fastest, cheapest, good for simple cleanup
- **sonnet**: Best balance (default)
- **opus**: Most capable, best for complex formatting

### Claude Account (Token)

**Claude token** (`claude-token`) chooses the account post-processing runs as: one of the
named tokens in `~/.claude-tokens/ccy/tokens/`, the same ones `cc` and `ccy` offer
(`ccy --create-token` makes one). The newest unexpired file of that name is used.
**Desktop login** (empty) uses the host's own login, which `cc` parks aside while a
named-token session is open, so post-processing fails then with "Not logged in".

If post-processing fails for any reason, **nothing is pasted**. The raw transcript goes
on the clipboard (Ctrl+V), the icon turns red, and the panel menu (and a notification,
if notifications are on) says why.

---

## Icon Reference

The extension icon indicates current status:

The panel shows a single microphone icon whose **colour** carries the state (it swaps to
a loading icon while transcribing):

| Appearance           | Status     | Meaning                                  |
| -------------------- | ---------- | ---------------------------------------- |
| Default colour       | Idle       | Ready                                    |
| Orange               | Preparing  | Starting up                              |
| Red                  | Recording  | Listening to your voice (Insert to stop) |
| Orange, loading icon | Processing | Transcribing audio                       |
| Green (2 seconds)    | Success    | Transcription delivered                  |
| Red (2 seconds)      | Error      | Check the debug log                      |

In Claude post-processing modes the panel **label** is additionally prefixed with
`🤖 REC` (corporate) or `💬 REC` (natural).
| ✅ | Success | Transcription complete |
| ⚠️ | Error | Something went wrong (check logs) |
| ⏸️ | Idle | Ready for next recording |

**Desktop notifications** also show:

- Recording status
- Transcription preview
- Error messages
- Paste instructions

---

## Troubleshooting

### CUDA / GPU Issues

**Symptom**: Slow transcription, "GPU not available" in logs

**Solutions:**

1. Verify NVIDIA drivers installed:

   ```bash
   nvidia-smi
   ```

   Should show GPU info and CUDA version.

2. Check CUDA libraries:

   ```bash
   python3 -c "import nvidia.cublas; import nvidia.cudnn; print('CUDA libs OK')"
   ```

3. Reinstall with fresh CUDA:

   ```bash
   pip uninstall -y nvidia-cublas-cu12 nvidia-cudnn-cu12
   ./playbooks/imports/optional/common/play-speech-to-text.yml
   ```

4. If the GPU is still unavailable, the default `auto` model stops with an error naming the
   broken CUDA setup rather than quietly running on the CPU. To dictate on the CPU until it
   is fixed, pick a model explicitly in Settings (Whisper Model), e.g. `small`.

### ydotool Permission Errors

**Symptom**: "ydotool socket not writable", auto-paste fails

**Solution:**

```bash
# Check socket exists and has correct permissions
ls -l /run/ydotool.socket
# Should show: srw-rw-rw- (0666 permissions)

# Restart service
sudo systemctl restart ydotool

# Verify service is running
systemctl status ydotool --no-pager
```

The playbook configures ydotool as a system service with world-writable socket (`0666`).

### Extension Not Loading

**Symptom**: Extension not visible in top bar

**Solutions:**

1. Check extension is enabled:

   ```bash
   gnome-extensions list --enabled | grep speech-to-text
   ```

2. Manually enable:

   ```bash
   gnome-extensions enable speech-to-text@fedora-desktop
   ```

3. Check GNOME Shell logs:

   ```bash
   journalctl --user -u org.gnome.Shell --since "5 minutes ago" --no-pager | grep -i speech
   ```

4. Re-run playbook:

   ```bash
   ./playbooks/imports/optional/common/play-speech-to-text.yml
   ```

### Keybinding Conflicts

**Symptom**: Insert key doesn't trigger recording

**Solutions:**

1. Check for conflicts:

   ```bash
   # List all keybindings
   gsettings list-recursively | grep -i insert
   ```

2. Test key detection:

   ```bash
   # Install wev (included in playbook)
   wev
   # Press Insert key and verify event fires
   ```

3. Alternative: Use extension menu to start recording

### No Speech Detected

**Symptom**: "No speech detected" error after recording

**Possible causes:**

1. **Microphone not working**:

   ```bash
   # Test microphone
   pw-record --rate 44100 test.wav
   # Speak for 5 seconds, then Ctrl+C
   # Play back
   pw-play test.wav
   ```

2. **Wrong input device selected**:

   - Open GNOME Settings → Sound → Input
   - Verify correct microphone is selected
   - Adjust input volume (70-90% recommended)

3. **Background noise too high**:

   - Reduce ambient noise
   - Move closer to microphone
   - Use noise-cancelling microphone if available

4. **Language mismatch**: check **Settings... → Transcription → Language**, or try
   language detection (see [Language Configuration](#language-configuration)).

### Slow Transcription

**Symptom**: Takes >10 seconds to transcribe short phrases

**Solutions:**

1. **Use a smaller model**: in **Settings... → Transcription → Whisper Model**, choose
   `base` or `tiny` instead of `auto` (download it first with **Manage Whisper Models...**).

2. **Check GPU is being used**:

   ```bash
   # During transcription, check GPU activity
   nvidia-smi -l 1
   # Should show ~80-100% GPU utilization
   ```

3. **Reduce model size** if VRAM insufficient:

   ```bash
   # Check VRAM usage
   nvidia-smi
   ```

4. **Close other GPU applications** (browsers with hardware acceleration, games, etc.)

### Incorrect Transcription

**Symptom**: Wrong words, incorrect spelling

**Solutions:**

1. **Speak more clearly**:

   - Normal pace, not too fast
   - Enunciate clearly
   - Pause between sentences

2. **Reduce background noise**

3. **Use a larger model** for better accuracy: in **Settings... → Transcription →
   Whisper Model**, choose `large-v3-turbo`, `large-v3` or, for English,
   `distil-large-v3.5` (on a GPU, `auto` already picks one of these).

4. **Force the correct language**: set **Settings... → Transcription → Language**
   rather than relying on language detection.

5. **Check microphone quality** - some built-in laptop mics are poor quality

### Streaming Mode Issues

**Symptom**: RealtimeSTT not working, dependencies fail to install

**Solutions:**

1. **Manual install** (if playbook fails):

   ```bash
   pip install --user RealtimeSTT portaudio-devel
   ```

2. **System dependencies**:

   ```bash
   sudo dnf install portaudio-devel python3-devel
   ```

3. **Check script exists**:

   ```bash
   ls -l ~/.local/bin/wsi-stream
   chmod +x ~/.local/bin/wsi-stream
   ```

4. **Test streaming mode**:

   ```bash
   wsi-stream --debug
   ```

### Debug Logs

All operations are logged for troubleshooting:

```bash
# View recent logs
tail -n 100 ~/.local/share/speech-to-text/debug.log

# Real-time logging
tail -f ~/.local/share/speech-to-text/debug.log

# Enable verbose logging
wsi -d  # Run with debug flag
```

**Log rotation**: Logs auto-rotate at 1MB to prevent disk space issues.

---

## Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                     GNOME Shell Extension                       │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  UI: Icon, Menu, Keybindings (Insert, Ctrl+Insert)      │   │
│  │  DBus: Signals (StateChanged, Error, Progress)          │   │
│  └────────────────┬─────────────────────────────────────────┘   │
└────────────────────┼─────────────────────────────────────────────┘
                     │ Spawns
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│                    WSI Backend Script                           │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Audio: pw-record → sox resample → wav file             │   │
│  │  Transcription: faster-whisper (GPU) or whisper.cpp     │   │
│  │  Post-process: wsi-claude-process (optional)            │   │
│  │  Output: ydotool auto-paste or clipboard                │   │
│  └────────────────┬─────────────────────────────────────────┘   │
└────────────────────┼─────────────────────────────────────────────┘
                     │ Calls
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│               Transcription Engines                             │
│  ┌─────────────────────┐    ┌──────────────────────────────┐   │
│  │  faster-whisper     │    │  RealtimeSTT (streaming)    │   │
│  │  - GPU: CUDA        │    │  - standard, pre-buffer     │   │
│  │  - CPU: fallback    │    │  - Higher GPU load          │   │
│  │  - Batch mode       │    │  - Experimental             │   │
│  │  - Server mode:     │    └──────────────────────────────┘   │
│  │    continuous: VAD  │                                       │
│  │    cuts, ordered    │                                       │
│  │    worker. Off: one │                                       │
│  │    clip, no VAD     │                                       │
│  └─────────────────────┘                                       │
└─────────────────────────────────────────────────────────────────┘
                     │ Optional
                     ▼
┌─────────────────────────────────────────────────────────────────┐
│              Claude Code Post-Processing                        │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Input: Raw transcription                                │   │
│  │  Prompt: ~/.config/speech-to-text/claude-prompt-*.txt   │   │
│  │  Model: sonnet (default), opus, haiku                   │   │
│  │  Output: Enhanced, formatted text                        │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### File Locations

```
System Files:
  /usr/bin/pw-record         - PipeWire audio capture
  /usr/bin/sox               - Audio resampling
  /usr/bin/ydotool           - Keyboard simulation
  /run/ydotool.socket        - ydotool daemon socket (0666)

Extension:
  ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/
    ├── extension.js         - Main extension logic
    ├── metadata.json        - Extension metadata
    └── schemas/             - GSettings schema

Scripts:
  ~/.local/bin/
    ├── wsi                  - Main backend (batch mode)
    ├── wsi-stream           - Streaming mode backend
    ├── wsi-stream-server    - Warm server for streaming Server mode
    ├── wsi-setting          - Prints one of the extension's settings
    ├── wsi-stop-grace       - Prints the stop grace (stop-grace-seconds)
    ├── wsi-resolve-model    - Prints the model a recording loads (what auto means)
    ├── wsi-claude-process   - Claude Code integration
    └── faster-whisper-transcribe - GPU Whisper wrapper

Units:
  ~/.config/systemd/user/wsi-stream-server-at-login.service - Server at login (if set)

Configuration:
  ~/.config/speech-to-text/
    ├── claude-prompt-corporate.txt  - Corporate style prompt
    └── claude-prompt-natural.txt    - Natural style prompt

Data:
  ~/.cache/huggingface/hub/  - Whisper models cache (~41MB-~3GB per model)
  ~/.cache/speech-to-text/   - Last transcription cache
  ~/.local/share/speech-to-text/
    └── debug.log            - Debug logs (auto-rotates at 1MB)
  /dev/shm/                  - Temporary audio files (RAM disk)
```

### Dependencies

**System packages** (via DNF):

- sox - Audio resampling
- ydotool - Keyboard simulation
- wl-clipboard - Wayland clipboard
- zenity - Dialogs
- wev - Wayland event viewer (debugging)
- portaudio-devel - Audio I/O (for RealtimeSTT)
- python3-devel - Python headers

**Python packages** (via pip):

- faster-whisper - GPU-accelerated Whisper
- nvidia-cublas-cu12 - CUDA BLAS library
- nvidia-cudnn-cu12 - CUDA DNN library
- RealtimeSTT - Real-time streaming transcription

---

## Performance Tips

1. **Model selection**: Start with `small`, upgrade to `medium` if accuracy matters more than speed

2. **GPU memory**: Close unnecessary applications before transcribing long sessions

3. **Audio quality**: Better microphone = better accuracy (garbage in, garbage out)

4. **Background noise**: Quiet environment dramatically improves accuracy

5. **Speaking style**: Natural pace, clear enunciation, pauses between thoughts

6. **Claude processing**: Use only when needed - adds 2-5 seconds processing time

---

## Credits

- **Whisper model**: OpenAI
- **faster-whisper**: https://github.com/guillaumekln/faster-whisper
- **RealtimeSTT**: https://github.com/KoljaB/RealtimeSTT
- **Integration**: fedora-desktop project

---

**See also:**

- [NVIDIA Driver Installation](../playbooks.md#play-nvidiayml)
- [Claude Code Setup](../playbooks.md#play-claude-yoloyml)
- [Containerization Guide](../containerization.md)
