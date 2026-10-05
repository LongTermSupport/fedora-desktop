/**
 * The speech-to-text outline frames the window a dictation pastes into (Plan 00148
 * Tasks 9.9, 9.10).
 *
 *     node --test tests/extensions/test-stt-focus-outline.mjs
 *
 * While a dictation runs, the panel outlines the window pinned at Insert, where every
 * paste goes. It stays on that window when focus moves (a focus change mid-dictation is
 * an accident the panel undoes before pasting), and hides when the window closes. The
 * outline sits in the shell's uiGroup, above everything, so it was drawn over the
 * overview too, framing a window that is not where it appears there. It hides while the
 * overview shows and comes back when it has closed, and every signal it connected is
 * disconnected when it is hidden. Driven against the shipped focusOutline.js.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {FocusOutline} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/focusOutline.js', import.meta.url).href);
const {SignalSource, overview, layoutManager} = await import('./gi-stubs.mjs');

class StubWindow extends SignalSource {
    constructor(rect = {x: 100, y: 50, width: 800, height: 600}) {
        super();
        this.rect = rect;
    }

    get_frame_rect() {
        return this.rect;
    }
}

function desktop() {
    overview.reset();
    layoutManager.uiGroup.children = [];
    const display = new SignalSource();
    display.focus_window = new StubWindow();
    globalThis.global = {display};
    return display;
}

function outlineActor() {
    return layoutManager.uiGroup.children.at(-1);
}

test('the outline frames the window it is given', () => {
    const display = desktop();
    const outline = new FocusOutline();
    outline.show(display.focus_window);
    assert.equal(outlineActor().visible, true);
    assert.deepEqual(outlineActor().position, [97, 47]);
    outline.hide();
});

test('the outline stays on the pinned window when focus moves', () => {
    const display = desktop();
    const pinned = display.focus_window;
    const outline = new FocusOutline();
    outline.show(pinned);
    display.focus_window = new StubWindow({x: 900, y: 10, width: 300, height: 200});
    display.emit('notify::focus-window');
    assert.deepEqual(outlineActor().position, [97, 47], 'the outline followed focus');
    assert.equal(outlineActor().visible, true);
    pinned.rect = {x: 200, y: 60, width: 800, height: 600};
    pinned.emit('position-changed');
    assert.deepEqual(outlineActor().position, [197, 57], 'the outline did not follow its window');
    outline.hide();
});

test('the pinned window closing hides the outline, and focus elsewhere does not bring it back', () => {
    const display = desktop();
    const pinned = display.focus_window;
    const outline = new FocusOutline();
    outline.show(pinned);
    pinned.emit('unmanaged');
    assert.equal(outlineActor().visible, false);
    display.focus_window = new StubWindow();
    display.emit('notify::focus-window');
    overview.setVisible(true);
    overview.setVisible(false);
    assert.equal(outlineActor().visible, false);
    outline.hide();
});

test('no window to frame shows no outline', () => {
    desktop();
    const outline = new FocusOutline();
    outline.show(null);
    assert.equal(layoutManager.uiGroup.children.length, 0);
    outline.hide();
});

test('the outline hides while the overview shows, and returns once it has closed', () => {
    const display = desktop();
    const outline = new FocusOutline();
    outline.show(display.focus_window);
    overview.setVisible(true);
    assert.equal(outlineActor().visible, false, 'drawn over the overview');
    overview.setVisible(false);
    assert.equal(outlineActor().visible, true, 'not back after the overview closed');
    outline.hide();
});

test('a dictation started from the overview is not outlined until it closes', () => {
    const display = desktop();
    overview.setVisible(true);
    const outline = new FocusOutline();
    outline.show(display.focus_window);
    assert.equal(outlineActor().visible, false);
    overview.setVisible(false);
    assert.equal(outlineActor().visible, true);
    outline.hide();
});

test('a move of the window during the overview does not bring the outline back over it', () => {
    const display = desktop();
    const pinned = display.focus_window;
    const outline = new FocusOutline();
    outline.show(pinned);
    overview.setVisible(true);
    pinned.emit('position-changed');
    assert.equal(outlineActor().visible, false);
    outline.hide();
});

test('hiding the outline disconnects every signal it connected and destroys it', () => {
    const display = desktop();
    const window = display.focus_window;
    const outline = new FocusOutline();
    outline.show(window);
    const actor = outlineActor();
    outline.hide();
    assert.equal(overview.connectedCount(), 0, 'overview signals left connected');
    assert.equal(display.connectedCount(), 0, 'display signals left connected');
    assert.equal(window.connectedCount(), 0, 'window signals left connected');
    assert.equal(actor.destroyed, true);
});
