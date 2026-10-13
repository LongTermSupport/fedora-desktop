# go-qa-ci M5: fmt and the golangci detector

Commit db98588 on main (pushed).

## Built

- `internal/golangci`: JSON report parser, `Runner` (Lint, Format), `Config` (project file or the embedded baseline in a temp file). Identifiers are `<linter>` or `<linter>::<code>` for staticcheck, gosec, govet, gocritic, revive (the tools' own message prefixes, unaltered). Absolute paths are requested (`--path-mode=abs`) and made module-relative, because golangci-lint otherwise makes them relative to the config file directory, which is wrong for the temp baseline.
- `internal/tools/golangci.go`: `fmt` (fix, `--no-fix` diff, CI "working tree changed") and `golangci` (module loop, scope filter, record applied via `Matcher.Finding`, compiler-style output). Missing binary fails loudly.
- `internal/worktree`: content-hash snapshot of Go files (no git dependency) to judge "working tree changed".
- `root.InScope`, `configs/golangci.yml` (embedded strict v2 baseline, no exclusions, depguard left out as project-specific), `qa/go-qa.toml` (switches off unbuilt tools for the self-gate), `.github/workflows/ci.yml` (jobs `go` and `self-gate`, pinned action SHAs), `.github/scripts/install-golangci-lint.sh` (checksum-verified), `docs/golangci.md`.
- This repo's `.golangci.yml` keeps depguard configured (allow-list). The self-gate found and I fixed real findings (G304 via os.OpenRoot, wrapcheck, shadow).

## Spec clauses served

TOOLING-SPEC 4.4 (identifiers printed unaltered), 4.5 (format then detectors, stop before runners), 8.2 (self-gate in CI), 9.2 (gaps recorded in docs/golangci.md). DETECTOR-SPEC 5.1-5.4 (local, subset, in-output), 7.1/7.2 (nolintlint with require-explanation/specific in the baseline). Not claimed: conformance.

## Gaps recorded (9.2)

golangci 4.3 partial, 6.1-6.3 partial (wrapped codes do not resolve via `go-qa rule-doc`), 4.4 none, 6.4 not verified; staticcheck 4.1 and 7.1 fail (`//lint:ignore` undetected until M6).

## Tests added

Parser (identifiers, malformed input), config resolution and baseline validity (`config verify`), lint on fixture modules (errcheck, staticcheck::SA1019, clean, non-compiling), fmt fix/check/CI/scope, scope drop proven both ways, record license (findings vs suppressions), nested modules, missing tool, CLI end-to-end (`--ci` unformatted fails with "working tree changed"; errcheck prints `error[errcheck]`; `-p` outside scope passes), worktree snapshots, `InScope`. Tests that need golangci-lint call `t.Fatal` when it is absent (required dependency, no skip).

## Deviations

- Scope filters after analysis rather than passing paths to golangci-lint (it loses type info on partial file sets). Documented.
- SA9003 is not in staticcheck's default set, so the SAxxxx proof uses SA1019.
- Stale-exception accounting is left to M6 (as in the record docs).
- Gates: gofmt, vet, `go test -race`, golangci-lint 0 issues, and `go-qa run --ci` all green locally.
