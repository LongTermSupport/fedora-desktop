# Plan 027: Contextual Shell History with Atuin

**Status**: Not Started
**Created**: 2026-04-04
**Owner**: User
**Priority**: Medium
**Estimated Effort**: 2-3 hours

## Overview

Replace standard bash history with Atuin to get directory-aware and git-workspace-aware
command recall. Atuin stores every command with rich metadata (CWD, exit code, duration,
hostname, session) in a SQLite database and provides explicit filter modes including
a "workspace" mode that surfaces commands from anywhere in the current git repository.

See [research.md](research.md) for the full tool landscape analysis.

## Goals

- Install Atuin via Ansible playbook (RPM or binary)
- Configure bash integration with appropriate preexec backend
- Set sensible defaults (workspace filter mode, fuzzy search)
- Optional: configure E2E encrypted sync for multi-machine use
- Import existing bash history into Atuin

## Non-Goals

- Replacing zsh/fish history (bash only for now)
- Running Atuin sync server (use hosted atuin.sh or local-only)
- Removing standard bash history (keep as fallback)

## Tasks

### Phase 1: Playbook Implementation

- [ ] **Task 1.1**: Create `play-atuin.yml` in `playbooks/imports/optional/common/`
  - [ ] Install Atuin (COPR, cargo, or binary from GitHub releases)
  - [ ] Determine best install method for Fedora
  - [ ] Pin version with SHA256 checksum (same pattern as RapidRAW)
- [ ] **Task 1.2**: Deploy bash integration
  - [ ] Add `eval "$(atuin init bash)"` via blockinfile to `.bashrc`
  - [ ] Determine preexec backend: bash-preexec vs ble.sh
  - [ ] Install chosen preexec backend
- [ ] **Task 1.3**: Deploy Atuin config
  - [ ] Create `files/home/.config/atuin/config.toml`
  - [ ] Set `filter_mode = "workspace"` as default (git-repo-aware)
  - [ ] Set `search_mode = "fuzzy"`
  - [ ] Configure `cwd_filter` to exclude `/tmp`, sensitive dirs
  - [ ] Set `style = "compact"` or preferred TUI style

### Phase 2: History Migration

- [ ] **Task 2.1**: Import existing bash history
  - [ ] `atuin import bash` after installation
  - [ ] Verify imported commands are searchable

### Phase 3: Sync (Optional)

- [ ] **Task 3.1**: Evaluate sync needs
  - [ ] Decide: local-only vs atuin.sh vs self-hosted
  - [ ] If sync desired: `atuin register` / `atuin login`
  - [ ] Store sync credentials in Ansible Vault if needed

## Technical Decisions

### Decision 1: Install Method

**Context**: Atuin can be installed via cargo, binary download, or package manager.
**Options**:
1. `cargo install atuin` — requires Rust toolchain
2. Binary from GitHub releases — same pattern as RapidRAW (pinned version + checksum)
3. COPR/Fedora repo — if available, simplest upgrade path

**Decision**: TBD — research Fedora package availability first
**Date**: 2026-04-04

### Decision 2: Preexec Backend

**Context**: Bash lacks native preexec hooks. Atuin needs one.
**Options**:
1. **bash-preexec** — simpler, widely used, but `ignorespace` not fully honored
2. **ble.sh** — recommended by Atuin, accurate timing, but heavier dependency

**Decision**: TBD — test both
**Date**: 2026-04-04

## Success Criteria

- [ ] Ctrl+R opens Atuin search in all bash sessions
- [ ] Workspace filter mode surfaces commands from current git repo
- [ ] Directory filter mode surfaces commands from current directory
- [ ] Existing bash history imported and searchable
- [ ] Playbook is idempotent (safe to re-run)
- [ ] No disruption to existing shell workflow

## Risks & Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Atuin conflicts with existing Ctrl+R bindings | Medium | Low | Atuin replaces the binding; old history still in ~/.bash_history |
| bash-preexec causes shell startup slowdown | Low | Low | Benchmark; switch to ble.sh if needed |
| Atuin update breaks bash integration | Medium | Low | Pin version in playbook |

## Notes & Updates

### 2026-04-04
- Created plan based on tool landscape research
- Atuin selected as clear winner over McFly, hishtory, and DIY approaches
- Key differentiator: explicit workspace (git repo) filter mode
