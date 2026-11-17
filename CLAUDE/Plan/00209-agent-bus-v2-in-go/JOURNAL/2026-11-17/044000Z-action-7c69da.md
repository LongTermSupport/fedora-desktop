## 04:40 · action · —

Green for SG2, item 12b (sc-D16).

- carry/guard.go: `DrillImported(root, owner, team)` (carried.json by the trust rule, strict
  decode, kind drill), `DrillImportRefusal` (ErrRefused, naming team carry-check and team remove
  --purge), `DrillImportGuard`.
- admin/carrygate.go: after SG1, every verb naming a drill import refuses but team carry-check
  and team remove; constant verbTeamRemove (goconst).
- admin/wire: DrillImportGuard joined to host.Deps.Guard and the LXC verbs' guard; the restore
  verb's guard too (admin/restoreverb.go). RemoveGuard and carry.Check's guard do not hold it.
- SPEC-carry: SG2's text, commit-order item 12b, sc-D16; PLAN.md's carry line.

`go test ./internal/teamchat/...` passes; golangci-lint over carry and admin: 0 issues.
