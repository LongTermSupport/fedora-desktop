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
4. **Disables recently-used file history** - Stops the `recently-used.xbel` stat storm (see below)
5. **Optionally Disables Thumbnails** - Removes thumbnail generation overhead

### Why the recent-files history matters

`~/.local/share/recently-used.xbel` grows without bound across *every* GTK application, and
Nautilus `stat()`s each entry at startup. When entries point at FUSE-backed remote mounts
(rclone, sshfs, gvfs) each `stat()` can block on network I/O, so the accumulated cost adds
multi-second startup delays.

The play sets the GNOME-native toggle `remember-recent-files=false` (Settings → Privacy → File
History), which stops GTK apps writing to the file and hides Nautilus's Recent tab. It then
deletes the existing file **once** — turning the toggle off does not truncate the hundreds of
KB already accumulated.

## Installation

### Basic Installation (Recommended)

```bash
ansible-playbook playbooks/imports/optional/common/play-fast-file-manager.yml
```

This applies:

- ✓ PCManFM installation
- ✓ GTK portal configuration
- ✓ GSK_RENDERER=ngl fix
- ✓ Recently-used file history disabled, and `recently-used.xbel` removed
- ✓ Thumbnails left enabled (useful for images/videos)

### Customization (Host-Level Configuration)

The playbook has sensible defaults, but you can override them in your host configuration:

```bash
# Edit your host variables (RECOMMENDED - survives playbook updates)
vim environment/localhost/host_vars/localhost.yml

# Add these variables to customise behaviour:
fast_file_manager_disable_thumbnails: true    # Disable for max performance
fast_file_manager_apply_gsk_fix: true         # Usually leave enabled
```

**The play exposes exactly two variables.** Both are read at
`playbooks/imports/optional/common/play-fast-file-manager.yml`, in its `vars:` block:

| Variable                               | Default | Effect when `true`                                  |
| -------------------------------------- | ------- | --------------------------------------------------- |
| `fast_file_manager_disable_thumbnails` | `false` | `show-image-thumbnails=never` — no previews, faster |
| `fast_file_manager_apply_gsk_fix`      | `true`  | Writes `GSK_RENDERER=ngl` into `/etc/environment`   |

Everything else the play does is unconditional: PCManFM, the portal config, and the
recently-used-history changes are always applied.

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

- `/etc/environment` - Added `GSK_RENDERER=ngl` (if `apply_gsk_fix`)
- `~/.config/xdg-desktop-portal/portals.conf` - Portal configuration
- `~/.local/share/recently-used.xbel` - **Deleted** (one-shot cleanup)
- dconf `/org/gnome/desktop/privacy/remember-recent-files` - Set to `false`
- Desktop MIME associations - PCManFM set as default file manager

### Services Affected

- `xdg-desktop-portal.service` - Restarted
- `xdg-desktop-portal-gnome.service` - Restarted

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

### Re-enable Recent Files History

```bash
gsettings set org.gnome.desktop.privacy remember-recent-files true
```

Nautilus's Recent tab returns and GTK apps start writing `recently-used.xbel` again. The
deleted history is not recoverable — the file rebuilds from new activity only.

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

- File picker open: **Instant** (\<100ms)
- PCManFM startup: **Instant** (\<100ms)
- Folder with 1000 images: **Instant** (if thumbnails disabled)

## Background and References

### Why Is GNOME File Picker Slow?

1. **GNOME 47 Change** - Portal switched from GTK to Nautilus-based picker
2. **Nautilus Performance** - General Nautilus slowness affects portal
3. **Fedora 41/42 GSK Bug** - GTK4 renderer issue causes app startup delays
4. **Recent-files stat storm** - Nautilus `stat()`s every `recently-used.xbel` entry at startup;
   entries on FUSE-backed remote mounts turn each one into blocking network I/O
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

With Fedora 44 shipping GNOME 50, the native GNOME portal may now be fast enough to switch back — re-test before keeping this workaround.

## Contributing

If you find additional optimizations or encounter issues, please:

1. Test the fix manually
2. Update this playbook
3. Document in this file
4. Submit pull request

## License

Same as fedora-desktop project.
