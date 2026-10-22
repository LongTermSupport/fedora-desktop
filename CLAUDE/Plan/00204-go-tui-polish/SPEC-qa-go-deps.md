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
| `keepout` | the module is kept out of a command (four fields, below)                             |

A `keepout` row has a package between the module and the reason:
`keepout <TAB> module path <TAB> ./cmd/<name> <TAB> reason` (Plan 00209 SPEC-n2b.md GD1-GD3,
decision n2b-D10).

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
- **G7.** `go mod verify` passes: no module in the cache has changed since it was downloaded
  (it compares the cache with the hashes recorded at download time, not with `go.sum`; `go.sum`
  is checked by G8 and G9, which fail with `SECURITY ERROR` on a hash that disagrees).
- **G8.** `go mod tidy -diff` prints nothing: `go.mod` and `go.sum` hold exactly what the imports
  need.
- **G9.** `go mod vendor -o <scratch>` reproduces `src-go/vendor/` byte for byte. Each file
  missing from `vendor/`, extra in it, or different is a finding naming the path. A tree that is
  absent counts as empty, so a module with no requirements and no `vendor/` agrees.
- **G10.** Every go command runs in `src-go` through `qa/internal/proc`, with `GOFLAGS` empty (a
  `-mod` from the caller's environment cannot change the verdict), `GOWORK=off`,
  `GOTOOLCHAIN=local`, `GOPROXY=https://proxy.golang.org,direct`, `GOSUMDB=sum.golang.org`, and
  `GONOSUMDB`, `GONOPROXY`, `GOPRIVATE` and `GOINSECURE` empty, so no module is exempted from
  the proxy or the checksum database by the caller's environment. A go command that cannot be
  started is exit 2; one that fails is a finding carrying its output (but see G12). The scratch
  directory is removed whatever the outcome.
- **G11.** Every finding is reported on stderr before the exit, not only the first. Pass: one
  stdout line, `GO-DEPS-OK <n> module(s) allowlisted (<p> at a pseudo-version, <r> replaced); go.sum verified, tidy, vendor/ in sync (<f> files)`.
- **G12.** A go command that failed because it could not fetch a module (its output says the
  lookup was disabled, a dial, DNS, proxy or TLS failure, or a timeout; a cold module cache with
  no network) could not judge, and says so. With no other finding the gate is exit 2; a definite
  finding (G3-G9) outranks it and the gate is exit 1, naming both. Output carrying
  `SECURITY ERROR` is always a finding.
- **G14.** A `keepout` row is refused by the allowlist reader (exit 2 naming the line) when its
  package is not `./cmd/<name>`, when the same module and package are listed twice, or when the
  module has no `module` row. For each row, `go list -deps -f '{{with .Module}}{{.Path}}{{end}}'
  <package>` runs in `src-go` (G10's environment); any package it lists in the module is a
  finding naming the package, the module and the row's reason. A `go list` that fails is a
  finding (the build is broken), or could not judge under G12. A keepout row is never stale
  under G6: its module row carries that check.
- **G13.** The pre-commit secret scan skips `src-go/vendor` only when this gate passes over the
  staged tree: when anything under `src-go/vendor/` is staged, `scripts/git-hooks/lib/staged-scan-list.bash`
  checks the staged `src-go` out of the index into a scratch directory and runs
  `qa-go-deps.bash --root` over it; a failure fails the commit.

## qa-go-vuln

- **V1.** govulncheck is pinned by the `tool` directive of `src-go/qa/go.mod`, so its version is
  in `go.mod` and its content in `go.sum`, and nothing is installed system-wide. The gate runs
  `go tool govulncheck -C <src-go> ./...` from `src-go/qa`. In `src-go` the vendor directory
  makes the package load read `vendor/`, so the scan is of the code the binaries are built from.
- **V2.** govulncheck's status 0 is a pass: one stdout line, `GO-VULN-OK src-go: govulncheck found no vulnerability the code reaches`. Status 3 (it found a vulnerability the code calls)
  is exit 1 with its report on stderr. Any other status, including a vulnerability database it
  could not fetch, is exit 2 with its output: the gate could not judge, which is never a pass.
- **V3.** The environment is G10's. govulncheck keeps no database cache, so every run fetches
  `vuln.go.dev`; offline it is exit 2 by V2.

## Both

- **B1.** `-h`/`--help`, `--root DIR` and `--scripts DIR` are `qa/internal/rootargs`; the root is
  resolved by `qa/internal/rootpath`. Exit codes: 0 pass, 1 a finding, 2 the run could not judge.
- **B2.** A closed stdout is exit 2 (`cmdmain.RefuseClosedStdout`).
