import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Clutter from 'gi://Clutter';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

export default class SpeechToTextExtension {
    constructor() {
        this._recording = false;
        this._recProcess = null;
        this._audioFile = null;
        this._keyPressId = null;
        this._indicator = null;
        this._debugEnabled = false;
        this._logFile = GLib.get_home_dir() + '/.cache/speech-to-text.log';
    }

    _log(message) {
        if (!this._debugEnabled) return;

        try {
            const timestamp = new Date().toISOString();
            const logLine = `[${timestamp}] ${message}\n`;
            const file = Gio.File.new_for_path(this._logFile);
            const stream = file.append_to(Gio.FileCreateFlags.NONE, null);
            stream.write(logLine, null);
            stream.close(null);
        } catch (e) {
            // Ignore logging errors
        }
    }

    enable() {
        this._log('Extension enabled');

        // Create panel indicator
        this._indicator = new PanelMenu.Button(0.0, 'Speech to Text', false);

        // Add icon to panel
        const icon = new St.Icon({
            icon_name: 'audio-input-microphone-symbolic',
            style_class: 'system-status-icon'
        });
        this._indicator.add_child(icon);

        // Add menu items
        const debugItem = new PopupMenu.PopupSwitchMenuItem('Debug Logging', this._debugEnabled);
        debugItem.connect('toggled', (item) => {
            this._debugEnabled = item.state;
            this._log(`Debug logging ${this._debugEnabled ? 'enabled' : 'disabled'}`);
        });
        this._indicator.menu.addMenuItem(debugItem);

        const statusItem = new PopupMenu.PopupMenuItem('Status: Ready', { reactive: false });
        this._statusItem = statusItem;
        this._indicator.menu.addMenuItem(statusItem);

        const logItem = new PopupMenu.PopupMenuItem('View Logs');
        logItem.connect('activate', () => {
            GLib.spawn_command_line_async(`gnome-text-editor ${this._logFile}`);
        });
        this._indicator.menu.addMenuItem(logItem);

        // Add to panel
        Main.panel.addToStatusArea('speech-to-text-indicator', this._indicator);

        // Listen for Insert key press (toggle mode)
        const stage = global.stage;
        this._keyPressId = stage.connect('key-press-event', (actor, event) => {
            const keyval = event.get_key_symbol();

            // Insert key = 0xff63 (Clutter.KEY_Insert)
            if (keyval === Clutter.KEY_Insert) {
                this._log(`Insert key pressed, recording=${this._recording}`);

                if (this._recording) {
                    this._stopRecording();
                } else {
                    this._startRecording();
                }
                return Clutter.EVENT_STOP;
            }
            return Clutter.EVENT_PROPAGATE;
        });
    }

    disable() {
        this._log('Extension disabled');

        if (this._keyPressId) {
            global.stage.disconnect(this._keyPressId);
            this._keyPressId = null;
        }

        // Remove indicator from panel
        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        // Stop any active recording
        if (this._recording && this._recProcess) {
            this._stopRecording();
        }
    }

    _startRecording() {
        this._log('_startRecording() called');
        this._recording = true;
        this._audioFile = `/dev/shm/stt-${GLib.get_user_name()}-${Date.now()}`;
        this._log(`Audio file: ${this._audioFile}`);

        // Update status
        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Recording...';
        }

        // Show notification
        Main.notify('Speech to Text', '🎤 Recording... (Press Insert to stop)');

        try {
            // Start sox recording at 44100Hz
            // No silence detection - user controls with toggle
            // Max 60s safety limit
            const recCmd = [
                'rec',
                '-q',
                '-r', '44100',
                '-c', '2',
                '-t', 'wav',
                this._audioFile,
                'trim', '0', '60'
            ];

            this._log(`Starting rec command: ${recCmd.join(' ')}`);

            this._recProcess = Gio.Subprocess.new(
                recCmd,
                Gio.SubprocessFlags.NONE
            );

            this._log(`Recording process started, PID: ${this._recProcess.get_identifier()}`);

            // Watch for process completion (should only happen on manual stop or 60s limit)
            this._recProcess.wait_async(null, (proc, result) => {
                this._log('Recording process wait_async callback triggered');
                try {
                    proc.wait_finish(result);
                    this._log(`Recording process exited with status: ${proc.get_exit_status()}`);

                    // Process ended (either stopped or hit 60s limit)
                    if (this._recording) {
                        this._log('Recording flag still true, calling _transcribeAndPaste()');
                        this._recording = false;
                        this._transcribeAndPaste();
                    }
                } catch (e) {
                    this._log(`Recording wait error: ${e.message}`);
                    this._recording = false;
                    Main.notify('Speech to Text', `❌ Recording failed: ${e.message}`);
                    if (this._statusItem) {
                        this._statusItem.label.text = 'Status: Error';
                    }
                }
            });
        } catch (e) {
            this._log(`Failed to start recording: ${e.message}`);
            this._recording = false;
            Main.notify('Speech to Text', `❌ Failed to start recording: ${e.message}`);
            if (this._statusItem) {
                this._statusItem.label.text = 'Status: Error';
            }
        }
    }

    _stopRecording() {
        this._log('_stopRecording() called');

        if (!this._recProcess) {
            this._log('No recording process, returning');
            this._recording = false;
            return;
        }

        this._recording = false;

        // Update status
        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Stopping...';
        }

        try {
            // Send SIGINT to rec process to stop gracefully
            this._log(`Sending SIGINT to process ${this._recProcess.get_identifier()}`);
            this._recProcess.send_signal(2);

            // Give it a moment to finish writing the file
            GLib.timeout_add(GLib.PRIORITY_DEFAULT, 100, () => {
                this._log('Timeout fired, calling _transcribeAndPaste()');
                this._transcribeAndPaste();
                return GLib.SOURCE_REMOVE;
            });
        } catch (e) {
            this._log(`Failed to stop recording: ${e.message}`);
            Main.notify('Speech to Text', `❌ Failed to stop recording: ${e.message}`);
            if (this._statusItem) {
                this._statusItem.label.text = 'Status: Error';
            }
        }
    }

    _transcribeAndPaste() {
        this._log('_transcribeAndPaste() called');

        if (!this._audioFile) {
            this._log('No audio file, returning');
            return;
        }

        this._log(`Audio file: ${this._audioFile}`);

        // Update status
        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Transcribing...';
        }

        Main.notify('Speech to Text', '⏳ Transcribing...');

        try {
            // Run wsi script to transcribe
            const wsiCmd = [
                GLib.get_home_dir() + '/.local/bin/wsi-transcribe',
                this._audioFile
            ];

            this._log(`Running command: ${wsiCmd.join(' ')}`);

            const wsiProcess = Gio.Subprocess.new(
                wsiCmd,
                Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
            );

            wsiProcess.communicate_utf8_async(null, null, (proc, result) => {
                this._log('wsi-transcribe async callback triggered');
                try {
                    const [, stdout, stderr] = proc.communicate_utf8_finish(result);
                    const exitStatus = proc.get_exit_status();

                    this._log(`wsi-transcribe exit status: ${exitStatus}`);
                    this._log(`stdout: ${stdout || '(empty)'}`);
                    this._log(`stderr: ${stderr || '(empty)'}`);

                    if (exitStatus === 0 && stdout && stdout.trim()) {
                        const text = stdout.trim();
                        this._log(`Transcribed text (${text.length} chars): ${text}`);
                        this._pasteText(text);

                        // Show success notification with preview
                        const preview = text.length > 50 ? text.substring(0, 50) + '...' : text;
                        Main.notify('Speech to Text', `✅ Pasted: ${preview}`);

                        if (this._statusItem) {
                            this._statusItem.label.text = 'Status: Ready';
                        }
                    } else {
                        this._log(`Transcription failed: exit=${exitStatus}, stderr=${stderr}`);
                        Main.notify('Speech to Text', `❌ Transcription failed: ${stderr || 'No output'}`);

                        if (this._statusItem) {
                            this._statusItem.label.text = 'Status: Failed';
                        }
                    }
                } catch (e) {
                    this._log(`Transcription error: ${e.message}`);
                    Main.notify('Speech to Text', `❌ Error: ${e.message}`);

                    if (this._statusItem) {
                        this._statusItem.label.text = 'Status: Error';
                    }
                } finally {
                    // Clean up temp file
                    try {
                        const file = Gio.File.new_for_path(this._audioFile);
                        file.delete(null);
                        this._log(`Deleted temp file: ${this._audioFile}`);
                    } catch (e) {
                        this._log(`Failed to delete temp file: ${e.message}`);
                    }
                    this._audioFile = null;
                }
            });
        } catch (e) {
            this._log(`Failed to start transcription: ${e.message}`);
            Main.notify('Speech to Text', `❌ Failed to transcribe: ${e.message}`);

            if (this._statusItem) {
                this._statusItem.label.text = 'Status: Error';
            }
        }
    }

    _pasteText(text) {
        this._log(`_pasteText() called with text: ${text}`);

        try {
            // Method 1: Try wtype (Wayland)
            this._log('Attempting wtype (Wayland)');
            try {
                const wtypeProcess = Gio.Subprocess.new(
                    ['wtype', text],
                    Gio.SubprocessFlags.NONE
                );
                wtypeProcess.wait(null);
                this._log('wtype succeeded');
                return;
            } catch (e) {
                this._log(`wtype failed: ${e.message}`);
            }

            // Method 2: Try xdotool (X11)
            this._log('Attempting xdotool (X11)');
            try {
                const xdotoolProcess = Gio.Subprocess.new(
                    ['xdotool', 'type', '--', text],
                    Gio.SubprocessFlags.NONE
                );
                xdotoolProcess.wait(null);
                this._log('xdotool succeeded');
                return;
            } catch (e) {
                this._log(`xdotool failed: ${e.message}`);
            }

            // Method 3: Fallback to clipboard (requires manual paste)
            this._log('Falling back to clipboard');
            const wm = GLib.getenv('XDG_SESSION_TYPE');
            this._log(`XDG_SESSION_TYPE: ${wm}`);

            if (wm === 'wayland') {
                Gio.Subprocess.new(
                    ['wl-copy'],
                    Gio.SubprocessFlags.STDIN_PIPE
                ).communicate_utf8(text, null);
                this._log('wl-copy executed');
            } else {
                Gio.Subprocess.new(
                    ['xsel', '-ib'],
                    Gio.SubprocessFlags.STDIN_PIPE
                ).communicate_utf8(text, null);
                this._log('xsel executed');
            }

            Main.notify('Speech to Text', '📋 Text in clipboard (auto-paste failed, press Ctrl+V)');
        } catch (e) {
            this._log(`Failed to paste: ${e.message}`);
            Main.notify('Speech to Text', `❌ Failed to paste: ${e.message}`);
        }
    }
}
