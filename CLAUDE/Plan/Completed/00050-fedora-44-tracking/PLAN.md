# Plan 00050: Fedora 44 Migration Tracking

**Status**: ✅ Complete (research → execution → audit → **merged to `F44`**, which is now the repo **default branch**). Remaining work is the HOST-VERIFY set in Phase 3, which needs a running Fedora 44 machine.
**Created**: 2026-06-12
**Owner**: Claude (Fable 5 multi-agent workflow) / joseph
**Priority**: Medium
**Type**: Migration Tracking / Research

## Overview

This repository currently targets **Fedora 43** (`vars/fedora-version.yml` → `fedora_version: 43`). Fedora 44 is the next target. This plan tracks everything in the repo that is version-sensitive and may break or need adjustment when the target moves from Fedora 43 to Fedora 44 — package and repository availability, the Python stack, GNOME Shell / extension compatibility, hardware/kernel modules, the install/bootstrap path, and the many hardcoded version literals scattered across scripts and docs.

**This is the research and planning phase only.** No fixes are made and no execution action plan is written yet. A dynamic multi-agent workflow (Fable 5) swept the codebase across six version-sensitivity dimensions, cross-referencing each against the live Fedora 44 changeset/release notes. Findings live in [research/](research/) (one document per dimension); the ranked summary with links is in [triage.md](triage.md).

The deliverable of this phase is **knowledge, not change**: a clear, evidence-based map of what the Fedora 44 bump will touch, so a future batch can be planned and executed deliberately.

## Goals

- ✅ Establish a tracking plan for the Fedora 43 → 44 migration
- ✅ Evidence-based sweep of every version-sensitive area of the repo (six dimensions)
- ✅ Cross-reference each dimension against the actual Fedora 44 release facts (GNOME version, Python version, kernel, package/repo changes) via live web research
- ✅ One research document per dimension plus a ranked [triage.md](triage.md) with links
- ⬜ (Future, separate decision) Convert triage into an execution action plan and perform the bump

## Non-Goals

- **No fixes in this phase.** Nothing is edited, bumped, or deployed. `fedora_version` stays at 43 until the migration is explicitly planned and executed.
- No execution/action phases — deliberately deferred ("no further planning as yet").
- No history rewrites, no CI changes, no container rebuilds.
- Not auditing general repo health — that is Plan 00049's remit. This plan is scoped strictly to **what changes because the Fedora target changes**.

## Research Dimensions

| #   | Dimension                       | Prefix | Document                                                       |
| --- | ------------------------------- | ------ | -------------------------------------------------------------- |
| 1   | Version literals & gating logic | `VER`  | [research/version-refs.md](research/version-refs.md)           |
| 2   | Packages, repos & DNF           | `PKG`  | [research/packages-repos.md](research/packages-repos.md)       |
| 3   | Python stack                    | `PY`   | [research/python.md](research/python.md)                       |
| 4   | GNOME Shell & extensions        | `EXT`  | [research/gnome-extensions.md](research/gnome-extensions.md)   |
| 5   | Hardware & kernel modules       | `HW`   | [research/hardware-kernel.md](research/hardware-kernel.md)     |
| 6   | Install & bootstrap path        | `BOOT` | [research/install-bootstrap.md](research/install-bootstrap.md) |

## Research Results Summary

55 findings across the six dimensions, ranked in [triage.md](triage.md). No fixes made.

| Severity | Count | Where the list lives                                                                  |
| -------- | ----- | ------------------------------------------------------------------------------------- |
| High     | 7     | [triage.md → High](triage.md#high--will-break-f44-install-or-a-core-flow-7)           |
| Medium   | 14    | [triage.md → Medium](triage.md#medium--likely-breaks-or-needs-a-deliberate-change-14) |
| Low      | 25    | [triage.md → Low](triage.md#low--minor-adjustment--verify-on-f44-25)                  |
| Info     | 9     | [triage.md → Info](triage.md#info--observations--no-migration-action-required-9)      |

**Confirmed Fedora 44 baseline** (live research): released 2026-04-28; **GNOME 50**; kernel 6.19 at GA → **7.0.x** by June 2026; **Python stays 3.14** (no interpreter break); **DNF5 migration complete**; toolchain GCC 16.1 / glibc 2.43.

**The 7 highs**: VER-01/BOOT-01 (the `fedora_version` pin gates everything), VER-02 (undefined `warn` crashes setup.bash on the mismatch path), PKG-01 (dnf5 rejects `config-manager --enable` → core play halts), PKG-02 (`pgdev/ghostty` Copr has no F44 build → core play halts), EXT-01 (all three custom extensions disabled on GNOME 50), HW-01 (fc43 kernel versionlock can block/half-install the fc44 kernel).

**The good news**: the bump's core is one line (`fedora_version: 44`) — all gates read it correctly; CCY containers are not Fedora-coupled (out of scope); no Python interpreter break; and almost every third-party repo already publishes for F44.

## Tasks

### Phase 1: Multi-Agent Research (this phase)

- [x] ✅ **Scope version-sensitive surface** of the repo
- [x] ✅ **Run research workflow**: six Fable agents, one per dimension, each combining codebase evidence with live Fedora 44 facts
- [x] ✅ **Write research docs**: one document per dimension under `research/`
- [x] ✅ **Consolidate** findings into [triage.md](triage.md), ranked by severity with links

### Phase 2: Decision Gate — plan the bump

- [x] ✅ **Review research + triage** — user approved execution ("get all the F44 problems fixed").
- [x] ✅ **Convert triage into execution** — done as Phase 3 below, on `feature/f44-migration` (branched off F44, branched off F43 post-audit-merge).

### Phase 3: Execution — implement the F44 fixes (on `feature/f44-migration`)

> Executed as a review-gated fan-out of 7 file-disjoint agents (opus for `ks.cfg`); the orchestrator did the central bump + reviewed every diff and fixed an EXT-05 fail-fast bug. Upstream availability was curl-verified at implementation time. QA green (6 stages). Items needing a running F44 system are flagged HOST-VERIFY.

- [x] ✅ **VER-01/BOOT-01** — `vars/fedora-version.yml: 44` (the linchpin; all gates read it).
- [x] ✅ **VER-02** — already fixed on the audit branch (00049 BSH-08 `warn()`), carried in via the F43 merge.
- [x] ✅ **PKG-01** — `play-rpm-fusion.yml`: dnf5 `config-manager setopt fedora-cisco-openh264.enabled=1`.
- [x] ✅ **PKG-02** — `play-terminal-emulators.yml`: ghostty Copr `pgdev/ghostty` → `alternateved/ghostty` (curl-verified F44 build `ghostty-1.3.1…fc44`); old Copr added to `play-ZZ-repo-cleanup.yml` orphan-removal.
- [x] ✅ **PKG-03/HW-03** — `play-nvidia.yml`: CUDA gpgkey `1940C73E.pub` → `73CD9B30.pub` (curl: 200 vs 404); exclude fence extended with the 610-era driver-coupled packages incl. the `nvidia-fs-dkms` kmod.
- [x] ✅ **EXT-01** — `"50"` added to `shell-version` in all three extension manifests (speech-to-text, workspace-names-overview, remote-desktop-toggle).
- [x] ✅ **EXT-04** — workspace-names-overview: deprecated `Gio.Settings({schema:…})` → `{schema_id:…}` (`node --check` clean).
- [x] ✅ **EXT-05** — `play-gnome-shell-extensions.yml`: real OUT_OF_DATE/ERROR runtime verification (positive `failed_when` assertion; graceful no-session probe — orchestrator-corrected from the agent's stderr-based version which mis-handled no-session).
- [x] ✅ **EXT-02/VER-07/EXT-08** — installer already derives the running shell version; stale GNOME-49 comment → 50; dash-to-dock `.fc44` note.
- [x] ✅ **VER-03/BOOT-02/BOOT-06** — `ks.cfg`: `SETUP_BRANCH`/version marker F43→F44 (both sites), BOOT-06 Anaconda-network behaviour documented at the network fragment.
- [x] ✅ **VER-04** — `docker-in-lxc`: `FEDORA_VERSION="44"`, `DIL_VERSION` bumped 1.0.1→1.0.2.
- [x] ✅ **VER-05/06/BOOT-07/EXT-06(docs)** — docs version sweep 43→44 (installation, README, development, post-upgrade, playbooks, speech-to-text, fast-file-manager); build-iso.bash ISO example corrected.
- [x] ✅ **VER-08/PKG-10** — `play-darktable-ai-build.yml`: chroot/dist tag already templated on `fedora_version`; stale f43 comments fixed; dist-git commit pin flagged `# TODO F44` (src.fedoraproject.org unreachable from the container — HOST-VERIFY the commit before first F44 build).
- [x] ✅ **PY-01/PY-05** — `play-python.yml`: pyenv pins → 3.11.15/3.12.13/3.13.14 (cpython-tag-verified); `zlib-devel` → `zlib-ng-compat-devel` (F44).
- [x] ✅ **PY-02** — `play-speech-to-text.yml`: removed the obsolete scipy 1.15.2 source-build scaffolding (scipy ≥1.16.1 ships cp314 wheels). HOST-VERIFY the streaming stack on F44.
- [x] ✅ **PKG-08/HW-01** — dropped the inert dnf4 `python3-dnf-plugin-versionlock` install; `manage-kernel-versions.py` hint updated to dnf5; pre-upgrade versionlock-clear procedure documented at the kernel-management entry point.
- [ ] ⬜ **HOST-VERIFY (cannot test in CCY)**: a real F44 install/run to validate — darktable dist-git commit (VER-08), speech-to-text streaming stack (PY-02), extension live state on GNOME 50 (EXT-03/05), kernel/akmod build window (HW-01/02/04/07), and the verify-only lows (PKG-04/05/06/07/09, BOOT-04/05/06).

## Severity Scale

- **High** — will break the Fedora 44 install or a core deploy/runtime flow if unaddressed.
- **Medium** — likely to break or definitely needs a deliberate change for Fedora 44.
- **Low** — minor adjustment, or "verify on F44" polish.
- **Info** — observation / no action required to migrate.

## Dependencies

- Related: Plan 00049 (full repo audit) — DOC-07 there flags stale Fedora 42 references on the F43 branch; this plan supersedes the version-bump concern for the 43 → 44 move.
- This plan's Phase 2 (execution) depends on the user's go-ahead.

## Success Criteria (research phase)

- [x] Every version-sensitive area has an evidence-backed research document
- [x] triage.md ranks all findings with working links to the evidence
- [x] Fedora 44 specifics (versions, package/repo changes) are sourced from live research, not assumed
- [ ] User has what they need to decide whether/when to plan the bump

## Notes & Updates

### 2026-06-12

- Plan created. Research-only phase: a dynamic Fable workflow swept six version-sensitivity dimensions against the live Fedora 44 changeset. Results in `research/` and `triage.md`. No code changed; `fedora_version` remains 43. Execution deliberately deferred.

### 2026-06-12 — Phase 3 executed (the F44 bump) on `feature/f44-migration`

- User greenlit execution unattended. Branch topology: audit branch `fable-audit-1` → merged to `F43` (merge-commit, CI green) → `F44` branched off F43 and pushed (resolves BOOT-03) → `feature/f44-migration` branched off F44 for the work.
- Implemented every code-change F44 finding (7 high, the actionable mediums, and the lows that need edits) via a 7-agent file-disjoint fan-out; upstream availability curl-verified (CUDA key, ghostty Copr, cpython tags, zlib-ng). The orchestrator did the central `fedora_version: 44` bump and reviewed all diffs, correcting an EXT-05 no-session fail-fast bug in the agent's first version.
- 25 files changed; `./scripts/qa-all.bash` green (6 stages, 285 files); all 71 playbooks `--syntax-check` clean. Edit-only — a set of HOST-VERIFY items (listed in Phase 3) genuinely need a running F44 box and cannot be validated in the CCY container.
- Next: QA (done) → independent audit round of the F44 diff → merge `feature/f44-migration` → `F44` → make `F44` the default branch.

### 2026-06-12 — Independent audit round of the F44 diff (5 adversarial reviewers)

- Five read-only reviewers re-verified the migration slice-by-slice against LIVE upstream (curl/git-ls-remote/PyPI), not the diff's own claims. **Verdict: migration sound.** Confirmed-correct with evidence: dnf5 `setopt` enable semantics, ghostty `alternateved` F44 build, CUDA key 73CD9B30 (200 vs 404), folded-scalar YAML, exclude-vs-cuda-toolkit safety, all 3 metadata.json valid, `schema_id` spelling, **EXT-05 fail-fast logic correct for all three cases (no-session/OUT_OF_DATE/active)**, no GNOME-50-removed APIs used, **zero LUKS/partition regression in ks.cfg** (numstat + word-diff + line-range proof), build-iso F44 ISO names, pyenv tags all real, zlib-ng-compat-devel on F44, scipy cp314 wheels real.
- **7 fixes applied from the audit:** (1) **nvidia exclude** `nvidia-fs-dkms*` → `nvidia-fs*` — the fedora44 CUDA repo also ships a plain `nvidia-fs` kmod the narrower glob missed (it would compete with the akmod stack); verified no cuda-toolkit package starts with `nvidia-fs`. (2) root **README.md** "Version Compatibility" still said Fedora 42 → 44 (the doc sweep had missed it — the primary onboarding doc). (3) **CLAUDE/GnomeShell.md** "Fedora 43 has 48.7" → "Fedora 44 has GNOME Shell 50". (4) **workspace-names extension.js** header: noted the private API paths are NOT re-verified against Shell 50 (HOST-VERIFY). (5) **darktable** `.fc43` expansion comment → `.fc44`; workflow comment clarified the commit is still the f43 pin pending the TODO. (6) **play-fast-file-manager** GSK_RENDERER F41/42 labels → version-agnostic + re-evaluate note. (7) **play-nvidia** header "Fedora 43+" → "Fedora 44+".
- Noted-not-fixed (correct as-is): EXT-05 `DISABLED`-state gap is pre-existing design, not a regression; the RealtimeSTT/scipy old-pin risk fails LOUDLY (correct fail-fast) and is a HOST-VERIFY. No fail-fast/error-hiding regressions anywhere in the diff. QA green after fixes (6 stages, 285 files); CI green on the feature branch.

### 2026-06-12 — Merged to F44; F44 promoted to default branch

- `feature/f44-migration` CI green (qa-all 6 stages + gitleaks) → merged into `F44` with a merge-commit (no squash, per project rule); `F44` pushed.
- **`gh repo edit --default-branch F44`** — the repo default moved `F43` → `F44` (the branch-per-version model: each new Fedora release becomes default). Verified via `gh repo view`.
- The full unattended chain is done: audit branch `fable-audit-1` → `F43` (Plan 00049) → `F44` branched + the Fedora 44 migration (Plan 00050) implemented, QA-passed, audited, merged → `F44` default.
- **Outstanding (needs a real Fedora 44 host — cannot be done in the CCY container):** run the HOST-VERIFY items in Phase 3 (darktable f44 dist-git commit, speech-to-text streaming stack, GNOME 50 live extension API paths + state, kernel/akmod build window), then re-run the affected playbooks on the host to confirm a clean F44 deploy.
