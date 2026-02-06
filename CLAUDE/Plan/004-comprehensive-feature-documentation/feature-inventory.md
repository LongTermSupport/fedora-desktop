# Feature Inventory - Documentation Planning

This document provides a detailed inventory of all features identified for comprehensive documentation.

## 1. CCY (Claude Code YOLO)

**Location**: `files/var/local/claude-yolo/claude-yolo`
**Size**: 2,139 lines
**Version**: 3.1.8
**Type**: Bash wrapper script

**Key Features**:
- Containerised Claude Code execution with `--dangerously-skip-permissions`
- Isolated OAuth token storage (prevents conflicts with desktop Claude Code)
- Named token management with expiry tracking
- SSH key mounting for git operations
- GitHub CLI authentication support
- Custom Dockerfile support per project (`.claude/ccy/Dockerfile`)
- Docker network auto-detection and management
- Multiple container engine support (Podman/Docker)
- Interactive token creation and management
- Automatic container version checking and rebuilding

**Dependencies**:
- Modular library structure in `files/var/local/claude-yolo/lib/`:
  - `common.bash` - Core utilities
  - `token-management.bash` - OAuth token handling
  - `ssh-handling.bash` - SSH key selection and mounting
  - `network-management.bash` - Docker network operations
  - `dockerfile-custom.bash` - Custom Dockerfile handling
  - `ui-helpers.bash` - User interface utilities
  - `docker-health.bash` - Container health monitoring

**Deployment**: `playbooks/imports/optional/common/play-install-claude-yolo.yml`

---

## 2. CCB (Claude Code Browser)

**Location**: `files/var/local/claude-yolo/claude-browser`
**Size**: 1,494 lines
**Version**: 1.4.0
**Type**: Bash wrapper script (CCY variant)

**Key Features**:
- All CCY features PLUS:
- Playwright MCP integration for browser automation
- X11/GUI support for visible browser windows
- Shares OAuth tokens with CCY (same token storage)
- Separate project state management
- Browser automation workflows (scraping, testing, form filling)

**Use Cases**:
- Web scraping and data extraction
- Browser-based testing
- Automated form filling
- Interactive web automation

**Deployment**: Same playbook as CCY

---

## 3. Nord (NordVPN OpenVPN Manager)

**Location**: `files/home/.local/bin/nord`
**Size**: 635 lines
**Version**: 1.0.0
**Type**: Bash script

**Key Features**:
- NetworkManager CLI-based VPN management
- Interactive numbered menu chooser
- On-demand .ovpn config import
- Multiple connection management (connect/disconnect/switch)
- Persistent NetworkManager connections (visible in GNOME Settings)
- Automatic credential handling
- Connection status with public IP display
- Comprehensive logging system
- Fail-fast error handling

**Commands**:
- `nord` - Interactive mode with numbered menu
- `nord list` - List available .ovpn configs
- `nord connect <name>` - Connect to VPN
- `nord disconnect` - Disconnect current VPN
- `nord switch <name>` - Switch to different VPN
- `nord status` - Show connection status and public IP
- `nord cleanup` - Remove all nordvpn-* connections

**Architecture**:
```
User downloads .ovpn → Places in ~/.config/nordvpn/configs/
                    ↓
Nord script imports → NetworkManager CLI (nmcli)
                    ↓
NetworkManager GUI ← GNOME Settings integration
```

**Deployment**: `playbooks/imports/optional/common/play-nordvpn-openvpn.yml`

---

## 4. WSI-Stream (Speech-to-Text Streaming)

**Location**: `files/home/.local/bin/wsi-stream`
**Size**: 856 lines
**Version**: Python script
**Type**: Real-time speech-to-text transcription

**Key Features**:
- GPU-accelerated Whisper transcription (faster-whisper)
- Real-time streaming transcription (transcribes DURING recording)
- Instant paste on stop (no transcription wait time)
- Streaming buffer file for live monitoring
- DBus integration with GNOME extension
- Optional Claude Code post-processing
- RealtimeSTT library integration
- Automatic cleanup on exit
- PID file management

**Workflow**:
```
1. Press Insert → Start recording
2. Speak → Real-time transcription to buffer
3. Press Insert → Instantly paste final text
```

**Integration**: Works with GNOME Shell extension for system-wide activation

**Deployment**: `playbooks/imports/optional/common/play-speech-to-text.yml`

---

## 5. GNOME Speech-to-Text Extension

**Location**: `extensions/speech-to-text@fedora-desktop/`
**Type**: GNOME Shell extension (JavaScript)

**Key Features**:
- System-wide voice typing with Insert key
- Visual recording indicator
- DBus communication with backend scripts
- State management (IDLE/RECORDING/TRANSCRIBING)
- Integration with WSI-Stream backend

**Components**:
- `extension.js` - Main extension logic
- `metadata.json` - Extension metadata
- `schemas/` - GSettings schema for configuration

**User Experience**:
- Press Insert key anywhere in GNOME
- Visual indicator shows recording state
- Speak naturally
- Press Insert again → Text automatically pasted

**Deployment**: Same playbook as WSI-Stream

---

## 6. GitHub CLI Multi-Account

**Location**: `playbooks/imports/optional/common/play-github-cli-multi.yml`
**Type**: Ansible playbook + bash helper functions

**Key Features**:
- Multiple GitHub account management
- Separate SSH keys per account
- SSH config automation
- Bash helper functions for account switching
- Account-specific git operations

**Helper Functions Generated**:
- `gh-list` - List all configured accounts
- `gh-whoami` - Show currently active account
- `gh-status` - Check authentication status for all accounts
- `gh-switch <account>` - Switch to specific account
- `github-test-ssh` - Test SSH connections for all accounts
- `gh-<account> <command>` - Run gh command as specific account
- `clone-<account> owner/repo` - Clone repo using account SSH key
- `remote-<account> owner/repo` - Set git remote for account
- `gh-token-<account>` - Get GitHub token for account
- `gh-<account>-make-default` - Set account as default

**Configuration**:
- Account definitions in `environment/localhost/host_vars/localhost.yml`
- SSH keys: `~/.ssh/github_<alias>` and `~/.ssh/github_<alias>.pub`
- SSH config: `~/.ssh/config` (separate blocks per account)
- Bash functions: `~/.bashrc-includes/gh-aliases.inc.bash`

**Deployment**: Run playbook with account list prompt

---

## 7. Distrobox Integration

**Location**: `playbooks/imports/optional/common/play-install-distrobox.yml`
**Type**: Container-based development environments

**Key Features**:
- Pre-configured development containers
- Seamless integration with host system
- GUI application support
- Package manager isolation
- Multiple distro support

**Use Cases**:
- Development in different distros
- Isolated package management
- Testing across distributions
- Legacy software compatibility

**Deployment**: Playbook installs distrobox and creates configured containers

---

## Documentation Priorities

### High Priority (Production-Ready, High Impact)
1. **CCY** - Most complex, most powerful, highest user value
2. **Nord** - Solves real problem (VPN without proprietary app)
3. **Speech-to-Text** - Unique feature, GPU acceleration, system-wide

### Medium Priority (Significant Value)
4. **CCB** - Extends CCY for browser automation use cases
5. **GitHub Multi-Account** - Common need, elegant solution

### Lower Priority (Nice to Have)
6. **Distrobox** - Well-documented upstream, less custom work
7. Other minor utilities

---

## Documentation Structure Template

Each feature documentation should include:

### 1. Overview
- What problem does it solve?
- Key benefits
- When to use it

### 2. Installation
- Prerequisites
- Playbook command
- Post-installation verification

### 3. Configuration
- Configuration files and locations
- Available options
- Customisation examples

### 4. Usage
- Basic usage patterns
- Common workflows
- Command reference (if CLI tool)
- Examples with expected output

### 5. Architecture (for complex features)
- Component diagram
- Workflow illustration
- Integration points

### 6. Troubleshooting
- Common issues and solutions
- Debug techniques
- Log locations
- How to get help

### 7. Advanced Topics (optional)
- Performance tuning
- Advanced configurations
- Integration with other tools

---

## Success Metrics

Documentation will be considered successful when:
- Users can install and use features without reading source code
- Common questions are answered in troubleshooting section
- Examples are clear and can be copy-pasted
- Architecture is understandable at a glance
- Users can debug issues themselves using provided information
