# Plan 00118: ccy on an SELinux-enforcing host — the container cannot read the project it was given

**Status**: In Progress
**Created**: 2026-09-14
**Owner**: joseph
**Priority**: High

## Overview

On a Fedora host with SELinux **Enforcing**, a ccy container cannot read its own workspace.
Measured on a headless box: `podman run -v "$PWD:/workspace"` and `head /workspace/README.md`
answers `Permission denied`, and the audit log says why — `container_t` denied `read` on
`user_home_t`. The same denial covers a mounted key file (`ssh_home_t`) and a copy of it in
`/tmp` (`user_tmp_t`). Nothing in ccy relabels or disables labelling, so every bind it makes
of a home-directory path is unreadable inside the container on such a host.

ccy has been working on desktops because they are not enforcing. `docs/containerization.md`
describes SELinux "in permissive mode for container compatibility (configured by playbook)",
and a ccy container on such a desktop runs as `container_t` and reads `user_home_t` freely —
which is only possible when nothing is enforced. The kickstarts, meanwhile, install every
machine `selinux --enforcing`. So the mount model has been resting on a posture the installer
does not set, and a box that keeps the installer's posture — every headless server this
project provisions — gets a ccy that starts and then cannot see the project.

The fix is podman's own answer for a project directory: mount it with `:Z`, the private
relabel, so exactly this container's MCS category can read it. Key files are not relabelled
in place — a `:z` on `~/.ssh/<key>` changes the label of the person's own key file — but
staged: copied into a per-session directory under `$XDG_RUNTIME_DIR` (tmpfs, owner-only,
removed when ccy exits) and that directory mounted `:Z`. Both happen only when the host is
enforcing, decided from the engine's own report and `getenforce`, so a permissive desktop's
launch is byte-identical to today's.

What this plan does NOT relabel: the tracked per-project extra mounts (`.claude/ccy/mounts`),
whose no-relabel rule stands for the reason it states, and the display sockets, which are a
`connectto` to an unconfined compositor that no file label permits. On an enforcing host those
stay unreadable and the launch says so.

## Goals

- On an enforcing host, a ccy container reads and writes `/workspace` and reads every mounted
  key file; `git fetch` inside works with a mounted deploy key.
- On a permissive or non-SELinux host, the `podman run` line is unchanged.
- The decision is a pure function of the engine report and `getenforce` output, unit-tested
  like the rootless guard.

## Non-Goals

- `--security-opt label=disable` for every container. Confinement is worth keeping; it is
  dropped only where a socket makes it unavoidable (`--ssh-agent`, Plan 00116).
- Relabelling extra project mounts or display sockets.
- Changing the desktop's SELinux mode. Whether desktops should be enforcing is a separate
  question this plan records and does not answer.

## Tasks

### Phase 1: Decide

- [x] ✅ **Task 1.1**: `selinux_enforcing_verdict <getenforce-output> <engine-selinux-report>` in
  `lib/common-pure.bash`: `enforcing` only when `getenforce` says `Enforcing` AND the engine
  reports `selinuxEnabled: true`; `off` otherwise; `unknown` for an unreadable answer, which
  is treated as enforcing (relabelling a readable tree costs nothing; not relabelling an
  unreadable one costs the session). `ccy_selinux_mode` in `common.bash` gathers both inputs.
- [x] ✅ **Task 1.2**: `scripts/test-ccy-selinux-verdict.bash` covers every input pair.

### Phase 2: Mount — BLOCKED BY Phase 1

- [x] ✅ **Task 2.1**: The workspace carries `:z` (shared, so a second session on the same
  project still reads it) and the config-import directory `:Z` when enforcing.
- [x] ✅ **Task 2.2**: Key files are staged into `$XDG_RUNTIME_DIR/ccy/<container>/keys/`
  (0700 dir, 0600 copies, removed by `cleanup`) and that directory is mounted `:Z,ro`;
  `SSH_KEY_PATHS` names the staged paths. When not enforcing the originals are mounted as
  today.
- [x] ✅ **Task 2.3**: The launch banner names the mode; the extra-mount validator and docs
  say what stays unreadable on an enforcing host.

### Phase 3: Proof — BLOCKED BY Phase 2

- [ ] ⬜ **Task 3.1**: Live on an enforcing headless box: `ccy` in a project with a deploy-key
  remote reads `/workspace` and completes `git fetch` inside.
- [ ] ⬜ **Task 3.2**: `qa-all.bash` green; `CCY_VERSION` minor bump.

## Success Criteria

- [ ] `head /workspace/README.md` inside a ccy container on an enforcing host succeeds.
- [ ] A mounted key authenticates to GitHub from inside the container on that host.
- [ ] `git diff` of the `podman run` argument list between a permissive and an enforcing host
  differs only by `:Z` suffixes and the staged-key directory.

## Delivery & Milestones

- Opened from fedora-desktop Plan 00116's first live proof, which found the workspace itself
  unreadable before any key was tried.
