# Plan 00054: Host-level GitHub SSH-over-443, unified with CCY

**Status**: Not Started
**Created**: 2026-06-16
**Owner**: joseph / Claude
**Priority**: Medium

## Overview

GitHub SSH-over-443 (`ssh.github.com:443`) keeps Git-over-SSH working on networks
that firewall outbound port 22. The CCY container already supports it (the
`--github-443` flag, the `GITHUB_SSH_443` env var consumed by the entrypoint, and
a launch-time auto-fallback probe). The host playbook `play-github-cli-multi.yml`
already has a persistent `github_ssh_over_443` toggle that rewrites the SSH config
HostName/Port.

This plan unifies the two sides behind a **single runtime signal**
(`GITHUB_SSH_443`) and fills the gaps so that 443 mode can be controlled at the
**host level** in two lifetimes — **temporary** (flip now for this bad network, no
Ansible run) and **always-on** (persisted in `host_vars`, applied by Ansible) —
and so that enabling it at the host automatically propagates into CCY.

The reference design is captured in `untracked/github-443-solution.md` (a portable
writeup from sibling work). Key principles from it: it is the **port** that
matters not the hostname; pin `[ssh.github.com]:443` in `known_hosts`; use a
single **first-wins override block at the top of `~/.ssh/config`** to cover deploy-
key aliases without editing each stanza; prefer an on-demand toggle over an
always-on default (leaving it on is harmless but a clean off-switch is hygiene).

## Goals

- One runtime signal — `GITHUB_SSH_443` — drives both host SSH and CCY.
- `export GITHUB_SSH_443=1; ccy` runs CCY in 443 mode (today it is clobbered to 0).
- A host-level **temporary** toggle (`github-ssh-443 on|off|auto|status`) that
  edits `~/.ssh/config` + `known_hosts` without a full Ansible run, and reconciles
  cleanly with the playbook-managed blocks (same markers).
- A host-level **always-on** path: `github_ssh_over_443: true` writes the SSH
  config + `known_hosts` pin **and** exports `GITHUB_SSH_443=1` for every shell so
  CCY inherits it.
- Foreign **deploy-key SSH aliases** (`HostName github.com`) are rerouted via a
  first-wins BOF override block keyed on a `github_443_extra_aliases` host_var.
- Host `known_hosts` gains the `[ssh.github.com]:443` pin (currently container-only).

## Non-Goals

- Changing the container-side auto-fallback behaviour (it already works; it only
  needs to also honour an inherited `GITHUB_SSH_443=1`).
- Solving DPI / TLS-only / SNI-whitelist proxies that also block raw SSH-over-443
  (out of scope — documented as a caveat; HTTPS remotes are the escape hatch).
- Converting any remotes to HTTPS. This is an SSH-transport solution only.
- Touching `gh` CLI / Git-over-HTTPS paths (already use 443, unaffected).

## Context & Background

### Current state (verified 2026-06-16)

- `files/var/local/claude-yolo/claude-yolo` (CCY_VERSION 3.20.0):
  - `--github-443` → `GITHUB_443_MODE=true`.
  - Lines ~535–539 set `GITHUB_SSH_443=1` if the flag was passed, **else hard-set
    `0`** — this clobbers any host-exported value. **Gap 1.**
  - `GITHUB_SSH_443` is passed into the container (`-e GITHUB_SSH_443=...`).
- `files/var/local/claude-yolo/lib/ssh-handling.bash`:
  - `_github_probe_user(key, host, port)` probes auth with isolation flags +
    `ConnectTimeout=10`.
  - `build_ssh_mounts_and_validate` does the primary-key auto-fallback (22 → 443),
    interactive prompt, headless auto-enable.
- `files/var/local/claude-yolo/entrypoint.sh`:
  - In `GITHUB_SSH_443=1` mode writes the container `~/.ssh/config` to
    `ssh.github.com:443` and pins `[ssh.github.com]:443` in `known_hosts`.
- `playbooks/imports/play-github-cli-multi.yml`:
  - Play vars `github_ssh_hostname` / `github_ssh_port` derive from
    `github_ssh_over_443 | default(false)`.
  - "Create SSH config entries" (per-alias `Host github.com-<alias>`) and "Add
    default Host github.com SSH config entry" both use those vars.
  - **No `known_hosts` pin** on the host. **Gap 3.**
  - Default block is written in place, **not** at BOF, and matches only
    `github.com` — foreign deploy aliases are not covered. **Gap 4.**

### The four gaps

1. CCY clobbers an inherited `GITHUB_SSH_443` (one-line fix).
2. No temporary host toggle (no off-Ansible flip).
3. Host `known_hosts` not pinned for `:443`.
4. Foreign deploy-key aliases not rerouted.

### ssh-agent / control-socket analysis (answers a design question)

- The **ssh-agent does NOT need restarting or flushing** when toggling 443. It
  holds private-key identities, which are endpoint-agnostic — the same key
  authenticates identically on `github.com:22` and `ssh.github.com:443`. Routing is
  from `~/.ssh/config`, host verification from `~/.ssh/known_hosts`; both re-read
  every invocation. The agent participates in neither.
- The only stale-state risk is an SSH **connection-multiplexing control socket**
  (`ControlMaster`/`ControlPersist`) pinned to `github.com:22`. A repo grep finds
  **no** `ControlMaster`/`ControlPath`/`ControlPersist` anywhere, so the default
  setup has nothing to flush. The toggle should still defensively run
  `ssh -O exit git@github.com` for any user-configured master.

## Tasks

### Phase 1: CCY honours the host-level env var (smallest, highest value)

- [ ] ⬜ **Task 1.1**: Make `claude-yolo` honour an inherited `GITHUB_SSH_443=1`
  - [ ] ⬜ Change the post-parse block so precedence is: `--github-443` flag →
    inherited `GITHUB_SSH_443=1` → else `0`. Auto-fallback still runs when `0`.
  - [ ] ⬜ Bump `CCY_VERSION` (minor) with a descriptive comment.
  - [ ] ⬜ Update `--help` / docs to mention `GITHUB_SSH_443=1 ccy`.
  - [ ] ⬜ Run QA: `./scripts/qa-all.bash`.

### Phase 2: Host `known_hosts` pin + deploy-key alias override

- [ ] ⬜ **Task 2.1**: Pin `[ssh.github.com]:443` in the host `known_hosts`
  - [ ] ⬜ Add a `blockinfile` (distinct marker) gated on `github_ssh_over_443`,
    `state: present/absent`, fetching keys from the GitHub meta API
    (`api.github.com/meta .ssh_keys`) or static published keys (decision below).
  - [ ] ⬜ Run QA + `ansible-playbook --syntax-check`.
- [ ] ⬜ **Task 2.2**: First-wins BOF override for deploy-key aliases
  - [ ] ⬜ Add `github_443_extra_aliases` (list, default `[]`) to vars + dist.
  - [ ] ⬜ Convert the default `Host github.com` handling to a BOF override block
    (`insertbefore: BOF`) matching
    `github.com ssh.github.com {{ github_443_extra_aliases | join(' ') }}`,
    carrying only endpoint-routing keywords (HostName/Port/User/ConnectTimeout).
    Per-key IdentityFile stanzas stay where they are (first-wins keeps them).
  - [ ] ⬜ Verify off-mode cleanly removes the override (`state: absent`).
  - [ ] ⬜ Run QA + `--syntax-check`.

### Phase 3: Temporary host toggle (TDD'd helper + thin CLI)

- [ ] ⬜ **Task 3.1**: `helpers/github443/` Python helper (stdlib only, TDD)
  - [ ] ⬜ Write tests first (`tests/helpers/github443/...`): managed-block
    insert-at-BOF / remove (idempotent), `known_hosts` pin/unpin, 22-vs-443
    probe, control-socket teardown command emission.
  - [ ] ⬜ Pure logic (block rendering, idempotent edit) split from a thin
    side-effecting executor; stable marker output for `changed_when`.
  - [ ] ⬜ Run `python3 -m unittest discover -s tests`.
- [ ] ⬜ **Task 3.2**: User CLI wrapper `github-ssh-443 on|off|auto|status`
  - [ ] ⬜ Deploy via the playbook; calls the helper; `on`/`off` flip the same
    managed blocks, `auto` probes + enables only if 22 blocked & 443 works,
    `status` reports the probe + current config state.
  - [ ] ⬜ `on` also exports/persists `GITHUB_SSH_443=1` for the session so CCY in
    the same shell goes 443 (mechanism decision below).
  - [ ] ⬜ Run QA.

### Phase 4: Always-on host path + propagation to CCY

- [ ] ⬜ **Task 4.1**: profile.d export when `github_ssh_over_443: true`
  - [ ] ⬜ Deploy `/etc/profile.d/zz-github-ssh-443.sh` exporting
    `GITHUB_SSH_443=1` (state gated on the var) so every login shell — and
    thus every `ccy` — inherits 443.
  - [ ] ⬜ Run QA + `--syntax-check`.
- [ ] ⬜ **Task 4.2**: Documentation
  - [ ] ⬜ Update `docs/github-multi-account.md` with the temporary vs always-on
    matrix, the ssh-agent note, and deploy-key alias handling.
  - [ ] ⬜ Add `github_ssh_over_443` + `github_443_extra_aliases` to
    `host_vars/localhost.yml.dist` with comments.

## Dependencies

- Builds on the shipped CCY 443 work (commit `311be26`, CCY 3.20.0) and the
  existing `github_ssh_over_443` host toggle.

## Technical Decisions

### Decision 1: Single runtime signal name

**Context**: three names exist — `--github-443` (flag), `GITHUB_SSH_443` (env),
`github_ssh_over_443` (Ansible var).
**Decision**: keep all three but make `GITHUB_SSH_443` the **runtime truth** that
flows host → container. The flag and the Ansible var are just two ways to *set* it.
No rename (avoids churn); document the relationship.
**Date**: 2026-06-16

### Decision 2 (OPEN): host known_hosts key source — static vs meta API

**Options**: (A) embed GitHub's published `[ssh.github.com]:443` keys statically
(simple, offline, but needs manual rotation); (B) fetch from
`api.github.com/meta` at play time (tracks rotation, needs network at deploy).
**Leaning**: B with A as fallback, mirroring the entrypoint which already fetches.
**Decision**: TBD.

### Decision 3 (OPEN): CLI + helper deployment location on the host

**Context**: the TDD helper lives in the repo; the user CLI must reach it from any
cwd. Options: deploy a launcher that `cd`s to the repo root; or install the helper
module to a stable path. **Decision**: TBD — resolve in Phase 3.

### Decision 4 (OPEN): how the temporary toggle hands 443 to CCY in the same shell

**Options**: (A) the toggle prints `export GITHUB_SSH_443=1` for the user to eval;
(B) it writes a flag file `~/.config/ccy/github-443` that ccy reads; (C) both.
**Leaning**: A is least magical and matches the env-var-is-truth model; ccy already
needs Phase 1 to honour the env. **Decision**: TBD.

## Success Criteria

- [ ] `export GITHUB_SSH_443=1; ccy` runs CCY in 443 mode (no flag needed).
- [ ] `github-ssh-443 on` reroutes host git + a foreign deploy-key alias over 443
  without an Ansible run; `off` cleanly reverts; both reconcile with Ansible.
- [ ] Host `known_hosts` is pinned for `[ssh.github.com]:443` in 443 mode.
- [ ] `github_ssh_over_443: true` + playbook run = always-on host SSH **and** every
  `ccy` inherits 443.
- [ ] ssh-agent untouched; control sockets (if any) torn down by the toggle.
- [ ] QA passes (`./scripts/qa-all.bash`), `--syntax-check` clean, helper unit
  tests pass.

## Risks & Mitigations

| Risk                                                  | Impact | Probability | Mitigation                                                                              |
| ----------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------- |
| Temporary toggle and Ansible blocks drift / duplicate | M      | M           | Identical `blockinfile` markers; toggle reuses the same render logic via the helper     |
| BOF override accidentally reroutes non-GitHub hosts   | H      | L           | Override `Host` patterns are explicit (github.com, ssh.github.com, declared globs only) |
| known_hosts meta-API fetch fails at deploy            | M      | L           | Static published-key fallback (Decision 2)                                              |
| Network also blocks raw SSH-over-443 (DPI/TLS proxy)  | M      | L           | Documented caveat; HTTPS remotes as escape hatch (out of scope)                         |
| CCY container rule: cannot run Ansible here           | —      | —           | Edit + commit only; user deploys + live-tests on HOST                                   |

## Notes & Updates

### 2026-06-16

- Plan created. Current state, the four gaps, and the ssh-agent/control-socket
  analysis verified against the tree. Three open decisions (2, 3, 4) flagged for
  resolution during implementation.
