# Security Rules

## Public Repository Warning

**THIS IS A PUBLIC REPOSITORY — EXTREME CAUTION REQUIRED**

### Never Commit

- ❌ **Personal information** — Names, email addresses, usernames, account IDs
- ❌ **Local configuration** — File paths with usernames, home directories, hostnames
- ❌ **Credentials** — API keys, tokens, passwords, SSH keys, certificates
- ❌ **Private data** — IP addresses, internal URLs, company information
- ❌ **Sensitive examples** — Real usernames in code examples or comments
- ❌ **Vault passwords** — vault-pass.secret is gitignored for a reason
- ❌ **Debug output** — Logs or error messages containing sensitive data
- ❌ **Account mappings** — Hardcoded user-to-account associations

### Always Use

- ✅ **Generic placeholders** — `user`, `example.com`, `<username>`, `{{ user_login }}`
- ✅ **Ansible variables** — Reference variables instead of hardcoded values
- ✅ **Ansible Vault** — Encrypt ALL sensitive data in host_vars/localhost.yml
- ✅ **Dynamic detection** — Query systems at runtime (e.g., `gh api user`, `ssh -T`)
- ✅ **Documentation variables** — Use `{{ user_login }}` in examples, never real usernames
- ✅ **Gitignore** — Keep sensitive files out of git (.credentials, .secret, etc.)

### Pre-Commit Checks

**Before committing:**
1. Review ALL changes with `git diff`
2. Search for usernames: `git diff | grep -i "yourname"`
3. Check for email addresses: `git diff | grep -E "[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}"`
4. Verify no hardcoded paths: `git diff | grep "/home/"`
5. Confirm no tokens/keys visible: `git diff | grep -E "(token|key|password|secret)"`

**Automated Protection:** `scripts/git-hooks/pre-commit` and `commit-msg` scan staged files and commit messages for secrets/sensitive patterns. Enforced by Ansible on every main playbook run. Manual install: `git config core.hooksPath scripts/git-hooks`

### If Accidentally Committed

1. DO NOT just delete in next commit — it's still in git history
2. Use `git filter-branch` or BFG Repo-Cleaner to purge from history
3. Rotate ALL exposed credentials immediately
4. Inform team/users if credentials were pushed to remote

---

## Vault Management

**This project uses VARIABLE-level encryption, NOT file-level encryption.**

- **Vault password**: Stored in `vault-pass.secret` (gitignored)
- **Vault ID**: Uses `localhost` vault ID with matching enforced (`vault_id_match=true`)
- **Encryption method**: Individual sensitive values encrypted with `ansible-vault encrypt_string`
- **File format**: `environment/localhost/host_vars/localhost.yml` is a regular YAML file with encrypted string values
- **Editing**: Use a regular text editor (vim, nano, etc.) — DO NOT use `ansible-vault edit`
- **Encrypting new values**: `ansible-vault encrypt_string 'secret' --name 'var_name'`

**Example of variable-level encryption:**
```yaml
# Regular unencrypted variables
user_login: example_user
user_name: Example User

# Encrypted variables (created with ansible-vault encrypt_string)
lastfm_api_key: !vault |
  $ANSIBLE_VAULT;1.2;AES256;localhost
  66386439653162636163623333...
```

---

## Security Principles

- **Never hardcode secrets** in version control
- **Validate inputs** before using them
- **Principle of least privilege** — Minimal permissions required
- **No credentials in logs** — Sanitise output
- **Passwordless sudo** configured for main user
- **SSH key authentication** for GitHub
- **Proper file permissions** on sensitive files (`mode: 0600`)
