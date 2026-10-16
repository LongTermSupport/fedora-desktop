# SPEC: qa-go-deps and qa-go-vuln, the gates on src-go's dependencies

The intended behaviour of the two gates that hold `src-go/go.mod`'s dependencies to the owner's
decision D2 ([PLAN.md](PLAN.md)): vendored, pinned, allowlisted and checked for known
vulnerabilities. Package `src-go/qa/godeps`; commands `src-go/qa/cmd/qa-go-deps` and
`src-go/qa/cmd/qa-go-vuln`; wrappers `scripts/qa-go-deps.bash` and `scripts/qa-go-vuln.bash`.
Each expectation is a Go test.

## The allowlist

`src-go/go-deps.allowlist`, tab separated, `#` starts a comment line, blank lines skipped:

```text
kind <TAB> module path <TAB> reason
```

| Kind      | Says                                                                                 |
| --------- | ------------------------------------------------------------------------------------ |
| `module`  | the module may be in `src-go/go.mod`'s requirements, direct or indirect              |
| `pseudo`  | the module may be required at a pseudo-version (an untagged commit) instead of a tag |
| `replace` | `src-go/go.mod` may carry a `replace` directive for the module                       |

Versions are not in the allowlist: `go.mod` pins them and `go.sum` locks their content, and a
second statement of a version would drift.

## qa-go-deps

- **G1.** The allowlist is read strictly. A row without three fields, an unknown kind, an empty
  module or reason, or a repeated kind and module is exit 2 naming the line. A missing or
  unreadable allowlist is exit 2.
- **G2.** `src-go/go.mod` is parsed with `golang.org/x/mod/modfile`, never with a line scan. A
  missing or unparseable `go.mod` is exit 2.
- **G3.** Every module `go.mod` requires, direct or indirect, has a `module` row. Each one that
  does not is a finding.
- **G4.** A requirement at a pseudo-version (`module.IsPseudoVersion`) without a `pseudo` row
  for its module is a finding.
- **G5.** A `replace` directive without a `replace` row for the module it replaces is a finding.
- **G6.** A row that `go.mod` does not need is a finding, so the allowlist stays the exact list:
  a `module` row for a module not required, a `pseudo` row for a module required at a tag (or not
  at all), a `replace` row with no such directive.
- **G7.** `go mod verify` passes: every module in the cache matches `go.sum`.
- **G8.** `go mod tidy -diff` prints nothing: `go.mod` and `go.sum` hold exactly what the imports
  need.
- **G9.** `go mod vendor -o <scratch>` reproduces `src-go/vendor/` byte for byte. Each file
  missing from `vendor/`, extra in it, or different is a finding naming the path. A tree that is
  absent counts as empty, so a module with no requirements and no `vendor/` agrees.
- **G10.** Every go command runs in `src-go` through `qa/internal/proc`, with `GOFLAGS` empty (a
  `-mod` from the caller's environment cannot change the verdict), `GOWORK=off` and
  `GOTOOLCHAIN=local`. A go command that cannot be started is exit 2; one that fails is a finding
  carrying its output. The scratch directory is removed whatever the outcome.
- **G11.** Every finding is reported on stderr before the exit, not only the first. Pass: one
  stdout line, `GO-DEPS-OK <n> module(s) allowlisted (<p> at a pseudo-version, <r> replaced); go.sum verified, tidy, vendor/ in sync (<f> files)`.

## qa-go-vuln

- **V1.** govulncheck is pinned by the `tool` directive of `src-go/qa/go.mod`, so its version is
  in `go.mod` and its content in `go.sum`, and nothing is installed system-wide. The gate runs
  `go tool govulncheck -C <src-go> ./...` from `src-go/qa`. In `src-go` the vendor directory
  makes the package load read `vendor/`, so the scan is of the code the binaries are built from.
- **V2.** govulncheck's status 0 is a pass: one stdout line, `GO-VULN-OK src-go: govulncheck found no vulnerability the code reaches`. Status 3 (it found a vulnerability the code calls)
  is exit 1 with its report on stderr. Any other status, including a vulnerability database it
  could not fetch, is exit 2 with its output: the gate could not judge, which is never a pass.
- **V3.** The environment is G10's.

## Both

- **B1.** `-h`/`--help`, `--root DIR` and `--scripts DIR` are `qa/internal/rootargs`; the root is
  resolved by `qa/internal/rootpath`. Exit codes: 0 pass, 1 a finding, 2 the run could not judge.
- **B2.** A closed stdout is exit 2 (`cmdmain.RefuseClosedStdout`).
