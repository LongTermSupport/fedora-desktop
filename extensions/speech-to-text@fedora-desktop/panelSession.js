/**
 * The one recorder the panel follows, and what Insert, Escape, the Claude keys and the
 * article item do in each state (Plan 00171).
 *
 * Each launch gets a session token, which the recorder sends with every D-Bus signal. A
 * signal from any other recorder, or from this one after the state that ended its
 * session (SUCCESS, ERROR or IDLE), is dropped and logged: a finished recorder's late
 * IDLE once overwrote the next recorder's RECORDING, and the panel then believed it was
 * idle while a microphone was recording (RESEARCH-race.md).
 *
 * There is only ever one recording. The panel is ready (the plain mic icon) only when
 * no recorder it started is alive, so a press on the ready icon always starts one. While
 * recording, a press stops it (the graced stop: TERM). Before the microphone is open a
 * press cancels the start. Once the stop has been asked for (the grace, transcribing, the
 * paste, a result before the exit) a press does nothing but flash BLOCKED: nothing is
 * queued, and nothing starts by itself afterwards (the owner's rule).
 *
 * A live recorder the panel did not start (one recording across a screen lock, which
 * disables and re-enables the extension) is looked for when the panel is created and at
 * each press. While it lives the icon is UNOWNED, never ready; a press asks it for its
 * graced stop, unless its stop has begun (it names itself in the stopping file), and
 * starts nothing.
 *
 * A recorder's exit and its last signal can reach the panel in either order, so an exit
 * with no ending state yet is judged only after EXIT_SETTLE_MS.
 *
 * Each recorder the panel starts holds the media (mediaHold.js) from just before its spawn
 * until its microphone has closed, however the recording ends (Plan 00212).
 *
 * No GNOME imports: the spawn, signals, PID file, timers and the icon are handed in, so
 * every decision is tested under Node (tests/extensions/test-stt-panel-session.mjs).
 */

/** How long a launch may go without a report before the panel looks again. */
export const LAUNCH_SAFETY_MS = 10000;
/** How long SUCCESS and ERROR stay on the icon. */
export const ENDING_DISPLAY_MS = 2000;
/** How long the BLOCKED flash of a press that does nothing stays on the icon. */
export const BLOCKED_FLASH_MS = 700;
/** How often the panel looks whether a recorder it did not start is still alive. */
export const UNOWNED_POLL_MS = 250;
/** How long after a recorder's exit its last signal may still arrive. */
export const EXIT_SETTLE_MS = 500;

/** States that end a recorder's session; nothing it sends after one is taken. */
const ENDING = ['SUCCESS', 'ERROR', 'IDLE'];
/** States in which the microphone is not open yet: a press cancels the start. */
const STARTING = ['LAUNCHING', 'WAITING', 'PREPARING'];
/** Starting states in which batch and pre-buffer recorders already capture: a press stops. */
const CAPTURING = ['PREPARING'];
/** States in which the microphone is open: Escape discards. */
const MIC_OPEN = ['RECORDING', 'STOPPING'];

export class PanelSession {
    constructor({holdMedia, spawn, kill, abort, readRecorderPid, readStoppingPid, isAlive,
        newToken, show, log, reportError, notify, addTimeout, removeTimeout}) {
        this._holdMedia = holdMedia;
        this._spawn = spawn;
        this._kill = kill;
        this._abort = abort;
        this._readRecorderPid = readRecorderPid;
        this._readStoppingPid = readStoppingPid;
        this._isAlive = isAlive;
        this._newToken = newToken;
        this._show = show;
        this._log = log;
        this._reportError = reportError;
        this._notify = notify;
        this._addTimeout = addTimeout;
        this._removeTimeout = removeTimeout;

        this._session = null;   // the recorder the panel follows, until it has finished
        this._unowned = null;   // a live recorder the panel did not start, while it lives
        this._ending = null;    // SUCCESS or ERROR while it is on display
        this._displayTimer = null;
        this._blocked = null;   // the BLOCKED flash: its timer, and the state under it
        this._shown = null;
        this._destroyed = false;
        const stranger = this._readRecorderPid();
        if (stranger !== null) {
            this._log(`A recorder the panel did not start is running (PID ${stranger}); ` +
                'Insert stops it');
            this._watchUnowned(stranger);
        }
        this._render();
    }

    /** What the icon shows: IDLE (ready), LAUNCHING or FINISHING (the hourglass),
     * BLOCKED (a press that did nothing), or the state the recorder reported. */
    get display() {
        return this._shown;
    }

    /** The PID of the recorder the panel follows, or null. */
    get ownedPid() {
        return this._session?.pid ?? null;
    }

    /** Insert (a plain dictation) or Ctrl/Alt+Insert (Claude): `launch` says which, and
     * which window the text goes to. */
    insert(launch) {
        const session = this._session;
        if (!session) {
            this._insertWithoutSession(launch);
            return;
        }
        if (session.launch.kind === 'article' && !session.ended) {
            this._stopArticle(session);
            return;
        }
        const state = session.ended ?? session.state;
        if (session.exited) {
            // Never signalled: its PID is free for reuse
            this._block(`${state} and ${session.pid} has exited (its last signal is not in yet)`);
        } else if (STARTING.includes(state)) {
            if (session.cancelled) {
                this._block(`${state}: the start is already being cancelled`);
                return;
            }
            session.cancelled = true;
            session.pressedAs = CAPTURING.includes(state) ? 'STOPPING' : 'CANCELLING';
            this._log(`Insert while ${state}: cancelling the start (TERM to ${session.pid})`);
            this._kill(session.pid, 'TERM');
            this._render();
        } else if (state === 'RECORDING' && !session.stopAsked) {
            session.stopAsked = true;
            this._log(`Insert while RECORDING: stop after the grace (TERM to ${session.pid})`);
            this._kill(session.pid, 'TERM');
        } else {
            this._block(`${state}: ${session.pid} is stopping or finishing the dictation`);
        }
    }

    /** A press with no recorder of the panel's own: start, unless a recorder it did not
     * start is alive. That one is asked for its graced stop, once; a press while its stop
     * runs does nothing. */
    _insertWithoutSession(launch) {
        const stopping = this._readStoppingPid();
        if (stopping !== null) {
            if (this._unowned?.pid !== stopping)
                this._watchUnowned(stopping);
            this._block(`recorder ${stopping} is stopping`);
            return;
        }
        const stranger = this._readRecorderPid();
        if (stranger === null)
            this._start(launch);
        else if (this._unowned?.pid === stranger && this._unowned.stopAsked)
            this._block(`recorder ${stranger}, which the panel did not start, is stopping`);
        else
            this._stopUnowned(stranger);
    }

    /** Escape: discard the dictation, only while the microphone is open. */
    escape() {
        const session = this._session;
        if (!session || session.ended || !MIC_OPEN.includes(session.state)) {
            this._log(`Escape ignored: no microphone open (${this._shown})`);
            return;
        }
        this._log(`Escape: discarding the dictation of ${session.pid}`);
        this._releaseMedia(session);
        session.aborted = true;
        session.ended = 'IDLE';
        this._abort(session.pid, session.launch);
        this._render();
    }

    /** The menu's Create Article: only while the panel is ready. */
    article(launch) {
        const stranger = this._session || this._unowned
            ? null : this._readStoppingPid() ?? this._readRecorderPid();
        if (stranger !== null)
            this._watchUnowned(stranger);
        if (this._session || this._unowned) {
            this._log(`Create Article refused: the panel is busy (${this._unowned
                ? `recorder ${this._unowned.pid}, which the panel did not start` : this._shown})`);
            this._notify('Finish the current dictation before starting an article');
            this._render();
            return;
        }
        this._start(launch);
    }

    /** A StateChanged signal. True if it was the current recorder's and was taken. */
    onState(state, token) {
        const session = this._current(token, `${state}`);
        if (!session)
            return false;
        this._settleLaunch(session);
        session.pressedAs = null;
        if (!STARTING.includes(state))
            session.cancelled = false;   // the start was under way before the press
        if (!STARTING.includes(state) && !MIC_OPEN.includes(state))
            this._releaseMedia(session);  // the microphone has closed
        if (ENDING.includes(state)) {
            session.ended = state;
            if (state !== 'IDLE')
                this._startEndingDisplay(state);
            if (session.exited || session.launch.kind === 'article')
                this._finish(session);   // an article's window stays open; its recorder has ended
            else
                this._render();
            return true;
        }
        session.state = state;
        this._render();
        return true;
    }

    /** An Error signal: true if the panel should show its text. */
    onError(text, token) {
        return this._current(token, `Error "${text}"`) !== null;
    }

    /** A Progress signal: true if it is the current recorder's. */
    acceptsProgress(token) {
        return this._current(token, 'Progress', false) !== null;
    }

    /** The extension is being disabled: remove every timer, and take nothing more (a
     * recorder's exit can still be delivered). The recorder runs on. */
    destroy() {
        // Nothing would release the media later, so its volume comes back even if the recorder runs on
        this._releaseMedia(this._session);
        this._settleLaunch(this._session);
        this._clearExitSettle(this._session);
        this._clearUnownedPoll();
        this._unowned = null;
        this._clearBlocked();
        this._clearEndingDisplay();
        this._session = null;
        this._destroyed = true;
    }

    // ── internals ───────────────────────────────────────────────────────────────────

    /** The current session if `token` is its own and it has not ended, else null. */
    _current(token, what, logDrop = true) {
        const session = this._session;
        if (!session || !token || token !== session.token) {
            if (logDrop)
                this._log(`Dropped ${what} from session ${token || '(none)'}: ` +
                    `the current one is ${session?.token ?? '(none)'}`);
            return null;
        }
        if (session.ended) {
            if (logDrop)
                this._log(`Dropped ${what} from session ${token}: it already ended with ${session.ended}`);
            return null;
        }
        return session;
    }

    _start(launch) {
        this._clearEndingDisplay();
        const token = this._newToken();
        const session = {
            token, launch, pid: null, state: 'LAUNCHING', ended: null, pressedAs: null,
            exited: false, cancelled: false, stopAsked: false, aborted: false,
            safetyTimer: null, settleTimer: null, media: null,
        };
        this._session = session;
        // Before the spawn, so the music has stopped by the time the microphone opens
        session.media = this._holdMedia(launch);
        try {
            session.pid = this._spawn(launch, token, (how, failed) => this._exited(session, how, failed));
        } catch (e) {
            this._releaseMedia(session);
            this._session = null;
            this._reportError(`The recorder could not be started: ${e.message}`);
            this._render();
            return;
        }
        this._log(`Started ${launch.kind} recorder ${session.pid} (session ${token})`);
        if (this._session === session && !session.exited)
            session.safetyTimer = this._addTimeout(LAUNCH_SAFETY_MS, () => this._launchOverdue(session));
        this._render();
    }

    _launchOverdue(session) {
        session.safetyTimer = null;
        if (session !== this._session || session.state !== 'LAUNCHING' || session.ended)
            return false;
        if (!session.exited) {
            this._log(`Recorder ${session.pid} has not reported ${LAUNCH_SAFETY_MS / 1000} s after ` +
                'its launch; still following it');
            return false;
        }
        this._reportError(`The ${session.launch.kind} launcher handed off, but no recorder ` +
            `reported within ${LAUNCH_SAFETY_MS / 1000} s`);
        this._finish(session);
        return false;
    }

    /** The first report (or the end) of a launch: the safety timer is not needed. */
    _settleLaunch(session) {
        if (session?.safetyTimer) {
            this._removeTimeout(session.safetyTimer);
            session.safetyTimer = null;
        }
    }

    _exited(session, how, failed) {
        if (session !== this._session) {
            this._log(`Recorder ${session.pid} (session ${session.token}) ended (${how}); ` +
                'the panel had already finished with it');
            return;
        }
        session.exited = true;
        if (session.ended) {
            this._log(`Recorder ${session.pid} exited (${how}) after ${session.ended}`);
            this._finish(session);
            return;
        }
        if (session.launch.kind === 'article' && session.state === 'LAUNCHING' && !failed &&
            !session.cancelled) {
            // wsi-article-window hands a second launch to the window already open
            this._log(`Launched process ended (${how}) before a report; waiting for the ` +
                'recorder it handed to');
            return;
        }
        this._releaseMedia(session);   // whatever it was doing, its microphone is closed
        if (session.cancelled || session.aborted) {
            this._log(`Recorder ${session.pid} ended (${how}) as asked`);
            this._finish(session);
            return;
        }
        // Its last signal may still be on its way: the D-Bus message and the exit are
        // delivered separately
        this._log(`Recorder ${session.pid} exited (${how}) while ${session.state}; ` +
            `waiting ${EXIT_SETTLE_MS} ms for its last signal`);
        session.settleTimer = this._addTimeout(EXIT_SETTLE_MS, () => {
            session.settleTimer = null;
            if (session !== this._session || session.ended)
                return false;
            const when = session.state === 'LAUNCHING'
                ? 'before it started; its error is in the session journal'
                : `while ${session.state}, without saying how the dictation ended`;
            this._reportError(`The recorder ended (${how}) ${when}`);
            this._finish(session);
            return false;
        });
    }

    /** The media the recording lowered gets its volume back (mediaHold.js); once per session. */
    _releaseMedia(session) {
        if (session?.media) {
            session.media.release();
            session.media = null;
        }
    }

    _clearExitSettle(session) {
        if (session?.settleTimer) {
            this._removeTimeout(session.settleTimer);
            session.settleTimer = null;
        }
    }

    /** The session is over: the panel is ready. Nothing starts by itself. */
    _finish(session) {
        this._releaseMedia(session);
        this._settleLaunch(session);
        this._clearExitSettle(session);
        if (this._session === session)
            this._session = null;
        this._render();
    }

    _stopArticle(session) {
        // A launcher that handed off has exited: its PID is free for reuse, never signalled
        const launcherAlive = session.state === 'LAUNCHING' && !session.exited;
        const pid = launcherAlive ? session.pid : this._readRecorderPid();
        if (pid === null) {
            this._log('Insert during an article: no live article recorder to stop yet');
            this._notify('The article recorder is not running yet; press Insert again in a moment');
            return;
        }
        if (launcherAlive)
            session.cancelled = true;
        this._log(`Insert during an article: stopping it (TERM to ${pid})`);
        this._kill(pid, 'TERM');
    }

    /** A press on a recorder the panel did not start: ask for its graced stop (TERM: its
     * text is still pasted) and follow it until it exits. The press starts nothing. */
    _stopUnowned(pid) {
        const message = `A recording the panel did not start was running (PID ${pid}). ` +
            'It is stopping; press Insert again to record once the icon is ready.';
        this._log(message);
        this._notify(message);
        this._kill(pid, 'TERM');
        if (this._unowned?.pid !== pid)
            this._watchUnowned(pid);
        this._unowned.stopAsked = true;
        this._render();
    }

    _watchUnowned(pid) {
        this._clearUnownedPoll();
        this._unowned = {pid, pollTimer: null, stopAsked: false};
        this._pollUnowned();
    }

    _pollUnowned() {
        const unowned = this._unowned;
        unowned.pollTimer = this._addTimeout(UNOWNED_POLL_MS, () => {
            unowned.pollTimer = null;
            if (this._unowned !== unowned)
                return false;
            if (this._isAlive(unowned.pid)) {
                this._pollUnowned();
                return false;
            }
            this._log(`Recorder ${unowned.pid}, which the panel did not start, has exited`);
            this._unowned = null;
            this._render();
            return false;
        });
    }

    _clearUnownedPoll() {
        if (this._unowned?.pollTimer) {
            this._removeTimeout(this._unowned.pollTimer);
            this._unowned.pollTimer = null;
        }
    }

    /** A press that does nothing: say why in the log and flash BLOCKED on the icon. */
    _block(why) {
        this._log(`Insert does nothing while ${why}`);
        this._clearBlocked();
        const blocked = {under: this._underlying().display, timer: null};
        blocked.timer = this._addTimeout(BLOCKED_FLASH_MS, () => {
            blocked.timer = null;
            if (this._blocked === blocked) {
                this._blocked = null;
                this._render();
            }
            return false;
        });
        this._blocked = blocked;
        this._render();
    }

    _clearBlocked() {
        if (this._blocked?.timer)
            this._removeTimeout(this._blocked.timer);
        this._blocked = null;
    }

    _startEndingDisplay(state) {
        this._clearEndingDisplay();
        this._ending = state;
        this._displayTimer = this._addTimeout(ENDING_DISPLAY_MS, () => {
            this._displayTimer = null;
            this._ending = null;
            this._render();
            return false;
        });
    }

    _clearEndingDisplay() {
        if (this._displayTimer) {
            this._removeTimeout(this._displayTimer);
            this._displayTimer = null;
        }
        this._ending = null;
    }

    /** What the icon shows apart from a BLOCKED flash, and the recorder's launch. */
    _underlying() {
        const session = this._session;
        if (session && !session.ended)
            return {display: session.pressedAs ?? session.state, launch: session.launch};
        if (session)   // ended, but the recorder is still exiting: never the ready icon
            return {display: this._ending ?? 'FINISHING', launch: session.launch};
        return {display: this._unowned ? 'UNOWNED' : this._ending ?? 'IDLE', launch: null};
    }

    _render() {
        if (this._destroyed)
            return;
        const {display: under, launch} = this._underlying();
        // The flash lasts only while what is under it stays the same: a new state is shown
        const flashing = this._blocked?.under === under;
        if (!flashing)
            this._clearBlocked();
        const display = flashing ? 'BLOCKED' : under;
        if (display === this._shown && launch?.kind === this._shownKind)
            return;
        this._shown = display;
        this._shownKind = launch?.kind;
        this._log(`Display: ${display}`);
        this._show(display, launch);
    }
}
