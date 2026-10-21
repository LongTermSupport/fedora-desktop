# qa-ready-wait-rules: specification of the Go port

Same method as [SPEC-qa-nokill-containerwatch.md](SPEC-qa-nokill-containerwatch.md): **the legacy
bash is not an oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-ready-wait-rules.bash` and its Python half `helpers/ready_wait/bash_ready_waits.py`
were read only to discover the requirements. Where they were wrong the expectation says the right
behaviour and a departure (B) says what they did. The proof is the Go tests in
`src-go/qa/readywait` (one or more per expectation, cited by number) and the real-tree run of the
gate at cut-over.

## 1. Role

The gate for the rule `ready-wait-ignores-child-exit` (Plan 00156, CLAUDE/QA.md): a loop that
sleeps between tries while it waits for a process this code started, and never asks whether that
process has exited, reports a crash as a timeout. Two halves: Python by semgrep
(`.semgrep/ready-wait.yml`, data, proven against `.semgrep/ready-wait.py`), and shell by a small
lexer-based rule in Go (`qa/readywait`, proven against `.semgrep/ready-wait.bash`; semgrep's bash
parser rejects about a quarter of this repo's scripts). Caller: `qa-all` (`scripts/qa-gates.tsv`,
gate `ready-wait-rules`, kind `count`, read from `passed: N failed: M`), people. Processes the tool
starts: `git ls-files` (through `qa/discovery`) and `semgrep`.

## 2. Expectations

### Command line, streams, exit codes

- R1. `[--root DIR] [--scripts DIR] [PATH...]`. `--root` names the repository root; `--scripts`
  names the scripts directory and the root is its parent (the wrapper passes this); neither: the
  working directory. `-h`/`--help` prints the usage to stdout, exit 0. An argument starting `-`
  that is not one of these is exit 2 `ERROR: unknown argument '<arg>'` with the usage on stderr; a
  flag missing its value is exit 2. Any other argument is a PATH, taken relative to the root
  (R9).
- R2. Exit codes: 0 clean, 1 findings or a rule that failed its fixture, 2 the run could not
  complete (a tool missing or broken, a file handed over not scanned, bad usage). Every exit-2
  message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2 with
  `ERROR: cannot write to stdout: <why>`.
- R3. The root is resolved by `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable
  is exit 2). No checkout path reaches stdout: paths printed are repository-relative.

### Prerequisites and the proof of the rules (every run, before any scan)

- R4. `semgrep` not runnable is exit 2, saying why: a program that is not on `PATH` is
  `ERROR: semgrep not found. Install with: pipx install semgrep`; one that cannot start, times out
  or leaves a process holding its output is `ERROR: semgrep --version could not run: <cause>`; one
  that exits non-zero is `ERROR: semgrep --version exited <N>: <its stderr>`. A scan that times out
  or leaves such a process is exit 2 the same way.
- R5. The rules file `.semgrep/ready-wait.yml` and the two fixtures `.semgrep/ready-wait.py` and
  `.semgrep/ready-wait.bash` must be readable regular files, else exit 2 naming the file.
- R6. `semgrep --test --metrics=off --disable-version-check --config <rules> <py fixture>` that
  fails is exit 1: `ERROR: .semgrep/ready-wait.yml failed its fixture .semgrep/ready-wait.py:`
  followed by semgrep's own output (stdout then stderr), on stderr.
- R7. The bash rule is run over `.semgrep/ready-wait.bash` and must report exactly the lines that
  follow its `# ruleid: ready-wait-ignores-child-exit` comments, and the fixture must mark at
  least one. Otherwise exit 1 with
  `ERROR: the bash rule failed its fixture .semgrep/ready-wait.bash: the fixture marks lines [..], the rule reports [..]`
  on stderr. (Both proofs run on every run, so a rule edited into matching nothing fails rather
  than reporting a clean repository.)

### Scope

- R8. **Default scope** (no PATH): the Python files are `qa/discovery`'s `tracked-python` list less
  everything under `.semgrep/`; the shell files are its `tracked-shell` list. Discovery failing is
  exit 2 with its message. Either list empty is exit 2
  `ERROR: discovery found N Python and M shell files; nothing would be checked`.
- R9. **Explicit scope** (PATH...): a file is Python if its name ends `.py`; shell if it ends
  `.bash` or `.sh`; otherwise Python if its first line is a shebang naming python, else shell.
  A path that cannot be read is exit 2 naming it. A path given twice is judged once. Emptiness is
  not an error here. The lists are not filtered by `.semgrep/`. A path is judged and printed
  relative to the root: an absolute path inside the root is made relative; one outside it, or one
  that climbs out with `..`, is exit 2 `ERROR: <path> is outside the repository root`. The caller
  is `qa-changed`'s `ready-wait-files` check, which names the changed Python and shell files.

### The Python half

- R10. With one or more Python files, `semgrep --metrics=off --disable-version-check --config <rules> --no-rewrite-rule-ids --scan-unknown-extensions --json --timeout=120 --quiet <files>` runs in the root (`--timeout=120` is `semgrepreport.TimeoutFlag`: semgrep's 5 s default is wall time, and a loaded host turns a clean file into a Timeout error). An exit
  of 2 or more, or an empty report, is exit 2 `ERROR: semgrep failed on the Python files (exit N):`
  and its stderr.
- R11. Any entry in the report's `errors` is exit 2 `ERROR: semgrep could not parse some Python files, so the rule did not run on them:` and one `    <path>: <message>` line each.
- R12. A file handed over that is not in the report's `paths.scanned` is exit 2
  `ERROR: semgrep did not scan Python files it was handed:` with the files listed. A report
  with no `paths.scanned` is exit 2 as well.
- R13. Each result is a finding `<path>:<line>: <check_id>` (a leading `./` removed). With no Python
  file (explicit scope) semgrep is not started for the scan.

### The shell half: what the rule means

The file is parsed as bash (`mvdan.cc/sh/v3`, the parser `qa/bashlimits` uses on every gated
file), and the text is read after the parts that are not code are blanked from the syntax tree
(every byte keeps its offset and every newline its place); everything below reads the blanked
text.

- R14. **What is code.** Blanked: single-quoted text and `$'...'`; double-quoted text, except a
  command or process substitution inside it (code again, with its own quoting) and `$!`; `#`
  comments (a `#` that follows a closing quote is part of its word, as bash reads it); the whole
  of a here-document, body and terminator, whatever its delimiter word (`<<EOF`, `<<-EOF`,
  `<<END-MARK`, `<<\EOF`, `<<"a b"`; a here-string `<<<` is not one); backslash escapes in
  unquoted words; and the inside of arithmetic (`$(( ))`, `(( ))`, the header of a C-style `for`).
  An `&` inside any of them is not a background start. **A file that does not parse is exit 2**
  `ERROR: <path>: cannot parse as bash: <why>`, and so is a fixture that does not parse: a rule
  that could not read a file has not judged it, and is never a skip.
- R15. **A background start** arms its scope: a lone `&` in code (not `&&`, `&>`, `>&`, `<&`,
  `|&`, and not a bit-and in arithmetic). A scope is a function body, or the top level outside
  any function.
- R16. A call to a function that starts a background process **and keeps its pid** (`$!` appears
  in its code in any form: `VAR=$!`, `local pid="$!"`, `pids+=("$!")`, `printf '%s' "$!"`; not in
  single quotes or a comment) arms the caller. A function that backgrounds something and keeps no
  pid (fire and forget) does not. A function called with `&` is itself a background start, not a
  call whose loops the caller waits in. A function is `name() { ... }` or `function name { ... }`
  with a brace body; a function whose body is another compound command is not read.
- R17. **A manager start** arms the scope too: `lxc-start`, `virt-install`, `virsh ... start`, as
  a command (after a separator, `then`/`do`/`else`/`if`/`while`..., `!`, `exec`, `nohup`, `sudo`
  with options, including options that take a value (`sudo -u root`), or variable assignments;
  also inside `$(...)`); the tool named as an argument
  (`command -v lxc-start`, `virsh autostart`, a word in a `for` list) is not a start. A function
  that runs one arms its callers.
- R18. **A wait** is a `for`/`while`/`until` loop whose body runs `sleep`, in an armed scope or in
  a function that an armed scope calls (a call from an armed scope descends into the function's
  loops, one level), and that ends when its condition is met: its body has `break`, `return` or
  `exit`, or it is a `while`/`until` whose condition is a test. A `for` without such a word, a
  `while true`/`while :`/`while ((...))` without one, runs its full course and is a sampler, not
  a wait. Stated limits, not departures: a `while read` loop is a test and so a wait if it sleeps
  after a start; a function named as a word elsewhere (`trap waiter EXIT`) counts as a call to it.
  Both can only add a finding, never hide one.
- R19. **Liveness** clears a wait: in its header or body, `kill -0`, `kill -s 0`,
  `kill --signal 0`, `wait -n`, `ps -p`, `lxc-info` asking the state (`-s`, `--state`, grouped
  short options containing `s`), `lxc-ls --running|--active`, `virsh domstate` or `virsh list`
  (options may stand between). A call by name, in the header or body, to a function whose own
  body has such a check counts too; **one level only** (a check made through a further function
  is not seen).
- R20. A liveness check only after the loop does not clear it.
- R21. **Disarm.** `wait` (but not `wait -n`) on the child, or the end of a `case` arm (`;;`),
  disarms the scope: a loop after it is not waiting for that child. Case arms are alternatives.
- R22. A loop before the start, a loop without `sleep`, and a poll in a scope with no start are
  not reported. A loop that is itself backgrounded (`done &`) is a start, not a wait for itself,
  whether or not the scope was already armed.
- R23. A finding is the 1-based line of the loop's first line (`for`/`while`/`until`). Several
  findings on one line are one finding.
- R24. Text that is not valid UTF-8 cannot be read as bash: it is the parse failure of R14 (exit 2
  naming the file), not a skip.

### Output

- R25. After the scans, stdout carries
  `scanned: <P> Python file(s) with semgrep, <S> shell file(s) with the bash rule`, then (findings
  sorted, one per line, no duplicates) `<path>:<line>: ready-wait-ignores-child-exit` for the
  shell half and `<path>:<line>: <check_id>` for the Python half.
- R26. With findings: after them
  `ready-wait rule: <N> finding(s) in <F> file(s). See CLAUDE/QA.md "ready-wait-ignores-child-exit".`
  and `passed: <P+S-F> failed: <F>`, exit 1. Clean: `passed: <P+S> failed: 0`, exit 0. (The qa-all
  reader matches `passed: N failed: M`.)

## 3. Departures from the bash and Python

| Id  | The legacy                                                                                                                                                                        | Now                                                                                                 |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| B1  | the shell half was a Python module with its own unit tests                                                                                                                        | Go package; the Python module and its tests are removed, the cases ported (R14-R24)                 |
| B2  | an unreadable or non-UTF-8 shell file was a Python traceback                                                                                                                      | unreadable or unparseable is exit 2 naming it (R9, R14, R24)                                        |
| B3  | the helper had a `--fixture` command line                                                                                                                                         | in-process proof (R7)                                                                               |
| B4  | the same line found twice was printed twice before `sort -u`                                                                                                                      | one finding per line (R23)                                                                          |
| B5  | scratch files in a temp directory                                                                                                                                                 | captured in memory                                                                                  |
| B6  | an explicit PATH was relative to whatever directory the script `cd`'d to                                                                                                          | relative to the root, stated (R1)                                                                   |
| B7  | the hand lexer: a here-string, a delimiter that is not an identifier, `"a"#b`, `$!` in double quotes and a bit-and in arithmetic were misread, silently hiding or inventing waits | the syntax tree of the bash parser decides what is code (R14-R16); a file it cannot parse is exit 2 |
| B8  | a backgrounded loop in an armed scope was reported, against the rule's own statement                                                                                              | not reported (R22)                                                                                  |
| B9  | `function name {` was not a function; `sudo -u user` hid a manager start                                                                                                          | both read (R16, R17)                                                                                |

## 4. Seams

`qa/internal/proc` runs `semgrep`; `qa/discovery` (called in process) runs `git`. Package
`qa/readywait` is otherwise pure over the injected `Env` (streams, working directory, `Runner`).
Command: `src-go/qa/cmd/qa-ready-wait-rules`; wrapper `scripts/qa-ready-wait-rules.bash` via
`scripts/lib/go-tool.bash`.
