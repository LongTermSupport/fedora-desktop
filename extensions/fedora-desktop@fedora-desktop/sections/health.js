/**
 * The health section (Plan 00109, Tasks 4.1 and 4.2).
 *
 * The scaffold's first real consumer, which is what keeps the registry honest: a
 * registry with nothing registered cannot be exercised (DESIGN-panel.md §5).
 *
 * It renders the three checks Phase 3 built — post-boot health, play freshness, and
 * installed-versus-pinned — and it renders them, never re-implements them. A check
 * reimplemented in JavaScript would be a second check that drifts from the one under
 * test.
 *
 * The one rule this file exists to hold: known faults and things nobody could check are
 * shown as different kinds of thing. A list that mixes them and distinguishes neither
 * reads like a complete picture of a machine, which is how the incident happened.
 */

import St from 'gi://St';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import * as StatusDocument from '../statusDocument.js';

/** Section ids, matching `login_report.HEALTH` / `FRESHNESS` / `PINS`. These are the
 * document's keys, so they are interface: rename one here and the section silently
 * reports unavailable for ever. */
const CHECKS = [
    {id: 'post-boot-health', title: 'This machine now'},
    // Second, in the document's own order: an empty ledger is a fault here and now, and
    // it is also why the two ledger-reading checks below would have nothing to say — so
    // reading top to bottom meets the cause before the silence it explains.
    {id: 'play-ledger', title: 'Record of what has run here'},
    {id: 'play-freshness', title: 'Plays since they last ran'},
    {id: 'installed-vs-pinned', title: 'Installed versus pinned'},
];

/** One line per finding. `reactive: false`, and that is Task 3.3's answer rather than an
 * absence of one: there is ONE handoff file and it describes EVERY finding, so a
 * clickable row per finding would offer the same command N times while implying each row
 * had its own. The offer is section-level, below. A row that looks clickable and is not
 * would be its own small lie. */
function findingItem(text, styleClass) {
    const item = new PopupMenu.PopupMenuItem('', {reactive: false});
    item.label.text = text;
    item.label.style_class = styleClass;
    return item;
}

/**
 * The handoff offer — Task 3.3's one-click half, and the decision DESIGN-panel.md §9
 * left to this task.
 *
 * **It copies the command; it does not run it.** Three reasons, and the first is the one
 * that would break a launch:
 *
 * 1. `claude` reads the repository it is started in, and this diagnosis is about
 *    playbooks. The panel does not know where the checkout is and the status document
 *    does not carry it, so a launch would start Claude Code in the compositor's working
 *    directory — where it cannot see the thing it is being asked about.
 * 2. `container-watch` already copies its inspect hint and notifies, on this same
 *    surface. A second idiom for "here is a command, you run it" would be one to learn
 *    for no gain.
 * 3. DESIGN-panel.md §8: the panel offers, a human decides — and a clickable surface is
 *    precisely where that erodes. §6's terminal-launching mechanism belongs to Task 4.3,
 *    which must choose it on evidence this task does not have.
 *
 * Absent when there is no handoff, which is the honest rendering of all three ways that
 * happens: a clean host has nothing to diagnose, and a failed write or an unreadable
 * document licenses no offer either. The findings themselves are rendered regardless, so
 * a host with faults never goes quiet — it just has no button.
 */
function appendHandoffOffer(menu, document) {
    const path = StatusDocument.handoffPath(document);
    if (path === '') {
        return;
    }
    const command = `${StatusDocument.HANDOFF_COMMAND} '${path}'`;
    menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
    const item = new PopupMenu.PopupMenuItem('Discuss these findings with Claude Code');
    const detail = new St.Label({
        text: command,
        style_class: 'fedora-desktop-detail',
    });
    item.add_child(detail);
    item.connect('activate', () => {
        St.Clipboard.get_default().set_text(St.ClipboardType.CLIPBOARD, command);
        Main.notify('Fedora Desktop', 'Copied the handoff command');
    });
    menu.addMenuItem(item);
}

function appendCheck(menu, document, check, runningKernel) {
    // `resolvedSection`, not `sectionOf`: the boot demotion has to be the same answer the
    // icon gets, and a copy of it here would be a second mechanism for one fact.
    const section = StatusDocument.resolvedSection(document, check.id, runningKernel);

    const header = new PopupMenu.PopupMenuItem(check.title, {reactive: false});
    header.label.style = 'font-weight: bold;';
    menu.addMenuItem(header);

    if (section.state === StatusDocument.OK) {
        menu.addMenuItem(findingItem('nothing to report', 'fedora-desktop-detail'));
        return;
    }

    for (const text of section.findings) {
        menu.addMenuItem(findingItem(text, 'fedora-desktop-finding'));
    }

    if (section.unchecked.length > 0) {
        // Stated, not implied by styling alone. "Not checked" reads as a mild caveat
        // next to a fault; it is in fact a statement that nothing is known, and the
        // heading has to say so in words.
        const caveat = new PopupMenu.PopupMenuItem('', {reactive: false});
        caveat.label.text = 'not checked — nothing is known about these:';
        caveat.label.style_class = 'fedora-desktop-caveat';
        menu.addMenuItem(caveat);
        for (const text of section.unchecked) {
            menu.addMenuItem(findingItem(text, 'fedora-desktop-detail'));
        }
    }
}

/**
 * The document's own account of itself, when it has one.
 *
 * `StatusDocument.read` answers an absent, unparseable or unknown-schema file with a
 * document whose ONLY section is `SELF_SECTION`, holding the one sentence that says what
 * actually happened. A real document has no such section, which is what makes its
 * presence the test here — `sectionOf` cannot be used to decide this, because it answers
 * for a missing section by deriving an `unavailable` one, so it says the same thing about
 * a healthy document as about an unreadable one.
 *
 * Rendering the three checks against such a document produces three derived "has no
 * <id> section" lines and drops the reason entirely, which makes "this host has never
 * recorded a status" and "the file is corrupt" look identical. The server-side login
 * report prints that reason; this is the primary surface and cannot say less.
 *
 * Returns true when it rendered, so the caller skips the checks: there is no data behind
 * them, and three `unavailable` blocks restate one absence three times.
 */
function appendSelfReport(menu, document) {
    const self = document?.sections?.[StatusDocument.SELF_SECTION];
    if (!self) {
        return false;
    }
    const normalised = StatusDocument.sectionOf(document, StatusDocument.SELF_SECTION);
    for (const text of normalised.findings) {
        menu.addMenuItem(findingItem(text, 'fedora-desktop-finding'));
    }
    for (const text of normalised.unchecked) {
        menu.addMenuItem(findingItem(text, 'fedora-desktop-detail'));
    }
    return true;
}

/** When the document was collected, always shown. A panel presenting login-time findings
 * at teatime as current states something the checks did not measure (DESIGN-panel.md §4).
 * An unknown age is reported as unknown rather than omitted, because omitting it reads
 * as "recent". */
function appendCollectedAt(menu, document, nowMillis) {
    const age = StatusDocument.ageDays(document, nowMillis);
    let text;
    if (age === null) {
        text = 'collection time unknown';
    } else if (age === 0) {
        text = 'collected today';
    } else if (age === 1) {
        text = 'collected yesterday';
    } else {
        text = `collected ${age} days ago`;
    }
    const item = new PopupMenu.PopupMenuItem('', {reactive: false});
    item.label.text = text;
    item.label.style_class = 'fedora-desktop-detail';
    menu.addMenuItem(item);
}

export const section = {
    id: 'health',
    title: 'Host health',

    /** Every check id this section reads, so the panel can compute its overall state
     * from the same set the menu renders — rather than from a second list that could
     * disagree with it. */
    documentSections: CHECKS.map(check => check.id),

    build(menu, document, nowMillis, runningKernel) {
        appendCollectedAt(menu, document, nowMillis);
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());
        if (appendSelfReport(menu, document)) {
            return;
        }
        // A shape this cannot read is REPORTED, before anything derived from it. Rendering
        // the four checks against a document with no readable sections produces four
        // "has no <id> section" lines and never says what actually happened.
        const reasons = StatusDocument.documentReasons(document);
        if (reasons.length > 0) {
            const caveat = new PopupMenu.PopupMenuItem('', {reactive: false});
            caveat.label.text = 'not checked — nothing is known about these:';
            caveat.label.style_class = 'fedora-desktop-caveat';
            menu.addMenuItem(caveat);
            for (const text of reasons) {
                menu.addMenuItem(findingItem(text, 'fedora-desktop-detail'));
            }
            return;
        }
        for (const check of CHECKS) {
            appendCheck(menu, document, check, runningKernel);
        }
        // LAST, after everything it refers to. The offer is about the findings above it,
        // and a button before them would ask the user to act before reading.
        appendHandoffOffer(menu, document);
    },
};
