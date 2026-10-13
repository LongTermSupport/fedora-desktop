# go-qa-ci M5 fix

- run.go: golangci-lint always run with --max-issues-per-linter=0 --max-same-issues=0 --uniq-by-line=false (TOOLING-SPEC 4.4 / DETECTOR-SPEC 4.3: every finding printed or licensed).
- golangci.go runFmt: error returned only when the diff write fails; every module with a diff is reported.
- Tests: consumer config with default caps (scope z, 4 runs, plus collapsed-identical check), fmt stderr has no spurious line, two modules both reported. An existing record test now also records gosec::G104 on the same line, previously hidden by uniq-by-line.
