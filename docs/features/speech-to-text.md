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

- **Fedora 42** (this branch)
- **NVIDIA GPU** with CUDA support (GTX 10-series or newer recommended)
- **NVIDIA drivers installed** via `play-nvidia.yml`
- **Active internet** for initial model downloads (cached afterwards)

### Hardware Recommendations

- **Minimum**: GTX 1050 Ti (2GB VRAM) - use tiny/base models
- **Recommended**: RTX 2060 (6GB VRAM) - use small/medium models
- **Optimal**: RTX 3060+ (12GB VRAM) - use large models

### Disk Space

- Model cache: 40MB (tiny) to 2.9GB (large-v3)
- RealtimeSTT dependencies: ~2GB on first install (PyTorch, etc.)
- Temporary audio files: ~10MB per recording

---

## Installation

### Step 1: Install NVIDIA Drivers (if not already done)

```bash
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/imports/optional/hardware-specific/play-nvidia.yml
```

Reboot after driver installation to ensure CUDA is available.

### Step 2: Install Speech-to-Text Extension

```bash
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
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

Edit your Ansible host variables **before** running the playbook:

```yaml
# File: environment/localhost/host_vars/localhost.yml

# Model size: tiny, base, small, medium, large-v3
stt_model: small  # Default, good balance of speed/accuracy

# Language: 'en' (English), 'es' (Spanish), etc. or '' for auto-detect
stt_language: en  # Default
```

**Model comparison:**

| Model    | Size  | VRAM | Speed     | Accuracy  | Use Case                        |
| -------- | ----- | ---- | --------- | --------- | ------------------------------- |
| tiny     | 40MB  | 1GB  | Very fast | Good      | Quick notes, commands           |
| base     | 150MB | 1GB  | Fast      | Better    | General use                     |
| small    | 500MB | 2GB  | Medium    | Good      | **Recommended default**         |
| medium   | 1.5GB | 5GB  | Slower    | Very good | Professional work               |
| large-v3 | 2.9GB | 10GB | Slow      | Excellent | Transcription accuracy critical |

**Re-run playbook after changing model:**

```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```

Models are cached in `~/.cache/huggingface/hub/` and shared between batch and streaming modes.

### Language Configuration

By default, transcription is configured for English (`en`). To change:

```yaml
# Auto-detect language (not recommended - slower and less accurate)
stt_language: ""

# Specific language
stt_language: es  # Spanish
stt_language: fr  # French
stt_language: de  # German
```

### Streaming Mode Setup

Enable real-time transcription in extension settings:

1. Click extension icon in top bar
2. Select **"Streaming mode (instant)"**

**First-time streaming setup:**

- Downloads large dependencies (~2GB PyTorch)
- May take 5-15 minutes
- Subsequent uses are instant

---

## Usage

### Basic Workflow

1. **Start Recording**: Press **Insert** key

   - 🎤 Red microphone icon appears
   - Desktop notification: "Recording..."
   - Maximum duration: 30 seconds

2. **Speak Clearly**: Say what you want to type

   - Speak at normal pace
   - Minimize background noise
   - Pause briefly between sentences

3. **Stop Recording**: Press **Insert** again

   - Icon changes to ⚙️ (processing)
   - Desktop notification: "Transcribing..."

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

- **Mode selection**: Batch (default) or Streaming (instant)
- **Quick settings**: Model info, configuration tips
- **Extension preferences**: Opens GNOME Settings

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

Enable via extension menu: **Settings → Streaming mode (instant)**

```
You: Press Insert → Start speaking "The quick brown fox..."
System: [Words appear in real-time as you speak]
Output: The quick brown fox jumps over the lazy dog.
```

**Characteristics:**

- Words appear instantly while speaking
- Useful for long-form dictation
- Higher GPU load
- Experimental feature

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

Configure via playbook host vars or command-line:

```yaml
# Host vars (permanent)
claude_stt_model: sonnet  # sonnet (default), opus, haiku
```

```bash
# Command-line (temporary)
wsi --claude-process --claude-model opus --claude-style natural
```

**Model trade-offs:**

- **haiku**: Fastest, cheapest, good for simple cleanup
- **sonnet**: Best balance (default)
- **opus**: Most capable, best for complex formatting

---

## Icon Reference

The extension icon indicates current status:

| Icon | Status            | Meaning                                  |
| ---- | ----------------- | ---------------------------------------- |
| 🎤   | Recording         | Listening to your voice (Insert to stop) |
| ⚙️   | Processing        | Transcribing audio                       |
| 🤖   | Claude Processing | AI enhancement (corporate mode)          |
| 💬   | Claude Processing | AI enhancement (natural mode)            |
| ✅   | Success           | Transcription complete                   |
| ⚠️   | Error             | Something went wrong (check logs)        |
| ⏸️   | Idle              | Ready for next recording                 |

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
   ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
   ```

4. If GPU still unavailable, extension falls back to CPU (slower but functional)

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
   ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
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

4. **Language mismatch**:

   ```yaml
   # Try auto-detect
   stt_language: ""
   ```

### Slow Transcription

**Symptom**: Takes >10 seconds to transcribe short phrases

**Solutions:**

1. **Use smaller model**:

   ```yaml
   stt_model: tiny  # or base
   ```

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

3. **Use larger model** for better accuracy:

   ```yaml
   stt_model: medium  # or large-v3
   ```

4. **Force correct language**:

   ```yaml
   stt_language: en  # Don't rely on auto-detect
   ```

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
│  │  DBus: Signals (StateChanged, Error)                    │   │
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
│  │  - GPU: CUDA        │    │  - Real-time transcription  │   │
│  │  - CPU: fallback    │    │  - Higher GPU load          │   │
│  │  - Batch mode       │    │  - Experimental             │   │
│  └─────────────────────┘    └──────────────────────────────┘   │
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
    ├── wsi-claude-process   - Claude Code integration
    └── faster-whisper-transcribe - GPU Whisper wrapper

Configuration:
  ~/.config/speech-to-text/
    ├── claude-prompt-corporate.txt  - Corporate style prompt
    └── claude-prompt-natural.txt    - Natural style prompt

Data:
  ~/.cache/huggingface/hub/  - Whisper models cache (40MB-2.9GB)
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
- [Claude Code Setup](../playbooks.md#play-claude-codeyml)
- [Containerization Guide](../containerization.md)
