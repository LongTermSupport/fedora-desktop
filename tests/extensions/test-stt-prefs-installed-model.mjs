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
