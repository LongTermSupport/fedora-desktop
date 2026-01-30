# ccb (Claude Code Browser) - Docker-based Browser Automation

## Strategic Pivot: Distrobox → Docker

**Date:** 2025-11-10
**Decision:** Abandoned ccyb (distrobox) in favor of ccb (Docker-based solution)

### Why Docker over Distrobox

**Docker Advantages:**
- ✅ **DRY Architecture** - Can share base images between `ccy` (general) and `ccb` (browser)
- ✅ **Proper Layering** - Dockerfile layers, caching, versioning
- ✅ **Version Control** - Container images are versioned artifacts
- ✅ **Build System** - Standard `docker build`, not shell scripts installing packages
- ✅ **Easier Debugging** - Standard Docker tooling (`docker exec`, `docker logs`)

**Distrobox Disadvantages:**
- ❌ No layering - everything installed via shell commands in playbook
- ❌ Can't share base configuration between containers
- ❌ Harder to maintain - package installation scattered across Ansible tasks
- ❌ No build caching - full rebuild every time
- ❌ Architecture dead-end - can't refactor to shared base

**GUI "Advantage" was a myth:**
Distrobox's "automatic GUI support" is just mounting sockets - Docker does the same thing with 5 lines:
```bash
--device /dev/dri:/dev/dri \
-v $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:/tmp/$WAYLAND_DISPLAY:ro \
-e WAYLAND_DISPLAY=$WAYLAND_DISPLAY \
-e XDG_RUNTIME_DIR=/tmp
```

## Current Status

### ✅ What's Working

1. **Container Image Built** - `claude-browser:latest`
   - Node.js 20, Playwright browsers installed
   - Chromium, Firefox, WebKit with system dependencies
   - Claude Code, gh CLI, ripgrep, jq, yq, vim, python3
   - MCP Playwright server (`@playwright/mcp`)

2. **Wrapper Script** - `/var/local/claude-yolo/claude-browser`
   - Token management (shares with `ccy`)
   - SSH key selection
   - Project-specific state directories
   - GPU device mounting
   - Wayland display socket mounting

3. **Manual Browser Launch Works** ✅
   ```bash
   docker run --rm \
     --device /dev/dri:/dev/dri \
     -v $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:/tmp/$WAYLAND_DISPLAY:ro \
     -e WAYLAND_DISPLAY=$WAYLAND_DISPLAY \
     -e XDG_RUNTIME_DIR=/tmp \
     --entrypoint bash \
     claude-browser:latest \
     -c '/root/.cache/ms-playwright/chromium-1194/chrome-linux/chrome \
       --enable-features=UseOzonePlatform \
       --ozone-platform=wayland \
       --no-sandbox \
       --disable-gpu \
       --disable-software-rasterizer \
       --disable-dev-shm-usage \
       https://ltscommerce.dev 2>&1'
   ```

   **Result:** Browser window opens on desktop, loads website successfully

4. **Root Causes Identified and Fixed**
   - ✅ GPU driver incompatibility (PCI ID 0x7d55 not supported by Mesa)
   - ✅ Missing `/dev/dri` mount → Added `--device /dev/dri:/dev/dri`
   - ✅ Chromium trying X11 instead of Wayland → Added Ozone platform flags
   - ✅ GPU process crashes → Forced software rendering with `--disable-gpu`

### ✅ What's Working (Partially)

**Playwright MCP Integration - FUNCTIONAL BUT HEADLESS**

**Current Status (2025-11-10 11:20):**
- ✅ MCP server starts successfully with `--isolated` and `--no-sandbox` flags
- ✅ Playwright MCP tools appear in Claude Code
- ✅ Browser automation works (navigate, screenshot, etc.)
- ✅ No "Browser already in use" errors
- ❌ **Browser runs in HEADLESS mode** - No visible window on desktop

**Evidence:**
```bash
# MCP tools available in Claude Code:
● playwright - Navigate to a URL (MCP)
● playwright - Take screenshot (MCP)
# Successfully navigated to https://ltscommerce.dev
# Screenshot saved to /workspace/.playwright-mcp/ltscommerce-homepage.png

# But browser process shows:
--headless --ozone-platform=headless
```

### ❌ What's Not Working

**Headed Browser Mode (Visible Window)**

**Problem:** Despite all configuration attempts, browser runs in headless mode when launched via MCP server.

**What We've Tried:**

1. **Playwright Config File Approach ❌**
   - Created `/opt/claude-browser/playwright.config.json` with `"headless": false`
   - Passed via `--config` flag to MCP server
   - **Result:** Config file not applied, browser still headless
   - **Error:** MCP server doesn't respect config file for launch options

2. **Environment Variable Approach ❌**
   - Set `PLAYWRIGHT_LAUNCH_OPTIONS` env var in MCP config
   - JSON with `headless: false` and Wayland args
   - **Result:** Environment variable ignored by MCP server

3. **Direct Browser Launch (Control Test) ✅**
   ```bash
   /root/.cache/ms-playwright/chromium-1194/chrome-linux/chrome \
     --enable-features=UseOzonePlatform \
     --ozone-platform=wayland \
     --no-sandbox \
     --disable-gpu \
     https://ltscommerce.dev
   ```
   - **Result:** Browser window DOES appear on Wayland desktop
   - **Proves:** GPU/Wayland/Docker configuration is correct

**Root Cause:**
The `--isolated` flag appears to force headless mode in Playwright MCP server. From MCP help:
```
--isolated    keep the browser profile in memory, do not save it to disk.
--headless    run browser in headless mode, headed by default
```

MCP says "headed by default" but `--isolated` seems to override this.

**Why `--isolated` Was Added:**
- Prevents browser lock files from persisting to disk
- Solves "Browser is already in use" errors
- Trade-off: Reliable operation vs. visible browser

**Browser Lock Issue (Original Problem - NOW SOLVED):**

**Error (before `--isolated`):**
```
Error: Browser is already in use for /root/.cache/ms-playwright/mcp-chrome-c52ddf6
```

**Root Cause (identified and fixed):**
1. Browser crashed on startup due to GPU/Wayland issues
2. Left stale lock directory: `/root/.cache/ms-playwright/mcp-chrome-c52ddf6/`
3. MCP retries → sees lock → error

**Solutions Applied:**
- ✅ Added `--device /dev/dri:/dev/dri` for GPU access
- ✅ Wayland socket mounting
- ✅ `--isolated` flag prevents disk-persisted profiles (no lock files)
- ✅ `--no-sandbox` for running as root in Docker

## Debugging Journey

### Phase 1: Host Pollution Discovery
- Found 2.7GB of Playwright cache on host: `~/.cache/ms-playwright/`
- **Cause:** Accidentally installed `@playwright/mcp` globally via npm while debugging distrobox
- **Fix:** `npm uninstall -g @playwright/mcp && rm -rf ~/.cache/ms-playwright/`

### Phase 2: Container Image Bug
- ccb wrapper looking for `.claude/ccy/Dockerfile` instead of `.claude/ccb/Dockerfile`
- **Cause:** Copy-paste bug when creating ccb from ccy
- **Fix:** Changed `PROJECT_DOCKERFILE=".claude/ccb/Dockerfile"` (line 1885)

### Phase 3: GPU/Display Issues
- Browser crashes immediately on launch
- Errors: `Missing X server or $DISPLAY`, `GPU process exited unexpectedly: exit_code=133`
- **Diagnosis:**
  - Missing `/dev/dri` device mount
  - MESA driver doesn't support GPU (PCI ID 0x7d55)
  - Chromium defaulting to X11, ignoring Wayland
- **Fix:** Added proper flags and GPU device mounting (see working command above)

### Phase 4: Playwright MCP Configuration (CURRENT)
- Manual browser launch works perfectly
- Playwright MCP still getting browser lock errors
- Need to pass browser launch flags to MCP server

## Technical Details

### GPU/MESA Issues

**Hardware:** Intel GPU PCI ID 0x7d55 (not supported by Mesa drivers in container)

**Errors Observed:**
```
MESA: warning: Driver does not support the 0x7d55 PCI ID.
[7:7:1110/094116.817636:ERROR:content/browser/gpu/gpu_process_host.cc:970] GPU process exited unexpectedly: exit_code=133
```

**Solution:** Force software rendering
- `--disable-gpu` - Disables GPU process entirely
- `--disable-software-rasterizer` - Uses CPU rasterization
- `--disable-dev-shm-usage` - Avoids shared memory issues

### Wayland Configuration

**Container Environment:**
```bash
WAYLAND_DISPLAY=wayland-0
XDG_RUNTIME_DIR=/tmp
# Socket mounted: /run/user/1000/wayland-0 → /tmp/wayland-0
```

**Chromium Flags Required:**
- `--enable-features=UseOzonePlatform` - Enable Ozone (Wayland support)
- `--ozone-platform=wayland` - Use Wayland backend
- `--no-sandbox` - Required when running as root in container

### Container Configuration

**Current Docker Run Flags:**
```bash
docker run -it --rm \
  --name "$CONTAINER_NAME" \
  --device /dev/dri:/dev/dri \                          # GPU device access
  -v "$PWD:/workspace" \                                 # Project files
  -v "$PROJECT_CLAUDE_DIR:/root/.claude" \              # Claude state
  -v "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:/tmp/$WAYLAND_DISPLAY:ro" \  # Wayland socket
  -e "WAYLAND_DISPLAY=$WAYLAND_DISPLAY" \
  -e "XDG_RUNTIME_DIR=/tmp" \
  -e "PLAYWRIGHT_LAUNCH_OPTIONS={...}" \                # Browser launch flags (JSON)
  claude-browser:latest \
  claude --mcp-config /opt/claude-browser/mcp_servers.json --dangerously-skip-permissions
```

## Current Problem: Headless vs Headed Browser

### The Trade-off

We face a choice between two approaches:

**Option A: Reliable + Headless (Current State)**
- Use `--isolated` flag
- ✅ No browser lock files
- ✅ No "Browser already in use" errors
- ✅ MCP tools work reliably
- ❌ Browser is invisible (headless mode)

**Option B: Visible + Risky**
- Remove `--isolated` flag
- ✅ Browser window visible on desktop
- ❌ Risk of stale lock files if browser crashes
- ❌ Potential "Browser already in use" errors

### Debugging Journey

#### Phase 1: Browser Lock Errors (SOLVED)
**Problem:** "Browser is already in use" errors
**Cause:** Browser crashed, left lock files
**Solution:** `--isolated` flag prevents disk persistence

#### Phase 2: JSON Config Format (SOLVED)
**Problem:** MCP server failed to start
**Error:** `SyntaxError: Unexpected token 'm', "module.exp"... is not valid JSON`
**Cause:** Config file was JavaScript (`module.exports = {...}`) not JSON
**Solution:** Changed to proper JSON format

#### Phase 3: Headless Mode (CURRENT ISSUE)
**Problem:** Browser runs headless despite config
**Attempts:**
1. Config file with `"headless": false` → Ignored
2. Environment variable `PLAYWRIGHT_LAUNCH_OPTIONS` → Ignored
3. MCP config `"env"` field → Ignored

**Tests Performed:**
```bash
# Control test - direct chrome launch:
✅ WORKS - Window appears on desktop

# MCP server with --isolated:
❌ Headless mode, no window

# MCP server without --isolated:
? NOT YET TESTED
```

### Configuration Attempts (All Failed for Headed Mode)

**✅ Solution Found: `--isolated` and `--no-sandbox` flags**

The `--isolated` flag DOES exist! It's documented in `npx @playwright/mcp --help`:
```
--isolated    keep the browser profile in memory, do not save it to disk.
```

This should prevent the stale lock file issue by not persisting browser profile to disk.

**Updated MCP Configuration:**
```json
{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": [
        "-y",
        "@playwright/mcp@latest",
        "--isolated",
        "--no-sandbox"
      ]
    }
  }
}
```

**Note:** User said they tried `--isolated` last week and it didn't work. Need to verify:
1. Was it spelled correctly?
2. Were GPU/Wayland issues fixed at that time?
3. Was the flag passed to the right place?

The browser may have been crashing for OTHER reasons (GPU, Wayland) and `--isolated` alone wasn't enough.

**❌ Attempt 2: `PLAYWRIGHT_CHROMIUM_EXTRA_LAUNCH_ARGS`**
```bash
-e "PLAYWRIGHT_CHROMIUM_EXTRA_LAUNCH_ARGS=--enable-features=UseOzonePlatform --ozone-platform=wayland"
```
**Result:** Browser still crashes, MCP doesn't respect this env var

**🔄 Attempt 3: `PLAYWRIGHT_LAUNCH_OPTIONS` (CURRENT)**
```bash
-e "PLAYWRIGHT_LAUNCH_OPTIONS={\"args\":[\"--enable-features=UseOzonePlatform\",\"--ozone-platform=wayland\",\"--no-sandbox\",\"--disable-gpu\",\"--disable-software-rasterizer\",\"--disable-dev-shm-usage\"]}"
```
**Status:** Implemented but not yet tested

### Research Complete ✅

**Playwright MCP CLI Flags Discovered:**

```bash
npx @playwright/mcp@latest --help
```

**Key Flags for Our Use Case:**
- `--isolated` - **Keep browser profile in memory, don't save to disk** (prevents lock files!)
- `--no-sandbox` - Disable sandbox (required for running as root in Docker)
- `--headless` - Run headless (we want headed for GUI)
- `--config <path>` - Path to configuration file (for complex launch options)
- `--browser <browser>` - Choose browser: chrome, firefox, webkit, msedge
- `--executable-path <path>` - Path to browser executable
- `--user-data-dir <path>` - Custom profile directory
- `--viewport-size <size>` - Browser window size
- `--ignore-https-errors` - Useful for development

**Repository:**
- GitHub: https://github.com/microsoft/playwright-mcp
- Package: `@playwright/mcp@0.0.45` (installed in container)

**Alternative Approaches:**
1. **Custom MCP wrapper script:**
   - Create wrapper around `@playwright/mcp` that sets launch options
   - Pass wrapper as MCP server command instead of npx

2. **Playwright config file:**
   - Check if Playwright respects `playwright.config.js` in container
   - Set launch options globally via config file

3. **Environment variables:**
   - Research all Playwright env vars
   - Check MCP server source code for env var handling

4. **Fork/patch MCP server:**
   - Last resort: fork `@playwright/mcp`
   - Add configuration for launch options
   - Use forked version in container

## Next Actions

### Immediate Decision Required

**Test removing `--isolated` flag:**
- Rebuild container without `--isolated`
- Test if browser becomes headed (visible)
- Monitor for lock file issues
- Decision: Accept risk vs accept headless

### If Headless is Acceptable

1. **Document current working state**
   - MCP works, automation functional
   - Headless mode for reliability
   - Update README with limitations

2. **Commit working code**
   - All ccb files
   - Documentation
   - Known limitation: headless only

### If Headed Mode is Required

1. **Remove `--isolated` flag**
   - Update Dockerfile MCP config
   - Rebuild container
   - Test browser visibility

2. **Implement lock file cleanup**
   - Enhance entrypoint cleanup
   - Add periodic cleanup job
   - Monitor for lock file buildup

3. **Alternative: Custom MCP wrapper**
   - Fork `@playwright/mcp`
   - Add explicit `headless: false` option
   - Use custom version in container

### Short-term (Important)
4. **Create reproducible test case**
   - Minimal script that launches Playwright MCP server
   - Separately test browser launch with our flags
   - Confirm flags work when applied to MCP server

5. **Document workarounds**
   - If env vars don't work, create wrapper script
   - Update Dockerfile to include custom MCP launcher
   - Ensure solution is maintainable

### Long-term (Architecture)
6. **Refactor for DRY**
   - Create `Dockerfile.base` with common deps
   - `ccy` extends base (general purpose)
   - `ccb` extends base + adds Playwright
   - Share wrapper code via `lib/common.bash`

7. **Add testing infrastructure**
   - Automated tests for browser launch
   - CI/CD for container builds
   - Smoke tests for MCP server startup

## File Locations

### ccb Implementation
- **Dockerfile:** `files/var/local/claude-yolo/Dockerfile.browser`
- **Entrypoint:** `files/var/local/claude-yolo/entrypoint-browser.sh`
- **Wrapper:** `files/var/local/claude-yolo/claude-browser`
- **Bash alias:** `files/home/bashrc-includes/claude-browser.bash`
- **Playbook:** `playbooks/imports/optional/common/play-install-claude-browser.yml`

### Key Line Numbers
- Docker run command: `claude-browser:2481-2496`
- Browser launch flags: `claude-browser:2493` (PLAYWRIGHT_LAUNCH_OPTIONS env var)
- GPU device mount: `claude-browser:2484`
- Wayland display setup: `claude-browser:2460-2478`
- MCP config creation: `Dockerfile.browser:86-97`
- Entrypoint cleanup: `entrypoint-browser.sh:103-107`

### ccyb (Abandoned)
- Distrobox approach documented in git history
- Playbook still exists: `playbooks/imports/optional/common/play-distrobox-playwright.yml`
- Can be removed once ccb is confirmed working

## Success Criteria

### Phase 1: Manual Browser Launch ✅ COMPLETE
- [x] Container image builds successfully
- [x] Can launch browser manually with docker run
- [x] Browser window appears on Wayland desktop
- [x] Can navigate to websites
- [x] GPU/MESA issues resolved with software rendering

### Phase 2: Playwright MCP Integration (IN PROGRESS)
- [ ] Playwright MCP server starts without crashing
- [ ] No "Browser already in use" errors
- [ ] MCP tools available in Claude Code
- [ ] Claude can navigate to URLs via MCP
- [ ] Browser windows appear when Claude uses MCP tools
- [ ] Screenshots work through MCP

### Phase 3: Production Ready
- [ ] Reproducible across launches
- [ ] No stale lock files
- [ ] Proper error handling
- [ ] Documentation complete
- [ ] Code committed to repository

## Test Commands

### Manual Browser Test (WORKING)
```bash
docker run --rm \
  --device /dev/dri:/dev/dri \
  -v $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:/tmp/$WAYLAND_DISPLAY:ro \
  -e WAYLAND_DISPLAY=$WAYLAND_DISPLAY \
  -e XDG_RUNTIME_DIR=/tmp \
  --entrypoint bash \
  claude-browser:latest \
  -c '/root/.cache/ms-playwright/chromium-1194/chrome-linux/chrome \
    --enable-features=UseOzonePlatform \
    --ozone-platform=wayland \
    --no-sandbox \
    --disable-gpu \
    --disable-software-rasterizer \
    --disable-dev-shm-usage \
    https://ltscommerce.dev 2>&1'
```

### ccb End-to-End Test (NOT WORKING YET)
```bash
cd ~/Projects/fedora-desktop
ccb --token ballicom_joseph --no-ssh
# Then in Claude Code:
> Navigate to https://ltscommerce.dev and take a screenshot
```

**Expected:** Browser opens, navigates, screenshot saved
**Actual:** "Browser is already in use" error

## Complete Solution Design

### Three-Part Fix

**Part 1: MCP Server Flags (Prevents Lock Files)**
```json
{
  "mcpServers": {
    "playwright": {
      "command": "npx",
      "args": [
        "-y",
        "@playwright/mcp@latest",
        "--isolated",          // In-memory profile (no disk writes)
        "--no-sandbox"         // Required for root in Docker
      ]
    }
  }
}
```

**Part 2: Playwright Config File (Browser Launch Options)**

Create `/opt/claude-browser/playwright.config.js`:
```javascript
module.exports = {
  use: {
    launchOptions: {
      args: [
        '--enable-features=UseOzonePlatform',
        '--ozone-platform=wayland',
        '--disable-gpu',
        '--disable-software-rasterizer',
        '--disable-dev-shm-usage'
      ]
    }
  }
};
```

Then add to MCP args: `"--config", "/opt/claude-browser/playwright.config.js"`

**Part 3: Docker GPU/Display (Already Implemented)**
```bash
--device /dev/dri:/dev/dri
-v $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:/tmp/$WAYLAND_DISPLAY:ro
-e WAYLAND_DISPLAY=$WAYLAND_DISPLAY
-e XDG_RUNTIME_DIR=/tmp
```

### Implementation Steps

1. **Update Dockerfile.browser:**
   - Create playwright.config.js with launch options
   - Update mcp_servers.json to include `--isolated`, `--no-sandbox`, `--config`

2. **Rebuild container:**
   ```bash
   cd /opt/claude-browser
   docker build -t claude-browser:latest .
   ```

3. **Test:**
   ```bash
   cd ~/Projects/fedora-desktop
   ccb --token ballicom_joseph --no-ssh
   > Navigate to https://ltscommerce.dev and take a screenshot
   ```

### Why This Should Work

**Problem:** Browser crashes on startup, leaves stale lock file, MCP can't reuse
**Solution:**
1. `--isolated` = No disk-persisted profile = No stale locks
2. `--no-sandbox` = Works as root in Docker
3. Config file = Proper Wayland + software rendering flags
4. GPU device mount = Hardware access (even if not fully supported)

## Version History

- **v1.0.0** - Initial ccb implementation (Docker-based)
- **v1.1.0** - Added GPU device mount and Wayland configuration
- **v1.2.0** - Discovered MCP `--isolated` flag and config file approach
- **v1.3.0** - Fixed JSON config format issue
- **v1.4.0** - MCP functional but headless, investigating headed mode
- **v1.5.0** - Identified distrobox GUI "magic" (full XDG_RUNTIME_DIR mount)
- **v1.6.0** (Current) - **✅ WORKING! Headed browser mode functional in Docker**

## SUCCESS: Headed Browser Mode Working (2025-11-10 15:10)

### The Final Solution

After extensive debugging, headed browser mode is now working in Docker! The solution required three critical fixes:

#### 1. Full XDG_RUNTIME_DIR Mount (Distrobox Approach)
**Problem:** Only mounting the Wayland socket file wasn't enough
**Solution:** Mount entire `/run/user/1000` directory (like distrobox does)
```bash
-v "$XDG_RUNTIME_DIR:$XDG_RUNTIME_DIR"  # Not read-only!
```

**Why needed:**
- Wayland socket
- dbus sockets
- dconf settings
- PulseAudio (future)

#### 2. Read-Write XDG_RUNTIME_DIR
**Problem:** Initial mount was read-only (`:ro`), causing dconf errors
**Error:** `unable to create file '/run/user/1000/dconf/user': Read-only file system`
**Solution:** Remove `:ro` flag to make it read-write

#### 3. Correct MCP Config File Format
**Problem:** Used Playwright test config format (`use.launchOptions`)
**Solution:** Use MCP config format (`browser.launchOptions`)
```json
{
  "browser": {
    "launchOptions": {
      "headless": false,
      "args": [
        "--enable-features=UseOzonePlatform",
        "--ozone-platform=wayland",
        "--no-sandbox",
        "--disable-gpu",
        "--disable-software-rasterizer",
        "--disable-dev-shm-usage"
      ]
    }
  }
}
```

#### 4. DISPLAY Environment Variable
**Problem:** MCP auto-detects headless mode on Linux when `DISPLAY` not set
**Code:** `headless: os.platform() === "linux" && !process.env.DISPLAY`
**Solution:** Set `DISPLAY=:0` even on Wayland

### What Works Now

✅ **Headed browser mode** - Windows appear on desktop
✅ **Full Wayland support** - Native Wayland, not XWayland
✅ **MCP browser automation** - Navigate, screenshot, interact
✅ **Proper isolation** - No /tmp sharing (unlike distrobox)
✅ **Desktop integration** - Full XDG_RUNTIME_DIR access
✅ **Software rendering** - Works despite GPU driver issues

### Key Learnings

1. **Distrobox "GUI magic" is just mounting XDG_RUNTIME_DIR**
   - Not special distrobox features
   - Can replicate in Docker
   - But distrobox shares /tmp (security issue)

2. **MCP config format is different from Playwright test config**
   - `browser.launchOptions` not `use.launchOptions`
   - Documentation unclear on this

3. **Read-only XDG_RUNTIME_DIR breaks GUI apps**
   - dconf needs to write settings
   - dbus needs to create sockets
   - Must be read-write

4. **DISPLAY variable prevents MCP auto-headless**
   - Set even on Wayland (uses Wayland socket via XDG_RUNTIME_DIR)
   - MCP checks for DISPLAY to determine headless mode

## Critical Status Update: Circular Problem (2025-11-10 13:20)

### The Real Problem

**The browser crashes when MCP tries to launch it in headed mode.** Period.

`--isolated` doesn't prevent crashes - it's a **symptomatic workaround** that hides crash aftermath:
- Browser crashes → leaves lock file on disk
- Next launch → sees lock file → "Browser already in use" error
- `--isolated` keeps profile in memory → no lock files → can launch again after crash
- **But the browser still crashes every time**

### What We Know

**Manual chrome launch WORKS:**
```bash
/root/.cache/ms-playwright/chromium-1194/chrome-linux/chrome \
  --enable-features=UseOzonePlatform \
  --ozone-platform=wayland \
  --no-sandbox \
  --disable-gpu \
  https://ltscommerce.dev
```
Browser window appears on desktop successfully.

**MCP launch in headed mode CRASHES:**
Even with:
- `DISPLAY=:0` set (prevents auto-headless)
- `--config` with Wayland launch args
- Same flags as manual launch
Browser crashes immediately, leaves lock file.

**MCP launch in headless mode WORKS:**
With `--isolated` flag, headless mode is reliable but invisible.

### Why Config Might Not Be Applied

The `--config` flag may not be passing browser launch options correctly, or MCP is overriding them. We don't have visibility into what MCP is actually doing.

### Decision Point: Pivot to Distrobox?

**Docker ccb:** Stuck. Can't get headed mode working despite GPU/Wayland being proven functional.

**Distrobox ccyb:** Already has working playbook (`play-distrobox-playwright.yml`). Might "just work" with GUI since distrobox has better desktop integration.

**Proposal:** Systematically test distrobox with proper isolation assessment.

## Distrobox Assessment Plan

Before pivoting to distrobox, we need to verify three critical requirements:

### 1. Safety Assessment

**Goal:** Ensure distrobox container cannot damage host system or leak data

**Critical Tests:**

1. **Filesystem Write Boundaries**
   ```bash
   # Inside container:
   ccyb
   > Try to write to /etc/hosts (should fail - no host system access)
   > Try to modify /home/joseph/.bashrc (should fail - isolated home)
   > Try to write to /tmp/test (should succeed - container's own /tmp)

   # From host:
   ls /tmp/test  # Should NOT exist (container has own /tmp)
   ```

2. **Verify No Shared /tmp**
   ```bash
   # From host:
   echo "host-temp-file" > /tmp/isolation-test

   # Inside container:
   cat /tmp/isolation-test  # Should FAIL - separate /tmp

   # Inside container:
   echo "container-temp-file" > /tmp/container-test

   # From host:
   cat /tmp/container-test  # Should FAIL - not shared
   ```

3. **Mount Point Verification**
   ```bash
   # Inside container:
   mount | grep -v "proc\|sys\|dev"

   # Should show ONLY these mounts:
   # ✅ /workspace -> current project directory
   # ✅ /home/joseph/Projects -> /home/joseph/Projects (read project files)
   # ✅ /var/lib/lxc -> /var/lib/lxc (follow LXC symlinks)

   # Should NOT show:
   # ❌ Host /tmp
   # ❌ Host /home/joseph (entire home)
   # ❌ Host /etc
   # ❌ Host /var (except /var/lib/lxc)
   ```

4. **Process Isolation**
   ```bash
   # Inside container:
   ps aux | wc -l  # Should show limited processes

   # From host:
   ps aux | grep playwright  # Can see container processes (podman architecture)
   ```

5. **Network Isolation Check**
   ```bash
   # Inside container:
   curl https://api.anthropic.com  # Should work (needs API access)
   ip addr  # Check if has own network namespace
   ```

**Expected Results:**
- ✅ Cannot modify host system files
- ✅ Cannot see host /tmp contents
- ✅ Only specified directories mounted
- ✅ Can access internet for API calls
- ✅ Proper UID/GID mapping for file ownership

### 2. Isolation Assessment

**Goal:** Verify no host pollution and complete separation

**Critical Tests:**

1. **Home Directory Isolation**
   ```bash
   # Inside container:
   ccyb
   echo $HOME  # Should be /home/joseph (but isolated)
   ls -la ~/   # Should NOT see host home contents

   # Install something globally:
   npm install -g some-test-package

   # From host:
   which some-test-package  # Should NOT exist
   npm list -g | grep some-test-package  # Should NOT appear
   ```

2. **Cache Isolation**
   ```bash
   # Inside container:
   du -sh ~/.cache/ms-playwright/

   # From host:
   du -sh ~/.cache/ms-playwright/  # Should be different size or not exist
   ```

3. **Config File Isolation**
   ```bash
   # Inside container:
   cat ~/.config/claude/settings.json

   # From host:
   cat ~/.config/claude/settings.json
   # Should be completely different files
   ```

4. **Verify Container Home Location**
   ```bash
   # Check where container home actually lives:
   ls -la ~/.claude-tokens/ccyb/container-home/
   # This should be the isolated home directory
   ```

**Expected Results:**
- ✅ Isolated home directory
- ✅ MCP cache stays in container
- ✅ npm global installs don't pollute host
- ✅ No /tmp sharing
- ✅ Container home persists in known location

### 3. Functionality Assessment

**Goal:** Verify all required features work correctly

**Critical Tests:**

1. **Headed Browser Launch (PRIMARY TEST)**
   ```bash
   ccyb
   > Use Playwright to navigate to https://ltscommerce.dev

   # Expected: Browser window appears on desktop
   # Watch for: Crashes, lock files, errors
   ```

2. **Browser Automation**
   ```bash
   ccyb
   > Navigate to https://example.com
   > Take a screenshot
   > Close browser
   > Navigate again (test recovery)
   ```

3. **Project File Access**
   ```bash
   ccyb
   > Read playbook-main.yml
   > Create test file in /workspace

   # From host:
   ls -la ~/Projects/fedora-desktop/test-file
   # Should exist with joseph:joseph ownership
   ```

4. **Git Operations**
   ```bash
   ccyb
   > Run: git status
   > Run: git log
   ```

5. **GitHub CLI**
   ```bash
   ccyb
   > Run: gh api user
   ```

6. **LXC Symlinks**
   ```bash
   ccyb
   > Navigate to project with LXC symlink
   > Verify can access symlinked files
   ```

7. **Persistence**
   ```bash
   ccyb
   > npm install -g cowsay
   > exit

   ccyb
   > cowsay "test"  # Should work
   ```

**Expected Results:**
- ✅ Browser windows visible on desktop
- ✅ No crashes or lock file errors
- ✅ File operations work correctly
- ✅ Git/GitHub work
- ✅ LXC symlinks work
- ✅ State persists

### 4. Decision Criteria

**Choose Distrobox IF:**
- ✅ Headed browser works reliably
- ✅ Complete isolation verified (including /tmp)
- ✅ All functionality tests pass
- ✅ No host pollution

**Stick with Docker IF:**
- ❌ Distrobox also has headed mode issues
- ❌ Isolation is insufficient
- ❌ Still causes host pollution

**Reject Both IF:**
- ❌ Neither can do headed mode
- Need to investigate alternative approaches

## Key Learnings

### What Works
1. ✅ Docker + GPU device mount + Wayland = GUI apps work
2. ✅ Direct chrome launch with our flags = window appears
3. ✅ `--isolated` flag = no lock files, reliable operation
4. ✅ `--no-sandbox` required for root in Docker
5. ✅ MCP server starts and responds to commands
6. ✅ Playwright automation fully functional

### What Doesn't Work
1. ❌ Playwright config files aren't applied by MCP server
2. ❌ Environment variables don't control MCP browser launch
3. ❌ MCP `"env"` field in config doesn't help
4. ❌ `--isolated` + headed mode combination

### Critical Insight
**The `--isolated` flag is a hack to work around browser crashes leaving cache files.**
- Original problem: GPU/Wayland issues caused crashes
- Crashes left lock files behind
- `--isolated` prevents lock files but forces headless
- **Solution was treating symptom, not root cause**

**Root cause was:** Missing GPU device + wrong Wayland flags
**Those are now fixed**, so we might not need `--isolated` anymore

## References

- **ccy (general YOLO mode):** Working Docker-based solution for general development
- **Playwright docs:** https://playwright.dev/docs/docker
- **MCP protocol:** https://github.com/anthropics/mcp
- **Playwright MCP:** Need to find official docs/repository
