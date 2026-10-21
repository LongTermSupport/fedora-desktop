/**
 * What the speech-to-text panel looks like in each state (Plan 00171).
 *
 *     node --test tests/extensions/test-stt-panel-look.mjs
 *
 * The owner's rules: green only ever means SUCCESS, so the recording badge is red like
 * the recording mic; a start you asked for looks different from the recorder finishing;
 * a press that cancels a start shows that it was taken, and one that does nothing flashes
 * BLOCKED.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {lookFor, isKnownState, recordingBadgeColor, articleBadgeColor, shownState, claudeEmoji, COLOURS} =
    await import(new URL('../../extensions/speech-to-text@fedora-desktop/panelLook.js', import.meta.url).href);

const STATES = ['IDLE', 'LAUNCHING', 'WAITING', 'FINISHING', 'BLOCKED', 'CANCELLING', 'UNOWNED',
    'PREPARING', 'RECORDING', 'STOPPING', 'TRANSCRIBING', 'CLAUDE_PROCESSING', 'SUCCESS', 'ERROR'];

test('every state has a look', () => {
    for (const state of STATES) {
        const look = lookFor(state);
        assert.ok(look.icon, `${state} has no icon`);
        assert.equal(typeof look.colour, 'string', `${state} has no colour`);
    }
});

test('an unknown state never looks ready (Decision 2): it is the please-wait hourglass', () => {
    assert.notDeepEqual(lookFor('SOMETHING-NEW'), lookFor('IDLE'));
    assert.deepEqual(lookFor('SOMETHING-NEW'), lookFor('FINISHING'));
    assert.equal(isKnownState('SOMETHING-NEW'), false);
    for (const state of STATES)
        assert.equal(isKnownState(state), true, state);
});

// Plan 00179: while the server transcribes recordings, the resting panel says so with a look
// of its own; Insert still dictates (the file job pauses), so every dictation state wins.
test('a file job shows its own look in place of the idle mic, and nothing else changes', () => {
    assert.equal(shownState('IDLE', true), 'FILES');
    assert.equal(shownState('IDLE', false), 'IDLE');
    for (const state of STATES.filter(s => s !== 'IDLE'))
        assert.equal(shownState(state, true), state, state);
    assert.equal(isKnownState('FILES'), true);
    const files = lookFor('FILES');
    for (const state of STATES) {
        assert.notEqual(files.icon, lookFor(state).icon, `FILES shares ${state}'s icon`);
        assert.notEqual(files.colour, lookFor(state).colour || 'default', `FILES shares ${state}'s colour`);
    }
    assert.notEqual(files.colour, COLOURS.green);
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

test('a start the owner asked for looks different from the recorder finishing', () => {
    assert.equal(lookFor('WAITING').icon, 'hourglass');
    assert.equal(lookFor('FINISHING').icon, 'hourglass');
    assert.notEqual(lookFor('WAITING').colour, lookFor('FINISHING').colour);
});

test('every wait for a recording the owner asked for looks the same', () => {
    assert.deepEqual(lookFor('LAUNCHING'), lookFor('WAITING'));
});

test('a cancelled start looks different from every wait', () => {
    for (const state of ['LAUNCHING', 'WAITING', 'FINISHING', 'IDLE'])
        assert.notDeepEqual(lookFor('CANCELLING'), lookFor(state), state);
});

test('a press that does nothing looks like no other state', () => {
    for (const state of STATES.filter(s => s !== 'BLOCKED'))
        assert.notEqual(lookFor('BLOCKED').icon, lookFor(state).icon, state);
});

test('a recorder the panel did not start never looks ready', () => {
    assert.notDeepEqual(lookFor('UNOWNED'), lookFor('IDLE'));
});

// Plan 00187 (#100): while Claude post-processes, the panel shows the owner's emoji for the
// style, the one the recording countdown already carries.
test('Claude post-processing shows the robot for Ctrl+Insert and the speech bubble for Alt+Insert', () => {
    assert.equal(isKnownState('CLAUDE_PROCESSING'), true);
    assert.equal(lookFor('CLAUDE_PROCESSING', 'corporate').text, '🤖');
    assert.equal(lookFor('CLAUDE_PROCESSING', 'natural').text, '💬');
    assert.equal(claudeEmoji('corporate'), '🤖');
    assert.equal(claudeEmoji('natural'), '💬');
});

test('Claude post-processing with no style known still shows the robot, never a blank', () => {
    assert.equal(lookFor('CLAUDE_PROCESSING').text, '🤖');
    assert.equal(lookFor('CLAUDE_PROCESSING', null).text, '🤖');
});

test('Claude post-processing looks different from transcribing, and is not green', () => {
    assert.notDeepEqual(lookFor('CLAUDE_PROCESSING', 'corporate'), lookFor('TRANSCRIBING'));
    assert.notEqual(lookFor('CLAUDE_PROCESSING', 'corporate').colour, COLOURS.green);
});

test('only Claude post-processing shows text in place of the icon', () => {
    for (const state of STATES.filter(s => s !== 'CLAUDE_PROCESSING'))
        assert.equal(lookFor(state, 'corporate').text, undefined, state);
});
