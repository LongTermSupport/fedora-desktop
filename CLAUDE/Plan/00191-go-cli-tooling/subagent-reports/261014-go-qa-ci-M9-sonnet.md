# go-qa-ci M9: runners and the deadcode detector

Built: `internal/runners` (vuln, mod, test, coverage, mutants, deadcode engines), `internal/tools/runners.go`
(executor glue, per module, scope, record licensing), `internal/faketool` (test stand-in for external
programs via a symlink to the test binary, no shell), `docs/runners.md`, config key
`mutants.threshold_efficacy` (default 100, validated 0..100), self-gate now runs deadcode, vuln, mod, test and
coverage (CI installs pinned deadcode v0.52.0 and govulncheck v1.9.0; mutants stays off).

Spec clauses served: TOOLING-SPEC 4.5 (runners after static phases, stop at first runner failure), 4.1/4.2
(findings under an identifier: advisory id, `deadcode`, `error[coverage|mod|test|mutants]`; record licensing and
stale detection for advisory and deadcode exceptions), 6 (dependency audit by reachability, symbol level),
8.2 (self-gate extended), 9.2 (gaps recorded in docs/runners.md and docs/defences.md; no conformance claimed).

Tests: runners package (stream parsing, argv per mode, real go fixtures for mod/test/coverage, missing program is
an error), executor tests for all acceptance fixtures: coverage gap fails with `error[coverage]`, untidy go.mod
fails `error[mod]`, failing test stops later runners (pipeline.Execute), mutants off skipped / diff / survivors,
vuln licensed then stale, deadcode skipped for a library.

Confirmed flags (gremlins v0.6.0 help): `--diff <branch or commit>`, `--threshold-efficacy`. Gremlins exits 0
whatever survives without a threshold, hence the new config key. deadcode `-test -json`, govulncheck
`-format json` (exit 0 whatever it finds) confirmed on real binaries.

Deviations: new config key `threshold_efficacy` (not in the brief); deadcode identifier is `deadcode`; advisory
and deadcode pages absent (recorded gap). Existing tests that used `test` as the "unbuilt" tool now use `summary`.
