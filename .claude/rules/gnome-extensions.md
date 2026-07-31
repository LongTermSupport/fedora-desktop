---
paths:
  - "extensions/**"
  - "files/home/.local/share/gnome-shell/**"
---

# You are editing a GNOME Shell extension

Full guide: [CLAUDE/GnomeShell.md](../../CLAUDE/GnomeShell.md) ·
scope + rules: [extensions/CLAUDE.md](../../extensions/CLAUDE.md)

## Lint after every change — via the binary, not npm

```bash
cd extensions && node_modules/.bin/eslint .
```

`npm run lint` is intercepted by the daemon's `npm_command` handler;
[CLAUDE/QA.md](../../CLAUDE/QA.md) specifies the binary form.

## Why this is stricter than normal JS

**A blocking operation freezes GNOME Shell completely**, and on Wayland the Shell *is* the
display server — there is no way to restart it without ending the session. A user may have
to hard-reboot. Everything that can block must be async.

## The traps

- **GNOME 45+ module syntax.** `import * as Main from 'resource:///org/gnome/shell/ui/main.js'`,
  not `imports.ui.main`.
- **Clean up everything in `disable()`** — disconnect signal handlers, `GLib.source_remove`
  timeouts, signal subprocesses. A leaked handler survives the extension.
- **Return the right event status.** `Clutter.EVENT_STOP` consumes; `EVENT_PROPAGATE` passes on.
  Getting it wrong leaks keystrokes into applications.
- **`metadata.json` must declare the GNOME major this branch's Fedora ships**
  (`vars/fedora-version.yml`). Checked statically by
  `python3 -m helpers.gnome.check_extension_compat`, which **fails on an unmapped Fedora
  version by design** — a human must confirm the GNOME major.

## Testing without logging out

`dbus-run-session -- gnome-shell --nested --wayland` (Alt+F1 for the overview inside it).
Real-session changes to `extension.js` need a full log out and back in on Wayland.
