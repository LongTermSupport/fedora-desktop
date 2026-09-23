/**
 * A Node ESM resolver that answers the panel's GNOME imports (Plan 00109 Task 4.2).
 *
 * `gi://GLib` and `resource:///org/gnome/shell/...` are resolvable only inside a GNOME
 * Shell process. Everything else about the panel's decision-making is ordinary
 * JavaScript, so this maps those specifiers onto `gi-stubs.mjs` and lets the tests import
 * the extension's REAL modules — the shipped files, not a copy of their logic.
 *
 * Each specifier gets its own generated module rather than one shared stub, because the
 * panel imports defaults (`import GLib from 'gi://GLib'`). A single shared default would
 * hand every one of them the same object, and a test that passed against that would be
 * reporting on the stub.
 *
 * An unknown GNOME specifier is a REFUSAL. Resolving it to an empty module would let a
 * new import land silently, and the first thing anyone would know is a panel that does
 * nothing in a live shell — the failure mode this harness exists to move earlier.
 */

const STUBS = new URL('./gi-stubs.mjs', import.meta.url).href;

/** `gi://` specifier -> the named export of gi-stubs.mjs that is its default. */
const GI_DEFAULTS = new Map([
    ['gi://GLib', 'GLib'],
    ['gi://Gio', 'Gio'],
    ['gi://St', 'St'],
    ['gi://Pango', 'Pango'],
]);

/** Shell module -> the named exports it must provide. */
const SHELL_NAMES = new Map([
    ['resource:///org/gnome/shell/ui/popupMenu.js', ['PopupMenuItem', 'PopupSeparatorMenuItem']],
    ['resource:///org/gnome/shell/ui/main.js', ['notify', 'panel']],
    ['resource:///org/gnome/shell/ui/panelMenu.js', ['Button']],
    ['resource:///org/gnome/shell/extensions/extension.js', ['Extension']],
]);

function dataModule(source) {
    return `data:text/javascript,${encodeURIComponent(source)}`;
}

export async function resolve(specifier, context, next) {
    const giDefault = GI_DEFAULTS.get(specifier);
    if (giDefault !== undefined) {
        return {
            url: dataModule(`export {${giDefault} as default} from ${JSON.stringify(STUBS)};`),
            shortCircuit: true,
        };
    }

    const names = SHELL_NAMES.get(specifier);
    if (names !== undefined) {
        const reexport = names.length > 0
            ? `export {${names.join(', ')}} from ${JSON.stringify(STUBS)};`
            : 'export {};';
        return {url: dataModule(reexport), shortCircuit: true};
    }

    if (specifier.startsWith('gi://') || specifier.startsWith('resource:///')) {
        throw new Error(
            `gjs-loader: no stub for ${specifier}. Add it to gi-stubs.mjs and to this ` +
            'map — resolving it to an empty module would let the import land silently.');
    }

    return next(specifier, context);
}
