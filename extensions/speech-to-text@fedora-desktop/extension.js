/**
 * Speech-to-Text GNOME Shell Extension
 *
 * Panel indicator with popup menu for:
 * - Status display (IDLE/RECORDING/TRANSCRIBING/SUCCESS/ERROR)
 * - Debug mode toggle
 * - View debug log
 * - Recent log preview
 *
 * Triggers wsi script on Insert key and listens for DBus signals.
 */

import St from 'gi://St';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import Meta from 'gi://Meta';
import Shell from 'gi://Shell';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

// DBus interface for wsi communication
const DBUS_PATH = '/org/fedoradesktop/SpeechToText';
const DBUS_INTERFACE = 'org.fedoradesktop.SpeechToText';

export default class SpeechToTextExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._indicator = null;
        this._settings = null;
        this._dbusSubscriptionId = null;
        this._dbusErrorSubscriptionId = null;

        // Debug mode state
        this._debugEnabled = false;
        this._clipboardMode = false;
        this._autoPaste = false;
        this._autoEnter = true;  // Default: send Enter after auto-paste
        this._wrapWithMarker = false;
        this._streamingMode = false;  // Use RealtimeSTT streaming instead of batch
        this._language = 'system';    // 'system' = detect from locale, or 'en', etc.
        this._currentState = 'IDLE';
        this._updatingToggles = false;  // Guard flag to prevent toggle cascade
        this._lastError = null;
        this._logDir = GLib.get_home_dir() + '/.local/share/speech-to-text';
        this._logFile = this._logDir + '/debug.log';
        this._iconResetTimeoutId = null;

        // Recording timer state
        this._recordingTimer = null;
        this._remainingSeconds = 27;  // 3s safety buffer before 30s hard limit
        this._countdownLabel = null;
        this._flashTimer = null;
        this._flashState = false;
    }

    enable() {
        // Create panel indicator with menu
        this._indicator = new PanelMenu.Button(0.0, 'Speech to Text', false);

        // Add icon
        this._icon = new St.Icon({
            icon_name: 'audio-input-microphone-symbolic',
            style_class: 'system-status-icon'
        });
        this._indicator.add_child(this._icon);

        // Build the popup menu
        this._buildMenu();

        // Add to panel
        Main.panel.addToStatusArea('speech-to-text', this._indicator);

        // Subscribe to DBus signals from wsi script
        this._subscribeToDBus();

        // Ensure log directory exists
        this._ensureLogDirectory();

        // Setup keybinding
        try {
            const schemaDir = this.path + '/schemas';
            const schemaSource = Gio.SettingsSchemaSource.new_from_directory(
                schemaDir,
                Gio.SettingsSchemaSource.get_default(),
                false
            );
            const schema = schemaSource.lookup('org.gnome.shell.extensions.speech-to-text', true);
            if (schema) {
                this._settings = new Gio.Settings({ settings_schema: schema });

                // Load debug setting
                this._loadDebugSetting();

                Main.wm.addKeybinding(
                    'toggle-recording',
                    this._settings,
                    Meta.KeyBindingFlags.NONE,
                    Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                    () => {
                        this._launchWSI();
                    }
                );
            }
        } catch (e) {
            Main.notify('STT Error', `Keybinding setup failed: ${e.message}`);
        }

        Main.notify('Speech to Text', 'Ready. Press Insert to record. Click icon for options.');
    }

    disable() {
        // Unsubscribe from DBus
        if (this._dbusSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusSubscriptionId);
            this._dbusSubscriptionId = null;
        }
        if (this._dbusErrorSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusErrorSubscriptionId);
            this._dbusErrorSubscriptionId = null;
        }

        // Remove keybinding
        try {
            Main.wm.removeKeybinding('toggle-recording');
        } catch (e) {
            // Ignore if keybinding doesn't exist
        }

        // Cancel pending icon reset timeout
        if (this._iconResetTimeoutId) {
            GLib.Source.remove(this._iconResetTimeoutId);
            this._iconResetTimeoutId = null;
        }

        // Cancel recording timer
        if (this._recordingTimer) {
            GLib.Source.remove(this._recordingTimer);
            this._recordingTimer = null;
        }

        // Cancel flash timer
        if (this._flashTimer) {
            GLib.Source.remove(this._flashTimer);
            this._flashTimer = null;
        }

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        this._settings = null;
        this._logLines = null;
    }

    // Prevent switch menu items from closing the menu on click
    // (GNOME Shell only fixed this for Space key, not mouse clicks)
    _preventMenuClose(switchItem) {
        switchItem.activate = () => {
            if (switchItem._switch.mapped) switchItem.toggle();
        };
    }

    _buildMenu() {
        const menu = this._indicator.menu;

        // Status section (non-interactive header)
        this._statusLabel = new PopupMenu.PopupMenuItem('Status: IDLE', { reactive: false });
        this._statusLabel.label.style = 'font-weight: bold;';
        menu.addMenuItem(this._statusLabel);

        // Separator
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Debug toggle
        this._debugSwitch = new PopupMenu.PopupSwitchMenuItem('Debug Logging', this._debugEnabled);
        this._preventMenuClose(this._debugSwitch);
        this._debugSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._debugEnabled = state;
            this._saveDebugSetting(state);
            this._log(`Debug mode ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._debugSwitch);

        // View logs button
        const viewLogsItem = new PopupMenu.PopupMenuItem('View Debug Log...');
        viewLogsItem.connect('activate', () => {
            this._openLogViewer();
        });
        menu.addMenuItem(viewLogsItem);

        // Clear logs button
        const clearLogsItem = new PopupMenu.PopupMenuItem('Clear Debug Log');
        clearLogsItem.connect('activate', () => {
            this._clearLog();
        });
        menu.addMenuItem(clearLogsItem);

        // Separator
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Copy last transcription button
        const copyTranscriptionItem = new PopupMenu.PopupMenuItem('Copy Last Transcription');
        copyTranscriptionItem.connect('activate', () => {
            this._copyLastTranscription();
        });
        menu.addMenuItem(copyTranscriptionItem);

        // View last transcription button
        const viewTranscriptionItem = new PopupMenu.PopupMenuItem('View Last Transcription...');
        viewTranscriptionItem.connect('activate', () => {
            this._openLastTranscription();
        });
        menu.addMenuItem(viewTranscriptionItem);

        // Separator
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Clipboard mode toggle
        this._clipboardSwitch = new PopupMenu.PopupSwitchMenuItem('Use Ctrl+V (not middle-click)', this._clipboardMode);
        this._preventMenuClose(this._clipboardSwitch);
        this._clipboardSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._clipboardMode = state;
            if (state) this._autoPaste = false;  // Mutually exclusive
            this._saveClipboardSetting(state);
            this._updatePasteToggles();
            this._log(`Clipboard mode ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._clipboardSwitch);

        // Auto-paste toggle
        this._autoPasteSwitch = new PopupMenu.PopupSwitchMenuItem('Auto-paste at cursor', this._autoPaste);
        this._preventMenuClose(this._autoPasteSwitch);
        this._autoPasteSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._autoPaste = state;
            if (state) this._clipboardMode = false;  // Mutually exclusive
            if (!state) {
                // Disable streaming when auto-paste is turned off
                this._streamingMode = false;
                this._saveStreamingSetting(false);
            }
            this._saveAutoPasteSetting(state);
            this._updatePasteToggles();
            this._log(`Auto-paste ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._autoPasteSwitch);

        // Auto-enter toggle (send Return after auto-paste) - child of auto-paste
        this._autoEnterSwitch = new PopupMenu.PopupSwitchMenuItem('  ↳ Send Enter after paste', this._autoEnter);
        this._preventMenuClose(this._autoEnterSwitch);
        this._autoEnterSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._autoEnter = state;
            this._saveAutoEnterSetting(state);
            this._log(`Auto-enter ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._autoEnterSwitch);

        // Streaming mode toggle - child of auto-paste (uses RealtimeSTT for real-time transcription)
        this._streamingSwitch = new PopupMenu.PopupSwitchMenuItem('  ↳ Streaming mode (instant)', this._streamingMode);
        this._preventMenuClose(this._streamingSwitch);
        this._streamingSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._streamingMode = state;
            this._saveStreamingSetting(state);
            this._log(`Streaming mode ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._streamingSwitch);

        // Wrap with marker toggle (works with all paste modes)
        this._wrapMarkerSwitch = new PopupMenu.PopupSwitchMenuItem('Wrap with speech-to-text marker', this._wrapWithMarker);
        this._preventMenuClose(this._wrapMarkerSwitch);
        this._wrapMarkerSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._wrapWithMarker = state;
            this._saveWrapMarkerSetting(state);
            this._log(`Wrap marker ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._wrapMarkerSwitch);

        // Separator before language
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Language submenu
        this._languageSubMenu = new PopupMenu.PopupSubMenuMenuItem('Language: System default');
        menu.addMenuItem(this._languageSubMenu);

        // Language options
        const languages = [
            ['system', 'System default'],
            ['en', 'English'],
        ];

        for (const [code, label] of languages) {
            const item = new PopupMenu.PopupMenuItem(label);
            item.connect('activate', () => {
                this._language = code;
                this._saveLanguageSetting(code);
                this._updateLanguageLabel();
                this._log(`Language set to: ${code}`);
            });
            this._languageSubMenu.menu.addMenuItem(item);
        }

        // Set initial visibility for child options
        this._updatePasteToggles();
        this._updateLanguageLabel();

        // Separator
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Recent log header
        const logHeader = new PopupMenu.PopupMenuItem('Recent Log:', { reactive: false });
        logHeader.label.style = 'font-style: italic; color: #888;';
        menu.addMenuItem(logHeader);

        // Placeholder for log lines (last 5)
        this._logLines = [];
        for (let i = 0; i < 5; i++) {
            const line = new PopupMenu.PopupMenuItem('', { reactive: false });
            line.label.style = 'font-size: 0.9em; font-family: monospace;';
            this._logLines.push(line);
            menu.addMenuItem(line);
        }

        // Refresh logs when menu opens
        menu.connect('open-state-changed', (menu, open) => {
            if (open) {
                this._refreshLogDisplay();
                this._updateStatusLabel();
            }
        });
    }

    _subscribeToDBus() {
        // Listen for StateChanged signals from wsi script
        this._dbusSubscriptionId = Gio.DBus.session.signal_subscribe(
            null,                    // sender (any)
            DBUS_INTERFACE,          // interface
            'StateChanged',          // signal name
            DBUS_PATH,               // object path
            null,                    // arg0 (any)
            Gio.DBusSignalFlags.NONE,
            (connection, sender, path, iface, signal, params) => {
                const state = params.get_child_value(0).get_string()[0];
                this._currentState = state;
                this._updateIconState(state);
                this._log(`State: ${state}`);

                // Clear error on success or new recording
                if (state === 'SUCCESS' || state === 'RECORDING') {
                    this._lastError = null;
                }
            }
        );

        // Listen for Error signals for detailed error messages
        this._dbusErrorSubscriptionId = Gio.DBus.session.signal_subscribe(
            null,
            DBUS_INTERFACE,
            'Error',
            DBUS_PATH,
            null,
            Gio.DBusSignalFlags.NONE,
            (connection, sender, path, iface, signal, params) => {
                const errorMsg = params.get_child_value(0).get_string()[0];
                this._lastError = errorMsg;
                this._log(`Error: ${errorMsg}`);
            }
        );
    }

    _updateIconState(state) {
        if (!this._icon) return;

        // Cancel any pending icon reset
        if (this._iconResetTimeoutId) {
            GLib.Source.remove(this._iconResetTimeoutId);
            this._iconResetTimeoutId = null;
        }

        switch (state) {
            case 'RECORDING':
                this._icon.style = 'color: #ff4444;';  // Red
                this._startCountdown();
                break;
            case 'TRANSCRIBING':
                this._stopCountdown();
                // Change icon to spinner/hourglass for transcribing
                this._icon.icon_name = 'content-loading-symbolic';
                this._icon.style = 'color: #ffaa00;';  // Orange/Yellow
                break;
            case 'SUCCESS':
                this._stopCountdown();
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = 'color: #44ff44;';  // Green
                // Reset to normal after 2 seconds
                this._iconResetTimeoutId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 2000, () => {
                    if (this._icon) this._icon.style = '';
                    this._iconResetTimeoutId = null;
                    return GLib.SOURCE_REMOVE;
                });
                break;
            case 'ERROR':
                this._stopCountdown();
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = 'color: #ff4444;';  // Red
                // Reset to normal after 2 seconds
                this._iconResetTimeoutId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 2000, () => {
                    if (this._icon) this._icon.style = '';
                    this._iconResetTimeoutId = null;
                    return GLib.SOURCE_REMOVE;
                });
                break;
            case 'IDLE':
            default:
                this._stopCountdown();
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = '';  // Default
                break;
        }
    }

    _startCountdown() {
        // Stop any existing countdown
        this._stopCountdown();

        // Initialize countdown (27s for 3s safety buffer before 30s hard limit)
        this._remainingSeconds = 27;

        // Replace icon with countdown label
        if (this._icon) {
            this._indicator.remove_child(this._icon);
        }

        // Start with green background, white text
        this._countdownLabel = new St.Label({
            text: 'REC 27',
            y_align: 2,  // Clutter.ActorAlign.CENTER
            style_class: 'system-status-icon',
            style: 'color: white; font-weight: bold; font-size: 13px; background-color: #44ff44; padding: 2px 4px; border-radius: 3px;'
        });
        this._indicator.add_child(this._countdownLabel);

        this._log('Countdown started: 27 seconds (3s safety buffer)');

        // Start 1-second timer
        this._recordingTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            this._remainingSeconds--;

            if (this._countdownLabel) {
                // Update text
                this._countdownLabel.text = `REC ${this._remainingSeconds}`;

                // Update color/style based on time remaining
                if (this._remainingSeconds > 10) {
                    // Green background, white text (27-11)
                    this._stopFlashing();
                    this._countdownLabel.style = 'color: white; font-weight: bold; font-size: 13px; background-color: #44ff44; padding: 2px 4px; border-radius: 3px;';
                } else if (this._remainingSeconds > 5) {
                    // Yellow background, white text (10-6)
                    this._stopFlashing();
                    this._countdownLabel.style = 'color: white; font-weight: bold; font-size: 13px; background-color: #ffaa00; padding: 2px 4px; border-radius: 3px;';
                } else {
                    // Red - start flashing (5-0)
                    this._startFlashing();
                }
            }

            this._log(`Countdown: ${this._remainingSeconds}s remaining`);

            // Auto-stop at 0 - trigger recording stop
            if (this._remainingSeconds <= 0) {
                this._log('Countdown reached 0 - stopping recording');
                this._recordingTimer = null;
                this._stopRecording();
                return GLib.SOURCE_REMOVE;
            }

            return GLib.SOURCE_CONTINUE;
        });
    }

    _startFlashing() {
        // Already flashing
        if (this._flashTimer) {
            return;
        }

        // Start flash timer (500ms interval)
        this._flashState = false;
        this._flashTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 500, () => {
            if (!this._countdownLabel) {
                this._flashTimer = null;
                return GLib.SOURCE_REMOVE;
            }

            // Toggle flash state
            this._flashState = !this._flashState;

            if (this._flashState) {
                // Red background, white text
                this._countdownLabel.style = 'color: white; font-weight: bold; font-size: 13px; background-color: #ff4444; padding: 2px 4px; border-radius: 3px;';
            } else {
                // White background, red text
                this._countdownLabel.style = 'color: #ff4444; font-weight: bold; font-size: 13px; background-color: white; padding: 2px 4px; border-radius: 3px;';
            }

            return GLib.SOURCE_CONTINUE;
        });
    }

    _stopFlashing() {
        if (this._flashTimer) {
            GLib.Source.remove(this._flashTimer);
            this._flashTimer = null;
            this._flashState = false;
        }
    }

    _stopCountdown() {
        // Cancel timers
        if (this._recordingTimer) {
            GLib.Source.remove(this._recordingTimer);
            this._recordingTimer = null;
            this._log('Countdown stopped');
        }

        // Stop flashing
        this._stopFlashing();

        // Early exit if indicator already destroyed (extension disabled)
        if (!this._indicator) {
            return;
        }

        // Restore icon
        if (this._countdownLabel) {
            this._indicator.remove_child(this._countdownLabel);
            this._countdownLabel = null;
        }

        if (!this._icon) {
            this._icon = new St.Icon({
                icon_name: 'audio-input-microphone-symbolic',
                style_class: 'system-status-icon'
            });
        }

        // Only add icon if it's not already a child
        const children = this._indicator.get_children();
        if (!children.includes(this._icon)) {
            this._indicator.add_child(this._icon);
        }
    }

    _launchWSI() {
        try {
            // If currently recording, stop it instead of starting new
            if (this._currentState === 'RECORDING') {
                this._stopRecording();
                return;
            }

            // Pass debug, clipboard, auto-paste, and wrap-marker flags if enabled
            // Note: auto-enter is ON by default in auto-paste mode, so we pass --no-auto-enter to disable
            const debugFlag = this._debugEnabled ? ' --debug' : '';
            const clipboardFlag = this._clipboardMode ? ' --clipboard' : '';
            const autoPasteFlag = this._autoPaste ? ' --auto-paste' : '';
            const noAutoEnterFlag = (this._autoPaste && !this._autoEnter) ? ' --no-auto-enter' : '';
            const wrapMarkerFlag = this._wrapWithMarker ? ' --wrap-marker' : '';

            // Use wsi-stream for streaming mode, otherwise use batch wsi
            const script = this._streamingMode ? 'wsi-stream' : 'wsi';
            const langFlag = ` --language ${this._getWhisperLanguage()}`;
            const command = GLib.get_home_dir() + '/.local/bin/' + script + debugFlag + clipboardFlag + autoPasteFlag + noAutoEnterFlag + wrapMarkerFlag + langFlag;

            this._log(`Launching: ${command}`);
            GLib.spawn_command_line_async(command);
        } catch (e) {
            this._lastError = e.message;
            this._log(`Launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _stopRecording() {
        const pidFile = '/dev/shm/stt-recording-' + GLib.get_user_name() + '.pid';
        this._log(`Stopping recording via PID file: ${pidFile}`);

        try {
            const file = Gio.File.new_for_path(pidFile);
            if (file.query_exists(null)) {
                const [success, contents] = file.load_contents(null);
                if (success) {
                    const pid = new TextDecoder().decode(contents).trim();
                    this._log(`Killing PID: ${pid}`);
                    GLib.spawn_command_line_async(`kill ${pid}`);
                }
            } else {
                this._log('PID file not found, trying pkill fallback');
                GLib.spawn_command_line_async('pkill -f "pw-record.*wfile"');
            }
        } catch (e) {
            this._log(`Stop error: ${e.message}`);
            // Fallback to pkill
            GLib.spawn_command_line_async('pkill -f "pw-record.*wfile"');
        }
    }

    _ensureLogDirectory() {
        const dir = Gio.File.new_for_path(this._logDir);
        if (!dir.query_exists(null)) {
            try {
                dir.make_directory_with_parents(null);
            } catch (e) {
                // Silent fail - will be created by wsi script
            }
        }
    }

    _loadDebugSetting() {
        if (this._settings) {
            try {
                this._debugEnabled = this._settings.get_boolean('debug-mode');
                if (this._debugSwitch) {
                    this._debugSwitch.setToggleState(this._debugEnabled);
                }
            } catch (e) {
                this._debugEnabled = false;
            }
            try {
                this._clipboardMode = this._settings.get_boolean('clipboard-mode');
                if (this._clipboardSwitch) {
                    this._clipboardSwitch.setToggleState(this._clipboardMode);
                }
            } catch (e) {
                this._clipboardMode = false;
            }
            try {
                this._autoPaste = this._settings.get_boolean('auto-paste');
                if (this._autoPasteSwitch) {
                    this._autoPasteSwitch.setToggleState(this._autoPaste);
                }
            } catch (e) {
                this._autoPaste = false;
            }
            try {
                this._wrapWithMarker = this._settings.get_boolean('wrap-marker');
                if (this._wrapMarkerSwitch) {
                    this._wrapMarkerSwitch.setToggleState(this._wrapWithMarker);
                }
            } catch (e) {
                this._wrapWithMarker = false;
            }
            try {
                this._autoEnter = this._settings.get_boolean('auto-enter');
                if (this._autoEnterSwitch) {
                    this._autoEnterSwitch.setToggleState(this._autoEnter);
                }
            } catch (e) {
                this._autoEnter = true;  // Default: send Enter after auto-paste
            }
            try {
                this._streamingMode = this._settings.get_boolean('streaming-mode');
                if (this._streamingSwitch) {
                    this._streamingSwitch.setToggleState(this._streamingMode);
                }
            } catch (e) {
                this._streamingMode = false;
            }
            try {
                this._language = this._settings.get_string('language');
            } catch (e) {
                this._language = 'system';
            }
        }
    }

    _updateLanguageLabel() {
        if (this._languageSubMenu) {
            let label;
            if (this._language === 'system') {
                // Get system locale and show it
                const locale = GLib.getenv('LANG') || 'en_GB.UTF-8';
                const langCode = locale.split('_')[0];  // en_GB.UTF-8 -> en
                label = `System (${locale.split('.')[0]} → ${langCode})`;
            } else {
                const labels = { 'en': 'English' };
                label = labels[this._language] || this._language;
            }
            this._languageSubMenu.label.text = `Language: ${label}`;
        }
    }

    _updatePasteToggles() {
        // Prevent cascading toggle events from causing menu issues
        this._updatingToggles = true;
        try {
            if (this._clipboardSwitch) {
                this._clipboardSwitch.setToggleState(this._clipboardMode);
            }
            if (this._autoPasteSwitch) {
                this._autoPasteSwitch.setToggleState(this._autoPaste);
            }
            // Auto-enter and streaming only visible when auto-paste is active
            if (this._autoEnterSwitch) {
                this._autoEnterSwitch.visible = this._autoPaste;
            }
            if (this._streamingSwitch) {
                this._streamingSwitch.visible = this._autoPaste;
                this._streamingSwitch.setToggleState(this._streamingMode);
            }
        } finally {
            this._updatingToggles = false;
        }
    }

    _saveDebugSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('debug-mode', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveClipboardSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('clipboard-mode', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveAutoPasteSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('auto-paste', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveWrapMarkerSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('wrap-marker', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveAutoEnterSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('auto-enter', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveStreamingSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('streaming-mode', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveLanguageSetting(lang) {
        if (this._settings) {
            try {
                this._settings.set_string('language', lang);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _getWhisperLanguage() {
        // Convert extension language setting to Whisper language code
        if (this._language === 'system') {
            const locale = GLib.getenv('LANG') || 'en_GB.UTF-8';
            return locale.split('_')[0];  // en_GB.UTF-8 -> en
        }
        return this._language;
    }

    _log(message) {
        if (!this._debugEnabled) return;

        const timestamp = new Date().toISOString();
        const logLine = `[${timestamp}] [EXT] ${message}\n`;

        try {
            const file = Gio.File.new_for_path(this._logFile);

            // Log rotation: if file > 1MB, rename to .old
            if (file.query_exists(null)) {
                const info = file.query_info('standard::size', Gio.FileQueryInfoFlags.NONE, null);
                if (info.get_size() > 1048576) {
                    const oldFile = Gio.File.new_for_path(this._logFile + '.old');
                    file.move(oldFile, Gio.FileCopyFlags.OVERWRITE, null, null);
                }
            }

            const stream = file.append_to(Gio.FileCreateFlags.NONE, null);
            stream.write_all(logLine, null);
            stream.close(null);
        } catch (e) {
            // Silent fail for logging
        }
    }

    _readRecentLogs(numLines = 10) {
        try {
            const file = Gio.File.new_for_path(this._logFile);
            if (!file.query_exists(null)) {
                return ['(no logs yet)'];
            }

            const [success, contents] = file.load_contents(null);
            if (!success) return ['(cannot read log)'];

            const text = new TextDecoder().decode(contents);
            const lines = text.trim().split('\n');
            return lines.slice(-numLines);
        } catch (e) {
            return [`(error: ${e.message})`];
        }
    }

    _refreshLogDisplay() {
        const recentLogs = this._readRecentLogs(5);
        for (let i = 0; i < 5; i++) {
            if (i < recentLogs.length) {
                // Truncate long lines for display
                let line = recentLogs[i];
                if (line.length > 50) {
                    line = line.substring(0, 47) + '...';
                }
                this._logLines[i].label.text = line;
            } else {
                this._logLines[i].label.text = '';
            }
        }
    }

    _updateStatusLabel() {
        let statusText = `Status: ${this._currentState}`;
        if (this._lastError) {
            statusText += ` - ${this._lastError.substring(0, 30)}`;
        }
        this._statusLabel.label.text = statusText;

        // Color-code based on state
        switch (this._currentState) {
            case 'RECORDING':
                this._statusLabel.label.style = 'font-weight: bold; color: #ff4444;';
                break;
            case 'TRANSCRIBING':
                this._statusLabel.label.style = 'font-weight: bold; color: #ffaa00;';
                break;
            case 'SUCCESS':
                this._statusLabel.label.style = 'font-weight: bold; color: #44ff44;';
                break;
            case 'ERROR':
                this._statusLabel.label.style = 'font-weight: bold; color: #ff4444;';
                break;
            default:
                this._statusLabel.label.style = 'font-weight: bold;';
        }
    }

    _openLogViewer() {
        // Open log file with default text editor
        try {
            Gio.AppInfo.launch_default_for_uri(
                'file://' + this._logFile,
                null
            );
        } catch (e) {
            // Fallback: open with gnome-text-editor
            try {
                GLib.spawn_command_line_async(`gnome-text-editor ${this._logFile}`);
            } catch (e2) {
                Main.notify('STT', `Cannot open log file: ${e2.message}`);
            }
        }
    }

    _copyLastTranscription() {
        const transcriptionFile = GLib.get_home_dir() + '/.cache/speech-to-text/last-transcription.txt';

        // Check if file exists
        const file = Gio.File.new_for_path(transcriptionFile);
        if (!file.query_exists(null)) {
            Main.notify('Speech to Text', 'No transcription available yet');
            return;
        }

        // Read transcription text
        try {
            const [success, contents] = file.load_contents(null);
            if (!success) {
                Main.notify('STT', 'Cannot read transcription file');
                return;
            }

            const text = new TextDecoder().decode(contents).trim();
            if (!text) {
                Main.notify('Speech to Text', 'Transcription is empty');
                return;
            }

            // Copy to clipboard using subprocess (no shell injection risk)
            try {
                const proc = Gio.Subprocess.new(
                    ['wl-copy'],
                    Gio.SubprocessFlags.STDIN_PIPE
                );
                proc.communicate_utf8_async(text, null, null);
            } catch (procError) {
                this._log(`wl-copy spawn error: ${procError.message}`);
                throw procError;
            }

            // Show preview in notification
            const preview = text.length > 60 ? text.substring(0, 57) + '...' : text;
            Main.notify('Speech to Text', `Copied: ${preview}`);

            this._log(`Copied transcription to clipboard: ${text}`);
        } catch (e) {
            Main.notify('STT', `Cannot copy transcription: ${e.message}`);
            this._log(`Copy error: ${e.message}`);
        }
    }

    _openLastTranscription() {
        const transcriptionFile = GLib.get_home_dir() + '/.cache/speech-to-text/last-transcription.txt';

        // Check if file exists
        const file = Gio.File.new_for_path(transcriptionFile);
        if (!file.query_exists(null)) {
            Main.notify('Speech to Text', 'No transcription available yet');
            return;
        }

        // Open transcription file with default text editor
        try {
            Gio.AppInfo.launch_default_for_uri(
                'file://' + transcriptionFile,
                null
            );
        } catch (e) {
            // Fallback: open with gnome-text-editor
            try {
                GLib.spawn_command_line_async(`gnome-text-editor ${transcriptionFile}`);
            } catch (e2) {
                Main.notify('STT', `Cannot open transcription: ${e2.message}`);
            }
        }
    }

    _clearLog() {
        try {
            const file = Gio.File.new_for_path(this._logFile);
            if (file.query_exists(null)) {
                file.delete(null);
            }
            this._log('Log cleared');
            this._refreshLogDisplay();
        } catch (e) {
            Main.notify('STT', `Cannot clear log: ${e.message}`);
        }
    }
}
