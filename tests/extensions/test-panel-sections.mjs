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
import {readFileSync} from 'node:fs';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

// Resolved relative to this file, like the loader above and `gjs-loader.mjs`'s own
// stub import. It was an absolute `/workspace/...` — the CCY container's mount point —
// so the suite could only ever run in a container, and `qa-all.bash` reached a different
// verdict per machine. The repo already says so in as many words: "runs both inside the
// CCY container and on the host, so never hardcode /workspace".
const EXTENSION = new URL(
    '../../extensions/fedora-desktop@fedora-desktop/', import.meta.url).href;

// GJS puts `log` in the global scope; Node does not. Stubbed rather than stripped from
// the extension, because a diagnostic that only exists when nobody is testing is a
// diagnostic nobody has ever seen work.
globalThis.log = () => {};

const StatusDocument = await import(`${EXTENSION}statusDocument.js`);
const {section: health} = await import(`${EXTENSION}sections/health.js`);
const {
    RecordingMenu, GLIB_FILES, CLIPBOARD, NOTIFICATIONS, SPAWNS, SPAWN_FAILURE, EXECUTABLES,
} = await import('./gi-stubs.mjs');

const OSRELEASE = '/proc/sys/kernel/osrelease';

const COLLECTED = '6.17.0-63.fc44.x86_64';
const REBOOTED_INTO = '6.18.2-100.fc44.x86_64';
const NOW = Date.parse('2026-09-15T13:00:00Z');
const BOOT_FINDING = 'evdi: no DKMS module installed for the running kernel 6.17.0-63.fc44.x86_64';

const HANDOFF = '/stub/state/fedora-desktop/play-ledger/host-health-findings.md';

/** A document in the producer's shape. `overrides` replaces whole sections. */
function document(sections, kernel = COLLECTED, handoff = '') {
    return {
        schema: 1,
        generated_at: '2026-09-15T12:00:00Z',
        kernel,
        handoff,
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

/**
 * The handoff offer — Task 3.3's one-click half (DESIGN-panel.md §9).
 *
 * Two rules are pinned here, and both are about what the panel must NOT do.
 *
 * The first is that the offer is **section-level, not per-finding**. There is one
 * handoff file and it describes every finding, so a clickable row per finding would
 * offer the same command N times while implying each row had its own.
 *
 * The second is that a document naming no handoff gets **no row at all** — while still
 * rendering its findings. A host with faults must never go quiet; it just has no button.
 * An offer row that appeared regardless and copied an empty command would be a button
 * that fails in the user's hands, which is worse than no button on a surface whose whole
 * job is being honest about what is known.
 */
function offerRow(menu) {
    return menu.items.find(
        item => item?.label?.text === 'Discuss these findings with Claude Code');
}

test('a document naming a handoff offers it', () => {
    const row = offerRow(render(document({}, COLLECTED, HANDOFF)));
    assert.ok(row !== undefined, 'no handoff row was rendered');
    assert.equal(row.reactive, true);
});

test('the offered command names the path the producer wrote', () => {
    const row = offerRow(render(document({}, COLLECTED, HANDOFF)));
    const detail = row.children.map(child => child.text).join(' ');
    assert.equal(detail, `${StatusDocument.HANDOFF_COMMAND} '${HANDOFF}'`);
});

test('activating it COPIES the command and says so', () => {
    CLIPBOARD.type = null;
    CLIPBOARD.text = null;
    NOTIFICATIONS.length = 0;
    offerRow(render(document({}, COLLECTED, HANDOFF))).emit('activate');
    assert.equal(CLIPBOARD.type, 'clipboard');
    assert.equal(CLIPBOARD.text, `${StatusDocument.HANDOFF_COMMAND} '${HANDOFF}'`);
    assert.equal(NOTIFICATIONS.length, 1);
});

test('activating it LAUNCHES nothing — the panel offers, a human decides', () => {
    // `claude` reads the repository it starts in, and the panel does not know where the
    // checkout is. A launch from here would start it in the compositor's working
    // directory, where it cannot see the playbooks the diagnosis is about.
    //
    // Watched through the stub, now that the stubs provide the spawn API (the report
    // row below uses it): a spawn through any OTHER API would still throw at activate
    // time, so the source check keeps covering the routes the stub does not.
    SPAWNS.length = 0;
    offerRow(render(document({}, COLLECTED, HANDOFF))).emit('activate');
    assert.equal(SPAWNS.length, 0, 'the handoff row spawned a process');
    const source = readFileSync(new URL(`${EXTENSION}sections/health.js`), 'utf8');
    assert.ok(!/spawn_command_line|spawn_async/.test(source),
        'the health section spawns through an API the tests cannot see');
});

test('a document naming no handoff gets no row', () => {
    assert.equal(offerRow(render(document({}))), undefined);
});

test('a findings document with no handoff still renders its findings', () => {
    const doc = document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: [],
    }});
    const menu = render(doc);
    assert.equal(offerRow(menu), undefined);
    assert.equal(groupIn(menu, 'This machine now', 'evdi'), 'findings');
});

test('a relative handoff path is refused rather than offered', () => {
    // The command is interpolated into something a human runs. A relative path would
    // resolve against whatever directory their terminal starts in, which is not where
    // the file is.
    assert.equal(StatusDocument.handoffPath({handoff: 'play-ledger/findings.md'}), '');
    assert.equal(offerRow(render(document({}, COLLECTED, 'play-ledger/findings.md'))),
        undefined);
});

test('a handoff that is not a string is refused rather than interpolated', () => {
    assert.equal(StatusDocument.handoffPath({handoff: 7}), '');
    assert.equal(StatusDocument.handoffPath({}), '');
    assert.equal(StatusDocument.handoffPath(null), '');
});

test('an unreadable document offers nothing', () => {
    // It has just finished saying nothing is known about this host. A button under that
    // sentence would contradict it.
    GLIB_FILES.delete(OSRELEASE);
    const menu = new RecordingMenu();
    health.build(menu, {
        schema: 1, generated_at: '', kernel: '', handoff: '',
        sections: {status: {state: 'unavailable', findings: [], unchecked: ['nothing yet']}},
    }, NOW, COLLECTED);
    assert.equal(offerRow(menu), undefined);
});

test('the offer comes LAST, after everything it refers to', () => {
    const doc = document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: [],
    }}, COLLECTED, HANDOFF);
    const menu = render(doc);
    assert.equal(menu.items.at(-1), offerRow(menu));
});

/**
 * The full report in a terminal (Plan 00136). The panel is an indicator; the text a
 * terminal prints is the reading surface, and this row is how a click gets there. It
 * launches ONE thing — the on-demand report command, which reads and never re-runs a
 * play — so DESIGN-panel.md §8 still holds: nothing here applies anything.
 */
const REPORT_ROW = 'Open the full report in a terminal';
const COMMAND_PATH = '/stub/home/.local/bin/fedora-desktop-health';

function reportRow(menu) {
    return menu.items.find(item => item?.label?.text === REPORT_ROW);
}

function resetLaunches() {
    SPAWNS.length = 0;
    NOTIFICATIONS.length = 0;
    SPAWN_FAILURE.message = null;
    EXECUTABLES.clear();
}

test('the report row is offered whatever the document says', () => {
    // A clean host, a host with findings, and a host nothing has checked all have a
    // report to read — the command says which. Unlike the handoff, there is no state
    // in which "open the report" would contradict the menu above it.
    const clean = render(document({}));
    const findings = render(document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: [],
    }}));
    GLIB_FILES.delete(OSRELEASE);
    const unreadable = new RecordingMenu();
    health.build(unreadable, {
        schema: 1, generated_at: '', kernel: '', handoff: '',
        sections: {status: {state: 'unavailable', findings: [], unchecked: ['nothing yet']}},
    }, NOW, COLLECTED);
    for (const menu of [clean, findings, unreadable]) {
        const row = reportRow(menu);
        assert.ok(row !== undefined, 'no report row was rendered');
        assert.equal(row.reactive, true);
    }
});

test('the row comes FIRST, before the findings it expands on', () => {
    // A person who wants the detail should not have to read past a summary to find the
    // way to it. Below the collection line, above the separator that opens the checks.
    const menu = render(document({}));
    const texts = menu.texts;
    assert.equal(texts[1], REPORT_ROW);
    assert.match(texts[0], /^collect/);
});

test('activating it opens the command in the default terminal, by argv, held open', () => {
    resetLaunches();
    EXECUTABLES.add(COMMAND_PATH);
    reportRow(render(document({}))).emit('activate');
    assert.equal(SPAWNS.length, 1);
    assert.deepEqual(SPAWNS[0].argv, ['xdg-terminal-exec', COMMAND_PATH, '--hold']);
});

test('the path is built from the home directory and the shared command name', () => {
    assert.equal(StatusDocument.onDemandCommandPath(), COMMAND_PATH);
    assert.ok(COMMAND_PATH.endsWith(`/${StatusDocument.ON_DEMAND_COMMAND}`));
});

test('a command that is not installed is said so, and nothing is spawned', () => {
    // The report play installs the command; the panel play does not. A host with the
    // panel and not the report has nothing to open, and a terminal that flashes up and
    // closes on "command not found" tells the user nothing.
    resetLaunches();
    reportRow(render(document({}))).emit('activate');
    assert.equal(SPAWNS.length, 0);
    assert.equal(NOTIFICATIONS.length, 1);
    assert.match(NOTIFICATIONS[0].body, /play-host-health-login-report\.yml/);
});

test('a terminal that cannot start is reported, naming the command to run by hand', () => {
    resetLaunches();
    EXECUTABLES.add(COMMAND_PATH);
    SPAWN_FAILURE.message = 'Failed to execute child process "xdg-terminal-exec"';
    reportRow(render(document({}))).emit('activate');
    assert.equal(NOTIFICATIONS.length, 1);
    assert.match(NOTIFICATIONS[0].body, /fedora-desktop-health/);
    assert.match(NOTIFICATIONS[0].body, /xdg-terminal-exec/);
});

test('the report row is the ONLY thing in the section that spawns', () => {
    // Every row is activated where it can be; only one may reach the process table.
    resetLaunches();
    EXECUTABLES.add(COMMAND_PATH);
    const menu = render(document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: [],
    }}, COLLECTED, HANDOFF));
    let activated = 0;
    for (const item of menu.items) {
        if (item?.handlers?.has('activate') && item !== reportRow(menu)) {
            item.emit('activate');
            activated += 1;
        }
    }
    // The copy row and the handoff row are the two other clickable rows. A count of zero
    // would mean the loop matched nothing and the assertion below was vouching blind.
    assert.equal(activated, 2);
    assert.equal(SPAWNS.length, 0);
});

/**
 * Wrapping and copying (Plan 00134, Task 1.3). One finding carrying a diagnostic and the
 * command that clears it rendered as a single line wider than the screen, and a menu
 * label cannot be selected, so the command in it could not be used either.
 */
function copyRow(menu) {
    return menu.items.find(item => item?.label?.text === 'Copy these findings');
}

test('every finding label wraps instead of widening the menu', () => {
    const menu = render(document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: ['nothing looked'],
    }}));
    const labels = menu.items
        .filter(item => item?.label?.text === BOOT_FINDING || item?.label?.text === 'nothing looked')
        .map(item => item.label.clutter_text);
    assert.equal(labels.length, 2);
    for (const text of labels) {
        assert.equal(text.line_wrap, true);
        assert.equal(text.line_wrap_mode, 'word-char');
        assert.equal(text.ellipsize, 'none');
    }
});

test('the copy row copies what the menu shows, fault and not-checked apart', () => {
    CLIPBOARD.text = null;
    NOTIFICATIONS.length = 0;
    copyRow(render(document({'post-boot-health': {
        state: 'findings', findings: [BOOT_FINDING], unchecked: ['nothing looked'],
    }}))).emit('activate');
    assert.equal(CLIPBOARD.text, [
        'This machine now',
        `  - ${BOOT_FINDING}`,
        '  not checked — nothing is known about these:',
        '  - nothing looked',
    ].join('\n'));
    assert.equal(NOTIFICATIONS.length, 1);
});

test('a clean host gets no copy row', () => {
    assert.equal(copyRow(render(document({}))), undefined);
});
