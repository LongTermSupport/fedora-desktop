/**
 * Workspace Names in Overview Extension
 * Displays workspace names on workspace thumbnail previews in Overview.
 */

import St from 'gi://St';
import Gio from 'gi://Gio';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const WM_PREFS_SCHEMA = 'org.gnome.desktop.wm.preferences';

export default class WorkspaceNamesOverviewExtension extends Extension {
    enable() {
        this._wmSettings = new Gio.Settings({ schema: WM_PREFS_SCHEMA });
        this._labels = [];

        this._shownId = Main.overview.connect('shown', () => {
            this._addLabels();
        });

        this._hidingId = Main.overview.connect('hiding', () => {
            this._removeLabels();
        });
    }

    disable() {
        if (this._shownId) {
            Main.overview.disconnect(this._shownId);
            this._shownId = null;
        }
        if (this._hidingId) {
            Main.overview.disconnect(this._hidingId);
            this._hidingId = null;
        }
        this._removeLabels();
        this._wmSettings = null;
    }

    _addLabels() {
        this._removeLabels();

        try {
            const names = this._wmSettings.get_strv('workspace-names');
            const controls = Main.overview._overview._controls;

            if (!controls || !controls._thumbnailsBox)
                return;

            const thumbnails = controls._thumbnailsBox._thumbnails;
            if (!thumbnails)
                return;

            for (let i = 0; i < thumbnails.length; i++) {
                const thumbnail = thumbnails[i];
                const name = names[i] || `Workspace ${i + 1}`;

                const label = new St.Label({
                    text: name,
                    style_class: 'workspace-thumbnail-label'
                });

                thumbnail.add_child(label);
                this._labels.push({ thumbnail, label });
            }
        } catch (e) {
            log(`workspace-names-overview: ${e.message}`);
        }
    }

    _removeLabels() {
        if (!this._labels)
            return;

        for (const item of this._labels) {
            try {
                if (item.thumbnail && item.label) {
                    item.thumbnail.remove_child(item.label);
                    item.label.destroy();
                }
            } catch (e) {
                // Ignore cleanup errors
            }
        }
        this._labels = [];
    }
}
