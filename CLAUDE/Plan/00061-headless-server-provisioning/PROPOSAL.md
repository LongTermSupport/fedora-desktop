# Proposal: Headless Server Provisioning — Self-Guarding Plays (standalone-runnable, auto-gating)

## Revision log (round 6 — final; one Check 4 fix from `AUDIT-round-5.md`)

`AUDIT-round-5.md` (Fable): **0 blockers, 1 should-fix**, and it verified the
round's highest-risk claim — the canonical guard passes its own gate
byte-for-byte. The one should-fix (applied here, judge-verified):
`TASK2_META`/`TASK2_WHEN` extraction did not strip a trailing `# comment`/CRLF
(unlike `SCOPE` extraction, which does), so an otherwise byte-correct guard
carrying a trailing comment on its `when:` was falsely rejected. Fixed by
mirroring `SCOPE`'s two `sub()` calls onto both guard-field rules (§5.2).
Re-verified independently: the canonical guard still matches and a
comment-suffixed guard now matches too. **Design converged — implementable
as-is.**

## Revision log (round 5 — gate moves into each play)

**Why**: a new owner requirement, recorded as PLAN.md Decision 5: **every
play must be runnable standalone**
(`ansible-playbook playbooks/imports/play-X.yml`), not only via the
`playbook-main.yml` batch — and still auto-gate to the detected profile.
Decision 4's `when:`-on-`import_playbook:` gate lives in `playbook-main.yml`,
so it does not fire on a standalone run — a `gnome`-bucket core play run by
itself would execute unconditionally on a server. The "core plays are
batch-only" assumption round 3/4 built on is void.

**Reused verbatim** (mechanism-independent, unchanged from round 3/4): the
entire §1 exhaustive classification (21 general / 10 gnome / 0 server), all
three core mixed-play discoveries and the `gsettings` latent-bug finding, the
`play-container-watch.yml` register/`when:` hazard (8 tasks), the
`play-virtualbox-windows.yml` two-`hosts:`-plays-in-one-file finding, **and
the entire §2 `group_vars/desktop.yml` detection layer** — re-verified this
round that it also loads correctly on a bare standalone single-play run (no
batch context needed), so nothing about it changes. **What changes is only
where the gate lives**: out of `playbook-main.yml`'s import lines, into each
play itself.

**Every load-bearing claim below was prototyped against
`ansible-core 2.19.11` in this container before being written down** —
continuing the empirical discipline `AUDIT-round-1/2/3.md` established:

- **The guard expression, all 6 cases (3 scopes × 2 profiles), run
  standalone**: built the exact canonical guard —
  `ansible.builtin.meta: end_play` /
  `when: (scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')`
  — into a real play and ran it with `TEST_SCOPE` × `-e provisioning_profile`
  covering all 6 combinations. Every case matched the logical prediction
  exactly: `general` runs under both profiles; `gnome` runs on desktop, ends
  on server; `server` ends on desktop, runs on server. `--syntax-check` on
  the exact YAML (parentheses, `and`/`or`, single-quoted string literals, no
  colons) parsed clean — no 2.19 splitter complaint.
- **Batch behaviour re-verified with `when:` dropped from the imports**: a
  2-play `main.yml` (`import_playbook:` lines with **no** `when:` at all) run
  with `-e provisioning_profile=server` showed the general play's real task
  executing and the gnome play's `PLAY [...]` header appearing with **no**
  real-task output — the play guarded itself. The default (auto-detect,
  desktop) run showed both real tasks executing. Confirms self-guarding
  plays compose correctly in a batch with no import-site gate at all.
- **The standalone-typo gap is real and is now closed differently than §2.3
  alone could close it**: with a typo'd `-e provisioning_profile=srever`, the
  single-condition guard **silently ran a gnome play** (`srever != 'server'`
  is true, so the `!=` clause never fires) — reproduced the exact hazard
  Decision 5 flagged. Fixed by making the guard **two tasks**: an `assert`
  that the resolved value is exactly `desktop` or `server` (hard-fails
  loudly on anything else, mirroring §2.3's own assert), followed by the
  `meta: end_play` skip check. Re-tested: a valid override now cleanly ends
  the play (no assert failure); the same typo now hard-fails immediately
  with a clear message, both **on a standalone run**, closing the gap §2.3
  alone left open.
- **The uniform per-play QA gate, run against a 6-fixture set covering every
  branch** (gnome play with correct 2-task guard, gnome play missing the
  guard entirely, general play correctly guard-free, general play with an
  *unnecessary* guard, missing `vars.scope`, invalid `vars.scope` value): the
  exact combined check produced the correct verdict on every fixture, using
  **zero `grep -c` calls in its main per-play loop** (a further `set -e`
  safety improvement over round 3/4's design, not just a parity match — see
  §5).

**Decided** (see §3 for full reasoning): `end_play` (not `end_host` — this
repo is always single-host, but `end_play`'s "this whole play doesn't apply"
reading stays correct even in a hypothetical multi-host future, where
`end_host`'s "let other hosts continue" framing would be misleading).
**General plays do NOT carry the guard** — only `gnome`/`server`-scoped plays
do; `vars: { scope: ... }` is still universal (every play, every scope). The
canonical guard **text** is uniform (one string, reused verbatim wherever a
guard is needed, driven by the play's own `scope` var — not three different
hardcoded per-scope strings) — this is what "uniform boilerplate" means here,
not "present in every file regardless of scope."

**Superseded from round 3/4** (mechanism-specific, no longer applicable): the
import-site `when:` on `playbook-main.yml`'s `import_playbook:` lines (now
bare again); the split 4a (import-parser) / 4b (`vars.scope`-only) Check 4
design (now one uniform per-play check, since core and optional plays are
now classified identically); the "core plays carry no `vars.scope`" rule
(now every play does, core and optional alike).

---

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
  loop — core plays never even reach the rest of the checks now. (Note:
  round 5 makes this fix moot — every play now carries `vars.scope`, core and
  optional alike, so the exclusion this fixed is removed again in round 5's
  rewrite, not because the fix was wrong, but because the rule it protected
  no longer exists.)
- **SHOULD-FIX (fixed)**: 4a's `when:` rule was gated on `pending != ""`, so
  a comment or blank line between an `import_playbook:` and its `when:` let
  the catch-all rule flush `pending` first and silently swallow the orphaned
  `when:` line — zero error reported. Fixed by making the `when:` rule match
  unconditionally and branch on whether `pending` is already empty. (Note:
  round 5 also makes this specific fix moot — there is no more import-site
  `when:` to parse — but the underlying lesson, verify a parser against the
  actual malformed input it's meant to catch, carried forward into round 5's
  QA gate testing.)
- Both fixes were tested as the exact text in the document at the time;
  zero crashes throughout. Full detail preserved here for the historical
  record even though round 5 supersedes the code these fixes touched.

---

## Revision log (round 3 — mechanism pivot to `when:`)

**Why**: PLAN.md Decision 4 (owner, after round 2 converged with zero
findings): the system must **auto-detect** desktop vs. server with **zero
config, zero runtime flags**. `--skip-tags` is resolved by the CLI before any
fact exists, so a tag-based design can never self-configure. `when:` is
evaluated at runtime against variables, so it *can* consume an
auto-detected `provisioning_profile` and gate itself with no flag. The
mechanism pivots; the underlying analysis does not.

**Reused verbatim from round 2**: the entire exhaustive classification of all
31 core plays, the fast-pass classification of the 41 optional plays, all
three mixed-play discoveries, the `play-container-watch.yml` register/`when:`
hazard, the `play-virtualbox-windows.yml` two-`hosts:`-plays finding.

**New in round 3, empirically verified**: the `group_vars`-based detection
layer (§2, still in force — round 5 reuses it verbatim); the `set -e`-safe
Check 4 rewrite (superseded by round 5's uniform per-play design); Ansible's
list-form `when:` short-circuit behaviour (still relevant, used again in
round 5's container-watch treatment, unchanged).

**Superseded from round 2**: the play-level `tags:` canonical form, the
tag-based Check 4, the `--skip-tags`-based canonical commands, the
`--list-tasks` zero-regression proof (confirmed `--list-tasks` does not
evaluate `when:`, so it can't prove a skip — this limitation persists
unchanged through round 5, see §8).

*(Historical detail preserved above each round's own log; round 5 is
authoritative for the current mechanism.)*

---

**Status of this document**: implementation-ready design for Plan 00061
Phase 3, per PLAN.md Decision 5 (self-guard, standalone-runnable — supersedes
Decision 4's import-site `when:`). Grounded in `prototype-self-guard.md` (the
owner-commissioned prototype establishing guard viability) plus this round's
own additional empirical verification (above). Where this document and any
earlier round disagree on mechanism, this document wins; where they agree on
classification or the detection layer, this document is the same analysis,
reused.

---

## 1. Exhaustive per-play classification (unchanged from round 2/3/4)

### 1.1 Core plays (all 31)

Classification reflects **what the play's tasks actually do**, not filename
or folder (the same discipline the owner applied to `play-rpm-fusion.yml`).
"General" means: no task requires a GNOME session, a display server, or a
GUI application to be meaningful. The bucket names (`general`/`gnome`/
`server`) are plain values, consumed by the `scope` var in every play (§3).

| #   | Play                               | Bucket                                                          | One-line justification                                                                                                                               |
| --- | ---------------------------------- | --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `play-AA-preflight-sanity.yml`     | `general`                                                       | Ansible-version + Fedora-version assertions only                                                                                                     |
| 2   | `play-AB-dnf-upgrade.yml`          | `general`                                                       | Package upgrade + kernel half-install cleanup, no GUI                                                                                                |
| 3   | `play-basic-configs.yml`           | `general` (+ 1 task-level `when:` override — §6.1)              | vim colours, sudo, PS1, SSH helper scripts, `yq`, GRUB, `fwupd` — all general; one task (USB audio fix) is the exception                             |
| 4   | `play-prevent-ssh-suspend.yml`     | `general` (+ 1 task-level `when:` override — §6.2)              | `ssh-suspend-guard` systemd service is general; one task calls `gsettings set org.gnome.settings-daemon...` — GNOME-only                             |
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
| 29  | `play-vpn.yml`                     | `general` (+ 1 task **split** with a `when:`-gated task — §6.3) | WireGuard/OpenVPN CLI tools + firewalld rule are general; `NetworkManager-openvpn-gnome` (bundled in the same `dnf` task today) is GNOME-applet-only |
| 30  | `play-gsettings.yml`               | `gnome`                                                         | Caps Lock remap + Ptyxis terminal tab setting via `dconf` — GNOME desktop settings                                                                   |
| 31  | `play-ZZ-repo-cleanup.yml`         | `general`                                                       | Removes orphaned COPRs, no GUI content                                                                                                               |

**Tally: 21 `general`, 10 `gnome`, 0 `server`.** Under round 5's design, all
31 now carry `vars: { scope: ... }` (§3), and the 10 `gnome` rows each also
carry the 2-task guard (§3) — this is the headline edit-surface change from
round 3/4, where only optional plays carried `vars.scope` and core plays'
classification lived solely at the import site.

### 1.2 Mixed-concern task catalogue (unchanged)

| Play                           | Exact task                                                      | Package/setting                                                                        | Why it's the exception                                                                                                                                     |
| ------------------------------ | --------------------------------------------------------------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-basic-configs.yml`       | `Deploy USB audio fix script`                                   | `files/home/bashrc-includes/usb-audio-fix.bash` bashrc-include                         | Desktop-audio-hardware concern, no headless-server consumer                                                                                                |
| `play-prevent-ssh-suspend.yml` | `Disable suspend on AC power (plugged in = never idle-suspend)` | `gsettings set org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type nothing` | GNOME Settings Daemon schema over D-Bus — **hard-fails** ("No such schema") on a server without `gnome-settings-daemon`; a real latent bug the split fixes |
| `play-vpn.yml`                 | `Install VPN Packages` (one `dnf` task, 3 packages)             | `NetworkManager-openvpn-gnome`                                                         | GNOME NetworkManager-applet integration; the other two packages are pure CLI                                                                               |

All three are trivial, single-item exceptions; zero core plays need a
file-split. See §6 for the exact edits (unchanged from round 3/4 — these are
task-level `when:` overrides inside otherwise-`general` plays, orthogonal to
the play-level guard mechanism §3 introduces).

### 1.3 Optional plays (fast-pass — unchanged)

The 41-file fast-pass classification (30 in `optional/common/`, 4 in
`optional/experimental/`, 7 in `optional/hardware-specific/`) is unchanged
from round 3/4 — same plays, same confidence markers, same notes, same two
flagged files (`play-container-watch.yml` — mixed, task-level fix in §6.4;
`play-virtualbox-windows.yml` — two `- hosts:` plays in one file, must be
split before it can be classified at all). **The headline change**: under
round 3/4, optional plays' `vars.scope` was purely informational; under round
5, a `gnome`/`server`-bucket optional play now also carries the real,
behaviour-gating 2-task guard (§3) — since `provisioning_profile` and the
guard mechanism are both inventory-scoped, not `playbook-main.yml`-scoped,
they apply identically to every play regardless of where it lives.

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
| `play-container-watch.yml`            | **MIXED — §6.4**                            | High                | Deploys a general-purpose watcher daemon AND a GNOME Shell panel extension in one file; not a trivial exception                                                                                |
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

| Play                                 | Bucket                                                                | Confidence               | Note                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ------------------------------------ | --------------------------------------------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-docker-in-lxc-support.yml`     | `general`                                                             | High                     | Container interop config                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| `play-docker-overlay2-migration.yml` | `general`                                                             | High                     | Docker storage-driver migration                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| `play-lxde-install.yml`              | `gnome`                                                               | High                     | Installs the LXDE **desktop environment** — not literally GNOME, but falls in the "needs a GUI session" bucket (see §10 naming note)                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| `play-virtualbox-windows.yml`        | **file has TWO `- hosts:` plays — MUST be split first (§5, §9 step)** | **Low — flag for owner** | "Install Virtualbox" (driver + packages + group membership — arguably headless-capable via `VBoxHeadless`/`VBoxManage`, same category of call as the rpm-fusion dispute) at line 3; a structurally separate "Setup Windows VMs" play (downloads/imports a specific Windows 11 VM image, more plausibly GUI-workflow-coupled) at line 44. Split into `play-virtualbox-windows.yml` (lines 1–43 + `handlers:`) and `play-virtualbox-windows-vm-setup.yml` (line 44 onward), each with its own shebang + exec bit, then get an explicit owner decision on both `scope` values |

**`playbooks/imports/optional/hardware-specific/` (7 files):**

| Play                                   | Bucket    | Confidence | Note                                                                                                                                                                 |
| -------------------------------------- | --------- | ---------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-darktable-ai-gpu.yml`            | `gnome`   | High       | GPU backend for the GUI darktable app                                                                                                                                |
| `play-displaylink.yml`                 | `gnome`   | High       | Extends physical monitors via a GNOME/mutter multi-monitor session                                                                                                   |
| `play-ipu6-webcam.yml`                 | `gnome`   | High       | Webcam driver stack; only consumer is GUI video-calling apps, no headless use case in this repo                                                                      |
| `play-laptop-lid-power-management.yml` | `general` | High       | ACPI/systemd lid-close behaviour, no GUI dependency (irrelevant to rack servers as *hardware*, but that's an inventory question, not a GUI-dependency one — see §10) |
| `play-laptop-thermal-diagnostics.yml`  | `general` | High       | CLI thermal diagnostics                                                                                                                                              |
| `play-musiccast.yml`                   | `gnome`   | High       | Play name explicitly says "SSDP diagnostics + gyrc **GUI**"                                                                                                          |
| `play-nvidia.yml`                      | `general` | High       | NVIDIA driver install — needed for both desktop GPU rendering *and* headless CUDA/compute servers; not GNOME-coupled                                                 |

**`playbooks/imports/optional/archived/` (1 file) and `untested/` (0 files):
exempt from classification per §5** (archived) or **not yet applicable**
(untested is currently empty).

---

## 2. Auto-detected `provisioning_profile` — the detection layer (unchanged from round 3/4)

### 2.1 Design: `group_vars`, not a preflight `set_fact`

**`environment/localhost/group_vars/desktop.yml`** (new file, unchanged from
round 3/4):

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
`CLAUDE/AgentNotes.md`.

**Rejected: a preflight-play `set_fact`.** `group_vars` is loaded before
**any** play runs, on **every** invocation shape — batch (`playbook-main.yml`)
and standalone (`ansible-playbook playbooks/imports/play-X.yml`) alike, since
group_vars are inventory-scoped, not playbook-scoped. A `set_fact` in a
preflight play would only be visible to plays that run *after* it in the same
batch — useless for standalone runs, which is exactly the capability round 5
requires. This is a stronger reason to reject `set_fact` than round 3/4 had:
it isn't just "less available," it's flatly incompatible with the
standalone-runnable requirement.

### 2.2 Empirical verification (unchanged from round 3/4, re-confirmed this round for standalone)

| Command                                               | Marker created (lookup ran)?   | Behaviour                                                        |
| ----------------------------------------------------- | ------------------------------ | ---------------------------------------------------------------- |
| `--syntax-check`                                      | No                             | n/a (parse-only)                                                 |
| `--list-tasks`                                        | No                             | lists gated tasks under every profile (doesn't evaluate `when:`) |
| standalone real run, no `-e`                          | **Yes**                        | profile auto-computed, correct                                   |
| standalone real run, `-e provisioning_profile=server` | **No** (extra-vars precedence) | override honoured, lookup skipped                                |

Round 5 re-ran the standalone rows specifically (round 3/4 tested via a
2-play batch `main.yml`; round 5 tested a single play run entirely on its
own, `ansible-playbook playbooks/imports/play-X.yml` with no `playbook-main.yml` in the invocation at all) — identical results. `group_vars`
loading has no dependency on batch context.

### 2.3 Fail-fast guard against a typo'd override — batch-run coverage (standalone coverage now lives in the guard itself, §3)

Unchanged from round 3/4: add a third assertion to
`playbooks/imports/play-AA-preflight-sanity.yml`:

```yaml
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

**Why this is still needed even though §3's guard also validates**: this
assertion is the very first task of the very first play in a **batch** run,
so a typo'd override is caught before any of the 21 `general`-bucket plays
(10 of which precede the first `gnome` play in import order) do real work.
Without it, a batch run with a bad override would run 10 general plays' worth
of real changes before the first `gnome` play's own guard (§3) ever gets a
chance to catch the typo. The two checks cover two different "as early as
possible" properties — batch-run earliness (this one) and standalone-run
coverage (§3's guard, which is the *only* thing that runs at all when a
`gnome`/`server` play is invoked on its own) — and are deliberately not
deduplicated into one shared location, because no single location can be
"first" in both invocation shapes simultaneously.

---

## 3. The self-guard mechanism (new in round 5)

### 3.1 The guard expression — prototyped for all 6 cases

**Canonical form**, added as the first task(s) of every `gnome`- or
`server`-scoped play (general plays carry no guard — see §3.3):

```yaml
    - name: Scope guard — assert provisioning_profile is recognised
      ansible.builtin.assert:
        that:
          - provisioning_profile in ['desktop', 'server']
        fail_msg: |
          provisioning_profile={{ provisioning_profile }} is not recognised.
          Valid values: desktop, server.
          Auto-detected from `systemctl get-default`
          (see environment/localhost/group_vars/desktop.yml) or overridden
          via -e provisioning_profile=desktop|server.

    - name: Scope guard — end play if provisioning_profile does not match declared scope
      ansible.builtin.meta: end_play
      when: (scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')
```

The `when:` expression is **one uniform string**, identical wherever it
appears — it is not three different hardcoded conditions for the three
scopes, it is a single boolean driven by the play's own `scope` var (§3.3),
so the guard task's YAML is byte-identical in every `gnome`/`server` play in
the repo; only the play's `vars: { scope: ... }` line differs.

**Prototyped against `ansible-core 2.19.11`, all 6 (scope × profile)
combinations, run standalone** (not via a batch — this is the capability
being added):

| `scope`   | `provisioning_profile` | Guard fires? | Real task runs? |
| --------- | ---------------------- | ------------ | --------------- |
| `general` | `desktop`              | No           | Yes             |
| `general` | `server`               | No           | Yes             |
| `gnome`   | `desktop`              | No           | Yes             |
| `gnome`   | `server`               | **Yes**      | No              |
| `server`  | `desktop`              | **Yes**      | No              |
| `server`  | `server`               | No           | Yes             |

All 6 rows matched the logical prediction exactly (worked through by hand
before prototyping, then confirmed empirically — not just asserted).
`--syntax-check` on the exact YAML (parentheses, `and`/`or`, single-quoted
literals, no colons in the expression itself) parsed clean under 2.19 — the
expression deliberately avoids every quirk `CLAUDE/AgentNotes.md` documents
for the 2.19 free-form parser (that parser applies to `shell:`/`command:`
content, not `when:`, but keeping the expression this simple sidesteps the
question entirely).

### 3.2 `end_play` vs. `end_host` — `end_play` chosen

`prototype-self-guard.md` confirmed both `meta: end_play` and
`meta: end_host` honour `when:` identically under 2.19, and this repo is
always single-host (`hosts: desktop` → `localhost` only, `connection: local`,
by design). The practical behaviour is identical today either way. `end_play`
is chosen because it's the semantically honest reading — "this whole play
does not apply to the current profile," full stop — versus `end_host`'s
framing of "stop processing *this host*, other hosts continue," which implies
a multi-host context this repo structurally doesn't have and has no plan to
grow into. `end_play`'s correctness doesn't depend on host count; `end_host`'s
intended meaning would become actively misleading if this repo ever did add a
second host to the `desktop` group (not planned, but `end_play` costs nothing
today and stays correct either way — a small, free forward-compatibility win).

### 3.3 Whether general plays carry the guard — decided: **no**

Two-task guard is added **only** to `gnome`- and `server`-scoped plays.
`vars: { scope: ... }` is universal (every play, every scope — §4/§5), but
the guard boilerplate is not.

**Why not universal-including-general** (the alternative strongly hinted at
in the brief): a guard that is always-false for `general` plays is genuinely
harmless at runtime (negligible Jinja-eval cost, confirmed by the round-5
prototype's `general`/`desktop` and `general`/`server` rows both cleanly
skipping the guard and running the real task) — so the *runtime* argument is
a wash. The real tradeoff is **reading cost vs. reclassification cost**:

- Universal guard: every future reader of any of the ~40+ `general`-bucket
  plays across this repo has to mentally file past two boilerplate tasks that
  can never fire, forever. Reclassifying a play from `general` to `gnome`
  becomes a one-line `scope:` edit with zero additional steps (the guard is
  already there, already correct).
- Guard-only-where-needed (chosen): `general` plays stay lean — their first
  task is their first *real* task, matching this repo's existing aesthetic of
  no dead code. Reclassifying `general` → `gnome` requires adding the 2-task
  guard block as a **second**, deliberate edit — but that edit is not a
  silent risk: the QA gate (§5) requires the guard for any `gnome`/`server`
  play and will hard-fail the moment `scope:` changes without it, so a
  forgotten guard is caught immediately, not silently.

Given the QA gate closes the "forgot to add it on reclassification" risk
completely, the ongoing reading-cost argument wins: reclassification is rare
(once per play, if ever); reading a play's task list happens on every future
touch. The gate goes further than "only require the guard for gnome/server" —
it also **rejects a guard present on a `general`-scope play** (§5), so the
"lean general plays" invariant is actively enforced, not just a convention
that could silently rot if someone over-cautiously copy-pasted a guard onto
a general play "just in case."

### 3.4 Closing the standalone-typo gap

The single-task guard (`meta: end_play` alone) has the same hazard §2.3
exists to prevent, but on a **standalone** run there is no play-AA to catch
it: prototyped `-e provisioning_profile=srever` (typo) against a single-task
guard and confirmed the gnome play **silently ran** — `srever != 'server'`
evaluates true, so the guard's `!=` clause never fires. This is exactly
Decision 5's flagged gap.

**Fix, prototyped**: make the guard **two tasks** — the `assert` shown in
§3.1, immediately followed by the `meta: end_play` check. Re-tested against
the typo:

```
$ ansible-playbook playbooks/imports/play-gnome2.yml -e provisioning_profile=srever
[ERROR]: Task failed: Action failed: provisioning_profile=srever is not recognised.
fatal: [localhost]: FAILED! => {"msg": "provisioning_profile=srever is not recognised.\nValid values: desktop, server.\n"}
```

Hard, loud, immediate failure — on a **standalone** run, which is exactly the
invocation shape §2.3 alone cannot protect. A valid override (`server`)
against the same two-task guard cleanly ends the play with no assert failure,
confirmed in the same prototype run. This assert is intentionally the exact
wording as §2.3's — same message, same remediation, so a user sees consistent
guidance regardless of which check happened to catch their typo.

---

## 4. `playbook-main.yml` reverts to bare imports

Since every `gnome`/`server` play now self-guards, the import-site `when:`
round 3/4 added is no longer needed **and must be removed** — leaving it in
place would double-gate (harmlessly redundant at best, but dead weight and a
second thing to keep in sync with the play's own `scope:` at worst; the QA
gate, §5, no longer has any concept of an import-site gate to validate, so a
leftover `when:` there would simply be inert, unvalidated cruft).

**Exact diff** — every one of the 10 `when:` lines round 3/4 added is
removed; the import list returns to exactly its original, pre-Plan-00061
shape (comments unchanged):

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
- import_playbook: imports/play-github-cli-multi.yml
- import_playbook: imports/play-ms-fonts.yml
- import_playbook: imports/play-rpm-fusion.yml
- import_playbook: imports/play-browsers.yml
- import_playbook: imports/play-toolbox-install.yml
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
- import_playbook: imports/play-gnome-shell.yml
- import_playbook: imports/play-gnome-shell-extensions.yml
- import_playbook: imports/play-markless.yml
- import_playbook: imports/play-terminal-emulators.yml
- import_playbook: imports/play-vscode.yml
- import_playbook: imports/play-vpn.yml
- import_playbook: imports/play-gsettings.yml
- import_playbook: imports/play-ZZ-repo-cleanup.yml
```

**Re-verified the batch composes correctly with no import-site gate at all**:
built a 2-play `main.yml` (bare `import_playbook:` lines, no `when:`) — one
`general`-scoped play, one `gnome`-scoped play each carrying its own guard —
and ran it both ways:

- `-e provisioning_profile=server`: general play's real task ran; gnome
  play's `PLAY [...]` header appeared with **no** real-task output (guarded
  itself closed).
- No `-e` (auto-detect, this container resolves to `desktop`): both real
  tasks ran.

This directly confirms self-guarding plays compose correctly in a batch
context with zero import-site involvement — the ordering-rationale comments
(LXC/Docker, claude-yolo/claude-code) are completely untouched by this
change, since they were never coupled to the `when:` lines being removed.

---

## 5. The uniform QA gate — Check 4 rewrite (one check, not two)

### 5.1 Design

Round 3/4's Check 4 was split into 4a (parse `playbook-main.yml`'s import +
`when:` lines) and 4b (parse each optional play's `vars.scope`), because core
and optional plays were classified two different ways. Round 5 collapses this
to **one uniform per-play check**, because core and optional plays are now
classified identically: every play file under `playbooks/` (core and
optional, minus archived) must

1. declare exactly one `vars: { scope: general|gnome|server }`, and
2. if `scope` is `gnome` or `server`, carry the exact canonical 2-task guard
   (§3.1) as its **first two tasks**; if `scope` is `general`, the guard must
   **not** be present (§3.3).

Insert as Check 4 in `scripts/qa-ansible.bash`, same slot as every prior
round (after Check 3's closing `done < "$TMP_MATCHES"`, before "Build JSON
output"). **Zero changes to `qa-all.bash`** — unchanged reasoning from every
prior round.

### 5.2 Exact script

```bash
# ---------------------------------------------------------------------------
# Check 4: provisioning-profile scope + guard declaration (Plan 00061, round 5)
# ---------------------------------------------------------------------------
# Every PLAYBOOK (core AND optional, any file with a top-level "- hosts:"
# line, minus imports/optional/archived/) must:
#   1. declare EXACTLY ONE vars.scope in general|gnome|server, and
#   2. if scope is gnome or server, carry the exact 2-task canonical guard
#      (see CLAUDE/AnsibleStyle.md "Provisioning Profile Self-Guard") as its
#      FIRST TWO tasks; if scope is general, the guard must be ABSENT (a
#      general play never needs it — see PROPOSAL.md §3.3 for why this is
#      enforced, not just conventional).
#
# playbook-main.yml no longer carries any when: gate (round 5 dropped it —
# every play self-guards) so it is scanned by this loop like any other file;
# it has no "- hosts:" line (it is an import-only entry point), so the
# playbook-discovery guard below naturally skips it without a special case.
#
# A file with more than one "- hosts:" play (repo's only current instance:
# play-virtualbox-windows.yml) is REJECTED outright before any scope/guard
# scan — this check cannot safely vouch for a second, unexamined play hiding
# behind the first play's declaration.
GUARD_ASSERT_NAME='Scope guard — assert provisioning_profile is recognised'
GUARD_END_NAME='Scope guard — end play if provisioning_profile does not match declared scope'
GUARD_END_META='end_play'
GUARD_END_WHEN="(scope == 'gnome' and provisioning_profile == 'server') or (scope == 'server' and provisioning_profile != 'server')"
SCOPE_VIOLATIONS=()

while IFS= read -r -d '' yml_file; do
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" 2>"$TMP_GREP_ERR" || continue
    [[ "$yml_file" == */optional/archived/* ]] && continue

    rel_file="${yml_file#"$REPO_ROOT"/}"

    hosts_block_count=$(grep -cE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" || true)
    if [[ $hosts_block_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — file contains $hosts_block_count separate '- hosts:' plays; this gate cannot safely vouch for a multi-play file. Split it (one play per file, matching every other playbook in the repo), then give each its own vars.scope (+ guard if needed)."
        SCOPE_VIOLATIONS+=("$rel_file (multi-play file: $hosts_block_count plays in one file, not supported)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    # Single pass: extract vars.scope (wherever it sits in the play-level
    # vars: block) and the first TWO tasks' name / meta / when fields, using
    # bash counters rather than `grep -c` throughout — no `grep -c`-on-empty
    # hazard anywhere in this loop (the round-1 blocker's root cause simply
    # does not exist in this design).
    scope_count=0
    scope_val=""
    t1_name=""; t2_name=""; t2_meta=""; t2_when=""
    while IFS='|' read -r key val; do
        case "$key" in
            SCOPE) scope_count=$((scope_count + 1)); scope_val="$val" ;;
            TASK1_NAME) t1_name="$val" ;;
            TASK2_NAME) t2_name="$val" ;;
            TASK2_META) t2_meta="$val" ;;
            TASK2_WHEN) t2_when="$val" ;;
        esac
    done < <(awk '
        /^  vars:[[:space:]]*$/ { in_vars=1; next }
        in_vars && /^    scope:[[:space:]]*/ {
            val = $0
            sub(/^    scope:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)
            sub(/\r$/, "", val)
            if (val != "") { print "SCOPE|" val }
            next
        }
        in_vars && /^[^[:space:]]/ { in_vars = 0 }
        in_vars && /^  [^ ]/ { in_vars = 0 }

        /^  tasks:[[:space:]]*$/ { in_tasks=1; task_count=0; next }
        in_tasks && /^    - name:[[:space:]]*/ {
            task_count++
            if (task_count > 2) { in_tasks = 0; next }
            val = $0
            sub(/^    - name:[[:space:]]*/, "", val)
            print "TASK" task_count "_NAME|" val
            next
        }
        in_tasks && task_count == 2 && /^      ansible\.builtin\.meta:[[:space:]]*/ {
            val = $0
            sub(/^      ansible\.builtin\.meta:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)   # strip trailing comment (parity with SCOPE)
            sub(/\r$/, "", val)                # strip stray CRLF (parity with SCOPE)
            print "TASK2_META|" val
            next
        }
        in_tasks && task_count == 2 && /^      when:[[:space:]]*/ {
            val = $0
            sub(/^      when:[[:space:]]*/, "", val)
            sub(/[[:space:]]*#.*$/, "", val)   # strip trailing comment (parity with SCOPE)
            sub(/\r$/, "", val)                # strip stray CRLF (parity with SCOPE)
            print "TASK2_WHEN|" val
            next
        }
        in_tasks && /^  [^ ]/ { in_tasks = 0 }
    ' "$yml_file")

    if [[ $scope_count -eq 0 ]]; then
        echo "  ERROR (scope): $rel_file — missing vars.scope (need exactly one of general|gnome|server)"
        SCOPE_VIOLATIONS+=("$rel_file (missing vars.scope)")
        ERRORS=$((ERRORS + 1))
        continue
    elif [[ $scope_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — multiple vars.scope entries declared (exactly one required)"
        SCOPE_VIOLATIONS+=("$rel_file (multiple vars.scope entries)")
        ERRORS=$((ERRORS + 1))
        continue
    fi

    case "$scope_val" in
        general|gnome|server) : ;;
        *)
            echo "  ERROR (scope): $rel_file — invalid vars.scope value: $scope_val (must be exactly one of general|gnome|server)"
            SCOPE_VIOLATIONS+=("$rel_file (invalid vars.scope: $scope_val)")
            ERRORS=$((ERRORS + 1))
            continue
            ;;
    esac

    guard_ok=0
    if [[ "$t1_name" == "$GUARD_ASSERT_NAME" && "$t2_name" == "$GUARD_END_NAME" && "$t2_meta" == "$GUARD_END_META" && "$t2_when" == "$GUARD_END_WHEN" ]]; then
        guard_ok=1
    fi

    if [[ "$scope_val" == "general" ]]; then
        if [[ "$t1_name" == "$GUARD_ASSERT_NAME" || "$t1_name" == "$GUARD_END_NAME" ]]; then
            echo "  ERROR (scope): $rel_file — unnecessary scope guard on a general-scope play (guard never fires for general; remove it)"
            SCOPE_VIOLATIONS+=("$rel_file (unnecessary guard on general play)")
            ERRORS=$((ERRORS + 1))
        fi
    else
        if [[ $guard_ok -eq 0 ]]; then
            echo "  ERROR (scope): $rel_file — scope=$scope_val requires the 2-task canonical guard (assert + meta:end_play) as its first two tasks; missing or incorrect"
            SCOPE_VIOLATIONS+=("$rel_file (scope=$scope_val missing/incorrect guard)")
            ERRORS=$((ERRORS + 1))
        fi
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
```

**`set -e` safety, by construction, improved again this round.** Round 1's
blocker and round 3/4's fix were both about `grep -c`-on-empty aborting under
`set -euo pipefail`. This round's main per-play loop uses **zero** `grep -c`
calls at all — `scope_count` and the task fields are populated by counting
inside a `while IFS='|' read` loop fed by a single `awk` pass, matching the
pattern round 3/4's 4a (the import parser) already used safely. The **only**
remaining `grep -c` in the whole check is the multi-play-file
`hosts_block_count` guard, carried forward with its established `|| true`
guard from round 1's fix. Tested this round against 6 fixtures covering every
branch (gnome-correct, gnome-missing-guard, general-correct,
general-with-unnecessary-guard, missing-scope, invalid-scope) plus the
multi-play-file and archived-exemption cases already proven in round 3/4 —
zero crashes, correct verdict on every fixture, using the **exact script text
above**, not a hand-simplified stand-in.

**JSON-array-building, final `jq -n` call, terse summary, `TOTAL` handling,
and the "zero `qa-all.bash` edits" confirmation are unchanged in shape from
every prior round** — `$SCOPE_VIOLATIONS` and `$ERRORS` are populated
identically to before, just from this round's per-play source instead of the
4a/4b split. Reuse those blocks verbatim (see round-4's committed
`PROPOSAL.md`, `git show 21414423` era, or any prior round's §4/§5 for the
verbatim `sc_json_array`/`jq -n`/terse-summary text — unchanged character for
character).

### 5.3 The uniform `vars: { scope: ... }` form (every play, every scope)

```yaml
- hosts: desktop
  name: <Play Name>
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
    scope: general   # general | gnome | server — see CLAUDE/AnsibleStyle.md
  tasks:
    ...
```

For `gnome`/`server` plays, the two guard tasks (§3.1) go immediately after
`tasks:`, before any real work. A bare top-level `scope:` key (sibling of
`hosts:`/`name:`) is confirmed (round 3) to be a hard Ansible parse error —
must be a `vars:` entry.

---

## 6. The exact mixed-play edits (unchanged from round 3/4 — task-level `when:`, independent of the play-level guard)

These four edits are **identical to round 3/4** — they are task-level
`when:` overrides inside otherwise-`general` plays (or, for
`play-container-watch.yml`, inside an otherwise-`general` optional play), and
none of them interact with the new play-level guard mechanism (§3), because
none of these four plays' overall `scope` is `gnome`/`server` — they stay
`general` and carry no play-level guard, exactly as before.

### 6.1 `playbooks/imports/play-basic-configs.yml`

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

### 6.2 `playbooks/imports/play-prevent-ssh-suspend.yml`

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

Still fixes the real latent bug (§1.2): without this gate, a headless run
would hit `gsettings`, get "No such schema," and — under this repo's
`any_errors_fatal = true` — abort the entire run.

### 6.3 `playbooks/imports/play-vpn.yml` — task split, then gate the split-off task

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

### 6.4 `playbooks/imports/optional/common/play-container-watch.yml` — 8-task diff (unchanged)

Play-level `vars: { scope: general }`, no play-level guard (its dominant
classification is general), plus the same 8 task-level `when:` overrides
round 3/4 derived — 4 plain scalar, 4 list-form with the profile gate first
(short-circuit protects the `register`-dependent conditions):

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

---

## 7. Canonical commands + documentation

**Batch, zero-flag default (unchanged in spirit — still genuinely zero-flag,
now via self-guarding plays instead of import-site gates):**

```bash
ansible-playbook playbooks/playbook-main.yml
```

**Batch, explicit override:**

```bash
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=server
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=desktop
```

**Standalone — new first-class capability this round.** Any play, core or
optional, can now be run entirely on its own and still auto-gates correctly:

```bash
ansible-playbook playbooks/imports/play-firefox.yml
# auto-detects; runs on a real desktop, cleanly ends on a real server

ansible-playbook playbooks/imports/play-firefox.yml -e provisioning_profile=server
# explicit override; ends immediately after the guard's assert passes

ansible-playbook playbooks/imports/optional/common/play-container-watch.yml
# runs the general watchdog tasks always; the 8 GNOME-extension tasks
# auto-skip on a detected/forced server, same as if run via playbook-main.yml
```

**Documentation locations:**

- **`docs/playbooks.md`** — insert `## Desktop vs. Headless Server Provisioning` immediately before line 17 (`## Core Playbooks`), after
  `## Quick Navigation`. Content: the commands above; the auto-detection
  explanation (server-biased when uncertain); the `-e` override; **explicitly
  note every play — core and optional — is safe and correct to run
  standalone**, since that's the new capability this round adds.
- **`CLAUDE/AnsibleStyle.md`** — add `### Provisioning Profile Self-Guard (Plan 00061)` after `### Tagging Strategy`. Content: the canonical
  `vars: { scope: ... }` form (§5.3), the exact 2-task guard block (§3.1)
  verbatim, the rule that `general` plays never carry it (§3.3), and a note
  that both are enforced by `scripts/qa-ansible.bash` Check 4 (§5).
- **`docs/playbooks.md`'s existing per-play sections** —
  `### play-basic-configs.yml`, `### play-prevent-ssh-suspend.yml`,
  `### play-vpn.yml` each get a one-line note about their task-level `when:`
  gate (unchanged from round 3/4); every `### play-<gnome play>.yml` section
  (10 core + however many optional gnome plays get documented) gets a
  one-line note that the play self-guards and is safe to run standalone.

---

## 8. Verification procedure

**`--list-tasks` still cannot prove a `when:`-based (or guard-based) skip** —
unchanged limitation from round 3/4, re-confirmed this round: it lists the
guard task and everything after it under every profile, because it doesn't
evaluate `when:`/`meta:` conditions, only enumerates structure.

**What CAN be verified in the CCY container** (parse-only):

```bash
export ANSIBLE_CONFIG="$(git rev-parse --show-toplevel)/ansible.cfg"
ansible-playbook playbooks/playbook-main.yml --syntax-check
ansible-playbook playbooks/imports/play-firefox.yml --syntax-check   # spot-check a standalone play too
```

...plus running the QA gate itself (§5), which statically confirms every
play's declarations (scope + guard shape) are well-formed — this validates
the *declarations*, not the runtime *behaviour*, same distinction as every
prior round.

**What requires a real run (host-side, never in the CCY container)** —
unchanged set of procedures from round 3/4, **plus a new standalone check**:

1. **Batch, throwaway VM/container or real host**: same as round 3/4 — no
   `-e` flag on a detected server, confirm every `gnome`-bucket play's recap
   shows the guard firing (`ok` on the assert, then the play ending) and
   every `general`-bucket play running fully.
2. **Batch, real desktop with override + `--check`**: same as round 3/4 —
   `--check` is legitimate on a real host, not in the CCY container.
3. **NEW — standalone spot-check**: pick at least one `gnome`-bucket core
   play and run it entirely on its own
   (`ansible-playbook playbooks/imports/play-firefox.yml`), with no `-e`, on
   both a real desktop (expect it to run fully) and a real server or
   `-e provisioning_profile=server` override (expect the guard to end it
   immediately) — this is the capability round 5 specifically adds, so it
   should be exercised directly, not only inferred from the batch behaviour.

Tracked under PLAN.md Task 3.8 (host validation), not duplicated here.

---

## 9. Implementation checklist (execute in order)

01. **Add `environment/localhost/group_vars/desktop.yml`** exactly as shown
    in §2.1 (unchanged from round 3/4 — if already implemented from an
    earlier round, confirm it matches; no new edit needed).
02. **Add the third `assert` task to `play-AA-preflight-sanity.yml`** exactly
    as shown in §2.3 (unchanged from round 3/4).
03. **Revert `playbook-main.yml`** to the bare-imports state in §4 — remove
    all 10 `when:` lines if a prior round's edit is already in place, or
    confirm the file was never touched if starting fresh from round 5.
04. **Add `vars: { scope: ... }` to all 31 core plays** (§1.1's table) —
    this is new edit surface round 3/4 did not require (core plays
    previously carried no `vars.scope` at all).
05. **Add the 2-task canonical guard (§3.1) to the 10 `gnome`-bucket core
    plays** (`play-firefox.yml`, `play-ms-fonts.yml`, `play-browsers.yml`,
    `play-toolbox-install.yml`, `play-comms.yml`, `play-gnome-shell.yml`,
    `play-gnome-shell-extensions.yml`, `play-terminal-emulators.yml`,
    `play-vscode.yml`, `play-gsettings.yml`) as the first two tasks. The
    remaining 21 core plays get `vars.scope` only, no guard.
06. **Apply the 4 mixed-play edits (§6.1–§6.4)** to `play-basic-configs.yml`,
    `play-prevent-ssh-suspend.yml`, `play-vpn.yml`, and
    `play-container-watch.yml` exactly as shown — unchanged from round 3/4.
07. **Add `vars: { scope: ... }` to all 41 non-archived optional plays**
    (§1.3's table), **and the 2-task guard to every one classified
    `gnome`/`server`** (not just informational `vars.scope` as in round 3/4)
    — this is the other major new edit-surface item, with the same two named
    exceptions:
    - `play-container-watch.yml` — already handled in step 6 (`scope: general`, no play-level guard, 8 task-level overrides).
    - `play-virtualbox-windows.yml` — still must be **split first** (two
      `- hosts:` plays in one file; §1.3's table has the exact file
      boundaries), then each half gets `vars.scope` (+ guard if
      `gnome`/`server`) independently — get an explicit owner decision on
      both values first, same category as `rpm-fusion`.
    - For every other Medium/Low-confidence row, read the play's actual task
      list first and correct the classification if the fast-pass guess was
      wrong.
08. **Edit `scripts/qa-ansible.bash`** per §5.2: replace round 3/4's split
    4a/4b Check 4 with this round's single uniform per-play loop (or insert
    fresh if starting from round 3/4's non-`when:` state). Reuse the
    JSON-array-building/`jq -n`/terse-summary blocks verbatim, unchanged
    shape. **Do not touch `qa-all.bash`.**
09. **Run `./scripts/qa-all.bash`.** Expect it to fail before steps 4–7 land
    (every play missing `vars.scope`) and pass once every playbook (except
    the one archived play) carries a valid `vars.scope` and, where required,
    the exact 2-task guard.
10. **Add the `docs/playbooks.md` section, the `CLAUDE/AnsibleStyle.md`
    subsection, and update the per-play doc sections** per §7 (including the
    new standalone-safety notes on every gnome play's doc entry).
11. **Run `--syntax-check`** (§8) inside the CCY container on both
    `playbook-main.yml` and at least one standalone play; confirm clean.
12. **Re-run `./scripts/qa-all.bash`** one final time to confirm green.
13. **(On HOST, never in the CCY container)** run all three §8 verification
    procedures — batch/no-flag, batch/override+`--check`, and the **new**
    standalone spot-check. This is PLAN.md Task 3.8.
14. **Update PLAN.md**: mark Phase 3 tasks complete in the same commit as the
    code (Plan Commit Rule), add a `JOURNAL/` entry, and record that
    Decision 5's mechanism is now implemented (not just decided).

---

## 10. Known limitations / failure modes consciously accepted

- **No cheap in-container end-to-end skip proof** — unchanged from round
  3/4, still true for the guard mechanism (§8). The QA gate proves
  *declarations*, not *behaviour*; only a real run proves behaviour.
- **`connection: local` is a load-bearing, documented assumption** —
  unchanged from round 3/4 (§2.1's file header).
- **A mis-detected profile degrades asymmetrically, by design** — unchanged
  from round 3/4: desktop-mis-detected-as-server silently skips GUI installs
  (recoverable); server-mis-detected-as-desktop attempts to run GUI plays
  against a session-less box (individually debuggable failures) — this is
  exactly why detection is server-biased-when-uncertain.
- **The QA gate validates declaration shape, not runtime correctness** —
  unchanged: it confirms every play has the right `scope`/guard shape, not
  that the shape is semantically correct for what the play's tasks actually
  do, nor that a future task addition doesn't quietly need a GUI.
- **The fast-pass optional-tree classification is unchanged**, same
  confidence caveats apply (§1.3, checklist step 7).
- **`play-container-watch.yml`'s file-split remains deferred** — same
  reasoning as every prior round; the 8-task `when:`-list interim (§6.4) is
  correct but structurally heavier than the "trivial exception" pattern was
  designed for.
- **`play-virtualbox-windows.yml`'s scope value(s) remain an open owner
  decision**, blocking on the mandatory file-split before either half can be
  classified at all.
- **The `gnome` bucket means "needs a GUI session," not literally
  "GNOME-specific"** — `play-lxde-install.yml` is `gnome`-bucketed despite
  being LXDE; unchanged naming note from every prior round.
- **A `general`-scope play with a task added later that happens to need a
  GUI is not caught by the QA gate** — same review-discipline gap named in
  every prior round; no static gate closes it without executing the play
  headless in CI (out of scope for this plan).
- **The uniform 2-task guard is now duplicated verbatim across 10+ core
  plays and however many `gnome`/`server` optional plays exist** — a
  deliberate tradeoff (§3.3): this is boilerplate repetition, not logic
  duplication (the QA gate enforces the text stays byte-identical
  everywhere), so a future change to the guard's own expression (e.g. adding
  a 4th profile value) is a mechanical find-and-replace across every
  `gnome`/`server` play plus the QA gate's canonical constants — more edit
  sites than round 3/4's single import-site `when:` design had, accepted as
  the direct cost of standalone-runnability, which the import-site design
  could not provide at any cost.
- **Two typo-protection asserts now exist** (`play-AA`'s, §2.3, and every
  guard's own, §3.4) with intentionally identical wording — a deliberate,
  explained (not accidental) duplication; each covers a different
  invocation-shape's "as early as possible" property and neither can be
  removed without reopening the gap the other doesn't cover.
