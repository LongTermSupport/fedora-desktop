# GNOME Shell extension pattern map

Produced by a read-only search subagent (sonnet) for Plan 00109 Phase 4, so the panel
extension matches the established pattern rather than inventing a second one.

**Read this as a survey, not as verified truth.** Every path and field set below was read
out of the tree, but nothing here was executed. The two things Task 4.1 must re-derive
rather than trust: the private `_`-prefixed GNOME Shell API paths (unverified against
Shell 50 by the report's own admission), and the exact `shell-version` array, which the
compat gate checks mechanically anyway.

## 1. `extensions/` inventory

```
extensions/
├── CLAUDE.md
├── .eslintrc.json
├── .gitignore              (node_modules/, tests/assets/)
├── package.json
├── package-lock.json
├── scripts/gnome-shell-extract-js.bash
├── container-watch@fedora-desktop/          extension.js, metadata.json
├── remote-desktop-toggle@fedora-desktop/    extension.js, metadata.json
├── speech-to-text@fedora-desktop/           extension.js, metadata.json, prefs.js, schemas/
└── workspace-names-overview@fedora-desktop/ extension.js, metadata.json, stylesheet.css, README.md
```

Four custom extensions. `node_modules/` is gitignored — `npm install`ed, never committed.

## 2. `metadata.json` — the field set actually in use

```json
{
  "name": "Workspace Names in Overview",
  "description": "Shows workspace names on thumbnails in the Overview",
  "uuid": "workspace-names-overview@fedora-desktop",
  "shell-version": ["45", "46", "47", "48", "49", "50"],
  "version": 1,
  "url": "https://github.com/LongTermSupport/fedora-desktop"
}
```

Across all four: `name`, `description`, `uuid`, `shell-version` (array of string majors),
`version` (integer, always `1`), and optionally `url` (3 of 4).

**Neither `settings-schema` nor `version-name` appears anywhere** — not even in
`speech-to-text`, which ships a compiled GSettings schema and a `prefs.js`. So there is no
precedent for adding them.

## 3. Import style — ESM only, uniform across all four

```js
import St from 'gi://St';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

export default class FooExtension extends Extension {
    enable() { /* ... */ }
    disable() { /* ... */ }
}
```

No legacy `imports.ui...` anywhere, matching `CLAUDE/GnomeShell.md`'s "pre-45 style does
not work".

## 4. ESLint

Config is the **legacy** `extensions/.eslintrc.json`, not a flat `eslint.config.js`
(eslint `^8.57.1`). The rules that matter for a new extension are `no-restricted-syntax`
entries that ban **blocking** calls outright, because they freeze GNOME Shell:

| Banned selector                        | Use instead                |
| -------------------------------------- | -------------------------- |
| `.communicate(...)`                    | `communicate_async()`      |
| `.communicate_utf8(...)`               | `communicate_utf8_async()` |
| `.wait(...)` / `.wait_check(...)`      | `wait_async()`             |
| `Subprocess.new` without `*_PIPE` args | an async method            |

Globals declared: `global`, `log`, `logError`, `_`, `C_`, `N_`, `ngettext`,
`TextDecoder`, `TextEncoder`. `no-unused-vars` with `argsIgnorePattern: "^_"`.

The command, per `extensions/CLAUDE.md` — invoke the binary directly, because the hooks
daemon intercepts bare `npm run`:

```bash
cd extensions && node_modules/.bin/eslint .
```

## 5. Deployment — two established patterns

### Pattern A: declared in a vars file, deployed by the generic play

`playbooks/imports/play-gnome-shell-extensions.yml`, driven by
`vars/gnome-shell-extensions.yml`, which holds three kinds: `egos` (fetched by id from
extensions.gnome.org), `system` (dnf), and `custom` (this repo's own directories).

```yaml
- name: Deploy Declared Custom Extensions
  become: true
  become_user: "{{ user_login }}"
  ansible.builtin.copy:
    src: "{{ root_dir }}/extensions/{{ item.uuid }}/"
    dest: "{{ user_extensions_dir }}/{{ item.uuid }}/"
    owner: "{{ user_login }}"
    group: "{{ user_login }}"
    mode: '0644'
    directory_mode: '0755'
  loop: "{{ gnome_shell_extensions.custom }}"
  loop_control:
    label: "{{ item.uuid }}"
```

A whole-directory recursive copy, looped over the declared list — not `with_fileglob`,
not one task per file. Adding an extension here needs **no playbook edit**, only a vars
entry.

Schemas are compiled by an unconditional sweep (`find` for `*.gschema.xml`, then
`glib-compile-schemas` per unique dirname, with `creates:` for idempotence).

Enabling does **not** use `gnome-extensions enable`. It declares the enabled set through
dconf via a helper, then verifies each uuid and asserts the verify loop was non-empty:

```yaml
- name: Declare Deployed Extensions Enabled
  ansible.builtin.command:
    argv: "{{ ['python3', '-m', 'helpers.gnome.apply_enabled_extensions',
               '--extensions-dir', user_extensions_dir,
               '--extensions-dir', system_extensions_dir]
              + (declared_extension_uuids | map('regex_replace', '^', '--uuid=') | list) }}"
    chdir: "{{ root_dir }}"
  register: gse_enabled
  changed_when: "'GNOME-EXT-ENABLED-CHANGED' in gse_enabled.stdout"
```

### Pattern B: a standalone opt-in play per extension

`playbooks/imports/optional/common/play-{container-watch,speech-to-text,remote-desktop-toggle}.yml`
— run directly by path, not imported by `playbook-main.yml`. Used by the three extensions
that have their own backend (services, system deps).

Its enable sequence is a disable/pause/enable dance built out of
`failed_when: false` and `changed_when: rc == 0`. **Do not copy that part**: this plan
already removed one such block from `play-container-watch.yml` as a fail-fast violation,
and `changed_when: "... or rc == 0"` is a condition that cannot be false.

**Which to use for the panel:** `DESIGN-panel.md` §7 already decided — its own play, and
declared in `vars/gnome-shell-extensions.yml` alongside the other backend-owning
extensions.

## 6. Panel indicator + popup menu — the one precedent

`container-watch@fedora-desktop/extension.js` is the only classic panel-button-with-menu
extension. (`remote-desktop-toggle` uses `QuickToggle`/`SystemIndicator` — quick settings,
a different surface.)

```js
this._indicator = new PanelMenu.Button(0.0, 'Container Watch', false);
this._icon = new St.Icon({
    icon_name: 'system-run-symbolic',
    style_class: 'system-status-icon',
});
this._indicator.add_child(this._icon);
Main.panel.addToStatusArea('container-watch', this._indicator);
```

Menu construction — `PanelMenu.Button` already owns a `PopupMenu`, reached as
`this._indicator.menu`:

```js
const menu = this._indicator.menu;
menu.removeAll();

const header = new PopupMenu.PopupMenuItem('3 flagged containers', {reactive: false});
header.label.style = 'font-weight: bold;';
menu.addMenuItem(header);
menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

const item = new PopupMenu.PopupMenuItem(summary);
item.add_child(new St.Label({                    // a dimmed second line inside the row
    text: cmd,
    style: 'font-size: 0.85em; color: #aaaaaa; padding-left: 1em;',
}));
item.connect('activate', () => { /* ... */ });
menu.addMenuItem(item);
```

`{reactive: false}` is how a non-clickable status row is made. The icon carries state by
swapping `icon_name` and setting an inline `style` colour.

## 7. Reading a JSON file from disk — the established idiom

**No `Gio.FileMonitor` anywhere in the repo.** `container-watch` is the only JSON reader,
and its shape is: async load, cancellable, DBus signal as the trigger, poll as the
backstop.

```js
_refresh() {
    if (this._readCancellable !== null) {
        this._readCancellable.cancel();          // overlapping triggers cannot race
    }
    this._readCancellable = new Gio.Cancellable();

    const file = Gio.File.new_for_path(this._reportPath());
    file.load_contents_async(this._readCancellable, (source, result) => {
        let contents;
        try {
            const [ok, data] = source.load_contents_finish(result);
            if (!ok) { this._applyFindings([]); return; }
            contents = data;
        } catch (e) {
            if (!e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED)) {
                this._applyFindings([]);
            }
            return;
        }
        try {
            const report = JSON.parse(new TextDecoder().decode(contents));
            this._applyFindings(Array.isArray(report?.findings) ? report.findings : []);
        } catch (e) {
            log(`container-watch: failed to parse report.json: ${e.message}`);
            this._applyFindings([]);
        }
    });
}
```

Triggering, and the teardown that has to match it:

```js
this._dbusSubscriptionId = Gio.DBus.session.signal_subscribe(
    null, DBUS_INTERFACE, DBUS_SIGNAL, DBUS_PATH, null,
    Gio.DBusSignalFlags.NONE, () => { this._refresh(); }
);
this._pollSourceId = GLib.timeout_add_seconds(
    GLib.PRIORITY_DEFAULT, POLL_INTERVAL_SECONDS,
    () => { this._refresh(); return GLib.SOURCE_CONTINUE; }
);
this._refresh();                                 // subscribe first, then read
```

`disable()` must `Gio.DBus.session.signal_unsubscribe(id)`, `GLib.source_remove(id)`,
cancel the in-flight `Cancellable`, and `destroy()` the indicator. Callbacks also guard
on `if (!this._indicator) return;` for a read that lands after `disable()`.

**The one part the panel must NOT copy.** Every error path above ends in
`this._applyFindings([])` — missing file, unreadable file, unparseable JSON all become an
empty findings list. That is right for `container-watch`, whose subject is live processes:
"the scanner has not run" genuinely means nothing is flagged right now. It is exactly
wrong for host health, where a dead DKMS module stays dead — which is why
`status_document.read` returns an `unavailable` document instead, and why
`DESIGN-panel.md` §3 exists. Copy the async/cancellable mechanics; replace the
error handling.

Path construction uses `GLib.build_filenamev([GLib.get_user_runtime_dir(), ...])`. The
panel needs `GLib.get_user_state_dir()` instead — runtime dir is tmpfs and cleared on
reboot, and a post-boot health verdict that disappears at boot is useless.

## 8. The compat gate

There is no `scripts/qa-extension-compat.bash`. The gate is
`python3 -m helpers.gnome.check_extension_compat`, invoked from `scripts/qa-all.bash`.

It reads `fedora_version` from `vars/fedora-version.yml`, maps it through a hand-maintained
table in `helpers/gnome/fedora_compat.py` (`44 → 50`; an unmapped version is a hard
failure, deliberately not computed arithmetically), then for **every** immediate
subdirectory of `extensions/` holding a `metadata.json` asserts that the mapped GNOME
major appears in that file's `shell-version`.

So on this branch a new extension's `shell-version` must include `"50"`. The four existing
extensions all declare `["45", "46", "47", "48", "49", "50"]`.
