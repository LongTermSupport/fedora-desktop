# Review: go-port/qa-cmdmain (shared cmdmain, emit, rootargs)

Diff reviewed: `origin/integration/261014...origin/go-port/qa-cmdmain` (one commit, 37 files,
+639/-875). Checked in a throwaway worktree: `go build`, `go vet`, `go test -race` over every
touched package, `golangci-lint run ./...` (0 issues), `scripts/qa-go.bash` (green, project
rules clean). Read-only; the worktree was removed afterwards.

Verdict: the byte-identity claim holds for every message, exit code, flush order and error
precedence I could trace. No BLOCK. Four FIX findings (routing, docs, one coverage hole, the
Lenient grammar), the rest are NOTEs.

## 1. Behaviour drift — none found

Traced each replacement against the deleted copy:

- `emit.Finish` (`src-go/qa/internal/emit/emit.go:55-64`) is the exact rule all eight `Main`s
  carried: a stdout write error prints `ERROR: cannot write to stdout: <err>` on stderr and is
  cannotRun; a stderr write error turns any code other than cannotRun into cannotRun; a
  cannotRun stays cannotRun with no extra line. Argument order in
  `emit.Finish(out, errOut, run(...), ExitCannotRun)` evaluates `run` first, as the old
  `code := run(...)` did, so the emitters hold their state before Finish reads it.
- `Emitter.Block` = `Line(TrimSuffix(text, "\n"))`. The old usage emission was either
  `out.line(TrimSuffix(usageText,"\n"))` (identical) or a per-line loop of `Fprintln` over
  `SplitSeq` (identical bytes; the only difference is one write call instead of N, and the
  first-error memory is the same either way). `ansible` keeps `Text(usageText)` as before.
- `rootargs.Strict` is the deleted loop verbatim (`options{}` on error becomes `Args{}`;
  messages `%s needs a directory` and `unknown argument '%s'` unchanged; the typed
  `*UnknownArgError` still gates the usage text). `rootargs.Lenient` is the deleted
  index-loop verbatim (`%s needs a value`, plain error for unknown, partial opts returned,
  which no caller reads on error).
- `cmdmain.WriteReport` (0644 then chmod) is what qa-bash and qa-ansible-syntax had;
  `cmdmain.WriteReportPrivate` (`os.WriteFile` 0600, no chmod) is what qa-js, qa-python,
  qa-ansible and qa-patterns had. The two modes were kept apart, correctly: an existing
  0600 report stays 0600 under `WriteReportPrivate` exactly as before, and the umask still
  cannot widen it.
- `cmdmain.RefuseClosedStdout` prints the same one line the five mains printed; qa-docs
  already used `StdoutWasClosed` from the base `cmdmain`.
- `helpertests`: `emit.Emitter.Write` is the old `(*emitter).Write` used as `StdoutTo`/
  `StderrTo` for the runner, same first-error semantics, same `(n, err)` passthrough.

## 2. Test coverage

- Deleted: `TestWriteReportWritesTheFile` x5 (qa-bash, qa-ansible-syntax, qa-js, qa-python,
  qa-patterns), `TestTheReportHasMode0644` (qa-bash, S2), `TestReportIsWorldReadable`
  (qa-ansible-syntax, S15/F7). Replaced by `cmdmain_test.go`
  `TestWriteReportWritesTheFileWorldReadable` (umask 077 set, mode 0644 asserted, missing
  directory refused) and `TestWriteReportPrivateWritesTheFileOwnerOnly` (0600, missing
  directory refused). The umask and mode cases are covered at least as well as before.
- **FIX (low)** `src-go/qa/internal/cmdmain/cmdmain_test.go:14,55`: the deleted tests read
  the file back and asserted `"{}"`; the shared tests only `Stat` it. A `WriteReport` that
  truncated or wrote the wrong bytes would pass. Add `os.ReadFile` + content assertion to
  both.
- **NOTE** Each command's `realRun` helper passes `cmdmain.WriteReport`/`WriteReportPrivate`
  explicitly, so no test exercises `main.go`'s choice of writer. Swapping
  `WriteReportPrivate` for `WriteReport` in `cmd/qa-js/main.go:34` (or the reverse in
  `cmd/qa-bash/main.go`) would change the report mode with every test green. Before the
  refactor this could not happen (one local function). Cheap guard: a test per cmd package
  that runs `run([]string{"--root", tree})` with `QA_JSON_OUT` set and stats the report.
- The closed-stdout tests in `cmd/qa-{bash,ansible-syntax,js,python,docs}` are untouched and
  still exercise the real `run` through a `bash -c 'exec ... >&-'` child.

## 3. Strict vs Lenient — move all six to Strict

`rootargs.Lenient` (`rootargs.go:35-57`) is used by `docs`, `js`, `python`, `ansiblesyntax`,
`patterns`, `ghscopes`. Two concrete failure scenarios the Lenient grammar allows:

1. `qa-docs --root ""` (an unset shell variable: `--root "$ROOT"`): Lenient accepts `""`,
   `rootpath.RootVia` (`internal/rootpath/rootpath.go:40-47`) treats an empty root as "not
   given" and silently falls back to `--scripts`' parent or the cwd. The gate judges a
   different tree from the one asked for and exits 0. That is a silent failure.
2. `qa-js --root --scripts /r/scripts`: `--scripts` becomes the root; the gate then reports
   `cannot resolve the repository root <cwd>/--scripts`. Not silent, but the message blames
   the wrong thing.

Strict refuses both with `--root needs a directory`. Nothing in the six packages' tests pins
`needs a value` (grep: only `rootargs_test.go:25` and `helpertests`' own parser do), and
every one of the six already prints the usage on any parse error (`fail(env, err, true)`), so
the typed `*UnknownArgError` changes nothing for them. The specs (D1, J1, Y1, S1, P1, G1)
say only "a flag missing its value is exit 2"; none promises the `needs a value` wording.

**FIX (medium)**: switch all six to `rootargs.Strict`, delete `Lenient` and its two tests, and
add one numbered departure per spec (SPEC-qa-docs, SPEC-qa-js, SPEC-qa-python,
SPEC-qa-ansible-syntax, SPEC-qa-patterns, SPEC-qa-gh-scopes): "a `--root`/`--scripts` value
that is empty or begins `-` is refused (`<flag> needs a directory`, exit 2); the bash would
have run against the wrong tree". `helpertests`, `bash`, `readywait`, `sttrules`,
`ansiblepause`, `deployeddrift`, `all`, `changed` keep their own parsers as the journal says.
One grammar, one package, no "two forms because the messages differ" paragraph.

## 4. Missing closed-stdout check (qa-ansible-pause, qa-helper-tests, qa-all)

Not a false pass. With stdout closed at start the Go runtime gives fd 1 `/dev/null` opened
RDWR, every write succeeds, `emit.Finish` sees no error, and the **exit code is the real
verdict**: findings and summaries of all three go to stderr, so a failing run still exits 1
and `qa-all` reads its children through pipes, never through fd 1. What is lost is the stdout
text (`passed: N failed: M`, `HELPER-TESTS-OK ...`, qa-all's verdict line) with no error.

Also missing from the other eleven mains (`qa-ansible`, `qa-patterns`, `qa-gh-scopes`,
`qa-nokill-containerwatch`, `qa-toolchain`, `qa-version-pins`, `qa-plan-script-logging`,
`qa-vmtest-manifest`, `qa-deployed-drift`, `qa-ready-wait-rules`, `qa-speech-to-text-rules`,
`qa-discovery`, `qa-bash-limits`, `qa-changed`). Only the four whose specs name it
(ansible-syntax S3, docs D2, js J2, python Y1/Y2) plus qa-bash carry it.

**NOTE (low, not this branch's defect)**: the inconsistency is pre-existing and the journal
is right that it is not a copy. Now that it is a two-line call it should become the rule for
every main, with one sentence in each spec's exit-code expectation. Not required to merge.

## 5. qa-changed routing

The new rows (`scripts/qa-routing.tsv:36,42,46,54,59,64,73,80,88,94,103,112,121`) match the
imports exactly for thirteen gates, and `TestAChangeToASharedCommandHelperRunsTheGatesBuiltOnIt`
(`changed/classify_test.go:136`) pins them plus a negative (`deployed-drift`). Two gaps:

- **FIX (medium)** `scripts/qa-routing.tsv`: `cmd/qa-ansible/main.go` now imports `cmdmain`
  and `qa/ansible/run.go` imports `emit` and `rootargs`, but the `ansible-patterns` check
  (`changed/checks.go:73`, which runs `qa-ansible.bash` whole) has **no** `also` row for
  `src-go/qa/ansible/*`, `src-go/qa/cmd/qa-ansible/*` or any shared package. It is routed
  from YAML edits only. A change to `emit.Finish` reruns every other gate built on it and
  not the ansible stage. The package-level gap predates this branch; the branch widens it
  and the journal line "the rows of the gates built on a shared package list it" is not
  true for `ansible`. Add
  `also src-go/qa/ansible/*,src-go/qa/cmd/qa-ansible/*,src-go/qa/internal/cmdmain/*,src-go/qa/internal/emit/*,src-go/qa/internal/rootargs/* ansible-patterns -`
  and add `"ansible-patterns"` to all three lists in the classify test.
- **NOTE** `helpertests` imports `emit`; `helper-tests` is not a `qa-changed` check (it is a
  `helpers`-kind row of `qa-gates.tsv`), so no row is possible. The Go tests run via the
  `first src-go/* go` row; acceptable.

## 6. Analyzer cleanliness

`golangci-lint run ./...` 0 issues; `scripts/qa-go.bash` green including the project rules.
Leftover `flagRoot`/`flagScripts` constants in `ansiblesyntax`, `docs`, `js`, `python`,
`vmtestmanifest` are still used by their discovery argv builders, so nothing is dead.

## 7. Docs drift

- **FIX (low)** `src-go/CLAUDE.md`: the qa section (lines 101-169) names `qa/internal/proc`
  as the one shared seam and nothing else. `qa/internal/cmdmain`, `emit`, `rootargs` (and the
  older `rootpath`) are now what every gate's main and `Main` are built on and a new port is
  expected to use them; a reader of this file would copy the boilerplate again. Add one
  paragraph after the `proc` sentence (line 118-119) naming the four and what each owns
  (closed stdout + report writers; the two-stream emitter and the exit rule; the
  `--root/--scripts/-h` grammar; root resolution).
- **NOTE** Only SPEC-qa-docs section 4 mentions `cmdmain`. The other specs' seams sections
  predate the shared packages; not drift, since the specs describe behaviour, but the
  Strict move in section 3 is the moment to add the one line each.

## Summary

| Sev  | Where                                                         | Finding                                                                                                                |
| ---- | ------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| FIX  | `src-go/qa/internal/rootargs/rootargs.go:35`, six `parseArgs` | Lenient lets `--root ""` silently judge the wrong tree; move all six to Strict, delete Lenient, one departure per spec |
| FIX  | `scripts/qa-routing.tsv`, `changed/classify_test.go:136`      | no row routes `qa/ansible`, `cmd/qa-ansible` or the shared packages to `ansible-patterns`                              |
| FIX  | `src-go/CLAUDE.md:118`                                        | the shared `cmdmain`/`emit`/`rootargs`/`rootpath` packages are not mentioned                                           |
| FIX  | `src-go/qa/internal/cmdmain/cmdmain_test.go:14,55`            | report content is no longer read back after the write                                                                  |
| NOTE | `cmd/*/main.go`                                               | no test pins which report writer each main wires                                                                       |
| NOTE | fourteen `cmd/*/main.go`                                      | closed-stdout refusal is in five mains only; exit code survives, stdout text is lost silently                          |
