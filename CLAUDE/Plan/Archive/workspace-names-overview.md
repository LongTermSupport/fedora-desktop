# Workspace Names in Overview Extension

## Project Status: **READY FOR IMPLEMENTATION**

---

## Overview

Create a GNOME Shell extension that displays workspace names on the workspace thumbnail previews in the Overview mode. This complements the existing Space Bar extension which shows workspace names in the top panel but not in the Overview.

**Problem**: Space Bar extension provides named workspaces in the top panel, but when entering Overview mode (Super key), the workspace thumbnails don't display their names, making it difficult to identify which workspace to jump to.

**Solution**: Create `workspace-names-overview@fedora-desktop` extension that adds text labels to workspace thumbnails in the Overview.

---

## Research Summary

### Existing Extensions Evaluated

| Extension | GNOME Support | Does What We Need? | Status |
|-----------|---------------|-------------------|--------|
| [Workspace Titles](https://extensions.gnome.org/extension/4393/workspace-titles/) | 3.28-3.36 only | Yes - adds names to overview thumbnails | Abandoned, incompatible |
| [Workspace Matrix](https://extensions.gnome.org/extension/1485/workspace-matrix/) | Up to 48 | No - labels in switcher popup only | Active but wrong feature |
| [Space Bar](https://extensions.gnome.org/extension/5090/space-bar/) | Up to 48 | No - panel only, not overview | Already installed |

**Conclusion**: No maintained extension exists for GNOME 48 that shows workspace names on Overview thumbnails. Must create custom extension.

### Workspace Names Storage

Space Bar uses the standard GNOME GSettings key:
```
org.gnome.desktop.wm.preferences.workspace-names
```

This is the standard location, so our extension can read from it directly without depending on Space Bar's internal schema.

---

## Technical Approach

### Architecture

The extension will:
1. Hook into the GNOME Shell Overview's workspace thumbnails
2. Read workspace names from `org.gnome.desktop.wm.preferences.workspace-names`
3. Add `St.Label` widgets to each `WorkspaceThumbnail` actor
4. Update labels when workspace names change (GSettings signal)
5. Clean up labels on disable

### Key GNOME Shell APIs

```javascript
// Access workspace thumbnails
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
// Main.overview._overview._controls._thumbnailsBox._thumbnails

// Read workspace names
const WM_PREFS_SCHEMA = 'org.gnome.desktop.wm.preferences';
const settings = new Gio.Settings({ schema: WM_PREFS_SCHEMA });
const names = settings.get_strv('workspace-names');

// Add label to thumbnail
const label = new St.Label({ text: 'Workspace Name', style_class: 'workspace-thumbnail-label' });
thumbnail.add_child(label);
```

### Styling

Labels should be:
- Positioned at bottom center of each thumbnail
- Semi-transparent background for readability over any content
- Small font size to not obscure the preview
- Fallback to "Workspace N" if no name is set

---

## Implementation Plan

### Phase 1: Create Extension Structure

**Files to create:**

1. `extensions/workspace-names-overview@fedora-desktop/metadata.json`
   ```json
   {
     "name": "Workspace Names in Overview",
     "description": "Shows workspace names on thumbnails in the Overview",
     "uuid": "workspace-names-overview@fedora-desktop",
     "shell-version": ["45", "46", "47", "48"],
     "version": 1,
     "url": "https://github.com/LongTermSupport/fedora-desktop"
   }
   ```

2. `extensions/workspace-names-overview@fedora-desktop/extension.js`
   - Main extension class
   - Hook into Overview workspace thumbnails
   - Add/remove labels on enable/disable
   - Listen for workspace name changes

3. `extensions/workspace-names-overview@fedora-desktop/stylesheet.css`
   - Style for `.workspace-thumbnail-label` class

### Phase 2: Core Implementation

**extension.js structure:**

```javascript
import St from 'gi://St';
import Gio from 'gi://Gio';
import GObject from 'gi://GObject';
import * as Main from 'resource:///org/gnome/shell/ui/main.js';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const WM_PREFS_SCHEMA = 'org.gnome.desktop.wm.preferences';

export default class WorkspaceNamesOverviewExtension extends Extension {
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
        const thumbnailsBox = Main.overview._overview._controls._thumbnailsBox;
        const thumbnails = thumbnailsBox._thumbnails;

        thumbnails.forEach((thumbnail, index) => {
            const name = names[index] || `Workspace ${index + 1}`;

            const label = new St.Label({
                text: name,
                style_class: 'workspace-thumbnail-label'
            });

            // Position at bottom of thumbnail
            label.set_position(0, thumbnail.height - label.height - 4);

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
            if (thumbnail.contains(label)) {
                thumbnail.remove_child(label);
            }
            label.destroy();
        });
        this._labels = [];
    }
}
```

### Phase 3: Styling

**stylesheet.css:**

```css
.workspace-thumbnail-label {
    font-size: 10px;
    font-weight: bold;
    color: white;
    background-color: rgba(0, 0, 0, 0.6);
    border-radius: 4px;
    padding: 2px 6px;
    margin: 4px;
    text-align: center;
}
```

### Phase 4: Validation & Deployment

1. **Run ESLint** to catch blocking operations:
   ```bash
   cd /workspace/extensions && npm run lint
   ```

2. **Create/update Ansible playbook** to deploy the extension:
   - Add to `playbooks/imports/optional/common/play-gnome-shell-extensions.yml`
   - Or create dedicated `play-workspace-names-overview.yml`

3. **Test deployment**:
   ```bash
   ansible-playbook playbooks/imports/optional/common/play-gnome-shell-extensions.yml
   gnome-extensions enable workspace-names-overview@fedora-desktop
   ```

---

## Potential Challenges

### 1. GNOME Shell API Path Changes

The path to access thumbnails (`Main.overview._overview._controls._thumbnailsBox._thumbnails`) may vary between GNOME versions. May need version detection or try/catch.

**Mitigation**: Check GNOME Shell source for exact path in GNOME 48.

### 2. Thumbnail Lifecycle

Thumbnails may be recreated when workspaces change. Need to handle:
- Workspace added/removed
- Overview hidden and re-shown
- Monitor configuration changes

**Mitigation**: Re-add labels on overview 'showing' signal, track labels for cleanup.

### 3. Label Positioning

Label positioning relative to thumbnail may need adjustment based on:
- Thumbnail scaling
- Number of workspaces
- Multi-monitor setups

**Mitigation**: Use relative positioning within the thumbnail actor.

---

## Testing Protocol

### Manual Testing

1. Enable extension:
   ```bash
   gnome-extensions enable workspace-names-overview@fedora-desktop
   ```

2. Set workspace names via Space Bar or dconf:
   ```bash
   gsettings set org.gnome.desktop.wm.preferences workspace-names "['Dev', 'Browser', 'Chat', 'Music']"
   ```

3. Press Super to open Overview - verify labels appear on thumbnails

4. Change a workspace name - verify label updates

5. Disable extension - verify labels are removed

### Edge Cases to Test

- [ ] No workspace names set (should show "Workspace 1", "Workspace 2", etc.)
- [ ] More workspaces than names (should fall back to numbered names)
- [ ] Single workspace (thumbnail may be hidden by default)
- [ ] Dynamic workspaces enabled/disabled
- [ ] Multi-monitor setup

---

## Files to Create/Modify

### New Files

| File | Description |
|------|-------------|
| `extensions/workspace-names-overview@fedora-desktop/metadata.json` | Extension metadata |
| `extensions/workspace-names-overview@fedora-desktop/extension.js` | Main extension code |
| `extensions/workspace-names-overview@fedora-desktop/stylesheet.css` | Label styling |

### Modified Files

| File | Change |
|------|--------|
| `playbooks/imports/optional/common/play-gnome-shell-extensions.yml` | Add deployment for new extension |

---

## Success Criteria

1. Extension passes ESLint with no errors
2. Extension loads without JS errors in GNOME Shell journal
3. Labels appear on workspace thumbnails in Overview
4. Labels show correct workspace names from GSettings
5. Labels update when workspace names change
6. Extension cleanly disables without leaving artifacts
7. Playbook deploys and enables extension successfully

---

## References

- [GNOME Shell Extensions Guide](https://gjs.guide/extensions/)
- [Space Bar Extension](https://github.com/christopher-l/space-bar) - Reference for workspace name handling
- [Workspace Titles (old)](https://github.com/ben8p/gnome-extension-workspace-titles) - Reference implementation (outdated API)
- Existing extension in project: `extensions/speech-to-text@fedora-desktop/` - Pattern reference

---

**Document Created**: 2025-01-12
**Status**: Ready for implementation
**Assigned Agent**: TBD
