# go-qa-ci M5 fix: self-gate derives from the bundled baseline

Commit 40a05bd on main (pushed).

Clauses served: TOOLING-SPEC 8.2 (partially), 9.2 (gaps recorded).

Built:

- `qa/golangci-overlay.yml`: the minimal overlay (depguard allow-list, gosec G204 exclusion, G306 test-file exclusion), with the documented exception comments.
- `.golangci.yml` is the merged result (baseline plus overlay), headed by a comment saying so. golangci-lint cannot merge files, so it is mirrored by hand and guarded by a test.
- `tests/selfgate/selfgate_test.go` (new dependency go.yaml.in/yaml/v3, one of the allowed set):
  - the embedded baseline equals `configs/golangci.yml`
  - `.golangci.yml` equals merge(baseline, overlay)
  - every baseline linter, setting, formatter and cap is present in `.golangci.yml`
  - negative tests show the checker rejects a dropped linter, formatter, setting or cap
  - an overlay cannot override a baseline scalar
  - the overlay holds only depguard, G204 and G306, and `docs/record.md` lists both exceptions
- `docs/golangci.md` Self-gate section rewritten. It records that 8.2 is NOT met in full: tools switched off in `qa/go-qa.toml`, the hand-mirrored merge, and only the golangci rules being proven.
- `ci.yml` header updated; README gap wording fixed.

Gates: gofmt, vet, `go test -race`, golangci-lint (0 issues). I did not run `go-qa run --ci` end to end, because a hook blocked the command.

Deviation: the self-gate runs the merged `.golangci.yml`, not the embedded baseline directly, and the overlay is not applied at run time.
