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
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const WM_PREFS_SCHEMA = 'org.gnome.desktop.wm.preferences';

export default class WorkspaceNamesOverviewExtension extends Extension {
    constructor(metadata) {
        super(metadata);
        this._wmSettings = null;
        this._overviewShownId = null;
        this._overviewHidingId = null;
        this._namesChangedId = null;
        this._labels = [];
        this._pendingLabelSourceId = null;
    }

    enable() {
        // Get workspace name settings
        this._wmSettings = new Gio.Settings({ schema: WM_PREFS_SCHEMA });

        // Track added labels for cleanup
        this._labels = [];

        // Use 'shown' signal - thumbnails are created during 'showing' asynchronously
        // By 'shown' time, all thumbnails on all monitors should exist
        this._overviewShownId = Main.overview.connect('shown', () => {
            this._addLabelsToThumbnails();
        });

        // Clean up labels when overview hides
        this._overviewHidingId = Main.overview.connect('hiding', () => {
            this._cancelPendingLabels();
            this._removeAllLabels();
        });

        // Connect to workspace names changed
        this._namesChangedId = this._wmSettings.connect('changed::workspace-names', () => {
            if (Main.overview.visible) {
                this._updateLabels();
            }
        });

        // If overview is already visible, add labels now (with delay to ensure thumbnails exist)
        if (Main.overview.visible) {
            this._scheduleAddLabels();
        }
    }

    disable() {
        // Cancel any pending operations
        this._cancelPendingLabels();

        // Disconnect signals
        if (this._overviewShownId) {
            Main.overview.disconnect(this._overviewShownId);
            this._overviewShownId = null;
        }

        if (this._overviewHidingId) {
            Main.overview.disconnect(this._overviewHidingId);
            this._overviewHidingId = null;
        }

        if (this._namesChangedId) {
            this._wmSettings.disconnect(this._namesChangedId);
            this._namesChangedId = null;
        }

        // Remove all labels
        this._removeAllLabels();

        this._wmSettings = null;
    }

    _cancelPendingLabels() {
        if (this._pendingLabelSourceId) {
            GLib.Source.remove(this._pendingLabelSourceId);
            this._pendingLabelSourceId = null;
        }
    }

    _scheduleAddLabels() {
        // Use idle_add to run after current event processing completes
        // This ensures thumbnails have been created
        this._cancelPendingLabels();
        this._pendingLabelSourceId = GLib.idle_add(GLib.PRIORITY_DEFAULT, () => {
            this._pendingLabelSourceId = null;
            this._addLabelsToThumbnails();
            return GLib.SOURCE_REMOVE;
        });
    }

    _addLabelsToThumbnails() {
        // Remove existing labels first
        this._removeAllLabels();

        // Get workspace names
        const names = this._wmSettings.get_strv('workspace-names');

        // Based on GNOME Shell source: js/ui/workspacesView.js and js/ui/overviewControls.js
        try {
            const controls = Main.overview._overview._controls;

            // Primary monitor: ControlsManager._thumbnailsBox
            if (controls._thumbnailsBox) {
                const thumbnails = controls._thumbnailsBox._thumbnails;
                if (thumbnails && thumbnails.length > 0) {
                    this._addLabelsToThumbnailsBox(controls._thumbnailsBox, names);
                }
            }

            // Secondary monitors: WorkspacesDisplay._workspacesViews
            // From GNOME Shell workspacesView.js:
            // - _workspacesViews is array of views per monitor
            // - Index 0 = primary monitor WorkspacesView (no thumbnails box)
            // - Index 1+ = SecondaryMonitorDisplay instances (have _thumbnails)
            const workspacesDisplay = controls._workspacesDisplay;
            if (workspacesDisplay && workspacesDisplay._workspacesViews) {
                const views = workspacesDisplay._workspacesViews;

                // Process all views - secondary monitors have _thumbnails
                for (let i = 0; i < views.length; i++) {
                    const view = views[i];

                    // SecondaryMonitorDisplay has _thumbnails (ThumbnailsBox)
                    // ThumbnailsBox has _thumbnails (array of WorkspaceThumbnail)
                    if (view._thumbnails) {
                        const thumbnailsBox = view._thumbnails;
                        if (thumbnailsBox._thumbnails && thumbnailsBox._thumbnails.length > 0) {
                            this._addLabelsToThumbnailsBox(thumbnailsBox, names);
                        }
                    }
                }
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
