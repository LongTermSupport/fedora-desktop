# Adversarial review: `go-port/qa-python` (delta over `go-port/qa-discovery`)

Reviewed: `35437cd4` (merge of qa-discovery into qa-python), delta `e894d13c`. Method: read the
Go, the spec and the removed bash; built the real binary; ran it over the live tree and over a
scratch git repository with planted inputs. Unit tests, `go vet` and `golangci-lint` for
`qa/python`, `qa/cmd/qa-python` and `qa/changed` all pass. `qa-all` was not run.

## Verdict

**Merge after fixing finding 1; findings 2 and 3 should land with it.** The port is sound: every
class of defect the bash caught is still caught, and several the bash missed are now exit 2. The
one real hole is that the "proof ruff read every file" proves selection, not the ruleset: a
nested `pyproject.toml` or `ruff.toml` anywhere under the root silently replaces the repo's rule
set for its subtree while `--show-files` still lists the file. One flag closes it.

## Findings, ranked

### 1. HIGH — a nested ruff config silently changes the ruleset; `--show-files` does not notice

`src-go/qa/python/ruff.go:97` (the lint argv) and `:139` (the `--show-files` argv) run ruff
without `--config`, so ruff's hierarchical config discovery applies: the *closest* `ruff.toml`,
`.ruff.toml` or `pyproject.toml [tool.ruff]` above each file wins. The spec's premise (section
1: "the ruleset is data, in `ruff.toml`") and the `--show-files` proof (Y15) both assume the
root file is the only one.

Failing input (fixture repo, root `ruff.toml` as in this repo):

```
sub/pyproject.toml   [tool.ruff.lint]\nselect = []
sub/bad.py           import os\nprint(undefined)
```

Result: exit 1 for an unrelated file only; `sub/bad.py` (F401, F821) is listed by `--show-files`,
linted under an empty rule set, and reported `pass`. Same with `sub2/ruff.toml` containing
`[lint]\nselect = []`. Verified fix on ruff 0.16.8: `ruff check --config <root>/ruff.toml …`
disables hierarchical discovery and reports both findings (`--isolated` also works but drops
the repo's `select`, adding I001). Add `--config` to both invocations, and a real-tool test
with a nested config under `qa/cmd/qa-python/main_test.go`.

Related: `src-go/qa/changed/checks.go:191` runs `ruff check <files>` per file with neither
`--config` nor `--no-cache`, so qa-changed has the same hole and writes `.ruff_cache` (not
gitignored; only `__pycache__/` is). Pre-existing, but the same one-line fix.

### 2. MEDIUM — `# ruff: noqa` / `# noqa` silence the gate

`ruff.go:97`: a committed `# ruff: noqa` (file-level) or `# noqa: F401` passes. Failing input:
`noqa.py` = `# ruff: noqa\nimport os\n` → `pass`. The hooks daemon blocks *writing* a
suppression directive, but a file that reaches the tree by any other route (merge, `git add` of
existing content, a Bash heredoc) is never checked by any gate (no `noqa` scan exists under
`scripts/` or `.semgrep/`). CLAUDE/QA.md says suppression directives are banned; the gate can
enforce that with `--ignore-noqa` (verified: reports F401 on the input above). Pre-existing in
the bash; worth closing while the argv is being touched.

### 3. MEDIUM — a relative or trailing-slash `--root` is a spurious exit 2 blaming `ruff.toml`

`run.go:120` (`rootDir`) uses the root as given. Discovery prints `<root>/<rel>` and
`discover.go:44` strips `g.root+"/"`; `ruff.go:139` compares `--show-files` output (always
absolute, verified) with `filepath.Join(g.root, rel)`.

- `--root .` or `--scripts scripts` (from the root): rels are right but the paths handed to
  ruff are relative, the listed paths are absolute, so every file is "not selected": exit 2
  "check ruff.toml's exclude rules". Wrong diagnosis of a usage case Y1 permits.
- `--root /path/` (trailing slash): the prefix strip misses, rels stay absolute, every path
  given to ruff and python3 is `/path//path/file.py`; python3 reports OSError for each
  (recorded as compile errors), then ruff fails selection: exit 2.

Fail-fast, never a false pass, but the message is wrong. Fix: `filepath.Abs` + `Clean` the root
once in `Main`. `TestRootComesFromTheFlagsOrTheWorkingDirectory` uses absolute roots only.

### 4. LOW — qa-changed triggers: a Python `.j2` runs nothing

`scripts/qa-routing.tsv:639` (`first *.j2 - -`, "only a suite naming it covers it") predates
this port: the python gate *does* compile a Python template, so an edit to
`files/home/.local/bin/git-account-helper.j2` (the one live template) runs no Python check
in qa-changed. Add `also *.j2 python -` (the gate is 0.8 s on the live tree) or a predicate.
A nested `pyproject.toml`/`ruff.toml` is also not a trigger (`ruff.toml` matches the root file
only); moot once finding 1 pins `--config`.

Nothing double-runs: the per-file `ruff` and `unit-tests` rows still fire for an ordinary `.py`
(`classify_test.go:280` asserts `python` does not), and the whole gate fires only for its own
inputs. `scripts/lib/go-tool.bash` and `qa-trees.tsv` fan out to `patterns` and `python`, which
is correct.

### 5. LOW — a closed stdout exits 1, not 2

Y3 and `TestAFailedStdoutWriteIsExitTwo` (`run_test.go:100`) cover a writer that *errors*
(`>/dev/full` → `ERROR: cannot write to stdout`, exit 2, verified). With fd 1 closed
(`qa-python --root X >&-`) the run exits 1 with the report intact and the stdout text lost
without error. Rare; note it against the Y3 claim rather than fix it.

### 6. INFO — the compiler's version is not pinned

`.qa-versions` pins ruff only; the compile verdict is the host's `python3`. Input
`type X = int` (3.12 syntax) is a SyntaxError on this 3.11 container and compiles on a 3.14
host. Same as the bash; neither the spec nor QA.md says so. Record it in the spec or pin.

### 7. INFO — template edge cases are fail-safe

`compile.go:38` substitutes `{{ … }}` only. A `{# comment #}` renders as-is and fails compile
(false failure, never a pass). A Python string containing `{%` (`"%s{%d}"`) is exit 2 by the
`strings.Contains(data, "{%")` test at `compile.go:53`. Both err on the safe side.

### 8. INFO — the `syntax-error` fallback is dead on the pinned ruff

`ruff.go:197`: ruff 0.16.8 gives syntax diagnostics `code: "invalid-syntax"`, not `null`, so
`judge` prints `invalid-syntax:1 …`; Y18's `syntax-error` text is reached only by an older
ruff. Harmless; the spec wording is slightly stale.

### 9. INFO — argv scales with the file count

One `python3` and two `ruff` processes each take every absolute path on argv (`compile.go:86`,
`ruff.go:92`): ~30 KB for 311 files, against an ARG_MAX of ~2 MB. Fine for this tree; a
`--files-from`/stdin form would remove the ceiling if the tree grows twenty-fold.

## What held up (all run against the real binary)

- Live tree: `✓ python: 311 files OK; 1 rendered template(s) compile`, exit 0, 0.8 s, no
  `__pycache__` or `.ruff_cache` written (the bash left `__pycache__` behind).
- Caught, each with a correct `✗` line and a `failures[]` entry: `def f(:`; `import os, sys`
  (E401) + `x == None` (E711) + F401; F821; bad `coding:` declaration (compile only);
  tabs/spaces (TabError, compile) ; invalid UTF-8 (compile + ruff E902); a NUL byte
  (ValueError, caught by the checker's except list); a 200 000-line file (compiles, no
  scanner overflow); an extensionless `#!/usr/bin/env python3` script with mode 0644 (F401
  reported); a `.j2` with a Python shebang rendered and compiled; a symlink under its own name.
- Exit 2, with the right message: a template holding `{% … %}`; `force-exclude` +
  `extend-exclude` dropping a given file (the Y15 proof works for *selection*); a dangling
  `*.py` symlink; two `RUFF=` lines; an unwritable `QA_JSON_OUT` (no report written);
  `python3` absent from PATH; unknown argument and `--root` without a value (usage on stderr).
- Exit 1 with the Y6 report, byte-for-byte as specified: ruff version mismatch (ruff not run).
- CRLF in `.qa-versions` tolerated (`TrimSpace`); a tracked `pkg.py/m.py` directory name is
  handled by discovery as a file path, not a file.
- Paths in stdout and the report are relative to the root (checked in the real-tool test and
  by hand); diagnostics keep every ruff field.
- Tests assert behaviour, not argv echoes: real ruff and python3 over a real work tree for the
  clean/failing/excluded/extensionless cases; fake runner for the exit-2 paths; the argv
  equality of the two ruff runs (`TestRuffIsGivenTheSameFilesBothTimes`).
- Lint exclusions in `src-go/.golangci.yml` are path- and text-scoped (G304 for computed
  reads, `ruff_diagnostics` tag casing); the allowlist row for the bash is removed, the
  wrapper is 4 code lines; `CLAUDE/QA.md` and `src-go/CLAUDE.md` describe what the code does.

## Fixture

Scratch repository under `/tmp` (not kept): root `ruff.toml` and `scripts/qa-trees.tsv` copied
from this repo, `.qa-versions` = `RUFF=0.16.8`, planted files as named above, run with
`qa-python --root <fixture>`.
