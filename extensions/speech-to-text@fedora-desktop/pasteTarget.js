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
 */

import * as Main from 'resource:///org/gnome/shell/ui/main.js';

export class PasteTargetPin {
    constructor(activate = window => Main.activateWindow(window)) {
        this._activate = activate;
        this._window = null;
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
        this._gone = false;
    }

    /** {window, focused, gone} for a paste about to happen, given the focused window. */
    answer(focusWindow) {
        if (this._gone)
            return {window: null, focused: false, gone: true};
        if (!this._window)
            return {window: focusWindow, focused: true, gone: false};
        if (focusWindow === this._window)
            return {window: this._window, focused: true, gone: false};
        this._activate(this._window);
        return {window: this._window, focused: false, gone: false};
    }
}
