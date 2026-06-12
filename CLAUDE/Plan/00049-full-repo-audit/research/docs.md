# Documentation Drift Audit

## Scope & Method

This audit covers documentation drift in `docs/**` and `CLAUDE/**` (excluding `CLAUDE/Plan/` history) versus the actual code on branch `F43`.

Method:

1. Enumerated all markdown files in `docs/` (17 files incl. `features/`) and `CLAUDE/` (9 topic files plus directory-level `CLAUDE.md` files).
2. Read every user-facing doc in full (`README.md`, `installation.md`, `configuration.md`, `architecture.md`, `development.md`, `playbooks.md`, `containerization.md`, `nordvpn-installation.md`, `ccy-debug-mounts.md`, `post-upgrade.md`, `features/*`, `ddev.md`/`kitty.md`/`fast-file-manager.md` spot-read) and all `CLAUDE/` topic files.
3. Cross-verified every suspected drift against the real artefacts: `playbooks/playbook-main.yml`, `playbooks/imports/**`, `scripts/qa-*.bash`, `run.bash`, `files/var/local/claude-yolo/{claude-yolo,Dockerfile,entrypoint.sh,ccy-ctrl-z-patch.js,lib/*}`, `files/home/.local/bin/wsi*`, `extensions/speech-to-text@fedora-desktop/extension.js`, `vars/*.yml`, and `environment/localhost/host_vars/localhost.yml` (gitignored; values redacted during inspection).
4. Ran a relative-link checker over all docs/CLAUDE markdown, and validated every `play-*.yml` name and `playbooks/...` path referenced in docs against the actual filesystem.
5. Grepped for stale Fedora-version references (branch is F43, `vars/fedora-version.yml` = 43).

## Summary

The repo's recent documentation (`docs/ddev.md`, `docs/kitty.md`, `docs/github-multi-account.md`, `docs/post-upgrade.md`, `docs/features/claude-devtools.md`, `CLAUDE/ContainerEngines.md`) is accurate and well cross-referenced. However, the older core docs (`installation.md`, `configuration.md`, `playbooks.md`, `architecture.md`, `containerization.md`, `docs/README.md`) have drifted badly from two major architectural changes that the code has already made:

1. **Docker became rootful and core.** `play-docker.yml` now installs rootful Docker (with explicit legacy-rootless cleanup tasks) and is imported by `playbook-main.yml`. Five docs still teach users that Docker is rootless, optional, and lives at `playbooks/imports/optional/common/play-docker.yml` — including troubleshooting commands (`systemctl --user status docker`) that will fail on a system this repo built.
2. **Vault moved to variable-level encryption.** `localhost.yml` is a plain YAML file with `!vault` string values; three docs still instruct `ansible-vault edit/view` on it, which errors on non-encrypted files — directly contradicting `CLAUDE/SecurityRules.md`.

Additionally, `docs/nordvpn-installation.md` documents a playbook that no longer exists, and `CLAUDE/PlanWorkflow.md` is substantially a copy of the *hooks-daemon* project's workflow, referencing QA scripts, coverage gates, and plan-numbering rules that do not exist in (or actively contradict) this repo.

18 findings: 4 high, 7 medium, 6 low, 1 info.

---

## DOC-01: Docker documented as rootless/optional — it is rootful and core

**Severity: high**

`playbooks/imports/play-docker.yml` is unambiguous: header comment line 3 reads `# Docker (rootful) — compatibility engine for DDEV.`, lines 15–76 are *legacy rootless cleanup* tasks (stopping the rootless user service, running `dockerd-rootless-setuptool.sh uninstall`), and lines 134–205 set up the system daemon, `docker` group, `docker.socket`/`docker.service` and reset the context from rootless to default. It is imported by `playbooks/playbook-main.yml:20`, i.e. it runs automatically. `CLAUDE/ContainerEngines.md` and `docs/ddev.md` describe this correctly.

Stale documentation contradicting this:

- `docs/containerization.md:140-152` — "Optional playbook (run manually): `ansible-playbook playbooks/imports/optional/common/play-docker.yml`" (wrong path, wrong category) and "Configures **rootless Docker** for security".
- `docs/containerization.md:176-192` — entire "Rootless Docker" section, including `systemctl --user status docker` / `systemctl --user restart docker` / `journalctl --user -u docker`, all of which fail against the rootful daemon actually deployed.
- `docs/containerization.md:683-686` — Security Considerations: "Rootless mode configured (enhanced security)".
- `docs/containerization.md:780-786` — Docker troubleshooting again uses `systemctl --user`.
- `docs/playbooks.md:13` ("Install Docker - Rootless Docker setup"), `docs/playbooks.md:131-139` ("Configures rootless Docker (user systemd service)"), `docs/playbooks.md:683` (wrong optional/common path).
- `docs/installation.md:195-196` — "# Docker (rootless)" with the wrong `optional/common` path, listed under "Optional Components".
- `docs/README.md:146-147` — "# Install Docker (rootless)" with the wrong path; `docs/README.md:253` — index link text "Docker rootless".

**Impact:** Users following the troubleshooting sections will run commands that fail; users will believe Docker is not installed by default (it is); the security posture described (rootless) is the opposite of reality (`docker` group = root-equivalent, which `CLAUDE/ContainerEngines.md` documents as a deliberate, security-relevant choice).

**Recommendation:** Rewrite the Docker sections of `containerization.md`, `playbooks.md`, `installation.md` and `docs/README.md` to match `play-docker.yml` and `CLAUDE/ContainerEngines.md`: rootful, core, system `systemctl` commands, correct path `playbooks/imports/play-docker.yml`, with a pointer to the rootful-vs-rootless rationale.

---

## DOC-02: `ansible-vault edit/view` instructions are broken (variable-level encryption)

**Severity: high**

`CLAUDE/SecurityRules.md:47-56` states: "This project uses VARIABLE-level encryption, NOT file-level encryption… Use a regular text editor — DO NOT use `ansible-vault edit`". I verified `environment/localhost/host_vars/localhost.yml` (gitignored; tracked template is `localhost.yml.dist`): it is a plain YAML file with `!vault |` string values — it does **not** start with `$ANSIBLE_VAULT`. `docs/github-multi-account.md:57` also correctly says "this is a regular YAML file".

Running `ansible-vault view`/`edit` on a non-vault-encrypted file fails with "input is not vault encrypted data". Yet three docs instruct exactly that:

- `docs/configuration.md:53-57` — "View encrypted variables: `ansible-vault view …`; Edit encrypted variables: `ansible-vault edit …`" (and line 17 calls the file "(encrypted)").
- `docs/installation.md:122` — Manual install Step 5: "`ansible-vault edit environment/localhost/host_vars/localhost.yml` … You'll be prompted to create a vault password" (this flow does not work at all).
- `docs/installation.md:399` — troubleshooting: "Test vault access: `ansible-vault view …`".
- `docs/README.md:166-169` — Quick Reference: both `ansible-vault view` and `ansible-vault edit` on the file.

**Impact:** The manual-installation path is broken at Step 5; the suggested "test" in troubleshooting produces an error even when everything is healthy, sending users down a false debugging path. Docs also directly contradict the project's own security rules.

**Recommendation:** Replace all `ansible-vault edit/view` instructions with: edit the file in a normal editor; encrypt individual secrets with `ansible-vault encrypt_string 'secret' --name 'var_name'`; reference `CLAUDE/SecurityRules.md` as the canonical workflow.

---

## DOC-03: `docs/nordvpn-installation.md` documents a playbook and implementation that no longer exist

**Severity: high**

The doc (lines 9, 22) is built around **`play-nordvpn-cli.yml`**, which does not exist anywhere under `playbooks/`. It describes installing the official NordVPN CLI client (`nordvpn login/connect`, `nordvpnd` daemon, `nordvpn` group) plus the `NordVPN_Connect@poilrouge.fr` GNOME extension.

The actual playbook is `playbooks/imports/optional/common/play-nordvpn-openvpn.yml` ("NordVPN OpenVPN Manager", per Plan 002): it installs `openvpn`, `NetworkManager-openvpn`, `NetworkManager-openvpn-gnome`, creates `~/.config/nordvpn/configs`, and prompts for NordVPN *service credentials* — a completely different architecture. There is also a deployed helper `files/home/.local/bin/nord`. Nothing in the current implementation matches the doc's CLI commands, group requirements, daemon troubleshooting, or extension steps.

**Impact:** Every command in the doc fails; the referenced playbook cannot be run.

**Recommendation:** Rewrite `docs/nordvpn-installation.md` against `play-nordvpn-openvpn.yml` and the `nord` helper (or delete the doc and fold a short guide into `docs/playbooks.md`). Note `docs/features/README.md:43` already advertises the new "Nord VPN Manager: Interactive OpenVPN connection chooser".

---

## DOC-04: `CLAUDE/PlanWorkflow.md` describes a different project's QA and planning infrastructure

**Severity: high**

`CLAUDE/PlanWorkflow.md` is substantially the hooks-daemon project's workflow document (footer: "Maintained by: Claude Code Hooks Daemon Contributors"). Verified mismatches against this repo:

- **QA commands do not exist**: 21 references to `./scripts/qa/run_all.sh`, `./scripts/qa/run_lint.sh`, `run_format_check.sh`, `run_type_check.sh`, `run_tests.sh`, `run_autofix.sh`. There is no `scripts/qa/` directory (`ls scripts/qa` → not found). The real command is `./scripts/qa-all.bash` per `CLAUDE/QA.md`.
- **Tooling does not exist here**: Black/MyPy strict/Pytest 95% coverage/Bandit gates, `scripts/debug_hooks.sh`, `tests/handlers/...`, `untracked/qa/coverage.json` — none are part of this repo.
- **Plan numbering contradicts the daemon**: the doc mandates 3-digit `001-` sequential numbering, while the active `plan_number_helper` handler mandates the git-config counter zero-padded to **5** digits (current plans are `00035`–`00048`).
- **Templates contradict an active handler**: every template includes `**Estimated Effort**: [Hours/Days]` and a `## Timeline` with `Target Completion: YYYY-MM-DD` — content the `plan_time_estimates` handler *blocks* from being written into `CLAUDE/Plan/*.md`.
- **Broken link**: line 735 links `001-handler-implementation/PLAN.md`, which does not exist relative to `CLAUDE/`.

**Impact:** This is an agent-instruction file; agents following it will invoke nonexistent QA scripts, generate plan documents that the hooks daemon rejects, and use the wrong plan numbering scheme.

**Recommendation:** Rewrite `CLAUDE/PlanWorkflow.md` for this repo: reference `./scripts/qa-all.bash` and `CLAUDE/QA.md`, the 5-digit git-counter plan numbering, and strip effort/timeline fields from the templates (or annotate them as prohibited). The `planning` skill loads this file, so the fix has direct workflow value.

---

## DOC-05: `docs/playbooks.md` catalogue badly misclassifies core vs optional and omits ~25 playbooks

**Severity: medium**

`playbooks/playbook-main.yml` (lines 5–44) imports 24 playbooks automatically, with a header note "Everything imported here runs by default and is NOT optional." Drift in `docs/playbooks.md`:

- **Core playbooks documented as "Optional"** (lines 107–612, "Run these manually as needed"): `play-comms.yml`, `play-docker.yml`, `play-claude-yolo.yml`, `play-firefox.yml`, `play-github-cli-multi.yml`, `play-gnome-shell.yml`/`play-gnome-shell-extensions.yml`, `play-gsettings.yml`, `play-python.yml`, `play-vscode.yml`, `play-vpn.yml`, `play-terminal-emulators.yml` — all are core imports of `playbook-main.yml`.
- **Core section lists only 9 of 24** (lines 17–105): missing `play-AB-dnf-upgrade`, `play-prevent-ssh-suspend`, `play-network-wait-tuning`, `play-systemd-user-tweaks`, `play-git-hooks-security`, `play-firefox`, `play-github-cli-multi`, `play-browsers`, `play-docker`, **`play-podman`** (not documented anywhere in the file despite Podman being the default engine), `play-python`, `play-claude-yolo`, `play-comms`, `play-gnome-shell*`, `play-markless`, `play-terminal-emulators`, `play-vscode`, `play-vpn`, `play-gsettings`, `play-ZZ-repo-cleanup`.
- **Genuinely optional playbooks missing from the catalogue**: of `playbooks/imports/optional/common/` (27 files), the doc omits e.g. `play-advanced-kernel-management`, `play-claude-devtools`, `play-clean-paste`, `play-collaboration`, `play-compression-helpers`, `play-darktable-ai-*`, `play-ddev`, `play-fast-file-manager`, `play-ftp-camera`, `play-gnome-shell-dev`, `play-image-watermarking`, `play-lightweight-ides`, `play-network-tools`, `play-nordvpn-openvpn`, `play-photography`, `play-rclone`, `play-remote-desktop-toggle`, `play-unifi-controller`, `play-videography`. Hardware-specific omits `play-ipu6-webcam`, `play-laptop-lid-power-management`, `play-laptop-thermal-diagnostics`, `play-musiccast`, `play-darktable-ai-gpu`. Experimental omits `play-docker-overlay2-migration`.
- **Wrong run paths** in "Running Optional Playbooks" (lines 680–695): `playbooks/imports/play-comms.yml` is correct, but `optional/common/play-docker.yml` and `optional/common/play-distrobox.yml`… `play-docker.yml` path is wrong (core), and the section presents core playbooks as needing manual runs.
- **Stale version**: line 320 "Latest stable version for Fedora 42".

(Positives: the `play-python.yml` pyenv versions 3.11.13/3.12.11/3.13.1 at lines 415/438-442 exactly match `play-python.yml:9-12`; the CCY flags `--create-token`, `--custom`, `--custom-docker` all exist in `claude-yolo`.)

**Recommendation:** Regenerate the catalogue from `playbook-main.yml` and the `optional/` tree; move the misfiled entries into the Core section; add the missing optional entries (one-liners suffice); fix "Fedora 42".

---

## DOC-06: `docs/architecture.md` execution flow lists 9 of 24 core playbooks

**Severity: medium**

`docs/architecture.md:77-87` enumerates the main-playbook order as 9 playbooks ending at `play-toolbox-install.yml`. The actual `playbook-main.yml` imports 24, including Docker, LXC-after-Docker ordering (with a documented iptables reason), Podman, Python, claude-yolo-before-claude-code ordering (Plan 00048 dependency), Firefox, GitHub CLI multi-account, GNOME Shell, terminal emulators, VS Code, VPN, gsettings and repo cleanup. Line 93 also lists "TLP" under hardware-specific, but `play-tlp-battery-optimisation.yml` moved to `optional/archived/`. The directory tree (lines 20–58) omits `optional/untested/`, `docs/`, `extensions/`, `tasks/`, `tests/`, `fedora-install/`.

**Impact:** Contributors reading the architecture doc get a materially wrong picture of what a default install does, and miss the two ordering constraints that the real file documents as load-bearing.

**Recommendation:** Update the execution-flow list from `playbook-main.yml` (including the two ordering comments), move TLP to an "archived" mention, and refresh the tree.

---

## DOC-07: Stale "Fedora 42" references on the F43 branch

**Severity: medium**

`vars/fedora-version.yml` sets `fedora_version: 43` and `docs/README.md:317` correctly says "Current branch: Fedora 43 (F43)". Stale references that state the branch target as 42:

- `docs/installation.md:10` — "Fresh Fedora installation - Fedora 42"
- `docs/installation.md:46` — "Checks Fedora version matches target (Fedora 42)"
- `docs/installation.md:106`, `:256` — `git checkout F42` as the example for this branch; `:247`/`:252` — troubleshooting outputs "Fedora release 42" / "fedora_version: 42"
- `docs/playbooks.md:320` — "Latest stable version for Fedora 42"
- `docs/features/speech-to-text.md:60` — "Fedora 42 (this branch)"
- `CLAUDE/GnomeShell.md:45` — "Fedora 42 has 48.7"
- `docs/ansible-lint-improvement-plan.md:24` — "Git branch: main (F42)"

(Historical/comparative references in `fast-file-manager.md`, `post-upgrade.md`, `development.md`, `architecture.md` are legitimately version-agnostic examples and were not flagged.)

**Impact:** Since `run.bash` and `play-AA-preflight-sanity.yml` fail-fast on version mismatch, a Fedora 43 user following installation.md literally is told their own setup is wrong.

**Recommendation:** Branch-wide sweep replacing target-version statements with 43 (or, better, with a reference to `vars/fedora-version.yml` so the next branch bump has fewer edits).

---

## DOC-08: `CLAUDE/GnomeShell.md` describes an obsolete speech-to-text pipeline

**Severity: medium**

The "Project-Specific: speech-to-text Extension" section (and the intro) claims: extension spawns a `rec` subprocess, calls a **`wsi-transcribe`** helper, transcribes with **whisperfile**, and pastes with **wtype**.

Reality, verified:

- `files/home/.local/bin/` contains `wsi`, `wsi-stream`, `wsi-stream-server`, `wsi-article*`, `wsi-claude-process`, `wsi-model-manager`, `wsi-server-manager` — **no `wsi-transcribe`**.
- `files/home/.local/bin/wsi:1-40` documents the actual architecture: extension → `wsi` → `faster-whisper-transcribe` (`WHISPERFILE="faster-whisper-transcribe"` at line 31), with DBus `StateChanged`/`Error` signals back to the extension.
- Auto-paste uses **ydotool**, not wtype (`wsi:305-308`).
- `extensions/speech-to-text@fedora-desktop/extension.js` triggers `wsi`/`wsi-stream` (lines 10, 789-790, 854-855) and supports a streaming mode (RealtimeSTT) the doc never mentions.

Also stale in the same file: GNOME Shell version claim at line 45 (see DOC-07).

**Impact:** This is an agent-instruction file; an agent debugging the extension would look for nonexistent scripts and the wrong paste tool.

**Recommendation:** Rewrite the project-specific section from `wsi`'s header comment block (which is accurate and current) and `docs/features/speech-to-text.md`.

---

## DOC-09: `docs/containerization.md` omits Podman entirely and calls CCY "Docker-based"

**Severity: medium**

The doc frames the project as "three containerization technologies: LXC, Docker, Distrobox" (lines 1–38) with no Podman section — yet Podman is the repo's **default** container engine (`vars/container-defaults.yml:10`, `play-podman.yml` core import, `CLAUDE/ContainerEngines.md`). Specific drift:

- `docs/containerization.md:646` — "Good: Use CCY with built-in agent-browser … ✅ Docker-based, isolated". CCY defaults to Podman (`claude-yolo:133` — "`--engine ENGINE … default: podman`").
- Custom-Dockerfile troubleshooting (lines 529–553) uses `docker build --check .claude/ccy/`, `docker build -t test .`, `docker images | grep claude-yolo` — on a default setup these inspect the wrong engine's store (`docker ps` never shows Podman containers, as `CLAUDE/ContainerEngines.md` itself notes).
- Structural: the entire "Custom Dockerfiles for CCY" section (lines 287–596) is nested under the "## Distrobox" heading, which breaks the doc's own navigation and produces the anchor `#custom-dockerfiles-for-ccy` — while `claude-yolo` (lines 2476, 2500) prints a link to `docs/containerization.md#custom-dockerfiles`, an anchor that does not exist.
- `docs/containerization.md:96-102` describes `play-docker-in-lxc-support.yml` as "Enables user namespaces for rootless Docker" — stale alongside DOC-01.

**Impact:** The primary container doc misleads on the engine the project actually standardises on, and the in-container help text in `claude-yolo` links to a dead anchor.

**Recommendation:** Add a Podman section (or fold in `CLAUDE/ContainerEngines.md` content), promote the CCY section to its own `##` heading whose anchor matches the URL printed by `claude-yolo` (or update the script's URL), and switch CCY troubleshooting examples to `podman` (or engine-neutral wording).

---

## DOC-10: `CLAUDE/QA.md` misstates how the QA scripts behave

**Severity: medium**

Verified against `scripts/qa-all.bash`, `qa-python.bash`, `qa-bash.bash`:

- `CLAUDE/QA.md` "When to Run What" table: "Ansible playbooks → `./scripts/qa-all.bash` (includes `qa-ansible.bash` **via `qa-patterns.bash`**)". Wrong: `qa-all.bash:54-59` runs `qa-ansible.bash` directly as a fourth standalone step; `qa-patterns.bash` is Semgrep-only.
- `CLAUDE/QA.md` "What QA Catches": "Common Python issues (via `ruff` **if installed**)". Wrong: `qa-python.bash:21-23` hard-fails with exit 2 ("ruff not installed (sudo dnf install ruff)") and `qa-all.bash:38-43` refuses to run at all — ruff is mandatory, in line with the fail-fast rule.
- `CLAUDE/QA.md` table says `qa-bash.bash` checks "shellcheck + `bash -n`" — but `qa-bash.bash:67` runs shellcheck only `if command -v shellcheck` (silently skipped when absent). The doc overstates the guarantee; arguably the script (not the doc) is what should change to honour the missing-dependency fail-fast rule, but as written doc and script disagree.

**Impact:** Agents relying on QA.md will mis-predict gate behaviour (e.g. believing a ruff-less environment degrades gracefully when it actually exits 2, or believing shellcheck always ran when it may not have).

**Recommendation:** Correct the two factual statements; decide whether shellcheck should be mandatory (exit 2 like ruff/semgrep) and align doc + script.

---

## DOC-11: `docs/ccy-debug-mounts.md` is a stale session log with broken instructions

**Severity: medium**

The doc is written as a work-in-progress note ("Current Status" checklist with ⬜ items, "This is future work", "Since CCY script doesn't support a `--mount` flag yet"). Verified problems:

- Its podman one-liner injects `CLAUDE_CODE_OAUTH_TOKEN=$(cat ~/.config/claude-code/oauth_token 2>/dev/null || echo '')`. The path `~/.config/claude-code/oauth_token` appears **nowhere else in the repo** — CCY tokens live in `$CCY_ROOT/tokens` as named `*.token` files (`claude-yolo:112`, `lib/token-management.bash`). The command silently passes an empty token (`|| echo ''` is itself the error-hiding pattern this project bans).
- It claims "The project includes `scripts/desktop-symlinks` which documents the approach, but currently requires CCY modification" — `scripts/desktop-symlinks` is an actual executable "CCY Read-Only Mount Wrapper", not documentation.
- "Custom Dockerfile creates these mount points… ✅ Custom Dockerfile has mount points" — no such mount points exist in `files/var/local/claude-yolo/Dockerfile`.

**Impact:** Following the doc yields a container without working auth; the status section misrepresents repo state.

**Recommendation:** Either delete the doc or rewrite it around the current mechanisms (`scripts/desktop-symlinks`, CCY token files). If kept, remove the status-log formatting per the project's own stable-content documentation standards.

---

## DOC-12: Broken relative links and anchors

**Severity: low**

Verified with a link checker plus manual anchor inspection:

- `docs/features/speech-to-text.md:759` — `[Containerization Guide](containerization.md)` resolves to `docs/features/containerization.md` (missing); should be `../containerization.md`.
- `docs/features/speech-to-text.md:757` — anchor typo `../playbooks.md#play-nvidiaym` (should be `#play-nvidiayml`).
- `CLAUDE/ContainerEngines.md:45,142-143` — references `CLAUDE/Plan/033-ddev-installation/{PLAN.md,container-engine-strategy.md}`; the plan moved to `CLAUDE/Plan/Completed/033-ddev-installation/`.
- `CLAUDE/PlanWorkflow.md:735` — `001-handler-implementation/PLAN.md` does not exist (covered by DOC-04).
- `files/var/local/claude-yolo/claude-yolo:2476,2500` — URL anchor `#custom-dockerfiles` does not exist in `docs/containerization.md` (heading is "Custom Dockerfiles for CCY" → `#custom-dockerfiles-for-ccy`; covered by DOC-09).

**Recommendation:** Fix the four link/anchor targets; consider adding a docs link-check to QA.

---

## DOC-13: `docs/README.md` index omits five docs and contains wrong quick-reference paths

**Severity: low**

`CLAUDE.md` declares "See `docs/README.md` for the full index", but the index never links: `docs/features/` (`speech-to-text.md`, `claude-devtools.md`), `docs/fast-file-manager.md`, `docs/nordvpn-installation.md`, `docs/ccy-debug-mounts.md`, `docs/ansible-lint-improvement-plan.md`. The "Project File Structure" tree (lines 195–230) lists only 7 docs files and omits `files/home/`, `files/usr/`, `extensions/`, `scripts/`, `CLAUDE/`. Quick-reference paths at lines 147/153/156 point to `optional/common/` for `play-docker.yml`, `play-python.yml`, `play-vscode.yml` — all actually core at `playbooks/imports/` (overlaps DOC-01/DOC-05 but the index itself needs the path fix).

**Recommendation:** Add the missing index entries (or consciously delete superseded docs per DOC-03/DOC-11), refresh the tree, and fix the three paths.

---

## DOC-14: `docs/development.md` references a nonexistent pre-commit setup and omits the QA gate

**Severity: low**

- Line 49: "`pre-commit install` (if using)" — there is no `.pre-commit-config.yaml` in the repo. Hooks are deployed by `playbooks/imports/play-git-hooks-security.yml` via `git config core.hooksPath scripts/git-hooks` (verified lines 20–45), exactly as `CLAUDE/SecurityRules.md` describes.
- The Testing section (lines 160–205) never mentions `./scripts/qa-all.bash`, which `CLAUDE/QA.md` mandates before every commit touching Bash/Python/Ansible. The contributor-facing guide should not have a weaker bar than the agent-facing one.

**Recommendation:** Replace the pre-commit line with the `core.hooksPath` mechanism and add a "Run QA" step (`./scripts/qa-all.bash`, plus the ESLint and ctrl+z-patch commands for the relevant file types).

---

## DOC-15: Stale playbook paths in directory guides

**Severity: low**

- `docs/configuration.md:181` — "Create in `playbooks/imports/optional/custom/`": no such directory or convention exists; actual categories are `common/`, `hardware-specific/`, `experimental/` (plus `archived/`, `untested/`), as `docs/development.md:227-231` correctly lists.
- `playbooks/CLAUDE.md` ("Verification" section) — example `./playbooks/imports/optional/common/play-comms.yml --version`: `play-comms.yml` moved to core `playbooks/imports/play-comms.yml`.

**Recommendation:** Align `configuration.md` with development.md's category list; update the playbooks/CLAUDE.md example path.

---

## DOC-16: `docs/features/README.md` "Coming Soon" lists docs that already exist

**Severity: low**

Lines 41–43 promise future documentation for "GitHub Multi-Account: Complete multi-account workflow guide" — which exists and is the declared authoritative guide (`docs/github-multi-account.md`, commit a2b536e) — and "Nord VPN Manager", whose doc exists (albeit obsolete, DOC-03).

**Recommendation:** Move GitHub Multi-Account into a cross-link, and update the NordVPN entry once DOC-03 is fixed; CCY remains a genuine gap (its main user doc is buried in `containerization.md`).

---

## DOC-17: `docs/ansible-lint-improvement-plan.md` is a stale plan document living in `docs/`

**Severity: info**

Header: "**Date**: 2025-12-03, **Status**: Planning Phase, **Git branch**: main (F42)". It is a planning document (the kind the project keeps in `CLAUDE/Plan/NNNNN-…/`), not user documentation; `scripts/lint/` exists, so it is at least partially implemented, but the doc's status/branch/file-count claims are frozen at December 2025.

**Recommendation:** Move it into `CLAUDE/Plan/` (with a proper 5-digit number) updating its status, or delete it if the lint work is considered done/abandoned.

---

## DOC-18: ctrl+z patch cross-references and missing native-binary mode in `CLAUDE/ContainerRules.md`

**Severity: low**

- `files/var/local/claude-yolo/ccy-ctrl-z-patch.js:25` and `files/var/local/claude-yolo/Dockerfile:168` both say 'See CLAUDE.md "KNOWN FRAGILE PATCH" section' — that section now lives in `CLAUDE/ContainerRules.md` ("Known Fragile Patch: Ink ctrl+z SIGSTOP Suppression"); root `CLAUDE.md` only links to it.
- `CLAUDE/ContainerRules.md` documents only the legacy `cli.js` patch mode. The actual script (lines 9–17, 109–185) also supports a **native binary** mode (Claude Code 2.1.x+ ELF/SEA packaging, flipping the optimised boolean guard `=!0` → `=!1`), with its own recovery diagnostics (`grep -ao ".\{0,5\}handleSuspend.\{0,100\}" <binary-path>`, `softFail()` at line 213). The doc's recovery instructions ("grep cli.js…, add to knownPatterns") only cover the cli.js path. Otherwise the doc is accurate: known patterns `fG5`/`wT5` exist (`ccy-ctrl-z-patch.js:70-72`), soft-fail behaviour and warning text match (`:214`), `entrypoint.sh:103` sets `CCY_DISABLE_SUSPEND=1`, and `Dockerfile:169-170` copies/runs the patch.

**Recommendation:** Update the two source-comment references to point at `CLAUDE/ContainerRules.md`, and extend ContainerRules.md with a short paragraph on the native-binary mode and its recovery steps (note: comment-only changes in `claude-yolo` files still require the version-bump discipline — Dockerfile edits need the label + `REQUIRED_CONTAINER_VERSION` bump).

---

## Positive Observations

- **Recent docs are exemplary**: `docs/ddev.md` precisely matches `play-docker.yml`'s rootful reality (including the `newgrp docker` caveat and the Podman-exception rationale); `docs/kitty.md` correctly describes the Ansible-managed block in `play-terminal-emulators.yml`; `docs/github-multi-account.md` correctly states `localhost.yml` is "a regular YAML file"; `docs/post-upgrade.md`'s `./run.bash --optional-only` flag exists exactly as documented (`run.bash:26,44`); `docs/features/claude-devtools.md` matches `play-claude-devtools.yml` and the deployed `ccdt`.
- **CLAUDE/ContainerEngines.md** is accurate and internally consistent with `play-docker.yml`, `play-podman.yml` and `vars/container-defaults.yml` (`container_engine: podman`, `podman-compose` installed by `play-podman.yml`) — it is the model the older docs should be brought up to.
- **`docs/playbooks.md` detail blocks that were spot-checked are precise** where current: pyenv versions match `play-python.yml` exactly; CCY flags (`--create-token`, `--custom`, `--custom-docker`) all exist in `claude-yolo`.
- **CLAUDE/ContainerRules.md** version-bump and soft-fail descriptions match the code (`CCY_VERSION` 3.17.0 with hash validation; Dockerfile label 2.18 = `REQUIRED_CONTAINER_VERSION` 2.18).
- **Security hygiene held up**: `environment/localhost/host_vars/localhost.yml` is gitignored (only `.dist` is tracked); no personal data found in tracked docs during this sweep.
- The `wsi` script's embedded architecture documentation (header comment) is excellent and should be the source for fixing DOC-08.

---

## Adversarial Verification Appendix

### DOC-01 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed every cited location. Reality: /workspace/playbooks/imports/play-docker.yml is explicitly rootful ('Docker (rootful) — compatibility engine for DDEV', lines 3-8) and contains legacy-rootless CLEANUP tasks (lines 14-60) that actively uninstall the rootless setup older docs describe; it is imported unconditionally by /workspace/playbooks/playbook-main.yml line 20, i.e. core, not optional. Docs contradicting this: docs/containerization.md lines 140-152 ('Optional playbook', path playbooks/imports/optional/common/play-docker.yml, 'Configures rootless Docker', 'user systemd service'), lines 176-192 (whole 'Rootless Docker' section with systemctl --user status/restart docker, journalctl --user -u docker), lines 682-686 ('Rootless mode configured'), lines 780-786 (troubleshooting via systemctl --user status docker — fails on the deployed rootful system since docker runs as a system service); docs/playbooks.md line 13 ('Rootless Docker setup'), lines 131-139 (rootless + user systemd service), line 683 (wrong optional/common path); docs/installation.md lines 195-196 ('# Docker (rootless)' + wrong path); docs/README.md lines 146-147 and 253 ('rootless' + wrong path). I verified the documented path playbooks/imports/optional/common/play-docker.yml does NOT exist — the documented install command fails outright. No compensating control: CLAUDE/ContainerEngines.md is correct but is agent-facing, not user-facing docs. Finding is real and the recommendation is correct. Severity adjusted high→medium: this is documentation drift only — no deployed-code defect, no secret exposure. The aggravating factors (docs claim a 'rootless / enhanced security' posture the system does not have, broken copy-paste commands, nonexistent path) keep it at medium rather than low, but it does not meet the bar of high (no functional or security defect in the IaC itself).

### DOC-02 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed empirically: localhost.yml uses variable-level encryption (plain YAML with !vault string values), and running 'ansible-vault view environment/localhost/host_vars/localhost.yml' with the real vault-pass.secret present fails with '[ERROR]: Input is not vault encrypted data.' All cited doc locations verified: docs/configuration.md:53-57 (view/edit), docs/installation.md:122 (Step 5 edit), docs/installation.md:399 (troubleshooting view), docs/README.md:165-169 (view/edit). The contradiction with CLAUDE/SecurityRules.md:47-56 is real — it explicitly prohibits 'ansible-vault edit' and mandates regular editor + encrypt_string. Step 5 of installation.md is broken in three ways: localhost.yml is gitignored and absent on fresh clone (only localhost.yml.dist exists), 'ansible-vault edit' cannot create files, and ansible.cfg sets ask_vault_pass=False with vault_password_file=./vault-pass.secret which Step 6 only creates after Step 5 — so the documented 'you'll be prompted to create a vault password' flow cannot occur. One correction: docs/github-multi-account.md contains no vault edit/view instructions (only two passing mentions of the vault), so the claimed contradiction with that file is unsubstantiated. Severity adjusted high→medium: documentation-only defect, no security impact, playbooks unaffected; it does break the documented manual-install path for new users but recovery is straightforward via the .dist template and SecurityRules.md.

### DOC-03 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed. /workspace/docs/nordvpn-installation.md (lines 9 and 22) is built entirely around a playbook 'play-nordvpn-cli.yml' that does not exist anywhere in the repo (only references are within the doc itself). The doc describes the official NordVPN CLI client architecture: nordvpn login/connect commands, nordvpnd systemd daemon, nordvpn group membership, and the community GNOME extension NordVPN_Connect@poilrouge.fr. The only actual NordVPN playbook is /workspace/playbooks/imports/optional/common/play-nordvpn-openvpn.yml, which is a completely different architecture: installs openvpn + NetworkManager-openvpn packages, prompts for NordVPN SERVICE credentials (stored in localhost.yml, deployed to ~/.config/nordvpn/.credentials mode 0600), and deploys the 'nord' helper script (files/home/.local/bin/nord, which exists) that manages .ovpn imports via nmcli. The doc's installation command (ansible-playbook ... play-nordvpn-cli.yml) fails with file-not-found, and every nordvpn CLI usage/troubleshooting command fails because nothing installs the nordvpn client. The recommendation (rewrite against play-nordvpn-openvpn.yml and the nord helper, or fold into docs/playbooks.md) is sound. Severity adjusted from high to medium: this is documentation-only staleness — it misleads users and wastes their time but causes no security exposure, no fail-fast violation, and no system breakage; the playbook itself even prints correct usage instructions at the end, providing a partial compensating control for users who run the real playbook.

### DOC-04 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed by reading /workspace/CLAUDE/PlanWorkflow.md and checking the filesystem. Line 5 states the doc is for "the Claude Code Hooks Daemon project"; footer says "Maintained by: Claude Code Hooks Daemon Contributors". 14 references to nonexistent ./scripts/qa/run_*.sh (auditor claimed 21 — minor overcount, likely including debug_hooks.sh/pytest refs); /workspace/scripts/qa/ does not exist, the real gate is ./scripts/qa-all.bash. scripts/debug_hooks.sh and CLAUDE/DEBUGGING_HOOKS.md (referenced line 758) do not exist. Doc mandates 3-digit 001- plan numbering, while actual plans are 5-digit (00035-00049) and git config hooksdaemon.latestPlanNumber=49 is the authoritative counter per the plan_number_helper handler. Templates (lines 60, 125-129, 369, 463, 537) include Estimated Effort/Timeline/Target Completion fields that the active plan_time_estimates hook blocks in CLAUDE/Plan/*.md. The planning skill reads this file, so the stale content is actively consumed. Severity adjusted high→medium: docs-only issue with compensating controls (hooks block time estimates and wrong numbering at write time; CLAUDE.md and CLAUDE/QA.md give the correct QA command), so impact is agent confusion/wasted turns, not system damage. Recommendation to rewrite for this repo is sound.


