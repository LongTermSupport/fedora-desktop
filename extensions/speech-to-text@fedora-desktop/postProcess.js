/**
 * The menu's Post-Process Last Transcription items (Plan 00208 Phase 3), and the check
 * that the record keys were registered.
 *
 * Ctrl+Insert and Alt+Insert choose Claude post-processing at the start of a recording.
 * These items run it on the last transcription after the fact, one item per style. The
 * work is wsi-postprocess's (src-go/cmd/wsi-postprocess): it reads the file Copy Last
 * Transcription reads, runs wsi-claude-process and saves Claude's text apart, in
 * last-postprocessed.txt, so Copy Last Transcription still gives the transcription. The
 * panel puts Claude's text on the clipboard (Ctrl+V), as the recorders do when Claude
 * fails; it never pastes, since a menu click leaves no window it could be sure of. Every
 * run ends in a notification, shown whatever Show Notifications says: the owner asked
 * for this run.
 *
 * No GNOME imports: the spawn, clipboard and notifications are handed in, so every
 * decision is tested under Node (tests/extensions/test-stt-post-process.mjs).
 */

/** The styles, in menu order, each with the key that records in that style. */
export const POST_PROCESS_STYLES = [
    {style: 'corporate', name: 'Corporate', key: 'Ctrl+Insert',
        label: 'Post-Process Last Transcription: Corporate (as Ctrl+Insert)'},
    {style: 'natural', name: 'Natural', key: 'Alt+Insert',
        label: 'Post-Process Last Transcription: Natural (as Alt+Insert)'},
];

const TOOL = 'wsi-postprocess';
const PREVIEW_CHARS = 120;

/** The command line for one run. */
export function postProcessArgv(bin, style, model, debug) {
    const argv = [`${bin}${TOOL}`, '--style', style, '--model', model];
    if (debug)
        argv.push('--debug');
    return argv;
}

/** Of `results` ({name, action, accels} per record key), the keys a press cannot reach:
 * refused by the shell (action `none`) or with no key set. */
export function unusableKeybindings(results, none) {
    const unusable = [];
    for (const {name, action, accels} of results) {
        if (action === none)
            unusable.push(`${name} (${accels.join(', ')}): the shell refused it`);
        else if (accels.length === 0)
            unusable.push(`${name}: no key is set`);
    }
    return unusable;
}

/** One post-processing run at a time, reported from start to end. */
export class LastTranscriptPostProcessor {
    /** run(argv, done) starts argv and calls done(exitStatus, stdout, stderr, error) once
     * it has ended: exitStatus is a number, or "signal N" for a killed run, or null with
     * error set when its output could not be read. It throws if argv cannot be started. */
    constructor({bin, run, setClipboard, notify, log}) {
        this._bin = bin;
        this._run = run;
        this._setClipboard = setClipboard;
        this._notify = notify;
        this._log = log;
        this._running = null;
        this._destroyed = false;
    }

    get busy() {
        return this._running !== null;
    }

    start(style, model, debug) {
        const name = POST_PROCESS_STYLES.find(s => s.style === style)?.name ?? style;
        if (this._running) {
            this._notify(`Post-processing is already running (${this._running}); ` +
                'wait for its result before starting another');
            return;
        }
        const argv = postProcessArgv(this._bin, style, model, debug);
        this._log(`Post-processing the last transcription: ${argv.join(' ')}`);
        this._running = name;
        try {
            this._run(argv, (status, stdout, stderr, error) =>
                this._finished(name, status, stdout, stderr, error));
        } catch (e) {
            this._running = null;
            this._report(`Post-processing (${name}) could not start: ${e.message}`);
            return;
        }
        this._notify(`Post-processing the last transcription (${name}) with Claude ` +
            `(${model}); the result goes to the clipboard`);
    }

    /** The extension is being disabled: a run that ends later changes nothing. */
    destroy() {
        this._destroyed = true;
    }

    _finished(name, status, stdout, stderr, error) {
        if (this._destroyed)
            return;
        this._running = null;
        const text = (stdout ?? '').trim();
        if (status === 0 && text !== '') {
            this._setClipboard(text);
            const preview = text.length > PREVIEW_CHARS ? `${text.substring(0, PREVIEW_CHARS - 3)}...` : text;
            this._log(`Post-processed (${name}): ${text.length} characters on the clipboard`);
            this._notify(`Post-processed (${name}), on the clipboard: Ctrl+V\n\n${preview}`);
            return;
        }
        this._report(`Post-processing (${name}) failed: ${this._reason(status, text, stderr, error)}`);
    }

    _reason(status, text, stderr, error) {
        if (error)
            return error;
        const said = (stderr ?? '').split('\n').map(line => line.trim())
            .filter(Boolean).map(line => line.replace(`${TOOL}: `, ''));
        if (said.length > 0)
            return said.join(' ');
        if (status === 0)
            return `${TOOL} printed no text`;
        const how = typeof status === 'number' ? `exit status ${status}` : status;
        return `${TOOL} ended with ${how} and said nothing ` +
            '(re-run play-speech-to-text.yml if it is not installed)';
    }

    _report(message) {
        this._log(message);
        this._notify(message);
    }
}
