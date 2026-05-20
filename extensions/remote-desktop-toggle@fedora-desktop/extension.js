// Remote Desktop Toggle — GNOME Shell quick-settings toggle
//
// Shells out to ~/.local/bin/rdt for all state changes so the script remains
// usable on its own from a terminal. Status is polled every 10 s to keep the
// toggle in sync with external state changes (e.g. user ran `rdt off` in a
// shell).
//
// Companion to: files/home/.local/bin/rdt
// Companion to: playbooks/imports/optional/common/play-remote-desktop-toggle.yml

import GObject from 'gi://GObject';
import Gio from 'gi://Gio';
import GLib from 'gi://GLib';

import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import {Extension} from 'resource:///org/gnome/shell/extensions/extension.js';
import {QuickToggle, SystemIndicator} from 'resource:///org/gnome/shell/ui/quickSettings.js';

const SCRIPT = GLib.build_filenamev([GLib.get_home_dir(), '.local', 'bin', 'rdt']);
const REFRESH_INTERVAL_SECONDS = 10;

function runRdt(arg, callback) {
    let proc;
    try {
        proc = Gio.Subprocess.new(
            [SCRIPT, arg],
            Gio.SubprocessFlags.STDOUT_PIPE | Gio.SubprocessFlags.STDERR_PIPE
        );
    } catch (e) {
        log(`rdt: failed to spawn ${SCRIPT} ${arg}: ${e.message}`);
        if (callback) callback('', false);
        return;
    }
    proc.communicate_utf8_async(null, null, (p, result) => {
        try {
            const [, stdout, stderr] = p.communicate_utf8_finish(result);
            const ok = p.get_exit_status() === 0;
            if (!ok && stderr) {
                log(`rdt ${arg} exited ${p.get_exit_status()}: ${stderr}`);
            }
            if (callback) callback(stdout.trim(), ok);
        } catch (e) {
            log(`rdt: communicate failed: ${e.message}`);
            if (callback) callback('', false);
        }
    });
}

const RdtToggle = GObject.registerClass(
class RdtToggle extends QuickToggle {
    _init() {
        super._init({
            title: 'Remote Desktop',
            iconName: 'network-server-symbolic',
            toggleMode: true,
        });

        this._refreshSourceId = 0;
        this._busy = false;

        this.connect('clicked', () => this._onClicked());

        this._refreshState();
        this._refreshSourceId = GLib.timeout_add_seconds(
            GLib.PRIORITY_DEFAULT,
            REFRESH_INTERVAL_SECONDS,
            () => {
                this._refreshState();
                return GLib.SOURCE_CONTINUE;
            }
        );
    }

    _refreshState() {
        if (this._busy) return;
        runRdt('status', stdout => {
            this.checked = stdout === 'ON';
        });
    }

    _onClicked() {
        // QuickToggle in toggleMode flips `checked` before the signal fires,
        // so `checked === true` here means "user just turned it on".
        const target = this.checked ? 'on' : 'off';
        this._busy = true;
        runRdt(target, (stdout, ok) => {
            this._busy = false;
            if (ok) {
                const firstLine = stdout.split('\n')[0] || `Remote Desktop: ${target.toUpperCase()}`;
                Main.notify('Remote Desktop', firstLine);
            } else {
                Main.notify('Remote Desktop', `Toggle ${target} failed — see journalctl`);
            }
            this._refreshState();
        });
    }

    destroy() {
        if (this._refreshSourceId) {
            GLib.source_remove(this._refreshSourceId);
            this._refreshSourceId = 0;
        }
        super.destroy();
    }
});

const RdtIndicator = GObject.registerClass(
class RdtIndicator extends SystemIndicator {
    _init() {
        super._init();
        this.quickSettingsItems.push(new RdtToggle());
    }
});

export default class RemoteDesktopToggleExtension extends Extension {
    enable() {
        this._indicator = new RdtIndicator();
        Main.panel.statusArea.quickSettings.addExternalIndicator(this._indicator);
    }

    disable() {
        if (this._indicator) {
            this._indicator.quickSettingsItems.forEach(item => item.destroy());
            this._indicator.destroy();
            this._indicator = null;
        }
    }
}
