## 09:52 · decision · T3.8   — the owner signs the go-qa record; switch-on blocked by two new exclusions

The owner answered the unblocking list of 2026-10-28 with "all recommended", typed directly in
the coordinator's session. Item A3 (this plan's Task 3.8, pingbus Q5): sign the go-qa project
record, medium tier.

- Signed: the `[ratchet]` table is at the top of `src-go/qa/record.toml` with `high_water = 27`,
  `decided_by = "repository-owner"`, `decided_on = 2026-10-28` and Task 3.8's reason.
  `go-qa.toml`'s tier is unchanged (`claude-opus = "medium"`, 30-day horizon). Signed one day
  after the entries were written, so they are not re-dated.
- Switch-on tried and backed out: with `record` and `suppression` on,
  `scripts/qa-go-qa.bash -t record -t suppression` gave record passed (`go-qa record check`: 27
  exceptions, all valid) and suppression FAILED, exit 1, on two exclusions added to
  `.golangci.yml` on 2026-10-27 after the record was written:
  `internal/netpolicy/podman\.go` tagliatelle (Plan 00232, b1f8fa51c) and
  `cmd/reclaim/main_test\.go` gosec G304|G306 (Plan 00192, e156f6aae). Both switches stay off;
  nothing was weakened. Recording them takes the record to 29, past the signed high water, which
  is the owner's to raise. A new unticked sub-task under Task 3.8 holds it.
