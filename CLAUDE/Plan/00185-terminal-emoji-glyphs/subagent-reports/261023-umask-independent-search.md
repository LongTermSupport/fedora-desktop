# Independent search: Ansible launched under the caller's umask

Searcher report. I did not open callback_plugins/, tests/helpers/umask_pin/, or any file named "umask"; I did not run the detector or Ansible.

Caveat on the tree: the tree changed while I searched (playbooks/imports/play-fonts.yml, ansible.cfg, the 00185 plan scripts and meta-deploy.list were modified, and an untracked callback plugin appeared). My first reads of play-fonts.yml and ansible.cfg predate those edits. play-fonts.yml line numbers below are from the final read. A comment in play-fonts.yml names the pin; I read it only as part of reading the play and did not follow it.

## Part 1. Launch points

Result in one line: NOTHING first-party pins the umask before Ansible starts. There is no `umask` call in run.bash, CLAUDE/Plan/_planlib.inc.bash, CLAUDE/Plan/meta-deploy.bash, the self-update helper or its systemd unit, the playbook shebangs, fedora-desktop-health, or the Go launchers. The `umask 077` in files/var/local/claude-code/cc:26 is the only one on the launch path and it is the source of the problem. The systemd units that run Ansible set no `UMask=` (only agent-bus-hs@.service does, and it does not run Ansible).

Columns: pin = umask pinned before ansible starts; cwd = whether ./ansible.cfg is loaded.

### A. Starts real tasks (can carry the hazard)

| # | file:line | what | pin | cwd / config |
|---|-----------|------|-----|--------------|
| A1 | playbooks/**/*.yml line 1 (all 68 plays) | shebang runs `run.bash "$p" "$@"` after `realpath` | no | run.bash cds to its own directory, so yes |
| A2 | run.bash:2228-2229, 2263-2272 | single-play mode: `cd "$_play_repo"`, then `ansible-playbook "$_play_abs"` (three variants: become-password-file, plain, headless) | no | repo root (script's own dir), ./ansible.cfg |
| A3 | run.bash:1993-1994, 1941-1948, 912 | rerun / changed / chooser path and the optional-playbook loop: `cd "$_play_repo"` then ansible-playbook | no | repo root |
| A4 | run.bash:3588-3597 | main run: `ansible-playbook playbooks/playbook-main.yml` | no | cwd is the hard-coded `~/Projects/fedora-desktop` (cd at run.bash:3104, 2718, 3857; 843 for optional). NOT the script's own directory, so a run.bash in another copy of the repo runs the hard-coded one's playbooks. Loads that copy's ./ansible.cfg |
| A5 | run.bash:3537 | `ansible-galaxy install -r requirements.yml` | no | same cwd as A4 |
| A6 | CLAUDE/Plan/_planlib.inc.bash:662 (`plan_ansible_playbook`) | `( cd "$PLAN_REPO_ROOT"; exec ansible-playbook ... )`; used by the plan scripts listed below | no | repo root, explicit cd |
| A7 | CLAUDE/Plan/_planlib.inc.bash:674 (`plan_ansible_adhoc`) | `exec ansible -v ... -m ansible.builtin.copy`, from 00203 _acceptance-robust.inc.bash:252, 00211 _acceptance-stage1.inc.bash:107 and stage2:85, 00213 _acceptance.inc.bash:82 | no | repo root |
| A8 | CLAUDE/Plan/meta-deploy.bash | the host entry point: runs entries as playbooks/*.yml (shebang), plan scripts, or `run.bash --changed --yes` | no | via A1, A6, A9 |
| A9 | CLAUDE/Plan/00141-run-bash-changed-plays/run-changed.bash:51 | `run.bash --changed --yes` | no | via A3 |
| A10 | CLAUDE/Plan/00054-*/update.bash:32,54,58 (the 443 host-level plan) | `cd "$REPO_ROOT"` then two ansible-playbook calls | no | repo root |
| A11 | CLAUDE/Plan/00055-container-process-watchdog/deploy.bash:20,22 | `cd "$REPO_ROOT"; exec ansible-playbook` | no | repo root |
| A12 | CLAUDE/Plan/00058-*/deploy.bash:25,46 (the version-pin plan) | `cd "$repo_root"` then ansible-playbook | no | repo root |
| A13 | CLAUDE/Plan/00061-headless-server-provisioning/acceptance.bash:25,33,88,106,123 | cd to repo root, `export ANSIBLE_CONFIG="$REPO_ROOT/ansible.cfg"`, ansible-playbook | no | repo root, ANSIBLE_CONFIG set |
| A14 | CLAUDE/Plan/00062-disk-reclaim-tui/deploy.bash:43 | `ansible-playbook "$PLAY"` where PLAY is the plan-folder file podman-store-repair.yml | no | NO cd and no ANSIBLE_CONFIG: runs from the caller's cwd, so ./ansible.cfg loads only if the caller is in the repo root. Unsure on config |
| A15 | CLAUDE/Plan/00062-disk-reclaim-tui/podman-store-repair.yml:1 | legacy shebang `#!/usr/bin/env ansible-playbook`; its doc line 44 and files/home/.local/bin/reclaim:355 tell the user to run it with bare `ansible-playbook <path>` | no | caller's cwd, no cd |
| A16 | CLAUDE/Plan/00063-headless-run-bash-server-cloud-provisioning/acceptance.bash:21 | `RUN_BASH="$REPO_ROOT/run.bash"`, run headless | no | via A2 |
| A17 | helpers/self_update/cycle.py:818 (`run_play`), `_as_user` at 727-733 | root cycle runs `runuser -u <user> -- env -i ... <deploy copy>/run.bash --headless <play>` with cwd = the deploy copy and search paths pinned in the env (`play_environment`, cycle.py ~617-650). Entered from files/usr/local/sbin/fedora-desktop-self-update, started by files/etc/systemd/system/fedora-desktop-self-update.service (no `UMask=`, so systemd's 0022) | no (inherits 0022) | deploy copy root; config paths pinned by env, not ANSIBLE_CONFIG |
| A18 | files/home/.local/bin/fedora-desktop-health.j2:129 | `"$ROOT_DIR/run.bash" --rerun`; the panel extension (extensions/fedora-desktop@fedora-desktop/sections/plays.js) opens a terminal running the same | no | via A3 |
| A19 | fedora-install/ks.cfg:680,684 | first-boot terminal: `cd '$SETUP_DIR' && ./run.bash` | no | repo root |
| A20 | src-go/internal/vmtest/steps.go:104 | in the VM guest: `TERM=dumb bash ~/run.bash` with RUN_BASH_* env | no | guest home; run.bash does its own cd (A4) |

Plan scripts calling `plan_ansible_playbook` / `plan_ansible_adhoc` (A6/A7; text search over CLAUDE/Plan excluding Completed/ and Cancelled/): 00092, 00098, 00104, 00109 (deploy.bash and acceptance.bash:637), 00134, 00137, 00139, 00144, 00148, 00158, 00160, 00161 (deploy.bash:150,152; _deploy-steps.inc.bash:133), 00165, 00169, 00171, 00173, 00185 (deploy.bash:60), 00192 (deploy.bash:119-123, deploy-rclone-cache-status.bash:56, deploy-rclone-tail.bash:56), 00195, 00196, 00203, 00211, 00213, 032. CLAUDE/Plan/Archive/ holds only .md files.

### B. Launches Ansible but runs no task that writes files (not this hazard)

| file:line | what |
|-----------|------|
| src-go/qa/ansiblesyntax/check.go:52, src-go/qa/changed/checks.go:371 | `ansible-playbook --syntax-check`; cwd = repo root, ANSIBLE_CONFIG pinned absolute (run.go:138, checks.go:369) |
| src-go/internal/ghaccount/app.go:422 | `ansible localhost -m debug` decrypting one vault value; Dir = repo root, ANSIBLE_CONFIG pinned |
| run.bash:2480, 3378 | `ansible localhost -c local -m debug` vault probe |
| run.bash:3467, fedora-install/push.bash:330, scripts/setup-rclone.bash:260 | `ansible-vault encrypt_string` (the last two pin ANSIBLE_CONFIG) |
| scripts/qa-ansible*.bash | ansible-lint |
| scripts/test-qa-tool-pin-converges.bash:62 and scripts/test-*-signing-declared.bash:61 | real tasks on localhost with a throwaway ANSIBLE_CONFIG, temp dir, no become |
| .github/workflows/qa.yml | syntax-check only |

### C. Mentions only (text matches that are not launches)

Echoed "Run: ansible-playbook ..." hints: files/home/.local/bin/nord:622, rclone-cache-warm:34, rclone-rc-auth.bash, files/var/local/claude-yolo/*, files/var/local/docker-in-lxc:283, scripts/desktop-symlinks:67, scripts/nvidia-status.bash:353, many playbooks, and src-go internals (open/mime.go:62, lxcfreeze, podfreeze, rclonerc, ftpcamera). scripts/make-playbooks-executable.bash:21 shows a legacy shebang in a comment. playbooks/CLAUDE.md still documents the legacy `cd ... && exec ansible-playbook` shebang although every play uses the run.bash form (documentation drift).

### D. Other umask sites

files/var/local/claude-code/cc:26 (the origin) and files/var/local/claude-yolo/entrypoint.sh:23 (container) are the only process-wide ones. Every other match is a local subshell or a Python helper pinning its own writes: history-search.bash:24, agent-bus:110, agent-bus-install:731, image-build-lock.bash:46, cc-teams.bash:113/125, token-handoff.bash:71, session-registry.bash, network-management.bash:144, agent-bus-membership.bash:81, ssh_agent_filter.py:259, helpers/agent_bus_lxc/members.py:151/283/317/337, src-go vmtest. Plan 00161's lxc-attach defect (test_u23_kit_unpack.py, members.py:125-151) is an earlier instance of the same class, fixed inside the guest script, not at the launch.

## Part 2. Root tasks that create or rewrite files with no explicit mode

Method: the 87 YAML files under playbooks/ and tasks/ (there is no roles/project) were parsed with a YAML loader into 1486 task records (play-level, block-level and task-level `become` resolved, handlers included); 892 of them run as root (`become: true`, no other `become_user`). tasks/*.yml inherit become from the including play and every file task there carries a mode (build-go-command.yml:68,110; deploy-freeze.yml:33), so they are not instances. Records were filtered by module and by `mode:`, and every `command`/`shell` root task (138) was read by hand. Root-task counts by kind: copy 151, file 111, command 108, dnf 68, systemd 39, shell 30, package 30, blockinfile 20, template 14, get_url 9, lineinfile 8, unarchive 5, user 5, others fewer. All 14 root `template` tasks, all 9 root `get_url` tasks and 149 of 151 root `copy` tasks carry a `mode:`.

### 2a. Hazard (consumer is a non-root user) with confidence

| # | file:line | form | why other users need to read it | confidence |
|---|-----------|------|---------------------------------|------------|
| H1 | playbooks/imports/play-fonts.yml:85-87 (handler rebuild-font-caches, notified from 47-56 and 69-78) | `command: fc-cache -f` as root | /usr/lib/fontconfig/cache/* rewritten 0600 root; the desktop user falls back to a stale user cache. The originating instance. The second handler at 94-96 is `become: false` (user-owned, fine) | confident |
| H2 | playbooks/imports/play-basic-configs.yml:522-530 (Install YQ) | `shell: wget -qO /usr/bin/yq ... && chmod +x /usr/bin/yq`, `creates: /usr/bin/yq` | The redirect makes /usr/bin/yq 0600; `chmod +x` with no who-class applies the umask, so under 077 only u+x is added: 0700 root. The desktop user cannot run yq, and because of `creates:` a re-run never repairs it | confident |
| H3 | playbooks/imports/optional/common/play-claude-devtools.yml:53-60 | `ansible.builtin.git` as root into /opt/claude-devtools (parent made 0755 at 44-51) | git writes files 0666 and dirs 0777 minus the umask: 0600/0700 root-only. The next task (62) runs `podman build` as the user and reads that tree | confident |
| H4 | playbooks/imports/optional/common/play-self-update.yml:413-420 | `ansible.builtin.git` as root, the deploy copy of the repo | The play's own comment (459-460) says "world-readable (the plays read it as the user)"; the assert at 490-501 checks owner root and no group/other WRITE bit, but nothing checks readability. Under 077 the unattended cycle (`runuser` into this tree, cycle.py:818) cannot read run.bash | confident |
| H5 | playbooks/imports/optional/common/play-self-update.yml:652-666 | `command: ansible-galaxy collection install --force -p <system collections dir>` as root | The cycle loads these collections as the user (play_environment, cycle.py ~617); the assert at 684-715 checks only owner and write bits, not read bits | confident on the consumer; unsure whether galaxy applies the umask to extracted files |
| H6 | playbooks/imports/play-suspend-and-lid-policy.yml:321-329 | `command: dconf update` as root | rewrites every compiled database under /etc/dconf/db; a database the user cannot read is ignored by that user's GNOME session (the play's comment at 298-312 says the compile is what makes the settings take effect) | confident on the consumer; unsure whether dconf applies the umask or sets its own mode |
| H7 | playbooks/imports/play-basic-configs.yml:141-146 | `blockinfile` with `create: true` on /etc/vimrc.local, no `mode:` | a new file takes the umask (0600 root); read by every user's vim. The only root blockinfile/lineinfile with `create: true` and no mode (the other 14 `create: true` sites all set `mode:`) | confident |

### 2b. Unsure: could carry the hazard, needs a fact I could not establish by reading

| file:line | form | what is unknown |
|-----------|------|-----------------|
| play-browsers.yml:208, 230; play-git-configure-and-tools.yml:302; optional/common/play-docker.yml:87 | `dnf config-manager addrepo --from-repofile=...` writes /etc/yum.repos.d/*.repo | whether dnf5 applies the umask; a user-run dnf query would need to read it |
| play-rpm-fusion.yml:25 | `config-manager setopt` rewrites existing repo files | rewrite may keep or change mode |
| play-terminal-emulators.yml:46; play-lxc-install-config.yml:40 (copr module); play-ZZ-repo-cleanup.yml:31 | `dnf copr enable/remove` writes /etc/yum.repos.d/_copr* | same |
| play-browsers.yml:181; optional/hardware-specific/play-nvidia.yml:104 | `yum_repository` module, no mode, new file | the module uses the umask for a new file; consumer is root dnf (probably root-only-is-fine) |
| play-vscode.yml:25-31 | shell `echo -e ... > /etc/yum.repos.d/vscode.repo` | new file 0600; consumer root dnf (probably root-only-is-fine) |
| play-comms.yml:29,35,55,65; optional/common/play-agent-bus-element.yml:65,73; optional/common/play-videography.yml:29,35 | `flatpak` / `flatpak_remote` / `flatpak override` as root, system installation | flatpak run as root may write /var/lib/flatpak under the caller's umask; users must read the whole tree. Not established whether flatpak resets the umask |
| optional/common/play-network-tools.yml:57; optional/hardware-specific/play-nvidia.yml:157 | `unarchive` as root, no mode, into /usr/local and /opt/cudnn | root tar usually keeps archive modes, but implicit parent directories take the umask |
| optional/hardware-specific/play-nvidia.yml:184 | `ldconfig` | glibc sets 0644 on /etc/ld.so.cache itself, I believe; unverified |
| optional/hardware-specific/play-nvidia.yml:225 | `kmodgenca -a` | creates /etc/pki/akmods/certs; the certificate is read by root tools, the private key must be 0600 |
| optional/hardware-specific/play-nvidia.yml:343-352; play-displaylink.yml:222; optional/experimental/play-virtualbox-windows.yml:46 (handler) | `akmods --force`, `dracut --force`, `dkms autoinstall`, `akmods --kernels` | module files are installed by rpm or dkms with their own modes; dracut writes /boot images 0600 by design |
| optional/archived/play-tlp-battery-optimisation.yml:48,57 | `copy` with no mode to /etc/tlp.d/*.conf | the only two root `copy` tasks without a mode; tlp reads them as root, so root-only-is-fine in practice; the play is archived |
| playbooks/dev/play-collect-diagnostics.yml:54 | runs scripts/collect-diagnostics.bash as root, which `mkdir -p`s and redirects into an output dir | no chown or chmod there; whether the user must read the output depends on where the output dir lives |
| dnf (68) and package (30) root tasks | package installs | rpm resets its own umask to 022 for the transaction, I believe; unverified here, so not counted |

### 2c. Looked at: root-only-is-fine, or no new file is created

- `file` tasks without mode: play-basic-configs.yml:251 (creates ~/.local and ~/.local/state owned by the account; owner-only is fine) and optional/common/play-unifi-controller.yml:122 (recursive chown, creates nothing). The other root `file` tasks without mode are `state: absent` or links.
- `lineinfile`/`blockinfile` editing existing system files (a rewrite keeps the file's mode): play-basic-configs.yml:125 (/etc/sudoers, validated), :611 (/etc/dnf/dnf.conf), play-lxc-install-config.yml:255,261 (/etc/sysconfig/lxc-net), :377 (/etc/sysctl.conf), optional/common/play-advanced-kernel-management.yml:27, play-hd-audio.yml:149,161, play-ftp-camera.yml:123 (/etc/shells), play-rclone.yml:126 (/etc/fuse.conf). I did not verify that each target exists before the task runs. Also play-basic-configs.yml:204,316 and optional/common/play-rust-dev.yml:52 (`{{ item }}` targets, no create).
- `sysctl` (optional/common/play-self-update.yml:720, optional/experimental/play-docker-in-lxc-support.yml:57) and `modprobe` (play-lxc-install-config.yml:390, play-docker-in-lxc-support.yml:16, play-docker-overlay2-migration.yml:100): drop-ins read by root (systemd-sysctl, systemd-modules-load); root-only-is-fine. The persistent module-load file at play-docker-in-lxc-support.yml:29 has `mode: "0644"`.
- `tempfile` play-agent-bus.yml:321: private by design, consumed by the root installer.
- `rpm_key`, `uri`, `acl`, `user`, `group`, `systemd`, `firewalld`, `nmcli`, `udevadm`, `systemd-tmpfiles`, `restorecon`, `mv` (ftp-camera:89, preserves modes): no new file under the caller's umask.
- Helpers run as root through `command: python3 -m ...`: helpers.pingbus.bundle (play-claude-yolo.yml:386) writes with an explicit `fchmod` 0755 (bundle.py:135); helpers.agent_bus_lxc.members pins its own umask; helpers.agent_bus.{config_restart,bridge,timer_state} and the agent-bus installer write under /var/lib/agent-bus (secrets, deliberately owner-only; I did not audit each file there).

## What each technique found

Text search found: the whole set of Ansible launch lines (ansible-playbook, ansible, ansible-galaxy, ansible-vault, ANSIBLE_CONFIG, run.bash across bash, Python, Go, YAML shebangs, systemd units, kickstart); the absence of any umask pin on the launch path (umask over run.bash, Plan libraries, helpers, files/etc, systemd units); the `create: true` list (15 sites); and the file-generating tool census over playbooks/ (fc-cache, dconf update, ldconfig, glib-compile-schemas, update-desktop-database, akmods, dkms, ssh-keygen, tee, redirects into /etc /usr /opt /var). No instance of update-mime-database, gtk-update-icon-cache, systemd-hwdb, mandb or update-ca-trust exists in playbooks/ or tasks/. glib-compile-schemas (play-gnome-shell-extensions.yml:106, play-speech-to-text.yml:389) and update-desktop-database (play-gsettings.yml:87, play-md-viewer.yml:164) run as the user, not root, so they are not instances.

Reading found what text search could not: that `chmod +x` after a redirect is umask-dependent (H2); that `become` is inherited from play and block level, so a task with no `become:` line can still be root (all of 2a); that the git module and `ansible-galaxy` create trees under the umask with no `mode:` to match on (H3-H5); that the "world-readable" comment at play-self-update.yml:459 is backed by an assert that checks only write bits (H4, H5); that `dconf update` rewrites every compiled database (H6); that run.bash's main path (A4) cds to a hard-coded directory rather than its own, and that plan script 00062 (A14, A15) neither changes directory nor pins ANSIBLE_CONFIG; that the self-update unit passes through systemd's default 0022 (A17); and that blockinfile/lineinfile on an existing file keeps its mode, so only the `create: true` one counts (H7). The YAML parse is a third, structural technique: it produced the per-module counts and the root-task filter.

## Searches run (forms looked for)

1. `ansible-playbook|ansible-pull|ansible-galaxy|ansible-vault|ansible localhost|ANSIBLE_CONFIG` over the repo excluding vendor, Completed/, Cancelled/, untracked/, callback_plugins/, umask-named files; then per-language views (src-go non-test, helpers, scripts, files, fedora-install, vault.bash, .github, extensions, kickstart).
2. `run.bash` invocations in files, scripts, helpers, src-go, CLAUDE/Plan scripts (exec, direct call, ExecStart, runuser).
3. `umask` (case sensitive and insensitive) over the repo, and for `UMask=` over files/etc, playbooks, tasks, vars, environment.
4. First line of every playbook (all use the run.bash shebang; the plan-folder play 00062 uses the legacy form).
5. YAML parse of playbooks/ and tasks/ with module/become/mode extraction; manual read of all 138 root command/shell tasks; reads of play-fonts, play-basic-configs, play-self-update, play-claude-devtools, play-vscode, play-suspend-and-lid-policy, play-lxc-install-config around each hit.
6. `create: true|yes` with the nine surrounding lines for mode/owner/become.
7. File-generator names over playbooks and tasks (list above).
8. Helpers read for explicit modes: pingbus/bundle.py, agent_bus_lxc/members.py; scripts called by root tasks: collect-diagnostics.bash.

Saturation: the last three searches (generator names, `create: true`, run.bash invocations) turned up nothing not already listed.

## Totals

Part 1: 20 launch points that start real tasks (A1-A20; A14-A15 may not load ./ansible.cfg, A4 uses a hard-coded directory), 7 that start Ansible without file-writing tasks (section B), and none that pin the umask.
Part 2: 7 hazard instances I am confident about (H1-H7), 13 rows of unsure instances (2b), and the root-only-is-fine set in 2c.
