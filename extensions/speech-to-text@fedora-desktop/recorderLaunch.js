/**
 * Start a recorder from the panel and learn when it ends (Plan 00156 Task 1.3, Plan 00171).
 *
 * The panel keeps the child's handle for its whole life, so an exit is noticed at once
 * however early or late it comes (CLAUDE/QA.md "ready-wait-ignores-child-exit"); what an
 * exit means is panelSession.js's decision. The recorder is started directly, with any
 * environment it needs set on the launcher: through a `/bin/bash -c` wrapper the PID the
 * panel held was the wrapper's, not the recorder's.
 */

import Gio from 'gi://Gio';

/** Start `argv` with `env` added to the panel's environment, and once the child has
 * ended call `onExit(how, failed)`: how is "exit status N" or "signal N", failed is true
 * for anything but exit status 0. Returns the child's PID. A spawn that fails throws. */
export function spawnWatched(argv, env, onExit) {
    const launcher = new Gio.SubprocessLauncher({flags: Gio.SubprocessFlags.NONE});
    for (const [name, value] of Object.entries(env))
        launcher.setenv(name, value, true);
    const proc = launcher.spawnv(argv);
    proc.wait_async(null, (source, result) => {
        source.wait_finish(result);
        if (source.get_if_exited())
            onExit(`exit status ${source.get_exit_status()}`, source.get_exit_status() !== 0);
        else
            onExit(`signal ${source.get_term_sig()}`, true);
    });
    return proc.get_identifier();
}
