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
    constructor(text, options) {
        this.label = {text: text ?? '', style_class: undefined, style: undefined};
        // `reactive` defaults to true in PopupMenuItem, and the default is what the
        // handoff row relies on — so the stub has to carry it rather than leave it
        // undefined, or a test asserting a row IS clickable would pass on a row that
        // was explicitly made inert.
        this.reactive = options?.reactive ?? true;
        this.children = [];
        this.handlers = new Map();
    }

    add_child(child) {
        this.children.push(child);
    }

    connect(signal, handler) {
        this.handlers.set(signal, handler);
    }

    /** Fire a signal the way the shell would. Absent means the row was never wired,
     * which a test must be able to tell from a row that was. */
    emit(signal) {
        const handler = this.handlers.get(signal);
        if (handler === undefined) {
            throw new Error(`stub menu item: nothing connected to '${signal}'`);
        }
        handler();
    }
}

class StubSeparator {}

export const PopupMenuItem = StubMenuItem;
export const PopupSeparatorMenuItem = StubSeparator;

/** What `Main.notify` was asked to show, in order. A test sets it empty and reads it
 * back; the panel notifies to confirm an action a user cannot otherwise see happened. */
export const NOTIFICATIONS = [];

export function notify(title, body) {
    NOTIFICATIONS.push({title, body});
}

/** What the clipboard was last set to, and by which type. `null` until something sets
 * it — distinct from the empty string, which is a thing the panel could wrongly copy. */
export const CLIPBOARD = {type: null, text: null};

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

/**
 * `St`, with only what the panel actually calls.
 *
 * `Label` records its construction properties and nothing else — a stub that laid out
 * text would be a second implementation of St, and a test passing against it would say
 * nothing about the shell. `Clipboard` records the last write, because "the command was
 * copied" is otherwise invisible to a test and is the whole outcome of the handoff row.
 */
export const St = {
    Label: class StubLabel {
        constructor(properties) {
            this.text = properties?.text ?? '';
            this.style_class = properties?.style_class;
            this.style = properties?.style;
        }
    },
    ClipboardType: {CLIPBOARD: 'clipboard', PRIMARY: 'primary'},
    Clipboard: {
        get_default: () => ({
            set_text(type, text) {
                CLIPBOARD.type = type;
                CLIPBOARD.text = text;
            },
        }),
    },
};

// No default export here on purpose. The panel writes `import GLib from 'gi://GLib'`, so
// each stubbed specifier needs its OWN default, and one shared default would silently
// hand `Gio` whatever this file happened to export last. `gjs-loader.mjs` builds the
// per-specifier module that re-exports the right name as its default.
