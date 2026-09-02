# Plan 00049: Full Repository Audit

**Status**: In Progress (nine batches landed on `fable-audit-1`; both decision gates resolved — detail in [JOURNAL/00049-Journal-26-08-24.md](JOURNAL/00049-Journal-26-08-24.md))
**Created**: 2026-06-12
**Owner**: Claude (Fable 5 multi-agent workflow) / joseph
**Priority**: High
**Type**: Audit / Research → Action Plan

## Overview

A full audit of the fedora-desktop repository by a 25-agent multi-agent workflow: ten parallel audit agents each swept one dimension (security, fail-fast compliance, Ansible correctness, bash quality, the CCY container system, GNOME Shell extensions, performance, documentation drift, opportunities, QA tooling gaps). Every critical/high finding was re-verified by an adversarial agent and a completeness critic identified residual coverage gaps.

The audit produced 134 findings (7 high, 64 medium, 50 low, 13 info; nothing critical, no leaked credentials) plus 6 coverage gaps. Evidence lives in [research/](research/) (one document per dimension), the ranked list with links is in [triage.md](triage.md), decision rationale, gates and deferrals are in [DECISIONS.md](DECISIONS.md), and the full original plan with its per-batch execution notes is kept verbatim in [PLAN_archive.md](PLAN_archive.md).

## Goals

- ✅ Systematic, evidence-based sweep of every first-party area of the repo
- ✅ Adversarially-verified high-severity findings
- ✅ One research document per dimension, full ranked triage with links
- ⬜ Execute the action phases below so every high and selected medium finding is fixed

## Non-Goals

- Fixing every one of the 134 findings — low/info items are batched or explicitly deferred (see [DECISIONS.md → Deferred](DECISIONS.md#deferred-findings-explicitly-not-in-the-action-plan))
- Deep-auditing vendored code (`roles/vendor/`) or the upstream hooks daemon
- Git history rewriting without an explicit user decision (Decision Gate 1)

## Tasks

Finding IDs (SEC-, FF-, ANS-, BSH-, CCY-, QA-, EXT-, PERF-, DOC-, OPP-, GAP-, FUP-) resolve in [triage.md](triage.md). Per-task implementation detail is in [PLAN_archive.md](PLAN_archive.md).

### Phase 1: Multi-Agent Research

- [x] ✅ **Run audit workflow**: 10 dimension agents → adversarial verification of critical/high → completeness critic
- [x] ✅ **Write research docs**: 11 documents in `research/` (10 dimensions + coverage gaps), verification verdicts appended

### Phase 2: Triage

- [x] ✅ **Consolidate findings** into [triage.md](triage.md): 134 findings ranked by effective severity, cross-dimension duplicates linked, all anchors validated

### Phase 3 (Action A): Public-Repo Hygiene

> Findings: SEC-01, OPP-01, SEC-02, QA-13, SEC-04 — see [research/security.md](research/security.md)

- [x] ✅ **Scrub PII from tracked plan docs** (SEC-01): working-tree scrub with placeholders; zero residual private tokens in tracked files. History untouched (Decision Gate 1)
- [x] ✅ **Remove junk tracked files** (OPP-01): `localhost`, `loclahost` and the U+00A0-named file removed from HEAD
- [x] ✅ **Decision Gate 1 — history purge → DECIDED: SKIP** — rationale in [DECISIONS.md → Gate 1](DECISIONS.md#gate-1--git-history-purge--skip)
- [x] ✅ **Harden secret-scan hooks** (SEC-02, QA-13, BSH-15): broadened patterns, per-line whitelist, dynamic identifier denylist from `localhost.yml` minus `.claude/public-token-allowlist.yml`, merge-commit body scanning, carve-outs for `git@host` and `@unit.service`
- [x] ✅ **Add `no_log: true` to MOK expect tasks** (SEC-04) in play-nvidia.yml and play-displaylink.yml
- [x] ✅ **Fix malformed `.gitignore` line** breaking the `!.env.dist` negation (GAP-03)

### Phase 4 (Action B): Make the QA Gate Actually Gate

> Findings: QA-01..QA-09, BSH-01..BSH-03, BSH-13, FF-03, FF-04, FF-10, PERF-01, EXT-01 — see [research/qa-gaps.md](research/qa-gaps.md)

- [x] ✅ **Fix dead CCY version-bump check** (QA-01/BSH-01): a real code change without a `CCY_VERSION` bump now rejects
- [x] ✅ **Add Ansible syntax validation** (QA-02): `qa-ansible-syntax.bash` runs `--syntax-check` over every top-level playbook, wired into qa-all.bash
- [x] ✅ **Make shellcheck a real gate** (QA-04/BSH-02/FF-03): error-level findings fail QA; BSH-07 and BSH-17 pulled forward so the gate is green
- [x] ✅ **Stop swallowing analyser crashes** (QA-08/BSH-13/FF-04): ruff and shellcheck/jq rc≥2 hard-fail; silent `ruff --fix` removed from the check path
- [x] ✅ **Scope QA to first-party code** (QA-05/PERF-01): vendored and daemon trees excluded; binaries skipped before shebang sniff
- [x] ✅ **Widen qa-ansible** (QA-06) plus playbook shebang/exec-bit hygiene check (QA-14)
- [x] ✅ **Semgrep `|| true`/`|| :` rule** (QA-07, FF-10): strict `bash-error-hiding-or-true` rule with `semgrep --test` self-check; every repo-owned occurrence refactored away — see [DECISIONS.md → Decision 3](DECISIONS.md#decision-3-eliminate--true-entirely-rather-than-annotate-it-qa-07)
- [ ] ⬜ **Add pytest stage** (QA-09): deferred — `pytest` is missing from the CCY image; needs an IaC install before `qa-pytest.bash` can run green
- [x] ✅ **Add ESLint/JS stage** (EXT-01/QA-10): `qa-js.bash` (`node --check` + `eslint .` in extensions/), wired into qa-all.bash
- [x] ✅ **Realign CLAUDE/QA.md** (QA-11/DOC-08): documents the 6-stage suite, shellcheck error-gating and crash hard-fail
- [x] ✅ **Decision Gate 2 — CI → DONE**: `.github/workflows/qa.yml` (QA-03) — see [DECISIONS.md → Gate 2](DECISIONS.md#gate-2--ci--done)

### Phase 5 (Action C): Fail-Fast Sweep in Playbooks and Deployed Scripts

> Findings: FF-01/ANS-01, ANS-02..ANS-04, FF-02/ANS-10, FF-05..FF-07, ANS-08, ANS-11 — see [research/fail-fast.md](research/fail-fast.md), [research/ansible.md](research/ansible.md)

- [x] ✅ **`set -euo pipefail` in multi-line `shell: |` blocks** (FF-01/ANS-01): all 28 playbooks with shell blocks; AnsibleStyle.md mandates it; all playbooks pass `--syntax-check`
- [x] ✅ **Fix cloudflare-warp play** (ANS-02..ANS-04): `get_url` instead of curl|tee, resolved.conf drop-in with reload handler, idempotent registration
- [x] ✅ **Fix skip-and-warn in play-toolbox-install** (FF-02/ANS-10): missing binary after install now exits 1
- [x] ✅ **Surface CCY entrypoint known_hosts failure** (FF-06/BSH-16/CCY-08): done in Phase 6 — explicit fetch, `accept-new` fallback, path reported
- [x] ✅ **Fix docker-in-lxc warn-and-continue** (FF-05): claude-version verify hard-fails; npm-update failure surfaced
- [x] ✅ **Create `~/.config/git` before blockinfile** (ANS-08)
- [x] ✅ **Fix localhost.yml.dist** (ANS-11): `github_ssh_passphrase` documented and asserted; `lastfm_api_secret` name corrected
- [x] ✅ **Qobuz secret handling** (ANS-12): secrets via `stdin:`, `mode 0600` + `no_log`, `blockinfile` instead of `>>` appends

### Phase 6 (Action D): CCY Correctness and Hardening Batch

> Findings: SEC-03/CCY-02, BSH-04..BSH-06, BSH-09, BSH-10, BSH-14, CCY-01, CCY-03, CCY-06, CCY-07, BSH-12 — see [research/ccy.md](research/ccy.md), [research/bash.md](research/bash.md). One batch, one `CCY_VERSION` bump — see [DECISIONS.md → Decision 4](DECISIONS.md#decision-4-one-ccy-batch-one-ccy_version-minor-bump)

- [x] ✅ **Narrow the Wayland mount** (SEC-03/CCY-02): socket-only `:ro` mount instead of the whole runtime dir
- [x] ✅ **Stop exposing tokens in argv** (BSH-09): tokens passed by env name only
- [x] ✅ **mktemp for predictable /tmp paths** (BSH-10)
- [x] ✅ **Fix create_token pipeline status** (BSH-04): `PIPESTATUS[0]` captured; 125/126/127 diagnostics reachable
- [x] ✅ **Fix select_token create/renew paths** (BSH-05): `CREATED_TOKEN_FILE` contract; no more `cat ""` crash or false "Cancelled"
- [x] ✅ **Fix multi-SSH-key identity mismatch** (BSH-06): primary key is authoritative for identity
- [x] ✅ **Fix `ccy --connect` project-name derivation** (BSH-14)
- [x] ✅ **Fix corrupted AI-Dockerfile heredocs** (CCY-01): validated by executing both generators
- [x] ✅ **ctrl+z patch: QA the native-binary path** (CCY-03) and surface soft-fail via sentinel (CCY-07); documented in ContainerRules.md (DOC-18)
- [x] ✅ **Token byte-range message consistency** (CCY-06); **implement `CCY_EXTRA_MOUNTS` consumption** (BSH-12)
- [x] ✅ **Leak-free temp update container** (CCY-09)
- [x] ✅ **CCY `|| true` elimination + gate** (QA-07 remainder): semgrep exclusion for CCY removed
- [x] ✅ **CCY_VERSION bump** 3.17.0 → 3.18.0 (single minor bump for the batch)

### Phase 7 (Action E): Shipped Runtime Bug Fixes

> Findings: BSH-07, BSH-08, BSH-17, EXT-02..EXT-04 — see [research/bash.md](research/bash.md), [research/extensions.md](research/extensions.md)

- [x] ✅ **Fix `qp` cold-start crash** (BSH-07): pulled forward in Batch 1
- [x] ✅ **Define missing `warn` in setup.bash** (BSH-08)
- [x] ✅ **Fix check-displaylink glob test** (BSH-17): pulled forward in Batch 1
- [x] ✅ **Sanitise the language setting in speech-to-text** (EXT-02): `_validateShellArg()` on all three launch methods
- [x] ✅ **Debounce Insert-key start/stop race** (EXT-03): `_launchPending` debounce plus a PID-file liveness check in `wsi`
- [x] ✅ **Replace silent empty catch blocks with logError** (EXT-04)

### Phase 8 (Action F): Performance and Idempotency Batch

> Findings: PERF-02..PERF-09, PERF-11, ANS-09, ANS-13..ANS-15 — see [research/performance.md](research/performance.md)

- [x] ✅ **Make play-rpm-fusion idempotent** (PERF-03)
- [x] ✅ **Drop `fwupdmgr refresh --force`** (PERF-04); **guard the recursive ~/.nvm chown** (PERF-05); **delete redundant `pdm self update`** (PERF-06)
- [x] ✅ **LXC `state: started`** (ANS-09); partial PERF-07/ANS-13 (firewalld module, nmcli probe-then-modify)
- [x] ✅ **wsi-stream restart via handler** (PERF-11)
- [x] ✅ **changed_when sweep** (PERF-08, PERF-09): play-rust-dev and play-gnome-shell-extensions
- [x] ✅ **Move CCY Dockerfile hash LABEL to the end of the final stage** (PERF-02): no version bump
- [x] ✅ **Native modules for flatpak** (PERF-07/ANS-07 remainder); gsettings→`dconf`, lxde→`dnf` module (ANS-13 remainder)
- [x] ✅ **ANS-15**: 16 duplicate post-`---` shebangs removed; `make-playbooks-executable.bash` cannot re-insert one
- [x] ✅ **ANS-14 (system-file copies)**: owner/group/mode added; temp-dir `unarchive` residual deferred as cosmetic

### Phase 9 (Action G): Documentation Realignment

> Findings: DOC-01..DOC-18, OPP-04, OPP-05, OPP-09, EXT-10 — see [research/docs.md](research/docs.md)

- [x] ✅ **Rewrite CLAUDE/PlanWorkflow.md for this repo** (DOC-04)
- [x] ✅ **Fix the four actively-misleading docs** (DOC-01..DOC-03, DOC-09): containerization, vault editing, nordvpn
- [x] ✅ **Regenerate docs/playbooks.md catalogue from playbook-main.yml** (DOC-05, DOC-06); Fedora version sweep (DOC-07)
- [x] ✅ **Fix CLAUDE/QA.md and CLAUDE/GnomeShell.md drift** (DOC-08, DOC-10/QA-11)
- [x] ✅ **Batch link/index fixes** (DOC-11..DOC-16, DOC-18 partial)
- [x] ✅ **Plan-index triage** (OPP-04, OPP-05): plans 020 and 024 moved to `Completed/`; revive-or-cancel calls reported for user decision — see [DECISIONS.md → Plan-index items](DECISIONS.md#plan-index-items-needing-a-user-decision-opp-04opp-05)
- [x] ✅ **DOC-17**: `docs/ansible-lint-improvement-plan.md` moved to Plan 00051 in standard plan form
- [ ] ⬜ **DOC-18 Dockerfile cross-ref**: deferred — one comment line would force a container version bump; fold into the next real Dockerfile change

### Phase 10 (Action H): Follow-Up Research (from coverage gaps)

> See [research/coverage-gaps.md](research/coverage-gaps.md) (scope) and [research/followup-gaps.md](research/followup-gaps.md) (findings FUP-01..29)

- [x] ✅ **Research all five gaps**: findings in [research/followup-gaps.md](research/followup-gaps.md)
- [x] ✅ **Fix FUP-01** (GAP-04): `ansible_enforcement.py` pip-block regex `install(?!\s)` → `install\b`; host must restart the daemon
- [x] ✅ **Harden fedora-install/ bootstrap** (GAP-01, FUP-08..18): secret lifecycle, strict mode, ISO verification, backup before reset. Edit-only — HOST netinstall test required
- [x] ✅ **Harden run.bash failure-report + config-sync** (GAP-02, FUP-19..29): hostname out of public issue body, fail-closed sanitiser, private-repo gate; `RUN_BASH_VERSION` 1.5.3
- [x] ✅ **Remaining .claude/ items** (GAP-04, FUP-02/03/04): `system_paths.py` root resolution hardened; `.claude/ccy/Dockerfile` pinned
- [x] ✅ **Pin galaxy dependencies** (GAP-05, FUP-05/06): role pinned to a SHA; collections version-bounded
- [x] ✅ **Resolve GPL-2.0-in-MIT-repo licensing** (GAP-06, FUP-07): installer un-tracked and fetched via checksummed `get_url` at deploy time

## Dependencies

- Phase ordering and gate dependencies: [DECISIONS.md → Dependencies and ordering](DECISIONS.md#dependencies-and-ordering)
- Outstanding HOST actions from the edit-only batches: [DECISIONS.md → Outstanding host actions](DECISIONS.md#outstanding-host-actions)

## Success Criteria

- [ ] All Phase 3 hygiene items done; Decision Gate 1 explicitly decided
- [ ] qa-all.bash fails on: missing tools, analyser crashes, shellcheck errors, Ansible syntax errors, fail-fast pattern violations — verified by intentionally-broken fixtures
- [ ] All 7 effective-high findings closed; all medium findings either closed or explicitly deferred with rationale
- [ ] CCY batch shipped with version bump and `./scripts/qa-ctrl-z-patch.bash` passing
- [ ] No doc in docs/ or CLAUDE/ contradicts the deployed behaviour for the items listed in Phase 9
- [ ] Plan index reflects reality (OPP-04)

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00049-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Research and triage complete (Phases 1–2)
- Batch 1: Phases 3 + 4 landed on `fable-audit-1`
- Batch 2a/2b: Phase 5 sweep and QA-07 `|| true` elimination
- Batch 3: Phase 6 CCY batch, `CCY_VERSION` 3.18.0
- Batch 4: Phase 7 runtime fixes
- Batches 5 + 6: Phase 8 idempotency and native modules
- Batch 7: Phase 9 documentation realignment
- Batch 8: Phase 10 research and the FUP-01 fix
- Batch 9 / 9b: Phase 10 implementation, both decision gates resolved, CI green
- Plan document slimmed; original kept in [PLAN_archive.md](PLAN_archive.md)
- Remaining: QA-09 pytest stage (IaC gap), DOC-18 Dockerfile comment, plan revive/cancel calls needing user input
