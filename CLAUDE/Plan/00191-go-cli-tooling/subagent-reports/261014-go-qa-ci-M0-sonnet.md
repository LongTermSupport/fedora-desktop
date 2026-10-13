# go-qa-ci M0: public-safety first

## Built

- go.mod (module github.com/LongTermSupport/go-qa-ci, go 1.26), README stub, MIT LICENSE.
- cmd/go-qa: `--version` exits 0; no arguments or an unknown subcommand exits 2; write failures exit 1.
- scripts/public-safety-scan.sh (modes tree, staged, message, history, stdin; env GQACI_DENYLIST or git config gqaci.denylist), scripts/secret-scan.sh (gitleaks, no suppression route), .githooks/, .github/workflows/public-safety.yml, .github/scripts/write-denylist.sh, docs/denylist.example, docs/PUBLICATION-CHECKLIST.md, PR and issue templates.
- .golangci.yml: the brief's 3.2 baseline as the interim config (M5 finalises).

## Review of the earlier uncommitted files

All reviewed against the brief; kept unchanged. Action SHAs re-resolved with gh api: actions/checkout v7.0.1 is commit 3d3c42e5aac5ba805825da76410c181273ba90b1 (latest release), gitleaks 8.30.1 (latest release). shellcheck is clean on all scripts.

## Owner action required

The repository secret GQACI_DENYLIST must be set by the owner (its content is the real denylist, format in docs/denylist.example). The workflow fails loudly without it (write-denylist.sh exits 1; only fork PRs, which get no secrets, warn). The local hooks are not enabled by default: each clone runs `git config core.hooksPath .githooks` and supplies a denylist.

## Tests

- cmd/go-qa/main_test.go: version, unknown subcommand, no arguments.
- tests/scripts/public_safety_test.go: a planted term in a tracked file fails the tree scan with "denylist line 3 matched at notes.txt:2" and the output never contains the term; clean tree passes; message scan fails by line; missing, in-repo and empty denylists exit 2.

## Spec clauses served

No TOOLING-SPEC conformance is claimed or served yet. M0 serves SPEC section 8 project-level hygiene only indirectly (nothing project-specific enters a public repository). Gaps are all of TOOLING-SPEC 3-9, recorded as not yet built.

## Deviations

- tests/scripts uses constant sh command lines with env-passed values, because gosec G204 flags variable exec arguments and no suppression is allowed.
- Gates: gofmt, go vet, go test -race, golangci-lint (0 issues) all green.
