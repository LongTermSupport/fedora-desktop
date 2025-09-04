# Development Guide

## Development Environment

### Recommended Setup

- **IDE**: PyCharm Community Edition
- **Plugins**:
  - [Ansible Plugin](https://plugins.jetbrains.com/plugin/14893-ansible)
  - [Ansible Vault Editor](https://plugins.jetbrains.com/plugin/14278-ansible-vault-editor)
- **Linting**: ansible-lint

### Project Setup

```bash
# Clone repository
git clone https://github.com/LongTermSupport/fedora-desktop.git
cd fedora-desktop

# Install development dependencies
sudo dnf install -y ansible ansible-lint

# Install Ansible Galaxy requirements
ansible-galaxy install -r requirements.yml

# Set up pre-commit hooks (if using)
pre-commit install
```

## Branching Strategy

### Branch Naming

- Format: `F<VERSION>` (e.g., `F42`, `F43`)
- Each branch targets specific Fedora version
- Version defined in `vars/fedora-version.yml`

### Creating New Version Branch

When Fedora releases a new version:

```bash
# 1. Update version in configuration
vim vars/fedora-version.yml
# Change: fedora_version: 43

# 2. Commit the change
git add vars/fedora-version.yml
git commit -m "Update target Fedora version to 43"

# 3. Create and push new branch
git checkout -b F43
git push -u origin F43

# 4. Set as default branch on GitHub
gh repo edit --default-branch F43

# 5. Update branch-specific changes
# - Test all playbooks
# - Update package versions if needed
# - Fix any compatibility issues
```

### Branch Lifecycle

- **Active**: Current Fedora version branch
- **Maintenance**: Previous version (critical fixes only)
- **Archive**: Older versions (reference only)

## Ansible Style Guide

### Playbook Structure

```yaml
- hosts: desktop
  name: Descriptive Name  # Clear, action-oriented
  become: true  # Only if needed
  vars:
    root_dir: "{{ inventory_dir }}/../../"
  vars_files:
    - "{{ root_dir }}/vars/fedora-version.yml"  # If version-dependent
  tasks:
    - name: Clear task description
      module_name:
        parameter: value
```

### File Modifications

**Always use `blockinfile` over `lineinfile`:**

```yaml
- name: Update configuration
  blockinfile:
    path: /path/to/file
    marker: "# {mark} ANSIBLE MANAGED: Purpose"
    block: |
      configuration content
    create: yes  # If file should be created
    owner: "{{ user_login }}"
    group: "{{ user_login }}"
    mode: '0644'
```

### Package Management

```yaml
# Simple installations
- name: Install packages
  package:
    name: "{{ packages }}"
    state: present
  vars:
    packages:
      - package1
      - package2

# Fedora-specific features
- name: Install from DNF
  dnf:
    name: package-name
    state: present
    enablerepo: repo-name  # If needed
```

### Variable Naming

- Use descriptive names: `user_login`, not `ul`
- Group related variables: `lastpass_accounts`, `github_accounts`
- Document complex variables with comments

### Task Organization

- Group related tasks with `block`
- Use meaningful tags for selective execution
- Order tasks logically: checks → installation → configuration

## Testing

### Local Testing

```bash
# Syntax check
ansible-playbook playbooks/playbook-main.yml --syntax-check

# Dry run
ansible-playbook playbooks/playbook-main.yml --check

# Run specific tags
ansible-playbook playbooks/playbook-main.yml --tags packages

# Test individual playbook
ansible-playbook playbooks/imports/play-basic-configs.yml -vv
```

### Debugging

```bash
# Verbose output levels
-v    # Basic debug
-vv   # More detailed
-vvv  # Connection debug
-vvvv # Full debug

# Step through execution
ansible-playbook playbook.yml --step

# Start at specific task
ansible-playbook playbook.yml --start-at-task="Task Name"
```

### Fact Gathering

```bash
# View all facts
ansible desktop -m setup

# Filter facts
ansible desktop -m setup -a "filter=ansible_distribution*"

# Save facts to file
ansible desktop -m setup --tree /tmp/facts
```

## Contributing

### Before Submitting

1. **Test on fresh Fedora installation**
2. **Verify idempotency** (run twice, no changes second time)
3. **Check style compliance** with ansible-lint
4. **Update documentation** if adding features
5. **Test both with and without** third-party repos

### Pull Request Process

1. Fork repository
2. Create feature branch from appropriate version branch
3. Make changes following style guide
4. Test thoroughly
5. Submit PR with clear description

### Adding New Playbooks

1. Create in appropriate directory:
   - `imports/` for core features
   - `imports/optional/common/` for general optional features
   - `imports/optional/hardware-specific/` for hardware support
   - `imports/optional/experimental/` for bleeding-edge

2. Follow naming convention: `play-<feature-name>.yml`

3. Include standard headers:
```yaml
- hosts: desktop
  name: Feature Description
  become: true  # If needed
  vars:
    root_dir: "{{ inventory_dir }}/../../"
```

4. Document in `docs/playbooks.md`

### Commit Messages

Use conventional format:
```
type: description

- Detail 1
- Detail 2

Fixes #issue
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation
- `style`: Formatting
- `refactor`: Code restructuring
- `test`: Testing
- `chore`: Maintenance

## Security Considerations

### Vault Usage

```bash
# Encrypt sensitive variables
ansible-vault encrypt_string 'secret' --name 'var_name'

# Always use vault for:
# - API keys
# - Passwords
# - Tokens
# - Private configuration
```

### File Permissions

```yaml
- name: Secure file
  copy:
    src: source
    dest: /path/to/dest
    owner: "{{ user_login }}"
    group: "{{ user_login }}"
    mode: '0600'  # Restrictive for sensitive files
```

### Never Commit

- `vault-pass.secret`
- Personal API keys
- SSH private keys
- Temporary files in `untracked/`

## Resources

- [Ansible Documentation](https://docs.ansible.com/)
- [Ansible Best Practices](https://docs.ansible.com/ansible/latest/user_guide/playbooks_best_practices.html)
- [Fedora Packaging Guidelines](https://docs.fedoraproject.org/en-US/packaging-guidelines/)
- [Project Issues](https://github.com/LongTermSupport/fedora-desktop/issues)