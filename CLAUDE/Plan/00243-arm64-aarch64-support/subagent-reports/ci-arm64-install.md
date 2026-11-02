# CI lane: the unattended install on real aarch64

Branch `arm/ci-arm64-install` (from F44-arm at 9a05de5e3). The lane is
`.github/workflows/arm64-install.yml`. Its 8th run on the branch was **green**:
<https://github.com/LongTermSupport/fedora-desktop/actions/runs/41394771757> (commit
843855bde, about 24 minutes). That run printed `PLAY RECAP ok=478 changed=174 failed=0`,
then `MAIN INSTALLATION COMPLETE` and `Headless provisioning complete!`.

## What the lane does

- Runs on `ubuntu-24.04-arm`. It is triggered by a push to `F44-arm` or `arm/**`, or by
  `workflow_dispatch`. A newer push cancels the older run of the same ref.
- It installs podman from apt and builds a Fedora 44 image inline (a heredoc Containerfile
  in the step). The image has `@core`, `systemd`, `kernel-core` and a user `ci` in `wheel`.
- It starts the container rootful and `--privileged`, with `--systemd=always`. The
  checkout is mounted read-only at `/src`. It waits until `systemctl is-system-running`
  says `running` or `degraded` (degraded prints the failed units). It asserts
  `uname -m` = `aarch64`.
- The sudo and vault passwords are fresh `openssl rand` values made for each run. They
  reach the container on stdin and are stored as 0600 files under `/run/ci-secrets`. The
  user gets ordinary password sudo, so no NOPASSWD is involved
  (`sudo=password (RUN_BASH_SUDO_PASSWORD_FILE)` in the log).
- It runs `/home/ci/run.bash` (copied from the checkout) with `RUN_BASH_HEADLESS=1`,
  `GITHUB_ACCOUNTS=none`, `CONFIG_SOURCE=none`, `OPTIONAL_PLAYBOOKS=none`,
  `PROVISIONING_PROFILE=server` and `GIT_REF=${{ github.sha }}`. The email and hostname
  are `example.com` values. run.bash clones over HTTPS itself and pins the clone to the
  pushed commit (`checkout pinned at commit 843855bde25f`), so the code that runs is the
  production path.
- The job fails when run.bash fails. The failing task is shown with its `Origin:` line.
  After a failure, an `if: failure()` step prints the failed units, the newuidmap
  diagnostics and the journal. That step only adds information: the job is already red.
  There is no `continue-on-error`.
- QA: `qa-changed` routes workflows to the go gate (`TestEveryWorkflowSetsUpTheGoFloor`;
  this workflow has no setup-go step) and to arch-pin. Both passed on every commit. The
  workflow contains no x86 literal.

## Failures found, run by run

Each failure was **container-only**. None was architecture-related. Every fix is in the
workflow; no play was changed and no play skips anything inside a container.

| Run | Failing task (play)                                                                               | Cause                                                                            | Fix in the workflow                                                                                                                              |
| --- | ------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Display current kernel and installed kernel-core versions (`play-AB-dnf-upgrade.yml`)             | The image has no `kernel-core`                                                   | Install `kernel-core`. Also enable linger and pass `XDG_RUNTIME_DIR` and `DBUS_SESSION_BUS_ADDRESS`, because podman exec opens no login session |
| 2   | Read GRUB Environment Block (`play-basic-configs.yml`)                                            | `grub2-editenv` cannot get the canonical path of the `overlay` root              | Make `/boot` an ext4 loop device created before the container starts                                                                             |
| 3   | (workflow bug) `BOOT_DEV` was unbound                                                             | `GITHUB_ENV` only reaches later steps                                            | Use the local variable                                                                                                                           |
| 4   | Verify ssh-suspend-guard is running (`play-prevent-ssh-suspend.yml`)                              | The service kept restarting; the hand-picked image had no `ss` (iproute)         | Build the image from `@core`, as a minimal install is                                                                                            |
| 5   | Ensure kernel modules loaded for Docker/OpenVPN in LXC containers (`play-lxc-install-config.yml`) | The running kernel's module tree was not in the container                        | Mount `/lib/modules/$(uname -r)` read-only                                                                                                       |
| 6   | Migrate Podman User Namespace State (`play-podman.yml`)                                           | `newuidmap: write to uid_map failed: Operation not permitted`                    | Set `kernel.apparmor_restrict_unprivileged_userns=0` on the runner. This alone was not enough                                                    |
| 7   | Same task                                                                                         | `getcap` shows no file capability on `newuidmap`/`newgidmap` in the built image | `chmod u+s` both helpers in the image                                                                                                            |
| 8   | green                                                                                             |                                                                                  |                                                                                                                                                  |

## Architecture findings

None. The server profile ran to completion on aarch64. The arch-keyed selections did their
job; for example, `Install intel-media-driver (... no aarch64 build, skipped there)` was
skipped by name. No download, package or path failed for the architecture, so DBF was not
needed and the arch-pinned-artefact rule is unchanged.

## Limits of this lane: what it does not prove

- **The server profile only.** No desktop plays, no GNOME and no optional plays. The next
  widening would be to run a list of optional plays (`RUN_BASH_OPTIONAL_PLAYBOOKS=...`) in
  a second job.
- **No reboot, no real boot loader, and no real kernel module set.** The modules are the
  runner's (Ubuntu/Azure) kernel's. `/boot` is a loop device. The Fedora `kernel-core` is
  installed but never booted.
- **The newuidmap setuid change is a difference from Fedora,** which uses file
  capabilities. Rootless podman on the real test bed still needs proving there.
- **The AppArmor sysctl may be unnecessary.** It did not fix run 6 by itself. It was kept
  because nested rootless podman on Ubuntu 24.04 commonly needs it, but it has not been
  shown to be required. The coordinator may drop it as a follow-up.
- The lts-infra test bed (Task 3.1) is still the proof on a real install.
- The lane is **not on every branch**, by design. F44 does not trigger it until
  `F44-arm` merges and the trigger list gains F44, if that is wanted.

## Worktree note

The agent was isolated in a harness worktree, not `/workspace/.claude/worktrees/arm-ci-lane`
(the harness refused any git operation there). The commits were made on the agent's branch
and pushed to `origin/arm/ci-arm64-install`. The local `arm/ci-arm64-install` branch in the
arm-ci-lane worktree is still at 9a05de5e3; fast-forward it from origin before using it.

## Plan state

PLAN.md was left to the coordinator, to avoid conflicting with other branches' edits.
Suggested entry: a task under Phase 3, "CI lane `arm64-install.yml` runs the server-profile
install on aarch64 per push to F44-arm/arm/\*\*; green at run 41394771757".
