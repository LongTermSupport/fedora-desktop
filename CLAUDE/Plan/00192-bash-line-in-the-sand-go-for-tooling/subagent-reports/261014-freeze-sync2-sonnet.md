# go-port/freeze: second merge of F44

Merged F44 into go-port/freeze (no-ff). Four textual conflicts, all resolved keeping both sides.

- `src-go/CLAUDE.md`: the layout bullet joins the freeze engines sentence with the teams package split; the `internal/freeze/` bullet stays.
- `src-go/internal/teams/app.go`: F44's version taken (the adapter wiring moved to `teams/host`). The sudo `OpenTTY` seam is re-added in `host.wireTools`, delegating to `Host.OpenTTY`.
- `src-go/internal/teams/ops/ops_test.go`: F44's `Ops` and `config.Config` names with this branch's sudo `OpenTTY` fake.
- `src-go/qa/rules/table.go`: constants are the union (freeze, sudo, agentbus, host, ops); the unused `teamsPkg` is dropped. `commandOwners` and `programRunners` keep this branch's lists, and the allowances name the new teams packages.

DRY: one `term.FileIsTerminal`, one `fsx.WriteAtomic`, one gum and picker `Confirm`; no second copy found.

QA: qa-go (39 packages, project rules clean), qa-bash-limits, and `qa-changed.bash --base F44` all pass, including ccy-teams 323, the other ccy suites, test-qa-changed 73 and the freeze golden replay (inside qa-go). Allowlist untouched. Ansible syntax-check passed for play-claude-yolo, play-podfreeze and play-lxcfreeze.
