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
        this._updatingToggles = false;  // Guard flag to prevent toggle cascade
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
        this._remainingSeconds = 27;  // Will be set based on mode in _startCountdown
        this._countdownLabel = null;
        this._flashTimer = null;
        this._flashState = false;
        this._isClaudeMode = false;  // Track if recording is in Claude mode
        this._claudeStyle = null;  // Track Claude style: 'corporate' or 'natural'

        // Whisper model definitions (name, label, size, description, englishOnly)
        this._whisperModels = [
            ['auto', 'Auto (optimized per mode)', 'varies', 'Base for streaming, small for batch', false],
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

        // Add to panel
        Main.panel.addToStatusArea('speech-to-text', this._indicator);

        // Subscribe to DBus signals from wsi script
        this._subscribeToDBus();

        // Ensure log directory exists
        this._ensureLogDirectory();

        // Start server status polling
        this._startServerStatusPolling();

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

        // Remove keybindings
        try {
            Main.wm.removeKeybinding('toggle-recording');
            Main.wm.removeKeybinding('toggle-recording-claude');
            Main.wm.removeKeybinding('toggle-recording-claude-natural');
            this._removeAbortKeybinding();
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

        // Stop server status polling
        this._stopServerStatusPolling();

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        this._settings = null;
        this._logLines = null;
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

        // Status section (non-interactive header)
        this._statusLabel = new PopupMenu.PopupMenuItem('Status: IDLE', { reactive: false });
        this._statusLabel.label.style = 'font-weight: bold;';
        menu.addMenuItem(this._statusLabel);

        // Separator
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Notifications toggle
        this._notifySwitch = new PopupMenu.PopupSwitchMenuItem('Show Notifications', this._showNotifications);
        this._preventMenuClose(this._notifySwitch);
        this._notifySwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._showNotifications = state;
            this._saveNotificationsSetting(state);
            this._log(`Notifications ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._notifySwitch);

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

        // Auto-paste toggle
        this._autoPasteSwitch = new PopupMenu.PopupSwitchMenuItem('Auto-paste at cursor', this._autoPaste);
        this._preventMenuClose(this._autoPasteSwitch);
        this._autoPasteSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;  // Prevent cascade
            this._autoPaste = state;
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
            if (!state) {
                // Reset to standard mode when streaming is turned off
                this._streamingStartupMode = 'standard';
                this._saveStreamingStartupModeSetting('standard');
            }
            this._saveStreamingSetting(state);
            this._updatePasteToggles();
            this._updateModelLabel();  // Update model label to show new auto mode model
            this._updateStreamingStartupLabel();
            this._checkServerStatus();  // Immediate check when streaming mode changes
            this._log(`Streaming mode ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._streamingSwitch);

        // Streaming startup mode - header item (non-interactive)
        this._streamingStartupHeader = new PopupMenu.PopupMenuItem('    ↳ Startup: Standard', { reactive: false });
        this._streamingStartupHeader.label.style = 'font-weight: bold; color: #888;';
        menu.addMenuItem(this._streamingStartupHeader);

        // Startup mode options (flat list, indented)
        const startupModes = [
            ['standard', 'Standard', 'Load model then start (~3-6s)'],
            ['pre-buffer', 'Pre-buffer', 'Record while loading (~2-4s)'],
            ['server', 'Server mode', 'Persistent server (<0.5s, uses memory)'],
        ];

        this._startupModeItems = [];
        for (const [mode, label, description] of startupModes) {
            const item = new PopupMenu.PopupMenuItem(`        • ${label} - ${description}`);
            item.connect('activate', () => {
                this._streamingStartupMode = mode;
                this._saveStreamingStartupModeSetting(mode);
                this._updateStreamingStartupLabel();
                this._updateStartupModeSelection();
                this._checkServerStatus();
                this._log(`Streaming startup mode set to: ${mode}`);
            });
            this._startupModeItems.push({ item, mode });
            menu.addMenuItem(item);
        }

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

        // Open paste config file
        const openConfigItem = new PopupMenu.PopupMenuItem('Open Config File...');
        openConfigItem.connect('activate', () => {
            this._openPasteConfigFile();
        });
        menu.addMenuItem(openConfigItem);

        // Separator before language
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Language - header item
        this._languageHeader = new PopupMenu.PopupMenuItem('Language: System default', { reactive: false });
        this._languageHeader.label.style = 'font-weight: bold;';
        menu.addMenuItem(this._languageHeader);

        // Language options (flat list)
        const languages = [
            ['system', 'System default'],
            ['en', 'English'],
        ];

        this._languageItems = [];
        for (const [code, label] of languages) {
            const item = new PopupMenu.PopupMenuItem(`  • ${label}`);
            item.connect('activate', () => {
                this._language = code;
                this._saveLanguageSetting(code);
                this._updateLanguageLabel();
                this._updateLanguageSelection();
                this._log(`Language set to: ${code}`);
            });
            this._languageItems.push({ item, code });
            menu.addMenuItem(item);
        }

        // Whisper model - header item
        this._modelHeader = new PopupMenu.PopupMenuItem('Model: Auto', { reactive: false });
        this._modelHeader.label.style = 'font-weight: bold;';
        menu.addMenuItem(this._modelHeader);

        // Model options — only installed models are shown; hidden items are refreshed on menu open.
        // Section headers are tracked so they can be hidden when no models in their section are installed.
        this._modelItems = [];
        this._multilingualHeader = null;
        this._englishOnlyHeader = null;

        for (const [modelName, label, size,, englishOnly] of this._whisperModels) {
            // Section header before first multilingual (non-auto) model
            if (!englishOnly && modelName !== 'auto' && !this._multilingualHeader) {
                const mlHeader = new PopupMenu.PopupMenuItem('  — Multilingual:', { reactive: false });
                mlHeader.label.style = 'color: #888; font-style: italic;';
                this._multilingualHeader = mlHeader;
                menu.addMenuItem(mlHeader);
            }

            // Section header before first English-only model
            if (englishOnly && !this._englishOnlyHeader) {
                const enHeader = new PopupMenu.PopupMenuItem('  — English-only (smaller, faster):', { reactive: false });
                enHeader.label.style = 'color: #888; font-style: italic;';
                this._englishOnlyHeader = enHeader;
                menu.addMenuItem(enHeader);
            }

            if (modelName === 'auto') {
                const item = new PopupMenu.PopupMenuItem('  ● Auto (optimized per mode)');
                item.connect('activate', () => {
                    this._whisperModel = 'auto';
                    this._saveModelSetting('auto');
                    this._updateModelLabel();
                    this._updateModelSelection();
                    this._log('Whisper model set to: auto');
                });
                this._modelItems.push({ item, modelName });
                menu.addMenuItem(item);
                continue;
            }

            // Non-auto models: create item, show only if installed
            const installed = this._checkModelInstalled(modelName);
            const item = new PopupMenu.PopupMenuItem(`  ✓ ${label} (${size})`);
            item.visible = installed;
            item.connect('activate', () => {
                this._whisperModel = modelName;
                this._saveModelSetting(modelName);
                this._updateModelLabel();
                this._updateModelSelection();
                this._log(`Whisper model set to: ${modelName}`);
            });
            this._modelItems.push({ item, modelName });
            menu.addMenuItem(item);
        }

        // Manager launcher — always visible
        const manageModelsItem = new PopupMenu.PopupMenuItem('  ⬇ Download more models...');
        manageModelsItem.label.style = 'color: #5af;';
        manageModelsItem.connect('activate', () => {
            this._openModelManager();
        });
        menu.addMenuItem(manageModelsItem);

        // Separator before Claude Code section
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        // Claude Code section header
        const claudeHeader = new PopupMenu.PopupMenuItem('Claude Code Post-Processing:', { reactive: false });
        claudeHeader.label.style = 'font-weight: bold;';
        menu.addMenuItem(claudeHeader);

        // Claude enabled toggle
        this._claudeSwitch = new PopupMenu.PopupSwitchMenuItem('Process with Claude (Ctrl+Insert)', this._claudeEnabled);
        this._preventMenuClose(this._claudeSwitch);
        this._claudeSwitch.connect('toggled', (item, state) => {
            if (this._updatingToggles) return;
            this._claudeEnabled = state;
            this._saveClaudeSetting(state);
            this._log(`Claude processing ${state ? 'enabled' : 'disabled'}`);
        });
        menu.addMenuItem(this._claudeSwitch);

        // Claude model - header item
        this._claudeModelHeader = new PopupMenu.PopupMenuItem('  ↳ Model: Sonnet', { reactive: false });
        this._claudeModelHeader.label.style = 'font-weight: bold; color: #888;';
        menu.addMenuItem(this._claudeModelHeader);

        // Claude model options (flat list)
        this._claudeModelItems = [];
        for (const [modelName, label, description] of this._claudeModels) {
            const item = new PopupMenu.PopupMenuItem(`      • ${label} - ${description}`);
            item.connect('activate', () => {
                this._claudeModel = modelName;
                this._saveClaudeModelSetting(modelName);
                this._updateClaudeModelLabel();
                this._updateClaudeModelSelection();
                this._log(`Claude model set to: ${modelName}`);
            });
            this._claudeModelItems.push({ item, modelName });
            menu.addMenuItem(item);
        }

        // Edit prompt menu item
        const editPromptItem = new PopupMenu.PopupMenuItem('  ↳ Edit Claude Prompt...');
        editPromptItem.connect('activate', () => {
            this._openClaudePromptEditor();
        });
        menu.addMenuItem(editPromptItem);

        // Set initial visibility for child options
        this._updatePasteToggles();
        this._updateLanguageLabel();
        this._updateLanguageSelection();
        this._updateModelLabel();
        this._updateModelSelection();
        this._updateClaudeModelLabel();
        this._updateClaudeModelSelection();
        this._updateStreamingStartupLabel();
        this._updateStartupModeSelection();

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

        // Refresh logs and installed model list when menu opens
        menu.connect('open-state-changed', (menu, open) => {
            if (open) {
                this._refreshLogDisplay();
                this._updateStatusLabel();
                this._refreshModelSection();
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

        switch (state) {
            case 'PREPARING':
                // Orange icon while audio pipeline initializes
                this._icon.style = 'color: #ffaa00;';  // Orange/Yellow
                break;
            case 'RECORDING':
                this._icon.style = 'color: #ff4444;';  // Red
                this._startCountdown();
                this._addAbortKeybinding();  // Enable Escape key during recording
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
                this._icon.icon_name = 'audio-input-microphone-symbolic';  // Restore mic icon
                this._icon.style = '';  // Default
                break;
        }
    }

    _startCountdown() {
        // Stop any existing countdown
        this._stopCountdown();

        // Initialize countdown based on mode:
        // - Streaming: 117s (3s safety buffer before 120s limit)
        // - Batch: 27s (3s safety buffer before 30s limit)
        this._remainingSeconds = this._streamingMode ? 117 : 27;

        // Replace iconBox (which contains icon + server status dot) with countdown label
        if (this._iconBox) {
            this._indicator.remove_child(this._iconBox);
        }

        // Start with green background, white text
        // Note: Don't use 'system-status-icon' style_class - it has max-width that
        // truncates "REC 117" to "REC 1..." in streaming mode
        // Use different prefix for Claude mode based on style
        let modePrefix = 'REC';
        if (this._isClaudeMode) {
            modePrefix = this._claudeStyle === 'natural' ? '💬 REC' : '🤖 REC';
        }
        this._countdownLabel = new St.Label({
            text: `${modePrefix} ${this._remainingSeconds}`,
            y_align: 2,  // Clutter.ActorAlign.CENTER
            style: 'color: white; font-weight: bold; font-size: 13px; background-color: #44ff44; padding: 2px 4px; border-radius: 3px;'
        });
        this._indicator.add_child(this._countdownLabel);

        const limit = this._streamingMode ? 120 : 30;
        this._log(`Countdown started: ${this._remainingSeconds}s (${limit}s limit, streaming: ${this._streamingMode})`);

        // Start 1-second timer
        this._recordingTimer = GLib.timeout_add_seconds(GLib.PRIORITY_DEFAULT, 1, () => {
            this._remainingSeconds--;

            if (this._countdownLabel) {
                // Update text with mode prefix based on Claude style
                let modePrefix = 'REC';
                if (this._isClaudeMode) {
                    modePrefix = this._claudeStyle === 'natural' ? '💬 REC' : '🤖 REC';
                }
                this._countdownLabel.text = `${modePrefix} ${this._remainingSeconds}`;

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

    _launchWSI() {
        try {
            // If currently in any active state, stop it instead of starting new
            if (this._currentState === 'RECORDING' ||
                this._currentState === 'PREPARING' ||
                this._currentState === 'TRANSCRIBING') {
                this._stopRecording();
                return;
            }

            // Track that this is regular mode (not Claude)
            this._isClaudeMode = false;
            this._claudeStyle = null;

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
            const langFlag = ` --language ${this._getWhisperLanguage()}`;

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
            GLib.spawn_command_line_async(command);
        } catch (e) {
            this._lastError = e.message;
            this._log(`Launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _launchWSIClaude(style = 'corporate') {
        try {
            // If currently in any active state, stop it instead of starting new
            if (this._currentState === 'RECORDING' ||
                this._currentState === 'PREPARING' ||
                this._currentState === 'TRANSCRIBING') {
                this._stopRecording();
                return;
            }

            // Track that this is Claude mode with specific style
            this._isClaudeMode = true;
            this._claudeStyle = style;

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
            const langFlag = ` --language ${this._getWhisperLanguage()}`;

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
            GLib.spawn_command_line_async(command);
        } catch (e) {
            this._lastError = e.message;
            this._log(`Launch error: ${e.message}`);
            Main.notify('STT Error', e.message);
        }
    }

    _readPasteConfig() {
        // Read ~/.config/speech-to-text/config.ini
        // Returns { defaultMode: 'with-shift'|'no-shift', ctrlVApps: [...] }
        const configFile = GLib.get_home_dir() + '/.config/speech-to-text/config.ini';
        try {
            const file = Gio.File.new_for_path(configFile);
            if (!file.query_exists(null)) {
                return { defaultMode: 'with-shift', ctrlVApps: [] };
            }
            const [success, contents] = file.load_contents(null);
            if (!success) return { defaultMode: 'with-shift', ctrlVApps: [] };

            const text = new TextDecoder().decode(contents);
            let defaultMode = 'with-shift';
            let ctrlVApps = [];

            for (const line of text.split('\n')) {
                const trimmed = line.trim();
                if (trimmed.startsWith('#') || trimmed.startsWith('[') || !trimmed) continue;
                const eqIdx = trimmed.indexOf('=');
                if (eqIdx < 0) continue;
                const key = trimmed.slice(0, eqIdx).trim();
                const value = trimmed.slice(eqIdx + 1).trim();
                if (key === 'default') defaultMode = value;
                if (key === 'ctrl_v_apps') ctrlVApps = value.split(',').map(s => s.trim()).filter(Boolean);
            }

            return { defaultMode, ctrlVApps };
        } catch (e) {
            this._log(`Paste config read error: ${e.message}`);
            return { defaultMode: 'with-shift', ctrlVApps: [] };
        }
    }

    _getPasteWithShift() {
        // Returns 1 (Ctrl+Shift+V) or 0 (Ctrl+V) based on focused window and config
        const wmClass = global.display.focus_window ? global.display.focus_window.get_wm_class() : null;
        const config = this._readPasteConfig();

        if (wmClass && config.ctrlVApps.includes(wmClass)) {
            this._log(`WM class "${wmClass}" in ctrl_v_apps → Ctrl+V`);
            return 0;
        }

        const useShift = config.defaultMode !== 'no-shift';
        this._log(`WM class "${wmClass}" → ${useShift ? 'Ctrl+Shift+V' : 'Ctrl+V'} (default: ${config.defaultMode})`);
        return useShift ? 1 : 0;
    }

    _openPasteConfigFile() {
        const configFile = GLib.get_home_dir() + '/.config/speech-to-text/config.ini';
        try {
            GLib.spawn_command_line_async(`xdg-open "${configFile}"`);
            this._log('Opening paste config file');
        } catch (e) {
            this._log(`Failed to open config file: ${e.message}`);
            Main.notify('STT Error', `Failed to open config file: ${e.message}`);
        }
    }

    _openClaudePromptEditor() {
        const promptFile = GLib.get_home_dir() + '/.config/speech-to-text/claude-prompt-corporate.txt';

        // Try to open with default text editor
        try {
            GLib.spawn_command_line_async(`xdg-open "${promptFile}"`);
            this._log('Opening Claude prompt editor');
        } catch (e) {
            this._log(`Failed to open editor: ${e.message}`);
            Main.notify('STT Error', `Failed to open editor: ${e.message}`);
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
                const binDir = GLib.get_home_dir() + '/.local/bin';
                // Kill both batch mode and streaming mode processes
                GLib.spawn_command_line_async('pkill -f "pw-record.*wfile"');
                GLib.spawn_command_line_async(`pkill -f "${binDir}/wsi-stream"`);
                GLib.spawn_command_line_async(`pkill -f "${binDir}/wsi --"`);
            }
        } catch (e) {
            this._log(`Stop error: ${e.message}`);
            const binDir = GLib.get_home_dir() + '/.local/bin';
            // Fallback to pkill - kill both batch and streaming
            GLib.spawn_command_line_async('pkill -f "pw-record.*wfile"');
            GLib.spawn_command_line_async(`pkill -f "${binDir}/wsi-stream"`);
            GLib.spawn_command_line_async(`pkill -f "${binDir}/wsi --"`);
        }
    }

    _abortRecording() {
        // Only abort if currently recording
        if (this._currentState !== 'RECORDING') {
            this._log('Abort ignored - not recording');
            return;
        }

        this._log('Aborting recording (Escape pressed)');

        // Kill the process tree forcefully with SIGKILL to prevent transcription
        try {
            // Kill wsi/wsi-stream and any child processes
            const binDir = GLib.get_home_dir() + '/.local/bin';
            GLib.spawn_command_line_async(`pkill -9 -f "${binDir}/wsi-stream"`);
            GLib.spawn_command_line_async(`pkill -9 -f "${binDir}/wsi --"`);
            GLib.spawn_command_line_async('pkill -9 -f "pw-record.*wfile"');

            // Clean up PID file
            const pidFile = '/dev/shm/stt-recording-' + GLib.get_user_name() + '.pid';
            const file = Gio.File.new_for_path(pidFile);
            if (file.query_exists(null)) {
                file.delete(null);
            }
        } catch (e) {
            this._log(`Abort cleanup error: ${e.message}`);
        }

        // Reset UI immediately
        this._currentState = 'IDLE';
        this._stopCountdown();
        this._updateIconState('IDLE');
        this._log('Recording aborted');
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

    _refreshModelSection() {
        // Re-check which models are installed and update item visibility.
        // Called each time the menu opens so newly downloaded models appear immediately.
        if (!this._modelItems) return;

        let hasMultilingual = false;
        let hasEnglishOnly = false;

        for (const { item, modelName } of this._modelItems) {
            if (modelName === 'auto') continue;
            const installed = this._checkModelInstalled(modelName);
            item.visible = installed;
            if (installed) {
                const modelInfo = this._whisperModels.find(m => m[0] === modelName);
                if (modelInfo) {
                    if (modelInfo[4]) hasEnglishOnly = true;
                    else hasMultilingual = true;
                }
            }
        }

        // Hide section headers when no models in their group are installed
        if (this._multilingualHeader) this._multilingualHeader.visible = hasMultilingual;
        if (this._englishOnlyHeader) this._englishOnlyHeader.visible = hasEnglishOnly;

        this._updateModelLabel();
        this._updateModelSelection();
    }

    _openModelManager() {
        // Launch wsi-model-manager in a terminal window
        const script = GLib.get_home_dir() + '/.local/bin/wsi-model-manager';
        this._log('Opening model manager');
        try {
            GLib.spawn_command_line_async(`gnome-terminal -- ${script}`);
        } catch (e) {
            // Fallback to xterm on minimal installs
            try {
                GLib.spawn_command_line_async(`xterm -title "Whisper Model Manager" -e ${script}`);
            } catch (e2) {
                this._log(`Cannot open terminal: ${e2.message}`);
                Main.notify('Speech to Text', 'Cannot open terminal. Run: wsi-model-manager');
            }
        }
    }

    _updateLanguageLabel() {
        if (this._languageHeader) {
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
            this._languageHeader.label.text = `Language: ${label}`;
        }
    }

    _updateLanguageSelection() {
        if (!this._languageItems) return;

        for (const {item, code} of this._languageItems) {
            if (code === this._language) {
                item.setOrnament(PopupMenu.Ornament.DOT);
            } else {
                item.setOrnament(PopupMenu.Ornament.NONE);
            }
        }
    }

    _updateModelLabel() {
        if (this._modelHeader) {
            const modelInfo = this._whisperModels.find(m => m[0] === this._whisperModel);
            if (modelInfo) {
                let label = modelInfo[1];  // Get label from array

                // For auto mode, show what it will actually use
                if (this._whisperModel === 'auto') {
                    const actualModel = this._streamingMode ? 'base' : 'small';
                    const actualInstalled = this._checkModelInstalled(actualModel);
                    const status = actualInstalled ? '✓' : '⚠';
                    label = `Auto (${status} ${actualModel} for ${this._streamingMode ? 'streaming' : 'batch'})`;
                }

                this._modelHeader.label.text = `Model: ${label}`;
            } else {
                this._modelHeader.label.text = `Model: ${this._whisperModel}`;
            }
        }
    }

    _updateModelSelection() {
        if (!this._modelItems) return;

        for (const {item, modelName} of this._modelItems) {
            if (modelName === this._whisperModel) {
                item.setOrnament(PopupMenu.Ornament.DOT);
            } else {
                item.setOrnament(PopupMenu.Ornament.NONE);
            }
        }
    }

    _updateClaudeModelLabel() {
        if (this._claudeModelHeader) {
            const modelInfo = this._claudeModels.find(m => m[0] === this._claudeModel);
            if (modelInfo) {
                this._claudeModelHeader.label.text = `  ↳ Model: ${modelInfo[1]}`;
            } else {
                this._claudeModelHeader.label.text = `  ↳ Model: ${this._claudeModel}`;
            }
        }
    }

    _updateClaudeModelSelection() {
        if (!this._claudeModelItems) return;

        for (const {item, modelName} of this._claudeModelItems) {
            if (modelName === this._claudeModel) {
                item.setOrnament(PopupMenu.Ornament.DOT);
            } else {
                item.setOrnament(PopupMenu.Ornament.NONE);
            }
        }
    }

    _updateStreamingStartupLabel() {
        if (this._streamingStartupHeader) {
            const labels = {
                'standard': 'Standard',
                'pre-buffer': 'Pre-buffer',
                'server': 'Server mode'
            };
            const label = labels[this._streamingStartupMode] || this._streamingStartupMode;
            this._streamingStartupHeader.label.text = `    ↳ Startup: ${label}`;
        }
    }

    _updateStartupModeSelection() {
        if (!this._startupModeItems) return;

        for (const {item, mode} of this._startupModeItems) {
            if (mode === this._streamingStartupMode) {
                item.setOrnament(PopupMenu.Ornament.DOT);
            } else {
                item.setOrnament(PopupMenu.Ornament.NONE);
            }
        }
    }

    _updatePasteToggles() {
        // Prevent cascading toggle events from causing menu issues
        this._updatingToggles = true;
        try {
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
            // Streaming startup options only visible when streaming is active
            if (this._streamingStartupHeader) {
                this._streamingStartupHeader.visible = this._autoPaste && this._streamingMode;
            }
            if (this._startupModeItems) {
                for (const {item} of this._startupModeItems) {
                    item.visible = this._autoPaste && this._streamingMode;
                }
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

    _saveStreamingStartupModeSetting(mode) {
        if (this._settings) {
            try {
                this._settings.set_string('streaming-startup-mode', mode);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveClaudeSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('claude-enabled', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveClaudeModelSetting(model) {
        if (this._settings) {
            try {
                this._settings.set_string('claude-model', model);
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

    _saveNotificationsSetting(enabled) {
        if (this._settings) {
            try {
                this._settings.set_boolean('show-notifications', enabled);
            } catch (e) {
                // Setting may not exist yet
            }
        }
    }

    _saveModelSetting(model) {
        if (this._settings) {
            try {
                this._settings.set_string('whisper-model', model);
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

    _checkModelInstalled(modelName) {
        // Check if a Whisper model is downloaded in huggingface cache
        // Models are stored in: ~/.cache/huggingface/hub/models--Systran--faster-whisper-{model}/
        if (modelName === 'auto') {
            return true;  // Auto is always "available" (uses whichever models are installed)
        }

        const cacheDir = GLib.get_home_dir() + '/.cache/huggingface/hub';
        const modelDir = `models--Systran--faster-whisper-${modelName}`;
        const fullPath = `${cacheDir}/${modelDir}`;

        try {
            const file = Gio.File.new_for_path(fullPath);
            return file.query_exists(null);
        } catch (e) {
            return false;
        }
    }

    _getModelDisplayName(modelName) {
        // Get display name with installation status
        const installed = this._checkModelInstalled(modelName);
        const modelInfo = this._whisperModels.find(m => m[0] === modelName);
        if (!modelInfo) return modelName;

        const label = modelInfo[1];
        const size = modelInfo[2];
        const status = installed ? '✓' : '⚠';
        return `${status} ${label} (${size})`;
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
