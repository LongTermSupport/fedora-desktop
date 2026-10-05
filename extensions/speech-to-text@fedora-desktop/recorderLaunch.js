/**
 * A recorder launched from the panel, waited for until it reports.
 *
 * Insert spawns a recorder and ignores further presses until the recorder's first D-Bus
 * StateChanged signal, so a second press cannot start a second recorder that clobbers the
 * shared PID file. The recorder is watched while that wait lasts: if it fails before
 * reporting (a crash, a missing interpreter), the wait ends at once and the failure is
 * reported with how it ended, instead of the panel ignoring Insert until a safety timer
 * runs out (CLAUDE/QA.md "ready-wait-ignores-child-exit"). A clean exit before the report
 * is only logged: wsi-article-window hands a second launch to the window already open
 * and exits 0, and the recorder that window starts is the one that reports. The safety
 * timer stays, for a recorder that neither reports nor fails.
 */

import GLib from 'gi://GLib';
import Gio from 'gi://Gio';

export const LAUNCH_SAFETY_MS = 10000;

/** Spawn `command` as GLib.spawn_command_line_async does, and once the child has ended
 * call `onExit(how, failed)`: how is "exit status N" or "signal N", failed is true for
 * anything but exit status 0. */
export function spawnWatched(command, onExit) {
    const [, argv] = GLib.shell_parse_argv(command);
    const proc = Gio.Subprocess.new(argv, Gio.SubprocessFlags.NONE);
    proc.wait_async(null, (source, result) => {
        source.wait_finish(result);
        if (source.get_if_exited())
            onExit(`exit status ${source.get_exit_status()}`, source.get_exit_status() !== 0);
        else
            onExit(`signal ${source.get_term_sig()}`, true);
    });
}

export class RecorderLaunch {
    constructor({
        onExitBeforeReport,
        log,
        spawn = spawnWatched,
        addTimeout = (ms, handler) => GLib.timeout_add(GLib.PRIORITY_DEFAULT, ms, handler),
        removeTimeout = id => GLib.Source.remove(id),
    }) {
        this._onExitBeforeReport = onExitBeforeReport;
        this._log = log;
        this._spawn = spawn;
        this._addTimeout = addTimeout;
        this._removeTimeout = removeTimeout;
        this._pending = false;
        this._timeoutId = null;
        this._generation = 0;
    }

    /** True from a launch until its recorder reports, exits, or the safety timer ends. */
    get pending() {
        return this._pending;
    }

    /** Spawn `command` and wait for its first report. A spawn that fails is raised, with
     * nothing left pending. */
    begin(command) {
        this.settle();
        const generation = ++this._generation;
        this._pending = true;
        this._timeoutId = this._addTimeout(LAUNCH_SAFETY_MS, () => {
            this._timeoutId = null;
            this._pending = false;
            this._log('Launch debounce expired without a DBus state signal');
            return GLib.SOURCE_REMOVE;
        });
        try {
            this._spawn(command, (how, failed) => {
                if (generation !== this._generation || !this._pending)
                    return;
                if (!failed) {
                    this._log(`Launched process ended (${how}) before a DBus state signal; waiting for the recorder it handed to`);
                    return;
                }
                this.settle();
                this._onExitBeforeReport(how);
            });
        } catch (e) {
            this.settle();
            throw e;
        }
    }

    /** The recorder reported (or the panel stopped it): the wait is over. */
    settle() {
        this._pending = false;
        if (this._timeoutId !== null) {
            this._removeTimeout(this._timeoutId);
            this._timeoutId = null;
        }
    }
}
