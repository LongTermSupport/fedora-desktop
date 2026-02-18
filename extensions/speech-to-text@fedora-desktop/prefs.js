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

        this._addComboRow(transcGroup, settings, 'whisper-model',
            'Whisper Model', 'Download missing models via "Manage Whisper Models" in the panel menu',
            ['auto', 'tiny', 'base', 'small', 'medium', 'large-v2', 'large-v3', 'large-v3-turbo',
                'tiny.en', 'base.en', 'small.en', 'medium.en'],
            ['Auto (optimized per mode)', 'Tiny (~75MB)', 'Base (~142MB)', 'Small (~466MB)',
                'Medium (~1.5GB)', 'Large v2 (~3GB)', 'Large v3 (~3GB)', 'Large v3 Turbo (~1.6GB)',
                'Tiny English (~41MB)', 'Base English (~77MB)', 'Small English (~252MB)', 'Medium English (~789MB)']);

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

        const promptRow = new Adw.ActionRow({
            title: 'Edit Claude Prompt',
            subtitle: 'Open the corporate-style prompt template in a text editor',
            activatable: true,
        });
        promptRow.add_suffix(new Gtk.Image({ icon_name: 'go-next-symbolic' }));
        promptRow.connect('activated', () => {
            const f = GLib.get_home_dir() + '/.config/speech-to-text/claude-prompt-corporate.txt';
            try {
                Gio.AppInfo.launch_default_for_uri(`file://${f}`, null);
            } catch (_e) {
                // Ignore — no default app configured for text files
            }
        });
        claudeGroup.add(promptRow);

        // === Debug ===
        const debugGroup = new Adw.PreferencesGroup({ title: 'Debug' });
        page.add(debugGroup);

        this._addSwitchRow(debugGroup, settings, 'debug-mode',
            'Debug logging', 'Write debug output to ~/.local/share/speech-to-text/debug.log');
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
