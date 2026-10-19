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
import { PanelSession } from './panelSession.js';
import { spawnWatched, killArgv, discardSignal } from './recorderLaunch.js';
import { lookFor, isKnownState, recordingBadgeColor, articleBadgeColor, shownState, COLOURS } from './panelLook.js';
import { POST_PROCESS_STYLES, LastTranscriptPostProcessor, unusableKeybindings } from './postProcess.js';
import { TOKEN_WARNING_FILE, tokenWarning } from './tokenWarning.js';

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

// The recorders' shared PID file, for finding a recorder the panel did not start
const RECORDER_PID_FILE = `/dev/shm/stt-recording-${GLib.get_user_name()}.pid`;
// wsi-stream names itself here from the start of its stop until it exits
const STOPPING_PID_FILE = `/dev/shm/stt-stopping-${GLib.get_user_name()}.pid`;
// A PID file naming a process that is not one of the recorders is stale (a reused PID)
const RECORDER_CMDLINE = /\/wsi(-stream|-article)?(\0|$)/;

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
        // The one recorder the panel follows, and what each key does (panelSession.js)
        this._panel = null;
        this._hourglassIcon = null;
        this._iconState = null;           // the state the icon shows
        this._stateUnderBlocked = null;   // the state a BLOCKED flash is shown over
        this._articleSpinner = false;
        this._updatingToggles = false;  // Guard flag to prevent toggle cascade
        this._settingsChangedId = null;
        this._lastError = null;
        this._logDir = GLib.get_home_dir() + '/.local/share/speech-to-text';
        this._logFile = this._logDir + '/debug.log';

        // Server status indicator
        this._serverStatusDot = null;
        this._serverPollTimer = null;
        this._serverIsHot = false;
        this._filesRunning = false;       // the server is transcribing recordings (stt)

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

        // No Claude token has allowance left (tokenWarning.js): shown until cleared
        this._tokenWarningIcon = new St.Icon({
            icon_name: 'dialog-warning-symbolic',
            style_class: 'system-status-icon',
            style: `color: ${COLOURS.amber};`,
            visible: false
        });
        this._iconBox.add_child(this._tokenWarningIcon);

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
                        this._onRecordKey('dictation');
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
                        this._onRecordKey('claude', 'corporate');
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
                        this._onRecordKey('claude', 'natural');
                    }
                );
                this._log(`Claude natural keybinding registration result: ${claudeNaturalBindingAdded}`);

                // A key the shell refused, or with no key set, does nothing when pressed;
                // the debug log above is off by default, so say it where it is seen
                const unusable = unusableKeybindings([
                    ['toggle-recording', bindingAdded],
                    ['toggle-recording-claude', claudeBindingAdded],
                    ['toggle-recording-claude-natural', claudeNaturalBindingAdded],
                ].map(([name, action]) => ({name, action, accels: this._settings.get_strv(name)})),
                Meta.KeyBindingAction.NONE);
                if (unusable.length > 0) {
                    this._log(`Unusable record keys: ${unusable.join('; ')}`);
                    Main.notify('STT Error', `These record keys do nothing: ${unusable.join('; ')}. ` +
                        'The menu items still work; the keys are set in Settings.');
                }

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

        // No installed icon theme has an hourglass, so the extension ships its own
        this._hourglassIcon = Gio.FileIcon.new(
            Gio.File.new_for_path(`${this.path}/stt-hourglass-symbolic.svg`));
        this._panel = this._createPanelSession();
        this._postProcessor = new LastTranscriptPostProcessor({
            bin: GLib.get_home_dir() + '/.local/bin/',
            run: (argv, done) => this._runCollectingOutput(argv, done),
            setClipboard: text => St.Clipboard.get_default().set_text(St.ClipboardType.CLIPBOARD, text),
            notify: message => Main.notify('Speech to Text', message),
            log: message => this._log(message),
        });

        // Ensure log directory exists
        this._ensureLogDirectory();
        this._watchTokenWarning();

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

        // The panel's timers end with it; a recorder still running runs on, and a panel
        // enabled later finds it at the next press (a recorder it did not start)
        if (this._panel) {
            this._panel.destroy();
            this._panel = null;
        }
        // A post-processing run still going finishes on its own; its result is dropped
        if (this._postProcessor) {
            this._postProcessor.destroy();
            this._postProcessor = null;
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

        // Stop server status polling
        this._stopServerStatusPolling();

        if (this._tokenWarningMonitor) {
            this._tokenWarningMonitor.cancel();
            this._tokenWarningMonitor = null;
        }
        this._tokenWarningIcon = null;
        this._tokenWarningItem = null;

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

        // Shown with the panel's warning icon while no Claude token has allowance left
        this._tokenWarningItem = new PopupMenu.PopupMenuItem('');
        this._tokenWarningItem.label.style = `color: ${COLOURS.amber};`;
        this._tokenWarningItem.visible = false;
        this._tokenWarningItem.connect('activate', () => { this._clearTokenWarning(); });
        menu.addMenuItem(this._tokenWarningItem);

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

        // Claude post-processing after the fact, one item per Ctrl/Alt+Insert style
        for (const {style, label} of POST_PROCESS_STYLES) {
            const item = new PopupMenu.PopupMenuItem(label);
            item.connect('activate', () => { this._postProcessLast(style); });
            menu.addMenuItem(item);
        }

        const viewLogItem = new PopupMenu.PopupMenuItem('View Debug Log...');
        viewLogItem.connect('activate', () => { this._openLogViewer(); });
        menu.addMenuItem(viewLogItem);

        const articleItem = new PopupMenu.PopupMenuItem('Create Article...');
        articleItem.connect('activate', () => { this._onArticleItem(); });
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

    /** The session token a recorder sends after the value; '' from one that sends none. */
    _signalToken(params) {
        return params.n_children() > 1 ? params.get_child_value(1).get_string()[0] : '';
    }

    _subscribeToDBus() {
        // StateChanged from the recorders: (state, session token). Any sender may emit it
        // (each signal is a throwaway gdbus process), so the token is what says whose it
        // is, and panelSession.js takes it only from the recorder it follows.
        this._dbusSubscriptionId = Gio.DBus.session.signal_subscribe(
            null,                    // sender (any)
            DBUS_INTERFACE,          // interface
            'StateChanged',          // signal name
            DBUS_PATH,               // object path
            null,                    // arg0 (any)
            Gio.DBusSignalFlags.NONE,
            (connection, sender, path, iface, signal, params) => {
                const state = params.get_child_value(0).get_string()[0];
                if (!this._panel?.onState(state, this._signalToken(params)))
                    return;
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
                if (!this._panel?.onError(errorMsg, this._signalToken(params)))
                    return;
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
                if (!this._panel?.acceptsProgress(this._signalToken(params)))
                    return;
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

    /** Whether the server is transcribing recordings (stt), in any startup mode: it marks a
     * file job with wsi-file-job while it runs. A marker beside no PID file is a server
     * that is gone, and means nothing. Idle shows FILES while it is so. */
    _checkFileJob() {
        const runtimeDir = GLib.get_user_runtime_dir();
        if (!runtimeDir)
            return;
        let filesRunning = false;
        try {
            filesRunning = Gio.File.new_for_path(`${runtimeDir}/wsi-file-job`).query_exists(null) &&
                Gio.File.new_for_path(`${runtimeDir}/wsi-stream-server.pid`).query_exists(null);
        } catch (e) {
            this._log(`File job check failed: ${e.message}`, 'WARN');
        }
        if (filesRunning !== this._filesRunning) {
            this._filesRunning = filesRunning;
            this._log(`File transcription ${filesRunning ? 'started' : 'ended'}`);
            if (this._icon && this._iconState === 'IDLE')
                this._showLook('IDLE');
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
            this._checkFileJob();
        } catch (e) {
            this._log(`Initial server check failed: ${e.message}`, 'ERROR');
        }

        // Start periodic polling
        this._serverPollTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 5, () => {
            try {
                this._checkServerStatus();
                this._checkFileJob();
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
        this._filesRunning = false;
    }

    /**
     * Show `state`, as panelSession.js derives it (the only caller): IDLE is ready; the
     * hourglass states say the press was taken and to wait; BLOCKED flashes for a press
     * that did nothing; the rest are the state the recorder reported. `launch` is the
     * recorder's, or null with none.
     */
    _updateIconState(state, launch = null) {
        if (!this._icon) return;
        // BLOCKED flashes over the state the panel stays in, and that state comes back
        // when it ends: only the icon changes, never the countdown, Escape or the outline
        const underBlocked = state === 'BLOCKED' || state === this._stateUnderBlocked;
        this._stateUnderBlocked = state === 'BLOCKED' ? this._iconState : null;
        this._iconState = state;
        if (underBlocked) {
            this._showLook(state);
            return;
        }
        this._isArticleMode = launch?.kind === 'article';

        // Outline the window the text will be pasted into, for as long as a paste can come;
        // the pin taken at Insert lasts until the dictation has ended
        const pasteCanCome = ['RECORDING', 'STOPPING', 'TRANSCRIBING'].includes(state);
        if (pasteCanCome && this._autoPaste && !this._isArticleMode)
            this._focusOutline?.show(this._pasteTarget?.window ?? null);
        else
            this._focusOutline?.hide();
        if (['IDLE', 'SUCCESS', 'ERROR'].includes(state))
            this._pasteTarget?.release();

        // An article shows its ART spinner from launch to end; it has no countdown and
        // no Escape (its window has the stop button)
        if (this._isArticleMode) {
            this._removeAbortKeybinding();
            if (!this._articleSpinner)
                this._startElapsedTimer();
            return;
        }

        this._showLook(state);

        switch (state) {
            case 'RECORDING':
                this._startCountdown();
                this._addAbortKeybinding();  // Enable Escape key during recording
                break;
            case 'STOPPING':
                // Insert was pressed: the microphone stays open for the stop grace, but
                // the icon already shows the press was taken. Escape still discards;
                // Insert does nothing until the text is pasted.
                this._stopCountdown();
                break;
            case 'SUCCESS':
                this._stopCountdown();
                this._removeAbortKeybinding();
                // When auto-paste is OFF the transcript only lands in the PRIMARY
                // selection — there is no visible feedback that it's ready. Show
                // a one-shot notification so the user knows to middle-click.
                // Auto-paste ON is self-evident (the text appears at the cursor).
                if (!this._autoPaste) {
                    Main.notify('Speech to Text', 'Transcribed — middle-click to paste');
                }
                break;
            case 'IDLE':
                this._stopCountdown();
                this._removeAbortKeybinding();
                this._elapsedSeconds = 0;
                break;
            default:
                // Every other state: no microphone the panel can discard, no countdown
                this._stopCountdown();
                this._removeAbortKeybinding();
                break;
        }
    }

    /** The icon and its colour for `state`, which are panelLook.js's; idle shows a file
     * job (FILES) while one runs. */
    _showLook(state) {
        if (!isKnownState(state))
            this._log(`Unknown state "${state}": shown as please-wait, never as ready`);
        const look = lookFor(shownState(state, this._filesRunning));
        if (look.icon === 'hourglass')
            this._icon.gicon = this._hourglassIcon;
        else
            this._icon.icon_name = look.icon;
        this._icon.style = look.colour ? `color: ${look.colour};` : '';
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
        // Red like the recording mic; amber when transcription is falling behind or in the
        // last 10 s of the maximum length. Never green: green means SUCCESS.
        const colour = recordingBadgeColor({behind: this._dictationBehind, secondsLeft: left});
        this._countdownLabel.style = `color: white; font-weight: bold; font-size: 13px; background-color: ${colour}; padding: 2px 4px; border-radius: 3px;`;
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
                this._countdownLabel.style = `color: white; font-weight: bold; font-size: 13px; background-color: ${COLOURS.red}; padding: 2px 4px; border-radius: 3px;`;
            } else {
                // White background, red text
                this._countdownLabel.style = `color: ${COLOURS.red}; font-weight: bold; font-size: 13px; background-color: white; padding: 2px 4px; border-radius: 3px;`;
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
        this._articleSpinner = false;

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

    /** `value` if it is one of `allowedValues`, else the first of them (logged). */
    _knownOr(value, allowedValues) {
        if (allowedValues.includes(value))
            return value;
        this._log(`Unknown setting value "${value}"; using "${allowedValues[0]}"`);
        return allowedValues[0];
    }

    /** Insert (dictation) or Ctrl/Alt+Insert (claude): what the press means is
     * panelSession.js's decision. */
    _onRecordKey(kind, style = null) {
        try {
            this._panel.insert(this._launchFor(kind, style));
        } catch (e) {
            this._lastError = e.message;
            this._log(`Insert failed: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _onArticleItem() {
        try {
            this._panel.article(this._launchFor('article'));
        } catch (e) {
            this._lastError = e.message;
            this._log(`Article mode failed: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    /** What a press asks for, fixed at the press: the kind of recording, the window the
     * text goes to, and the recorder the settings chose. */
    _launchFor(kind, style = null) {
        return {
            kind,
            style,
            target: global.display.focus_window,
            script: this._streamingMode ? 'wsi-stream' : 'wsi',
            serverMode: this._streamingMode && this._streamingStartupMode === 'server',
        };
    }

    _createPanelSession() {
        return new PanelSession({
            spawn: (launch, token, onExit) => this._spawnRecorder(launch, token, onExit),
            kill: (pid, signalName) => this._signalProcess(pid, signalName),
            abort: (pid, launch) => this._discardRecording(pid, launch),
            readRecorderPid: () => this._readLiveRecorderPid(RECORDER_PID_FILE),
            readStoppingPid: () => this._readLiveRecorderPid(STOPPING_PID_FILE),
            isAlive: pid => Gio.File.new_for_path(`/proc/${pid}`).query_exists(null),
            newToken: () => GLib.uuid_string_random(),
            show: (state, launch) => this._updateIconState(state, launch),
            log: message => this._log(message),
            reportError: message => {
                this._lastError = message;
                this._log(message);
                Main.notify('STT Error', message);
            },
            notify: message => Main.notify('Speech to Text', message),
            addTimeout: (ms, handler) => GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, handler),
            removeTimeout: id => GLib.Source.remove(id),
        });
    }

    /** Start the recorder for `launch`, telling it `token`; returns its PID. */
    _spawnRecorder(launch, token, onExit) {
        this._isClaudeMode = launch.kind === 'claude';
        this._claudeStyle = launch.style;
        if (launch.kind !== 'article')
            this._pasteTarget.pin(launch.target);
        const {argv, env} = this._recorderCommand(launch, token);
        const model = env.WHISPER_MODEL ? ` (WHISPER_MODEL=${env.WHISPER_MODEL})` : '';
        this._log(`Launching ${launch.kind} (session ${token}): ${argv.join(' ')}${model}`);
        return spawnWatched(argv, env, onExit);
    }

    /** The recorder's argv and the environment it needs. No shell is involved, so no
     * argument can be read as shell syntax, and the PID is the recorder's own. */
    _recorderCommand(launch, token) {
        const bin = GLib.get_home_dir() + '/.local/bin/';
        const claudeModel = this._knownOr(this._claudeModel, this._claudeModels.map(m => m[0]));
        const argv = [];
        if (launch.kind === 'article') {
            argv.push(bin + 'wsi-article-window');
            if (this._debugEnabled) argv.push('--debug');
            if (!this._showNotifications) argv.push('--no-notify');
            argv.push('--language', this._getWhisperLanguage(), '--model', claudeModel,
                '--session-token', token);
            return {argv, env: {}};
        }

        const claude = launch.kind === 'claude';
        argv.push(bin + launch.script);
        if (this._debugEnabled) argv.push('--debug');
        if (this._autoPaste) argv.push('--auto-paste');
        // Claude's text is reviewed before it is sent, so never an Enter after it
        if (this._autoPaste && (claude || !this._autoEnter)) argv.push('--no-auto-enter');
        if (this._wrapWithMarker) argv.push('--wrap-marker');
        if (!this._showNotifications) argv.push('--no-notify');
        argv.push('--language', this._getWhisperLanguage());
        if (launch.script === 'wsi-stream' && this._streamingStartupMode === 'pre-buffer')
            argv.push('--pre-buffer');
        if (launch.serverMode)
            argv.push('--server-mode');
        // The key for the window pinned at the press; the recorder asks again (PasteKey)
        // at each paste, and this is its answer if the panel cannot say
        if (this._autoPaste)
            argv.push('--paste-with-shift', this._pasteTargetFor(launch.target).withShift ? '1' : '0');
        if (claude)
            argv.push('--claude-process', '--claude-model', claudeModel, '--claude-style', launch.style);
        argv.push('--session-token', token);

        const env = {};
        if (this._whisperModel !== 'auto')
            env.WHISPER_MODEL = this._knownOr(this._whisperModel, this._whisperModels.map(m => m[0]));
        return {argv, env};
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
            style: `color: white; font-weight: bold; font-size: 13px; background-color: ${articleBadgeColor()}; padding: 2px 4px; border-radius: 3px;`
        });
        this._indicator.add_child(this._countdownLabel);

        this._log('Article mode spinner started');

        // Advance spinner every 600ms — clearly signals indefinite recording
        this._articleSpinner = true;
        this._recordingTimer = GLib.timeout_add(GLib.PRIORITY_DEFAULT, 600, () => {
            spinnerIdx = (spinnerIdx + 1) % spinnerChars.length;
            if (this._countdownLabel)
                this._countdownLabel.text = `ART ${spinnerChars[spinnerIdx]}`;
            return GLib.SOURCE_CONTINUE;
        });
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

    /** Send `signalName` to recorder `pid`, or to its whole process group with `group`. */
    _signalProcess(pid, signalName, group = false) {
        const argv = killArgv(pid, signalName, group);
        this._log(argv.join(' '));
        GLib.spawn_async(null, argv, null, GLib.SpawnFlags.SEARCH_PATH, null);
    }

    /** Escape: throw the dictation away (recorderLaunch.js discardSignal says how). */
    _discardRecording(pid, launch) {
        const {signal, group} = discardSignal(launch);
        this._signalProcess(pid, signal, group);
        if (launch.serverMode)
            return;
        // A killed recorder cannot remove its own PID file
        if (this._readPidFile(RECORDER_PID_FILE) === String(pid)) {
            Gio.File.new_for_path(RECORDER_PID_FILE).delete(null);
            this._log(`Removed the PID file of the discarded recorder ${pid}`);
        }
    }

    /** The PID the file at `path` holds, or null with no file. */
    _readPidFile(path) {
        // One read, no exists-then-read: a recorder can remove the file in between
        let contents;
        try {
            [, contents] = Gio.File.new_for_path(path).load_contents(null);
        } catch (e) {
            if (e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND))
                return null;  // no file is the answer "no recorder"
            throw e;
        }
        return new TextDecoder().decode(contents).trim();
    }

    /** The PID of a live recorder the file at `path` names, or null: a missing file, or
     * one naming a process that is gone or is not a recorder (a reused PID), is none. */
    _readLiveRecorderPid(path) {
        const pid = this._readPidFile(path);
        if (pid === null)
            return null;
        if (!/^[0-9]+$/.test(pid)) {
            this._log(`${path} holds a non-numeric value: "${pid}"`);
            return null;
        }
        let contents;
        try {
            [, contents] = Gio.File.new_for_path(`/proc/${pid}/cmdline`).load_contents(null);
        } catch (e) {
            // FAIL-FAST-OK: no /proc entry is the answer "not running", not a failure
            this._log(`${path} is stale (PID ${pid} not running: ${e.message})`);
            return null;
        }
        if (!RECORDER_CMDLINE.test(new TextDecoder().decode(contents))) {
            this._log(`${path} is stale (PID ${pid} is not a recorder)`);
            return null;
        }
        return pid;
    }

    _abortRecording() {
        try {
            this._panel?.escape();
        } catch (e) {
            this._lastError = e.message;
            this._log(`Escape failed: ${e.message}`);
            Main.notify('STT Error', `Escape failed: ${e.message}`);
        }
    }

    /** Show the token warning for as long as wsi-claude-token's file is there. */
    _watchTokenWarning() {
        this._tokenWarningFile = Gio.File.new_for_path(`${this._logDir}/${TOKEN_WARNING_FILE}`);
        this._lastTokenWarning = null;
        try {
            this._tokenWarningMonitor = this._tokenWarningFile.monitor_file(Gio.FileMonitorFlags.NONE, null);
            this._tokenWarningMonitor.connect('changed', () => this._refreshTokenWarning());
        } catch (e) {
            logError(e, 'STT: watching the Claude token warning');
            Main.notify('STT Error', 'The panel cannot watch for every Claude token running out ' +
                `(${e.message}); it shows only what it finds at login`);
        }
        this._refreshTokenWarning();
    }

    _refreshTokenWarning() {
        this._tokenWarningFile.load_contents_async(null, (file, res) => {
            let text = null;
            try {
                const [, contents] = file.load_contents_finish(res);
                text = new TextDecoder().decode(contents);
            } catch (e) {
                // No file is the answer "no warning"; a file that cannot be read still warns
                if (!e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND))
                    text = `cannot read ${file.get_path()}: ${e.message}`;
            }
            this._showTokenWarning(tokenWarning(text));
        });
    }

    _showTokenWarning(warning) {
        if (!this._tokenWarningIcon || !this._tokenWarningItem)
            return;  // the extension was disabled while the file was read
        this._tokenWarningIcon.visible = warning !== null;
        this._tokenWarningItem.visible = warning !== null;
        if (warning) {
            this._tokenWarningItem.label.text = warning.label;
            if (warning.log !== this._lastTokenWarning)
                this._log(warning.log);
        }
        this._lastTokenWarning = warning?.log ?? null;
    }

    /** The owner clears the warning: the file goes, and the monitor hides it. */
    _clearTokenWarning() {
        this._tokenWarningFile.delete_async(GLib.PRIORITY_DEFAULT, null, (file, res) => {
            try {
                file.delete_finish(res);
                this._log('Claude token warning cleared from the menu');
            } catch (e) {
                if (!e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND)) {
                    this._log(`Could not clear the Claude token warning: ${e.message}`);
                    Main.notify('STT Error', `Could not clear the Claude token warning: ${e.message}`);
                }
            }
            this._refreshTokenWarning();
        });
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
        const state = this._panel?.display ?? 'IDLE';
        let statusText = `Status: ${state}`;

        // Add server status if in server mode
        if (this._streamingMode && this._streamingStartupMode === 'server') {
            const serverStatus = this._serverIsHot ? '🔵 Hot' : 'Cold';
            statusText += ` (Server: ${serverStatus})`;
        }

        if (this._lastError) {
            statusText += ` - ${this._lastError.substring(0, 30)}`;
        }
        this._statusLabel.label.text = statusText;

        // The colour the icon has in this state
        const {colour} = lookFor(state);
        this._statusLabel.label.style = `font-weight: bold;${colour ? ` color: ${colour};` : ''}`;
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

    /** A Post-Process Last Transcription item: postProcess.js runs it and reports it. */
    _postProcessLast(style) {
        const model = this._knownOr(this._claudeModel, this._claudeModels.map(m => m[0]));
        this._postProcessor?.start(style, model, this._debugEnabled);
    }

    /** Run `argv` without blocking the shell; `done(exitStatus, stdout, stderr, error)`
     * once it has ended (exitStatus null, error set, if its output could not be read). */
    _runCollectingOutput(argv, done) {
        const proc = Gio.Subprocess.new(argv,
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE);
        proc.communicate_utf8_async(null, null, (source, result) => {
            let stdout, stderr;
            try {
                [, stdout, stderr] = source.communicate_utf8_finish(result);
            } catch (e) {
                done(null, '', '', `its output could not be read: ${e.message}`);
                return;
            }
            const status = source.get_if_exited() ? source.get_exit_status()
                : `signal ${source.get_term_sig()}`;
            done(status, stdout, stderr, null);
        });
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
