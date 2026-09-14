/**
 * Reading the host status document (Plan 00109, Task 4.1).
 *
 * The producer is `helpers/host_health/status_document.py`; this is the panel's side of
 * the same contract. Three states, and they must stay distinct:
 *
 *   ok          checked, nothing to report
 *   findings    checked, and here is what is wrong
 *   unavailable NOT checked — nothing is known
 *
 * `unavailable` is never a quiet state. A neutral icon over an empty menu is what a
 * healthy host looks like AND what a missing file, an unparseable one and a crashed
 * producer look like; collapsing those is the incident this plan exists for, rebuilt in
 * the UI layer.
 *
 * `container-watch` is the template for the async read below, but NOT for the error
 * handling. Every one of its failure paths ends in an empty findings array, which is
 * right for it — its subject is live container processes, so "the scanner has not run"
 * genuinely means nothing is flagged right now. These facts are not live: a dead DKMS
 * module for the running kernel stays dead. So absence here is ignorance and reads as
 * such.
 *
 * Design: CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md
 */

import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

export const OK = 'ok';
export const FINDINGS = 'findings';
export const UNAVAILABLE = 'unavailable';

/** Must match `status_document.SCHEMA_VERSION`. A document from a schema this does not
 * know is reported unavailable rather than rendered on the parts that happen to parse. */
export const SCHEMA_VERSION = 1;

/** Must match `status_document.SELF_SECTION`. */
export const SELF_SECTION = 'status';

/** Must match `status_document.FILE_NAME`. `helpers/gnome/check_panel_contract.py` is
 * the gate that compares the two: a mismatch makes the panel report unavailable for
 * ever, which is indistinguishable from a producer that never ran, so nothing at runtime
 * would announce it. */
export const FILE_NAME = 'host-status.json';

/** Must match `ledger.STATE_DIR_NAME`, and compared by the same gate. Half of the path
 * agreeing is not enough: the wrong directory and the wrong file name fail identically
 * and silently. */
export const STATE_DIR_NAME = 'fedora-desktop';

/** `GLib.get_user_state_dir()` applies the same XDG rule as `ledger.state_dir`. The
 * runtime dir that `container-watch` uses would be wrong here: it is cleared at boot,
 * and a post-boot health verdict that vanishes at boot has no reader. */
export function documentPath() {
    return GLib.build_filenamev([GLib.get_user_state_dir(), STATE_DIR_NAME, FILE_NAME]);
}

/** A document describing why there is no document — the same shape, so every consumer
 * renders it through the path it already has rather than needing an absence branch. */
function cannotRead(reason) {
    return {
        schema: SCHEMA_VERSION,
        generated_at: '',
        kernel: '',
        sections: {
            [SELF_SECTION]: {state: UNAVAILABLE, findings: [], unchecked: [reason]},
        },
    };
}

function parse(text) {
    let document;
    try {
        document = JSON.parse(text);
    } catch (e) {
        return cannotRead(`the host status file could not be parsed: ${e.message}`);
    }
    if (document === null || typeof document !== 'object' || Array.isArray(document)) {
        return cannotRead('the host status file does not hold a status document');
    }
    if (document.schema !== SCHEMA_VERSION) {
        return cannotRead(
            `the host status file declares schema ${document.schema}, and this panel ` +
            `only understands ${SCHEMA_VERSION}`
        );
    }
    return document;
}

/**
 * Read the document, or produce one saying why it could not be had.
 *
 * Async because a synchronous read freezes GNOME Shell — the repo's ESLint bans the
 * blocking variants outright. `callback` always receives a document, never an error:
 * there is no failure mode here that should leave the panel with nothing to render.
 */
export function read(cancellable, callback) {
    const file = Gio.File.new_for_path(documentPath());
    file.load_contents_async(cancellable, (source, result) => {
        let contents;
        try {
            const [ok, data] = source.load_contents_finish(result);
            if (!ok) {
                callback(cannotRead('the host status file could not be read'));
                return;
            }
            contents = data;
        } catch (e) {
            // A cancelled read is our own doing — a newer read is already in flight, so
            // reporting it would overwrite that one's answer with a complaint about a
            // request we withdrew.
            if (e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED)) {
                return;
            }
            if (e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND)) {
                callback(cannotRead(
                    'no host status has been recorded yet, so nothing is known about ' +
                    'this host'
                ));
                return;
            }
            callback(cannotRead(`the host status file could not be read: ${e.message}`));
            return;
        }
        callback(parse(new TextDecoder().decode(contents)));
    });
}

/** One section of a document, or an `unavailable` stating that the document has no such
 * section. A registered section that silently rendered nothing would be a check that
 * cannot fail wearing a different hat (DESIGN-panel.md §5). */
export function sectionOf(document, id) {
    const section = document?.sections?.[id];
    if (section === undefined || section === null || typeof section !== 'object') {
        return {
            state: UNAVAILABLE,
            findings: [],
            unchecked: [`the host status document has no ${id} section`],
        };
    }
    return {
        state: section.state ?? UNAVAILABLE,
        findings: Array.isArray(section.findings) ? section.findings : [],
        unchecked: Array.isArray(section.unchecked) ? section.unchecked : [],
    };
}

/** The whole document's state: the worst of its sections. A panel showing a neutral icon
 * because two of three sections are fine would be hiding the third. */
export function overallState(document, ids) {
    let sawUnavailable = false;
    for (const id of ids) {
        const state = sectionOf(document, id).state;
        if (state === FINDINGS) {
            return FINDINGS;
        }
        if (state !== OK) {
            sawUnavailable = true;
        }
    }
    return sawUnavailable ? UNAVAILABLE : OK;
}

/** Whole days since collection, or null when that cannot be known — which is NOT the
 * same as fresh, and the caller must not render it as such. */
export function ageDays(document, nowMillis) {
    const at = document?.generated_at;
    if (typeof at !== 'string' || at === '') {
        return null;
    }
    const then = Date.parse(at);
    if (Number.isNaN(then)) {
        return null;
    }
    return Math.floor((nowMillis - then) / 86400000);
}
