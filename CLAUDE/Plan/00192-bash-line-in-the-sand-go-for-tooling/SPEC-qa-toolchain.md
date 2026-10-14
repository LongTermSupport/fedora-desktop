# qa-toolchain: specification of the Go port

Same method as [SPEC-qa-ansible-pause.md](SPEC-qa-ansible-pause.md): **the legacy bash is not an
oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-toolchain.bash` (92 code lines) was read only to discover the requirements. Where it
was wrong the expectation says the right behaviour and a departure (B) says what the bash did.
The proof is the Go tests in `src-go/qa/toolchain` and `src-go/qa/cmd/qa-toolchain` (one or more
per expectation, cited by number) and a run over the live tree at cut-over.

## 1. Role

A gate's verdict belongs to the binary that produced it: two shellchecks four minor releases apart
reported 141 and 173 issues on one tree. `.qa-versions` is the single statement of the version of
every tool a gate runs (ruff, semgrep, shellcheck, golangci-lint); the Go toolchain is a floor
taken from `src-go/go.mod`, not a pin. This gate asserts the machine matches. Callers: `qa-all`
(`scripts/qa-gates.tsv`, gate `toolchain`, a `detail:` reader of the stdout line) and `qa-changed`
(check `toolchain`).

A wrong or absent QA tool fails **this** gate with exit 1 and leaves the other gates their
chance: exit 2 makes `qa-all` abort the whole run, which is how one drifted ruff once took 44
unrelated gates offline. Exit 2 is for the gate itself being unable to judge (its inputs).

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes

- T1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory and the
  root is its parent (the wrapper passes this); neither: the working directory. `-h`/`--help` prints
  the usage to stdout, exit 0. Any other argument beginning `-`, and any positional argument (the
  gate takes no PATH: it judges the machine), is exit 2 with `ERROR: unknown argument '<arg>'` and
  the usage on stderr; a flag missing its value is exit 2. The root is resolved by the shared
  `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is exit 2).
- T2. Exit codes: 0 every tool matches, 1 at least one tool is absent, unreadable, or at another
  version than pinned (or Go is below the floor), 2 the run could not complete (T3-T7, T1, a failed
  write). Every exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2
  with `ERROR: cannot write to stdout: <why>`. Tool drift is never exit 2.

### The pins (`.qa-versions`)

- T3. The pins are read from `<root>/.qa-versions`, parsed as shell with the shared shell parser
  (`mvdan.cc/sh`), never executed. A missing or unreadable file is exit 2 naming it, and says that
  it is the single source of truth for the QA tool versions.
- T4. The file may hold only comments, blank lines and plain assignments `KEY=VALUE`, VALUE one
  literal word (no quotes, no expansion, no command substitution, no `export`, no array, no
  append). Anything else, or a file the parser rejects, is exit 2 naming the line.
- T5. The keys are exactly `RUFF`, `SEMGREP`, `SHELLCHECK`, `GOLANGCI_LINT`. A missing key, an empty
  value, a duplicated key, or a key outside that set (a pin that no assertion reads) is exit 2
  naming the key.
- T6. The Go floor is the `go` directive of `<root>/src-go/go.mod`, read with the `modfile` parser.
  A missing, unreadable or unparseable file, or one with no `go` directive, is exit 2 naming it.
- T7. A file with no entries at all (empty or comment-only `.qa-versions`) is T5's missing keys,
  exit 2: an empty scope never passes.

### The assertions

- T8. The tools are asserted in this order: ruff, semgrep, shellcheck, golangci-lint, go. Every
  one is asserted before the verdict: all mismatches are reported, not the first.
- T9. A tool is looked up on PATH. Absent is a mismatch, never a skip:
  `✗ toolchain: <tool> is not installed (expected <pin>)`. For go: `(need >= <floor>, src-go/go.mod)`.
- T10. The version is read by running `<tool> --version` in the root, bounded in time, from the
  first line (ruff: second field, `ruff 0.16.8`; semgrep: first field, bare number;
  golangci-lint: fourth field, `golangci-lint has version 2.14.0 built ...`) or, for shellcheck,
  the field after `version:` on the line that starts with it. A run that cannot start, times
  out, exits non-zero, or prints no such version is a mismatch:
  `✗ toolchain: <tool> is installed but its version could not be read (expected <pin>)`, followed by
  `its stderr was:` and the tool's captured stderr, or the reason when it has none.
- T11. The pin and the found version must be equal as strings (`0.16.8` is not `0.16.80`, not
  `v0.16.8`). A difference prints `✗ toolchain: <tool> version mismatch`, then
  `    expected : <pin>  (.qa-versions)` and `    found    : <version>  (<path of the tool>)`.
- T12. Go: the version is the first word of `go env GOVERSION` with its `go` prefix, run in the root.
  It must be a valid Go version and not older than the floor, compared by the Go version rules
  (`1.26.2` satisfies `1.26`; `1.25.9` does not satisfy `1.26`). Unreadable (cannot start, times
  out, exits non-zero, prints nothing valid) is the T10 message for `go`. Older prints
  `✗ toolchain: go is older than src-go/go.mod requires`, then `    need >= : <floor>  (src-go/go.mod)`
  and `    found   : <version>  (<path of go>)`.
- T13. Each tool's `--version` stderr is kept and printed only on the unreadable path (T10), never
  discarded and never mixed into a passing run.

### Output

- T14. On any mismatch: after the lines of T9-T12, a blank line and the remedy block on stderr (these
  gates are version dependent; bring the machine into line: the host play, the container rebuild),
  nothing on stdout, exit 1.
- T15. On success: stdout is exactly one line `TOOLCHAIN-OK <N> tool(s) match .qa-versions`, N the
  number of assertions (5); stderr is empty. The qa-all `detail:` reader takes the line.
- T16. No checkout path reaches stdout or stderr; the located path of a tool (T11, T12) is not the
  checkout.

## 3. Departures from the bash

| Id  | The bash                                                                                    | Now                                                                    |
| --- | ------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| B1  | `source`d `.qa-versions`, so any command in it ran                                          | parsed as assignments only, never executed (T3, T4)                    |
| B2  | a missing `.qa-versions` was exit 1                                                         | exit 2: the gate cannot judge without its pins (T3)                    |
| B3  | a pin missing from the file read as unset and aborted on `set -u`, or as a wrong comparison | exit 2 naming the key; an unknown or duplicate key is also exit 2 (T5) |
| B4  | pipeline exit status was ignored: a tool that failed after printing a number still matched  | non-zero exit is unreadable (T10)                                      |
| B5  | a tool hung forever                                                                         | each run is bounded (T10)                                              |
| B6  | `sort -V` compared Go versions, awk took the first `go` line of go.mod whatever it was in   | `go/version` and `modfile` (T6, T12)                                   |
| B7  | an empty or comment-only `.qa-versions` surfaced as set -u errors per tool                  | exit 2 once (T7)                                                       |
| B8  | no timeout, no cause printed when `go env` failed beyond its stderr                         | same message form, reason added when stderr is empty (T10)             |

## 4. Seams

Package `qa/toolchain` is pure except for the injected `Env` (streams, working directory,
`Resolve`, `Open` for the file system, `LookPath`, and a `proc.Runner` for the version runs).
Command: `src-go/qa/cmd/qa-toolchain` fills them with the OS; wrapper `scripts/qa-toolchain.bash`
via `scripts/lib/go-tool.bash`. Nothing is discovered: the population is the fixed tool list (T8),
so an empty scope cannot occur except through the pins file (T5, T7).
