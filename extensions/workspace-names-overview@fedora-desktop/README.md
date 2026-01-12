# Workspace Names in Overview

A GNOME Shell extension that displays workspace names on workspace thumbnail previews in the Overview mode.

## Purpose

This extension complements the **Space Bar** extension which shows workspace names in the top panel. When you press the Super key to enter Overview mode, this extension adds text labels to the workspace thumbnails showing each workspace's name, making it easier to identify which workspace to jump to.

## Features

- Displays workspace names on workspace thumbnails in Overview
- Reads workspace names from standard GSettings location: `org.gnome.desktop.wm.preferences.workspace-names`
- Automatically updates when workspace names change
- Falls back to "Workspace 1", "Workspace 2", etc. if no names are set
- Clean, semi-transparent labels that don't obscure the preview
- Compatible with GNOME Shell 45-48 (Fedora 40-42+)

## Installation

### Via Ansible Playbook (Recommended)

This is the recommended installation method for the fedora-desktop project:

```bash
ansible-playbook /workspace/playbooks/imports/optional/common/play-gnome-shell-extensions.yml
```

The playbook will:
1. Install other useful GNOME extensions
2. Deploy this custom extension to `~/.local/share/gnome-shell/extensions/`
3. Automatically enable the extension

### Manual Installation

If you need to install manually:

```bash
cp -r /workspace/extensions/workspace-names-overview@fedora-desktop \
      ~/.local/share/gnome-shell/extensions/workspace-names-overview@fedora-desktop
```

Then restart GNOME Shell:
- X11: Press Alt+F2, type `r`, press Enter
- Wayland: Log out and log back in

## Usage

### Enabling the Extension

The extension is automatically enabled when deployed via the Ansible playbook. If you need to manually enable/disable:

```bash
# Enable
gnome-extensions enable workspace-names-overview@fedora-desktop

# Disable
gnome-extensions disable workspace-names-overview@fedora-desktop
```

Or use GNOME Extensions app (install with `dnf install gnome-extensions-app`).

### Setting Workspace Names

You can set workspace names using:

1. **Space Bar extension** (if installed) - Use its UI
2. **dconf-editor** - Navigate to `org.gnome.desktop.wm.preferences.workspace-names`
3. **gsettings command**:
   ```bash
   gsettings set org.gnome.desktop.wm.preferences workspace-names "['Dev', 'Browser', 'Chat', 'Music']"
   ```

### Viewing the Labels

1. Press Super key to open Overview
2. Look at the workspace thumbnails on the left or right side (depending on your setup)
3. Each thumbnail will show its workspace name at the bottom

## Configuration

The extension uses standard GNOME settings, so no additional configuration is needed. It automatically:
- Detects when workspace names change
- Updates labels in real-time
- Cleans up when disabled

## Troubleshooting

### Labels Don't Appear

1. **Check if extension is enabled:**
   ```bash
   gnome-extensions list
   gnome-extensions info workspace-names-overview@fedora-desktop
   ```

2. **Enable the extension:**
   ```bash
   gnome-extensions enable workspace-names-overview@fedora-desktop
   ```

3. **Check for errors in GNOME Shell logs:**
   ```bash
   journalctl -f /usr/bin/gnome-shell
   ```

4. **Verify workspace names are set:**
   ```bash
   gsettings get org.gnome.desktop.wm.preferences workspace-names
   ```

### Extension Fails to Load

1. **Restart GNOME Shell:**
   - X11: Alt+F2, type `r`, press Enter
   - Wayland: Log out and log back in

2. **Check GNOME Shell version compatibility:**
   ```bash
   gnome-shell --version
   ```
   This extension supports GNOME Shell 45-48.

3. **Reinstall via playbook:**
   ```bash
   ansible-playbook /workspace/playbooks/imports/optional/common/play-gnome-shell-extensions.yml
   ```

### Labels Appear in Wrong Position

This may occur if GNOME Shell's internal thumbnail structure changes. The extension includes error handling for this, but you may need to wait for an update if a major GNOME version changes the API.

## Disabling

To disable the extension:

```bash
gnome-extensions disable workspace-names-overview@fedora-desktop
```

All labels will be automatically removed when the extension is disabled.

## Uninstalling

```bash
rm -rf ~/.local/share/gnome-shell/extensions/workspace-names-overview@fedora-desktop
```

Then restart GNOME Shell.

## Technical Details

### How It Works

1. **Initialization:** On enable, the extension connects to:
   - `Main.overview` 'showing' signal - to add labels when Overview opens
   - GSettings 'changed::workspace-names' signal - to update labels when names change

2. **Label Creation:** When Overview opens:
   - Reads workspace names from `org.gnome.desktop.wm.preferences.workspace-names`
   - Accesses workspace thumbnails via `Main.overview._overview._controls._thumbnailsBox._thumbnails`
   - Creates `St.Label` widget for each thumbnail
   - Adds label as child to thumbnail actor

3. **Cleanup:** On disable:
   - Disconnects all signals
   - Removes all label widgets
   - Destroys label objects
   - Clears internal references

### Files

- `extension.js` - Main extension logic
- `metadata.json` - Extension metadata (name, version, compatibility)
- `stylesheet.css` - Label styling (colors, fonts, padding)
- `README.md` - This documentation

### Dependencies

No external dependencies. Uses standard GNOME Shell APIs:
- `St` (Shell Toolkit) - for Label widgets
- `Gio` - for GSettings access
- `Main` - for Overview access

## Development

### Testing Changes

After modifying the extension:

1. **Run ESLint** to check for blocking operations:
   ```bash
   cd /workspace/extensions && npm run lint
   ```

2. **Deploy via playbook:**
   ```bash
   ansible-playbook /workspace/playbooks/imports/optional/common/play-gnome-shell-extensions.yml
   ```

3. **Restart GNOME Shell** and test

### Code Style

This extension follows the project's code quality standards:
- No synchronous/blocking operations (enforced by ESLint)
- Proper cleanup in disable() method
- Error handling for GNOME API changes
- No memory leaks

## License

Part of the fedora-desktop project: https://github.com/LongTermSupport/fedora-desktop

## See Also

- [Space Bar Extension](https://extensions.gnome.org/extension/5090/space-bar/) - Shows workspace names in top panel
- [GNOME Shell Extensions Guide](https://gjs.guide/extensions/) - Developer documentation
