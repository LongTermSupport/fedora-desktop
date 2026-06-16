# Plan 00054: Host-level GitHub SSH-over-443, unified with CCY

**Status**: In Progress (implementation + QA complete; HOST deploy + live test pending)
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

### Phase 1: CCY honours the host-level env var (smallest, highest value) ✅

- [x] ✅ **Task 1.1**: Make `claude-yolo` honour an inherited `GITHUB_SSH_443=1`
  - [x] ✅ Precedence now: `--github-443` flag → inherited `GITHUB_SSH_443=1` →
    else `0`. Auto-fallback still runs when `0`. (`claude-yolo` ~line 535.)
  - [x] ✅ Bumped `CCY_VERSION` 3.20.0 → 3.21.0 with a descriptive comment.
  - [x] ✅ `--help` now notes `export GITHUB_SSH_443=1` as the flag's equivalent.
  - [x] ✅ QA green (`./scripts/qa-all.bash` exit 0).

### Phase 2: Host `known_hosts` pin + deploy-key alias override ✅

Folded into the helper (Phase 3) as the single 443-routing mechanism, rather than
a parallel `blockinfile`, so the temporary CLI and the persistent apply share one
source of truth.

- [x] ✅ **Task 2.1**: Pin `[ssh.github.com]:443` in the host `known_hosts`
  - [x] ✅ The helper writes a managed `[ssh.github.com]:443` block in
    `~/.ssh/known_hosts` (present/absent with the 443 state). Keys come from the
    meta API with an `ssh-keyscan -p 443` fallback (Decision 2 → both, runtime).
  - [x] ✅ QA + `--syntax-check` clean (73 playbooks).
- [x] ✅ **Task 2.2**: First-wins BOF override for deploy-key aliases
  - [x] ✅ Added `github_443_extra_aliases` (list, default `[]`) to play vars + dist.
  - [x] ✅ Helper writes one BOF override block matching
    `github.com ssh.github.com github.com-* <extra>` carrying only
    HostName/Port/User/ConnectTimeout; per-key `IdentityFile` stanzas survive
    (first-wins). Per-alias + default play blocks reverted to literal github.com:22
    so routing lives in exactly one place. Verified by smoke test (deploy alias +
    `github.com-work` both rerouted; `off` round-trips to original content).
  - [x] ✅ Off-mode cleanly removes the block (`apply_state(present=False)`).
  - [x] ✅ QA + `--syntax-check` clean.

### Phase 3: Temporary host toggle (TDD'd helper + thin CLI) ✅

- [x] ✅ **Task 3.1**: `helpers/github443/` Python helper (stdlib only, TDD)
  - [x] ✅ Tests written first: `tests/helpers/github443/test_core.py` (25) +
    `test_cli.py` (11). Cover idempotent insert-at-BOF / replace / remove,
    known_hosts render, meta+keyscan parsing, auto decision, env line, file modes,
    fetch fallback, probe parsing. 36 new tests; 71 helper tests total green.
  - [x] ✅ Pure logic (`core.py`) split from the side-effecting executor
    (`cli.py`); stable `GH443-CHANGED` marker for `changed_when`.
  - [x] ✅ `python3 -m unittest tests.helpers.github443.test_core test_cli` green.
- [x] ✅ **Task 3.2**: User CLI `github-ssh-443 on|off|auto|status|env`
  - [x] ✅ Deployed by the play: helper modules → `/usr/local/lib/ccy-helpers/`,
    wrapper `files/usr/local/bin/github-ssh-443`. `on`/`off` flip the managed
    blocks, `auto` probes + enables only if 22 blocked & 443 works, `status`
    reports reachability + state. Verified working from a non-repo cwd via
    PYTHONPATH.
  - [x] ✅ Decision 4 → env var: `github-ssh-443 env` prints `export`/`unset GITHUB_SSH_443` for `eval`, so the same shell (and ccy via Phase 1) follows it.
  - [x] ✅ QA green.

### Phase 4: Always-on host path + propagation to CCY ✅

- [x] ✅ **Task 4.1**: profile.d export when `github_ssh_over_443: true`
  - [x] ✅ `files/etc/profile.d/zz-github-ssh-443.sh` exports `GITHUB_SSH_443=1`;
    deployed when the var is true, removed (`state: absent`) when false. Every
    login shell — thus every `ccy` — inherits 443.
  - [x] ✅ QA + `--syntax-check` clean.
- [x] ✅ **Task 4.2**: Documentation
  - [x] ✅ `docs/github-multi-account.md`: one-signal/two-layer/two-lifetime model,
    temporary CLI vs always-on var, the ssh-agent note, deploy-key aliases, and the
    ccy env-var precedence.
  - [x] ✅ `host_vars/localhost.yml.dist`: documented `github_ssh_over_443`
    (always-on) + `github_443_extra_aliases`.

### Phase 5: Launch banner + one-shot deploy script ✅

- [x] ✅ **Task 5.1**: ccy launch banner when 443 mode is active
  - [x] ✅ `claude-yolo` prints a prominent yellow `🔒 GitHub SSH-over-443 MODE ACTIVE` banner after SSH validation whenever `GITHUB_SSH_443=1`, naming the
    trigger (flag / host env / auto-detected). A pre-validate snapshot
    (`GITHUB_443_PRELAUNCH`) distinguishes env-enable from auto-fallback. So it
    fires for all three paths, including the auto-fallback that flips it mid-launch.
  - [x] ✅ `CCY_VERSION` 3.21.0 → 3.22.0. Brief note added to docs CCY section.
  - [x] ✅ `bash -n` + QA green.
- [x] ✅ **Task 5.2**: `update.bash` in the plan folder
  - [x] ✅ Runs the plays in order (`play-github-cli-multi.yml` →
    `play-claude-yolo.yml`), resolves the repo root from its own location, refuses
    to run inside the CCY container, then interactively offers to enable 443 mode
    now (`github-ssh-443 on`) with always-on guidance. Idempotent, fail-fast,
    shellcheck-clean.

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

### Decision 2 (RESOLVED): host known_hosts key source — static vs meta API

**Decision**: runtime fetch, no embedded keys. `fetch_keys()` tries the meta API
(`api.github.com/meta`, itself HTTPS/443 so it works on the very networks that
block port 22) and falls back to `ssh-keyscan -p 443 ssh.github.com`; it raises
`KeyFetchError` if neither yields keys (fail fast — never pin an empty set).
Avoids both embedded-key rotation staleness and any secret-scanner friction.
**Date**: 2026-06-16

### Decision 3 (RESOLVED): CLI + helper deployment location on the host

**Decision**: install the helper modules to a stable path
(`/usr/local/lib/ccy-helpers/helpers/github443/`) and ship a thin wrapper
(`/usr/local/bin/github-ssh-443`) that runs `PYTHONPATH=/usr/local/lib/ccy-helpers python3 -m helpers.github443.cli`. Self-contained — works from any cwd, no runtime
dependency on the repo clone location. The playbook's own persistent apply still
runs the in-repo copy via `chdir: root_dir` (pyenv pattern). Verified from a
non-repo cwd.
**Date**: 2026-06-16

### Decision 4 (RESOLVED): how the temporary toggle hands 443 to CCY in the same shell

**Decision**: env var (option A). `github-ssh-443 env` prints `export GITHUB_SSH_443=1` / `unset GITHUB_SSH_443` for `eval "$(github-ssh-443 env)"`. No
ccy flag-file path — Phase 1 already makes ccy honour the env var, keeping a single
signal. Always-on uses the profile.d export of the same var.
**Date**: 2026-06-16

## Success Criteria

- [x] ✅ `export GITHUB_SSH_443=1; ccy` runs CCY in 443 mode (no flag needed) —
  code path verified (`claude-yolo` precedence); HOST live-run pending.
- [x] ✅ `github-ssh-443 on` reroutes host git + a foreign deploy-key alias over
  443; `off` cleanly reverts (round-trips to original); both share the helper's
  managed blocks with the Ansible apply. Smoke-tested against a temp `~/.ssh`.
- [x] ✅ Host `known_hosts` is pinned for `[ssh.github.com]:443` in 443 mode
  (helper writes the managed block).
- [x] ✅ `github_ssh_over_443: true` + playbook = always-on host SSH **and** every
  `ccy` inherits 443 (profile.d export of `GITHUB_SSH_443=1`). Wiring complete;
  HOST `ansible-playbook` run pending (CCY container cannot deploy).
- [x] ✅ ssh-agent untouched (documented); control sockets torn down by the toggle
  (`teardown_control_sockets`).
- [x] ✅ QA passes (`./scripts/qa-all.bash` exit 0, 316 files), `--syntax-check`
  clean (73 playbooks), 71 helper unit tests pass.
- [ ] ⬜ **HOST verification** (user, not CCY): run
  `ansible-playbook playbooks/imports/play-github-cli-multi.yml`, then
  `github-ssh-443 status`, a real `git ls-remote`, and `ssh -T -p 443 git@ssh.github.com`.

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
- All four phases implemented. Phase 1 (ccy honours inherited `GITHUB_SSH_443`)
  shipped as its own commit (CCY 3.21.0). Phases 2–4 landed together: the
  `helpers/github443` module (36 TDD tests) is the single source of truth for host
  443 routing — one first-wins BOF override block + a `[ssh.github.com]:443`
  known_hosts pin — driven by both the `github-ssh-443` CLI (temporary) and the
  play (always-on, gated on `github_ssh_over_443`, plus a profile.d export of the
  env var so ccy inherits it). Decisions 2/3/4 resolved (runtime key fetch;
  `/usr/local/lib/ccy-helpers` + wrapper; env-var signal). Per-alias/default play
  blocks reverted to literal github.com:22 so routing lives in one place. QA green
  (316 files), syntax-check clean (73 playbooks), 71 helper tests pass.
- **Remaining**: HOST-only deploy + live test (this is a CCY container — edit/commit
  only). See the unchecked Success Criterion.
- **Runtime regression found + fixed during HOST deploy.** The play var
  `github_443_extra_aliases: "{{ github_443_extra_aliases | default([]) }}"` is a
  self-default that aborts under ansible-core 2.19 ("Recursive loop detected in
  template") when the apply task's `argv` is finalized. It passed BOTH
  `qa-all.bash` and `--syntax-check` because neither evaluates templates — the
  failure is runtime-only. Fix: dropped the self-referential play var and apply
  `| default([])` at the point of use in the `argv`. Reproduced the recursion and
  verified the fix with throwaway plays. **QA hardened**: added a `qa-ansible.bash`
  self-default check (PCRE backreference, targets the `\1 | default` idiom only so
  it doesn't false-positive on `blockinfile` literals like nordvpn's
  `x: {{ x }}`). Documented in `CLAUDE/QA.md` and `CLAUDE/AgentNotes.md`.
- **Restricted-network chicken-and-egg fixed (found on first HOST run, port 22
  blocked).** Two more port-22 hardwires that only fail at runtime on a blocked
  network:
  1. The "Verify SSH access" probe targeted only the configured endpoint
     (`github.com:22` when the toggle is off), so the play died with "Connection
     refused" before 443 could help. Fix: the probe now falls back to
     `ssh.github.com:443` (same host keys/identity) when the primary endpoint does
     not authenticate — the same auto-fallback pattern ccy uses. Verification is
     now port-agnostic; ansible task `timeout` 30→45 to fit two attempts.
  2. The deployed per-account git wrappers (`git()` auto-detect, `git-<alias>()`,
     `clone-<alias>`) forced `ssh -F /dev/null -o HostName=github.com`, which
     isolates the key but also bypasses the `~/.ssh/config` 443 override — so even
     with 443 enabled, wrapper-driven `git push` stayed on port 22. Fix: a shared
     `_gh443_sshcmd` helper in the deployed include builds the sshCommand from the
     `GITHUB_SSH_443` runtime signal (ssh.github.com:443 when set, else
     github.com:22; unset → byte-identical to the old string). Key-isolation model
     untouched. Note: wrapper users need `GITHUB_SSH_443` in the shell — automatic
     with always-on (profile.d) or `eval "$(github-ssh-443 env)"`; plain `git`
     with `git@github.com-<alias>:` remotes already routes via the override block.
     Everything else in the play (key audit, override apply, key fetch) already used
     the GitHub HTTPS API / meta endpoint, so it works on a port-22-blocked network.
