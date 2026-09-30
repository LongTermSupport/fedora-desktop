/**
 * The containers section and its report reader, exercised against the shipped modules
 * (Plan 00144).
 *
 *     node --test tests/extensions/test-panel-containers.mjs
 *
 * What is proved here is the section's decisions: what each report outcome says, what it
 * contributes to the icon, when it notifies, what a click copies, and that nothing in it
 * starts a process. The widgets are stubs, so this cannot say whether St draws the lines
 * legibly; that is the human pass.
 */

import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {register} from 'node:module';
import test from 'node:test';

register(new URL('./gjs-loader.mjs', import.meta.url).href);

const EXTENSION = new URL(
    '../../extensions/fedora-desktop@fedora-desktop/', import.meta.url);

globalThis.log = () => {};

const StatusDocument = await import(`${EXTENSION.href}statusDocument.js`);
const ContainerReport = await import(`${EXTENSION.href}containerReport.js`);
const {section} = await import(`${EXTENSION.href}sections/containers.js`);
const {
    GLIB_FILES, EXECUTABLES, TIMERS, DBUS, NOTIFICATIONS, CLIPBOARD, SPAWNS, RecordingMenu,
} = await import('./gi-stubs.mjs');

const REPORT_PATH = ContainerReport.reportPath();
const COMMAND_PATH = ContainerReport.commandPath();

const PROCESS_FINDING = {
    kind: 'process', container_id: 'aaa111', container_name: 'web', host_pid: 4242,
    cpu_pct: 95, age_s: 125, cmd: 'python3 /srv/worker.py --loop',
    exec_hint: 'podman exec -it web ps -o pid,args',
};
const CRASH_FINDING = {
    kind: 'crashloop', container_id: 'bbb222', container_name: 'queue',
    restart_count: 17, restarts_per_min: 4, reasons: ['rate', 'burst'],
    exec_hint: 'podman logs queue',
};
const ADVISORY = {
    container_id: 'ccc333', container_name: 'db', policy: 'always',
    advice: 'podman update --restart on-failure:5 db',
};

function report(findings = [], advisories, overrides = {}) {
    const body = {schema: ContainerReport.SCHEMA_VERSION, findings, ...overrides};
    if (advisories !== undefined) {
        body.advisories = advisories;
    }
    return JSON.stringify(body);
}

/** A clean stage, the backend installed unless told otherwise, the source started. */
function started({installed = true, contents = null} = {}) {
    section.source.stop();
    GLIB_FILES.clear();
    EXECUTABLES.clear();
    TIMERS.clear();
    DBUS.reset();
    NOTIFICATIONS.length = 0;
    CLIPBOARD.type = null;
    CLIPBOARD.text = null;
    SPAWNS.length = 0;
    if (installed) {
        EXECUTABLES.add(COMMAND_PATH);
    }
    if (contents !== null) {
        GLIB_FILES.set(REPORT_PATH, contents);
    }
    const changes = {count: 0};
    section.source.start(() => {
        changes.count += 1;
    });
    return changes;
}

function built() {
    const menu = new RecordingMenu();
    section.build(menu);
    return menu;
}

function row(menu, prefix) {
    const found = menu.items.find(item => item?.label?.text.startsWith(prefix));
    assert.ok(found, `no row starting ${JSON.stringify(prefix)} in ${JSON.stringify(menu.texts)}`);
    return found;
}

test('a process finding renders its age, cpu and truncated command', () => {
    started({contents: report([PROCESS_FINDING])});
    const texts = built().texts;
    assert.ok(texts.includes('1 flagged container'), texts.join('\n'));
    assert.ok(texts.includes('web — 2m, 95% CPU'), texts.join('\n'));
    assert.ok(texts.includes('python3 /srv/worker.py --loop'), texts.join('\n'));
});

test('a crash-loop finding renders its rate, count and reasons, not process columns', () => {
    started({contents: report([CRASH_FINDING])});
    const texts = built().texts;
    assert.ok(texts.includes('queue — crash loop: 4/min'), texts.join('\n'));
    assert.ok(texts.includes('17 restarts total · triggered by: rate + burst'),
        texts.join('\n'));
    assert.ok(!texts.some(text => text.includes('0% CPU')), texts.join('\n'));
});

test('a crash loop with no known rate says so rather than showing a blank', () => {
    const finding = {...CRASH_FINDING};
    delete finding.restarts_per_min;
    started({contents: report([finding])});
    assert.ok(built().texts.includes('queue — crash loop: rate unknown this tick'));
});

test('an advisory never moves the icon and never notifies', () => {
    started({contents: report([], [ADVISORY])});
    assert.equal(section.state(), StatusDocument.OK);
    assert.equal(section.leads(), false);
    assert.deepEqual(NOTIFICATIONS, []);
    const texts = built().texts;
    assert.ok(texts.includes('No flagged containers'), texts.join('\n'));
    assert.ok(texts.includes('1 container set to restart without limit'), texts.join('\n'));
    assert.ok(texts.includes('db — always'), texts.join('\n'));
});

test('advisories sit below the findings, behind their own separator', () => {
    started({contents: report([PROCESS_FINDING], [ADVISORY])});
    const menu = built();
    const separator = menu.items.findIndex(item => item !== null && item.label === undefined);
    const finding = menu.items.indexOf(row(menu, 'web — '));
    const advisory = menu.items.indexOf(row(menu, 'db — '));
    assert.ok(finding < separator && separator < advisory, JSON.stringify(menu.texts));
});

test('activating a finding copies its exec_hint and says so', () => {
    started({contents: report([PROCESS_FINDING])});
    NOTIFICATIONS.length = 0;
    row(built(), 'web — ').emit('activate');
    assert.equal(CLIPBOARD.text, PROCESS_FINDING.exec_hint);
    assert.deepEqual(NOTIFICATIONS, [
        {title: 'Container Watch', body: 'Copied inspect command for web'}]);
});

test('a finding with no exec_hint copies nothing and says there was none', () => {
    const finding = {...PROCESS_FINDING};
    delete finding.exec_hint;
    started({contents: report([finding])});
    NOTIFICATIONS.length = 0;
    row(built(), 'web — ').emit('activate');
    assert.equal(CLIPBOARD.text, null);
    assert.equal(NOTIFICATIONS.at(-1).body, 'No inspect hint for web');
});

test('activating an advisory copies its restart-policy advice', () => {
    started({contents: report([], [ADVISORY])});
    NOTIFICATIONS.length = 0;
    row(built(), 'db — ').emit('activate');
    assert.equal(CLIPBOARD.text, ADVISORY.advice);
    assert.equal(NOTIFICATIONS.at(-1).body, 'Copied restart-policy advice for db');
});

test('a newly appeared finding notifies once, and not again on the next tick', () => {
    started({contents: report([PROCESS_FINDING])});
    assert.deepEqual(NOTIFICATIONS, [{title: 'Container Watch', body: 'Flagged: web'}]);
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    assert.equal(NOTIFICATIONS.length, 1, 'the same finding notified again');
});

test('several new findings notify once, as a count', () => {
    started({contents: report([PROCESS_FINDING, CRASH_FINDING])});
    assert.deepEqual(NOTIFICATIONS, [{title: 'Container Watch', body: '2 containers flagged'}]);
});

test('a finding that disappears and recurs notifies again', () => {
    started({contents: report([PROCESS_FINDING])});
    GLIB_FILES.set(REPORT_PATH, report([]));
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    assert.equal(NOTIFICATIONS.length, 1);
    GLIB_FILES.set(REPORT_PATH, report([PROCESS_FINDING]));
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    assert.equal(NOTIFICATIONS.length, 2);
});

test('a crash loop and a hot process in one container are two findings', () => {
    const sameContainer = {...CRASH_FINDING, container_id: PROCESS_FINDING.container_id};
    assert.notEqual(
        ContainerReport.findingKey(PROCESS_FINDING), ContainerReport.findingKey(sameContainer));
});

test('an unreadable report does not reset the dedupe, so recovery is not a storm', () => {
    started({contents: report([PROCESS_FINDING])});
    GLIB_FILES.set(REPORT_PATH, '{not json');
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    GLIB_FILES.set(REPORT_PATH, report([PROCESS_FINDING]));
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    assert.equal(NOTIFICATIONS.length, 1);
});

test('findings make the section lead and move the icon to findings', () => {
    started({contents: report([CRASH_FINDING])});
    assert.equal(section.state(), StatusDocument.FINDINGS);
    assert.equal(section.leads(), true);
    assert.equal(section.hidden(), false);
});

test('D2: backend not installed -> hidden, contributes nothing, and is not even read', () => {
    started({installed: false, contents: report([PROCESS_FINDING])});
    assert.equal(section.hidden(), true);
    assert.equal(section.state(), StatusDocument.OK);
    assert.deepEqual(NOTIFICATIONS, []);
});

test('D2: installed, no report yet -> said in words, and counts as ok', () => {
    started();
    assert.equal(section.hidden(), false);
    assert.equal(section.state(), StatusDocument.OK);
    const texts = built().texts;
    assert.ok(texts.includes('no scan recorded since this boot'), texts.join('\n'));
    assert.ok(!texts.includes('No flagged containers'),
        'an absent report must not read as a clean one');
});

test('D2: an unparseable report is unavailable, with the reason', () => {
    started({contents: '{not json'});
    assert.equal(section.state(), StatusDocument.UNAVAILABLE);
    const texts = built().texts;
    assert.ok(texts.some(text => text.startsWith('the container report could not be read: ')),
        texts.join('\n'));
    assert.ok(!texts.includes('No flagged containers'), texts.join('\n'));
});

test('D2: an unknown schema is unavailable, not rendered on what happens to parse', () => {
    started({contents: report([PROCESS_FINDING], [], {schema: 99})});
    assert.equal(section.state(), StatusDocument.UNAVAILABLE);
    assert.ok(built().texts.some(text => text.includes('schema 99')));
});

test('D2: a report with no findings list, or a non-list, is unavailable', () => {
    started({contents: JSON.stringify({schema: ContainerReport.SCHEMA_VERSION})});
    assert.equal(section.state(), StatusDocument.UNAVAILABLE);
    started({contents: report([], 'nope')});
    assert.equal(section.state(), StatusDocument.UNAVAILABLE);
    started({contents: '[]'});
    assert.equal(section.state(), StatusDocument.UNAVAILABLE);
});

test('the source has read nothing yet: hidden, and contributes nothing', () => {
    section.source.stop();
    assert.equal(section.hidden(), true);
    assert.equal(section.state(), StatusDocument.OK);
});

test('the DBus signal triggers a re-read and a change callback', () => {
    const changes = started({contents: report([])});
    const before = changes.count;
    assert.equal(DBUS.live().length, 1);
    const [subscription] = DBUS.live();
    assert.equal(subscription.iface, ContainerReport.DBUS_INTERFACE);
    assert.equal(subscription.member, ContainerReport.DBUS_SIGNAL);
    assert.equal(subscription.path, ContainerReport.DBUS_PATH);

    GLIB_FILES.set(REPORT_PATH, report([PROCESS_FINDING]));
    DBUS.emit(ContainerReport.DBUS_INTERFACE, ContainerReport.DBUS_SIGNAL,
        ContainerReport.DBUS_PATH);
    assert.equal(section.state(), StatusDocument.FINDINGS);
    assert.equal(changes.count, before + 1);
});

test('the fallback poll re-reads too', () => {
    const changes = started({contents: report([])});
    const timers = [...TIMERS.values()];
    assert.equal(timers.length, 1);
    assert.equal(timers[0].seconds, ContainerReport.POLL_INTERVAL_SECONDS);
    GLIB_FILES.set(REPORT_PATH, report([CRASH_FINDING]));
    const before = changes.count;
    timers[0].handler();
    assert.equal(section.state(), StatusDocument.FINDINGS);
    assert.equal(changes.count, before + 1);
});

test('stop() unsubscribes, removes the poll and forgets what was read', () => {
    started({contents: report([PROCESS_FINDING])});
    section.source.stop();
    assert.equal(DBUS.live().length, 0);
    assert.equal(TIMERS.size, 0);
    assert.equal(section.hidden(), true);
    assert.equal(section.state(), StatusDocument.OK);

    // The dedupe went with it: a re-enable on the same finding notifies afresh.
    NOTIFICATIONS.length = 0;
    GLIB_FILES.set(REPORT_PATH, report([PROCESS_FINDING]));
    EXECUTABLES.add(COMMAND_PATH);
    section.source.start(() => {});
    assert.equal(NOTIFICATIONS.length, 1);
    section.source.stop();
});

test('nothing in the section starts a process', () => {
    started({contents: report([PROCESS_FINDING, CRASH_FINDING], [ADVISORY])});
    const menu = built();
    for (const item of menu.items) {
        if (item?.handlers?.has('activate')) {
            item.emit('activate');
        }
    }
    assert.deepEqual(SPAWNS, [], 'a click or a render spawned something');

    // And the modules cannot: no process API is named in either file.
    for (const name of ['containerReport.js', 'sections/containers.js']) {
        const source = readFileSync(new URL(name, EXTENSION), 'utf8');
        assert.doesNotMatch(source, /Subprocess|spawn_|GLib\.spawn|communicate/,
            `${name} names a process API`);
    }
});
