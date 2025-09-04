# Fedora Desktop

Automated configuration management for Fedora desktop environments, transforming a fresh installation into a fully-configured development workstation.

## Overview

This project uses Ansible to automate the setup of a Fedora desktop system with development tools, customizations, and optional components. A bootstrap script handles initial setup, then Ansible playbooks configure the system according to your preferences.

## Documentation

Comprehensive documentation is available in the [docs/](docs/) directory:

- **[Installation Guide](docs/installation.md)** - Getting started and installation methods
- **[Architecture Overview](docs/architecture.md)** - Project structure and execution flow
- **[Playbooks Reference](docs/playbooks.md)** - Available playbooks and their functions
- **[Configuration Guide](docs/configuration.md)** - Customization and settings
- **[Development Guide](docs/development.md)** - Contributing and development workflow

For a complete index, see [docs/README.md](docs/README.md)

## Quick Start

### Prerequisites

1. **Install Fedora** - Target version for this branch is defined in `vars/fedora-version.yml`
2. **Enable third-party repositories** during installation (required for many packages)
3. **Encrypt root filesystem** (strongly recommended)
4. **Create your user account** and log in

## Fedora Version Branching Strategy

This repository uses a branching strategy where each Fedora version has its own branch:

- **Branch Naming**: `F<VERSION>` (e.g., `F42`, `F43`)
- **Default Branch**: Updated to the latest Fedora version being worked on
- **Version Configuration**: Each branch has its target Fedora version defined in `vars/fedora-version.yml`

### Branch Lifecycle

- **Active Development**: Latest Fedora version branch
- **Maintenance**: Previous version branches receive critical fixes only
- **Archive**: Older branches are kept for reference but not actively maintained

This repo is in active development and is generally updated shortly after each Fedora release.


### Installation

Once Fedora is installed and you're logged in as your regular user, run:

```
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))
```

This will:
- Install dependencies (git, ansible, python3)
- Configure GitHub CLI and SSH keys
- Clone this repository
- Run the main configuration playbook

## Features

### Core Components (Automatic)
- System package installation and DNF optimization
- Bash environment with custom prompt and Git integration
- Development tools (Git, ripgrep, GitHub CLI)
- Node.js via NVM
- Claude Code CLI
- LXC container support
- Microsoft fonts
- RPM Fusion repositories
- JetBrains Toolbox

### Optional Components
Additional features can be installed as needed:
- Docker and container tools
- Programming languages (Python, Go)
- VS Code and development IDEs
- Audio enhancements
- Hardware drivers (NVIDIA, DisplayLink)
- VPN clients
- And more...

See [Playbooks Documentation](docs/playbooks.md) for the complete list.

## Contributing

See the [Development Guide](docs/development.md) for information on:
- Setting up a development environment
- Project structure and conventions
- Creating new playbooks
- Testing and debugging
- Submitting pull requests

## Support

- [GitHub Issues](https://github.com/LongTermSupport/fedora-desktop/issues)
- [Documentation](docs/README.md)

## License

This project is open source under the MIT License. See [LICENSE](LICENSE) file for details.
