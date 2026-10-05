/**
 * Speech-to-text Settings list a Whisper model as installed only when its weights are
 * on disk (Plan 00156).
 *
 *     node --test tests/extensions/test-stt-prefs-installed-model.mjs
 *
 * An interrupted download leaves a cache snapshot holding the small config files and no
 * model.bin. Settings once counted any snapshot as installed, so the `auto` model was
 * offered as present while the warm server could not load it. Driven against the
 * shipped prefs.js, with the cache laid out as stub files.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {default: SpeechToTextPreferences} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/prefs.js', import.meta.url).href);
const {GLIB_FILES} = await import('./gi-stubs.mjs');

const REPO = 'distil-whisper/distil-large-v3.5-ct2';
const SNAPSHOT = '/stub/home/.cache/huggingface/hub/models--distil-whisper--distil-large-v3.5-ct2/snapshots/9793ccc0';

function installed(files) {
    GLIB_FILES.clear();
    for (const file of files)
        GLIB_FILES.set(`${SNAPSHOT}/${file}`, '');
    return new SpeechToTextPreferences({}).__proto__._isModelInstalled.call({}, REPO);
}

test('a snapshot with only config files is not installed', () => {
    assert.equal(installed(['config.json', 'tokenizer.json', 'vocabulary.json']), false);
});

test('a snapshot holding model.bin is installed', () => {
    assert.equal(installed(['config.json', 'model.bin']), true);
});

test('a model with no cache directory is not installed', () => {
    assert.equal(installed([]), false);
});

// No model is downloaded by default (Plan 00156 Task 2.4), so the Whisper Model row says
// which one Auto picks on this machine: the one to download.
const {SPAWNS, SPAWN_OUTCOME} = await import('./gi-stubs.mjs');

function suggestion({language = 'en', streaming = true, stdout = '', successful = true, stderr = ''}) {
    SPAWNS.length = 0;
    Object.assign(SPAWN_OUTCOME, {successful, stdout, stderr, exitStatus: successful ? 0 : 1});
    const row = {subtitle: 'BASE'};
    const settings = {
        get_string: key => ({language})[key],
        get_boolean: key => ({'streaming-mode': streaming})[key],
    };
    SpeechToTextPreferences.prototype._showAutoSuggestion.call({}, row, settings, 'BASE');
    Object.assign(SPAWN_OUTCOME, {successful: true, stdout: '', stderr: '', exitStatus: 0});
    return {row, argv: SPAWNS[0]?.argv};
}

test('the resolver is asked for the suggestion, without its disk check', () => {
    const {argv} = suggestion({language: 'de', streaming: false, stdout: 'large-v3-turbo\n'});
    assert.deepEqual(argv, ['/stub/home/.local/bin/wsi-resolve-model', '--suggest',
        '--mode', 'batch', '--language', 'de', 'auto']);
});

test('the row names the model Auto picks, by its label', () => {
    const {row} = suggestion({stdout: 'distil-whisper/distil-large-v3.5-ct2\n'});
    assert.equal(row.subtitle, 'On this machine Auto picks Distil v3.5 English. BASE');
});

test('a resolver that cannot choose is shown with its reason', () => {
    const {row} = suggestion({successful: false,
        stderr: 'wsi-resolve-model: this machine has NVIDIA GPUs but CTranslate2 counts 0\n'});
    assert.match(row.subtitle, /^Auto cannot choose a model here: .*counts 0\. BASE$/);
});
