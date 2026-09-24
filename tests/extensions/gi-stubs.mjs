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

    /** What the shell's menu does, and the panel calls it before every render: the menu
     * is rebuilt, not appended to. A stub without it would leave each render's lines
     * stacked on the last one's, so a test reading the menu after two renders would see
     * a menu no user could ever have. */
    removeAll() {
        this.items = [];
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
        this.label = {
            text: text ?? '', style_class: undefined, style: undefined, clutter_text: {},
        };
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
 * The shell's `Extension` base class. It keeps its metadata and does nothing else.
 *
 * `gjs-loader.mjs` claimed to answer this import from the first commit and this file had
 * no such export, so any test importing `extension.js` failed on the import — which is
 * why none did, and why the icon-per-state decision the extension's own header calls
 * "the whole reason this extension is shaped like this" had no test at all. A loader
 * entry that cannot be used reads as coverage of the file it names.
 */
export class Extension {
    constructor(metadata) {
        this.metadata = metadata;
    }
}

/** `PanelMenu.Button`, carrying the one thing the panel reads back off it: `menu`. */
export class Button {
    constructor(alignment, nameText, dontCreateMenu) {
        this.alignment = alignment;
        this.nameText = nameText;
        this.dontCreateMenu = dontCreateMenu;
        this.menu = new RecordingMenu();
        this.children = [];
        //: `disable()` must destroy the indicator; a test cannot see that it did unless
        //: the stub remembers, and an undestroyed indicator is a duplicate icon on the
        //: next enable rather than an error anybody would notice.
        this.destroyed = false;
    }

    add_child(child) {
        this.children.push(child);
    }

    destroy() {
        this.destroyed = true;
    }
}

/** What was added to the status area, by role. The shell's own panel, as far as this
 * extension is concerned. */
export const STATUS_AREA = new Map();

export const panel = {
    addToStatusArea(role, indicator) {
        STATUS_AREA.set(role, indicator);
    },
};

/**
 * `Main.screenShield`: a lock state and the one signal the shell emits for it.
 *
 * `setLocked` emits `locked-changed` only when the state actually changes, as
 * `ScreenShield._setLocked` does, so a test cannot produce a signal the shell never would.
 * `emit` exists for the one case a test must force: a duplicate signal.
 */
export const SCREEN_SHIELD = {
    locked: false,
    handlers: new Map(),
    nextId: 1,
    reset() {
        this.locked = false;
        this.handlers.clear();
    },
    connect(signal, handler) {
        const id = this.nextId++;
        this.handlers.set(id, {signal, handler});
        return id;
    },
    disconnect(id) {
        if (!this.handlers.delete(id)) {
            throw new Error(`stub screenShield: no handler ${id} to disconnect`);
        }
    },
    connectedCount() {
        return this.handlers.size;
    },
    emit(signal) {
        for (const connected of [...this.handlers.values()]) {
            if (connected.signal === signal) {
                connected.handler();
            }
        }
    },
    setLocked(locked) {
        if (this.locked === locked) {
            return;
        }
        this.locked = locked;
        this.emit('locked-changed');
    },
};

/** The shell's own binding is `export let`, and it is `null` on a system that cannot
 * lock. Re-exported live by the loader, so `setScreenShield(null)` is what an extension
 * importing `main.js` then reads. */
export let screenShield = SCREEN_SHIELD;

export function setScreenShield(value) {
    screenShield = value;
}

/**
 * `GLib`, with only what the panel actually calls.
 *
 * `file_get_contents` answers from `GLIB_FILES`, which a test sets. Absent means absent —
 * it throws the way GLib does, because "the file is not there" is a state the panel has
 * to have an answer for and a stub that returned empty would hide it.
 */
export const GLIB_FILES = new Map();

/** Paths `GLib.file_test` reports as executable. A test adds the on-demand command's
 * path to say the report play has run on this host, and leaves it out to say it has
 * not — the row must answer differently for the two (Plan 00136). */
export const EXECUTABLES = new Set();

export const GLib = {
    get_user_state_dir: () => '/stub/state',
    get_home_dir: () => '/stub/home',
    build_filenamev: parts => parts.join('/'),
    FileTest: {IS_EXECUTABLE: 'is-executable'},
    file_test(path, test) {
        return test === 'is-executable' && EXECUTABLES.has(path);
    },
    file_get_contents(path) {
        if (!GLIB_FILES.has(path)) {
            const error = new Error(`stub GLib: no such file ${path}`);
            error.code = 'NOT_FOUND';
            throw error;
        }
        return [true, new TextEncoder().encode(GLIB_FILES.get(path))];
    },
    PRIORITY_DEFAULT: 0,
    SOURCE_CONTINUE: true,
    SOURCE_REMOVE: false,
    /** Records the timer; it never fires on its own. A stub clock that fired would make
     * every test's outcome depend on how long it took to run. `TIMERS` is how a test
     * drives the poll deliberately, and how `disable()` removing it becomes visible. */
    timeout_add_seconds(priority, seconds, handler) {
        TIMERS.set(NEXT_SOURCE_ID.value, {priority, seconds, handler});
        return NEXT_SOURCE_ID.value++;
    },
    source_remove(id) {
        TIMERS.delete(id);
    },
};

/** Live timers by source id. */
export const TIMERS = new Map();

const NEXT_SOURCE_ID = {value: 1};

/** The error domain the panel matches on. Identity is all that matters: the panel asks
 * `e.matches(Gio.IOErrorEnum, Gio.IOErrorEnum.NOT_FOUND)`, so the stub's errors have to
 * answer for this exact object. */
const IO_ERROR_ENUM = {CANCELLED: 'cancelled', NOT_FOUND: 'not-found'};

/**
 * Hold the read instead of answering it, so a test can look at the panel in the state it
 * is in BEFORE any document has arrived.
 *
 * The default synchronous answer is convenient and hides one whole state: on a real host
 * the shell paints the indicator and the menu the moment `enable()` returns, and the file
 * read completes some time after that. Every decision the panel makes for `document ===
 * null` — the "reading host status…" row, the icon it starts on — is only reachable in
 * that window, and with an immediate answer the window does not exist to be asserted on.
 *
 * `enabled` is set by a test and cleared by it; `flush()` delivers what was held.
 */
export const DEFERRED_READS = {
    enabled: false,
    pending: [],
    flush() {
        const held = this.pending;
        this.pending = [];
        for (const deliver of held) {
            deliver();
        }
        return held.length;
    },
};

function ioError(code, message) {
    const error = new Error(message);
    error.matches = (domain, candidate) => domain === IO_ERROR_ENUM && candidate === code;
    return error;
}

/** Every process the panel asked for, as `{argv, flags}`, in order. The health
 * section's terminal row is the ONE place the panel spawns anything, and a test that
 * could not see the argv could not tell a report viewer from a play runner. */
export const SPAWNS = [];

/** Set `message` to make the next spawn throw the way `Gio.Subprocess.new` does when
 * the program is not there — the panel has to say so rather than fail silently. */
export const SPAWN_FAILURE = {message: null};

/** How a started process ends, for a caller that waits on it. The callback fires
 * synchronously; a test sets this before the spawn and reads the outcome after. */
export const SPAWN_OUTCOME = {successful: true, exitStatus: 0, stderr: ''};

export const Gio = {
    IOErrorEnum: IO_ERROR_ENUM,
    SubprocessFlags: {NONE: 0, STDOUT_PIPE: 1, STDERR_PIPE: 2},
    Subprocess: {
        new(argv, flags) {
            SPAWNS.push({argv, flags});
            if (SPAWN_FAILURE.message !== null) {
                throw new Error(SPAWN_FAILURE.message);
            }
            return {
                communicate_utf8_async(input, cancellable, callback) {
                    callback(this, {});
                },
                communicate_utf8_finish() {
                    return [true, '', SPAWN_OUTCOME.stderr];
                },
                get_successful: () => SPAWN_OUTCOME.successful,
                get_exit_status: () => SPAWN_OUTCOME.exitStatus,
            };
        },
    },
    Cancellable: class StubCancellable {
        constructor() {
            this.cancelled = false;
        }

        cancel() {
            this.cancelled = true;
        }
    },
    File: {
        /**
         * Answers from `GLIB_FILES`, like `GLib.file_get_contents` — one place a test
         * puts a document and both readers find it.
         *
         * The callback fires SYNCHRONOUSLY, which the real one does not, and that is the
         * one place this stub is not merely dumb: a test that had to await a shell's
         * main loop could not assert what the icon was *before* the first read landed,
         * and "starts as unavailable" is a decision the extension states in as many
         * words. Nothing in the panel depends on the callback being deferred.
         */
        new_for_path: path => ({
            path,
            load_contents_async(cancellable, callback) {
                const result = {path: this.path, cancellable};
                if (DEFERRED_READS.enabled) {
                    DEFERRED_READS.pending.push(() => callback(this, result));
                    return;
                }
                callback(this, result);
            },
            load_contents_finish(result) {
                if (result.cancellable?.cancelled) {
                    throw ioError(IO_ERROR_ENUM.CANCELLED, 'stub Gio: read cancelled');
                }
                if (!GLIB_FILES.has(result.path)) {
                    throw ioError(
                        IO_ERROR_ENUM.NOT_FOUND, `stub Gio: no such file ${result.path}`);
                }
                return [true, new TextEncoder().encode(GLIB_FILES.get(result.path))];
            },
        }),
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
    Icon: class StubIcon {
        constructor(properties) {
            /** Every value the icon has been given, in order. The icon is assigned on
             * each render, so the FINAL value is all a plain property could report —
             * and "starts as unavailable until the first read lands" is a decision the
             * extension states in as many words, which lives entirely in the values
             * that came before the last one. Recording, never behaving. */
            this.iconNames = [];
            this.icon_name = properties?.icon_name ?? '';
            this.style_class = properties?.style_class;
            //: The empty string, not undefined: the panel CLEARS the style for `ok` by
            //: assigning `''`, so a stub starting at undefined would make "cleared" and
            //: "never set" the same reading — which is the distinction under test.
            this.style = properties?.style ?? '';
        }

        get icon_name() {
            return this._iconName;
        }

        set icon_name(value) {
            this._iconName = value;
            this.iconNames.push(value);
        }
    },
    Label: class StubLabel {
        constructor(properties) {
            this.text = properties?.text ?? '';
            this.style_class = properties?.style_class;
            this.style = properties?.style;
            this.clutter_text = {};
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

/** `Pango`, only the two enums the panel sets on a label's text. Named values rather
 * than numbers, so a test can say which mode a label was given. */
export const Pango = {
    WrapMode: {WORD: 'word', CHAR: 'char', WORD_CHAR: 'word-char'},
    EllipsizeMode: {NONE: 'none', START: 'start', MIDDLE: 'middle', END: 'end'},
};

// No default export here on purpose. The panel writes `import GLib from 'gi://GLib'`, so
// each stubbed specifier needs its OWN default, and one shared default would silently
// hand `Gio` whatever this file happened to export last. `gjs-loader.mjs` builds the
// per-specifier module that re-exports the right name as its default.
