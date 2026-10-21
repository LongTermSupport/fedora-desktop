/**
 * The speech-to-text panel's hold on the media players while it records (Plan 00212).
 *
 *     node --test tests/extensions/test-stt-media-hold.mjs
 *
 * With "Pause media while recording" on, each recording starts wsi-pause-media, which pauses
 * what is playing and plays it again when it is sent TERM. The release is sent once, and
 * never to a hold that has already exited (its PID may belong to another process by then). A
 * hold that cannot start, or ends badly, is reported, never only logged; the recording goes
 * on, since it does not depend on the music.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {PAUSE_MEDIA_KEY, startMediaHold} =
    await import(new URL('../../extensions/speech-to-text@fedora-desktop/mediaHold.js',
        import.meta.url).href);

const ARGV = ['/home/u/.local/bin/wsi-pause-media'];

function world({enabled = true, spawnError = null} = {}) {
    const w = {spawned: [], kills: [], errors: [], logs: []};
    w.hold = startMediaHold({
        enabled,
        argv: ARGV,
        spawn(argv, onExit) {
            if (spawnError)
                throw new Error(spawnError);
            w.spawned.push({argv, onExit});
            return '4242';
        },
        kill: pid => w.kills.push(pid),
        reportError: message => w.errors.push(message),
        log: message => w.logs.push(message),
    });
    w.exit = (how, failed) => w.spawned[0].onExit(how, failed);
    return w;
}

test('the setting key is the schema key the menu switch writes', () => {
    assert.equal(PAUSE_MEDIA_KEY, 'pause-media-while-recording');
});

test('the switch off is no hold and starts nothing', () => {
    const w = world({enabled: false});
    assert.equal(w.hold, null);
    assert.equal(w.spawned.length, 0);
});

test('on: wsi-pause-media is started, and the release sends it TERM once', () => {
    const w = world();
    assert.deepEqual(w.spawned.map(s => s.argv), [ARGV]);
    w.hold.release();
    w.hold.release();
    assert.deepEqual(w.kills, ['4242']);
    w.exit('exit status 0', false);
    assert.deepEqual(w.errors, []);
});

test('a hold that has exited is never signalled: nothing was playing, or it ended itself', () => {
    const w = world();
    w.exit('exit status 0', false);
    w.hold.release();
    assert.deepEqual(w.kills, []);
    assert.deepEqual(w.errors, []);
});

test('a hold that ends badly is reported, before or after the release', () => {
    for (const released of [false, true]) {
        const w = world();
        if (released)
            w.hold.release();
        w.exit('exit status 1', true);
        assert.equal(w.errors.length, 1, `released ${released}`);
        assert.match(w.errors[0], /Pausing the media failed \(exit status 1\)/);
        assert.match(w.errors[0], /journalctl/);
    }
    const killed = world();
    killed.exit('signal 9', true);
    assert.match(killed.errors[0], /signal 9/);
});

test('a hold that cannot start is reported, and is no hold', () => {
    const w = world({spawnError: 'No such file or directory'});
    assert.equal(w.hold, null);
    assert.equal(w.errors.length, 1);
    assert.match(w.errors[0], /could not be started: No such file or directory/);
});
