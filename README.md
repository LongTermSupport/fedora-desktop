# Fedora Desktop Configuration Manager

Transform your fresh Fedora installation into a fully-configured development powerhouse in minutes, not hours.

## What is This?

An Ansible-based automation project that eliminates the tedious manual setup of a new Fedora workstation. Run one command, grab a coffee, and come back to a system ready for serious development work.

**Perfect for:**
- Developers who want consistent environments across machines
- System administrators managing multiple Fedora workstations
- Anyone who's tired of manually reinstalling tools after a fresh OS install
- Teams wanting standardized development setups

## Why Use This?

Instead of spending hours:
- Installing packages one by one
- Configuring Git, SSH, and development tools
- Setting up Docker, LXC, and container environments
- Tweaking bash prompts and shell configurations
- Installing fonts, codecs, and system utilities

You run a single command and get:
- A reproducible, version-controlled system configuration
- Automatic dependency management
- Secure vault-encrypted secrets
- Optional components you can enable as needed
- Battle-tested configurations used in production

## Quick Start

### One-Command Installation

On a fresh Fedora installation, run this as your regular user:

```bash
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))
```

That's it. The script will:
1. Verify your Fedora version matches this branch
2. Install Ansible and dependencies
3. Configure GitHub CLI and SSH keys
4. Clone this repository to `~/Projects/fedora-desktop`
5. Run the main configuration playbook

**Time required:** 10-30 minutes depending on your internet connection.

### Before You Run It

Make sure you have:
- [ ] A fresh Fedora installation (check version compatibility below)
- [ ] Enabled third-party repositories during Fedora installation
- [ ] Logged in as your regular user (not root)
- [ ] A stable internet connection

Need more details? See the full [Installation Guide](docs/installation.md).

## Version Compatibility

**Current branch targets: Fedora 42**

This project uses version-specific branches:
- `F42` - Fedora 42 (current)
- `F43` - Fedora 43 (future)
- Each branch is maintained separately

The bootstrap script automatically verifies your Fedora version matches the branch. If you're running a different version, checkout the appropriate branch or wait for it to be created after the next Fedora release.

## What You Get

### Automatically Installed

The main playbook configures these essentials without any interaction:

**System Foundations**
- Optimized DNF configuration (10 parallel downloads, fastest mirror)
- Essential packages (vim, wget, htop, bash-completion, ripgrep)
- Microsoft fonts for document compatibility
- RPM Fusion repositories (free and non-free)

**Development Environment**
- Git with bash-git-prompt (Solarized theme)
- GitHub CLI (gh) for repository management
- Node.js 20 via NVM
- Claude Code CLI
- JetBrains Toolbox

**Container Platform**
- LXC with networking configured
- SSH keys for container access
- Firewall rules for container networking

**Shell Experience**
- Custom bash prompt with error state indicators
- Enhanced history (20K lines)
- Docker helper functions
- Passwordless sudo for your user

### Optional Add-Ons

Choose what you need from these curated playbooks:

**Containerization** ([learn more](docs/containerization.md))
- Docker (rootless) for application containers
- Distrobox for seamless development environments
- Docker-in-LXC for isolated project testing
- [Playwright testing environment](docs/containerization.md#playwright-distrobox-automated) (automated browser setup)

**Programming Languages**
- Python with pyenv, PDM, Hugging Face tools
- Go compiler and tools
- Rust toolchain

**IDEs & Editors**
- VS Code with Microsoft repository
- PyCharm Community (via Toolbox)
- Enhanced Vim configuration (already included)

**Hardware Support**
- NVIDIA proprietary drivers
- DisplayLink dock support
- HD audio configuration (192kHz, LDAC, aptX HD)
- TLP battery optimization (laptops)

**Productivity Tools**
- Flatpak applications (Slack, etc.)
- Firefox with profile switcher
- LastPass CLI
- VPN clients (WireGuard, Cloudflare WARP)

See [Playbooks Reference](docs/playbooks.md) for the complete list with usage examples.

## Documentation

Comprehensive guides are available in the [docs/](docs/) directory:

- **[Installation Guide](docs/installation.md)** - Step-by-step setup instructions
- **[Playbooks Reference](docs/playbooks.md)** - Complete list of what you can install
- **[Configuration Guide](docs/configuration.md)** - Customize your setup
- **[Containerization Guide](docs/containerization.md)** - LXC vs Docker vs Distrobox explained
- **[Architecture Overview](docs/architecture.md)** - How the project is structured
- **[Development Guide](docs/development.md)** - Contributing and development workflow

**Quick links:**
- Stuck? Check [Troubleshooting](docs/installation.md#troubleshooting)
- Want to add features? See [Configuration Guide](docs/configuration.md#adding-custom-configurations)
- Running optional playbooks? See [Playbooks Reference](docs/playbooks.md#running-optional-playbooks)

## Project Philosophy

This project follows these core principles:

**Fail Fast** - Errors stop execution immediately with clear messages
**YAGNI** - Only include what's actually needed, keep it simple
**DRY** - Don't repeat yourself, extract common patterns
**Idempotent** - Safe to run multiple times, same result every time
**Security First** - Vault-encrypted secrets, no credentials in version control

Read more about these principles in [CLAUDE.md](CLAUDE.md) if you're contributing.

## Contributing

Contributions are welcome! See the [Development Guide](docs/development.md) for:
- Setting up a development environment
- Project structure and Ansible patterns
- Creating new playbooks
- Testing and debugging procedures
- Pull request guidelines

**Quick contribution checklist:**
- [ ] Test on fresh Fedora installation
- [ ] Verify idempotency (run twice, no changes second time)
- [ ] Follow the Ansible style guide
- [ ] Update relevant documentation
- [ ] Don't commit secrets (use Ansible Vault)

## Support & Community

- **Bug reports:** [GitHub Issues](https://github.com/LongTermSupport/fedora-desktop/issues)
- **Questions:** [GitHub Discussions](https://github.com/LongTermSupport/fedora-desktop/discussions)
- **Documentation:** [docs/README.md](docs/README.md)

## License

MIT License - see [LICENSE](LICENSE) file for details.

---

**Note:** This is a public repository. Never commit personal information, API keys, or secrets. Use Ansible Vault for sensitive data. See the [security guidelines](CLAUDE.md#-public-repository-warning) for details.
