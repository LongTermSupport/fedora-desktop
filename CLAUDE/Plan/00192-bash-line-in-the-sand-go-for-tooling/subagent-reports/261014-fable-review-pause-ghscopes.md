# Review: go-port/qa-ansible-pause and go-port/qa-gh-scopes (Plan 00192, P3)

Adversarial, read-only review of the two branches against `integration/261014` (both sit on its
tip, merge-base `1e9553b8`). Method checked: spec with numbered expectations, tests first, bash
not an oracle, thin `go-tool.bash` wrapper. Verified by running `go test`, `golangci-lint`,
`gofmt`, `go vet` over the new packages and `qa/changed` in detached worktrees (all green on
both branches), by `qa-changed --plan` dry runs on the routing, and by probing the real semgrep
where the spec makes a claim about it. No install-specific name was found in either diff or in
the commit messages.

Severity: **BLOCK** = a gate can pass when it should fail, or a check vouches for a file it
never reads; **FIX** = wrong but fail-closed, or a spec claim the code does not meet; **NOTE** =
drift, duplication, housekeeping.

## Findings: go-port/qa-gh-scopes

### G-1 BLOCK: the rule self-test passes vacuously on a fixture with no annotations (G6)

- `src-go/qa/ghscopes/scan.go:27-36` (`proveRule`); spec G6 "A rule edited into matching
  nothing fails here and never reports a clean repository".
- Scenario: `.semgrep/gh-scopes.fixture` loses its `# ruleid:` lines (a careless edit, a
  merge, a reformat). `semgrep --test` then prints `No unit tests found` and **exits 0**
  (verified with the real engine against the project's own rule). `proveRule` returns `ExitOK`,
  the scan runs, and from then on the rule is "proven" by nothing: a rule edited into matching
  nothing passes G6 and reports a clean tree. The ansible-pause port guards exactly this
  (`flagged.yml marks no EXPECT-FINDING line; the red case proves nothing`); this port does not.
- Fix: after a passing `--test`, require the fixture to carry at least one `# ruleid: gh-scope-outside-ssot` line (read the fixture; it is already stat'ed in `readRule`), or run
  `semgrep --test --json` and demand a non-zero count of checked rules; exit 2 otherwise with
  "the fixture marks no ruleid line; the self-test proves nothing". Add a G6 test (fixture with
  no annotations, `testCode: 0` -> exit 2) and a spec sentence. The same class exists in
  `src-go/qa/patterns/semgrep.go:39` (pre-existing, outside this branch): a Defence Before Fix
  candidate for one rule over both.

### G-2 FIX: the exclude globs are anchored; semgrep matches them gitignore-style (G12, B5)

- `src-go/qa/ghscopes/excludes.go:80-101` (`globRegexp` writes `^...$`); spec G12 "A glob
  uses semgrep's meaning".
- Verified with the real engine over the project's rule: semgrep silently dropped
  `sub/vars/github-required-scopes.yml` and `x/CLAUDE/Plan/deep.md` (the pattern matches at any
  depth, as `.gitignore` does), and scanned only `plain.txt`. The Go expects both nested files
  to be scanned, so the moment such a path exists in the tree the gate exits 2 "did not scan ...
  not a pass". Fail-closed, so no false pass, but it is a gate outage waiting for the first
  nested `CLAUDE/Plan` directory or a copied `vars/` tree, and the spec claim is false.
- Fix: match each glob against the path and against every suffix that starts at a `/`
  boundary (or prefix the regexp with `^(?:.*/)?` when the glob has no leading `/`), add a
  real-engine test in `src-go/qa/cmd/qa-gh-scopes/main_test.go` with a nested
  `vars/github-required-scopes.yml` copy, and amend G12/B5 to state the anchoring rule.

### G-3 FIX: the check is not `whole`, so a deletion never asks for it

- `src-go/qa/changed/checks.go:80` (`gh-scopes` has `gate: true` but no `whole: true`);
  `src-go/qa/changed/classify.go:206` drops a deleted file's non-whole checks.
- Scenario: `git rm .semgrep/gh-scopes.fixture` and commit; `qa-changed` runs nothing for it
  (the row's `covered` flag even marks it covered), and the breakage surfaces only at `qa-all`.
  The gate scans the whole repository whatever changed; it is `whole` by the field's own
  definition (`checks.go:31-33`), as `ansible-pause` correctly is on the sibling branch.
- Fix: `whole: true`; extend `TestDeletedFilesOweOnlyTheWholeGates` with a deleted
  `.semgrep/gh-scopes.fixture`.

### G-4 NOTE: scope duplicates the discovery's "what git would publish"

- `src-go/qa/ghscopes/scan.go:41-68` runs its own `git ls-files -z --cached --others --exclude-standard` and its own regular-file filter. Every other P3 gate takes its list from
  `qa/discovery` so the trees table (`scripts/qa-trees.tsv`) applies and the routing row
  `src-go/qa/discovery/*` reruns it. The spec's reason (the rule is language-agnostic) is sound;
  the answer is an `all`/`published` list in discovery (one D-expectation), not a second git
  walk. Not blocking: the semantics are the same today.

### G-5 NOTE: YAML decode is slightly laxer than G12 states

- `src-go/qa/ghscopes/excludes.go:44-51`. `yaml.v3` decodes `exclude: [123, true]` into
  `[]string{"123","true"}` without error, and reads only the first document of a multi-document
  file. G12 says "not a list of strings is exit 2". Harmless for the real rule; either decode
  `Exclude` as `[]yaml.Node` and require `!!str` scalars, or soften the spec sentence.

### G-6 NOTE: `requireSemgrep` hides the real cause and borrows the git timeout

- `src-go/qa/ghscopes/scan.go:16-22`: a `--version` that times out or exits 1 is reported as
  "semgrep not found. Install with: pipx install semgrep", the `err`/`res.Stderr` discarded;
  and the limit is `gitTimeout`. Append the cause (`: %w` / stderr) and name a `versionTimeout`.

### G-7 NOTE: routing hole stated nowhere

- Every published file is an input to this gate, but `qa-changed` runs it only for the rule,
  fixture, wrapper and tool (`scripts/qa-routing.tsv:44-47`; a `docs/README.md` edit plans
  `check docs` only, verified with `--plan`). Pre-existing, and a per-commit semgrep over ~2600
  files may be too slow to want; but the gap should be a row in `SPEC-qa-changed.md` (the
  sibling branch recorded its equivalent as B38) rather than unstated.

### G-8 NOTE: docs drift

- `scripts/qa-gh-scopes.bash:9-10` still says "a change to either file runs this gate
  (scripts/qa-routing.tsv selects a suite by the files its script names)"; the branch moved
  that to explicit `also` rows and a check, and took the script out of the suite set
  (`perfile-gate`). Reword to name the rows.
- `CLAUDE/Plan/00192-.../PLAN.md` Task 3.1 has a paragraph per merged port; this port has none
  yet (by convention written at merge; the integrator owns it).

## Findings: go-port/qa-ansible-pause

### P-1 BLOCK: the fixtures are routed to a check that never reads them

- `scripts/qa-routing.tsv:78` (`also tests/fixtures/ansible-pause/* ansible-pause covered`);
  SPEC-qa-changed B38 "its fixtures count as covered".
- The tool does no self-test (B1, B7): the only thing that reads `flagged.yml`/`clean.yml` is
  the Go test `TestE17RuleProvesItself...` in `src-go/qa/ansiblepause/scan_test.go:77-104`,
  run by the `go` check. Verified with `--plan`: an edit to
  `tests/fixtures/ansible-pause/flagged.yml` plans `check ansible-pause` and nothing else, so
  a fixture emptied of `EXPECT-FINDING` lines (or an `EXPECT-FINDING` left on a registered task)
  is committed "covered" while E17 is never exercised before the push. This is the class the
  `qa-reviewer` brief names: verification that does not exercise the code path it vouches for.
- Fix: route the fixtures to `go` (`also tests/fixtures/ansible-pause/* go covered`), as the
  existing `also ... go covered` rows do for other Go-test inputs; drop them from the
  `ansible-pause` row; make `TestAPauseGateEditRunsTheGate` assert `p.has("go")` for the two
  fixture paths; reword B38 and the routing comment. (The `go` gate is the slow one, so keeping
  the fixtures out of the `ansible-pause` row is fine.)

### P-2 FIX: a dash followed by more than one space is not a pause key (E8)

- `src-go/qa/ansiblepause/scan.go:13` (`^ *(- )?...pause:`) and `:17`, and `task()` assumes
  the key column is dash indent + 2 (`scan.go:68`). YAML allows `-   pause:` and `-   name: x`
  with keys at column 4. `-   pause:` is not matched at all (false negative: a pause with no
  register passes); `-   name: x` as a head works only by luck of the `register:` indent check.
  Rare in this tree, but a false negative in a text rule is exactly what E13 says the rule may
  not have. Fix: `^( *)-( +)` with the key indent taken from the match, tests for both forms.

### P-3 FIX: an absolute PATH through a symlinked checkout is "outside the root" (E3)

- `src-go/qa/ansiblepause/run.go:201-214`: the root is resolved through `env.Resolve`, the
  argument is not, so `filepath.Rel` yields `../...` and exit 2 for a legitimate file. Resolve
  the argument the same way (or its parent directory) before `Rel`; one test in the cmd package.

### P-4 NOTE: E6's "a symlink to a readable file is read as that file" has no test

- `os.DirFS` + `fs.ReadFile` does follow symlinks, so the behaviour holds, but the
  expectation is cited by no test. One case in `src-go/qa/cmd/qa-ansible-pause/main_test.go`.

### P-5 NOTE: spec/behaviour that is right and worth keeping

- E5 zero-population exit 2, E6 exit 2 on an unreadable file after judging the rest, E14-E16
  stream split (count line alone on stdout; `qa-gates.tsv:60` reads it with `count`), E17 with
  the "no EXPECT-FINDING" guard, the `whole: true` check and the deleted-playbook test, the
  cut-over (86/86 on the live tree) all check out. The scanner reproduces the awk's rule exactly
  and the E9-E11 cases (comment at dash indent ends a task, nested/next-task `register:`,
  blank line breaking the comment run, empty reason) are each tested.

## Both branches

### B-1 NOTE: they will conflict on merge, and share a journal stamp

- Both add at the same insertion points: `src-go/qa/changed/checks.go:80` and `:133`,
  `checks_test.go` (three lists), the `perfile-gate` line of `scripts/qa-routing.tsv`, adjacent
  lines of `scripts/bash-limits.allowlist`, the same paragraph slot in `src-go/CLAUDE.md`, the
  tail of `JOURNAL/00192-Journal-26-10-14.md`, and `CLAUDE/QA.md`. Whichever merges second needs
  a hand resolution; the checks and perfile-gate lists must end up holding both ids.
- Both journal entries are stamped `22:05`. Entries are meant to come from `mkplan.bash --journal`; two branches landing the identical minute is possible but worth a look.

### B-2 NOTE: method compliance

- Specs exist with numbered expectations; every G1-G16 and E1-E17 (bar the stated E13 limit)
  is cited by a test that drives `Main()` through fakes, plus a real-engine test each. The bash
  was read for requirements and departures are tabled (B1-B8, B1-B7). Wrappers are two-line
  `run_go_tool` calls, off the allowlist. `golangci-lint`, `gofmt`, `go vet` clean. The only
  method gaps are G-1 (a spec claim the test corpus does not falsify) and P-1 (a routing claim
  that the dry run falsifies).

## Verdict

Not mergeable as they stand: G-1 and P-1 are each a gate or a cover that reports green without
reading what it vouches for. G-2 and G-3 should go in with G-1 since the same files are open.
P-2 and P-3 are small and local. Everything else is a note for the integrator.
