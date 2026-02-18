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

// Whisper model catalogue — mirrors wsi-model-manager.
// Each entry: [modelId, huggingFaceRepo, displayLabel]
const WHISPER_MODELS = [
    ['tiny',           'Systran/faster-whisper-tiny',                    'Tiny (~75MB)'],
    ['base',           'Systran/faster-whisper-base',                    'Base (~142MB)'],
    ['small',          'Systran/faster-whisper-small',                   'Small (~466MB)'],
    ['medium',         'Systran/faster-whisper-medium',                  'Medium (~1.5GB)'],
    ['large-v2',       'Systran/faster-whisper-large-v2',                'Large v2 (~3GB)'],
    ['large-v3',       'Systran/faster-whisper-large-v3',                'Large v3 (~3GB)'],
    ['large-v3-turbo', 'mobiuslabsgmbh/faster-whisper-large-v3-turbo',   'Large v3 Turbo (~1.6GB)'],
    ['tiny.en',        'Systran/faster-whisper-tiny.en',                 'Tiny English (~41MB)'],
    ['base.en',        'Systran/faster-whisper-base.en',                 'Base English (~77MB)'],
    ['small.en',       'Systran/faster-whisper-small.en',                'Small English (~252MB)'],
    ['medium.en',      'Systran/faster-whisper-medium.en',               'Medium English (~789MB)'],
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
        const [modelValues, modelLabels] = this._buildInstalledModelList(settings);
        this._addComboRow(transcGroup, settings, 'whisper-model',
            'Whisper Model',
            'Only downloaded models shown — use "Manage Whisper Models" to download more',
            modelValues, modelLabels);

        // === Streaming ===
        const streamGroup = new Adw.PreferencesGroup({ title: 'Streaming Mode' });
        page.add(streamGroup);

        this._addSwitchRow(streamGroup, settings, 'streaming-mode',
            'Streaming mode', 'Real-time transcription using RealtimeSTT (requires Auto-paste)');

        this._addComboRow(streamGroup, settings, 'streaming-startup-mode',
            'Startup mode', 'How the streaming server initialises',
            ['standard', 'pre-buffer', 'server'],
            ['Standard — load then start (~3-6s)',
                'Pre-buffer — record while loading (~2-4s)',
                'Server mode — persistent server (<0.5s, uses more memory)']);

        // === Output ===
        const outputGroup = new Adw.PreferencesGroup({ title: 'Output' });
        page.add(outputGroup);

        this._addSwitchRow(outputGroup, settings, 'auto-paste',
            'Auto-paste at cursor', 'Automatically paste transcription at the cursor position');

        this._addSwitchRow(outputGroup, settings, 'auto-enter',
            'Send Enter after paste', 'Press Enter after pasting the transcription');

        this._addSwitchRow(outputGroup, settings, 'wrap-marker',
            'Wrap with marker', 'Surround transcription with [STT]...[/STT] markers');

        this._addSwitchRow(outputGroup, settings, 'show-notifications',
            'Show notifications', 'Show desktop notifications for transcription events');

        this._addComboRow(outputGroup, settings, 'paste-default-mode',
            'Paste shortcut', 'Key combo used to paste transcription (find your app\'s WM_CLASS with: xprop WM_CLASS)',
            ['with-shift', 'no-shift'],
            ['Ctrl+Shift+V (default — correct for terminals)',
                'Ctrl+V (correct for browsers and most GUI apps)']);

        const ctrlVRow = new Adw.EntryRow({
            title: 'Apps using Ctrl+V',
            text: settings.get_string('paste-ctrl-v-apps'),
            show_apply_button: true,
        });
        ctrlVRow.connect('apply', () => {
            settings.set_string('paste-ctrl-v-apps', ctrlVRow.text);
        });
        settings.connect('changed::paste-ctrl-v-apps', () => {
            const val = settings.get_string('paste-ctrl-v-apps');
            if (ctrlVRow.text !== val) ctrlVRow.text = val;
        });
        outputGroup.add(ctrlVRow);

        // === Claude Code ===
        const claudeGroup = new Adw.PreferencesGroup({ title: 'Claude Code Post-Processing' });
        page.add(claudeGroup);

        this._addSwitchRow(claudeGroup, settings, 'claude-enabled',
            'Enable Claude processing', 'Post-process transcription with Claude Code (Ctrl+Insert)');

        this._addComboRow(claudeGroup, settings, 'claude-model',
            'Claude model', 'Claude model used for post-processing',
            ['sonnet', 'opus', 'haiku'],
            ['Sonnet — balanced speed and quality',
                'Opus — best quality, slower',
                'Haiku — fastest']);

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
     * Build the values/labels arrays for the whisper-model combo row.
     * Always includes 'auto'. Only adds models whose HuggingFace cache
     * snapshot directory exists and is non-empty.  If the currently saved
     * model is not installed it is still included (labelled "not installed")
     * so the setting is never silently lost.
     */
    _buildInstalledModelList(settings) {
        const values = ['auto'];
        const labels = ['Auto (optimized per mode)'];
        const currentModel = settings.get_string('whisper-model');

        for (const [id, repo, label] of WHISPER_MODELS) {
            const installed = this._isModelInstalled(repo);
            if (installed) {
                values.push(id);
                labels.push(label);
            } else if (id === currentModel) {
                // Keep the saved value even if not installed so we don't lose it
                values.push(id);
                labels.push(`${label} — not installed`);
            }
        }

        return [values, labels];
    }

    /**
     * Return true if the given HuggingFace repo has a non-empty snapshots
     * directory in the local cache (meaning the model was fully downloaded).
     * repo format: "Org/model-name"  →  cache: models--Org--model-name/snapshots/
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
            const hasChild = enumerator.next_file(null) !== null;
            enumerator.close(null);
            return hasChild;
        } catch (_e) {
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
            } catch (_e) {
                // No default text editor configured — ignore silently
            }
        });
        group.add(row);
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
