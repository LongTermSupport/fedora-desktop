/**
 * Reading the container-watch report (Plan 00144, Task 3.2).
 *
 * The producer is `helpers/containerwatch/cli.py` (`build_report`), which writes
 * `$XDG_RUNTIME_DIR/container-watch/report.json` atomically. This is the panel's side of
 * it, kept apart from the widgets the way `statusDocument.js` is, so the cases below can
 * be tested without a shell.
 *
 * Four outcomes, and they are not the same thing:
 *
 *   NOT_INSTALLED  the backend command is not on this host, so the host does not have the
 *                  feature. The section is hidden; it contributes nothing to the icon.
 *   NO_REPORT      installed, and no report yet. The runtime directory is cleared at boot
 *                  and the subject is LIVE, so "not scanned yet" is not a fault: nothing is
 *                  flagged now. It counts as `ok`, but the menu says it in words.
 *   UNREADABLE     a report is there and cannot be interpreted. That is ignorance, never
 *                  "clear" — it reads as `unavailable`. (The previous extension collapsed
 *                  this case into "no flagged containers".)
 *   READ           a report with findings (possibly none) and advisories.
 *
 * Advisories never move the icon and never notify: they are standing configuration, true
 * on every tick until someone changes it, and counting them would make an alarm nobody can
 * clear.
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

import {FINDINGS, OK, UNAVAILABLE} from './statusDocument.js';

export const NOT_INSTALLED = 'not-installed';
export const NO_REPORT = 'no-report';
export const UNREADABLE = 'unreadable';
export const READ = 'read';

/** Must match `core.SCHEMA_VERSION`. A report from a schema this does not know is
 * unreadable rather than rendered on the parts that happen to parse. */
export const SCHEMA_VERSION = 1;

/** The backend's command, as `play-container-watch.yml` installs it. Its presence is what
 * "this host has the feature" means. */
export const COMMAND = 'container-watch';

// The DBus signal the backend emits whenever it rewrites the report. Treated purely as
// "re-read now": the file is authoritative, so nothing depends on the signal's arguments.
export const DBUS_PATH = '/org/fedoradesktop/ContainerWatch';
export const DBUS_INTERFACE = 'org.fedoradesktop.ContainerWatch';
export const DBUS_SIGNAL = 'FindingsChanged';

/** Fallback poll (seconds): catches a signal that fired before the subscription, or any
 * missed emission. It only re-reads the report file. */
export const POLL_INTERVAL_SECONDS = 60;

export function reportPath() {
    return GLib.build_filenamev([GLib.get_user_runtime_dir(), 'container-watch', 'report.json']);
}

export function commandPath() {
    return GLib.build_filenamev([GLib.get_home_dir(), '.local', 'bin', COMMAND]);
}

function outcome(kind, reason = '') {
    return {kind, reason, findings: [], advisories: []};
}

function parse(text) {
    let report;
    try {
        report = JSON.parse(text);
    } catch (e) {
        return outcome(UNREADABLE, `the report could not be parsed: ${e.message}`);
    }
    if (report === null || typeof report !== 'object' || Array.isArray(report)) {
        return outcome(UNREADABLE, 'the report file does not hold a report');
    }
    if (report.schema !== SCHEMA_VERSION) {
        return outcome(UNREADABLE,
            `the report declares schema ${report.schema}, and this panel only ` +
            `understands ${SCHEMA_VERSION}`);
    }
    if (!Array.isArray(report.findings)) {
        return outcome(UNREADABLE, 'the report has no findings list, so nothing in it is known');
    }
    const advisories = report.advisories ?? [];
    if (!Array.isArray(advisories)) {
        return outcome(UNREADABLE, "the report's advisories could not be read");
    }
    return {kind: READ, reason: '', findings: report.findings, advisories};
}

/**
 * Read the report, or produce an outcome saying why there is none.
 *
 * Async because a synchronous read freezes GNOME Shell. `callback` receives an outcome,
 * never an error — except for a read this caller cancelled, which is silent: a newer read
 * is already in flight and answering for the withdrawn one would overwrite its result.
 */
export function read(cancellable, callback) {
    if (!GLib.file_test(commandPath(), GLib.FileTest.IS_EXECUTABLE)) {
        callback(outcome(NOT_INSTALLED));
        return;
    }
    const file = Gio.File.new_for_path(reportPath());
    file.load_contents_async(cancellable, (source, result) => {
        let contents;
        try {
            const [ok, data] = source.load_contents_finish(result);
            if (!ok) {
                callback(outcome(UNREADABLE, 'the report could not be read'));
                return;
            }
            contents = data;
        } catch (e) {
            if (e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED)) {
                return;
            }
            if (e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND)) {
                callback(outcome(NO_REPORT));
                return;
            }
            callback(outcome(UNREADABLE, `the report could not be read: ${e.message}`));
            return;
        }
        callback(parse(new TextDecoder().decode(contents)));
    });
}

/** What this outcome contributes to the icon. `null` (no read yet) and the absent feature
 * contribute `ok` — they add nothing to worst-of — while an unreadable report is
 * `unavailable`. */
export function stateOf(report) {
    if (report === null) {
        return OK;
    }
    if (report.kind === READ) {
        return report.findings.length > 0 ? FINDINGS : OK;
    }
    return report.kind === UNREADABLE ? UNAVAILABLE : OK;
}

/** A finding's identity for the new-finding notification. `kind` leads because one
 * container can be both crash-looping and running a hot process, and those are two
 * findings a human wants told about separately; a crash-loop finding has no `host_pid`. */
export function findingKey(finding) {
    const kind = finding.kind || 'process';
    return `${kind}:${finding.host_pid}:${finding.container_id}`;
}
