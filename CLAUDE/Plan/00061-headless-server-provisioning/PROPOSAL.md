# Proposal: Headless Server Provisioning — Native Play-Level `tags:` + `--skip-tags`

## Revision log (round 2)

Fixes applied in response to `AUDIT-round-1.md` (Fable). Re-audit only needs
to check these deltas — the mechanism, classifications, and everything not
listed below are unchanged from round 1.

- **BLOCKER 1.1 (fixed)** — Check 4's `valid_count=$(...)` /
  `scope_like_count=$(...)` lines crashed `qa-ansible.bash` under `set -e`
  on the zero-match case (exactly the "no scope tag" case the check exists to
  catch), which also silently corrupted `qa-all.bash`'s merged JSON for every
  *other* check via `jq -s`'s empty-input-shrinks-the-array behaviour. Added
  `|| true` to both lines (§4). Audited the rest of Check 4 for the same
  hazard — the one other `$(...)` assignment that runs a `grep`
  (`bad=$(... | grep -vxE ... | paste ...)`) only executes on the branch
  where `scope_like_count -gt 0` is already known true, so `grep -vxE` is
  guaranteed at least one match; no `|| true` needed there, verified by
  re-reading the branch guard.
- **SHOULD-FIX 1.2 (fixed)** — the awk extraction didn't strip a trailing
  `# comment` (or a stray `\r`), so a correctly-spelled, commented scope tag
  (this repo's own "comments explain WHY" convention encourages exactly this)
  read as "missing." Added `sub(/[[:space:]]*#.*$/, "")` and `sub(/\r$/, "")`
  to the awk item branch (§4); updated §8's limitation note.
- **SHOULD-FIX 5.1 (fixed)** — `play-virtualbox-windows.yml` has two
  `- hosts:` play blocks in one file (the only such file in the repo); the
  file-granular gate would false-pass if only the first play were tagged.
  Split its classification into two independent rows (§1.3), added an
  explicit multi-play-file detection branch to Check 4 (§4) that errors
  loudly rather than silently trusting the first play's tag for the whole
  file, and called the file out by name in the checklist (§7 step 4).
- **SHOULD-FIX 6.1 (fixed)** — `play-container-watch.yml`'s interim
  task-tag edit had no exact diff, unlike §3.1–§3.3, even though step 4
  required applying it in this pass. Added the full exact diff for the
  GNOME-shaped tasks (§3.4), matching the rigor of §3.1–§3.3, so nothing is
  left to prose-derived guesswork. **While deriving it, found an 8th task the
  round-1 audit's own enumeration of 7 had missed** (`Container-watch extension reload complete`, line 175) — it consumes a variable the round-1
  list's 6th task registers, so tagging only the audit's 7 would have left an
  untagged task referencing an undefined variable on a server run (a hard
  Ansible error under this repo's `ansible.cfg`, not a silent gap). §3.4 now
  documents this as a general hazard for register/`when:`-chained tasks
  under task-level overrides, beyond just fixing this one instance.
- **NITPICK 6.2 (fixed)** — §5's docs-insertion instruction was
  self-contradictory ("after Quick Navigation, before Core Playbooks" vs.
  "right after line 17", which *is* the Core Playbooks heading). Corrected to
  "immediately before line 17."
- **NITPICK 6.3 (fixed)** — added a checklist item (§7) to update the three
  existing per-play `docs/playbooks.md` sections for the mixed plays,
  including noting the bug fixed in `play-prevent-ssh-suspend.yml`.
- **NITPICK 6.4 (addressed, not changed)** — `TOTAL` is deliberately left
  un-extended for the new check, for the same reason Check 3 (self-ref vars)
  doesn't extend it either — noted explicitly in §4 now instead of being a
  silent omission.
- Not changed: the `play-vpn.yml` changed-tracking-granularity nitpick
  (splitting one `dnf` task into two produces two recap lines instead of one)
  — noted as accepted, cosmetic-only, no proposal text depended on the old
  single-result behaviour.

---

**Status of this document**: implementation-ready design for Plan 00061 Phase 3,
per Decision 1 (mechanism), Decision 2 (rpm-fusion=general), and Decision 3
(this proposal → Fable audit → judge → loop) in `PLAN.md`. Supersedes
`brainstorm-sonnet.md` where they disagree — this document is the
authoritative source for implementation. Written after an exhaustive,
task-by-task re-read of all 31 core plays (not the one-pass reads that missed
`play-basic-configs.yml`'s USB-audio task and `play-vpn.yml`'s GNOME package
in the first brainstorm round) plus a fast-pass sweep of the 41 non-archived
optional plays, because the QA gate (§4) scans every playbook under
`playbooks/`, not just the core 31 — leaving the optional tree unscoped would
make the gate fail on day one.

---

## 1. Exhaustive per-play classification

### 1.1 Core plays (all 31, task-by-task re-read)

Scope reflects **what the play's tasks actually do**, not what its filename or
folder implies (this is the same discipline the owner applied to
`play-rpm-fusion` in Decision 2). "General" means: no task requires a GNOME
session, a display server, or a GUI application to be meaningful.

| #   | Play                               | Scope                                                                                     | One-line justification                                                                                                                                                                                                                                                                                                                         |
| --- | ---------------------------------- | ----------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `play-AA-preflight-sanity.yml`     | `scope-general`                                                                           | Ansible-version + Fedora-version assertions only                                                                                                                                                                                                                                                                                               |
| 2   | `play-AB-dnf-upgrade.yml`          | `scope-general`                                                                           | Package upgrade + kernel half-install cleanup, no GUI                                                                                                                                                                                                                                                                                          |
| 3   | `play-basic-configs.yml`           | `scope-general` (+ 1 task-level `scope-gnome` override — §3.1)                            | vim colours, sudo, PS1, SSH helper scripts, `yq`, GRUB, `fwupd` — all general; one task (USB audio fix) is the exception                                                                                                                                                                                                                       |
| 4   | `play-prevent-ssh-suspend.yml`     | `scope-general` (+ 1 task-level `scope-gnome` override — §3.2)                            | `ssh-suspend-guard` systemd service is general; one task calls `gsettings set org.gnome.settings-daemon...` — GNOME-only                                                                                                                                                                                                                       |
| 5   | `play-network-wait-tuning.yml`     | `scope-general`                                                                           | `NetworkManager-wait-online.service` boot-timing tune, systemd-only                                                                                                                                                                                                                                                                            |
| 6   | `play-mask-intel-lpmd.yml`         | `scope-general`                                                                           | Masks a systemd unit on Intel CPUs, self-probes and no-ops on AMD, no GUI                                                                                                                                                                                                                                                                      |
| 7   | `play-systemd-user-tweaks.yml`     | `scope-general`                                                                           | `systemd-oomd` memory-pressure fix for `user.slice`; explicitly protects rootless Podman/Docker containers — server-relevant, not GNOME-coupled. **(resolves the flagged ambiguous call — firm: general)**                                                                                                                                     |
| 8   | `play-nvm-install.yml`             | `scope-general`                                                                           | Node.js/npm via nvm, pure CLI toolchain                                                                                                                                                                                                                                                                                                        |
| 9   | `play-git-configure-and-tools.yml` | `scope-general`                                                                           | git config, `gh` CLI, bash-git-prompt, SSH agent prompt                                                                                                                                                                                                                                                                                        |
| 10  | `play-git-hooks-security.yml`      | `scope-general`                                                                           | Configures `core.hooksPath` on this repo's own clone                                                                                                                                                                                                                                                                                           |
| 11  | `play-firefox.yml`                 | `scope-gnome`                                                                             | Installs the Firefox GUI browser                                                                                                                                                                                                                                                                                                               |
| 12  | `play-github-cli-multi.yml`        | `scope-general`                                                                           | Multi-account `gh`/SSH-key/git-wrapper setup — 100% CLI/API/SSH, zero GUI dependency despite being the largest play in the repo                                                                                                                                                                                                                |
| 13  | `play-ms-fonts.yml`                | `scope-gnome`                                                                             | MS core fonts are consumed only by GUI apps (browsers, office viewers) rendering on-screen; no headless consumer exists in this repo's scope. **(resolves the flagged ambiguous call — firm: gnome)**                                                                                                                                          |
| 14  | `play-rpm-fusion.yml`              | `scope-general`                                                                           | **Owner-decided (Decision 2).** Enables free/nonfree repos — foundational plumbing later general-scope packages may need; the multimedia-codec/`intel-media-driver` tasks in the file are the only current *content*, but the play's *purpose* (repo enablement) is general and future packages should not be blocked by it being tagged gnome |
| 15  | `play-browsers.yml`                | `scope-gnome`                                                                             | Installs Chrome/Brave/Vivaldi GUI browsers                                                                                                                                                                                                                                                                                                     |
| 16  | `play-toolbox-install.yml`         | `scope-gnome`                                                                             | JetBrains Toolbox — a GUI IDE manager; already self-guards on `has_display` but tagging avoids a pointless JetBrains-API network call on a server                                                                                                                                                                                              |
| 17  | `play-docker.yml`                  | `scope-general`                                                                           | Rootful Docker CE — DDEV-class server workloads need this too                                                                                                                                                                                                                                                                                  |
| 18  | `play-lxc-install-config.yml`      | `scope-general`                                                                           | System containers, iptables/bridge networking, no GUI content anywhere in the file                                                                                                                                                                                                                                                             |
| 19  | `play-podman.yml`                  | `scope-general`                                                                           | Rootless Podman + podman-compose                                                                                                                                                                                                                                                                                                               |
| 20  | `play-python.yml`                  | `scope-general`                                                                           | pyenv/pipx/`semgrep`/PDM dev toolchain, no GUI. **(resolves the flagged ambiguous call — firm: general)**                                                                                                                                                                                                                                      |
| 21  | `play-claude-yolo.yml`             | `scope-general`                                                                           | Container-based Claude Code tooling; the in-container Chromium (`agent-browser`) runs inside the container regardless of host GUI — host-side tasks are all file/container-engine ops                                                                                                                                                          |
| 22  | `play-claude-code.yml`             | `scope-general`                                                                           | Claude Code CLI installer + `cc` wrapper, pure CLI                                                                                                                                                                                                                                                                                             |
| 23  | `play-comms.yml`                   | `scope-gnome`                                                                             | Installs Slack via Flatpak (GUI app)                                                                                                                                                                                                                                                                                                           |
| 24  | `play-gnome-shell.yml`             | `scope-gnome`                                                                             | Installs `gnome-tweaks` (note: its `name:` field says "Gnome Shell Extensions", duplicating `play-gnome-shell-extensions.yml`'s name — pre-existing repo quirk, out of this plan's scope, flagged for the record only)                                                                                                                         |
| 25  | `play-gnome-shell-extensions.yml`  | `scope-gnome`                                                                             | GNOME extension installer, dconf schema compilation, custom extension deploy — entirely GNOME-Shell-session-dependent                                                                                                                                                                                                                          |
| 26  | `play-markless.yml`                | `scope-general`                                                                           | Terminal-based markdown viewer, no GUI dependency (Kitty/Sixel image rendering is a terminal graphics protocol, not a GUI toolkit)                                                                                                                                                                                                             |
| 27  | `play-terminal-emulators.yml`      | `scope-gnome`                                                                             | Alacritty/Kitty/Ghostty/Foot are GUI windowed applications requiring a display server to run, despite "terminal" in the name. **(resolves the flagged ambiguous call — firm: gnome)**                                                                                                                                                          |
| 28  | `play-vscode.yml`                  | `scope-gnome`                                                                             | Installs the VS Code GUI editor                                                                                                                                                                                                                                                                                                                |
| 29  | `play-vpn.yml`                     | `scope-general` (+ 1 task-level `scope-gnome` override, requires a task **split** — §3.3) | WireGuard/OpenVPN CLI tools + firewalld rule are general; `NetworkManager-openvpn-gnome` (bundled in the same `dnf` task today) is GNOME-applet-only                                                                                                                                                                                           |
| 30  | `play-gsettings.yml`               | `scope-gnome`                                                                             | Caps Lock remap + Ptyxis terminal tab setting via `dconf` — both GNOME desktop settings, meaningless without a GNOME session                                                                                                                                                                                                                   |
| 31  | `play-ZZ-repo-cleanup.yml`         | `scope-general`                                                                           | Removes orphaned COPRs, no GUI content                                                                                                                                                                                                                                                                                                         |

**Tally: 21 `scope-general`, 10 `scope-gnome`, 0 `scope-server`.** The empty
`scope-server` bucket is intentional (§ non-goals of this plan rule out
server-hardening content now) — see PLAN.md's own non-goals; the taxonomy
needs the bucket to exist for a *future* plan, not for this one to populate.

### 1.2 Exhaustive mixed-concern task catalogue (core plays)

The exhaustive sweep found **three** genuinely mixed-concern instances across
the 31 core plays — one more than either brainstorm found individually,
confirming the owner's instinct that a one-pass read is unreliable:

| Play                           | Exact task                                                      | Package/setting                                                                        | Why it's the exception                                                                                                                                                                                                                                                                                                        |
| ------------------------------ | --------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-basic-configs.yml`       | `Deploy USB audio fix script`                                   | `files/home/bashrc-includes/usb-audio-fix.bash` bashrc-include                         | USB-audio troubleshooting is a desktop-audio-hardware concern with no headless-server consumer                                                                                                                                                                                                                                |
| `play-prevent-ssh-suspend.yml` | `Disable suspend on AC power (plugged in = never idle-suspend)` | `gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type nothing` | Calls a GNOME Settings Daemon schema over D-Bus — **on a server without `gnome-settings-daemon` installed, this task does not silently no-op, it hard-fails** with "No such schema" (`gsettings` errors when the schema isn't registered). This is a *real latent bug* the scope split fixes, not just a tidiness improvement |
| `play-vpn.yml`                 | `Install VPN Packages` (currently one `dnf` task, 3 packages)   | `NetworkManager-openvpn-gnome`                                                         | GNOME NetworkManager-applet integration; `wireguard-tools` + `NetworkManager-openvpn` in the same task are pure CLI                                                                                                                                                                                                           |

All three are **trivial, single-item exceptions** (one task, or one package
within a task) inside plays that are overwhelmingly single-scope. Per the
graft rule (PLAN.md Decision 1, grafted from Fable): task-level tag override
for a trivial exception, file-split only for a non-trivial one. **The
exhaustive sweep found zero core plays that need a file-split** — no core
play carries a substantial fraction of its tasks in the non-dominant scope.
The file-split rule is retained for future mixed plays that do earn it (see
§3.4 for the worked example that would need one, found in the optional tree).

### 1.3 Optional plays (fast-pass — 41 files, second-tier rigor)

**Scope note on rigor**: PLAN.md Task 2.2 scoped the exhaustive sweep to core
plays only. But the QA gate in §4 recursively scans `playbooks/imports/**`
(matching `qa-ansible-syntax.bash`'s existing discovery), so it will fail
immediately on every unscoped optional play the moment it's turned on. This
table exists so the gate can pass on day one — it is a **fast-pass**
classification (play name + a `grep` for GUI/desktop signal words + one
targeted read where the name was ambiguous), not the full task-by-task audit
the core 31 got. Confidence is marked; **Low-confidence rows must be
re-verified against the play's actual task list before its tag is trusted**,
per the same review-discipline caveat already called out in
`brainstorm-sonnet.md`'s failure-mode section. `imports/optional/archived/`
(currently one file, `play-tlp-battery-optimisation.yml`) is exempt — see §4.
`imports/optional/untested/` currently has no play files (README only); when
one is added it is **not** exempt (untested ≠ unclassified).

**`playbooks/imports/optional/common/` (30 files):**

| Play                                  | Scope                                             | Confidence          | Note                                                                                                                                                                                 |
| ------------------------------------- | ------------------------------------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `play-advanced-kernel-management.yml` | `scope-general`                                   | High                | Kernel versionlock/retention, no GUI                                                                                                                                                 |
| `play-claude-devtools.yml`            | `scope-general`                                   | High                | CLI session viewer (`ccdt`)                                                                                                                                                          |
| `play-clean-paste.yml`                | `scope-gnome`                                     | High                | Wayland clipboard sanitiser bound to a GNOME/Wayland keybinding                                                                                                                      |
| `play-cloudflare-dns.yml`             | `scope-general`                                   | High                | DNS-over-TLS resolver config, systemd/network only                                                                                                                                   |
| `play-cloudflare-warp.yml`            | `scope-general`                                   | **Medium — verify** | Name suggests a CLI daemon (`warp-cli`); grep hit on "GUI" needs confirming it isn't the optional GUI client                                                                         |
| `play-collaboration.yml`              | `scope-gnome`                                     | **Medium — verify** | Likely Flatpak GUI apps (naming convention matches `play-comms.yml`); confirm task list                                                                                              |
| `play-compression-helpers.yml`        | `scope-general`                                   | High                | `compress`/`uncompress` CLI commands                                                                                                                                                 |
| `play-container-watch.yml`            | **MIXED — needs the file-split rule** (§3.4)      | High                | Deploys a general-purpose watcher daemon AND a GNOME Shell panel extension in one file; not a trivial exception — see §3.4                                                           |
| `play-darktable-ai-appimage.yml`      | `scope-gnome`                                     | High                | GUI photo-editing AppImage (darktable), Flatpak-adjacent                                                                                                                             |
| `play-darktable-ai-build.yml`         | `scope-gnome`                                     | High                | Builds the same GUI app from source                                                                                                                                                  |
| `play-ddev.yml`                       | `scope-general`                                   | High                | Docker-based local dev environment, CLI                                                                                                                                              |
| `play-distrobox.yml`                  | `scope-general`                                   | High                | Container tooling, CLI                                                                                                                                                               |
| `play-fast-file-manager.yml`          | `scope-gnome`                                     | High                | Configures GNOME Nautilus/file-picker performance                                                                                                                                    |
| `play-ftp-camera.yml`                 | `scope-gnome`                                     | **Medium — verify** | Camera FTP server naming suggests a background service (general), but grep hit on GNOME/window warrants a task-list check                                                            |
| `play-gnome-shell-dev.yml`            | `scope-gnome`                                     | High                | GNOME Shell extension development tooling                                                                                                                                            |
| `play-golang.yml`                     | `scope-general`                                   | High                | Go toolchain, CLI                                                                                                                                                                    |
| `play-hd-audio.yml`                   | `scope-general`                                   | **Medium — verify** | Audio/Bluetooth enhancement — could be general (audio subsystem) or gnome (desktop sound settings); check tasks                                                                      |
| `play-image-watermarking.yml`         | `scope-general`                                   | High                | `watermark` CLI command (ImageMagick + exiftool)                                                                                                                                     |
| `play-lastpass.yml`                   | `scope-gnome`                                     | **Medium — verify** | Likely the LastPass GUI/browser-extension config; confirm                                                                                                                            |
| `play-lightweight-ides.yml`           | `scope-gnome`                                     | High                | GUI IDE installs                                                                                                                                                                     |
| `play-network-tools.yml`              | `scope-general`                                   | High                | Network discovery CLI tools                                                                                                                                                          |
| `play-nordvpn-openvpn.yml`            | `scope-gnome`                                     | **Medium — verify** | Name suggests CLI VPN manager (general), but grep hits GUI/GNOME strongly — likely a mixed play like `play-vpn.yml`; needs the same task-level-override treatment once read in full  |
| `play-photography.yml`                | `scope-gnome`                                     | High                | GUI photography app suite                                                                                                                                                            |
| `play-qobuz.yml`                      | `scope-gnome` (+ likely mixed, per own play name) | **Medium — verify** | Play name literally says "Native GUI Player, CLI Player and Last.fm Scrobbling" — self-describes as mixed; needs a real read to decide file-split vs. task-tag before implementation |
| `play-rclone.yml`                     | `scope-general`                                   | High                | Cloud storage CLI mounts; grep GNOME hits are likely a GUI tray-icon extra, verify if so it needs a task-level split like `play-vpn.yml`                                             |
| `play-remote-desktop-toggle.yml`      | `scope-gnome`                                     | High                | Toggles GNOME's built-in remote-desktop sharing                                                                                                                                      |
| `play-rust-dev.yml`                   | `scope-general`                                   | High                | Rust toolchain, CLI                                                                                                                                                                  |
| `play-speech-to-text.yml`             | `scope-gnome`                                     | High                | GNOME Shell extension + Wayland keybinding (per `CLAUDE/GnomeShell.md`)                                                                                                              |
| `play-unifi-controller.yml`           | `scope-general`                                   | High                | Podman-Compose UniFi controller — a server workload if anything                                                                                                                      |
| `play-videography.yml`                | `scope-gnome`                                     | High                | GUI video-editing tool suite                                                                                                                                                         |

**`playbooks/imports/optional/experimental/` (4 files):**

| Play                                 | Scope                                                                                                      | Confidence               | Note                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------ | ---------------------------------------------------------------------------------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-docker-in-lxc-support.yml`     | `scope-general`                                                                                            | High                     | Container interop config                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| `play-docker-overlay2-migration.yml` | `scope-general`                                                                                            | High                     | Docker storage-driver migration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| `play-lxde-install.yml`              | `scope-gnome`                                                                                              | High                     | Installs the LXDE **desktop environment** — not literally GNOME, but falls in the "needs a GUI session" bucket this repo names `scope-gnome` (see §8 naming caveat)                                                                                                                                                                                                                                                                                                                                                                                      |
| `play-virtualbox-windows.yml`        | **NOT ONE SCOPE — this file has TWO independent `- hosts:` plays and MUST be split first (§4, §7 step 4)** | **Low — flag for owner** | Verified (round 2 audit) the file contains "Install Virtualbox" (driver + packages + group membership — arguably headless-capable via `VBoxHeadless`/`VBoxManage`, same category of call as the rpm-fusion dispute) at line 3 and a structurally separate "Setup Windows VMs" play (downloads/imports a specific Windows 11 VM image, more plausibly GUI-workflow-coupled) at line 44. Check 4 (§4) hard-rejects multi-play files, so implementation must split this into two files before either half can be tagged — see §7 step 4 for the exact split |

**`playbooks/imports/optional/hardware-specific/` (7 files):**

| Play                                   | Scope           | Confidence | Note                                                                                                                                                                |
| -------------------------------------- | --------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-darktable-ai-gpu.yml`            | `scope-gnome`   | High       | GPU backend for the GUI darktable app                                                                                                                               |
| `play-displaylink.yml`                 | `scope-gnome`   | High       | Extends physical monitors via a GNOME/mutter multi-monitor session                                                                                                  |
| `play-ipu6-webcam.yml`                 | `scope-gnome`   | High       | Webcam driver stack; only consumer is GUI video-calling apps, no headless use case in this repo                                                                     |
| `play-laptop-lid-power-management.yml` | `scope-general` | High       | ACPI/systemd lid-close behaviour, no GUI dependency (irrelevant to rack servers as *hardware*, but that's an inventory question, not a GUI-dependency one — see §8) |
| `play-laptop-thermal-diagnostics.yml`  | `scope-general` | High       | CLI thermal diagnostics                                                                                                                                             |
| `play-musiccast.yml`                   | `scope-gnome`   | High       | Play name explicitly says "SSDP diagnostics + gyrc **GUI**"                                                                                                         |
| `play-nvidia.yml`                      | `scope-general` | High       | NVIDIA driver install — needed for both desktop GPU rendering *and* headless CUDA/compute servers; not GNOME-coupled                                                |

**`playbooks/imports/optional/archived/` (1 file) and `untested/` (0 files):
exempt from classification per §4** (archived) or **not yet applicable**
(untested is currently empty).

---

## 2. The exact `tags:` block — canonical form

Show once here; every play in §1 gets exactly this shape, with its own scope
value substituted:

```yaml
- hosts: desktop
  name: <Play Name>
  become: <true|false>
  tags:
    - scope-general
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
  tasks:
    ...
```

**Grammar rules** (also going into `CLAUDE/AnsibleStyle.md`, §5):

- `tags:` is a **play-level key**, sibling of `hosts:`/`name:`/`become:`/`vars:`,
  at **exactly 2-space indent**.
- Its value is a **block list**, each item at **exactly 4-space indent**
  starting with `- `. Inline array form (`tags: [scope-general]`) is valid
  YAML and valid Ansible, but is **not recognised by the QA gate's parser**
  (§4) — a deliberate, documented limitation (§8), not a bug. Always use the
  block-list form shown above.
- Insert it after whichever of `become:`/`name:` the play already has (some
  plays, e.g. `play-gsettings.yml` and `play-toolbox-install.yml`, have
  neither `become:` nor `vars:` at play level — insert right after `name:` in
  that case). Position among the play's other top-level keys does not matter
  to Ansible or to the QA gate's parser — only the 2-space indent and the
  block-list shape matter.
- Exactly **one** of `scope-gnome` | `scope-general` | `scope-server` per
  play. Other, unrelated play-level tags may coexist in the same list (none
  of the 31 core plays currently have any, so this doesn't arise yet, but the
  QA gate tolerates it — see §4).

Every row in §1's tables gets this block with its listed scope value. That is
**28 of the 31 core plays** verbatim (no other change to the file). The
remaining 3 need the edits in §3.

---

## 3. The exact mixed-play edits

### 3.1 `playbooks/imports/play-basic-configs.yml` — task-level override

**Before** (`playbooks/imports/play-basic-configs.yml`, existing task, no line
number shift for anything else in the file):

```yaml
    - name: Deploy USB audio fix script
      ansible.builtin.copy:
        src: "{{ root_dir }}/files/home/bashrc-includes/usb-audio-fix.bash"
        dest: "{{ item }}/.bashrc-includes/usb-audio-fix.bash"
        owner: "{{ item.split('/')[2] if item != '/root' else 'root' }}"
        group: "{{ item.split('/')[2] if item != '/root' else 'root' }}"
        mode: "0644"
      loop:
        - /root
        - /home/{{ user_login }}
```

**After** (play-level `tags: [scope-general]` added near the top per §2, plus
this one task gets a task-level override):

```yaml
    - name: Deploy USB audio fix script
      ansible.builtin.copy:
        src: "{{ root_dir }}/files/home/bashrc-includes/usb-audio-fix.bash"
        dest: "{{ item }}/.bashrc-includes/usb-audio-fix.bash"
        owner: "{{ item.split('/')[2] if item != '/root' else 'root' }}"
        group: "{{ item.split('/')[2] if item != '/root' else 'root' }}"
        mode: "0644"
      loop:
        - /root
        - /home/{{ user_login }}
      tags:
        - scope-gnome
```

No behaviour change on desktop (the task still runs, since `scope-gnome` is
not skipped there); on a server run (`--skip-tags scope-gnome`) this one task
is dropped and every other task in the play still runs.

### 3.2 `playbooks/imports/play-prevent-ssh-suspend.yml` — task-level override

**Before:**

```yaml
    - name: Disable suspend on AC power (plugged in = never idle-suspend)
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        argv:
          - gsettings
          - set
          - org.gnome.settings-daemon.plugins.power
          - sleep-inactive-ac-type
          - nothing
      environment:
        DBUS_SESSION_BUS_ADDRESS: "unix:path=/run/user/{{ ansible_facts.getent_passwd[user_login][1] }}/bus"
      changed_when: false
```

**After:**

```yaml
    - name: Disable suspend on AC power (plugged in = never idle-suspend)
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        argv:
          - gsettings
          - set
          - org.gnome.settings-daemon.plugins.power
          - sleep-inactive-ac-type
          - nothing
      environment:
        DBUS_SESSION_BUS_ADDRESS: "unix:path=/run/user/{{ ansible_facts.getent_passwd[user_login][1] }}/bus"
      changed_when: false
      tags:
        - scope-gnome
```

**This is not merely a tidiness fix — it fixes a real latent bug.** Without
this tag, running the whole play (or the whole `playbook-main.yml`) against a
headless server would hit this task, `gsettings` would fail with "No such
schema `org.gnome.settings-daemon.plugins.power`" (the schema is registered
by `gnome-settings-daemon`, not installed on a headless box), and — because
this task has no `failed_when: false` — the **entire play would abort**,
taking the still-general `ssh-suspend-guard` deployment down with it if it
runs later in task order (it doesn't, here, but the principle generalises:
an unscoped GNOME task inside a general play is a ticking failure, not just
wasted work). Play-level scope alone (§1) does not catch this — only the
task-level override does.

### 3.3 `playbooks/imports/play-vpn.yml` — task split (not just a tag add)

The existing single task bundles a GNOME-only package with two general ones,
so the fix is a genuine **split into two tasks**, not just appending `tags:`
to the existing one:

**Before:**

```yaml
    - name: Install VPN Packages
      ansible.builtin.dnf:
        name:
          - wireguard-tools
          - NetworkManager-openvpn
          - NetworkManager-openvpn-gnome
        state: present
```

**After:**

```yaml
    - name: Install VPN Packages (CLI, headless-safe)
      ansible.builtin.dnf:
        name:
          - wireguard-tools
          - NetworkManager-openvpn
        state: present

    - name: Install NetworkManager GNOME Applet Integration
      ansible.builtin.dnf:
        name:
          - NetworkManager-openvpn-gnome
        state: present
      tags:
        - scope-gnome
```

Plus the play-level `tags: [scope-general]` block per §2. On a server run,
only the applet package drops; the WireGuard/OpenVPN CLI tools and the
firewalld rule later in the file still install.

### 3.4 `playbooks/imports/optional/common/play-container-watch.yml` — exact interim task-tag diff (8 tasks, corrected from round 1)

`play-container-watch.yml` is the one instance found (core + fast-pass
optional sweep combined) where the graft rule's file-split branch is the
architecturally *right* fix long-term: the play deploys (a) a general-purpose
container-watchdog script + systemd timer, entirely CLI/general, and (b) a
GNOME Shell panel extension as a comparably-sized, non-trivial block of tasks
— not a single package or a single task. That split (e.g.
`play-container-watch.yml` watchdog-only + `play-container-watch-gnome-panel.yml`
extension-only) is **out of scope for this plan's Phase 3 implementation
checklist** — it lives in the optional tree and PLAN.md's non-goals don't
require restructuring it, only tagging it. Per §7 step 4, this pass applies
the **interim task-tag approach** instead: play-level `tags: [scope-general]`
plus a task-level `scope-gnome` override on every GNOME-shaped task. Below is
the exact diff for that interim, so nothing is left to prose-derived
guesswork.

**Round-2 correction**: the round-1 audit enumerated 7 GNOME-shaped tasks
(lines 88, 96, 142, 151, 161, 166, 180). Re-deriving the diff from the real
file surfaced an **8th task the audit's own list missed** — `Container-watch extension reload complete` (line 175, a `debug:` task with `when: enable_result.rc == 0`). This matters for more than completeness: `enable_result`
is `register`ed only by the "Enable container-watch extension" task (line
166), which the diff below tags `scope-gnome`. If line 175 were left
untagged while line 166 is skipped on a server run, evaluating `when: enable_result.rc == 0` would reference a variable that was never registered
— under this repo's `ansible.cfg` (which removed the old opt-out and now
always errors on undefined variables), that is a **hard Ansible error**, not
a silent no-op. This is the general hazard behind any task-level override:
**before tagging a task, check whether any other task in the same play
consumes a variable it `register`s via `when:` or templating — if so, the
consumer needs the identical override, or the pair belongs in a file-split
instead of a task-tag.** None of §3.1–§3.3's three core-play overrides have
this hazard (each is a self-contained, non-`register`ing task with no
downstream consumer) — it is specific to this file's longer register/when
chain, which is itself further evidence the file-split is the more robust
long-term fix here.

**Exact diff** — play-level block (§2) plus `tags: [scope-gnome]` appended to
each of these 8 tasks, verbatim, no other change to any task body:

```yaml
    - name: Ensure extension destination directory exists
      ansible.builtin.file:
        path: "{{ extension_dest }}"
        state: directory
        owner: "{{ user_login }}"
        group: "{{ user_login }}"
        mode: "0755"
      tags:
        - scope-gnome

    - name: Deploy container-watch GNOME extension
      ansible.builtin.copy:
        src: "{{ extension_src }}/"
        dest: "{{ extension_dest }}/"
        owner: "{{ user_login }}"
        group: "{{ user_login }}"
        mode: "0644"
        directory_mode: "0755"
      tags:
        - scope-gnome
```

```yaml
    - name: Check if container-watch extension is currently enabled
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.shell:
        cmd: gnome-extensions list --enabled | grep -q "{{ extension_name }}"
      register: extension_enabled
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe — extension may not be enabled yet
      tags:
        - scope-gnome

    - name: Disable container-watch extension to force reload
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        cmd: gnome-extensions disable {{ extension_name }}
      when: extension_enabled.rc == 0
      register: disable_result
      changed_when: disable_result.rc == 0
      failed_when: false  # FAIL-FAST-OK: disable may fail if GNOME session is unavailable
      tags:
        - scope-gnome

    - name: Wait for container-watch extension to unload
      ansible.builtin.pause:
        seconds: 2
      when: extension_enabled.rc == 0
      tags:
        - scope-gnome

    - name: Enable container-watch extension
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        cmd: gnome-extensions enable {{ extension_name }}
      register: enable_result
      changed_when: "'is now enabled' in enable_result.stderr or enable_result.rc == 0"
      failed_when: false  # FAIL-FAST-OK: enable may fail if GNOME session is unavailable
      tags:
        - scope-gnome

    - name: Container-watch extension reload complete
      ansible.builtin.debug:
        msg: "Container-watch extension reloaded successfully"
      when: enable_result.rc == 0
      tags:
        - scope-gnome

    - name: Container-watch extension enable deferred
      ansible.builtin.debug:
        msg: "Container-watch extension will be enabled on next GNOME session start (no active session detected)"
      when: enable_result.rc != 0
      tags:
        - scope-gnome
```

All other tasks in the file (the helper-library deploy, the CLI wrapper
deploy, the systemd `--user` timer deploy/enable) are untouched — they are
`scope-general` by virtue of the play-level tag alone, exactly like the 28
plain-addition plays in §2.

---

## 4. The QA check — Check 4 in `scripts/qa-ansible.bash`

Insert as a new section between the existing "Check 3: self-default vars"
block and the "Build JSON output" section (i.e. after the line
`done < "$TMP_MATCHES"` that closes Check 3, before the `# Encode FF_VIOLATIONS as a JSON array` comment). **Zero changes to `qa-all.bash`** —
this reuses the JSON blob `qa-ansible.bash` already emits and the merge
`qa-all.bash` already performs at `.checks.ansible`.

```bash
# ---------------------------------------------------------------------------
# Check 4: play-level scope declaration (Plan 00061 — headless server support)
# ---------------------------------------------------------------------------
# Every PLAYBOOK (any file with a top-level "- hosts:" line, same definition
# as Check 2) must declare EXACTLY ONE of scope-gnome / scope-general /
# scope-server as a PLAY-LEVEL tag — a "tags:" key at EXACTLY 2-space indent,
# sibling of hosts:/name:/become:, holding a block list of 4-space "- " items
# (see CLAUDE/AnsibleStyle.md "Scope Tags"). A task-level tag at deeper
# indentation (e.g. a trivial scope-gnome override inside an otherwise
# scope-general play — see play-vpn.yml) is INVISIBLE to this check by
# design: it enforces the play's own declared scope, not every scope-shaped
# tag mentioned anywhere in the file.
#
# imports/optional/archived/ is exempted: those plays are shelved and never
# imported by anything, so classifying them against a taxonomy invented after
# they were archived is not useful (Check 2's hygiene check does NOT exempt
# archived plays — this is a deliberate divergence: shebang/exec-bit hygiene
# is a mechanical property any tracked file should have; scope is a semantic
# "should this run in profile X" classification that doesn't apply to a file
# nobody runs). imports/optional/untested/ is NOT exempted — an "untested"
# play is still importable and runnable by a user.
#
# Inline array form (tags: [scope-general]) is NOT recognised by this parser
# — only block-list form. This is a stated, accepted limitation (see
# CLAUDE/AnsibleStyle.md), consistent with this script's existing grep-based
# checks all being format-sensitive to this repo's documented conventions
# rather than general-purpose YAML parsers (helpers/CLAUDE.md keeps helpers
# stdlib-only with no PyYAML available to lean on).
#
# A file with more than one "- hosts:" play (repo's only current instance:
# play-virtualbox-windows.yml) is REJECTED outright, not partially validated
# — this check operates at file granularity and cannot safely vouch for a
# second, unexamined play hiding behind the first play's tag. See the
# hosts_block_count guard below.
SCOPE_VALID_RE='scope-gnome|scope-general|scope-server'
SCOPE_VIOLATIONS=()

while IFS= read -r -d '' yml_file; do
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR" || continue
    [[ "$yml_file" == */optional/archived/* ]] && continue

    rel_file="${yml_file#"$REPO_ROOT"/}"

    # Multi-play-file guard: the awk extraction below only finds the FIRST
    # "  tags:" block in the file, so a file with more than one "- hosts:"
    # play (the repo's only current instance is
    # playbooks/imports/optional/experimental/play-virtualbox-windows.yml —
    # "Install Virtualbox" + "Setup Windows VMs") would otherwise let a
    # tagged first play silently vouch for an untagged second play, which
    # would then run on every profile regardless of --skip-tags. Detect this
    # explicitly and fail loudly rather than validate only the first play.
    hosts_block_count=$(grep -cE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" || true)
    if [[ $hosts_block_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — file contains $hosts_block_count separate '- hosts:' plays; this gate validates one scope tag per FILE and cannot safely vouch for a multi-play file. Give each play its own play-level scope tag AND split the file (one play per file, matching every other playbook in the repo), or the gate will keep rejecting it."
        SCOPE_VIOLATIONS+=("$rel_file (multi-play file: $hosts_block_count plays in one file, not supported)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    # Extract just the play-level tags: block's item VALUES (one per line,
    # "- " prefix stripped) — the FIRST "  tags:" line at exactly 2-space
    # indent, then every immediately-following 4-space "- " list item,
    # stopping at the first line that breaks that shape.
    play_tags=$(awk '
        /^  tags:[[:space:]]*$/ { in_block=1; next }
        in_block && /^    - [[:space:]]*[^[:space:]]/ {
            sub(/^    - [[:space:]]*/, "")
            sub(/[[:space:]]*#.*$/, "")   # strip a trailing "# why" comment
            sub(/\r$/, "")                # defensive: strip a stray CRLF remnant
            print
            next
        }
        in_block { exit }
    ' "$yml_file")

    # `grep -c` exits 1 (not 0) when it finds zero matches, even though it
    # still prints "0" to stdout — under this script's `set -euo pipefail`,
    # an unguarded `var=$(... | grep -c ...)` would abort the WHOLE script on
    # exactly the "no scope tag" case this check exists to catch (see Checks
    # 1 and 3 above, which guard the same hazard with `|| rc=$?`). `|| true`
    # here preserves the correct count while preventing the abort.
    valid_count=$(printf '%s\n' "$play_tags" | grep -xcE "$SCOPE_VALID_RE" || true)
    scope_like_count=$(printf '%s\n' "$play_tags" | grep -xcE 'scope-[A-Za-z0-9_-]+' || true)

    if [[ $valid_count -eq 1 ]]; then
        : # exactly one valid scope tag — OK, regardless of other unrelated tags in the list
    elif [[ $valid_count -eq 0 && $scope_like_count -eq 0 ]]; then
        echo "  ERROR (scope): $rel_file — no play-level scope tag (need exactly one of scope-gnome|scope-general|scope-server)"
        SCOPE_VIOLATIONS+=("$rel_file (missing scope tag)")
        ERRORS=$((ERRORS + 1))
    elif [[ $valid_count -eq 0 && $scope_like_count -gt 0 ]]; then
        bad=$(printf '%s\n' "$play_tags" | grep -xE 'scope-[A-Za-z0-9_-]+' | grep -vxE "$SCOPE_VALID_RE" | paste -sd, -)
        echo "  ERROR (scope): $rel_file — invalid scope tag(s): $bad (must be exactly one of scope-gnome|scope-general|scope-server)"
        SCOPE_VIOLATIONS+=("$rel_file (invalid scope tag: $bad)")
        ERRORS=$((ERRORS + 1))
    else
        # valid_count > 1
        echo "  ERROR (scope): $rel_file — multiple play-level scope tags declared (exactly one required)"
        SCOPE_VIOLATIONS+=("$rel_file (multiple scope tags)")
        ERRORS=$((ERRORS + 1))
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
```

**JSON-array-building** (insert alongside the existing `ff_json_array` /
`hy_json_array` / `sr_json_array` blocks, same idiom):

```bash
# Encode SCOPE_VIOLATIONS as a JSON array
sc_json_array="[]"
for v in "${SCOPE_VIOLATIONS[@]+"${SCOPE_VIOLATIONS[@]}"}"; do
    sc_json_array=$(printf '%s' "$sc_json_array" | jq --arg v "$v" '. + [$v]')
done
```

**Final `jq -n` call** — add one `--argjson`, one `failures` map branch, one
`checks` key (exact diff against the existing call at the bottom of
`qa-ansible.bash`):

```bash
jq -n \
    --arg status "$STATUS" \
    --argjson total "$TOTAL" \
    --argjson errors "$ERRORS" \
    --argjson ff "$ff_json_array" \
    --argjson hy "$hy_json_array" \
    --argjson sr "$sr_json_array" \
    --argjson sc "$sc_json_array" \
    '{
        "type": "ansible",
        "status": $status,
        "summary": {
            "total":  $total,
            "passed": ($total - $errors),
            "failed": $errors
        },
        "failures": (
            ($ff | map({"file": ., "type": "ansible", "status": "fail", "error": "fail-fast pattern without FAIL-FAST-OK annotation"})) +
            ($hy | map({"file": (. | split(" (")[0]), "type": "ansible", "status": "fail", "error": ("hygiene: " + (. | split(" (")[1] | rtrimstr(")"))) })) +
            ($sr | map({"file": ., "type": "ansible", "status": "fail", "error": "self-referential var (Ansible 2.19 recursive-loop error at runtime)"})) +
            ($sc | map({"file": (. | split(" (")[0]), "type": "ansible", "status": "fail", "error": ("scope: " + (. | split(" (")[1] | rtrimstr(")"))) }))
        ),
        "results": [],
        "checks": {
            "fail_fast": $ff,
            "hygiene":   $hy,
            "self_ref":  $sr,
            "scope":     $sc
        }
    }' > "$JSON_OUT"
```

**Terse summary** — extend the existing failure-branch echo to mention the
scope count (cosmetic, matches the existing `FF_COUNT`/`HY_COUNT`/`SR_COUNT`
pattern):

```bash
else
    FF_COUNT=${#FF_VIOLATIONS[@]}
    HY_COUNT=${#HYGIENE_VIOLATIONS[@]}
    SR_COUNT=${#SELFREF_VIOLATIONS[@]}
    SC_COUNT=${#SCOPE_VIOLATIONS[@]}
    echo "✗ ansible: $ERRORS violation(s) — $FF_COUNT fail-fast, $HY_COUNT hygiene, $SR_COUNT self-ref var, $SC_COUNT scope"
    echo "  Hygiene fixer: ./scripts/make-playbooks-executable.bash"
    echo "  Details: jq '.failures[]' $JSON_OUT"
    exit 1
fi
```

The success-branch echo (`✓ ansible: ...`) also gets `; $PLAYBOOK_COUNT... `
extended with a scope clause for symmetry — exact wording is an
implementation detail, not load-bearing.

**Confirmed: zero `qa-all.bash` edits.** `qa-all.bash` calls
`scripts/qa-ansible.bash`, captures its one JSON blob into `$TMP_ANSIBLE`,
and merges it into `.checks.ansible` at `.[3]` in the positional `jq -s`
array — all of that is unchanged; the new `checks.ansible.scope` key rides
along inside the same blob `qa-ansible.bash` already produces.

**`TOTAL` is deliberately not extended for the new check** (same reason Check
3's self-ref-var check doesn't extend it either): pass/fail is driven purely
by the shared `$ERRORS` counter, which Check 4 increments identically to
Checks 1–3, so a scope violation genuinely fails the script and propagates
through `qa-all.bash` regardless of `TOTAL`'s value. `TOTAL` only affects the
cosmetic "N files checked" count, not correctness — leaving it as
`PLAYBOOK_COUNT + 1` (synthetic fail-fast-scan entry) rather than `+ 2` is
consistent with Check 3's existing precedent, not an oversight.

**`imports/optional/**` coverage confirmed**: the `find "$REPO_ROOT/playbooks/" ...` root recurses into every subdirectory including `imports/optional/common`,
`experimental`, `hardware-specific`, `archived` (excluded by the explicit
`[[ ... == */optional/archived/* ]]` guard), and `untested` (included, though
currently empty) — this matches `qa-ansible-syntax.bash`'s existing playbook
discovery exactly, so a playbook that is syntax-checked today is scope-checked
too, with the one documented archived-tree exception.

---

## 5. Canonical commands + documentation

**Desktop** (documented as *the* desktop command going forward, replacing the
bare invocation — see §8 for the honest cost of this):

```bash
ansible-playbook playbooks/playbook-main.yml --skip-tags scope-server
```

**Server:**

```bash
ansible-playbook playbooks/playbook-main.yml --skip-tags scope-gnome
```

**Documentation locations:**

- **`docs/playbooks.md`** — insert a new `## Desktop vs. Headless Server Provisioning` section **immediately before line 17**, the existing
  `## Core Playbooks (Automatically Run)` heading (i.e. directly after the
  `## Quick Navigation` section that precedes it). Content: the two commands
  above, a one-paragraph explanation of the `scope-gnome`/`scope-general`/
  `scope-server` taxonomy, and a pointer to this plan folder for the full
  rationale. This is the file every other command in `docs/playbooks.md`
  already documents plays from, so it's the natural home.
- **`CLAUDE/AnsibleStyle.md`** — add a new subsection titled `### Scope Tags (Plan 00061)` directly after the existing `### Tagging Strategy` subsection
  (which already documents the `packages`/`pyenv`/`sysctl` task-tag
  convention this repo uses — scope tags are the play-level sibling
  convention). Content: the canonical block from §2 verbatim, plus the one-
  sentence rule "every playbook declares exactly one of `scope-gnome` |
  `scope-general` | `scope-server` as a play-level tag; a QA gate enforces
  this (`scripts/qa-ansible.bash`)."
- **`docs/playbooks.md`'s existing per-play sections** — `docs/playbooks.md`
  already has `### play-basic-configs.yml`, `### play-prevent-ssh-suspend.yml`,
  and `### play-vpn.yml` write-ups (confirmed present). Update each to
  mention its scope split: for `play-basic-configs.yml` and `play-vpn.yml`,
  a one-line note that the USB-audio-fix / GNOME-applet package is skipped on
  a server; for `play-prevent-ssh-suspend.yml`, note the bug being fixed
  (§3.2) — a headless run would previously hard-fail on the
  `gsettings`/GNOME-schema task, now cleanly skipped instead.

---

## 6. Zero-regression proof procedure

**Correction from `brainstorm-sonnet.md`**: the brainstorm proposed combining
`--check` with `--list-tasks`. That is unnecessary and, on closer reading of
`CLAUDE/ContainerRules.md`, **not container-safe** — `--check` still gathers
facts and evaluates the play against the live target (`localhost` inside the
CCY container, which is not a real desktop/server and lacks the expected
users/systemd state per `ContainerRules.md`'s "What CCY Container IS NOT"
list), unlike `--syntax-check`, which `qa-ansible-syntax.bash`'s own comment
says is safe specifically *because* "it does NOT execute any tasks."
`--list-tasks` alone shares that same safety property — it is a static
enumeration mode that parses and lists what *would* run without gathering
facts or executing anything, exactly like `--syntax-check` but showing
resolved task names and tags instead of just validating parse-ability. Use
**`--list-tasks` only, never `--check`,** inside the CCY container for this
proof:

```bash
export ANSIBLE_CONFIG="$(git rev-parse --show-toplevel)/ansible.cfg"

ansible-playbook playbooks/playbook-main.yml --list-tasks \
    > /tmp/list-before.txt

ansible-playbook playbooks/playbook-main.yml --skip-tags scope-server --list-tasks \
    > /tmp/list-desktop.txt
diff /tmp/list-before.txt /tmp/list-desktop.txt
# MUST be empty — proves the new documented desktop command is byte-identical
# to today's unscoped output (scope-server is empty today).

ansible-playbook playbooks/playbook-main.yml --skip-tags scope-gnome --list-tasks \
    > /tmp/list-server.txt
diff /tmp/list-before.txt /tmp/list-server.txt
# MUST show only scope-gnome-tagged tasks/plays removed — every GNOME play
# and the 3 task-level GNOME overrides from §3 should disappear from the list.
```

This is safe to run **inside the CCY container** (per the same rationale
`qa-ansible-syntax.bash` already documents for `--syntax-check`) and requires
no target-system state. A full `--check` dry-run (deeper validation, e.g.
catching a task whose `when:` references an undefined fact only available on
a real host) is legitimate but must happen **on the HOST**, per
`CLAUDE/ContainerRules.md` — it is not part of this proof and not required by
this plan's success criteria.

---

## 7. Implementation checklist (execute in order)

01. **Read this proposal's §1 tables** as the source of truth for every play's
    scope value. Do not re-derive classifications from scratch.
02. **Add the canonical `tags:` block (§2)** to all 31 core plays in
    `playbooks/imports/*.yml`, using the scope from §1.1's table. This is 28
    plain additions + the 3 mixed plays below.
03. **Apply the 3 mixed-play edits (§3.1–§3.3)** to `play-basic-configs.yml`,
    `play-prevent-ssh-suspend.yml`, and `play-vpn.yml` exactly as shown
    (note §3.3 is a task split, not just a tag add).
04. **Add the canonical `tags:` block** to all non-archived optional plays in
    `playbooks/imports/optional/{common,experimental,hardware-specific}/`
    using §1.3's fast-pass table, **with two named exceptions handled
    separately, not swept up in this bulk step**:
    - `play-container-watch.yml` — apply the exact 8-task interim diff in
      §3.4 verbatim (do **not** re-derive it from prose; do **not** perform
      the file-split in this pass).
    - `play-virtualbox-windows.yml` — **do not tag this file as-is.** Check 4
      (§4) hard-rejects it (two `- hosts:` plays in one file). First **split
      it** into two files — e.g. `play-virtualbox-windows.yml` (the existing
      "Install Virtualbox" play, lines 1–43 of the current file, plus its
      `handlers:` block) and `play-virtualbox-windows-vm-setup.yml` (the
      existing "Setup Windows VMs" play, lines 44 onward) — each with its own
      shebang (`#!/usr/bin/env ansible-playbook`) and executable bit per
      `playbooks/CLAUDE.md`. Then tag each independently: `scope-value` for
      "Install Virtualbox" is the same Low-confidence, owner-flagged call as
      `play-rpm-fusion.yml` (§1.3); "Setup Windows VMs" is more plausibly
      `scope-gnome` (interactive VM import/config workflow) but get an
      explicit owner decision for both before tagging, don't guess.
    - For every other row marked **Medium — verify** or **Low**, read the
      play's actual task list first and correct the classification if the
      fast-pass guess was wrong — do not blindly apply a flagged row.
05. **Edit `scripts/qa-ansible.bash`** per §4: insert Check 4 after Check 3
    (before the "Build JSON output" comment), add the `sc_json_array` block
    alongside the existing three, extend the final `jq -n` call with `--argjson sc`, the `failures` branch, and `"scope": $sc`, and extend both terse
    summary echoes. Confirm the two `|| true` guards (§4's blocker fix) and
    the multi-play-file guard are present exactly as shown — these are the
    load-bearing parts of this step. **Do not touch `qa-all.bash`.**
06. **Run `./scripts/qa-all.bash`.** Expect it to fail before step 2/4 land
    (every playbook missing a scope tag) and pass once all playbooks in
    `playbooks/` (except `imports/optional/archived/play-tlp-battery-optimisation.yml`)
    carry a valid scope tag, and `play-virtualbox-windows.yml` has been split
    per step 4. Fix any findings — most likely a missed optional play or a
    classification that needs the on-the-spot correction from step 4.
07. **Add the `docs/playbooks.md` section** per §5 (immediately before line 17,
    `## Core Playbooks`), the `CLAUDE/AnsibleStyle.md` subsection per §5
    (after `### Tagging Strategy`), and update the three existing per-play
    `docs/playbooks.md` sections (`play-basic-configs.yml`,
    `play-prevent-ssh-suspend.yml`, `play-vpn.yml`) per §5's per-play-doc
    bullet.
08. **Run the zero-regression proof (§6)** inside the CCY container. Both
    `diff` commands must behave exactly as specified — an empty diff for the
    desktop command, a GNOME-only-shrinkage diff for the server command. If
    the desktop diff is non-empty, something outside the documented 3 mixed
    plays introduced an unexpected task-level scope tag — find and fix it
    before proceeding.
09. **Re-run `./scripts/qa-all.bash`** one final time to confirm green after
    all edits.
10. **(On HOST, not in the CCY container — per `CLAUDE/ContainerRules.md`)**
    validate a real headless run in a VM/container per PLAN.md Task 3.7 —
    out of scope for this proposal document, tracked separately in PLAN.md.
11. **Update PLAN.md**: mark Phase 3 tasks 3.1–3.6 complete in the same
    commit as the code, per the project's Plan Commit Rule, and add a
    `JOURNAL/` entry recording what changed.

---

## 8. Known limitations / failure modes consciously accepted

- **The desktop command is no longer flag-free.** `--skip-tags scope-server`
  must be remembered/documented forever, even though `scope-server` is empty
  today and the flag is currently a no-op. If `docs/playbooks.md` (§5) is not
  kept in sync, and someone runs the bare `ansible-playbook playbooks/playbook-main.yml`, a *future* `scope-server`-only play would
  silently run on a desktop too. Mitigation: the doc update in step 7 is not
  optional cleanup, it is load-bearing for the zero-regression guarantee to
  hold structurally rather than just today.
- **Inline array `tags: [...]` form is invisible to the QA parser (§4).**
  A play written with that form would read as "no scope tag" (fails safe,
  over-strict) rather than being correctly parsed. Documented in both the
  script comment and `CLAUDE/AnsibleStyle.md`; the fix if it ever bites is
  "use block-list form, like every other example in this repo," not "add a
  YAML parser." (A trailing `# comment` on a tag line, by contrast, is now
  handled — round 1 caught this as a gap, fixed by stripping trailing `#...`
  and a stray `\r` in the awk extraction, §4.)
- **The fast-pass optional-tree classification (§1.3) is genuinely lower
  confidence than the core-31 table.** Several rows are marked Medium/Low and
  explicitly need a real read before their tag is trusted — this is called
  out in the implementation checklist (step 4) as a mandatory verification,
  not an optional nice-to-have. Shipping a wrong `scope-general` on a
  GUI-only optional play means it silently runs (and likely half-fails, e.g.
  on a missing display) on a server; shipping a wrong `scope-gnome` on a
  general optional play means it's silently skipped on a server that wanted
  it. Neither is a QA-gate-visible failure — the gate only checks that
  *a* valid tag exists, not that it's the *correct* one. This is the same
  "review-discipline gap, not a tooling gap" limitation `brainstorm-sonnet.md`
  already named for future task additions; it applies with extra force here
  because the fast-pass rows are lower-confidence by construction.
- **`play-virtualbox-windows.yml` is a second genuinely-arguable call**, in
  the same category as `play-rpm-fusion.yml` (Decision 2) — VirtualBox has a
  real, supported headless mode. Flagged in §1.3 for an explicit human
  decision rather than silently picking one side, matching how rpm-fusion was
  handled. **Round-2 addition**: this file also turned out to be the repo's
  only multi-play file (two independent `- hosts:` plays), which Check 4 now
  hard-rejects rather than silently trusting one play's tag for both (§4) —
  so this file needs a file-split (§7 step 4) before either play can be
  tagged at all, independent of which way the scope-value call goes.
- **`scope-gnome` is used as this repo's "needs a GUI session" bucket, not
  literally "GNOME-specific."** `play-lxde-install.yml` (an LXDE desktop, not
  GNOME) is tagged `scope-gnome` because the taxonomy's three names were
  locked by the owner in Decision 1/2 and this plan does not reopen that
  naming. Documented here so a future contributor isn't confused about why a
  non-GNOME desktop environment carries a `scope-gnome` tag.
- **`play-container-watch.yml`'s file-split is deferred; the interim
  task-tag diff (§3.4, 8 tasks) is what actually ships** in this pass's
  checklist (step 4). This is a deliberate scope-control decision — PLAN.md's
  non-goals don't require touching the optional tree's internal structure,
  only tagging it — but it does mean one optional play ships with a
  play-level/task-level split larger than, and with more register/`when:`
  interdependency than, the "trivial exception" the graft rule was designed
  for, as an accepted interim state. **Round-2 addition**: deriving the exact
  diff surfaced a genuine correctness hazard behind this — a task-level
  override on a `register`-ing task can silently break an untagged downstream
  task's `when:` (undefined variable, hard Ansible error under this repo's
  `ansible.cfg`) if their scopes are tagged inconsistently. The diff in §3.4
  accounts for this (all 8 interdependent tasks tagged together, not just the
  7 the round-1 audit found), but this class of hazard is exactly why a
  register/`when:`-heavy mixed play is a file-split candidate rather than a
  task-tag candidate in the first place — the interim here is accepted, not
  endorsed as the ideal shape.
- **`--skip-tags` still prints an empty `PLAY [...]` banner** for every
  skipped play (cosmetic console noise on a server run, ~10 empty banners for
  the GNOME core plays alone) — not a functional problem, previously noted in
  `brainstorm-sonnet.md`, repeated here because it's still true of the final
  design.
- **A future task added to an existing `scope-general` play that happens to
  need a GUI is not caught by this QA gate** — the gate checks that a play
  *has* a valid scope tag, not that every task in it still matches that
  scope. This is a review-discipline requirement on future contributors, not
  something a static grep-based gate can close without literally executing
  the play headless in CI (explicitly out of scope for this plan).
