/**
 * The play re-runner (Plan 00109, Task 4.3; Plan 00141).
 *
 * ONE menu item, "Re-run a play…", opens a terminal running `run.bash --rerun` from the
 * repo checkout (through `fedora-desktop-health --rerun`, which knows where the checkout
 * is). The plays themselves — which ran here, which changed, which failed — are listed,
 * picked and run by that menu, not by this panel: a per-play list in a drop-down is
 * bloated, and the chance of needing any one row is small. DESIGN-panel.md §6 and §9.
 *
 * Three rules, each the §8 boundary held on a surface that launches plays:
 *
 * 1. **A person picks; nothing runs otherwise.** The row opens a menu and runs nothing on
 *    render, on refresh or in the background. The plays run only once the person names
 *    them in that terminal.
 * 2. **The count is the producer's.** Freshness is judged once, in `check_freshness`, and
 *    carried in the document; the label counts the plays it marked as not fresh and never
 *    re-judges them.
 * 3. **It says what it launched, never that a play ran.** A spawn reports only that the
 *    terminal started. Whether a play did its work is the ledger's answer, read at the
 *    next refresh.
 *
 * It reads no check section, so it cannot move the icon: a stale play is already a
 * finding in the health section's freshness block.
 */

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';

import {wrap} from '../labels.js';
import * as StatusDocument from '../statusDocument.js';
import {launchOnDemand} from '../terminal.js';

const RERUN_ARGS = ['--rerun', '--hold'];

/** What to type if the terminal cannot start. */
const BY_HAND = `${StatusDocument.ON_DEMAND_COMMAND} --rerun`;

/** The row's text: the count of plays not known to be unchanged, when there are any. */
export function rerunLabel(plays) {
    const changed = plays.filter(entry => entry.state !== 'fresh').length;
    return changed > 0 ? `Re-run a play… (${changed} changed)` : 'Re-run a play…';
}

function detail(menu, text, styleClass = 'fedora-desktop-detail') {
    const item = new PopupMenu.PopupMenuItem('', {reactive: false});
    item.label.text = text;
    item.label.style_class = styleClass;
    wrap(item.label);
    menu.addMenuItem(item);
}

export const section = {
    id: 'plays',
    title: 'Re-run a play',

    /** None: the row reads the document's `plays` list for its count and reads no check
     * section, so the overall state — the icon — is the health section's answer alone. */
    documentSections: [],

    build(menu, document) {
        const {plays, reasons} = StatusDocument.playsOf(document);
        const item = new PopupMenu.PopupMenuItem('');
        item.label.text = rerunLabel(plays);
        item.label.style_class = 'fedora-desktop-play';
        wrap(item.label);
        item.connect('activate', () => {
            if (launchOnDemand(RERUN_ARGS, BY_HAND)) {
                Main.notify('Fedora Desktop', 'Opened a terminal to re-run plays');
            }
        });
        menu.addMenuItem(item);

        // The row still works when the list cannot be read: run.bash reads the ledger
        // itself. The missing count is said, not shown as zero.
        if (reasons.length > 0) {
            detail(menu, 'the changed-play count is not known:', 'fedora-desktop-caveat');
            for (const reason of reasons) {
                detail(menu, reason);
            }
        }
    },
};
