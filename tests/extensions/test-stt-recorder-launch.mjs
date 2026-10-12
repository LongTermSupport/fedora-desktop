/**
 * How the speech-to-text panel starts a recorder and learns that it has ended (Plan 00156
 * Task 1.3; Plan 00171 Task 2.2).
 *
 *     node --test tests/extensions/test-stt-recorder-launch.mjs
 *
 * The panel keeps the recorder's handle for its whole life (panelSession.js decides what
 * an exit means), and its PID is the recorder's own: the Whisper Model setting reaches the
 * recorder as an environment variable, not through a `/bin/bash -c` wrapper whose PID
 * would have been the one the panel signalled. The recorder runs in a process group of
 * its own (setsid), so Escape kills it with every child it started. GJS (which a real
 * Gio.Subprocess needs) is not installed where these tests run: what is checked is the
 * argv and environment spawnWatched hands GLib, the PID it returns, what it makes of how
 * a child ended, and, with a real process, that setsid keeps the PID and leads a group.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {spawnWatched, killArgv, discardSignal, SETSID} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/recorderLaunch.js', import.meta.url).href);
const {SPAWNS, SPAWN_OUTCOME, STUB_PID} = await import('./gi-stubs.mjs');
const {execFileSync} = await import('node:child_process');

const ARGV = ['/stub/home/.local/bin/wsi-stream', '--server-mode', '--session-token', 'token-1'];
const SPAWNED = [SETSID, ...ARGV];

function endedAs(outcome, env = {}) {
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null}, outcome);
    const calls = [];
    const pid = spawnWatched(ARGV, env, (how, failed) => calls.push({how, failed}));
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null});
    return {calls, pid};
}

test('spawnWatched runs the recorder through setsid only, with no shell in between', () => {
    SPAWNS.length = 0;
    endedAs({});
    assert.deepEqual(SPAWNS.at(-1).argv, SPAWNED);
});

test('the Whisper Model reaches the recorder as WHISPER_MODEL in its environment', () => {
    SPAWNS.length = 0;
    endedAs({}, {WHISPER_MODEL: 'small'});
    assert.deepEqual(SPAWNS.at(-1).env, {WHISPER_MODEL: 'small'});
    assert.deepEqual(SPAWNS.at(-1).argv, SPAWNED);
});

test('setsid, started by a process that is not a group leader, execs: the PID is the recorder\'s and it leads its own group', () => {
    // What the panel relies on: GNOME Shell's children are not group leaders, so setsid
    // does not fork, and the recorder's PID is the panel's handle and its group's ID
    const out = execFileSync(SETSID, ['sh', '-c', 'echo "$$ $(ps -o pgid= -p $$)"'], {encoding: 'utf8'});
    const [pid, pgid] = out.trim().split(/\s+/);
    assert.equal(pid, pgid, `the recorder is not its group's leader: ${out}`);
    assert.notEqual(pgid, String(process.pid));
});

test('a signal to the whole group names the negative PID', () => {
    assert.deepEqual(killArgv('4242', 'KILL', true), ['kill', '-KILL', '--', '-4242']);
    assert.deepEqual(killArgv('4242', 'TERM', false), ['kill', '-TERM', '--', '4242']);
});

test('killArgv refuses anything but a numeric PID and a recorder signal', () => {
    assert.throws(() => killArgv('42; rm', 'KILL', false));
    assert.throws(() => killArgv('42', 'HUP', false));
    assert.throws(() => killArgv('', 'TERM', false));
});

test('Escape: a server-mode recorder is asked to ABORT; any other is killed with its whole group', () => {
    assert.deepEqual(discardSignal({serverMode: true}), {signal: 'USR1', group: false});
    // batch wsi records with a pw-record child: the group takes it too
    assert.deepEqual(discardSignal({serverMode: false}), {signal: 'KILL', group: true});
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
