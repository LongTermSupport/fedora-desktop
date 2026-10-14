# go-qa-ci M7a: bespoke type-aware rules through go vet -vettool

## Built

- `driver` (public): `Main(extra ...*analysis.Analyzer)` wrapping `unitchecker.Main`.
- `cmd/go-qa`: with `GQACI_VETTOOL=1` the binary is its own vet tool.
- `internal/vet`: `Self`, `Build` (project driver via `go build`), `Resolve`, `Tool.Analyzers` (from `-flags`, summary from `help <name>`), `Tool.Help`, `Tool.Vet` (`go vet -vettool -json ./...`, exit status never trusted for findings), `Parse` (stream of JSON objects, dedupe of package/test variants, analyzer `error` entries and unplaceable positions are errors).
- `analyzers` tool in `internal/tools`: per module, findings keyed by analyzer name, scope filter, `covers = "findings"` licensing, stale report (`StaleFindingsOf`, only analyzer names, only on a full run).
- Config `[analyzers] driver = "./pkg"` (validated, README default block kept equal by the existing test).
- `go-qa rules` (analyzers only for now) and `rule-doc` fallback to analyzer `help`.
- `internal/relpath` extracted from golangci (DRY); `internal/vettest` (trivial `gqaci_test` analyzer, `Serve` hook, analysistest fixture).
- Self-gate: `analyzers` switched on in `qa/go-qa.toml` and added to the proven tools in `tests/selfgate`.
- Docs: `docs/analyzers.md` (protocol, config, harness, recorded gaps); README, defences, record, golangci pages updated.
- Dependency added: `golang.org/x/tools v0.52.0` (brings `golang.org/x/sync`, `golang.org/x/mod` indirect). `go.mod` go line became 1.26.0 as x/tools requires.

## Confirmed against x/tools v0.52.0

- `-flags` lists an analyzer as a bool flag named after it with usage `enable "<name>" analysis`; core flags have other usages. Used as the registration test.
- `help` lists analyzers; `help <name>` prints `name: first line` then the rest of `Doc`; an unknown name fails (tested).
- `go vet -json` prints `{pkg:{analyzer:[{posn,end,message}]}}` on stdout and exits 0 with findings; compile errors go to stderr with exit 1.
- A vet tool with zero analyzers works (`go-qa` today).

## Spec clauses served

TOOLING-SPEC 4.1 (bespoke rules via a driver, stable identifier printed with every finding), 4.2 (offline resolution of analyzer ids through `help`, single-rule harness), 7 (no inline suppression route; exceptions only as record `covers = "findings"`), 6.2 staleness for analyzers, 8.2 (the self-gate runs the tool). DETECTOR-SPEC 4.1 and 4.2 through the go vet register entry.

## Tests

`internal/vet` (parse, self, listing, help, vet with a test variant, non-compiling code, missing tool, Build of a real project driver against this checkout, Resolve), `internal/vettest` (analysistest), `internal/tools` (red `error[gqaci_test]` Failed, clean, licensed finding passes, suppression-type entry does not license, unused findings entry stale on full run only, other detector rules not judged, scope, further modules, non-compiling, unbuildable driver), `internal/record` (`StaleFindingsOf`), `internal/config`, `internal/cli` (`rules`, `rule-doc` fallback and precedence), `cmd/go-qa` (built binary, with and without the marker), `driver`, `internal/relpath`. Gates: gofmt, go vet, `go test -race ./...`, golangci-lint 0 issues, `go-qa run --no-fix` green.

## Deviations and gaps

- The acceptance fixture runs the test binary as the vet tool through `GQACI_VETTOOL` (the production protocol) rather than a prebuilt fixture driver; a built project driver is separately tested in `internal/vet`.
- `go-qa rules` lists analyzers only (M11 completes it); the docs detector does not yet know analyzers (recorded in `docs/analyzers.md`); no bundled analyzers (M7b); no check that a project driver's go-qa-ci version matches the binary. No conformance is claimed.
