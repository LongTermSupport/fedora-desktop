# Fast File Manager Configuration

## Overview

This playbook addresses the slow GNOME file picker issue that causes 1-2 second delays when opening file dialogs in Chrome, Firefox, and other applications.

## The Problem

Starting with GNOME 47 (Fedora 42), `xdg-desktop-portal-gnome` uses Nautilus for file picking instead of the traditional GTK file chooser. Combined with general Nautilus performance issues, this causes noticeable delays:

- **1-2 second delays** opening file picker in browsers
- **Progressive slowdown** - each subsequent dialog slower
- **Overall Nautilus sluggishness** affecting file operations

## The Solution

This playbook implements multiple optimizations:

1. **Installs PCManFM** - Lightweight, fast GTK file manager as Nautilus alternative
2. **Configures GTK Portal** - Forces use of faster GTK file chooser in browsers
3. **Applies GSK_RENDERER Fix** - Fixes Fedora 41/42 GTK4 app startup delays
4. **Optionally Disables Tracker** - Stops GNOME indexing service that slows file operations
5. **Optionally Disables Thumbnails** - Removes thumbnail generation overhead

## Installation

### Basic Installation (Recommended)

```bash
ansible-playbook playbooks/imports/optional/common/play-fast-file-manager.yml
```

This applies:
- ✓ PCManFM installation
- ✓ GTK portal configuration
- ✓ GSK_RENDERER=ngl fix
- ✓ Tracker disabled (improves performance, rarely used)
- ✓ Thumbnails enabled (useful for images/videos)

### Customization (Host-Level Configuration)

The playbook has sensible defaults, but you can override them in your host configuration:

```bash
# Edit your host variables (RECOMMENDED - survives playbook updates)
vim environment/localhost/host_vars/localhost.yml

# Add these variables to customize behavior:
fast_file_manager_disable_tracker: false      # Keep tracker if you use GNOME search
fast_file_manager_disable_thumbnails: true    # Disable for max performance
fast_file_manager_apply_gsk_fix: true        # Usually leave enabled
```

**Default values** (if not overridden in host_vars):
- `disable_tracker: true` - Disabled by default (rarely used, slows file ops)
- `disable_thumbnails: false` - Enabled by default (useful for images/videos)
- `apply_gsk_fix: true` - Enabled by default (fixes Fedora 41/42 slowness)

⚠️ **Configuration trade-offs:**
- `disable_tracker: false` - Keeps GNOME Activities file search, but slows file operations
- `disable_thumbnails: true` - No image previews, but faster folder browsing

## Activation

**IMPORTANT:** Changes require logout/login to take full effect:

```bash
# 1. Log out and log back in (for GSK_RENDERER environment variable)
# 2. Restart browsers:
killall chrome firefox

# 3. Test file picker in Chrome - should be instant!
```

## What Changed

### System Files Modified

- `/etc/environment` - Added `GSK_RENDERER=ngl`
- `~/.config/xdg-desktop-portal/portals.conf` - Portal configuration
- Desktop MIME associations - PCManFM set as default file manager

### Services Affected

- `xdg-desktop-portal.service` - Restarted
- `xdg-desktop-portal-gnome.service` - Restarted
- `tracker-*.service` - Stopped/masked (if `disable_tracker: true`)

### Packages Installed

- `pcmanfm` - ~2MB, minimal dependencies
- `xdg-desktop-portal-gtk` - Usually already installed

## Testing

### Test File Picker Performance

1. Open Chrome/Firefox
2. Go to any upload dialog (e.g., Gmail attachment)
3. Click "Choose File"
4. **Should open instantly** (not 1-2 seconds)

### Test PCManFM

```bash
# Open PCManFM
pcmanfm

# Or click any folder - should open with PCManFM now
```

### Verify Portal Configuration

```bash
# Check portal config
cat ~/.config/xdg-desktop-portal/portals.conf

# Should show:
# [preferred]
# default=gnome
# org.freedesktop.impl.portal.FileChooser=gtk

# Check portal services
systemctl --user status xdg-desktop-portal-gtk.service
```

## Reverting Changes

### Restore GNOME File Picker

```bash
# Edit portal config
vim ~/.config/xdg-desktop-portal/portals.conf

# Change:
org.freedesktop.impl.portal.FileChooser=gtk
# To:
org.freedesktop.impl.portal.FileChooser=gnome

# Restart portal
systemctl --user restart xdg-desktop-portal.service
systemctl --user restart xdg-desktop-portal-gnome.service
```

### Restore Nautilus as Default

```bash
xdg-mime default org.gnome.Nautilus.desktop inode/directory
```

### Re-enable Tracker

```bash
systemctl --user unmask tracker-extract-3.service \
    tracker-miner-fs-3.service \
    tracker-miner-rss-3.service \
    tracker-writeback-3.service \
    tracker-xdg-portal-3.service \
    tracker-miner-fs-control-3.service

systemctl --user start tracker-miner-fs-3.service
```

### Remove GSK_RENDERER Fix

```bash
sudo vim /etc/environment
# Remove the GSK_RENDERER=ngl line
# Log out and back in
```

## Troubleshooting

### File Picker Still Slow

1. **Verify portal config:**
   ```bash
   cat ~/.config/xdg-desktop-portal/portals.conf
   ```

2. **Check GTK portal is running:**
   ```bash
   systemctl --user status xdg-desktop-portal-gtk.service
   ```

3. **Restart everything:**
   ```bash
   systemctl --user restart xdg-desktop-portal.service
   systemctl --user restart xdg-desktop-portal-gnome.service
   killall chrome firefox
   ```

4. **Did you log out/in?** GSK_RENDERER requires new session

### Screen Sharing Broken

If screen sharing stops working, you may need GNOME portal for that:

```bash
# Edit portal config
vim ~/.config/xdg-desktop-portal/portals.conf

# Change to:
[preferred]
default=gnome
org.freedesktop.impl.portal.FileChooser=gtk
org.freedesktop.impl.portal.ScreenCast=gnome
```

### PCManFM Doesn't Match GNOME Theme

```bash
# Install GTK theme support
sudo dnf install gnome-themes-extra

# PCManFM should now follow GNOME theme
```

## Performance Expectations

### Before Optimization

- File picker open: **1-2 seconds**
- Nautilus startup: **2-5 seconds**
- Folder with 1000 images: **5-10 seconds** to load thumbnails

### After Optimization

- File picker open: **Instant** (<100ms)
- PCManFM startup: **Instant** (<100ms)
- Folder with 1000 images: **Instant** (if thumbnails disabled)

## Background and References

### Why Is GNOME File Picker Slow?

1. **GNOME 47 Change** - Portal switched from GTK to Nautilus-based picker
2. **Nautilus Performance** - General Nautilus slowness affects portal
3. **Fedora 41/42 GSK Bug** - GTK4 renderer issue causes app startup delays
4. **Tracker Overhead** - Continuous indexing slows file operations
5. **Thumbnail Generation** - On-the-fly thumbnail creation adds delay

### Related Issues

- [Bug #2018539 - File selector extremely slow (Ubuntu)](https://bugs.launchpad.net/bugs/2018539)
- [[FIX] Fedora 41 apps slow to load (Framework Community)](https://community.frame.work/t/fix-fedora-41-apps-slow-to-load/60612)
- [Planning FileChooser portal implementation with nautilus (GNOME Discourse)](https://discourse.gnome.org/t/planning-filechooser-portal-implementation-with-nautilus/20335)

### Alternative File Managers Considered

- **PCManFM** ✓ - Chosen for minimal dependencies, speed
- **Thunar** - Good but pulls XFCE dependencies
- **Nemo** - Feature-rich but heavier than PCManFM
- **Dolphin** - Excellent but requires KDE dependencies

## Future Improvements

**GNOME 50** (expected mid-2026) will include:
- **40% faster thumbnails** - New asynchronous loading
- Better Nautilus performance overall

If you're still on Fedora 42 when GNOME 50 releases, the native GNOME portal may become fast enough to switch back.

## Contributing

If you find additional optimizations or encounter issues, please:

1. Test the fix manually
2. Update this playbook
3. Document in this file
4. Submit pull request

## License

Same as fedora-desktop project.
