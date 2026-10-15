# Adversarial review: `go-port/qa-version-pins`, `go-port/qa-vmtest-manifest`, `go-port/qa-helper-tests`

Three Plan 00192 ports, each one commit on `origin/integration/261014` (`1c8af5ea`), reviewed
with the method of `src-go/CLAUDE.md` "How a port is built": the spec first, then the Go and the
base-branch bash side by side, then the tests against the spec's numbered expectations, then the
wiring (`qa-routing.tsv`, `checks.go`, `qa-gates.tsv`, deviations), the docs and the journal. Each
branch was checked out in a detached scratch worktree (removed afterwards); `go test -count=1` over
the new packages, `qa/changed`, `qa/gates` and `qa/internal/proc`, and `golangci-lint run` over the
new packages, were run on each and are clean. `qa-all.bash` and the full helper suite were not run.
The defect classes of the earlier reviews (`261014-fable-review-*.md`, `261015-fable-review-psl-toolchain-stt.md`:
vacuous self-tests, unread or unparsed files passing, lost exit codes, lost counts, scope drift from
"what git would publish", routing holes, path leaks, stale docs, stale exit-2 rationale) were hunted
on each branch. PLAN.md Task 3.3 not yet naming these ports is done at merge and is not reported.

Severity: **BLOCK** (do not merge), **FIX** (fix before or with the merge), **LOW** (fix when
touched), **INFO** (recorded, no action owed).

## Verdict

- **qa-version-pins**: merge after two FIXes: the spec's routing paragraph names trees the
  routing table does not route (and a test pins that it does not), and the two definitions of the
  manifest's shape (Go for the gate, Python for the host consumers) need a cross-check test that
  pins both to the same verdicts, which is the judgement asked for below (VP-2). Two LOWs.
  Live tree: `VERSION-PINS-OK 11 pin(s), 1 with install state tracked, 10 declared untracked — COVERAGE: 11 of 11`, the same as the bash, reproduced through `TestTheLiveManifestResolves`.
- **qa-vmtest-manifest**: merge; no FIX. The design (strict YAML in Go, one Python definition of
  a valid manifest fed as JSON, cross-checks in Go) is the right one for this gate and the
  tests prove every V-number. Three LOWs. Live tree: the three lines the bash printed
  (`scenarios=8 runnable=8 bridge=7 host_only=1 bases=3`, `8 scenario(s) agree`, `1 scenario fixture(s) run before a declared reboot`), reproduced through `TestTheLiveManifestPasses`
  against the real validator and the real guest scripts.
- **qa-helper-tests**: merge; no FIX. B9 (no `qa-changed` check) is the right decision, judged
  below (HT-1). Two LOWs. Live tree per the journal: 4301 tests in 130 modules (130 tracked), 2
  skipped, both tools; the real-runner tests in `cmd/qa-helper-tests` exercise the production
  path (real git index, real discovery, real `unittest_counts.py`) on small trees.

No BLOCK on any branch. No install-specific name in any diff (the `/workspace` and `hostname`
hits in the vmtest branch's `CLAUDE/QA.md` diff are pre-existing rows the table formatter
re-aligned: the ccy mount point and the RFC 1123 hostname grammar row).

---

## Branch `go-port/qa-version-pins` (`d9711d3c`)

### VP-1 — FIX — the spec's routing paragraph names trees the table does not route

`SPEC-qa-version-pins.md:133-136` (section 4): "`qa-changed` runs the gate whole when the
manifest, **a playbook or task or vars file**, the `.pin` file ... changes or is deleted".
`scripts/qa-routing.tsv:157-160` routes `vars/version-pins.yml`, the `tuwunel.pin` file,
`playbooks/*.yml,playbooks/*.yaml` and the gate's own files, and nothing under `tasks/` or
`vars/` beyond the manifest. `changed/classify_test.go:512-518` pins the opposite of the spec:
`tasks/x.yml` "must not run the version-pins gate". The row comment (`:152-156`) says
"playbooks and the agent-bus pin file", which is what the table does.

Failure scenario: a pin is later declared in `vars/some-pins.yml` or a `tasks/` file (the manifest
field is documented as "path, from the repo root, to the playbook holding the pin", but V17-V20
accept any repository-relative file, and `tuwunel.pin` already is one); the author reads section 4,
expects the commit check to catch a renamed var there, and it is caught only by `qa-all`. The
code is right and the spec is wrong: today no pin lives under `tasks/` or `vars/`, the stated limit
in the same paragraph ("a pin may name any repository file, and one outside those trees is judged
by `qa-all`") already covers them, and widening the rows to `vars/*.yml` would run the gate on
every vars edit for no pin. Fix: drop "or task or vars file" from section 4 (and from the B45 row
if it is repeated there; it is not), so the three documents (spec, table comment, test) say the
same thing. One sentence.

### VP-2 — FIX — two definitions of the manifest's shape, with no test that they agree

The facts:

- `src-go/qa/versionpins/manifest.go` (V7-V16) states the row shape in Go for the gate.
  `helpers/version_pins/manifest.py:147-206` states it in Python for `check_pins.py` (the login-time
  installed-against-pinned check) and `scripts/check-pinned-versions.bash:97-112` (the upstream
  review tool). Both consumers still read the Python; neither runs in `qa-all`.
- The spec records this as a stated limit (`SPEC-qa-version-pins.md:128-130`): "A rule added to
  one is added to the other." That is a hope, not a check, and this plan's own method says a
  stated limit is where the next false pass lives.
- Known divergences today, read side by side: Go-only rules are B9 (only `version_pins` at the top
  level), V16 (at least one tracked pin; the bash had it, the Python `main` does not) and the
  reporting of every finding with a line number. Every rule the Python enforces (unknown keys,
  required fields, non-string fields, the three row delimiters in all five fields, `owner/repo`,
  note-when-no-github, the `installed` block and its kinds, the `(playbook, var)` pair once) has a
  Go counterpart with the same verdict. I found no manifest the Go accepts and the Python rejects.
  That is the dangerous direction: it is the manifest the gate passes and the host tool then
  refuses at login or review time, which is the exact failure this gate exists to surface early.
  The other direction (Go stricter) only costs a commit.

The judgement asked for: **which design is right for version-pins, Go parsing or piping to the
Python as vmtest does?** Go parsing is right here, for three reasons that do not hold for vmtest.
First, half the gate's value is in facts a YAML-to-JSON conversion destroys: a repeated key (B1),
a non-string scalar's tag, an alias, a line number for the finding; the vmtest port needs none of
those beyond "one document, no duplicate key" and so can hand the JSON on. Second, the gate's
other half (V17-V21, every row resolves on disk) was never in the Python and is not the Python's
job, so the Go half exists either way. Third, the vmtest validator is the deploy path itself
(Ansible renders the manifest to JSON and runs that Python on the host), so one definition there
is one definition of what gets deployed; the version-pins Python is read by two host review
tools, and the gate judging the manifest before those tools do is the point. Piping would also
make `python3` a dependency of a gate that otherwise starts no process, to gain a verdict the Go
already gives with more detail.

So the drift is closed with a cross-check, not by choosing an owner. Fix: a test in
`src-go/qa/cmd/qa-version-pins` (the command package already runs on the real file system, and
"some Go tests run the real ... Python helpers and compare results" is the stated precedent in
`src-go/CLAUDE.md`) that takes a table of manifests, the good one and one per shape rule (the V7-V15
cases already in `run_test.go` are the table), decodes each with `go.yaml.in/yaml/v3` into `any`,
`json.Marshal`s it, runs `python3 -m helpers.version_pins.manifest` on it through `proc.OS` in the
repository root, and asserts: Python's `VERSION-PINS-OK`/`VERSION-PINS-INVALID` equals the Go
verdict "no shape finding"/"a shape finding", **except** for the rules listed by name as Go-only
(B9, V16), which the test asserts the Python *accepts* so a later Python rule for them is noticed
too; plus the live manifest through the Python. A manifest the YAML library cannot decode into
JSON (a repeated key) is Go-only by construction and skipped with that reason. Then reword section
4: the two definitions are pinned to each other by `Test<name>`, not by discipline. With this, also
fix the stale prose that the port left behind: `helpers/version_pins/manifest.py:6-9` ("this takes
the JSON that `scripts/qa-version-pins.bash` converts the YAML into"; nothing in the gate feeds it
now, only `check-pinned-versions.bash` and `check_pins.py`), and
`scripts/check-pinned-versions.bash:88-92` ("for the reason qa-version-pins.bash records"; the
wrapper records no such reason since the port).

### VP-3 — LOW — the tracked-pin floor (V16) is withheld while any other finding exists

`manifest.go:90-93`: the V16 finding is added only when `len(r.findings) == 0 && r.tracked == 0`,
and `r.tracked` is counted (`:161-167`) only for rows with no finding of their own. A manifest with
one typo'd key and no tracked pin reports the typo; the author fixes it and is then told about V16.
V24 promises "every finding is reported". The guard exists because a row whose `installed` block
is malformed cannot be called tracked; but `readInstalled` already returns the `tracked` bool
independently of the row's other findings, so `r.tracked` can count `p.tracked` for every mapping
row and the `len(r.findings) == 0` condition can go. Small; the exit code is 1 either way.

### VP-4 — INFO — what held up

- V1-V3: `-h`/`--help`, unknown argument with usage, missing values, a stray positional (exit 2);
  `--scripts` opens the parent, `--root` wins; `/dev/full`-class stdout failure (`failWriter`);
  unresolvable root; a finding prints nothing on stdout; no `/repo` in stderr; on the real file
  system a root reached through a symlink leaks neither path (`TestTheRealFileSystemASymlinkedRootAndASymlinkedPlaybook`).
- V4-V6: absent, directory, symlink (fake and real: `os.DirFS` implements `ReadLinkFS`, so
  `fs.Lstat` sees the link; `go 1.26.0`) and unreadable manifest exit 2; four non-YAML shapes and
  a two-document file exit 2 naming the file; seven empty shapes exit 2 for being empty and are
  not reported as findings (B2, the control the bash ran every time).
- V7-V16: nine document shapes; repeated keys at row, `installed` and top level; the merge key is
  an unknown key; an alias is followed (`&inst`/`*inst`) and an anchored scalar reused across rows
  passes; `githug:`; eight field-type cases; null optional fields read as empty and values are
  trimmed; `|`, newline and CR in any string field including `playbook`; seven bad `github` shapes
  and one good one with every allowed character; no-github-no-note and a blank note; the pair
  declared twice names the first row; ten `installed` cases and the three tracked kinds; zero
  tracked pins across one and two rows.
- V17-V21: seven escaping or non-canonical paths refused **without opening the file**
  (`fs.ValidPath` plus `.`); missing, directory, symlink (fake and real, pointing outside the tree)
  are findings and an unreadable target is exit 2; the var read in the consumer's line form
  (`check_pins.py:98-108`, `^\s*<var>\s*:(.*)$` with `re.escape`, first match, strip blanks and
  quotes): `declaration()` (`resolve.go:114-127`) is the same function — seven non-declarations,
  six declarations, five metacharacter vars taken literally (B4), five empty values (B5), the first
  line wins; `.yml`/`.yaml` targets must also hold the var as a parsed scalar key (five text-only
  forms refused, five real positions accepted, B6), an unparseable `.yml` is exit 2 and a `.pin`
  line-form file is not parsed as YAML; a file named by three rows is read once (`countingFS`).
- V22-V25: `coverage()` unit-tested and reached through the tool; two findings sorted by line
  under the header; a problem (exit 2) still prints the findings; the clean line byte for byte;
  the `qa-gates.tsv:124` reader `detail:vars/[a-z-]+[.]yml: VERSION-PINS-OK .+` still matches it.
- Wiring: `version-pins` registered as a whole gate needing bash and go after `stt-rules`; its
  script runs from the scripts directory; the manifest and `tuwunel.pin` count as covered;
  playbooks at any depth (`*` crosses `/`, `discover.go:23`) and a deleted playbook route to it;
  `tasks/`, `helpers/`, a plan file do not; the wrapper is in `perfile-gate` and not picked as a
  suite; `TestEveryRealPinnedFileRunsTheVersionPinsCheck` reads the live manifest so a new pin is
  routed without a test change (floor of 10 inputs guards an empty read);
  `scripts/test-qa-changed.bash:121-122` updated (`check version-pins`, no suite); 20 golden
  scenarios re-pinned as B45 with one reason; the allowlist row removed legitimately (the wrapper
  is `go-tool.bash` plus one call). `go test` (3 packages) and `golangci-lint` clean.

---

## Branch `go-port/qa-vmtest-manifest` (`58a580c6`)

### VM-1 — LOW — every stderr line of an invalid run becomes a finding

`src-go/qa/vmtestmanifest/validator.go:65-76`: on exit 1 with the `INVALID` marker, each non-empty
stderr line, `ERROR: ` prefix stripped, is a finding. A Python `DeprecationWarning` or a
`SyntaxWarning` emitted on import lands in the report as
`ERROR: vars/vm-test-scenarios.yml: <path>: DeprecationWarning: ...`. The exit is 1 either way (the
validator said INVALID), so no false pass; only a wrong diagnosis beside the right one. Keep the
lines that start `ERROR: ` as findings and print the rest after them as the validator's stderr, or
state in V6 that the validator's whole stderr is the finding list.

### VM-2 — LOW — the "outside the repository root" branch cannot fire

`guest.go:140-143`: `filepath.Rel(g.root, abs)` returns `../x` with no error for a path outside the
root, so the error "discovery named %s outside the repository root" is unreachable; such a path
would be dropped by `dir != guestDir` instead, silently. Discovery is given the root and never
lists outside it, so nothing is wrong today. Either test `strings.HasPrefix(rel, "../")` or drop the
branch and say why.

### VM-3 — LOW — V9 says `readonly`/`declare`/`typeset -r`; the code takes any declaration builtin

`guest.go:212-235`: `*syntax.DeclClause` covers `export PLANNED=13`, `local PLANNED=13` (parses at
top level) and `declare -i PLANNED=13` as well as the three the spec names. Harmless (they are all
the script's declaration) but the spec reads as a closed list. Reword V9 to "a declaration builtin
(`readonly`, `declare`, `typeset`, `export`, `local`)", or restrict `plannedAssigns` to the named
ones; the test `TestV9PlannedIsReadFromTheSyntaxTreeNotTheText` should then carry the case that
decides it.

### VM-4 — INFO — design judgement: keeping the Python validator as the one definition is right

`helpers/vmtest/scenarios.py` is what Ansible runs on the host over the JSON it renders from the
same manifest: the definition of "valid" there is the definition of what gets deployed, and a Go
copy would be the second definition the plan's method forbids. The Go owns exactly what the
Python cannot see after the conversion (B1: a repeated key, a second document, a non-string key,
`.nan`) and what the Python never did (the guest-script cross-checks). The B2 controls became Go
tests against the real validator (`cmd/qa-vmtest-manifest/main_test.go:99-135`: an empty manifest
rejected by the real Python, one more `planned` than the checker declares, a fixture without its
reboot), so the proof is on the production path. This is the shape VP-2 contrasts with.

### VM-5 — INFO — `proc.Cmd.Stdin`

`src-go/qa/internal/proc/proc.go:26-29,69`: one field, set on `exec.Cmd` unconditionally (nil keeps
the previous behaviour of no stdin), tested with `cat` (`proc_test.go:27-38`). The child is still
started in its own process group and killed as a group on cancel (`:73-74`), so a validator that
blocks on stdin is covered by the timeout; a child that exits without reading gets the copy
goroutine an `EPIPE`, which `exec` handles. The fake runner in `vmtestmanifest/helpers_test.go:65-75`
drains `Stdin` so the test can assert what was fed (`TestV6TheValidatorIsRunInTheRootOnTheManifestAsJSON`:
the whole manifest, `null` kept as `null`). No concern.

### VM-6 — INFO — what held up

- V1-V2: help, unknown option, positional, missing values; `--root`, parent of `--scripts`, cwd; a
  symlinked root resolved and never printed; unresolvable root; stdout write failure; three exact
  lines and empty stderr on a pass; nothing on stdout on exit 1 or 2; every finding reported, in
  scenario-id order.
- V3-V5: missing and non-regular inputs exit 2 naming them; the manifest and the version file
  parsed strictly (empty, comments only, second document, non-mapping top, non-string key,
  duplicate key, `.nan`); `fedora_version` as a string, bool, float, zero, negative or absent is
  exit 2.
- V6-V7: exact argv, run in the root, `PYTHONDONTWRITEBYTECODE=1`, the JSON fed; invalid is exit 1
  with the validator's reason and the cross-checks do not run; ten "did not judge" outcomes exit
  2 (cannot start, timeout, leftover, traceback, exit 2, killed, exit 0 without a marker, nothing,
  INVALID with exit 0, OK with exit 1); the checkout path scrubbed out of a traceback; eight
  malformed OK lines refused and the counts passed through.
- V8-V12: only a published checker counts and a fresh unignored one does (real temporary git
  repositories); discovery failure exit 2; the scenario's own checker over the profile's;
  `PLANNED` from the syntax tree (comment, string, heredoc, function, prefix assignment,
  `$((...))`, quoted, second assignment); an unparseable checker exit 2; `planned: null` not
  checked and zero runnable exit 2 (B7); fixtures without a declared reboot, of no scenario, and
  none at all; orphan acceptance scripts; other files in the directory ignored.
- V13-V16: git failing under discovery exit 2; a shape the validator accepted but the cross-checks
  cannot read exit 2 naming the scenario; no checkout path on a failing run.
- Wiring: `vmtest-manifest` registered as a whole gate needing bash, go and python3 after
  `stt-rules`; the manifest covered; the version file, the three validator modules, the guest
  acceptance and prepare scripts, the wrapper, package, command, shared discovery, trees table
  and `go-tool.bash` all route to it, and a deletion of each owes it; `vars/other.yml`,
  `helpers/vmtest/freshness.py`, the `vmtest` CLI, `guest-cleanup.bash` and the docs do not; the
  wrapper is in `perfile-gate`; `TestTheRealRoutingSendsEveryVmtestManifestInputToTheCheck`
  reads every real guest script from git (floor of 4); `qa-gates.tsv:122` unchanged and its three
  `detail:` readers match the three lines; no golden scenario changes; `scenarios.py:3-8`
  docstring updated; `CLAUDE/QA.md:158` row rewritten accurately; the spec gives the 1/2 split
  as a contract, not an abort story (TC-2 class avoided). `go test` (4 packages) and
  `golangci-lint` clean.

---

## Branch `go-port/qa-helper-tests` (`f9df03a8`)

### HT-1 — INFO — judgement: no `qa-changed` check is right (B9)

The question put: does helper-tests need a `qa-changed` check? No. The whole gate is about 4.5
minutes, which is the wrong order of magnitude for a per-commit check, and every input of the gate
already reaches a check that exercises the code path in question before the commit:

- the Go package and command route to the `go` check, which runs `qa-go.bash` over `src-go/qa`,
  and that includes `cmd/qa-helper-tests/main_test.go` — the real git index, the real discovery and
  the real `unittest_counts.py` on small trees — so a defect in the gate's own logic is met before
  the push, on the production path minus the wrapper;
- the wrapper routes to the shell checks (`bash-syntax`, `shellcheck`, `bash-limits`) and is one
  `go-tool.bash` call;
- `helpers/qa_environment/unittest_counts.py` routes to `ruff` and to the `unit-tests` check over
  `tests/helpers/qa_environment/test_unittest_counts.py`, which exists in the tree;
- a changed helper routes to the `unit-tests` check over its mirrored module, which is the only
  part of the suite that change can turn red.

Pinned by `TestTheHelperTestsGateInputsRouteToTheChecksThatCoverThem` (`classify_test.go:641-671`),
`TestTheRealRoutingCoversEveryHelperTestsGateInput` (`realtree_test.go:687-700`) against the real
table, and `TestTheHelperTestsGateIsNotAChangedCheck` (`checks_test.go:618-624`), which also
records that adding one is a decision with routing rows, a deviation and a B-row, not a side
effect. What `qa-changed` cannot see is a module-level collision between two helpers that each
pass alone (an import-order or shared-fixture defect); that has always been `qa-all`'s, and the
spec's B9 says so. No B-row needed, none written, and `deviations.json` is untouched: correct.

### HT-2 — LOW — `HelperCounts` and `HelperRun` are two readers of one file

`src-go/qa/gates/readers.go:136-168`: both call `readCounts` (one parse, good), but `HelperCounts`
formats the digit strings and `HelperRun` converts them, so the integer refusal ("too large for an
int") exists in one and not the other, and the two tests have to prove they refuse the same files
(`helperrun_test.go:19-44` does). Building `HelperCounts` on `HelperRun` would give one reader and
one refusal set; the one byte that could differ is a leading zero (`tests=007`), which the runner
never writes (`unittest_counts.py:265-268` formats ints). The spec's "byte for byte" promise for
`HelperCounts` would then be a statement about the runner's output, which is where it belongs.
Fine as it is; note it for the next touch of `readers.go`.

### HT-3 — LOW — the counts of a status-1 run that disagree are reported as "could not run"

`helpertests/run.go:342-367` (H12): a runner that exits 1 and writes `modules=` short is exit 2, and
`qa-all` will say the gate "could not run" rather than "the helper unit suite failed". The spec
chose this ("a runner that reports failure and no counts is a runner that did not finish") and the
gate fails either way, so nothing is softened. The cost is only to the human reading the stage
line, who is told the wrong thing first. If it is cheap, print the H13 failure line before the
H12 refusal when `status == 1`, so both facts are on stderr.

### HT-4 — INFO — what held up

- H1-H3: help, unknown argument with usage, four options and their missing, empty, `-`-prefixed
  and repeated values (B8); the root rules; unresolvable root; a green run and a red run both with
  empty stdout; a stdout or stderr write failure exit 2; the help's stdout failure named.
  `qa-all`'s own token is `qa-all-<pid>-<ns>` (`cmd/qa-all/main.go:43-45`) and its counts path is
  absolute, so neither trips the `-`-prefix refusal.
- H4-H7: tests found at any depth by name; a directory named like a test entered and not run;
  none found exit 2 (B1); a missing `tests/helpers` and an unreadable directory exit 2 naming it; a
  symlink to a regular file followed and a dangling one exit 2 (B2); a linked directory not
  entered; a tracked test the walk missed exit 2 naming each before anything runs; a discovery
  failure carries its message; an untracked test runs and is named after the `COVERAGE:` line; a
  clean tree reports full coverage; a dash, dot or space in a segment exit 2 naming the path (B5).
- H8-H13: the exact argv including the `env -u ... GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1`
  prefix, in the root, with modules in path order; the token forwarded only when given; a relative
  counts file made absolute against the caller's directory; the file emptied first so a stale one
  is never trusted (B6) and a runner that wrote nothing is exit 2 (B4); a private file used and
  removed; a file that cannot be emptied exit 2; the runner's streams reach the caller's live;
  only 0 and 1 are verdicts (B3; 127, 2, a signal, 130 refused); the run bounded (B7, process
  group killed by `proc.OS`); the counts must agree (zero tests, wrong module count, wrong
  tracked count, wrong token); the closing lines exact. On a real tree: a passing run whose
  counts file `gates.HelperCounts` then reads as `qa-all` would; a failing run exit 1 with the
  traceback; an untracked test named and a deleted tracked one not run; no tests exit 2; an
  unimportable test is a failure not a skip; a missing runner exit 2; the machine's git
  configuration provably absent inside a test; a symlinked root.
- G1: `HelperRun` returns the four integers and refuses the eight shapes `HelperCounts` refuses
  plus a number no `int` holds; no token wanted means no token accepted.
- Wiring: `qa-gates.tsv:42` (kind `helpers`) unchanged; `qa-routing.tsv` unchanged; the allowlist
  row removed legitimately; `CLAUDE/QA.md:81` row and `:398-406` prose updated to the Go facts;
  the `src-go/CLAUDE.md` paragraph; the journal records red (a compile failure against no code,
  which is a legitimate red), green, an eight-break mutation check and the live tree. `go test`
  (4 packages) and `golangci-lint` clean.

---

## Across the three branches

### X-1 — LOW — the command-line scaffolding is now copied into three more packages

`versionpins/run.go:55-103`, `vmtestmanifest/run.go:77-125` and `helpertests/run.go:80-142` each
carry `options`, `unknownArgError`, `parseArgs` (the `--root`/`--scripts` grammar with the
`-`-prefixed-value refusal), `emitter` and the usage-splitting loop, byte for byte or nearly
(helpertests adds two options and the repeat check). `rootpath` was extracted for exactly this
reason one review ago; the parser and the emitter are the next candidates (`qa/internal/gatecli`:
`ParseRootArgs(args, extra map[string]*string)` and `Emitter`). Not this merge's job, but the
fourth copy is the one that drifts.

### X-2 — INFO — B-rows do not collide; the merges will conflict textually

B45 (version-pins) and B46 (vmtest-manifest) are distinct and each branch keys its deviations
to its own number; helper-tests adds no B-row and changes no golden scenario, correctly. Both
numbers are free on the integration branch (its last row is B44). The two rows are inserted at
the same line of `SPEC-qa-changed.md` (`:539`), the vmtest row with unpadded cells the formatter
will re-align. Expect conflicts on the second and third merge in: `changed/checks.go` (both gates
insert after `stt-rules`; keep both, version-pins then vmtest-manifest, before `deployed-drift`),
the three `want` lists in `changed/checks_test.go`, the `perfile-gate` line of `qa-routing.tsv`
(append both names), `scripts/bash-limits.allowlist` (three adjacent rows removed), the
`JOURNAL/00192-Journal-26-10-15.md` append point (all three), the `CLAUDE/QA.md` gate table (three
rows and a formatter re-alignment on vmtest), and `src-go/CLAUDE.md` (three paragraphs at three
different anchors, so clean). `testdata/deviations.json` is changed only by version-pins (20 keys
gain B45); after all three merge, run `go test ./changed -update-deviations` and confirm it
rewrites nothing further, since neither vmtest nor helper-tests touches a golden scenario.

### X-3 — INFO — the method was followed

Each branch: a spec with numbered expectations and a departure table read against the base
bash; tests cite the numbers; the journal records red (28 failing tests against a stub; 34 top-level
tests with a corrected count; a compile failure against no code), green, and the live tree run
with both tools and the identical result; the exit-2 rationale is stated as "cannot judge", not
the abort story TC-2 corrected. Nothing records the bash's output as an oracle. The three
run-time control patterns of the bash (version-pins: four; vmtest: three) are Go tests, two of
vmtest's against the real validator.

## Suggested order

1. VP-1 (one sentence) and VP-2 (one test, two spec sentences, two stale comments) before the
   version-pins merge.
2. Merge vmtest-manifest and helper-tests as they are; VM-1, VM-2, VM-3, HT-2, HT-3 and VP-3 when
   the files are next touched.
3. X-1 as its own small change after the merges, over all the ports at once.
