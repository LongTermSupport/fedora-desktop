# go-qa-ci M3c fix

- Removed the `sh -c 'exec git "$@"'` indirection: git is now launched directly with an argument vector in internal/record/gate.go, internal/record/gate_test.go and tests/scripts/public_safety_test.go (repository setup also uses the git helper instead of `sh -ec`).
- gosec G204 excluded module-wide, openly, in .golangci.yml with a comment naming hazard, why it does not apply, scope.
- Documented in docs/record.md "Configuration exceptions": M6 suppression detector must require it recorded in qa/record.toml.
- Gates: gofmt, go vet, go test -race, golangci-lint (0 issues) green.
- Remaining sh/bash uses are in tests/scripts, which run the shell scripts under test (thin glue), not git.
