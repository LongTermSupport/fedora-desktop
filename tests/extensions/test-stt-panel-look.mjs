/**
 * What the speech-to-text panel looks like in each state (Plan 00171).
 *
 *     node --test tests/extensions/test-stt-panel-look.mjs
 *
 * The owner's rules: green only ever means SUCCESS, so the recording badge is red like
 * the recording mic; a start queued behind a finishing recorder looks different from the
 * recorder finishing; a press that cancels a start shows that it was taken.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {lookFor, recordingBadgeColor, articleBadgeColor, COLOURS} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/panelLook.js', import.meta.url).href);

const STATES = ['IDLE', 'LAUNCHING', 'WAITING', 'QUEUED', 'FINISHING', 'CANCELLING', 'UNOWNED',
    'PREPARING', 'RECORDING', 'STOPPING', 'TRANSCRIBING', 'SUCCESS', 'ERROR'];

test('every state has a look', () => {
    for (const state of STATES) {
        const look = lookFor(state);
        assert.ok(look.icon, `${state} has no icon`);
        assert.equal(typeof look.colour, 'string', `${state} has no colour`);
    }
});

test('an unknown state looks ready, never like a recording', () => {
    assert.deepEqual(lookFor('SOMETHING-NEW'), lookFor('IDLE'));
});

test('green is SUCCESS and nothing else', () => {
    for (const state of STATES)
        assert.equal(lookFor(state).colour === COLOURS.green, state === 'SUCCESS', state);
});

test('the recording badge is red, like the recording mic, and never green', () => {
    assert.equal(recordingBadgeColor({behind: false, secondsLeft: null}), COLOURS.red);
    assert.equal(recordingBadgeColor({behind: false, secondsLeft: null}), lookFor('RECORDING').colour);
    assert.equal(articleBadgeColor(), COLOURS.red);
});

test('the recording badge turns amber when transcription falls behind or the limit is near', () => {
    assert.equal(recordingBadgeColor({behind: true, secondsLeft: null}), COLOURS.amber);
    assert.equal(recordingBadgeColor({behind: false, secondsLeft: 10}), COLOURS.amber);
    assert.equal(recordingBadgeColor({behind: false, secondsLeft: 11}), COLOURS.red);
});

test('a queued start looks different from the recorder finishing', () => {
    assert.equal(lookFor('QUEUED').icon, 'hourglass');
    assert.equal(lookFor('FINISHING').icon, 'hourglass');
    assert.notEqual(lookFor('QUEUED').colour, lookFor('FINISHING').colour);
});

test('every wait for a recording the owner asked for looks the same', () => {
    for (const state of ['LAUNCHING', 'WAITING'])
        assert.deepEqual(lookFor(state), lookFor('QUEUED'), state);
});

test('a cancelled start looks different from every wait', () => {
    for (const state of ['LAUNCHING', 'WAITING', 'QUEUED', 'FINISHING', 'IDLE'])
        assert.notDeepEqual(lookFor('CANCELLING'), lookFor(state), state);
});

test('a recorder the panel did not start never looks ready', () => {
    assert.notDeepEqual(lookFor('UNOWNED'), lookFor('IDLE'));
});
