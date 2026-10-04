/**
 * Speech-to-Text Extension Preferences
 *
 * GTK4/Adwaita preferences window for all extension settings.
 * Opened via the "Settings..." item in the panel popup menu.
 */

import Adw from 'gi://Adw';
import Gtk from 'gi://Gtk';
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import { ExtensionPreferences } from 'resource:///org/gnome/Shell/Extensions/js/extensions/prefs.js';

// Whisper model catalogue — mirrors wsi-model-manager, which shows each model's size.
// Each entry: [modelId, huggingFaceRepo, displayLabel]; a label is a short name, since
// the dropdown truncates anything longer.
const WHISPER_MODELS = [
    ['tiny',           'Systran/faster-whisper-tiny',                    'Tiny'],
    ['base',           'Systran/faster-whisper-base',                    'Base'],
    ['small',          'Systran/faster-whisper-small',                   'Small'],
    ['medium',         'Systran/faster-whisper-medium',                  'Medium'],
    ['large-v2',       'Systran/faster-whisper-large-v2',                'Large v2'],
    ['large-v3',       'Systran/faster-whisper-large-v3',                'Large v3'],
    ['large-v3-turbo', 'mobiuslabsgmbh/faster-whisper-large-v3-turbo',   'Large v3 Turbo'],
    ['tiny.en',        'Systran/faster-whisper-tiny.en',                 'Tiny English'],
    ['base.en',        'Systran/faster-whisper-base.en',                 'Base English'],
    ['small.en',       'Systran/faster-whisper-small.en',                'Small English'],
    ['medium.en',      'Systran/faster-whisper-medium.en',               'Medium English'],
    ['distil-large-v3.5', 'distil-whisper/distil-large-v3.5-ct2',        'Distil v3.5 English'],
];

export default class SpeechToTextPreferences extends ExtensionPreferences {
    fillPreferencesWindow(window) {
        const settings = this.getSettings('org.gnome.shell.extensions.speech-to-text');
        window.set_default_size(600, 700);

        const page = new Adw.PreferencesPage({
            title: 'Speech to Text',
            icon_name: 'audio-input-microphone-symbolic',
        });
        window.add(page);

        // === Transcription ===
        const transcGroup = new Adw.PreferencesGroup({ title: 'Transcription' });
        page.add(transcGroup);

        this._addComboRow(transcGroup, settings, 'language',
            'Language', 'Speech recognition language',
            ['system', 'en'],
            ['System default', 'English']);

        // Build model list: auto + only installed models (+ current selection if missing)
        const [modelValues, modelLabels, missingLabel] = this._buildInstalledModelList(settings);
        const modelSubtitle = 'Auto: with a GPU, Distil v3.5 English for English, else Large v3 Turbo; ' +
            'without one, Small (batch) or Base (streaming). ' +
            'Only downloaded models are listed; use "Manage Whisper Models" to download more';
        this._addComboRow(transcGroup, settings, 'whisper-model',
            'Whisper Model',
            missingLabel === null
                ? modelSubtitle
                : `${missingLabel} is selected but not downloaded. ${modelSubtitle}`,
            modelValues, modelLabels);

        // === Streaming ===
        const streamGroup = new Adw.PreferencesGroup({ title: 'Streaming Mode' });
        page.add(streamGroup);

        this._addSwitchRow(streamGroup, settings, 'streaming-mode',
            'Streaming mode', 'Real-time transcription using RealtimeSTT (requires Auto-paste)');

        this._addComboRow(streamGroup, settings, 'streaming-startup-mode',
            'Startup mode',
            'Standard: load the model at each Insert, then record (~3-6 s). ' +
            'Pre-buffer: record while the model loads (~2-4 s). ' +
            'Server: a background process keeps the model loaded between recordings, ' +
            'so recording starts at once (<0.5 s) but holds GPU memory while it runs',
            ['standard', 'pre-buffer', 'server'],
            ['Standard', 'Pre-buffer', 'Server']);

        this._addSpinRow(streamGroup, settings, 'server-idle-timeout-minutes',
            'Server idle timeout (minutes)',
            'Server mode: shut the server down after this long unused; 0 = never. Applies from the next server start',
            5);

        this._addSwitchRow(streamGroup, settings, 'server-start-at-login',
            'Start the server at login',
            'Server mode: load the model when you log in, so the first recording is instant');

        // === Continuous dictation (server mode) ===
        const dictationGroup = new Adw.PreferencesGroup({
            title: 'Continuous Dictation',
            description: 'Needs Streaming mode on with Startup mode set to Server (above). Dictate for as long as you like: each phrase is transcribed while you speak, and the text is pasted when you stop, or as you go (below)',
        });
        page.add(dictationGroup);

        this._addSwitchRow(dictationGroup, settings, 'continuous-dictation',
            'Continuous dictation',
            'Off: server mode stops at the fixed streaming limit, like the other streaming modes');

        this._addSpinRow(dictationGroup, settings, 'max-recording-minutes',
            'Maximum length (minutes)',
            'Stops and transcribes after this long, in case the microphone was forgotten; the panel counts down the last minute',
            5);

        this._addSpinRow(dictationGroup, settings, 'silence-autostop-seconds',
            'Stop after silence (seconds)',
            'Stops and transcribes after this long without speech; 0 = never',
            30);

        this._addSpinRow(dictationGroup, settings, 'dictation-paste-interval-seconds',
            'Paste while dictating, every (seconds)',
            'Pastes the text transcribed so far into the focused window (outlined in red), so ' +
            'it appears as you speak; Enter only at the end. 0 = paste everything once at stop',
            30);

        // === Output ===
        const outputGroup = new Adw.PreferencesGroup({ title: 'Output' });
        page.add(outputGroup);

        this._addSwitchRow(outputGroup, settings, 'auto-paste',
            'Auto-paste at cursor', 'Automatically paste transcription at the cursor position');

        this._addSwitchRow(outputGroup, settings, 'auto-enter',
            'Send Enter after paste', 'Press Enter after pasting the transcription');

        this._addSwitchRow(outputGroup, settings, 'wrap-marker',
            'Wrap with marker', 'Surround transcription with speech-to-text:"…"');

        this._addSwitchRow(outputGroup, settings, 'show-notifications',
            'Show notifications', 'Show desktop notifications for transcription events');

        this._addComboRow(outputGroup, settings, 'paste-default-mode',
            'Paste shortcut for other apps',
            'Chosen at each paste for the window focused then. Terminals always get ' +
            'Ctrl+Shift+V; apps in the list below always get Ctrl+V; every other app gets ' +
            'this. With debug logging on, the log names each paste\'s window class',
            ['no-shift', 'with-shift'],
            ['Ctrl+V', 'Ctrl+Shift+V']);

        this._addListRow(outputGroup, settings, 'paste-ctrl-v-apps', 'Apps using Ctrl+V');
        this._addListRow(outputGroup, settings, 'paste-save-apps',
            'Apps to save after pasting (Ctrl+S; never terminals)');

        // === Claude Code ===
        const claudeGroup = new Adw.PreferencesGroup({ title: 'Claude Code Post-Processing' });
        page.add(claudeGroup);

        this._addSwitchRow(claudeGroup, settings, 'claude-enabled',
            'Enable Claude processing', 'Post-process transcription with Claude Code (Ctrl+Insert)');

        this._addComboRow(claudeGroup, settings, 'claude-model',
            'Claude model',
            'Claude model used for post-processing. Sonnet balances speed and quality; ' +
            'Opus gives the best quality, more slowly; Haiku is fastest',
            ['sonnet', 'opus', 'haiku'],
            ['Sonnet', 'Opus', 'Haiku']);

        const tokenNames = this._listClaudeTokens(settings.get_string('claude-token'));
        this._addComboRow(claudeGroup, settings, 'claude-token',
            'Claude token',
            'The account post-processing runs as: a named token from ~/.claude-tokens/ccy/tokens ' +
            '(ccy --create-token makes one; the newest unexpired one is used). Desktop login is ' +
            'parked while a cc named-token session runs, so post-processing fails then',
            ['', ...tokenNames],
            ['Desktop login', ...tokenNames]);

        this._addPromptRow(claudeGroup,
            'Edit Corporate Prompt',
            'Professional/corporate style — used with Ctrl+Insert',
            'claude-prompt-corporate.txt');

        this._addPromptRow(claudeGroup,
            'Edit Natural Prompt',
            'Casual/natural style — used with Alt+Insert',
            'claude-prompt-natural.txt');

        // === Debug ===
        const debugGroup = new Adw.PreferencesGroup({ title: 'Debug' });
        page.add(debugGroup);

        this._addSwitchRow(debugGroup, settings, 'debug-mode',
            'Debug logging', 'Write debug output to ~/.local/share/speech-to-text/debug.log');
    }

    // -------------------------------------------------------------------------
    // Helpers
    // -------------------------------------------------------------------------

    /**
     * Build the values/labels arrays for the whisper-model combo row, and the label
     * of the saved model when it is not installed (else null).
     * Always includes 'auto'. Only adds models _isModelInstalled finds. If the
     * currently saved model is not installed it is still included, so the setting is
     * never silently lost, and the caller says so in the row's subtitle.
     */
    _buildInstalledModelList(settings) {
        const values = ['auto'];
        const labels = ['Auto'];
        const currentModel = settings.get_string('whisper-model');
        let missingLabel = null;

        for (const [id, repo, label] of WHISPER_MODELS) {
            const installed = this._isModelInstalled(repo);
            if (installed || id === currentModel) {
                values.push(id);
                labels.push(label);
            }
            if (!installed && id === currentModel)
                missingLabel = label;
        }

        return [values, labels, missingLabel];
    }

    /**
     * Return true if a snapshot of the given HuggingFace repo in the local cache
     * holds model.bin. An interrupted download leaves a snapshot with the small
     * config files and no model.bin, which WhisperModel cannot load.
     * repo format: "Org/model-name"  →  cache: models--Org--model-name/snapshots/<rev>/
     */
    _isModelInstalled(repoId) {
        const cacheBase = GLib.get_home_dir() + '/.cache/huggingface/hub';
        const snapshotsPath = `${cacheBase}/models--${repoId.replace('/', '--')}/snapshots`;
        const dir = Gio.File.new_for_path(snapshotsPath);
        if (!dir.query_exists(null))
            return false;
        try {
            const enumerator = dir.enumerate_children(
                'standard::name', Gio.FileQueryInfoFlags.NONE, null);
            let installed = false;
            let info;
            while (!installed && (info = enumerator.next_file(null)) !== null)
                installed = dir.get_child(info.get_name()).get_child('model.bin').query_exists(null);
            enumerator.close(null);
            return installed;
        } catch (e) {
            // A cache that cannot be read offers nothing, and says why in the journal.
            logError(e, `STT prefs: reading ${snapshotsPath}`);
            return false;
        }
    }

    /** Add a clickable row that opens a Claude prompt file in the default text editor. */
    _addPromptRow(group, title, subtitle, filename) {
        const row = new Adw.ActionRow({ title, subtitle, activatable: true });
        row.add_suffix(new Gtk.Image({ icon_name: 'go-next-symbolic' }));
        row.connect('activated', () => {
            const f = `${GLib.get_home_dir()}/.config/speech-to-text/${filename}`;
            try {
                Gio.AppInfo.launch_default_for_uri(`file://${f}`, null);
            } catch (e) {
                // No default text editor configured (or launch failed) — log, don't swallow.
                logError(e, 'STT prefs: opening prompt file');
            }
        });
        group.add(row);
    }

    /**
     * The token names in ~/.claude-tokens/ccy/tokens (NAME.YYYY-MM-DD.token), sorted, plus
     * the saved one if its file is gone, so the setting is never silently changed.
     */
    _listClaudeTokens(saved) {
        const names = new Set();
        const dir = Gio.File.new_for_path(`${GLib.get_home_dir()}/.claude-tokens/ccy/tokens`);
        if (dir.query_exists(null)) {
            try {
                const enumerator = dir.enumerate_children(
                    'standard::name', Gio.FileQueryInfoFlags.NONE, null);
                let info;
                while ((info = enumerator.next_file(null)) !== null) {
                    const match = info.get_name().match(/^(.+)\.\d{4}-\d{2}-\d{2}\.token$/);
                    if (match)
                        names.add(match[1]);
                }
                enumerator.close(null);
            } catch (e) {
                logError(e, 'STT prefs: listing Claude tokens');
            }
        }
        if (saved)
            names.add(saved);
        return [...names].sort();
    }

    /** A comma-separated list of window classes, saved when its apply button is pressed. */
    _addListRow(group, settings, key, title) {
        const row = new Adw.EntryRow({
            title,
            text: settings.get_string(key),
            show_apply_button: true,
        });
        row.connect('apply', () => {
            settings.set_string(key, row.text);
        });
        settings.connect(`changed::${key}`, () => {
            const val = settings.get_string(key);
            if (row.text !== val) row.text = val;
        });
        group.add(row);
        return row;
    }

    /** A number row bound to an integer key; its range is the schema's, not a copy. */
    _addSpinRow(group, settings, key, title, subtitle, step) {
        const [, [min, max]] = settings.settings_schema.get_key(key).get_range().recursiveUnpack();
        const row = Adw.SpinRow.new_with_range(min, max, step);
        row.title = title;
        row.subtitle = subtitle;
        settings.bind(key, row, 'value', Gio.SettingsBindFlags.DEFAULT);
        group.add(row);
        return row;
    }

    _addSwitchRow(group, settings, key, title, subtitle) {
        const row = new Adw.SwitchRow({ title, subtitle });
        settings.bind(key, row, 'active', Gio.SettingsBindFlags.DEFAULT);
        group.add(row);
        return row;
    }

    _addComboRow(group, settings, key, title, subtitle, values, labels) {
        const row = new Adw.ComboRow({ title, subtitle });
        const model = new Gtk.StringList();
        for (const label of labels)
            model.append(label);
        row.model = model;

        // Set initial selection — connect handler AFTER to avoid spurious write-back
        const currentVal = settings.get_string(key);
        const idx = values.indexOf(currentVal);
        row.selected = idx >= 0 ? idx : 0;

        row.connect('notify::selected', () => {
            settings.set_string(key, values[row.selected]);
        });

        // Keep in sync if changed externally (e.g. from the panel popup toggles)
        settings.connect(`changed::${key}`, () => {
            const val = settings.get_string(key);
            const newIdx = values.indexOf(val);
            if (newIdx >= 0 && row.selected !== newIdx)
                row.selected = newIdx;
        });

        group.add(row);
        return row;
    }
}
