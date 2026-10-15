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

- [00196-team-coordinator-role](00196-team-coordinator-role/) - In Progress. One agent per team is the coordinator: copied on what workers tell humans, addressable with `--to-coordinator`, set from `ccy-teams` and bare `ccy --teams`

- [00197-agent-bus-seat-broker-daemon](00197-agent-bus-seat-broker-daemon/) - Not Started. A host daemon that grants team seats on the owner's approval, so `ccy --teams` inside an LXC container joins with no config edit

- [00198-gnome-text-editor-opens-a-new-window-on-the-current-workspace](00198-gnome-text-editor-opens-a-new-window-on-the-current-workspace/) - In Progress. Opening a file from Nautilus opens GNOME Text Editor in a new window on the current workspace, not as a tab on another workspace

- [00199-quick-markdown-viewer](00199-quick-markdown-viewer/) - In Progress. A desktop viewer that renders a Markdown file nicely and opens quickly; adopt one or build one

- [00190-docker-podman-coexistence-on-host](00190-docker-podman-coexistence-on-host/) - Not Started. The Docker/Podman coexistence concerns left on #41 after Plan 00091

- [00189-ouch-download-sha256-pin](00189-ouch-download-sha256-pin/) - Not Started. The ouch tarball is checked against a pinned sha256 (#63)

- [00188-run-bash-rerun-noise](00188-run-bash-rerun-noise/) - Not Started. A `run.bash` re-run injects only missing pipx packages, with no warnings (#54)

- [00187-stt-panel-robot-face](00187-stt-panel-robot-face/) - Not Started. The speech-to-text panel shows 🤖 or 💬 while Claude post-processes (#100)

- [00186-lxc-claude-session-reboot-warning](00186-lxc-claude-session-reboot-warning/) - Not Started (low priority). Claude sessions inside LXC containers get the reboot warning and restore (#55)

- [00185-terminal-emoji-glyphs](00185-terminal-emoji-glyphs/) - Not Started. Triage why the terminal draws the wrong emoji, then defend in a play (#60)

- [00184-deployed-tools-menu](00184-deployed-tools-menu/) - Not Started (low priority). One launcher and panel item for the deployed user tools (#81)

- [00183-prompt-shows-stale-tracking-ref](00183-prompt-shows-stale-tracking-ref/) - Not Started. The git prompt never shows up-to-date against a stale tracking ref (#52)

- [00182-run-bash-private-plays-and-config-clone](00182-run-bash-private-plays-and-config-clone/) - Not Started. `run.bash` runs private plays from a local clone of the config repo (#49, #97)

- [00181-ccy-session-limits](00181-ccy-session-limits/) - Not Started. ccy memory limits and admission by free RAM (#67), and #61's remaining host checks

- [00180-desktop-only-pr-merge-gate](00180-desktop-only-pr-merge-gate/) - Not Started. A written, scope-picked QA gate for PRs merged from a desktop (#94)

- [00179-stt-recording-batch-and-engine-research](00179-stt-recording-batch-and-engine-research/) - Not Started. Batch transcription of long recordings, and the engine and CPU-path decisions (#98, #11, #9, #84)

- [00191-go-cli-tooling](00191-go-cli-tooling/) - In Progress (high priority). This repo's large interactive tools leave bash for a Go module under `src-go/`: `ccy-teams` first, then `ccy-sessions`, then possibly `ccy`; and a new `go-qa-ci` repository, sibling of rust-qa-ci, that its QA runs

- [00195-slack-reads-lxc-vhosts-through-project-symlinks](00195-slack-reads-lxc-vhosts-through-project-symlinks/) - In Progress (medium priority). Slack Flatpak granted `/var/lib/lxc:ro` so a file dragged from a folder symlinked into an LXC container uploads; follow-up to 00103

- [00194-agent-bus-live-delivery-supervisor-plugin](00194-agent-bus-live-delivery-supervisor-plugin/) - In Progress (high priority). Agent bus messages delivered live into a ccy team session by a ccy supervisor plugin, with human and agent text kept distinct

- [00193-coral-adoption-audit](00193-coral-adoption-audit/) - In Progress (medium priority). Audit of the external agent-supervision project coral against ccy, ccy-sessions, ccy-teams, the agent bus and the hooks daemon: fit, how opinionated it is, adopt, fork, borrow or leave

- [00192-bash-line-in-the-sand-go-for-tooling](00192-bash-line-in-the-sand-go-for-tooling/) - In Progress (high priority). No more tooling in bash: the limits in `CLAUDE.md`, a Go gate with a ratchet allowlist, project DBF rules for `src-go/`, and the bash (then standalone Python) tools rewritten in Go, one feature branch per programme

- [00178-agent-team-history-export](00178-agent-team-history-export/) - Not Started (low priority). Export an agent team's room history to JSON and Markdown for archiving, from `ccy-teams`

- [00177-ccy-teams-human-friendly](00177-ccy-teams-human-friendly/) - Blocked on the owner (host run, first use). `ccy-teams` made human friendly: honest failures (a refused sudo is not "no teams"), an offer to create a team when there is none, clear prompts, colour

- [00176-dash-to-dock-stays-up-over-windows-again](00176-dash-to-dock-stays-up-over-windows-again/) - Blocked on the owner (five observations at the screen). Dash to Dock stays up over Ptyxis again; the answers decide the fix.

- [00174-polyglot-tooling-folders-with-qa-ci](00174-polyglot-tooling-folders-with-qa-ci/) - Not Started. One tooling folder per language, each with its own QA pipeline; first consumer is Plan 00155's Rust mapper.

- [00173-shutdown-with-update-always-goes-down](00173-shutdown-with-update-always-goes-down/) - Blocked on the owner (host deploy, evening test, failed-dnf question). On a desktop, `shutdown-with-update` always updates and powers off; ccy sessions with no container never block it.

- [00172-bluetooth-headset-mic-for-dictation](00172-bluetooth-headset-mic-for-dictation/) - In Progress (triage done). A Bluetooth headset chosen as input still left dictation on the laptop mic; fix in `play-hd-audio.yml`.

- [00171-stt-insert-never-ignored-icon-in-sync](00171-stt-insert-never-ignored-icon-in-sync/) - In Progress (built; host deploy and acceptance pending). Insert is never swallowed and the stt icon stays in sync, via session-tokened signals.

- [00170-unifi-network-tooling](00170-unifi-network-tooling/) - In Progress. A vaulted UniFi controller login, a read-only API client and a DSCP probe.

- [00169-ccy-image-keeps-its-tools-current](00169-ccy-image-keeps-its-tools-current/) - In Progress (Phase 1 accepted on the host; Phase 3 next). The ccy image keeps its tools current; ccy refuses a git that cannot read the repo.

- [00168-managed-config-files-changed-out-of-band](00168-managed-config-files-changed-out-of-band/) - Not Started. Report play-managed files changed since their play ran. Issue #91.

- [00167-cc-refuses-to-start-outside-a-repo](00167-cc-refuses-to-start-outside-a-repo/) - Not Started. `cc` outside a git working tree offers the checkout or refuses. Issue #90.

- [00165-ccy-container-uses-host-time-zone](00165-ccy-container-uses-host-time-zone/) - In Progress (owner's clock check left). ccy containers run in the host's time zone through `TZ`.

- [00161-agent-team-bus-matrix](00161-agent-team-bus-matrix/) - Implements #59: a private Matrix homeserver per team, the `pingbus` CLI (pings point at committed artefacts, never free text), a warden for human `!` commands, hooks that wake idle sessions, ccy opt-in, Element profiles.

- [00160-ccy-env-local-override](00160-ccy-env-local-override/) - In Progress (owner check left). ccy sources an untracked `ccy.env.local` after `ccy.env`, from a tracked `.dist` template.

- [00159-speech-to-text-vocabulary](00159-speech-to-text-vocabulary/) - Not Started. A personal vocabulary primes Whisper and a "heard => meant" list corrects its text.

- [00158-ccy-sessions-take-over](00158-ccy-sessions-take-over/) - In Progress (two tasks left). `ccy-sessions` Ctrl-T takes over a session open in another terminal.

- [00156-speech-to-text-auto-model-never-downloaded](00156-speech-to-text-auto-model-never-downloaded/) - In Progress (host verification left). The `auto` model was never downloaded, a regression from Plan 00148.

- [00155-headset-button-toggles-speech-to-text](00155-headset-button-toggles-speech-to-text/) - In Progress (design settled; needs Plan 00174). A headset button toggles speech-to-text through a gesture-to-key mapper.

- [00154-ccy-sessions-freeze-and-thaw](00154-ccy-sessions-freeze-and-thaw/) - Not Started. Freeze and thaw ccy sessions from the `ccy-sessions` picker through `podfreeze`.

- [00153-release-tags-fedora-major-semver](00153-release-tags-fedora-major-semver/) - In Progress (host acceptance, then the first tag). Signed `44.MINOR.PATCH` release tags that the self-update follows.

- [00148-stt-unlimited-dictation-loop-and-buffer](00148-stt-unlimited-dictation-loop-and-buffer/) - In Progress (host checks and owner decisions open). Delayed stop, unlimited dictation and a model review; new speech-to-text work goes here.

- [00149-up-arrow-loses-an-interrupted-command](00149-up-arrow-loses-an-interrupted-command/) - Dormant (not reproduced). A command stopped with Ctrl+C is missing from up-arrow history.

- [00147-tmux-f12-menu-fedora-desktop-items](00147-tmux-f12-menu-fedora-desktop-items/) - Not Started (owner decisions). A "Fedora Desktop" entry in the tmux F12 menu, built from drop-ins.

- [00146-ccy-token-switch-through-the-supervisor](00146-ccy-token-switch-through-the-supervisor/) - Not Started (owner decisions). Switch a running ccy session's token through the supervisor.

- [00144-single-fedora-desktop-panel-icon](00144-single-fedora-desktop-panel-icon/) - In Progress (deployed; logout and visual check left). Container Watch folds into the one Fedora Desktop panel icon.

- [00143-pointer-lag-and-trackpoint-speed](00143-pointer-lag-and-trackpoint-speed/) - In Progress (needs a capture at the laptop). Intermittent touchpad lag and a TrackPoint that is far too fast.

- [00141-run-bash-changed-plays](00141-run-bash-changed-plays/) - In Progress (host run left). `./run.bash --changed` runs every play whose inputs changed.

- [00139-commit-signing-everywhere](00139-commit-signing-everywhere/) - In Progress (host verification left). Every commit and tag made on the owner's machine is signed. Addresses #4.

- [00136-host-health-report-in-a-terminal](00136-host-health-report-in-a-terminal/) - In Progress (host run left). The host health report as a command, a login reminder and a panel row.

- [00137-unattended-server-self-update](00137-unattended-server-self-update/) - In Progress (server proof left). A headless server updates itself, runs the affected plays and reboots with sessions restored.

- [00135-ccy-sessions-survive-a-reboot](00135-ccy-sessions-survive-a-reboot/) - In Progress (reboot test left). ccy sessions are recorded and restored after a reboot. Issue #44.

- [00134-startup-log-triage-and-status-panel-unavailable](00134-startup-log-triage-and-status-panel-unavailable/) - In Progress (host checks left). False "crash" notices and the status panel's question mark after a reboot.

- [00131-semgrep-or-true-rule-is-blind-to-the-enclosed-form](00131-semgrep-or-true-rule-is-blind-to-the-enclosed-form/) - Not Started. The semgrep `|| true` rule misses the `$( cmd || true )` form; 18 live sites.

- [00129-semgrep-per-rule-coverage-is-invisible](00129-semgrep-per-rule-coverage-is-invisible/) - Not Started (one owner decision). The pattern gate's file count hides per-rule coverage gaps.

- [00128-qa-tool-abort-silences-thirty-gates](00128-qa-tool-abort-silences-thirty-gates/) - In Progress (owner decision on Phase 2). One absent dev-only dependency aborts `qa-all.bash` before about thirty gates run.

- [00127-docker-and-podman-inside-lxc](00127-docker-and-podman-inside-lxc/) - Dormant (parked by the owner). Docker and Podman inside the LXC system containers.

- [00121-run-log-secret-scrubber-and-token-scenario](00121-run-log-secret-scrubber-and-token-scenario/) - Dormant (parked by the owner). A fail-closed run-log secret scrubber and the `server-github-token` scenario.

- [00119-headless-github-ssh-443-input](00119-headless-github-ssh-443-input/) - In Progress (a run on a port-22-blocked box left). `RUN_BASH_GITHUB_SSH_443=1` for headless provisioning.

- [00118-ccy-selinux-enforcing-host](00118-ccy-selinux-enforcing-host/) - In Progress (an attended launcher run left). ccy works on an SELinux-enforcing host through relabelled mounts.

- [00117-vmtest-acceptance-script-version-gate](00117-vmtest-acceptance-script-version-gate/) - Not Started. An acceptance run records its guest checker's version and fails a mismatch.

- [00116-ccy-deploy-keys-and-forwarded-agent](00116-ccy-deploy-keys-and-forwarded-agent/) - In Progress (an attended `--ssh-agent` push left). ccy supports deploy-key remotes and a forwarded agent.

- [00115-playbook-shebang-runs-through-run-bash](00115-playbook-shebang-runs-through-run-bash/) - In Progress (a headless proof left). Every play's shebang routes it through run.bash.

- [00114-run-bash-single-play-mode](00114-run-bash-single-play-mode/) - In Progress (a run on a password-sudo box left). `./run.bash <playbook>` runs one play with the right become handling.

- [00113-ccy-ci-runner-implementation](00113-ccy-ci-runner-implementation/) - Not Started. Build the non-interactive ccy CI runner that Plan 00068 specified.

- [00109-desktop-drift-detection-and-fedora-desktop-panel](00109-desktop-drift-detection-and-fedora-desktop-panel/) - In Progress (host tasks left). Desktop drift detection surfaced at login and in one `fedora-desktop` panel.

- [00104-suspend-aborts-on-dock-unplug-never-resuspends](00104-suspend-aborts-on-dock-unplug-never-resuspends/) - Blocked (needs the laptop and dock). Unplugging the dock aborts suspend and nothing re-suspends.

- [00098-encrypted-claude-transcripts-at-rest](00098-encrypted-claude-transcripts-at-rest/) - In Progress. Reduce the exposure of plaintext Claude Code transcripts.

- [00093-ccy-version-gate-covers-two-files-of-eight](00093-ccy-version-gate-covers-two-files-of-eight/) - Not Started. The CCY version gate covers two of eight files; widen and test it.

- [00092-ccy-child-claude-spawn-mode](00092-ccy-child-claude-spawn-mode/) - Blocked (host deploy and `ccy --rebuild`). Opt-in mode for a ccy session to spawn authenticated child `claude` processes.

- [00089-ssh-handling-runner-token-guard](00089-ssh-handling-runner-token-guard/) - Dormant (host image rebuild and live launch). An exported `GH_TOKEN` is used explicitly by a `--no-ssh` runner.

- [00088-claude-code-state-dir-stale-home-fact](00088-claude-code-state-dir-stale-home-fact/) - Dormant (downstream live re-proof). `claude_state_dir` comes from `user_login`, not a poisoned `HOME`.

- [00086-kernel-modules-absent-enumeration](00086-kernel-modules-absent-enumeration/) - Dormant (downstream live re-proof). A guest without `kernel-modules` no longer fails the kernel enumeration.

- [00080-ccy-session-network-isolation](00080-ccy-session-network-isolation/) - Dormant (until the owner schedules Phase 3). A network per ccy session.

- [00074-grub-cgroup-check-reports-absence-it-cannot-prove](00074-grub-cgroup-check-reports-absence-it-cannot-prove/) - Blocked (host Task 3.3). run.bash's grub cgroup check aborts on a proven failure.

- [00073-headless-sudo-password-file](00073-headless-sudo-password-file/) - Dormant (host verification). `RUN_BASH_SUDO_PASSWORD_FILE` for headless runs with password sudo.

- [00072-ccy-assert-rootless-engine](00072-ccy-assert-rootless-engine/) - Dormant (host desktop check). ccy refuses a container engine that is not rootless.

- [00069-plan-md-edited-in-place](00069-plan-md-edited-in-place/) - Not Started. State that `PLAN.md` is edited in place and narration goes to `JOURNAL/`.

- [00065-headless-server-cloud-base-blocker-fixes](00065-headless-server-cloud-base-blocker-fixes/) - Dormant (a fresh Cloud Base run). Fixes from the first headless run on Fedora Cloud Base.

- [00064-open-command-universal-file-opener](00064-open-command-universal-file-opener/) - Dormant (host deploy and live checks). `open`, one command for any file, directory or URL.

- [00063-headless-run-bash-server-cloud-provisioning](00063-headless-run-bash-server-cloud-provisioning/) - In Progress (in production use; token and SSH paths await a host run). Unattended headless `run.bash` provisioning.

- [00062-disk-reclaim-tui](00062-disk-reclaim-tui/) - Blocked (host tasks). `reclaim`, a confirm-first disk reclamation TUI.

- [00061-headless-server-provisioning](00061-headless-server-provisioning/) - Dormant (host acceptance run). Headless Fedora Server provisioning with a per-play scope gate.

- [00058-github-version-pin-updates](00058-github-version-pin-updates/) - Dormant (darktable bump and a host run). Bump every upstream version pin that is behind.

- [00056-displaylink-dock-hotplug-recovery](00056-displaylink-dock-hotplug-recovery/) - Dormant (needs a DisplayLink dock). Recover from a dock replug without logging out.

- [00055-container-process-watchdog](00055-container-process-watchdog/) - Dormant (host deploy and verify). A reporting-only watchdog for long-running container processes.

- [00054-github-ssh-443-host-level](00054-github-ssh-443-host-level/) - Dormant (host deploy and live test). One `GITHUB_SSH_443` signal across the host and ccy.

- [00053-fedora-44-fresh-install-audit](00053-fedora-44-fresh-install-audit/) - Dormant (needs a fresh Fedora 44 install). The first fresh-install host audit.

- [00051-ansible-lint-improvement](00051-ansible-lint-improvement/) - Dormant (owner to keep or cancel). ansible-lint compliance and FQCN across the playbooks.

- [00049-full-repo-audit](00049-full-repo-audit/) - In Progress (two deferred items and owner calls left). A full repository audit across ten dimensions.

- [00048-cc-token-source-parity](00048-cc-token-source-parity/) - In Progress (Phase 4 host deploy). Host `cc` gets ccy's named-token chooser.

- [00046-localhost-yml-leak-guard](00046-localhost-yml-leak-guard/) - In Progress (research done). A handler that blocks `gh` posts carrying `localhost.yml` values.

- [00045-project-personas-multi-tool-accounts](00045-project-personas-multi-tool-accounts/) - Dormant (owner's Phase 1 decision gate). A `project_personas` map driving several tools' accounts.

- [00041-remote-desktop-quick-toggle](00041-remote-desktop-quick-toggle/) - Dormant (host deploy and a test from a second machine). A quick-settings toggle for GNOME Remote Desktop.

- [00040-raw-clipping-scanner](00040-raw-clipping-scanner/) - Dormant (host probes and manual tests). `clip-scan` flags clipped Sony ARW files before import.

- [00039-ftp-camera-viewer-tui](00039-ftp-camera-viewer-tui/) - Dormant (host test matrix). Live preview modes for `ftp-camera`.

- [00038-musiccast-controller](00038-musiccast-controller/) - Blocked (owner reads `DECISION.md`). A MusicCast controller with Qobuz browse and play.

- [00037-image-watermarking-toolkit](00037-image-watermarking-toolkit/) - Dormant (owner to resume). A `watermark` CLI with visible marks and licence metadata.

- [00035-gh-multi-account-hardening](00035-gh-multi-account-hardening/) - Dormant (owner decision). Harden the fresh-install flow around gh accounts and SSH keys.

- [034-localhost-config-account](034-localhost-config-account/) - Dormant (owner to keep or cancel). Declare the config repo's GitHub account in `localhost.yml`.

- [032-compression-helpers](032-compression-helpers/) - In Progress (host re-run of acceptance left). `compress` / `uncompress` wrappers around `ouch`.

- [031-reliable-screen-sharing](031-reliable-screen-sharing/) - Dormant (owner go-ahead for Phase 3). Reliable screen-sharing alternatives on Wayland. Issue #17.

- [030-phpantom-lsp](030-phpantom-lsp/) - Dormant (owner go/no-go). PHPantom as the PHP language server instead of Intelephense.

- [029-rapid-raw-cloud-ai](029-rapid-raw-cloud-ai/) - Dormant (owner to schedule or cancel). RapidRAW generative AI on rented cloud GPUs.

- [028-fedora-screen-sharing](028-fedora-screen-sharing/) - Dormant (owner to keep or cancel). Fedora 43 Slack and Meet screen-sharing fixes.

- [026-repo-spring-cleaning](026-repo-spring-cleaning/) - In Progress (unscheduled since April). Repository cleanup outside ccy: status scripts and playbook hygiene.

- [023-hostname-based-inventory](023-hostname-based-inventory/) - Dormant (owner to keep or cancel). Ansible inventory keyed by machine hostname.

- [022-install-security-and-resilience](022-install-security-and-resilience/) - In Progress (blocked tasks). Install fixes from real-hardware kickstart testing.

- [018-fedora-kickstart-install](018-fedora-kickstart-install/) - Dormant (owner to close or retarget). The automated Fedora kickstart install pipeline.

- [014-whisper-model-manager](014-whisper-model-manager/) - Dormant (desktop tests). `wsi-model-manager`, a TUI for Whisper models.

- [009-claude-devtools](009-claude-devtools/) - Dormant (host run and a live check). `ccdt`, the claude-devtools session viewer.

- [007-speech-to-text-resource-leak-fixes](007-speech-to-text-resource-leak-fixes/) - Dormant (laptop tests, or cancel). Speech-to-text mic leak, truncation and paste fixes.

- [004-comprehensive-feature-documentation](004-comprehensive-feature-documentation/) - Dormant (owner to keep or cancel). User documentation for the major features.

## Completed Plans

- [00164-stt-enter-lost-after-focus-retake](Completed/00164-stt-enter-lost-after-focus-retake/) - When a dictation's window lost focus, the text was pasted back into it but the Enter did not send it. Both recorders wait for the window to keep focus before the paste and check it again before the Enter. Deployed; the owner confirmed Enter works.

- [00166-laptop-panel-stays-on-with-lid-closed-when-docked](Completed/00166-laptop-panel-stays-on-with-lid-closed-when-docked/) - Docked with the lid closed, GNOME kept the laptop panel live because `IgnoreLid=true` hid the lid from UPower. The play sets `IgnoreLid=false` and asserts UPower sees the lid; deployed and accepted. Undocked suspend rows waived by the owner.

- [00163-ccy-one-key-agent-forwarding](Completed/00163-ccy-one-key-agent-forwarding/) - ccy forwards one chosen key from the owner's ssh-agent through a filtering socket, never the whole agent, so a headless launch can use a passphrase-protected key with no prompt. Accepted on the host by Plan 00161: every seat launched headless on a passphrase key, and inside a seat the agent listed one key, refused changes and signed with it.

- [00157-cc-desktop-launcher-broken](Completed/00157-cc-desktop-launcher-broken/) - Bug: host `cc` stopped starting sessions. Triage found the wrapper, its libraries, the claude install and the credentials all sound; `cc` worked again once the session the boot restore had started (`cc --continue`) was gone. Likely cause, unconfirmed: claude's "No conversation found to continue" in that restored session. Left behind: a triage whose interactive-shell probe cannot hang, and a reaper for a hung run.

- [00151-container-clipboard-sharing](Completed/00151-container-clipboard-sharing/) - Ctrl+V image paste works inside ccy: `wl-clipboard` and a 5 s `wl-paste` guard in the shared image (container 2.41+), deployed with a 20-check acceptance and confirmed by the owner. LXC stays a documented manual recipe, as this repo has no LXC config under IaC.

- [00150-image-paste-cli](Completed/00150-image-paste-cli/) - Feature: `imgpaste <image>` prints a self-decoding bash block (WebP, base64, sha256) to paste into any Claude chat, so screenshots reach agents with no clipboard. Deployed by the new `play-cli-tools.yml`, the one play for small CLI utilities. Host acceptance 8 of 8.

- [00142-qa-tool-pin-does-not-converge-under-pipx-uv](Completed/00142-qa-tool-pin-does-not-converge-under-pipx-uv/) - `play-python.yml` could not move a QA tool at the wrong version onto its pin: under pipx's uv backend `install --force` re-runs `uv venv` on the existing venv, which uv refuses. An off-pin tool is now removed, installed fresh and re-pinned; reproduced offline and gated. Search: "A virtual environment already exists".

- [00145-ccy-key-menu-steers-away-from-keys-that-cannot-push](Completed/00145-ccy-key-menu-steers-away-from-keys-that-cannot-push/) - Bug fix: ccy's launch-time SSH key menu accepted a key its own push probe had just found cannot push. When any key can push, the menu now offers only those; the full list is behind `a`, and a key there that cannot push needs a `y`. CCY 3.71.0. Host-confirmed by the owner.

- [00079-podman-container-control](Completed/00079-podman-container-control/) - `podfreeze`: freeze and unfreeze Podman containers individually, as a CCY group, or by network, via `podman pause` — the one mechanism that works rootless. Renumbered from 00078 after two clones each handed out that number from a `--local` counter.

- [00140-agent-browser-sessions-leak](Completed/00140-agent-browser-sessions-leak/) - Agents leave browser sessions running and open new ones, and every session name is its own Chromium. Upstream's skill tells them to make one per task, and bare `close` shuts only one. A guard in the three browser commands refuses a new session while one is live (`CCY_BROWSER_MAX_SESSIONS`, default 1) and names the one to reuse or close.

- [00138-bash-history-and-ctrl-r](Completed/00138-bash-history-and-ctrl-r/) - Bash history is saved at every prompt, timestamped and unlimited in `~/.local/state/bash` (user and root), and Ctrl+R searches every terminal's history, ranking this directory's then this repo's commands first, via a builtins-only recorder (Atuin rejected). Deployed; acceptance 15/15.

- [00130-legacy-plan-scripts-lose-their-last-log-chunk](Completed/00130-legacy-plan-scripts-lose-their-last-log-chunk/) - `PlanWorkflow.md` taught `exec > >(tee "$LOG") 2>&1` and a plan-local `logs/` tree, both forbidden by PlanScriptStandards R4. The doc is fixed, and the ten scripts already written from it were converted and each run to its last line. The one it could not run, 00066's, was moot once that plan was cancelled.

- [00112-gnome-extensions-enabled-state-declared](Completed/00112-gnome-extensions-enabled-state-declared/) - Plan 00110's desktop scenario found a fresh install leaves every deployed GNOME extension INITIALIZED and none enabled (the enable races the shell's scan and its failure is hidden); make the enabled list declared, idempotent gsettings state and let the desktop scenario certify it.

- [00132-crashloop-killed-shell-then-greeter-suspended-on-ac](Completed/00132-crashloop-killed-shell-then-greeter-suspended-on-ac/) - A container crash loop exhausted the session D-Bus quota and killed `gnome-shell`, then the unmanaged GDM greeter idle-suspended the plugged-in machine. Both fixed and deployed; the watchdog now stops a runaway restart loop instead of only reporting it.

- [00133-ccy-auto-compact-window-default-and-daemon-check](Completed/00133-ccy-auto-compact-window-default-and-daemon-check/) - CCY sets `CLAUDE_CODE_AUTO_COMPACT_WINDOW=600000` for every session. The first attempt shipped `600k`, which the variable parses as 600 and floors to 100,000 — six times tighter than intended, and survivable enough to run for a week. Three review rounds were needed because the first two corrections were each wrong in turn.

- [00099-rclone-rc-auth-broke-unmigrated-clients](Completed/00099-rclone-rc-auth-broke-unmigrated-clients/) - Plan 00094 authenticated the rclone RC on a false premise, so three unmigrated clients got HTTP 401 and reported it as a dead mount for a week. One sourced credential library, every client migrated, plus the new `qa-deployed-drift.bash` gate. Re-deployed and ACCEPTED on the host, `COVERAGE: 9 of 9`.

- [00124-chrome-install-gpg-failure-on-upgraded-host](Completed/00124-chrome-install-gpg-failure-on-upgraded-host/) - The key was never missing: rpm 6 names `gpg-pubkey` packages by full fingerprint where the helper assumed a short id, so the lookup matched nothing and reported an absent key that was verifying packages throughout. ACCEPTED on the host, 23 assertions.

- [00075-fail-signal-discard-sweep-and-gate](Completed/00075-fail-signal-discard-sweep-and-gate/) - Sweeps repo-owned bash, Python and playbooks for one defect class — a command's failure silently converted into data and then trusted — and builds a gate that fails the build rather than advising. Closed after re-checking its last task: both hooks-daemon defects it meant to report upstream had since been fixed there.

- [00122-lxc-freeze-thaw-shared-with-podfreeze](Completed/00122-lxc-freeze-thaw-shared-with-podfreeze/) - `podfreeze` groups, previews and toggles Podman containers; LXC is a first-class engine here with no equivalent. Adds a sibling `lxcfreeze` — rootful, so a separate tool rather than a flag — on a shared library both tools source. ACCEPTED on the host: 12 of 12 checks, 33 assertions, 0 failed. `podfreeze`'s fzf picker remains the one path no gate exercises.

- [00082-run-bash-github-accounts-none](Completed/00082-run-bash-github-accounts-none/) - Lets `run.bash` headless v1 provision with `RUN_BASH_GITHUB_ACCOUNTS=none`, which previously failed preflight as an unsupported follow-up. Merged as PR #33 (`68f1596f`); Phase 5's live proof came from Plan 00110's lab run, whose `vmtest` default provisions with `none` and asserts a clone in a guest with no GitHub identity.

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

**Older completed plans** — everything beyond the most recent 30 — are in
[Completed/README.md](Completed/README.md), moved there verbatim. The retention window
keeps this index readable; the archive keeps the record whole.

## Cancelled Plans

- [013-claude-devtools](Cancelled/013-claude-devtools/) - Cancelled. A byte-identical copy of Plan 011; superseded by Plan 009.

- [011-claude-devtools](Cancelled/011-claude-devtools/) - Cancelled. A renumbered, never-started copy of Plan 009; superseded by Plan 009.

- [00066-ftp-camera-airbnb-wifi-and-hotspot-triage](Cancelled/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/) - Two `ftp-camera` failures on untrusted WiFi: `--async-copy` stalling after the first frame, and a `--hotspot` IaC gap. CANCELLED by the owner with its HOST triage never run. The hotspot profile, `triage.bash` and `--debug-ftp` shipped. The retry loop has no confirmed cause.

- [027-contextual-shell-history](Cancelled/027-contextual-shell-history/) - Atuin for directory-aware recall. CANCELLED: superseded by Plan 00138; Atuin can only filter by directory, not rank, and its bash hook exposes command lines in argv.

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
