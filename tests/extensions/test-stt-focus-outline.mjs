/**
 * The speech-to-text focus outline stays off the GNOME overview (Plan 00148 Task 9.9).
 *
 *     node --test tests/extensions/test-stt-focus-outline.mjs
 *
 * While a dictation runs, the panel outlines the focused window, where the next paste
 * goes. The outline sits in the shell's uiGroup, above everything, so it was drawn over
 * the overview too, framing a window that is not where it appears there. It now hides
 * while the overview shows and comes back when it has closed, and every signal it
 * connected is disconnected when it is hidden. Driven against the shipped focusOutline.js.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const {FocusOutline} = await import(
    new URL('../../extensions/speech-to-text@fedora-desktop/focusOutline.js', import.meta.url).href);
const {SignalSource, overview, layoutManager} = await import('./gi-stubs.mjs');

class StubWindow extends SignalSource {
    get_frame_rect() {
        return {x: 100, y: 50, width: 800, height: 600};
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

test('the outline frames the focused window', () => {
    desktop();
    const outline = new FocusOutline();
    outline.show();
    assert.equal(outlineActor().visible, true);
    assert.deepEqual(outlineActor().position, [97, 47]);
    outline.hide();
});

test('the outline hides while the overview shows, and returns once it has closed', () => {
    desktop();
    const outline = new FocusOutline();
    outline.show();
    overview.setVisible(true);
    assert.equal(outlineActor().visible, false, 'drawn over the overview');
    overview.setVisible(false);
    assert.equal(outlineActor().visible, true, 'not back after the overview closed');
    outline.hide();
});

test('a dictation started from the overview is not outlined until it closes', () => {
    desktop();
    overview.setVisible(true);
    const outline = new FocusOutline();
    outline.show();
    assert.equal(outlineActor().visible, false);
    overview.setVisible(false);
    assert.equal(outlineActor().visible, true);
    outline.hide();
});

test('a focus change during the overview does not bring the outline back over it', () => {
    const display = desktop();
    const outline = new FocusOutline();
    outline.show();
    overview.setVisible(true);
    display.focus_window = new StubWindow();
    display.emit('notify::focus-window');
    assert.equal(outlineActor().visible, false);
    outline.hide();
});

test('hiding the outline disconnects every signal it connected and destroys it', () => {
    const display = desktop();
    const window = display.focus_window;
    const outline = new FocusOutline();
    outline.show();
    const actor = outlineActor();
    outline.hide();
    assert.equal(overview.connectedCount(), 0, 'overview signals left connected');
    assert.equal(display.connectedCount(), 0, 'display signals left connected');
    assert.equal(window.connectedCount(), 0, 'window signals left connected');
    assert.equal(actor.destroyed, true);
});
