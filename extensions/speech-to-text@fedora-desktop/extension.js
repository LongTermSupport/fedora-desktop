import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import St from 'gi://St';
import Meta from 'gi://Meta';
import Shell from 'gi://Shell';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

export default class SpeechToTextExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._recording = false;
        this._recProcess = null;
        this._audioFile = null;
        this._indicator = null;
        this._icon = null;
        this._statusItem = null;
        this._settings = null;
        this._logFile = '/tmp/stt-debug.log';

        // Clear log file on construction
        try {
            const f = Gio.File.new_for_path(this._logFile);
            f.replace_contents('=== STT Extension Log ===\n', null, false, Gio.FileCreateFlags.REPLACE_DESTINATION, null);
        } catch(e) {}
    }

    _log(message) {
        try {
            const timestamp = new Date().toISOString();
            const logLine = `[${timestamp}] ${message}\n`;
            const file = Gio.File.new_for_path(this._logFile);
            const stream = file.append_to(Gio.FileCreateFlags.NONE, null);
            stream.write(logLine, null);
            stream.close(null);
        } catch (e) {}
    }

    enable() {
        this._log('=== ENABLE CALLED ===');

        // Create panel indicator
        this._indicator = new PanelMenu.Button(0.0, 'Speech to Text', false);
        this._log('Created panel button');

        // Add icon to panel
        this._icon = new St.Icon({
            icon_name: 'audio-input-microphone-symbolic',
            style_class: 'system-status-icon'
        });
        this._indicator.add_child(this._icon);
        this._log('Created icon');

        // Add menu items
        const statusItem = new PopupMenu.PopupMenuItem('Status: Ready', { reactive: false });
        this._statusItem = statusItem;
        this._indicator.menu.addMenuItem(statusItem);

        const logItem = new PopupMenu.PopupMenuItem('View Debug Log');
        logItem.connect('activate', () => {
            GLib.spawn_command_line_async(`gnome-text-editor ${this._logFile}`);
        });
        this._indicator.menu.addMenuItem(logItem);
        this._log('Created menu items');

        // Add to panel
        Main.panel.addToStatusArea('speech-to-text-indicator', this._indicator);
        this._log('Added to panel status area');

        // Set up keybinding using proper GNOME Shell API
        this._log('Setting up keybinding via Main.wm.addKeybinding...');

        try {
            // Get settings from extension's schema
            const schemaDir = this.path + '/schemas';
            this._log(`Schema directory: ${schemaDir}`);

            const schemaSource = Gio.SettingsSchemaSource.new_from_directory(
                schemaDir,
                Gio.SettingsSchemaSource.get_default(),
                false
            );
            this._log('Created schema source');

            const schema = schemaSource.lookup('org.gnome.shell.extensions.speech-to-text', true);
            if (!schema) {
                this._log('ERROR: Schema not found!');
                Main.notify('STT Error', 'Schema not found - keybinding will not work');
            } else {
                this._log('Schema found');
                this._settings = new Gio.Settings({ settings_schema: schema });
                this._log('Settings created');

                // Add the keybinding
                Main.wm.addKeybinding(
                    'toggle-recording',
                    this._settings,
                    Meta.KeyBindingFlags.NONE,
                    Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                    () => {
                        this._log('>>> KEYBINDING TRIGGERED! <<<');
                        this._log(`Current recording state: ${this._recording}`);
                        Main.notify('STT', `Keybind! recording=${this._recording}`);

                        if (this._recording) {
                            this._stopRecording();
                        } else {
                            this._startRecording();
                        }
                    }
                );
                this._log('Keybinding added successfully');
            }
        } catch (e) {
            this._log(`ERROR setting up keybinding: ${e.message}`);
            this._log(`Stack: ${e.stack}`);
            Main.notify('STT Error', `Keybinding setup failed: ${e.message}`);
        }

        Main.notify('Speech to Text', 'Extension loaded. Press Insert to record.');
        this._log('=== ENABLE COMPLETE ===');
    }

    disable() {
        this._log('=== DISABLE CALLED ===');

        // Remove keybinding
        try {
            Main.wm.removeKeybinding('toggle-recording');
            this._log('Keybinding removed');
        } catch (e) {
            this._log(`Error removing keybinding: ${e.message}`);
        }

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        if (this._recording && this._recProcess) {
            this._stopRecording();
        }

        this._settings = null;
        this._log('=== DISABLE COMPLETE ===');
    }

    _startRecording() {
        this._log('=== _startRecording() ===');
        this._recording = true;
        this._audioFile = `/dev/shm/stt-${GLib.get_user_name()}-${Date.now()}.wav`;
        this._log(`Audio file: ${this._audioFile}`);

        if (this._icon) {
            this._icon.style = 'color: #ff4444;';
        }

        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Recording...';
        }

        Main.notify('Speech to Text', 'Recording... (Press Insert to stop)');

        try {
            const recCmd = [
                'rec', '-q', '-r', '44100', '-c', '2', '-t', 'wav',
                this._audioFile, 'trim', '0', '60'
            ];
            this._log(`Executing: ${recCmd.join(' ')}`);

            this._recProcess = Gio.Subprocess.new(recCmd, Gio.SubprocessFlags.NONE);
            this._log(`Recording started, PID: ${this._recProcess.get_identifier()}`);

            this._recProcess.wait_async(null, (proc, result) => {
                this._log('Recording process callback');
                try {
                    proc.wait_finish(result);
                    if (this._recording) {
                        this._recording = false;
                        this._transcribeAndPaste();
                    }
                } catch (e) {
                    this._log(`Recording error: ${e.message}`);
                    this._recording = false;
                }
            });
        } catch (e) {
            this._log(`EXCEPTION: ${e.message}`);
            this._recording = false;
            Main.notify('STT Error', e.message);
        }
    }

    _stopRecording() {
        this._log('=== _stopRecording() ===');

        if (this._icon) {
            this._icon.style = '';
        }

        if (!this._recProcess) {
            this._log('No process');
            this._recording = false;
            return;
        }

        this._recording = false;

        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Stopping...';
        }

        try {
            this._recProcess.send_signal(2);
            this._log('Sent SIGINT');

            GLib.timeout_add(GLib.PRIORITY_DEFAULT, 500, () => {
                this._log('Timeout - calling transcribe');
                this._transcribeAndPaste();
                return GLib.SOURCE_REMOVE;
            });
        } catch (e) {
            this._log(`EXCEPTION: ${e.message}`);
        }
    }

    _transcribeAndPaste() {
        this._log('=== _transcribeAndPaste() ===');

        if (!this._audioFile) {
            this._log('No audio file');
            return;
        }

        const audioFileObj = Gio.File.new_for_path(this._audioFile);
        if (!audioFileObj.query_exists(null)) {
            this._log('Audio file missing');
            Main.notify('STT Error', 'Audio file not found');
            return;
        }

        let fileSize = 0;
        try {
            const info = audioFileObj.query_info('standard::size', Gio.FileQueryInfoFlags.NONE, null);
            fileSize = info.get_size();
            this._log(`File size: ${fileSize} bytes`);
        } catch(e) {}

        if (this._statusItem) {
            this._statusItem.label.text = 'Status: Ready';
        }

        // For now, just report success without transcribing to test the flow
        Main.notify('Speech to Text', `Recording saved! ${fileSize} bytes. File: ${this._audioFile}`);
        this._log('=== SKIPPING TRANSCRIPTION FOR TEST ===');

        // Don't delete the file so we can test manually
        // try { Gio.File.new_for_path(this._audioFile).delete(null); } catch(e) {}
        this._audioFile = null;
        this._recProcess = null;
    }

    _pasteText(text) {
        this._log(`=== _pasteText("${text}") ===`);

        try {
            const wtypeProcess = Gio.Subprocess.new(['wtype', '--', text], Gio.SubprocessFlags.NONE);
            wtypeProcess.wait(null);
            if (wtypeProcess.get_exit_status() === 0) {
                this._log('wtype succeeded');
                return;
            }
        } catch (e) {
            this._log(`wtype failed: ${e.message}`);
        }

        try {
            const wlcopyProcess = Gio.Subprocess.new(['wl-copy', '--', text], Gio.SubprocessFlags.NONE);
            wlcopyProcess.wait(null);
            this._log('wl-copy succeeded');
            Main.notify('STT', 'Copied to clipboard (Ctrl+V)');
        } catch (e) {
            this._log(`wl-copy failed: ${e.message}`);
        }
    }
}
