## 10:03 · decision · —

The second half of the owner's Go QA ruling of 2026-11-18 is applied, with the extras list from the coordinator's research. src-go's golangci-lint baseline is now default: standard, with nothing set to enable-all, and these extras:

- the checks at least one major Go project enables: errorlint, bodyclose, exhaustive, misspell, unconvert, usestdlibvars, predeclared, testifylint, nolintlint, unparam, gocritic and revive (each with its default checks), and gosec;
- narrow bug patterns: nilerr, nilnil, errchkjson, durationcheck and thelper;
- gocognit as the one complexity measure;
- govet with the analyzers gopls runs by default (nilness, unusedwrite, atomicalign, deepequalerrors, sortslice, timeformat), and shadow and fieldalignment off.

The 27 disables are gone, and so are goconst and dupword, along with their test-file exclusion. No new finding appeared, because every linter kept was already on under default: all. Two exclusion rules are left, G204 at the two process seams. The record holds 10 entries: G204, the generated header, and the eight gosec rules that are off repo-wide. Those eight remain only until go-qa-ci treats a repo-wide rule switch-off like a linter disable (go-qa-ci issue 11). go-qa lowered high_water to 10.
