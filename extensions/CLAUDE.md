# GNOME Shell Extensions Development

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

## Development Workflow

1. **Make changes** to extension JavaScript
2. **Run ESLint** immediately:
   ```bash
   cd /workspace/extensions && npm run lint
   ```
3. **Fix any errors** before proceeding
4. **Deploy** via Ansible playbook
5. **Test** in GNOME Shell

## ESLint Auto-fix

For some issues, ESLint can auto-fix:
```bash
cd /workspace/extensions && npm run lint:fix
```

**Note:** Auto-fix won't fix blocking operations - those require manual refactoring to async patterns.

## Testing Extensions

After deployment, test thoroughly:
- Enable/disable extension to reload
- Check for errors: `journalctl -f /usr/bin/gnome-shell`
- Monitor logs: `tail -f ~/.local/share/speech-to-text/debug.log`

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
