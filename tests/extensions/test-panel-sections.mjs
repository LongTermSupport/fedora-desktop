/**
 * The panel's decisions, exercised against the shipped extension (Plan 00109 Task 4.2).
 *
 *     node --test tests/extensions/
 *
 * These import `statusDocument.js` and `sections/health.js` themselves — the files the
 * play deploys — with `gjs-loader.mjs` answering the GNOME imports. Nothing here is a
 * re-implementation, so a passing test is a statement about what a shell would render.
 *
 * WHAT THIS IS FOR. `helpers/gnome/check_panel_contract.py` proves the two languages use
 * the same WORDS: the same section ids, the same state constants, the same file name. It
 * cannot prove either side asks a question with them, and a gate that looks like it does
 * is worse than none. The three populations below are the ones the login report already
 * handles and the panel did not — a document from a previous boot, a malformed one, and
 * one whose `state` disagrees with its own lists.
 *
 * Each group opens with the case that must ALREADY pass, so every assertion after it is
 * a change from a known-good baseline rather than a claim about a renderer that might be
 * producing nothing at all.
 */

import assert from 'node:assert/strict';
import {register} from 'node:module';
import test from 'node:test';
import {pathToFileURL} from 'node:url';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const EXTENSION = pathToFileURL(
    '/workspace/extensions/fedora-desktop@fedora-desktop/').href;

// GJS puts `log` in the global scope; Node does not. Stubbed rather than stripped from
// the extension, because a diagnostic that only exists when nobody is testing is a
// diagnostic nobody has ever seen work.
globalThis.log = () => {};

const StatusDocument = await import(`${EXTENSION}statusDocument.js`);
const {section: health} = await import(`${EXTENSION}sections/health.js`);
const {RecordingMenu, GLIB_FILES} = await import('./gi-stubs.mjs');

const OSRELEASE = '/proc/sys/kernel/osrelease';

const COLLECTED = '6.17.0-63.fc44.x86_64';
const REBOOTED_INTO = '6.18.2-100.fc44.x86_64';
const NOW = Date.parse('2026-09-15T13:00:00Z');
const BOOT_FINDING = 'evdi: no DKMS module installed for the running kernel 6.17.0-63.fc44.x86_64';

/** A document in the producer's shape. `overrides` replaces whole sections. */
function document(sections, kernel = COLLECTED) {
    return {
        schema: 1,
        generated_at: '2026-09-15T12:00:00Z',
        kernel,
        sections: {
            'post-boot-health': {state: 'ok', findings: [], unchecked: []},
            'play-ledger': {state: 'ok', findings: [], unchecked: []},
            'play-freshness': {state: 'ok', findings: [], unchecked: []},
            'installed-vs-pinned': {state: 'ok', findings: [], unchecked: []},
            ...sections,
        },
    };
}

function render(doc, runningKernel = COLLECTED) {
    const menu = new RecordingMenu();
    health.build(menu, doc, NOW, runningKernel);
    return menu;
}

/** The menu's check headers, which are what divide it into blocks. */
const TITLES = new Set([
    'This machine now',
    'Record of what has run here',
    'Plays since they last ran',
    'Installed versus pinned',
]);

/**
 * Which group a line sits in WITHIN one check: `findings` above that check's not-checked
 * heading, `unchecked` below it, `absent` if the check does not render it at all.
 *
 * Scoped to the named check, not scanned flat across the menu. A flat scan cannot tell
 * which check a line belongs to, so one demoted section earlier in the menu made every
 * later section's findings read as unchecked — and several assertions here passed only
 * because the ordering happened to suit them. That is the defect this whole task is
 * about, in the test helper written to catch it.
 */
function groupIn(menu, title, needle) {
    let inBlock = false;
    let below = false;
    for (const text of menu.texts) {
        if (TITLES.has(text)) {
            inBlock = text === title;
            below = false;
            continue;
        }
        if (!inBlock) {
            continue;
        }
        if (text.startsWith('not checked')) {
            below = true;
            continue;
        }
        if (text.includes(needle)) {
            return below ? 'unchecked' : 'findings';
        }
    }
    return 'absent';
}

/** The same question for lines that belong to no check — the self report and the
 * document-level reasons, which are rendered before any header exists. */
function documentGroupOf(menu, needle) {
    let below = false;
    for (const text of menu.texts) {
        if (TITLES.has(text)) {
            break;
        }
        if (text.startsWith('not checked')) {
            below = true;
            continue;
        }
        if (text.includes(needle)) {
            return below ? 'unchecked' : 'findings';
        }
    }
    return 'absent';
}

const ALL_IDS = health.documentSections;

test('a clean document reports nothing and reads as ok', () => {
    const menu = render(document({}));
    assert.equal(StatusDocument.overallState(document({}), ALL_IDS, COLLECTED),
        StatusDocument.OK);
    assert.ok(menu.texts.includes('nothing to report'));
});

test('a finding collected under the running kernel is a current fault', () => {
    const doc = document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    });
    assert.equal(groupIn(render(doc), 'This machine now', BOOT_FINDING), 'findings');
    assert.equal(StatusDocument.overallState(doc, ALL_IDS, COLLECTED),
        StatusDocument.FINDINGS);
});

test('a state that disagrees with its own lists does not silence the lists', () => {
    // `status_document.section()` DERIVES `state` from the lists, so the lists are the
    // fact and `state` is a restatement of it. A reader that branches on the restatement
    // is a second mechanism for one fact, living in one of two consumers — which is how
    // the boot predicate went wrong in the first place.
    const doc = document({
        'play-freshness': {state: 'ok', findings: ['play-nvidia.yml: 40 days'], unchecked: []},
    });
    assert.equal(groupIn(render(doc), 'Plays since they last ran', 'play-nvidia.yml: 40 days'), 'findings');
    assert.equal(StatusDocument.overallState(doc, ALL_IDS, COLLECTED),
        StatusDocument.FINDINGS);
});

test('a state claiming ok over an unchecked list still reads as unavailable', () => {
    const doc = document({
        'play-ledger': {state: 'ok', findings: [], unchecked: ['the ledger could not be read']},
    });
    assert.equal(groupIn(render(doc), 'Record of what has run here', 'the ledger could not be read'), 'unchecked');
    assert.equal(StatusDocument.overallState(doc, ALL_IDS, COLLECTED),
        StatusDocument.UNAVAILABLE);
});

test('a group that is not a list is reported, not read as nothing to say', () => {
    // The sharpest shape: `state` says findings while `findings` is not a list, so the
    // document says something is wrong and a defensive reader prints nothing. On this
    // surface nothing means healthy.
    const doc = document({
        'post-boot-health': {state: 'findings', findings: 'evdi is broken', unchecked: []},
    });
    const menu = render(doc);
    assert.equal(groupIn(menu, 'This machine now', "post-boot-health section's findings could not be read"),
        'unchecked');
    assert.notEqual(StatusDocument.overallState(doc, ALL_IDS, COLLECTED),
        StatusDocument.OK);
});

test('entries this reader cannot show are reported rather than dropped', () => {
    const doc = document({
        'play-ledger': {state: 'findings', findings: ['a real one', {not: 'a string'}], unchecked: []},
    });
    const menu = render(doc);
    assert.equal(groupIn(menu, 'Record of what has run here', 'a real one'), 'findings');
    assert.equal(groupIn(menu, 'Record of what has run here', "play-ledger section's findings holds entries"), 'unchecked');
});

test('a document naming no checks at all says so', () => {
    // `collect` guarantees a key per producer — four even when every one of them raises
    // — so a document with no sections has no legitimate origin. Rendering it as four
    // absent sections describes one absence four times and never says what happened.
    const menu = render({schema: 1, generated_at: '2026-09-15T12:00:00Z', kernel: COLLECTED, sections: {}});
    assert.equal(documentGroupOf(menu, 'names no checks at all'), 'unchecked');
});

test('a document whose sections cannot be read says so', () => {
    const menu = render({schema: 1, generated_at: '2026-09-15T12:00:00Z', kernel: COLLECTED, sections: 'gone'});
    assert.equal(documentGroupOf(menu, 'sections could not be read'), 'unchecked');
});

test('after a reboot the boot-scoped findings are demoted, not repeated as current', () => {
    // They read as present tense — "no DKMS module installed for the running kernel
    // 6.17.0" — while naming a kernel that is not running. Left among the current faults
    // they put two different values for "the running kernel" on consecutive lines of one
    // report, one of them wrong, in exactly the scenario this rule exists for.
    const doc = document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    });
    const menu = render(doc, REBOOTED_INTO);
    assert.equal(groupIn(menu, 'This machine now', BOOT_FINDING), 'unchecked');
});

test('the demotion explains itself, naming both kernels', () => {
    const doc = document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    });
    const menu = render(doc, REBOOTED_INTO);
    const explanation = menu.texts.find(text => text.includes('collected under kernel'));
    assert.ok(explanation, `no explanation in: ${JSON.stringify(menu.texts)}`);
    assert.ok(explanation.includes(COLLECTED));
    assert.ok(explanation.includes(REBOOTED_INTO));
});

test('a reboot does not demote the three sections it survives', () => {
    // The ledger, play freshness and installed-versus-pinned are true across a reboot.
    // Demoting them too would be an overclaim about three quarters of the document.
    const doc = document({
        'play-freshness': {state: 'findings', findings: ['play-nvidia.yml: 40 days'], unchecked: []},
    });
    assert.equal(groupIn(render(doc, REBOOTED_INTO), 'Plays since they last ran', 'play-nvidia.yml: 40 days'), 'findings');
});

test('a boot-stale document does not read as ok in the icon', () => {
    const doc = document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    });
    assert.equal(StatusDocument.overallState(doc, ALL_IDS, REBOOTED_INTO),
        StatusDocument.UNAVAILABLE);
});

test('a mismatch is not manufactured out of ignorance', () => {
    // Both sides must be known. A document carrying `kernel: ""` has already explained
    // itself, and an unreadable /proc gives an empty running kernel — neither is
    // evidence of a mismatch, and reporting one from either would be the inverse of this
    // plan's rule and just as wrong.
    const doc = document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    }, '');
    assert.equal(groupIn(render(doc, REBOOTED_INTO), 'This machine now', BOOT_FINDING), 'findings');
    assert.equal(groupIn(render(document({
        'post-boot-health': {state: 'findings', findings: [BOOT_FINDING], unchecked: []},
    }), ''), 'This machine now', BOOT_FINDING), 'findings');
});

test('isBootStale is the document module answer, shared with the icon and the menu', () => {
    // The predicate belongs to the document, not to whichever consumer noticed it first:
    // two readers with two copies is a question one of them silently never asks.
    assert.equal(StatusDocument.isBootStale(document({}), REBOOTED_INTO), true);
    assert.equal(StatusDocument.isBootStale(document({}), COLLECTED), false);
    assert.equal(StatusDocument.isBootStale(document({}, ''), REBOOTED_INTO), false);
    assert.equal(StatusDocument.isBootStale(document({}), ''), false);
    assert.equal(StatusDocument.BOOT_SCOPED_SECTION, 'post-boot-health');
});

test('an unreadable document still renders the reason it could not be read', () => {
    // `read` answers absent, unparseable and unknown-schema with a document whose only
    // section is the self section. Rendering the four checks against that produces four
    // derived "has no <id> section" lines and drops the reason entirely.
    const menu = render({
        schema: 1,
        generated_at: '',
        kernel: '',
        sections: {status: {state: 'unavailable', findings: [], unchecked: ['no host status has been recorded yet']}},
    });
    // Asserted on presence rather than on group: the self report is the WHOLE content,
    // so it carries no not-checked heading to sit under. What matters is that the reason
    // is shown and the four checks are not — four derived "has no <id> section" lines
    // would describe one absence four times and never say what happened.
    assert.ok(menu.texts.some(text => text.includes('no host status has been recorded yet')),
        `reason absent from: ${JSON.stringify(menu.texts)}`);
    assert.ok(!menu.texts.includes('This machine now'));
});

test('the running kernel is read from procfs, trailing newline and all', () => {
    GLIB_FILES.set(OSRELEASE, `${REBOOTED_INTO}\n`);
    try {
        assert.equal(StatusDocument.runningKernel(), REBOOTED_INTO);
    } finally {
        GLIB_FILES.delete(OSRELEASE);
    }
});

test('an unreadable procfs gives "could not tell", which suppresses the claim', () => {
    // NOT "no mismatch". An empty answer has to make `isBootStale` false, because the
    // alternative is a panel that reports a boot mismatch against a kernel it could not
    // read — a finding manufactured out of ignorance.
    GLIB_FILES.delete(OSRELEASE);
    const running = StatusDocument.runningKernel();
    assert.equal(running, '');
    assert.equal(StatusDocument.isBootStale(document({}), running), false);
});
