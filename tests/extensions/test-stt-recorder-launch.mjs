/**
 * How the speech-to-text panel starts a recorder and learns that it has ended (Plan 00156
 * Task 1.3; Plan 00171 Task 2.2).
 *
 *     node --test tests/extensions/test-stt-recorder-launch.mjs
 *
 * The panel keeps the recorder's handle for its whole life (panelSession.js decides what
 * an exit means), and its PID is the recorder's own: the Whisper Model setting reaches the
 * recorder as an environment variable, not through a `/bin/bash -c` wrapper whose PID
 * would have been the one the panel signalled. No real process runs: the harness is Node,
 * and GJS (which a real Gio.Subprocess needs) is not installed where these tests run.
 * What is checked is the argv and environment spawnWatched hands GLib, the PID it
 * returns, and what it makes of how a child ended.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {spawnWatched} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/recorderLaunch.js', import.meta.url).href);
const {SPAWNS, SPAWN_OUTCOME, STUB_PID} = await import('./gi-stubs.mjs');

const ARGV = ['/stub/home/.local/bin/wsi-stream', '--server-mode', '--session-token', 'token-1'];

function endedAs(outcome, env = {}) {
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null}, outcome);
    const calls = [];
    const pid = spawnWatched(ARGV, env, (how, failed) => calls.push({how, failed}));
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null});
    return {calls, pid};
}

test('spawnWatched runs the recorder itself, with no shell in between', () => {
    SPAWNS.length = 0;
    endedAs({});
    assert.deepEqual(SPAWNS.at(-1).argv, ARGV);
});

test('the Whisper Model reaches the recorder as WHISPER_MODEL in its environment', () => {
    SPAWNS.length = 0;
    endedAs({}, {WHISPER_MODEL: 'small'});
    assert.deepEqual(SPAWNS.at(-1).env, {WHISPER_MODEL: 'small'});
    assert.deepEqual(SPAWNS.at(-1).argv, ARGV);
});

test('spawnWatched returns the PID of the process it started', () => {
    assert.equal(endedAs({}).pid, STUB_PID);
});

test('spawnWatched reports a clean exit as not failed', () => {
    assert.deepEqual(endedAs({exitStatus: 0}).calls, [{how: 'exit status 0', failed: false}]);
});

test('spawnWatched reports a non-zero exit as failed', () => {
    assert.deepEqual(endedAs({exitStatus: 2}).calls, [{how: 'exit status 2', failed: true}]);
});

test('spawnWatched reports a killing signal as failed', () => {
    assert.deepEqual(endedAs({termSig: 9}).calls, [{how: 'signal 9', failed: true}]);
});
