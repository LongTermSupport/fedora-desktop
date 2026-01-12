# Speech to Text GNOME Extension

Local, offline speech-to-text for GNOME Shell using OpenAI's Whisper model with GPU acceleration.

## Features

- **🎤 Press Insert to Record** - One key to start/stop recording
- **⏱️ 30-Second Countdown Timer** - Visual countdown shows remaining time
  - Green background: 27-11 seconds
  - Yellow background: 10-6 seconds
  - Red flashing: 5-0 seconds - Alternates red/white backgrounds
- **🚀 GPU-Accelerated** - Uses NVIDIA CUDA for fast transcription
- **🔒 100% Offline** - No cloud services, all processing local
- **📋 Flexible Output** - Auto-paste, clipboard, or middle-click paste
- **💾 Transcription History** - Last transcription saved and accessible
- **🎯 Auto-Stop** - Recording automatically stops at 30 seconds
- **⚙️ Debug Mode** - Built-in logging and troubleshooting tools

## Requirements

### Hardware
- **NVIDIA GPU** (recommended for speed) - Falls back to CPU if unavailable
- **Microphone** - Any working audio input device

### Software
- **Fedora 42+** (or compatible GNOME 45-48)
- **PipeWire** - For audio recording (default in Fedora)
- **sox** - Audio processing
- **ydotool** - For auto-paste functionality
- **wl-clipboard** - Clipboard management (Wayland)
- **Python 3.11+** with pip
- **NVIDIA drivers** - For GPU acceleration (optional but recommended)

## Installation

### Automated Install (Recommended)

```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```

This will:
1. Install all system dependencies
2. Set up GPU-accelerated Whisper
3. Install the GNOME Shell extension
4. Configure ydotool for auto-paste

### Manual Install

1. Install dependencies:
```bash
sudo dnf install sox ydotool wl-clipboard python3-pip
```

2. Install faster-whisper:
```bash
pip install --user faster-whisper nvidia-cublas-cu12 nvidia-cudnn-cu12
```

3. Copy extension files:
```bash
cp -r extensions/speech-to-text@fedora-desktop ~/.local/share/gnome-shell/extensions/
```

4. Enable extension:
```bash
gnome-extensions enable speech-to-text@fedora-desktop
```

5. Restart GNOME Shell (logout/login)

## Usage

### Basic Recording

1. **Press Insert** - Start recording (countdown begins at 27 seconds)
2. **Speak clearly** into your microphone
3. **Press Insert again** - Stop recording manually
   - Or wait for auto-stop at 30 seconds
4. **Text appears** - Auto-pasted at cursor or in clipboard

### Panel Icon States

- **Microphone** (gray) - Idle, ready to record
- **REC 27** (white text, green background) - Recording, 11-27 seconds remaining
- **REC 9** (white text, yellow background) - Recording, 6-10 seconds remaining
- **REC 3** (flashing) - Recording, 5 or fewer seconds remaining
  - Flashes between red background/white text and white background/red text every 500ms
- **Spinner/Loading** (orange) - Transcribing audio
- **Microphone** (green) - Success! Text ready
- **Microphone** (red) - Error occurred

### Output Modes

Click the microphone icon to access the menu:

1. **Auto-paste** (default) - Types text directly at cursor position
   - Best for: Quick dictation into any app
   - Uses: ydotool for universal text insertion

2. **Use Ctrl+V (not middle-click)** - Copies to CLIPBOARD
   - Best for: Pasting with Ctrl+V
   - Uses: Standard clipboard (Ctrl+V to paste)

3. **Middle-click paste** (neither option enabled) - Copies to PRIMARY
   - Best for: Linux-style middle-click paste
   - Uses: Primary selection (middle-click to paste)

### Menu Options

- **Debug Logging** - Enable detailed logs to troubleshoot issues
- **View Debug Log** - Open log file in text editor
- **Clear Debug Log** - Remove all log entries
- **Copy Last Transcription** - Copy previous result to clipboard
- **View Last Transcription** - Open saved transcription in editor
- **Auto-paste at cursor** - Toggle direct text insertion
- **Use Ctrl+V (not middle-click)** - Toggle clipboard mode

## Configuration

### Change Whisper Model

Edit your Ansible host variables:

```yaml
# environment/localhost/host_vars/localhost.yml
stt_model: "small"  # Options: tiny, base, small, medium, large-v3
```

Then re-run the playbook:
```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```

**Model Sizes:**
- `tiny` - Fastest, least accurate (~75MB)
- `base` - Fast, decent accuracy (~145MB)
- `small` - Good balance (~466MB) - **Default**
- `medium` - Better accuracy, slower (~1.5GB)
- `large-v3` - Best accuracy, slowest (~2.9GB)

### Keybinding

Default: **Insert** key

To change, edit the schema:
```xml
<!-- schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml -->
<key name="toggle-recording" type="as">
  <default>["Insert"]</default>
</key>
```

Then recompile:
```bash
glib-compile-schemas ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop/schemas
```

## Troubleshooting

### Infinite Key Repeat / Character Stuck

**Symptom:** After recording, a character repeats infinitely (usually 'a' or last typed key).

**Fix:**
```bash
pkill -9 ydotool
sudo systemctl restart ydotool
```

**Root Cause:** ydotool got stuck in a loop. This has been fixed in the latest version with proper timeout handling.

### Extension Not Loading

**Check if enabled:**
```bash
gnome-extensions list --enabled | grep speech-to-text
```

**If not listed, enable it:**
```bash
gnome-extensions enable speech-to-text@fedora-desktop
```

**Check for errors:**
```bash
journalctl -f /usr/bin/gnome-shell
```

### No Audio Recording

**Check PipeWire:**
```bash
pw-record --list-targets
```

**Test microphone:**
```bash
pw-record --rate 44100 --channels 2 test.wav
# Press Ctrl+C after a few seconds
aplay test.wav
```

### Recording Stops Immediately

**Check for conflicting Insert key binding:**
```bash
dconf list /org/gnome/desktop/wm/keybindings/ | grep -i insert
```

**Check ydotool service:**
```bash
sudo systemctl status ydotool
```

### Transcription Errors

**Enable debug mode:**
1. Click microphone icon
2. Toggle "Debug Logging" ON
3. Try recording again
4. View logs: Click "View Debug Log"

**Check Whisper installation:**
```bash
~/.local/bin/faster-whisper-transcribe --help
```

### GPU Not Working

**Verify NVIDIA drivers:**
```bash
nvidia-smi
```

**Check CUDA libraries:**
```bash
python3 -c "import nvidia.cublas; import nvidia.cudnn; print('CUDA libraries OK')"
```

**Falls back to CPU automatically** if GPU unavailable (slower but works).

### Auto-Paste Failed / ydotool Issues

**Check if ydotool service is running:**
```bash
sudo systemctl status ydotool
```

**If not running, start it:**
```bash
sudo systemctl start ydotool
sudo systemctl enable ydotool
```

**Check socket exists and has correct permissions:**
```bash
ls -l /run/ydotool.socket
```

Should show: `srw-rw-rw-` (writable by everyone)

**If socket missing or wrong permissions:**
```bash
sudo systemctl stop ydotool
sudo rm -f /run/ydotool.socket
sudo systemctl start ydotool
```

**If still failing, re-run playbook to reconfigure:**
```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```

The playbook configures ydotool to create a world-writable socket at `/run/ydotool.socket`.

## Known Limitations

- **30-second maximum** - Hard limit due to Whisper's context window
- **CPU mode is slow** - Transcription takes 5-10 seconds without GPU
- **English by default** - Whisper supports other languages but not configured
- **No streaming** - Entire recording must complete before transcription
- **Background noise** - Quiet environment recommended for best results
- **Audio quality** - Built-in laptop mics may produce poor results

## Advanced Usage

### View Saved Transcriptions

All transcriptions saved to:
```
~/.cache/speech-to-text/last-transcription.txt
```

View with:
```bash
cat ~/.cache/speech-to-text/last-transcription.txt
```

### Debug Logs

Located at:
```
~/.local/share/speech-to-text/debug.log
```

View with:
```bash
tail -f ~/.local/share/speech-to-text/debug.log
```

### Manual Recording with WSI Script

```bash
# Basic recording
~/.local/bin/wsi

# With debug output
~/.local/bin/wsi --debug

# Auto-paste mode
~/.local/bin/wsi --auto-paste

# Clipboard mode
~/.local/bin/wsi --clipboard
```

## Development

### Project Structure

```
speech-to-text@fedora-desktop/
├── README.md              # This file
├── metadata.json          # Extension metadata
├── extension.js           # Main extension code (GNOME Shell)
├── wsi                    # Bash script (recording + transcription)
└── schemas/               # GSettings schema
    └── org.gnome.shell.extensions.speech-to-text.gschema.xml
```

### Testing Changes

1. Make changes to `extension.js` or `wsi`
2. Run linter (JavaScript only):
   ```bash
   cd /workspace/extensions && npm run lint
   ```
3. Deploy via playbook:
   ```bash
   ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
   ```
4. Reload extension:
   ```bash
   gnome-extensions disable speech-to-text@fedora-desktop
   gnome-extensions enable speech-to-text@fedora-desktop
   ```

### ESLint Protection

The project uses ESLint to prevent blocking operations that freeze GNOME Shell:

```bash
cd /workspace/extensions && npm run lint
```

This catches dangerous synchronous calls like `communicate()`, `wait()`, etc. that would lock up the UI.

## Credits

- **Based on:** [Blurt](https://github.com/QuantiusBenignus/blurt) by QuantiusBenignus
- **Whisper:** OpenAI's speech recognition model
- **faster-whisper:** SYSTRAN's optimized implementation

## License

Part of the [fedora-desktop](https://github.com/LongTermSupport/fedora-desktop) configuration management project.

## Support

For issues and feature requests:
- Check debug logs first
- Review troubleshooting section above
- Open an issue on GitHub with logs attached
