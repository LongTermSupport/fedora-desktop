/**
 * The dock-recovery-on-unlock extension, exercised against the shipped `extension.js`
 * (Plan 00109 Task 5.4a, DESIGN-panel.md §12).
 *
 *     node --test tests/extensions/test-dock-recovery-on-unlock.mjs
 *
 * The extension's whole job is one decision: on an unlock, and only then, start the
 * DisplayLink dock recovery service once. Every way that goes wrong is silent in a live
 * shell. A refresh fired while locked costs memory (gnome-shell#9188), a second start per
 * unlock is a wasted recovery run, and a start that failed looks exactly like a desktop
 * that needed nothing. So each of those is pinned here, driven through `enable()` and the
 * shell's own `locked-changed` signal rather than a private method.
 */

import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const EXTENSION_DIR = new URL(
    '../../extensions/dock-recovery-on-unlock@fedora-desktop/', import.meta.url);

const LOGGED = [];
globalThis.log = message => LOGGED.push(message);

const Unlock = await import(new URL('extension.js', EXTENSION_DIR).href);
const {default: DockRecoveryOnUnlockExtension} = Unlock;
const {
    SCREEN_SHIELD, SPAWNS, SPAWN_FAILURE, SPAWN_OUTCOME, TIMERS, setScreenShield,
} = await import('./gi-stubs.mjs');

const METADATA = JSON.parse(readFileSync(new URL('metadata.json', EXTENSION_DIR), 'utf8'));

/** A fresh shell: unlocked, nothing spawned, no timers, the extension enabled. */
function enabled() {
    SPAWNS.length = 0;
    SPAWN_FAILURE.message = null;
    SPAWN_OUTCOME.successful = true;
    SPAWN_OUTCOME.exitStatus = 0;
    SPAWN_OUTCOME.stderr = '';
    TIMERS.clear();
    LOGGED.length = 0;
    SCREEN_SHIELD.reset();
    setScreenShield(SCREEN_SHIELD);
    const extension = new DockRecoveryOnUnlockExtension(METADATA);
    extension.enable();
    return extension;
}

/** Let every pending timer fire, the way the main loop would once its time came. */
function elapse() {
    for (const [id, timer] of [...TIMERS]) {
        TIMERS.delete(id);
        timer.handler();
    }
}

test('an unlock starts the recovery service once, after the settle delay', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);

    assert.equal(SPAWNS.length, 0, 'nothing may start before logind has seen the unlock');
    assert.deepEqual([...TIMERS.values()].map(timer => timer.seconds), [Unlock.SETTLE_SECONDS]);

    elapse();
    assert.deepEqual(SPAWNS.map(spawn => spawn.argv), [
        ['systemctl', 'start', '--no-block', 'displaylink-dock-recovery.service'],
    ]);
});

test('the start is an argv, never a shell command line', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    elapse();
    const [argv] = SPAWNS.map(spawn => spawn.argv);
    assert.ok(!['sh', 'bash', '/bin/sh', '/bin/bash'].includes(argv[0]));
    assert.equal(argv[0], 'systemctl');
});

test('locking starts nothing', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    elapse();
    assert.equal(SPAWNS.length, 0);
    assert.equal(TIMERS.size, 0);
});

test('locking again before the delay ends cancels the pending start', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    SCREEN_SHIELD.setLocked(true);
    assert.equal(TIMERS.size, 0, 'the pending start must be removed, not left to fire');
    elapse();
    assert.equal(SPAWNS.length, 0);
});

test('a second unlock inside the delay does not queue a second start', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    // The shell emits locked-changed only on a real change, so a repeat has to come
    // through the handler directly: it is what a duplicate signal would do.
    SCREEN_SHIELD.emit('locked-changed');
    assert.equal(TIMERS.size, 1);
    elapse();
    assert.equal(SPAWNS.length, 1);
});

test('each separate unlock starts the service again', () => {
    enabled();
    for (let round = 0; round < 2; round++) {
        SCREEN_SHIELD.setLocked(true);
        SCREEN_SHIELD.setLocked(false);
        elapse();
    }
    assert.equal(SPAWNS.length, 2);
});

test('a start that fails is logged with its exit status and what systemctl said', () => {
    enabled();
    SPAWN_OUTCOME.successful = false;
    SPAWN_OUTCOME.exitStatus = 4;
    SPAWN_OUTCOME.stderr = 'Access denied\n';
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    elapse();
    assert.equal(LOGGED.length, 1);
    assert.match(LOGGED[0], /exited 4/);
    assert.match(LOGGED[0], /Access denied/);
    assert.match(LOGGED[0], /displaylink-dock-recovery\.service/);
});

test('a start that succeeds logs nothing', () => {
    enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    elapse();
    assert.deepEqual(LOGGED, []);
});

test('systemctl that cannot be run at all is logged, not thrown into the shell', () => {
    enabled();
    SPAWN_FAILURE.message = 'Failed to execute child process "systemctl"';
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    assert.doesNotThrow(elapse);
    assert.equal(LOGGED.length, 1);
    assert.match(LOGGED[0], /Failed to execute child process/);
});

test('disable disconnects from the shell and drops a pending start', () => {
    const extension = enabled();
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    extension.disable();
    assert.equal(TIMERS.size, 0);
    assert.equal(SCREEN_SHIELD.connectedCount(), 0);
    SCREEN_SHIELD.setLocked(true);
    SCREEN_SHIELD.setLocked(false);
    elapse();
    assert.equal(SPAWNS.length, 0);
});

test('a session with no lock screen says so and does nothing', () => {
    SPAWNS.length = 0;
    TIMERS.clear();
    LOGGED.length = 0;
    setScreenShield(null);
    const extension = new DockRecoveryOnUnlockExtension(METADATA);
    assert.doesNotThrow(() => extension.enable());
    assert.equal(LOGGED.length, 1);
    assert.match(LOGGED[0], /no lock screen/);
    assert.doesNotThrow(() => extension.disable());
    assert.equal(SPAWNS.length, 0);
    setScreenShield(SCREEN_SHIELD);
});

test('the extension stays enabled while locked, or it could never see the unlock', () => {
    // The shell disables every extension whose session-modes omit unlock-dialog when the
    // screen locks, and enables it again after. Without it, this extension would be torn
    // down at the lock and rebuilt after the unlock it exists to see.
    assert.deepEqual([...METADATA['session-modes']].sort(), ['unlock-dialog', 'user']);
    assert.equal(METADATA.uuid, 'dock-recovery-on-unlock@fedora-desktop');
});
