# Plan 023: Hostname-Based Inventory Migration

**Status**: Not Started
**Created**: 2026-03-15
**Owner**: To be assigned
**Priority**: Medium
**Recommended Executor**: Sonnet
**Execution Strategy**: Single-Threaded

## Overview

The project currently uses a hardcoded inventory hostname of `localhost` for all machines.
This means every machine that runs this configuration shares the same `host_vars/localhost.yml`
file and the same Ansible host identity. While functional for a single-machine setup, this
design cannot support multiple laptops or workstations with per-machine configuration
differences (different SSH keys, different colour schemes, different optional services, etc.)
without ugly workarounds such as the existing `hostname_overrides` dict in `play-basic-configs.yml`.

This plan migrates the Ansible inventory to use the actual machine hostname as the Ansible
host identity. Each machine gets its own `host_vars/<hostname>.yml` file. Machines with
different configurations are naturally isolated. The migration provides a clean path for
existing single-machine installs and handles new installs where the hostname is set during
Fedora installation.

The vault ID ("localhost" in `ansible.cfg`) is a separate concern from the inventory
hostname. This plan deliberately keeps the vault ID as "localhost" because changing it
would require re-encrypting every vault string in every machine's config file — a
high-risk, high-effort operation with no functional benefit. The two concepts are
documented as intentionally decoupled.

## Goals

- Replace the hardcoded `localhost` inventory hostname with the actual machine hostname
- Support multiple machines, each with its own `host_vars/<hostname>.yml`
- Provide a migration path for existing installs (`localhost.yml` → `<hostname>.yml`)
- Update `run.bash` bootstrap to create `host_vars/<hostname>.yml` at the correct path
- Keep the vault ID as "localhost" (no re-encryption required)
- Handle edge cases: generic hostnames ("fedora", "localhost"), missing host_vars
- Update `localhost.yml.dist` to document the new convention
- Ensure all playbooks continue to work unchanged (they target `desktop` group, not the hostname directly)

## Non-Goals

- Changing the vault ID from "localhost" — this would require re-encrypting all secrets; defer to a future plan if ever needed
- Multi-host inventory (remote machines, SSH targets) — this project is always local-only
- Changing the inventory directory name (`environment/localhost/`) — the directory name is just a label and does not need to match the host name
- Supporting Ansible's `--limit` flag for host selection — all playbooks target the `desktop` group which always has exactly one host
- Dynamic inventory generation — static YAML inventory is sufficient

## Context and Background

### Current Inventory Structure

```
environment/localhost/
├── hosts.yml                     # Defines group "desktop" with one host named "localhost"
└── host_vars/
    ├── localhost.yml             # Per-machine variables (secrets, user config, github accounts)
    └── localhost.yml.dist        # Template for new installs
```

`ansible.cfg` sets `inventory = ./environment/localhost`. Ansible auto-loads
`host_vars/localhost.yml` because the host is named `localhost` in `hosts.yml`.

### How Ansible host_vars Auto-Loading Works

Ansible loads `host_vars/<hostname>/` or `host_vars/<hostname>.yml` automatically
when a play runs against that host. The filename must exactly match the inventory
hostname. If the inventory hostname is `mylaptop`, then `host_vars/mylaptop.yml`
is loaded automatically. `host_vars/localhost.yml` would be ignored.

This is the core mechanism this plan exploits: by changing the inventory hostname
from `localhost` to the actual machine hostname, Ansible automatically loads the
correct per-machine config file.

### Vault ID vs Inventory Hostname (Critical Distinction)

The vault ID ("localhost") is embedded in every encrypted string:
```
$ANSIBLE_VAULT;1.2;AES256;localhost
```
This string is used by Ansible to match the decryption key. `vault_id_match=true`
in `ansible.cfg` means Ansible will only decrypt strings that were encrypted with
the "localhost" vault ID.

**This vault ID is completely independent of the inventory hostname.** A machine
named `mylaptop` in the inventory can still have vault strings encrypted with the
"localhost" vault ID. There is no requirement to keep them in sync. The vault ID
is just a label on the encryption key — it is NOT the machine hostname.

**Decision: Keep vault ID as "localhost" permanently.** This avoids re-encrypting
all existing vault strings and maintains backward compatibility.

### Existing Per-Machine Pattern

`play-basic-configs.yml` already has a partial per-machine pattern using
`hostname_overrides` dict keyed on `ansible_facts['hostname']`. After this
migration, per-machine settings live in separate `host_vars` files, making
`hostname_overrides` redundant (though it can remain for backward compatibility).

### run.bash Bootstrap Observations

- Already prompts for and sets a custom hostname using `hostnamectl set-hostname`
  when the hostname is the generic default "fedora" — needs extending to also catch
  "localhost", "localhost.localdomain", empty string
- `localhost_yml` variable is hardcoded to the `localhost.yml` path throughout
- The config repo (`${primary_gh_username}/fedora-desktop-config`) stores the file
  as `localhost.yml` — after migration it should store it as `<hostname>.yml`,
  with a fallback to `localhost.yml` for existing users

### Generic Hostname Problem

Fedora's default hostname is "fedora". The bootstrap already handles this with a
rename step. However "localhost" itself is also a possible hostname.
**Generic hostnames to detect**: `fedora`, `localhost`, `localhost.localdomain`, empty string.
When detected, bootstrap must require the user to set a real hostname.

## Tasks

### Phase 1: Understand and Document the Full Scope

- [ ] **Task 1.1**: Audit all playbooks for literal `localhost` host name references
  - Distinguish: connection target (`ansible_host: localhost` — stays unchanged) vs host name
  - Check for `hostvars['localhost']` references which would break after migration
- [ ] **Task 1.2**: Identify all `localhost_yml` hardcoded path references in `run.bash`
  - Line 565: `localhost_yml` variable definition
  - Line 569: config repo fetch of `localhost.yml` by name
  - Line 581-587: `base64 -d` write to `$localhost_yml`
  - Lines 601-618: manual config write to `$localhost_yml`
  - Lines 625, 657, 694: subsequent reads/checks of `$localhost_yml`
- [ ] **Task 1.3**: Check scripts/, files/, CLAUDE.md, README.md for `host_vars/localhost.yml` references
- [ ] **Task 1.4**: Document config repo convention (currently `localhost.yml`; post-migration `<hostname>.yml`)

### Phase 2: Design the New Inventory Structure

- [ ] **Task 2.1**: Design new `hosts.yml` — generate at bootstrap time with real hostname baked in
  - `ansible_host: localhost` stays as `localhost` (connection address, not host name)
  - `hosts.yml` becomes a generated artifact; `hosts.yml.dist` is the committed template
- [ ] **Task 2.2**: Design `host_vars` naming — `environment/localhost/host_vars/<hostname>.yml`
  - `localhost.yml.dist` remains as the template name (it is a dist, not a real host)
- [ ] **Task 2.3**: Design config repo convention
  - Primary: `<hostname>.yml`; fallback: `localhost.yml` with migration warning
- [ ] **Task 2.4**: Confirm vault ID strategy — keep as "localhost", add clarifying comment to `ansible.cfg`

### Phase 3: Implement the New Inventory

- [ ] **Task 3.1**: Create `hosts.yml.dist` template:
  ```yaml
  desktop:
    hosts:
      <hostname>:        # Replace with: hostname -s
        vars:
          ansible_host: localhost
          connection: local
          ansible_connection: local
          ansible_python_interpreter: "{{ansible_playbook_python}}"
  ```
- [ ] **Task 3.2**: Add `environment/localhost/hosts.yml` to `.gitignore` (machine-generated)
- [ ] **Task 3.3**: Verify `host_vars/*.yml` is gitignored; keep `*.yml.dist` tracked
- [ ] **Task 3.4**: Add clarifying comment to `ansible.cfg` before `vault_identity=localhost`:
  ```ini
  # vault_identity: label embedded in encrypted vault strings.
  # Intentionally kept as "localhost" — NOT the machine hostname.
  # Changing this would require re-encrypting all vault strings.
  # The inventory hostname (in hosts.yml) is completely separate.
  ```

### Phase 4: Update run.bash Bootstrap

- [ ] **Task 4.1**: Harden hostname detection — extend generic hostname list:
  ```bash
  generic_hostnames=("fedora" "localhost" "localhost.localdomain" "")
  ```
  Validate after `hostnamectl set-hostname`; abort if still generic.

- [ ] **Task 4.2**: Generate `hosts.yml` during bootstrap after hostname is confirmed:
  ```bash
  cat > "$hosts_yml" <<EOF
  desktop:
    hosts:
      ${machine_hostname}:
        vars:
          ansible_host: localhost
          connection: local
          ansible_connection: local
          ansible_python_interpreter: "{{ansible_playbook_python}}"
  EOF
  ```

- [ ] **Task 4.3**: Replace `localhost_yml` variable with hostname-aware path:
  ```bash
  host_vars_dir=~/Projects/fedora-desktop/environment/localhost/host_vars
  machine_config_yml="${host_vars_dir}/${machine_hostname}.yml"
  ```

- [ ] **Task 4.4**: Config repo fetch — try `<hostname>.yml` first, fall back to `localhost.yml`:
  ```bash
  if raw=$(gh api "repos/${config_repo}/contents/${machine_hostname}.yml" --jq '.content' 2>/dev/null); then
    info "Found ${machine_hostname}.yml in config repo."
  elif raw=$(gh api "repos/${config_repo}/contents/localhost.yml" --jq '.content' 2>/dev/null); then
    warn "Found legacy localhost.yml — consider renaming to ${machine_hostname}.yml in your config repo."
  fi
  ```

- [ ] **Task 4.5**: Update all remaining `$localhost_yml` references to `$machine_config_yml`

### Phase 5: Migration Path for Existing Installs

- [ ] **Task 5.1**: Write `playbooks/imports/optional/common/play-migrate-inventory-hostname.yml`
  - Reads `ansible_facts['hostname']`
  - If `host_vars/localhost.yml` exists and `host_vars/<hostname>.yml` does not: copy it
  - Regenerates `hosts.yml` with real hostname
  - Does NOT auto-delete `localhost.yml` — instructs user to verify then delete manually
  - Emits clear next-steps message including config repo rename instructions

- [ ] **Task 5.2**: Document migration procedure in CLAUDE.md / README

- [ ] **Task 5.3**: Add advisory preflight check in `play-AA-preflight-sanity.yml`
  - Warn if `host_vars/localhost.yml` still exists after migration (old file not cleaned up)

### Phase 6: Update Documentation

- [ ] **Task 6.1**: Update `localhost.yml.dist` — add comment explaining the `<hostname>.yml` naming convention and vault ID decoupling
- [ ] **Task 6.2**: Update `CLAUDE.md` — key file locations, vault ID clarification, `hosts.yml` is machine-generated
- [ ] **Task 6.3**: Update `ansible.cfg` comments (see Task 3.4)

### Phase 7: QA and Validation

- [ ] **Task 7.1**: `./scripts/qa-all.bash` on all modified bash files
- [ ] **Task 7.2**: `ansible-inventory --list` confirms generated `hosts.yml` parses correctly
- [ ] **Task 7.3**: `./playbooks/imports/play-AA-preflight-sanity.yml --check` targets correct host
- [ ] **Task 7.4**: Verify `host_vars/<hostname>.yml` auto-loaded: `ansible desktop -m debug -a 'var=user_login'`
- [ ] **Task 7.5**: Verify vault decryption still works after hostname change:
  `ansible desktop -m debug -a 'var=lastfm_api_key'` — must decrypt correctly
  (vault ID "localhost" in `ansible.cfg` is independent of inventory hostname)
- [ ] **Task 7.6**: End-to-end test of migration playbook on a test scenario

## Dependencies

- Depends on: None
- Blocks: None
- Related: Plan 018 (kickstart install) — if kickstart sets a real hostname before `run.bash` runs, this plan benefits from that alignment

## Technical Decisions

### Decision 1: Keep Vault ID as "localhost"

Changing it requires re-encrypting every vault string across all machines.
The functional benefit (a different label) does not justify the cost and risk.
Vault ID and inventory hostname are intentionally decoupled. Documented in `ansible.cfg`.

### Decision 2: Generate hosts.yml at Bootstrap Time

Three options: static with placeholder (fragile), generated at bootstrap (clean,
fits existing pattern), dynamic inventory plugin (overkill for single local machine).
**Decision**: generate at bootstrap. `hosts.yml` is gitignored; `hosts.yml.dist` is tracked.

### Decision 3: Config Repo Fallback to localhost.yml

Try `<hostname>.yml` first; fall back to `localhost.yml` with a warning.
Backward compatible for existing users; gives them a clear migration path.

### Decision 4: Keep Inventory Directory Name as `environment/localhost/`

The name is a path label meaning "local machine environment config", which remains
accurate. Renaming adds friction for no functional gain.

### Decision 5: hosts.yml in .gitignore

Generated artifact — never committed. `hosts.yml.dist` is the committed template.
Mirrors the existing `localhost.yml` / `localhost.yml.dist` pattern.

### Decision 6: Migration Playbook Does Not Auto-Delete localhost.yml

Safer to require explicit human confirmation. User is clearly instructed to verify
the new file then delete the old one manually. Consistent with fail-fast principle.

## Success Criteria

- [ ] `./playbooks/playbook-main.yml` runs successfully on a machine named `mylaptop` with `host_vars/mylaptop.yml` (not `localhost.yml`)
- [ ] Two machines (`mylaptop`, `homepc`) can each have their own `host_vars/<hostname>.yml` in the same repo
- [ ] Vault decryption works correctly after inventory hostname change
- [ ] `run.bash` generates correct `hosts.yml` and `host_vars/<hostname>.yml` on new install
- [ ] `run.bash` handles `localhost.yml` fallback in config repo gracefully with warning
- [ ] Migration playbook correctly migrates an existing install without data loss
- [ ] `./scripts/qa-all.bash` passes on all modified bash files
- [ ] `ansible-inventory --list` shows real machine hostname (not `localhost`) after bootstrap

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Vault decryption breaks after hostname change | High | Low | Vault ID stays as "localhost"; decryption independent of inventory hostname. Verify in Task 7.5. |
| Existing installs fail on re-run before migration | High | Medium | Provide migration playbook (Phase 5); document clearly. Old `hosts.yml` with `localhost` keeps working until migrated. |
| Config repo file not found if user had `localhost.yml` | Medium | High | Fallback to `localhost.yml` with migration warning (Task 4.4). |
| `hostname -s` returns empty or generic value | High | Low | Validate after set; abort if still generic. |
| `hosts.yml` accidentally committed with real hostname | Low | Low | `.gitignore` entry for `environment/localhost/hosts.yml`. |
| `hostvars['localhost']` references in playbooks break | High | Low | Audit all playbooks in Task 1.1; replace with `hostvars[inventory_hostname]`. |
| Hostname contains characters invalid for YAML keys or filenames | Low | Low | Validate against safe character set (alphanumeric + hyphen) before generating files. |

## Notes and Updates

### 2026-03-15

- Plan created from codebase research (see background agent analysis)
- Key finding: `play-basic-configs.yml` already uses `ansible_facts['hostname']` for `hostname_overrides` — confirms the project already recognises per-machine config; this plan formalises it at the inventory level
- Key finding: `run.bash` line 449 already has hostname-setting logic for the "fedora" default — Phase 4 Task 4.1 extends this to other generic hostnames
- Key finding: config repo fetches `localhost.yml` by literal name — highest-friction change in `run.bash`, requires fallback logic
- Primary use case driving this plan: same user running multiple laptops wanting different configs per machine, using the same git repo
