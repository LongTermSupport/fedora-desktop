# CCY Standalone Extraction Plan

## Overview
Extract CCY from fedora-desktop into a standalone repository that works on any Linux/macOS system with Docker/Podman installed.

**GitHub dependency**: KEPT (acceptable requirement)
**Target users**: Anyone with Docker/Podman, not just Fedora users

## Required Changes Summary

- **20 references** to update across 7 files
- **3 documentation URLs** to replace
- **1 standalone installer** to create
- **4 documentation files** to write

---

## Code Changes Required

### 1. ssh-handling.bash (6 references)

| Line | Current | Replace With |
|------|---------|--------------|
| 14-16 | Comments about `play-github-cli-multi.yml` | "These keys follow GitHub multi-account pattern: `~/.ssh/github_<alias>`" |
| 27 | "managed by play-github-cli-multi.yml" | "following github_<alias> naming pattern" |
| 80 | `ansible-playbook playbooks/.../play-github-cli-multi.yml` | "See: https://github.com/USER/ccy/blob/main/docs/github-setup.md" |
| 126 | `ansible-playbook playbooks/.../play-github-cli-multi.yml` | "Visit: https://github.com/settings/keys to add your SSH key" |
| 136 | `ansible-playbook playbooks/.../play-git-configure-and-tools.yml` | "Install GitHub CLI: https://cli.github.com/" |
| 164-181 | Error about missing gh-token function | Simplify: just fall back to `gh auth token` without mentioning playbooks |

**Recommendation**: The multi-account token functions (`gh-token-<alias>`) are a fedora-desktop feature. For standalone:
- Keep trying to load them if they exist (backwards compat)
- Always fall back gracefully to `gh auth token`
- Document how to set up multi-account manually (optional feature)

### 2. claude-yolo (5 references)

| Line | Current | Replace With |
|------|---------|--------------|
| 801 | `ansible-playbook .../play-docker.yml` | "Enable rootless Docker: https://docs.docker.com/engine/security/rootless/" |
| 975 | `ansible-playbook .../play-install-claude-yolo.yml` | "Rebuild container: ccy --rebuild-container" |
| 989 | `ansible-playbook .../play-install-claude-yolo.yml` | "Rebuild container: ccy --rebuild-container" |
| 1567 | `https://github.com/LongTermSupport/fedora-desktop/blob/main/docs/containerization.md` | `https://github.com/USER/ccy/blob/main/docs/custom-dockerfiles.md` |
| 1591 | Same as 1567 | Same replacement |

### 3. claude-browser (5 references)

Same pattern as claude-yolo above:

| Line | Current | Replace With |
|------|---------|--------------|
| 676 | `ansible-playbook .../play-docker.yml` | "Enable rootless Docker: https://docs.docker.com/engine/security/rootless/" |
| 746 | `ansible-playbook .../play-install-claude-yolo.yml` | "Rebuild container: ccyb --rebuild-container" |
| 760 | Same as 746 | Same replacement |
| 1325 | fedora-desktop docs URL | ccy docs URL |
| 1349 | Same as 1325 | Same replacement |

### 4. dockerfile-custom.bash (2 references)

| Line | Current | Replace With |
|------|---------|--------------|
| 101 | `ansible-playbook .../play-install-claude-yolo.yml` | "Reinstall: bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)" |
| 525 | fedora-desktop docs URL | ccy docs URL |

### 5. entrypoint.sh (2 references)

| Line | Current | Replace With |
|------|---------|--------------|
| 47 | "ensure play-github-cli-multi.yml is configured" | "ensure you've authenticated: gh auth login" |
| 82 | `ansible-playbook .../play-github-cli-multi.yml` | "Setup: https://github.com/USER/ccy/blob/main/docs/github-setup.md" |

---

## New Files to Create

### 1. install.sh (Standalone Installer)

**Purpose**: One-command installation for any Linux/macOS system

**Features**:
- Detect OS and package manager (apt, dnf, yum, brew, pacman)
- Check prerequisites: git, docker/podman
- Offer to install GitHub CLI if missing
- Copy files to installation directory (default: `/usr/local/lib/ccy`)
- Symlink executables to `/usr/local/bin/ccy` and `/usr/local/bin/ccyb`
- Build container images
- Run post-install verification

**Installation methods**:
```bash
# Method 1: Clone and install
git clone https://github.com/USER/ccy.git
cd ccy
sudo ./install.sh

# Method 2: One-liner (recommended)
bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)
```

**Installation locations**:
- Scripts: `/usr/local/lib/ccy/` (or `~/.local/lib/ccy` for user install)
- Binaries: `/usr/local/bin/{ccy,ccyb}` (or `~/.local/bin/` for user install)
- Dockerfiles: `/usr/local/lib/ccy/docker/`
- Libs: `/usr/local/lib/ccy/lib/`

### 2. README.md (Main Documentation)

**Sections**:
1. What is CCY?
2. Features
3. Quick Start
4. Prerequisites
   - Docker or Podman (rootless recommended)
   - Git
   - GitHub CLI (`gh`)
   - GitHub account with SSH key
5. Installation
6. Basic Usage
7. Advanced Features
8. Troubleshooting
9. Contributing
10. License

### 3. docs/github-setup.md

**Content**:
- Installing GitHub CLI on various platforms
- Running `gh auth login`
- Creating SSH keys
- Registering SSH keys with GitHub
- Multi-account setup (optional)
- Testing authentication

### 4. docs/custom-dockerfiles.md

**Content**: (Replace fedora-desktop containerization.md)
- What are custom Dockerfiles?
- When to use them
- Creating project-specific Dockerfile
- Using templates
- Examples for common stacks (Ansible, Golang, Python, etc.)

---

## Installation Script Pseudocode

```bash
#!/bin/bash
# CCY Installer - Works on Linux and macOS

set -e  # Fail fast

# Configuration
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local}"  # Allow override
CCY_LIB_DIR="$INSTALL_PREFIX/lib/ccy"
CCY_BIN_DIR="$INSTALL_PREFIX/bin"

# Detect if we need sudo
SUDO=""
if [ "$EUID" -ne 0 ] && [ "$INSTALL_PREFIX" = "/usr/local" ]; then
    SUDO="sudo"
fi

# Prerequisite checks
check_prerequisites() {
    echo "Checking prerequisites..."

    # Check git
    if ! command -v git &>/dev/null; then
        echo "ERROR: git not found. Install: <package manager specific>"
        exit 1
    fi

    # Check container engine
    if ! command -v docker &>/dev/null && ! command -v podman &>/dev/null; then
        echo "ERROR: Neither docker nor podman found."
        echo "Install Docker: https://docs.docker.com/get-docker/"
        echo "Or Podman: https://podman.io/getting-started/installation"
        exit 1
    fi

    # Check GitHub CLI (offer to install)
    if ! command -v gh &>/dev/null; then
        echo "WARNING: GitHub CLI (gh) not found"
        echo "CCY requires 'gh' for authentication"
        echo ""
        read -p "Install GitHub CLI now? (y/N): " install_gh

        if [ "$install_gh" = "y" ] || [ "$install_gh" = "Y" ]; then
            install_github_cli
        else
            echo "Install manually: https://cli.github.com/"
            exit 1
        fi
    fi
}

# Install GitHub CLI based on platform
install_github_cli() {
    if [ "$(uname)" = "Darwin" ]; then
        brew install gh
    elif command -v apt &>/dev/null; then
        curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
            sudo dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg
        sudo chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
            sudo tee /etc/apt/sources.list.d/github-cli.list > /dev/null
        sudo apt update
        sudo apt install gh
    elif command -v dnf &>/dev/null; then
        sudo dnf install -y gh
    elif command -v pacman &>/dev/null; then
        sudo pacman -S github-cli
    else
        echo "Could not auto-install. Install manually: https://cli.github.com/"
        exit 1
    fi
}

# Install CCY files
install_files() {
    echo "Installing CCY to $CCY_LIB_DIR..."

    # Create directories
    $SUDO mkdir -p "$CCY_LIB_DIR"/{bin,lib,docker/templates}

    # Copy files (from either cloned repo or downloaded tarball)
    $SUDO cp -r bin/* "$CCY_LIB_DIR/bin/"
    $SUDO cp -r lib/* "$CCY_LIB_DIR/lib/"
    $SUDO cp -r docker/* "$CCY_LIB_DIR/docker/"

    # Update shebang paths in scripts to reference install location
    $SUDO sed -i "s|^LIB_DIR=.*|LIB_DIR=\"$CCY_LIB_DIR/lib\"|" "$CCY_LIB_DIR/bin/ccy"
    $SUDO sed -i "s|^LIB_DIR=.*|LIB_DIR=\"$CCY_LIB_DIR/lib\"|" "$CCY_LIB_DIR/bin/ccyb"

    # Create symlinks
    $SUDO ln -sf "$CCY_LIB_DIR/bin/ccy" "$CCY_BIN_DIR/ccy"
    $SUDO ln -sf "$CCY_LIB_DIR/bin/ccyb" "$CCY_BIN_DIR/ccyb"

    # Set permissions
    $SUDO chmod +x "$CCY_LIB_DIR/bin"/*
}

# Build container images
build_containers() {
    echo "Building container images..."

    # Detect container engine
    if command -v docker &>/dev/null; then
        CONTAINER_ENGINE="docker"
    else
        CONTAINER_ENGINE="podman"
    fi

    cd "$CCY_LIB_DIR/docker"

    # Build base image
    echo "Building claude-yolo:latest..."
    $CONTAINER_ENGINE build -t claude-yolo:latest .

    # Build browser variant
    if [ -f Dockerfile.browser ]; then
        echo "Building claude-yolo-browser:latest..."
        $CONTAINER_ENGINE build -f Dockerfile.browser -t claude-yolo-browser:latest .
    fi
}

# Post-install verification
verify_installation() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "Installation complete!"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "✓ CCY installed to: $CCY_LIB_DIR"
    echo "✓ Executables: ccy, ccyb"
    echo ""
    echo "Next steps:"
    echo "  1. Authenticate with GitHub: gh auth login"
    echo "  2. Set up SSH key: ssh-keygen -t ed25519 -f ~/.ssh/github_main"
    echo "  3. Add key to GitHub: https://github.com/settings/keys"
    echo "  4. Test: cd <your-repo> && ccy --help"
    echo ""
    echo "Documentation: https://github.com/USER/ccy"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# Main installation flow
main() {
    check_prerequisites
    install_files
    build_containers
    verify_installation
}

main "$@"
```

---

## Migration Strategy

### For Existing fedora-desktop Users

**Zero breaking changes** - CCY continues to work exactly as before:

1. Token storage remains: `~/.claude-tokens/ccy/`
2. Session storage remains: `.claude/ccy/`
3. Container names unchanged: `claude-yolo:latest`
4. Command-line interface identical
5. Multi-account setup (if configured) continues working

**Update playbook** to install from standalone repo:

```yaml
# playbooks/imports/optional/common/play-install-claude-yolo.yml
- name: Install Claude Code YOLO
  hosts: desktop
  tasks:
    - name: Download and run CCY installer
      shell: |
        curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh | bash
      args:
        creates: /usr/local/bin/ccy
```

**Or keep local playbook** that copies from local files for offline installs.

### For New Users (Non-fedora-desktop)

**Simple installation**:

```bash
# One command install
bash <(curl -fsSL https://raw.githubusercontent.com/USER/ccy/main/install.sh)

# Setup GitHub
gh auth login

# Create SSH key
ssh-keygen -t ed25519 -f ~/.ssh/github_main

# Add to GitHub
cat ~/.ssh/github_main.pub
# Copy and paste at: https://github.com/settings/keys

# Use it
cd your-project/
ccy --help
ccy --ssh-key ~/.ssh/github_main
```

---

## Testing Checklist

### Platforms to Test

- [ ] Fedora (existing users - ensure no breakage)
- [ ] Ubuntu 24.04
- [ ] Debian 12
- [ ] Arch Linux
- [ ] macOS (Intel)
- [ ] macOS (Apple Silicon)

### Features to Test

- [ ] Fresh install via install.sh
- [ ] Container building
- [ ] Token creation
- [ ] SSH key detection
- [ ] GitHub authentication
- [ ] Session management
- [ ] Custom Dockerfiles
- [ ] Multi-account setup (optional)
- [ ] Update from previous version

---

## Timeline Estimate

- **Code changes**: 2-3 hours (20 references to update)
- **Installer script**: 3-4 hours (robust error handling, multi-platform)
- **Documentation**: 2-3 hours (README + 3 docs files)
- **Testing**: 2-3 hours (multiple platforms)
- **Total**: 9-13 hours (~2 work days)

---

## Repository Setup

### GitHub Repository

**Name**: `ccy` or `claude-code-yolo`
**Description**: "Containerized Claude Code environment with GitHub integration"
**Topics**: `claude-code`, `ai-coding`, `docker`, `github`, `development-environment`

### Initial Release

**v1.0.0** - First standalone release
- Extract from fedora-desktop
- Multi-platform support
- Comprehensive documentation
- Installer script

### Backwards Compatibility

**Maintain compatibility** with fedora-desktop:
- Keep same file paths in containers
- Keep same token/session structure
- Keep same environment variables
- Fedora-desktop playbook can optionally pull from new repo

---

## Next Steps

1. ✅ Review and approve this plan
2. Create GitHub repository
3. Update code references (20 changes)
4. Write installer script
5. Write documentation
6. Test on multiple platforms
7. Create initial release (v1.0.0)
8. Update fedora-desktop playbook to reference new repo
9. Announce to users

---

## Questions for Decision

1. **Repository name**: `ccy` (short) or `claude-code-yolo` (descriptive)?
2. **Installation prefix**: `/usr/local/lib/ccy` (system) or `~/.local/lib/ccy` (user)?
3. **Backwards compat**: Keep optional fedora-desktop Ansible playbook in `contrib/`?
4. **Multi-account**: Document as optional advanced feature or simplify for standalone?
5. **License**: Same as fedora-desktop or different?
