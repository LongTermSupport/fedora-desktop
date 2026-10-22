# Plan 00209 N1: qa-reviewer verdict over feat/bus-v2-n1 at d5d64557

The qa-reviewer agent has no Write tool, so the build session recorded its final reply here.

**Verdict: FIX-BEFORE-MERGE.**

1. PLAN.md (Task 1.4) names the `tuwunel`-tagged Go test as Task 1.4's regression form. But SPEC-matrix.md says presence is not in that test, and HC-A1 is checked only by `hca_probe.py`. HC-A1 failed on a fact and changed the design, so it needs a lasting regression check. Fix the PLAN text, or put HC-A1 in a Go test.
2. The tagged real-server test (M44) has never run. `261021-n1-matrix.md` says it "cannot run in the container". `261021-n1-hostchecks-run.md` shows the same pinned binary did run in the container with `db_pool_workers=4`, which M44's config (`tuwunel_test.go`) does not set. PLAN.md lists GO-2026-6809 as Task 1.5's only red check and leaves this out. Run M44 with that key, or record it as open in Task 1.5.
3. Confirm the merge target. N0 (db3c8350) is not in F44. PLAN.md keeps the matrix client, `paths` and `proto`, which have no caller yet, out of F44 until their first caller. MERGE is valid only into the plan branch.

Nit: the gate has three names: `teamchat-keeps-off-v1`, `qa-teamchat-separate.bash` and `teamchatseparate`.

Clean:

- v1 untouched: nothing under helpers/, tests/, files/, playbooks/, tasks/, vars/, internal/pingbus or internal/teams.
- No install names: every Matrix ID is example.org, \*.teamchat.internal or hca.invalid, and home paths are /home/u.
- No version bump needed.
- Gate wiring: listed in qa-gates, qa-routing, qa-changed checks.go, QA.md and addedgates. Its run gave exit 0, covered 112 of 112 teamchat files, and found 3 allowed references.
- Python probes: acceptable plan scaffolding; plans 00161, 00203 and 00211 keep Python in their folders. The build session owns that decision; it chose to keep them over the Go port the earlier handoff proposed, because the tagged Go test becomes the lasting form.

Go tests, go-deps, bash-limits and test_hca (23 OK) pass, and plan-qa has 0 blocks. qa-all was not run (sub-agents are blocked from it).

## Disposition (build session)

- 1 and 2: a follow-up agent runs M44 in the container with `db_pool_workers=4` and adds HC-A1 presence to the tagged test, or records it as open in PLAN.md.
- 3: N1 goes to the coordinator to hold on the plan branch, not into an F44-bound integration, until its first caller (N2 or N3) merges.
- Nit: left; the gate's id, script and package names follow the repo's existing pattern.
