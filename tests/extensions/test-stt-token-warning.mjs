/**
 * The speech-to-text panel's warning that no Claude token has allowance left (Plan 00208
 * Phase 4).
 *
 *     node --test tests/extensions/test-stt-token-warning.mjs
 *
 * With "Use the token with the most allowance left" on, wsi-claude-token writes
 * ~/.local/share/speech-to-text/claude-tokens-exhausted when every token is out, naming
 * each token's state, and removes it when a token has room again. The panel shows a
 * warning icon and a menu item for as long as the file is there; the item says what the
 * file says and clears the warning when clicked. The warning is a visible state, never
 * only a log line.
 */

import assert from 'node:assert/strict';
import test from 'node:test';

const {TOKEN_WARNING_FILE, tokenWarning} =
    await import(new URL('../../extensions/speech-to-text@fedora-desktop/tokenWarning.js',
        import.meta.url).href);

const FILE = 'No Claude token has allowance left\n' +
    'week: 5-hour 12%, 7-day 100% (resets Sun 18 Oct 09:00)\n' +
    'gone: unavailable (could not reach the API)\n';

test('the file is the one wsi-claude-token writes', () => {
    assert.equal(TOKEN_WARNING_FILE, 'claude-tokens-exhausted');
});

test('no file is no warning', () => {
    assert.equal(tokenWarning(null), null);
});

test('the warning names each token and how to clear it', () => {
    assert.deepEqual(tokenWarning(FILE), {
        label: 'No Claude token has allowance left (click to clear)\n' +
            '  week: 5-hour 12%, 7-day 100% (resets Sun 18 Oct 09:00)\n' +
            '  gone: unavailable (could not reach the API)',
        log: 'No Claude token has allowance left: week: 5-hour 12%, 7-day 100% ' +
            '(resets Sun 18 Oct 09:00); gone: unavailable (could not reach the API)',
    });
});

test('a file that says nothing is still a warning, never a silent one', () => {
    const warning = tokenWarning('\n');
    assert.equal(warning.label, 'No Claude token has allowance left (click to clear)');
});
