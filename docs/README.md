# Documentation Index

Complete guide to the Fedora Desktop Configuration Manager.

## New Here? Start Here

**First time setup:**
1. Read [Installation Guide](installation.md) - Get up and running
2. Browse [Playbooks Reference](playbooks.md) - See what optional features you can add
3. Check [Configuration Guide](configuration.md) - Learn how to customize

**Quick answers:**
- "How do I install this?" → [One-command installation](installation.md#quick-install)
- "What does it install?" → [Core playbooks](playbooks.md#core-playbooks-automatically-run)
- "How do I add Docker?" → [Optional playbooks](playbooks.md#optional-playbooks)
- "What's the difference between LXC, Docker, and Distrobox?" → [Containerization Guide](containerization.md#overview-comparison)

## Documentation by Purpose

### I want to install the system

**[Installation Guide](installation.md)**

Everything you need to get started:
- Prerequisites and system requirements
- One-command installation walkthrough
- What happens during installation
- Manual installation steps
- Common installation issues and fixes

**Time to read:** 5 minutes | **Time to install:** 10-30 minutes

---

### I want to add optional features

**[Playbooks Reference](playbooks.md)**

Complete catalog of available features:
- Core features (automatic)
- Development tools (Docker, Python, Go, VS Code)
- Container platforms (LXC, Docker, Distrobox)
- Hardware support (NVIDIA, DisplayLink, audio)
- Desktop enhancements (GNOME, Firefox, VPN)
- How to run optional playbooks
- Creating custom playbooks

**Time to read:** 10 minutes | **Quick reference format**

---

### I want to understand containerization options

**[Containerization Guide](containerization.md)**

Comprehensive comparison and usage guide:
- LXC vs Docker vs Distrobox comparison table
- When to use each technology
- Installation and configuration
- Real-world usage examples
- Docker-in-LXC for isolated development
- Troubleshooting container issues

**Time to read:** 15 minutes | **Includes decision tree**

---

### I want to customize my setup

**[Configuration Guide](configuration.md)**

Learn how to make it yours:
- User configuration (name, email, vault)
- System configuration (DNF, bash, SSH, Git)
- Optional feature configuration
- Ansible patterns and best practices
- Adding custom playbooks and files
- Debugging configuration issues

**Time to read:** 10 minutes | **Reference guide**

---

### I want to understand how it works

**[Architecture Overview](architecture.md)**

Deep dive into project structure:
- Directory structure and organization
- Execution flow (bootstrap → main → optional)
- Configuration management patterns
- Variable hierarchy
- File management
- Security model

**Time to read:** 8 minutes | **For contributors and curious minds**

---

### I want to contribute or modify

**[Development Guide](development.md)**

Everything for contributors:
- Development environment setup
- Branching strategy (version-specific branches)
- Ansible style guide and patterns
- Testing and debugging procedures
- Contribution workflow
- Pull request guidelines
- Security considerations

**Time to read:** 12 minutes | **Essential for contributors**

## Common Tasks Quick Reference

### Installation & Setup
```bash
# One-command install (fresh Fedora)
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))

# Re-run main playbook
cd ~/Projects/fedora-desktop
ansible-playbook playbooks/playbook-main.yml --ask-become-pass

# Check what's installed
ansible desktop -m setup | grep ansible_distribution
```

### Adding Optional Features
```bash
cd ~/Projects/fedora-desktop

# Install Docker (rootless)
ansible-playbook playbooks/imports/optional/common/play-docker.yml

# Install Distrobox
ansible-playbook playbooks/imports/optional/common/play-install-distrobox.yml

# Install Python environment
ansible-playbook playbooks/imports/optional/common/play-python.yml

# Add VS Code
ansible-playbook playbooks/imports/optional/common/play-vscode.yml
```

### Configuration Management
```bash
# View encrypted settings
ansible-vault view environment/localhost/host_vars/localhost.yml

# Edit encrypted settings
ansible-vault edit environment/localhost/host_vars/localhost.yml

# Check DNF optimization
grep max_parallel /etc/dnf/dnf.conf

# Test playbook without applying changes
ansible-playbook playbook.yml --check
```

### Containerization
```bash
# LXC: Create container
sudo lxc-create -n mycontainer -t download -- -d ubuntu -r jammy -a amd64
sudo lxc-start -n mycontainer

# Docker: Run service
docker run -d -p 8080:80 nginx

# Distrobox: Create dev environment
distrobox create --name dev --image ubuntu:22.04
distrobox enter dev
```

## Project File Structure

```
~/Projects/fedora-desktop/
├── docs/                          # Documentation (you are here)
│   ├── README.md                  # This index
│   ├── installation.md            # Setup guide
│   ├── playbooks.md               # Feature catalog
│   ├── configuration.md           # Customization guide
│   ├── containerization.md        # Container tech comparison
│   ├── architecture.md            # Technical deep dive
│   └── development.md             # Contributor guide
│
├── playbooks/                     # Ansible automation
│   ├── playbook-main.yml          # Main orchestrator (automatic)
│   └── imports/
│       ├── play-*.yml             # Core playbooks
│       └── optional/
│           ├── common/            # General features
│           ├── hardware-specific/ # Hardware drivers
│           └── experimental/      # Bleeding-edge features
│
├── environment/localhost/         # Configuration
│   ├── hosts.yml                  # Inventory (localhost)
│   └── host_vars/localhost.yml    # User settings (encrypted)
│
├── files/                         # Static files deployed to system
│   ├── etc/                       # System configs
│   └── var/                       # Scripts and data
│
├── vars/                          # Project variables
│   └── fedora-version.yml         # Target Fedora version
│
├── run.bash                       # Bootstrap installer
├── ansible.cfg                    # Ansible configuration
├── vault-pass.secret              # Vault password (gitignored)
└── CLAUDE.md                      # AI coding assistant instructions
```

## Documentation Topics A-Z

**Architecture & Design**
- [Directory structure](architecture.md#directory-structure)
- [Execution flow](architecture.md#execution-flow)
- [Security model](architecture.md#security-model)
- [Variable hierarchy](architecture.md#configuration-management)

**Configuration & Customization**
- [User variables](configuration.md#user-configuration)
- [Vault encryption](configuration.md#vault-configuration)
- [Custom playbooks](configuration.md#adding-custom-configurations)
- [Bash environment](configuration.md#bash-environment)
- [Git configuration](configuration.md#git-configuration)

**Containerization**
- [Technology comparison](containerization.md#overview-comparison)
- [LXC setup](containerization.md#lxc-linux-containers)
- [Docker rootless](containerization.md#docker)
- [Distrobox integration](containerization.md#distrobox)
- [Docker-in-LXC](containerization.md#advanced-docker-in-lxc)

**Development & Contributing**
- [Development setup](development.md#development-environment)
- [Branching strategy](development.md#branching-strategy)
- [Ansible style guide](development.md#ansible-style-guide)
- [Testing procedures](development.md#testing)
- [Pull request process](development.md#contributing)

**Installation & Setup**
- [Prerequisites](installation.md#prerequisites)
- [Quick install](installation.md#quick-install)
- [Manual installation](installation.md#manual-installation)
- [Post-installation](installation.md#post-installation)
- [Troubleshooting](installation.md#troubleshooting)

**Playbooks & Features**
- [Core playbooks](playbooks.md#core-playbooks-automatically-run)
- [Optional features](playbooks.md#optional-playbooks)
- [Running playbooks](playbooks.md#running-optional-playbooks)
- [Creating playbooks](playbooks.md#creating-custom-playbooks)

## Troubleshooting Quick Links

- **Installation fails:** [Installation troubleshooting](installation.md#troubleshooting)
- **Version mismatch:** [Version troubleshooting](installation.md#version-mismatch)
- **Container issues:** [Container troubleshooting](containerization.md#troubleshooting)
- **Configuration errors:** [Configuration debugging](configuration.md#troubleshooting-configuration)
- **Playbook debugging:** [Testing guide](development.md#debugging)

## Getting Help

- **Bug reports:** [GitHub Issues](https://github.com/LongTermSupport/fedora-desktop/issues)
- **Questions & discussions:** [GitHub Discussions](https://github.com/LongTermSupport/fedora-desktop/discussions)
- **Source code:** [GitHub Repository](https://github.com/LongTermSupport/fedora-desktop)
- **Main README:** [Back to main page](../README.md)

## Version Information

**Current branch:** Fedora 42 (`F42`)

This documentation matches the configuration in this branch. Other Fedora versions have separate branches with version-specific changes.

Check your installed version:
```bash
cat /etc/fedora-release                    # Your system version
cat ~/Projects/fedora-desktop/vars/fedora-version.yml  # Target version
```

If versions don't match, checkout the appropriate branch:
```bash
cd ~/Projects/fedora-desktop
git fetch origin
git checkout F43  # or your version
```