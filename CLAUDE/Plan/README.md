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

- [00040-raw-clipping-scanner](00040-raw-clipping-scanner/) - Standalone `clip-scan` CLI: flags Sony ARW files whose highlights or shadows clip, by weighted per-side score, renaming them `.wclip` / `.bclip` before Lightroom import. Awaiting decision-gate confirmation.

- [00041-remote-desktop-quick-toggle](00041-remote-desktop-quick-toggle/) - One-click GNOME quick-settings toggle for `gnome-remote-desktop` on Wayland: `rdt` CLI plus extension, LAN-scoped non-persistent firewalld rule, off by default and after reboot.

- [00042-darktable-ai-features](00042-darktable-ai-features/) - Enable darktable's optional AI features (object masks, denoise, upscale), which neither the Fedora RPM nor the Flatpak builds in. The AI nightly is installed alongside the stable RPM and Phase 3 adds NVIDIA GPU acceleration; host deploy and CUDA-provider verification pending. (Renumbered from local 00039, which collided with remote 00039.)

- [00045-project-personas-multi-tool-accounts](00045-project-personas-multi-tool-accounts/) - Generalise the per-alias `github_accounts` pattern into a top-level `project_personas` map in `localhost.yml` driving multiple tools (gh today, wrangler next). Awaiting Phase 1 decision gate.

- [00046-localhost-yml-leak-guard](00046-localhost-yml-leak-guard/) - Project-level hooks-daemon handler blocking `gh issue/pr/gist` and HTTP-POST commands whose body carries a token derived from `localhost.yml` — the surface git hooks do not cover.

- [00048-cc-token-source-parity](00048-cc-token-source-parity/) - Supersedes cancelled Plan 00036: give host `cc` ccy's named-token chooser over the shared pool, with a "Desktop" pseudo-option meaning today's `~/.claude/` OAuth behaviour.

- [00049-full-repo-audit](00049-full-repo-audit/) - Full repository audit via dynamic multi-agent workflow: 10 audit dimensions (security, fail-fast, Ansible, bash, CCY, extensions, performance, docs drift, opportunities, QA gaps) with adversarial verification of critical/high findings; research docs + triage.md + final action plan in the plan folder

- [00051-ansible-lint-improvement](00051-ansible-lint-improvement/) - Systematic ansible-lint compliance improvement: `scripts/lint` tooling, FQCN enforcement, and per-rule violation fixes across all 37 playbooks

- [00053-fedora-44-fresh-install-audit](00053-fedora-44-fresh-install-audit/) - First fresh-F44 host audit using the new diagnostics collector, splitting findings into generic, hardware-specific, and defects in the collector itself. Records F44's TuneD + tuned-ppd as the supported Power Mode backend.

- [00054-github-ssh-443-host-level](00054-github-ssh-443-host-level/) - Unify GitHub SSH-over-443 behind one runtime signal (`GITHUB_SSH_443`) across host and CCY, adding the temporary host toggle that previously needed a full Ansible run.

- [00055-container-process-watchdog](00055-container-process-watchdog/) - Reporting-only host watchdog: a user timer attributes every long-running CPU-pinned process to its container and surfaces it via GNOME panel and CLI. CPU caps explicitly rejected as symptom-hiding.

- [00056-displaylink-dock-hotplug-recovery](00056-displaylink-dock-hotplug-recovery/) - Resurrects issue #28 (closed as a hardware fault, left reopenable): the DisplayLink dock unplug/replug wedges GNOME/mutter on Wayland. Research into whether recovery is possible without a logout.

- [00058-github-version-pin-updates](00058-github-version-pin-updates/) - Bumps every hardcoded upstream version pin the pinned-version checker found behind (nvm, markless, rescrobbled, ouch, RapidRAW, ART, DisplayLink/evdi), one commit per pin, with a plan-local `deploy.bash`.

- [00061-headless-server-provisioning](00061-headless-server-provisioning/) - Provision a headless Fedora Server from the same source tree as the desktop: a per-play `scope` taxonomy, a server entry point, and a QA gate that fails on any play mis-declaring its scope.

- [00062-disk-reclaim-tui](00062-disk-reclaim-tui/) - General-purpose disk-reclamation tooling: `play-disk-reclaim.yml` plus `reclaim`, a pure-bash confirm-first TUI for targeted cleanup. QA green; HOST deploy and live test pending.

- [00063-headless-run-bash-server-cloud-provisioning](00063-headless-run-bash-server-cloud-provisioning/) - Make `run.bash` provision a headless Fedora Server or Cloud box unattended, driven entirely by `RUN_BASH_*` env vars and failing fast by name when one is missing with no TTY. Depends on Plan 00061.

- [00064-open-command-universal-file-opener](00064-open-command-universal-file-opener/) - Adds `open` — one command for any file, directory or URL, supplying the two behaviours `xdg-open` and `mimeopen` lack: session awareness and a chooser when there is no default. HOST deploy pending.

- [00065-headless-server-cloud-base-blocker-fixes](00065-headless-server-cloud-base-blocker-fixes/) - Fixes what a first live headless run hit on minimal Fedora Cloud Base: three core plays abort the whole run and two leave a container host silently wrong. Declares the missing deps in IaC. HOST test pending.

- [00066-ftp-camera-airbnb-wifi-and-hotspot-triage](00066-ftp-camera-airbnb-wifi-and-hotspot-triage/) - Two `ftp-camera` failures on Airbnb WiFi — `--async-copy` stalling after the first frame (three live hypotheses; cause deliberately not asserted, `triage.bash` and `--debug-ftp` ship to discriminate them), and a `--hotspot` IaC gap where the play only tuned a profile a human had to create by hand. HOST triage pending.

- [00071-encrypted-claude-transcripts-at-rest](00071-encrypted-claude-transcripts-at-rest/) - Claude Code writes plaintext transcripts at well-known paths — inside the repo working tree for CCY. Research inverted the design: blast-radius reduction first (permissions, retention, backup exclusion), live-state encryption gated on evidence that did not arrive.

- [00072-rclone-rc-auth-broke-unmigrated-clients](00072-rclone-rc-auth-broke-unmigrated-clients/) - Plan 00067 authenticated the rclone RC on a false premise, so three unmigrated clients got HTTP 401 and reported it as a dead mount for a week. One sourced credential library, every client migrated, plus the new `qa-deployed-drift.bash` gate. ACCEPTED 8/8 on the host.

- [00074-ccy-token-usage-via-ratelimit-headers](00074-ccy-token-usage-via-ratelimit-headers/) - Successor to Plan 00073, taking the one route it left alive: usage figures travel as `/v1/messages` response headers. Deliberately a key the user presses, since reading them costs a billed request. Shipped and deployed; the utilisation scale is undocumented and stays behind `CCY_USAGE_SCALE`.

- [00075-fail-signal-discard-sweep-and-gate](00075-fail-signal-discard-sweep-and-gate/) - Sweeps repo-owned bash, Python and playbooks for one defect class — a command's failure silently converted into data and then trusted — and builds a gate that fails the build rather than advising.

- [00076-bash-gate-coverage-hole-nonexecutable-scripts](00076-bash-gate-coverage-hole-nonexecutable-scripts/) - `qa-all.bash` reported 125 bash files OK against 152 in the repo: the other 27 were never opened, having neither a shell extension nor an execute bit, and hid 34 gating findings. Discovery now keys on the shebang, with a coverage assertion behind it.

- [00079-podman-container-control](00079-podman-container-control/) - `podfreeze`: freeze and unfreeze Podman containers individually, as a CCY group, or by network, via `podman pause` — the one mechanism that works rootless. Renumbered from 00078 after two clones each handed out that number from a `--local` counter.

- [00080-ccy-session-network-isolation](00080-ccy-session-network-isolation/) - Every CCY session launched without `--network` joins the same Podman bridge. Research-gated into whether that matters, with five hypotheses of which H4 (can a per-session network be cleaned up after SIGKILL?) decides feasibility. May legitimately decide to change nothing.

- [00081-secret-scanner-and-qa-gate-coverage-holes](00081-secret-scanner-and-qa-gate-coverage-holes/) - Seven more instances of the partial-result defect class, two of them in the pre-commit secret scanner on a public repo: `--diff-filter=ACM` skipped staged renames entirely, and the email whitelist filtered whole lines rather than tokens. Remaining phases cover `qa-python.bash` and `qa-deployed-drift.bash`.

- [00082-run-bash-github-accounts-none](00082-run-bash-github-accounts-none/) - Lets `run.bash` headless v1 provision with `RUN_BASH_GITHUB_ACCOUNTS=none`, which previously failed preflight as an unsupported follow-up. Of the two blockers Plan 00063 cited, one is confirmed fixed and the other is recorded NOT REPRODUCIBLE rather than asserted.

- [00087-gitleaks-generic-key-false-positive](00087-gitleaks-generic-key-false-positive/) - Fixes a gitleaks CI false positive (`generic-api-key` on a "Medium/byteiota" source citation) by rephrasing the flagged text rather than growing `.gitleaks.toml`'s allowlist.

## Completed Plans

- [00085-headless-path-local-bin](Completed/00085-headless-path-local-bin/) - A downstream live proof of the composed PR #33/#34 headless mechanisms found a third, unrelated blocker: `ansible-galaxy: command not found` under a non-interactive `sudo -u` invocation, since pipx's `~/.local/bin` shims are never put on PATH there. Exports PATH right after the pipx install block. Merged (`dac4f7c`).

- [00084-port-sudo-password-file-onto-f44](Completed/00084-port-sudo-password-file-onto-f44/) - Ports Plan 00073's `RUN_BASH_SUDO_PASSWORD_FILE` (stranded on an unmerged, diverged branch) onto `F44` so it composes with Plan 00082's `GITHUB_ACCOUNTS=none` — no single commit previously carried both. Merged (`d48fabd`). Also fixed two real VM hostnames from the downstream consumer estate that had been committed into this public repo's tracked content.

- [00083-plan-index-hygiene-and-comment-handlers](Completed/00083-plan-index-hygiene-and-comment-handlers/) - Enables the three handlers the 3.54.0 daemon upgrade shipped disabled (`comment_changelog`, `comment_size`, `sensitive_content`), each after measuring its existing backlog rather than assuming it, and clears the 39 over-length rows the new `index-row-length` check found in this index.

- [025-ccy-spring-cleaning](Completed/025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues

- [00050-fedora-44-tracking](Completed/00050-fedora-44-tracking/) - Fedora 43 → 44 migration tracking, research only: 55 findings across six version-sensitivity dimensions. The bump's core is one line, but seven highs gate it; execution deferred to a decision gate.

- [00077-ansible-inject-facts-as-vars-deprecation](Completed/00077-ansible-inject-facts-as-vars-deprecation/) - Converts the 11 live `ansible_<fact>` references that ansible-core 2.24 removes, and closes the hole with `qa-ansible.bash` Check 5. COMPLETE — proven on the host, after the first acceptance run certified nothing because it set an env var that does not exist.

- [00078-ccy-network-preflight-skip](Completed/00078-ccy-network-preflight-skip/) - Adds `CCY_SKIP_NETWORK_PREFLIGHT=1` so `ccy` can launch on an egress-fenced host, where the unconditional alpine-pull-and-HTTP liveness probe cannot pass by design. CCY 3.39.0.

- [00070-lightweight-agent-browser-engine](Completed/00070-lightweight-agent-browser-engine/) - Adds Lightpanda 0.3.6 as a complementary lightweight engine, reached through the `--engine` flag `agent-browser` already had: 379 ms / ~25 MB against Chromium's 1177 ms / ~1345 MB at identical fidelity on eight JS fixtures. Chromium stays the default because Lightpanda fails silently outside its scope.

- [00067-rclone-rc-auth-instead-of-no-auth](Completed/00067-rclone-rc-auth-instead-of-no-auth/) - Replaces blanket `--rc-no-auth` on the rclone RC — equivalent to shell access as the rclone user, and reachable by every local uid — with a host-generated 0600 secret loaded via systemd `EnvironmentFile=`. ACCEPTED on the host, 11 passed / 0 failed.

- [00057-lxc-net-networkmanager-bridge-race](Completed/00057-lxc-net-networkmanager-bridge-race/) - `lxc-net` failed at boot because an NM autoconnect profile claimed `lxcbr0` first, so dnsmasq never launched and containers never leased — while the play's own bridge check false-passed. Verified on the host: triage 9 failures → 0.

- [00069-docs-drift-repo-wide-fix](Completed/00069-docs-drift-repo-wide-fix/) - Audited every doc under `docs/` plus the root README against the real playbooks: 36 factual defects fixed, dominated by core plays documented as optional, and including two features documented that no task implements.

- [00068-document-ccy-system](Completed/00068-document-ccy-system/) - Shipped `docs/ccy.md` — the repo's daily driver had no user-facing documentation at all. A 10-agent adversarial pass caught six high-severity defects in the first draft, an invented flag among them.

- [00060-stderr-hygiene-coding-standard](Completed/00060-stderr-hygiene-coding-standard/) - A generated `gh-<alias>()` wrapper printed its status line on stdout, breaking `$(… --json)` captures. Fixed, audited repo-wide (0 other real bugs), and shipped `CLAUDE/StderrHygiene.md` as a coding standard.

- [00047-claude-code-mouse-wheel-pageup](Completed/00047-claude-code-mouse-wheel-pageup/) - The wheel clobbered the prompt in Claude Code's alt-screen renderer under `CLAUDE_CODE_DISABLE_MOUSE=1`. Path E shipped: drop the var so Claude Code captures the mouse and scrolls natively. Container 2.22, CCY 3.27.0.

- [00059-plan-folder-cleanup-and-plan-qa](Completed/00059-plan-folder-cleanup-and-plan-qa/) - Made `plan_workflow.qa` explicit in `.claude/hooks-daemon.yaml` and resolved the pre-existing plan-tree drift its first sweep surfaced (missing index rows, completed/cancelled plans left in the active root, missing status headers, a lowercase `plan.md`); `plan-qa --sweep` went 16 findings → 0.

- [002-nordvpn-openvpn-manager](Completed/002-nordvpn-openvpn-manager/) - `nord` bash script + Ansible playbook to manage NordVPN OpenVPN connections via NetworkManager (on-demand import, persistent connections, vault credentials). Shipped `files/home/.local/bin/nord`, `play-nordvpn-openvpn.yml`, and `docs/nordvpn-installation.md`.

- [006-documentation-audit-and-update](Completed/006-documentation-audit-and-update/) - Documentation coverage audit and feature inventory across the repo (coverage assessment + feature inventory supporting docs).

- [015-article-mode](Completed/015-article-mode/) - Article mode for speech-to-text: an indefinite looped recording mode that flushes every 120 s and re-polishes the whole raw article via Claude in a two-pane GTK window (`Shift+Insert`). Shipped `files/home/.local/bin/wsi-article` + `wsi-article-window`.

- [017-merge-ccy-ccb](Completed/017-merge-ccy-ccb/) - Retire CCB and CCB-Browser and consolidate into the single CCY tool.

- [021-firstboot-wizard-redesign](Completed/021-firstboot-wizard-redesign/) - Firstboot wizard redesign.

- [00052-run-bash-human-friendly](Completed/00052-run-bash-human-friendly/) - Made `run.bash` human-friendly (1.5.4 → 1.6.1): Enter accepts at every confirm, safe-polarity defaults, a visible `[default]` on every prompt, and verify-before-write hardening of the vault-password recovery path. HOST live run deferred to the user.

- [020-semgrep-custom-bash-rules](Completed/020-semgrep-custom-bash-rules/) - Add Semgrep with custom bash convention rules (no error hiding, fail-fast enforcement) integrated into qa-all.bash. Semgrep 1.153.1 installed via pipx in CCY Dockerfile (v2.10); 0 violations in 44 bash files.

- [024-claude-md-modular-restructure](Completed/024-claude-md-modular-restructure/) - Restructure monolithic CLAUDE.md (40k+ chars) into modular architecture: lean front page + CLAUDE/ topic files + docs/ for user content. All CLAUDE/ topic files created and @ pointers in place.

- [033-ddev-installation](Completed/033-ddev-installation/) - Install DDEV on rootful Docker (Approach C); rootless Podman remains default engine, LXC unchanged. End-to-end host run verified (`ddev v1.25.1` + `docker 29.4.0`).

- [00043-ipu6-webcam-fallout](Completed/00043-ipu6-webcam-fallout/) - Incident and recovery: the IPU6 play pulled an akmod that dragged in a half-installed kernel with no iwlwifi or btusb. Play rewritten to drop the akmod; recovery via the new `play-AB-dnf-upgrade.yml`, which also cleans up future half-installs.

- [00044-laptop-health-audit](Completed/00044-laptop-health-audit/) - Read-only audit of the daily-driver X1 Carbon, cross-checked against IaC to separate real gaps from busywork: five new or extended plays shipped, three items dropped. Established the "work WITH GNOME, not against it" principle.

## Cancelled Plans

- [00073-ccy-token-usage-limits](Cancelled/00073-ccy-token-usage-limits/) - Show per-account usage in `ccy`'s token menu. CANCELLED on host evidence: all four stored tokens return 403 on `/api/oauth/usage`, because a setup-token lacks the `user:profile` scope and nothing client-side can mint it. The figures are reachable as `/v1/messages` response headers instead — taken up by Plan 00074.

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

- ✅ `plan_number_helper` - Directs plan creation through `mkplan.bash` and the authoritative git counter (replaced `validate_plan_number`, removed in daemon 3.53.0)
- ✅ `plan_time_estimates` - Blocks time estimates in plans
- ✅ `plan_workflow` - Provides guidance when creating plans
- ✅ `plan_qa_edit` / `plan_qa_commit_gate` / `plan_qa_sweep` - Lint plan-tree drift (status headers, row↔folder bijection, archive placement) at edit, commit, and session-start; policy under `plan_workflow.qa` in `.claude/hooks-daemon.yaml`

## References

- [PlanWorkflow.md](../PlanWorkflow.md) - Complete plan workflow documentation
- [CLAUDE.md](../../CLAUDE.md) - Project-level Claude configuration
