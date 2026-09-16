/**
 * The panel indicator's decisions, exercised against the shipped `extension.js`.
 *
 *     node --test tests/extensions/test-panel-indicator.mjs
 *
 * WHY THIS EXISTS, separately from `test-panel-sections.mjs`. `extension.js`'s own header
 * calls the three-state icon "the whole reason this extension is shaped like this": a
 * neutral icon over an empty menu is what a healthy host looks like, and also what a
 * missing document, an unparseable one and a crashed producer look like. Nothing
 * exercised it. `gjs-loader.mjs` had mapped the shell's `extension.js` import since the
 * first commit, so the harness read as covering this file, while `gi-stubs.mjs` exported
 * no `Extension` — any test importing it failed on the import, which is why none did.
 *
 * What is driven here is `enable()` and `disable()`, not `_render` reached through the
 * back door: a test that sets private fields and calls a private method proves the
 * mapping and not the wiring, and the wiring is where an icon goes neutral.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const EXTENSION = new URL(
    '../../extensions/fedora-desktop@fedora-desktop/', import.meta.url).href;

globalThis.log = () => {};

const StatusDocument = await import(`${EXTENSION}statusDocument.js`);
const {default: FedoraDesktopExtension} = await import(`${EXTENSION}extension.js`);
const {GLIB_FILES, STATUS_AREA, TIMERS, DEFERRED_READS} = await import('./gi-stubs.mjs');

const OSRELEASE = '/proc/sys/kernel/osrelease';
const KERNEL = '6.17.0-63.fc44.x86_64';
// Asked of the module rather than spelled again: the panel reads this exact path, and a
// test that built its own would go on passing while the two drifted apart.
const DOCUMENT_PATH = StatusDocument.documentPath();

const NEUTRAL = 'emblem-ok-symbolic';
const ATTENTION = 'dialog-warning-symbolic';
const NOTHING_KNOWN = 'dialog-question-symbolic';

/** The four section ids the health section registers, all in one state. A document
 * missing any of them resolves that one `unavailable`, which would make every case
 * below come out `unavailable` whatever the icon logic did. */
function document(state, findings = [], unchecked = []) {
    const ids = [
        'post-boot-health', 'play-ledger', 'play-freshness', 'installed-vs-pinned'];
    const sections = {};
    for (const id of ids) {
        sections[id] = {state, findings, unchecked};
    }
    return JSON.stringify({
        schema: StatusDocument.SCHEMA_VERSION,
        generated_at: new Date().toISOString(),
        kernel: KERNEL,
        handoff: '',
        state,
        findings,
        unchecked,
        sections,
    });
}

/** A host with the given document on disk, enabled. Returns the live icon. */
function enabled(contents) {
    GLIB_FILES.clear();
    STATUS_AREA.clear();
    TIMERS.clear();
    DEFERRED_READS.enabled = false;
    DEFERRED_READS.pending = [];
    GLIB_FILES.set(OSRELEASE, `${KERNEL}\n`);
    if (contents !== null) {
        GLIB_FILES.set(DOCUMENT_PATH, contents);
    }
    const extension = new FedoraDesktopExtension({uuid: 'fedora-desktop@fedora-desktop'});
    extension.enable();
    const indicator = STATUS_AREA.get('fedora-desktop');
    assert.ok(indicator, 'the indicator was never added to the status area');
    return {extension, indicator, icon: indicator.children[0]};
}

test('a clean host gets the neutral icon and no colour', () => {
    const {icon} = enabled(document(StatusDocument.OK));
    assert.equal(icon.icon_name, NEUTRAL);
    assert.equal(icon.style, '');
});

test('findings get the attention icon and a colour', () => {
    const {icon} = enabled(document(StatusDocument.FINDINGS, ['evdi: no module']));
    assert.equal(icon.icon_name, ATTENTION);
    assert.notEqual(icon.style, '');
});

test('UNAVAILABLE gets its OWN icon and colour, never the neutral pair', () => {
    // The plan's premise in one assertion. Sharing either with `ok` would report a host
    // nobody could check as a host with nothing wrong, which is the incident.
    const clean = enabled(document(StatusDocument.OK));
    const findings = enabled(document(StatusDocument.FINDINGS, ['evdi: no module']));
    const {icon} = enabled(document(StatusDocument.UNAVAILABLE, [], ['nothing known']));

    assert.equal(icon.icon_name, NOTHING_KNOWN);
    assert.notEqual(icon.icon_name, clean.icon.icon_name);
    assert.notEqual(icon.icon_name, findings.icon.icon_name);
    assert.notEqual(icon.style, clean.icon.style);
    assert.notEqual(icon.style, findings.icon.style);
});

test('before the first read lands, nothing is known — not a clean bill of health', () => {
    // enable() renders once with no document, then starts a read. Only the ORDER can say
    // whether that first render claimed health, and the stub's read answers immediately,
    // so the final value cannot.
    const {icon} = enabled(document(StatusDocument.OK));
    assert.equal(icon.iconNames[0], NOTHING_KNOWN);
    assert.equal(icon.iconNames.at(-1), NEUTRAL,
        'the read did not land, so this test is not observing the order it claims');
});

test('a host with no document at all says so, and does not render as clean', () => {
    const {icon, indicator} = enabled(null);
    assert.equal(icon.icon_name, NOTHING_KNOWN);
    assert.ok(indicator.menu.texts.some(text => text.includes('nothing is known')),
        `the menu never said why: ${JSON.stringify(indicator.menu.texts)}`);
});

/** Enabled with the read HELD, so the panel is observable in the state it occupies
 * between `enable()` returning and the document arriving. `deliver()` releases it. */
function enabledWithHeldRead(contents) {
    const live = (() => {
        DEFERRED_READS.enabled = true;
        DEFERRED_READS.pending = [];
        GLIB_FILES.clear();
        STATUS_AREA.clear();
        TIMERS.clear();
        GLIB_FILES.set(OSRELEASE, `${KERNEL}\n`);
        GLIB_FILES.set(DOCUMENT_PATH, contents);
        const extension = new FedoraDesktopExtension({uuid: 'fedora-desktop@fedora-desktop'});
        extension.enable();
        const indicator = STATUS_AREA.get('fedora-desktop');
        assert.ok(indicator, 'the indicator was never added to the status area');
        return {extension, indicator, icon: indicator.children[0]};
    })();
    assert.equal(DEFERRED_READS.pending.length, 1,
        'enable() did not start exactly one read, so nothing is being held');
    return {
        ...live,
        deliver() {
            assert.equal(DEFERRED_READS.flush(), 1);
        },
    };
}

test('with no document yet, the menu SAYS it is reading rather than showing nothing', () => {
    // The `document === null` branch, which is unreachable once a read has answered — so
    // with the stub answering immediately this whole state was untestable, and mutating
    // the branch to `if (false)` changed no test's outcome.
    const live = enabledWithHeldRead(document(StatusDocument.OK));
    assert.deepEqual(live.indicator.menu.texts, ['reading host status…']);
    assert.equal(live.icon.icon_name, NOTHING_KNOWN);

    live.deliver();
    assert.notDeepEqual(live.indicator.menu.texts, ['reading host status…'],
        'the document arrived and the menu still says it is reading');
    assert.equal(live.icon.icon_name, NEUTRAL);
});

test('each render REPLACES the menu rather than appending to it', () => {
    // Without menu.removeAll() every poll stacks another copy of the whole menu under the
    // last one. Nothing raises; the panel just grows a duplicate set of lines per
    // interval, which is why only a second render can catch it.
    const live = enabledWithHeldRead(document(StatusDocument.FINDINGS, ['evdi: no module']));
    live.deliver();
    const afterFirst = live.indicator.menu.texts;
    assert.ok(afterFirst.length > 0, 'the first render produced no lines to compare');

    DEFERRED_READS.enabled = false;
    live.extension._refresh();
    assert.deepEqual(live.indicator.menu.texts, afterFirst,
        'the second render appended to the menu instead of replacing it');
});

test('disable() destroys the indicator and removes the poll', () => {
    // A surviving timer fires into a destroyed indicator on every interval, and a
    // surviving indicator is a second icon after the next enable — neither raises.
    const {extension, indicator} = enabled(document(StatusDocument.OK));
    assert.equal(TIMERS.size, 1);
    extension.disable();
    assert.equal(indicator.destroyed, true);
    assert.equal(TIMERS.size, 0);
});
