# Adversarial review: `go-port/qa-plan-script-logging`, `go-port/qa-toolchain`, `go-port/qa-speech-to-text-rules`

Three Plan 00192 ports, each one commit on `origin/integration/261014` (`61bbc0b3`), reviewed
against their specs with the method of `src-go/CLAUDE.md` "How a port is built": the spec read
first, then the Go and the base-branch bash side by side, then the tests against the spec's
numbered expectations, then the wiring (`qa-routing.tsv`, `checks.go`, `qa-gates.tsv`,
deviations), the docs and the journal. Each branch was checked out in a detached scratch worktree
(removed afterwards); `go test -count=1` over the new packages and `qa/changed`, and
`golangci-lint run` over the same, were run on each; each Go gate was run on its live tree through
its wrapper. `qa-all.bash` was not run. The defect classes of the earlier reviews
(`261014-fable-review-*.md`: vacuous self-tests, unread or unparsed files passing, lost exit
codes, scope drift from "what git would publish", routing holes, path leaks, stale docs) were
hunted on each branch.

Severity: **BLOCK** (do not merge), **FIX** (fix before or with the merge), **LOW** (fix when
touched), **INFO** (recorded, no action owed).

## Verdict

- **qa-plan-script-logging**: merge; one FIX (the plan record), two LOWs. Tests, lint and the
  live tree are green (`PLAN-SCRIPT-LOGGING-OK: 82 plan script(s) examined, no offences`, the
  same count as the bash, which held because the 111 active plan folders hold no `.sh` or
  shebang-only script the bash would have missed).
- **qa-toolchain**: merge; two FIXes (the plan record, stale exit-2 rationale across spec,
  wrapper and package doc), two LOWs. The `.qa-versions` parser is tight (section T-5 below
  lists what was tried). Live tree: `TOOLCHAIN-OK 5 tool(s) match .qa-versions`.
- **qa-speech-to-text-rules**: merge after three FIXes: a `todoruleid:` annotation satisfies
  the S7 proof (a real false pass of the self-test guard), the per-file check must apply the
  push gate's exclusion trees (judged below: the inconsistency must go, and the trees table is
  where the fix belongs), and the plan record. Three LOWs on duplicated logic. Live tree:
  `passed: 337 failed: 0`, as the bash.

No BLOCK on any branch. No install-specific name in any diff (grepped for home paths, the
container checkout path, user names, hostnames, non-example mail domains).

---

## Branch `go-port/qa-plan-script-logging` (`50859dfa`)

### PSL-1 — FIX — PLAN.md Task 3.3 does not record the port

`CLAUDE/Plan/00192-.../PLAN.md:92-275` (Task 3.3) enumerates every QA port landed so far
(`qa-changed`, `qa-all`, `qa-ansible`, `qa-nokill-containerwatch`, `qa-patterns`, `qa-python`,
`qa-ansible-syntax`, `qa-bash`, `qa-discovery`, `qa-deployed-drift`, `qa-js`,
`qa-ansible-pause`, `qa-gh-scopes`, `qa-ready-wait-rules`, each with branch, spec, package and
state). This branch adds the journal entry and the spec but no PLAN.md sentence, so the plan's
own record of Task 3.3 lags the work (CLAUDE.md "Plan Commit Rule"). Same on the other two
branches (TC-1, STT-1). Fix: one sentence per port in Task 3.3 in the shape of the
`qa-ready-wait-rules` entry, in the merge commit or on the branch.

### PSL-2 — LOW — a symlink named `logs` is neither reported nor refused

`src-go/qa/planscriptlogging/scan.go:85-97`: `walkFolder` reports `logs` only through
`d.IsDir()`, and a symlink is neither a directory nor a regular file, so
`CLAUDE/Plan/00130-x/logs -> ../../../untracked/anything` is silently nothing. L5 ("a symlink
below a plan folder is left out") wins over L11 ("a directory named `logs` at any depth,
whether or not git would publish it"), and the spec does not say which. This is the "skipped
without a word" class of the qa-ansible review's F2: the link is exactly how a plan would keep a
local run-log directory while hiding it from the gate. Fix: a symlink whose name is `logs` is the
L11 offence (it is a plan-local run-log location whatever it points at), or exit 2 naming it; one
test in `run_test.go` beside `TestL11ALogsDirectoryAtAnyDepthIsAnOffence` with
`fstest.MapFile{Mode: fs.ModeSymlink}`.

### PSL-3 — LOW — the routing row also fires for `Completed/` and the plan tooling

`scripts/qa-routing.tsv:221`: `CLAUDE/Plan/*.bash,CLAUDE/Plan/*.sh,CLAUDE/Plan/*/logs/*`. The
matcher's `*` crosses `/` (`discover.go:24-25`), so a change to
`CLAUDE/Plan/Completed/00099-x/run.bash`, `CLAUDE/Plan/_planlib.inc.bash` or
`CLAUDE/Plan/mkplan.bash` runs the whole plan-script-logging gate, which by L4 never reads them.
Harmless (the gate is a file-system walk of about a hundred folders) but the row comment says
"the shell scripts and the logs/ directories of the active plans", and
`TestAPlanScriptLoggingEditRunsTheGate` (`classify_test.go:299-330`) says in its comment that
"a Completed plan's non-shell file" does not route, then tests only an active plan's `PLAN.md`.
Either state the over-match in the row comment and the test, or add a test that a
`Completed/.../x.bash` does route (it does today) so the behaviour is pinned rather than
implied.

### PSL-4 — INFO — `command exec` / `builtin exec` are not the offence

`scan.go:171-174`: `isBareExec` needs `Args[0] == "exec"` and one argument. `command exec > >(tee x)`
and `builtin exec > >(tee x)` are the same un-waitable redirection and pass. Nobody writes
them; recording it so the spec's section 4 can list it as a stated limit rather than a surprise.

### PSL-5 — INFO — what held up

- L1-L3: `--root`/`--scripts`/neither, `-h`, unknown argument with usage, missing value, a
  stray positional (exit 2), unresolvable root (`rootpath.RootVia`), `/dev/full`-class stdout
  failure (`failWriter`, `ERROR: cannot write to stdout:`), no checkout path in output through a
  symlinked root (`TestASymlinkedRootAndASymlinkedPlanFolder`, real file system).
- L4-L5: `Completed/`, `_planlib.inc.bash`, `README.md`, `abc-notaplan`, `1abc-nodigitsonly`
  not examined (B4 fixed as stated); a symlinked plan folder exit 2 (fake and real); recursive
  walk; a link below a folder left out; unreadable file and unreadable folder exit 2 with the
  rest judged first (`unreadableFS`).
- L6-L8: `.bash`, `.sh`, bash and sh shebangs examined; python shebang, `.txt`, `.md`,
  `#!/bin/bash -c` not; zero scripts exit 2 (the count is in the OK line); an unparseable
  script exit 2 naming it (B3).
- L9-L10: fifteen offence forms (classic, append, `2>`, `&>`, high fd, `{fd}>`, `cat`, in a
  function, `if`, subshell, brace group, prefix assignment, two redirections, line
  continuation, case arm) each reported at the statement's first line; eleven non-offences
  (comment, end-of-line comment, both quote kinds, heredoc body, `| tee`, plain file, input
  substitution, `exec cmd > >(...)`, `foo > >(...)`, the library call, `exec "$0"`). The bash
  matched one line of text and missed `cat`, `2>`, `&>` and continuations (B1): verified
  against the base-branch awk.
- L11-L14: `logs` at any depth, not `blogs`/`Logs`, not in `Completed/`; findings sorted by
  path then line, directories after scripts, the remedy block byte for byte, nothing on stdout
  on failure; the OK line exact; the converted-script comment clean. The bash's two run-time
  controls are `TestL9`/`TestL10`/`TestL14` (B6).
- Wiring: `qa-gates.tsv:123` unchanged (`detail:[0-9]+ plan script[(]s[)] examined, no offences`
  still matches the stdout line); the gate is a `gate` row, so its new exit 2 (B3, B5, B7) fails
  the gate without aborting `qa-all`; `checks.go` registers `plan-script-logging` as a whole
  gate needing bash and go; the `also` rows and `TestEveryRealActivePlanScriptRunsThePlanScriptLoggingCheck`
  route every real active plan script and the gate's own files; the wrapper is off
  `bash-limits.allowlist` legitimately (four statements over `go-tool.bash`). SPEC-qa-changed
  B42 states the shebang-only limit honestly.
- `go test` (3 packages) and `golangci-lint` clean.

---

## Branch `go-port/qa-toolchain` (`5fd8f596`)

### TC-1 — FIX — PLAN.md Task 3.3 does not record the port

As PSL-1.

### TC-2 — FIX — the exit-2 rationale is stale in three places and self-contradictory in one

- `SPEC-qa-toolchain.md:19-21`: "exit 2 makes `qa-all` abort the whole run, which is how one
  drifted ruff once took 44 unrelated gates offline".
- `scripts/qa-toolchain.bash:14-15`: "1 a tool is absent, unreadable or drifted (never 2: exit 2
  aborts the whole qa-all run), 2 the pins could not be read or the tool could not run" — the
  same sentence says never 2 and then names two exit-2 cases.
- `src-go/qa/toolchain/run.go:4-5` (package doc): "exit 2 makes qa-all abort the whole run".

Under the Go `qa-all` the toolchain row is kind `gate` (`qa-gates.tsv:26`), and a hard gate's
exit 1 and exit 2 both fail that gate and let the rest run (SPEC-qa-all K2, confirmed in
`261014-fable-review-qa-all.md`); only a `stage` row aborts on exit 2. The 1/2 split is still
right — `qa-changed` reports a gate's exit 2 as "could not run" and exits 2 itself
(`changed/run.go:265,282`), and the semantics "1 = the machine is wrong, 2 = the gate's inputs
are wrong" is the contract the tests pin — but the reason given for it is no longer true, and a
reader of the wrapper is told two opposite things. Fix: state the split as a contract (exit 2 is
"cannot judge": pins, go.mod, usage, a failed write; it is never used for tool drift so that
`qa-changed` and `qa-all` report drift as a verdict, not as a broken gate), and drop the abort
story from all three places. CLAUDE/QA.md:71 already has the right wording.

### TC-3 — LOW — an invalid Go floor is reported as drift (exit 1), not as a bad input (exit 2)

`versions.go:140-143`: `goFloor` fails the assertion (`a.fail`, so exit 1 and the remedy block
"bring the machine into line") when `version.IsValid("go"+floor)` is false. T6 says a go.mod the
gate cannot use is exit 2, and the machine is not what is wrong. `modfile.ParseLax` rejects
`go one.two` today (`TestT6TheGoFloorComesFromGoMod/bad version` passes through the parse
error), so the branch is unreachable in practice, but it is the wrong classification waiting
for a floor string modfile accepts and `go/version` does not. Fix: validate in `readGoFloor`
(`pins.go:112-125`) and return the error there; drop the branch from `goFloor`.

### TC-4 — LOW — docs drift left in QA.md's own toolchain section

`CLAUDE/QA.md:954-955` ("The QA Toolchain Is Pinned"): "every tool a gate runs — ruff
(`qa-python`), semgrep (`qa-patterns`), shellcheck (`qa-bash`)" omits golangci-lint
(`qa-go`), which `.qa-versions` has pinned since the Go gate landed and which this port asserts.
The table row at `QA.md:71` was corrected on this branch; the prose that owns the subject was
not. One phrase.

### TC-5 — INFO — the `.qa-versions` parse was attacked and held

What `readPins`/`assignment`/`plainAssign` (`pins.go:21-98`) were tried against, by reading the
`mvdan.cc/sh` node shapes and the tests (`pins_test.go:58-81` covers the first block):

- quoted (`"0.16.8"`, `'0.16.8'`, `$'1'`): `Parts[0]` is `DblQuoted`/`SglQuoted`, not `Lit` →
  exit 2;
- `$X`, `${X}`, `$(cmd)`, backticks, `a$X`: `ParamExp`/`CmdSubst` or two parts → exit 2;
- `export`, `local`, `declare`, any command word, `KEY=1 cmd`: `Args` non-empty → exit 2;
- `+=`, arrays, `KEY[0]=`, two assignments on a line, redirections, `&`, `!`, `coproc`,
  compound commands, a function: refused by `plainAssign`;
- `~`, globs, braces, backslash escapes, `#` glued to the value, `;`-joined extra words, a
  CRLF `\r`, `=` in the value, a `\`-newline continuation that glues the next key in: the `Lit`
  value then holds a character outside `versionChars` → exit 2;
- `KEY=` (empty), a key twice, a key outside the four, an empty or comment-only file, a parse
  error: exit 2 with `.qa-versions:<line>` (T5, T7 tests).

Nothing bash would expand or execute is read as a version. The one grammar the Go accepts that
the bash did not need — a trailing `# comment` after the value — is correct shell. The CI step
and the Dockerfile still `source` the file (`.github/workflows/qa.yml:66`); the Go grammar is a
strict subset, so any file they accept and the Go refuses fails loudly here first.

### TC-6 — INFO — two assertions are unreachable through the wrapper

`scripts/lib/go-tool.bash:13-16` exits 2 when `go` is absent, before the binary exists. So
T9's `✗ toolchain: go is not installed (need >= ...)` and the wrapper's "exit 1 for an absent
go" are reachable only by running the binary directly; through `qa-all` an absent go is the
wrapper's exit 2, a failed `toolchain` gate (not an abort, TC-2), and every other Go-built gate
fails the same way. True of every Go port and acceptable; the spec could say so in section 4.

### TC-7 — INFO — what held up

- T1-T2: help, unknown option, positional (refused: the gate takes no PATH), missing values,
  symlinked root resolved and never printed (T16), unresolvable root, stdout write failure.
- T8-T12: assertion order and all mismatches reported (`TestT8`); absent tool is a mismatch
  (`TestT9`); each tool's own `--version` shape (`TestT10`, and `TestRealToolsAreReadFromPathAndMatch`
  with fake executables on a private PATH plus the real `go`); non-zero exit after printing the
  pin is not a match (B4, fake and real); timeout, cannot start, no version text, shellcheck
  without a `version:` line → unreadable with the stderr and the reason (B8); every run bounded
  and in the root (`deadlineSpy`); exact string equality (`0.16.80`, `v0.16.8`, `0.16`,
  `0.16.8.1` refused); go floor by `go/version` (`1.26.2`, `1.26`, `1.27.0`, a `X:` experiment
  suffix pass; `1.25.9` older; `devel garbage`, empty, non-zero, timeout, start error
  unreadable); a three-component floor; a `go` line inside a `require` block is not the
  directive (B6 fixed as stated).
- T13-T15: stderr empty on a pass; the remedy block after a blank line; one stdout line.
- Wiring: `qa-gates.tsv:26` unchanged and matched; `checks.go` makes `toolchain` a whole gate
  needing bash and go (a deleted `.qa-versions` owes it: `TestDeletedFilesOweOnlyTheWholeGates`);
  the `post any:@sh,any:@py` row kept; `also` rows for the pins (covered), `src-go/go.mod`,
  wrapper, package, command and `go-tool.bash`, proven on the real table
  (`TestTheRealRoutingSendsEveryToolchainInputToTheToolchainCheck`); two golden scenarios
  re-pinned as B43 with the reason; `go.mod` promotes `x/mod` to direct (it was already
  indirect, `go.sum` unchanged). The remedy text names `play-python.yml` (installs
  golangci-lint, imports `play-golang.yml`) and `ccy --rebuild`, both verified against the play.
- `go test` (3 packages) and `golangci-lint` clean; `TestT4` iterates a map and has a dead
  `_ = name` — `t.Run(name, ...)` as `TestT5` does would name the failing case.

---

## Branch `go-port/qa-speech-to-text-rules` (`4e210a15`)

### STT-1 — FIX — PLAN.md Task 3.3 does not record the port

As PSL-1.

### STT-2 — FIX — a `todoruleid:` annotation satisfies the S7 proof, so the self-test can prove nothing

`src-go/qa/sttrules/rules.go:177`: `annotationRe = ruleid:\s*([\w.-]+...)` has no left
boundary, so it matches inside `todoruleid:`. semgrep's test grammar has four annotations:
`ruleid:` (must match), `ok:` (must not), `todoruleid:` (should match but is known not to, so a
non-match is **not** a failure) and `todook:`. A fixture whose only mark for a rule is
`// todoruleid: dropdown-label-carries-explanation` passes `semgrep --test` whether or not the
rule matches, and passes `proveRules` (`rules.go:201-206`) because `annotated` counts it as a
`ruleid:` line. That is the B2 class ("a fixture with no annotation passed its self-test")
re-admitted through the back door: the rule is certified by a line that certifies nothing.
Scenario: a rule is loosened until it stops matching its fixture line; the author, following
semgrep's docs, downgrades the annotation to `todoruleid:` to get the suite green; the gate
reports a clean repository for a rule that matches nothing. Fix: anchor the regex
(`(?:^|[^\w])ruleid:` — `\b` alone does not help because `todoruleid` ends in a word
character run), and add a case to `TestAFixtureWithoutAnnotationsOrMissingARuleProvesNothingAndIsExitTwo`
(`run_test.go:164-181`) with `// todoruleid: <id>` expecting exit 2.

### STT-3 — FIX — the per-file check judges files the push gate excludes; it must apply the same trees

The facts, then the judgement.

- Default scope (`scope.go:27-34`, `excluded`): the `js` and `python` discovery lists less
  `.semgrep/`, `.claude/hooks-daemon/`, `.claude/skills/` and any `node_modules` segment.
- `scripts/qa-trees.tsv`: the `shell` list (which also governs JavaScript discovery) already
  excludes `.semgrep`, `.claude/hooks-daemon`, `.claude/skills`, `node_modules`; the `python`
  list excludes `.claude/hooks-daemon` and `node_modules` but **not** `.claude/skills` and
  **not** `.semgrep`. So for Python the gate's exclusion list is wider than the discovery it
  reuses, and `excluded()` is where the difference lives.
- `scripts/qa-routing.tsv:107` (`first @py ... stt-rules-files covered,tree:python`): a changed
  `.py` is handed to `stt-rules-files` unless the `python` tree list drops it. `.semgrep/*.py` is
  caught earlier by the `first .semgrep/*` rows (`:102-104`), so the fixture never reaches the
  per-file check. **`.claude/skills/**/*.py` is not caught by anything**: it is routed to
  `stt-rules-files`, the gate judges it because it was named (S10, `TestANamedFileInAnExcludedTreeIsJudgedBecauseItWasNamed`
  pins exactly this), and the push gate leaves it out.
- `.claude/skills/` is tracked (31 files on the branch) and not ignored; it is a daemon-deployed
  payload ("installed skill payloads", the trees table's own words) and holds no `.py` or `.js`
  today, so the hole is latent, not live.

The spec records this as a decision (section 4: "a vendored edit can fail the commit check
where the push gate would not"). My judgement is that it must not stand, for three reasons.
First, it inverts the one relation `qa-changed` is built on: the commit check is a subset of the
push gate run over the diff (SPEC-qa-changed: a per-file check "gets the verdict the full gate
gives", B36, B37, B40). A commit check that is stricter than the push gate on a path the
repository has declared not its code gives the author one remedy, editing vendored code, or
skipping QA. Second, the exclusion exists because those trees are daemon-owned and rewritten on
upgrade: the next hooks-daemon release that ships a Python skill helper makes every commit that
touches that payload (which upgrades do, in bulk) fail on a rule written for this repository's
own panel code. Third, two sources of truth for "what the speech-to-text gate does not judge" —
the trees table for JavaScript, `excluded()` for Python — is the drift the trees table's header
comment was written to prevent ("a hand edit to one reader would be invisible to the others, so
there is only this file").

Fix, in the one place the other gates keep it: add `python<TAB>.claude/skills` to
`scripts/qa-trees.tsv` (the shell list already has it, for the same reason), which makes the
`@py` row skip it as an excluded tree in `qa-changed`, makes the `python` discovery list drop it
for `qa-python` and `qa-ready-wait-rules` too (consistent with the `shell` list and with the
file's stated reason), and lets `excluded()` shrink to `.semgrep/` alone — the one exclusion
that is this gate's own (its fixtures), with `.claude/hooks-daemon` and `node_modules` deleted
from it because discovery already drops them for both lists (two dead entries that misstate
where the exclusions live). Then: a `classify_test.go` case that `.claude/skills/s/run.py` is
reported `skipped (excluded tree)` and not handed to `stt-rules-files`; the `TestANamedFileInAnExcludedTreeIsJudgedBecauseItWasNamed`
case keeps `.semgrep/unrelated.py` (a named fixture is still judged, which is right for a
human running the tool by hand) and loses the skills path; spec section 4 and B10 reworded. The
alternative of filtering in the tool's named mode was rejected: a named run whose every file is
excluded would have to print `passed: 0 failed: 0`, the vacuous pass this whole plan exists to
remove, or exit 2 and abort a commit check over a vendored edit.

### STT-4 — LOW — duplicated logic that the shared packages already own

- `scope.go:141` reimplements the Python shebang test as `strings.HasPrefix(first, "#!") && strings.Contains(first, "python")`.
  `qa/discovery/shebang.go:50` has `pythonShebang` (unexported), which parses the interpreter
  (`#!/usr/bin/env -S python3`, `python3.12`) and requires the *name* to start with `python`;
  the copy accepts `#!/usr/bin/pythonista` or `#!/bin/sh -c 'exec python'` and so can hand a
  non-Python file to the Python rules. The gate already imports `discovery` for the lists and
  `ShellShebang` is exported for exactly this reuse (the plan-script-logging port uses it);
  export `PythonShebang` and call it.
- `scan.go:26-36` is the fourth copy of the semgrep JSON envelope (`Results`, `Errors{path,message}`,
  `Paths.Scanned`) after `patterns/semgrep.go:80-85`, `readywait/scan.go:201-206`,
  `ghscopes/scan.go:94`, and `judge` (`scan.go:113-143`) repeats readywait's and patterns'
  "errors are exit 2 / no `paths.scanned` / handed file not scanned" verbatim, message included.
  `qa/internal/semgrepreport` was created (review F14) to be the one home; it holds only
  `Result`. Move the envelope and the coverage judgement there; four gates then cannot disagree
  on what "semgrep read it" means.
- `changed/checks.go:157-160`: `runSttRulesFiles` is `runReadyWaitFiles` with another script
  name; the third per-file gate will copy it again. A `s.scriptOver(ctx, name, items)` beside
  `s.script` removes the pair.

### STT-5 — LOW — `scanned` counts files handed, not files read

`scan.go:78`: `total.scanned += len(files)`. S17 says `<scanned>` "is the number of files
semgrep read across both languages". The two are equal only because S15 refuses a run in which
semgrep read fewer; if semgrep ever lists more (it does not for named targets), the count is
wrong without a test noticing. Either count `len(paths.scanned)` after de-duplication, or reword
S17 to "files handed over, each proven read". The bash counted `paths.scanned`.

### STT-6 — LOW — a self-test semgrep cannot even run is exit 1, "failed its fixture"

`rules.go:215-217`: any non-zero `semgrep --test` is `failed(...)`, exit 1. semgrep exits 2 or
more when it cannot load the rule file (a YAML it rejects, an invalid `pattern-regex`), which is
the gate's input being unusable, not a rule that stopped matching. S8 states it this way, so the
code is to spec, but it is the same misclassification as TC-3; `qa-all` fails the gate either
way (exit 1 with no `passed:` line is B2 of SPEC-qa-all). Suggest `Code >= 2` → exit 2 with
semgrep's stderr, `Code == 1` → the fixture failure, and S8 amended.

### STT-7 — INFO — `statusAfter(_ int, _ error, code)`

`run.go:183-185` exists to drop the result of writing to stderr. The package doc explains it
("a stderr that cannot be written to has nowhere left to say so") and the code is honest about
it, but it is the project's "no discarded errors" rule met with a function whose two parameters
are `_`. The other two ports keep the same fact in an `emitter` that records the first failure
and lets `Main` decide; either shape is fine, this one just reads as an evasion. No action owed.

### STT-8 — INFO — what held up

- S1-S3: usage, unknown option, missing value (bare words are paths, B7), `--scripts`, stdout
  write failure, stdout holds only the payload, a symlinked root resolved and semgrep run in the
  real root, no checkout path in output (fake and real, including the scrub of semgrep's own
  absolute paths, red test first per the journal), unresolvable root.
- S4-S8: `semgrep --version` missing (real `exec.ErrNotFound` on an empty PATH), exit 1, timed
  out, leftover child → exit 2 before anything else runs; rule file and both fixtures must be
  regular files; ten malformed rule-file shapes refused including a one-language rule file (B5);
  a fixture missing an annotation for a rule of its language → exit 2 before the self-test
  runs (B2; see STT-2 for the hole); a multi-id annotation; the exact `--test` argv; a failing
  fixture is exit 1 with semgrep's words and no scan; a self-test that cannot start or times out
  is exit 2.
- S9: the default scope is the two discovery lists less the exclusions, including an untracked
  unignored file (B1) and `tests/fixtures/f.py` (handed by name, so semgrep's default ignore of
  `tests/` cannot drop it); a language with no file left is exit 2; a discovery failure is exit
  2 with its message.
- S10: named files only; language by name then Python shebang; neither language → exit 2 (B4);
  `./`, absolute, `//` forms judged once; an absolute path through a symlinked checkout
  accepted; outside or climbing out → exit 2; missing or a directory → exit 2.
- S11-S12: one rule file per language, re-encoded from the decoded nodes (a `pattern-regex`
  with `—`, `(`, `)` survives), mode 0600, outside the checkout, removed on success and on
  failure; a language with no file is not scanned; the exact scan argv with
  `--max-target-bytes=0` (B3) and the files by name from the root.
- S13-S15: exit ≥2, not JSON, empty, no `paths`, timeout, leftover → exit 2; exit 1 with a
  report is a report; an `errors` entry is exit 2 naming the file; a handed file not in
  `paths.scanned` is exit 2 naming it with "is not a pass"; `./` prefixes equal; zero scanned
  is exit 2.
- S16-S17: sorted by path then numeric line (B6: 9 before 10), duplicates folded, the summary
  line, `passed:` counts files not findings, the clean line exact.
- Real semgrep (`cmd/qa-speech-to-text-rules/main_test.go`, 24.8 s here): the shipped rules over
  a clean tree; both rules in both languages under the bare id; an extensionless Python script
  meets the Python rules only (the reason for S11); a rule edited to match nothing fails its
  fixture; a fixture with no annotation is refused; named files only.
- Wiring: `qa-gates.tsv:58` unchanged (`count`); `stt-rules-files` per file (gate, needs
  bash/go/semgrep) and `stt-rules` whole; `.semgrep/speech-to-text.*` covered by `stt-rules` and
  never handed per file (`semgrepReaders` updated to `check:stt-rules`); the wrapper not picked
  as a suite; 35 golden scenarios re-pinned as B44 with one reason; allowlist row removed
  legitimately. `go test` (3 packages) and `golangci-lint` clean.

---

## Across the three branches

### X-1 — INFO — B-rows do not collide; the merges will conflict textually

B42 (plan-script-logging), B43 (toolchain) and B44 (speech-to-text) are distinct and each
branch's deviations are keyed to its own number. But every branch was cut from the same base
and edits the same lines: the B39-B41 rows of `SPEC-qa-changed.md` (re-aligned by the table
formatter on all three), the `perfile-gate` line of `qa-routing.tsv` (a word appended on all
three), the `want` lists in `changed/checks_test.go` (all three), `checkDefs` in
`changed/checks.go` (all three), the `qa-ready-wait-rules.bash` row of `CLAUDE/QA.md`
(re-aligned on plan-script-logging and toolchain), and `src-go/CLAUDE.md` (plan-script-logging
and speech-to-text insert their paragraph at the same anchor after `qa/ghscopes`). Expect a
conflict in each on the second and third merge; resolve by keeping all additions in registry
order (`stt-rules-files` after `ready-wait-files`; `plan-script-logging` and `stt-rules` after
`ready-wait`, before `deployed-drift`).

`testdata/deviations.json`: toolchain adds two keys (`file-src-go-go-mod`, `group-go-closure`),
speech-to-text rewrites 35 (`file-*`, `group-everything`, `group-shell-and-python`, `run-*`);
the key sets are disjoint, but the pinned stdout of a scenario that changes both a Python file
and `src-go/go.mod` needs both the B43 `check toolchain` line and the B44 `stt-rules-files`
line once both are merged. After each merge run `go test ./changed -update-deviations` and
re-pin; a hand edit of a pinned file is the thing the golden exists to refuse.

### X-2 — INFO — the method was followed

Each branch: spec first with numbered expectations and a departure table; tests cite the
numbers; the journal records red (20, 20 and 40 failing tests against a stub) and green; the
live tree was run with both tools and the identical result explained (82 scripts; 5 tools; 337
files) — all three reproduced here. Nothing records the bash's output as an oracle; where the
bash was wrong (B-rows), the spec says what is right. The journal entries use the `—` reference
form the grammar allows (`PlanJournalling.md:73`).

## Suggested order

1. STT-2 (one regex, one test) and STT-3 (one trees row, drop two `excluded()` entries, two
   tests, two spec sentences): both before the speech-to-text merge.
2. TC-2 (three comment edits) before the toolchain merge; TC-3 with it if cheap.
3. PSL-1 / TC-1 / STT-1: the three Task 3.3 sentences, in the merge commits.
4. PSL-2, TC-4, STT-4, STT-5, STT-6, PSL-3 when the files are next touched.
