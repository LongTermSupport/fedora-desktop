# go-qa-ci M1: config load, defaults, unknown-key rejection, `go-qa tools`

## Built

- `internal/config`: `Config` types, `Default()`, `Load(root, explicit)`. Strict decoding with go-toml/v2 over the defaults; unknown keys give one error naming every key (`tools.bogus`). `record = "<path>"` shorthand is expanded to the `[record]` table before the single strict decode. Range and coherence checks (coverage 0..100, max_per_rule \<= max_total, max_policy \<= max_per_rule, mutants mode, tier names, local paths via `os.Root`/`filepath.IsLocal`, `modules` entries hold a `go.mod`).
- `internal/tools`: catalogue in phase order (fmt; record, suppression, summary, docs, ast-grep, golangci, analyzers, deadcode; vuln, mod, test, coverage, mutants), `Phase`, `FromName`, `Enabled`.
- `internal/root`: `go env -json GOWORK GOMOD` root discovery.
- `internal/cli`: `--root`, `--config`, `--version`, `tools`; config failure is exit 2. `cmd/go-qa` is now a thin `main`.
- README: Commands and Configuration sections (defaults).
- `.golangci.yml`: depguard allow list (stdlib, own module, go-toml, yaml, x/tools).

## Spec clauses served

- TOOLING-SPEC 6.4 (state own defaults): README Configuration is the stated default listing, and a test decodes its toml blocks and requires equality with `Default()`.
- TOOLING-SPEC 5.1 groundwork (listing without running): `go-qa tools`; the defence listing (`rules`) is M11.
- Fail fast: unknown or invalid configuration is exit 2, never silently ignored. No conformance is claimed; gaps are not yet recorded in a declaration (M12).

## Tests

`config_test.go` (README defaults equal built-ins, no file gives defaults, partial override, record shorthand and table, 16 rejection cases, modules, explicit path), `tools_test.go`, `root_test.go` (module, go.work, outside a module), `cli_test.go` (tools output exact, disabled shown, unknown key exits 2 naming it, `--config`, extra args).

## Deviations from the brief

- `audit` switch dropped (brief folds it into `vuln`); `deadcode` and `golangci`/`analyzers`/`mod`/`vuln` switches added. `fail_under_statements` replaces `fail_under_lines`.
- `[[stage]]`, `[[resolver]]`, `[analyzers] driver` are not accepted yet (M10, M7a add them); rejected as unknown keys, stated in the README.
- go-toml/v2 does not call `UnmarshalText` for string-kinded types, so tier and mutants-mode values are validated after decode.
- Decider/trust-model semantic validation (automation-like deciders, `human` as a model tier, tier strictness ordering) is left to M3b with the policy logic.
- The old `cmd/go-qa/main_test.go` moved to `internal/cli/cli_test.go` with the logic; `cmd/go-qa` has no test of its own.
- Dependency: only go-toml/v2 added.
