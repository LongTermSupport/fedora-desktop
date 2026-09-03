---
paths:
  - "extensions/**"
  - "files/home/.local/share/gnome-shell/**"
---

# You are editing a GNOME Shell extension

A blocking call freezes GNOME Shell, and on Wayland the Shell is the display server, so everything that can block must be async. Lint after every change via the ESLint binary, not `npm run`.

- [extensions/CLAUDE.md](../../extensions/CLAUDE.md) — the ESLint command, the Wayland logout requirement, thin-extension architecture, the blocking patterns
- [CLAUDE/GnomeShell.md](../../CLAUDE/GnomeShell.md) — GNOME 45+ imports, cleanup in `disable()`, event status, `metadata.json`, nested-shell testing
- [CLAUDE/QA.md](../../CLAUDE/QA.md) — the ESLint requirement and the extension version-compatibility gate
