# go-qa-ci M12 fix: docs detector identifiers

- internal/docs/project.go: Run now enumerates project ast-grep rule ids (from sgconfig.yml via astgrep.LoadProject/Inventory, the same source as `go-qa rules`; no ast-grep binary needed; skipped when the ast-grep tool is off) as required identifiers, and project vet-driver analyzers as allowed-not-required. A rule without a page is gqaci.docs.dangling; its page is no orphan.
- Tests: internal/docs/project_test.go (dangling without page, clean with page, tool off) and internal/cli/docs_test.go (docs-check and rule-doc agree in both directions).
- README, defence-before-fix.toml (4.2), docs/CONFORMANCE.md and docs/defences.md say what is true. Remaining limits recorded as 9.2 gaps: golangci-lint linters are not enumerated; the bundled gqaci.ast.\* rules have no pages and docs-check does not check them (rule-doc reports them dangling).
- Clauses: TOOLING-SPEC 4.2, 9, 9.2.
