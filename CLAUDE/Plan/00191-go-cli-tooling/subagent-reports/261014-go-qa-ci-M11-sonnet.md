# go-qa-ci M11: enumeration and agent context

## Built

- `internal/defences`: `Collect` derives the listing from the active configuration on every call:
  built-ins of enabled tools (owner prefix table, fails on a built-in no tool owns), golangci-lint's
  enabled linters with descriptions (`golangci-lint linters --json`, new `golangci.Runner.Linters`),
  the vet tool's analyzers (`vet.WithAnalyzers`, shared with `rule-doc`), the bundled and project
  ast-grep rules (new `Pack.Rules`, `Rule.Message`), project stages and the rules their `rules`
  command prints. Each entry has engine, origin (bundled or project), phase, level, description and
  fix (quoted from the page's `fix_headings` section via new `docs.Fix`, or the stage's own text).
  Also the gate steps (`tools.Steps`), the record and its budget (new `record.BudgetOf`).
- `go-qa rules [--json]` (text and JSON: keys `defences`, `stages`, `runners`, `record`, `budget`).
- `internal/summary` and `go-qa summary [--check]`: delimited generated region, splice that never
  touches outside text and refuses broken markers, `Check` returning `gqaci.summary.stale`.
  The `summary` tool is now built and runs in the gate; self-gate enables it with `AGENTS.md`.
- `docs.Resolve`, `docs.Check`, `docs.Run` now take a context (contextcheck), callers updated.
- `docs/enumeration.md`; README, defences, stages, astgrep, analyzers, golangci docs updated; gaps recorded.

## Clauses served

TOOLING-SPEC 5.1 (list of defences, derived), 5.2 (machine-readable, stable shape), 5.3 (agent
context region, regenerated, stale fails), 6.3 (record and budget enumerated), 7.1 and 7.2 (what is
active is asked of the tool, descriptions and fixes included). Not claimed as conformance: gaps in
`docs/enumeration.md` (docs detector does not yet consume the listing; no pages for linters).

## Tests

record budget; golangci `ParseLinters` and a real `Linters` run; astgrep pack and inventory messages;
`docs.Fix`; `vet.WithAnalyzers`; defences (engines, fix quoting, disabled tools, stage added with no
other edit, project ast-grep rule, runners split, record and budget, JSON shape, unowned built-in);
summary (render, splice, broken markers, check, write); tools (`Steps`, summary executor); cli
(`rules --json` shape mirroring rust-qa-ci `rules_json_is_machine_readable`, added stage listed, record
and budget, `summary` write and `--check` stale and fresh, `run -t summary`).

## Deviations

- `record` stays an array (as in rust-qa-ci); the budget is a separate top-level `budget` key.
- No per-defence `summarised` switch (YAGNI); the summary lists every active defence.
- Tests that used `summary` as the example unbuilt tool now use an unknown step name.
- The docs detector is not rewired to the listing (would invert the package dependency); recorded as a gap.

## Fix round: TOOLING-SPEC 5.1 documentation route (commit a6dfef0)

- Defence gains a `doc` field (JSON, text listing, summary). `go-qa rule-doc <id>` where it resolves (pages, stages, analyzers); `golangci-lint help linters` for golangci linters; empty otherwise, and the summary says "No documentation route yet".
- Test TestEveryListedDocumentationRouteResolves runs every listed rule-doc route against the self-run listing; summary test asserts a golangci linter is not routed via rule-doc.
- Gap recorded in docs/enumeration.md (9.2): the golangci route lists all linters, not the one named; undocumented defences are not yet reported as dangling.
- AGENTS.md region regenerated; gates green.
