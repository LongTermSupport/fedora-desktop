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

## Active Plans

- [002-nordvpn-openvpn-manager](002-nordvpn-openvpn-manager/) - NordVPN OpenVPN connection manager

- [004-comprehensive-feature-documentation](004-comprehensive-feature-documentation/) - Documentation for all major features (CCY, CCB, Nord, Speech-to-Text, etc.)

- [007-speech-to-text-resource-leak-fixes](007-speech-to-text-resource-leak-fixes/) - Fix microphone resource leak, transcription truncation, and browser paste failures

- [009-claude-devtools](009-claude-devtools/) - Install and integrate claude-devtools session visualiser (implementation committed, pending host deployment and testing)

- [011-claude-devtools](011-claude-devtools/) - claude-devtools (ccdt) installation plan (supersedes 009)

- [013-claude-devtools](013-claude-devtools/) - claude-devtools (ccdt) installation plan (latest iteration)

- [014-whisper-model-manager](014-whisper-model-manager/) - Replace cluttered model dropdown with a dedicated Textual TUI (`wsi-model-manager`) for browsing and downloading Whisper models

- [023-hostname-based-inventory](023-hostname-based-inventory/) - Migrate Ansible inventory from hardcoded `localhost` to machine hostname, supporting per-machine host_vars and multiple laptops

- [025-ccy-spring-cleaning](025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues

- [026-repo-spring-cleaning](026-repo-spring-cleaning/) - Repository-wide spring cleaning (non-CCY): remove tracked backups, fix bash scripts (set -e, shellcheck), fix Ansible playbooks (duplicate shebangs, curl-to-bash, state:latest)

- [027-contextual-shell-history](027-contextual-shell-history/) - Replace bash history with Atuin for directory/git-workspace-aware command recall

- [028-fedora-screen-sharing](028-fedora-screen-sharing/) - Diagnose and fix unstable screen sharing on Fedora 43 GNOME (Slack desktop broken by `app.asar` hardcode; Meet freezes traced to mutter ScreenCast bugs fixed in 49.3/49.5)

- [029-rapid-raw-cloud-ai](029-rapid-raw-cloud-ai/) - Evaluate cloud GPU paths for RapidRAW Tier 2 generative AI: free local-first verification (dGPU + Tier 1), local SD 1.5 prototype, vast.ai $10-credit prototype with SDXL/Flux Fill, then evidence-based decision gate before any productionisation

- [030-phpantom-lsp](030-phpantom-lsp/) - Research PHPantom (Rust-based PHP LSP) as replacement for Intelephense; decision gate before implementation

- [031-reliable-screen-sharing](031-reliable-screen-sharing/) - Find reliable screen-sharing alternatives for WFH devs on Fedora 43 Wayland (complements Plan 028); 4 parallel research tracks: self-hosted platforms, native Linux tools, current SaaS, unconventional approaches

- [032-compression-helpers](032-compression-helpers/) - `compress` / `uncompress` CLI wrappers around `ouch`: xz by default, `--zip` flag, auto-detect on decompress, always-extract-into-folder (tarbomb protection)

- [034-localhost-config-account](034-localhost-config-account/) - Track config-owning GitHub account in `localhost.yml` instead of relying on volatile `gh api user` (config repo lookup was driven by active gh default)

- [00035-gh-multi-account-hardening](00035-gh-multi-account-hardening/) - Harden fresh-install flow (gh multi-account first, then SSH keys); fix SSH probe fallback bug in playbook and ccy; replace manual paste with `gh ssh-key add`; research signed commits

- [00037-image-watermarking-toolkit](00037-image-watermarking-toolkit/) - Composable `watermark` CLI primitive: ImageMagick two-layer visible mark (corner + faint diagonal tile) plus full EXIF/IPTC/XMP licence metadata; idempotent via filename suffix and XMP sentinel; delivered via optional Ansible playbook; wrappable by client projects via config precedence chain and named profiles

- [00038-musiccast-controller](00038-musiccast-controller/) - MusicCast controller for Linux desktop with full UX (now-playing + Qobuz browse/search/play); 5 parallel research tracks complete (YXC API surface, OSS landscape, HA integration, Qobuz architecture, stack options); live-confirmed Qobuz is native on the WXA-50 via YXC; decision gate recommends Python + Textual + `aiomusiccast` with parallel KsanStone-fork experiment

- [00039-ftp-camera-viewer-tui](00039-ftp-camera-viewer-tui/) - Extend `ftp-camera` with orthogonal `--view` / `--view-jpg` modifier flags that compose with any FTP-server mode (default / `--async` / `--async-copy`); class-filtered live single-window preview via geeqie's implicit single-instance; pre-warm only in sort modes; two-step `gum choose` TUI for argument-free invocation; structured startup confirmation banner; ready to implement

- [00040-raw-clipping-scanner](00040-raw-clipping-scanner/) - Standalone `clip-scan` CLI: pre-Lightroom-import bulk flagger that decodes Sony ARW via `rawpy`, computes a weighted clipping score per side (linear ramp from cutoff to saturation; pixels closer to extremes weighted higher), renames files with `.wclip` / `.bclip` sub-extensions when score exceeds threshold; iterated through binary count → two-axis cutoff+count → weighted score (Mertens 2007 well-exposedness prior art); awaiting decision-gate confirmation and subagent review

- [00041-remote-desktop-quick-toggle](00041-remote-desktop-quick-toggle/) - One-click GNOME quick-settings toggle for `gnome-remote-desktop` Desktop Sharing on F43 Wayland: `rdt` CLI + quick-settings extension, LAN-scoped (user-configured CIDR) non-persistent firewalld rich-rule, off by default and after reboot, RDP creds in GNOME Keyring (not the repo); rejected NoMachine/AnyDesk (user reports prior instability) and Remote Login mode (separate session not wanted); lock-screen-but-show-real-desktop documented as impossible in mirror-mode RDP — workaround is lid-close as privacy

- [00046-localhost-yml-leak-guard](00046-localhost-yml-leak-guard/) - Project-level PreToolUse hooks-daemon handler that blocks `gh issue/pr/gist (create|edit|comment)` and HTTP-POST commands (`curl -d`, `wget --post-*`) when the command body contains any token from a deny-list dynamically derived from `localhost.yml`. Allowlist of public-by-design tokens (`joseph`, `LongTermSupport`, …) lives in `.claude/public-token-allowlist.yml`. Closes the safeguard gap surfaced by a recent leak into the public fedora-desktop issue tracker via `gh issue create`.

- [00045-project-personas-multi-tool-accounts](00045-project-personas-multi-tool-accounts/) - Generalise the gh `github_accounts` + per-alias-bash-function pattern into a top-level `project_personas` map in `localhost.yml` driving multiple tools (gh today + wrangler next; npm/aws/gcloud/etc. in follow-ups). KISS migration: gh playbook reads `project_personas` directly and fail-fasts on legacy schema with copy-pasteable migration YAML — no compat shim. Wrangler uses explicit env-var injection per `wrangler-<alias>` call; API tokens stored in GNOME Keyring via `secret-tool`, never on disk in plaintext. Awaiting Phase 1 decision gate.

- [00047-claude-code-mouse-wheel-pageup](00047-claude-code-mouse-wheel-pageup/) - Claude Code's fullscreen-rendering mode (in-process alt-screen, not tmux) combined with CCY's `CLAUDE_CODE_DISABLE_MOUSE=1` (preserves native click-drag selection) leaves the terminal's DECSET-1007 fallback in charge of the wheel; on alt-screen the wheel emits arrow-up/down which the CC prompt reads as history recall, clobbering input. Path C+ chosen: deploy per-emulator wheel→PageUp config (kitty, alacritty, wezterm) via Ansible AND add a runtime `terminal_preflight_check` in `claude-yolo` with gum-styled abort banner for unsupported terminals (ghostty, VTE/gnome-terminal/Ptyxis, konsole, foot). Ghostty initially recommended as "best" but corrected after upstream-docs verification: ghostty has no mouse-binding config in v1.x; **kitty is the new recommendation**. Implementation ready (lib/terminal-detection.bash + claude-yolo version bump to v3.15.0).

- [00042-darktable-ai-features](00042-darktable-ai-features/) - Enable darktable's optional AI features (object masks, denoise, upscale) on Fedora 43; research found Fedora RPM and Flathub Flatpak are both built WITHOUT `-DUSE_AI=ON`. Recommended path: local source-built RPM via `mock` (cmake auto-downloads ONNX Runtime; one-flag delta from Fedora spec; A7V cameras.xml becomes a build-time patch). Fallback: upstream AppImage as `darktable-ai`. Optional GPU acceleration playbook gated by `lspci` vendor ID `10de` for safe multi-laptop deploy. Awaiting Phase 1 decision gate. (Renumbered from local 00039 on merge — collided with remote 00039 ftp-camera-viewer-tui.)

- [00048-cc-token-source-parity](00048-cc-token-source-parity/) - Supersedes cancelled Plan 00036: extend host `cc` to use ccy's named-token chooser (shared pool at `~/.claude-tokens/ccy/tokens/`) via a new wrapper script at `/var/local/claude-code/cc` that sources the existing `token-management.bash` library. Adds a "Desktop" pseudo-option to the chooser meaning "use host `~/.claude/` OAuth" (current `cc` behaviour, now explicit); empty-pool path falls through to Desktop with inline instructions to create tokens via `ccy --create-token`. Phase 1 also factors a new `common-pure.bash` so the host wrapper can source helpers without triggering `common.bash`'s podman-check `exit 1`. No container required for `cc` (creation stays ccy-only). Not blocked by Plan 00047 — does not export `CLAUDE_CODE_DISABLE_MOUSE=1`.

- [00049-full-repo-audit](00049-full-repo-audit/) - Full repository audit via dynamic multi-agent workflow: 10 audit dimensions (security, fail-fast, Ansible, bash, CCY, extensions, performance, docs drift, opportunities, QA gaps) with adversarial verification of critical/high findings; research docs + triage.md + final action plan in the plan folder

- [00050-fedora-44-tracking](00050-fedora-44-tracking/)

- [00051-ansible-lint-improvement](00051-ansible-lint-improvement/) - Systematic ansible-lint compliance improvement: `scripts/lint` tooling, FQCN enforcement, and per-rule violation fixes across all 37 playbooks - Fedora 43 → 44 migration tracking (research phase only — no fixes). Dynamic Fable workflow swept six version-sensitivity dimensions (version literals, packages/repos+DNF, Python, GNOME extensions, hardware/kernel, install/bootstrap) against the live F44 changeset. 55 findings (7 high, 14 medium, 25 low, 9 info) in research/ + triage.md. Confirmed F44 baseline: GNOME 50, kernel 7.0.x, Python stays 3.14, DNF5 complete. The bump's core is one line (`fedora_version: 44`) but seven highs gate it; execution deliberately deferred to a future decision gate.

- [00053-fedora-44-fresh-install-audit](00053-fedora-44-fresh-install-audit/) - First fresh-F44 host audit using the new `playbooks/dev/play-collect-diagnostics.yml` collector. Splits findings into generic (NetworkManager-wait-online failure, firewalld⇄docker NAME_CONFLICT, gkr-pam at GDM login, intel-lpmd noise, dnf-makecache boot cost), hardware-specific (ThinkPad DYTC thermal mask, NVIDIA RTX 500 Ada Optimus on Meteor Lake, bluetoothd hci0, slow TPM/serial discovery), dev-tooling fixes for the collector itself (`lsblk -fO` flag clash, stale cached timestamp, `powerprofilesctl` probe misreads F44's tuned-ppd backend, rc=127 "tool absent" noise), and a firmware-update workflow decision (Intel ME multi-CVE behind). Decision recorded: F44's TuneD + tuned-ppd is the supported Power Mode backend; no `power-profiles-daemon` install.

## Completed Plans

- [00052-run-bash-human-friendly](Completed/00052-run-bash-human-friendly/) - Made `run.bash` totally human-friendly (v1.5.4 → 1.6.1): Enter accepts at every confirm (fixed the `promptForValue` "Is this correct? (y/n)" bug that forced a full re-type on Enter), safe-polarity defaults (`(Y/n)` benign / `(y/N)` for reboot+public-issue+untested-playbooks), visible `[default]` on every prompt, all prompts on the shared helper family, friendlier error messaging, and verify-before-write hardening of the vault-password recovery path with an `abort` escape hatch. Opus implementation + adversarial Fable review (caught a raw-escape-render defect on the headline prompt + two interrupt-safety nits). QA green (285 files), smoke 22/22. HOST-only live run (Task 6.4) deferred to the user.

- [020-semgrep-custom-bash-rules](Completed/020-semgrep-custom-bash-rules/) - Add Semgrep with custom bash convention rules (no error hiding, fail-fast enforcement) integrated into qa-all.bash. Semgrep 1.153.1 installed via pipx in CCY Dockerfile (v2.10); 0 violations in 44 bash files.

- [024-claude-md-modular-restructure](Completed/024-claude-md-modular-restructure/) - Restructure monolithic CLAUDE.md (40k+ chars) into modular architecture: lean front page + CLAUDE/ topic files + docs/ for user content. All CLAUDE/ topic files created and @ pointers in place.

- [033-ddev-installation](Completed/033-ddev-installation/) - Install DDEV on rootful Docker (Approach C); rootless Podman remains default engine, LXC unchanged. End-to-end host run verified (`ddev v1.25.1` + `docker 29.4.0`).

- [00043-ipu6-webcam-fallout](Completed/00043-ipu6-webcam-fallout/) - Incident + recovery: `play-ipu6-webcam.yml` (commit e5d0e33) pulled `akmod-intel-ipu6` which dragged in a half-installed kernel 7.0.9 because the previous-minor `kernel` versionlock silently filtered the metapackage out of the depsolve while the unlocked sub-packages came in — leaving a "kernel" with no iwlwifi/btusb. IPU6 play rewritten to drop the akmod (mainline IPU6 is in-tree on F43+); recovery executed via new `play-AB-dnf-upgrade.yml` which now also auto-detects + cleans up future half-installs. Verified end-to-end on 7.0.9-105: WiFi, BT, camera all working.

- [00044-laptop-health-audit](Completed/00044-laptop-health-audit/) - Read-only audit (4 parallel sub-agents) of the daily-driver X1 Carbon Gen 10 i7-1260P, then IaC cross-check to separate real gaps from busywork. Net result: new `play-AB-dnf-upgrade.yml` (also recovers half-install state), `play-ZZ-repo-cleanup.yml` (orphan COPR removal), `play-laptop-thermal-diagnostics.yml` (mask thermald + install lm_sensors), `play-network-wait-tuning.yml` (cap NM-wait-online to 5s), `play-rclone.yml` extended with per-mount `MemoryHigh`/`MemoryMax`. Dropped as busywork: `tuned` profile (tuned-ppd auto-switches), WWAN modprobe blacklist (GNOME Settings is the toggle), warp-svc verbosity (no stable config surface). Established the "work WITH GNOME, not against it" principle.

## Cancelled Plans

- [012-fix-plugin-handlers](012-fix-plugin-handlers/) - Upstream bug in `claude-code-hooks-daemon`; bug report filed at `untracked/upstream-bug-report-plugin-handler-suffix.md`
- [00036-cc-ccy-parity](00036-cc-ccy-parity/) - Cancelled — superseded by Plan 00048. Was: bashrc include exporting `CLAUDE_CODE_NO_FLICKER` + `CLAUDE_CODE_DISABLE_MOUSE` on every host shell. Both vars independently obsoleted — NO_FLICKER dropped from ccy in commit a32c3d3 (Plan 00047 Path D), DISABLE_MOUSE now owned by Plan 00047. Per-invocation wrapper-script approach in Plan 00048 covers the remaining cc/ccy parity space.

## Archive

The `Archive/` directory contains legacy plans created before adopting the structured plan workflow:

- **ccb-browser-automation.md** - CCB browser automation implementation
- **ccyb.md** - CCY background service planning
- **speech-to-text.md** - Speech-to-text integration
- **workspace-names-overview.md** - Workspace naming conventions

These are preserved for reference but don't follow the current plan structure.

## Creating New Plans

See [PlanWorkflow.md](../PlanWorkflow.md) for complete instructions.

**Quick start:**

```bash
# Create new plan directory
mkdir -p CLAUDE/Plan/001-my-feature

# Copy plan template
cp CLAUDE/Plan/templates/PLAN-template.md CLAUDE/Plan/001-my-feature/PLAN.md

# Edit plan with goals, approach, and tasks
vim CLAUDE/Plan/001-my-feature/PLAN.md
```

## Plan Workflow Integration

The hooks daemon enforces plan workflow standards:

- ✅ `validate_plan_number` - Ensures correct numbering format
- ✅ `plan_time_estimates` - Blocks time estimates in plans
- ✅ `plan_workflow` - Provides guidance when creating plans

## References

- [PlanWorkflow.md](../PlanWorkflow.md) - Complete plan workflow documentation
- [CLAUDE.md](../../CLAUDE.md) - Project-level Claude configuration
