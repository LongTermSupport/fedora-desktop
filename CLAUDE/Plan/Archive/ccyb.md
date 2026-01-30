# ccyb (Claude Code Browser) Debugging - Current Status

## Overview

**ccyb** (ccy-browser / claude-yolo-browser) is a wrapper that runs Claude Code inside a distrobox container for browser automation using MCP (Model Context Protocol).

**Purpose:** Enable Claude to interactively control browsers for acceptance testing, debugging, and web scraping.

## Current Problem

### Error Message
```
Error: crun: chdir to `/run/host/home/<username>/Projects/<project>`: No such file or directory:
OCI runtime attempted to invoke a command that was not found
```

### Context
- User ran the playbook: `ansible-playbook playbooks/imports/optional/common/play-distrobox-playwright.yml`
- Container appears to be created
- ccyb script launches but fails when entering the container
- The container cannot access `/home/{{ user_login }}/Projects/<project>` directory

### Root Cause Hypothesis
The container was created with `--volume ~/Projects:~/Projects` flag, but:
1. Either the volume mount didn't actually get created
2. Or the mount path is wrong
3. Or the container needs to be fully removed and recreated from scratch

## Architecture

### Component Locations
- **Main wrapper script:** [`files/var/local/claude-yolo/claude-yolo-browser`](../../files/var/local/claude-yolo/claude-yolo-browser)
- **Shared library:** [`files/var/local/claude-yolo/lib/common.bash`](../../files/var/local/claude-yolo/lib/common.bash)
- **Container playbook:** [`playbooks/imports/optional/common/play-distrobox-playwright.yml`](../../playbooks/imports/optional/common/play-distrobox-playwright.yml)

### Container Configuration
- **Container name:** `playwright-tests`
- **Base image:** `ubuntu:22.04`
- **Isolated home:** `/var/local/claude-yolo/ccyb/home`
- **Required mount:** `/home/{{ user_login }}/Projects:/home/{{ user_login }}/Projects` (THIS IS THE ISSUE)

### How It Works
1. User runs `ccyb` from a directory inside `~/Projects/`
2. Script validates:
   - Must be in git repo
   - Must be within `~/Projects/` tree
   - `~/Projects` must exist
3. Script enters distrobox with: `distrobox enter playwright-tests -- bash -c "$CLAUDE_CMD"`
4. Container should have Projects mounted and accessible

## What We've Tried

### Attempt 1: Mount at container entry (FAILED)
- **Issue:** Used `--additional-flags "--volume $CURRENT_DIR:$CURRENT_DIR"`
- **Error:** `podman exec` doesn't support `--volume` flag
- **Commit:** 24d8b21

### Attempt 2: Fix preflight checks with cd / (FAILED)
- **Issue:** Preflight checks failing because distrobox preserves CWD
- **Fix:** Added `cd /` before checks
- **Result:** Didn't actually work because CWD preservation happens before command runs
- **Commit:** 5547066

### Attempt 3: Use --no-workdir flag (PARTIAL)
- **Fix:** Use `--no-workdir` flag to skip CWD preservation in preflight checks
- **Result:** Preflight checks now pass, but launch still fails
- **Commit:** 0558a43

### Attempt 4: Mount Projects at container creation (CURRENT)
- **Change:** Added `--volume /home/{{ user_login }}/Projects:/home/{{ user_login }}/Projects` to `distrobox create`
- **Location:** [`playbooks/imports/optional/common/play-distrobox-playwright.yml:57`](../../playbooks/imports/optional/common/play-distrobox-playwright.yml#L57)
- **Result:** Playbook runs successfully, but container still can't access Projects
- **Commits:** f9d442c, 5482ece

### Attempt 5: Add fail-fast validation (CURRENT)
- **Change:** Added validation that `~/Projects` exists in both playbook and script
- **Result:** Validation passes, but container mount still doesn't work
- **Commit:** 5482ece

## Key Files and Line Numbers

### Container Creation (THE PROBLEM AREA)
[`playbooks/imports/optional/common/play-distrobox-playwright.yml:51-59`](../../playbooks/imports/optional/common/play-distrobox-playwright.yml#L51-L59)
```yaml
- name: Create Playwright Distrobox Container with Isolated Home
  command: >
    distrobox create
    --name {{ distrobox_name }}
    --image {{ distrobox_image }}
    --home {{ ccyb_home }}
    --volume /home/{{ user_login }}/Projects:/home/{{ user_login }}/Projects
    --yes
  register: container_created
```

### Container Entry (WHERE ERROR OCCURS)
[`files/var/local/claude-yolo/claude-yolo-browser:494-496`](../../files/var/local/claude-yolo/claude-yolo-browser#L494-L496)
```bash
# Enter distrobox and run Claude Code
# Projects directory is mounted at container creation time
exec distrobox enter "$DISTROBOX_NAME" -- bash -c "$CLAUDE_CMD"
```

### Projects Validation
[`files/var/local/claude-yolo/claude-yolo-browser:183-206`](../../files/var/local/claude-yolo/claude-yolo-browser#L183-L206)
```bash
# Validate we're inside ~/Projects (required for container mount)
PROJECTS_DIR="$HOME/Projects"
if [ ! -d "$PROJECTS_DIR" ]; then
    print_error "Projects directory does not exist: $PROJECTS_DIR"
    exit 1
fi

if [[ "$CURRENT_DIR" != "$PROJECTS_DIR"* ]]; then
    print_error "Must run ccyb from within ~/Projects directory"
    exit 1
fi
```

## Version History

- **v3.1.1** - Added CWD volume mount (broken approach)
- **v3.1.2** - Fixed preflight checks with cd /
- **v3.1.3** - Fixed preflight checks with --no-workdir
- **v3.2.0** - Projects mount at creation + YOLO colors fix
- **v3.2.1** - Fail-fast validation for Projects directory

## What Needs Investigation

### Critical Questions
1. **Does the container actually have the volume mount?**
   - Run: `podman inspect playwright-tests | grep -A 10 Mounts`
   - Expected: Should see `~/Projects` in mount list

2. **Was the container actually recreated?**
   - The playbook has a task to remove old container (line 46-49)
   - Check: `podman ps -a | grep playwright-tests` - verify creation timestamp

3. **Is the distrobox create command correct?**
   - The `--volume` flag might not work with distrobox + isolated home
   - May need `--additional-flags` at creation time instead

4. **Does distrobox support volumes with --home flag?**
   - Research needed: Can distrobox use `--home` AND `--volume` together?
   - Alternative: Use `--additional-flags "--volume ..."`

### Debug Commands to Run

```bash
# 1. Check container mounts
podman inspect playwright-tests --format '{{range .Mounts}}{{.Source}}:{{.Destination}} {{end}}'

# 2. Check container creation command
podman inspect playwright-tests --format '{{.Config.CreateCommand}}'

# 3. List all distrobox containers
distrobox list

# 4. Try entering container and checking mounts
distrobox enter playwright-tests --no-workdir -- mount | grep Projects

# 5. Check if Projects is visible inside container
distrobox enter playwright-tests --no-workdir -- ls -la ~/

# 6. Manually test distrobox create with volume
distrobox create --name test-mount --image ubuntu:22.04 \
  --home /tmp/test-home \
  --volume ~/Projects:~/Projects \
  --yes

# 7. Check distrobox version (might be a bug)
distrobox --version

# 8. Read distrobox logs
journalctl --user -u podman | tail -50
```

## Potential Solutions

### Solution A: Use --additional-flags at creation
```yaml
- name: Create Playwright Distrobox Container with Isolated Home
  command: >
    distrobox create
    --name {{ distrobox_name }}
    --image {{ distrobox_image }}
    --home {{ ccyb_home }}
    --additional-flags "--volume /home/{{ user_login }}/Projects:/home/{{ user_login }}/Projects"
    --yes
```

### Solution B: Mount after creation
```bash
# Recreate container without --home, use default home
# Then mount Projects manually via podman
```

### Solution C: Don't use isolated home
```yaml
# Remove --home flag entirely
# Let distrobox share host home (security trade-off)
# Projects will be accessible automatically
```

### Solution D: Use --init false
```yaml
# Some distrobox versions have issues with init + volumes
distrobox create ... --init false
```

## Recent Commits

- `5482ece` - Add fail-fast validation for ~/Projects directory
- `f9d442c` - Fix ccyb Projects directory mounting and YOLO warning colors
- `a53f485` - Add distrobox debugging commands to troubleshooting docs
- `0558a43` - Fix ccyb preflight with --no-workdir flag
- `5547066` - Fix ccyb preflight checks for isolated container home
- `24d8b21` - Fix ccyb to mount current working directory

## Next Steps

1. **Verify container state** - Check if volume mount exists in container config
2. **Test manual recreation** - Try creating test container with volume + isolated home
3. **Check distrobox docs** - Research if `--volume` + `--home` are compatible
4. **Consider alternatives** - May need to rethink isolated home approach
5. **Debug with podman directly** - Bypass distrobox to test podman volume mounting

## Success Criteria

ccyb should:
- ✓ Launch without errors
- ✓ Access project files in ~/Projects/
- ✓ Display system prompt about browser capabilities
- ✓ Show available MCP tools (Playwright)
- ✓ Be able to control browser and navigate websites

## Test Command

```bash
cd ~/Projects/front/acceptance-tests
ccyb "list your available MCP tools"
```

Expected: Should launch Claude Code and show Playwright MCP tools available.
