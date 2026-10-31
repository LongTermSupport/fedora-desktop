/**
 * A dictation pastes into the window focused at Insert (Plan 00148 Task 9.10).
 *
 *     node --test tests/extensions/test-stt-paste-target.mjs
 *
 * Each paste used to go to whatever was focused at that moment, so a stray click or a
 * window that stole focus mid-dictation took the text. The panel now pins the window
 * focused at Insert and answers the recorder's PasteKey for it: focused, so paste; not
 * focused, so the panel gives it focus back and the recorder asks again; closed, so the
 * recorder pastes nothing. Driven against the shipped pasteTarget.js.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {PasteTargetPin, normaliseTabTitle} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/pasteTarget.js', import.meta.url).href);
const {SignalSource} = await import('./gi-stubs.mjs');

class StubWindow extends SignalSource {
    constructor(wmClass = 'org.gnome.TextEditor', title = 'notes.txt') {
        super();
        this.wmClass = wmClass;
        this.title = title;
    }

    get_wm_class() {
        return this.wmClass;
    }

    get_title() {
        return this.title;
    }
}

const PTYXIS = 'org.gnome.Ptyxis';

function pinWithActivations() {
    const activated = [];
    return {activated, pin: new PasteTargetPin(window => activated.push(window))};
}

test('the pinned window, still focused, is pasted into', () => {
    const {activated, pin} = pinWithActivations();
    const editor = new StubWindow();
    pin.pin(editor);
    assert.deepEqual(pin.answer(editor), {window: editor, focused: true, gone: false, tabChanged: false});
    assert.deepEqual(activated, []);
    pin.release();
});

test('focus moved away: the pinned window is given focus back, and is not yet focused', () => {
    const {activated, pin} = pinWithActivations();
    const editor = new StubWindow();
    const intruder = new StubWindow();
    pin.pin(editor);
    assert.deepEqual(pin.answer(intruder), {window: editor, focused: false, gone: false, tabChanged: false});
    assert.deepEqual(activated, [editor], 'the pinned window was not activated');
    assert.deepEqual(pin.answer(editor), {window: editor, focused: true, gone: false, tabChanged: false});
    assert.equal(activated.length, 1, 'activated again once it had focus');
    pin.release();
});

test('nothing focused at all still answers for the pinned window', () => {
    const {activated, pin} = pinWithActivations();
    const editor = new StubWindow();
    pin.pin(editor);
    assert.deepEqual(pin.answer(null), {window: editor, focused: false, gone: false, tabChanged: false});
    assert.deepEqual(activated, [editor]);
    pin.release();
});

test('the pinned window closed: gone, and nothing is activated', () => {
    const {activated, pin} = pinWithActivations();
    const editor = new StubWindow();
    const other = new StubWindow();
    pin.pin(editor);
    editor.emit('unmanaged');
    assert.equal(pin.window, null);
    assert.deepEqual(pin.answer(other), {window: null, focused: false, gone: true, tabChanged: false});
    assert.deepEqual(activated, []);
    pin.release();
});

test('with no pin (a recorder started by hand) the focused window is the target', () => {
    const {activated, pin} = pinWithActivations();
    const focused = new StubWindow();
    assert.deepEqual(pin.answer(focused), {window: focused, focused: true, gone: false, tabChanged: false});
    pin.pin(null);
    assert.deepEqual(pin.answer(focused), {window: focused, focused: true, gone: false, tabChanged: false});
    assert.deepEqual(activated, []);
});

test('release disconnects from the window and forgets it, closed or not', () => {
    const {pin} = pinWithActivations();
    const editor = new StubWindow();
    pin.pin(editor);
    assert.equal(editor.connectedCount(), 1);
    pin.release();
    assert.equal(editor.connectedCount(), 0, 'unmanaged left connected');
    assert.equal(pin.window, null);

    pin.pin(editor);
    editor.emit('unmanaged');
    pin.release();
    const focused = new StubWindow();
    assert.deepEqual(pin.answer(focused), {window: focused, focused: true, gone: false, tabChanged: false},
        'a closed pin outlived its release');
});

test('pinning again moves the pin and disconnects from the old window', () => {
    const {pin} = pinWithActivations();
    const first = new StubWindow();
    const second = new StubWindow();
    pin.pin(first);
    pin.pin(second);
    assert.equal(first.connectedCount(), 0);
    assert.equal(pin.window, second);
    pin.release();
});

/*
 * Issue #106: a Ptyxis window holds several tabs, and the pin is the whole window, so a
 * tab switched to mid-dictation would take the text. Ptyxis offers no way to read or
 * restore the active tab, but its window title is the active tab's title: a pinned
 * Ptyxis window whose title is no longer the one at Insert gets no paste.
 */

test('a terminal title is compared without the glyphs and spaces before it', () => {
    assert.equal(normaliseTabTitle('✳ Fix the paste'), 'Fix the paste');
    assert.equal(normaliseTabTitle('⠐ Fix the paste'), 'Fix the paste');
    assert.equal(normaliseTabTitle('  · ✶ Fix the paste '), 'Fix the paste');
    assert.equal(normaliseTabTitle('~/repo'), 'repo');
    assert.equal(normaliseTabTitle('2 tabs'), '2 tabs', 'a leading digit is part of the title');
    assert.equal(normaliseTabTitle(''), '');
    assert.equal(normaliseTabTitle(null), '');
});

test('a Ptyxis tab still showing, its spinner moved on, is pasted into', () => {
    const {activated, pin} = pinWithActivations();
    const terminal = new StubWindow(PTYXIS, '✳ Fix the paste');
    pin.pin(terminal);
    terminal.title = '⠂ Fix the paste';
    assert.deepEqual(pin.answer(terminal), {window: terminal, focused: true, gone: false, tabChanged: false});
    assert.deepEqual(activated, []);
    pin.release();
});

test('another Ptyxis tab showing: no paste, and the window is not activated', () => {
    const {activated, pin} = pinWithActivations();
    const terminal = new StubWindow(PTYXIS, '✳ Fix the paste');
    pin.pin(terminal);
    terminal.title = '✳ Review the plan';
    assert.deepEqual(pin.answer(terminal), {window: terminal, focused: true, gone: false, tabChanged: true});
    const intruder = new StubWindow();
    assert.deepEqual(pin.answer(intruder), {window: terminal, focused: false, gone: false, tabChanged: true});
    assert.deepEqual(activated, [], 'a window showing the wrong tab was given focus');
    terminal.title = '✶ Fix the paste';
    assert.deepEqual(pin.answer(terminal), {window: terminal, focused: true, gone: false, tabChanged: false},
        'the tab shown again is pasted into');
    pin.release();
});

test('a title change in any other app is not a tab change', () => {
    const {pin} = pinWithActivations();
    const editor = new StubWindow('org.gnome.TextEditor', 'notes.txt');
    pin.pin(editor);
    editor.title = '• notes.txt (modified)';
    editor.title = 'other.txt';
    assert.deepEqual(pin.answer(editor), {window: editor, focused: true, gone: false, tabChanged: false});
    pin.release();
});

test('the title is taken at the pin, not at release or a later pin', () => {
    const {pin} = pinWithActivations();
    const terminal = new StubWindow(PTYXIS, 'first');
    pin.pin(terminal);
    pin.release();
    terminal.title = 'second';
    pin.pin(terminal);
    assert.equal(pin.answer(terminal).tabChanged, false, 'the title of an earlier pin was kept');
    pin.release();
});
