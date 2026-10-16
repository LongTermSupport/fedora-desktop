/**
 * The panel's warning that no Claude token has allowance left (Plan 00208 Phase 4).
 *
 * With "Use the token with the most allowance left" on, wsi-claude-token writes this file
 * in ~/.local/share/speech-to-text when every token is out (a heading, then a token's state
 * a line) and removes it when a token has room again. While it is there the panel shows a
 * warning icon and a menu item saying what the file says; clicking the item removes the
 * file. No GNOME imports, so tests/extensions/test-stt-token-warning.mjs runs it under Node.
 */

export const TOKEN_WARNING_FILE = 'claude-tokens-exhausted';

const HEADING = 'No Claude token has allowance left';

/** The warning for the file's text, or null when there is no file (`text` null):
 * label is the menu item's text, log the one line the debug log gets. */
export function tokenWarning(text) {
    if (text === null)
        return null;
    const lines = text.split('\n').map(line => line.trim()).filter(Boolean);
    const states = lines[0] === HEADING ? lines.slice(1) : lines;
    return {
        label: [`${HEADING} (click to clear)`, ...states.map(state => `  ${state}`)].join('\n'),
        log: states.length > 0 ? `${HEADING}: ${states.join('; ')}` : HEADING,
    };
}
