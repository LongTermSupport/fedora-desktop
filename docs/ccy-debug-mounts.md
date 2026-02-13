# CCY Debug Mounts - Read-Only Host Access

## Problem

When debugging speech-to-text from within CCY container, we can't access:
- Debug logs: `~/.local/share/speech-to-text/debug.log`
- Deployed scripts: `~/.local/bin/wsi-stream*`
- Configuration: `~/.config/speech-to-text/`

## Solution: Manual Container Launch with Extra Mounts

Since CCY script doesn't support a `--mount` flag yet, we need to manually launch the container with extra read-only mounts.

### Option 1: One-Line podman Command (Quickest)

Exit CCY and run this from your project directory:

```bash
cd ~/Projects/fedora-desktop

podman run -it --rm \
  --name claude-code-fedora-desktop \
  -v "$PWD:/workspace" \
  -v "$HOME/.local/share/speech-to-text:/host-debug-logs:ro" \
  -v "$HOME/.local/bin:/host-bin:ro" \
  -v "$HOME/.config/speech-to-text:/host-config:ro" \
  -e "CLAUDE_CODE_OAUTH_TOKEN=$(cat ~/.config/claude-code/oauth_token 2>/dev/null || echo '')" \
  -w /workspace \
  claude-yolo:fedora-desktop \
  claude --dangerously-skip-permissions
```

Then in container:
```bash
# Watch debug logs in real-time
tail -f /host-debug-logs/debug.log

# Read deployed script
cat /host-bin/wsi-stream

# Check configuration
ls -la /host-config/
```

### Option 2: Wrapper Script (Reusable)

The project includes `scripts/desktop-symlinks` which documents the approach, but currently requires CCY modification.

**To use later** (after CCY enhancement):
```bash
./scripts/desktop-symlinks
```

### Option 3: Modify CCY (Permanent Solution)

Add support for `--mount` flag to CCY script:

1. Edit `/var/local/claude-yolo/claude-yolo`
2. Add mount flag parsing
3. Bump CCY_VERSION
4. Redeploy via Ansible

**This is future work** - use Option 1 for now.

## Mount Points

The custom Dockerfile creates these mount points:
- `/host-debug-logs/` - Speech-to-text debug logs
- `/host-bin/` - User binaries (~/.local/bin/)
- `/host-config/` - Speech-to-text configuration

## Security

All mounts are **read-only** (`:ro` flag), ensuring you cannot accidentally modify host files from within the container.

## Current Status

- ✅ Custom Dockerfile has mount points
- ⬜ CCY wrapper script needs CCY modification
- ✅ Manual podman command works now

Use the manual podman command until we enhance CCY with `--mount` support.
