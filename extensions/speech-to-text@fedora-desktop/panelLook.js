/**
 * What the panel icon looks like in each state panelSession.js shows (Plan 00171).
 *
 * Green only ever means SUCCESS: the recording badge is red like the recording mic. The
 * hourglass in purple says a recording you asked for is coming (starting, or waiting for
 * the server); in orange it says the last dictation is still finishing. BLOCKED, a red
 * no-entry sign, flashes when a press does nothing (the stop grace, transcribing, the
 * paste). No GNOME imports, so tests/extensions/test-stt-panel-look.mjs runs it under
 * Node.
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
    FINISHING: {icon: 'hourglass', colour: COLOURS.amber},
    BLOCKED: {icon: 'action-unavailable-symbolic', colour: COLOURS.red},
    CANCELLING: {icon: 'microphone-disabled-symbolic', colour: COLOURS.grey},
    UNOWNED: {icon: 'media-record-symbolic', colour: COLOURS.red},
    PREPARING: {icon: MIC, colour: COLOURS.amber},
    RECORDING: {icon: MIC, colour: COLOURS.red},
    STOPPING: {icon: LOADING, colour: COLOURS.amber},
    TRANSCRIBING: {icon: LOADING, colour: COLOURS.amber},
    SUCCESS: {icon: MIC, colour: COLOURS.green},
    ERROR: {icon: MIC, colour: COLOURS.red},
};

/** Whether `state` has a look of its own. */
export function isKnownState(state) {
    return Object.hasOwn(LOOKS, state);
}

/** The look of `state`. An unknown state is never shown as ready (the icon must not
 * claim ready when it does not know): it gets the please-wait hourglass. */
export function lookFor(state) {
    return isKnownState(state) ? LOOKS[state] : LOOKS.FINISHING;
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
