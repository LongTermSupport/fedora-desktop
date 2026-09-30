/**
 * The containers section (Plan 00144), the container-watch watchdog's surface inside the
 * one Fedora Desktop panel. It replaces the separate Container Watch extension.
 *
 * Read-only, like the backend. It NEVER spawns the scanner and NEVER kills or throttles
 * anything: its only actions are copying an engine-correct inspect command (a finding's
 * `exec_hint`) or restart-policy advice (an advisory's `advice`) to the clipboard, and
 * saying so.
 *
 * Unlike the other sections it has its own data source — a different file, a DBus trigger
 * and its own cadence — so it exports the optional registry hooks `extension.js` looks
 * for (DESIGN-panel.md §5, Plan 00144 D5):
 *
 *   source    start(onChange) / stop(): the DBus subscription, the poll and the read
 *   state     its contribution to the icon, folded worst-of with the document's
 *   hidden    true when the host does not have the feature: no header, no separator
 *   leads     true when it has findings, so it is listed before the standing drift
 *
 * The icon and the notification track FINDINGS ONLY. An advisory is configuration that is
 * true on every tick until someone changes it; counting it would light the panel amber for
 * ever, and an alarm nobody can clear teaches the reader to ignore the panel.
 *
 * The report and its four outcomes are `containerReport.js`; the wording of each is here.
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {wrap} from '../labels.js';
import * as ContainerReport from '../containerReport.js';

const NOTIFY_TITLE = 'Container Watch';
const CMD_TRUNCATE_LEN = 60;

/** The latest outcome, or `null` before the first read lands. Module state, owned by
 * `source` and dropped by `stop()`, so a re-enable starts from "nothing read yet". */
let current = null;
/** Keys (`findingKey`) of findings already notified; pruned when one disappears so a later
 * recurrence notifies again. */
let seenKeys = new Set();
let subscriptionId = null;
let pollSourceId = null;
let readCancellable = null;
let onChange = () => {};

function nameOf(entry) {
    return entry.container_name || entry.container_id || 'unknown';
}

function truncate(text, max) {
    return text.length <= max ? text : `${text.substring(0, max - 1)}…`;
}

function formatAge(seconds) {
    if (seconds < 60) {
        return `${seconds}s`;
    }
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) {
        return `${minutes}m`;
    }
    return `${Math.floor(minutes / 60)}h${minutes % 60}m`;
}

function describeProcess(finding, name) {
    const ageS = Number.isFinite(finding.age_s) ? finding.age_s : 0;
    const cpu = Number.isFinite(finding.cpu_pct) ? finding.cpu_pct : 0;
    return {
        summary: `${name} — ${formatAge(ageS)}, ${cpu}% CPU`,
        detail: truncate(finding.cmd || finding.argv0 || '', CMD_TRUNCATE_LEN),
    };
}

function describeCrashLoop(finding, name) {
    const count = Number.isFinite(finding.restart_count) ? finding.restart_count : 0;
    // The RATE leads when it is known, because it describes what is happening now; the
    // cumulative count never resets, so on its own it cannot tell a loop running right now
    // from one dealt with weeks ago.
    const rate = Number.isFinite(finding.restarts_per_min)
        ? `${finding.restarts_per_min}/min`
        : 'rate unknown this tick';
    const reasons = Array.isArray(finding.reasons) && finding.reasons.length > 0
        ? finding.reasons.join(' + ')
        : 'unknown';
    return {
        summary: `${name} — crash loop: ${rate}`,
        detail: `${count} restarts total · triggered by: ${reasons}`,
    };
}

function addLine(menu, text, styleClass, reactive = false) {
    const item = new PopupMenu.PopupMenuItem('', {reactive});
    item.label.text = text;
    item.label.style_class = styleClass;
    wrap(item.label);
    menu.addMenuItem(item);
    return item;
}

function copyOrSay(text, missing, copied) {
    if (!text) {
        Main.notify(NOTIFY_TITLE, missing);
        return;
    }
    St.Clipboard.get_default().set_text(St.ClipboardType.CLIPBOARD, text);
    Main.notify(NOTIFY_TITLE, copied);
}

function appendFinding(menu, finding) {
    const name = nameOf(finding);
    // TWO FINDING SHAPES SHARE THIS REPORT AND DESCRIBE DIFFERENT THINGS. A process finding
    // is about one busy PID inside a container (cpu_pct, age_s, cmd); a crash-loop finding
    // is about the CONTAINER restarting without bound and carries no process. Rendered
    // through the process columns a crash loop read "<name> — 0s, 0% CPU" with a blank
    // second line: present in the report and invisible to the reader, which for the
    // finding that can take the desktop down is the worst of both.
    const {summary, detail} = finding.kind === 'crashloop'
        ? describeCrashLoop(finding, name)
        : describeProcess(finding, name);
    const hint = typeof finding.exec_hint === 'string' ? finding.exec_hint : '';
    const item = addLine(menu, summary, 'fedora-desktop-play', true);
    item.connect('activate', () => {
        copyOrSay(hint, `No inspect hint for ${name}`, `Copied inspect command for ${name}`);
    });
    addLine(menu, detail, 'fedora-desktop-detail');
}

// Advisories sit BELOW the findings, behind their own separator and in dimmed text,
// because they are a different kind of statement: nothing is wrong now, this is how
// something is configured. Clicking one copies the remedy.
function appendAdvisories(menu, advisories) {
    if (advisories.length === 0) {
        return;
    }
    menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
    const plural = advisories.length === 1 ? '' : 's';
    addLine(menu, `${advisories.length} container${plural} set to restart without limit`,
        'fedora-desktop-detail');
    for (const advisory of advisories) {
        const name = nameOf(advisory);
        const item = addLine(menu, `${name} — ${advisory.policy || 'unknown'}`,
            'fedora-desktop-play', true);
        const advice = typeof advisory.advice === 'string' ? advisory.advice : '';
        item.connect('activate', () => {
            copyOrSay(advice, `No advice recorded for ${name}`,
                `Copied restart-policy advice for ${name}`);
        });
    }
}

// One notification per newly appeared finding, deduped so the next tick does not repeat it.
// Only a report that was READ speaks for what is flagged; an unreadable one says nothing, so
// it leaves the set alone rather than make every finding look new when it becomes readable.
function notifyNew(report) {
    if (report.kind === ContainerReport.UNREADABLE) {
        return;
    }
    const currentKeys = new Set();
    const fresh = [];
    for (const finding of report.findings) {
        const key = ContainerReport.findingKey(finding);
        currentKeys.add(key);
        if (!seenKeys.has(key)) {
            fresh.push(finding);
        }
    }
    if (fresh.length === 1) {
        Main.notify(NOTIFY_TITLE, `Flagged: ${nameOf(fresh[0])}`);
    } else if (fresh.length > 1) {
        Main.notify(NOTIFY_TITLE, `${fresh.length} containers flagged`);
    }
    seenKeys = currentKeys;
}

function refresh() {
    // Cancel any in-flight read so overlapping triggers cannot race.
    if (readCancellable !== null) {
        readCancellable.cancel();
    }
    readCancellable = new Gio.Cancellable();
    ContainerReport.read(readCancellable, report => {
        // Log the transition into "unreadable" once, not on every poll.
        if (report.kind === ContainerReport.UNREADABLE &&
            (current?.kind !== ContainerReport.UNREADABLE || current.reason !== report.reason)) {
            log(`fedora-desktop: container report unreadable: ${report.reason}`);
        }
        current = report;
        notifyNew(report);
        onChange();
    });
}

const source = {
    start(changed) {
        onChange = changed;
        // Subscribe first, then read: a signal landing between the two is harmless because
        // the read reflects current state.
        subscriptionId = Gio.DBus.session.signal_subscribe(
            null,
            ContainerReport.DBUS_INTERFACE,
            ContainerReport.DBUS_SIGNAL,
            ContainerReport.DBUS_PATH,
            null,
            Gio.DBusSignalFlags.NONE,
            refresh
        );
        pollSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            ContainerReport.POLL_INTERVAL_SECONDS,
            () => {
                refresh();
                return GLib.SOURCE_CONTINUE;
            }
        );
        refresh();
    },

    stop() {
        if (subscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(subscriptionId);
            subscriptionId = null;
        }
        if (pollSourceId !== null) {
            GLib.source_remove(pollSourceId);
            pollSourceId = null;
        }
        if (readCancellable !== null) {
            readCancellable.cancel();
            readCancellable = null;
        }
        current = null;
        seenKeys = new Set();
        onChange = () => {};
    },
};

function findingCount() {
    return current?.kind === ContainerReport.READ ? current.findings.length : 0;
}

export const section = {
    id: 'containers',
    title: 'Containers',

    /** None: this section reads no status-document section. Its state comes from `state`. */
    documentSections: [],

    source,

    state() {
        return ContainerReport.stateOf(current);
    },

    /** Not installed, or nothing read yet: there is nothing to say, so no header. */
    hidden() {
        return current === null || current.kind === ContainerReport.NOT_INSTALLED;
    },

    leads() {
        return findingCount() > 0;
    },

    build(menu) {
        addLine(menu, this.title, 'fedora-desktop-heading');

        if (current.kind === ContainerReport.NO_REPORT) {
            addLine(menu, 'no scan recorded since this boot', 'fedora-desktop-detail');
            return;
        }
        if (current.kind === ContainerReport.UNREADABLE) {
            addLine(menu, 'not checked — nothing is known about these:',
                'fedora-desktop-caveat');
            addLine(menu, `the container report could not be read: ${current.reason}`,
                'fedora-desktop-detail');
            return;
        }

        const count = current.findings.length;
        if (count === 0) {
            addLine(menu, 'No flagged containers', 'fedora-desktop-detail');
        } else {
            addLine(menu, `${count} flagged container${count === 1 ? '' : 's'}`,
                'fedora-desktop-detail');
            for (const finding of current.findings) {
                appendFinding(menu, finding);
            }
        }
        appendAdvisories(menu, current.advisories);
    },
};
