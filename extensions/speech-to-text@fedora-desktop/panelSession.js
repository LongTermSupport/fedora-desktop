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
 * The panel is ready (the plain mic icon) only when no recorder it started is alive, so
 * a press on the ready icon always starts a recording. A press while not ready is never
 * ignored: in the stop grace it stops at once, before the microphone is open it cancels
 * the start, and once the microphone has closed it shows the hourglass and queues a
 * start that runs when the recorder has exited (a second press cancels that). A live
 * recorder the panel did not start, found at a press, is stopped and reported, and the
 * start waits for it to exit.
 *
 * No GNOME imports: the spawn, signals, PID file, timers and the icon are handed in, so
 * every decision is tested under Node (tests/extensions/test-stt-panel-session.mjs).
 */

/** How long a launch may go without a report before the panel looks again. */
export const LAUNCH_SAFETY_MS = 10000;
/** How long SUCCESS and ERROR stay on the icon. */
export const ENDING_DISPLAY_MS = 2000;
/** How often a start waiting for a recorder the panel did not start looks again. */
export const UNOWNED_POLL_MS = 250;

/** States that end a recorder's session; nothing it sends after one is taken. */
const ENDING = ['SUCCESS', 'ERROR', 'IDLE'];
/** States in which the microphone is not open yet: a press cancels the start. */
const STARTING = ['LAUNCHING', 'WAITING', 'PREPARING'];
/** States in which the microphone is open: a press stops (graced, then at once). */
const MIC_OPEN = ['RECORDING', 'STOPPING'];

export class PanelSession {
    constructor({spawn, kill, abort, readRecorderPid, isAlive, newToken, show, log,
        reportError, notify, addTimeout, removeTimeout}) {
        this._spawn = spawn;
        this._kill = kill;
        this._abort = abort;
        this._readRecorderPid = readRecorderPid;
        this._isAlive = isAlive;
        this._newToken = newToken;
        this._show = show;
        this._log = log;
        this._reportError = reportError;
        this._notify = notify;
        this._addTimeout = addTimeout;
        this._removeTimeout = removeTimeout;

        this._session = null;   // the recorder the panel follows, until it has finished
        this._queued = null;    // a start waiting for a recorder to exit
        this._ending = null;    // SUCCESS or ERROR while it is on display
        this._displayTimer = null;
        this._shown = null;
        this._destroyed = false;
        this._render();
    }

    /** What the icon shows: IDLE (ready), LAUNCHING, QUEUED or FINISHING (the
     * hourglass), or the state the recorder reported. */
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
        if (this._queued) {
            this._cancelQueued('a second press');
            return;
        }
        const session = this._session;
        if (!session) {
            this._startWhenReady(launch);
            return;
        }
        if (session.launch.kind === 'article' && !session.ended) {
            this._stopArticle(session);
            return;
        }
        const state = session.ended ? 'ENDED' : session.state;
        if (STARTING.includes(state)) {
            session.cancelled = true;
            this._log(`Insert while ${state}: cancelling the start (TERM to ${session.pid})`);
            this._kill(session.pid, 'TERM');
        } else if (MIC_OPEN.includes(state)) {
            this._log(`Insert while ${state}: stop (TERM to ${session.pid})`);
            this._kill(session.pid, 'TERM');
        } else {
            this._log(`Insert while ${state === 'ENDED' ? session.ended : state} and ${session.pid} ` +
                'is still finishing: the start is queued until it exits');
            this._queue({launch, waitFor: 'owned'});
        }
    }

    /** Escape: discard the dictation, only while the microphone is open. */
    escape() {
        const session = this._session;
        if (!session || session.ended || !MIC_OPEN.includes(session.state)) {
            this._log(`Escape ignored: no microphone open (${this._shown})`);
            return;
        }
        this._log(`Escape: discarding the dictation of ${session.pid}`);
        session.aborted = true;
        session.ended = 'IDLE';
        this._abort(session.pid, session.launch);
        this._render();
    }

    /** The menu's Create Article: only while the panel is ready. */
    article(launch) {
        if (this._session || this._queued) {
            this._log(`Create Article refused: the panel is busy (${this._shown})`);
            this._notify('Finish the current dictation before starting an article');
            return;
        }
        this._startWhenReady(launch);
    }

    /** A StateChanged signal. True if it was the current recorder's and was taken. */
    onState(state, token) {
        const session = this._current(token, `${state}`);
        if (!session)
            return false;
        this._settleLaunch(session);
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
        this._settleLaunch(this._session);
        this._clearQueuedPoll();
        this._queued = null;
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

    /** Start now, unless a recorder the panel did not start is alive: that is stopped,
     * reported, and the start waits for it. */
    _startWhenReady(launch) {
        const stranger = this._readRecorderPid();
        if (stranger === null) {
            this._start(launch);
            return;
        }
        const message = `A recording the panel did not start was still running (PID ${stranger}). ` +
            'It was stopped; yours starts once it has finished.';
        this._log(message);
        this._notify(message);
        this._kill(stranger, 'INT');
        this._queue({launch, waitFor: stranger});
    }

    _start(launch) {
        this._clearEndingDisplay();
        const token = this._newToken();
        const session = {
            token, launch, pid: null, state: 'LAUNCHING', ended: null,
            exited: false, cancelled: false, aborted: false, safetyTimer: null,
        };
        this._session = session;
        try {
            session.pid = this._spawn(launch, token, (how, failed) => this._exited(session, how, failed));
        } catch (e) {
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
        if (session.state === 'LAUNCHING' && !failed && !session.cancelled) {
            // wsi-article-window hands a second launch to the window already open
            this._log(`Launched process ended (${how}) before a report; waiting for the ` +
                'recorder it handed to');
            return;
        }
        if (session.cancelled || session.aborted) {
            this._log(`Recorder ${session.pid} ended (${how}) as asked`);
        } else {
            const when = session.state === 'LAUNCHING'
                ? 'before it started; its error is in the session journal'
                : `while ${session.state}, without saying how the dictation ended`;
            this._reportError(`The recorder ended (${how}) ${when}`);
        }
        this._finish(session);
    }

    /** The session is over: the panel is ready, and a start queued behind it runs. */
    _finish(session) {
        this._settleLaunch(session);
        if (this._session === session)
            this._session = null;
        const queued = this._queued;
        if (queued?.waitFor === 'owned') {
            this._queued = null;
            this._log('The recorder has finished: running the queued start');
            this._startWhenReady(queued.launch);
            return;
        }
        this._render();
    }

    _stopArticle(session) {
        const pid = session.state === 'LAUNCHING' ? session.pid : this._readRecorderPid();
        if (pid === null) {
            this._log('Insert during an article: no live article recorder to stop yet');
            this._notify('The article recorder is not running yet; press Insert again in a moment');
            return;
        }
        if (session.state === 'LAUNCHING')
            session.cancelled = true;
        this._log(`Insert during an article: stopping it (TERM to ${pid})`);
        this._kill(pid, 'TERM');
    }

    _queue(queued) {
        this._queued = queued;
        if (queued.waitFor !== 'owned')
            this._pollStranger();
        this._render();
    }

    _pollStranger() {
        this._queued.pollTimer = this._addTimeout(UNOWNED_POLL_MS, () => {
            const queued = this._queued;
            if (!queued || queued.waitFor === 'owned')
                return false;
            queued.pollTimer = null;
            if (this._isAlive(queued.waitFor)) {
                this._pollStranger();
                return false;
            }
            this._log(`Recorder ${queued.waitFor} has exited: running the queued start`);
            this._queued = null;
            this._startWhenReady(queued.launch);
            return false;
        });
    }

    _clearQueuedPoll() {
        if (this._queued?.pollTimer) {
            this._removeTimeout(this._queued.pollTimer);
            this._queued.pollTimer = null;
        }
    }

    _cancelQueued(why) {
        this._log(`Queued start cancelled by ${why}`);
        this._clearQueuedPoll();
        this._queued = null;
        this._render();
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

    _render() {
        if (this._destroyed)
            return;
        let display;
        let launch = null;
        const session = this._session;
        if (this._queued) {
            display = 'QUEUED';
        } else if (session && !session.ended) {
            display = session.state;
            launch = session.launch;
        } else if (session) {
            // Ended, but the recorder is still exiting: never the ready icon
            display = this._ending ?? 'FINISHING';
            launch = session.launch;
        } else {
            display = this._ending ?? 'IDLE';
        }
        if (display === this._shown && launch?.kind === this._shownKind)
            return;
        this._shown = display;
        this._shownKind = launch?.kind;
        this._show(display, launch);
    }
}
