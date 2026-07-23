# Plan 00063: Headless `run.bash` — Server & Cloud Provisioning

**Status**: In Progress
**Created**: 2026-07-23
**Owner**: joseph
**Priority**: Medium

## Overview

Plan 00061 made the **Ansible layer** headless-capable: every play declares a
`scope:` (`general` | `gnome` | `server`) and self-guards on an auto-detected
`provisioning_profile` (`systemctl get-default` → `graphical.target` = desktop,
else server, server-biased). Running `./playbooks/playbook-main.yml` on a Fedora
**Server** or **Cloud** box already skips all GNOME plays and installs only the
general + server subset. That half is done.

The remaining blocker is **`run.bash`** — the bootstrap installer that a user (or
the kickstart pipeline) runs to go from a fresh Fedora to a fully-provisioned box.
It is desktop-shaped and **100% interactive**: every configuration value (user
identity, GitHub accounts, vault password, SSH passphrase, config-repo choice,
optional-playbook menu, reboot) is gathered from a terminal `read`/prompt. There
is **no non-interactive path**, which means `run.bash` cannot provision a headless
server or an unattended cloud instance — the prompts either EOF-abort or hang when
no TTY is present. This also violates `CLAUDE/InteractiveScripts.md` rule 11 (a
non-interactive escape hatch must exist and must fail fast, never hang).

This plan adds a **headless execution mode** to `run.bash`, driven entirely by
**environment variables** (12-factor; drops straight into cloud-init user-data and
CI), so the same script can provision a desktop interactively **or** a
server/cloud box unattended via IaC. GitHub setup **remains mandatory** (fed from
env in headless mode; missing → fail fast). It also expands `--help` and adds a
dedicated `--help-run-headless` deep-dive documenting the full env-var IaC
contract without flooding the normal help output.

## Goals

- Add a **headless / non-interactive mode** to `run.bash` that provisions a Fedora
  **Server or Cloud** box end-to-end with **zero interactive prompts**, driven by
  environment variables.
- **Fail fast** when a required value is missing and no TTY is available — a clear
  message naming the exact env var to set, never a hang (satisfies
  `CLAUDE/InteractiveScripts.md` rules 03 & 11).
- Keep **GitHub setup mandatory** in every mode; headless supplies the accounts
  (and SSH passphrase / vault password) from env. SSH remains the sole GitHub auth
  path.
- Preserve the **interactive desktop path with zero regression** — a bare
  `./run.bash` on a workstation behaves exactly as today.
- Make headless mode **auto-detect** the target the same way the Ansible layer
  does (no TTY and/or `RUN_BASH_HEADLESS=1`), and pass/allow the
  `provisioning_profile` override through to the playbook.
- Expand `--help`; add **`--help-run-headless`** documenting the complete env-var
  contract, a cloud-init example, and the fail-fast rules.
- Document the headless/server/cloud story in `docs/` and cross-link from
  `README.md`.

## Non-Goals

- **Not** adding a new play `scope` — Fedora Cloud correctly resolves to
  `provisioning_profile: server` via Plan 00061's auto-detection. The
  general+server subset is the right coverage for Cloud; a `cloud` scope would be
  an empty-bucket taxonomy bloat. (Confirmed: `VERSION_ID` is edition-independent,
  so the version gate already passes on Server/Cloud editions.)
- **Not** changing the Ansible layer / play scoping / the `provisioning_profile`
  detection — that is Plan 00061's delivery and is reused verbatim.
- **Not** re-architecting `run.bash`'s interactive desktop flow — headless mode is
  added *alongside* it through the existing prompt-helper chokepoints, not a
  rewrite.
- **Not** building remote-driven provisioning (`run.bash` is `connection: local`;
  it runs *on* the target). "Run against servers" = run `run.bash` on the server /
  via cloud-init user-data, consistent with the repo's local-transport design.
- **Not** changing the kickstart `ks.cfg` / ISO build (Plan 00018/00022 territory),
  though this plan makes `run.bash` *callable* unattended from that pipeline.
- **Not** adding an answers-file mechanism — env vars are the chosen single input
  channel (owner decision). An answers file can layer on later if needed.

## Context & Background

- **Entry point**: `run.bash` (1941 lines, `RUN_BASH_VERSION` at line 6 — **must
  be bumped on every change**, same discipline as CCY). Whole body is wrapped in
  `main()` run inside a subshell; `set -e -u -o pipefail`, `IFS=$'\n\t'`.
- **Interactive chokepoints** (the surface headless mode must neutralise):
  - `confirm()` (220), `promptForValue()` (676), `promptChoice()` (760),
    `promptSecretConfirmed()` (791), `promptDefault()` (818),
    `prompt_verified_vault_password()` (860), `prompt_github_accounts_yaml()` (329).
  - Identity gather: lines 1343–1345 / 1373–1375 (`user_login`, `user_name`,
    `user_email`), GitHub accounts 1355/1381, vault 1405–1436, SSH passphrase
    1462–1470.
  - **Hard SSH-for-GitHub gate**: line 1072 (loops until confirmed).
  - Config-repo import choice: `promptChoice` 1266/1327.
  - Main playbook invocation: 1540/1543 (NOPASSWD detection → `--ask-become-pass`).
  - Post-main: projects restore 1568, optional-playbook menu 1773–1906, reboot
    1927\.
- **`connection: local`**: `run.bash` provisions the box it runs on. Server/cloud
  use = copy `run.bash` onto the box (or curl it in cloud-init user-data) and run.
- **Prior art in-repo**: `scripts/gh-account-setup.bash` already takes
  `GITHUB_SSH_PASSPHRASE`, `LOCALHOST_YML`, `VAULT_PASS_FILE` from env (invoked at
  line 1492) — the env-driven pattern is established; this plan extends it to the
  whole bootstrap.
- **Standards that bind this work**:
  - `CLAUDE/InteractiveScripts.md` — rules 03 (clean EOF exit), 08 (stderr
    hygiene), 10 (`-h` always works, unknown opts fail fast), 11 (non-interactive
    escape hatch, fail-not-hang).
  - `CLAUDE/StderrHygiene.md` — prompts/diagnostics → stderr; captured values →
    stdout.
  - `CLAUDE.md` #1 Fail-Fast; `CLAUDE/QA.md` (`./scripts/qa-all.bash` before every
    commit); CCY container rule (edit + commit only; deploy/verify on HOST).
  - `CLAUDE/SecurityRules.md` — headless must never echo secrets or place them in
    `argv`; env-var secrets read once and unset.

## Design (proposed — to be hardened by the hostile review loop)

### D1. Headless trigger

Headless mode is ON when **any** of:

1. `RUN_BASH_HEADLESS=1` explicitly set, **or**
2. stdin is not a TTY (`[ ! -t 0 ]`) **and** at least one `RUN_BASH_*` config var
   is set (so an accidental pipe on a desktop does not silently go headless).

Explicit `--headless` flag forces (1). `--interactive` forces it OFF (escape hatch
for a piped-but-still-interactive edge case). Precedence and the exact
auto-detect predicate are a **review-loop question**.

### D2. Env-var contract (initial draft — names/finality set by the loop)

| Variable                         | Purpose                                       | Required (headless) | Default               |
| -------------------------------- | --------------------------------------------- | ------------------- | --------------------- |
| `RUN_BASH_HEADLESS`              | Force headless                                | —                   | unset                 |
| `RUN_BASH_USER_LOGIN`            | System login                                  | no                  | `$(whoami)`           |
| `RUN_BASH_USER_NAME`             | Full name                                     | no                  | = login               |
| `RUN_BASH_USER_EMAIL`            | Git/commit email                              | **yes**             | — (fail fast)         |
| `RUN_BASH_GITHUB_ACCOUNTS`       | Comma-sep gh usernames (GitHub is mandatory)  | **yes**             | — (fail fast)         |
| `RUN_BASH_GITHUB_SSH_PASSPHRASE` | Passphrase for generated GitHub SSH keys      | no                  | empty (no passphrase) |
| `RUN_BASH_VAULT_PASSWORD`        | Ansible vault password                        | no                  | auto-generate         |
| `RUN_BASH_CONFIG_SOURCE`         | Which config-repo host file to import         | no                  | fresh (no import)     |
| `RUN_BASH_PROVISIONING_PROFILE`  | Force `desktop`/`server` → `-e` passthrough   | no                  | auto-detect           |
| `RUN_BASH_OPTIONAL_PLAYBOOKS`    | Space/comma list of optional plays, or `none` | no                  | `none`                |
| `RUN_BASH_RESTORE_PROJECTS`      | `1`/`0` restore from config manifest          | no                  | `0`                   |
| `RUN_BASH_REBOOT`                | `1`/`0` reboot at end                         | no                  | `0`                   |

Secrets (`*_PASSPHRASE`, `*_VAULT_PASSWORD`) are read into locals and `unset` from
the environment immediately (never in `argv`, never logged).

### D3. Prompt-helper neutralisation

The cleanest chokepoint: make each prompt helper **headless-aware** so call sites
barely change. In headless mode a helper returns its supplied env value or the
documented default; if a **required** value is absent it calls a shared
`headless_fail <VAR> <what>` that prints the exact env var to set and exits
non-zero (fail fast, rule 11). `confirm()` returns the env-driven boolean (or the
safe default). This keeps the interactive code paths untouched.

### D4. GitHub gate under headless

SSH is already the only GitHub auth path. In headless mode the line-1072
"confirm you'll choose SSH" loop is **auto-satisfied** (SSH is implied), and
`prompt_github_accounts_yaml` consumes `RUN_BASH_GITHUB_ACCOUNTS`. Missing
accounts → fail fast (GitHub mandatory per owner decision).

### D5. Post-main behaviour

Optional menu, projects restore, and reboot all become env-gated and default to
the **safe/no-op** choice headless, so an unattended run terminates cleanly.

### D6. `--help` / `--help-run-headless`

`--help` gains a one-line pointer to server/cloud + `--help-run-headless`.
`--help-run-headless` prints the full env-var table, a copy-paste cloud-init
`user-data` example, the fail-fast rules, and the desktop-vs-server/cloud model.

## Tasks

### Phase 1: Plan & hostile review (this session)

- [x] ✅ **Task 1.1**: Confirm Fedora Cloud needs no new scope; establish that the
  gap is `run.bash`'s interactive-only bootstrap, not the Ansible layer.
- [x] ✅ **Task 1.2**: Owner decisions captured — env-var input, GitHub always
  required, plan-first + hostile opus review loop + execute (see Technical
  Decisions).
- [x] ✅ **Task 1.3**: Author this plan (problem, goals, non-goals, draft design).
- [ ] 🔄 **Task 1.4**: **Hostile review loop** — ≥2 independent Opus agents
  adversarially audit this plan/design (headless trigger correctness, env-var
  contract completeness, fail-fast coverage of every prompt, security of secret
  handling, zero-regression proof for desktop, standards compliance). Judge each
  round, feed real findings back, iterate to convergence. Record rounds in
  `JOURNAL/` and the verdict below.
- [ ] ⬜ **Task 1.5**: Freeze the converged design as the implementation spec.

### Phase 2: Implementation (post-convergence)

- [ ] ⬜ **Task 2.1**: Add headless trigger + arg parsing (`--headless`,
  `--interactive`); bump `RUN_BASH_VERSION`.
- [ ] ⬜ **Task 2.2**: Make prompt helpers headless-aware + add `headless_fail`;
  wire env vars into every identity/vault/SSH/GitHub gather point.
- [ ] ⬜ **Task 2.3**: Env-gate the SSH-for-GitHub confirm, config-repo import,
  optional menu, projects restore, and reboot; pass
  `RUN_BASH_PROVISIONING_PROFILE` through to `playbook-main.yml` when set.
- [ ] ⬜ **Task 2.4**: Expand `--help`; add `--help-run-headless` with the env
  contract + cloud-init example.
- [ ] ⬜ **Task 2.5**: Run `./scripts/qa-all.bash`; fix findings. Add a plan-local
  `acceptance.bash` proving headless fail-fast + a dry desktop-path check (safe in
  container where possible; host for the real run).
- [ ] ⬜ **Task 2.6**: Docs — new `docs/` section (headless/server/cloud provisioning
  via env) + `README.md` cross-link.

### Phase 3: Verification (HOST — not CCY container)

- [ ] ⬜ **Task 3.1**: On a real/VM Fedora **Server or Cloud** box, run `run.bash`
  headless via env and confirm end-to-end provisioning with zero prompts.
- [ ] ⬜ **Task 3.2**: Confirm desktop interactive path unchanged (zero regression).

## Dependencies

- **Depends on**: Plan 00061 (Headless / Server Provisioning — Ansible-layer scope
  split + `provisioning_profile` auto-detect). Reused verbatim; not modified here.

## Technical Decisions

### Decision 1: Env vars as the single headless input channel

**Context**: Headless mode needs config without a TTY; options were env vars, an
answers file, or Ansible extra-vars passthrough.
**Decision** (owner, 2026-07-23): **Environment variables** (`RUN_BASH_*`). Rationale:
12-factor, drops directly into cloud-init `user-data` and CI, matches the existing
`gh-account-setup.bash` env pattern, no new file format to parse or secure. An
answers file is an explicit **Non-Goal** for now.
**Date**: 2026-07-23

### Decision 2: GitHub setup stays mandatory in every mode

**Context**: A bare server arguably needs no GitHub identity; option was to skip
GitHub if unset headless.
**Decision** (owner, 2026-07-23): **Always require GitHub.** Headless supplies
accounts via `RUN_BASH_GITHUB_ACCOUNTS`; missing → fail fast. SSH stays the only
auth path. Keeps behaviour uniform with the desktop flow and every provisioned box
gets a working GitHub identity.
**Date**: 2026-07-23

### Decision 3: Plan-first, then hostile Opus review loop, then execute

**Context**: `run.bash` is a 1941-line critical bootstrap; a regression bricks a
provision.
**Decision** (owner, 2026-07-23): Write this plan, then run an **adversarial review
loop with independent Opus agents** (mirroring Plan 00061's Sonnet↔Fable↔judge
convergence) hostile-auditing the design before any code is written. Only execute
once the loop converges.
**Date**: 2026-07-23

## Success Criteria

- [ ] `run.bash` provisions a headless Fedora **Server/Cloud** box end-to-end with
  **zero interactive prompts**, driven only by `RUN_BASH_*` env vars.
- [ ] A missing **required** value with no TTY **fails fast** naming the exact env
  var — never hangs.
- [ ] GitHub setup runs (mandatory) from env in headless mode; SSH-only auth.
- [ ] Desktop interactive `./run.bash` is **unchanged** (zero regression).
- [ ] `--help` points to it; `--help-run-headless` documents the full contract with
  a cloud-init example.
- [ ] `./scripts/qa-all.bash` passes; `RUN_BASH_VERSION` bumped.

## Risks & Mitigations

| Risk                                                     | Impact | Probability | Mitigation                                                                                              |
| -------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------- |
| Headless auto-detect fires on an accidental desktop pipe | H      | M           | Require an explicit `RUN_BASH_*` var (or `--headless`) in addition to no-TTY; `--interactive` override  |
| A prompt is missed → hangs on a TTY-less box             | H      | M           | Central prompt-helper neutralisation + `headless_fail`; acceptance test enumerates every prompt         |
| Secret leakage (env in argv / logs)                      | H      | L           | Read secrets to locals, `unset` from env, never pass in `argv`, never echo                              |
| Desktop regression from shared helpers                   | H      | L           | Headless branch is additive/guarded; desktop path byte-unchanged; QA + host regression check (Task 3.2) |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; Fedora-Cloud/no-new-scope confirmed; owner decisions recorded;
  hostile Opus review loop commissioned.
