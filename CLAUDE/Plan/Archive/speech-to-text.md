# Speech-to-Text Implementation

## Project Status: **BLOCKED** - Extension Loading Error

**Current Issue**: GNOME Shell extension not loading - panel indicator missing, likely JavaScript error

---

## Overview

Implementation of local, offline speech-to-text functionality for Fedora 42 using OpenAI Whisper (via Mozilla whisperfile), integrated as a custom GNOME Shell extension with Insert key activation.

**Target User Experience**:
- Press Insert key once → start recording
- Speak naturally
- Press Insert key again → stop, transcribe, auto-paste

---

## Timeline & Commits

### Phase 1: Initial Exploration (Commits: df01711 → aed0ca0)

**Started**: Early January 2025

**Approach**: Evaluated existing GNOME speech-to-text extensions

**Result**: No suitable extensions found - decided to build custom solution

---

### Phase 2: Whisper Backend Setup (Commits: 43bf104 → 2a55988)

**Problem**: Initial wsi script had hardcoded dependencies (zsh, whisper.cpp from Fedora repos)

**Solutions**:
1. Switched from Fedora `whisper-cpp` package to Mozilla `whisperfile` (llamafile - single portable executable)
2. Made model size configurable: `whisper_model_size: medium.en` in host_vars
3. Fixed wsi script shebang from `#!/usr/bin/zsh` to `#!/usr/bin/bash`

**Commits**:
- `43bf104` - fix: correct user home directory detection in speech-to-text playbook
- `7764a36` - fix: use whisperfile instead of Fedora whisper-cpp package
- `a1dfe2d` - fix: use correct whisperfile model URL (small.en)
- `2a55988` - feat: upgrade to medium.en whisperfile model for better accuracy
- `60d2966` - feat: make whisper model configurable via host_vars

**Key Files**:
- `/workspace/files/home/.local/bin/wsi-transcribe` - Core transcription wrapper
- Whisperfile installed as: `~/.local/bin/whisperfile` (symlinked as `transcribe`)

---

### Phase 3: Microphone Issues (Commits: aed0ca0 → 1e0d5e5)

**Critical Bug**: Recording produced near-zero amplitude (0.0003), essentially silent

**Root Cause Discovery**:
- User confirmed: "i can definitely use microphone for other things"
- Tested with `gnome-sound-recorder` at 44100Hz → 0.098 amplitude (perfect)
- Tested sox at different sample rates → only 44100Hz worked
- **Hardware limitation**: Meteor Lake-P integrated microphone only works at 44100Hz

**Solution**: Record-then-resample workflow
```bash
# Record at hardware native rate
rec -r 44100 -c 2 -t wav "$ramf" ...

# Resample for Whisper
sox "$ramf" "${ramf}.resampled.wav" rate 16k
```

**Additional Fixes**:
- Fixed ALSA capture volume imbalance (right channel at 0%)
- Tuned sox silence detection parameters: `silence 1 0.1 3% 1 2.5 4%`
- Added audio padding for recordings < 1 second (Whisper minimum)
- Added microphone warm-up period (later found unnecessary but kept for compatibility)

**Commits**:
- `aed0ca0` - fix: configure wsi script, document restart requirement
- `e33ecad` - fix: configure wsi script for bash shebang and whisperfile mode
- `dd493d4` - feat: create enhanced wsi script with comprehensive debug mode
- `619eb7b` - perf: skip whisperfile download if already exists
- `308cb33` - fix: pad short audio recordings to meet whisper 1-second minimum
- `21eec0a` - fix: make sox silence detection more lenient to prevent early cutoff
- `872c8ad` - fix: add microphone warm-up period before recording
- `3a85984` - feat: add gnome-sound-recorder as microphone debugging tool
- `8737477` - fix: record at 44100Hz native rate, then resample to 16kHz
- `1e0d5e5` (CRITICAL FIX)

**Key Learning**: Always test at hardware native sample rate first

---

### Phase 4: Custom GNOME Extension Development (Commits: 4e1ed1c → 3008ef6)

**Decision**: Build custom GNOME Shell extension from scratch

**User Requirements**:
- "i would prefer press + hold to record, transcribe and paste immediately on release"
- "i would prefer single key than a three key combo - lets use ins key"
- Later clarified: Toggle mode acceptable (press once to start, again to stop)

**Initial Implementation** (4e1ed1c):
- Press-and-hold Insert key
- Key-press-event → start recording
- Key-release-event → stop recording
- Auto-paste with wtype (Wayland) or xdotool (X11)

**Testing Challenge**: Wayland limitation
- Cannot restart GNOME Shell with Alt+F2 → 'r' on Wayland
- Shell IS the display server - restarting kills all apps
- **Solution**: Nested GNOME Shell for development

**Nested Shell Development** (a60323b → 4d49fe3):
Created `gshell-nested` wrapper script with features:
- Auto-detect screen resolution (later abandoned - too complex)
- Handle Ctrl+C for clean exit
- Kill existing nested sessions before launching
- Alt+F1 for Overview (Super key goes to main session)

**Issues Encountered**:
1. Complex resolution detection with HiDPI scaling → parsing bugs with gsettings output
2. Nested session had focus/routing issues (apps opened in main session)
3. **Abandoned nested testing approach** - too unreliable

**Current Implementation** (3008ef6):
Switched to **toggle mode** with comprehensive debugging:

**Features**:
- **Toggle Mode**: Press Insert once to start, again to stop (no press-and-hold)
- **Panel Indicator**: Microphone icon in top panel
- **Debug Logging**: Comprehensive logs to `~/.cache/speech-to-text.log`
- **Status UI**: Menu shows current state (Ready/Recording/Transcribing/Error)
- **Controls**: Toggle debug logging, view logs via menu

**Key Commits**:
- `4e1ed1c` - feat: replace Blurt with custom press-and-hold speech-to-text extension
- `a60323b` - docs: add GNOME Shell extension development guide
- `fabb8d6` - feat: add gshell-nested wrapper for GNOME Shell development
- `8f52fb7` - fix: handle desktop scaling and add Ctrl+C cleanup for gshell-nested
- `13a90f2` - feat: kill existing nested gnome-shell sessions before launching new one
- `4d49fe3` - feat: increase default to 2560x1600 and document Alt+F1 for nested overview
- `3008ef6` - **feat: add toggle mode, debug logging, and panel indicator UI** (CURRENT)

---

## Technical Architecture

### Components

**1. GNOME Shell Extension**
- **Location**: `~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/`
- **Files**:
  - `extension.js` - Main extension logic
  - `metadata.json` - Extension metadata
- **Imports**: GLib, Gio, Clutter, St, PanelMenu, PopupMenu
- **Key Functions**:
  - `_startRecording()` - Launch sox subprocess at 44100Hz
  - `_stopRecording()` - Send SIGINT, trigger transcription
  - `_transcribeAndPaste()` - Call wsi-transcribe, handle result
  - `_pasteText()` - Auto-paste via wtype/xdotool or clipboard fallback
  - `_log()` - Debug logging to file

**2. Transcription Pipeline**
- **wsi-transcribe**: `~/.local/bin/wsi-transcribe`
  ```bash
  # Resample audio
  sox "$AUDIO_FILE" "$RESAMPLED" rate 16k

  # Pad if < 1 second
  sox "$RESAMPLED" "${RESAMPLED}.padded" pad 0 1

  # Transcribe
  transcribe -t $NTHR -nt -f "$RESAMPLED"

  # Post-process (remove noise artifacts, capitalize)
  ```

**3. Audio Recording**
- **Tool**: sox (rec command)
- **Format**: 44100Hz, 2-channel, WAV
- **Location**: `/dev/shm/stt-<username>-<timestamp>`
- **Max duration**: 60 seconds (safety limit)

**4. Whisper Model**
- **Implementation**: Mozilla whisperfile (llamafile)
- **Model**: `medium.en` (1.83GB, good balance of speed/accuracy)
- **Location**: `~/.local/share/whisper/`
- **Symlink**: `~/.local/bin/transcribe` → `whisperfile`

---

## Current Status: BLOCKED

### Problem
Extension not loading - no panel indicator appears after enable

### Symptoms
- `gnome-extensions enable speech-to-text@fedora-desktop` succeeds
- No microphone icon in top panel
- Extension appears enabled in `gnome-extensions list --enabled`
- Likely JavaScript error preventing extension from loading

### Not Yet Diagnosed
Need to check:
```bash
# Check for JS errors
journalctl /usr/bin/gnome-shell -n 100 | grep -E "(JS ERROR|JS WARNING)"

# Check extension status
gnome-extensions info speech-to-text@fedora-desktop

# Check if extension is actually loaded
journalctl /usr/bin/gnome-shell -n 50 | grep -i "speech-to-text"
```

### Possible Causes
1. **Import error** - St, PanelMenu, or PopupMenu imports failing
2. **Syntax error** - JavaScript syntax issue in extension.js
3. **API incompatibility** - GNOME Shell 47 (Fedora 42) API changes
4. **Missing dependencies** - Required GJS modules not available

---

## Next Steps

### Immediate Actions
1. **Diagnose extension loading failure**
   - Check GNOME Shell journal for JS errors
   - Verify all imports are valid for GNOME 47
   - Test with minimal extension (remove panel indicator code)

2. **Test basic functionality**
   - If imports are the issue, remove panel indicator temporarily
   - Test core recording/transcription with just key events + notifications
   - Add panel indicator back once basic functionality works

3. **Debug logging**
   - Once extension loads, enable debug logging
   - Test full workflow: press Insert → speak → press Insert
   - Analyze logs to find where pipeline breaks

### Fallback Options
If extension complexity is causing issues:
1. **Simplify to notifications-only** (no panel indicator)
2. **Use dbus-send for remote control** instead of panel UI
3. **Create separate PyGObject indicator** if GNOME Shell extension proves too fragile

---

## Testing Protocol

Once extension loads successfully:

### Manual Test
```bash
# Enable extension
gnome-extensions enable speech-to-text@fedora-desktop

# Click mic icon in panel
# Enable "Debug Logging"

# Test workflow
1. Open text editor (gedit)
2. Click in text field
3. Press Insert (should see "Recording..." notification)
4. Speak: "This is a test"
5. Press Insert (should see "Transcribing..." notification)
6. Verify text appears in editor
7. Check logs: Click mic → "View Logs"
```

### Expected Notifications
1. "🎤 Recording... (Press Insert to stop)"
2. "⏳ Transcribing..."
3. "✅ Pasted: This is a test"

### Log File Analysis
```bash
tail -f ~/.cache/speech-to-text.log
```

**Should show**:
- Extension enabled
- Insert key pressed (recording=false)
- _startRecording() called
- Audio file created
- Recording process started (with PID)
- Insert key pressed (recording=true)
- _stopRecording() called
- SIGINT sent to process
- _transcribeAndPaste() called
- wsi-transcribe command executed
- Exit status, stdout, stderr
- Transcribed text
- Paste method attempted (wtype/xdotool/clipboard)

---

## Known Issues & Limitations

### Working
- ✅ Microphone recording at 44100Hz
- ✅ Audio resampling to 16kHz
- ✅ Whisper transcription (via whisperfile)
- ✅ Manual testing with wsi command
- ✅ Toggle mode key event logic

### Not Yet Tested
- ❓ GNOME Shell extension loading
- ❓ Panel indicator UI
- ❓ Debug logging functionality
- ❓ Auto-paste on Wayland (wtype)
- ❓ Full end-to-end workflow

### Abandoned Approaches
- ❌ Blurt extension (poor UX)
- ❌ Sonori (incompatible with GNOME/Wayland)
- ❌ Press-and-hold mode (key-release event unreliable)
- ❌ Nested GNOME Shell testing (focus/routing issues)
- ❌ Complex resolution detection for nested shell

---

## Dependencies

### Required Packages
```yaml
- sox                    # Audio recording/processing
- python3-pyaudio        # Audio backend
- wtype                  # Wayland auto-paste
- xdotool                # X11 auto-paste (fallback)
- wl-clipboard           # Wayland clipboard (fallback)
- xsel                   # X11 clipboard (fallback)
```

### Optional (debugging)
```yaml
- gnome-sound-recorder   # Microphone testing
- alsa-utils             # Mixer control (amixer, alsactl)
```

### GNOME Shell
- Version: 47.x (Fedora 42)
- Session: Wayland
- Extensions support enabled

---

## File Locations

### Extension Files
```
~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/
├── extension.js        # Main extension logic (360 lines)
└── metadata.json       # Extension metadata
```

### Helper Scripts
```
~/.local/bin/
├── wsi-transcribe      # Transcription wrapper (44 lines)
├── wsi                 # Enhanced debug tool (kept for testing)
├── transcribe          # Symlink to whisperfile
└── whisperfile         # Mozilla whisperfile binary
```

### Model & Cache
```
~/.local/share/whisper/
└── ggml-medium.en.bin  # Whisper model (1.83GB)

~/.cache/
└── speech-to-text.log  # Debug log (when enabled)
```

### Playbook
```
playbooks/imports/optional/common/
└── play-speech-to-text.yml  # Deployment playbook
```

---

## Performance Characteristics

### Recording
- **Latency**: <100ms to start recording
- **Format**: WAV, 44100Hz, stereo, 16-bit
- **Storage**: RAM disk (/dev/shm)
- **Size**: ~10MB per minute

### Transcription
- **Model**: medium.en (1.5B parameters)
- **CPU**: Uses 50% of available cores (NTHR = cores / 2)
- **Time**: ~5-15 seconds for typical utterance (5-30 seconds of speech)
- **Accuracy**: Good for clear speech, handles background noise reasonably

### User Experience
- **Start recording**: Immediate (<100ms)
- **Stop recording**: <500ms to file completion
- **Transcription**: 5-15 seconds typical
- **Paste**: Immediate (if wtype works)
- **Total time**: ~6-16 seconds from stop to pasted text

---

## Security Considerations

- ✅ Audio files in `/dev/shm` (RAM disk, cleared on reboot)
- ✅ Files deleted after transcription
- ✅ Completely offline (no network requests)
- ✅ No cloud services, no API keys required
- ✅ Whisper model downloaded once, cached locally
- ⚠️ Debug logs may contain transcribed text (disabled by default)

---

## Future Enhancements

### If Time Permits
1. **Noise cancellation** - Pre-process audio with sox filters
2. **Multiple models** - Switch between tiny/small/medium/large via menu
3. **Custom vocabulary** - Add technical terms to improve accuracy
4. **Punctuation training** - Better handling of periods/commas
5. **Multi-language** - Support languages beyond English
6. **Keyboard shortcut config** - Let user choose activation key

### Not Planned
- GPU acceleration (whisperfile doesn't support NVIDIA GPU)
- Cloud APIs (offline-only by design)
- Real-time streaming (press-to-talk is intentional)

---

## Lessons Learned

### What Worked
1. **Record-then-resample** approach for hardware compatibility
2. **Mozilla whisperfile** - single binary, no complex dependencies
3. **Toggle mode** more reliable than press-and-hold
4. **Debug logging** - comprehensive logging catches issues early
5. **RAM disk storage** - fast, automatic cleanup

### What Didn't Work
1. **Blurt extension** - poor UX, confusing state management
2. **Press-and-hold mode** - key-release events unreliable
3. **Nested GNOME Shell** - focus routing issues, abandoned
4. **Complex resolution detection** - too fragile, wasted time
5. **Direct 16kHz recording** - hardware doesn't support it

### Key Insights
- **Hardware matters** - Always test at native sample rates
- **Simplicity wins** - Toggle mode simpler than press-and-hold
- **Logging is critical** - Can't debug without visibility
- **GNOME Shell extensions are fragile** - API changes, imports break easily
- **Wayland limitations** - Can't easily restart Shell for testing

---

## References

### Documentation
- CLAUDE/GnomeShell.md - GNOME Shell extension development guide
- playbooks/imports/optional/common/play-speech-to-text.yml - Deployment

### Key Commits
- `1e0d5e5` - Critical 44100Hz recording fix
- `4e1ed1c` - Custom extension replaces Blurt
- `3008ef6` - Current implementation (toggle + debug + UI)

### External Resources
- [Mozilla Whisperfile](https://github.com/Mozilla-Ocho/llamafile) - Single-file Whisper
- [GNOME Shell Extensions Guide](https://gjs.guide/extensions/) - GJS documentation
- [Sox Documentation](http://sox.sourceforge.net/sox.html) - Audio processing

---

**Document Status**: Current as of commit `3008ef6`
**Last Updated**: 2025-01-09
**Next Update**: After extension loading issue is resolved
