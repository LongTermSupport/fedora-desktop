/**
 * Container Watch GNOME Shell Extension
 *
 * Thin, read-only front-end for the reporting-only container-watch backend
 * (Plan 00055). It NEVER spawns the scanner and NEVER kills/throttles any
 * process — its only action is copying an engine-correct inspect command
 * (the backend's exec_hint) to the clipboard.
 *
 * Behaviour:
 * - Panel indicator: neutral when 0 findings, attention style when >0.
 * - Popup menu: one entry per finding (container_name, truncated cmd, age,
 *   cpu%); activating an entry copies that finding's exec_hint to the clipboard.
 * - Re-reads report.json (the single source of truth) on the DBus
 *   "FindingsChanged" signal AND on a fallback poll timer.
 * - Desktop notification when a NEW finding appears (deduped on host_pid + id).
 *
 * Companion to: helpers/containerwatch/ (core.py + cli.py)
 * Companion to: files/home/.local/bin/container-watch
 * Companion to: files/home/.config/systemd/user/container-watch.{service,timer}
 */

import St from 'gi://St';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import * as PanelMenu from 'resource:///org/gnome/shell/ui/panelMenu.js';
import * as PopupMenu from 'resource:///org/gnome/shell/ui/popupMenu.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';

// DBus signal emitted by the backend whenever it rewrites report.json. We treat
// it purely as a "re-read report.json now" trigger — the file is authoritative,
// so we never depend on the signal's argument GVariant types.
const DBUS_PATH = '/org/fedoradesktop/ContainerWatch';
const DBUS_INTERFACE = 'org.fedoradesktop.ContainerWatch';
const DBUS_SIGNAL = 'FindingsChanged';

// Fallback poll interval (seconds): catches a signal that fired before we
// subscribed, or any missed emission. It only re-reads the report file.
const POLL_INTERVAL_SECONDS = 60;

const CMD_TRUNCATE_LEN = 60;

export default class ContainerWatchExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._indicator = null;
        this._icon = null;
        this._dbusSubscriptionId = null;
        this._pollSourceId = null;
        this._readCancellable = null;
        // Dedupe keys (host_pid:container_id) for findings already notified.
        this._seenKeys = new Set();
    }

    enable() {
        this._indicator = new PanelMenu.Button(0.0, 'Container Watch', false);

        this._icon = new St.Icon({
            icon_name: 'system-run-symbolic',
            style_class: 'system-status-icon',
        });
        this._indicator.add_child(this._icon);

        Main.panel.addToStatusArea('container-watch', this._indicator);

        // Subscribe first, then do an initial read — any signal that fires
        // between these two is harmless because the read reflects current state.
        this._dbusSubscriptionId = Gio.DBus.session.signal_subscribe(
            null,                   // sender (any)
            DBUS_INTERFACE,         // interface
            DBUS_SIGNAL,            // signal name
            DBUS_PATH,              // object path
            null,                   // arg0 (any)
            Gio.DBusSignalFlags.NONE,
            () => {
                this._refresh();
            }
        );

        this._pollSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            POLL_INTERVAL_SECONDS,
            () => {
                this._refresh();
                return GLib.SOURCE_CONTINUE;
            }
        );

        this._refresh();
    }

    disable() {
        if (this._dbusSubscriptionId !== null) {
            Gio.DBus.session.signal_unsubscribe(this._dbusSubscriptionId);
            this._dbusSubscriptionId = null;
        }

        if (this._pollSourceId !== null) {
            GLib.source_remove(this._pollSourceId);
            this._pollSourceId = null;
        }

        if (this._readCancellable !== null) {
            this._readCancellable.cancel();
            this._readCancellable = null;
        }

        if (this._indicator) {
            this._indicator.destroy();
            this._indicator = null;
        }

        this._icon = null;
        this._seenKeys = new Set();
    }

    _reportPath() {
        return GLib.build_filenamev([
            GLib.get_user_runtime_dir(),
            'container-watch',
            'report.json',
        ]);
    }

    // Asynchronously re-read report.json and refresh the UI. Never blocks the
    // shell thread (ESLint forbids the synchronous load_contents/communicate).
    _refresh() {
        // Cancel any in-flight read so overlapping triggers cannot race.
        if (this._readCancellable !== null) {
            this._readCancellable.cancel();
        }
        this._readCancellable = new Gio.Cancellable();

        const file = Gio.File.new_for_path(this._reportPath());
        file.load_contents_async(this._readCancellable, (source, result) => {
            let contents;
            try {
                const [ok, data] = source.load_contents_finish(result);
                if (!ok) {
                    this._applyFindings([]);
                    return;
                }
                contents = data;
            } catch (e) {
                // Missing file (scanner has not run yet) or cancelled read:
                // treat as "no findings" rather than swallowing silently.
                if (!e.matches?.(Gio.IOErrorEnum, Gio.IOErrorEnum.CANCELLED)) {
                    this._applyFindings([]);
                }
                return;
            }

            let findings;
            try {
                const text = new TextDecoder().decode(contents);
                const report = JSON.parse(text);
                findings = Array.isArray(report?.findings) ? report.findings : [];
            } catch (e) {
                log(`container-watch: failed to parse report.json: ${e.message}`);
                this._applyFindings([]);
                return;
            }

            this._applyFindings(findings);
        });
    }

    _applyFindings(findings) {
        // Guard against a callback that lands after disable().
        if (!this._indicator || !this._icon) {
            return;
        }

        this._updateIcon(findings.length);
        this._rebuildMenu(findings);
        this._notifyNew(findings);
    }

    _updateIcon(count) {
        if (!this._icon) {
            return;
        }
        if (count > 0) {
            this._icon.icon_name = 'dialog-warning-symbolic';
            this._icon.style = 'color: #ffaa00;';  // amber attention accent
        } else {
            this._icon.icon_name = 'system-run-symbolic';
            this._icon.style = '';  // neutral
        }
    }

    _rebuildMenu(findings) {
        const menu = this._indicator.menu;
        menu.removeAll();

        if (findings.length === 0) {
            const item = new PopupMenu.PopupMenuItem('No flagged containers', {
                reactive: false,
            });
            menu.addMenuItem(item);
            return;
        }

        const header = new PopupMenu.PopupMenuItem(
            `${findings.length} flagged container${findings.length === 1 ? '' : 's'}`,
            {reactive: false}
        );
        header.label.style = 'font-weight: bold;';
        menu.addMenuItem(header);
        menu.addMenuItem(new PopupMenu.PopupSeparatorMenuItem());

        for (const finding of findings) {
            const name = finding.container_name || finding.container_id || 'unknown';
            const ageS = Number.isFinite(finding.age_s) ? finding.age_s : 0;
            const cpu = Number.isFinite(finding.cpu_pct) ? finding.cpu_pct : 0;
            const cmd = this._truncate(finding.cmd || finding.argv0 || '', CMD_TRUNCATE_LEN);

            const summary = `${name} — ${this._formatAge(ageS)}, ${cpu}% CPU`;
            const item = new PopupMenu.PopupMenuItem(summary);

            // Second line: the command, dimmed.
            const detail = new St.Label({
                text: cmd,
                style: 'font-size: 0.85em; color: #aaaaaa; padding-left: 1em;',
            });
            item.add_child(detail);

            const hint = typeof finding.exec_hint === 'string' ? finding.exec_hint : '';
            item.connect('activate', () => {
                this._copyHint(hint, name);
            });
            menu.addMenuItem(item);
        }
    }

    // Copy the engine-correct inspect command (guidance only — never executed
    // by this extension) to the clipboard and confirm.
    _copyHint(hint, name) {
        if (!hint) {
            Main.notify('Container Watch', `No inspect hint for ${name}`);
            return;
        }
        St.Clipboard.get_default().set_text(St.ClipboardType.CLIPBOARD, hint);
        Main.notify('Container Watch', `Copied inspect command for ${name}`);
    }

    // Notify once per newly-appeared finding. Dedupe on host_pid:container_id so
    // the same finding on the next tick does not re-notify; prune keys that have
    // disappeared so a later recurrence notifies again.
    _notifyNew(findings) {
        const currentKeys = new Set();
        const fresh = [];

        for (const finding of findings) {
            const key = `${finding.host_pid}:${finding.container_id}`;
            currentKeys.add(key);
            if (!this._seenKeys.has(key)) {
                fresh.push(finding);
            }
        }

        if (fresh.length === 1) {
            const f = fresh[0];
            const name = f.container_name || f.container_id || 'unknown';
            Main.notify('Container Watch', `Flagged: ${name}`);
        } else if (fresh.length > 1) {
            Main.notify('Container Watch', `${fresh.length} containers flagged`);
        }

        this._seenKeys = currentKeys;
    }

    _truncate(text, max) {
        if (text.length <= max) {
            return text;
        }
        return `${text.substring(0, max - 1)}…`;
    }

    _formatAge(seconds) {
        if (seconds < 60) {
            return `${seconds}s`;
        }
        const minutes = Math.floor(seconds / 60);
        if (minutes < 60) {
            return `${minutes}m`;
        }
        const hours = Math.floor(minutes / 60);
        return `${hours}h${minutes % 60}m`;
    }
}
