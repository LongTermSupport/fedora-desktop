/**
 * What the panel icon looks like in each state panelSession.js shows (Plan 00171).
 *
 * Green only ever means SUCCESS: the recording badge is red like the recording mic. The
 * hourglass in purple says a recording you asked for is coming (starting, waiting for the
 * server, or queued behind the last dictation); in orange it says the last dictation is
 * still finishing. No GNOME imports, so tests/extensions/test-stt-panel-look.mjs runs it
 * under Node.
 */

export const COLOURS = {
    red: '#ff4444',
    amber: '#ffaa00',
    green: '#44ff44',
    purple: '#c678dd',
    grey: '#999999',
};

const MIC = 'audio-input-microphone-symbolic';
const LOADING = 'content-loading-symbolic';
const COMING = {icon: 'hourglass', colour: COLOURS.purple};

/** icon: a theme icon name, or 'hourglass' (the extension's own); colour: '' is the
 * theme's default. */
const LOOKS = {
    IDLE: {icon: MIC, colour: ''},
    LAUNCHING: COMING,
    WAITING: COMING,
    QUEUED: COMING,
    FINISHING: {icon: 'hourglass', colour: COLOURS.amber},
    CANCELLING: {icon: 'microphone-disabled-symbolic', colour: COLOURS.grey},
    UNOWNED: {icon: 'media-record-symbolic', colour: COLOURS.red},
    PREPARING: {icon: MIC, colour: COLOURS.amber},
    RECORDING: {icon: MIC, colour: COLOURS.red},
    STOPPING: {icon: LOADING, colour: COLOURS.amber},
    TRANSCRIBING: {icon: LOADING, colour: COLOURS.amber},
    SUCCESS: {icon: MIC, colour: COLOURS.green},
    ERROR: {icon: MIC, colour: COLOURS.red},
};

/** The look of `state`; an unknown state looks ready. */
export function lookFor(state) {
    return LOOKS[state] ?? LOOKS.IDLE;
}

/** The elapsed-time badge while recording: amber when transcription is falling behind
 * or the maximum length is 10 s away, else red. */
export function recordingBadgeColor({behind, secondsLeft}) {
    return behind || (secondsLeft !== null && secondsLeft <= 10) ? COLOURS.amber : COLOURS.red;
}

/** The article recorder's badge: it is recording. */
export function articleBadgeColor() {
    return COLOURS.red;
}
