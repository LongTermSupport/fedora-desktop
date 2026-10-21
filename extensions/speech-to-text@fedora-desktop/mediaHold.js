/**
 * The panel's hold on the media while it records (Plan 00212).
 *
 * With the "Lower media volume while recording" switch on, each recording starts
 * ~/.local/bin/wsi-lower-media-volume, which lowers the media that is playing to a third of
 * its volume (or pauses a player whose sound it cannot find) and waits; TERM tells it the
 * microphone has closed, and it restores what it changed that nobody has changed since. It
 * exits at once when nothing was playing. The release is sent once, and never
 * after the hold has exited, since its PID may by then be another process's. A hold that
 * cannot start or ends badly is reported; the recording goes on without it.
 *
 * No GNOME imports: the spawn and the signal are handed in, so
 * tests/extensions/test-stt-media-hold.mjs runs it under Node.
 */

/** The GSettings key of the menu switch. */
export const LOWER_MEDIA_KEY = 'lower-media-volume-while-recording';

const JOURNAL_HINT = 'its reason is in the session journal (journalctl --user -b --no-pager -g wsi-lower-media-volume)';

/** Start the hold for one recording: null when `enabled` is false or the command could not
 * start, else `{release()}`. spawn(argv, onExit) returns the PID and calls onExit(how,
 * failed) once the command has ended; kill(pid) sends it TERM. */
export function startMediaHold({enabled, argv, spawn, kill, reportError, log}) {
    if (!enabled)
        return null;
    const hold = {pid: null, exited: false, released: false};
    try {
        hold.pid = spawn(argv, (how, failed) => {
            hold.exited = true;
            log(`wsi-lower-media-volume ${hold.pid} ended (${how})`);
            if (failed)
                reportError(`Lowering the media volume failed (${how}); ${JOURNAL_HINT}`);
        });
    } catch (e) {
        reportError(`wsi-lower-media-volume could not be started: ${e.message}`);
        return null;
    }
    log(`wsi-lower-media-volume ${hold.pid} holds the media for this recording`);
    return {
        release() {
            if (hold.released || hold.exited)
                return;
            hold.released = true;
            log(`The microphone has closed: TERM to wsi-lower-media-volume ${hold.pid}`);
            kill(hold.pid);
        },
    };
}
