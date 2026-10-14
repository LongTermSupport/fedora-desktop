# Adversarial review: `go-port/qa-discovery` against `integration/261014`

Reviewer: Fable. Subject: commits `d1c4989a`, `87561699` (the port of `scripts/qa-discovery.bash`
to `src-go/qa/discovery` + `qa/cmd/qa-discovery`, spec `SPEC-qa-discovery.md` D1-D14, departures
B1-B11, and the five callers). Method: read the spec, the Go, the wrapper and the base-commit
library side by side; `go test` on the package; the old library (checked out at the base commit)
and the new wrapper run over the same live tree and over a synthetic tree of edge cases, lists
diffed; a planted discovery failure run through each of the five callers; five mutations of the
Go against the test suite; the bash-limits gate; routing and docs read against the code.
`qa-all` was not run.

## Verdict

**Merge after one fix.** The port is sound: the live tree lists are identical to the old
library's on all seven lists (checked independently, not from the report), every intended
departure (B1-B11) behaves as the spec says, NUL handling survives every awkward file name end to
end, a tool failure stops all five callers at exit 2 with nothing on stdout, the empty-list rule
covers every list but templates, and the tests are behavioural with teeth (five mutations, five
cited expectations failing). One class of silent shrinkage is real and undocumented: a file inside
an untracked nested repository is gated by the old `find` and dropped by the Go with no message.
The rest is small follow-up.

## Findings, ranked

### 1. MEDIUM: files in an untracked nested repository vanish silently

`src-go/qa/discovery/list.go:106-121` (`regular`) and `list.go:48-71` (`files`). `git ls-files --others --exclude-standard` does not descend into a nested repository; it prints the one entry
`nested/` (trailing slash). `regular()` Lstats it, finds a directory, returns `false, nil`, and the
whole tree is out of every list with no diagnostic. The spec's D11 says "any other entry that is
not a regular file (a submodule, a directory) is not a script", which reads as a per-file rule; it
never says that this is the one shape in which git hands over an entire tree it will not look
into. No test has such an entry.

Failing input (synthetic tree, proven):

```
git init nested && printf '#!/bin/bash\n' > nested/inner-tool && printf 'x\n' > nested/inner.bash
```

Old library: both files in `shell`. Go: neither, exit 0, stderr empty. On this repository today no
such entry exists (`git ls-files -o --exclude-standard` prints no `*/`), and the one nested clone
(`.claude/hooks-daemon`) is excluded by the trees table, but that is the table's doing, not
discovery's. A second nested clone dropped anywhere else is invisible to every gate.

Fix: a `--others` entry ending in `/` is exit 2 naming it (an untracked repository inside the
tree is either excluded on purpose in `qa-trees.tsv` or a mistake), with a D-expectation and a
real-tree test. The weaker alternative is to state in D4/D11 that nested repositories are out of
scope by design, with the test pinning that; the current text does neither.

### 2. LOW-MEDIUM: the shell predicate now has two owners

`src-go/qa/patterns/discover.go:279-299` (`shellShebang`) is a byte-for-byte copy of
`src-go/qa/discovery/shebang.go` (`parseShebang` + `shellShebang`), and `isShellScript` there
repeats `isOfKind`'s `.j2`/extension rules. `CLAUDE/QA.md:464-466` says `qa-patterns` "applies the
same shell predicate" and D9 says "one predicate per language". That is true today only because
the two texts are identical; a D6 change (say, closing the `-ec` gap below) made in `discovery`
leaves `qa-patterns` scanning a different set, which is exactly the Plan 00076 class the library
exists to prevent. Fix: export the predicate from `discovery` and import it in `patterns`, or a
test in `patterns` that runs both over one table of first lines.

### 3. LOW: the bash trees reader does not enforce D5's two-row rule

`scripts/lib/qa-trees.bash:17-29` accepts a table with no `python` (or no `shell`) row; the Go
(`trees.go:55-57`) refuses it. D14 says the bash reader exits 2 on "a missing table or unknown
list", which is accurate, but the asymmetry is unstated. Exposure is small: every `qa_discover`
caller fails on the Go side (proven below), so only `qa-js` and `qa-ansible-syntax`'s own `find`
loops over `qa_is_excluded` could run with an empty list. Failing input: `qa-trees.tsv` with only
`shell` rows; `qa_py_is_excluded .venv/x.py` returns 1. Fix: the same two-line check in the bash
reader, or a sentence in D14.

### 4. LOW: `QA.md` keeps two bash-era descriptions

- `CLAUDE/QA.md:915-916`: "the shared `scripts/qa-discovery.bash` lists them repo-relative for
  this reason" in a paragraph about `find -path`; the wrapper no longer lists anything, the Go
  does, from git. The paragraph is still right about `qa-js`/`qa-ansible-syntax`, which still use
  `find` + `qa_is_excluded`.
- `CLAUDE/QA.md:1088-1089`: "one list for the shell gates through `qa-discovery.bash` and for
  this tool; a test asserts the two readers agree". There are now three Go readers (`changed`,
  `patterns`, `discovery`) plus the bash one; `scripts/qa-trees.tsv`'s header was updated to say so,
  `QA.md` was not.

### 5. LOW: D6's `-c` exclusion is a literal-field match

`shebang.go:35-38`: a shebang is "inline" only when `-c` is its own field. `#!/bin/bash -ec 'exec x'` (combined flags) is listed as a shell script. The old regex had the same gap; the 82 playbooks
all use `-S bash -c` and are excluded correctly. Failing input: a file whose first line is
`#!/usr/bin/env -S bash -ec 'exec ansible-playbook "$0"'`. Not a regression; noted because the
spec's wording ("without a `-c` argument") promises more than the code checks.

### 6. INFO: test coverage gaps (no defect found behind them)

No test for: a `--others` directory entry (finding 1), a CR-terminated shebang (works:
`strings.Fields` strips `\r`), a BOM (not a shebang, same as before), a file name with a newline
through the wrapper (works, proven on the synthetic tree), `#! /bin/bash` with a space (works, and
is a gain over the old regex). `TestAnUnreadableFileIsNamedNotQuietlyClassified` (D10) skips when
run as root, so it never runs inside the CCY container; it runs in CI.

## What held up

- **Live tree.** Old library (base commit) and new wrapper over the same checkout: `shell` 433,
  `tracked-shell` 433, `python` 311, `tracked-python` 311, `python-templates` 1,
  `tracked-playbooks` 82, `tracked-helper-tests` 131; `comm` of the sorted lists is empty for
  every list. The report's numbers are right.
- **Synthetic tree** (names with a space, a newline, a tab, a leading dash, unicode, a hidden
  `.bash`, a `dir.sh/` directory; shebangs `#! /bin/bash`, `#!/usr/bin/env -S bash -e`, CRLF, BOM,
  `#!/bin/dash`, `#!/bin/sh -e`, `#!/usr/bin/env sh`, `#!/usr/bin/bashful`, `-S bash -c`,
  `FOO=1 bash`, no trailing newline, empty, `python3.11`, `-S python3 -u`, `pypy3`,
  `#!/bin/bash # python`, `#!/usr/bin/python3 # bash`; `*.py.j2`, `*.j2`, `*.sh.j2`, plain `.j2`;
  symlinks to a script, to a tool, dangling, to a directory, to `/usr/bin/bash`; an ignored
  script, an ignored `build/gen.py`, an untracked fresh script and tool, a tracked playbook
  deleted from disk, a nested repository). Every difference from the old library is B1 (ignored
  files), B2/B3 (`bashful`, `# python`, `# bash` mentions no longer match; `sh -e`, `env sh`,
  `#! /bin/bash` now do), or B4 (symlinks now listed), except the nested repository (finding 1).
  All the awkward names came through `qa_discover` into `QA_FILES` intact.
- **Exit-2 propagation.** A trees table with no `python` row (accepted by the bash reader, refused
  by the Go) planted in a scratch checkout: `qa-bash`, `qa-python`, `qa-ansible-syntax`,
  `qa-helper-tests`, `qa-ready-wait-rules` each exit 2, stdout empty, first stderr line the tool's
  `ERROR:`. `qa_discover` resets `QA_FILES` only after the status check and `exit`s the caller, so
  no caller can continue on a stale or empty list; all five call it at top level, never in a
  subshell or `if`.
- **Empty-list rule (B9/D13).** Every list but `python-templates`; a mutation making
  `tracked-playbooks` tolerate empty fails `TestAnEmptyListIsABrokenDiscovery`.
- **Exclusion parity (B11).** `trees.go:excluded` and `_qa_path_in_dirs` are the same test
  (`/dir/` as a substring of `/rel/`) over the same table; the ancestor-of-root case is handled
  on both sides because both see only the relative path.
- **Tests have teeth.** Five mutations on a scratch copy, each caught by the expectation the spec
  cites: drop the `-c` check (D6 `TestShellScriptIsByNameOrShebangNeverByMode`), exclusion as a
  prefix match (D5 `TestExcludedTreesApplyAsWholeSegmentsAnywhereOnTheRelativePath`), symlinks
  not followed (D11, two tests), the `--deleted` filter removed (D4
  `TestPublishedMeansTrackedOrFreshButNeverIgnoredNorDeleted`), playbooks may be empty (D13).
  The wrapper tests were written after the wrapper, as the journal says, but they assert
  behaviour (exit 2 on a non-git root, a space in a name, an empty template list) rather than
  replaying output.
- **Wrapper size, ratchet, routing.** `scripts/qa-bash-limits.bash` passes with the
  `qa-discovery.bash` allowlist row removed (4 functions, no data structure). A change to the
  wrapper or `lib/qa-trees.bash` routes to the `@sh` checks, the Go gate (whose real-tree tests
  run the wrapper) and bash-limits. The `qa-discovery` name in the `perfile-gate` row predates
  this branch.
- **Performance.** One `qa_discover` (build with a warm cache + run over 433 files) is about
  0.2 s; nine calls per full run. The per-file exclusion stays in bash where `qa-js` and
  `qa-ansible-syntax` ask it once per file.
- **`scripts/vault`**, the one tracked symlink (to a vendored directory, dangling in a checkout
  without `roles/vendor`): not a script, no error, on both sides, as D11 says for a link without a
  script extension.
- **CI** installs Go (`setup-go`, `go-version-file: src-go/go.mod`), so the three callers that
  previously needed only git (`qa-ansible-syntax`, `qa-helper-tests`, `qa-ready-wait-rules`) build
  the tool there too.
