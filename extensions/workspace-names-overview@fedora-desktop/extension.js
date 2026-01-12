/**
 * Workspace Names in Overview Extension
 *
 * Displays workspace names on workspace thumbnail previews in the Overview mode.
 * Reads workspace names from the standard GSettings key:
 * org.gnome.desktop.wm.preferences.workspace-names
 *
 * This complements Space Bar extension which shows names in the top panel.
 */

import St from 'gi://St';
import Gio from 'gi://Gio';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const WM_PREFS_SCHEMA = 'org.gnome.desktop.wm.preferences';

export default class WorkspaceNamesOverviewExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._wmSettings = null;
        this._overviewShowingId = null;
        this._namesChangedId = null;
        this._labels = [];
    }

    enable() {
        // Get workspace name settings
        this._wmSettings = new Gio.Settings({ schema: WM_PREFS_SCHEMA });

        // Track added labels for cleanup
        this._labels = [];

        // Connect to overview showing signal
        this._overviewShowingId = Main.overview.connect('showing', () => {
            this._addLabelsToThumbnails();
        });

        // Connect to workspace names changed
        this._namesChangedId = this._wmSettings.connect('changed::workspace-names', () => {
            this._updateLabels();
        });

        // If overview is already showing, add labels now
        if (Main.overview.visible) {
            this._addLabelsToThumbnails();
        }
    }

    disable() {
        // Disconnect signals
        if (this._overviewShowingId) {
            Main.overview.disconnect(this._overviewShowingId);
            this._overviewShowingId = null;
        }

        if (this._namesChangedId) {
            this._wmSettings.disconnect(this._namesChangedId);
            this._namesChangedId = null;
        }

        // Remove all labels
        this._removeAllLabels();

        this._wmSettings = null;
    }

    _addLabelsToThumbnails() {
        // Remove existing labels first
        this._removeAllLabels();

        // Get workspace names
        const names = this._wmSettings.get_strv('workspace-names');

        // Access thumbnails - path may vary by GNOME version
        try {
            const thumbnailsBox = Main.overview._overview._controls._thumbnailsBox;
            if (!thumbnailsBox || !thumbnailsBox._thumbnails) {
                return;
            }

            const thumbnails = thumbnailsBox._thumbnails;

            thumbnails.forEach((thumbnail, index) => {
                const name = names[index] || `Workspace ${index + 1}`;

                const label = new St.Label({
                    text: name,
                    style_class: 'workspace-thumbnail-label'
                });

                // Add label to thumbnail
                thumbnail.add_child(label);
                this._labels.push({ thumbnail, label });
            });
        } catch (e) {
            // Silently fail if thumbnail path is invalid (GNOME version change)
            logError(e, 'Failed to add workspace labels');
        }
    }

    _updateLabels() {
        const names = this._wmSettings.get_strv('workspace-names');

        this._labels.forEach(({ label }, index) => {
            label.text = names[index] || `Workspace ${index + 1}`;
        });
    }

    _removeAllLabels() {
        this._labels.forEach(({ thumbnail, label }) => {
            if (thumbnail && thumbnail.contains(label)) {
                thumbnail.remove_child(label);
            }
            if (label) {
                label.destroy();
            }
        });
        this._labels = [];
    }
}
