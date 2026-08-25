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

- [00086-kernel-modules-absent-enumeration](00086-kernel-modules-absent-enumeration/) - A downstream live proof of `play-AB-dnf-upgrade.yml` on a guest lacking the `kernel-modules` package (present only `kernel-core`/`kernel-modules-core`) found the half-installed-kernel enumeration hard-failed instead of treating "not installed" as zero versions. Fixed with a probe-then-fail `assert`, not a blanket `failed_when: false`.

- [00088-claude-code-state-dir-stale-home-fact](00088-claude-code-state-dir-stale-home-fact/) - The same downstream live proof, past PR #36, hard-failed in `play-claude-code.yml` on `Permission denied: /root/.claude`: `claude_state_dir` trusted `ansible_facts['env']['HOME']`, which `gathering=smart` + a play-level `become: true` on the FIRST play in `playbook-main.yml` poisons to `/root` for every later play. Fixed to match the file's own `/home/{{ user_login }}` convention used everywhere else in it.

- [00089-ssh-handling-runner-token-guard](00089-ssh-handling-runner-token-guard/) - A carve-out from Plan 00068 (unmerged). Not a bug fix — `gh` already gives an exported `GH_TOKEN` precedence — but it makes that explicit and drops the `gh auth token` dependency for a runner authenticating purely by token via `--no-ssh`.

- [00068-ccy-ci-runner-variant](00068-ccy-ci-runner-variant/) - `ccy` is built for one situation — a human at a workstation TTY — so a consuming repo (`actions-hub`, private) needing it on a headless GitHub runner built its own stack instead. This plan designs the missing capabilities **at source**. **Seven audit rounds; Rounds 1–6 each found blockers and Round 7 came back clean, closing the loop** (Task 6.4) and releasing the held one-page restatement (`reports/one-page-restatement.md`). Twenty-four corrections, **nine of them found without a review** — the last four by a mechanical sweep of "every correction that assigns work, checked against the task that owes it", which nobody, including seven hostile rounds, had ever walked. Two failure modes dominate and both have mechanical fixes: **twelve instances** of *a true statement about a check presented as a stronger statement about the world* (a citation can be accurate and point at code that has never executed — D10; a quality gate can be met while the work it governs is false — D15), and **ten instances** of corrections written correctly and never propagated to the tasks they governed. Three consecutive rounds shrank the CI framing — Round 1 took the original thesis, Round 2 took the launcher-mediated CI path, Round 3 (**D6**) took provisioning as well, establishing that **the `claude-yolo` launcher is never on the CI path at all**: `play-claude-yolo.yml:338-343` and lts-infra's project-image task both call `podman build` **directly**, and the base-image task only *reads* the launcher to check version coupling. So the retracted "build-and-exit mode" was never needed, and **Phase 2 (`--non-interactive`) plus token-by-value are desktop-only hardening** — still worth doing on their own merits, no longer CI enablers. What `ccy` owes CI is exactly two things: the image `LABEL` identity and a CI entrypoint. The owner steered that each project should get its own ccy runner *the normal way* (its own `.claude/ccy/Dockerfile`) with CI safety and MCP added ad-hoc or by full customisation. The restatement: **`ccy` is THREE layers — image, entrypoint, launcher — and the steer is an image mechanism**, so it delivers tooling superbly and cannot deliver safety, because the entrypoint is *inside* the image (`Dockerfile:215`; no `ENTRYPOINT` in any project template), so taking the base image also takes `GH_TOKEN`-or-die, `gh auth login`, the checkout symlink and the trust flags. **E10**: ccy asserts "this workspace is trusted" in **four** places, the fourth being `entrypoint.sh:269-274` sourcing the workspace's own `ccy.env` as shell and `exec`ing `CCY_CLAUDE_WRAPPER` from it — the checked-out tree controls the command that runs. Hence **Decision 4: no permission surface** (price stated — trusted automation only), **5: that scope asserted at the call site**, **6: adopt a second small CI entrypoint** — the correction for a defect three codebases hand-rolled and two got wrong. Census corrected **35 → 46** prompts, classified by call graph (32 abort, 6 spin, 6 by path); the `--rebuild` staleness identity must move from host-local `$HOME/.cache` state into an image `LABEL` — now **actually specified** in `reports/label-convention-spec.md` (**D10**), which found that the rebuild decision reads **two** cache files, not one: the project Dockerfile hash *and* the base version it was built against (`claude-yolo:1455`, compared `:1499`), the latter having no `LABEL` convention in any of the three repos. A spec covering only the first would have passed every round and left half the decision in `$HOME`. **D10 also withdrew the claim that this approach is proven in production**: the cited proof (`actions-hub/ci.yml:97`,`:99`) is a branch that has never executed — that repo ships no `.claude/ccy/Dockerfile` and returns at the baseline path — and the citation survived D5, D8 and a re-verification. Corrections found by trying to build on the plan's own text: **`claude-yolo:full` never existed**, **Ansible never builds `claude-yolo:base`** though three docs offer it, and `--no-network` does not isolate. Design + audit only, **no code changes** (`files/`, `playbooks/` provably untouched); host-run items remain in `triage.bash`.

- [00069-plan-md-edited-in-place](00069-plan-md-edited-in-place/) - Plan 00068's `PLAN.md` hit 1,894 lines / 137 KB: **50% blockquote corrections, 2% task checkboxes**, never once shrinking across 34 commits. Cause was a convention **00068 invented and imposed on itself** (its Task 6.3) — no project rule says corrections append to `PLAN.md`; every append-only statement in the docs scopes itself to `JOURNAL/`. `PLAN.md` is a living git-tracked document: **edit it in place, git is the history**. State that boundary in `PlanWorkflow.md`, keep narration in `JOURNAL/`, and replace the remembered coherence `grep` (which failed **fifteen** times in 00068, twice while cataloguing itself) with a mechanical check.

- [00072-ccy-assert-rootless-engine](00072-ccy-assert-rootless-engine/) - `ccy` runs `claude --dangerously-skip-permissions` (`claude-yolo:2792`) and bind-mounts the project at `/workspace`; the whole safety case rests on the container engine being **rootless**, so container uid 0 maps to an unprivileged host user through a userns. Nothing asked the engine. Two things that **looked** like checks were doing the work: a docker-only branch comparing the **context name** to the string `rootless` — a context can be named anything regardless of where it points, and `DOCKER_HOST` overrides context entirely — and, for podman, the comment *"Podman running as non-root is inherently rootless - no check needed"*, an **inference standing in for a check**. The uid-0 guard at `:1178` is correct and kept, but it constrains the **client**; a non-root user can still drive a rootful engine via `CONTAINER_HOST`/`DOCKER_HOST` pointing at a system socket. Now `ccy` asks the engine (`{{.Host.Security.Rootless}}` / `{{.SecurityOptions}}`) and refuses otherwise. **The load-bearing decision is that UNKNOWN IS A REFUSAL** — an engine whose posture cannot be read is not an engine known to be safe, and a guard that treats silence as safety passes hardest exactly when it can see least. That is not a hypothetical: Plan 00068's group-F probe **measured** the same shape, where asking podman for an absent label returns exit 0 and **zero bytes**, so comparing two unknowns returns "equal" and reports the safe-sounding answer having measured nothing. The decision is split into a **pure** `engine_rootless_verdict()` in `common-pure.bash` — the library whose contract is "no engine dependencies" — so the rootful and unreadable cases are unit-testable with **no engine present**, which matters because a rootful daemon cannot be conjured on demand to prove the guard notices one. 15 cases in `scripts/test-ccy-rootless-guard.bash`, **wired into CI** (the existing `test-ccy-ssh-probe.bash` is the precedent for a ccy bash test that runs nowhere), and **verified to discriminate by perturbation**: flipping `false → rootless` failed exactly 2 assertions — the rootful case and the discrimination check — with syntax still valid and the other 13 passing, so the failures are semantic rather than breakage. Found while answering a challenge to a `/root/.claude` citation, where the correct answer was that `ccy` **already** refuses to run as root and `/root` is the in-container HOME of a userns-mapped unprivileged identity. Also corrects `Dockerfile:211`, which claimed a `--user` flag at runtime that is **never passed** (`DOCKER_FLAGS` is only ever `-i`/`-it`).

- [00073-headless-sudo-password-file](00073-headless-sudo-password-file/) - Headless `run.bash` **requires `NOPASSWD:ALL`** and hard-fails without it (`run.bash:193-195`); this adds a second, equally supported credential — `RUN_BASH_SUDO_PASSWORD_FILE`, a `0600` file — so an unattended install works on a box with ordinary password sudo. **Not a correction of Plan 00063**: that plan chose the requirement deliberately (D4, Decision 5) and its risk table already carries the password-sudo row as "mitigated by fail fast", which was right for v1; and the repo's own docs name the gap as a limitation (`docs/headless-server-install.md:249`, `run.bash:503-504`). The driver is a CI runner host with **two** accounts — a normal user owning the ccy install and the rootless podman store, and a locked-down `actions-runner` with one `NOPASSWD` rule that launches ccy *as* that user — a containment that only holds if the normal user does **not** itself hold `NOPASSWD:ALL`, else `actions-runner → normal user → root` is a ladder. Chosen over grant-then-drop on **failure mode, not window safety**: during provisioning the two are equivalent (anything running as that user reaches root either way, and claiming otherwise would be the overclaim this repo keeps catching), but a sudoers file that fails to be removed leaves permanent passwordless root, while a tmpfs password file that fails to be shredded dies at the next boot. **Smaller than it looks**: `rg -c "sudo "` says 32, but reading the hits rather than counting them gives **12** real privileged commands in one contiguous region (`:1485`–`:1620`), 2 `sudo reboot` already skipped under `RUN_BASH_REBOOT=0`, 3 probes and 15 comments. Three of the four pieces reuse machinery that exists: the preflight asserts *one of two credentials* (an option set decided once, never a skip-gate); the 12 calls route through a `_sudo` wrapper adding `-A` with `SUDO_ASKPASS`, an idiom **`hl_ssh_agent_start:222-248` already implements** for the SSH passphrase down to the quoted heredoc that keeps the secret out of the helper's own text; the two Ansible invocations gain `--become-password-file`, **verified present** in ansible-core 2.19.11; and cleanup needs nothing new because `hl_cleanup`'s EXIT trap already shreds `HL_SECRET_FILES`. `HL_SUDO_OPTS` is empty on every pre-existing path, so NOPASSWD and interactive runs must emit **byte-identical** argv — asserted, not assumed. Carries Plan 00063's **V3.7 limitation forward explicitly** rather than inheriting it silently: `sudo -k -n true` is a weak probe (a command-scoped rule passes `true` and fails `dnf`), so the password probe proves *the password authenticates*, not *that this user may run `dnf`*, and `ALL`-scope stays the documented requirement. Blocks lts-infra Plan 00031 Task 3.2.

- [00074-grub-cgroup-check-reports-absence-it-cannot-prove](00074-grub-cgroup-check-reports-absence-it-cannot-prove/) - `run.bash`'s legacy-grub cgroup step held **two opposite defects** in one block, found while implementing Plan 00073 and deliberately not bundled into it. **(1) It reported an absence it could not prove**: `grubby --info=ALL 2>/dev/null | grep -q …` turned a *failing* grubby into `✓ No legacy cgroup configuration found`, because after the pipe an empty stdout and a genuine negative are the same non-zero — and `pipefail` does not separate them either, since "grubby failed" and "grep found nothing" both route to the `else`. **(2) It reported a failure it HAD proven, and continued**: `error()` is `echo -e` and does **not** exit, so a verifiably-failed removal printed its message, printed manual instructions, and let the installer run on to exit **0**. The second is worse and was **not** the one originally flagged — opening the block to fix defect 1 is what surfaced it, which is the argument for fixing a thing rather than only noting it. Defect 1 is ignorance mistaken for knowledge; defect 2 is knowledge discarded. Now four states get four distinct outcomes, and the narrowing that keeps the blast radius small is that **only a NON-ZERO grubby exit is fatal** — exit 0 with no legacy args is a real negative answer, so a box with unusual boot entries is unaffected rather than newly broken. Adds `fatal <step> <what> [debug]`, the both-modes abort (`hl_abort` when headless, `error` + `exit 1` otherwise) that interactive code had never had — **its absence is *how* the skip-and-warn arose**, by accident rather than by choice. Extracted to a top-level `check_legacy_grub_cgroup` **so it can be tested at all**: both defects survived precisely because inline code inside `main()` is unreachable by any test. A stub `grubby` on `PATH` drives all four states, which is not a weaker substitute for a host run — a real box exhibits exactly **one** state, so the stub exercises strictly more. **Discrimination proven by perturbation, not asserted**: reintroducing each defect separately in a fixture fails exactly one leg each time (defect 1 → `broken`, defect 2 → `stuck`, message control → `clean`), a perfect diagonal, versus the RED run's uniform four-way failure which proved nothing about any leg. Also moves the `RUN_BASH_VERSION` changelog — **4,791 characters on a single line**, a changelog wearing a comment's clothes — into `docs/run-bash-changelog.md`.

## Completed Plans

- [00090-resync-ccy-ci-runner-branch-onto-f44](Completed/00090-resync-ccy-ci-runner-branch-onto-f44/) - `origin/plan-00066-ccy-ci-runner` (Plan 00068) diverged 205/90 commits from `F44`; a dry-run showed only 18 of 107 changed files conflict. Resolved those 18, fixed a self-contradicting `ruff.toml` and a dropped changelog entry an `infra-reviewer` round caught, and landed the rest via [PR #39](https://github.com/LongTermSupport/fedora-desktop/pull/39) (merge commit `102243f`), not a bypass push.

- [00085-headless-path-local-bin](Completed/00085-headless-path-local-bin/) - A downstream live proof of the composed PR #33/#34 headless mechanisms found a third, unrelated blocker: `ansible-galaxy: command not found` under a non-interactive `sudo -u` invocation, since pipx's `~/.local/bin` shims are never put on PATH there. Exports PATH right after the pipx install block. Merged (`dac4f7c`).

- [00070-documentation-drift-audit](Completed/00070-documentation-drift-audit/) - An audit of `CLAUDE.md`, `CLAUDE/`, `.claude/rules/`, `docs/`, `README.md` and the nested `*/CLAUDE.md` files found **17 confirmed defects and 6 suspected**; fixing them surfaced **six more**, so **23 confirmed findings were fixed** and all six suspected resolved. The defect is **drift**, not sloppiness: the code and the newer docs are disciplined; the older docs were never re-read after the changes that invalidated them. **Five docs actively caused harm** — a reader following them ends up worse off than if the doc did not exist. Four were found by the audit: `root_dir: "{{ inventory_dir }}/../../"` taught in four places including the copy-paste template new contributors are told to use, the pattern `CLAUDE/AnsibleStyle.md:20` bans **by name** because it makes `vars_files` entries silently skip; `helpers/CLAUDE.md:76`'s test command measured to run **0 tests and print `OK`**; `play-nordvpn-openvpn.yml`'s instruction to run `./vault.bash set` on a variable the same play wrote seconds earlier, which `vault.bash:171` always refuses — while `vault.bash:173` **already printed the right answer**, so only the playbook's own message was wrong; and `extensions/CLAUDE.md` mandating `npm run lint` while citing as its authority the file that forbids it. The fifth was found only while fixing the others: `docs/playbooks.md` described `play-cloudflare-warp.yml` as **installing** WARP when the play **uninstalls** it by design — the RPM needs `webkit2gtk3`, retired in F44, so it is uninstallable *and blocks the F43→F44 distupgrade*. The audit missed it because it checked whether catalogue entries **exist**, not whether they still point the same direction as the code. One finding was a **source** bug, not a doc bug: `play-lxc-install-config.yml`'s task was named `Enable LXD Copr Repository` while enabling `ganto/lxc4` and installing only `lxc` — `lxd` appeared exactly **once** in the whole file, in that name, and that name is where the doc's "installs LXC and LXD packages" came from. **The lasting deliverable is `scripts/qa-docs.bash`**, a new gate (seventh JSON-merged, eighth overall) backed by `helpers/docs/link_check.py` with 42 unit tests. Its inclusion bar was *the check would have caught a real finding in this audit* — links/anchors, playbook-catalogue completeness, topic-file index all cleared it; nothing speculative was added. It found **four defects five read-only agent passes had missed** and showed a hand-written finding ("two broken links") had undercounted by half. Two lessons outlast the fixes: **the checker's first version was wrong in the same class as the defects it hunts** — it collapsed whitespace when slugging headings, but GitHub hyphenates each space, so `Fail Fast — HARD RULE` needs a *double* hyphen; it under-reported by 32, and was corrected against author-written anchors observed working in rendered documents rather than against recall. And **the audit's own new report contained two broken links**, citing `.claude/rules/*.md` files belonging to the **outer lts-infra checkout** that vendors this repo — the same working-directory-versus-known-root confusion that produced three other errors the same day, and that the owner caught a fifth instance of independently. `CLAUDE/Plan/**` is excluded from the gate **on principle** (a core gate sweeping plan content is a core→plan dependency, so archiving a plan would flip core CI with no core file changing); the ~60 broken anchors that exclusion hides in Plan 00049 are recorded, not discarded. Closes the H/H "docs re-drift on the next code change" risk with a gate rather than a promise: `qa-all.bash` rc=0 across 488 files, proven to fail on a broken anchor and pass when restored.

- [00071-qa-gate-correctness](Completed/00071-qa-gate-correctness/) - `./scripts/qa-all.bash` — the gate `CLAUDE.md` makes **mandatory** before every Bash/Python/Ansible commit — exited **1 on a clean tree**, from three unrelated defects, all found incidentally while running it for another plan. Same class as Plan 00067, one turn worse: 00067's gates checked **nothing**; these checked the **wrong things**, which looks like work being done. **(1)** The Ansible fail-fast grep scanned raw lines with no YAML comment stripping, so `play-systemd-user-tweaks.yml:242` — a comment recording that `ignore_errors: true` had been *removed*, and why — was reported as a violation; the incentive that creates is to delete the explanation, leaving the gate green and the repo dumber. Fixed by stripping comments *after* checking `FAIL-FAST-OK` on the full line (that annotation lives in a comment too), proven by four controls including the two a naive strip breaks. **(2)** `qa-python.bash` has two discovery passes and only one carried the `venv`/`.venv`/`__pycache__` exclusions, so the gate linted `.claude/untracked/venv/bin/pip` — third-party console scripts — as repo-owned code; the directory being *half*-excluded is exactly what made it invisible. Both passes now share one `PY_EXCLUDES` array, and `.claude/ccy/*` + `.claude/skills/*` are excluded whole to match `qa-bash.bash`. **(3)** `ruff.toml` never enumerates `select`, so the enforced ruleset **is** ruff's default set — which grows every release — while its own comment claimed "E, F, W", **wrong by ~350 rules**; with ruff unpinned at all three install sites the gate's strictness was whatever happened to be installed, and main reddened with no commit behind it. A first hypothesis that a parent `ruff.toml` was leaking in from the outer lts-infra checkout was **disproved by `ruff check --isolated`** before it could become a confident, well-cited, wrong finding. `/.ruff-version` is now the single source of truth read by the ccy Dockerfile and CI; the third path (`dnf: ruff`) tracks Fedora and cannot be pinned from here, so `qa-python.bash` **asserts** the version — converting an unpinnable divergence from silent to loud (wrong pin ⇒ exit 2 naming both values). All 37 remaining findings fixed in source (161 helper tests still pass, proving the `with`-rewrites behaviour-preserving); the two `BLE001` in `clip-scan` are exempted rather than "fixed" because both handlers are correct, and the exemption is recorded as **a deliberate design decision, not an external cause**, so it is not mistaken for precedent. Two self-inflicted errors recorded: a `# noqa` reflex that the daemon correctly blocked, and a `replace_all` on `stats` that renamed six sites which still used it. Gate now exits **0**, 432 files. **Unmitigated risk, stated: the QA gates still have no tests of their own** — every fix here was proven with a fixture that was then thrown away, and that is now three gate defects across two plans.

- [00067-qa-gates-inert-in-nested-checkout](Completed/00067-qa-gates-inert-in-nested-checkout/) - `./scripts/qa-all.bash` — the gate `CLAUDE.md` makes **mandatory** before every Bash/Python commit — reported `✓ bash: 0 files OK` while checking **nothing**, in any checkout living under a path segment it excludes (lts-infra vendors this repo at `untracked/repos/fedora-desktop`). Cause: the exclusions are *repo-root-relative* in intent but were written as unanchored globs, and `find -path` matches the **whole** printed path, so `! -path "*/untracked/*"` excluded the entire repository. 112 bash files, all JS and (masked behind `ruff not installed`) all Python went unscanned, exit 0 throughout — a control silently degraded to a no-op, the same outcome `CLAUDE.md`'s ban on `\|\| true` exists to prevent, achieved without any banned token. Fixed by anchoring root-relative exclusions to `$REPO_ROOT` (leaving `.git`/`node_modules`/`__pycache__`/`.venv` any-depth) **plus** a zero-file guard in each gate that exits 2 rather than passing — anchoring stops this instance, the guard stops the class. Proven by measurement in the affected checkout: bash 0 → 112 (105 shellcheck findings surfaced, 0 `error`-level), js 0 → 6, python 0 → 38 (via a stub analyser, since `ruff not installed` short-circuits before discovery); negative control confirms the 167 bash files in excluded trees are still skipped; all three guards fire `exit 2` on an empty tree. Recorded in `CLAUDE/QA.md`, along with a follow-up finding of the same class left unfixed on purpose (`shellcheck` optional vs `ruff` required).

- [00060-stderr-hygiene-coding-standard](Completed/00060-stderr-hygiene-coding-standard/) - A generated `gh-<alias>()` wrapper echoed its "Switching to <user>..." status line to stdout, polluting `$(gh-<alias> … --json)` captures and breaking `jq` when the account wasn't the box default; fixed with `>&2` (`f2c7f93`). A repo-wide audit (bash executables, generated dotfile functions, playbook shell blocks, `scripts/`, `helpers/`) found **0 other real bugs** — the repo is already uniformly disciplined. Shipped `CLAUDE/StderrHygiene.md` as a first-class coding standard (stdout = captured payload; all chatter → `>&2`; help/report commands carved out), wired into CLAUDE.md + back-linked from InteractiveScripts.md rule 08 (`62ac5c5`).

- [00087-gitleaks-generic-key-false-positive](Completed/00087-gitleaks-generic-key-false-positive/) - Fixes a gitleaks CI false positive (`generic-api-key` on a "Medium/byteiota" source citation) by rephrasing the flagged text rather than growing `.gitleaks.toml`'s allowlist. Merged (`b15fc4d`).

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
