# Plan 00093: CCY version gate covers two files of eight

**Status**: Not Started
**Created**: 2026-09-02
**Owner**: joseph
**Priority**: Medium

## Overview

The pre-commit CCY version-bump gate enforces far less than the rule it implements.
`.claude/rules/ccy-version-bump.md` says a change to the launcher needs `CCY_VERSION`
bumped, and a change to the `Dockerfile` or `entrypoint.sh` needs the container version
bumped, because those are baked into the image and a running container never sees them
otherwise. The enforcement at `scripts/git-hooks/pre-commit:123-131` matches exactly
two paths: `files/var/local/claude-yolo/claude-yolo` and `lib/*.bash`.

So `entrypoint.sh`, the `Dockerfile`, and everything staged from
`files/opt/claude-yolo/` — the `browsing` skill, and since Plan 00092 the
`optional/child-claude/` wrapper and skill — can change with no bump. Nothing forces a
rebuild, and the change never reaches a session. The comment directly above the gate
already narrates this class from Plan 00081 F7 ("enforcement covered one file of
seven"). It is now two of eight, and Plan 00092 widened it by adding a new image-content
path and then editing a skill in it with no gate involvement.

Found during Plan 00092's review; filed here rather than fixed there because it is a
gate defect independent of that feature, and a change to a security-adjacent commit
hook deserves its own scope and its own test.

## Goals

- Every path whose content is baked into the CCY image is covered by the bump gate.
- The gate has a test that proves it fires for each covered path, and stays silent for
  an uncovered one, so the coverage cannot silently narrow again.
- `.claude/rules/ccy-version-bump.md` and the hook agree on what is covered.

## Non-Goals

- Changing what the version numbers mean, or how `REQUIRED_CONTAINER_VERSION` and the
  Dockerfile label are coupled. That mechanism is fine; only its enforcement is thin.
- Retroactively bumping for past unbumped changes.

## Tasks

### Phase 1: Establish the gap precisely

- [ ] ⬜ **Task 1.1**: `triage.bash` — list every path the Dockerfile `COPY`s and every
  path `play-claude-yolo.yml` stages into the build context; diff that set against
  what the hook matches. This is the ground truth, not a hand-written list.
- [ ] ⬜ **Task 1.2**: Decide which bump each path owes. Launcher and lib →
  `CCY_VERSION`. Image content → the container version pair.

### Phase 2: Widen the gate

- [ ] ⬜ **Task 2.1**: Extend the match in `scripts/git-hooks/pre-commit` to the full
  set from Task 1.1, keyed to the correct version value per path.
- [ ] ⬜ **Task 2.2**: Add a test suite for the hook's path matching, synthetic staged
  lists only. Assert it fires for each covered path and not for an uncovered one.
- [ ] ⬜ **Task 2.3**: Update `.claude/rules/ccy-version-bump.md` so the documented
  rule and the enforced rule are the same rule.

### Phase 3: Verify and review

- [ ] ⬜ **Task 3.1**: `./scripts/qa-all.bash`.
- [ ] ⬜ **Task 3.2**: `qa-reviewer` over the diff. Required.

## Dependencies

- Raised by: Plan 00092 (review finding 4).
- Related: Plan 00081 F7, which fixed the same class for `lib/*.bash`.

## Success Criteria

- [ ] A commit touching only `entrypoint.sh` is rejected without a container bump.
- [ ] A commit touching only `files/opt/claude-yolo/optional/child-claude/bin/ccy-claude`
  is rejected without a container bump.
- [ ] The hook test suite passes and is wired into `qa-all.bash`.
- [ ] `qa-reviewer` returns no BLOCK or FIX-BEFORE-MERGE finding.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00093-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed
