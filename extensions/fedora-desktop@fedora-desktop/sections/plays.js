/**
 * The play runner (Plan 00109, Task 4.3).
 *
 * Lists the plays this host's ledger has seen, each with the state `check_freshness`
 * judged for it, and launches one — the one clicked — in the user's default terminal
 * through `fedora-desktop-health --run-play`. DESIGN-panel.md §6 and §9 settle the shape.
 *
 * Three rules, each the §8 boundary held on a surface that now launches plays:
 *
 * 1. **A person clicks one named play; nothing runs otherwise.** No row runs anything on
 *    render, on refresh or in the background, and there is no "run everything" row.
 * 2. **The state is the producer's.** Freshness is judged once, in `check_freshness`,
 *    and carried in the document; this file renders it and never re-judges it.
 * 3. **It says what it launched, never that the play ran.** A spawn reports only that
 *    the terminal started. Whether the play did its work is the ledger's answer, read at
 *    the next refresh — which is what the ledger is for.
 *
 * It reads no check section, so it cannot move the icon: a stale play is already a
 * finding in the health section's freshness block.
 */

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {wrap} from '../labels.js';
import * as StatusDocument from '../statusDocument.js';
import {launchOnDemand} from '../terminal.js';

/** Words for the states `freshness.py` names. A state with no entry here is shown as it
 * is: dropping a row because this panel lacks a word for it would hide a runnable play. */
const STATE_WORDS = {
    fresh: 'unchanged since it last ran here',
    stale: 'changed since it last ran here',
    unexplained: 'differs from the checkout, and no commit explains it',
};

function describe(state) {
    return STATE_WORDS[state] ?? `state: ${state}`;
}

function detail(menu, text, styleClass = 'fedora-desktop-detail') {
    const item = new PopupMenu.PopupMenuItem('', {reactive: false});
    item.label.text = text;
    item.label.style_class = styleClass;
    wrap(item.label);
    menu.addMenuItem(item);
}

function appendPlay(menu, {play, state}) {
    const item = new PopupMenu.PopupMenuItem('');
    item.label.text = `${play} — ${describe(state)}`;
    item.label.style_class = 'fedora-desktop-play';
    wrap(item.label);
    item.connect('activate', () => {
        const byHand = `${StatusDocument.ON_DEMAND_COMMAND} --run-play ${play}`;
        if (launchOnDemand(['--run-play', play, '--hold'], byHand)) {
            Main.notify('Fedora Desktop', `Opened a terminal to run ${play}`);
        }
    });
    menu.addMenuItem(item);
}

export const section = {
    id: 'plays',
    title: 'Plays run on this machine',

    /** None: the runner renders the document's `plays` list and reads no check section,
     * so the overall state — the icon — is the health section's answer alone. */
    documentSections: [],

    build(menu, document) {
        const header = new PopupMenu.PopupMenuItem(this.title, {reactive: false});
        header.label.style = 'font-weight: bold;';
        menu.addMenuItem(header);

        const {plays, reasons} = StatusDocument.playsOf(document);
        if (reasons.length > 0) {
            detail(menu, 'not checked — nothing is known about these:',
                'fedora-desktop-caveat');
            for (const reason of reasons) {
                detail(menu, reason);
            }
        }
        if (plays.length === 0) {
            if (reasons.length === 0) {
                detail(menu, 'no play with a known state to run — the ledger has none, ' +
                    'or play freshness could not judge them');
            }
            return;
        }
        for (const entry of plays) {
            appendPlay(menu, entry);
        }
    },
};
