/**
 * Start a recorder from the panel and learn when it ends (Plan 00156 Task 1.3, Plan 00171).
 *
 * The panel keeps the child's handle for its whole life, so an exit is noticed at once
 * however early or late it comes (CLAUDE/QA.md "ready-wait-ignores-child-exit"); what an
 * exit means is panelSession.js's decision. The recorder is started directly, with any
 * environment it needs set on the launcher: through a `/bin/bash -c` wrapper the PID the
 * panel held was the wrapper's, not the recorder's.
 *
 * The one thing in between is setsid, which puts the recorder in a process group of its
 * own and then execs it (a child of GNOME Shell is not a group leader, so setsid does not
 * fork): the PID stays the recorder's, and Escape can kill the group, which takes the
 * recorder's pw-record with it.
 */

import Gio from 'gi://Gio';

export const SETSID = '/usr/bin/setsid';

/** The signals the panel sends a recorder. */
const RECORDER_SIGNALS = ['TERM', 'INT', 'USR1', 'KILL'];

/** Start `argv` with `env` added to the panel's environment, and once the child has
 * ended call `onExit(how, failed)`: how is "exit status N" or "signal N", failed is true
 * for anything but exit status 0. Returns the child's PID. A spawn that fails throws. */
export function spawnWatched(argv, env, onExit) {
    const launcher = new Gio.SubprocessLauncher({flags: Gio.SubprocessFlags.NONE});
    for (const [name, value] of Object.entries(env))
        launcher.setenv(name, value, true);
    const proc = launcher.spawnv([SETSID, ...argv]);
    proc.wait_async(null, (source, result) => {
        source.wait_finish(result);
        if (source.get_if_exited())
            onExit(`exit status ${source.get_exit_status()}`, source.get_exit_status() !== 0);
        else
            onExit(`signal ${source.get_term_sig()}`, true);
    });
    return proc.get_identifier();
}

/** The kill(1) argv sending `signalName` to recorder `pid`, or to its whole process
 * group with `group`. Throws for anything but a numeric PID and a recorder signal. */
export function killArgv(pid, signalName, group) {
    if (!/^[0-9]+$/.test(String(pid)) || !RECORDER_SIGNALS.includes(signalName))
        throw new Error(`refusing to send "${signalName}" to "${pid}"`);
    return ['kill', `-${signalName}`, '--', group ? `-${pid}` : String(pid)];
}

/** Escape's signal for the recorder of `launch`. In server mode the audio is the warm
 * server's, so the recorder is asked (USR1) to ABORT it; any other recorder is killed
 * with its whole group, before anything is transcribed. */
export function discardSignal(launch) {
    return launch.serverMode ? {signal: 'USR1', group: false} : {signal: 'KILL', group: true};
}
