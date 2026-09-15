/**
 * The `fedora-desktop` panel (Plan 00109, Phase 4).
 *
 * A read-only surface over the host status document. It renders what the checks said; it
 * runs no check of its own, applies no fix, and launches no play. A check reimplemented
 * here would be a second check that drifts from the one under test, and a clickable
 * surface is exactly where "offer, never apply" erodes.
 *
 * Three states, and the icon is where they matter most:
 *
 *   ok          neutral
 *   findings    attention
 *   unavailable ITS OWN icon, never neutral
 *
 * That third one is the whole reason this extension is shaped like this. A neutral icon
 * over an empty menu is what a healthy host looks like — and also what a missing
 * document, an unparseable one and a crashed producer look like. This plan exists because
 * a broken machine reported nothing and every automated check stayed green; showing
 * "nothing known" as "nothing wrong" would rebuild that in the panel.
 *
 * Sections are registered, not hardcoded (DESIGN-panel.md §5). Adding the play runner in
 * Task 4.3 means one array entry and one module.
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

import * as StatusDocument from './statusDocument.js';
import {section as healthSection} from './sections/health.js';

/**
 * The registry. Task 4.4's requirement, and the scaffold lands with a real consumer
 * rather than a placeholder, because a registry with nothing registered cannot be
 * exercised.
 */
const SECTIONS = [healthSection];

/** The document is rewritten once per graphical login, so this is a backstop for a
 * rewrite that happened while the shell was already running — not a check interval.
 * Re-reading a small local file is cheap; nothing here touches the network. */
const POLL_INTERVAL_SECONDS = 300;

const ICONS = {
    [StatusDocument.OK]: 'emblem-ok-symbolic',
    [StatusDocument.FINDINGS]: 'dialog-warning-symbolic',
    [StatusDocument.UNAVAILABLE]: 'dialog-question-symbolic',
};

export default class FedoraDesktopExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._indicator = null;
        this._icon = null;
        this._pollSourceId = null;
        this._readCancellable = null;
        this._runningKernel = '';
    }

    enable() {
        // Read ONCE, here. The running kernel cannot change without a reboot, and a
        // reboot ends this shell — so re-reading it per render would be repeated
        // synchronous I/O in the compositor process for an answer that cannot have
        // moved. Cached on the extension rather than in the module, so `disable()`
        // drops it with everything else.
        this._runningKernel = StatusDocument.runningKernel();

        this._indicator = new PanelMenu.Button(0.0, 'Fedora Desktop', false);
        this._icon = new St.Icon({
            icon_name: ICONS[StatusDocument.UNAVAILABLE],
            style_class: 'system-status-icon',
        });
        this._indicator.add_child(this._icon);
        Main.panel.addToStatusArea('fedora-desktop', this._indicator);

        // Starts as `unavailable` deliberately: until the first read lands, nothing IS
        // known. Starting neutral would state a clean bill of health for the window
        // between enable and the first callback.
        this._render(null);

        this._pollSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            POLL_INTERVAL_SECONDS,
            () => {
                this._refresh();
                return GLib.SOURCE_CONTINUE;
            }
        );

        this._refresh();
    }

    disable() {
        if (this._pollSourceId !== null) {
            GLib.source_remove(this._pollSourceId);
            this._pollSourceId = null;
        }
        if (this._readCancellable !== null) {
            this._readCancellable.cancel();
            this._readCancellable = null;
        }
        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }
        this._icon = null;
        // Back to "could not tell" rather than a stale value: a re-enable reads it again,
        // and an empty answer suppresses the boot claim instead of inventing one.
        this._runningKernel = '';
    }

    _refresh() {
        if (this._readCancellable !== null) {
            this._readCancellable.cancel();
        }
        this._readCancellable = new Gio.Cancellable();
        StatusDocument.read(this._readCancellable, document => {
            this._render(document);
        });
    }

    /** `document` may be null, meaning no read has completed yet. Every branch below
     * treats that as `unavailable`, which is what it is. */
    _render(document) {
        // A callback can land after disable(); the indicator is gone by then.
        if (!this._indicator || !this._icon) {
            return;
        }

        const ids = SECTIONS.flatMap(entry => entry.documentSections);
        const state = document === null
            ? StatusDocument.UNAVAILABLE
            : StatusDocument.overallState(document, ids, this._runningKernel);

        this._icon.icon_name = ICONS[state] ?? ICONS[StatusDocument.UNAVAILABLE];
        // `unavailable` gets its own colour rather than sharing the attention amber. It
        // is not a fault in the host and must not be read as one — but it is not quiet
        // either, so it does not share the neutral style.
        if (state === StatusDocument.FINDINGS) {
            this._icon.style = 'color: #ffaa00;';
        } else if (state === StatusDocument.UNAVAILABLE) {
            this._icon.style = 'color: #8ab4f8;';
        } else {
            this._icon.style = '';
        }

        const menu = this._indicator.menu;
        menu.removeAll();

        if (document === null) {
            const item = new PopupMenu.PopupMenuItem('reading host status…', {
                reactive: false,
            });
            menu.addMenuItem(item);
            return;
        }

        SECTIONS.forEach((entry, index) => {
            if (index > 0) {
                menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
            }
            entry.build(menu, document, Date.now(), this._runningKernel);
        });
    }
}
