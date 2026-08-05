# Implementation Plans Directory

This directory contains implementation plans following the claude-code-hooks-daemon plan workflow.

## Plan Workflow

**IMPORTANT**: All non-trivial implementation work should follow the planning workflow documented in [PlanWorkflow.md](../PlanWorkflow.md).

### Quick Reference

Plans use numbered prefixes for sequential organization:

- `001-description/` - First plan
- `002-description/` - Second plan
- etc.

Each plan directory contains:

- `PLAN.md` - Main plan document with tasks, goals, and progress tracking
- Supporting files (implementation, tests, documentation)

### Task Status Icons

Use these Unicode icons in plan documents:

- ⬜ `TODO` - Not started
- ✅ `DONE` - Completed successfully
- 🔄 `IN_PROGRESS` - Currently working on
- 🚫 `BLOCKED` - Cannot proceed (dependency/issue)
- ❌ `FAILED` - Attempted but failed (requires rework)
- ⏸️ `PAUSED` - Temporarily suspended
- 👁️ `REVIEW` - Needs review/approval
- 💤 `DORMANT` - Paused indefinitely, blocked on an external/human decision

## Active Plans

- [004-comprehensive-feature-documentation](004-comprehensive-feature-documentation/) - Documentation for all major features (CCY, CCB, Nord, Speech-to-Text, etc.)

- [007-speech-to-text-resource-leak-fixes](007-speech-to-text-resource-leak-fixes/) - Fix microphone resource leak, transcription truncation, and browser paste failures

- [009-claude-devtools](009-claude-devtools/) - Install and integrate claude-devtools session visualiser (implementation committed, pending host deployment and testing)

- [011-claude-devtools](011-claude-devtools/) - claude-devtools (ccdt) installation plan (supersedes 009)

- [013-claude-devtools](013-claude-devtools/) - claude-devtools (ccdt) installation plan (latest iteration)

- [014-whisper-model-manager](014-whisper-model-manager/) - Replace cluttered model dropdown with a dedicated Textual TUI (`wsi-model-manager`) for browsing and downloading Whisper models

- [018-fedora-kickstart-install](018-fedora-kickstart-install/) - Fully automated Fedora install pipeline: GRUB netinstall boot entry, a `%pre` TUI collecting WiFi/LUKS/user info upfront, and a LUKS2 + Btrfs single encrypted volume

- [022-install-security-and-resilience](022-install-security-and-resilience/) - Fixes found during real-hardware kickstart testing (FDINST partition wiped by `clearpart --all` on reinstall; install security + resilience hardening)

- [023-hostname-based-inventory](023-hostname-based-inventory/) - Migrate Ansible inventory from hardcoded `localhost` to machine hostname, supporting per-machine host_vars and multiple laptops

- [025-ccy-spring-cleaning](025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues

- [026-repo-spring-cleaning](026-repo-spring-cleaning/) - Repository-wide spring cleaning (non-CCY): remove tracked backups, fix bash scripts (set -e, shellcheck), fix Ansible playbooks (duplicate shebangs, curl-to-bash, state:latest)

- [027-contextual-shell-history](027-contextual-shell-history/) - Replace bash history with Atuin for directory/git-workspace-aware command recall

- [028-fedora-screen-sharing](028-fedora-screen-sharing/) - Diagnose and fix unstable screen sharing on Fedora 43 GNOME (Slack desktop broken by `app.asar` hardcode; Meet freezes traced to mutter ScreenCast bugs fixed in 49.3/49.5)

- [029-rapid-raw-cloud-ai](029-rapid-raw-cloud-ai/) - Evaluate cloud GPU paths for RapidRAW Tier 2 generative AI: free local-first verification (dGPU + Tier 1), local SD 1.5 prototype, vast.ai $10-credit prototype with SDXL/Flux Fill, then evidence-based decision gate before any productionisation

- [030-phpantom-lsp](030-phpantom-lsp/) - 💤 Dormant — Research PHPantom (Rust-based PHP LSP) as replacement for Intelephense; awaiting decision-gate go/no-go before implementation

- [031-reliable-screen-sharing](031-reliable-screen-sharing/) - 💤 Dormant — Reliable screen-sharing alternatives for WFH devs on Fedora 43 Wayland (complements Plan 028); Phase 2 complete, blocked awaiting user go-ahead for Phase 3

- [032-compression-helpers](032-compression-helpers/) - `compress` / `uncompress` CLI wrappers around `ouch`: xz by default, `--zip` flag, auto-detect on decompress, always-extract-into-folder (tarbomb protection)

- [034-localhost-config-account](034-localhost-config-account/) - Track config-owning GitHub account in `localhost.yml` instead of relying on volatile `gh api user` (config repo lookup was driven by active gh default)

- [00035-gh-multi-account-hardening](00035-gh-multi-account-hardening/) - 💤 Dormant — Harden fresh-install flow (gh multi-account first, then SSH keys); fix SSH probe fallback bug in playbook and ccy; replace manual paste with `gh ssh-key add`; research signed commits

- [00037-image-watermarking-toolkit](00037-image-watermarking-toolkit/) - Composable `watermark` CLI primitive: ImageMagick two-layer visible mark (corner + faint diagonal tile) plus full EXIF/IPTC/XMP licence metadata; idempotent via filename suffix and XMP sentinel; delivered via optional Ansible playbook; wrappable by client projects via config precedence chain and named profiles

- [00038-musiccast-controller](00038-musiccast-controller/) - MusicCast controller for Linux desktop with full UX (now-playing + Qobuz browse/search/play); 5 parallel research tracks complete (YXC API surface, OSS landscape, HA integration, Qobuz architecture, stack options); live-confirmed Qobuz is native on the WXA-50 via YXC; decision gate recommends Python + Textual + `aiomusiccast` with parallel KsanStone-fork experiment

- [00039-ftp-camera-viewer-tui](00039-ftp-camera-viewer-tui/) - Extend `ftp-camera` with orthogonal `--view` / `--view-jpg` modifier flags that compose with any FTP-server mode (default / `--async` / `--async-copy`); class-filtered live single-window preview via geeqie's implicit single-instance; pre-warm only in sort modes; two-step `gum choose` TUI for argument-free invocation; structured startup confirmation banner; ready to implement

- [00040-raw-clipping-scanner](00040-raw-clipping-scanner/) - Standalone `clip-scan` CLI: pre-Lightroom-import bulk flagger that decodes Sony ARW via `rawpy`, computes a weighted clipping score per side (linear ramp from cutoff to saturation; pixels closer to extremes weighted higher), renames files with `.wclip` / `.bclip` sub-extensions when score exceeds threshold; iterated through binary count → two-axis cutoff+count → weighted score (Mertens 2007 well-exposedness prior art); awaiting decision-gate confirmation and subagent review

- [00041-remote-desktop-quick-toggle](00041-remote-desktop-quick-toggle/) - One-click GNOME quick-settings toggle for `gnome-remote-desktop` Desktop Sharing on F43 Wayland: `rdt` CLI + quick-settings extension, LAN-scoped (user-configured CIDR) non-persistent firewalld rich-rule, off by default and after reboot, RDP creds in GNOME Keyring (not the repo); rejected NoMachine/AnyDesk (user reports prior instability) and Remote Login mode (separate session not wanted); lock-screen-but-show-real-desktop documented as impossible in mirror-mode RDP — workaround is lid-close as privacy

- [00042-darktable-ai-features](00042-darktable-ai-features/) - Enable darktable's optional AI features (object masks, denoise, upscale) on Fedora 43; research found Fedora RPM and Flathub Flatpak are both built WITHOUT `-DUSE_AI=ON`. Recommended path: local source-built RPM via `mock` (cmake auto-downloads ONNX Runtime; one-flag delta from Fedora spec; A7V cameras.xml becomes a build-time patch). Fallback: upstream AppImage as `darktable-ai`. Optional GPU acceleration playbook gated by `lspci` vendor ID `10de` for safe multi-laptop deploy. Awaiting Phase 1 decision gate. (Renumbered from local 00039 on merge — collided with remote 00039 ftp-camera-viewer-tui.)

- [00045-project-personas-multi-tool-accounts](00045-project-personas-multi-tool-accounts/) - Generalise the gh `github_accounts` + per-alias-bash-function pattern into a top-level `project_personas` map in `localhost.yml` driving multiple tools (gh today + wrangler next; npm/aws/gcloud/etc. in follow-ups). KISS migration: gh playbook reads `project_personas` directly and fail-fasts on legacy schema with copy-pasteable migration YAML — no compat shim. Wrangler uses explicit env-var injection per `wrangler-<alias>` call; API tokens stored in GNOME Keyring via `secret-tool`, never on disk in plaintext. Awaiting Phase 1 decision gate.

- [00046-localhost-yml-leak-guard](00046-localhost-yml-leak-guard/) - Project-level PreToolUse hooks-daemon handler that blocks `gh issue/pr/gist (create|edit|comment)` and HTTP-POST commands (`curl -d`, `wget --post-*`) when the command body contains any token from a deny-list dynamically derived from `localhost.yml`. Allowlist of public-by-design tokens (`joseph`, `LongTermSupport`, …) lives in `.claude/public-token-allowlist.yml`. Closes the safeguard gap surfaced by a recent leak into the public fedora-desktop issue tracker via `gh issue create`.

- [00048-cc-token-source-parity](00048-cc-token-source-parity/) - Supersedes cancelled Plan 00036: extend host `cc` to use ccy's named-token chooser (shared pool at `~/.claude-tokens/ccy/tokens/`) via a new wrapper script at `/var/local/claude-code/cc` that sources the existing `token-management.bash` library. Adds a "Desktop" pseudo-option to the chooser meaning "use host `~/.claude/` OAuth" (current `cc` behaviour, now explicit); empty-pool path falls through to Desktop with inline instructions to create tokens via `ccy --create-token`. Phase 1 also factors a new `common-pure.bash` so the host wrapper can source helpers without triggering `common.bash`'s podman-check `exit 1`. No container required for `cc` (creation stays ccy-only). Not blocked by Plan 00047 — does not export `CLAUDE_CODE_DISABLE_MOUSE=1`.

- [00049-full-repo-audit](00049-full-repo-audit/) - Full repository audit via dynamic multi-agent workflow: 10 audit dimensions (security, fail-fast, Ansible, bash, CCY, extensions, performance, docs drift, opportunities, QA gaps) with adversarial verification of critical/high findings; research docs + triage.md + final action plan in the plan folder

- [00050-fedora-44-tracking](00050-fedora-44-tracking/) - Fedora 43 → 44 migration tracking (research phase only — no fixes). Dynamic Fable workflow swept six version-sensitivity dimensions (version literals, packages/repos+DNF, Python, GNOME extensions, hardware/kernel, install/bootstrap) against the live F44 changeset. 55 findings (7 high, 14 medium, 25 low, 9 info) in research/ + triage.md. Confirmed F44 baseline: GNOME 50, kernel 7.0.x, Python stays 3.14, DNF5 complete. The bump's core is one line (`fedora_version: 44`) but seven highs gate it; execution deliberately deferred to a future decision gate.

- [00051-ansible-lint-improvement](00051-ansible-lint-improvement/) - Systematic ansible-lint compliance improvement: `scripts/lint` tooling, FQCN enforcement, and per-rule violation fixes across all 37 playbooks

- [00053-fedora-44-fresh-install-audit](00053-fedora-44-fresh-install-audit/) - First fresh-F44 host audit using the new `playbooks/dev/play-collect-diagnostics.yml` collector. Splits findings into generic (NetworkManager-wait-online failure, firewalld⇄docker NAME_CONFLICT, gkr-pam at GDM login, intel-lpmd noise, dnf-makecache boot cost), hardware-specific (ThinkPad DYTC thermal mask, NVIDIA RTX 500 Ada Optimus on Meteor Lake, bluetoothd hci0, slow TPM/serial discovery), dev-tooling fixes for the collector itself (`lsblk -fO` flag clash, stale cached timestamp, `powerprofilesctl` probe misreads F44's tuned-ppd backend, rc=127 "tool absent" noise), and a firmware-update workflow decision (Intel ME multi-CVE behind). Decision recorded: F44's TuneD + tuned-ppd is the supported Power Mode backend; no `power-profiles-daemon` install.

- [00054-github-ssh-443-host-level](00054-github-ssh-443-host-level/) - Unify GitHub SSH-over-443 behind one runtime signal (`GITHUB_SSH_443`) across host and CCY. Fills four gaps: CCY currently clobbers an inherited `GITHUB_SSH_443` (so `export GITHUB_SSH_443=1; ccy` is a one-line fix); no temporary host toggle (adds a TDD'd `helpers/github443/` + `github-ssh-443 on|off|auto|status` CLI that edits `~/.ssh/config`+`known_hosts` without an Ansible run and reconciles with the playbook markers); host `known_hosts` lacks the `[ssh.github.com]:443` pin; foreign deploy-key aliases aren't rerouted (first-wins BOF override keyed on a new `github_443_extra_aliases` var). Always-on path = `github_ssh_over_443: true` + profile.d export so every `ccy` inherits 443. Records the ssh-agent analysis: the agent needs NO flush (keys are endpoint-agnostic); only multiplexing control sockets can go stale, and the repo sets none.

- [00055-container-process-watchdog](00055-container-process-watchdog/) - Reporting-only host watchdog: a `systemd --user` timer scans `/proc`, attributes every long-running (≥15 min) CPU-pinned process to its container (Podman/Docker/LXC via cgroup markers + rootless-uid extraction + NSpid host→container PID translation), and surfaces actionable findings through a GNOME Shell panel/notification AND a CLI, guiding the human to fix it inside the container. Triggered by a real incident: a single multithreaded `ugrep` (Claude Grep tool with an unscoped `/` path) pinned ~11/22 cores for ~1.9h inside one CCY container with no fast way to identify which. CPU caps explicitly rejected as symptom-hiding. `context.md` carries full incident forensics, the attribution technique (incl. the `comm`-lies-use-cmdline gotcha), full-stack design, multi-engine cgroup table, and open decisions.

- [00056-displaylink-dock-hotplug-recovery](00056-displaylink-dock-hotplug-recovery/) - Resurrects GitHub issue #28 (closed as a monitor-port hardware fault, but explicitly left reopenable): the DisplayLink dock unplug/replug wedge is back, confusing GNOME Shell/mutter (Wayland) and requiring a logout or reboot to recover. Multi-agent research pass into whether the evdi/USB-side wedge and/or mutter's monitor-manager state corruption can be recovered without a reboot, aiming to replace the current service-restart-only watchdog with a real udev/systemd-based recovery mechanism.

- [00057-lxc-net-networkmanager-bridge-race](00057-lxc-net-networkmanager-bridge-race/) - `lxc-net.service` fails at boot with `RTNETLINK answers: File exists` because NetworkManager owns `lxcbr0` (a persisted autoconnect bridge profile, created as a side effect of the play's `nmcli connection modify … zone` task) and assigns the bridge address before `lxc-net` runs — so `lxc-net`'s `ip addr add` fails and `dnsmasq` never starts, leaving LXC containers with no DHCP lease and unreachable, while the bridge-is-up sanity check false-passes. "Defence before fix": a read-only `triage.bash` + a real DHCP-readiness playbook gate, then the root-cause fix (mark `lxcbr0` NM-unmanaged via `conf.d`, drop the nmcli-zone tasks in favour of the firewalld interface binding, enable `lxc-net`, and recover a broken host non-disruptively).

- [00058-github-version-pin-updates](00058-github-version-pin-updates/) - Bumps every hardcoded upstream version pin in the playbooks that `scripts/check-pinned-versions.bash` found behind upstream (nvm, markless, rescrobbled, ouch, RapidRAW, ART, DisplayLink/evdi), including each adjacent sha256 checksum and release-asset-name change, one commit per pin. darktable held back (Fedora dist-git still ships 5.4.1, so a 5.6.0 bump would break the spec-driven build); cuDNN left as a manual NVIDIA check. Ships a plan-local `deploy.bash` to re-run every affected playbook on the HOST.

- [00061-headless-server-provisioning](00061-headless-server-provisioning/) - Enable the repo to provision a headless Fedora Server (no GNOME/GUI) from the same source tree as the desktop, at the lowest possible maintenance burden: a per-play scope taxonomy (`scope-gnome`/`scope-general`/`scope-server`), a server provisioning entry point/selector, and a mandatory QA gate that fails on any play missing/mis-declaring its scope. Design decided via two independent brainstorm passes (Fable vs Sonnet) at a decision gate.

- [00062-disk-reclaim-tui](00062-disk-reclaim-tui/) - Adds general-purpose disk-reclamation tooling (the repo previously shipped only the narrow `raw-prune`): `play-disk-reclaim.yml` installs `ncdu`/`duf`/`trash-cli`/`baobab` (GUI gated off on servers) and deploys `reclaim` — a pure-bash, dependency-light interactive TUI that reports disk usage and runs targeted, confirm-first cleanup actions (dnf autoremove+clean, old kernels, journal vacuum, podman/docker prune, flatpak unused, `~/.cache`, trash, plus a safe sweep) and launches ncdu/baobab for deep dives. Follows InteractiveScripts + StderrHygiene standards; `reclaim report` is the non-interactive stdout payload. QA green (403 files); standalone opt-in play, HOST deploy/live-test pending.

- [00063-headless-run-bash-server-cloud-provisioning](00063-headless-run-bash-server-cloud-provisioning/) - Makes `run.bash` (the interactive-only bootstrap installer) provision a headless Fedora **Server or Cloud** box unattended, driven entirely by `RUN_BASH_*` environment variables (12-factor / cloud-init friendly). Adds a headless execution mode that neutralises every terminal prompt through the existing prompt-helper chokepoints and **fails fast** (naming the exact env var) when a required value is missing with no TTY. GitHub setup stays mandatory (fed from env, SSH-only auth); desktop interactive path unchanged. Expands `--help` and adds `--help-run-headless` documenting the full env IaC contract. Confirms Fedora Cloud needs **no new play scope** (it resolves to `provisioning_profile: server` via Plan 00061). Depends on Plan 00061. Design hardened via a hostile Opus review loop before implementation.

- [00064-open-command-universal-file-opener](00064-open-command-universal-file-opener/) - Adds `open` — one command to open any file, directory, or URL. Prior art was surveyed first: `xdg-open`/`gio open` open the registered default and silently fail (exit 3) without one, and `mimeopen` (perl-File-MimeInfo, the closest existing tool, retained here for setting defaults) only ever knows GRAPHICAL apps, so over SSH it offers apps with nowhere to render; `handlr` is not in Fedora's repos. The wrapper adds the two missing behaviours — **session awareness** (no display ⇒ only terminal viewers actually installed are offered) and **a chooser when unsure** (no default or unknown type ⇒ fzf, else a numbered menu; `-a` forces it) — and delegates everything else. Ships `files/home/.local/bin/open` + `play-open-command.yml` (`scope: general`, so it works on the headless server too). Smoke tests caught two defects review missed (a known type with no installed handler ran silently; the chooser hung with no TTY). QA green (413 files); HOST deploy + live verification pending.

- [00065-headless-server-cloud-base-blocker-fixes](00065-headless-server-cloud-base-blocker-fixes/) - The headless `run.bash` server path (Plan 00063) is documented as supporting Fedora **Server or Cloud**, but a first live run against minimal **Cloud Base** revealed it cannot complete there: three `general` core plays each abort the whole run under `any_errors_fatal` (fwupd `get-devices` unguarded at import 3; `play-lxc-install-config` at import 18 hard-depends on `firewalld`/`dnsmasq`/`iptables-nft`/`NetworkManager` it never installs, plus a vault + `git@`-SSH clone of the *public* `lxc-bash`; `play-markless` `cd ~/Downloads` at import 26), and two more leave a container host silently wrong (no `loginctl enable-linger` ⇒ oomd override inert and `podman.socket` skipped). Fix = declare deps in IaC at the layer that needs them (play `dnf`-installs; `run.bash` installs pre-ansible deps), switch `lxc-bash` to HTTPS, enable linger, and add the missing preflight edition/flavour fail-fast (`VERSION_ID` can't distinguish edition). Edit+commit only; HOST-run test (first `run.bash` on a fresh Cloud Base VM) pending.

- [00066-ftp-camera-airbnb-wifi-and-hotspot-triage](00066-ftp-camera-airbnb-wifi-and-hotspot-triage/) - Two `ftp-camera` failures on Airbnb WiFi. (1) `--async-copy` never gets past the first frame: the camera re-uploaded the same `DSC06824` ~6 times in 8 minutes. The log carries **zero `FAIL LOGIN`/`FAIL UPLOAD`**, so no *data* transfer failed — but a first revision over-read that as "the network is fine", which is corrected: `OK UPLOAD` is logged when the data completes, while the `226` that tells the *camera* travels afterwards on the control connection, so a dropped control socket yields this exact zero-failure signature on a bad network. Three live hypotheses, none confirmed — H1 (async `sudo mv` moves the file out of the chroot before the camera can check it back), H2 (camera-side timeout), H2b (control connection dies before the `226` lands; fits the overlapping sessions and the owner's report that the same setup works on other networks). Cause deliberately **not** asserted: ships `triage.bash` + a new `ftp-camera --debug-ftp` (runtime-only `log_ftp_protocol=YES`, PASS masked), whose verb stream discriminates all three. Also logs three unverified premises the analysis had leaned on unnoticed (that more frames were even waiting; that Sony's client verifies uploads at all; that the camera's FTP menu was unchanged). (2) `--hotspot` exposed a real **IaC gap readable in the source** — the play only ever *tuned* an NM profile the user was told to create by hand in GNOME Settings, a prohibited manual change; the play now creates the WPA2 AP itself (vaulted PSK, pmf disabled for camera compat, ch6/40MHz). An earlier revision overstated this as "the profile never existed / `--hotspot` never worked" — corrected: the owner reports it working before, and GNOME's hotspot toggle creates a profile named exactly `Hotspot`, so the open question is what *deleted* it (Task 1.5 probes the on-disk store, renamed AP profiles, and NM journal events — a profile that vanishes once can vanish again). Also fixed a `qa-ansible.bash` false positive that flagged the directive-name in a comment. **No triage has been run** — everything so far is from source-reading plus the owner's pasted session output; QA green (417 files), HOST triage pending.

## Completed Plans

- [00068-document-ccy-system](Completed/00068-document-ccy-system/) - CCY was the repo's daily driver and had **no user-facing documentation** — `docs/features/README.md` still listed it as "planned", and the only coverage was a custom-Dockerfile section in `containerization.md`, the narrow `ccy-debug-mounts.md`, and two *agent*-facing artifacts (the in-image `CCY-GUIDE.txt`, `CLAUDE/ContainerRules.md`). Shipped `docs/ccy.md`: what/why, install, the real launch sequence, an explicit **threat model** and mount-by-mount exposure table, the token pool, the full flag surface, per-project config (`Dockerfile`/`ccy.env`/`allowed-hostnames`), the hooks-daemon **supervisor** (auto-compaction + ctrl+z guarding), networking, versioning/auto-update, and troubleshooting — indexed from `docs/README.md` and cross-linked rather than duplicating what sibling docs own. A 10-agent adversarial pass (7 sonnet ground-truth verifiers + 3 fable clarity lenses) caught six high-severity defects in the first draft, each re-verified against source before fixing: an **invented `--no-cache` flag** (only ever hardcoded inside `--rebuild`), a wrong launch-sequence order (SSH precedes token; the root guard is late), an **omitted Wayland/X11 socket + `/dev/dri` exposure** in the security model, a "warns loudly" claim where `check_ccy_gitignore_safety` actually hard-blocks the launch, **backwards supervisor guidance** (the per-project deploy hardcodes `--arm`, so projects start armed, not in dry-run), and containment framing that overclaimed given the account-wide SSH key and `gh` token. Also repaired three pre-existing broken `installation.md` anchors. Docs only — no code changed, so no `CCY_VERSION` bump. QA green (417 files).

- [00060-stderr-hygiene-coding-standard](Completed/00060-stderr-hygiene-coding-standard/) - A generated `gh-<alias>()` wrapper echoed its "Switching to <user>..." status line to stdout, polluting `$(gh-<alias> … --json)` captures and breaking `jq` when the account wasn't the box default; fixed with `>&2` (`f2c7f93`). A repo-wide audit (bash executables, generated dotfile functions, playbook shell blocks, `scripts/`, `helpers/`) found **0 other real bugs** — the repo is already uniformly disciplined. Shipped `CLAUDE/StderrHygiene.md` as a first-class coding standard (stdout = captured payload; all chatter → `>&2`; help/report commands carved out), wired into CLAUDE.md + back-linked from InteractiveScripts.md rule 08 (`62ac5c5`).

- [00047-claude-code-mouse-wheel-pageup](Completed/00047-claude-code-mouse-wheel-pageup/) - Claude Code's fullscreen (alt-screen) renderer + CCY's `CLAUDE_CODE_DISABLE_MOUSE=1` let the terminal's DECSET-1007 fallback turn the wheel into arrow keys that clobber the prompt. After Path C+ (per-emulator wheel→PageUp remap + preflight gate) was proven dead and Path D (force the classic renderer) shipped as a workaround, the real fix — **Path E** — landed: drop `DISABLE_MOUSE` so Claude Code captures the mouse and scrolls the conversation natively inside the alt-screen (works on GNOME-Terminal/VTE), then drop the `DISABLE_ALTERNATE_SCREEN` kill switch so fullscreen is opt-in again (`/tui fullscreen`, or per-repo via `.claude/ccy/ccy.env`). `entrypoint.sh` now sets neither var; container 2.20 → 2.22, CCY 3.27.0. Verified on GNOME-Terminal.

- [00059-plan-folder-cleanup-and-plan-qa](Completed/00059-plan-folder-cleanup-and-plan-qa/) - Made `plan_workflow.qa` explicit in `.claude/hooks-daemon.yaml` and resolved the pre-existing plan-tree drift its first sweep surfaced (missing index rows, completed/cancelled plans left in the active root, missing status headers, a lowercase `plan.md`); `plan-qa --sweep` went 16 findings → 0.

- [002-nordvpn-openvpn-manager](Completed/002-nordvpn-openvpn-manager/) - `nord` bash script + Ansible playbook to manage NordVPN OpenVPN connections via NetworkManager (on-demand import, persistent connections, vault credentials). Shipped `files/home/.local/bin/nord`, `play-nordvpn-openvpn.yml`, and `docs/nordvpn-installation.md`.

- [006-documentation-audit-and-update](Completed/006-documentation-audit-and-update/) - Documentation coverage audit and feature inventory across the repo (coverage assessment + feature inventory supporting docs).

- [015-article-mode](Completed/015-article-mode/) - Article mode for speech-to-text: an indefinite looped recording mode that flushes every 120 s and re-polishes the whole raw article via Claude in a two-pane GTK window (`Shift+Insert`). Shipped `files/home/.local/bin/wsi-article` + `wsi-article-window`.

- [017-merge-ccy-ccb](Completed/017-merge-ccy-ccb/) - Retire CCB and CCB-Browser and consolidate into the single CCY tool.

- [021-firstboot-wizard-redesign](Completed/021-firstboot-wizard-redesign/) - Firstboot wizard redesign.

- [00052-run-bash-human-friendly](Completed/00052-run-bash-human-friendly/) - Made `run.bash` totally human-friendly (v1.5.4 → 1.6.1): Enter accepts at every confirm (fixed the `promptForValue` "Is this correct? (y/n)" bug that forced a full re-type on Enter), safe-polarity defaults (`(Y/n)` benign / `(y/N)` for reboot+public-issue+untested-playbooks), visible `[default]` on every prompt, all prompts on the shared helper family, friendlier error messaging, and verify-before-write hardening of the vault-password recovery path with an `abort` escape hatch. Opus implementation + adversarial Fable review (caught a raw-escape-render defect on the headline prompt + two interrupt-safety nits). QA green (285 files), smoke 22/22. HOST-only live run (Task 6.4) deferred to the user.

- [020-semgrep-custom-bash-rules](Completed/020-semgrep-custom-bash-rules/) - Add Semgrep with custom bash convention rules (no error hiding, fail-fast enforcement) integrated into qa-all.bash. Semgrep 1.153.1 installed via pipx in CCY Dockerfile (v2.10); 0 violations in 44 bash files.

- [024-claude-md-modular-restructure](Completed/024-claude-md-modular-restructure/) - Restructure monolithic CLAUDE.md (40k+ chars) into modular architecture: lean front page + CLAUDE/ topic files + docs/ for user content. All CLAUDE/ topic files created and @ pointers in place.

- [033-ddev-installation](Completed/033-ddev-installation/) - Install DDEV on rootful Docker (Approach C); rootless Podman remains default engine, LXC unchanged. End-to-end host run verified (`ddev v1.25.1` + `docker 29.4.0`).

- [00043-ipu6-webcam-fallout](Completed/00043-ipu6-webcam-fallout/) - Incident + recovery: `play-ipu6-webcam.yml` (commit e5d0e33) pulled `akmod-intel-ipu6` which dragged in a half-installed kernel 7.0.9 because the previous-minor `kernel` versionlock silently filtered the metapackage out of the depsolve while the unlocked sub-packages came in — leaving a "kernel" with no iwlwifi/btusb. IPU6 play rewritten to drop the akmod (mainline IPU6 is in-tree on F43+); recovery executed via new `play-AB-dnf-upgrade.yml` which now also auto-detects + cleans up future half-installs. Verified end-to-end on 7.0.9-105: WiFi, BT, camera all working.

- [00044-laptop-health-audit](Completed/00044-laptop-health-audit/) - Read-only audit (4 parallel sub-agents) of the daily-driver X1 Carbon Gen 10 i7-1260P, then IaC cross-check to separate real gaps from busywork. Net result: new `play-AB-dnf-upgrade.yml` (also recovers half-install state), `play-ZZ-repo-cleanup.yml` (orphan COPR removal), `play-laptop-thermal-diagnostics.yml` (mask thermald + install lm_sensors), `play-network-wait-tuning.yml` (cap NM-wait-online to 5s), `play-rclone.yml` extended with per-mount `MemoryHigh`/`MemoryMax`. Dropped as busywork: `tuned` profile (tuned-ppd auto-switches), WWAN modprobe blacklist (GNOME Settings is the toggle), warp-svc verbosity (no stable config surface). Established the "work WITH GNOME, not against it" principle.

## Cancelled Plans

- [012-fix-plugin-handlers](Cancelled/012-fix-plugin-handlers/) - Upstream bug in `claude-code-hooks-daemon`; bug report filed at `untracked/upstream-bug-report-plugin-handler-suffix.md`
- [00036-cc-ccy-parity](Cancelled/00036-cc-ccy-parity/) - Cancelled — superseded by Plan 00048. Was: bashrc include exporting `CLAUDE_CODE_NO_FLICKER` + `CLAUDE_CODE_DISABLE_MOUSE` on every host shell. Both vars independently obsoleted — NO_FLICKER dropped from ccy in commit a32c3d3 (Plan 00047 Path D), DISABLE_MOUSE now owned by Plan 00047. Per-invocation wrapper-script approach in Plan 00048 covers the remaining cc/ccy parity space.

## Archive

The `Archive/` directory contains legacy plans created before adopting the structured plan workflow:

- **ccb-browser-automation.md** - CCB browser automation implementation
- **ccyb.md** - CCY background service planning
- **speech-to-text.md** - Speech-to-text integration
- **workspace-names-overview.md** - Workspace naming conventions

These are preserved for reference but don't follow the current plan structure.

## Creating New Plans

See [PlanWorkflow.md](../PlanWorkflow.md) for complete instructions.

**Quick start** — use the scaffolding script (reads the authoritative git counter and creates the numbered folder atomically):

```bash
CLAUDE/Plan/mkplan.bash "my-feature"
```

Then add an index row for the new plan under **Active Plans** above. The `PLAN.md`
skeleton is rendered from the tracked, project-owned template
[`_TEMPLATE_.md`](_TEMPLATE_.md).

## Plan Workflow Integration

The hooks daemon enforces plan workflow standards:

- ✅ `validate_plan_number` - Ensures correct numbering format
- ✅ `plan_time_estimates` - Blocks time estimates in plans
- ✅ `plan_workflow` - Provides guidance when creating plans
- ✅ `plan_qa_edit` / `plan_qa_commit_gate` / `plan_qa_sweep` - Lint plan-tree drift (status headers, row↔folder bijection, archive placement) at edit, commit, and session-start; policy under `plan_workflow.qa` in `.claude/hooks-daemon.yaml`

## References

- [PlanWorkflow.md](../PlanWorkflow.md) - Complete plan workflow documentation
- [CLAUDE.md](../../CLAUDE.md) - Project-level Claude configuration
