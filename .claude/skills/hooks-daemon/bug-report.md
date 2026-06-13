# Bug Report Generator

Generate a comprehensive bug report with full system diagnostics for triage.

## Usage

```bash
/hooks-daemon bug-report "description of the issue"
/hooks-daemon bug-report "plan race condition" -o /tmp/report.md
```

## What's Included

The bug report collects:

1. **Daemon Version** - Version, git commit, install mode
2. **System Info** - OS, kernel, architecture, Python version, hostname
3. **Daemon Status** - Running/stopped, PID, uptime, request stats
4. **Configuration** - Full hooks-daemon.yaml contents
5. **Loaded Handlers** - Count by event type, list with priorities
6. **Recent Logs** - Last 100 log entries (errors/warnings highlighted)
7. **Environment** - Relevant environment variables
8. **Bug Description** - User's description for context
9. **Health Summary** - Pass/fail checklist of all diagnostics

## Options

- `description` (required) - Brief description of the bug
- `-o, --output PATH` - Output file path. Default: auto-generated in `untracked/bug-reports/`
- `-o -` - Print to stdout instead of file

## When to Use

- Reporting bugs to maintainers
- Troubleshooting daemon issues
- Capturing diagnostic state before attempting fixes
- Providing context for GitHub issues
