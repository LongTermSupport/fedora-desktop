# Feature Inventory - Fedora Desktop Repository

**Generated**: 2026-02-12
**Total Features**: 55+
**Purpose**: Complete inventory for documentation audit and prioritization

---

## SUMMARY STATISTICS

| Category | Count | Installation Type |
|----------|-------|------------------|
| Core Playbooks | 12 | Automatic |
| Optional Common Playbooks | 23 | Manual |
| Hardware-Specific Playbooks | 3 | Manual |
| Experimental Playbooks | 4 | Manual |
| Custom User Scripts | 7+ | Automated |
| System-wide Scripts/Tools | 4+ | Automated |
| GNOME Extensions | 2 | Automated |
| **TOTAL** | **55+** | Mixed |

---

## COMPLEXITY CLASSIFICATION

### Simple (20 features)
Installation/configuration only, minimal user interaction needed.

### Medium (18 features)
Advanced configuration, multiple components, some user decisions required.

### Complex (7 features)
Deep integration, dependencies, significant configuration, ongoing management.

---

## USER IMPACT CLASSIFICATION

### High Impact (19 features)
Core to development workflow or benefits most users.

### Medium Impact (15 features)
Specific use cases, targeted user groups.

### Low Impact (11 features)
Niche features, specialized hardware, experimental.

---

## CORE PLAYBOOKS (Automatically Installed)

### 1. Basic System Configuration
- **Location**: `playbooks/imports/play-basic-configs.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Customizable PS1 prompt (7 colour variants)
  - Vim Deus colorscheme
  - Essential packages (vim, wget, bash-completion, htop, etc.)
  - DNF parallel downloads (10 concurrent)
  - Automatic firmware updates
  - USB audio fix

### 2. Git Configuration and Tools
- **Location**: `playbooks/imports/play-git-configure-and-tools.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Global git configuration
  - Bash-git-prompt with Solarized theme
  - GitHub CLI (gh) with multi-account support
  - git-filter-repo for history rewriting
  - Global aliases: gu, gd, gs, c
  - Global .gitignore

### 3. Node.js/NVM Installation
- **Location**: `playbooks/imports/play-nvm-install.yml`
- **Complexity**: Simple
- **Impact**: High
- **Features**:
  - NVM v0.40.1
  - Node.js LTS installation
  - Automatic .bashrc/.bash_profile integration

### 4. Podman Container Runtime
- **Location**: `playbooks/imports/play-podman.yml`
- **Complexity**: Simple
- **Impact**: High
- **Features**:
  - Rootless by default
  - podman-compose (Docker Compose compatibility)
  - Docker CLI compatibility via socket

### 5. LXC Container Support
- **Location**: `playbooks/imports/play-lxc-install-config.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - LXC 4 from COPR
  - Network bridge (lxcbr0) with firewall
  - SSH key generation for containers
  - System limits optimization
  - Kernel modules for Docker/OpenVPN in LXC

### 6. systemd User Tweaks
- **Location**: `playbooks/imports/play-systemd-user-tweaks.yml`
- **Complexity**: Simple
- **Impact**: Medium

### 7. Microsoft Fonts
- **Location**: `playbooks/imports/play-ms-fonts.yml`
- **Complexity**: Simple
- **Impact**: Low

### 8. RPM Fusion Repositories
- **Location**: `playbooks/imports/play-rpm-fusion.yml`
- **Complexity**: Simple
- **Impact**: High

### 9. Git Security Hooks
- **Location**: `playbooks/imports/play-git-hooks-security.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Pre-commit scanning for secrets
  - Commit message validation
  - Private email detection
  - Username pattern detection

### 10. Toolbox Installation
- **Location**: `playbooks/imports/play-toolbox-install.yml`
- **Complexity**: Simple
- **Impact**: Medium

### 11. Claude Code CLI
- **Location**: `playbooks/imports/play-claude-code.yml`
- **Complexity**: Complex
- **Impact**: High
- **Features**:
  - Claude YOLO (CCY) - General development container
  - Claude Browser (CCB) - Browser automation with Playwright MCP
  - Token management (~/.claude-tokens/)
  - Custom Dockerfile support
  - Shared libraries (common, SSH, network, etc.)

### 12. GitHub CLI Multi-Account
- **Location**: `playbooks/imports/optional/common/play-github-cli-multi.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Multiple account support
  - SSH key per account
  - Account-specific aliases (gh-work, gh-personal)
  - Functions: gh-list, gh-whoami, gh-status, gh-switch
  - Direct cloning per account

---

## OPTIONAL PLAYBOOKS - COMMON

### 1. Python Development Environment
- **Location**: `playbooks/imports/optional/common/play-python.yml`
- **Complexity**: Complex
- **Impact**: High
- **Features**:
  - Pyenv with Python 3.11.13, 3.12.11, 3.13.1
  - PDM (Python Dependency Manager)
  - pipx for CLI tools
  - Hugging Face Hub CLI

### 2. Golang Development
- **Location**: `playbooks/imports/optional/common/play-golang.yml`
- **Complexity**: Simple
- **Impact**: High

### 3. Rust Development
- **Location**: `playbooks/imports/optional/common/play-rust-dev.yml`
- **Complexity**: Complex
- **Impact**: High
- **Features**:
  - Rustup toolchain manager
  - Components: rustfmt, clippy, rust-analyzer, rust-src, llvm-tools
  - Cargo tools: watch, edit, audit, outdated, expand, machete, nextest, deny, tarpaulin
  - System dependencies for common crates

### 4. VS Code
- **Location**: `playbooks/imports/optional/common/play-vscode.yml`
- **Complexity**: Simple
- **Impact**: High

### 5. Docker
- **Location**: `playbooks/imports/optional/common/play-docker.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Docker CE from official repository
  - Rootless setup
  - UID mapping configuration

### 6. Distrobox
- **Location**: `playbooks/imports/optional/common/play-install-distrobox.yml`
- **Complexity**: Simple
- **Impact**: Medium
- **Features**:
  - Container creation helpers
  - Playwright browser testing option

### 7. Speech-to-Text GNOME Extension ⭐
- **Location**: `playbooks/imports/optional/common/play-speech-to-text.yml`
- **Complexity**: Complex
- **Impact**: High
- **Features**:
  - faster-whisper with CUDA GPU acceleration
  - RealtimeSTT for streaming transcription
  - Model sizes: tiny, base, small, medium, large-v3
  - Language-specific transcription
  - Claude Code post-processing integration
  - Two prompt modes: corporate vs natural
  - ydotool for text insertion
  - GNOME Shell extension with GSettings
  - Prompt backup system
  - Icons: 🎤 recording, 🤖 processing, 💬 natural mode

### 8. GNOME Shell Extensions
- **Location**: `playbooks/imports/optional/common/play-gnome-shell-extensions.yml`
- **Complexity**: Medium
- **Impact**: High
- **Features**:
  - Blur my Shell (3193)
  - Vitals (1460) - system monitoring
  - AppIndicator Support (615)
  - Clipboard Indicator (779)
  - Just Perfection (3843)
  - Tiling Shell (7065)
  - Space Bar (5090)
  - Custom: workspace-names-overview

### 9. GNOME Shell Configuration
- **Location**: `playbooks/imports/optional/common/play-gnome-shell.yml`
- **Complexity**: Simple
- **Impact**: Medium

### 10. GSettings Configuration
- **Location**: `playbooks/imports/optional/common/play-gsettings.yml`
- **Complexity**: Medium
- **Impact**: Medium

### 11. Firefox
- **Location**: `playbooks/imports/optional/common/play-firefox.yml`
- **Complexity**: Simple
- **Impact**: Medium
- **Features**:
  - Firefox Profile Switcher Connector
  - Enterprise policies via /etc/firefox/policies/policies.json

### 12. Flatpak Applications
- **Location**: `playbooks/imports/optional/common/play-install-flatpaks.yml`
- **Complexity**: Simple
- **Impact**: Low-Medium
- **Apps**: Slack, Shotcut

### 13. Modern Terminal Emulators
- **Location**: `playbooks/imports/optional/common/play-install-terminal-emulators.yml`
- **Complexity**: Simple
- **Impact**: High
- **Options**:
  - Alacritty (GPU-accelerated, lowest latency)
  - Kitty (feature-rich, tabs, images)
  - Ghostty (GTK4 native, new Dec 2025)
  - Foot (Wayland-native, 21MB memory)

### 14. Lightweight IDEs
- **Location**: `playbooks/imports/optional/common/play-install-lightweight-ides.yml`
- **Complexity**: Simple
- **Impact**: Medium
- **IDEs**: Geany

### 15. Qobuz HD Audio Streaming
- **Location**: `playbooks/imports/optional/common/play-qobuz-cli.yml`
- **Complexity**: Complex
- **Impact**: Medium
- **Features**:
  - hifi-rs (Rust player)
  - qobuz-player (web interface)
  - rescrobbled (Last.fm scrobbling)
  - Shell functions: play(), hplay(), qplay()

### 16. LastPass CLI
- **Location**: `playbooks/imports/optional/common/play-lastpass.yml`
- **Complexity**: Medium
- **Impact**: Medium
- **Features**:
  - Single or multi-account support
  - Account-specific aliases (lpass-work, lpass-personal)
  - Status checking functions

### 17. VPN Configuration
- **Location**: `playbooks/imports/optional/common/play-vpn.yml`
- **Complexity**: Medium
- **Impact**: Medium
- **Features**: Wireguard, OpenVPN

### 18. Cloudflare WARP
- **Location**: `playbooks/imports/optional/common/play-cloudflare-warp.yml`
- **Complexity**: Simple
- **Impact**: Medium
- **Features**:
  - Zero-trust VPN
  - DNS over HTTPS with malware filtering

### 19. NordVPN/OpenVPN
- **Location**: `playbooks/imports/optional/common/play-nordvpn-openvpn.yml`
- **Complexity**: Medium
- **Impact**: Medium

### 20. Advanced Kernel Management
- **Location**: `playbooks/imports/optional/common/play-advanced-kernel-management.yml`
- **Complexity**: Complex
- **Impact**: Low

### 21. HD Audio & Bluetooth
- **Location**: `playbooks/imports/optional/common/play-hd-audio.yml`
- **Complexity**: Complex
- **Impact**: High
- **Features**:
  - Sample rates: 44.1kHz - 192kHz
  - Dynamic rate switching
  - PipeWire quantum optimization
  - Bluetooth codecs: LDAC, aptX, AAC
  - USB audio device optimization

### 22. Markless
- **Location**: `playbooks/imports/optional/common/play-install-markless.yml`
- **Complexity**: Simple
- **Impact**: Low

### 23. Fast File Manager
- **Location**: `playbooks/imports/optional/common/play-fast-file-manager.yml`
- **Complexity**: Simple
- **Impact**: Medium

### 24. GNOME Shell Development
- **Location**: `playbooks/imports/optional/common/play-gnome-shell-dev.yml`
- **Complexity**: Medium
- **Impact**: Low

---

## OPTIONAL PLAYBOOKS - HARDWARE-SPECIFIC

### 1. NVIDIA GPU Drivers
- **Location**: `playbooks/imports/optional/hardware-specific/play-nvidia.yml`
- **Complexity**: Complex
- **Impact**: High (for NVIDIA users)
- **Features**:
  - akmod-nvidia with CUDA
  - Hardware video acceleration
  - Vulkan support
  - Secure Boot MOK support
  - Testing tools: vulkaninfo, glxinfo, vainfo

### 2. DisplayLink Driver
- **Location**: `playbooks/imports/optional/hardware-specific/play-displaylink.yml`
- **Complexity**: Medium
- **Impact**: Low (DisplayLink users only)

### 3. Laptop Power Management
- **Location**: `playbooks/imports/optional/hardware-specific/play-laptop-lid-power-management.yml`
- **Complexity**: Simple
- **Impact**: Medium (laptop users)

---

## OPTIONAL PLAYBOOKS - EXPERIMENTAL

### 1. LXDE Desktop
- **Location**: `playbooks/imports/optional/experimental/play-lxde-install.yml`
- **Complexity**: Simple
- **Impact**: High (alternative desktop)
- **Status**: ⚠️ NOT TESTED

### 2. VirtualBox with Windows
- **Location**: `playbooks/imports/optional/experimental/play-virtualbox-windows.yml`
- **Complexity**: Complex
- **Impact**: Medium
- **Features**:
  - VirtualBox with DKMS
  - Windows key extraction from UEFI

### 3. Docker in LXC
- **Location**: `playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml`
- **Complexity**: Complex
- **Impact**: Low

### 4. Docker Overlay2 Migration
- **Location**: `playbooks/imports/optional/experimental/play-docker-overlay2-migration.yml`
- **Complexity**: Complex
- **Impact**: Low

---

## CUSTOM SCRIPTS & TOOLS

### User Scripts (~/.local/bin/)
1. **wsi-stream** - Real-time speech-to-text streaming
2. **wsi-stream-server** - Server mode for wsi-stream
3. **wsi-claude-process** - Claude post-processing for STT
4. **wsi** - Speech-to-text integration
5. **gshell-nested** - Nested GNOME Shell support
6. **nord** - NordVPN CLI helper
7. **git-account-helper** - Git account switching

### System Scripts (/var/local/)
1. **ps1-prompt** - Dynamic shell prompt
2. **colours** - Colour palette definitions
3. **claude-yolo/** - Complete CCY/CCB system
   - Main wrapper and Dockerfile
   - Shared libraries (9+ files)
   - Custom Dockerfile templates
   - Browser mode with Playwright MCP
   - chrome-ws CLI tool
4. **docker-in-lxc** - Docker support in LXC

### GNOME Extensions
1. **speech-to-text@fedora-desktop** - STT extension
2. **workspace-names-overview@fedora-desktop** - Workspace naming

### Bash Configuration
**File**: `/etc/profile.d/zz_lts-fedora-desktop.bash`
- Aliases: rm/cp/mv with -i, ll, dmesg -T, gti=git, vi=vim
- Docker Node functions: dnode, dnpm, dnpx, dyarn
- Shell options: histappend, cdspell, completion-ignore-case
- History: 20K file size, 10K memory
- Environment: EDITOR=vim, LESSCHARSET=utf-8

**User Includes** (~/.bashrc-includes/):
- shutdown-with-update.bash
- usb-audio-fix.bash
- claude-yolo.bash
- claude-browser.bash
- qobuz-players (optional)
- lastpass-aliases.inc.bash (optional)
- gh-aliases.inc.bash (optional)

---

## NOTES

⭐ = Recently enhanced (2026-02-12)
⚠️ = Experimental/Untested

This inventory represents a snapshot as of 2026-02-12. Features may be added, removed, or modified over time.
