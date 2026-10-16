/**
 * The speech-to-text panel's Post-Process Last Transcription items, and its check that
 * the record keys were registered (Plan 00208 Phase 3).
 *
 *     node --test tests/extensions/test-stt-post-process.mjs
 *
 * Ctrl+Insert and Alt+Insert choose Claude post-processing only at the start of a
 * recording, and only by key. The menu now offers each style as an item of its own, on
 * the last transcription, after the fact (the owner's rule: keys are fine, never the only
 * way). The work is wsi-postprocess's; the panel runs it, puts Claude's text on the
 * clipboard and says what happened, every time: a run that fails or has nothing to work
 * on is a notification, never a silent no-op.
 *
 * A record key the shell refused (Main.wm.addKeybinding answers NONE) or that has no key
 * set was reported only in the debug log, which is off by default: the press then did
 * nothing at all. It is now a notification at enable.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {POST_PROCESS_STYLES, postProcessArgv, LastTranscriptPostProcessor, unusableKeybindings} =
    await import(new URL('../../extensions/speech-to-text@fedora-desktop/postProcess.js',
        import.meta.url).href);

const BIN = '/stub/home/.local/bin/';

/** A processor over fakes: `runs` holds each started run, finished by the test. */
function processor({spawnThrows = null} = {}) {
    const fakes = {runs: [], clipboard: [], notes: [], logs: []};
    fakes.processor = new LastTranscriptPostProcessor({
        bin: BIN,
        run: (argv, done) => {
            if (spawnThrows)
                throw new Error(spawnThrows);
            fakes.runs.push({argv, done});
        },
        setClipboard: text => fakes.clipboard.push(text),
        notify: message => fakes.notes.push(message),
        log: message => fakes.logs.push(message),
    });
    return fakes;
}

test('each post-processing style is a menu item of its own, named with the key that records in it', () => {
    assert.deepEqual(POST_PROCESS_STYLES.map(s => s.style), ['corporate', 'natural']);
    for (const {label, key} of POST_PROCESS_STYLES) {
        assert.match(label, /^Post-Process Last Transcription/);
        assert.ok(label.includes(key), `${label} does not name ${key}`);
    }
    assert.deepEqual(POST_PROCESS_STYLES.map(s => s.key), ['Ctrl+Insert', 'Alt+Insert']);
});

test('the argv is wsi-postprocess with the style and model, and --debug only when debugging', () => {
    assert.deepEqual(postProcessArgv(BIN, 'natural', 'opus', false),
        [`${BIN}wsi-postprocess`, '--style', 'natural', '--model', 'opus']);
    assert.deepEqual(postProcessArgv(BIN, 'corporate', 'sonnet', true),
        [`${BIN}wsi-postprocess`, '--style', 'corporate', '--model', 'sonnet', '--debug']);
});

test('a run says it started, and its text goes to the clipboard with a notification', () => {
    const f = processor();
    f.processor.start('corporate', 'sonnet', false);
    assert.equal(f.runs.length, 1);
    assert.match(f.notes.at(-1), /Post-processing the last transcription \(Corporate\)/);
    f.runs[0].done(0, 'Polished text.\n', '');
    assert.deepEqual(f.clipboard, ['Polished text.']);
    assert.match(f.notes.at(-1), /Corporate.*on the clipboard: Ctrl\+V/s);
    assert.match(f.notes.at(-1), /Polished text\./);
    assert.equal(f.processor.busy, false);
});

test('a failure says why, from wsi-postprocess, and touches nothing on the clipboard', () => {
    const f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done(1, '', 'wsi-postprocess: Claude post-processing failed: Not logged in\n');
    assert.deepEqual(f.clipboard, []);
    assert.equal(f.notes.at(-1), 'Post-processing (Natural) failed: Claude post-processing failed: Not logged in');
    assert.equal(f.processor.busy, false);
});

test('no last transcription is a notification that says so, not a silent no-op', () => {
    const f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done(1, '', 'wsi-postprocess: No transcription yet: dictate one first\n');
    assert.equal(f.notes.at(-1), 'Post-processing (Natural) failed: No transcription yet: dictate one first');
});

test('a run that fails without a word, or succeeds with no text, is still reported', () => {
    let f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done(127, '', '');
    assert.equal(f.notes.at(-1), 'Post-processing (Natural) failed: wsi-postprocess ended with exit status 127 ' +
        'and said nothing (re-run play-speech-to-text.yml if it is not installed)');
    f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done(0, '\n', '');
    assert.deepEqual(f.clipboard, []);
    assert.match(f.notes.at(-1), /failed: wsi-postprocess printed no text/);
    f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done('signal 9', '', '');
    assert.match(f.notes.at(-1), /failed: wsi-postprocess ended with signal 9 and said nothing/);
    f = processor();
    f.processor.start('natural', 'sonnet', false);
    f.runs[0].done(null, '', '', 'the pipe broke');
    assert.equal(f.notes.at(-1), 'Post-processing (Natural) failed: the pipe broke');
});

test('a run that cannot start is reported, and the next can start', () => {
    const f = processor({spawnThrows: 'No such file or directory'});
    f.processor.start('corporate', 'sonnet', false);
    assert.equal(f.notes.at(-1), 'Post-processing (Corporate) could not start: No such file or directory');
    assert.equal(f.processor.busy, false);
});

test('one run at a time: a second press while one runs says so and starts nothing', () => {
    const f = processor();
    f.processor.start('corporate', 'sonnet', false);
    f.processor.start('natural', 'sonnet', false);
    assert.equal(f.runs.length, 1);
    assert.match(f.notes.at(-1), /already running \(Corporate\)/);
});

test('after destroy, a run that ends late changes nothing', () => {
    const f = processor();
    f.processor.start('corporate', 'sonnet', false);
    const notes = f.notes.length;
    f.processor.destroy();
    f.runs[0].done(0, 'Late.', '');
    assert.deepEqual(f.clipboard, []);
    assert.equal(f.notes.length, notes);
});

test('a record key the shell refused, or with no key set, is named; registered ones are not', () => {
    const NONE = 0;
    const results = [
        {name: 'toggle-recording', action: 7, accels: ['Insert']},
        {name: 'toggle-recording-claude', action: NONE, accels: ['<Primary>Insert']},
        {name: 'toggle-recording-claude-natural', action: 9, accels: []},
    ];
    assert.deepEqual(unusableKeybindings(results, NONE), [
        'toggle-recording-claude (<Primary>Insert): the shell refused it',
        'toggle-recording-claude-natural: no key is set',
    ]);
    assert.deepEqual(unusableKeybindings(results.slice(0, 1), NONE), []);
});
