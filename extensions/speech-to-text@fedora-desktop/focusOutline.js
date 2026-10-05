/**
 * An outline around the window a dictation pastes into while it runs: the window pinned
 * at Insert (pasteTarget.js), so the owner can see where the text will go.
 *
 * It stays on that window when focus moves (the panel gives the window focus back before
 * each paste), follows its moves and resizes, and hides when it closes. It takes no
 * input: the outline is a non-reactive border drawn above the windows, so clicks pass
 * through to them. It is hidden while the overview shows (from its 'showing' signal until
 * 'hidden'), where the window it frames is not where it appears.
 */

import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

const BORDER_PX = 3;

export class FocusOutline {
    constructor() {
        this._actor = null;
        this._overviewSignals = [];
        this._window = null;
        this._windowSignals = [];
    }

    /** Frame `window` (none: no outline) until hide(). */
    show(window) {
        if (this._actor && window === this._window)
            return;
        this.hide();
        if (!window)
            return;
        this._actor = new St.Widget({
            reactive: false,
            style: `border: ${BORDER_PX}px solid rgba(255, 68, 68, 0.9); border-radius: 12px;`,
        });
        Main.layoutManager.uiGroup.add_child(this._actor);
        this._overviewSignals = [
            Main.overview.connect('showing', () => this._actor.hide()),
            Main.overview.connect('hidden', () => this._place()),
        ];
        this._window = window;
        this._windowSignals = [
            window.connect('position-changed', () => this._place()),
            window.connect('size-changed', () => this._place()),
            // A closed window's signals are gone with it
            window.connect('unmanaged', () => {
                this._windowSignals = [];
                this._window = null;
                this._actor.hide();
            }),
        ];
        this._place();
    }

    hide() {
        if (!this._actor)
            return;
        for (const id of this._overviewSignals)
            Main.overview.disconnect(id);
        this._overviewSignals = [];
        for (const id of this._windowSignals)
            this._window.disconnect(id);
        this._windowSignals = [];
        this._window = null;
        this._actor.destroy();
        this._actor = null;
    }

    _place() {
        if (!this._window)
            return;
        const rect = this._window.get_frame_rect();
        this._actor.set_position(rect.x - BORDER_PX, rect.y - BORDER_PX);
        this._actor.set_size(rect.width + 2 * BORDER_PX, rect.height + 2 * BORDER_PX);
        if (Main.overview.visible)
            this._actor.hide();
        else
            this._actor.show();
    }
}
