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

- [00131-semgrep-or-true-rule-is-blind-to-the-enclosed-form](00131-semgrep-or-true-rule-is-blind-to-the-enclosed-form/) - The `|| true` rule is anchored to end-of-line, so `$( cmd || true )` is invisible to it — which is how two instances shipped. Widening it finds 18 live sites in 8 files; four are the git hooks that gate secret scanning for this public repo, where a wrong fix fails open. Carried from Plan 00122 Task 3.7.

- [00130-legacy-plan-scripts-lose-their-last-log-chunk](00130-legacy-plan-scripts-lose-their-last-log-chunk/) - `PlanWorkflow.md` taught `exec > >(tee "$LOG") 2>&1` and a plan-local `logs/` tree, both forbidden by PlanScriptStandards R4. The doc is fixed; ten scripts in seven active plans, of 53 examined, were already written from it. A process substitution cannot be waited on, so a failing run can lose the chunk explaining why, and the gitignored `logs/` tree orphans on archival.

- [00128-qa-tool-abort-silences-thirty-gates](00128-qa-tool-abort-silences-thirty-gates/) - `qa-all.bash` exits 2 on a fresh clone before roughly thirty gates have run, and reports nothing about any of them, because one gate's dev-only ESLint dependency is absent. Two written-down positions conflict — `CLAUDE.md`'s missing-dependency rule against `qa-js.bash`'s deliberate dev-only comment — so the remedy is an owner decision. Measured and deferred by Plan 00125.

- [00127-docker-and-podman-inside-lxc](00127-docker-and-podman-inside-lxc/) - **Parked.** Docker inside this host's LXC system containers is not working and Podman inside LXC was never established. Triage against `play-docker-in-lxc-support.yml`, then fix in IaC. Distinct from issue #41, which is engine coexistence on the host.

- [00124-chrome-install-gpg-failure-on-upgraded-host](00124-chrome-install-gpg-failure-on-upgraded-host/) - `run.bash` stops at Chrome on a host upgraded from F41, and there were two causes stacked: dnf5 validating against a repo's own keys (so a package URL lands in keyless `@commandline`), and beneath it an imported key that rpm will never refresh because presence is judged by primary id, leaving the newer signing subkey absent.

- [00122-lxc-freeze-thaw-shared-with-podfreeze](00122-lxc-freeze-thaw-shared-with-podfreeze/) - `podfreeze` groups, previews and toggles Podman containers; LXC is a first-class engine here with no equivalent. Adds a sibling `lxcfreeze` — rootful, so a separate tool rather than a flag — on a shared library holding the menu, the derived verb and the dry run that are not engine-specific.

- [00121-run-log-secret-scrubber-and-token-scenario](00121-run-log-secret-scrubber-and-token-scenario/) - Secrets are scanned at the git boundary only, so runtime artefacts like VM transcripts go unchecked; builds a fail-closed scrubber and the opt-in `server-github-token` scenario that Plan 00063's Tasks 3.3 and 3.4 need.

- [00119-headless-github-ssh-443-input](00119-headless-github-ssh-443-input/) - Headless provisioning had no input for the always-on `ssh.github.com:443` route, so a box whose egress blocks port 22 could upload its GitHub key and then hang on every SSH use of it; `RUN_BASH_GITHUB_SSH_443=1` writes `github_ssh_over_443: true` into the fresh localhost.yml.

- [00118-ccy-selinux-enforcing-host](00118-ccy-selinux-enforcing-host/) - On an SELinux-enforcing host a ccy container cannot read the project it was handed (container_t vs user_home_t; desktops only worked because they are not enforcing); relabel the workspace `:z` and stage key files into a `:Z` tmpfs dir, decided from getenforce and the engine report.

- [00117-vmtest-acceptance-script-version-gate](00117-vmtest-acceptance-script-version-gate/) - An acceptance run copies its guest checker from the host's DEPLOYED copy, so a run certifies one commit with a checker from another: `20260914T100220Z` passed 16/16 with `deployed-extensions-active` expecting 1 of 9. Record the checker's version and fail a mismatch as a harness failure.

- [00116-ccy-deploy-keys-and-forwarded-agent](00116-ccy-deploy-keys-and-forwarded-agent/) - ccy only knows `~/.ssh/github_*` account keys, so a box provisioned with per-repo deploy keys and no GitHub account cannot run it; teach ccy the remote's ssh-config alias (deploy key) and a `--ssh-agent` forwarded from the operator's session.

- [00115-playbook-shebang-runs-through-run-bash](00115-playbook-shebang-runs-through-run-bash/) - Every play's shebang hands it to run.bash, so `./playbooks/…/play-x.yml` works on password sudo; run.bash calls ansible-playbook explicitly so it never re-enters itself.

- [00114-run-bash-single-play-mode](00114-run-bash-single-play-mode/) - Running one play by hand dies on password sudo (`sudo: a password is required`) because nothing tells Ansible to prompt; expose run.bash's proven become logic as `./run.bash <playbook>` and make every doc name it.

- [00113-ccy-ci-runner-implementation](00113-ccy-ci-runner-implementation/) - Build the non-interactive `ccy` Plan 00068 specified: close every prompt site on the CI path, propagate the container's exit status rather than the compose block's, and impose the per-event tool surface with four startup assertions that can each actually fail.

- [00112-gnome-extensions-enabled-state-declared](00112-gnome-extensions-enabled-state-declared/) - Plan 00110's desktop scenario found a fresh install leaves every deployed GNOME extension INITIALIZED and none enabled (the enable races the shell's scan and its failure is hidden); make the enabled list declared, idempotent gsettings state and let the desktop scenario certify it.

- [00109-desktop-drift-detection-and-fedora-desktop-panel](00109-desktop-drift-detection-and-fedora-desktop-panel/) - A reboot into kernel 7.2.4 killed both DisplayLink monitors while every QA gate stayed green; add the missing drift axes (repo pin vs installed, play-at-last-run vs play-at-HEAD), surface breakage at login with a Claude Code handoff, and front it all with one `fedora-desktop` GNOME panel

- [00104-suspend-aborts-on-dock-unplug-never-resuspends](00104-suspend-aborts-on-dock-unplug-never-resuspends/) - Unplugging the dock aborts s2idle and nothing re-suspends, so the closed laptop runs hot in a bag; make the suspend request durable (udev wakeup policy, a re-suspend hook, battery idle-suspend as backstop)

- [00093-ccy-version-gate-covers-two-files-of-eight](00093-ccy-version-gate-covers-two-files-of-eight/) - The pre-commit CCY bump gate misses `entrypoint.sh`, the Dockerfile and all of `files/opt/claude-yolo/`; widen it and test it

- [00092-ccy-child-claude-spawn-mode](00092-ccy-child-claude-spawn-mode/) - Opt-in `ccy.env` mode letting a CCY session spawn authenticated child `claude` processes, with no new token exposure

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

- [00039-ftp-camera-viewer-tui](00039-ftp-camera-viewer-tui/) - Extend `ftp-camera` with orthogonal `--view` / `--view-jpg` modifier flags that compose with any FTP-server mode (default / `--async` / `--async-copy`); class-filtered live single-window preview via geeqie's implicit single-instance; pre-warm only in sort modes; two-step `gum choose` TUI for argument-free invocation; structured startup confirmation banner; code shipped, Dormant pending the HOST manual test matrix

- [00040-raw-clipping-scanner](00040-raw-clipping-scanner/) - Standalone `clip-scan` CLI: flags Sony ARW files whose highlights or shadows clip, by weighted per-side score, renaming them `.wclip` / `.bclip` before Lightroom import. Tool, playbook and core tests shipped; host probes and integration tests open.

- [00041-remote-desktop-quick-toggle](00041-remote-desktop-quick-toggle/) - One-click GNOME quick-settings toggle for `gnome-remote-desktop` on Wayland: `rdt` CLI plus extension, LAN-scoped non-persistent firewalld rule, off by default and after reboot.

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

- [00098-encrypted-claude-transcripts-at-rest](00098-encrypted-claude-transcripts-at-rest/) - Claude Code writes plaintext transcripts at well-known paths — inside the repo working tree for CCY. Research inverted the design: blast-radius reduction first (permissions, retention, backup exclusion), live-state encryption gated on evidence that did not arrive.

- [00099-rclone-rc-auth-broke-unmigrated-clients](00099-rclone-rc-auth-broke-unmigrated-clients/) - Plan 00094 authenticated the rclone RC on a false premise, so three unmigrated clients got HTTP 401 and reported it as a dead mount for a week. One sourced credential library, every client migrated, plus the new `qa-deployed-drift.bash` gate. Awaiting a host re-deploy — the closing review changed deployed files, so the last host run no longer describes this build.

- [00075-fail-signal-discard-sweep-and-gate](00075-fail-signal-discard-sweep-and-gate/) - Sweeps repo-owned bash, Python and playbooks for one defect class — a command's failure silently converted into data and then trusted — and builds a gate that fails the build rather than advising.

- [00129-semgrep-per-rule-coverage-is-invisible](00129-semgrep-per-rule-coverage-is-invisible/) - The pattern gate's `N files OK` is the union of every rule's target set and reads as per-rule coverage; one rule is blind to 67 of 157 files. Carried out of Plan 00076, which met all twelve of its own criteria without it. One owner decision left: ~4.9× scan time for measured numbers, or model the globs in-gate.

- [00079-podman-container-control](00079-podman-container-control/) - `podfreeze`: freeze and unfreeze Podman containers individually, as a CCY group, or by network, via `podman pause` — the one mechanism that works rootless. Renumbered from 00078 after two clones each handed out that number from a `--local` counter.

- [00080-ccy-session-network-isolation](00080-ccy-session-network-isolation/) - Every CCY session launched without `--network` joins the same Podman bridge. Research-gated into whether that matters, with five hypotheses of which H4 (can a per-session network be cleaned up after SIGKILL?) decides feasibility. May legitimately decide to change nothing.

- [00082-run-bash-github-accounts-none](00082-run-bash-github-accounts-none/) - Lets `run.bash` headless v1 provision with `RUN_BASH_GITHUB_ACCOUNTS=none`, which previously failed preflight as an unsupported follow-up. Of the two blockers Plan 00063 cited, one is confirmed fixed and the other is recorded NOT REPRODUCIBLE rather than asserted.

- [00086-kernel-modules-absent-enumeration](00086-kernel-modules-absent-enumeration/) - A downstream live proof of `play-AB-dnf-upgrade.yml` on a guest lacking the `kernel-modules` package (present only `kernel-core`/`kernel-modules-core`) found the half-installed-kernel enumeration hard-failed instead of treating "not installed" as zero versions. Fixed with a probe-then-fail `assert`, not a blanket `failed_when: false`.

- [00088-claude-code-state-dir-stale-home-fact](00088-claude-code-state-dir-stale-home-fact/) - `claude_state_dir` trusted a `HOME` fact that an early `become: true` play poisons to `/root`; now derived from `user_login` like the rest of the file

- [00089-ssh-handling-runner-token-guard](00089-ssh-handling-runner-token-guard/) - A carve-out from Plan 00068 (unmerged). Not a bug fix — `gh` already gives an exported `GH_TOKEN` precedence — but it makes that explicit and drops the `gh auth token` dependency for a runner authenticating purely by token via `--no-ssh`.

- [00069-plan-md-edited-in-place](00069-plan-md-edited-in-place/) - State in `PlanWorkflow.md` that `PLAN.md` is edited in place with git as its history, and narration belongs in `JOURNAL/`

- [00072-ccy-assert-rootless-engine](00072-ccy-assert-rootless-engine/) - `ccy` now asks the container engine whether it is rootless and refuses when the answer is no or unreadable; Dormant pending a HOST desktop check

- [00073-headless-sudo-password-file](00073-headless-sudo-password-file/) - Adds `RUN_BASH_SUDO_PASSWORD_FILE` as a second credential so headless `run.bash` works with ordinary password sudo; Dormant pending HOST verification

- [00074-grub-cgroup-check-reports-absence-it-cannot-prove](00074-grub-cgroup-check-reports-absence-it-cannot-prove/) - `run.bash`'s legacy-grub cgroup step now distinguishes a failing `grubby` from a genuine negative and aborts on a proven failure instead of continuing

## Completed Plans

- [00076-bash-gate-coverage-hole-nonexecutable-scripts](Completed/00076-bash-gate-coverage-hole-nonexecutable-scripts/) - `qa-all.bash` reported 125 bash files OK against 152 in the repo: the other 27 were never opened, having neither a shell extension nor an execute bit, and hid 34 gating findings. Discovery now keys on the shebang, with a coverage assertion behind it. Task 4.5, found while closing and outside every success criterion, became Plan 00129.

- [00125-ci-qa-gate-red-and-machine-dependent](Completed/00125-ci-qa-gate-red-and-machine-dependent/) - The `QA` workflow had been red for three weeks and `qa-all.bash` answered differently per machine; the docs gate now asks trackedness rather than existence, and a failing gate no longer aborts the suite.

- [00120-ccy-gpu-device-optional-on-headless-hosts](Completed/00120-ccy-gpu-device-optional-on-headless-hosts/) - ccy handed every container `--device /dev/dri` unconditionally, and on a host with no GPU (a headless server, a serial-console VM) podman aborted the session with `stat /dev/dri: no such file or directory`, exit 125; the flags are now a pure function of whether the node exists (ccy 3.56.0, PR #43).

- [00068-ccy-ci-runner-variant](Completed/00068-ccy-ci-runner-variant/) - Specifies what `ccy` owes a headless CI runner: the fail-fast contract at every prompt site, a per-event tool surface asserted at startup, and compose/networking kept as capability minus negotiation. No code here — Plan 00113 implements it.

- [00110-vm-lifecycle-acceptance-testing](Completed/00110-vm-lifecycle-acceptance-testing/) - Full fresh-install lifecycle acceptance testing against VMs for both the server and desktop profiles, off a reused base snapshot kept current by a TTL policy, triggerable from a CCY container through a closed-verb host-action bridge; its first desktop run found the extensions defect Plan 00112 carries.

- [00111-terminal-death-takes-all-ccy-sessions](Completed/00111-terminal-death-takes-all-ccy-sessions/) - A Wayland protocol error killed Ptyxis and took every in-flight CCY session with it (not OOM); `ccy` and `cc` now run inside a tmux server under `systemd --user`, one attach per session, with `ccy-sessions` and the `ccy` re-attach offer to get back in. CCY 3.53.3.

- [00081-secret-scanner-and-qa-gate-coverage-holes](Completed/00081-secret-scanner-and-qa-gate-coverage-holes/) - Seven more instances of the partial-result defect class, two in the pre-commit secret scanner on a public repo; every fix has a gate that fails against the unfixed code, each proved by re-introducing the defect.

- [00101-ccy-token-usage-via-ratelimit-headers](Completed/00101-ccy-token-usage-via-ratelimit-headers/) - Per-account 5-hour and weekly usage in `ccy`'s token menu, read from `/v1/messages` response headers behind a keypress. Shipped and deployed; the Fable allowance (Phase 6) is WON'T DO.

- [00108-headless-prompt-colour](Completed/00108-headless-prompt-colour/) - A headless run answered the prompt-colour question with nothing and left `PS1_COLOUR=` on the box; run.bash 1.19.0 takes `RUN_BASH_PS1_COLOUR` and the empty answer takes the default. Proven on a headless box following this branch.

- [00107-server-profile-never-grants-passwordless-sudo](Completed/00107-server-profile-never-grants-passwordless-sudo/) - The server profile declares `play-basic-configs.yml`'s passwordless-sudo block absent instead of granting it; the desktop profile is unchanged. run.bash 1.18.1 passes the become password to headless optional playbooks, a gap the grant had been masking. Proven live on a headless box.

- [00106-run-bash-git-ref-headless](Completed/00106-run-bash-git-ref-headless/) - `RUN_BASH_GIT_REF`: the headless provisioner checks out a declared branch (tracks its tip) or a 40-hex commit (pinned) instead of always the default branch; unresolvable refs abort. run.bash 1.18.0; proven on a real headless box.

- [00105-tmux-sessions-single-key-menu](Completed/00105-tmux-sessions-single-key-menu/) - tmux for detachable long-running dev sessions behind one key: F12 opens a new/rename/switch/detach/kill menu; status bar off. Deployed and verified on a headless box.

- [00103-slack-flatpak-rejects-md-dropped-from-nautilus](Completed/00103-slack-flatpak-rejects-md-dropped-from-nautilus/) - Slack Flatpak rejected a `.md` dragged from Nautilus: the sandbox only had `xdg-download`; `play-comms.yml` now grants `home:ro`, deployed and confirmed on the host

- [00102-dash-to-dock-does-not-dodge-ptyxis-terminal](Completed/00102-dash-to-dock-does-not-dodge-ptyxis-terminal/) - Dash to Dock stayed on top of an un-maximised Ptyxis window; `intellihide-mode` is now `ALL_WINDOWS` via `play-gnome-shell-extensions.yml`, deployed and accepted on the host

- [00091-podman-first-docker-optional](Completed/00091-podman-first-docker-optional/) - Demoted rootful Docker from core to an optional playbook (podman-first); merged via PR #42

- [00090-resync-ccy-ci-runner-branch-onto-f44](Completed/00090-resync-ccy-ci-runner-branch-onto-f44/) - Resynced the diverged Plan 00068 branch onto `F44` and landed it via [PR #39](https://github.com/LongTermSupport/fedora-desktop/pull/39)

- [00085-headless-path-local-bin](Completed/00085-headless-path-local-bin/) - A downstream live proof of the composed PR #33/#34 headless mechanisms found a third, unrelated blocker: `ansible-galaxy: command not found` under a non-interactive `sudo -u` invocation, since pipx's `~/.local/bin` shims are never put on PATH there. Exports PATH right after the pipx install block. Merged (`dac4f7c`).

- [00070-documentation-drift-audit](Completed/00070-documentation-drift-audit/) - Fixed 23 confirmed documentation-drift defects and shipped `scripts/qa-docs.bash` as a permanent link and catalogue gate

- [00071-qa-gate-correctness](Completed/00071-qa-gate-correctness/) - Fixed three defects that made `qa-all.bash` exit 1 on a clean tree, and pinned ruff via `/.ruff-version`

- [00067-qa-gates-inert-in-nested-checkout](Completed/00067-qa-gates-inert-in-nested-checkout/) - QA gates scanned nothing in a nested checkout; exclusions are now anchored to the repo root and each gate exits 2 on an empty file set

- [00060-stderr-hygiene-coding-standard](Completed/00060-stderr-hygiene-coding-standard/) - Fixed a `gh-<alias>()` wrapper that echoed status to stdout and shipped `CLAUDE/StderrHygiene.md` as the coding standard

- [00087-gitleaks-generic-key-false-positive](Completed/00087-gitleaks-generic-key-false-positive/) - Fixes a gitleaks CI false positive (`generic-api-key` on a "Medium/byteiota" source citation) by rephrasing the flagged text rather than growing `.gitleaks.toml`'s allowlist. Merged (`b15fc4d`).

- [00084-port-sudo-password-file-onto-f44](Completed/00084-port-sudo-password-file-onto-f44/) - Ports Plan 00073's `RUN_BASH_SUDO_PASSWORD_FILE` (stranded on an unmerged, diverged branch) onto `F44` so it composes with Plan 00082's `GITHUB_ACCOUNTS=none` — no single commit previously carried both. Merged (`d48fabd`). Also fixed two real VM hostnames from the downstream consumer estate that had been committed into this public repo's tracked content.

- [00083-plan-index-hygiene-and-comment-handlers](Completed/00083-plan-index-hygiene-and-comment-handlers/) - Enables the three handlers the 3.54.0 daemon upgrade shipped disabled (`comment_changelog`, `comment_size`, `sensitive_content`), each after measuring its existing backlog rather than assuming it, and clears the 39 over-length rows the new `index-row-length` check found in this index.

- [025-ccy-spring-cleaning](Completed/025-ccy-spring-cleaning/) - CCY codebase spring cleaning: fix 63 shellcheck warnings, remove 20 dead functions, fix double-sourcing, exit-vs-return, and code quality issues

- [00050-fedora-44-tracking](Completed/00050-fedora-44-tracking/) - Fedora 43 → 44 migration tracking, research only: 55 findings across six version-sensitivity dimensions. The bump's core is one line, but seven highs gate it; execution deferred to a decision gate.

- [00077-ansible-inject-facts-as-vars-deprecation](Completed/00077-ansible-inject-facts-as-vars-deprecation/) - Converts the 11 live `ansible_<fact>` references that ansible-core 2.24 removes, and closes the hole with `qa-ansible.bash` Check 5. COMPLETE — proven on the host, after the first acceptance run certified nothing because it set an env var that does not exist.

- [00078-ccy-network-preflight-skip](Completed/00078-ccy-network-preflight-skip/) - Adds `CCY_SKIP_NETWORK_PREFLIGHT=1` so `ccy` can launch on an egress-fenced host, where the unconditional alpine-pull-and-HTTP liveness probe cannot pass by design. CCY 3.39.0.

- [00097-lightweight-agent-browser-engine](Completed/00097-lightweight-agent-browser-engine/) - Adds Lightpanda 0.3.6 as a complementary lightweight engine, reached through the `--engine` flag `agent-browser` already had: 379 ms / ~25 MB against Chromium's 1177 ms / ~1345 MB at identical fidelity on eight JS fixtures. Chromium stays the default because Lightpanda fails silently outside its scope.

- [00094-rclone-rc-auth-instead-of-no-auth](Completed/00094-rclone-rc-auth-instead-of-no-auth/) - Replaces blanket `--rc-no-auth` on the rclone RC — equivalent to shell access as the rclone user, and reachable by every local uid — with a host-generated 0600 secret loaded via systemd `EnvironmentFile=`. ACCEPTED on the host, 11 passed / 0 failed.

**Older completed plans** — everything beyond the most recent 30 — are in
[Completed/README.md](Completed/README.md), moved there verbatim. The retention window
keeps this index readable; the archive keeps the record whole.

## Cancelled Plans

- [00042-darktable-ai-features](Cancelled/00042-darktable-ai-features/) - Enable darktable's AI features via a source-built RPM and the upstream nightly AppImage. CANCELLED: the pre-release path never worked reliably, and darktable 5.6.0+ (Fedora 44 ships 5.6.1) decodes the Sony A7V natively, so the local rebuild and cameras.xml overlay are no longer needed. The AI playbooks were removed; `play-photography.yml` now enforces the darktable version floor and cleans up the retired install.

- [00100-ccy-token-usage-limits](Cancelled/00100-ccy-token-usage-limits/) - Show per-account usage in `ccy`'s token menu. CANCELLED on host evidence: all four stored tokens return 403 on `/api/oauth/usage`, because a setup-token lacks the `user:profile` scope and nothing client-side can mint it. The figures are reachable as `/v1/messages` response headers instead — taken up by Plan 00074.

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
