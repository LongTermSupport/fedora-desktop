# Opportunities Audit

> **Redaction note**: real identifiers quoted as evidence are replaced here with
> `<email-a>`-style placeholders, per the same public-repo rule this audit enforces
> (and to avoid re-leaking the SEC-01 data in a new tracked file). The cited
> file:line references locate the actual values in the repo.

## Scope & Method

This audit hunts for improvement opportunities — not defects — across the fedora-desktop repository: dead code and unused artefacts (YAGNI), DRY extractions, modernisation, abandoned plans worth reviving, testing gaps on high-risk code, and developer-experience improvements.

Method: systematic enumeration rather than sampling. I listed every playbook under `playbooks/` (44 core + 28 optional/common + 7 hardware-specific + 4 experimental), every script under `scripts/`, every deployed binary under `files/usr/local/bin/` and `files/home/.local/bin/`, the CCY container system under `files/var/local/claude-yolo/`, the GNOME extensions, the docs index, and all 40+ plan directories under `CLAUDE/Plan/`. Cross-references were established with `rg`/`git ls-files` (e.g. every deployed file was checked against playbook references; every helper script was checked for inbound references). Excluded: `.git/`, `node_modules/`, `untracked/`, `.claude/hooks-daemon/`, `roles/vendor`.

## Summary

The repository is in good shape: fail-fast compliance is at 100% (zero unannotated `failed_when: false`/`ignore_errors` across all playbooks), FQCN module names are adopted everywhere, and the QA tooling is genuinely LLM-friendly. The opportunities cluster in five areas:

1. **Stray tracked artefacts** at the repo root (two typo'd empty files and a non-breaking-space-named file containing a directory listing of the user's home — the strongest finding here, with public-repo hygiene overlap).
2. **QA gap for Ansible**: the project's own memory notes document Ansible 2.19 parser gotchas that `qa-all.bash` cannot catch, yet neither `ansible-playbook --syntax-check` nor the existing `scripts/lint` (ansible-lint) wrapper is wired into the QA gate.
3. **No automated tests for CCY** — 6,600+ lines of high-risk bash (the daily-driver container wrapper) with only one manual diagnostic script.
4. **Plan-state drift** — several plans listed "Active" in the index are demonstrably complete in code (PHPantom LSP, claude-devtools, semgrep QA, CLAUDE.md restructure), and three overlapping claude-devtools plans coexist.
5. **DRY extractions** — the "resolve latest GitHub release" pattern is implemented four different ways across six playbooks, and the bash colour/log-helper boilerplate is duplicated in ten or more scripts.

All findings are severity info/low/medium per the opportunities remit.

---

## OPP-01: Stray tracked artefacts at repo root (`localhost`, `loclahost`, and a U+00A0-named file leaking a home-directory listing)

**Evidence**

- `/workspace/localhost` and `/workspace/loclahost` — both zero-byte, both tracked in git (`git ls-files` confirms). Introduced in commit `6552e1ce` ("Add comprehensive qobuz-player controller script (qp)"). The `loclahost` spelling makes clear these are accidental shell artefacts (e.g. a mis-fired `ansible-playbook -i localhost` redirect), not intentional files.
- `/workspace/ ` — a tracked 3,060-byte file whose name is a single U+00A0 (no-break space) character (`find -maxdepth 1 -type f | cat -A` shows `M-BM- `). Introduced in commit `3008ef68` ("feat: add toggle mode, debug logging, and panel indicator UI"). Its content is an `ls -la` dump of the user's `~/.local/bin`, e.g.:
  ```
  lrwxrwxrwx. 1 joseph joseph   56 Aug 29 08:09 ansible -> /home/<user>/.local/share/pipx/venvs/ansible/bin/ansible
  -rwxr-xr-x. 1 joseph joseph  11K Nov 26 22:34 git-account-helper
  ```

**Impact**

Beyond clutter, the U+00A0 file violates the repo's own public-repo hygiene rules (`CLAUDE/SecurityRules.md`: never commit "file paths with usernames, home directories"). The username `joseph` is on the public-by-design allowlist, but the file leaks a full inventory of locally installed tooling and `/home/<user>/...` paths — exactly the class of content the rules prohibit. It is also invisible in most directory listings (whitespace name), so it will never be noticed organically.

**Recommendation**

Delete all three files (`git rm 'localhost' 'loclahost' "$(printf '\xc2\xa0')"`). The content is already in public git history, so per `SecurityRules.md` decide whether history purging is warranted (likely not — the username is allowlisted and no credentials are present — but the security auditor should make that call). Consider extending `scripts/git-hooks/pre-commit` to reject staged filenames that are empty/whitespace-only, which would have caught both accidents.

---

## OPP-02: QA gate has no Ansible syntax validation despite documented 2.19 parser gotchas

**Evidence**

- `/workspace/scripts/qa-ansible.bash` (33 lines total) only greps for unannotated `failed_when: false` / `ignore_errors` patterns — nothing else.
- `/workspace/scripts/qa-all.bash:56` invokes it as the sole Ansible check.
- No script under `scripts/` invokes `ansible-playbook --syntax-check` (`rg 'syntax-check' scripts/` returns nothing).
- Meanwhile a full ansible-lint wrapper already exists at `/workspace/scripts/lint` (with JSON output, summaries and `--fix` support) plus a tuned `/workspace/.ansible-lint` config — but it is not part of `qa-all.bash` and is only mentioned in `docs/development.md`.
- The project's own memory notes record two Ansible 2.19 parser traps that PyYAML-level checks cannot catch (apostrophes/backticks in `#` comments inside `shell: |` blocks; `: -<letter>` in unquoted task names) and explicitly state "qa-all.bash do[es] not catch this — use `ansible-playbook --syntax-check`".

**Impact**

The single mandated pre-commit gate (`./scripts/qa-all.bash`) passes playbooks that will fail at deploy time on the host with Ansible 2.19 parse errors. The repo has already been bitten by this class of bug (hence the memory notes). The fix is cheap because the tooling already exists in-repo.

**Recommendation**

Add a syntax-check pass to `qa-ansible.bash`: loop `ansible-playbook --syntax-check` over `playbooks/playbook-main.yml` and the optional `play-*.yml` files (or at minimum over files changed since HEAD). Optionally also wire `scripts/lint` (ansible-lint) into `qa-all.bash` as a non-blocking or blocking stage — it is already built, configured, and documented but currently orphaned from the mandatory workflow. Note: ansible-core must be available where QA runs; in the CCY container this is an IaC dependency to add to the container image (see `CLAUDE.md` Missing Dependencies rule), or the check can be made host-only with an explicit hard failure message in the container.

---

## OPP-03: No automated test coverage for CCY — the highest-risk bash in the repo

**Evidence**

- `/workspace/files/var/local/claude-yolo/claude-yolo` — 2,633 lines (v3.17.0, `CCY_VERSION` at line 12).
- `/workspace/files/var/local/claude-yolo/lib/` — 4,016 further lines across 7 libraries (`token-management.bash` 841, `dockerfile-custom.bash` 776, `network-management.bash` 746, `common.bash` 614, `docker-health.bash` 544, `ssh-handling.bash` 437, `common-pure.bash` 58).
- The only tests touching this code are `/workspace/scripts/test-ccy-ssh-probe.bash` (a manual, host-run diagnostic for one function chain in `ssh-handling.bash`) and `/workspace/scripts/qa-ctrl-z-patch.bash` (which tests only `ccy-ctrl-z-patch.js`). `rg 'bats|shunit'` across `scripts/` and the CCY tree finds no test framework.
- Risk is increasing: Plan 00048 just made the host `cc` wrapper source `token-management.bash` and the newly factored `common-pure.bash`, so regressions in these libraries now break both the container workflow and the host Claude Code launcher.

**Impact**

shellcheck + `bash -n` (via `qa-bash.bash`) catch syntax, not behaviour. Functions like the token chooser's new "host" mode, SSH-URL parsing in `ssh-handling.bash`, and Dockerfile-template generation in `dockerfile-custom.bash` are pure-ish and eminently unit-testable, yet every change is verified only by manually running CCY. A regression in `claude-yolo` bricks the user's primary daily workflow.

**Recommendation**

Introduce a bats-core suite (e.g. `tests/ccy/`) targeting the pure functions first: `common-pure.bash` (trivially sourceable by design), `ssh-handling.bash` URL/probe parsing (port the assertions already written in `test-ccy-ssh-probe.bash`), and `select_token`'s mode dispatch with a fixture token pool. Install bats via a playbook (IaC rule) and add a `qa-ccy-lib.bash` stage to `qa-all.bash`. Even 30–40 assertions would convert the repo's riskiest code from manually-verified to regression-guarded.

---

## OPP-04: Plan index drift — implemented work still listed as "Active"; three overlapping claude-devtools plans

**Evidence** (from `/workspace/CLAUDE/Plan/README.md` "Active Plans" vs reality):

- **030-phpantom-lsp** — README/PLAN say research with decision gate "In Progress", but PHPantom is fully shipped in CCY: `files/var/local/claude-yolo/Dockerfile:7-14` builds `phpantom_lsp` 0.7.0 from source, `:228-231` installs the binary and plugin, `entrypoint.sh:175-182` installs the plugin into `/root/.claude/plugins/`, and `files/var/local/claude-yolo/plugins/phpantom-lsp/.claude-plugin/plugin.json` is tracked.
- **009 / 011 / 013-claude-devtools** — three sequential plans for the same feature, all listed Active, 013 marked "Not Started" — yet `playbooks/imports/optional/common/play-claude-devtools.yml` exists and is a polished, container-engine-aware playbook.
- **020-semgrep-custom-bash-rules** — `PLAN.md:3` says "🟢 Complete" (and `.semgrep/bash-conventions.yml` + `qa-patterns.bash` exist) but it is listed under Active.
- **024-claude-md-modular-restructure** — `PLAN.md:3` says "🟢 Complete" (and the modular CLAUDE/ structure is live) but listed Active.
- **032-compression-helpers** — "awaiting host deployment" while `play-compression-helpers.yml`, `files/usr/local/bin/compress` and `uncompress` are all in the tree.

**Impact**

The Plan Commit Rule exists precisely to prevent "drift between plan state and code state"; the index is the first thing agents read when checking for existing plans. Stale Active entries cause duplicate planning (the 009→011→013 chain is itself evidence of this failure mode).

**Recommendation**

Run a one-off triage session: verify each Active plan against the codebase, move completed ones (at least 020, 024, 030, and the claude-devtools trio collapsed into 013) to `Completed/`, mark superseded iterations Cancelled, and update README.md statuses. Consider a tiny QA helper that diffs `PLAN.md` `**Status**:` lines against the README index sections.

---

## OPP-05: Abandoned plans worth an explicit revive-or-cancel decision

**Evidence** — plans listed Active whose `PLAN.md` status shows no progress:

- **023-hostname-based-inventory** (`PLAN.md:3` "Not Started") — migrate from hardcoded `localhost` inventory to hostname-based per-machine `host_vars`, enabling multi-laptop support. The Completed 00044 laptop-health audit and the F43 branch workflow suggest multiple machines are real; this is the highest-value revival candidate.
- **027-contextual-shell-history** (`PLAN.md:3` "Not Started") — Atuin shell history. Self-contained, small playbook.
- **014-whisper-model-manager** (`PLAN.md:3` "In Progress (Research & Planning)", with some tasks already ticked) — Textual TUI for Whisper models.
- **002-nordvpn-openvpn-manager** — listed Active since the early numbering era; `play-nordvpn-openvpn.yml` and `docs/nordvpn-installation.md` exist, so part of the scope shipped.
- **004-comprehensive-feature-documentation** (`PLAN.md:3` "⬜ Not Started") — partially overtaken by the `docs/` build-out from Plan 024/006.
- **007-speech-to-text-resource-leak-fixes** — "🔄 In Progress" with two sub-items still "⬜ Pending decision"/"To be implemented if requested".

**Impact**

None of these block anything, but each stale Active entry adds noise to the planning workflow the repo's agents are instructed to consult.

**Recommendation**

For each: revive (023 and 027 look genuinely useful), or mark Cancelled/Complete with a one-line rationale, mirroring how 00036 was cleanly cancelled with a pointer to its successor.

---

## OPP-06: DRY — "resolve latest GitHub release" implemented four different ways across six playbooks

**Evidence**

- `ansible.builtin.uri` + JSON variants: `playbooks/imports/optional/hardware-specific/play-displaylink.yml:51`, `playbooks/imports/optional/common/play-darktable-ai-build.yml:337`, `playbooks/imports/optional/common/play-photography.yml:101` and `:171` (each followed by its own register/changed_when/FAIL-FAST-OK advisory block).
- `shell: curl -s … | jq -r '.tag_name' | sed 's/^v//'` variants: `playbooks/imports/optional/common/play-qobuz-cli.yml:24-34` (hifi-rs) and `:43-55` (qobuz-player), each with its own `set_fact` pin-or-latest dance.

**Impact**

Six copies of the same concern with divergent error handling (some advisory `failed_when: false`, some hard-fail), divergent rate-limit behaviour (no `Authorization` header anywhere — unauthenticated GitHub API calls are limited to 60/hour), and divergent version-prefix stripping. Every new "install from GitHub releases" playbook re-invents it.

**Recommendation**

Extract a shared include — `tasks/github-latest-release.yml` taking `repo:` and returning `latest_tag` — following the existing `tasks/ensure-jq.yml` precedent (already consumed by three playbooks via `include_tasks`). Centralise the pin-or-latest logic and the FAIL-FAST-OK advisory policy in one place; optionally support a `GITHUB_TOKEN` env passthrough to dodge rate limits.

---

## OPP-07: DRY — bash colour palette and log-helper boilerplate duplicated in 10+ scripts

**Evidence** — `RED='\033[0;31m'` (plus GREEN/YELLOW/NC and usually `die()/ok()/warn()/info()` helpers) defined independently in: `run.bash:56`, `scripts/lint`, `scripts/gh-account-setup.bash:44`, `scripts/check-displaylink-status.sh:10`, `scripts/setup-rclone.bash:27-40`, `scripts/nvidia-status.bash:11`, `scripts/git-hooks/pre-commit:10`, `scripts/git-hooks/commit-msg`, `fedora-install/push.bash:17`, `fedora-install/pull-projects.bash` (and again inside the CCY lib tree, which legitimately must stay self-contained for deployment).

**Impact**

Pure boilerplate duplication; the helper sets have already drifted (`die` vs `fail`, differing icons/format). Low cost to live with, but a one-file fix.

**Recommendation**

Create `scripts/lib/colours.bash` with the palette + `die/ok/warn/info/header` helpers and source it from repo-local scripts (`SCRIPT_DIR` resolution is already present in each). Leave `fedora-install/` and `files/var/local/claude-yolo/` self-contained since they run detached from the repo checkout.

---

## OPP-08: Orphaned deployed file — `files/usr/local/bin/debug-pipewire.bash` is tracked but never deployed

**Evidence**

Every other file in `files/usr/local/bin/` is deployed by a named playbook task (`gh-print-auth-url` → `play-github-cli-multi.yml:50`, `shutdown-with-update` → `play-basic-configs.yml:160`, `ssh-suspend-guard` → `play-prevent-ssh-suspend.yml:11`, `RapidRAW` → `play-photography.yml:92`, `qp` → `play-qobuz-cli.yml:114`, `watermark` → `play-image-watermarking.yml:40`, `compress`/`uncompress` → `play-compression-helpers.yml:70`, `darktable-ai` → two playbooks, `manage-kernel-versions.py` → `play-advanced-kernel-management.yml`). `debug-pipewire.bash` has zero playbook references — its only mentions are in two historical plan documents (`CLAUDE/Plan/018-…/codebase-analysis.md`, `CLAUDE/Plan/020-…/PLAN.md`).

**Impact**

A `files/` entry that no playbook deploys is dead weight under the repo's own IaC model ("the playbook IS the source of truth") — it can never reach a system through the sanctioned workflow.

**Recommendation**

Delete it (YAGNI), or if PipeWire debugging is still wanted, deploy it from `play-speech-to-text.yml` or `play-hd-audio.yml` where it topically belongs.

---

## OPP-09: `run.bash` prints a stale path for the Python playbook

**Evidence**

`/workspace/run.bash:1453-1454`:
```bash
echo -e "  ${ARROW} Python development environment (pyenv + pyenv versions):"
echo -e "    ${BOLD}./playbooks/imports/optional/common/play-python.yml${NC}"
```
`play-python.yml` no longer exists at that path — it lives at `playbooks/imports/play-python.yml` and is imported by `playbook-main.yml:29`, i.e. it already ran as part of the main run the message concludes.

**Impact**

Fresh-install users following the end-of-run guidance hit "No such file or directory", and the advice itself is obsolete (Python setup is no longer optional).

**Recommendation**

Drop the two lines (the playbook already ran) or correct the path if the intent is "re-run to add pyenv versions".

---

## OPP-10: Unreferenced/under-documented helper scripts

**Evidence**

- `scripts/setup-rclone.bash` — a polished 400+-line interactive helper (rclone config → vault → mount definitions → patches `localhost.yml`) with **zero** inbound references from `docs/`, `run.bash`, `scripts/setup.bash`, or `play-rclone.yml`. A user reading `docs/configuration.md` or the rclone playbook would never discover it.
- `scripts/test-ccy-ssh-probe.bash` — useful CCY diagnostic, zero references anywhere (not in `docs/ccy-debug-mounts.md`, not in `CLAUDE/QA.md`).
- `scripts/desktop-symlinks` — the filename says symlinks, but the content (lines 1-30) is a "CCY Read-Only Mount Wrapper" that launches CCY with debug mounts; referenced only from `docs/ccy-debug-mounts.md`. The name/content mismatch invites accidental misuse.

**Impact**

Tooling that took real effort to build is effectively invisible (setup-rclone), and a misleadingly named script is a small foot-gun.

**Recommendation**

Reference `setup-rclone.bash` from `docs/configuration.md` and the header comment of `play-rclone.yml`; mention `test-ccy-ssh-probe.bash` in the CCY debug doc; rename `desktop-symlinks` to something like `ccy-debug-mounts.bash` (updating the doc), or delete it if the workflow is dead.

---

## OPP-11: docs/README.md index missing four documents; a planning doc stranded in docs/

**Evidence**

Comparing link targets in `/workspace/docs/README.md` against `docs/*.md` on disk, four files are unindexed: `ansible-lint-improvement-plan.md`, `ccy-debug-mounts.md`, `fast-file-manager.md`, `nordvpn-installation.md`.

`docs/ansible-lint-improvement-plan.md` is additionally miscategorised: it is a dated plan document ("**Date**: 2025-12-03, **Status**: Planning Phase") living in user-docs space. Several of its objectives have since shipped (the `scripts/lint` wrapper exists; FQCN adoption is complete — `ansible.builtin.` appears in all 70 task-bearing playbooks), making it part-stale as well as misplaced.

**Impact**

`CLAUDE.md` declares `docs/README.md` "the full index"; unindexed docs are undiscoverable. The stranded plan doc duplicates the CLAUDE/Plan workflow's job.

**Recommendation**

Add the three genuine user docs to the index. For the lint plan: fold its remaining open objectives into OPP-02's work (wiring lint/syntax-check into QA), then delete or archive the document under `CLAUDE/Plan/`.

---

## OPP-12: `play-unifi-controller.yml` hardcodes Podman instead of the `container_engine` variable

**Evidence**

`vars/container-defaults.yml` defines `container_engine: podman` and `CLAUDE.md` mandates: "New playbooks needing a container engine must use the `container_engine` variable (default `podman`), not hardcode an engine." `play-claude-devtools.yml` and `play-claude-yolo.yml` comply; `playbooks/imports/optional/common/play-unifi-controller.yml` hardcodes `podman compose` throughout (`:114` `command: podman compose pull`, `:177` `podman unshare chown …`, `:188` `podman compose -f … ps`). `play-ddev.yml` is exempt by design (DDEV is the documented Docker-compatibility case).

**Impact**

Limited — Podman is the right default and `podman unshare` is genuinely Podman-specific, so full abstraction may be more trouble than it is worth. But the playbook is silent about being Podman-only, contrary to the convention of documenting engine choices.

**Recommendation**

Either parameterise the compose invocations on `container_engine` (loading `vars/container-defaults.yml` as the devtools playbook does), or add a header comment + preflight assertion documenting that this playbook is intentionally Podman-only because of `podman unshare` semantics.

---

## OPP-13: Extensions DX — empty test scaffold and no hooks-compliant lint script

**Evidence**

- `/workspace/extensions/tests/` contains only `.gitkeep` and `assets/test-recording.wav` — a test scaffold (and a fixture) with no tests, across three shipped extensions (`speech-to-text`, `workspace-names-overview`, `remote-desktop-toggle`).
- `/workspace/extensions/package.json` defines only `lint`/`lint:fix`, but the hooks daemon blocks `npm run` without an `llm:` prefix — which is why `CLAUDE/QA.md` instructs invoking `node_modules/.bin/eslint` directly.

**Impact**

Minor friction: the documented lint workflow exists only because the package scripts don't match the project's own hooks policy; the empty tests directory either signals abandoned intent or should be removed (YAGNI).

**Recommendation**

Add `"llm:lint": "eslint --no-color ."` to `extensions/package.json` and simplify `CLAUDE/QA.md` accordingly. Decide on the tests directory: either remove it, or seed it with a smoke test (e.g. GJS-free unit tests of pure helper logic, or a `gnome-extensions`-based metadata/version check) — the `test-recording.wav` fixture suggests an STT integration test was once planned.

---

## Positive Observations

- **Fail-fast compliance is total**: every `failed_when: false`/`ignore_errors` instance in `playbooks/` carries a `# FAIL-FAST-OK:` annotation (0 unannotated matches), and `qa-ansible.bash` enforces it.
- **FQCN adoption is complete** — `ansible.builtin.` is used across all 70 task-bearing playbooks, which most Ansible repos never achieve.
- **QA tooling is genuinely LLM-friendly**: `qa-all.bash` emits terse stdout plus structured JSON to `/tmp/qa-results.json` with documented jq recipes, and distinguishes "missing tool" (exit 2, refuse to run) from "check failed" — a textbook fail-fast design.
- **The `tasks/ensure-jq.yml` shared-include precedent** shows the team already knows the right DRY pattern for playbooks; OPP-06 just extends it.
- **Recent plan discipline is excellent**: the 00043/00044 completed plans and the 00036 cancellation (with successor pointer) are model write-ups; the drift in OPP-04/05 is concentrated in the older 002–034 era.
- **`container_engine` abstraction** is correctly used by the playbooks that most need it (CCY, claude-devtools), and the Docker exception (DDEV) is documented with its rationale in `CLAUDE/ContainerEngines.md`.
