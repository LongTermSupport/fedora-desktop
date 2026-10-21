qa-reviewer report (read-only pass on Tasks 3.3-3.12). Verdict: fix before merge; only doc and plan drift, the code is sound.

1. CLAUDE/QA.md:1117 still says "a workflow… is listed as `not covered by qa-changed`". Commit 03822051 routes .github/workflows/\*.yml to the go check. Fix: drop "a workflow" from that example.
2. PLAN.md:176-190: Task 3.12 is still marked 🔄. CI run 39318237801 (commit 8a4def2d, which contains ae9bc47e and 03822051) is green, "Install QA tooling" included. Fix: mark it ✅ and cite the run. No GitHub issue is needed, since it is fixed; record that the issue text in the journal was not filed for that reason.
3. PLAN.md:107 says go-qa-ci was "created private; the owner makes it public". It is PUBLIC. Fix the text.
4. src-go/qa/toolchain/pins_test.go:186 and scripts/qa-routing.tsv:88 match `*.yml` only, so a `.yaml` workflow would escape both. Add `*.yaml` to both, test first, with a qa-changed case if the suite has them.
5. src-go/qa/go-qa.toml:23 claims the floor "sits a few points under the measured total", but go-qa's total has not been measured. Fix this under Task 3.9 (below).
