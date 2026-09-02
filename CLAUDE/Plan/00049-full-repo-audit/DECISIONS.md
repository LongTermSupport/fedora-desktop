# Plan 00049 — Decisions, Gates and Deferrals

Durable rationale extracted from the original plan document. The verbatim
historical text, including the per-batch execution notes, is in
[PLAN_archive.md](PLAN_archive.md). The ranked finding list is in
[triage.md](triage.md); per-dimension evidence is in [research/](research/).

## Audit results summary

| Severity (effective) | Count | Where the list lives                                                  |
| -------------------- | ----- | --------------------------------------------------------------------- |
| High                 | 7     | [triage.md → High](triage.md#high--must-address-7)                    |
| Medium               | 64    | [triage.md → Medium](triage.md#medium--should-address-64)             |
| Low                  | 50    | [triage.md → Low](triage.md#low--polish--batch-up-50)                 |
| Info                 | 13    | [triage.md → Info](triage.md#info--observations-and-opportunities-13) |

The 7 effective-high findings: SEC-01 (committed PII), FF-01/ANS-01 (shell
blocks without `set -e`), ANS-02 (cloudflare-warp self-wedging install),
BSH-02/QA-04 (shellcheck never gates), QA-03 (no CI; committed evidence of
hook bypass).

The headline: the repo's own #1 rule (fail fast) was widely violated inside
playbooks and inside the QA gate itself, which could not fail on most of what
it claimed to check. There was committed PII in tracked plan documents of this
public repo. The CCY container's isolation was weaker than advertised.

## Technical decisions

### Decision 1: Effective severity follows the adversarial verdict

**Context**: 14 findings were rated high by their finder; verifiers confirmed
all 14 as real but adjusted 7 down to medium (e.g. a dead pre-commit check is
real but the consequence is process drift, not data loss).
**Decision**: triage.md and the plan rank by adjusted severity; original
ratings remain visible in the research docs.
**Date**: 2026-06-12

### Decision 2: Fix the gate before the violations

**Context**: Most fail-fast violations exist because no QA rule catches them;
fixing violations first would let new ones regrow.
**Decision**: Phase 4 (QA gate) is ordered before Phase 5 (sweep), and every
sweep item gets a corresponding QA rule.
**Date**: 2026-06-12

### Decision 3: Eliminate `|| true` entirely rather than annotate it (QA-07)

**Context**: The original Phase 4 design was a semgrep rule with an arithmetic
carve-out and a same-line `# FAIL-FAST-OK:` escape. Mid-batch the user
challenged the approach ("why do we need `|| true`? it is a massive smell").
**Decision**: The `bash-error-hiding-or-true` rule is strict, with no
arithmetic carve-out and no annotation escape, mirroring the write-time
`error_hiding_blocker` hook. Every repo-owned occurrence was refactored away:
arithmetic to `n=$(( n + 1 ))`, output probes to `var=$(cmd) || var=""`,
best-effort teardown to a named `attempt()` helper or an explicit `if`.
**Date**: 2026-06-12

### Decision 4: One CCY batch, one `CCY_VERSION` minor bump

**Context**: The Phase 6 findings are concentrated in the single 2,600-line
`claude-yolo` wrapper plus tightly coupled libs, are security-critical, and are
untestable in-container.
**Decision**: Drive Phase 6 directly as one batch (no file-partitioned
fan-out) with a single bump 3.17.0 → 3.18.0. No Dockerfile change, so
`REQUIRED_CONTAINER_VERSION` stays 2.18.
**Date**: 2026-06-12

## Decision gates

### Gate 1 — git history purge → SKIP

HEAD is already clean (working-tree PII scrub in Batch 1; verified zero real
emails and no U+00A0-named file in HEAD). The residual exposure is
history-only. The worst item (email/alias) is already public off-repo in gh
issue #22, which is outside git and unreachable by `git-filter-repo`. The
primary email is in every commit-author line and cannot be purged without
rewriting every SHA. Cost (destructive force-push, breaks every clone/fork) far
exceeds benefit.

Durable mitigations instead: the SEC-02 scanner hardening prevents recurrence;
the CI gitleaks job adds a server-side net; closing or editing gh issue #22 is
the only lever on already-public data and is left as a one-line manual user
action because it mutates a public artefact on the user's account.

### Gate 2 — CI → DONE

`.github/workflows/qa.yml` runs `./scripts/qa-all.bash` (all 6 stages) plus a
separate gitleaks job, on push to all branches and on PR. This is the
non-bypassable server-side layer the `--no-verify`-able local hooks lacked.

First-run corrections: `qa-ansible.bash` had to build its search list from the
directories that actually exist (`roles/` is a galaxy install target, absent on
a clean checkout); `gitleaks-action@v2` needs a paid licence for org-owned
repos, so the job uses the free OSS binary (pinned v8.30.1) scanning the
working tree, since Gate 1 accepts the historical PII and a history scan would
red-fail every run.

## Dependencies and ordering

- Phase 4 (QA gate) lands before Phase 5 (fail-fast sweep) so the new rules
  lock in the sweep's results.
- Phase 9 QA-doc updates depend on Phase 4 outcomes.
- Phase 6 is one CCY batch: single `CCY_VERSION` minor bump, plus a container
  version bump only where the Dockerfile changes.
- Decision Gates 1 and 2 needed user input; everything else proceeded without.

## Deferred findings (explicitly not in the action plan)

- **OPP-03** (bats test suite for 6,600 lines of CCY bash): high value but
  large; deserves its own plan if taken up.
- **EXT-11** (extension unit tests + ESLint 9 migration): bundle with the next
  substantive extension work.
- **OPP-06/OPP-07** (DRY extractions: github-latest-release task file, shared
  colours lib): nice-to-have refactors; do opportunistically.
- **PERF-10** (event-driven extensions instead of polling), **PERF-12** (PDFs
  in git, precedent-only), **CCY-10**, **BSH-19**, and remaining info-severity
  observations: recorded in triage; no action planned.
- **ANS-14 residual**: temp-dir `unarchive` tasks (toolbox/nvidia/darktable/
  virtualbox) and the archived TLP play extract into a root-owned mktemp dir,
  so mode is immaterial. Cosmetic.
- **DOC-18 Dockerfile cross-ref**: fixing one comment line forces a container
  version bump and user image rebuild. Fold into the next real Dockerfile
  change.
- **QA-09 pytest stage**: `pytest` is absent from the CCY image; needs an IaC
  install before `qa-pytest.bash` can run green.

## Plan-index items needing a user decision (OPP-04/OPP-05)

Reported in Batch 9, not acted on: the claude-devtools trio 009/011/013 (009
partially shipped, 011/013 duplicate stubs), 030-phpantom-lsp (Phase 2 shipped,
host-verify pending), and 002/004/007/014/023/027 (revive-or-cancel).

## Outstanding host actions

Recorded across batches; all edit-only work needs HOST execution:

- Netinstall test of the `fedora-install/` hardening (disk/LUKS media,
  untestable in CCY).
- Deploy `play-claude-yolo.yml` (CCY 3.18.0), `play-speech-to-text.yml`,
  `play-gnome-shell-extensions.yml` (now fetches the installer via `get_url`).
- Restart the hooks daemon to load the corrected `ansible_enforcement.py`.
- Wayland socket is mounted `:ro`; if a GUI window fails to open, drop `:ro`
  (the isolation win is socket-only scoping, not the read-only flag).
- Optional one-line user action on Gate 1: close or edit gh issue #22.
