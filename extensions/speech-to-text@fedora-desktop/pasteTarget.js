/**
 * The window a dictation pastes into: the one focused at Insert, pinned until the
 * dictation ends. A focus change in between (a stray click, a window that steals focus)
 * is treated as an accident, never as a new target.
 *
 * The recorder asks before each paste (PasteKey), and `answer()` says one of three
 * things: the pinned window has focus, so paste; it has not, so it is given focus back
 * (the Shell may activate a window on Wayland where an app may not, switching workspace
 * if need be) and the recorder asks again; or it was closed, so nothing is pasted. With
 * no pin (nothing was focused at Insert, or the recorder was started by hand) the
 * focused window is the target, as it always was.
 *
 * A Ptyxis window holds tabs, and nothing outside Ptyxis can read or choose its active
 * tab, so the tab cannot be given back. Its window title is the active tab's title, so
 * the title at Insert is kept, and a different title before a paste says another tab is
 * showing: tabChanged, nothing is pasted and the window is not activated. The reliable
 * answer is upstream: Ptyxis exposing the active tab's UUID for its focus-tab-by-uuid.
 */

import * as Main from 'resource:///org/gnome/shell/ui/main.js';

const PTYXIS_WM_CLASS = 'org.gnome.Ptyxis';

/** A tab title without the glyphs and spaces before it (Claude Code puts a spinner there). */
export function normaliseTabTitle(title) {
    return (title ?? '').replace(/^[^\p{L}\p{N}]+/u, '').trim();
}

/** The active tab's title of a Ptyxis window, or null for any other window. */
function tabTitle(window) {
    return window.get_wm_class() === PTYXIS_WM_CLASS ? normaliseTabTitle(window.get_title()) : null;
}

export class PasteTargetPin {
    constructor(activate = window => Main.activateWindow(window)) {
        this._activate = activate;
        this._window = null;
        this._tabTitle = null;
        this._unmanagedId = null;
        this._gone = false;
    }

    /** The pinned window, or null (none pinned, or it was closed). */
    get window() {
        return this._window;
    }

    pin(window) {
        this.release();
        if (!window)
            return;
        this._window = window;
        this._tabTitle = tabTitle(window);
        this._unmanagedId = window.connect('unmanaged', () => {
            // A closed window's signals are gone with it
            this._unmanagedId = null;
            this._window = null;
            this._gone = true;
        });
    }

    release() {
        if (this._unmanagedId !== null)
            this._window.disconnect(this._unmanagedId);
        this._unmanagedId = null;
        this._window = null;
        this._tabTitle = null;
        this._gone = false;
    }

    /** {window, focused, gone, tabChanged} for a paste about to happen, given the focused window. */
    answer(focusWindow) {
        if (this._gone)
            return {window: null, focused: false, gone: true, tabChanged: false};
        if (!this._window)
            return {window: focusWindow, focused: true, gone: false, tabChanged: false};
        const focused = focusWindow === this._window;
        if (this._tabTitle !== null && tabTitle(this._window) !== this._tabTitle)
            return {window: this._window, focused, gone: false, tabChanged: true};
        if (!focused)
            this._activate(this._window);
        return {window: this._window, focused, gone: false, tabChanged: false};
    }
}
