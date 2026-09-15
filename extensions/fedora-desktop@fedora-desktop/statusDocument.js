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

/** The one section whose findings are true only of the boot they were collected in: DKMS
 * state against the kernel that was running, and units that failed during it. Matches
 * `status_document.BOOT_SCOPED_SECTION`, and the contract gate checks that it does.
 *
 * The other three checks — the ledger, play freshness, installed-versus-pinned — survive
 * a reboot unchanged, so demoting those too would be its own overclaim. */
export const BOOT_SCOPED_SECTION = 'post-boot-health';

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

/** A group's lines, and the reason it could not be read when that is what it is.
 *
 * A wrong type here must not degrade to "nothing in this group". Every defensive
 * substitution of `[]` answers "unreadable", "absent" and "genuinely empty" identically —
 * and on this surface empty means healthy, so a malformed document carrying a current
 * timestamp and a known schema reads as a clean host. `helpers/host_health/
 * status_document.unreadable_reasons` is the same rule on the producing side.
 */
function group(section, id, key) {
    const value = section[key];
    if (value === undefined || value === null) {
        return {lines: [], reasons: []};
    }
    if (!Array.isArray(value)) {
        return {
            lines: [],
            reasons: [`the ${id} section's ${key} could not be read, so what it ` +
                'reported is not known'],
        };
    }
    const lines = value.filter(line => typeof line === 'string');
    const reasons = lines.length === value.length
        ? []
        : [`the ${id} section's ${key} holds entries this reader cannot show, so what ` +
           'they said is not known'];
    return {lines, reasons};
}

/** One section of a document, or an `unavailable` stating that the document has no such
 * section. A registered section that silently rendered nothing would be a check that
 * cannot fail wearing a different hat (DESIGN-panel.md §5).
 *
 * `state` is DERIVED here rather than read off the document. The producer derives it from
 * the lists too (`status_document.section`), so the lists are the fact and the stored
 * `state` is a restatement of it. Reading the restatement made this panel a second
 * mechanism for one fact, living in one of two consumers — which is exactly how the boot
 * predicate went wrong — and a section saying `state: "ok"` over a populated `findings`
 * list rendered "nothing to report" while the login report showed the fault.
 */
export function sectionOf(document, id) {
    const section = document?.sections?.[id];
    if (section === undefined || section === null || typeof section !== 'object') {
        return {
            state: UNAVAILABLE,
            findings: [],
            unchecked: [`the host status document has no ${id} section`],
        };
    }
    const findings = group(section, id, 'findings');
    const unchecked = group(section, id, 'unchecked');
    return derived(findings.lines,
        [...unchecked.lines, ...findings.reasons, ...unchecked.reasons]);
}

/** A section from its two lists, with the state that follows from them. Something
 * known-wrong outranks something unknown, and the unchecked list is carried either way:
 * dropping it beside a fault would show a partial picture as a complete one. */
function derived(findings, unchecked) {
    let state = OK;
    if (findings.length > 0) {
        state = FINDINGS;
    } else if (unchecked.length > 0) {
        state = UNAVAILABLE;
    }
    return {state, findings, unchecked};
}

/**
 * The section as it should be READ on this boot — the one place the demotion happens.
 *
 * Both the menu and the icon need the same answer. Applying it in the menu alone would
 * leave the icon reporting a fault the menu had already explained away, which is two
 * mechanisms for one fact one more time.
 */
export function resolvedSection(document, id, running) {
    const section = sectionOf(document, id);
    if (id !== BOOT_SCOPED_SECTION || !isBootStale(document, running)) {
        return section;
    }
    // DEMOTED, not repeated. These read as present tense — "no DKMS module installed for
    // the running kernel 6.17.0" — but the text was written at collection time and that
    // is not what is running now. Left among the faults they put two different values for
    // "the running kernel" on consecutive lines of one report, one of them wrong, in
    // exactly the scenario this rule exists for. Unchecked is what they now are.
    //
    // The explanation goes FIRST, because it explains the demoted lines that follow it.
    const explanation =
        `these results were collected under kernel ${collectedKernel(document)} and this ` +
        `host is now running ${running}, so the post-boot checks describe a different ` +
        'boot and nothing has looked at the kernel you are on';
    return derived([], [explanation, ...section.findings, ...section.unchecked]);
}

/**
 * Every way the DOCUMENT's own structure could not be read, each named.
 *
 * `read` already turns absent, unparseable and unknown-schema into a self-reporting
 * document. This covers the gap immediately after it: one that parses, declares a schema
 * this reader knows, and then carries no sections this reader can interpret.
 */
export function documentReasons(document) {
    if (document === null || typeof document !== 'object') {
        return ['the host status is not a document this reader can interpret, so nothing ' +
                'in it has been read'];
    }
    const sections = document.sections;
    if (sections === null || typeof sections !== 'object' || Array.isArray(sections)) {
        return ["the host status file's sections could not be read, so no check's result " +
                'has been read from it'];
    }
    if (Object.keys(sections).length === 0) {
        // No legitimate origin: `collect` guarantees a key per producer — four even when
        // every one of them raises — and the self report emits one. So a document with no
        // sections is version skew, a truncation or a hand-edit.
        return ['the host status names no checks at all, so nothing has been established ' +
                'about this host'];
    }
    return [];
}

/** The whole document's state: the worst of its sections, read on this boot. A panel
 * showing a neutral icon because two of three sections are fine would be hiding the
 * third. */
export function overallState(document, ids, running) {
    let sawUnavailable = documentReasons(document).length > 0;
    for (const id of ids) {
        const state = resolvedSection(document, id, running).state;
        if (state === FINDINGS) {
            return FINDINGS;
        }
        if (state !== OK) {
            sawUnavailable = true;
        }
    }
    return sawUnavailable ? UNAVAILABLE : OK;
}

/** The kernel this document was collected under, or `''` when it does not say. Read
 * defensively once, here, so a consumer naming the kernel does not re-implement the
 * guard: the document comes off disk and may be from another version or truncated. */
export function collectedKernel(document) {
    const kernel = document?.kernel;
    return typeof kernel === 'string' ? kernel : '';
}

/**
 * The kernel actually running, or `''` when that could not be established.
 *
 * `/proc/sys/kernel/osrelease` rather than spawning `uname`: the panel runs inside the
 * compositor process, where a synchronous subprocess would block the shell, and this is
 * the same value `os.uname().release` reads on the producing side.
 */
export function runningKernel() {
    try {
        const [, contents] = GLib.file_get_contents('/proc/sys/kernel/osrelease');
        return new TextDecoder().decode(contents).trim();
    } catch (e) {
        // "Could not tell" — NOT "no mismatch". `isBootStale` requires both sides to be
        // known for exactly this reason, so an empty answer here suppresses the claim
        // rather than inventing one. Logged, because a panel that silently stopped
        // asking would look like a host that never reboots.
        log(`fedora-desktop: cannot read the running kernel: ${e.message}`);
        return '';
    }
}

/**
 * Whether this document describes a boot other than the one now running.
 *
 * A property of the DOCUMENT, so it lives with the document rather than in whichever
 * consumer noticed it first. There are two declared consumers — this panel and the
 * server login report — and a predicate implemented in one of them is a question the
 * other silently never asks.
 *
 * Both sides must be known. A self-reporting document carries `kernel: ''` and has
 * already explained itself; an empty running kernel means "could not tell". Reporting a
 * mismatch from either would be a finding manufactured out of ignorance, which is the
 * inverse of this plan's rule and just as wrong.
 */
export function isBootStale(document, running) {
    const collected = collectedKernel(document);
    return collected !== '' && typeof running === 'string' && running !== '' &&
        collected !== running;
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
