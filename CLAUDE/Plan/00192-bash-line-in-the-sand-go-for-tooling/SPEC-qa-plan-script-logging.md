# qa-plan-script-logging: specification of the Go port

Same method as [SPEC-qa-gh-scopes.md](SPEC-qa-gh-scopes.md): **the legacy bash is not an oracle.**
This states the *intended* behaviour as numbered, testable expectations;
`scripts/qa-plan-script-logging.bash` (an awk line match over `find` output, plus two self-controls)
was read only to find the requirements. Where it was wrong the expectation says what is right and
section 3 lists the departure. Proof is the Go tests in `src-go/qa/planscriptlogging` (cited by
number), one on the real file system in `src-go/qa/cmd/qa-plan-script-logging`, and a run of both
tools over the live tree at cut-over.

## 1. Role

The gate for Plan 00130: a plan script that redirects its own output into a process substitution
(`exec > >(tee "$LOG") 2>&1`) loses the last chunk of its run log, because the shell cannot wait on
a process substitution; and a plan-local `logs/` directory hides unscrubbed run logs from every
`git status`. Run logs belong under `untracked/plan-runs/`, written by the plan library's
`plan_start_log`. Scope is the **active** plans only, `CLAUDE/Plan/NNNNN-*/`; the archived tree under
`Completed/` is frozen history. Caller: `qa-all` (`scripts/qa-gates.tsv`, gate `plan-script-logging`,
kind `detail:`, reading the OK line) and `qa-changed` (check `plan-script-logging`, whole). The tool
reads the file system and starts no process.

## 2. Expectations

### Command line, streams, exit codes

- L1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument, or a flag missing
  its value, is exit 2 `ERROR: ...` with the usage on stderr for an unknown argument.
- L2. Exit codes: 0 clean, 1 an offence found, 2 the run could not complete. Every exit-2 message is
  on stderr and starts `ERROR:`. A failed write to stdout is exit 2 with
  `ERROR: cannot write to stdout: <why>`.
- L3. The root is resolved by `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is
  exit 2). Paths printed are relative to the root; no checkout path reaches either stream.

### What is examined

- L4. **Active plan folders** are the directories directly under `CLAUDE/Plan/` whose name is digits,
  a hyphen, then at least one more character (`00130-name`). `Completed/`, the plan tooling files
  and anything else beside them are not examined. `CLAUDE/Plan` unreadable or absent is exit 2; no
  active plan folder is exit 2 `ERROR: no active plan folder under CLAUDE/Plan; nothing was checked`.
  A symlink standing where a plan folder would be is exit 2 (a link is not followed, so its scripts
  would silently go unread).
- L5. Each active folder is walked recursively, whatever the file's git status (an ignored or
  untracked file is examined: that is where run logs hide). Only regular files are looked at. A
  symlink below a plan folder is left out: a link's target is examined in its own place or is not
  plan content. A directory or file that cannot be read is exit 2 naming it, after the rest has been
  judged.
- L6. A **plan script** is a regular file named `*.bash` or `*.sh`, or whatever it is called, one
  whose first line is a bash or sh shebang (`qa/discovery`'s `ShellShebang`, the one shell
  predicate). Every other file is not examined.
- L7. No plan script found is exit 2 `ERROR: examined 0 plan scripts; the search matched nothing, so this gate certified nothing`.
  The count `N` of scripts examined is part of the OK line: a clean verdict says how much it read.

### What is an offence

- L8. Each plan script is parsed with `mvdan.cc/sh/v3` (bash). A script that does not parse is exit 2
  naming the file and the parser's message: a file the analyser could not read is not a clean file.
- L9. **The offence** is a statement that is a bare `exec` (the word `exec` and nothing else after any
  assignments, so the shell keeps running) carrying a redirection whose target is an output process
  substitution `>( ... )`. Any file descriptor (`>`, `>>`, `&>`, `2>`, `3>`, `{fd}>`), any command in
  the substitution (`tee`, `cat`, `ts`, a function), and the statement anywhere in the tree (top
  level, a function, an `if` or a loop, a subshell, a brace group, a continued or multi-line form).
  The reported line is the statement's first line.
- L10. **Not an offence**: the text in a comment, in a quoted string, or in a here-document body;
  `cmd | tee f`; `exec > "$LOG"` and `exec >> file` (a plain file); `exec < <(cmd)` (an *input*
  process substitution); `exec cmd > >(tee f)` (the shell is replaced, nothing is left to wait);
  `foo > >(tee f)` where the command is not `exec`.
- L11. **The paired offence** is a directory named `logs` at any depth under an active plan folder,
  whether or not git would publish it.

### Output

- L12. A finding is printed to stderr as `<path>:<line>:<the source line, leading and trailing blanks trimmed>` for a script, and `<dir>: plan-local run-log directory` for a directory; scripts first,
  each group sorted by path then line. Then `ERROR: <F> plan-script logging offence(s) across <N> script(s) examined:` precedes the findings (indented two spaces), and the remedy follows: convert
  to the plan library (`source _planlib.inc.bash`, `plan_init`, then `plan_start_log auto`) and remove
  every consumer of the `$LOG` deleted. Exit 1, nothing on stdout.
- L13. Clean: stdout is `PLAN-SCRIPT-LOGGING-OK: <N> plan script(s) examined, no offences`, exit 0.
  The qa-all `detail:` reader matches `[0-9]+ plan script[(]s[)] examined, no offences`.
- L14. The scanner is proven by its own tests rather than at run time: the offence in each of the L9
  forms is reported, and each L10 form and a converted script that mentions the pattern only in a
  comment is not. (The bash built two throwaway controls on every run to show the same.)

## 3. Departures from the bash

| Id  | The bash                                                                                                                                                                                                                               | Now                                                                                                            |
| --- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| B1  | an awk match of `exec > >(tee` on a line that is not a comment: `exec > >(cat)`, `exec 2> >(tee ..)`, `exec &> >(tee ..)` and a continued line escaped it; the same text inside a here-document or a quoted string was a false finding | judged on the syntax tree: any bare `exec` with an output process substitution, none of the L10 text (L9, L10) |
| B2  | only `*.bash` files                                                                                                                                                                                                                    | `*.bash`, `*.sh`, or any file with a shell shebang (L6)                                                        |
| B3  | never read a script's meaning: a file that is not valid bash was greped all the same                                                                                                                                                   | a script that does not parse is exit 2 (L8)                                                                    |
| B4  | folder glob `[0-9]*-*` matched `1abc-x`                                                                                                                                                                                                | digits then a hyphen (L4)                                                                                      |
| B5  | a plan folder that could not be read vanished from `find` output; a symlinked plan folder was skipped silently                                                                                                                         | exit 2 (L4, L5)                                                                                                |
| B6  | two throwaway control files built on every run                                                                                                                                                                                         | the controls are Go tests (L14); a run is not slowed to repeat them                                            |
| B7  | arguments ignored                                                                                                                                                                                                                      | an unknown argument is exit 2 (L1)                                                                             |

## 4. Stated limits

- The detection is of one pattern on purpose: output sent to a process substitution by `exec`. A
  brace group or function whose output is piped to `>(tee)` loses output the same way but is not the
  pattern Plan 00130 measured; widening it is a new rule.
- The tool does not start `git`: the active plans are on disk, and an ignored `logs/` directory
  must be seen, which a git listing would hide.
- `qa-changed` runs the gate whole when an active plan's shell file or a file under a `logs/`
  directory in a plan changes or is deleted, or the wrapper, the tool or the shared discovery
  change. It is a file-system walk of about a hundred folders, cheap enough to run whole.
