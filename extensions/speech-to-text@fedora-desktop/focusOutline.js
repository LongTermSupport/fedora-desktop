/**
 * An outline around the focused window while a dictation runs: that window is where
 * the next paste lands (the recorder asks the panel which window is focused at each
 * paste), so the owner can see it before the text arrives.
 *
 * It follows focus changes, moves and resizes, and takes no input: the outline is a
 * non-reactive border drawn above the windows, so clicks pass through to them. It is
 * hidden while the overview shows (from its 'showing' signal until 'hidden'), where the
 * window it frames is not where it appears.
 */

import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

const BORDER_PX = 3;

export class FocusOutline {
    constructor() {
        this._actor = null;
        this._focusSignal = null;
        this._overviewSignals = [];
        this._window = null;
        this._windowSignals = [];
    }

    show() {
        if (this._actor)
            return;
        this._actor = new St.Widget({
            reactive: false,
            style: `border: ${BORDER_PX}px solid rgba(255, 68, 68, 0.9); border-radius: 12px;`,
        });
        Main.layoutManager.uiGroup.add_child(this._actor);
        this._focusSignal = global.display.connect('notify::focus-window', () => this._follow());
        this._overviewSignals = [
            Main.overview.connect('showing', () => this._actor.hide()),
            Main.overview.connect('hidden', () => this._follow()),
        ];
        this._follow();
    }

    hide() {
        if (!this._actor)
            return;
        global.display.disconnect(this._focusSignal);
        this._focusSignal = null;
        for (const id of this._overviewSignals)
            Main.overview.disconnect(id);
        this._overviewSignals = [];
        this._release();
        this._actor.destroy();
        this._actor = null;
    }

    _follow() {
        this._release();
        this._window = global.display.focus_window;
        if (!this._window) {
            this._actor.hide();
            return;
        }
        this._windowSignals = [
            this._window.connect('position-changed', () => this._place()),
            this._window.connect('size-changed', () => this._place()),
            // A closed window's signals are gone with it; focus moves on by itself
            this._window.connect('unmanaged', () => {
                this._windowSignals = [];
                this._window = null;
                this._actor?.hide();
            }),
        ];
        this._place();
    }

    _release() {
        for (const id of this._windowSignals)
            this._window.disconnect(id);
        this._windowSignals = [];
        this._window = null;
    }

    _place() {
        const rect = this._window.get_frame_rect();
        this._actor.set_position(rect.x - BORDER_PX, rect.y - BORDER_PX);
        this._actor.set_size(rect.width + 2 * BORDER_PX, rect.height + 2 * BORDER_PX);
        if (Main.overview.visible)
            this._actor.hide();
        else
            this._actor.show();
    }
}
