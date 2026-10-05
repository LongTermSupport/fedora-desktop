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
import Pango from 'gi://Pango';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';
import { FocusOutline } from './focusOutline.js';
import { PasteTargetPin } from './pasteTarget.js';

// DBus interface for wsi communication
const DBUS_PATH = '/org/fedoradesktop/SpeechToText';
const DBUS_INTERFACE = 'org.fedoradesktop.SpeechToText';

// The panel's own object, which the recorders call (on org.gnome.Shell) before each
// paste. focused false: the pinned window was given focus back, ask again; gone true:
// it was closed, paste nothing.
const PANEL_DBUS_PATH = '/org/fedoradesktop/SpeechToText/Panel';
const PANEL_DBUS_XML = `
<node>
  <interface name="org.fedoradesktop.SpeechToText.Panel">
    <method name="PasteKey">
      <arg type="s" direction="out" name="wm_class"/>
      <arg type="b" direction="out" name="with_shift"/>
      <arg type="b" direction="out" name="save_after"/>
      <arg type="b" direction="out" name="focused"/>
      <arg type="b" direction="out" name="gone"/>
    </method>
  </interface>
</node>`;

// Continuous dictation: count down only in the last minute of its maximum length
const COUNTDOWN_LAST_SECONDS = 60;

export default class SpeechToTextExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._indicator = null;
        this._settings = null;
        this._dbusSubscriptionId = null;
        this._dbusErrorSubscriptionId = null;

        // Debug mode state
        this._debugEnabled = false;
        this._autoPaste = false;
        this._autoEnter = true;  // Default: send Enter after auto-paste
        this._wrapWithMarker = false;
        this._streamingMode = false;  // Use RealtimeSTT streaming instead of batch
        this._streamingStartupMode = 'standard';  // 'standard', 'pre-buffer', or 'server'
        this._language = 'system';    // 'system' = detect from locale, or 'en', etc.
        this._showNotifications = false;  // Show desktop notifications (off by default)
        this._whisperModel = 'auto';  // Whisper model: 'auto', 'tiny', 'base', 'small', 'medium', 'large-v3'
        this._claudeEnabled = false;  // Enable Claude Code post-processing
        this._claudeModel = 'sonnet';  // Claude model: 'sonnet', 'opus', 'haiku'
        this._currentState = 'IDLE';
        this._launchPending = false;  // Debounce: a recorder has been spawned but has not yet reported via DBus
        this._launchPendingTimeoutId = null;
        this._updatingToggles = false;  // Guard flag to prevent toggle cascade
        this._settingsChangedId = null;
        this._lastError = null;
        this._logDir = GLib.get_home_dir() + '/.local/share/speech-to-text';
        this._logFile = this._logDir + '/debug.log';
        this._iconResetTimeoutId = null;

        // Server status indicator
        this._serverStatusDot = null;
        this._serverPollTimer = null;
        this._serverIsHot = false;

        // Recording timer state
        this._recordingTimer = null;
        this._recordingSeconds = 0;  // Elapsed recording time shown on the panel label
        // Continuous dictation's maximum length, read from GSettings when a recording
        // starts; 0 means no countdown (the other modes' recorders enforce their own caps)
        this._recordingLimitSeconds = 0;
        this._dictationPending = 0;  // Segments waiting to be transcribed (Progress signal)
        this._dictationBehind = false;  // Transcription slower than speech (Progress signal)
        this._dbusProgressSubscriptionId = null;
        this._countdownLabel = null;
        this._flashTimer = null;
        this._flashState = false;
        this._isClaudeMode = false;  // Track if recording is in Claude mode
        this._claudeStyle = null;  // Track Claude style: 'corporate' or 'natural'
        this._isArticleMode = false;  // Track if article mode window is open
        this._elapsedSeconds = 0;  // Elapsed seconds shown in article mode indicator

        // Whisper model definitions (name, label, size, description, englishOnly)
        this._whisperModels = [
            ['auto', 'Auto (optimized per mode)', 'varies', 'GPU: Distil Large v3.5 (English) or Large v3 Turbo; CPU: base streaming, small batch', false],
            // Multilingual models
            ['tiny', 'Tiny', '~75MB', 'Fastest, basic accuracy', false],
            ['base', 'Base', '~142MB', 'Fast, good accuracy', false],
            ['small', 'Small', '~466MB', 'Balanced speed/accuracy', false],
            ['medium', 'Medium', '~1.5GB', 'Slow, great accuracy', false],
            ['large-v2', 'Large v2', '~3GB', 'Very high accuracy', false],
            ['large-v3', 'Large v3', '~3GB', 'Best quality', false],
            ['large-v3-turbo', 'Large v3 Turbo', '~1.6GB', 'Distilled, fast + accurate', false],
            // English-only models (smaller and faster — no multilingual capability)
            ['tiny.en', 'Tiny English', '~41MB', 'Fastest, English only', true],
            ['base.en', 'Base English', '~77MB', 'Fast, good accuracy, English only', true],
            ['small.en', 'Small English', '~252MB', 'Balanced, English only', true],
            ['medium.en', 'Medium English', '~789MB', 'Great accuracy, English only', true],
            ['distil-large-v3.5', 'Distil Large v3.5', '~1.5GB', 'Fewer errors than Turbo, faster, English only', true],
        ];

        // Claude model definitions (name, label, description)
        this._claudeModels = [
            ['sonnet', 'Sonnet', 'Balanced speed and quality'],
            ['opus', 'Opus', 'Best quality, slower'],
            ['haiku', 'Haiku', 'Fastest, lower quality'],
        ];
    }

    enable() {
        // Create panel indicator with menu
        this._indicator = new PanelMenu.Button(0.0, 'Speech to Text', false);

        // Create a box to hold icon and status dot
        this._iconBox = new St.BoxLayout({
            style_class: 'panel-status-indicators-box'
        });

        // Add icon
        this._icon = new St.Icon({
            icon_name: 'audio-input-microphone-symbolic',
            style_class: 'system-status-icon'
        });
        this._iconBox.add_child(this._icon);

        // Add server status indicator dot (initially hidden)
        this._serverStatusDot = new St.Icon({
            icon_name: 'media-record-symbolic',
            style_class: 'system-status-icon',
            style: 'color: #00d4ff; font-size: 8px;',  // Removed negative margin
            visible: false
        });
        this._iconBox.add_child(this._serverStatusDot);

        // Add the box to the indicator
        this._indicator.add_child(this._iconBox);

        // Setup keybinding and load settings BEFORE building menu
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

                // Load ALL settings FIRST (before building menu)
                this._loadDebugSetting();

                this._log('Attempting to add keybinding...');
                const bindingAdded = Main.wm.addKeybinding(
                    'toggle-recording',
                    this._settings,
                    Meta.KeyBindingFlags.NONE,
                    Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                    () => {
                        this._log('INSERT KEY PRESSED - callback fired!');
                        this._launchWSI();
                    }
                );
                this._log(`Keybinding registration result: ${bindingAdded}`);

                // Add Ctrl+Insert keybinding for Claude processing (corporate style)
                const claudeBindingAdded = Main.wm.addKeybinding(
                    'toggle-recording-claude',
                    this._settings,
                    Meta.KeyBindingFlags.NONE,
                    Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                    () => {
                        this._log('CTRL+INSERT PRESSED - Claude processing (corporate)!');
                        this._launchWSIClaude('corporate');
                    }
                );
                this._log(`Claude keybinding registration result: ${claudeBindingAdded}`);

                // Add Alt+Insert keybinding for Claude processing (natural style)
                const claudeNaturalBindingAdded = Main.wm.addKeybinding(
                    'toggle-recording-claude-natural',
                    this._settings,
                    Meta.KeyBindingFlags.NONE,
                    Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                    () => {
                        this._log('ALT+INSERT PRESSED - Claude processing (natural)!');
                        this._launchWSIClaude('natural');
                    }
                );
                this._log(`Claude natural keybinding registration result: ${claudeNaturalBindingAdded}`);

                // Note: abort-recording keybinding is added dynamically when recording starts
            } else {
                Main.notify('STT Error', 'Schema lookup failed');
            }
        } catch (e) {
            Main.notify('STT Error', `Keybinding setup failed: ${e.message}`);
        }

        // Now build the menu AFTER settings are loaded
        this._buildMenu();

        // Sync internal state when prefs window changes a setting
        this._connectSettingsSignals();

        // Add to panel
        Main.panel.addToStatusArea('speech-to-text', this._indicator);

        // Subscribe to DBus signals from wsi script, and answer its paste questions
        this._subscribeToDBus();
        this._exportPanelDBus();
        this._focusOutline = new FocusOutline();
        this._pasteTarget = new PasteTargetPin();

        // Ensure log directory exists
        this._ensureLogDirectory();

        // Start server status polling
        this._startServerStatusPolling();

        Main.notify('Speech to Text', 'Ready. Press Insert to record. Click icon for options.');
    }

    disable() {
        this._unexportPanelDBus();
        if (this._focusOutline) {
            this._focusOutline.hide();
            this._focusOutline = null;
        }
        if (this._pasteTarget) {
            this._pasteTarget.release();
            this._pasteTarget = null;
        }

        // Unsubscribe from DBus
        if (this._dbusSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusSubscriptionId);
            this._dbusSubscriptionId = null;
        }
        if (this._dbusErrorSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusErrorSubscriptionId);
            this._dbusErrorSubscriptionId = null;
        }
        if (this._dbusProgressSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusProgressSubscriptionId);
            this._dbusProgressSubscriptionId = null;
        }

        // Remove keybindings
        try {
            Main.wm.removeKeybinding('toggle-recording');
            Main.wm.removeKeybinding('toggle-recording-claude');
            Main.wm.removeKeybinding('toggle-recording-claude-natural');
            this._removeAbortKeybinding();
        } catch (e) {
            // A keybinding may not have been registered; surface it rather than swallow it.
            logError(e, 'STT: removing keybindings in disable()');
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

        // Cancel pending launch-debounce timeout
        this._clearLaunchDebounce();

        // Stop server status polling
        this._stopServerStatusPolling();

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        // Disconnect settings change listener
        if (this._settingsChangedId && this._settings) {
            this._settings.disconnect(this._settingsChangedId);
            this._settingsChangedId = null;
        }

        this._settings = null;
    }

    _addAbortKeybinding() {
        // Add Escape key binding to abort recording
        if (!this._settings) return;

        try {
            Main.wm.addKeybinding(
                'abort-recording',
                this._settings,
                Meta.KeyBindingFlags.NONE,
                Shell.ActionMode.NORMAL | Shell.ActionMode.OVERVIEW,
                () => {
                    this._abortRecording();
                }
            );
            this._log('Abort keybinding added (Escape active)');
        } catch (e) {
            this._log(`Failed to add abort keybinding: ${e.message}`);
        }
    }

    _removeAbortKeybinding() {
        // Remove Escape key binding when not recording
        try {
            Main.wm.removeKeybinding('abort-recording');
            this._log('Abort keybinding removed (Escape restored)');
        } catch (e) {
            // Keybinding doesn't exist, ignore
        }
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

        // Status
        this._statusLabel = new PopupMenu.PopupMenuItem('Status: IDLE', { reactive: false });
        this._statusLabel.label.style = 'font-weight: bold;';
        menu.addMenuItem(this._statusLabel);

        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Auto-paste toggle — most-used setting, kept in popup for quick access
        this._autoPasteSwitch = new PopupMenu.PopupSwitchMenuItem('Auto-paste at cursor', this._autoPaste);
        this._preventMenuClose(this._autoPasteSwitch);
        this._autoPasteSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;
            this._autoPaste = state;
            this._saveAutoPasteSetting(state);
            this._log(`Auto-paste ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._autoPasteSwitch);

        // Debug toggle — kept in popup so it's reachable when diagnosing issues
        this._debugSwitch = new PopupMenu.PopupSwitchMenuItem('Debug Logging', this._debugEnabled);
        this._preventMenuClose(this._debugSwitch);
        this._debugSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;
            this._debugEnabled = state;
            this._saveDebugSetting(state);
            this._log(`Debug mode ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._debugSwitch);

        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        const copyItem = new PopupMenu.PopupMenuItem('Copy Last Transcription');
        copyItem.connect('activate', () => { this._copyLastTranscription(); });
        menu.addMenuItem(copyItem);

        const viewLogItem = new PopupMenu.PopupMenuItem('View Debug Log...');
        viewLogItem.connect('activate', () => { this._openLogViewer(); });
        menu.addMenuItem(viewLogItem);

        const articleItem = new PopupMenu.PopupMenuItem('Create Article...');
        articleItem.connect('activate', () => { this._launchArticleMode(); });
        menu.addMenuItem(articleItem);

        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        const manageModelsItem = new PopupMenu.PopupMenuItem('Manage Whisper Models...');
        manageModelsItem.connect('activate', () => { this._openModelManager(); });
        menu.addMenuItem(manageModelsItem);

        const serverManagerItem = new PopupMenu.PopupMenuItem('Server Manager...');
        serverManagerItem.connect('activate', () => { this._openServerManager(); });
        menu.addMenuItem(serverManagerItem);

        const settingsItem = new PopupMenu.PopupMenuItem('Settings...');
        settingsItem.connect('activate', () => { this.openPreferences(); });
        menu.addMenuItem(settingsItem);

        // Update status line each time the menu opens
        menu.connect('open-state-changed', (m, open) => {
            if (open) this._updateStatusLabel();
        });
    }

    _connectSettingsSignals() {
        if (!this._settings) return;

        this._settingsChangedId = this._settings.connect('changed', (_settings, key) => {
            this._updatingToggles = true;
            try {
                switch (key) {
                case 'debug-mode':
                    this._debugEnabled = this._settings.get_boolean(key);
                    if (this._debugSwitch) this._debugSwitch.setToggleState(this._debugEnabled);
                    break;
                case 'auto-paste':
                    this._autoPaste = this._settings.get_boolean(key);
                    if (this._autoPasteSwitch) this._autoPasteSwitch.setToggleState(this._autoPaste);
                    break;
                case 'auto-enter':
                    this._autoEnter = this._settings.get_boolean(key);
                    break;
                case 'wrap-marker':
                    this._wrapWithMarker = this._settings.get_boolean(key);
                    break;
                case 'streaming-mode':
                    this._streamingMode = this._settings.get_boolean(key);
                    this._checkServerStatus();
                    break;
                case 'streaming-startup-mode':
                    this._streamingStartupMode = this._settings.get_string(key);
                    this._checkServerStatus();
                    break;
                case 'language':
                    this._language = this._settings.get_string(key);
                    break;
                case 'whisper-model':
                    this._whisperModel = this._settings.get_string(key);
                    break;
                case 'claude-enabled':
                    this._claudeEnabled = this._settings.get_boolean(key);
                    break;
                case 'claude-model':
                    this._claudeModel = this._settings.get_string(key);
                    break;
                case 'show-notifications':
                    this._showNotifications = this._settings.get_boolean(key);
                    break;
                }
            } finally {
                this._updatingToggles = false;
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
                this._clearLaunchDebounce();  // the spawned recorder has now reported
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

        // Continuous dictation's progress from wsi-stream:
        // "elapsed=<s> pending=<segments> backlog=<s> behind=<0|1>"
        this._dbusProgressSubscriptionId = Gio.DBus.session.signal_subscribe(
            null,
            DBUS_INTERFACE,
            'Progress',
            DBUS_PATH,
            null,
            Gio.DBusSignalFlags.NONE,
            (connection, sender, path, iface, signal, params) => {
                const fields = {};
                for (const pair of params.get_child_value(0).get_string()[0].split(' ')) {
                    const [name, value] = pair.split('=');
                    fields[name] = Number(value);
                }
                if (Number.isFinite(fields.elapsed))
                    this._recordingSeconds = fields.elapsed;
                this._dictationPending = Number.isFinite(fields.pending) ? fields.pending : 0;
                this._dictationBehind = fields.behind === 1;
                this._refreshRecordingLabel();
            }
        );
    }

    _checkServerStatus() {
        // Safety check - don't run if objects aren't initialized
        if (!this._serverStatusDot || !this._indicator) {
            return;
        }

        // Only check if server mode is enabled and streaming mode is active
        if (this._streamingStartupMode !== 'server' || !this._streamingMode) {
            // Not in server mode - always hide dot and mark server as cold
            if (this._serverStatusDot.visible) {
                this._serverStatusDot.visible = false;
                this._serverIsHot = false;
                this._log('Server status: Not in server mode, hiding dot');
            }
            return;
        }

        // In server mode - check if server is actually running
        const runtimeDir = GLib.get_user_runtime_dir();
        if (!runtimeDir) {
            this._log('Cannot get runtime directory', 'WARN');
            return;
        }

        const socketPath = `${runtimeDir}/wsi-stream.socket`;

        // Check if PID file and socket both exist (non-blocking file stat)
        const pidFilePath = `${runtimeDir}/wsi-stream-server.pid`;
        let serverIsRunning = false;
        try {
            const pidFile = Gio.File.new_for_path(pidFilePath);
            if (pidFile.query_exists(null)) {
                // PID file exists - check if socket also exists (both needed)
                const socketFile = Gio.File.new_for_path(socketPath);
                serverIsRunning = socketFile.query_exists(null);
            }
        } catch (e) {
            this._log(`Server status check failed: ${e.message}`, 'WARN');
            serverIsRunning = false;
        }

        // Update dot visibility
        if (serverIsRunning !== this._serverIsHot) {
            this._serverIsHot = serverIsRunning;
            if (this._serverStatusDot) {
                this._serverStatusDot.visible = serverIsRunning;
            }

            this._log(`Server status changed: ${serverIsRunning ? 'hot' : 'cold'}`);
        }
    }

    _startServerStatusPolling() {
        // Poll server status every 5 seconds
        if (this._serverPollTimer) {
            return; // Already polling
        }

        // Initial check (wrapped in try-catch)
        try {
            this._checkServerStatus();
        } catch (e) {
            this._log(`Initial server check failed: ${e.message}`, 'ERROR');
        }

        // Start periodic polling
        this._serverPollTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 5, () => {
            try {
                this._checkServerStatus();
            } catch (e) {
                this._log(`Server polling error: ${e.message}`, 'ERROR');
            }
            return GLib.SOURCE_CONTINUE;
        });

        this._log('Server status polling started');
    }

    _stopServerStatusPolling() {
        if (this._serverPollTimer) {
            GLib.Source.remove(this._serverPollTimer);
            this._serverPollTimer = null;
            this._log('Server status polling stopped');
        }

        // Hide dot
        if (this._serverStatusDot) {
            this._serverStatusDot.visible = false;
        }
        this._serverIsHot = false;
    }

    _updateIconState(state) {
        if (!this._icon) return;

        // Cancel any pending icon reset
        if (this._iconResetTimeoutId) {
            GLib.Source.remove(this._iconResetTimeoutId);
            this._iconResetTimeoutId = null;
        }

        // Outline the window the text will be pasted into, for as long as a paste can come;
        // the pin taken at Insert lasts until the dictation has ended
        const pasteCanCome = ['RECORDING', 'STOPPING', 'TRANSCRIBING'].includes(state);
        if (pasteCanCome && this._autoPaste && !this._isArticleMode)
            this._focusOutline?.show(this._pasteTarget?.window ?? null);
        else
            this._focusOutline?.hide();
        if (['IDLE', 'SUCCESS', 'ERROR'].includes(state))
            this._pasteTarget?.release();

        switch (state) {
            case 'PREPARING':
                if (!this._isArticleMode) {
                    // Orange icon while audio pipeline initializes
                    this._icon.style = 'color: #ffaa00;';  // Orange/Yellow
                }
                break;
            case 'RECORDING':
                if (!this._isArticleMode) {
                    this._icon.style = 'color: #ff4444;';  // Red
                    this._startCountdown();
                }
                this._addAbortKeybinding();  // Enable Escape key during recording
                break;
            case 'STOPPING':
                // Insert was pressed: the microphone stays open for the stop grace, but
                // the icon already shows the press was taken. Escape still discards and
                // Insert again stops at once.
                this._stopCountdown();
                this._icon.icon_name = 'content-loading-symbolic';
                this._icon.style = 'color: #ffaa00;';  // Orange/Yellow
                break;
            case 'TRANSCRIBING':
                this._stopCountdown();
                this._removeAbortKeybinding();  // Disable Escape key when transcribing
                // Change icon to spinner/hourglass for transcribing
                this._icon.icon_name = 'content-loading-symbolic';
                this._icon.style = 'color: #ffaa00;';  // Orange/Yellow
                break;
            case 'SUCCESS':
                this._stopCountdown();
                this._removeAbortKeybinding();  // Disable Escape key when done
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = 'color: #44ff44;';  // Green
                // Reset to normal after 2 seconds
                this._iconResetTimeoutId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 2000, () => {
                    if (this._icon) this._icon.style = '';
                    this._iconResetTimeoutId = null;
                    return GLib.SOURCE_REMOVE;
                });
                // When auto-paste is OFF the transcript only lands in the PRIMARY
                // selection — there is no visible feedback that it's ready. Show
                // a one-shot notification so the user knows to middle-click.
                // Auto-paste ON is self-evident (the text appears at the cursor).
                if (!this._autoPaste) {
                    Main.notify('Speech to Text', 'Transcribed — middle-click to paste');
                }
                break;
            case 'ERROR':
                this._stopCountdown();
                this._removeAbortKeybinding();  // Disable Escape key on error
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
                this._removeAbortKeybinding();  // Disable Escape key when idle
                this._isArticleMode = false;
                this._elapsedSeconds = 0;
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = '';  // Default
                break;
        }
    }

    /** m:ss */
    _formatSeconds(seconds) {
        const whole = Math.max(0, Math.floor(seconds));
        return `${Math.floor(whole / 60)}:${String(whole % 60).padStart(2, '0')}`;
    }

    /** Seconds left of continuous dictation's maximum length, or null with no countdown. */
    _secondsLeft() {
        if (this._recordingLimitSeconds <= 0)
            return null;
        const left = this._recordingLimitSeconds - this._recordingSeconds;
        return left <= COUNTDOWN_LAST_SECONDS ? Math.max(0, left) : null;
    }

    _countdownText() {
        // Elapsed time, or the time left in the last minute of continuous dictation's
        // maximum length, plus "·N" while N segments wait to be transcribed. The coloured
        // badge already says "recording", and panel space is tight. Claude modes keep an
        // emoji so the style is still visible.
        const left = this._secondsLeft();
        let text = this._formatSeconds(left === null ? this._recordingSeconds : left);
        if (this._dictationPending > 0)
            text += ` ·${this._dictationPending}`;
        if (this._isClaudeMode) {
            const emoji = this._claudeStyle === 'natural' ? '💬' : '🤖';
            return `${emoji} ${text}`;
        }
        return text;
    }

    _refreshRecordingLabel() {
        if (!this._countdownLabel || this._isArticleMode)
            return;
        this._countdownLabel.text = this._countdownText();
        const left = this._secondsLeft();
        if (left !== null && left <= 5) {
            this._startFlashing();  // Red: the maximum length is about to stop it
            return;
        }
        this._stopFlashing();
        // Amber: transcription is falling behind, or the last 10 s of the maximum length
        const amber = this._dictationBehind || (left !== null && left <= 10);
        this._countdownLabel.style = `color: white; font-weight: bold; font-size: 13px; background-color: ${amber ? '#ffaa00' : '#44ff44'}; padding: 2px 4px; border-radius: 3px;`;
    }

    /** Continuous dictation: streaming, server startup mode, and the setting on. */
    _continuousDictation() {
        return this._streamingMode && this._streamingStartupMode === 'server' &&
            this._settings.get_boolean('continuous-dictation');
    }

    _startCountdown() {
        // Stop any existing timer
        this._stopCountdown();

        // The recorders stop themselves at their limits; the panel only shows the time.
        // Continuous dictation's maximum length comes from the same GSettings key
        // wsi-stream reads, so the countdown in its last minute matches the server's stop.
        this._recordingSeconds = 0;
        this._dictationPending = 0;
        this._dictationBehind = false;
        this._recordingLimitSeconds = this._continuousDictation()
            ? this._settings.get_int('max-recording-minutes') * 60 : 0;

        // Replace iconBox (which contains icon + server status dot) with the label
        if (this._iconBox) {
            this._indicator.remove_child(this._iconBox);
        }

        // Note: Don't use 'system-status-icon' style_class - it has max-width that
        // truncates the label. Ellipsizing is disabled too: on a crowded panel the
        // label is squeezed, and "RE…" hides the one thing it exists to show.
        this._countdownLabel = new St.Label({
            text: this._countdownText(),
            y_align: 2,  // Clutter.ActorAlign.CENTER
        });
        this._countdownLabel.clutter_text.ellipsize = Pango.EllipsizeMode.NONE;
        this._indicator.add_child(this._countdownLabel);
        this._refreshRecordingLabel();

        this._log(`Recording timer started (${this._recordingLimitSeconds
            ? `continuous dictation, stops at ${this._recordingLimitSeconds}s`
            : 'elapsed time; the recorder enforces its own limit'})`);

        this._recordingTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            this._recordingSeconds++;
            this._refreshRecordingLabel();
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

        // Restore iconBox (which contains icon + server status dot)
        if (this._countdownLabel) {
            this._indicator.remove_child(this._countdownLabel);
            this._countdownLabel = null;
        }

        // Restore the iconBox if it's not already a child of indicator
        if (this._iconBox) {
            const children = this._indicator.get_children();
            if (!children.includes(this._iconBox)) {
                this._indicator.add_child(this._iconBox);
            }
        }
    }

    _validateShellArg(value, allowedValues) {
        // Only allow known-safe values to prevent shell injection
        if (allowedValues && !allowedValues.includes(value)) {
            this._log(`Invalid argument value: ${value}`);
            return allowedValues[0]; // Return safe default
        }
        // Additional safety: strip any shell metacharacters
        return value.replace(/[;&|`$(){}'"\\!#~<>]/g, '');
    }

    _beginLaunchDebounce() {
        // Mark a launch in flight. _currentState only updates when the spawned
        // recorder emits its first DBus StateChanged signal (hundreds of ms to
        // seconds away for the streaming path), so without this flag a second
        // Insert press in that window would spawn a second recorder that clobbers
        // the shared PID file. Cleared on the first DBus signal or by a safety
        // timeout (so a recorder that dies before signalling cannot wedge launch).
        this._launchPending = true;
        if (this._launchPendingTimeoutId) {
            GLib.Source.remove(this._launchPendingTimeoutId);
        }
        this._launchPendingTimeoutId = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 10000, () => {
            this._log('Launch debounce expired without a DBus state signal');
            this._launchPending = false;
            this._launchPendingTimeoutId = null;
            return GLib.SOURCE_REMOVE;
        });
    }

    _clearLaunchDebounce() {
        this._launchPending = false;
        if (this._launchPendingTimeoutId) {
            GLib.Source.remove(this._launchPendingTimeoutId);
            this._launchPendingTimeoutId = null;
        }
    }

    _launchWSI() {
        try {
            // Debounce: ignore a repeat press while a spawned recorder has not yet reported
            if (this._launchPending) {
                this._log('Launch already in progress; ignoring repeat press');
                return;
            }
            // If currently in any active state, stop it instead of starting new
            if (this._currentState === 'RECORDING' ||
                this._currentState === 'STOPPING' ||
                this._currentState === 'PREPARING' ||
                this._currentState === 'TRANSCRIBING') {
                this._stopRecording();
                return;
            }

            // Track that this is regular mode (not Claude)
            this._isClaudeMode = false;
            this._claudeStyle = null;
            this._pasteTarget.pin(global.display.focus_window);

            // Pass debug, auto-paste, and wrap-marker flags if enabled
            // Note: auto-enter is ON by default in auto-paste mode, so we pass --no-auto-enter to disable
            const debugFlag = this._debugEnabled ? ' --debug' : '';
            const autoPasteFlag = this._autoPaste ? ' --auto-paste' : '';
            const noAutoEnterFlag = (this._autoPaste && !this._autoEnter) ? ' --no-auto-enter' : '';
            const wrapMarkerFlag = this._wrapWithMarker ? ' --wrap-marker' : '';
            const noNotifyFlag = !this._showNotifications ? ' --no-notify' : '';
            const pasteWithShiftFlag = this._autoPaste ? ` --paste-with-shift ${this._getPasteWithShift()}` : '';

            // Use wsi-stream for streaming mode, otherwise use batch wsi
            const script = this._streamingMode ? 'wsi-stream' : 'wsi';
            // Strip shell metacharacters: language is a free-form GSettings string and
            // is interpolated into a double-quoted `bash -c` command in the non-auto path.
            const langFlag = ` --language ${this._validateShellArg(this._getWhisperLanguage())}`;

            // Add startup mode flags for streaming mode
            let startupModeFlag = '';
            if (this._streamingMode) {
                if (this._streamingStartupMode === 'pre-buffer') {
                    startupModeFlag = ' --pre-buffer';
                } else if (this._streamingStartupMode === 'server') {
                    startupModeFlag = ' --server-mode';
                }
                // 'standard' mode has no flag
            }

            const scriptPath = GLib.get_home_dir() + '/.local/bin/' + script;
            const scriptArgs = debugFlag + autoPasteFlag + noAutoEnterFlag + wrapMarkerFlag + noNotifyFlag + langFlag + startupModeFlag + pasteWithShiftFlag;

            // Build command - wrap in bash if we need to set environment variables
            let command;
            if (this._whisperModel !== 'auto') {
                // Validate model name against known values before shell interpolation
                const safeModel = this._validateShellArg(
                    this._whisperModel,
                    this._whisperModels.map(m => m[0])
                );
                // Need to set WHISPER_MODEL env var - use bash -c
                command = `/bin/bash -c "WHISPER_MODEL=${safeModel} ${scriptPath}${scriptArgs}"`;
            } else {
                // Auto mode uses script defaults - no env var needed
                command = scriptPath + scriptArgs;
            }

            this._log(`Launching: ${command}`);
            this._beginLaunchDebounce();
            GLib.spawn_command_line_async(command);
        } catch (e) {
            this._lastError = e.message;
            this._log(`Launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _launchWSIClaude(style = 'corporate') {
        try {
            // Debounce: ignore a repeat press while a spawned recorder has not yet reported
            if (this._launchPending) {
                this._log('Launch already in progress; ignoring repeat press');
                return;
            }
            // If currently in any active state, stop it instead of starting new
            if (this._currentState === 'RECORDING' ||
                this._currentState === 'STOPPING' ||
                this._currentState === 'PREPARING' ||
                this._currentState === 'TRANSCRIBING') {
                this._stopRecording();
                return;
            }

            // Track that this is Claude mode with specific style
            this._isClaudeMode = true;
            this._claudeStyle = style;
            this._pasteTarget.pin(global.display.focus_window);

            // Build flags similar to _launchWSI
            const debugFlag = this._debugEnabled ? ' --debug' : '';
            const autoPasteFlag = this._autoPaste ? ' --auto-paste' : '';
            // FORCE no-auto-enter for Claude modes - transcription needs review before sending
            const noAutoEnterFlag = this._autoPaste ? ' --no-auto-enter' : '';
            const wrapMarkerFlag = this._wrapWithMarker ? ' --wrap-marker' : '';
            const noNotifyFlag = !this._showNotifications ? ' --no-notify' : '';
            const pasteWithShiftFlag = this._autoPaste ? ` --paste-with-shift ${this._getPasteWithShift()}` : '';

            // Use wsi-stream for streaming mode, otherwise use batch wsi
            const script = this._streamingMode ? 'wsi-stream' : 'wsi';
            // Strip shell metacharacters: language is a free-form GSettings string and
            // is interpolated into a double-quoted `bash -c` command in the non-auto path.
            const langFlag = ` --language ${this._validateShellArg(this._getWhisperLanguage())}`;

            // Add startup mode flags for streaming mode
            let startupModeFlag = '';
            if (this._streamingMode) {
                if (this._streamingStartupMode === 'pre-buffer') {
                    startupModeFlag = ' --pre-buffer';
                } else if (this._streamingStartupMode === 'server') {
                    startupModeFlag = ' --server-mode';
                }
                // 'standard' mode has no flag
            }

            // Claude-specific flags with style parameter - validate before interpolation
            const safeClaudeModel = this._validateShellArg(
                this._claudeModel,
                this._claudeModels.map(m => m[0])
            );
            const claudeProcessFlag = ' --claude-process';
            const claudeModelFlag = ` --claude-model ${safeClaudeModel}`;
            const claudeStyleFlag = ` --claude-style ${style}`;

            const scriptPath = GLib.get_home_dir() + '/.local/bin/' + script;
            const scriptArgs = debugFlag + autoPasteFlag + noAutoEnterFlag + wrapMarkerFlag + noNotifyFlag + langFlag + startupModeFlag + pasteWithShiftFlag + claudeProcessFlag + claudeModelFlag + claudeStyleFlag;

            // Build command - wrap in bash if we need to set environment variables
            let command;
            if (this._whisperModel !== 'auto') {
                // Validate model name against known values before shell interpolation
                const safeModel = this._validateShellArg(
                    this._whisperModel,
                    this._whisperModels.map(m => m[0])
                );
                // Need to set WHISPER_MODEL env var - use bash -c
                command = `/bin/bash -c "WHISPER_MODEL=${safeModel} ${scriptPath}${scriptArgs}"`;
            } else {
                // Auto mode uses script defaults - no env var needed
                command = scriptPath + scriptArgs;
            }

            this._log(`Launching with Claude processing: ${command}`);
            this._beginLaunchDebounce();
            GLib.spawn_command_line_async(command);
        } catch (e) {
            this._lastError = e.message;
            this._log(`Launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _launchArticleMode() {
        try {
            // Debounce: ignore a repeat press while a spawned recorder has not yet reported
            if (this._launchPending) {
                this._log('Article mode: launch already in progress; ignoring repeat press');
                return;
            }
            // Don't allow opening article mode while another recording is active
            if (this._currentState === 'RECORDING' ||
                this._currentState === 'STOPPING' ||
                this._currentState === 'PREPARING' ||
                this._currentState === 'TRANSCRIBING') {
                this._log('Article mode: another recording is already active');
                return;
            }

            this._isArticleMode = true;
            this._isClaudeMode = false;
            this._claudeStyle = null;

            const debugFlag = this._debugEnabled ? ' --debug' : '';
            const noNotifyFlag = !this._showNotifications ? ' --no-notify' : '';
            // Strip shell metacharacters: language is a free-form GSettings string and
            // is interpolated into a double-quoted `bash -c` command in the non-auto path.
            const langFlag = ` --language ${this._validateShellArg(this._getWhisperLanguage())}`;
            const safeClaudeModel = this._validateShellArg(
                this._claudeModel,
                this._claudeModels.map(m => m[0])
            );
            const modelFlag = ` --model ${safeClaudeModel}`;

            const scriptPath = GLib.get_home_dir() + '/.local/bin/wsi-article-window';
            const command = scriptPath + debugFlag + noNotifyFlag + langFlag + modelFlag;

            this._log(`Launching article mode: ${command}`);
            this._beginLaunchDebounce();
            GLib.spawn_command_line_async(command);

            // Start elapsed timer immediately for visual feedback
            this._startElapsedTimer();
        } catch (e) {
            this._isArticleMode = false;
            this._lastError = e.message;
            this._log(`Article mode launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _startElapsedTimer() {
        // Reuse the same _countdownLabel and _recordingTimer variables
        // so _stopCountdown() handles cleanup correctly.
        this._stopCountdown();

        // Replace iconBox with a spinner label — no countdown since article
        // mode records indefinitely. Spinner communicates "keeps going".
        if (this._iconBox) {
            this._indicator.remove_child(this._iconBox);
        }

        const spinnerChars = ['◐', '◓', '◑', '◒'];
        let spinnerIdx = 0;

        this._countdownLabel = new St.Label({
            text: `ART ${spinnerChars[0]}`,
            y_align: 2,  // Clutter.ActorAlign.CENTER
            style: 'color: white; font-weight: bold; font-size: 13px; background-color: #44ff44; padding: 2px 4px; border-radius: 3px;'
        });
        this._indicator.add_child(this._countdownLabel);

        this._log('Article mode spinner started');

        // Advance spinner every 600ms — clearly signals indefinite recording
        this._recordingTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 600, () => {
            spinnerIdx = (spinnerIdx + 1) % spinnerChars.length;
            if (this._countdownLabel)
                this._countdownLabel.text = `ART ${spinnerChars[spinnerIdx]}`;
            return GLib.SOURCE_CONTINUE;
        });
    }

    _getPasteWithShift() {
        // 1 (Ctrl+Shift+V) or 0 (Ctrl+V) for the window focused at Insert. The recorder
        // asks again (PasteKey) at each paste; this is its answer if the panel cannot.
        return this._pasteTargetFor(global.display.focus_window).withShift ? 1 : 0;
    }

    /**
     * How to paste into `window`: its WM class, whether it takes Ctrl+Shift+V, and
     * whether to send Ctrl+S after.
     *
     * A terminal is an app whose desktop entry lists the TerminalEmulator category, the
     * freedesktop way of saying so, rather than a list of terminals kept by hand. In order:
     * an app in paste-ctrl-v-apps pastes with Ctrl+V; a terminal with Ctrl+Shift+V; any
     * other app with paste-default-mode (Ctrl+V by default, what GUI toolkits bind).
     * Ctrl+S is for apps in paste-save-apps, and never for a terminal (XOFF there).
     */
    _pasteTargetFor(window) {
        const wmClass = window ? window.get_wm_class() ?? '' : '';
        const list = key => (this._settings ? this._settings.get_string(key) : '')
            .split(',').map(s => s.trim()).filter(Boolean);
        const app = window ? Shell.WindowTracker.get_default().get_window_app(window) : null;
        const categories = app?.get_app_info()?.get_categories() ?? '';
        const isTerminal = categories.split(';').includes('TerminalEmulator');

        let withShift, why;
        if (wmClass && list('paste-ctrl-v-apps').includes(wmClass)) {
            withShift = false;
            why = 'in the Ctrl+V list';
        } else if (isTerminal) {
            withShift = true;
            why = 'a terminal';
        } else {
            const defaultMode = this._settings ? this._settings.get_string('paste-default-mode') : 'no-shift';
            withShift = defaultMode === 'with-shift';
            why = `not a terminal (paste-default-mode ${defaultMode})`;
        }
        const saveAfter = !isTerminal && wmClass !== '' && list('paste-save-apps').includes(wmClass);
        this._log(`Paste into "${wmClass}", ${why}: ${withShift ? 'Ctrl+Shift+V' : 'Ctrl+V'}` +
            `${saveAfter ? ', then Ctrl+S' : ''}`);
        return { wmClass, withShift, saveAfter };
    }

    /**
     * The panel's D-Bus object: the recorder asks it how to paste, just before pasting,
     * into the window pinned at Insert (pasteTarget.js).
     */
    _exportPanelDBus() {
        this._panelDBus = Gio.DBusExportedObject.wrapJSObject(PANEL_DBUS_XML, {
            PasteKey: () => {
                const { window, focused, gone } = this._pasteTarget.answer(global.display.focus_window);
                if (gone) {
                    this._log('The window this dictation started in was closed: nothing is pasted');
                    return ['', false, false, false, true];
                }
                const { wmClass, withShift, saveAfter } = this._pasteTargetFor(window);
                if (!focused)
                    this._log(`"${wmClass}" lost focus during the dictation; giving it focus back`);
                return [wmClass, withShift, saveAfter, focused, false];
            },
        });
        this._panelDBus.export(Gio.DBus.session, PANEL_DBUS_PATH);
    }

    _unexportPanelDBus() {
        if (this._panelDBus) {
            this._panelDBus.unexport();
            this._panelDBus = null;
        }
    }

    _killRecorders(signalName) {
        // `wsi-stream( |$)` is anchored so it never matches the warm wsi-stream-server:
        // killing that throws away the loaded model and forces a cold start next time.
        const binDir = GLib.get_home_dir() + '/.local/bin';
        GLib.spawn_command_line_async(`pkill --signal ${signalName} -f "pw-record.*wfile"`);
        GLib.spawn_command_line_async(`pkill --signal ${signalName} -f "${binDir}/wsi-stream( |$)"`);
        GLib.spawn_command_line_async(`pkill --signal ${signalName} -f "${binDir}/wsi --"`);
    }

    _resetToIdle() {
        this._currentState = 'IDLE';
        this._clearLaunchDebounce();
        this._stopCountdown();
        this._updateIconState('IDLE');
    }

    _readLiveRecorderPid(pidFile) {
        // Returns the recorder PID if the PID file names a live process, else null
        const file = Gio.File.new_for_path(pidFile);
        if (!file.query_exists(null)) {
            return null;
        }
        const [, contents] = file.load_contents(null);
        const pid = new TextDecoder().decode(contents).trim();
        if (!/^[0-9]+$/.test(pid)) {
            this._log(`PID file holds a non-numeric value: "${pid}"`);
            return null;
        }
        if (!Gio.File.new_for_path(`/proc/${pid}`).query_exists(null)) {
            this._log(`PID file is stale (PID ${pid} not running)`);
            return null;
        }
        return pid;
    }

    _stopRecording() {
        const pidFile = '/dev/shm/stt-recording-' + GLib.get_user_name() + '.pid';
        this._log(`Stopping recording via PID file: ${pidFile}`);

        let pid = null;
        try {
            pid = this._readLiveRecorderPid(pidFile);
        } catch (e) {
            this._log(`Stop error reading PID file: ${e.message}`);
        }

        if (pid) {
            this._log(`Killing PID: ${pid}`);
            GLib.spawn_command_line_async(`kill ${pid}`);
            return;
        }

        // No live recorder owns the state, so no process will ever emit IDLE for it.
        // Sweep any stragglers and reset here, otherwise the indicator stays wedged
        // and every later Insert press lands back in this branch.
        this._log('No live recorder PID; pkill fallback and reset to IDLE');
        this._killRecorders('TERM');
        this._resetToIdle();
    }

    _abortRecording() {
        // Only abort while the microphone is open: recording, or in the stop grace
        if (this._currentState !== 'RECORDING' && this._currentState !== 'STOPPING') {
            this._log('Abort ignored - not recording');
            return;
        }

        this._log('Aborting recording (Escape pressed)');

        const pidFile = '/dev/shm/stt-recording-' + GLib.get_user_name() + '.pid';
        try {
            // Server mode: the audio lives in the warm server, so the client must send it
            // STOP. SIGUSR1 asks the client to do that and discard the text.
            const pid = this._readLiveRecorderPid(pidFile);
            if (pid && this._streamingMode && this._streamingStartupMode === 'server') {
                this._log(`Discarding server-mode recording: SIGUSR1 to ${pid}`);
                GLib.spawn_command_line_async(`kill -USR1 ${pid}`);
                this._resetToIdle();
                return;
            }

            // Other modes own their audio: SIGKILL the process tree to prevent transcription
            this._killRecorders('KILL');

            // Clean up PID file
            const file = Gio.File.new_for_path(pidFile);
            if (file.query_exists(null)) {
                file.delete(null);
            }
        } catch (e) {
            this._log(`Abort cleanup error: ${e.message}`);
        }

        // Reset UI immediately
        this._resetToIdle();
        this._log('Recording aborted');
    }

    _ensureLogDirectory() {
        const dir = Gio.File.new_for_path(this._logDir);
        if (!dir.query_exists(null)) {
            try {
                dir.make_directory_with_parents(null);
            } catch (e) {
                // Non-fatal: the wsi script also creates this dir. Log so it is diagnosable.
                logError(e, 'STT: creating log directory');
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
            try {
                this._showNotifications = this._settings.get_boolean('show-notifications');
                if (this._notifySwitch) {
                    this._notifySwitch.setToggleState(this._showNotifications);
                }
            } catch (e) {
                this._showNotifications = false;
            }
            try {
                this._whisperModel = this._settings.get_string('whisper-model');
            } catch (e) {
                this._whisperModel = 'auto';
            }
            try {
                this._streamingStartupMode = this._settings.get_string('streaming-startup-mode');
            } catch (e) {
                this._streamingStartupMode = 'standard';
            }
            try {
                this._claudeEnabled = this._settings.get_boolean('claude-enabled');
                if (this._claudeSwitch) {
                    this._claudeSwitch.setToggleState(this._claudeEnabled);
                }
            } catch (e) {
                this._claudeEnabled = false;
            }
            try {
                this._claudeModel = this._settings.get_string('claude-model');
            } catch (e) {
                this._claudeModel = 'sonnet';
            }
        }
    }

    _openModelManager() {
        // Launch wsi-model-manager in a terminal window.
        // Strategy:
        //   1. foot (installed by playbook) — Wayland-native, --window-size-chars gives
        //      exact initial geometry without relying on escape sequences that Wayland
        //      compositors ignore for security.
        //   2. xdg-terminal-exec (installed by playbook) — XDG standard that opens
        //      whatever terminal the user has set as default (Ptyxis, kgx, etc.).
        //      No size control, but at least uses the right terminal.
        const script = GLib.get_home_dir() + '/.local/bin/wsi-model-manager';
        this._log('Opening model manager');

        try {
            // foot: exact 120×35 window, Wayland-native, HiDPI-aware
            GLib.spawn_command_line_async(`foot --window-size-chars=120x35 -- ${script}`);
        } catch (_e1) {
            try {
                // xdg-terminal-exec: opens the user's configured default terminal
                GLib.spawn_command_line_async(`xdg-terminal-exec ${script}`);
            } catch (e2) {
                this._log(`Cannot open terminal: ${e2.message}`);
                Main.notify('Speech to Text', 'Cannot open terminal. Run: wsi-model-manager');
            }
        }
    }

    _openServerManager() {
        // Launch wsi-server-manager in a compact terminal window.
        // 60×24 is sufficient for the status display and menu.
        const script = GLib.get_home_dir() + '/.local/bin/wsi-server-manager';
        this._log('Opening server manager');

        try {
            GLib.spawn_command_line_async(`foot --window-size-chars=60x24 -- ${script}`);
        } catch (_e1) {
            try {
                GLib.spawn_command_line_async(`xdg-terminal-exec ${script}`);
            } catch (e2) {
                this._log(`Cannot open terminal: ${e2.message}`);
                Main.notify('Speech to Text', 'Cannot open terminal. Run: wsi-server-manager');
            }
        }
    }

    _saveDebugSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('debug-mode', enabled);
            } catch (e) {
                logError(e, 'STT: saving debug-mode setting');
            }
        }
    }

    _saveAutoPasteSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('auto-paste', enabled);
            } catch (e) {
                logError(e, 'STT: saving auto-paste setting');
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
            // FAIL-FAST-OK: this IS the file logger; fall back to the journal rather than
            // recurse into _log. Never throw from here (the caller is on the shell thread).
            log(`STT: failed to write debug log: ${e.message}`);
        }
    }

    _updateStatusLabel() {
        let statusText = `Status: ${this._currentState}`;

        // Add server status if in server mode
        if (this._streamingMode && this._streamingStartupMode === 'server') {
            const serverStatus = this._serverIsHot ? '🔵 Hot' : 'Cold';
            statusText += ` (Server: ${serverStatus})`;
        }

        if (this._lastError) {
            statusText += ` - ${this._lastError.substring(0, 30)}`;
        }
        this._statusLabel.label.text = statusText;

        // Color-code based on state
        switch (this._currentState) {
            case 'RECORDING':
                this._statusLabel.label.style = 'font-weight: bold; color: #ff4444;';
                break;
            case 'STOPPING':
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

}
