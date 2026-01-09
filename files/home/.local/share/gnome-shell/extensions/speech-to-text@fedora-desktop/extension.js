import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Clutter from 'gi://Clutter';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

export default class SpeechToTextExtension {
    constructor() {
        this._recording = false;
        this._recProcess = null;
        this._audioFile = null;
        this._keyPressId = null;
        this._keyReleaseId = null;
    }

    enable() {
        const stage = global.stage;

        // Listen for Insert key press (key down)
        this._keyPressId = stage.connect('key-press-event', (actor, event) => {
            const keyval = event.get_key_symbol();

            // Insert key = 0xff63 (Clutter.KEY_Insert)
            if (keyval === Clutter.KEY_Insert && !this._recording) {
                this._startRecording();
                return Clutter.EVENT_STOP;
            }
            return Clutter.EVENT_PROPAGATE;
        });

        // Listen for Insert key release (key up)
        this._keyReleaseId = stage.connect('key-release-event', (actor, event) => {
            const keyval = event.get_key_symbol();

            // Insert key released
            if (keyval === Clutter.KEY_Insert && this._recording) {
                this._stopRecording();
                return Clutter.EVENT_STOP;
            }
            return Clutter.EVENT_PROPAGATE;
        });
    }

    disable() {
        if (this._keyPressId) {
            global.stage.disconnect(this._keyPressId);
            this._keyPressId = null;
        }

        if (this._keyReleaseId) {
            global.stage.disconnect(this._keyReleaseId);
            this._keyReleaseId = null;
        }

        // Stop any active recording
        if (this._recording && this._recProcess) {
            this._stopRecording();
        }
    }

    _startRecording() {
        this._recording = true;
        this._audioFile = `/dev/shm/stt-${GLib.get_user_name()}-${Date.now()}`;

        // Show notification
        Main.notify('Speech to Text', '🎤 Recording... (Hold Insert key)');

        try {
            // Start sox recording at 44100Hz
            // No silence detection - user controls with key release
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

            this._recProcess = Gio.Subprocess.new(
                recCmd,
                Gio.SubprocessFlags.NONE
            );

            // Watch for process completion (should only happen on manual stop or 60s limit)
            this._recProcess.wait_async(null, (proc, result) => {
                try {
                    proc.wait_finish(result);

                    // Process ended (either stopped or hit 60s limit)
                    if (this._recording) {
                        this._recording = false;
                        this._transcribeAndPaste();
                    }
                } catch (e) {
                    this._recording = false;
                    Main.notify('Speech to Text', `❌ Recording failed: ${e.message}`);
                }
            });
        } catch (e) {
            this._recording = false;
            Main.notify('Speech to Text', `❌ Failed to start recording: ${e.message}`);
        }
    }

    _stopRecording() {
        if (!this._recProcess) {
            this._recording = false;
            return;
        }

        this._recording = false;

        try {
            // Send SIGINT to rec process to stop gracefully
            this._recProcess.send_signal(2);

            // Give it a moment to finish writing the file
            GLib.timeout_add(GLib.PRIORITY_DEFAULT, 100, () => {
                this._transcribeAndPaste();
                return GLib.SOURCE_REMOVE;
            });
        } catch (e) {
            Main.notify('Speech to Text', `❌ Failed to stop recording: ${e.message}`);
        }
    }

    _transcribeAndPaste() {
        if (!this._audioFile) {
            return;
        }

        Main.notify('Speech to Text', '⏳ Transcribing...');

        try {
            // Run wsi script to transcribe
            const wsiCmd = [
                GLib.get_home_dir() + '/.local/bin/wsi-transcribe',
                this._audioFile
            ];

            const wsiProcess = Gio.Subprocess.new(
                wsiCmd,
                Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
            );

            wsiProcess.communicate_utf8_async(null, null, (proc, result) => {
                try {
                    const [, stdout, stderr] = proc.communicate_utf8_finish(result);

                    if (proc.get_exit_status() === 0 && stdout && stdout.trim()) {
                        const text = stdout.trim();
                        this._pasteText(text);

                        // Show success notification with preview
                        const preview = text.length > 50 ? text.substring(0, 50) + '...' : text;
                        Main.notify('Speech to Text', `✅ Pasted: ${preview}`);
                    } else {
                        Main.notify('Speech to Text', `❌ Transcription failed: ${stderr || 'No output'}`);
                    }
                } catch (e) {
                    Main.notify('Speech to Text', `❌ Error: ${e.message}`);
                } finally {
                    // Clean up temp file
                    try {
                        const file = Gio.File.new_for_path(this._audioFile);
                        file.delete(null);
                    } catch (e) {
                        // Ignore cleanup errors
                    }
                    this._audioFile = null;
                }
            });
        } catch (e) {
            Main.notify('Speech to Text', `❌ Failed to transcribe: ${e.message}`);
        }
    }

    _pasteText(text) {
        try {
            // Method 1: Try wtype (Wayland)
            try {
                const wtypeProcess = Gio.Subprocess.new(
                    ['wtype', text],
                    Gio.SubprocessFlags.NONE
                );
                wtypeProcess.wait(null);
                return;
            } catch (e) {
                // wtype not available, try xdotool
            }

            // Method 2: Try xdotool (X11)
            try {
                const xdotoolProcess = Gio.Subprocess.new(
                    ['xdotool', 'type', '--', text],
                    Gio.SubprocessFlags.NONE
                );
                xdotoolProcess.wait(null);
                return;
            } catch (e) {
                // xdotool not available
            }

            // Method 3: Fallback to clipboard (requires manual paste)
            const wm = GLib.getenv('XDG_SESSION_TYPE');
            if (wm === 'wayland') {
                Gio.Subprocess.new(
                    ['wl-copy'],
                    Gio.SubprocessFlags.STDIN_PIPE
                ).communicate_utf8(text, null);
            } else {
                Gio.Subprocess.new(
                    ['xsel', '-ib'],
                    Gio.SubprocessFlags.STDIN_PIPE
                ).communicate_utf8(text, null);
            }

            Main.notify('Speech to Text', '📋 Text in clipboard (auto-paste failed, press Ctrl+V)');
        } catch (e) {
            Main.notify('Speech to Text', `❌ Failed to paste: ${e.message}`);
        }
    }
}
