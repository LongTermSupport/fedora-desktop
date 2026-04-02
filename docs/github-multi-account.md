# GitHub Multi-Account Management

**Playbook**: `playbooks/imports/optional/common/play-github-cli-multi.yml`

The project supports multiple GitHub accounts with separate SSH keys and convenient shell functions.

## Initial Setup

Run the playbook to configure accounts for the first time:
```bash
ansible-playbook ./playbooks/imports/optional/common/play-github-cli-multi.yml
```

The playbook will prompt you to enter accounts in format: `alias:username,alias:username`
Example: `work:johndoe-work,personal:johndoe,oss:johndoe-oss`

## Adding a New Account to Existing Setup

**IMPORTANT**: The playbook skips configuration prompts if accounts already exist. To add a new account:

1. **Edit the host vars file directly** (it's a regular YAML file, not encrypted):
   ```bash
   vim environment/localhost/host_vars/localhost.yml
   ```

2. **Add the new account** to the `github_accounts` section:
   ```yaml
   # GitHub CLI accounts
   github_accounts:
     work: "johndoe-work"      # existing
     personal: "johndoe"       # existing
     oss: "johndoe-oss"        # <-- ADD NEW ACCOUNT HERE
   ```

3. **Re-run the playbook** to complete setup:
   ```bash
   ansible-playbook ./playbooks/imports/optional/common/play-github-cli-multi.yml
   ```

The playbook will:
- Generate SSH key for the new account (`~/.ssh/github_oss`)
- Add SSH config entry for `github.com-oss`
- Regenerate bash aliases/functions with all accounts
- Prompt for `gh auth login` for the new account

## Available Commands

After setup, these functions are available in your shell:

```bash
# Account management
gh-list                    # List all configured accounts
gh-whoami                  # Show currently active account
gh-status                  # Check authentication status for all accounts
gh-switch work             # Switch to a specific account
github-test-ssh            # Test SSH connections for all accounts

# Account-specific commands (using work account as example)
gh-work pr list            # Run gh command as work account
clone-work owner/repo      # Clone repo using work account
remote-work owner/repo     # Set git remote for work account
gh-token-work              # Get GitHub token for work account
gh-work-make-default       # Set work as default account
```

## Configuration Files

- **Account definitions**: `environment/localhost/host_vars/localhost.yml` (under `github_accounts`)
- **SSH keys**: `~/.ssh/github_<alias>` and `~/.ssh/github_<alias>.pub`
- **SSH config**: `~/.ssh/config` (separate blocks per account)
- **Bash functions**: `~/.bashrc-includes/gh-aliases.inc.bash` (regenerated on each run)

## Removing an Account

1. Edit `environment/localhost/host_vars/localhost.yml` and remove the account from `github_accounts`
2. Re-run the playbook — bash functions will be regenerated without that account
3. Manually remove SSH keys and config if desired:
   ```bash
   rm ~/.ssh/github_<alias>*
   # Edit ~/.ssh/config to remove the account's block
   ```
