# GNOME Shell Extensions Development

## ⚠️ WAYLAND: Extension Reload Requires Logout

**This system runs Wayland, NOT X11.**

On Wayland, GNOME Shell cannot be restarted without ending the session. This means:

- ❌ **`Alt+F2` → `r` does NOT work** (X11 only)
- ❌ **Disable/enable extension does NOT reload code** (only toggles existing loaded code)
- ✅ **Log out and log back in** is the ONLY way to reload extension JavaScript

**When extension code is updated:**
1. Deploy via Ansible playbook
2. **Inform the user clearly**: "You must log out and log back in for the extension changes to take effect."
3. Do not suggest any other reload method - they do not work on Wayland

## ⚠️ ARCHITECTURE: Keep Extension Code Thin

**Because reloading extensions requires logout, minimize code in extension.js.**

Extensions should be **thin wrappers** that:
- Handle GNOME Shell integration (panel indicators, keybindings, DBus signals)
- Launch external scripts for actual functionality
- Read state from files/DBus rather than computing it

**All business logic should live in external scripts** (Python, Bash) in `~/.local/bin/`:
- Scripts can be updated and take effect immediately
- No logout required for script changes
- Easier to test and debug outside GNOME Shell

**Example - GOOD architecture:**
```
extension.js:     Keybinding → spawns wsi-stream → listens for DBus signals → updates icon
wsi-stream:       All transcription logic, clipboard handling, notifications
```

**Example - BAD architecture:**
```
extension.js:     Keybinding → does transcription inline → handles clipboard → etc.
```

**Rule of thumb:** If you can move it to a script, move it to a script.

## Critical Safety Rules

### ⚠️ ALWAYS RUN ESLINT AFTER CHANGES

**MANDATORY**: After making ANY changes to extension JavaScript files, you MUST run ESLint:

```bash
cd /workspace/extensions && npm run lint
```

**Why this matters:**
- Blocking operations freeze GNOME Shell completely
- Users may need to hard reboot their machine
- ESLint catches dangerous patterns before deployment

### Dangerous Patterns (Blocked by ESLint)

❌ **NEVER use these - they will freeze GNOME Shell:**
```javascript
// WRONG - Blocks UI thread
proc.communicate(input, null);
proc.communicate_utf8(text, null);
proc.wait(null);
proc.wait_check(null);
```

✅ **ALWAYS use async versions:**
```javascript
// CORRECT - Non-blocking
proc.communicate_async(input, null, (proc, res) => {
    const [, stdout, stderr] = proc.communicate_finish(res);
    // Handle result
});

// Or use GLib.spawn_command_line_async for simple cases
GLib.spawn_command_line_async('echo hello | wl-copy');
```

## GNOME Shell API Documentation

### ⚠️ NEVER GUESS - ALWAYS RESEARCH

**CRITICAL**: When working with GNOME Shell APIs, you MUST research the actual source code. Guessing API paths leads to broken code.

### Local GNOME Shell Source (PREFERRED)

**Always use local source** - it matches the exact installed version.

**Extract JS source from installed GNOME Shell:**
```bash
./extensions/scripts/gnome-shell-extract-js.bash
```

This extracts to: `./untracked/gnome-shell/<version>/js-extracted/`

**Before working on extensions, verify source is current:**
```bash
# Check GNOME Shell version
gnome-shell --version

# Check if extracted source exists for this version
ls ./untracked/gnome-shell/
```

**If GNOME Shell was updated**, re-run the script - it automatically removes old versions.

**Read local source files:**
```bash
# Example: Read workspacesView.js
cat ./untracked/gnome-shell/48.7/js-extracted/org/gnome/shell/ui/workspacesView.js
```

### Online Sources (Fallback)

**GNOME GitLab** (Most up-to-date for development branch):
- **Browse source**: https://gitlab.gnome.org/GNOME/gnome-shell/-/tree/main/js/ui

**GitHub Mirror** (Easier for quick browsing):
- **Browse source**: https://github.com/GNOME/gnome-shell/tree/main/js/ui

### Key Files to Know

**For workspace-related APIs**:
- `js/ui/workspacesView.js` - Workspace thumbnails, multi-monitor displays
- `js/ui/workspace.js` - Individual workspace representation
- `js/ui/overviewControls.js` - Overview layout and controls
- `js/ui/main.js` - Global objects and initialization

**For panel and UI**:
- `js/ui/panel.js` - Top panel and indicators
- `js/ui/popupMenu.js` - Menu system
- `js/ui/panelMenu.js` - Panel menu buttons

**For system integration**:
- `js/misc/util.js` - Utility functions
- `js/ui/modalDialog.js` - Dialog system
- `js/ui/messageList.js` - Notification system

### Research Workflow

1. **Identify the feature** you need to interact with (e.g., "workspace thumbnails")

2. **Search the GNOME Shell source** on GitLab or GitHub:
   - Search for class names: `class WorkspaceThumbnail`
   - Search for property names: `_thumbnails`
   - Search for method names: `_addThumbnails`

3. **Read the actual source code** - don't guess based on similar-sounding properties:
   ```bash
   # Fetch and read the actual file
   curl -s https://raw.githubusercontent.com/GNOME/gnome-shell/main/js/ui/workspacesView.js | less
   ```

4. **Understand the object hierarchy**:
   - Look for `class ClassName extends ParentClass`
   - Find `this._propertyName` assignments in constructor
   - Trace through `_init()` and `enable()` methods

5. **Verify the API path** exists in your target GNOME Shell version

### Example: Multi-Monitor Workspace Thumbnails

**Wrong approach** (guessing):
```javascript
// ❌ WRONG - Guessed API path
const secondaryMonitor = controls._secondaryMonitorOverviews[i];
```

**Correct approach** (researched from source):
```javascript
// ✅ CORRECT - From workspacesView.js source code
// WorkspacesDisplay._workspacesViews contains:
// - Index 0: Primary monitor (not a SecondaryMonitorDisplay)
// - Index 1+: SecondaryMonitorDisplay instances
const views = controls._workspacesDisplay._workspacesViews;
for (let i = 1; i < views.length; i++) {
    const secondaryDisplay = views[i];  // This is a SecondaryMonitorDisplay
    if (secondaryDisplay._thumbnails) {  // SecondaryMonitorDisplay has _thumbnails
        // ...
    }
}
```

**Source**: https://github.com/GNOME/gnome-shell/blob/main/js/ui/workspacesView.js
- Lines with `class SecondaryMonitorDisplay` show it has `this._thumbnails` property
- Lines with `WorkspacesDisplay` show `this._workspacesViews` array structure

### GJS Documentation

**GJS (GNOME JavaScript bindings)**:
- **Official docs**: https://gjs.guide/
- **Extensions guide**: https://gjs.guide/extensions/
- **GI bindings**: https://gjs-docs.gnome.org/

**GTK and GLib APIs**:
- **St (Shell Toolkit)**: Documented in GNOME Shell source comments
- **Gio**: https://gjs-docs.gnome.org/gio20/
- **GLib**: https://gjs-docs.gnome.org/glib20/

### When APIs Change

GNOME Shell's internal APIs (anything with `_` prefix) are **not stable** and may change between versions:

- Always check the source for your target GNOME Shell version
- Use try/catch blocks for version-dependent code
- Test on actual GNOME Shell installation, not just in isolation

## Development Workflow

1. **Make changes** to extension JavaScript
2. **Run ESLint** immediately:
   ```bash
   cd /workspace/extensions && npm run lint
   ```
3. **Fix any errors** before proceeding
4. **Deploy** via Ansible playbook
5. **Inform user to log out and log back in** (extension.js changes only - script changes are immediate)
6. **Test** in GNOME Shell

## ESLint Auto-fix

For some issues, ESLint can auto-fix:
```bash
cd /workspace/extensions && npm run lint:fix
```

**Note:** Auto-fix won't fix blocking operations - those require manual refactoring to async patterns.

## Testing Extensions

After deployment:
1. **Log out and log back in** to reload extension code (required on Wayland)
2. Check for errors: `journalctl -f /usr/bin/gnome-shell`
3. Monitor logs: `tail -f ~/.local/share/speech-to-text/debug.log`

**Remember:** Script changes (wsi, wsi-stream) take effect immediately. Extension.js changes require logout.

## Extension Structure

```
extensions/
├── CLAUDE.md                    # This file
├── .eslintrc.json              # ESLint config (blocking ops detection)
├── package.json                # npm scripts for linting
└── speech-to-text@fedora-desktop/
    ├── extension.js            # Main extension code
    ├── metadata.json           # Extension metadata
    ├── wsi                     # Bash script (not linted)
    └── schemas/                # GSettings schemas
```

## Common Mistakes

### 1. Using Synchronous File Operations
```javascript
// WRONG - May block on slow filesystems
const [success, contents] = file.load_contents(null);

// BETTER - Check file size first, or use async
if (file.query_info('standard::size', 0, null).get_size() < 1024) {
    const [success, contents] = file.load_contents(null);
}
```

### 2. Spawning Processes Without Async
```javascript
// WRONG - Can block if process takes time
const proc = Gio.Subprocess.new(['long-command'], 0);
proc.wait(null);

// CORRECT - Use async spawn
GLib.spawn_command_line_async('long-command');
```

### 3. Network Operations in Main Thread
```javascript
// WRONG - Network delays freeze UI
const response = httpClient.send(request, null);

// CORRECT - Use async network operations
httpClient.send_async(request, null, (client, res) => {
    const response = client.send_finish(res);
});
```

## Remember

**If in doubt, run ESLint. It's there to save you from hard reboots.**
