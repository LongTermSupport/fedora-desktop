# go-qa-ci M8: ast-grep detector (commit dd55253)

## Built

- `internal/astgrep`: sgconfig.yml loader (rule and test dirs confined to the root via `os.Root`), rule inventory (ids, severities, unnamed rule files), tested-rule discovery (needs non-empty `valid` and `invalid`), `scan --json=compact` parser (1-based positions, only `error` blocks), `Runner` (Locate, Scan, Test; exit 0/1 are results, anything else or non-JSON means the scan did not run), embedded bundled pack with load-time integrity checks (namespace `gqaci.ast.`, file named after id, Go, error severity, note pointing at `go-qa rule-doc <id>`).
- `internal/tools/astgrep.go` wired into the Executor: static `gqaci.rules.unnamed` / `gqaci.rules.untested` judged even without the binary; project scan, then pack scan from a private temp file (never in the tree; `scan_exclude` as `--globs`); record applied to findings; stale findings exceptions on a full run; `ast-grep test --skip-snapshot-tests`. Missing binary: step skipped with "ast-grep is not installed ..." unless a rule finding already fails it. No sgconfig and empty or off pack: skipped with reason.
- `internal/astgreptest`: the test binary stands in for `ast-grep` through a symlink (no executable file with loosened permissions, so no new gosec exception).
- `qa/go-qa.toml` no longer switches `ast_grep` off; `docs/astgrep.md` added; `docs/golangci.md` pointer updated.

## Spec clauses served

- DETECTOR-SPEC 4.1 (bespoke rules): project ast-grep rules run as Defences; the pack machinery is the bundled route.
- TOOLING-SPEC 4.2 (identifiers): rule ids as ast-grep prints them; `gqaci.rules.unnamed` makes an id mandatory. Rule pages for project rules remain a recorded gap (listing is M11).
- DBF red/green proof: `gqaci.rules.untested` requires valid and invalid cases; `ast-grep test` runs them.
- TOOLING-SPEC 7 (suppression): `ast-grep-ignore` already routed by the M6 detector; the record is applied to findings and stale findings exceptions are failed.
- TOOLING-SPEC 9.2: gaps recorded, not hidden (see below).

## Tests

- `internal/astgrep`: project, inventory, tested, parse, pack integrity, runner argument vector and exit codes (stand-in).
- `internal/astgreptest`: the stand-in itself.
- `internal/tools/astgrep_test.go`: 18 executor cases (skips, missing binary, unnamed/untested, record licensing, stale, scope, pack scan args, exclude, off switch, tree untouched).
- `tests/astgrep`: ports of `ast_grep_test.rs` and the `main_test.rs` ast-grep cases with Go fixtures (violating fires only its rule, clean is quiet, ignore comment, rule tests pass, project plus bundled in one run, opt-out, failing rule test, skip reason). Gated per test on the binary with `t.Skipf` and a stated reason.

## Deviations and gaps

- ast-grep is NOT installed here (`sg` on this host is newgrp), so every `tests/astgrep` case except the skip-reason test was skipped. The Go rule patterns (`fmt.Println($$$ARGS)`, `kind: interpreted_string_literal` plus regex) are unverified against the real binary and may need adjusting on the first run where it is installed.
- The bundled pack is empty by design (brief marks it INFERRED: gosec covers G402/G204); the red-proof harness for pack rules is in place. The "every pack rule has fixtures and a page" test is therefore enforced by `LoadPack` invariants rather than a populated list.
- CI does not install ast-grep (no verified checksum route), so CI skips the gated tests; recorded in `docs/astgrep.md`.
- The pack is written to a temp dir, not `target/`, so CI mode's working-tree check cannot be tripped by it.
- Ruled-out: the skip when the binary is missing follows the milestone text; it is recorded as a gap, not claimed as conformance.

## Fix round (verifier rejection)

- The fixture println rule used a bare fmt.Println pattern, which tree-sitter-go parses as a type conversion and never matches. It now uses a context (func t() { fmt.Println($$$ARGS) }) with selector call_expression.
- Verified against ast-grep 0.50.0 on PATH: all tests/astgrep tests pass, full go test -race green, golangci-lint 0 issues.
- TestAnIgnoreCommentSilencesARule now has a positive control: the same snippet without the comment must fire exactly once. The println half of the clean-snippet test is no longer vacuous, as the violating cases fire.
- docs/astgrep.md: the recorded gap says the tests were verified with 0.50.0, and a new section documents the context plus selector form for Go call patterns.
- Without the binary the gated tests still skip with a stated reason.
