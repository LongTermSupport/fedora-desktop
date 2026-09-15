/**
 * Stand-ins for the modules only a GNOME Shell process can provide (Plan 00109 Task 4.2).
 *
 * The panel's decisions — is this finding current, is this document readable, what does
 * the icon say — are plain functions over a plain object. Its *rendering* is St widgets
 * inside a live shell. Only the second needs GJS, and tying the first to it meant the
 * decisions were unprovable outside a Wayland session, which is where they most needed
 * proving: a wrong answer there is silent, and a screenshot cannot tell a demoted finding
 * from a current one.
 *
 * So `gjs-loader.mjs` resolves every `gi://` and `resource:///org/gnome/shell/` import
 * here, and the tests import the panel's REAL modules. What is exercised is the shipped
 * file, not a copy of its logic.
 *
 * The stubs are deliberately dumb. A stub that behaved would be a second implementation
 * of GNOME, and a test passing against it would say nothing about the shell.
 */

/** Records what a menu was asked to render, in order, so a test can assert on the
 * OUTCOME rather than on which function was called to produce it. */
export class RecordingMenu {
    constructor() {
        this.items = [];
    }

    addMenuItem(item) {
        this.items.push(item);
    }

    /** Every rendered line as `{text, styleClass}`, separators included as nulls so a
     * test can see the structure without depending on widget internals. */
    get lines() {
        return this.items.map(item =>
            item === null || item.label === undefined
                ? null
                : {text: item.label.text, styleClass: item.label.style_class});
    }

    /** Just the text, separators dropped — what a person would read down the menu. */
    get texts() {
        return this.lines.filter(line => line !== null).map(line => line.text);
    }
}

class StubMenuItem {
    constructor(text) {
        this.label = {text: text ?? '', style_class: undefined, style: undefined};
    }
}

class StubSeparator {}

export const PopupMenuItem = StubMenuItem;
export const PopupSeparatorMenuItem = StubSeparator;

/**
 * `GLib`, with only what the panel actually calls.
 *
 * `file_get_contents` answers from `GLIB_FILES`, which a test sets. Absent means absent —
 * it throws the way GLib does, because "the file is not there" is a state the panel has
 * to have an answer for and a stub that returned empty would hide it.
 */
export const GLIB_FILES = new Map();

export const GLib = {
    get_user_state_dir: () => '/stub/state',
    build_filenamev: parts => parts.join('/'),
    file_get_contents(path) {
        if (!GLIB_FILES.has(path)) {
            const error = new Error(`stub GLib: no such file ${path}`);
            error.code = 'NOT_FOUND';
            throw error;
        }
        return [true, new TextEncoder().encode(GLIB_FILES.get(path))];
    },
};

export const Gio = {
    File: {
        new_for_path: path => ({path}),
    },
};

export const St = {};

// No default export here on purpose. The panel writes `import GLib from 'gi://GLib'`, so
// each stubbed specifier needs its OWN default, and one shared default would silently
// hand `Gio` whatever this file happened to export last. `gjs-loader.mjs` builds the
// per-specifier module that re-exports the right name as its default.
