# Plan 00049: Full Repository Audit

**Status**: 🔄 In Progress (research complete; Batch 1 = Phases 3+4 landed on branch `fable-audit-1`)
**Created**: 2026-06-12
**Owner**: Claude (Fable 5 multi-agent workflow) / joseph
**Priority**: High
**Type**: Audit / Research → Action Plan

## Overview

A full audit of the fedora-desktop repository performed by a dynamic multi-agent workflow (run `wf_70cb99e0-10f`, 25 agents): ten parallel audit agents each swept one dimension (security, fail-fast compliance, Ansible correctness, bash quality, the CCY container system, GNOME Shell extensions, performance, documentation drift, opportunities, QA tooling gaps). Every critical/high finding was independently re-verified by an adversarial agent, and a completeness critic identified residual coverage gaps.

The audit produced **134 findings** (7 high, 64 medium, 50 low, 13 info after verification adjustments; nothing critical, no leaked credentials) plus 6 coverage gaps. All evidence lives in [research/](research/) (one document per dimension), the full ranked list with links is in [triage.md](triage.md), and this document carries the selected plan of action.

The headline: the repo's own #1 rule (fail fast) is widely violated inside playbooks and — worse — inside the QA gate itself, which currently cannot fail on most of what it claims to check. There is committed PII in tracked plan documents of this public repo. The CCY container's isolation is weaker than advertised. None of it is on fire, but the safety net has holes exactly where the project's hard rules say it must not.

## Goals

- ✅ Systematic, evidence-based sweep of every first-party area of the repo
- ✅ Adversarially-verified high-severity findings
- ✅ One research document per dimension, full ranked triage with links
- ⬜ Execute the action phases below so every high and selected medium finding is fixed

## Non-Goals

- Fixing every one of the 134 findings — low/info items are batched or explicitly deferred (see "Deferred" below)
- Deep-auditing vendored code (`roles/vendor/`) or the upstream hooks daemon
- Git history rewriting without an explicit user decision (Decision Gate 1)

## Audit Results Summary

| Severity (effective) | Count | Where the list lives                                                  |
| -------------------- | ----- | --------------------------------------------------------------------- |
| High                 | 7     | [triage.md → High](triage.md#high--must-address-7)                    |
| Medium               | 64    | [triage.md → Medium](triage.md#medium--should-address-64)             |
| Low                  | 50    | [triage.md → Low](triage.md#low--polish--batch-up-50)                 |
| Info                 | 13    | [triage.md → Info](triage.md#info--observations-and-opportunities-13) |

The 7 effective-high findings: SEC-01 (committed PII), FF-01/ANS-01 (shell blocks without `set -e`), ANS-02 (cloudflare-warp self-wedging install), BSH-02/QA-04 (shellcheck never gates), QA-03 (no CI; committed evidence of hook bypass).

## Tasks

### Phase 1: Multi-Agent Research

- [x] ✅ **Run audit workflow**: 10 dimension agents → adversarial verification of critical/high → completeness critic
- [x] ✅ **Write research docs**: 11 documents in `research/` (10 dimensions + coverage gaps), verification verdicts appended

### Phase 2: Triage

- [x] ✅ **Consolidate findings** into [triage.md](triage.md): 134 findings ranked by effective severity, cross-dimension duplicates linked, all anchors validated

### Phase 3 (Action A): Public-Repo Hygiene — highest priority

> Findings: SEC-01, OPP-01, SEC-02, QA-13, SEC-04 — see [research/security.md](research/security.md)

- [x] ✅ **Scrub PII from tracked plan docs** (SEC-01): real emails, the private account-mapping, hostnames, and `/home/<user>` paths replaced with placeholders across `CLAUDE/Plan/**` and `playbooks/imports/play-basic-configs.yml` comments; denylist derived deterministically from the gitignored `localhost.yml` minus the public allowlist. Verified zero residual private tokens in tracked files. *(Working-tree scrub only — history still holds them; see Decision Gate 1.)*
- [x] ✅ **Remove junk tracked files** (OPP-01): `git rm`'d the `localhost`, `loclahost`, and U+00A0-named files (the last leaked a home-directory listing). *(Removed from HEAD; still in history — Decision Gate 1.)*
- [ ] ⬜ **Decision Gate 1 — history purge** *(awaiting user)*: purge the U+00A0 file and PII from git history (`git filter-repo`/BFG, force-push, destructive) or accept the identifiers as burned. `git-filter-repo` is not installed in the container — needs host tooling.
- [x] ✅ **Harden secret-scan hooks** (SEC-02, QA-13, BSH-15): broadened email + token + private-key patterns; per-line whitelist filtering (no more whole-file skip); dynamic identifier denylist from `localhost.yml` minus the new `.claude/public-token-allowlist.yml` (public-by-design tokens); merge-commit body scanning; quoted/`mapfile` staged-file loop; carve-outs for `git@host` SSH URLs and systemd `@unit.service` names so the gate is usable. Pre-flighted against the staged set (exit 0).
- [x] ✅ **Add `no_log: true` to MOK expect tasks** (SEC-04) in play-nvidia.yml and play-displaylink.yml (intentional on-screen display kept).
- [x] ✅ **Fix malformed `.gitignore` line** breaking the `!.env.dist` negation (coverage GAP-03).

### Phase 4 (Action B): Make the QA Gate Actually Gate

> Findings: QA-01..QA-09, BSH-01..BSH-03, BSH-13, FF-03, FF-04, FF-10, PERF-01, EXT-01 — see [research/qa-gaps.md](research/qa-gaps.md)

- [x] ✅ **Fix dead CCY version-bump check** (QA-01/BSH-01): rewrote the always-false grep pipeline (strips `+++`/`---` headers + comment/blank lines); a real code change without a `CCY_VERSION` bump now REJECTS, comment/blank-only ALLOWS. Functionally verified in throwaway repos.
- [x] ✅ **Add Ansible syntax validation** (QA-02): new `qa-ansible-syntax.bash` runs `ansible-playbook --syntax-check` over all playbooks with a top-level `- hosts:` (71 today; parse-only, CCY-safe), wired into qa-all.bash with mergeable JSON.
- [x] ✅ **Make shellcheck a real gate** (QA-04/BSH-02/FF-03): error-level findings now fail QA; warning/info/style advisory. Pulled forward the two cited bug-fixes (BSH-07 `qp` SC2168, BSH-17 check-displaylink SC2144) so the gate is green (0 errors).
- [x] ✅ **Stop swallowing analyser crashes** (QA-08/BSH-13/FF-04): ruff rc≥2 and shellcheck/jq rc≥2 now hard-fail via PIPESTATUS/explicit rc (stderr kept in temp files); removed the silent `ruff --fix` mutation from the check path.
- [x] ✅ **Scope QA to first-party code** (QA-05/PERF-01): qa-bash excludes `.claude/hooks-daemon`, `.claude/ccy`, `.claude/skills`, `roles/vendor`; binaries skipped before shebang sniff (scan 196→64 files, null-byte noise gone).
- [x] ✅ **Widen qa-ansible** (QA-06): scans `playbooks/ tasks/ vars/ environment/ roles/` (vendor excluded), `*.yml`+`*.yaml`, case-insensitive booleans, templated-`ignore_errors` flagging, mktemp, JSON output; **+ QA-14** playbook shebang/exec-bit hygiene check (70 playbooks pass).
- [ ] ⬜ **Semgrep `|| true`/`|| :` rules** (QA-07, FF-10): *deferred to Phase 5* — the rule cascades onto ~55 existing occurrences the fail-fast sweep must annotate; landing them together keeps the gate green. (`semgrep --test .semgrep/` lands with it.)
- [ ] ⬜ **Add pytest stage** (QA-09): *deferred — IaC gap* — `pytest` is missing from the CCY image; needs an IaC install (Dockerfile/playbook) before `qa-pytest.bash` can run green. First item of the next batch.
- [x] ✅ **Add ESLint/JS stage** (EXT-01/QA-10): new `qa-js.bash` (`node --check` on repo JS + `eslint .` in extensions/, covering the previously-uncovered `ccy-ctrl-z-patch.js`), wired into qa-all.bash.
- [x] ✅ **Realign CLAUDE/QA.md** (QA-11/DOC-08): documented the new 6-stage suite + shellcheck error-gating + crash-hard-fail (brought forward from Phase 9 to avoid shipping a stale canonical QA doc alongside the suite change).
- [ ] ⬜ **Decision Gate 2 — CI** *(awaiting user)*: add a GitHub Actions workflow (QA-03) running qa-all.bash + syntax-check + eslint + gitleaks on push/PR — the only non-bypassable layer.

### Phase 5 (Action C): Fail-Fast Sweep in Playbooks and Deployed Scripts

> Findings: FF-01/ANS-01, ANS-02..ANS-04, FF-02/ANS-10, FF-05..FF-07, ANS-08, ANS-11 — see [research/fail-fast.md](research/fail-fast.md), [research/ansible.md](research/ansible.md)

- [ ] ⬜ **`set -euo pipefail` in multi-line `shell: |` blocks** (FF-01/ANS-01): fix the ~63 unguarded blocks (priority: play-rpm-fusion, play-lxc-install-config, play-rust-dev, play-claude-code, curl|bash installers); update CLAUDE/AnsibleStyle.md; backed by the new QA rule from Phase 4
- [ ] ⬜ **Fix cloudflare-warp play** (ANS-02..ANS-04): `get_url` instead of curl|tee (self-heals, no empty-file wedge); reinstate the commented-out resolved.conf handler as a drop-in; make registration idempotent; drop interactive `dnf update`
- [ ] ⬜ **Fix skip-and-warn in play-toolbox-install** (FF-02/ANS-10): missing binary after install must fail, not warn
- [ ] ⬜ **Surface CCY entrypoint known_hosts failure** (FF-06/BSH-16/CCY-08): loud warning or fail instead of silent empty known_hosts
- [ ] ⬜ **Fix docker-in-lxc warn-and-continue** (FF-05)
- [ ] ⬜ **Create `~/.config/git` before blockinfile** (ANS-08) — first-run failure in a core playbook
- [ ] ⬜ **Fix localhost.yml.dist** (ANS-11): add `github_ssh_passphrase`, correct `lastfm_api_secret` name
- [ ] ⬜ **Qobuz secret handling** (ANS-12): stdin instead of inline templating, mode 0600 + no_log on config.toml, blockinfile instead of `>>` appends

### Phase 6 (Action D): CCY Correctness and Hardening Batch

> Findings: SEC-03/CCY-02, BSH-04..BSH-06, BSH-09, BSH-10, BSH-14, CCY-01, CCY-03, CCY-06, CCY-07, BSH-12 — see [research/ccy.md](research/ccy.md), [research/bash.md](research/bash.md). One batch = one CCY_VERSION bump (minor).

- [ ] ⬜ **Narrow the Wayland mount** (SEC-03/CCY-02): mount only `$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY` (read-only) instead of the whole runtime dir
- [ ] ⬜ **Stop exposing tokens in argv** (BSH-09): name-only `-e CLAUDE_CODE_OAUTH_TOKEN -e GH_TOKEN` pass-through
- [ ] ⬜ **mktemp for predictable /tmp paths** (BSH-10): CONFIG_TEMP gitconfig staging (0600), PROBE_LOG_DIR, fixed log files
- [ ] ⬜ **Fix create_token pipeline status** (BSH-04): branch on `PIPESTATUS[0]` so the existing failure diagnostics become reachable
- [ ] ⬜ **Fix select_token create/renew paths** (BSH-05): set SELECTED_TOKEN after creation instead of crashing on `cat ""`
- [ ] ⬜ **Fix multi-SSH-key identity mismatch** (BSH-06): derive username and token alias from the same primary key
- [ ] ⬜ **Fix `ccy --connect` project-name derivation** (BSH-14): use get_project_name() on both sides
- [ ] ⬜ **Fix corrupted AI-Dockerfile heredocs** (CCY-01) + smoke test for stray PROMPT_EOF
- [ ] ⬜ **ctrl+z patch: QA the native-binary path** (CCY-03) and surface soft-fail at launch via sentinel file (CCY-07); document native-binary mode in ContainerRules.md (DOC-18)
- [ ] ⬜ **Token byte-range message consistency** (CCY-06); decide CCY_EXTRA_MOUNTS implement-or-delete (BSH-12)

### Phase 7 (Action E): Shipped Runtime Bug Fixes (small, immediate)

> Findings: BSH-07, BSH-08, BSH-17, EXT-02..EXT-04 — see [research/bash.md](research/bash.md), [research/extensions.md](research/extensions.md)

- [ ] ⬜ **Fix `qp` cold-start crash** (BSH-07): `local pid=$!` outside a function
- [ ] ⬜ **Define missing `warn` in setup.bash** (BSH-08): version-mismatch path currently crashes instead of prompting
- [ ] ⬜ **Fix check-displaylink glob test** (BSH-17)
- [ ] ⬜ **Sanitise the language setting in speech-to-text** (EXT-02): apply `_validateShellArg` to `_getWhisperLanguage()` (closes a command-execution path via dconf)
- [ ] ⬜ **Debounce Insert-key start/stop race** (EXT-03) + PID-file liveness check in wsi
- [ ] ⬜ **Replace silent empty catch blocks with logError** (EXT-04)

### Phase 8 (Action F): Performance and Idempotency Batch

> Findings: PERF-02..PERF-09, PERF-11, ANS-09, ANS-13..ANS-15 — see [research/performance.md](research/performance.md)

- [ ] ⬜ **Move CCY Dockerfile LABELs to the end of the final stage** (PERF-02): stops full image rebuilds on any Dockerfile edit
- [ ] ⬜ **Make play-rpm-fusion idempotent** (PERF-03): seven unguarded dnf transactions per run today
- [ ] ⬜ **Drop `fwupdmgr refresh --force`** (PERF-04); **guard the recursive ~/.nvm chown** (PERF-05); **delete redundant `pdm self update`** (PERF-06)
- [ ] ⬜ **Native modules for flatpak/firewalld/nmcli** (PERF-07, ANS-07); **LXC `state: started` + restart handler** (ANS-09)
- [ ] ⬜ **wsi-stream restart via handler** (PERF-11): stop killing the warm speech server on every play run
- [ ] ⬜ **Batch cleanups**: duplicate shebangs in 16 playbooks + fix make-playbooks-executable.bash (ANS-15); missing mode/owner/group on 15 file tasks (ANS-14); changed_when sweep (ANS-13, PERF-08, PERF-09)

### Phase 9 (Action G): Documentation Realignment

> Findings: DOC-01..DOC-18, OPP-04, OPP-05, OPP-09, EXT-10 — see [research/docs.md](research/docs.md)

- [ ] ⬜ **Rewrite CLAUDE/PlanWorkflow.md for this repo** (DOC-04): it currently describes the hooks-daemon project (wrong QA scripts, wrong numbering, templates containing fields the daemon itself blocks)
- [ ] ⬜ **Fix the four actively-misleading docs** (DOC-01..DOC-03, DOC-09): Docker rootful/core, variable-level vault editing, nordvpn doc vs actual playbook, Podman section in containerization.md
- [ ] ⬜ **Regenerate docs/playbooks.md catalogue from playbook-main.yml** (DOC-05, DOC-06); sweep Fedora 42→43 references (DOC-07)
- [ ] ⬜ **Fix CLAUDE/QA.md and CLAUDE/GnomeShell.md drift** (DOC-08, DOC-10/QA-11) — after Phase 4 lands so docs describe the new reality
- [ ] ⬜ **Triage the plan index** (OPP-04, OPP-05): move shipped plans (020, 024, 030, claude-devtools trio) to Completed/, decide revive-or-cancel for 023/027/014/002/004/007
- [ ] ⬜ **Batch link/index fixes** (DOC-12..DOC-16, OPP-09, OPP-11, EXT-10); relocate or close docs/ansible-lint-improvement-plan.md (DOC-17)

### Phase 10 (Action H): Follow-Up Research (from coverage gaps)

> See [research/coverage-gaps.md](research/coverage-gaps.md)

- [ ] ⬜ **Audit fedora-install/ bootstrap** (GAP-01): ~2,770 lines of security-critical install code nobody covered
- [ ] ⬜ **Review run.bash failure-report flow** (GAP-02): posts hostname and weakly-sanitised logs to the public issue tracker
- [ ] ⬜ **Audit tracked .claude/ custom code** (GAP-04): two Python hook handlers and the second CCY Dockerfile
- [ ] ⬜ **Pin galaxy dependencies** (GAP-05): requirements.yml tracks `master`, collections unversioned
- [ ] ⬜ **Resolve GPL-2.0-in-MIT-repo licensing question** (GAP-06)

## Deferred (explicitly not in the action plan)

- **OPP-03** (bats test suite for 6,600 lines of CCY bash) — high value but large; deserves its own plan if taken up
- **EXT-11** (extension unit tests + ESLint 9 migration) — bundle with the next substantive extension work
- **OPP-06/OPP-07** (DRY extractions: github-latest-release task file, shared colours lib) — nice-to-have refactors; do opportunistically when touching those files
- **PERF-10** (event-driven extensions instead of polling), **PERF-12** (PDFs in git, precedent-only), **CCY-10**, **BSH-19**, and remaining info-severity observations — recorded in triage; no action planned

## Dependencies

- Phase 4 (QA gate) should land before Phase 5 (fail-fast sweep) so the new rules lock in the sweep's results
- Phase 9 QA-doc updates depend on Phase 4 outcomes
- Phase 6 is one CCY batch → single CCY_VERSION minor bump + container version bump where the Dockerfile changes
- Decision Gates 1 (history purge) and 2 (CI) need user input; everything else proceeds without

## Technical Decisions

### Decision 1: Effective severity follows the adversarial verdict

**Context**: 14 findings were rated high by their finder; verifiers confirmed all 14 as real but adjusted 7 down to medium (e.g. dead pre-commit check is real but the consequence is process drift, not data loss).
**Decision**: triage.md and this plan rank by adjusted severity; original ratings remain visible in the research docs.
**Date**: 2026-06-12

### Decision 2: Fix the gate before the violations

**Context**: Most fail-fast violations exist because no QA rule catches them; fixing violations first would let new ones regrow.
**Decision**: Phase 4 (QA gate) is ordered before Phase 5 (sweep), and every sweep item gets a corresponding QA rule.
**Date**: 2026-06-12

## Success Criteria

- [ ] All Phase 3 hygiene items done; Decision Gate 1 explicitly decided
- [ ] qa-all.bash fails on: missing tools, analyser crashes, shellcheck errors, Ansible syntax errors, fail-fast pattern violations — verified by intentionally-broken fixtures
- [ ] All 7 effective-high findings closed; all medium findings either closed or explicitly deferred with rationale
- [ ] CCY batch shipped with version bump and `./scripts/qa-ctrl-z-patch.bash` passing
- [ ] No doc in docs/ or CLAUDE/ contradicts the deployed behaviour for the items listed in Phase 9
- [ ] Plan index reflects reality (OPP-04)

## Notes & Updates

### 2026-06-12

- Audit workflow completed: 25 agents, 134 findings across 10 dimensions, 6 coverage gaps. All 14 originally-high findings CONFIRMED by adversarial verification (7 adjusted to medium); zero refuted.
- Research docs, triage.md (all 134 findings, links validated), and this action plan written.
- Awaiting user review of the action phases and the two decision gates (history purge, CI).

### 2026-06-12 — Batch 1 executed (Phases 3 + 4) on branch `fable-audit-1`

- Ran a dynamic multi-agent workflow (`wf_dee26ed1-3ea`, 14 agents: 7 edit + 7 review, partitioned by file to avoid write conflicts; opus for security/gate/integration units, sonnet for mechanical units — fable not warranted). Each unit was edited then independently reviewed.
- Landed: SEC-01, OPP-01 (working-tree), SEC-02, QA-13, BSH-15, SEC-04, GAP-03 (Phase 3); QA-01/BSH-01, QA-02, QA-04, QA-08, QA-05, QA-06, QA-14, EXT-01/QA-10, QA-11/DOC-08, plus pulled-forward BSH-07 + BSH-17 (Phase 4).
- Orchestrator verification (not delegated): full `./scripts/qa-all.bash` green across all **6 stages** (bash, python, patterns, ansible, ansible-syntax, js — 285 files); independent PII residual sweep (zero real identifiers in tracked files); hardened pre-commit hook pre-flighted against the staged set (exit 0). The reviewer caught one residual private token (a domain in a filename) and one error-hiding line in qa-ansible.bash; both fixed. The broadened email pattern initially over-blocked `git@host`/`@unit.service` shapes — added precise carve-outs so the gate is usable.
- Deferred with reasons: QA-07 (`|| true` semgrep rule → cascades onto the Phase-5 fail-fast sweep); QA-09 (pytest stage → `pytest` absent from the CCY image, needs an IaC install first). Decision Gates 1 (history purge — `git-filter-repo` absent, destructive) and 2 (CI) remain with the user.
- No CCY container changes in this batch → no `CCY_VERSION` bump required. All edit-only; nothing deployed (CCY container rule).
