/**
 * A consumer install has no Claude post-processing, and the speech-to-text panel and Settings
 * hide every item that needs it (Plan 00228 Task 6.5, the owner's decision).
 *
 *     node --test tests/extensions/test-stt-consumer-post-processing.mjs
 *
 * Post-processing (Ctrl+Insert, Alt+Insert, the menu's Post-Process Last Transcription items,
 * Create Article and the Settings' Claude group) runs as a token from ccy's pool, and a
 * consumer box has no ccy. Whether the install offers it is one fact the play knows, the
 * install's audience: play-speech-to-text.yml compiles it into the schema as the default of
 * post-processing-offered (a schema override), and the extension reads that default, never a
 * value a user could set, so nothing second-guesses the play. Plain dictation (Insert) stays.
 */

import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const EXT = new URL('../../extensions/speech-to-text@fedora-desktop/', import.meta.url);
const {OFFERED_KEY, postProcessingOffered, recordKeys} = await import(new URL('postProcess.js', EXT).href);
const {default: SpeechToTextPreferences} = await import(new URL('prefs.js', EXT).href);
const {Adw, Gio} = await import('./gi-stubs.mjs');
Gio.SettingsBindFlags ??= {GET: 1, INVERT_BOOLEAN: 4};

/** Settings whose schema has (or lacks) the key, with a compiled default and a user value. */
function settings({hasKey = true, compiled = true, user = !compiled} = {}) {
    return {
        settings_schema: {has_key: key => hasKey && key === OFFERED_KEY},
        get_default_value: key => {
            assert.equal(key, OFFERED_KEY);
            return {get_boolean: () => compiled};
        },
        get_boolean: () => user,
        get_string: () => '',
        bind: () => {},
    };
}

test('the key is post-processing-offered', () => {
    assert.equal(OFFERED_KEY, 'post-processing-offered');
});

test('offered when the compiled default says so (a developer install)', () => {
    assert.equal(postProcessingOffered(settings({compiled: true})), true);
});

test('not offered when the compiled default says not (a consumer install), whatever a user set', () => {
    assert.equal(postProcessingOffered(settings({compiled: false, user: true})), false);
});

test('not offered without a schema, or with a schema that lacks the key', () => {
    assert.equal(postProcessingOffered(null), false);
    assert.equal(postProcessingOffered(settings({hasKey: false})), false);
});

test('a developer install binds all three record keys; a consumer install binds Insert only', () => {
    assert.deepEqual(recordKeys(true), [
        {name: 'toggle-recording', kind: 'dictation', style: null},
        {name: 'toggle-recording-claude', kind: 'claude', style: 'corporate'},
        {name: 'toggle-recording-claude-natural', kind: 'claude', style: 'natural'},
    ]);
    assert.deepEqual(recordKeys(false), [{name: 'toggle-recording', kind: 'dictation', style: null}]);
});

test('the shipped schema has the key, offered by default', () => {
    const xml = readFileSync(new URL('schemas/org.gnome.shell.extensions.speech-to-text.gschema.xml', EXT), 'utf8');
    assert.match(xml, /<key name="post-processing-offered" type="b">\s*<default>true<\/default>/);
});

test('the play compiles the install audience into the schema, and reloads on a change of it', () => {
    const play = readFileSync(new URL('../../playbooks/imports/optional/common/play-speech-to-text.yml',
        import.meta.url), 'utf8');
    assert.match(play, /post-processing-offered=\{\{ 'true' if install_audience == 'developer' else 'false' \}\}/);
    assert.match(play, /dest: "\{\{ extension_dest \}\}\/schemas\/fedora-desktop-audience\.gschema\.override"/);
    assert.match(play, /register: schema_override/);
    assert.match(play, /changed_when: schema_copy\.changed or schema_override\.changed/);
    assert.match(play, /extension_files_changed: "\{\{ extension_copy\.changed or schema_copy\.changed or schema_override\.changed \}\}"/);
});

/** The Settings window's group titles, built over stub Adwaita widgets. */
function prefsGroups(offered) {
    const groups = [];
    Object.assign(Adw, {
        PreferencesPage: class { add(group) { groups.push(group.title); } },
        PreferencesGroup: class { constructor({title}) { this.title = title; } },
    });
    const prefs = Object.create(SpeechToTextPreferences.prototype);
    const row = {};
    Object.assign(prefs, {
        getSettings: () => settings({compiled: offered}),
        _buildInstalledModelList: () => [['auto'], ['Auto'], null],
        _showAutoSuggestion: () => {},
        _addComboRow: () => row,
        _addSwitchRow: () => row,
        _addSpinRow: () => row,
        _addListRow: () => row,
        _addPromptRow: () => row,
        _listClaudeTokens: () => [],
    });
    prefs.fillPreferencesWindow({set_default_size: () => {}, add: () => {}});
    return groups;
}

test('Settings shows the Claude group on a developer install only', () => {
    assert.ok(prefsGroups(true).includes('Claude Code Post-Processing'));
    const consumer = prefsGroups(false);
    assert.ok(!consumer.includes('Claude Code Post-Processing'));
    assert.ok(consumer.includes('Transcription') && consumer.includes('Debug'));
});
