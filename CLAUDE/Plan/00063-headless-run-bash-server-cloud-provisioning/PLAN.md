# Plan 00063: Headless `run.bash` — Server & Cloud Provisioning

**Status**: In Progress
**Created**: 2026-07-23
**Owner**: joseph
**Priority**: Medium

> Lean plan. The full pre-slimming text is preserved verbatim in
> [PLAN_archive.md](PLAN_archive.md). The frozen implementation spec is in
> [DESIGN.md](DESIGN.md), the owner decisions in [DECISIONS.md](DECISIONS.md),
> and the round-by-round audit and implementation narrative in `JOURNAL/`.

## Overview

Plan 00061 made the Ansible layer headless-capable: every play declares a
`scope:` and self-guards on an auto-detected `provisioning_profile`, so
`playbook-main.yml` on a Fedora Server or Cloud box already installs only the
general plus server subset.

The remaining blocker was `run.bash`, the bootstrap installer. It was
desktop-shaped and fully interactive: identity, GitHub accounts, vault password,
SSH passphrase, config-repo choice, optional-playbook menu and reboot were all
gathered from terminal prompts, so it could not provision a headless server or an
unattended cloud instance and violated `CLAUDE/InteractiveScripts.md` rule 11.

This plan adds a headless execution mode to `run.bash`, driven by `RUN_BASH_*`
environment variables (non-secret values) and `0600` secret files (paths via
env), so the same script provisions a desktop interactively or a server/cloud box
unattended. GitHub setup remains mandatory and is fed from a scoped token. The
design was hardened through three rounds of hostile review before implementation
(Decision 3) and is code-complete at `run.bash` v1.10.0; host verification is
outstanding.

## Goals

- A headless, non-interactive mode for `run.bash` that provisions a Fedora
  Server or Cloud box end-to-end with zero prompts, driven by env vars and
  secret files.
- Fail fast, never hang, when a required value is missing or a precondition is
  unmet, naming the exact env var or fix.
- GitHub setup stays mandatory in every mode; headless supplies the account,
  token, SSH passphrase and vault password non-interactively.
- The interactive desktop path is unchanged (zero regression).
- Headless auto-detects the same way the Ansible layer does and passes a
  `provisioning_profile` override through to the playbook.
- Expanded `--help` plus a `--help-run-headless` deep-dive documenting the env
  contract, a cloud-init example and the fail-fast rules.
- User-facing docs for the server/cloud story, cross-linked from `README.md`.

## Non-Goals

- No new play `scope`: Fedora Cloud correctly resolves to
  `provisioning_profile: server`.
- No changes to the Ansible layer, play scoping or `provisioning_profile`
  detection (Plan 00061's delivery, reused verbatim).
- No re-architecture of the interactive desktop flow; headless is added alongside
  it through the existing prompt-helper chokepoints.
- No remote-driven provisioning: `run.bash` is `connection: local` and runs on
  the target.
- No kickstart or ISO changes (Plan 00018/00022 territory).
- No answers-file mechanism (Decision 1).
- The `RUN_BASH_GITHUB_ACCOUNTS=none` HTTPS-only path is deferred to a follow-up
  plan (Decision 2, Task 1.6).

## Tasks

### Phase 1: Plan and hostile review

- [x] ✅ **Task 1.1**: Confirm Fedora Cloud needs no new scope; establish that the
  gap is `run.bash`'s interactive-only bootstrap, not the Ansible layer.
- [x] ✅ **Task 1.2**: Owner decisions captured: env-var input, GitHub always
  required, plan-first plus hostile review loop ([DECISIONS.md](DECISIONS.md)
  1–3).
- [x] ✅ **Task 1.3**: Author the plan (problem, goals, non-goals, draft design).
- [x] ✅ **Task 1.4 round 1**: Two independent auditors (coverage and security
  lenses) found the draft not implementation-ready; design hardened to v2
  (D1–D11 in [DESIGN.md](DESIGN.md)).
- [x] ✅ **Task 1.4 round 2**: Re-audit of v2; architecture sound, residual hangs
  and one cloud secret-delivery blocker folded into V3.1–V3.9.
- [x] ✅ **Task 1.5a**: Three owner decisions resolved (Decisions 2, 4, 6);
  canonical invocation documented.
- [x] ✅ **Task 1.5b**: Round-3 focused review; no new architecture breaks, only
  bounded hardening V3.10–V3.15.
- [x] ✅ **Task 1.5c**: Design frozen, then re-opened when the delayed round-3
  coverage audit found the empty-GitHub path breaks `playbook-main.yml` via two
  latent server-profile play bugs (V3.14). No bug shipped.
- [x] ✅ **Task 1.6**: Owner decision: defer the empty-GitHub path. v1 is
  GitHub-token-required; `RUN_BASH_GITHUB_ACCOUNTS=none` fails fast in
  `headless_preflight` naming the follow-up (run.bash v1.9.1). Design re-frozen.

### Phase 2: Implementation

- [x] ✅ **Task 2.1**: Headless trigger, arg parsing (`--headless` /
  `--interactive`, auto-detect), version bump, and the startup `sudo -k -n true`
  NOPASSWD probe plus non-root check in `headless_preflight` (v1.8.0, v1.9.0).
- [ ] 🔄 **Task 2.2**: Secret file-pointer plumbing (D2). Done: `hl_resolve_secret`
  with the V3.10 guardrails, literal-env `unset` before the first child, the
  `set -u`-safe `HL_SECRET_FILES` trap, stderr-clean `headless_fail`. Pending:
  delete-after-use and ssh-agent teardown are HOST-verified in Phase 3.
- [ ] 🔄 **Task 2.3**: GitHub token auth (D3). Done: headless
  `gh auth login --with-token` block in `run.bash` (PAT via stdin) ahead of the
  interactive one (v1.9.4); `gh-account-setup.bash` fails loud under
  `RUN_BASH_HEADLESS` instead of opening a device flow (v1.10.0). Code complete;
  HOST-verified in Phase 3.
- [ ] 🔄 **Task 2.4**: SSH keys and vault (D5, D6). Done: passphrase file required
  in preflight (v1.9.2); `hl_ssh_agent_start` / `hl_ssh_agent_stop` with the
  transient `SSH_ASKPASS` helper and `hl_cleanup` EXIT trap (v1.9.4);
  `hl_reconcile_vault` provided-or-fail, never auto-generate (v1.9.5). Code
  complete; the agent load, clone and teardown end-to-end is HOST-verified in
  Phase 3.
- [ ] 🔄 **Task 2.5**: Read-prompt neutralisation (D9). Done: `hl_abort` backstop
  at the top of every shared prompt helper (v1.9.3); every call site
  headless-branched: hostname, `hl_write_localhost_yml`, vault,
  `hl_run_optional_playbooks`, projects restore, reboot (v1.10.0). Code complete;
  end-to-end is HOST-verified in Phase 3.
- [x] ✅ **Task 2.6**: Failure semantics (D7): a headless main-playbook failure
  aborts loud and exits non-zero, no public-tracker prompt, no continue-anyway;
  `RUN_BASH_PROVISIONING_PROFILE` forwarded via `-e` (v1.10.0).
- [x] ✅ **Task 2.7**: `--help` expanded and `--help-run-headless` documents the
  full v1 token-required contract with an out-of-band cloud-init example (v1.8.0,
  retuned v1.9.1).
- [ ] 🔄 **Task 2.8**: QA and acceptance. Done: `./scripts/qa-all.bash` green each
  slice; plan-local [acceptance.bash](acceptance.bash) passes 10 preflight
  fail-fast gates in-container via `runuser -u nobody`; no new `2>/dev/null`,
  `|| true` or `sed` (D10). Remaining: end-to-end execution assertions are
  HOST-only (Phase 3).
- [x] ✅ **Task 2.9**: Docs: `docs/headless-provisioning.md` (reference) and
  `docs/headless-server-install.md` (runbook), cross-linked from `docs/README.md`,
  `docs/installation.md` and the root `README.md`.

### Phase 3: Verification (HOST, not the CCY container)

- [ ] ⬜ **Task 3.1**: On a real or VM Fedora Server or Cloud box, run `run.bash`
  headless via env and confirm end-to-end provisioning with zero prompts.
- [ ] ⬜ **Task 3.2**: Confirm the desktop interactive path is unchanged.

## Dependencies

- Depends on Plan 00061 (Ansible-layer scope split and `provisioning_profile`
  auto-detect), reused verbatim.
- Follow-up: a separate plan re-enables the empty-GitHub path after fixing
  `play-github-cli-multi.yml` and `play-lxc-install-config.yml` for the server
  profile (V3.14).

## Success Criteria

- [ ] `run.bash` provisions a headless Fedora Server or Cloud box end-to-end with
  zero interactive prompts, driven by `RUN_BASH_*` env plus `0600` secret files.
- [ ] Every missing required value or unmet precondition (email, GitHub account,
  token file, SSH passphrase file, vault password, NOPASSWD sudo) fails fast
  naming the exact fix, never hangs.
- [ ] GitHub auth works non-interactively via a scoped token; SSH-only git auth.
- [ ] A failed main or optional playbook makes a headless run exit non-zero.
- [ ] No secret bytes enter the environment or cloud-init `user-data`.
- [ ] Desktop interactive `./run.bash` is unchanged.
- [ ] `--help` points to it; `--help-run-headless` documents the full contract
  and an out-of-band cloud-init example.
- [ ] `./scripts/qa-all.bash` passes; `RUN_BASH_VERSION` bumped; no new
  `2>/dev/null`, `|| true` or `sed`.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only. Blow-by-blow in JOURNAL/. -->

- Plan created; no-new-scope confirmed; hostile review loop commissioned
  (`47e3e757`).
- Design v2 after round 1 (`ccff44f9`); v3 deltas after round 2 (`d379b5b9`);
  convergence and freeze after round 3 (`a58bddec`).
- Empty-GitHub path deferred, v1 token-required (`83cdb2c1`, run.bash v1.9.1).
- Headless slices: help and trigger v1.8.0 (`79742fa4`); preflight v1.9.0
  (`68c9305b`); backstop v1.9.3 (`0ef1a5be`); GitHub/SSH mechanics v1.9.4
  (`9f3195e4`); localhost.yml and vault v1.9.5 (`b88bc544`); stop flipped,
  headless live v1.10.0 (`c62c180a`).
- User-facing docs (`383365b0`, `c0854cf9`).
- Plan slimmed: history to PLAN_archive.md, spec to DESIGN.md and DECISIONS.md.
