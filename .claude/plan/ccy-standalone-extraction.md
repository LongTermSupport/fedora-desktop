# CCY Standalone Extraction - Implementation Plan

## Overview

Extract the CCY (Claude Code YOLO) system from fedora-desktop into a standalone, multi-platform repository. CCY will maintain its GitHub-centric design while becoming installable on any Linux/macOS system with Docker/Podman.

**Goals**:
- Make CCY usable by non-fedora-desktop users
- Maintain 100% backward compatibility for existing users
- Support multiple platforms (Fedora, Ubuntu, Debian, Arch, macOS)
- Keep GitHub as acceptable hard requirement
- Preserve all existing features and functionality

**Non-Goals**:
- Supporting non-GitHub git hosting (GitLab, Bitbucket, etc.)
- Windows support (WSL2 may work but unsupported)
- Removing container dependency

## Current Architecture Analysis

### File Structure (in fedora-desktop)

```
fedora-desktop/
├── files/
│   ├── var/local/claude-yolo/          # Main scripts location
│   │   ├── claude-yolo                 # CCY wrapper (1,637 lines)
│   │   ├── claude-browser              # CCB wrapper (browser mode)
│   │   ├── Dockerfile                  # Base container image
│   │   ├── Dockerfile.browser          # Browser variant
│   │   ├── entrypoint.sh               # Container init
│   │   ├── entrypoint-browser.sh       # Browser container init
│   │   ├── Dockerfile.project-template # User customization template
│   │   ├── Dockerfile.example-ansible  # Example: Ansible projects
│   │   ├── Dockerfile.example-golang   # Example: Golang projects
│   │   └── lib/                        # Modular libraries (8 files, ~4K LOC)
│   │       ├── common.bash             # Core utilities, container abstraction
│   │       ├── token-management.bash   # OAuth token lifecycle
│   │       ├── session-management.bash # Named sessions with TUI
│   │       ├── ssh-handling.bash       # GitHub SSH integration
│   │       ├── dockerfile-custom.bash  # Custom Dockerfile workflows
│   │       ├── network-management.bash # Docker network detection
│   │       ├── docker-health.bash      # Container diagnostics
│   │       └── ui-helpers.bash         # Output formatting
│   ├── opt/claude-yolo/                # Static assets
│   │   └── ccy-startup-info.txt        # Welcome message
│   ├── opt/claude-browser/             # Browser mode assets
│   │   ├── ccb-startup-info.txt
│   │   ├── chrome-ws/                  # Chrome WebSocket CLI tool
│   │   └── skills/browsing/            # MCP skill documentation
│   └── home/bashrc-includes/
│       ├── claude-yolo.bash.j2         # Shell integration (ccy alias)
│       └── claude-browser.bash         # Shell integration (ccb alias)
├── playbooks/imports/optional/common/
│   └── play-install-claude-yolo.yml    # Ansible installer
└── vars/
    └── container-defaults.yml          # Container engine preference
```

### Installation Flow (Current - Ansible)

1. Check prerequisites (container engine)
2. Create directories (`/opt/claude-yolo`, `/var/local/claude-yolo/lib`)
3. Copy all files to system locations
4. Deploy bashrc includes with container engine preference
5. Create user directories (`~/.claude-tokens/ccy/`)
6. Build container images with hash tracking
7. Install browser mode (optional, controlled by var)

### Dependencies Identified

**Runtime (Host System)**:
- Container engine: Docker or Podman (user's choice)
- Git: Repository detection and operations
- GitHub CLI (`gh`): Authentication and token management
- SSH: GitHub key authentication
- POSIX shell: bash 4.0+

**Runtime (Container)**:
- Debian slim base
- Node.js 20 LTS
- Claude Code CLI (npm install -g @anthropic-ai/claude-code)
- System tools: curl, wget, jq, yq, ripgrep, git, gh CLI, vim

**Soft Dependencies (fedora-desktop patterns)**:
- `github_<alias>` SSH key pattern (can use `--ssh-key` instead)
- `gh-token-<alias>` functions (falls back to `gh auth token`)
- `~/.bashrc-includes/` directory (installation convention)
- `vars/container-defaults.yml` (container engine setting)

### Coupling Points Found

**20 references to update across 7 files**:

1. **ssh-handling.bash** (6 references):
   - Line 14-16: Comments about `play-github-cli-multi.yml`
   - Line 27: "managed by play-github-cli-multi.yml"
   - Line 80: Suggests running `ansible-playbook .../play-github-cli-multi.yml`
   - Line 126: Same suggestion
   - Line 136: Suggests `ansible-playbook .../play-git-configure-and-tools.yml`
   - Lines 142-186: Tries to load gh-token functions, suggests playbook on failure

2. **claude-yolo** (5 references):
   - Line 801: Suggests `ansible-playbook .../play-docker.yml`
   - Line 975: Suggests `ansible-playbook .../play-install-claude-yolo.yml`
   - Line 989: Same as 975
   - Lines 1567, 1591: fedora-desktop documentation URLs

3. **claude-browser** (5 references):
   - Line 676: Docker playbook suggestion
   - Lines 746, 760: Install playbook suggestions
   - Lines 1325, 1349: fedora-desktop documentation URLs

4. **dockerfile-custom.bash** (2 references):
   - Line 101: Install playbook suggestion
   - Line 525: fedora-desktop documentation URL

5. **entrypoint.sh** (2 references):
   - Line 47: "ensure play-github-cli-multi.yml is configured"
   - Line 82: Playbook suggestion

All coupling is in **error messages, help text, and documentation links** - no runtime dependencies on fedora-desktop code.

## Proposed Standalone Architecture

### Repository Structure

```
ccy/                                    # New standalone repo
├── README.md                           # Main documentation
├── INSTALL.md                          # Installation guide
├── LICENSE                             # MIT License
├── install.sh                          # One-command installer
├── uninstall.sh                        # Clean removal
├── bin/
│   ├── ccy                            # Renamed from claude-yolo
│   └── ccyb                           # Renamed from claude-browser
├── lib/                               # All .bash libraries (unchanged)
│   ├── common.bash
│   ├── token-management.bash
│   ├── session-management.bash
│   ├── ssh-handling.bash
│   ├── dockerfile-custom.bash
│   ├── network-management.bash
│   ├── docker-health.bash
│   └── ui-helpers.bash
├── docker/
│   ├── Dockerfile                     # Base image
│   ├── Dockerfile.browser             # Browser variant
│   ├── entrypoint.sh                  # Container init
│   ├── entrypoint-browser.sh
│   └── templates/                     # Custom Dockerfile templates
│       ├── Dockerfile.project-template
│       ├── Dockerfile.example-ansible
│       └── Dockerfile.example-golang
├── assets/
│   ├── ccy-startup-info.txt
│   ├── ccb-startup-info.txt
│   └── browser/                       # Browser mode assets
│       ├── chrome-ws/
│       └── skills/browsing/
├── docs/
│   ├── github-setup.md                # Replace playbook references
│   ├── custom-dockerfiles.md          # Replace fedora-desktop docs
│   ├── multi-account.md               # Optional multi-account setup
│   ├── troubleshooting.md
│   └── contributing.md
├── contrib/
│   ├── install-fedora-desktop.yml     # Optional Ansible playbook
│   └── completions/                   # Shell completions (future)
│       ├── ccy.bash
│       └── ccy.zsh
└── tests/
    ├── test-install.sh                # Installation tests
    └── test-platforms.sh              # Multi-platform validation
```

### Installation Flow (Standalone - Shell Script)

```bash
#!/bin/bash
# install.sh - CCY Standalone Installer

# 1. Detect platform (Linux distro, macOS, architecture)
# 2. Check prerequisites
#    - git, docker/podman
#    - Offer to install gh CLI if missing
# 3. Determine installation prefix
#    - System-wide: /usr/local (default, requires sudo)
#    - User-only: ~/.local (--user flag)
# 4. Install files
#    - Scripts → $PREFIX/lib/ccy/
#    - Symlinks → $PREFIX/bin/{ccy,ccyb}
#    - Dockerfiles → $PREFIX/lib/ccy/docker/
#    - Assets → $PREFIX/lib/ccy/assets/
# 5. Configure shell integration
#    - Add to ~/.bashrc or ~/.zshrc
#    - Set CCY_CONTAINER_ENGINE preference
# 6. Create user directories
#    - ~/.claude-tokens/ccy/
# 7. Build container images
#    - ccy:latest (base)
#    - ccy-browser:latest (optional)
# 8. Post-install verification
#    - Test ccy --version
#    - Prompt for gh auth login
#    - Display quick start guide
```

**Installation methods**:
```bash
# Method 1: Clone and install
git clone https://github.com/USER/ccy.git
cd ccy
sudo ./install.sh

# Method 2: One-liner (recommended)
bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)

# Method 3: User-only (no sudo)
bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh) --user
```

## Implementation Steps

### Phase 1: Code Refactoring (3-4 hours)

**Step 1.1: Update Error Messages and Help Text**

Replace all 20 Ansible playbook references with platform-agnostic alternatives:

| Current | Replace With |
|---------|--------------|
| `ansible-playbook .../play-docker.yml` | "Enable rootless: https://docs.docker.com/engine/security/rootless/" |
| `ansible-playbook .../play-install-claude-yolo.yml` | "Reinstall: bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)" |
| `ansible-playbook .../play-github-cli-multi.yml` | "Setup GitHub: https://github.com/USER/ccy/blob/main/docs/github-setup.md" |
| `ansible-playbook .../play-git-configure-and-tools.yml` | "Install GitHub CLI: https://cli.github.com/" |

**Files to edit**:
- `lib/ssh-handling.bash` (6 changes)
- `bin/ccy` (5 changes)
- `bin/ccyb` (5 changes)
- `lib/dockerfile-custom.bash` (2 changes)
- `docker/entrypoint.sh` (2 changes)

**Step 1.2: Update Documentation URLs**

Replace 4 fedora-desktop documentation URLs:
```
FROM: https://github.com/LongTermSupport/fedora-desktop/blob/main/docs/containerization.md
TO:   https://github.com/USER/ccy/blob/main/docs/custom-dockerfiles.md
```

**Files to edit**:
- `bin/ccy` (2 URLs)
- `bin/ccyb` (2 URLs)
- `lib/dockerfile-custom.bash` (1 URL)

**Step 1.3: Simplify Multi-Account Token Logic**

In `lib/ssh-handling.bash` lines 142-186:
- Keep trying to load `gh-token-<alias>` functions if they exist
- Always fall back gracefully to `gh auth token`
- Remove scary error messages about missing playbooks
- Document multi-account as "optional advanced feature"

Before:
```bash
if ! type "$token_func" &>/dev/null; then
    print_error "Function not found: $token_func"
    echo "Required: gh-token-<alias> functions from play-github-cli-multi.yml"
    echo "To fix:"
    echo "  1. Run: ansible-playbook playbooks/.../play-github-cli-multi.yml"
    exit 1
fi
```

After:
```bash
if ! type "$token_func" &>/dev/null; then
    # Multi-account functions not available - fall back to default token
    GH_TOKEN=$(gh auth token 2>/dev/null)
fi
```

**Step 1.4: Make Bashrc Include Paths Configurable**

Currently hardcoded: `~/.bashrc-includes/gh-aliases.inc.bash`

Add fallback search paths for standalone:
```bash
# Try multiple locations for multi-account functions
for include_path in \
    ~/.bashrc-includes/gh-aliases.inc.bash \
    ~/.config/ccy/gh-aliases.inc.bash \
    ~/.ccy/gh-aliases.inc.bash; do
    if [ -f "$include_path" ]; then
        source "$include_path"
        break
    fi
done
```

### Phase 2: Create Installer (4-5 hours)

**Step 2.1: Write Core Installation Logic**

`install.sh` structure:
```bash
#!/usr/bin/env bash
set -e  # Fail fast

# Configuration
VERSION="1.0.0"
DEFAULT_PREFIX="/usr/local"
USER_PREFIX="$HOME/.local"

# Color codes for output
# ... (from lib/common.bash)

# Detect platform
detect_platform() {
    # OS: Linux (distro), Darwin (macOS)
    # Package manager: apt, dnf, yum, pacman, brew
    # Container engine: docker, podman, or missing
}

# Check prerequisites
check_prerequisites() {
    # Required: git
    # Required: docker OR podman
    # Optional: gh (offer to install)
}

# Install GitHub CLI
install_github_cli() {
    # Platform-specific installation
    # Ubuntu/Debian: Add GitHub apt repository
    # Fedora/RHEL: dnf install gh
    # Arch: pacman -S github-cli
    # macOS: brew install gh
}

# Determine installation prefix
get_install_prefix() {
    # System: /usr/local (needs sudo)
    # User: ~/.local (no sudo)
    # Custom: --prefix=/opt/ccy
}

# Install files
install_files() {
    # Create directory structure
    # Copy bin/, lib/, docker/, assets/
    # Set permissions
    # Create symlinks
}

# Configure shell integration
setup_shell_integration() {
    # Detect shell: bash or zsh
    # Add to rc file if not present
    # Set CCY_CONTAINER_ENGINE
    # Export PATH if needed
}

# Create user directories
create_user_directories() {
    mkdir -p ~/.claude-tokens/ccy/{tokens,projects}
    chmod 700 ~/.claude-tokens/ccy
}

# Build container images
build_containers() {
    # Detect container engine
    # Build ccy:latest
    # Build ccy-browser:latest (optional)
    # Verify images
}

# Post-install verification
verify_installation() {
    # Test ccy --version
    # Prompt for gh auth login
    # Display quick start guide
}

# Main installation flow
main() {
    detect_platform
    check_prerequisites

    # Parse command-line flags
    while [[ $# -gt 0 ]]; do
        case $1 in
            --user) USE_USER_PREFIX=true ;;
            --prefix=*) CUSTOM_PREFIX="${1#*=}" ;;
            --no-browser) SKIP_BROWSER=true ;;
            --help) show_help; exit 0 ;;
            *) echo "Unknown option: $1"; exit 1 ;;
        esac
        shift
    done

    get_install_prefix
    install_files
    setup_shell_integration
    create_user_directories
    build_containers
    verify_installation
}

main "$@"
```

**Step 2.2: Platform-Specific Logic**

Handle differences between platforms:

**Package manager detection**:
```bash
if command -v apt-get &>/dev/null; then
    PKG_MANAGER="apt"
    INSTALL_CMD="sudo apt-get install -y"
elif command -v dnf &>/dev/null; then
    PKG_MANAGER="dnf"
    INSTALL_CMD="sudo dnf install -y"
elif command -v brew &>/dev/null; then
    PKG_MANAGER="brew"
    INSTALL_CMD="brew install"
fi
```

**GitHub CLI installation**:
- Ubuntu/Debian: Add apt repository, install package
- Fedora/RHEL: Direct package install
- Arch: pacman package
- macOS: Homebrew
- Others: Download binary from GitHub releases

**Step 2.3: Shell Integration**

Add to `~/.bashrc` or `~/.zshrc`:
```bash
# CCY - Claude Code YOLO
export CCY_CONTAINER_ENGINE="${CCY_CONTAINER_ENGINE:-podman}"
export PATH="/usr/local/bin:$PATH"  # Or ~/.local/bin for user install
alias ccy='/usr/local/lib/ccy/bin/ccy'
alias ccyb='/usr/local/lib/ccy/bin/ccyb'
```

**Step 2.4: Uninstaller**

`uninstall.sh`:
```bash
#!/usr/bin/env bash
# Remove installed files
# Remove container images
# Remove shell integration (prompt first)
# Keep user data (~/.claude-tokens/ccy/) unless --purge flag
```

### Phase 3: Documentation (3-4 hours)

**Step 3.1: Main README.md**

Sections:
1. **What is CCY?** - Brief description, features
2. **Quick Start** - One-liner installation + basic usage
3. **Prerequisites** - System requirements, dependencies
4. **Installation** - Detailed installation instructions
5. **Usage** - Common commands and workflows
6. **Configuration** - Environment variables, customization
7. **Troubleshooting** - Common issues and solutions
8. **Advanced Features** - Custom Dockerfiles, multi-account, sessions
9. **Contributing** - How to contribute
10. **License** - MIT License

**Step 3.2: INSTALL.md**

Detailed installation guide:
- System requirements
- Platform-specific instructions (Ubuntu, Fedora, Arch, macOS)
- Manual installation steps
- Docker vs Podman setup
- Rootless container configuration
- GitHub CLI setup
- SSH key configuration
- Verification steps

**Step 3.3: docs/github-setup.md**

Replace all playbook references with this guide:
1. **Install GitHub CLI** - Platform-specific
2. **Authenticate** - `gh auth login` walkthrough
3. **Create SSH key** - `ssh-keygen` command
4. **Add to GitHub** - Via web or `gh ssh-key add`
5. **Test connection** - `ssh -T git@github.com`
6. **Troubleshooting** - Common authentication issues

**Step 3.4: docs/custom-dockerfiles.md**

Replace fedora-desktop containerization.md:
1. **What are custom Dockerfiles?**
2. **When to use them** - Python versions, system packages, etc.
3. **Quick start** - `ccy --custom-docker` (AI-guided)
4. **Manual creation** - Edit `.claude/ccy/Dockerfile`
5. **Templates** - Available templates and usage
6. **Examples**:
   - Ansible projects (Python 3.12, Ansible, yamllint)
   - Golang projects (Go toolchain, linters)
   - Python data science (Jupyter, pandas, numpy)
7. **Troubleshooting** - Build failures, missing packages

**Step 3.5: docs/multi-account.md**

Document optional multi-account setup:
1. **Overview** - What multi-account support provides
2. **Manual setup** - Without Ansible
3. **SSH key creation** - Multiple keys with aliases
4. **SSH config** - Host aliases
5. **GitHub CLI** - Multiple auth tokens
6. **Shell functions** - Optional gh-token-alias functions
7. **fedora-desktop integration** - Mention Ansible playbook for automatic setup

**Step 3.6: docs/troubleshooting.md**

Common issues and solutions:
- Container engine not found
- GitHub CLI authentication failed
- SSH key not working
- Permission denied errors
- Container build failures
- Token creation issues
- Session not persisting

### Phase 4: Testing (3-4 hours)

**Step 4.1: Local Testing**

Test on primary development system:
- Fresh install via `install.sh`
- Verify all commands work
- Test token creation
- Test session management
- Test custom Dockerfiles
- Test browser mode (if available)
- Uninstall and verify cleanup

**Step 4.2: Multi-Platform Testing**

Create test VMs or containers for:
- Ubuntu 24.04 (apt-based)
- Fedora 41 (dnf-based)
- Arch Linux (pacman-based)
- macOS (Homebrew)

Test matrix:
| Platform | Docker | Podman | Install Method | Status |
|----------|--------|--------|----------------|--------|
| Ubuntu 24.04 | ✓ | ✓ | curl \| bash | |
| Ubuntu 24.04 | ✓ | ✓ | git clone | |
| Fedora 41 | ✓ | ✓ | curl \| bash | |
| Arch Linux | ✓ | ✓ | git clone | |
| macOS Intel | ✓ | | curl \| bash | |
| macOS ARM | ✓ | | curl \| bash | |

**Step 4.3: Upgrade Testing**

Test upgrade path for fedora-desktop users:
1. Start with existing fedora-desktop CCY installation
2. Run standalone installer
3. Verify existing tokens still work
4. Verify existing sessions still work
5. Verify no duplicate aliases/commands

**Step 4.4: Automated Tests**

`tests/test-install.sh`:
```bash
#!/usr/bin/env bash
# Test installation in clean environment
# Verify all commands are available
# Test basic functionality
# Exit with status code
```

`tests/test-platforms.sh`:
```bash
#!/usr/bin/env bash
# Loop through platform containers
# Run test-install.sh in each
# Collect results
# Generate report
```

### Phase 5: Migration (1-2 hours)

**Step 5.1: Update fedora-desktop Playbook**

Option A: Install from standalone repo
```yaml
# play-install-claude-yolo.yml
- name: Install CCY from standalone repository
  shell: |
    bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)
  args:
    creates: /usr/local/bin/ccy
```

Option B: Keep local installation, add note
```yaml
# Add note at top of playbook
# NOTE: CCY is now available as standalone project
# Install from: https://github.com/USER/ccy
# This playbook remains for offline/local installations

# Existing tasks unchanged
```

**Step 5.2: Add Migration Notice**

In fedora-desktop README:
```markdown
## Claude Code YOLO (CCY)

**Note**: CCY is now available as a standalone project!
- Standalone repo: https://github.com/USER/ccy
- Works on any Linux/macOS system
- One-command installation

For fedora-desktop users, CCY will continue to be installed via the existing playbook.
```

**Step 5.3: Maintain Compatibility**

Ensure fedora-desktop integration continues working:
- Keep bashrc-includes/claude-yolo.bash.j2 template
- Keep setting CCY_CONTAINER_ENGINE from vars/container-defaults.yml
- Keep multi-account gh-token functions integration
- Keep same file paths and aliases

**Step 5.4: Update Documentation**

Update fedora-desktop docs to reference CCY standalone:
- Link to standalone repo for detailed CCY documentation
- Note that advanced features are documented in CCY repo
- Keep basic usage instructions in fedora-desktop docs

## Implementation Order

```
Day 1 (Morning): Phase 1 - Code Refactoring
  [X] Update error messages in all 7 files
  [X] Update documentation URLs
  [X] Simplify multi-account token logic
  [X] Make bashrc include paths configurable

Day 1 (Afternoon): Phase 2 - Installer (Part 1)
  [X] Write core installation logic
  [X] Platform detection
  [X] Prerequisite checks
  [X] GitHub CLI installation helper

Day 2 (Morning): Phase 2 - Installer (Part 2)
  [X] File installation logic
  [X] Shell integration
  [X] Container building
  [X] Post-install verification
  [X] Uninstaller script

Day 2 (Afternoon): Phase 3 - Documentation
  [X] README.md
  [X] INSTALL.md
  [X] docs/github-setup.md
  [X] docs/custom-dockerfiles.md
  [X] docs/multi-account.md (optional)

Day 3 (Morning): Phase 4 - Testing
  [X] Local testing
  [X] Multi-platform VMs
  [X] Test matrix execution

Day 3 (Afternoon): Phase 5 - Migration
  [X] Update fedora-desktop playbook
  [X] Add migration notices
  [X] Test compatibility
  [X] Update documentation
```

## Risk Assessment & Mitigation

### Risk 1: Breaking Existing fedora-desktop Users

**Likelihood**: Low
**Impact**: High
**Mitigation**:
- All user data paths unchanged (`~/.claude-tokens/ccy/`)
- All file paths unchanged (`/var/local/claude-yolo/`)
- All command aliases unchanged (`ccy`, `ccyb`)
- Ansible playbook can optionally use standalone installer
- Comprehensive upgrade testing before release

### Risk 2: Platform-Specific Installation Issues

**Likelihood**: Medium
**Impact**: Medium
**Mitigation**:
- Test on all major platforms before release
- Provide platform-specific troubleshooting docs
- Support manual installation as fallback
- Clear error messages for missing prerequisites

### Risk 3: GitHub CLI Installation Failures

**Likelihood**: Medium
**Impact**: Low
**Mitigation**:
- Detect gh CLI, offer to install
- Provide manual installation instructions
- Support scenario where user installs gh later
- Clear error messages with docs links

### Risk 4: Docker vs Podman Configuration

**Likelihood**: Low
**Impact**: Low
**Mitigation**:
- Auto-detect available container engine
- Allow user override via CCY_CONTAINER_ENGINE
- Document both Docker and Podman setup
- Test both engines on all platforms

### Risk 5: Incomplete Documentation

**Likelihood**: Medium
**Impact**: Medium
**Mitigation**:
- Comprehensive README with quick start
- Platform-specific installation guides
- Troubleshooting documentation
- Link to GitHub Discussions for support

## Success Criteria

### Must Have (v1.0.0)

- [ ] Installs successfully on Ubuntu, Fedora, Arch, macOS
- [ ] All existing CCY features work identically
- [ ] No breaking changes for fedora-desktop users
- [ ] GitHub CLI authentication works
- [ ] Container building works with Docker and Podman
- [ ] Token management works
- [ ] Session management works
- [ ] Custom Dockerfiles work
- [ ] Documentation complete and accurate

### Should Have (v1.1.0)

- [ ] Shell completions (bash, zsh)
- [ ] Automated tests run in CI
- [ ] macOS Apple Silicon tested
- [ ] Windows WSL2 support documented
- [ ] Homebrew formula (macOS)
- [ ] AUR package (Arch Linux)

### Could Have (v1.2.0)

- [ ] GUI installer option
- [ ] Integration with other git hosting (GitLab, Bitbucket)
- [ ] Non-container mode for restricted environments
- [ ] Plugin system for extensions

## Questions for User Decision

Before proceeding with implementation, the following decisions are needed:

### 1. Repository Name

**Options**:
- `ccy` (short, memorable)
- `claude-code-yolo` (descriptive, searchable)
- `claude-code-containers` (broader scope)

**Recommendation**: `ccy` - matches the command name, easier to type

### 2. Default Installation Location

**Options**:
- System-wide `/usr/local/lib/ccy` (requires sudo, standard)
- User-local `~/.local/lib/ccy` (no sudo, per-user)
- Both supported with `--user` flag

**Recommendation**: System-wide default, support `--user` flag

### 3. Multi-Account Support Stance

**Options**:
- Keep as core feature, document prominently
- Keep as optional advanced feature, document separately
- Simplify to single-account only, remove multi-account code

**Recommendation**: Keep as optional advanced feature (current approach)

### 4. Browser Mode (CCB) Distribution

**Options**:
- Included in main repo, installed by default
- Included in main repo, optional flag to install
- Separate repository (ccy-browser)

**Recommendation**: Included, optional flag to skip (current approach)

### 5. fedora-desktop Integration

**Options**:
- Update playbook to install from standalone repo
- Keep existing playbook, add note about standalone
- Remove CCY from fedora-desktop entirely

**Recommendation**: Keep existing playbook with note about standalone

### 6. License

**Options**:
- MIT (permissive, simple)
- Apache 2.0 (permissive, patent protection)
- GPL v3 (copyleft)
- Same as fedora-desktop (if applicable)

**Recommendation**: MIT - most permissive, easiest for users

## Next Steps After Approval

1. **Create GitHub repository** - Initialize with README and LICENSE
2. **Create feature branch** - `feature/standalone-extraction`
3. **Begin Phase 1** - Code refactoring
4. **Regular commits** - Small, atomic commits with clear messages
5. **Documentation updates** - As features are implemented
6. **Testing** - Continuous testing on local system
7. **Create PR** - For review before merging
8. **Tag v1.0.0** - First stable release
9. **Announce** - To fedora-desktop users and broader community

## Estimated Timeline

**Total: 12-15 hours (2 work days)**

- Phase 1 (Code Refactoring): 3-4 hours
- Phase 2 (Installer): 4-5 hours
- Phase 3 (Documentation): 3-4 hours
- Phase 4 (Testing): 3-4 hours
- Phase 5 (Migration): 1-2 hours

Can be spread across 1 week at ~2 hours/day, or completed in 2 intensive days.
