# Proposal: Headless Server Provisioning — Auto-Detected `provisioning_profile` + `when:`

## Revision log (round 4 — two Check 4 fixes from `AUDIT-round-3.md`)

Fable's round-3 audit executed the Check 4 rewrite against a fixture that
reproduces the real repo's directory shape (core plays as direct children of
`playbooks/imports/`, optional plays under `playbooks/imports/optional/`) and
found two bugs, both confined to §4.1's Check 4 script. Everything else round
3 shipped — the detection layer, the fail-fast assert, the `playbook-main.yml`
diff, the container-watch list-`when:` fix, reuse integrity against round
2 — audited clean and is unchanged.

- **BLOCKER (fixed)**: 4b's file-discovery loop excluded only
  `playbook-main.yml` (by exact path) and `/optional/archived/`, so it
  processed all 31 core play files under `playbooks/imports/*.yml` too — each
  one satisfies 4b's "has a `- hosts:` line" playbook check and correctly has
  **no** `vars.scope` (core plays are classified at the import site, §4.1, by
  design), so 4b flagged all 31 as "missing `vars.scope`," unconditionally,
  from the very first `qa-all.bash` run. Fixed by adding
  `[[ "$yml_file" != */optional/* ]] && continue` as the first line of 4b's
  loop — core plays never even reach the rest of the checks now. Re-verified
  against a fixture with 3 core plays (general/gnome/server-shaped, correctly
  `when:`-gated, zero `vars.scope`), 2 optional plays (one clean, one
  genuinely missing `vars.scope`), and one archived play: exactly 1 error
  (the genuine optional-play violation), 0 false positives on the core or
  archived files. Also re-confirmed a `scope:` entry as the *last* of several
  `vars:` keys (mirroring `play-container-watch.yml`'s real shape) is still
  correctly recognised.
- **SHOULD-FIX (fixed, folded in rather than deferred — drift-prevention is
  the gate's whole purpose)**: 4a's `when:` rule was gated on
  `pending != ""`, so a comment or blank line between an `import_playbook:`
  and its `when:` let the catch-all rule flush `pending` first (recording the
  play as `general`/UNGUARDED) and silently swallow the orphaned `when:` line
  when it was reached — zero error reported, contradicting §3.1's own claim
  that the no-comment grammar is "enforced by Check 4." Did not affect the
  round-3 diff as shipped (no comment sits in that position for any of the 10
  real `gnome` imports), but was a real, permanent gap. Fixed by making the
  `when:` rule match **unconditionally** and branch on whether `pending` is
  already empty — emit an `ORPHANED|<line>` record instead of silently
  matching nothing, paired with a new bash `case` branch that reports it as
  an error. Re-verified: a comment-between-import-and-when: fixture now
  correctly errors (`orphaned when: line ... not immediately following an import_playbook: line`); the real, complete 31-import `playbook-main.yml`
  (§3.2, both real multi-line comment blocks included) still produces 31/31
  correct classifications, 0 errors — no regression. One residual gap noted
  in-code (a `when:` at the *wrong* indent depth is still invisible to
  either rule's regex) and in §9 below, per Fable's own severity call
  ("worth one comment line, not a separate fix").
- Both fixes were tested as the **exact text now in this document** — the
  Check 4 script was extracted from the markdown verbatim (not a
  hand-simplified stand-in) and executed under `set -euo pipefail` against
  every scenario above; zero crashes throughout.

---

## Revision log (round 3 — mechanism pivot to `when:`)

**Why**: PLAN.md Decision 4 (owner, after round 2 converged with zero
findings): the system must **auto-detect** desktop vs. server with **zero
config, zero runtime flags**. `--skip-tags` is resolved by the CLI before any
fact exists, so a tag-based design can never self-configure. `when:` is
evaluated at runtime against variables, so it *can* consume an
auto-detected `provisioning_profile` and gate itself with no flag. The
mechanism pivots; the underlying analysis does not.

**Reused verbatim from round 2** (mechanism-independent, unchanged): the
entire exhaustive classification of all 31 core plays (21 general / 10 gnome
/ 0 server, including the owner's `rpm-fusion`→general ruling and every
firm resolution of the round-2 ambiguous calls); the fast-pass classification
of the 41 optional plays; all three mixed-play discoveries (`play-basic-configs.yml` USB-audio, `play-prevent-ssh-suspend.yml` gsettings,
`play-vpn.yml` NetworkManager-openvpn-gnome) and the real latent-bug finding
behind the `gsettings` one; the `play-container-watch.yml` register/`when:`
interdependency hazard (8, not 7, tasks); the `play-virtualbox-windows.yml`
two-`hosts:`-plays-in-one-file finding. **Only the selection layer changes**
— sections renumbered below to fit the new flow (detect → gate core → gate
optional/QA → mixed edits → commands → verify → checklist → limits).

**New in this round, empirically verified** (not just reasoned about — every
claim below was tested against a real `ansible-playbook` [core 2.19.11] in
this container before being written down, mirroring the rigor
`AUDIT-round-1.md`/`AUDIT-round-2.md` established):

- **Auto-detect layer = `group_vars`, not a preflight `set_fact`.** Tested
  that `environment/localhost/group_vars/desktop.yml`'s `pipe` lookup
  (`systemctl get-default`) is **not** evaluated during `--syntax-check` or
  `--list-tasks` (container-safe, confirmed with a marker-file probe: 0
  evaluations in both modes), **is** evaluated on a real run (marker file
  created, `provisioning_profile` correctly resolved to `desktop` from
  `graphical.target`), and that `-e provisioning_profile=server` **skips the
  lookup entirely** (extra-vars precedence means the template is never
  rendered) while still correctly gating the downstream `when:`.
- **A bespoke top-level `scope:` play key is a hard Ansible error** (tested:
  `[ERROR]: 'scope' is not a valid attribute for a Play`) — confirms the
  optional-play marker *must* live inside `vars:`, exactly as the owner's
  brief specified ("a lightweight play-level `scope:` **var**").
- **The Check 4 rewrite (import-site `when:` parser) is `set -e`-safe by
  construction**, not by an `|| true` patch — tested against a fixture that
  exactly reproduces `playbook-main.yml`'s comment-block-between-imports
  shape (the real LXC/claude-yolo ordering comments) plus a zero-import-lines
  edge case; zero crashes, zero `grep -c`-on-empty hazards (the round-1
  blocker's root cause doesn't exist in this design — it never counts with
  `grep -c`).
- **Ansible's list-form `when:` short-circuits** (tested: a downstream task
  referencing `my_result.rc` where `my_result` is `register`ed by a task
  gated on the *same first condition* correctly skips without an undefined-
  variable error when the first condition is false) — this is what makes the
  `play-container-watch.yml` fix correct: combine the profile gate with each
  task's existing `register`-dependent `when:` as a **list**, not a
  replacement.
- **The final Check 4 script (§4.1), byte-for-byte as it now sits in this
  document**, was extracted from the markdown and executed against a
  fixture reproducing the real directory shape (`playbook-main.yml` with a
  mix of unguarded/gated imports, an optional play with `vars.scope`, one
  missing it, one archived) — 1 correctly-identified violation, 0 false
  positives, 0 crashes under `set -euo pipefail`. Not a hand-simplified
  stand-in script — the actual text.

**Superseded from round 2** (mechanism-specific, no longer applicable): the
play-level `tags:` canonical form, the tag-based Check 4 (grep/awk over
`tags:` blocks), the `--skip-tags`-based canonical commands, and the
`--list-tasks` zero-regression proof (confirmed this round, empirically:
`--list-tasks` does **not** evaluate `when:` — it lists the gnome task under
every profile, matching `prototype-when-import.md`'s Result 2 — so that
static in-container proof is gone; replaced with a host-side procedure, §7).

---

**Status of this document**: implementation-ready design for Plan 00061
Phase 3, superseding round 2's mechanism per PLAN.md Decision 4. Grounded in
`prototype-when-import.md` (the owner-commissioned prototype that established
`when:` viability) plus this round's own additional empirical verification
(above). Where this document and `brainstorm-sonnet.md` or the round-1/2
`PROPOSAL.md` disagree on mechanism, this document wins; where they agree on
classification, this document is the same analysis, restated.

---

## 1. Exhaustive per-play classification (unchanged from round 2)

### 1.1 Core plays (all 31)

Classification reflects **what the play's tasks actually do**, not filename
or folder (the same discipline the owner applied to `play-rpm-fusion.yml`).
"General" means: no task requires a GNOME session, a display server, or a
GUI application to be meaningful. The bucket names (`general`/`gnome`/
`server`) are now plain values, not tag strings — see §2–§3 for how each
expresses itself in the new mechanism.

| #   | Play                               | Bucket                                                          | One-line justification                                                                                                                               |
| --- | ---------------------------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `play-AA-preflight-sanity.yml`     | `general`                                                       | Ansible-version + Fedora-version assertions only                                                                                                     |
| 2   | `play-AB-dnf-upgrade.yml`          | `general`                                                       | Package upgrade + kernel half-install cleanup, no GUI                                                                                                |
| 3   | `play-basic-configs.yml`           | `general` (+ 1 task-level `when:` override — §5.1)              | vim colours, sudo, PS1, SSH helper scripts, `yq`, GRUB, `fwupd` — all general; one task (USB audio fix) is the exception                             |
| 4   | `play-prevent-ssh-suspend.yml`     | `general` (+ 1 task-level `when:` override — §5.2)              | `ssh-suspend-guard` systemd service is general; one task calls `gsettings set org.gnome.settings-daemon...` — GNOME-only                             |
| 5   | `play-network-wait-tuning.yml`     | `general`                                                       | `NetworkManager-wait-online.service` boot-timing tune, systemd-only                                                                                  |
| 6   | `play-mask-intel-lpmd.yml`         | `general`                                                       | Masks a systemd unit on Intel CPUs, self-probes and no-ops on AMD, no GUI                                                                            |
| 7   | `play-systemd-user-tweaks.yml`     | `general`                                                       | `systemd-oomd` memory-pressure fix for `user.slice`; protects rootless Podman/Docker containers — server-relevant, not GNOME-coupled                 |
| 8   | `play-nvm-install.yml`             | `general`                                                       | Node.js/npm via nvm, pure CLI toolchain                                                                                                              |
| 9   | `play-git-configure-and-tools.yml` | `general`                                                       | git config, `gh` CLI, bash-git-prompt, SSH agent prompt                                                                                              |
| 10  | `play-git-hooks-security.yml`      | `general`                                                       | Configures `core.hooksPath` on this repo's own clone                                                                                                 |
| 11  | `play-firefox.yml`                 | `gnome`                                                         | Installs the Firefox GUI browser                                                                                                                     |
| 12  | `play-github-cli-multi.yml`        | `general`                                                       | Multi-account `gh`/SSH-key/git-wrapper setup — 100% CLI/API/SSH, zero GUI dependency                                                                 |
| 13  | `play-ms-fonts.yml`                | `gnome`                                                         | MS core fonts are consumed only by GUI apps rendering on-screen; no headless consumer                                                                |
| 14  | `play-rpm-fusion.yml`              | `general`                                                       | **Owner-decided (Decision 2).** Enables free/nonfree repos — foundational plumbing later general packages may need                                   |
| 15  | `play-browsers.yml`                | `gnome`                                                         | Installs Chrome/Brave/Vivaldi GUI browsers                                                                                                           |
| 16  | `play-toolbox-install.yml`         | `gnome`                                                         | JetBrains Toolbox — a GUI IDE manager; already self-guards on `has_display` but gating avoids a pointless API call on a server                       |
| 17  | `play-docker.yml`                  | `general`                                                       | Rootful Docker CE — DDEV-class server workloads need this too                                                                                        |
| 18  | `play-lxc-install-config.yml`      | `general`                                                       | System containers, iptables/bridge networking, no GUI content                                                                                        |
| 19  | `play-podman.yml`                  | `general`                                                       | Rootless Podman + podman-compose                                                                                                                     |
| 20  | `play-python.yml`                  | `general`                                                       | pyenv/pipx/`semgrep`/PDM dev toolchain, no GUI                                                                                                       |
| 21  | `play-claude-yolo.yml`             | `general`                                                       | Container-based Claude Code tooling; in-container Chromium runs inside the container regardless of host GUI                                          |
| 22  | `play-claude-code.yml`             | `general`                                                       | Claude Code CLI installer + `cc` wrapper, pure CLI                                                                                                   |
| 23  | `play-comms.yml`                   | `gnome`                                                         | Installs Slack via Flatpak (GUI app)                                                                                                                 |
| 24  | `play-gnome-shell.yml`             | `gnome`                                                         | Installs `gnome-tweaks` (its `name:` duplicates `play-gnome-shell-extensions.yml`'s — pre-existing quirk, out of scope)                              |
| 25  | `play-gnome-shell-extensions.yml`  | `gnome`                                                         | GNOME extension installer, dconf schema compilation, custom extension deploy                                                                         |
| 26  | `play-markless.yml`                | `general`                                                       | Terminal-based markdown viewer, no GUI dependency                                                                                                    |
| 27  | `play-terminal-emulators.yml`      | `gnome`                                                         | Alacritty/Kitty/Ghostty/Foot are GUI windowed applications, despite "terminal" in the name                                                           |
| 28  | `play-vscode.yml`                  | `gnome`                                                         | Installs the VS Code GUI editor                                                                                                                      |
| 29  | `play-vpn.yml`                     | `general` (+ 1 task **split** with a `when:`-gated task — §5.3) | WireGuard/OpenVPN CLI tools + firewalld rule are general; `NetworkManager-openvpn-gnome` (bundled in the same `dnf` task today) is GNOME-applet-only |
| 30  | `play-gsettings.yml`               | `gnome`                                                         | Caps Lock remap + Ptyxis terminal tab setting via `dconf` — GNOME desktop settings                                                                   |
| 31  | `play-ZZ-repo-cleanup.yml`         | `general`                                                       | Removes orphaned COPRs, no GUI content                                                                                                               |

**Tally: 21 `general`, 10 `gnome`, 0 `server`.** The empty `server` bucket is
intentional — this plan's non-goals rule out server-hardening content now;
the taxonomy needs the bucket to exist for a *future* plan.

### 1.2 Mixed-concern task catalogue (unchanged from round 2)

| Play                           | Exact task                                                      | Package/setting                                                                        | Why it's the exception                                                                                                                                     |
| ------------------------------ | --------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-basic-configs.yml`       | `Deploy USB audio fix script`                                   | `files/home/bashrc-includes/usb-audio-fix.bash` bashrc-include                         | Desktop-audio-hardware concern, no headless-server consumer                                                                                                |
| `play-prevent-ssh-suspend.yml` | `Disable suspend on AC power (plugged in = never idle-suspend)` | `gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type nothing` | GNOME Settings Daemon schema over D-Bus — **hard-fails** ("No such schema") on a server without `gnome-settings-daemon`; a real latent bug the split fixes |
| `play-vpn.yml`                 | `Install VPN Packages` (one `dnf` task, 3 packages)             | `NetworkManager-openvpn-gnome`                                                         | GNOME NetworkManager-applet integration; the other two packages are pure CLI                                                                               |

All three are trivial, single-item exceptions; zero core plays need a
file-split. See §5 for the `when:`-based fix (round 2 used task-level tags;
this round uses task-level `when:`).

### 1.3 Optional plays (fast-pass — reused from round 2, table content

unchanged, `scope-general`/`scope-gnome` labels now read as plain
`general`/`gnome`)

The 41-file fast-pass classification (30 in `optional/common/`, 4 in
`optional/experimental/`, 7 in `optional/hardware-specific/`) is unchanged
from round 2 — same plays, same confidence markers, same notes, same two
flagged files (`play-container-watch.yml` — mixed, needs the interim fix in
§5.4; `play-virtualbox-windows.yml` — two `- hosts:` plays in one file, must
be split before it can be classified at all, same Low-confidence owner-flag
as `rpm-fusion` for the resulting scope value(s)). It is reproduced verbatim
below (values now read as plain `general`/`gnome` since there's no tag string
to prefix — same substance as round 2's `scope-general`/`scope-gnome`
columns). The exact, byte-identical round-2 source is also committed at
`568a5d5` (`git show 568a5d5:CLAUDE/Plan/00061-headless-server-provisioning/PROPOSAL.md`, §1.3) if a diff against this reproduction is ever needed.

**`playbooks/imports/optional/common/` (30 files):**

| Play                                  | Bucket                                      | Confidence          | Note                                                                                                                                                                                           |
| ------------------------------------- | ------------------------------------------- | ------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-advanced-kernel-management.yml` | `general`                                   | High                | Kernel versionlock/retention, no GUI                                                                                                                                                           |
| `play-claude-devtools.yml`            | `general`                                   | High                | CLI session viewer (`ccdt`)                                                                                                                                                                    |
| `play-clean-paste.yml`                | `gnome`                                     | High                | Wayland clipboard sanitiser bound to a GNOME/Wayland keybinding                                                                                                                                |
| `play-cloudflare-dns.yml`             | `general`                                   | High                | DNS-over-TLS resolver config, systemd/network only                                                                                                                                             |
| `play-cloudflare-warp.yml`            | `general`                                   | **Medium — verify** | Name suggests a CLI daemon (`warp-cli`); grep hit on "GUI" needs confirming it isn't the optional GUI client                                                                                   |
| `play-collaboration.yml`              | `gnome`                                     | **Medium — verify** | Likely Flatpak GUI apps (naming convention matches `play-comms.yml`); confirm task list                                                                                                        |
| `play-compression-helpers.yml`        | `general`                                   | High                | `compress`/`uncompress` CLI commands                                                                                                                                                           |
| `play-container-watch.yml`            | **MIXED — §5.4**                            | High                | Deploys a general-purpose watcher daemon AND a GNOME Shell panel extension in one file; not a trivial exception                                                                                |
| `play-darktable-ai-appimage.yml`      | `gnome`                                     | High                | GUI photo-editing AppImage (darktable), Flatpak-adjacent                                                                                                                                       |
| `play-darktable-ai-build.yml`         | `gnome`                                     | High                | Builds the same GUI app from source                                                                                                                                                            |
| `play-ddev.yml`                       | `general`                                   | High                | Docker-based local dev environment, CLI                                                                                                                                                        |
| `play-distrobox.yml`                  | `general`                                   | High                | Container tooling, CLI                                                                                                                                                                         |
| `play-fast-file-manager.yml`          | `gnome`                                     | High                | Configures GNOME Nautilus/file-picker performance                                                                                                                                              |
| `play-ftp-camera.yml`                 | `gnome`                                     | **Medium — verify** | Camera FTP server naming suggests a background service (general), but grep hit on GNOME/window warrants a task-list check                                                                      |
| `play-gnome-shell-dev.yml`            | `gnome`                                     | High                | GNOME Shell extension development tooling                                                                                                                                                      |
| `play-golang.yml`                     | `general`                                   | High                | Go toolchain, CLI                                                                                                                                                                              |
| `play-hd-audio.yml`                   | `general`                                   | **Medium — verify** | Audio/Bluetooth enhancement — could be general (audio subsystem) or gnome (desktop sound settings); check tasks                                                                                |
| `play-image-watermarking.yml`         | `general`                                   | High                | `watermark` CLI command (ImageMagick + exiftool)                                                                                                                                               |
| `play-lastpass.yml`                   | `gnome`                                     | **Medium — verify** | Likely the LastPass GUI/browser-extension config; confirm                                                                                                                                      |
| `play-lightweight-ides.yml`           | `gnome`                                     | High                | GUI IDE installs                                                                                                                                                                               |
| `play-network-tools.yml`              | `general`                                   | High                | Network discovery CLI tools                                                                                                                                                                    |
| `play-nordvpn-openvpn.yml`            | `gnome`                                     | **Medium — verify** | Name suggests CLI VPN manager (general), but grep hits GUI/GNOME strongly — likely a mixed play like `play-vpn.yml`; needs the same task-level `when:` treatment once read in full             |
| `play-photography.yml`                | `gnome`                                     | High                | GUI photography app suite                                                                                                                                                                      |
| `play-qobuz.yml`                      | `gnome` (+ likely mixed, per own play name) | **Medium — verify** | Play name literally says "Native GUI Player, CLI Player and Last.fm Scrobbling" — self-describes as mixed; needs a real read to decide file-split vs. task-level `when:` before implementation |
| `play-rclone.yml`                     | `general`                                   | High                | Cloud storage CLI mounts; grep GNOME hits are likely a GUI tray-icon extra, verify if so it needs a task-level split like `play-vpn.yml`                                                       |
| `play-remote-desktop-toggle.yml`      | `gnome`                                     | High                | Toggles GNOME's built-in remote-desktop sharing                                                                                                                                                |
| `play-rust-dev.yml`                   | `general`                                   | High                | Rust toolchain, CLI                                                                                                                                                                            |
| `play-speech-to-text.yml`             | `gnome`                                     | High                | GNOME Shell extension + Wayland keybinding (per `CLAUDE/GnomeShell.md`)                                                                                                                        |
| `play-unifi-controller.yml`           | `general`                                   | High                | Podman-Compose UniFi controller — a server workload if anything                                                                                                                                |
| `play-videography.yml`                | `gnome`                                     | High                | GUI video-editing tool suite                                                                                                                                                                   |

**`playbooks/imports/optional/experimental/` (4 files):**

| Play                                 | Bucket                                                                    | Confidence               | Note                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------------ | ------------------------------------------------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-docker-in-lxc-support.yml`     | `general`                                                                 | High                     | Container interop config                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `play-docker-overlay2-migration.yml` | `general`                                                                 | High                     | Docker storage-driver migration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `play-lxde-install.yml`              | `gnome`                                                                   | High                     | Installs the LXDE **desktop environment** — not literally GNOME, but falls in the "needs a GUI session" bucket (see §9 naming note)                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| `play-virtualbox-windows.yml`        | **file has TWO `- hosts:` plays — MUST be split first (§4.1, §8 step 5)** | **Low — flag for owner** | "Install Virtualbox" (driver + packages + group membership — arguably headless-capable via `VBoxHeadless`/`VBoxManage`, same category of call as the rpm-fusion dispute) at line 3; a structurally separate "Setup Windows VMs" play (downloads/imports a specific Windows 11 VM image, more plausibly GUI-workflow-coupled) at line 44. Split into `play-virtualbox-windows.yml` (lines 1–43 + `handlers:`) and `play-virtualbox-windows-vm-setup.yml` (line 44 onward), each with its own shebang + exec bit, then get an explicit owner decision on both `scope` values |

**`playbooks/imports/optional/hardware-specific/` (7 files):**

| Play                                   | Bucket    | Confidence | Note                                                                                                                                                                |
| -------------------------------------- | --------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-darktable-ai-gpu.yml`            | `gnome`   | High       | GPU backend for the GUI darktable app                                                                                                                               |
| `play-displaylink.yml`                 | `gnome`   | High       | Extends physical monitors via a GNOME/mutter multi-monitor session                                                                                                  |
| `play-ipu6-webcam.yml`                 | `gnome`   | High       | Webcam driver stack; only consumer is GUI video-calling apps, no headless use case in this repo                                                                     |
| `play-laptop-lid-power-management.yml` | `general` | High       | ACPI/systemd lid-close behaviour, no GUI dependency (irrelevant to rack servers as *hardware*, but that's an inventory question, not a GUI-dependency one — see §9) |
| `play-laptop-thermal-diagnostics.yml`  | `general` | High       | CLI thermal diagnostics                                                                                                                                             |
| `play-musiccast.yml`                   | `gnome`   | High       | Play name explicitly says "SSDP diagnostics + gyrc **GUI**"                                                                                                         |
| `play-nvidia.yml`                      | `general` | High       | NVIDIA driver install — needed for both desktop GPU rendering *and* headless CUDA/compute servers; not GNOME-coupled                                                |

**`playbooks/imports/optional/archived/` (1 file) and `untested/` (0 files):
exempt from classification per §4.1** (archived) or **not yet applicable**
(untested is currently empty).

---

## 2. Auto-detected `provisioning_profile` — the detection layer

### 2.1 Design: `group_vars`, not a preflight `set_fact`

**Decision: `environment/localhost/group_vars/desktop.yml`.**

This repo's `ansible.cfg` sets `transport=local` and every play targets
`hosts: desktop`, with `environment/localhost/hosts.yml` mapping that group
to the single host `localhost` with `ansible_connection: local`. Because the
controller **is** the target, a `pipe` lookup in a group_vars file executes
on the real box. `environment/localhost/host_vars/localhost.yml` already
exists and is the established, working precedent for
`environment/localhost/` being an inventory-relative `group_vars`/`host_vars`
root — `group_vars/desktop.yml` is the direct sibling of that pattern, named
after the group every play already targets.

**Rejected: a preflight-play `set_fact`.** Two reasons. First, it is strictly
*less* available: a `set_fact` task only makes its fact visible to plays that
run *after* the task that sets it, so it constrains where the assertion/gate
logic can safely live in the ordering. `group_vars` is loaded before **any**
play runs, including the very first task of the very first play
(`play-AA-preflight-sanity.yml`), which is exactly where this proposal adds a
validation assertion (§2.3) — no ordering dependency to get wrong. Second, it
is an extra moving part (a task, in a specific play, that must never be
reordered ahead of) for no benefit over a file that Ansible already
auto-loads for free.

**Exact file** (new):

```yaml
# environment/localhost/group_vars/desktop.yml
---
# Auto-detected provisioning profile — desktop vs. headless server.
#
# `ansible.cfg` sets transport=local and every play targets hosts: desktop,
# which environment/localhost/hosts.yml maps to localhost with
# ansible_connection: local — the controller IS the target, so this `pipe`
# lookup reads the REAL box's systemd default target. This assumption breaks
# if this repo is ever pointed at a remote host (lookups execute on the
# CONTROL node, not the managed node) — it never is; every play in this repo
# is hosts: desktop / connection: local, by design (CLAUDE/AnsibleStyle.md).
#
# Server-biased when uncertain: ONLY a confirmed graphical.target resolves to
# desktop; multi-user.target, a get-default failure, or any unrecognised
# target resolves to server. A mis-detected desktop just skips GUI installs
# (recoverable — re-run with the override below); a mis-detected server would
# run the gnome plays against a box with no GNOME session and hit the
# gsettings hard-fail (§1.2) under this repo's `any_errors_fatal = true`
# (whole-RUN abort, not just the one play) — server-biased avoids that
# failure mode by construction, not by luck.
#
# Override for testing/CI (also skips the lookup entirely — extra-vars have
# the highest precedence, so this template is never rendered when overridden):
#   -e provisioning_profile=desktop
#   -e provisioning_profile=server
_systemd_default_target: "{{ lookup('ansible.builtin.pipe', 'systemctl get-default') }}"
provisioning_profile: "{{ 'desktop' if _systemd_default_target == 'graphical.target' else 'server' }}"
```

Not a self-referential var (`provisioning_profile` templates from
`_systemd_default_target`, a *different* variable) — safe under the
Ansible 2.19 self-default recursion footgun documented in
`CLAUDE/AgentNotes.md`; that footgun is specifically about a variable
defaulting to itself (`x: "{{ x | default(...) }}"`), which this is not.

### 2.2 Empirical verification this round

Built a throwaway fixture (`inv/group_vars/desktop.yml` with a marker-file
side-effect wired into the `pipe` lookup, `playbooks/main.yml` with one
general play and one `when: provisioning_profile != 'server'`-gated play,
`hosts.yml` matching this repo's real shape) and tested against
`ansible-playbook [core 2.19.11]`, the same version `AUDIT-round-1.md`/
`AUDIT-round-2.md` used:

| Command                                    | Marker created (lookup ran)?                      | Gnome task                                                                                          |
| ------------------------------------------ | ------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `--syntax-check`                           | No                                                | n/a (parse-only)                                                                                    |
| `--list-tasks`                             | No                                                | listed under every profile (doesn't evaluate `when:` — matches `prototype-when-import.md` Result 2) |
| real run, no `-e`                          | **Yes**                                           | ran (auto-detected `graphical.target` → `desktop`)                                                  |
| real run, `-e provisioning_profile=server` | **No** (extra-vars precedence skips the template) | `skipping: [localhost]`                                                                             |

This directly confirms: (a) the detection layer is inert during both
CCY-container-safe static checks this repo already relies on
(`--syntax-check` for `qa-ansible-syntax.bash`, `--list-tasks` for docs/human
inspection), (b) it correctly self-configures on a real run with zero flags,
and (c) the override is not just "correct" but *free* — it never shells out
to `systemctl` at all when a human or CI already knows the answer.

### 2.3 Fail-fast guard against a typo'd override

Per this repo's #1 rule (`CLAUDE.md` "Fail Fast — HARD RULE"), a mistyped
`-e provisioning_profile=srever` should not silently degrade into
desktop-like behaviour (every `gnome`-bucket play would run, since
`!= 'server'` is true for any value that isn't literally `'server'`) — it
should fail loudly. Add a third assertion to
`playbooks/imports/play-AA-preflight-sanity.yml` (which already exists
purely to assert Ansible/Fedora version — the natural, zero-new-file home):

**Before** (existing file, full content):

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: Preflight Sanity
  become: true
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
  pre_tasks:
    - name: Load Fedora version config
      ansible.builtin.include_vars:
        file: "{{ root_dir }}/vars/fedora-version.yml"
  tasks:
    - name: Check Ansible
      ansible.builtin.assert:
        that:
          - ansible_version.full is version('2.20', '>=')
        fail_msg: |
          This project requires Ansible 2.20 or greater

    - name: Check Fedora
      ansible.builtin.assert:
        that:
          - ansible_facts['os_family'] == 'RedHat'
          - ansible_facts['distribution'] == 'Fedora'
          - ansible_facts['distribution_major_version'] | int == fedora_version | int
        fail_msg: "This project expects Fedora {{ fedora_version }}"
```

**After** (add a third `assert` task):

```yaml
    - name: Check Fedora
      ansible.builtin.assert:
        that:
          - ansible_facts['os_family'] == 'RedHat'
          - ansible_facts['distribution'] == 'Fedora'
          - ansible_facts['distribution_major_version'] | int == fedora_version | int
        fail_msg: "This project expects Fedora {{ fedora_version }}"

    - name: Check provisioning_profile is a recognised value
      ansible.builtin.assert:
        that:
          - provisioning_profile in ['desktop', 'server']
        fail_msg: |
          provisioning_profile={{ provisioning_profile }} is not recognised.
          Valid values: desktop, server.
          Auto-detected from `systemctl get-default`
          (see environment/localhost/group_vars/desktop.yml) or overridden
          via -e provisioning_profile=desktop|server.
```

This runs before any `import_playbook: ... when:` is evaluated
(`play-AA-preflight-sanity.yml` is the first import in `playbook-main.yml`),
so a typo'd override is caught at the very start of the run, not partway
through with some gnome plays already applied.

---

## 3. Selection for core plays — `when:` on `import_playbook:`

### 3.1 The three canonical forms

```yaml
# general — no gate, always imported
- import_playbook: imports/play-docker.yml

# gnome — imported unless the profile is server
- import_playbook: imports/play-firefox.yml
  when: provisioning_profile != 'server'

# server (bucket exists, currently unused — see §1.1's tally) — imported ONLY when the profile is server
- import_playbook: imports/play-some-future-hardening-play.yml
  when: provisioning_profile == 'server'
```

`when:` on `import_playbook` is a documented, supported Ansible feature: the
condition is merged into every task of every play the import brings in and
evaluated per-task at actual run time (not at parse time, so no facts-before-
parsing chicken-and-egg problem) — this is exactly what
`prototype-when-import.md` verified and this round's own test (§2.2)
re-confirmed against the real inventory shape.

**Deliberately no `| default('desktop')` in the condition.** Since
`group_vars/desktop.yml` (§2) unconditionally defines `provisioning_profile`
before any play runs, the variable is never undefined when a core import's
`when:` evaluates — adding a defensive default would be dead code. This also
keeps the condition string exactly as simple as PLAN.md Decision 4 asked
("avoid colons/quotes/complex Jinja that trips the 2.19 splitter" —
`provisioning_profile != 'server'` has no colon, one pair of single quotes,
no filters, no `{{ }}` — about as simple as a Jinja boolean gets). Confirmed
by this round's `--syntax-check` test against the real-shaped fixture: clean
parse, no splitter complaints.

**Grammar rule, enforced by Check 4 (§4.1)**: `when:` must be the line
**immediately following** its `import_playbook:` line, at exactly 2-space
indent (sibling of `import_playbook:` within the same list item) — no
comment or blank line between them. Ordering-rationale comments (like the
existing LXC/claude-yolo notes in `playbook-main.yml`) go **before** the
`- import_playbook:` line they relate to, never between an import and its
`when:`. This matches how the file is already written today — the existing
comment blocks sit between two *different* imports, never inside one.

### 3.2 Exact `playbook-main.yml` diff

Full file, "after" state (comments preserved verbatim from the current file;
only the 10 `gnome`-bucket imports gain a `when:` line — the shebang and the
top "Everything imported here runs by default" note are unchanged):

```yaml
#!/usr/bin/env ansible-playbook
---
# NOTE: Everything imported here runs by default and is NOT optional.
# Do not store playbooks imported here under imports/optional/ — move them to imports/.
- import_playbook: imports/play-AA-preflight-sanity.yml
- import_playbook: imports/play-AB-dnf-upgrade.yml
- import_playbook: imports/play-basic-configs.yml
- import_playbook: imports/play-prevent-ssh-suspend.yml
- import_playbook: imports/play-network-wait-tuning.yml
- import_playbook: imports/play-mask-intel-lpmd.yml
- import_playbook: imports/play-systemd-user-tweaks.yml
- import_playbook: imports/play-nvm-install.yml
- import_playbook: imports/play-git-configure-and-tools.yml
- import_playbook: imports/play-git-hooks-security.yml
- import_playbook: imports/play-firefox.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-github-cli-multi.yml
- import_playbook: imports/play-ms-fonts.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-rpm-fusion.yml
- import_playbook: imports/play-browsers.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-toolbox-install.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-docker.yml
# LXC runs AFTER Docker so DOCKER-USER chain exists when LXC reconciles
# iptables for outbound connectivity (Docker coexistence). See the
# "Reconcile iptables" block in play-lxc-install-config.yml.
#
# LXC does NOT need to come after Podman — play-podman.yml installs rootless
# Podman (slirp4netns/pasta networking), which does not touch host iptables.
- import_playbook: imports/play-lxc-install-config.yml
- import_playbook: imports/play-podman.yml
- import_playbook: imports/play-python.yml
# claude-yolo (ccy) MUST run before claude-code: play-claude-code's cc
# wrapper sources /var/local/claude-yolo/lib/{common-pure,token-management}.bash
# at runtime, and play-claude-code asserts the lib is present before
# deploying the wrapper. See CLAUDE/Plan/00048-cc-token-source-parity.
- import_playbook: imports/play-claude-yolo.yml
- import_playbook: imports/play-claude-code.yml
- import_playbook: imports/play-comms.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-gnome-shell.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-gnome-shell-extensions.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-markless.yml
- import_playbook: imports/play-terminal-emulators.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-vscode.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-vpn.yml
- import_playbook: imports/play-gsettings.yml
  when: provisioning_profile != 'server'
- import_playbook: imports/play-ZZ-repo-cleanup.yml
```

Empirically re-validated this round: fed this **exact** text (including the
real comment blocks, reconstructed verbatim) through both the Check 4 parser
prototype (§4.1 — 31/31 imports correctly classified, 0 errors) and
`ansible-playbook --syntax-check` equivalent structure — no parser
complaints, no misattributed `when:` (the LXC/claude-yolo comment blocks
correctly do **not** get treated as belonging to the gnome import that
precedes them).

---

## 4. Where scope lives for optional plays + the QA gate (Check 4 rewrite)

### 4.1 Core plays: the `when:` gate **is** the declaration — Check 4 parses `playbook-main.yml`

There is no separate "declare scope" step for core plays distinct from
§3 — the import-site `when:` (or its absence) **is** the classification.
Check 4 validates that every one of the 31 `import_playbook:` lines in
`playbook-main.yml` carries exactly one **recognised** shape: no `when:`
(general), `when: provisioning_profile != 'server'` (gnome), or
`when: provisioning_profile == 'server'` (server). Anything else — a typo, an
unrecognised condition, a `when:` using `| default(...)` or double quotes or
any other variant — is a hard error. This intentionally does **not** attempt
to be a general Jinja-expression evaluator; like every other check in
`qa-ansible.bash`, it is a format-sensitive parser matched to one documented,
canonical string (see the grammar rule in §3.1).

Insert as Check 4 in `scripts/qa-ansible.bash`, in the same slot round 2 used
(between Check 3's closing `done < "$TMP_MATCHES"` and the "Build JSON
output" section). **Zero changes to `qa-all.bash`** — same reasoning as
round 2: this reuses the JSON blob `qa-ansible.bash` already emits and the
merge `qa-all.bash` already performs at `.checks.ansible`.

```bash
# ---------------------------------------------------------------------------
# Check 4: provisioning-profile scope declaration (Plan 00061, round 3)
# ---------------------------------------------------------------------------
# CORE plays (imported from playbook-main.yml): the scope declaration IS the
# import-site `when:` gate, not a tag inside the play file. Every
# `- import_playbook: ...` line in playbook-main.yml must carry EXACTLY ONE
# recognised shape:
#   (no `when:` line)                        -> general
#   when: provisioning_profile != 'server'    -> gnome   (exact string match)
#   when: provisioning_profile == 'server'    -> server  (exact string match)
# `when:` must be the line IMMEDIATELY following its import (2-space indent,
# sibling of import_playbook: in the same list item) — no comment or blank
# line between them (ordering-rationale comments go BEFORE the import line;
# this matches how playbook-main.yml is already written today).
#
# OPTIONAL plays (never imported from playbook-main.yml, invoked by path)
# have no import site for a `when:` gate to attach to, so they carry an
# informational play-level `scope: general|gnome|server` VAR inside their
# `vars:` block instead (§4.2 of PROPOSAL.md). A bare top-level `scope:` key
# (sibling of hosts:/name:) is NOT valid Ansible — confirmed empirically,
# `ansible-playbook` rejects it with "'scope' is not a valid attribute for a
# Play" — so this MUST be a vars: entry, never a play-level key.
#
# Both loops below share the same multi-play-file guard: a file with more
# than one "- hosts:" play (repo's only current instance: the OPTIONAL play
# play-virtualbox-windows.yml) is REJECTED outright — this check cannot
# safely vouch for a second, unexamined play hiding behind the first play's
# declaration.
CORE_MAIN="$REPO_ROOT/playbooks/playbook-main.yml"
SCOPE_VIOLATIONS=()

# --- 4a: core plays — parse playbook-main.yml's import + when: lines -------
while IFS='|' read -r import_path cond; do
    [[ -z "$import_path" ]] && continue
    if [[ "$import_path" == "ORPHANED" ]]; then
        echo "  ERROR (scope): playbooks/playbook-main.yml — orphaned when: line ($cond) not immediately following an import_playbook: line. Move any rationale comment BEFORE the import line, not between the import and its when:."
        SCOPE_VIOLATIONS+=("playbooks/playbook-main.yml (orphaned when: $cond)")
        ERRORS=$((ERRORS + 1))
        continue
    fi
    case "$cond" in
        UNGUARDED) : ;;                                    # general — OK
        "provisioning_profile != 'server'") : ;;            # gnome — OK
        "provisioning_profile == 'server'") : ;;            # server — OK
        *)
            echo "  ERROR (scope): playbooks/playbook-main.yml — $import_path has an unrecognised when: condition: $cond"
            SCOPE_VIOLATIONS+=("playbooks/playbook-main.yml:$import_path (invalid when: $cond)")
            ERRORS=$((ERRORS + 1))
            ;;
    esac
done < <(awk '
    /^- import_playbook: / {
        if (pending != "") { print pending "|UNGUARDED" }
        pending = $0
        sub(/^- import_playbook: /, "", pending)
        next
    }
    # Fires UNCONDITIONALLY (not gated on pending != "") so a when: line
    # that is NOT immediately preceded by an import (a comment or blank
    # line intervened, or two when: lines follow one import) is reported
    # as ORPHANED instead of being silently swallowed by the catch-all
    # below, which would otherwise flush `pending` first and let the
    # when: fall through as a second, ignored UNGUARDED resolution. A
    # when: at the WRONG indent depth (not exactly 2 spaces) still will
    # not match this rule at all — that residual gap is a smaller,
    # separately-tracked limitation (PROPOSAL.md §9), not fixed here.
    /^  when: / {
        if (pending == "") { print "ORPHANED|" $0; next }
        cond = $0
        sub(/^  when: /, "", cond)
        print pending "|" cond
        pending = ""
        next
    }
    { if (pending != "") { print pending "|UNGUARDED"; pending = "" } }
    END { if (pending != "") print pending "|UNGUARDED" }
' "$CORE_MAIN")

# --- 4b: optional plays — parse each play's vars.scope -----------------------
# Discovery mirrors Check 2's existing "is this a playbook" definition
# (top-level "- hosts:" line present); archived plays are exempt for the same
# reason round 2 established (a taxonomy invented after a play was shelved
# does not apply to it retroactively).
while IFS= read -r -d '' yml_file; do
    # CORE plays (playbooks/imports/*.yml, including playbook-main.yml
    # itself) are correctly classified at the import site (4a) and MUST
    # carry no vars.scope per §4.2 — this loop only applies to the
    # OPTIONAL tree. Without this guard, every one of the 31 core play
    # files (each satisfying the "- hosts:" playbook check below) would be
    # flagged "missing vars.scope", unconditionally, from the very first
    # run — round 3's BLOCKER, fixed here.
    [[ "$yml_file" != */optional/* ]] && continue
    [[ "$yml_file" == "$CORE_MAIN" ]] && continue
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR" || continue
    [[ "$yml_file" == */optional/archived/* ]] && continue

    rel_file="${yml_file#"$REPO_ROOT"/}"

    hosts_block_count=$(grep -cE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" || true)
    if [[ $hosts_block_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — file contains $hosts_block_count separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope."
        SCOPE_VIOLATIONS+=("$rel_file (multi-play file: $hosts_block_count plays in one file, not supported)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    # Scan the WHOLE play-level vars: block (2-space "vars:" through the next
    # 2-space top-level key) for a "scope:" entry, wherever it sits among
    # other vars — not just as the first key. A task's OWN vars: block (task
    # attributes sit at 6-space indent) never matches the 2-space anchor, so
    # it can't be confused with the play-level one.
    scope_vals=$(awk '
        /^  vars:[[:space:]]*$/ { in_vars=1; next }
        in_vars && /^    scope:[[:space:]]*/ {
            val = $0
            sub(/^    scope:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)
            sub(/\r$/, "", val)
            if (val != "") { print val }
            next
        }
        in_vars && /^[^[:space:]]/ { in_vars = 0 }
        in_vars && /^  [^ ]/ { in_vars = 0 }
    ' "$yml_file")

    n=$(printf '%s\n' "$scope_vals" | grep -c . || true)
    if [[ $n -eq 0 ]]; then
        echo "  ERROR (scope): $rel_file — missing vars.scope (need exactly one of general|gnome|server)"
        SCOPE_VIOLATIONS+=("$rel_file (missing vars.scope)")
        ERRORS=$((ERRORS + 1))
    elif [[ $n -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — multiple vars.scope entries declared (exactly one required)"
        SCOPE_VIOLATIONS+=("$rel_file (multiple vars.scope entries)")
        ERRORS=$((ERRORS + 1))
    else
        case "$scope_vals" in
            general|gnome|server) : ;;
            *)
                echo "  ERROR (scope): $rel_file — invalid vars.scope value: $scope_vals (must be exactly one of general|gnome|server)"
                SCOPE_VIOLATIONS+=("$rel_file (invalid vars.scope: $scope_vals)")
                ERRORS=$((ERRORS + 1))
                ;;
        esac
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
```

**`set -e` safety, by construction, not by patch.** Round 1's blocker was a
`grep -c` (returns "0" on stdout but exits 1) used directly inside a
`var=$(...)` assignment under `set -euo pipefail`. This design's core-import
loop (4a) never calls `grep -c` at all — it's an `awk` state machine feeding
a `case` statement, and `awk` exits 0 on a normal empty match unless the
script explicitly calls `exit N` (it never does here). The optional-play loop
(4b) *does* use `grep -c` (`n=$(... | grep -c . || true)`), but it inherits
round 2's fix directly: `|| true` guards it from day one, not as an
after-the-fact patch. Both were tested this round against fixtures
reproducing every edge case (valid/invalid/missing/multiple/multi-play-file,
plus a zero-import-lines file) under `set -euo pipefail` — zero crashes, see
the Revision Log.

**JSON-array-building, final `jq -n` call, terse summary, `TOTAL` handling,
and the "zero `qa-all.bash` edits" confirmation are unchanged from round 2**
— reuse those exact blocks verbatim (they operate on `$SCOPE_VIOLATIONS` and
`$ERRORS`, both populated identically in shape by this new Check 4, just from
a different source of violations). See round-2 `PROPOSAL.md` §4 (in this
plan folder's git history) for the verbatim `sc_json_array` / `jq -n` /
terse-summary text — not re-quoted here since not one character of it
changes.

### 4.2 Optional plays: `vars: { scope: ... }`, informational only

Optional plays are never imported from `playbook-main.yml` — there is no
import site for a `when:` gate to attach to. Their classification exists
purely for **QA-completeness and documentation** (so the gate can assert
"every playbook in this repo has been consciously classified," and so
`docs/` can tell a user which optional plays are GUI-only before they run one
on a headless box by hand). It does **not** gate anything by itself.

**Exact form** — a `vars:` entry, never a bare top-level key (confirmed this
round: a top-level `scope:` sibling of `hosts:`/`name:` is a hard Ansible
parse error, `'scope' is not a valid attribute for a Play`):

```yaml
- hosts: desktop
  name: Communication Tools
  become: false
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
    scope: gnome   # general | gnome | server — informational for QA + docs only; this play is opt-in (invoked by path), never auto-gated by provisioning_profile
  tasks:
    ...
```

**A useful emergent property**: because `provisioning_profile` lives in
`group_vars` (inventory-scoped, not attached to `playbook-main.yml`
specifically), it is automatically available to **any** playbook run against
this inventory — including an optional play invoked directly by path. So a
mixed optional play (e.g. `play-container-watch.yml`, §5.4) can use the exact
same task-level `when: provisioning_profile != 'server'` pattern as a core
mixed play, and it will correctly auto-skip its GNOME-only tasks even though
the play itself was never imported through a gated `import_playbook:` line.
The play-level `scope:` var stays purely informational; the task-level
`when:` (where a play needs one) does the actual work, exactly like §5.

---

## 5. The exact mixed-play edits (`when:` replaces task-level `tags:`)

### 5.1 `playbooks/imports/play-basic-configs.yml`

**Before** (existing task, unchanged elsewhere in the file):

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

**After:**

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
      when: provisioning_profile != 'server'
```

`play-basic-configs.yml`'s import in `playbook-main.yml` stays **unguarded**
(§3.2 — it's a `general`-bucket play); this one task carries its own gate
directly, independent of the play's import-site status.

### 5.2 `playbooks/imports/play-prevent-ssh-suspend.yml`

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
      when: provisioning_profile != 'server'
```

Still fixes the same real latent bug (§1.2): without this gate, a headless
run would hit `gsettings`, get "No such schema" (no `gnome-settings-daemon`
on a server), and — with no `failed_when: false` on the task and this repo's
`ansible.cfg` setting `any_errors_fatal = true` — abort the **entire run**,
not just this play.

### 5.3 `playbooks/imports/play-vpn.yml` — task split, then gate the split-off task

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
      when: provisioning_profile != 'server'
```

Same split as round 2 (still required — the existing task bundles a
GNOME-only package with two general ones); only the gate mechanism on the
split-off task changes from `tags: [scope-gnome]` to `when:`.

### 5.4 `playbooks/imports/optional/common/play-container-watch.yml` — 8-task diff, list-form `when:` where a `register`-dependent condition already exists

Same analysis as round 2 (§1.3, §8): the play mixes a general-purpose
watchdog (CLI/systemd, `general`) with a GNOME Shell panel extension block
(comparably sized, not a trivial exception) — the file-split remains the
architecturally right long-term fix and is still deferred (PLAN.md's
non-goals don't require restructuring the optional tree, only classifying
it). This pass applies the interim: play-level `vars: { scope: general }`
(§4.2) plus a `when:` gate on each of the same 8 GNOME-shaped tasks round 2
identified (including the 8th, `Container-watch extension reload complete`,
that the round-1 audit's own enumeration of 7 missed — see round-2
`PROPOSAL.md` §3.4 for the full derivation).

**New wrinkle for the `when:` mechanism, verified this round**: 4 of the 8
tasks already have their own `when:` (a `register`-dependent condition from
an earlier task in the same chain — `extension_enabled.rc == 0`,
`enable_result.rc == 0`, `enable_result.rc != 0`). Ansible's list-form
`when:` is an **AND with short-circuit** — tested this round with a
register/consumer pair gated the same way: when the first list item is
false, the second is never evaluated, so referencing an attribute of a
`register`ed var that a *skipped* producer task never set does **not** raise
an undefined-variable error. This is exactly the shape needed here: put the
profile gate **first** in the list, so on a server the whole chain
short-circuits closed without ever touching the (unset) `register`ed
variable.

**Exact diff** — play-level `vars: { scope: general }` (§4.2) plus, per task:

```yaml
    - name: Ensure extension destination directory exists
      ansible.builtin.file:
        path: "{{ extension_dest }}"
        state: directory
        owner: "{{ user_login }}"
        group: "{{ user_login }}"
        mode: "0755"
      when: provisioning_profile != 'server'

    - name: Deploy container-watch GNOME extension
      ansible.builtin.copy:
        src: "{{ extension_src }}/"
        dest: "{{ extension_dest }}/"
        owner: "{{ user_login }}"
        group: "{{ user_login }}"
        mode: "0644"
        directory_mode: "0755"
      when: provisioning_profile != 'server'

    - name: Check if container-watch extension is currently enabled
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.shell:
        cmd: gnome-extensions list --enabled | grep -q "{{ extension_name }}"
      register: extension_enabled
      changed_when: false
      failed_when: false  # FAIL-FAST-OK: probe — extension may not be enabled yet
      when: provisioning_profile != 'server'

    - name: Disable container-watch extension to force reload
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        cmd: gnome-extensions disable {{ extension_name }}
      when:
        - provisioning_profile != 'server'
        - extension_enabled.rc == 0
      register: disable_result
      changed_when: disable_result.rc == 0
      failed_when: false  # FAIL-FAST-OK: disable may fail if GNOME session is unavailable

    - name: Wait for container-watch extension to unload
      ansible.builtin.pause:
        seconds: 2
      when:
        - provisioning_profile != 'server'
        - extension_enabled.rc == 0

    - name: Enable container-watch extension
      become: true
      become_user: "{{ user_login }}"
      ansible.builtin.command:
        cmd: gnome-extensions enable {{ extension_name }}
      register: enable_result
      changed_when: "'is now enabled' in enable_result.stderr or enable_result.rc == 0"
      failed_when: false  # FAIL-FAST-OK: enable may fail if GNOME session is unavailable
      when: provisioning_profile != 'server'

    - name: Container-watch extension reload complete
      ansible.builtin.debug:
        msg: "Container-watch extension reloaded successfully"
      when:
        - provisioning_profile != 'server'
        - enable_result.rc == 0

    - name: Container-watch extension enable deferred
      ansible.builtin.debug:
        msg: "Container-watch extension will be enabled on next GNOME session start (no active session detected)"
      when:
        - provisioning_profile != 'server'
        - enable_result.rc != 0
```

The 4 tasks that had no pre-existing `when:` (directory/deploy/probe/enable)
get a plain scalar `when: provisioning_profile != 'server'`. The 4 that
already depended on a `register`ed var get a **list**, profile gate first —
NOT a replacement of the existing condition, an addition to it. Getting this
wrong (e.g. scalar-overwriting an existing `when:` instead of listing both)
would silently drop the original guard; getting the *order* wrong (register
condition first) would not short-circuit correctly and could still error.
Both were verified against the real task bodies, not re-derived from memory.

---

## 6. Canonical commands + documentation

**Desktop — the zero-flag default, now genuinely zero-flag** (this is the
whole point of the pivot):

```bash
ansible-playbook playbooks/playbook-main.yml
```

**Server — explicit override, for testing/CI or a human who wants to force
it**:

```bash
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=server
```

**Desktop, explicit override** (symmetry / testing):

```bash
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=desktop
```

**Documentation locations** (same targets as round 2, content rewritten for
the new mechanism):

- **`docs/playbooks.md`** — insert a new `## Desktop vs. Headless Server Provisioning` section immediately before line 17 (`## Core Playbooks (Automatically Run)`), directly after `## Quick Navigation`. Content: the
  three commands above; an explanation that the profile is **auto-detected**
  from `systemctl get-default` (server-biased when uncertain) with no flags
  needed in the common case; the `-e provisioning_profile=...` override for
  testing/CI; a pointer to this plan folder for the full rationale.
- **`CLAUDE/AnsibleStyle.md`** — add a new subsection `### Provisioning Profile Gates (Plan 00061)` after the existing `### Tagging Strategy`
  subsection. Content: the three canonical `when:` forms from §3.1 verbatim,
  the "immediately following line, no comment in between" grammar rule, and
  the optional-play `vars: { scope: ... }` form from §4.2.
- **`docs/playbooks.md`'s existing per-play sections** —
  `### play-basic-configs.yml`, `### play-prevent-ssh-suspend.yml`,
  `### play-vpn.yml` (confirmed present) each get a one-line note about their
  task-level `when:` gate; `play-prevent-ssh-suspend.yml`'s note should
  specifically mention the bug being fixed (§5.2).

---

## 7. Verification procedure (replaces round 2's `--list-tasks` proof)

**`--list-tasks` cannot prove a `when:`-based skip.** Confirmed empirically
both by `prototype-when-import.md` and again this round (§2.2): it lists the
gnome-gated task under every profile, because it enumerates what *could* run
statically without evaluating `when:`. The cheap in-container zero-regression
proof round 2 relied on (`--skip-tags` **is** honoured by `--list-tasks`) has
no equivalent for this mechanism.

**What CAN be verified in the CCY container** (parse-only, matching this
repo's existing "`--syntax-check` is container-safe because it does not
execute tasks" precedent):

```bash
export ANSIBLE_CONFIG="$(git rev-parse --show-toplevel)/ansible.cfg"
ansible-playbook playbooks/playbook-main.yml --syntax-check
```

...plus running the QA gate itself (§4.1), which statically confirms every
import carries a recognised gate shape — this is the in-container
correctness check for this mechanism; it validates the *declarations*, not
the runtime *behaviour*.

**What requires a real run (host-side, per `CLAUDE/ContainerRules.md` — never
in the CCY container)**: proving a gnome play is actually skipped on a
detected/forced server profile. Two acceptable procedures, either is
sufficient — this replaces round 2's §6 entirely, it is not an addition to
it:

1. **A throwaway VM/container**, matching `prototype-when-import.md`'s own
   validation method: install a minimal Fedora, confirm `systemctl get-default` reports `multi-user.target`, run
   `ansible-playbook playbooks/playbook-main.yml` with **no** `-e` flag, and
   confirm the recap shows every `gnome`-bucket play (and the container-watch
   task-level overrides, if that optional play is separately exercised) as
   `skipping`, with every `general`-bucket play still `ok`.
2. **A real desktop host with the override**, faster to set up: on any host
   this repo already provisions,
   `ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=server --check`
   — `--check` is legitimate **here** because it's the real host being
   dry-run, not the CCY container (the container-unsafety of `--check` was
   specifically about running it against `localhost` *inside* the container,
   which lacks the real box's systemd/user state per `ContainerRules.md`'s
   "What CCY Container IS NOT" list — that concern does not apply on the
   actual target). Confirm the same skip/ok split in the recap.

Either procedure is PLAN.md Task 3.8 territory (host validation) — tracked
there, not duplicated here.

---

## 8. Implementation checklist (execute in order)

01. **Add `environment/localhost/group_vars/desktop.yml`** exactly as shown
    in §2.1 (new file).
02. **Add the third `assert` task to `play-AA-preflight-sanity.yml`** exactly
    as shown in §2.3.
03. **Edit `playbook-main.yml`** to the exact "after" state in §3.2 (10
    `when:` lines added, nothing else changes).
04. **Apply the 3 core mixed-play edits (§5.1–§5.3)** to
    `play-basic-configs.yml`, `play-prevent-ssh-suspend.yml`, and
    `play-vpn.yml` exactly as shown.
05. **Add `vars: { scope: ... }`** to all 41 non-archived optional plays
    using round 2's fast-pass table (§1.3 — same table, values now go in
    `vars:` instead of a `tags:` block), **with the same two named
    exceptions handled separately**:
    - `play-container-watch.yml` — apply the exact 8-task `when:` diff in
      §5.4 verbatim, plus `vars: { scope: general }`.
    - `play-virtualbox-windows.yml` — still must be **split first** (two
      `- hosts:` plays in one file — Check 4 hard-rejects it, same finding
      as round 2, same required split into
      `play-virtualbox-windows.yml` + `play-virtualbox-windows-vm-setup.yml`
      (exact file boundaries: §1.3's table above), then each half gets its
      own `vars: { scope: ... }` — get an explicit owner decision on both
      values first (same Low-confidence category as `rpm-fusion`), don't
      guess.
    - For every other Medium/Low-confidence row, read the play's actual task
      list first and correct the classification if the fast-pass guess was
      wrong.
06. **Edit `scripts/qa-ansible.bash`** per §4.1: insert Check 4 (both 4a and
    4b sub-loops) after Check 3, reuse round 2's JSON-array-building /
    `jq -n` / terse-summary blocks verbatim (unchanged shape, just fed by
    this round's `$SCOPE_VIOLATIONS` source). **Do not touch `qa-all.bash`.**
07. **Run `./scripts/qa-all.bash`.** Expect it to fail before steps 1–5 land
    (every core import unguarded-but-should-be-gated reads as fine —
    unguarded IS valid for general — but every optional play is missing
    `vars.scope`, and `play-virtualbox-windows.yml` is rejected as
    multi-play) and pass once every playbook (except the one archived play)
    carries a recognised declaration.
08. **Add the `docs/playbooks.md` section, the `CLAUDE/AnsibleStyle.md`
    subsection, and update the three existing per-play doc sections** per §6.
09. **Run `--syntax-check`** (§7) inside the CCY container as the static
    sanity gate; confirm clean.
10. **Re-run `./scripts/qa-all.bash`** one final time to confirm green.
11. **(On HOST, never in the CCY container)** run one of §7's two real-run
    verification procedures; confirm the skip/ok recap split matches
    expectations. This is PLAN.md Task 3.8.
12. **Update PLAN.md**: mark Phase 3 tasks complete in the same commit as the
    code (Plan Commit Rule), add a `JOURNAL/` entry, and record that Decision
    4's mechanism is now implemented (not just decided).

---

## 9. Known limitations / failure modes consciously accepted

- **A `when:` line at the wrong indent depth is still invisible to Check 4's
  4a parser** (round 4, residual gap after fixing the orphaned-`when:` bug —
  Fable's own severity call: "worth one comment line, not a separate fix").
  Both the import rule and the `when:` rule anchor on an exact indent (`^- import_playbook: ` and `^  when: ` respectively); a `when:` written at,
  say, 4-space indent instead of 2 matches neither rule, so it's silently
  invisible rather than caught as ORPHANED or anything else — the import it
  was meant to gate would resolve as `general`/UNGUARDED with no error. This
  is a narrower version of the same "format-sensitive, not a general parser"
  tradeoff already accepted throughout `qa-ansible.bash` (round 2 accepted
  the identical shape of gap for inline-array `tags:` form); the fix, if it
  ever bites, is "match `CLAUDE/AnsibleStyle.md`'s documented 2-space
  grammar," not "add a YAML parser."
- **No cheap in-container end-to-end skip proof.** Round 2 had one
  (`--skip-tags` honoured by `--list-tasks`); this mechanism doesn't (§7).
  The QA gate (§4.1) proves the *declarations* are well-formed; only a real
  run proves the *behaviour*. Accepted per Decision 4 — the owner's
  requirement (true zero-flag auto-detection) outweighs this loss, and
  PLAN.md Task 3.8 already required host validation regardless of mechanism.
- **`connection: local` is a load-bearing, undocumented-until-now
  assumption.** If this repo were ever pointed at a remote host, the `pipe`
  lookup in `group_vars/desktop.yml` would read the **controller's**
  `systemctl get-default`, not the target's — silently wrong, not a crash.
  This repo has never done that (every play is `hosts: desktop` /
  `connection: local` by design, `CLAUDE/AnsibleStyle.md`), so this is a
  named, accepted assumption rather than a live risk — but it is now
  explicitly documented (§2.1's file header) where it previously wasn't
  documented anywhere at all.
- **A mis-detected profile degrades differently in each direction, by
  design.** Desktop mis-detected as server: GUI plays silently skipped,
  recoverable with `-e provisioning_profile=desktop`. Server mis-detected as
  desktop: GUI plays attempt to run against a box with no GNOME session —
  most either no-op cleanly (e.g. `play-toolbox-install.yml`'s own
  `has_display` guard) or fail loudly in ways that are individually
  debuggable, but this is a materially worse failure mode than the reverse,
  which is exactly why detection is server-biased-when-uncertain (§2.1) —
  the asymmetry is deliberate, not overlooked.
- **The QA gate validates declaration shape, not runtime correctness.**
  Exactly the round-2 limitation, restated for the new mechanism: Check 4
  confirms every import/optional-play has a recognised gate, not that the
  gate is semantically the *right* one for what the play actually does, nor
  that a *future* task added to a `general`-bucket play doesn't quietly need
  a GUI. Same review-discipline gap as before; no static gate closes it
  without executing the play headless in CI (out of scope).
- **The fast-pass optional-tree classification is unchanged from round 2,
  same confidence caveats apply** — Medium/Low rows still need a real read
  before their value is trusted (§1.3, checklist step 5).
- **`play-container-watch.yml`'s file-split remains deferred**, same
  reasoning as round 2 — the interim `when:`-list diff (§5.4) is more
  correct than round 2's tag diff (the short-circuit ordering is now
  explicit and tested) but is still, structurally, a play carrying more
  interdependent conditional logic than the "trivial exception" pattern was
  designed for.
- **`play-virtualbox-windows.yml`'s scope value(s) remain an open owner
  decision**, same category as `rpm-fusion` — unchanged from round 2, now
  also blocking on the mandatory file-split before either half can even be
  tagged.
- **The `gnome` bucket means "needs a GUI session," not literally
  "GNOME-specific"** — unchanged substance from round 2's equivalent note,
  restated for the new value names. `play-lxde-install.yml` (an LXDE
  desktop, not GNOME) is still classified `gnome` because that's the bucket
  the taxonomy provides for GUI-dependent content; the owner's Decision 1/2
  locked the three-bucket taxonomy and this plan does not reopen the naming.
- **The `scope-gnome`-style naming convention is gone; bucket names are now
  plain `general`/`gnome`/`server`** (no more literal `scope-` prefix,
  since there's no tag string to prefix) — this is a terminology
  simplification from the pivot, not a new limitation, noted here so a
  reader comparing this document against round 2's doesn't mistake it for an
  inconsistency.
