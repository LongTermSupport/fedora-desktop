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
            const controls = Main.overview._overview._controls;

            // Add labels to primary monitor thumbnails
            if (controls._thumbnailsBox && controls._thumbnailsBox._thumbnails) {
                this._addLabelsToThumbnailsBox(controls._thumbnailsBox, names);
                log(`Added labels to primary monitor (${controls._thumbnailsBox._thumbnails.length} workspaces)`);
            }

            // Add labels to secondary monitors
            // Try different paths for multi-monitor setup

            // Path 1: _workspacesDisplay._workspacesViews (GNOME 40-44)
            if (controls._workspacesDisplay && controls._workspacesDisplay._workspacesViews) {
                const views = controls._workspacesDisplay._workspacesViews;
                log(`Found ${views.length} workspace views`);

                for (let i = 0; i < views.length; i++) {
                    const view = views[i];

                    // Try view._thumbnails._thumbnails
                    if (view._thumbnails && view._thumbnails._thumbnails) {
                        this._addLabelsToThumbnailsBox(view._thumbnails, names);
                        log(`Added labels to monitor ${i} via view._thumbnails`);
                    }
                    // Try view.thumbnailsBox._thumbnails (alternative path)
                    else if (view.thumbnailsBox && view.thumbnailsBox._thumbnails) {
                        this._addLabelsToThumbnailsBox(view.thumbnailsBox, names);
                        log(`Added labels to monitor ${i} via view.thumbnailsBox`);
                    }
                }
            }

            // Path 2: Secondary monitors array (GNOME 45+)
            if (Main.overview._overview._secondaryMonitorOverviews) {
                const secondaryOverviews = Main.overview._overview._secondaryMonitorOverviews;
                log(`Found ${secondaryOverviews.length} secondary monitors`);

                secondaryOverviews.forEach((overview, index) => {
                    if (overview._thumbnailsBox && overview._thumbnailsBox._thumbnails) {
                        this._addLabelsToThumbnailsBox(overview._thumbnailsBox, names);
                        log(`Added labels to secondary monitor ${index}`);
                    }
                });
            }
        } catch (e) {
            logError(e, 'Failed to add workspace labels');
        }
    }

    _addLabelsToThumbnailsBox(thumbnailsBox, names) {
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
