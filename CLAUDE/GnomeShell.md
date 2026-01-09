# GNOME Shell Development Guide

## Extensions

### Overview

GNOME Shell extensions extend the functionality of the GNOME desktop environment. This project includes custom extensions for desktop automation.

**Current Extensions:**
- `speech-to-text@fedora-desktop` - Press-and-hold Insert key for speech-to-text with auto-paste

### Extension Development Workflow

#### Directory Structure

Extensions are stored in:
```
~/.local/share/gnome-shell/extensions/<extension-uuid>/
├── metadata.json    # Extension metadata (name, version, shell versions)
└── extension.js     # Main extension code
```

**Project Structure:**
```
files/home/.local/share/gnome-shell/extensions/
└── speech-to-text@fedora-desktop/
    ├── metadata.json
    └── extension.js
```

#### Deployment

Extensions are deployed via Ansible playbook:
```bash
ansible-playbook playbooks/imports/optional/common/play-speech-to-text.yml
```

This copies extension files from `files/home/.local/share/gnome-shell/extensions/` to user home directory.

### Testing Extensions

#### Method 1: Nested GNOME Shell (Recommended for Development)

**Requirements:**
- GNOME Shell 3.36+ (Fedora 42 has 48.7 ✓)
- Wayland session
- No logout/login required

**Usage:**
```bash
# Start nested GNOME Shell in a window
dbus-run-session -- gnome-shell --nested --wayland
```

**Inside Nested Window:**
1. Press **Super** key to open Activities
2. Type "Extensions" and open Extensions app
3. Enable your extension (e.g., "Speech to Text")
4. Test functionality in nested apps
5. Press **CTRL+Q** or close window to exit

**Advantages:**
- Test without logging out
- Rapid iteration during development
- Isolated from main session
- Extensions from `~/.local/share/gnome-shell/extensions/` are available

**Limitations:**
- Some features may behave differently than real session
- Performance might not match real session
- Hardware interactions (mic, clipboard) work within nested instance only

#### Method 2: Real Session (Required for Final Testing)

**On X11 (if using X11 session):**
```bash
# Quick restart (keeps applications open)
Alt+F2 → type "r" → Enter
```

**On Wayland (default):**
- **NO way to restart GNOME Shell without killing all apps**
- GNOME Shell IS the display server on Wayland
- Must log out and log back in:
  ```bash
  gnome-session-quit --logout --no-prompt
  ```

**After restart/login:**
```bash
# Enable extension
gnome-extensions enable <extension-uuid>

# Check extension status
gnome-extensions list --enabled

# View extension details
gnome-extensions show <extension-uuid>
```

**Wayland Limitation Reference:**
- [Red Hat Bugzilla #1909803](https://bugzilla.redhat.com/show_bug.cgi?id=1909803) - Alt-F2+r doesn't work in Wayland
- [Linux Uprising](https://www.linuxuprising.com/2020/07/how-to-restart-gnome-shell-from-command.html) - Restart only works on X11

### Extension Management

#### Enable/Disable

```bash
# Enable extension
gnome-extensions enable speech-to-text@fedora-desktop

# Disable extension
gnome-extensions disable speech-to-text@fedora-desktop

# List all extensions
gnome-extensions list

# List enabled extensions only
gnome-extensions list --enabled

# Show extension details
gnome-extensions show speech-to-text@fedora-desktop
```

#### Uninstall

```bash
# Remove extension files
rm -rf ~/.local/share/gnome-shell/extensions/speech-to-text@fedora-desktop

# Restart GNOME Shell (see Testing section above)
```

### Debugging Extensions

#### View Extension Logs

```bash
# Real-time GNOME Shell logs (includes extensions)
journalctl -f /usr/bin/gnome-shell

# Filter for specific extension
journalctl /usr/bin/gnome-shell --since "5 minutes ago" | grep -i "speech-to-text"

# View all logs since boot
journalctl /usr/bin/gnome-shell -b
```

#### Looking Glass (GNOME Shell Debugger)

**Access:**
1. Press `Alt+F2`
2. Type `lg` and press Enter
3. Looking Glass window opens

**Features:**
- JavaScript console for live debugging
- Extension error messages
- Object inspection
- Live code execution

**Useful Commands in Looking Glass:**
```javascript
// List loaded extensions
global.display

// Inspect extension object
Main.extensionManager.lookup('speech-to-text@fedora-desktop')

// View recent errors
Main.notificationDaemon
```

### Extension Development Tips

#### Key Event Handling

**Press-and-Hold Pattern (used in speech-to-text extension):**
```javascript
enable() {
    const stage = global.stage;

    // Capture key press (key down)
    this._keyPressId = stage.connect('key-press-event', (actor, event) => {
        const keyval = event.get_key_symbol();
        if (keyval === Clutter.KEY_Insert) {
            this._startAction();
            return Clutter.EVENT_STOP;
        }
        return Clutter.EVENT_PROPAGATE;
    });

    // Capture key release (key up)
    this._keyReleaseId = stage.connect('key-release-event', (actor, event) => {
        const keyval = event.get_key_symbol();
        if (keyval === Clutter.KEY_Insert) {
            this._stopAction();
            return Clutter.EVENT_STOP;
        }
        return Clutter.EVENT_PROPAGATE;
    });
}

disable() {
    if (this._keyPressId) {
        global.stage.disconnect(this._keyPressId);
    }
    if (this._keyReleaseId) {
        global.stage.disconnect(this._keyReleaseId);
    }
}
```

**Important:**
- `EVENT_STOP` prevents key from propagating to applications
- `EVENT_PROPAGATE` allows normal key handling
- Always disconnect event handlers in `disable()`

#### Notifications

```javascript
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

// Show notification
Main.notify('Extension Name', 'Message text');

// Notification shows in top bar and notification area
```

#### Running Subprocesses

```javascript
import Gio from 'gi://Gio';

// Start background process
const process = Gio.Subprocess.new(
    ['command', 'arg1', 'arg2'],
    Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
);

// Wait for completion asynchronously
process.communicate_utf8_async(null, null, (proc, result) => {
    try {
        const [, stdout, stderr] = proc.communicate_utf8_finish(result);
        const exitCode = proc.get_exit_status();

        if (exitCode === 0) {
            // Success
            log(`Output: ${stdout}`);
        } else {
            // Error
            log(`Error: ${stderr}`);
        }
    } catch (e) {
        log(`Exception: ${e.message}`);
    }
});

// Send signal to process
process.send_signal(2); // SIGINT
```

### Common Pitfalls

#### 1. Forgotten Cleanup in disable()

**Problem:** Extension leaves event handlers or timeouts running after disable.

**Solution:** Always clean up in `disable()`:
```javascript
disable() {
    // Disconnect all event handlers
    if (this._handlerId) {
        someObject.disconnect(this._handlerId);
        this._handlerId = null;
    }

    // Remove timeouts
    if (this._timeoutId) {
        GLib.source_remove(this._timeoutId);
        this._timeoutId = null;
    }

    // Stop subprocesses
    if (this._process) {
        this._process.send_signal(2);
        this._process = null;
    }
}
```

#### 2. Incorrect Module Imports (GNOME Shell 45+)

**Old style (pre-45, doesn't work):**
```javascript
const Main = imports.ui.main;
const Gio = imports.gi.Gio;
```

**New style (45+, required):**
```javascript
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import Gio from 'gi://Gio';
```

#### 3. Forgetting to Return Proper Event Status

**Problem:** Key events leak to applications when they shouldn't.

**Solution:**
- Return `Clutter.EVENT_STOP` to consume the event
- Return `Clutter.EVENT_PROPAGATE` to allow normal handling

#### 4. Extension Not Appearing After Install

**Cause:** GNOME Shell hasn't rescanned extensions directory.

**Solution:**
- Use nested GNOME Shell for testing
- Or restart GNOME Shell (see Testing section)

### Resources

**Official Documentation:**
- [GJS Guide - Extension Development](https://gjs.guide/extensions/development/creating.html)
- [GNOME Shell Extension Tutorial](https://wiki.gnome.org/Projects/GnomeShell/Extensions)

**API References:**
- [GJS Documentation](https://gjs-docs.gnome.org/)
- [GNOME Shell Source](https://gitlab.gnome.org/GNOME/gnome-shell)

**Debugging:**
- [Looking Glass Documentation](https://wiki.gnome.org/Projects/GnomeShell/LookingGlass)
- [Extension Review Guidelines](https://gjs.guide/extensions/review-guidelines/review-guidelines.html)

### Project-Specific: speech-to-text Extension

**Purpose:** Press-and-hold Insert key for speech-to-text with automatic paste.

**Implementation:**
- `extension.js` - Captures Insert key press/release, manages recording subprocess
- `wsi-transcribe` - Helper script that transcribes audio file with whisperfile
- `wtype` - Auto-paste tool for Wayland (types text into active window)

**Workflow:**
1. User presses Insert key → extension starts `rec` subprocess
2. User releases Insert key → extension kills `rec`, calls `wsi-transcribe`
3. `wsi-transcribe` resamples audio to 16kHz, runs whisperfile, returns text
4. Extension receives transcribed text, uses `wtype` to paste into active window
5. Notifications shown at each step for user feedback

**Testing:**
```bash
# Test transcription manually
~/.local/bin/wsi -v

# Test in nested GNOME Shell
dbus-run-session -- gnome-shell --nested --wayland

# View extension logs
journalctl -f /usr/bin/gnome-shell | grep -i speech
```

**Key Bindings:**
- Insert key (press and hold)
- No modifier keys required
- Works in any application with text input
