/**
 * Launching the on-demand command in the user's default terminal — the ONE way this panel
 * starts a process (DESIGN-panel.md §6).
 *
 * Two rows use it: the health section's "open the full report" (Plan 00136) and the play
 * runner (Task 4.3). Both hand a person to `fedora-desktop-health` in a terminal they are
 * looking at, so there is one launcher and one set of failure messages rather than two
 * copies that drift.
 *
 * An argv, never an interpolated command string: the path carries the home directory and
 * a play name comes from a state file, and GLib's command-line form would word-split both.
 * `Gio.Subprocess.new` reports only whether the TERMINAL started; what happens inside it
 * is the command's business, and it holds its own window open on an error.
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';

import * as StatusDocument from './statusDocument.js';

/**
 * Run `fedora-desktop-health <args...>` in a terminal. Returns true when the terminal
 * started. `byHand` is what to tell the user to type if it did not.
 *
 * Both failures are said rather than swallowed: the command is not installed (the report
 * play has not run on this host — a terminal flashing "command not found" and closing
 * would tell the user nothing), and the terminal itself cannot start.
 */
export function launchOnDemand(args, byHand) {
    const command = StatusDocument.onDemandCommandPath();
    if (!GLib.file_test(command, GLib.FileTest.IS_EXECUTABLE)) {
        Main.notify('Fedora Desktop',
            `${StatusDocument.ON_DEMAND_COMMAND} is not installed here; ` +
            'run play-host-health-login-report.yml');
        return false;
    }
    try {
        Gio.Subprocess.new(['xdg-terminal-exec', command, ...args], Gio.SubprocessFlags.NONE);
    } catch (e) {
        Main.notify('Fedora Desktop',
            `Could not open a terminal through xdg-terminal-exec (${e.message}). ` +
            `Run ${byHand} in one.`);
        return false;
    }
    return true;
}
