/**
 * Dock recovery on unlock (Plan 00109 Task 5.4a, DESIGN-panel.md §12).
 *
 * The DisplayLink recovery repaints a desktop background that mutter left black after a
 * monitor change (mutter#4767). The dock's udev rule and the resume service both start it,
 * but after a resume the screen is still locked, and the recovery rightly refuses to
 * repaint a locked session (gnome-shell#9188). Nothing started it again at the unlock.
 * This extension does, and does nothing else.
 *
 * It decides nothing about the display. On each unlock it starts
 * `displaylink-dock-recovery.service`, the same unit the udev rule starts, and that run
 * reads the heads, the lock state and the journal and picks its own action, which is
 * usually none. A polkit rule deployed with it lets the desktop user start that one unit
 * from the active local session without a password prompt.
 *
 * Why a separate extension: the `fedora-desktop` panel is detect-only. It applies no fix
 * and launches only what a person clicked. Starting a recovery with nobody clicking would
 * end that, so the one action lives here instead.
 *
 * `session-modes` includes `unlock-dialog`. Without it the shell disables this extension
 * at the lock and enables it after the unlock, so it would never see the signal.
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

export const RECOVERY_UNIT = 'displaylink-dock-recovery.service';

/** `--no-block`: the shell must never wait on a recovery run that takes seconds. */
export const START_RECOVERY_ARGV = ['systemctl', 'start', '--no-block', RECOVERY_UNIT];

/** The shell updates logind's LockedHint asynchronously as it unlocks, and the recovery
 * skips any session whose hint still says locked. The wait gives that update time to
 * land, and any monitor change the unlock set off time to settle. */
export const SETTLE_SECONDS = 5;

export default class DockRecoveryOnUnlockExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._lockedChangedId = null;
        this._pendingStartId = null;
    }

    enable() {
        if (Main.screenShield === null) {
            log(`${this.metadata.uuid}: this session has no lock screen, so there is ` +
                'no unlock to start the dock recovery on');
            return;
        }
        this._lockedChangedId = Main.screenShield.connect(
            'locked-changed', () => this._onLockedChanged());
    }

    disable() {
        if (this._lockedChangedId !== null) {
            Main.screenShield.disconnect(this._lockedChangedId);
            this._lockedChangedId = null;
        }
        this._cancelPendingStart();
    }

    _onLockedChanged() {
        if (Main.screenShield.locked) {
            this._cancelPendingStart();
            return;
        }
        if (this._pendingStartId !== null) {
            return;
        }
        this._pendingStartId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT, SETTLE_SECONDS, () => {
                this._pendingStartId = null;
                this._startRecovery();
                return GLib.SOURCE_REMOVE;
            });
    }

    _cancelPendingStart() {
        if (this._pendingStartId !== null) {
            GLib.source_remove(this._pendingStartId);
            this._pendingStartId = null;
        }
    }

    _startRecovery() {
        const command = START_RECOVERY_ARGV.join(' ');
        let proc;
        try {
            proc = Gio.Subprocess.new(START_RECOVERY_ARGV, Gio.SubprocessFlags.STDERR_PIPE);
        } catch (e) {
            log(`${this.metadata.uuid}: could not run ${command}: ${e.message}`);
            return;
        }
        proc.communicate_utf8_async(null, null, (finished, result) => {
            try {
                const [, , stderr] = finished.communicate_utf8_finish(result);
                if (!finished.get_successful()) {
                    log(`${this.metadata.uuid}: ${command} exited ` +
                        `${finished.get_exit_status()}: ${(stderr ?? '').trim()}`);
                }
            } catch (e) {
                log(`${this.metadata.uuid}: ${command} could not be waited on: ${e.message}`);
            }
        });
    }
}
