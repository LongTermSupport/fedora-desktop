# Fedora Desktop Configuration - Documentation

Welcome to the comprehensive documentation for the Fedora Desktop configuration project.

## Table of Contents

### Getting Started

1. **[Installation Guide](installation.md)**  
   Step-by-step installation instructions, prerequisites, and troubleshooting.

2. **[Architecture Overview](architecture.md)**  
   Project structure, execution flow, and design decisions.

### Usage and Configuration

3. **[Playbooks Reference](playbooks.md)**  
   Complete list of available playbooks, their purposes, and how to use them.

4. **[Configuration Guide](configuration.md)**  
   Customization options, user settings, and system configuration.

### Development

5. **[Development Guide](development.md)**  
   Contributing guidelines, development environment setup, and best practices.

## Quick Reference

### Essential Commands

```bash
# Quick install (run as regular user)
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))

# Run main playbook manually
ansible-playbook playbooks/playbook-main.yml --ask-become-pass

# Run optional playbook
ansible-playbook playbooks/imports/optional/common/play-docker.yml

# Check system configuration
ansible desktop -m setup
```

### Project Structure

```
fedora-desktop/
├── docs/                # This documentation
├── playbooks/           # Ansible playbooks
│   ├── playbook-main.yml
│   └── imports/
│       └── optional/
├── environment/         # Ansible inventory
├── files/              # Static config files
├── vars/               # Configuration variables
└── run.bash            # Bootstrap script
```

### Key Files

- `vars/fedora-version.yml` - Target Fedora version
- `environment/localhost/host_vars/localhost.yml` - User configuration
- `playbooks/playbook-main.yml` - Main orchestrator
- `ansible.cfg` - Ansible configuration

## Documentation by Topic

### System Setup
- [Prerequisites and system requirements](installation.md#prerequisites)
- [Bootstrap process](architecture.md#execution-flow)
- [Core components](playbooks.md#core-playbooks-automatically-run)

### Customization
- [User settings](configuration.md#user-configuration)
- [Optional features](playbooks.md#optional-playbooks)
- [Custom playbooks](configuration.md#adding-custom-configurations)

### Development
- [Branching strategy](development.md#branching-strategy)
- [Ansible style guide](development.md#ansible-style-guide)
- [Testing procedures](development.md#testing)

### Troubleshooting
- [Installation issues](installation.md#troubleshooting)
- [Configuration debugging](configuration.md#troubleshooting-configuration)
- [Development debugging](development.md#debugging)

## Getting Help

- **Issues**: [GitHub Issues](https://github.com/LongTermSupport/fedora-desktop/issues)
- **Source**: [GitHub Repository](https://github.com/LongTermSupport/fedora-desktop)
- **Main README**: [../README.md](../README.md)

## Version Information

This documentation applies to the branch targeting **Fedora 42**.  
Other Fedora versions may have different branches with version-specific changes.

Check your branch's target version:
```bash
cat vars/fedora-version.yml
```