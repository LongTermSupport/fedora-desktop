# Playbooks Reference

Complete catalog of available features and how to use them.

> **Scope**: this catalogues the playbooks you **provision a machine with** —
> `playbooks/playbook-main.yml` and everything under `playbooks/imports/`.
>
> `playbooks/dev/` is **deliberately excluded**. Those are repo-development playbooks that
> operate on this repository rather than configuring your desktop — e.g.
> `play-collect-diagnostics.yml`, which gathers a host diagnostic snapshot into
> `untracked/diagnostics/` so the playbooks themselves can be audited. They are documented in
> [playbooks/dev/CLAUDE.md](../playbooks/dev/CLAUDE.md).
>
> This note exists so an absence here reads as a **decision**. A playbook missing from this
> catalogue without one has historically meant it was forgotten, not excluded.

## Quick Navigation

**Just installed?** Start with [Optional Playbooks](#optional-playbooks) to see what you can add.

**Creating your own?** See [Creating Custom Playbooks](#creating-custom-playbooks) for templates.

**Common tasks:**

- [Podman (default engine)](#play-podmanyml) - Rootless container engine
- [Set up Distrobox](#play-distroboxyml) - Seamless dev environments
- [Python development](#play-pythonyml) - pyenv and PDM

## Desktop vs. Headless Server Provisioning

This repo provisions **both** a Fedora desktop and a headless Fedora server from
the same playbooks. Which one you get is **auto-detected with zero flags** — the
`provisioning_profile` variable is computed from `systemctl get-default`:

- `graphical.target` → **desktop** (all playbooks run)
- anything else (`multi-user.target`, a lookup failure, an unrecognised target) →
  **server** (GUI-only playbooks and GUI-only tasks auto-skip)

Detection is **server-biased when uncertain**: only a confirmed graphical target
is treated as a desktop, so a headless box never tries to run GNOME-only tasks.

```bash
# Zero-flag default — auto-detects desktop vs. server:
ansible-playbook playbooks/playbook-main.yml

# Force a profile (testing / CI / an atypical box):
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=server
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=desktop
```

**Every playbook is safe to run standalone.** Each GUI-oriented play self-guards,
so running one on its own auto-detects and cleanly no-ops on a server:

```bash
ansible-playbook playbooks/imports/play-firefox.yml
# runs fully on a desktop; ends immediately (guard) on a server

ansible-playbook playbooks/imports/play-docker.yml
# general play — runs on both desktop and server
```

How a play is classified (`scope: general | gnome | server`) and the self-guard
mechanism are documented in `CLAUDE/AnsibleStyle.md` ("Provisioning Profile
Self-Guard"). On a server, GUI-only plays end via their guard and a handful of
`general` plays skip their one or two GUI-only tasks (e.g. `play-vpn.yml`'s
`NetworkManager-openvpn-gnome`, `play-basic-configs.yml`'s USB-audio fix).

## Core Playbooks (Automatically Run)

These playbooks are executed automatically by `playbook-main.yml` during initial installation. They run in the order listed below. Do **not** run them manually — just run `playbook-main.yml`.

### play-AA-preflight-sanity.yml

**Purpose**: Validates system requirements\
**Actions**:

- Verifies Ansible version >= 2.9.9
- Confirms Fedora distribution and version
- Ensures system matches target in `vars/fedora-version.yml`

### play-AB-dnf-upgrade.yml

**Purpose**: DNF upgrade — refresh and upgrade all packages\
**Actions**:

- Refreshes DNF metadata
- Upgrades all installed packages to latest versions

### play-basic-configs.yml

**Purpose**: Essential system configuration\
**Actions**:

- Installs basic packages (vim, wget, bash-completion, htop, etc.)
- Configures passwordless sudo for user
- Sets up Vim with Deus colorscheme
- Configures bash environment and custom PS1 prompt
- Copies SSH keys to root user
- Installs YQ binary
- Optimizes DNF with parallel downloads
- Configures hardware settings

### play-prevent-ssh-suspend.yml

**Purpose**: Prevent suspend during SSH sessions\
**Actions**:

- Configures systemd inhibitor so active SSH sessions block suspend

### play-network-wait-tuning.yml

**Purpose**: Mask `NetworkManager-wait-online.service`\
**Actions**:

- Masks the unit outright, so boot neither stalls on it nor ends with it in a `failed` state
- Writes an `NM_ONLINE_TIMEOUT=5` drop-in that is **inert while masked** — it applies only if
  someone later unmasks the unit, capping the stall at 5s instead of the 30s default
- Clears any stale `failed` record left by earlier boots (`systemctl reset-failed`)

> **Superseded behaviour**: the first version of this play (Plan 00044) only capped the timeout
> to 5s. That reduced the stall but the unit still *failed* on every Wi-Fi-only boot — the cap
> fires **as** the failure exit rather than avoiding it. Plan 00053 replaced the cap with the
> mask. To reverse: `sudo systemctl unmask NetworkManager-wait-online.service`.

### play-mask-intel-lpmd.yml

**Purpose**: Mask `intel_lpmd.service` where it only produces boot noise\
**Actions**:

- Probes for the unit and masks it **only if present** — a no-op on hosts without it (e.g. AMD),
  which is why it can be imported unconditionally
- Removes the recurring `Open /proc/sys/kernel/sched_itmt_enabled failed` error from
  `journalctl -b -p err` on Intel hosts whose kernel does not expose that knob

> On a TuneD-managed desktop, `tuned-ppd` already owns the GNOME Power Mode panel, so masking
> `intel_lpmd` does not change the user-visible power model. To reverse:
> `sudo systemctl unmask intel_lpmd.service`.

### play-systemd-user-tweaks.yml

**Purpose**: Systemd user service tweaks\
**Actions**:

- Enables lingering for user systemd services (survive logout)
- Applies user-level systemd configuration adjustments

### play-nvm-install.yml

**Purpose**: Node.js environment setup\
**Actions**:

- Installs Node Version Manager (NVM)
- Installs Node.js version 20
- Sets up bash integration

### play-git-configure-and-tools.yml

**Purpose**: Git environment configuration\
**Actions**:

- Configures git user name and email
- Installs bash-git-prompt with Solarized theme
- Sets up ripgrep for fast searching
- Installs GitHub CLI (gh)
- Note: SSH keys are generated by run.bash as ~/.ssh/id (Ed25519)
- Configures SSH for LXC containers

### play-git-hooks-security.yml

**Purpose**: Configure git security hooks\
**Actions**:

- Deploys project-level git hooks via `core.hooksPath`
- Scans staged files and commit messages for secrets/sensitive patterns

### play-firefox.yml

**Purpose**: Firefox with enterprise policies\
**Actions**:

- Firefox browser from DNF
- **Firefox Profile Switcher Connector** for multi-profile management
- **Enterprise policies** via `/etc/firefox/policies/policies.json`

**What policies control**:

- Default homepage and search engine
- Extension installation sources
- Privacy and security settings
- Update behaviour
- Developer tools access

**Customizing policies**: Edit `files/etc/firefox/policies/policies.json` in repository, re-run playbook

### play-github-cli-multi.yml

**Purpose**: Multi-account GitHub CLI management\
**Actions**:

- **Multiple GitHub accounts** (work, personal, open-source, etc.)
- **SSH keys per account** with account-specific configuration
- **Bash helper functions** for seamless account switching
- **Account-specific operations** (clone, remote setup, gh commands)

**Setup process**:

1. Authenticate each account **with the required scopes** via `./scripts/gh-account-setup.bash` (do **not** use a bare `gh auth login` — it misses the scopes the playbook audits for)
2. Run the playbook to deploy SSH config and regenerate the bash helper functions
3. SSH keys are generated and uploaded per account automatically
4. Bash functions are available after `source ~/.bashrc`

> Full workflow, commands, scopes, and troubleshooting: **[GitHub Multi-Account Management](github-multi-account.md)**.

**Available functions**:

```bash
gh-list                    # List all configured accounts
gh-whoami                  # Show current active account
gh-status                  # Check authentication status
gh-switch work             # Switch to work account
github-test-ssh            # Test SSH for all accounts

# Account-specific commands (example with 'work')
gh-work pr list            # Run gh CLI as work account
clone-work owner/repo      # Clone with work account SSH
remote-work owner/repo     # Set git remote for work account
gh-token-work              # Get GitHub token
gh-work-make-default       # Set as default account
```

**Configuration files**:

- Account definitions: `environment/localhost/host_vars/localhost.yml`
- SSH keys: `~/.ssh/github_<alias>` (per account)
- SSH config: `~/.ssh/config` (separate host blocks)
- Bash functions: `~/.bashrc-includes/gh-aliases.inc.bash`

**Adding new accounts**: `./scripts/gh-account-setup.bash --add=alias:username` then re-run the playbook — see [GitHub Multi-Account Management](github-multi-account.md#adding-a-new-account-the-important-workflow)

**Example workflow**:

```bash
# Work on company project
gh-switch work
clone-work company/private-repo
cd private-repo
gh-work pr create

# Switch to personal project
gh-switch personal
clone-personal myusername/hobby-project
cd hobby-project
gh-personal issue list
```

### play-ms-fonts.yml

**Purpose**: Microsoft fonts installation\
**Actions**:

- Installs Windows-compatible fonts
- Configures font rendering

### play-rpm-fusion.yml

**Purpose**: Third-party repository setup\
**Actions**:

- Enables RPM Fusion free and non-free repositories
- Required for many multimedia codecs and proprietary software

### play-browsers.yml

**Purpose**: Additional browsers\
**Actions**:

- Installs Chromium and other browsers from DNF/Flatpak

### play-toolbox-install.yml

**Purpose**: JetBrains Toolbox installation\
**Actions**:

- Downloads and installs JetBrains Toolbox
- Configures desktop integration

### play-docker.yml

**Purpose**: Docker container platform (rootful — compatibility engine for DDEV)\
**Actions**:

- Removes any previous rootless Docker setup (legacy cleanup)
- Adds Docker CE repository
- Installs Docker CE and tools
- Configures rootful system daemon (`docker.service` / `docker.socket`)
- Adds user to `docker` group (root-equivalent; deliberate for DDEV compatibility)
- See [Containerization Guide](containerization.md) for rootful vs rootless rationale

> **Note**: Docker runs as a rootful system daemon. `docker` group membership is root-equivalent by design. See `CLAUDE/ContainerEngines.md` for the full security trade-off analysis.

### play-lxc-install-config.yml

**Purpose**: LXC full-system containers\
**Actions**:

- Installs LXC from the `ganto/lxc4` Copr, plus `lxc-templates` and the play's declared
  dependencies (`firewalld`, `python3-firewall`, `dnsmasq`, `iptables-nft`, `NetworkManager`)
- Configures container networking
- Sets up SSH configuration for containers
- Configures firewall rules
- Runs **after** `play-docker.yml` so the DOCKER-USER iptables chain exists before LXC reconciles outbound connectivity

### play-podman.yml

**Purpose**: Install Podman and podman-compose (default container engine)\
**Actions**:

- Installs Podman (rootless, daemonless)
- Installs podman-compose for compose-file workflows
- Default engine for CCY and all new container work
- See [Containerization Guide](containerization.md) for Podman vs Docker role split

### play-python.yml

**Purpose**: Python development environment\
**Actions**:

- **pyenv** for Python version management
- **Python versions**: 3.11.13 (LTS), 3.12.11 (stable), 3.13.1 (latest)
- **PDM** (Python Dependency Manager) for modern dependency management
- **pipx** for isolated CLI tool installations
- **Hugging Face Hub CLI** for ML model management
- Development dependencies: SDK headers, compression libs, cryptography support

**What you get**:

```bash
# Switch Python versions
pyenv versions
pyenv global 3.12.11

# PDM workflow
pdm init
pdm add requests
pdm install

# Isolated tools
pipx install black
pipx install ruff
```

**Installed Python versions**:

- 3.11.13 - Long-term support (recommended for production)
- 3.12.11 - Current stable (best balance)
- 3.13.1 - Latest features (experimental)

**Package managers**:

- pip - Standard (pre-installed with Python)
- PDM - Modern, fast, PEP-compliant (recommended for new projects)
- pipx - For installing CLI tools in isolation

### play-claude-yolo.yml

**Purpose**: Claude Code containerised environment (CCY)\
**Actions**:

- **CCY (YOLO Mode)**: General-purpose development container with browser automation built in
- Unified token management
- Custom Dockerfile support per project
- Proper isolation with container home directories
- Uses Podman by default (rootless); Docker override available
- See [Containerisation Guide](containerization.md) for full details

> **Note**: `play-claude-yolo.yml` must run before `play-claude-code.yml` — the `cc` wrapper sources CCY lib files that the claude-code playbook asserts are present.

**What's installed**:

- git, gh, ripgrep, jq, yq, vim, python, Node.js 20, Claude Code
- **agent-browser CLI**: Token-efficient browser automation via Chromium (no Playwright needed)

**Key features of agent-browser**:

- 93% context reduction for multi-page flows vs traditional DOM inspection
- Headed mode support (`--headed` flag) for visual debugging
- Reference-based selection (`@e1`, `@e2`) from compact accessibility snapshots

**Usage**:

```bash
# Create a token
ccy --create-token

# General development (browser automation included)
cd ~/Projects/my-project
ccy

# Inside CCY — browser automation ready to use
agent-browser --help              # Comprehensive built-in docs
agent-browser --headed open https://example.com
agent-browser snapshot -i         # Get @refs for elements
agent-browser click @e5           # Click using reference
agent-browser fill @e3 "test"     # Fill form fields
```

**Token efficiency example**:

```bash
# Traditional DOM inspection: 100,000+ tokens for 5-page flow
# agent-browser: ~8,000 tokens for same flow (93% reduction!)
```

**Custom Dockerfiles**:

```bash
# AI-guided customisation (comprehensive planning)
ccy --custom-docker

# Quick template-based customisation
ccy --custom
```

### play-claude-code.yml

**Purpose**: Claude Code CLI installation\
**Actions**:

- Downloads and installs Claude Code binary
- Deploys the `cc` wrapper (sources CCY token-management lib)
- Configures system-wide access

### play-comms.yml

**Purpose**: Communication applications\
**Actions**:

- Enables Flathub repository
- Installs Slack

### play-gnome-shell.yml & play-gnome-shell-extensions.yml

**Purpose**: GNOME desktop customization and extensions

**System extensions**:

- **dash-to-dock**: Application dock with customization

**Third-party extensions** (via gnome-shell-extension-installer):

- **Blur my Shell** (3193): Blur effects for panels and overview
- **Vitals** (1460): System monitoring (CPU, memory, temperature)
- **AppIndicator Support** (615): System tray icons
- **Clipboard Indicator** (779): Clipboard history manager
- **Just Perfection** (3843): Customize GNOME Shell behaviour
- **Tiling Shell** (7065): Window tiling and snapping
- **Space Bar** (5090): Workspace navigation enhancements

**Custom extensions**:

- **workspace-names-overview**: Show workspace names in overview

**What you get**:

- Enhanced window management (tiling)
- System monitoring in top bar
- Clipboard history access
- Visual enhancements (blur effects)
- Better workspace navigation

### play-markless.yml

**Purpose**: Install Markless — terminal-based Markdown viewer\
**Actions**:

- Installs Markless for rendering Markdown in the terminal

### play-terminal-emulators.yml

**Purpose**: Modern high-performance terminal emulators optimized for Claude Code\
**Actions**:

- **Alacritty**: GPU-accelerated, lowest input latency, OpenGL rendering
- **Kitty**: Feature-rich with native tabs, image protocol, ligature support
- **Ghostty**: New GTK4 terminal (v1.0 Dec 2025), zero config, hundreds of themes
- **Foot**: Wayland-native minimalist, 21MB memory, CPU rendering, server/client mode

**Comparison**:

| Terminal  | GPU | Latency | Features | Memory | Best For                |
| --------- | --- | ------- | -------- | ------ | ----------------------- |
| Alacritty | ✅  | Lowest  | Minimal  | ~50MB  | Speed, responsiveness   |
| Kitty     | ✅  | Low     | Rich     | ~80MB  | Power users, features   |
| Ghostty   | ❌  | Low     | Balanced | ~40MB  | GTK integration, themes |
| Foot      | ❌  | Medium  | Minimal  | ~20MB  | Wayland, efficiency     |

All terminals support:

- True colour (24-bit)
- Fast rendering
- Excellent Claude Code performance
- Customizable via config files

**Usage**:

```bash
# Launch your preferred terminal
alacritty
kitty
ghostty
footclient  # or 'foot' for standalone
```

### play-vscode.yml

**Purpose**: Visual Studio Code installation\
**Actions**:

- Adds Microsoft's official Fedora repository
- Installs VS Code with GPG key verification
- Latest stable version

**Recommended extensions** (install via VS Code):

- Python: ms-python.python
- Rust: rust-lang.rust-analyzer
- Go: golang.go
- GitLens: eamodio.gitlens
- Claude Code: Anthropic's official extension

### play-vpn.yml

**Purpose**: VPN client setup\
**Actions**:

- **WireGuard** tools and NetworkManager integration
- **OpenVPN** firewall rules
- Network profile importing

**Usage**:

```bash
# Import WireGuard config
nmcli connection import type wireguard file vpn-config.conf

# Connect
nmcli connection up vpn-name

# Disconnect
nmcli connection down vpn-name
```

### play-gsettings.yml

**Purpose**: Desktop settings configuration\
**Actions**:

- Applies custom GNOME settings via gsettings

### play-ZZ-repo-cleanup.yml

**Purpose**: Repository cleanup — remove orphaned third-party repos / COPRs\
**Actions**:

- Removes stale or orphaned DNF repository files added by earlier playbooks
- Runs last (ZZ prefix) to clean up after all other plays

## Optional Playbooks

Run these manually as needed after the main installation completes.

**General syntax**:

```bash
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/imports/optional/<category>/<playbook>.yml
```

**Note:** Most optional playbooks don't require `--ask-become-pass` as they use sudo internally. If prompted for a password, just enter your sudo password.

### Common Optional Features

Popular add-ons for development work:

#### play-advanced-kernel-management.yml

Auto-retain previous minor kernel version:

- Keeps prior kernel available for rollback after upgrades

#### play-claude-devtools.yml

Claude DevTools (ccdt) — on-demand session viewer:

- Installs `ccdt` for inspecting Claude Code session logs
- See [docs/features/claude-devtools.md](features/claude-devtools.md) for full details

#### play-clean-paste.yml

Clean Paste — Ctrl+Alt+V clipboard sanitiser:

- Strips formatting and hidden characters from clipboard content before pasting

#### play-cloudflare-dns.yml

Cloudflare encrypted DNS — DNS-over-TLS with malware filtering, no client:

- Points `systemd-resolved` at `1.1.1.2` / `1.0.0.2` (Cloudflare's malware-blocking
  "for Families" tier) over DNS-over-TLS, with certificate verification
- Replaces the only capability this host ever used WARP for — see below

#### play-cloudflare-warp.yml

**Removes** Cloudflare WARP. This play is an uninstaller, not an installer:

- The stable `cloudflare-warp` RPM hard-requires `webkit2gtk3`, which Fedora retired in F44
  (libsoup2 is EOL). The package is uninstallable **and blocks the F43 → F44 distupgrade**
  (`nothing provides webkit2gtk3 needed by cloudflare-warp-…`).
- Its install branch is intentionally dead, gated behind `cloudflare_warp_uninstall: true`.
  Flip that only once Cloudflare ships an F44-compatible RPM **and** you want the client back.
- The encrypted, malware-filtered DNS it used to provide is now supplied natively by
  `play-cloudflare-dns.yml`, with no client and no `webkit2gtk3`.

#### play-collaboration.yml

Collaboration tools:

- Installs team collaboration and screen-sharing applications

#### play-compression-helpers.yml

Compression helpers — installs `compress` and `uncompress` commands:

- Provides legacy UNIX compress/uncompress utilities

#### play-container-watch.yml

Container process watchdog — **reporting only, it never kills or throttles anything**:

- Deploys the `containerwatch` helper, its CLI wrapper, and a `systemd --user` timer that runs
  periodic scans
- Installs a GNOME Shell panel extension that surfaces the findings
- Writes a `report.json` and emits a DBus signal; taking action is left to you
- Its no-kill guarantee is enforced by a dedicated QA gate — see
  [CLAUDE/QA.md](../CLAUDE/QA.md)

#### play-darktable-ai-appimage.yml

Install darktable AI nightly (AppImage, with Sony A7V support):

- Deploys darktable AI nightly build as an AppImage with Sony A7V denoise profiles

#### play-darktable-ai-build.yml

Build and install darktable RPM with AI features (USE_AI=ON):

- Compiles darktable from source with AI scene-referred processing enabled

#### play-ddev.yml

DDEV local development environment:

- Installs DDEV for PHP/WordPress/Drupal local dev
- Requires rootful Docker (`play-docker.yml` — core)
- See [docs/ddev.md](ddev.md) for full setup guide

#### play-disk-reclaim.yml

Disk reclaim — disk-usage analysers plus the `reclaim` cleanup TUI:

- Installs `ncdu`, `duf` and `trash-cli`; adds the `baobab` GUI analyser on desktops only
- Deploys `reclaim`, a dependency-light bash menu that reports what is using disk and runs
  targeted cleanups (dnf autoremove/clean, old kernels, journal vacuum, container prune,
  flatpak/cache/trash) — **each behind an explicit confirmation**

#### play-distrobox.yml

Distrobox installation:

- Installs distrobox package
- Provides seamless container integration for development
- Enables running GUI apps from other distros
- Auto-shares home directory with containers
- See [Containerization Guide](containerization.md) for comparison with LXC/Docker

#### play-fast-file-manager.yml

Configure fast file manager and optimize file picker performance:

- Installs and configures a fast file manager
- Tunes GTK file-picker portal for snappier response
- See [docs/fast-file-manager.md](fast-file-manager.md) for details

#### play-ftp-camera.yml

Install camera FTP server (Sony A7V):

- Deploys an FTP server for tethered shooting from Sony cameras

#### play-gnome-shell-dev.yml

Install GNOME Shell development tools:

- Development dependencies for writing and testing GNOME Shell extensions

#### play-golang.yml

Go programming language:

- Golang compiler and standard tools from DNF repositories
- Latest stable version for Fedora 44

**What you get**:

```bash
go version
go build
go test
go mod init
```

#### play-hd-audio.yml

High-fidelity audio system:

- **HD sample rate support**: 44.1kHz, 48kHz, 88.2kHz, 96kHz, 176.4kHz, **192kHz**
- **Dynamic rate switching**: Automatic based on active audio streams
- **PipeWire optimization**: Quantum tuning (32-8192) for low latency
- **Bluetooth codecs**: LDAC (HQ), aptX, aptX-HD, AAC, SBC-XQ
- **USB audio**: Special handling with larger buffers for DACs
- **High-quality resampling**: Quality level 10

**What you get**:

- Studio-quality audio playback up to 192kHz/24-bit
- LDAC codec for wireless headphones (990kbps)
- Optimized latency for music production
- Better Bluetooth headphone compatibility (controller mode: bredr)

**For audiophiles**:

- Works with external DACs
- Supports high-resolution audio files (FLAC, DSD)
- Professional music production capabilities
- Low-latency monitoring

#### play-image-watermarking.yml

Image watermarking — installs `watermark` command (ImageMagick 7 + exiftool):

- Batch image watermarking with EXIF metadata preservation

#### play-lastpass.yml

LastPass CLI password manager:

- LastPass command-line interface
- **Single or multi-account** support
- Account-specific aliases (lpass-work, lpass-personal)

**Setup** (prompted during playbook):

- Single account: Simple setup
- Multiple accounts: Define aliases (e.g., `work,personal`)

**Usage**:

```bash
# Single account
lpass login user@example.com
lpass show github
lpass logout

# Multi-account
lpass-work login work@example.com
lpass-personal login you@example.com
lpass-status  # Check all accounts
```

#### play-lightweight-ides.yml

Install lightweight IDEs:

- Installs compact editor/IDE options as alternatives to VS Code

#### play-network-tools.yml

Network discovery tools:

- Installs nmap, arp-scan, and other network diagnostic utilities

#### play-nordvpn-openvpn.yml

NordVPN OpenVPN manager:

- Installs openvpn and NetworkManager-openvpn packages
- Deploys `nord` helper for interactive OpenVPN connection management
- Uses NordVPN service credentials (stored encrypted in `localhost.yml`)
- See [docs/nordvpn-installation.md](nordvpn-installation.md) for setup guide

#### play-open-command.yml

`open` — one command to open any file, directory, or URL:

- Deploys `~/.local/bin/open`, plus the MIME/viewer stack it delegates to
  (`xdg-utils`, `perl-File-MimeInfo`, `shared-mime-info`, `desktop-file-utils`,
  `file`, `fzf`, `less`, `chafa`, `w3m`, `poppler-utils`, `bsdtar`, `jq`, `tree`)
- Uses the registered default app when there is one — same as `xdg-open`
- Shows a chooser when there is **not** one, or the file type is unrecognised:
  fzf if available, otherwise a plain numbered menu (`-a` forces it either way)
- Never offers a GUI app when there is no display — over SSH or on a headless
  server you get terminal viewers, and only ones actually installed
- `open .` (file manager or directory listing), `open -l FILE` (what could open
  this?), `open -d FILE` (choose, and remember it as the default), `open -t FILE`
  (terminal handlers only), `open -n FILE` (print the command, run nothing)
- Works on desktop and server (`scope: general`). Fedora ships no `/usr/bin/open`
  and `~/.local/bin` comes first in PATH, so nothing is masked

#### play-photography.yml

Photography tools:

- Installs RAW processors, colour management utilities, and camera tethering tools

#### play-qobuz.yml

Qobuz apps — gapless HD audio streaming from Qobuz:

- **hifi-rs**: Rust-based Qobuz CLI player
- **QBZ**: Native full-featured hi-fi Qobuz **GUI** player (Rust/Slint, no
  webview), installed as a Flatpak from Flathub (`com.blitzfc.qbz`). Launches
  from the app grid. Bit-perfect DAC passthrough / exclusive mode works — the
  Flatpak manifest grants `--device=all`, `--socket=pulseaudio`, and PipeWire
  access.
- **rescrobbled**: Last.fm scrobbling systemd service

**Shell functions**:

```bash
play [album/track]    # Play a Qobuz URL with hifi-rs
hplay [album/track]   # Alias for play
qobuz_status          # Show hifi-rs status
```

**Features**:

- High-resolution audio streaming (up to 24-bit/192kHz)
- Last.fm scrobbling integration

#### play-rclone.yml

Install and configure rclone with cloud storage mounts:

- Installs rclone and configures cloud storage remotes (e.g. Google Drive, S3)
- Sets up systemd mount units for persistent cloud mounts

#### play-remote-desktop-toggle.yml

Remote desktop quick toggle:

- Deploys a helper to enable/disable GNOME remote desktop (RDP/VNC) on demand

#### play-rust-dev.yml

Rust development environment:

- **Rustup** toolchain manager for Rust version management
- **Stable toolchain** with automatic updates
- **Essential components**: rustfmt, clippy, rust-analyzer, rust-src, llvm-tools-preview
- **cargo-binstall** for faster binary installations
- **20+ Cargo tools** for development workflow

**Cargo tools installed**:

- `cargo-watch` - Auto-rebuild on file changes
- `cargo-edit` - Add/remove/upgrade dependencies from CLI
- `cargo-audit` - Security vulnerability scanning
- `cargo-outdated` - Check for outdated dependencies
- `cargo-expand` - Expand macros (debugging)
- `cargo-machete` - Find unused dependencies
- `cargo-nextest` - Next-generation test runner
- `cargo-deny` - Dependency linting
- `cargo-tarpaulin` - Code coverage

**What you get**:

```bash
# Rust toolchain
rustc --version
cargo --version
rustfmt --version
cargo clippy --version

# Development workflow
cargo new my-project
cd my-project
cargo watch -x run          # Auto-rebuild
cargo clippy                # Linting
cargo audit                 # Security check
cargo nextest run           # Fast testing
```

**Cargo configuration optimizations**:

- Parallel jobs optimized for your CPU
- Git fetch with shallow clones
- Sparse registry for faster updates

**System dependencies included**: GCC, CMake, OpenSSL, SQLite, PostgreSQL, MySQL development libraries for common crate compilation

#### play-speech-to-text.yml

GPU-accelerated speech-to-text with AI enhancement:

- **faster-whisper** with NVIDIA CUDA GPU acceleration
- **RealtimeSTT** for real-time streaming transcription
- **Model sizes**: tiny, base, small, medium, large-v3
- **Claude Code integration**: Professional text formatting (corporate/natural modes)
- **GNOME Shell extension** with keyboard shortcuts
- **Auto-paste**: Text types automatically at cursor

**Keyboard shortcuts**:

- **Insert**: Record and transcribe (raw)
- **Ctrl+Insert**: Record with corporate AI processing 🤖
- **Alt+Insert**: Record with natural AI processing 💬

**Requirements**: NVIDIA GPU with drivers installed (`play-nvidia.yml`)

**See comprehensive guide**: [Speech-to-Text Documentation](features/speech-to-text.md)

#### play-unifi-controller.yml

Deploy UniFi Network Controller for seamless WiFi roaming (Podman Compose):

- Runs the UniFi controller in a rootless Podman container
- Manages WiFi access point adoption and roaming configuration

#### play-videography.yml

Videography tools:

- Installs video editing and transcoding tools (Kdenlive, FFmpeg extras, etc.)

### Hardware-Specific

#### play-darktable-ai-gpu.yml

Install GPU ONNX Runtime for darktable AI:

- Installs CUDA/OpenCL ONNX runtime libraries for GPU-accelerated darktable AI processing
- **Requires**: `play-darktable-ai-appimage.yml` or `play-darktable-ai-build.yml`

#### play-displaylink.yml

DisplayLink dock support:

- Installs DisplayLink drivers
- Creates suspend/resume service
- Configures display management

#### play-ipu6-webcam.yml

Install Intel IPU6 webcam userspace stack:

- Deploys userspace drivers for Intel IPU6 (Alder Lake / Raptor Lake) built-in webcams

#### play-laptop-lid-power-management.yml

Laptop lid power management:

- Configures systemd-logind lid-close action (suspend/hibernate/ignore)

#### play-laptop-thermal-diagnostics.yml

Laptop thermal management cleanup and diagnostic tools:

- Removes conflicting thermal daemons
- Installs `thermald`, `turbostat`, `s-tui` and other thermal diagnostics

#### play-musiccast.yml

MusicCast controllers (experimental) — SSDP diagnostics and gyrc GUI:

- Installs SSDP discovery tools and the gyrc GUI for Yamaha MusicCast devices

#### play-nvidia.yml

NVIDIA GPU drivers:

- Installs proprietary NVIDIA drivers
- Configures kernel modules

### Experimental

#### play-docker-in-lxc-support.yml

Configure Docker-in-LXC support:

- Configures host for Docker inside LXC containers
- Loads kernel modules (overlay, br_netfilter)
- Configures sysctl for IP forwarding
- Installs `docker-in-lxc` command for project-based containers
- **Requires**: `play-lxc-install-config.yml` (core)
- See [Containerization Guide](containerization.md) for advanced use cases

**Usage**:

```bash
cd ~/Projects/my-docker-project
docker-in-lxc --create  # Create LXC for this project
docker-in-lxc --enter   # Enter the container
# Inside: docker-compose up
```

#### play-docker-overlay2-migration.yml

Migrate Docker to native overlay2:

- Migrates an existing Docker installation from legacy storage driver to native overlay2

#### play-lxde-install.yml

LXDE desktop (experimental):

- Installs LXDE as alternative lightweight desktop environment

#### play-virtualbox-windows.yml

Install VirtualBox:

- Installs VirtualBox
- Configures Windows VM support
- Sets up ACPI tools

#### play-virtualbox-windows-vm-setup.yml

Build the Windows 11 VM itself (download + import + interactive-use tuning):

- Split out of `play-virtualbox-windows.yml` (Plan 00061) — that play installs the **engine**,
  this one provisions the **VM**
- `scope: gnome` — it provisions a GUI Windows desktop workflow, so it ends immediately on a
  server profile
- Run the engine play first:

```bash
./playbooks/imports/optional/experimental/play-virtualbox-windows.yml
./playbooks/imports/optional/experimental/play-virtualbox-windows-vm-setup.yml
```

### Archived

#### play-tlp-battery-optimisation.yml

Laptop power management (deprecated):

- TLP battery optimization
- Note: Conflicts with newer power-profiles-daemon

## Running Optional Playbooks

```bash
# Install communication applications (this is core — runs automatically)
# No need to run play-comms.yml manually

# Install Distrobox
ansible-playbook playbooks/imports/optional/common/play-distrobox.yml

# Install NVIDIA drivers
ansible-playbook playbooks/imports/optional/hardware-specific/play-nvidia.yml

# Set up DDEV (requires Docker, which is core)
ansible-playbook playbooks/imports/optional/common/play-ddev.yml

# Advanced: Docker-in-LXC support
ansible-playbook playbooks/imports/optional/experimental/play-docker-in-lxc-support.yml
```

## Creating Custom Playbooks

Place custom playbooks in `playbooks/imports/optional/` following the naming convention `play-<feature>.yml`.

Template:

```yaml
- hosts: desktop
  name: Your Feature Name
  become: true  # If root required
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
  vars_files:
    - "{{ root_dir }}/vars/fedora-version.yml"  # If version-specific
  tasks:
    - name: Your task here
      package:
        name: your-package
        state: present
```
