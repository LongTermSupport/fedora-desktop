# Ansible Correctness Audit

## Scope & Method

**Scope**: `playbooks/` (75 playbooks: `playbook-main.yml`, 31 core imports, 43 optional), `tasks/ensure-jq.yml`, `vars/` (3 files), `environment/` (inventory, `localhost.yml.dist`). Vendor roles excluded per brief. Audit dimension: Ansible correctness — idempotency, fail-fast compliance, module usage, Ansible 2.19 parser hazards, container-engine rule, version assumptions, become/user targeting, undefined variables, duplication.

**Method** (systematic, not sample-based):

1. Enumerated every YAML file under the in-scope directories.
2. Ran `ansible-playbook --syntax-check` (ansible-core **2.19.10**, the project's known-hazard version) against **all 75 playbooks** — **all pass**. This conclusively clears the two known 2.19 parser hazards (apostrophes/backticks in `#` comments inside `shell: |` blocks; `: -<letter>` in unquoted task names).
3. Scripted PyYAML AST sweeps over every play/task (including `block`/`rescue`/`always` nesting): (a) `shell`/`command`/`raw`/`script` tasks lacking `creates`/`removes`/`changed_when` → 43 hits, each triaged by reading the playbook; (b) file-deploying tasks (`copy`/`template`/`file`/`unarchive`/`get_url`) lacking `mode` → 15 hits.
4. Pattern sweeps: `failed_when`/`ignore_errors` annotations, `get_url` checksums, hardcoded Fedora versions, hardcoded `podman`/`docker`, flatpak/flathub usage, duplicate shebangs.
5. Cross-referenced variables consumed by playbooks against `environment/localhost/host_vars/localhost.yml.dist` and the (untracked) live `localhost.yml`.
6. Full reads of every playbook implicated by a sweep hit: rpm-fusion, python, gsettings, comms, videography, markless, toolbox-install, lxc-install-config, git-configure-and-tools, rust-dev, cloudflare-warp, lastpass, qobuz-cli, unifi-controller, basic-configs, nvm-install, claude-code, gnome-shell-extensions, AA-preflight-sanity, AB-dnf-upgrade, ZZ-repo-cleanup, github-cli-multi (relevant sections), docker (relevant sections), lxde-install, fast-file-manager (relevant sections), playbook-main.

## Summary

The fail-fast annotation discipline is excellent: **every** `failed_when: false` / `ignore_errors: true` in the tree carries a justified `# FAIL-FAST-OK:` annotation, and the probe-then-fail pattern is used correctly throughout (no violations found). All playbooks parse cleanly on Ansible 2.19. The newer playbooks (AB-dnf-upgrade, ZZ-repo-cleanup, github-cli-multi, darktable-ai-\*, nvidia, photography) are high quality: checksum-pinned downloads, ordering asserts, `set -eo pipefail`, `always:` cleanup of secret temp files.

The defects cluster in the **older, hand-rolled shell-task playbooks**. The two most serious: (1) multi-command `shell: |` blocks without `set -e`, where an intermediate command can fail while the task reports success because only the last command's exit code counts — a direct violation of the project's #1 fail-fast rule; (2) `play-cloudflare-warp.yml`, which can write an **empty repo file on curl failure and then permanently skip repair** via its `creates:` guard, and whose `systemd-resolved` configuration is deployed with the reload handler **commented out**, so the DNS change silently never applies. There is also one clear violation of the Podman-first `container_engine` rule (`play-unifi-controller.yml`), several become/idempotency bugs, and onboarding gaps in `localhost.yml.dist`.

______________________________________________________________________

## ANS-01: Multi-command `shell: |` blocks without `set -e` mask intermediate failures

**Severity: high** — direct violation of the project's #1 rule (Fail Fast).

An Ansible `shell` task's success is the exit code of the **last** command. In a multi-line block without `set -e`, any earlier command can fail and the task still reports OK, silently leaving the system half-configured. The repo's own style guide (`CLAUDE/AnsibleStyle.md` → "External Repository Integration") and QA semgrep rules target exactly this class, but only for standalone `.bash` files — embedded playbook shell is unchecked.

**Evidence** (all read in full):

- `playbooks/imports/play-rpm-fusion.yml:8-16` — seven `dnf` commands under `set -x` only (no `set -e`). If `dnf -y group install multimedia --allowerasing` fails, the final `dnf -y install intel-media-driver` still succeeds and the task reports OK. RPM Fusion is core (`playbook-main.yml:17`), so a broken multimedia stack would be masked on every fresh install.
- `playbooks/imports/play-git-configure-and-tools.yml:14-26` — eleven `git config` commands; a failure in any of the first ten is masked by the last succeeding.
- `playbooks/imports/optional/common/play-rust-dev.yml:67-75` (`rustup update stable; rustup default stable`), `:153-160` — update failure masked by `rustup default` succeeding.
- `playbooks/imports/play-lxc-install-config.yml:72-75` — `firewall-cmd --zone=trusted --change-interface=lxcbr0 --permanent` failure masked by `firewall-cmd --reload` succeeding; the bridge silently never enters the trusted zone.
- `playbooks/imports/play-nvm-install.yml:60-65` — `nvm install`/`use`/`alias` chain without `set -e` (partially mitigated by `creates:`).
- `playbooks/imports/optional/common/play-lastpass.yml:129-139` — `lpass login …; lpass sync` — login failure masked if sync exits 0.
- `playbooks/imports/optional/common/play-cloudflare-warp.yml:11-13` — see ANS-02.

**Contrast (done right)**: `play-AB-dnf-upgrade.yml:150-162` (`set -eo pipefail`), `play-basic-configs.yml:240-257` (`set -e` plus explicit rc handling), `play-unifi-controller.yml:125-141` (`set -e`).

**Recommendation**: Add `set -e` (and `set -o pipefail` where pipes exist) as the first line of every multi-command `shell: |` block listed above. Consider extending `.semgrep/bash-conventions.yml` / `scripts/qa-ansible.bash` to flag multi-line `shell:` blocks lacking `set -e`.

______________________________________________________________________

## ANS-02: cloudflare-warp repo install can write an empty repo file, then `creates:` permanently skips repair

**Severity: high** — broken-state-then-never-retry; silent failure trap.

`playbooks/imports/optional/common/play-cloudflare-warp.yml:10-16`:

```yaml
- name: Install Repo
  ansible.builtin.shell: |
    curl -fsSl https://pkg.cloudflareclient.com/cloudflare-warp-ascii.repo | sudo tee /etc/yum.repos.d/cloudflare-warp.repo
    dnf update
  args:
    executable: /bin/bash
    creates: /etc/yum.repos.d/cloudflare-warp.repo
```

Three compounding defects:

1. **No `pipefail`**: if `curl` fails (network blip, URL change), `tee` still exits 0 — the task reports success **and creates an empty repo file**. On every subsequent run the `creates:` guard sees the file and skips the task forever. The very next task (`dnf: name=cloudflare-warp state=latest`) then fails with an unhelpful "package not found", and re-running never self-heals.
2. **`sudo` inside a `become: true` play** is redundant and bypasses Ansible's privilege escalation accounting.
3. **`dnf update`** (no `-y`): in a non-TTY Ansible run dnf assumes "no" and aborts with rc 1 whenever updates are pending — so the task can fail spuriously on first run; and a full system upgrade does not belong inside a repo-install task (AB-dnf-upgrade already owns that).

**Recommendation**: Replace with `ansible.builtin.get_url` (`url:`, `dest: /etc/yum.repos.d/cloudflare-warp.repo`, `owner/group/mode`) — atomic, fail-fast, idempotent — and delete the `dnf update` line entirely.

______________________________________________________________________

## ANS-03: resolved.conf deployed but the reload handler is commented out — DNS config silently never applies

**Severity: medium** — deployed configuration has no effect until an unrelated reboot.

`playbooks/imports/optional/common/play-cloudflare-warp.yml:53-67`: the play overwrites `/etc/systemd/resolved.conf` (pointing DNS at Warp's 127.0.2.2/127.0.2.3), but the `notify: resolved-reload` line **and the entire handlers block are commented out** (lines 60-67). systemd-resolved keeps running with the old config; the play's whole purpose (DoH DNS routing) is not in effect at the end of the run — a silent no-op contradicting fail-fast. The `copy` also lacks `owner/group/mode` and clobbers the entire distro `resolved.conf` instead of using a drop-in.

**Recommendation**: Reinstate the handler (uncomment, `state: restarted`), add `notify:` back to the copy task, and prefer a drop-in at `/etc/systemd/resolved.conf.d/cloudflare-warp.conf` with explicit `owner: root, group: root, mode: "0644"`.

______________________________________________________________________

## ANS-04: Warp registration is deleted and re-created on every run

**Severity: medium** — designed-in non-idempotency.

`play-cloudflare-warp.yml:28-50`: the probe (`registration show`, correctly FAIL-FAST-OK-annotated) feeds a task that **deletes** any existing registration (`when: _warp_reg_show.rc == 0`), and the unconditional "Configure" task then runs `yes | warp-cli --accept-tos registration new` plus `connect`/`mode doh`/`dns families malware` on **every** run. Each `playbook-main`-style re-run churns the device registration (briefly disconnecting Warp, accumulating device entries server-side). The probe result is used backwards: it should *skip* re-registration when one exists, not trigger deletion.

**Recommendation**: Invert the logic — only run `registration new` when `_warp_reg_show.rc != 0`; make the `connect`/`mode`/`dns` settings separate idempotent steps (probe current values via `warp-cli settings`, or accept `changed_when: false`-style convergence).

______________________________________________________________________

## ANS-05: play-unifi-controller hardcodes `podman`, violating the `container_engine` rule

**Severity: medium** — explicit project hard rule #4 ("new playbooks needing a container engine must use the `container_engine` variable").

`playbooks/imports/optional/common/play-unifi-controller.yml` is a recent playbook that hardcodes the engine throughout: `podman compose pull` (line 114), `podman unshare` (line 177), and `podman compose` in the deployed launcher script (lines 188, 193, 202, 221, 224). It neither loads `vars/container-defaults.yml` nor documents a deliberate Podman-only decision. Compare `play-claude-yolo.yml:14-63` and `play-claude-devtools.yml:17-63`, which template `{{ container_engine }}` correctly.

**Recommendation**: Either load `vars/container-defaults.yml` and template `{{ container_engine }} compose` / `{{ container_engine }} unshare` through the playbook and the deployed launcher, or add a comment block documenting why this playbook is intentionally Podman-only (rootless `unshare` semantics would be a defensible reason — but it must be written down per the ContainerEngines rule "document the reason").

______________________________________________________________________

## ANS-06: play-unifi-controller — error-hiding in deployed launcher, shell firewalling, and meaningless `changed_when`

**Severity: medium**.

Same file, three correctness defects:

1. **Line 177** (inside the deployed `unifi-controller` launcher): `podman unshare chown -R 0:0 "$COMPOSE_DIR/config/data/" 2>/dev/null || true` — stderr discarded and exit code suppressed in a `set -e` script. If the ownership reclaim fails, the subsequent `grep -v > tmp && mv` writes `system_ip` into a file the user may not own, failing later in a confusing place. This is the prohibited `|| true` error-hiding pattern with no annotation.
2. **Lines 124-144**: firewall ports opened via a `firewall-cmd` shell block with `changed_when: true` — always reports changed, and bypasses the `ansible.posix.firewalld` module (used correctly elsewhere in the repo) which is declarative and idempotent.
3. **Line 118**: `changed_when: "'Pull complete' in compose_pull.stdout or 'Downloaded' in compose_pull.stdout or compose_pull.rc == 0"` — the final `or rc == 0` makes the expression true whenever the task succeeds, i.e. it is `changed_when: true` in disguise.

**Recommendation**: (1) handle the chown failure explicitly (test writability, emit an actionable error); (2) replace the shell block with `ansible.posix.firewalld` (`port:`, `permanent: true`, `immediate: true`, `state: enabled`, loop over ports); (3) drop the `rc == 0` clause so changed-status reflects actual pulls.

______________________________________________________________________

## ANS-07: Flatpak become misuse and inconsistency between play-comms and play-videography

**Severity: medium** — likely first-run failure on a fresh system.

- `playbooks/imports/optional/common/play-videography.yml:10-11`: "Enable Flathub" runs **without** `become` in a `become: false` play. `flatpak remote-add` defaults to the **system** installation, which requires root; from a non-interactive Ansible run this depends on a polkit grant that is not guaranteed → flaky/failing first run.
- `play-videography.yml:12-15`: "Install Shotcut" uses `become: true` + `become_user: "{{ user_login }}"` — escalating back to the same user that is already connecting (a no-op privilege round-trip), again hitting polkit for the system-level install.
- `playbooks/imports/play-comms.yml:11-16` does the same two operations with plain `become: true` (root) — the working pattern — proving the two playbooks have drifted.

Both also use raw `command:` (always reports changed; `flatpak install -y` re-resolves on every run) where `community.general.flatpak_remote` / `community.general.flatpak` are idempotent, and the "Enable Flathub" task is duplicated logic (DRY: candidate for `tasks/ensure-flathub.yml` alongside `tasks/ensure-jq.yml`).

**Recommendation**: Align both playbooks on `become: true` with `community.general.flatpak_remote` (flathub) and `community.general.flatpak` (`name: com.slack.Slack` / `org.shotcut.Shotcut`), and extract the flathub-enable step into a shared task file.

______________________________________________________________________

## ANS-08: Global git ignore blockinfile fails on fresh systems — parent directory never created

**Severity: medium** — first-run failure in a core playbook.

`playbooks/imports/play-git-configure-and-tools.yml:27-36` writes `~/.config/git/ignore` via `blockinfile` with `create: true`. `blockinfile` does **not** create missing parent directories, and nothing in the repo creates `~/.config/git` (verified: the only `.config/git*` references are this task and `play-github-cli-multi.yml`'s unrelated `git-account-helper` dir). `git config --global` writes `~/.gitconfig`, not `~/.config/git/`, so on a genuinely fresh Fedora account this task fails with "Destination directory does not exist". The task also lacks `owner/group/mode`.

**Recommendation**: Precede it with `ansible.builtin.file: path=~/.config/git state=directory owner/group={{ user_login }} mode "0755"`, and add ownership/mode to the blockinfile.

______________________________________________________________________

## ANS-09: LXC service restarted on every playbook-main run

**Severity: medium** — anti-idempotent; can bounce running containers.

`playbooks/imports/play-lxc-install-config.yml:62-68`:

```yaml
- name: Setup LXC Service
  ansible.builtin.systemd:
    name: "{{ item }}"
    state: restarted
    enabled: true
```

`state: restarted` in a regular task restarts `lxc.service` unconditionally on **every** main-playbook run (the play is core, `playbook-main.yml:27`). `lxc.service` manages container autostart — a restart can bounce long-lived LXC containers mid-session. This contradicts both idempotency and the repo's own style rule ("use handlers for service restarts triggered by config changes"). It also guarantees the play always reports changed.

**Recommendation**: Change to `state: started`, and move restart behaviour into a handler notified by the tasks that actually change LXC config (`/etc/sysconfig/lxc-net` blockinfiles already notify `restart lxc-net` correctly — mirror that pattern).

______________________________________________________________________

## ANS-10: JetBrains Toolbox post-install launch uses skip-and-warn instead of failing

**Severity: medium** — prohibited "skip and warn" pattern.

`playbooks/imports/play-toolbox-install.yml:96-110`: the integration-launch shell ends with

```bash
else
  echo "WARNING: Could not find JetBrains Toolbox binary after installation"
fi
```

— exit 0 either way. If the installer silently failed to place the binary, the task prints a warning into Ansible stdout (where nobody reads it) and the play succeeds. Per the project rule, "If an operation should succeed, FAIL on error". Minor extras in the same file: `when: st_jetbrains_toolbox.stat.exists == False` literal-comparison style (lines 78, 114-116), and the `find … 2>/dev/null` stderr suppression.

**Recommendation**: Replace the `else` branch with `echo "ERROR: …" >&2; exit 1`, and switch the literal comparisons to `not st_jetbrains_toolbox.stat.exists`.

______________________________________________________________________

## ANS-11: localhost.yml.dist missing required `github_ssh_passphrase`; documents wrong variable name `lastfm_secret`

**Severity: medium** — onboarding gap in the canonical variables template.

`environment/localhost/host_vars/localhost.yml.dist` documents only `user_login`/`user_name`/`user_email` plus optional Qobuz/MOK entries. But:

- `github_ssh_passphrase` is **required by the core flow**: `play-github-cli-multi.yml:239-248` asserts it (good, actionable message) and `play-lxc-install-config.yml:123` consumes it with **no assert** — run standalone, the LXC play dies with a raw undefined-variable templating error at "Create passphrase file for SSH operations". The dist template never mentions the variable, so every fresh user hits the assert (best case) or the template error (worst case).
- Line 7 documents `# lastfm_secret: !vault |...` but the consumer is `lastfm_api_secret` (`play-qobuz-cli.yml:139,180,194,208`). A user following the dist comment verbatim defines the wrong variable and is re-prompted forever.

**Recommendation**: Add a documented `github_ssh_passphrase: !vault |...` entry to the dist file, correct `lastfm_secret` → `lastfm_api_secret`, and add a defined-and-non-empty assert (mirroring github-cli-multi's) at the top of `play-lxc-install-config.yml`'s SSH block for standalone runs.

______________________________________________________________________

## ANS-12: Qobuz playbook secret handling — shell-quoting injection hazard, world-readable secrets file, append-duplication risk

**Severity: medium**.

`playbooks/imports/optional/common/play-qobuz-cli.yml`:

1. **Lines 183-186 / 192-195**: `printf '%s' '{{ lastfm_key_prompt.user_input }}' | ansible-vault encrypt_string … >> localhost.yml`. The prompt value is interpolated inside single quotes in a shell script — any apostrophe in the input breaks out of the quoting (command injection / run failure). `no_log: true` is present (good), but the quoting is unsafe. Same pattern at lines 277-284 for `qobuz_username`/`qobuz_password`.
2. **Lines 204-209**: `config.toml` containing the Last.fm key and secret is written with **no `mode`** (default umask → 0644 world-readable) and **no `no_log`**, so `--diff` runs print the secrets.
3. **Append (`>>`) to localhost.yml** is not idempotent: if the play dies between the two vault-append tasks, a re-run within the same conditions re-appends, producing duplicate keys in the vars file.

**Recommendation**: Pass secrets via `stdin:` on the task (`ansible.builtin.command` + `stdin: "{{ var }}"`) or environment, never inline-templated into quoted shell; add `mode: "0600"`, `owner/group`, and `no_log: true`/`diff: false` to the config.toml copy; replace the raw `>>` with `blockinfile` (markered) so re-runs converge.

______________________________________________________________________

## ANS-13: ~40 command/shell tasks lack `changed_when` — changed-status reporting is meaningless; native modules bypassed

**Severity: low** (breadth, not depth) — idempotency *reporting* defect across many playbooks.

The AST sweep found 43 `shell`/`command` tasks without `creates`/`changed_when`; after triage (some are guarded by `when:` probes or genuinely mutate every run), the notable always-report-changed offenders are:

- `play-gsettings.yml:7-17` — gsettings via shell loop (also fragile `\[\'caps:none\'\]` escaping); `community.general.dconf` is already used in `play-fast-file-manager.yml:134` — inconsistent.
- `play-gnome-shell-extensions.yml:23-25` — `gnome-shell-extension-installer --yes --restart-shell` loop re-executes per run (and `--restart-shell` is a no-op on Wayland per the repo's own `CLAUDE/GnomeShell.md`).
- `play-lxc-install-config.yml:70-71` — `nmcli connection modify` (use `community.general.nmcli`); `:97-103` `state: touch` re-touches timestamps every run (use `access_time/modification_time: preserve`).
- `play-python.yml:53-54` (`pdm self update`), `:87-93` (`pyenv install -s`), `play-rust-dev.yml:67-90,153-160` (rustup), `play-git-configure-and-tools.yml:14-26` (git config), `play-comms.yml`/`play-videography.yml` (flatpak — see ANS-07), `play-basic-configs.yml:235-237` (`grub2-editenv` with hardcoded `changed_when: true`).
- `play-lxde-install.yml:10-11` — `dnf group install lxde-desktop` via `command` **without `-y`**: aborts in a non-TTY run (dnf assumes no). File is marked "NOT TESTED"; should use the `dnf` module with `name: '@lxde-desktop'`.

**Impact**: `--check` mode and changed-counts are useless for these plays; genuine drift is indistinguishable from noise.

**Recommendation**: Sweep these tasks adding `changed_when:` based on registered output (or `changed_when: false` for pure converge-reads), and migrate gsettings→`dconf`, nmcli→`community.general.nmcli`, flatpak→`community.general.flatpak*`, lxde→`dnf` module.

______________________________________________________________________

## ANS-14: 15 file-deploying tasks missing `mode` (and mostly `owner`/`group`)

**Severity: low** — violates the style rule "Always set owner, group, mode on every file task"; results depend on umask/source perms.

Enumerated by the AST sweep (excluding the deliberate recursive-chown in `play-nvm-install.yml:31-37`):

- `play-basic-configs.yml:106-113` (Basic Bash Tweaks Files → /etc/profile.d, /var/local), `:115-118` (Prompt Colour File)
- `play-cloudflare-warp.yml:53-59` (resolved.conf — see ANS-03)
- `play-qobuz-cli.yml:131-134, 204-209` (secrets file — see ANS-12), `:211-214, 216-230`
- `play-toolbox-install.yml:45-50` (get_url), `:55-59` (unarchive)
- `play-nvidia.yml` cuDNN unarchive; `play-darktable-ai-gpu.yml:151+` ONNX unarchive; `play-virtualbox-windows.yml:89-90` unarchive
- `optional/archived/play-tlp-battery-optimisation.yml` (2 copies — archived, lowest priority)

**Recommendation**: Add explicit `owner`, `group`, `mode` to each. For tmp-dir downloads/extracts the mode matters less but costs nothing.

______________________________________________________________________

## ANS-15: Duplicate shebang line in 16 playbooks

**Severity: low** — cosmetic, but signals `scripts/make-playbooks-executable.bash` mis-inserting.

16 playbooks contain `#!/usr/bin/env ansible-playbook` **twice** — once as line 1 (correct) and again immediately after `---` as a stray comment: play-terminal-emulators, play-gsettings, play-lxde-install, play-displaylink, play-docker-overlay2-migration, play-virtualbox-windows, play-nvidia, play-nordvpn-openvpn, play-lightweight-ides, play-cloudflare-warp, play-speech-to-text, play-lastpass, play-hd-audio, play-gnome-shell-dev, play-golang, play-rust-dev.

**Recommendation**: Delete the post-`---` duplicates; fix `scripts/make-playbooks-executable.bash` to detect an existing shebang anywhere in the first three lines before inserting.

______________________________________________________________________

## ANS-16: Unpinned, checksum-less binary downloads + `rm -rf` glob in ~/Downloads; version-gated installer logic duplicated

**Severity: low**.

- `play-basic-configs.yml:221-227` — yq fetched from `releases/latest/download` with no checksum, then frozen forever by `creates: /usr/bin/yq` (unreproducible *and* never updated — worst of both).
- `play-qobuz-cli.yml:65-109` — hifi-rs and qobuz-player tarballs downloaded with **no checksum** (the same repo pins sha256 for markless, RapidRAW, ART, darktable — inconsistent), and the installer runs `rm -rf hifi-rs*` / `rm -rf qobuz-player*` **inside `~/Downloads`**, which will delete any unrelated user files matching the glob. `play-markless.yml:34-35` shares the `rm -rf markless*` pattern (though it does verify sha256, line 39).
- The "compare installed `--version` to desired, else download+untar+mv" shell block is copy-pasted three times (`play-qobuz-cli.yml:65-84, 86-109`, `play-markless.yml:22-47`) — a shared task file (parameterised name/URL/sha/dest) would remove the duplication.

**Recommendation**: Pin versions + sha256 for hifi-rs/qobuz-player (digest available from GitHub releases API, as play-darktable-ai-appimage already does); operate in an `ansible.builtin.tempfile` directory instead of `~/Downloads`; extract the version-gated installer into `tasks/install-github-binary.yml`.

______________________________________________________________________

## ANS-17: NVM grep-guard prevents the managed bashrc block from ever updating

**Severity: info**.

`play-nvm-install.yml:39-57`: the `blockinfile` (idempotent by design) is gated on `grep -q 'NVM_DIR' ~/.bashrc` failing. Once **any** `NVM_DIR` string exists — including a pre-existing manual install, or the block itself after first run — the blockinfile never executes again, so future changes to the block content in the repo silently never deploy (drift between repo and host, the exact failure mode IaC exists to prevent). The guard is redundant: `blockinfile` already no-ops when the markered content matches.

**Recommendation**: Delete the grep probe and the `when:`; let `blockinfile` converge unconditionally.

______________________________________________________________________

## ANS-18: LastPass login task contains duplicated dead Jinja branches

**Severity: info**.

`play-lastpass.yml:129-139`: the `{% if %}`/`{% else %}` branches both render the identical `export LPASS_HOME=/home/{{ user_login }}/.lpass-{{ item.item.key }}` line — dead conditional logic left over from a refactor. The task also runs interactive `lpass login` inside a shell task with no `changed_when` (acceptable as an interactive bootstrap, but worth simplifying).

**Recommendation**: Collapse to a single unconditional export; add `changed_when` based on the login output.

______________________________________________________________________

## Cross-cutting checks with no findings

- **Ansible 2.19 parser hazards**: all 75 playbooks pass `ansible-playbook --syntax-check` on ansible-core 2.19.10. No apostrophe/backtick-in-comment hazards inside `shell: |` blocks and no `: -<letter>` task-name hazards remain.
- **`failed_when: false` / `ignore_errors: true`**: 60+ occurrences, **every one** annotated `# FAIL-FAST-OK:` with a justified probe/advisory reason, and probe results are explicitly checked downstream (e.g. play-lxc-install-config 240-274, play-nvidia 284-287, play-docker 29). Fully compliant.
- **Hardcoded Fedora versions**: none in task logic; all hits are comments/docs. `play-rpm-fusion.yml` correctly derives the release via `rpm -E %fedora`; `play-AA-preflight-sanity.yml` validates against `vars/fedora-version.yml`.
- **`container_engine`**: `play-claude-yolo.yml` and `play-claude-devtools.yml` use the variable correctly; `play-docker.yml`/`play-ddev.yml` are the documented rootful-Docker exceptions with fail-fast asserts and rationale. Only ANS-05 violates the rule.
- **Variable definedness**: besides ANS-11, all consumed vars trace to inventory, `vars/`, `set_fact`, or prompts.

## Positive Observations

- **Fail-fast culture is real**: the FAIL-FAST-OK annotation regime is applied consistently and correctly across the whole tree; probe-then-fail is the dominant pattern, with actionable error messages (play-github-cli-multi, play-docker, play-ddev are exemplary).
- **Ordering dependencies are asserted, not assumed**: `playbook-main.yml` documents Docker→LXC and ccy→claude-code ordering, and both downstream plays assert their preconditions (`play-lxc-install-config.yml:21-45`, `play-claude-code.yml:13-20`).
- **`play-AB-dnf-upgrade.yml`** is a model playbook: documented history, `set -eo pipefail`, registered probes, half-installed-kernel reconciliation, explicit reboot pause.
- **`play-ZZ-repo-cleanup.yml`** converges removed-repo drift declaratively with `removes:` guards — a genuinely good IaC pattern.
- **Newer downloads are checksum-pinned** (markless, photography, darktable-ai-\*, nvidia cuDNN, JetBrains Toolbox via its checksumLink API).
- **Secret temp files** (`/tmp/.github_ssh_pp`, `/tmp/.lxc_ssh_pp`) are mode-0600, `no_log`-protected, and cleaned in `always:` blocks.
- **Shared task files** (`tasks/ensure-jq.yml`) show the right DRY mechanism exists — it just needs wider adoption (flathub, GitHub-binary installs).

---

## Adversarial Verification Appendix

### ANS-01 — CONFIRMED (high confidence)

Verified all 7 cited shell blocks by reading the files. Confirmed: play-rpm-fusion.yml:8-16 runs 7 dnf commands with only 'set -x' — a failed 'dnf group install multimedia' is masked by the final 'dnf install intel-media-driver'; play-git-configure-and-tools.yml:14-26 (11 git config lines, plus 80-86 gh install); play-rust-dev.yml:67-75 (rustup update masked by rustup default) and 153-160 (weaker, 2-line block); play-lxc-install-config.yml:72-75 (firewall-cmd change-interface masked by reload); play-nvm-install.yml:59-65 (nvm install masked by nvm alias; the creates: guard can then permanently skip the task after partial failure); play-lastpass.yml:129-139 (lpass login masked by lpass sync — weakest instance). No compensating controls: no FAIL-FAST-OK annotations on these tasks (the one at play-lastpass.yml:116 covers a separate probe task), no register+check follows the blocks, and neither .semgrep/bash-conventions.yml nor qa-ansible.bash enforces set -e inside playbook shell blocks (only the QA scripts' own headers contain set -euo pipefail). CLAUDE.md rule #1 explicitly mandates 'set -e in all bash scripts', and CLAUDE/AnsibleStyle.md's own example recommends only 'set -x', making the gap systemic. Severity high is justified by the core-playbook rpm-fusion and nvm cases; the git-config, lastpass, and rust-analyzer instances are lower-impact pattern matches.

### ANS-02 — CONFIRMED (high confidence)

Verified against playbooks/imports/optional/common/play-cloudflare-warp.yml lines 10-16. All claim elements confirmed: (1) bash shell block has no pipefail/set -e, so a curl failure (-f exits non-zero on HTTP error) is masked by tee, which writes an empty /etc/yum.repos.d/cloudflare-warp.repo and exits 0; (2) creates: /etc/yum.repos.d/cloudflare-warp.repo then skips the task on all subsequent runs, so the corrupt repo file never self-heals via the playbook (manual deletion required, itself an IaC violation); (3) line 13 runs interactive 'dnf update' with no -y, which aborts rc=1 in non-TTY when updates are pending — after the empty file is already written; (4) 'sudo tee' is redundant under the play-level become: true. No FAIL-FAST-OK annotation or compensating control exists for this task (the annotated failed_when: false at line 31 belongs to a separate warp registration probe). Mitigating nuance: the breakage is not fully silent — the next task (dnf install cloudflare-warp) fails when the repo is empty — but the error misattributes the cause and re-runs never repair it, violating the repo's #1 fail-fast rule. Recommendation (get_url with owner/group/mode; drop dnf update) matches project Ansible patterns. High severity is defensible; medium would also be arguable given the visible downstream failure.


