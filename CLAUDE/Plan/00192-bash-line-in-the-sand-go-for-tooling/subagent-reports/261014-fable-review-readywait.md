# Review: go-port/qa-ready-wait-rules against SPEC-qa-ready-wait-rules.md

Read-only adversarial review of `git diff integration/261014...go-port/qa-ready-wait-rules`
(tip `c18ec5d3`). Method checked: spec with numbered expectations, tests first, bash not the
oracle, script a `go-tool.bash` wrapper. `go test` and `golangci-lint` over `qa/readywait`,
`qa/cmd/qa-ready-wait-rules` and `qa/changed` are green. Probe scripts were run against
`Findings` and `codeOnly` in a detached worktree (removed); the probe results quoted below are
what the branch's rule does.

## Verdict

The orchestration half (R1-R13, R25-R26) is sound: every semgrep failure mode that could become
a false pass (exit 2, empty report, unparseable JSON, `errors[]`, missing `paths.scanned`, a file
handed over but not scanned, empty default scope) is refused with exit 2 and has a test; the
wrapper propagates the exit code; the `passed: N failed: M` line qa-all reads is unchanged; the
35 Python rule cases are carried one for one (35 entries in `cases_test.go`, names identical) and
the six non-rule Python tests map to `TestCodeOnlyKeepsLinesAndOffsets`,
`TestTheProjectFixtureAnnotationsMatchTheFindings`, `TestABashRuleThatFailsItsFixtureIsExitOne`
and the run tests.

The shell half is a faithful port of the Python hand lexer, including its defects. Three of them
are false passes (the gate says clean when a wait ignores its child), one is a spec violation
(R22), and all of the lexer ones disappear if `codeOnly` is produced from the `mvdan.cc/sh/v3`
syntax tree, which the module already depends on and which `qa/bashlimits` already proves parses
every gated bash file in this tree (`measure.go:146` makes an unparseable file an error, and the
gate is green). Findings F1-F4, F7 and F15 should be settled before merge; F1 and F2 are the ones
that matter most because their failure mode is silence.

## Blocking findings

### F1 (High) A function that keeps its pid other than as bare `VAR=$!` is not a launcher

- `src-go/qa/readywait/rule.go:28` `pidCaptureRe = [A-Za-z_]\w*=\$!`, applied to the blanked code
  in `analysis.go:29`.
- Failure: the lexer blanks `$!` inside double quotes, so `local pid="$!"`, `pids+=("$!")`,
  `printf '%s' "$!" > pidfile` leave no `$!` in the code the regex reads. The function is treated
  as fire and forget; a sleeping wait after a call to it is not reported. Probe:
  `start() { w &; local pid="$!"; }; start; until [ -e r ]; do sleep 1; done` gives `[]`, and the
  array form gives `[]`. Real tree: `files/var/local/claude-yolo/lib/token-management.bash:542`
  keeps pids as `pids+=("$!")` (no live finding because the function then `wait`s, but the
  launcher classification is wrong today).
- Spec: R16 says "keeps its pid (`VAR=$!`)". The parenthesis is the Python's limitation written
  into the spec; the intent is "keeps its pid".
- Fix: in `lexer.go` `quoted()`, treat `$!` inside double quotes as code (as `$(` already is), or
  test the raw text of the function body for `$!`. Reword R16 to "keeps `$!` in any form". Add
  cases for the quoted and array forms.

### F2 (High) A here-document whose delimiter is not `[A-Za-z_]\w*` blanks the rest of the file

- `src-go/qa/readywait/rule.go:30` `heredocRe = ^<<(-?)[ \t]*(['"]?)([A-Za-z_]\w*)(['"]?)`,
  used by `lexer.go:197 openHeredoc`.
- Failure: `cat <<END-MARK` is read as delimiter `END`; no line equals `END`, so every byte to
  the end of the file is blanked and every later loop is invisible. Probe:
  `cat <<END-MARK ... END-MARK; w &; p=$!; until ...; do sleep 1; done` gives `[]` (expected
  `[5]`). The other direction: `cat <<\EOF` (backslash-quoted delimiter, legal bash) is not a
  here-document at all, the body is read as code, and a `server &` inside it arms the scope
  (probe gives `[4]`, expected `[]`). The blanked text of that probe is `cat <<  OF` followed by
  the live body. Real tree today: no tracked script uses either form (the `<< GIT-PREFLIGHT`
  hits are comments), so no live false pass, but the failure mode is a silent whole-file skip.
- Fix: parse with mvdan (see F8), or widen the delimiter to bash's word grammar:
  `(\\?)(?:'[^']*'|"[^"]*"|[^\s;&|<>()'"\\]+)` and strip the quoting before comparison. Add
  cases for `<<\EOF`, `<<END-MARK`, `<<"a b"`.

### F3 (Medium) A `#` right after a closing quote starts a comment

- `src-go/qa/readywait/lexer.go:120-131` (`quoted()` pop) and `:143-149` (quote open in
  `code()`): neither sets `wordStart = false`, so after `echo "a"` the `#` in `echo "a"#b` is at a
  "word start" and blanks the rest of the line.
- Failure: `echo "a"#b; w &` loses the `w &`; the following wait is not reported. Probe gives
  `[]`, expected `[4]`. Bash treats `"a"#b` as one word.
- Fix: set `l.wordStart = false` when a quote opens and closes (and after `$'`). One case.

### F4 (Medium) A lone `&` inside arithmetic arms the scope

- `src-go/qa/readywait/rule.go:118 launchesIn` reads the blanked code; the lexer keeps `$((...))`
  and `((...))` as code.
- Failure: `x=$(( a & 1 ))` is a background start; every later sleeping wait in the scope is
  reported. Probe gives `[2]`, expected `[]`. Real tree:
  `files/usr/local/sbin/fedora-desktop-self-update:105` `((!(8#${mode} & 8#022)))` arms the top
  level of that script for its remaining 54 lines (no sleeping loop there today, so no live
  finding; the next one added is a false failure).
- Fix: blank arithmetic (`$((`, `((`, `$[`) as a quoting level in the lexer, or take it from the
  syntax tree (F8).

### F7 (Medium) A backgrounded loop in an armed scope is reported, against R22

- `src-go/qa/readywait/analysis.go:60-70 events`: the loop event is at the loop's header and the
  `&` after its `done` is a separate launch event later in the order, so `armed` is already true
  when the loop is seen.
- Failure: `w &; p=$!; while true; do [ -e r ] && break; sleep 1; done &` gives `[3]`. R22 says a
  loop that is itself backgrounded is a start, not a wait. `cases_test.go`
  `a_backgrounded_loop_is_not_a_wait_for_itself` only covers the unarmed case, so the test
  passes while the expectation is violated.
- Fix: in `unguardedPolls`, drop a loop whose `done` is followed by a lone `&` on its line
  (`backgrounded(code, donePos+len("done"))`). Add the armed case.

### F8 (Medium, design) Build `codeOnly` from the mvdan syntax tree

- F1-F4 and the low lexer findings below (F6, F9) are one defect: a hand lexer that re-derives
  bash's word grammar with regexes. `mvdan.cc/sh/v3 v3.14.1` is in `qa/go.mod`, is used by
  `qa/bashlimits/measure.go:144` with `syntax.Variant(syntax.LangBash)` and
  `KeepComments(true)`, and that gate fails on any file it cannot parse, so it is already proven
  against every bash file this gate scans (less the `tests/`, plan-folder and vendored trees
  bashlimits exempts; worth a one-off check over `tracked-shell`).
- Shape: walk the tree and blank by byte offset (`Pos().Offset()`..`End().Offset()`) every
  `*syntax.SglQuoted`, `*syntax.DblQuoted` minus its `*syntax.CmdSubst` children and `$!`
  `*syntax.ParamExp`, every `*syntax.Comment`, every `Redirect.Hdoc` word (the body), and every
  `*syntax.ArithmExp`/`ArithmCmd`. Newlines stay in place as now; `rule.go` and `analysis.go`
  need not change. The spec's "semgrep's bash parser rejects about a quarter of this repo's
  scripts" is about semgrep, not mvdan, and should not be read as an argument against this.
- If the hand lexer is kept, fix F1-F4 individually and add a parity test that runs `codeOnly`
  over every `tracked-shell` file and asserts that mvdan parses it to the same set of comment
  and quoted spans, so the lexer cannot silently diverge again.

### F15 (Medium) Routing: the per-file scope the spec builds (R9, B6) has no caller

- `scripts/qa-routing.tsv:244-248` and `qa/changed/checks.go:80` run the gate whole only when
  the gate itself changes; an ordinary `.py`/`.sh` edit never meets the rule before the push.
  The comment justifies this by the whole-tree cost, but the tool has explicit scope exactly
  for this: `qa-ready-wait-rules.bash <changed files>` costs `semgrep --version`, the two fixture
  proofs and a semgrep run over a handful of files, in the same order as the existing
  per-file `semgrep` check. The rule's purpose is a new wait in an edited script; the gate still
  runs in `qa-all`, so the push catches it, but the commit does not, and the spec's R9/R13
  ("with no Python file semgrep is not started") exist for a caller that does not exist.
- Either add a listed `ready-wait-files` check routed from the `@py` and `@sh` rows (noun files,
  tools bash+go+semgrep), or remove the explicit-scope mode and R9/B6 from the spec (YAGNI). The
  first is the right one; the gate was designed for it.

## Other findings

### F5 (Medium-low) `sudo` with a valued option before a manager start is not a start

- `src-go/qa/readywait/rule.go:23` `sudo(?:[ \t]+-[\w-]+)*` takes only valueless flags.
- `sudo -u root lxc-start -n c; until ...; do sleep 1; done` gives `[]`, expected `[2]`
  (R17: "sudo with options"). No live instance. Fix: allow `-u user`/`-g group` values, e.g.
  `(?:[ \t]+-[\w-]+(?:[ \t]+[^-\s;&|][^\s;&|]*)?)*`.

### F6 (Low) `function name {` is not a function

- `rule.go:29 functionRe` requires `()`. Bash accepts `function name {`. Probe: a launcher in
  that form is read as top-level code, so its `w &` arms the top level and the loop after the
  definition is reported (`[5]`, expected `[]`); a launcher in that form is also never a
  launcher for its callers. Real tree: `files/var/local/colours` defines 16 functions this way
  (no launches, so harmless today). Fix: add the alternative, or F8.

### F9 (Low) A `case` pattern's `)` inside a quoted `$(...)` closes the substitution early

- `lexer.go:218 plain()` pops the level on the first unmatched `)`. `x="$(case $y in a) echo 1;; esac)"` blanks `echo 1;; esac)` (a `;;` disarm is lost) and only recovers because the line
  ends in `"`. No wrong verdict found; a longer substitution would read the rest of the file as
  quoted text. F8 removes it.

### F10 (Low) `while read` loops that sleep are waits

- `rule.go:177 isWait`: any `while`/`until` condition other than `true`, `:` or `((` is "a
  test", so `while read -r l; do sleep 1; done < f` after a start is reported (probe `[3]`).
  The tree has 218 `while read` loops. Same as the Python. Either exclude `read` conditions in
  `isWait` and say so in R18, or state in R18 that `read` counts as a test.

### F11 (Low) `trap waiter EXIT` counts as a call to `waiter`

- `analysis.go:63-67`: every word equal to a function name that is not the definition and not
  backgrounded is a call, so `trap`, `declare -f`, `export -f` register a call and the
  function's loops are flagged (probe `[2]`). Conservative (a false failure), not a false pass.
  Note it in R18 or exclude the `trap` word.

### F12 (Low) An absolute or escaping PATH argument prints the checkout path

- `run.go:243 path()` returns an absolute `rel` unchanged and `scan.go:130` prints `rel` as
  given, so `qa-ready-wait-rules.bash /abs/checkout/x.bash` prints `/abs/checkout/x.bash:4: ...`
  against R3 ("no checkout path reaches stdout"); `../other/x.bash` reads outside the root.
  Fix: exit 2 for an absolute path or one that cleans to `..`, or make it root-relative and
  print that. The wrapper passes `"$@"` straight through, so this is reachable.

### F13 (Low) `run()` pairs a dead exit code with the proof failure

- `run.go:200-203`: `proveRules` returns a `*failure` carrying `ExitFailed`, but `run` returns
  `ExitCannotRun, err`; `Main` ignores the code when `err != nil` and `report` derives it from
  the error. Correct, but the pair reads as exit 2 for a fixture failure. Return `0, err` or
  drop the code from the error path. Also `requireSemgrep` (`scan.go:17-22`) says "not found" for
  a timeout or a `Leftover` process; name the real cause.

### F14 (Low) Third copy of the semgrep report type and third `tagliatelle` exclusion

- `qa/changed/checks.go:303`, `qa/patterns/semgrep.go:71`, `qa/readywait/scan.go:164` (and the
  test twin `run_test.go:56`) each define `check_id`/`path`/`start.line`, each with a
  `.golangci.yml` exclusion. The new exclusion (`.golangci.yml:150-153`) is narrow (`check_id`
  only, two files) and correctly reasoned; the defect is the duplication. A
  `qa/internal/semgrepreport` type with one exclusion serves all three.

### F16 (Low) Spec expectations with no test

- R19: `kill -s 0`, `kill --signal 0`, `lxc-info --state`, `lxc-ls --active` (only `-sH`,
  `--running`, `domstate`, `list`, `kill -0`, `ps -p`, `wait -n` are exercised).
- R17: `exec`/`nohup`/`!`/assignment prefixes before a manager start (only `sudo`, `sudo -n`
  and `if !` are exercised).
- R2: a timed-out or `Leftover` process (the `runTool` branches at `run.go:227-233`).
- R22's armed backgrounded loop (F7).

### F17 (Low) Housekeeping

- PLAN.md is not updated; the journal entry (`00192-Journal-26-10-14.md`, 22:11) defers the
  one-line port entry to the integrator. Acceptable only if the merge commit carries it (Plan
  Commit Rule).
- `CLAUDE/Plan/00156-speech-to-text-auto-model-never-downloaded/PLAN.md:58` still names
  `helpers/ready_wait/bash_ready_waits.py` as the bash half; one-line update (its JOURNAL and
  subagent reports are history and stay).
- `.semgrep/ready-wait.bash`, `.semgrep/ready-wait.yml`, `CLAUDE/QA.md`, `src-go/CLAUDE.md`,
  `scripts/bash-limits.allowlist` and `SPEC-qa-changed.md` B40 are consistent with the port.
- No install-specific names in the diff. Default shell scope relies on `.semgrep` being an
  excluded shell tree in `scripts/qa-trees.tsv` (it is); if that row were removed the fixture
  would report itself, loudly, which is the right failure.
- `src-go/qa/all/testdata/golden/fail-ready-wait-rules.json` is unchanged and the output format
  is byte-compatible with it.

## What was verified and found correct

- Exit codes: wrapper `run_go_tool` returns the binary's status; `Main` maps usage to 2,
  proof failure to 1, findings to 1, run failure to 2; a stdout write failure is 2 (tested).
- Scope: default is `qa/discovery` `tracked-python` less `.semgrep/` plus `tracked-shell`, both
  through the one seam, with discovery failure and empty lists as exit 2 (tested); explicit
  paths are deduplicated and classified by name then shebang (tested).
- semgrep: argv matches R10 exactly (tested); `--test` runs before any scan and a scan never
  runs after a failed proof (tested); `./` prefixes are normalised both ways (tested).
- B7 (here-string `<<<`) is fixed and pinned (`TestHereDocumentForms`); `<<-` tab stripping
  and quoted delimiters of the simple form work.
- Invalid UTF-8 is bytes throughout (R24, tested at rule and file level).
- Several findings on one line are one finding (R23, tested); findings are sorted and
  deduplicated across halves (R25, tested).
