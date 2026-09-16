# Bug Report Generator

Generate a comprehensive local diagnostic bundle: version, status, config,
handlers, recent logs and a health checklist.

> **This output is for the person who ran it. It is not a filing artefact.**
> It reproduces this project's configuration on purpose, because you are the
> reader — which is exactly why it must never be pasted into a public issue.
> To file a defect upstream, use this skill's `issue-report` args, whose generator
> collects a controlled field set and never gathers a config dump or logs at
> all.

## Usage

```claude-code
/hooks-daemon bug-report "description of the issue"
/hooks-daemon bug-report "plan race condition" -o untracked/scratch/report.md
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

- Troubleshooting a daemon issue yourself
- Capturing diagnostic state before attempting a fix, so you can compare after
- Working out WHETHER what you are seeing is a daemon defect at all

## When not to use

- **Filing upstream.** Use this skill's `issue-report` args instead. It drives the
  checks that establish there is a defect, then builds a body that carries none
  of the material this bundle deliberately includes.
