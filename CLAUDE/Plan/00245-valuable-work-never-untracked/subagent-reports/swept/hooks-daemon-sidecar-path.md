# Bug report: context-sidecar path mismatch — RESOLVED in daemon v3.34.1

**Status:** ✅ FIXED upstream in claude-code-hooks-daemon **v3.34.1** (originally reported against v3.33.0).
**Component:** claude-code-hooks-daemon — PTY supervisor (Plan 00135) vs status_line context sidecar.
**Reported from:** `LongTermSupport/fedora-desktop#31` (CCY supervisor, dry-run).
**Fix landed:** refreshed `claude-supervise.py` committed in fedora-desktop `0d69536`.

---

## The bug (v3.33.0)

In a **normal client install** the daemon and supervisor disagreed on the sidecar directory, so the supervisor's compact-trigger loop was permanently inert (armed or dry-run), failing silently.

- **Daemon writes** (correct — runtime artifacts belong here): `daemon_untracked_dir()/context-sidecar` → `{project}/.claude/hooks-daemon/untracked/context-sidecar` in normal installs.
- **Supervisor read** (`claude-supervise.py:_default_sidecar_dir`, v3.33.0): hardcoded `$CLAUDE_PROJECT_DIR/untracked/context-sidecar` — the **self-install** layout only.

The two only coincided in self-install mode (the daemon's own repo), which is why it was never caught in dogfooding. The daemon's write location was correct; the defect was entirely on the supervisor's read side.

## The fix (v3.34.1)

`_default_sidecar_dir()` is now **install-mode-aware** — self-install iff `{project}/src/claude_code_hooks_daemon` exists, else normal:

```python
project_dir = Path(os.environ.get("CLAUDE_PROJECT_DIR") or Path.cwd())
self_install = (project_dir / "src" / "claude_code_hooks_daemon").exists()
daemon_untracked = project_dir / "untracked" if self_install \
    else project_dir / ".claude" / "hooks-daemon" / "untracked"
return daemon_untracked / _SIDECAR_SUBDIR
```

The supervisor now reads the daemon's real write dir in both modes — matching the reported "read-side only, never move the write" recommendation. Stdlib-only, no daemon import.

## Verify after next launch

The fix is in the tracked/deployed `claude-supervise.py`, but the supervisor process wrapping the **current** session is the pre-fix binary (started at container launch). On the **next `ccy` launch**, confirm:

```
grep polling <repo>/untracked/supervise/decision.log
# expect: polling <repo>/.claude/hooks-daemon/untracked/context-sidecar
```

That path must match where the daemon writes (`ls <repo>/.claude/hooks-daemon/untracked/context-sidecar/*.json`).
