## 09:14 · decision · —

The repository owner ruled on Go QA (2026-11-18), relayed by the team coordinator from the questions room. In their words: no long lists of exceptions, which is just baselining; checks that are more noisy than reliable are turned off; go with Go's own opinions; be pragmatic, and where there is a clear winner, act without asking. Q40 is answered: fieldalignment is dropped entirely.

Applied to src-go's golangci-lint configuration:

- fieldalignment is off, both halves (govet disable). The 103 struct reorders stay. qa/lintconfig now fails if the project configuration runs it, while its control still shows the fixture draws both diagnostics.
- tagliatelle (one JSON key case per module) is off as a linter. That retires the per-path exclusions for other programs' wire formats and the snake_case overrides for teamchat's packages.
- gosec G301, G302, G304, G306, G401, G501, G505 and G703 (file modes, file paths from variables, MD5 and SHA-1) are off repo-wide. That retires 41 per-path exclusions.

The exclusion list fell from 52 rules to 3: goconst and dupword in test files, and G204 at each module's one process seam. go-qa-ci's suppression detector counts a rule switched off inside a linter (gosec.excludes, govet.disable) as a suppression, so each of the nine is a permanent record entry giving its one-line reason. A whole linter in linters.disable needs no entry. The record went from 24 entries to 12. The signed ratchet ceiling is unchanged, and go-qa lowers high_water to the new count.

Still open: replacing default: all and its 27 disables with Go's defaults plus the extras the major Go projects run. The extras list is in the coordinator's research, which this checkout cannot read. Also open: the same change in go-qa-ci's own baseline.
