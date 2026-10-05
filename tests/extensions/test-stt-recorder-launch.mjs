/**
 * The panel's wait for a recorder it launched (Plan 00156 Task 1.3; CLAUDE/QA.md
 * "ready-wait-ignores-child-exit").
 *
 *     node --test tests/extensions/test-stt-recorder-launch.mjs
 *
 * Insert spawns a recorder and ignores further presses until the recorder reports its
 * first state over D-Bus. The spawn kept no handle, so a recorder that died before
 * reporting was noticed only when a 10 s safety timer ran out, and then only as a log
 * line. The launch now watches the child: an exit before the first report ends the wait
 * at once and is reported with how the recorder ended. Driven against the shipped
 * recorderLaunch.js, with the spawn and the timer handed in.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {RecorderLaunch, LAUNCH_SAFETY_MS, spawnWatched} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/recorderLaunch.js', import.meta.url).href);
const {SPAWNS, SPAWN_OUTCOME} = await import('./gi-stubs.mjs');

/** A launch whose child, timers and reports the test controls. */
function harness({spawnError = null} = {}) {
    const spawned = [];
    const timers = new Map();
    const reports = [];
    const logs = [];
    let nextId = 1;
    const launch = new RecorderLaunch({
        spawn(command, onExit) {
            if (spawnError)
                throw new Error(spawnError);
            spawned.push({command, exit: (how, failed = true) => onExit(how, failed)});
        },
        addTimeout(ms, handler) {
            timers.set(nextId, {ms, handler});
            return nextId++;
        },
        removeTimeout(id) {
            if (!timers.delete(id))
                throw new Error(`no timer ${id} to remove`);
        },
        log: message => logs.push(message),
        onExitBeforeReport: how => reports.push(how),
    });
    return {launch, spawned, timers, reports, logs};
}

test('a launch is pending until the recorder reports', () => {
    const {launch, spawned, timers, reports} = harness();
    launch.begin('/stub/wsi --toggle');
    assert.equal(launch.pending, true);
    assert.deepEqual(spawned.map(s => s.command), ['/stub/wsi --toggle']);
    assert.deepEqual([...timers.values()].map(t => t.ms), [LAUNCH_SAFETY_MS]);
    launch.settle();
    assert.equal(launch.pending, false);
    assert.equal(timers.size, 0, 'the safety timer outlived the report');
    spawned[0].exit('exit status 1');
    assert.deepEqual(reports, [], 'a recorder that reported and then ended is not reported again');
});

test('a recorder that fails before reporting ends the wait at once, saying how', () => {
    const {launch, spawned, timers, reports} = harness();
    launch.begin('/stub/wsi-stream');
    spawned[0].exit('exit status 1');
    assert.equal(launch.pending, false, 'Insert stayed ignored for a recorder that was gone');
    assert.equal(timers.size, 0);
    assert.deepEqual(reports, ['exit status 1']);
});

test('a launcher that exits cleanly before the report leaves the wait to the report', () => {
    // wsi-article-window hands a second launch to the window already open and exits 0;
    // the recorder that window starts is the one that reports.
    const {launch, spawned, timers, reports, logs} = harness();
    launch.begin('/stub/wsi-article-window');
    spawned[0].exit('exit status 0', false);
    assert.equal(launch.pending, true);
    assert.equal(timers.size, 1);
    assert.deepEqual(reports, []);
    assert.match(logs.join('\n'), /exit status 0/);
});

test('the safety timer still ends a launch whose recorder neither reports nor exits', () => {
    const {launch, timers, reports, logs} = harness();
    launch.begin('/stub/wsi');
    const [[, timer]] = [...timers];
    timer.handler();
    assert.equal(launch.pending, false);
    assert.deepEqual(reports, []);
    assert.match(logs.join('\n'), /without a DBus state signal/);
});

test('an exit left over from an earlier launch does not end a later one', () => {
    const {launch, spawned, reports} = harness();
    launch.begin('/stub/wsi');
    launch.settle();
    launch.begin('/stub/wsi');
    spawned[0].exit('exit status 0');
    assert.equal(launch.pending, true);
    assert.deepEqual(reports, []);
    spawned[1].exit('signal 9');
    assert.deepEqual(reports, ['signal 9']);
});

test('a spawn that fails leaves nothing pending and is raised', () => {
    const {launch, timers} = harness({spawnError: 'Failed to execute child process'});
    assert.throws(() => launch.begin('/stub/missing'), /Failed to execute child process/);
    assert.equal(launch.pending, false);
    assert.equal(timers.size, 0);
});

/*
 * The real spawn, against the gi stubs. No real process runs: the harness is Node, and
 * GJS (which a real Gio.Subprocess needs) is not installed where these tests run. What is
 * checked is the argv spawnWatched builds and what it makes of how a child ended.
 */
function endedAs(outcome) {
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null}, outcome);
    const calls = [];
    spawnWatched('/bin/bash -c "WHISPER_MODEL=small /stub/wsi --toggle"',
        (how, failed) => calls.push({how, failed}));
    Object.assign(SPAWN_OUTCOME, {exitStatus: 0, termSig: null});
    return calls;
}

test('spawnWatched spawns the command line as GLib splits it', () => {
    SPAWNS.length = 0;
    endedAs({});
    assert.deepEqual(SPAWNS.at(-1).argv,
        ['/bin/bash', '-c', 'WHISPER_MODEL=small /stub/wsi --toggle']);
});

test('spawnWatched reports a clean exit as not failed', () => {
    assert.deepEqual(endedAs({exitStatus: 0}), [{how: 'exit status 0', failed: false}]);
});

test('spawnWatched reports a non-zero exit as failed', () => {
    assert.deepEqual(endedAs({exitStatus: 2}), [{how: 'exit status 2', failed: true}]);
});

test('spawnWatched reports a killing signal as failed', () => {
    assert.deepEqual(endedAs({termSig: 9}), [{how: 'signal 9', failed: true}]);
});
