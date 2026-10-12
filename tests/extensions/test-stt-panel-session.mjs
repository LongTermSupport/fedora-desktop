/**
 * The speech-to-text panel's ownership of one recorder, and what Insert, Escape, the Claude
 * keys and the article item do in every state (Plan 00171).
 *
 *     node --test tests/extensions/test-stt-panel-session.mjs
 *
 * The incident this guards (RESEARCH-race.md): Insert pressed 376 ms after a dictation's
 * SUCCESS started a new recorder, the old recorder's late IDLE signals overwrote the new
 * one's RECORDING, and from then on the panel believed it was idle while a microphone
 * was recording. Every press after that spawned a recorder the server refused, and the
 * refusal was wiped from the icon within 15 ms.
 *
 * The owner's rules, each with a test below: every press does something visible; a
 * press on the ready icon starts a recording; a press while not ready shows the
 * hourglass and starts the recording once ready, and a second press cancels that; in
 * the stop grace Insert still means "stop now"; the icon never shows idle while the
 * panel's recorder is alive. Driven against the shipped panelSession.js with the
 * spawn, the signals, the PID file and the timers handed in.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {PanelSession, LAUNCH_SAFETY_MS, ENDING_DISPLAY_MS, UNOWNED_POLL_MS, EXIT_SETTLE_MS} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/panelSession.js', import.meta.url).href);

const DICTATION = {kind: 'dictation', target: 'window-1'};

/** A panel whose recorders, PID file, timers and screen the test controls. */
function harness({pidFile = null, alive = []} = {}) {
    // pidFile and alive: what the panel finds when it is created (the extension enabled again)
    const h = {
        spawned: [], kills: [], aborts: [], shown: [], logs: [], errors: [], notes: [],
        timers: new Map(), alive: new Set(alive), pidFile, spawnError: null,
    };
    let nextTimer = 1;
    let tokens = 0;
    h.panel = new PanelSession({
        spawn(launch, token, onExit) {
            if (h.spawnError)
                throw new Error(h.spawnError);
            const pid = String(1000 + h.spawned.length);
            h.alive.add(pid);
            h.spawned.push({
                launch, token, pid,
                exit(how = 'exit status 0', failed = false) {
                    h.alive.delete(pid);
                    onExit(how, failed);
                },
            });
            return pid;
        },
        kill: (pid, signal) => h.kills.push([pid, signal]),
        abort: pid => h.aborts.push(pid),
        readRecorderPid: () => (h.pidFile !== null && h.alive.has(h.pidFile) ? h.pidFile : null),
        isAlive: pid => h.alive.has(pid),
        newToken: () => `token-${++tokens}`,
        show: (state, launch) => h.shown.push({state, kind: launch?.kind ?? null}),
        log: message => h.logs.push(message),
        reportError: message => h.errors.push(message),
        notify: message => h.notes.push(message),
        addTimeout(ms, handler) {
            h.timers.set(nextTimer, {ms, handler});
            return nextTimer++;
        },
        removeTimeout(id) {
            if (!h.timers.delete(id))
                throw new Error(`no timer ${id} to remove`);
        },
    });
    /** Run every pending timer of `ms`, as GLib would once that long has passed. */
    h.fire = ms => {
        const due = [...h.timers].filter(([, t]) => t.ms === ms);
        assert.ok(due.length > 0, `no ${ms} ms timer is pending`);
        for (const [id, t] of due) {
            h.timers.delete(id);
            t.handler();
        }
    };
    h.shows = () => h.panel.display;
    h.recorder = (n = h.spawned.length - 1) => h.spawned[n];
    /** The recorder `n` (the latest by default) reports `state`. */
    h.report = (state, n = h.spawned.length - 1) => h.panel.onState(state, h.spawned[n].token);
    /** The icon never says idle while the panel's own recorder is alive. */
    h.noLie = () => {
        const owned = h.panel.ownedPid;
        if (owned !== null && h.alive.has(owned))
            assert.notEqual(h.shows(), 'IDLE', `the icon said idle while recorder ${owned} was alive`);
    };
    return h;
}

/** A dictation started and recording, as the panel sees it. */
function recording() {
    const h = harness();
    h.panel.insert(DICTATION);
    h.report('RECORDING');
    return h;
}

// ── The incident ────────────────────────────────────────────────────────────────────

test('the incident: Insert just after SUCCESS starts a dictation, and the next Insert stops it', () => {
    const h = recording();
    const a = h.recorder();
    h.panel.insert(DICTATION);                      // graced stop
    assert.deepEqual(h.kills, [[a.pid, 'TERM']]);
    h.report('STOPPING');
    h.report('TRANSCRIBING');
    h.report('SUCCESS');
    h.noLie();
    assert.equal(h.shows(), 'SUCCESS');

    h.panel.insert(DICTATION);                      // 376 ms after SUCCESS, A still alive
    assert.equal(h.spawned.length, 1, 'a second recorder was started beside one still alive');
    assert.equal(h.shows(), 'QUEUED', 'the press was not shown');
    h.noLie();

    a.exit();                                       // A exits: the queued start runs
    assert.equal(h.spawned.length, 2);
    const b = h.recorder();
    assert.notEqual(b.token, a.token);
    assert.equal(h.shows(), 'LAUNCHING');
    h.report('RECORDING');
    assert.equal(h.shows(), 'RECORDING');

    // A's finishing IDLEs, which overwrote B's RECORDING, change nothing now
    for (let i = 0; i < 3; i++)
        assert.equal(h.panel.onState('IDLE', a.token), false);
    assert.equal(h.shows(), 'RECORDING');
    h.noLie();
    assert.match(h.logs.join('\n'), new RegExp(`Dropped IDLE from session ${a.token}`));

    h.panel.insert(DICTATION);                      // the second press stops B
    assert.deepEqual(h.kills.at(-1), [b.pid, 'TERM']);
    assert.equal(h.spawned.length, 2, 'the second press started yet another recorder');
});

test('the incident with the old recorder already gone: Insert after SUCCESS starts at once', () => {
    const h = recording();
    h.report('SUCCESS');
    h.recorder().exit();
    assert.equal(h.shows(), 'SUCCESS', 'the green icon stays for its whole display time');
    h.panel.insert(DICTATION);
    assert.equal(h.spawned.length, 2);
    assert.equal(h.shows(), 'LAUNCHING');
});

test('late signals of a recorder that has exited are dropped, whatever they say', () => {
    const h = recording();
    const a = h.recorder();
    h.report('SUCCESS');
    a.exit();
    h.panel.insert(DICTATION);
    h.report('RECORDING');
    for (const state of ['IDLE', 'ERROR', 'SUCCESS', 'RECORDING', 'TRANSCRIBING'])
        assert.equal(h.panel.onState(state, a.token), false, state);
    assert.equal(h.panel.onError('Recording already active', a.token), false);
    assert.equal(h.panel.acceptsProgress(a.token), false);
    assert.equal(h.shows(), 'RECORDING');
});

test('a signal with no token (a recorder from before this panel) changes nothing', () => {
    const h = recording();
    assert.equal(h.panel.onState('IDLE', ''), false);
    assert.equal(h.panel.onState('IDLE', null), false);
    assert.equal(h.shows(), 'RECORDING');
});

test('nothing is taken from the current recorder after the state that ended its session', () => {
    const h = recording();
    h.report('SUCCESS');
    assert.equal(h.report('IDLE'), false);
    assert.equal(h.shows(), 'SUCCESS');
});

// ── Insert in every state ───────────────────────────────────────────────────────────

test('idle: Insert starts a recorder with a fresh token and shows the press at once', () => {
    const h = harness();
    h.panel.insert(DICTATION);
    assert.equal(h.spawned.length, 1);
    assert.equal(h.recorder().token, 'token-1');
    assert.deepEqual(h.recorder().launch, DICTATION);
    assert.equal(h.shows(), 'LAUNCHING', 'nothing visible until the recorder reported');
    assert.deepEqual([...h.timers.values()].map(t => t.ms), [LAUNCH_SAFETY_MS]);
});

/** Insert, Ctrl+Insert and Alt+Insert: each follows the same rules in every state. */
const KEYS = [
    ['Insert', DICTATION],
    ['Ctrl+Insert', {kind: 'claude', style: 'corporate', target: 'window-1'}],
    ['Alt+Insert', {kind: 'claude', style: 'natural', target: 'window-1'}],
];

/** A recorder started by `launch` that has reported `states` in turn. */
function reported(launch, ...states) {
    const h = harness();
    h.panel.insert(launch);
    for (const state of states)
        h.report(state);
    return h;
}

for (const [key, launch] of KEYS) {
    for (const state of ['LAUNCHING', 'WAITING', 'PREPARING']) {
        test(`${state}: ${key} cancels the start, and shows it was taken`, () => {
            const h = reported(launch, ...(state === 'LAUNCHING' ? [] : [state]));
            h.panel.insert(launch);
            assert.deepEqual(h.kills, [[h.recorder().pid, 'TERM']]);
            assert.equal(h.spawned.length, 1);
            // PREPARING: batch and pre-buffer recorders are already capturing, so it is a stop
            assert.equal(h.shows(), state === 'PREPARING' ? 'STOPPING' : 'CANCELLING');
            h.recorder().exit('signal 15', true);
            assert.equal(h.shows(), 'IDLE');
            assert.deepEqual(h.errors, [], 'a start the owner cancelled was reported as a failure');
        });
    }

    test(`RECORDING: ${key} asks for the graced stop`, () => {
        const h = reported(launch, 'RECORDING');
        h.panel.insert(launch);
        assert.deepEqual(h.kills, [[h.recorder().pid, 'TERM']]);
        assert.equal(h.spawned.length, 1);
    });

    test(`STOPPING (the grace, mic still open): ${key} means stop now`, () => {
        const h = reported(launch, 'RECORDING', 'STOPPING');
        h.panel.insert(launch);
        assert.deepEqual(h.kills, [[h.recorder().pid, 'TERM']]);
        assert.notEqual(h.shows(), 'QUEUED', 'the grace is a stop, not a queued start');
    });

    for (const ended of ['TRANSCRIBING', 'SUCCESS', 'ERROR', 'IDLE']) {
        test(`${ended} with the recorder still alive: ${key} shows the hourglass and queues the start`, () => {
            const h = reported(launch, 'RECORDING', ended);
            h.panel.insert(launch);
            assert.equal(h.shows(), 'QUEUED');
            assert.deepEqual(h.kills, [], 'the recorder finishing the last dictation was signalled');
            assert.equal(h.spawned.length, 1);
            if (ended === 'TRANSCRIBING')
                h.report('SUCCESS');
            assert.equal(h.shows(), 'QUEUED', 'the hourglass stays until the start runs');
            h.recorder().exit();
            assert.equal(h.spawned.length, 2);
            assert.deepEqual(h.recorder().launch, launch, 'the queued start lost its key');
            assert.equal(h.shows(), 'LAUNCHING');
        });

        test(`${ended} after the recorder exited: ${key} starts at once`, () => {
            const h = reported(launch, 'RECORDING', ended);
            if (ended === 'TRANSCRIBING')
                h.report('SUCCESS');
            h.recorder().exit();
            h.panel.insert(launch);
            assert.equal(h.spawned.length, 2);
            assert.equal(h.shows(), 'LAUNCHING');
        });
    }

    test(`QUEUED: a second ${key} cancels the queued start`, () => {
        const h = reported(launch, 'RECORDING', 'TRANSCRIBING');
        h.panel.insert(launch);
        h.panel.insert(launch);
        assert.equal(h.shows(), 'TRANSCRIBING', 'the icon went back to what the recorder is doing');
        h.report('SUCCESS');
        h.recorder().exit();
        assert.equal(h.spawned.length, 1, 'a cancelled start still ran');
        assert.match(h.logs.join('\n'), /cancelled/);
    });
}

test('a recorder that ended with IDLE but is still exiting shows the hourglass, not ready', () => {
    const h = recording();
    h.report('IDLE');
    assert.equal(h.shows(), 'FINISHING');
    h.noLie();
    h.recorder().exit();
    assert.equal(h.shows(), 'IDLE');
});

// ── What the icon shows after the session ───────────────────────────────────────────

test('ERROR stays on the icon for its whole display time, then the icon is ready', () => {
    const h = recording();
    h.report('ERROR');
    h.recorder().exit('exit status 1', true);
    assert.equal(h.shows(), 'ERROR', 'the refusal was wiped from the icon at once');
    assert.deepEqual(h.errors, [], 'the recorder said ERROR itself; no second report');
    h.fire(ENDING_DISPLAY_MS);
    assert.equal(h.shows(), 'IDLE');
});

test('the display timer does not make a recorder still alive look idle', () => {
    const h = recording();
    h.report('SUCCESS');
    h.fire(ENDING_DISPLAY_MS);
    assert.equal(h.shows(), 'FINISHING');
    h.noLie();
});

test('a recorder that exits without saying how it ended resets the panel and says why', () => {
    const h = recording();
    h.recorder().exit('signal 11', true);
    assert.deepEqual(h.errors, [], 'judged before a late last signal could arrive');
    h.fire(EXIT_SETTLE_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.equal(h.errors.length, 1);
    assert.match(h.errors[0], /signal 11/);
});

test('a recorder that fails before its first report is reported with how it ended', () => {
    const h = harness();
    h.panel.insert(DICTATION);
    h.recorder().exit('exit status 1', true);
    h.fire(EXIT_SETTLE_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.match(h.errors[0], /exit status 1.*before it started/);
    assert.equal(h.timers.size, 0, 'the safety timer outlived the recorder');
});

test('a launcher that hands off and exits cleanly leaves the wait to the report', () => {
    // wsi-article-window hands a second launch to the window already open and exits 0
    const h = harness();
    h.panel.article({kind: 'article'});
    h.recorder().exit('exit status 0', false);
    assert.equal(h.shows(), 'LAUNCHING');
    assert.deepEqual(h.errors, []);
    h.report('PREPARING');
    assert.equal(h.shows(), 'PREPARING');
});

test('the safety timer: a live recorder that has not reported is still waited for', () => {
    const h = harness();
    h.panel.insert(DICTATION);
    h.fire(LAUNCH_SAFETY_MS);
    assert.equal(h.shows(), 'LAUNCHING');
    h.noLie();
    assert.match(h.logs.join('\n'), /not reported/);
});

test('the safety timer: a handed-off launch nobody reported for is given up', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    h.recorder().exit('exit status 0', false);
    h.fire(LAUNCH_SAFETY_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.equal(h.errors.length, 1);
});

test('a spawn that fails is reported and leaves the panel ready', () => {
    const h = harness();
    h.spawnError = 'Failed to execute child process';
    h.panel.insert(DICTATION);
    assert.equal(h.shows(), 'IDLE');
    assert.match(h.errors[0], /Failed to execute child process/);
    assert.equal(h.timers.size, 0);
    h.spawnError = null;
    h.panel.insert(DICTATION);
    assert.equal(h.spawned.length, 1);
});

// ── Escape ──────────────────────────────────────────────────────────────────────────

for (const state of ['RECORDING', 'STOPPING']) {
    test(`${state}: Escape discards the dictation`, () => {
        const h = recording();
        if (state === 'STOPPING')
            h.report('STOPPING');
        h.panel.escape();
        assert.deepEqual(h.aborts, [h.recorder().pid]);
        assert.equal(h.shows(), 'FINISHING', 'the icon said ready while the recorder was alive');
        h.recorder().exit('signal 9', true);
        assert.equal(h.shows(), 'IDLE');
        assert.deepEqual(h.errors, [], 'a discard the owner asked for was reported as a failure');
    });
}

for (const state of ['LAUNCHING', 'WAITING', 'PREPARING', 'TRANSCRIBING', 'SUCCESS']) {
    test(`${state}: Escape does nothing (no microphone open)`, () => {
        const h = harness();
        h.panel.insert(DICTATION);
        if (state !== 'LAUNCHING')
            h.report(state);
        const before = h.shows();
        h.panel.escape();
        assert.deepEqual(h.aborts, []);
        assert.equal(h.shows(), before);
    });
}

test('idle: Escape does nothing', () => {
    const h = harness();
    h.panel.escape();
    assert.deepEqual(h.aborts, []);
    assert.equal(h.shows(), 'IDLE');
});

// ── The Claude keys ─────────────────────────────────────────────────────────────────

for (const style of ['corporate', 'natural']) {
    test(`Claude (${style}): the same rules, and a queued start keeps its style`, () => {
        const claude = {kind: 'claude', style, target: 'window-2'};
        const h = harness();
        h.panel.insert(claude);
        assert.deepEqual(h.recorder().launch, claude);
        h.report('RECORDING');
        h.panel.insert(claude);
        assert.deepEqual(h.kills, [[h.recorder().pid, 'TERM']]);
        h.report('TRANSCRIBING');
        h.panel.insert(claude);
        assert.equal(h.shows(), 'QUEUED');
        h.report('SUCCESS');
        h.recorder().exit();
        assert.deepEqual(h.recorder().launch, claude);
    });
}

test('a start queued by Insert during a Claude dictation is a plain dictation', () => {
    const h = harness();
    h.panel.insert({kind: 'claude', style: 'corporate', target: 'w'});
    h.report('TRANSCRIBING');
    h.panel.insert(DICTATION);
    h.report('SUCCESS');
    h.recorder().exit();
    assert.deepEqual(h.recorder().launch, DICTATION);
});

// ── A recorder the panel did not start ──────────────────────────────────────────────

test('a live recorder found when the panel starts (screen unlocked): never shown as ready', () => {
    // GNOME disables extensions at the screen lock and enables them at unlock; a
    // dictation recording across the lock is alive, and its panel has gone
    const h = harness({pidFile: '4242', alive: ['4242']});
    assert.equal(h.shows(), 'UNOWNED');
    assert.deepEqual(h.kills, [], 'a recording was stopped without a press');
    h.fire(UNOWNED_POLL_MS);
    assert.equal(h.shows(), 'UNOWNED');
    h.alive.delete('4242');
    h.fire(UNOWNED_POLL_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.equal(h.timers.size, 0);
});

test('Insert on a recorder the panel did not start only stops it', () => {
    const h = harness({pidFile: '4242', alive: ['4242']});
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, [['4242', 'INT']]);
    assert.equal(h.notes.length, 1);
    assert.match(h.notes[0], /4242/);
    assert.equal(h.shows(), 'UNOWNED');
    assert.equal(h.spawned.length, 0, 'a second recorder was started beside a live one');
    h.alive.delete('4242');
    h.fire(UNOWNED_POLL_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.equal(h.spawned.length, 0, 'a press that stopped a recording also started one');
});

test('idle, then a recorder the panel did not start turns up: Insert only stops it', () => {
    const h = harness();
    h.alive.add('4242');
    h.pidFile = '4242';
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, [['4242', 'INT']]);
    assert.equal(h.shows(), 'UNOWNED');
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, [['4242', 'INT'], ['4242', 'INT']], 'the second press did nothing');
    assert.equal(h.spawned.length, 0);
});

test('a start queued behind the panel\'s recorder still waits for one it did not start', () => {
    const h = recording();
    h.report('TRANSCRIBING');
    h.panel.insert(DICTATION);
    h.alive.add('4242');
    h.pidFile = '4242';
    h.report('SUCCESS');
    h.recorder().exit();
    assert.deepEqual(h.kills.at(-1), ['4242', 'INT']);
    assert.equal(h.shows(), 'QUEUED');
    assert.equal(h.spawned.length, 1);
    h.alive.delete('4242');
    h.fire(UNOWNED_POLL_MS);
    assert.equal(h.spawned.length, 2);
});

test('article: refused while a recorder the panel did not start is alive', () => {
    const h = harness({pidFile: '4242', alive: ['4242']});
    h.panel.article({kind: 'article'});
    assert.equal(h.spawned.length, 0);
    assert.equal(h.notes.length, 1);
});

test('a stale PID file (no live recorder) does not hold up the start', () => {
    const h = harness();
    h.pidFile = '4242';  // not alive
    h.panel.insert(DICTATION);
    assert.equal(h.spawned.length, 1);
    assert.deepEqual(h.kills, []);
});

// ── Article mode ────────────────────────────────────────────────────────────────────

test('article: started from the menu, it ends with its IDLE while its window stays open', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    assert.equal(h.shown.at(-1).kind, 'article');
    h.report('PREPARING');
    h.report('IDLE');
    assert.equal(h.shows(), 'IDLE', 'the panel waited for the window to close');
    h.panel.insert(DICTATION);
    assert.equal(h.spawned.length, 2, 'Insert after an article did not start a dictation');
});

test('article: Insert while it records stops the article recorder, which the PID file names', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    h.report('PREPARING');
    h.alive.add('5151');
    h.pidFile = '5151';
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, [['5151', 'TERM']]);
    assert.equal(h.spawned.length, 1);
});

test('article: refused while a dictation is under way, and the refusal is shown', () => {
    const h = recording();
    h.panel.article({kind: 'article'});
    assert.equal(h.spawned.length, 1);
    assert.equal(h.notes.length, 1);
});

test('article: a start queued behind it is not run by its ending', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    h.report('PREPARING');
    h.report('ERROR');
    h.fire(ENDING_DISPLAY_MS);
    assert.equal(h.shows(), 'IDLE');
});

test('article: Insert after a handed-off launch stops the article recorder, not the launcher', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    const launcher = h.recorder().pid;
    h.recorder().exit('exit status 0', false);      // handed to the window already open
    h.alive.add('5151');
    h.pidFile = '5151';
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, [['5151', 'TERM']]);
    assert.ok(!h.kills.some(([pid]) => pid === launcher), 'the launcher (gone, its PID free for reuse) was signalled');
});

test('article: Insert after a handed-off launch with no recorder yet says so', () => {
    const h = harness();
    h.panel.article({kind: 'article'});
    h.recorder().exit('exit status 0', false);
    h.panel.insert(DICTATION);
    assert.deepEqual(h.kills, []);
    assert.equal(h.notes.length, 1);
});

// ── The exit and the last signal, in either order ───────────────────────────────────

for (const [final, how, failed] of [['SUCCESS', 'exit status 0', false], ['ERROR', 'exit status 1', true]]) {
    test(`${final} then the exit: shown, no error`, () => {
        const h = recording();
        h.report('TRANSCRIBING');
        h.report(final);
        h.recorder().exit(how, failed);
        assert.equal(h.shows(), final);
        assert.deepEqual(h.errors, []);
        assert.equal(h.panel.ownedPid, null);
    });

    test(`the exit seen before the last ${final}: ${final} still shown, no false error`, () => {
        const h = recording();
        h.report('TRANSCRIBING');
        h.recorder().exit(how, failed);
        assert.deepEqual(h.errors, [], 'an error was reported before the last signal could arrive');
        assert.notEqual(h.shows(), 'IDLE');
        assert.equal(h.report(final), true, `the late ${final} was dropped`);
        assert.equal(h.shows(), final);
        assert.equal(h.panel.ownedPid, null);
        assert.ok(![...h.timers.values()].some(t => t.ms === EXIT_SETTLE_MS), 'the settle outlived the session');
        h.fire(ENDING_DISPLAY_MS);
        assert.equal(h.shows(), 'IDLE');
        assert.deepEqual(h.errors, []);
    });
}

test('an exit no last signal follows is reported once the settle has passed', () => {
    const h = recording();
    h.report('TRANSCRIBING');
    h.recorder().exit('exit status 0', false);
    h.fire(EXIT_SETTLE_MS);
    assert.equal(h.shows(), 'IDLE');
    assert.equal(h.errors.length, 1);
    assert.match(h.errors[0], /TRANSCRIBING/);
});

test('a cancel that came too late: the panel follows the recorder', () => {
    const h = reported(DICTATION, 'WAITING');
    h.panel.insert(DICTATION);
    assert.equal(h.shows(), 'CANCELLING');
    h.report('RECORDING');                          // the start was accepted before the TERM
    assert.equal(h.shows(), 'RECORDING');
});

// ── The log ─────────────────────────────────────────────────────────────────────────

test('every change of the icon is logged as "Display: <state>"', () => {
    const h = recording();
    const shown = h.logs.filter(l => l.startsWith('Display: '));
    assert.deepEqual(shown, ['Display: IDLE', 'Display: LAUNCHING', 'Display: RECORDING']);
});

// ── Error and Progress ──────────────────────────────────────────────────────────────

test('Error and Progress are taken only from the current recorder', () => {
    const h = recording();
    const {token} = h.recorder();
    assert.equal(h.panel.onError('Not recording: Recording already active', token), true);
    assert.equal(h.panel.acceptsProgress(token), true);
    assert.equal(h.panel.onError('stale', 'token-0'), false);
    assert.equal(h.panel.acceptsProgress('token-0'), false);
});

// ── Teardown ────────────────────────────────────────────────────────────────────────

test('destroy removes every timer it holds', () => {
    const h = harness({pidFile: '4242', alive: ['4242']});   // the unowned poll
    h.panel.destroy();
    assert.equal(h.timers.size, 0);

    const s = recording();
    s.recorder().exit();            // the exit settle
    s.panel.destroy();
    assert.equal(s.timers.size, 0);

    const g = recording();
    g.report('ERROR');
    g.recorder().exit('exit status 1', true);  // the ERROR display timer
    g.panel.destroy();
    assert.equal(g.timers.size, 0);
});

test('a recorder that exits after the panel was destroyed touches nothing', () => {
    const h = recording();
    const shown = h.shown.length;
    h.panel.destroy();
    h.recorder().exit('exit status 0');
    assert.equal(h.panel.onState('IDLE', h.recorder().token), false);
    assert.equal(h.shown.length, shown, 'a destroyed panel drew on the icon');
    assert.equal(h.spawned.length, 1);
});
