# Claude DevTools (ccdt)

Visualise Claude Code session logs with a web UI. The `ccdt` command launches
[claude-devtools](https://github.com/matt1398/claude-devtools) as an on-demand Podman container,
restoring the detailed tool-output visibility that recent Claude Code updates replaced with opaque
summaries such as "Read 3 files".

---

## Overview

Claude Code's recent updates removed detailed tool output from the UI, replacing it with brief
summaries. claude-devtools reconstructs and visualises the full session log by reading the raw
`.claude/` session files on disk — no network access required.

`ccdt` wraps the tool as an on-demand Podman container. It starts instantly (image pre-built at
install time), opens a web UI at `http://localhost:3456`, and cleans up completely on exit.

### Session Types

`ccdt` supports two distinct session locations:

| Session type | Host filesystem path | When used |
|---|---|---|
| Host Claude Code | `~/.claude/` | Regular `claude` / `claude-code` sessions |
| CCY project sessions | `<project-dir>/.claude/ccy/` | Sessions run inside the CCY container |

The command auto-detects which type applies based on your current directory.

### Why On-Demand vs Persistent Service?

Sessions are plain files on disk — there is nothing to "watch" at idle. Launching on demand means:

- Zero resource usage when not viewing sessions
- The correct `CLAUDE_ROOT` is passed at launch time (no restart needed to switch projects)
- Clean container teardown on Ctrl+C (no dangling processes)

---

## Installation

### Prerequisites

- Podman installed (`ansible-playbook playbooks/imports/play-podman.yml`)
- `~/.bashrc-includes/` directory (created by main playbook)
- Internet access to clone `https://github.com/matt1398/claude-devtools` during install

### Deploy with Ansible

```bash
ansible-playbook playbooks/imports/optional/common/play-install-claude-devtools.yml
```

The playbook:
1. Clones `https://github.com/matt1398/claude-devtools` to `/opt/claude-devtools/`
2. Builds the container image locally as `claude-devtools:latest`
3. Installs `~/.local/bin/ccdt` (the wrapper script)
4. Installs `~/.bashrc-includes/claude-devtools.bash` (shell alias)

After deployment:

```bash
source ~/.bashrc
ccdt --help
```

---

## Usage

### Auto-Detection (recommended)

Run `ccdt` with no arguments from anywhere. It walks up the directory tree looking for
`.claude/ccy/` (a CCY project). If found, it uses that. Otherwise it falls back to `~/.claude`
(host sessions).

```bash
# From inside a CCY project → shows project sessions
cd ~/Projects/my-project
ccdt

# From anywhere else → shows host Claude Code sessions
cd ~
ccdt
```

### Force Host Sessions

Use `--host` to always view host sessions regardless of your current directory:

```bash
ccdt --host
```

Useful when you are inside a CCY project directory but want to review host sessions.

### Explicit Path

Pass a path directly. `ccdt` checks for `.claude/ccy/` within it first, then `.claude/`:

```bash
# View sessions for a specific project
ccdt ~/Projects/my-project

# View host sessions by path
ccdt ~/.claude
```

### Error Cases

```bash
# Non-existent path → clear error, non-zero exit
ccdt /nonexistent/path
# ERROR: Path does not exist: /nonexistent/path
```

### Help

```bash
ccdt --help
ccdt-help        # alias
```

---

## Web UI

Once running, open `http://localhost:3456` in your browser.

The web UI shows:
- Session list with timestamps
- Full tool call detail (the detail Claude Code's UI now hides)
- Tool inputs and outputs
- File reads, writes, bash commands, and their results

Press **Ctrl+C** in the terminal to stop. The container is removed automatically (`--rm`).

---

## How It Works

`ccdt` runs:

```bash
podman run \
  --rm \
  -p 3456:3456 \
  -v <CLAUDE_ROOT>:/data/.claude:ro \
  -e CLAUDE_ROOT=/data/.claude \
  claude-devtools
```

Key points:
- **Read-only mount** (`:ro`) — claude-devtools cannot modify your session files
- **No network required at runtime** — reads files directly from disk
- **Port 3456** — the claude-devtools default; must be free before running

---

## Troubleshooting

### Port 3456 Already in Use

Check what is using the port:

```bash
ss -tlnp | grep 3456
```

Stop the conflicting process, then retry `ccdt`.

### Image Not Found

If the container image was not built during installation:

```bash
ansible-playbook playbooks/imports/optional/common/play-install-claude-devtools.yml
```

This re-clones the repo (or pulls updates) and rebuilds the image.

### No Sessions Shown

Verify the session directory is correct and non-empty:

```bash
# For host sessions
ls ~/.claude/projects/

# For CCY project sessions
ls <project-dir>/.claude/ccy/projects/
```

If the directory is empty, no sessions have been saved yet. Run Claude Code in the project first.

### CCY Sessions Not Detected

Ensure you are running `ccdt` from within the project directory (or a subdirectory), not from
an unrelated location:

```bash
cd ~/Projects/my-project
ccdt    # should auto-detect .claude/ccy/
```

Or pass the path explicitly:

```bash
ccdt ~/Projects/my-project
```

---

## Updating

To update to the latest claude-devtools:

```bash
# On host system (not in CCY container)
ansible-playbook playbooks/imports/optional/common/play-install-claude-devtools.yml
```

The playbook pulls the latest source and rebuilds the image.

---

## Related

- [CCY/CCB Guide](../containerization.md) — containerised Claude Code
- [Playbooks Reference](../playbooks.md)
- [claude-devtools upstream](https://github.com/matt1398/claude-devtools)
