# qa-nokill-containerwatch: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md). Same method as [SPEC-qa-ansible.md](SPEC-qa-ansible.md):
**the legacy bash is not an oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-nokill-containerwatch.bash` (276 code lines at `0700654d`) was read only to discover the
requirements. Where it was wrong the expectation says the right behaviour and a departure (B) says
what the bash did. There is no recording of the bash. The proof is the Go tests in
`src-go/qa/nokill` (one or more per expectation, cited by number) and a run of both tools over the
live tree at cut-over.

## 1. Role

The L0 no-kill safety gate for the container-watch watchdog (Plan 00055 Phase 5, Plan 00132 Phase 6).
The watchdog must never signal a process, and must never end a container, with ONE audited exception:
`helpers/containerwatch/containment.py` may build a graceful `stop` and nothing else. The gate fails
the build when an *executable* termination call site appears. It keys on call-site syntax, so the
word "kill" stays free in guidance strings, comments and prose (an `exec_hint`, a report line telling
a human `podman stop <container>`). Caller: `qa-all` (`scripts/qa-gates.tsv`, gate
`nokill-containerwatch`, detail read from `N container-watch file(s) clean`), `qa-changed`, people.
The tool starts one process: `git ls-files` (section 4).

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes

- E1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Anything else is exit 2 with
  `ERROR: unknown argument '<arg>'` and the usage on stderr; a flag missing its value is exit 2.
  *(B11: the bash accepted `--self-test`; its cases are Go tests now, so the flag is an unknown
  argument.)*
  The root is resolved by the one shared helper `qa/internal/rootpath` (absolute, clean, symlink-free; a root that cannot be resolved is exit 2), and a symlinked-root test pins that no checkout path reaches stdout or the report.
- E2. Exit codes: 0 clean, 1 at least one forbidden call site, 2 the run could not complete. Every
  exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2 with
  `ERROR: cannot write to stdout: <why>`; a failed write to stderr is exit 2.

### Which files are judged; when the run cannot complete (exit 2, never a pass)

- E3. **Scope: what git would publish.** The files judged are among those listed by
  `git ls-files --cached --others --exclude-standard` under `helpers/containerwatch/` and the
  declared JavaScript targets. A gitignored file is never read, counted or an error. `git` failing
  (not a work tree, not installed) is `ERROR: cannot list the files git would publish under <root>: <why>`,
  exit 2. *(B3: the bash used a glob over the disk.)*
- E4. **Python targets**: every published file whose name ends `.py` at any depth below
  `helpers/containerwatch/` (nested packages included), in path order. *(B1: the glob was one level
  deep, so a nested module was never judged.)*
- E5. **JavaScript targets** are NAMED, not globbed: `extensions/fedora-desktop@fedora-desktop/containerReport.js`
  and `extensions/fedora-desktop@fedora-desktop/sections/containers.js`. A declared target that is
  not among the published files (missing, or gitignored) is exit 2, with
  `ERROR: declared container-watch JavaScript target is missing: <path>` for each one and a closing
  line saying the gate refuses to pass on a scan that did not cover it.
- E6. No Python target at all is exit 2 (`ERROR: no Python file under helpers/containerwatch/ ...`):
  the watchdog must exist. *(B2: the bash passed on the JavaScript alone.)*
- E7. A judged file that cannot be read (unreadable, a dangling symlink, listed by git but deleted)
  is exit 2 naming it. A symlink to a readable file is read as that file.
- E8. A line ends at `\n`; a last line without one counts; `\r` is part of the line. A NUL byte is an
  ordinary byte: the file is matched as text. *(B13: grep called such a file binary.)*

### The rules

All rules are case-sensitive. A rule is a regular expression matched over the whole file text, so a
call written across lines is seen; the finding is the line where the match starts. A line matched by
several rules is reported once (with the first rule's name, in the order below).

- E9. **Signal rules (every file, containment.py included).** Each is forbidden:
  - `os-kill`: `os.kill(`, `os.killpg(`, `os.abort(` (whitespace allowed before the paren);
  - `os-import-kill`: `from os import ... kill|killpg`, `from signal import ... pthread_kill|raise_signal|SIG...`
    (the bare `kill(pid, 9)` that follows has no `os.` to key on), including the parenthesised form
    that a formatter wraps over several lines, `from os import (\n getpid,\n kill,\n)`;
  - `signal-const`: `signal.SIG<A-Z>`, `signal.raise_signal(`, `signal.pthread_kill(`
    (`signal.SIG_IGN` and `SIG_DFL` are dispositions, not deliveries, and pass);
  - `send-signal`: `.send_signal(` and `.pidfd_send_signal(` (so `os.pidfd_send_signal(`);
  - `force-exit`: `.force_exit(`;
  - `proc-kill`: `.kill(` and `.terminate(` as a method call on any object (Popen, asyncio
    process, psutil, a GJS subprocess). *(B6: the bash had no rule for these.)*
  - `argv-kill`: a quoted `kill`, `pkill` or `killall` as the first element of a list or tuple
    (`["kill", ...]`, `('pkill', ...)`, also on the next line after the bracket) or as any element
    of an argv list (`["sudo", "kill", pid]`);
  - `shell-kill`: the word `pkill` or `killall` followed by a space or tab (`\bpkill `), and
    `kill` followed by blanks and `-` (`kill -9`); same line only.
  - `unit-kill`: ending a service or machine, which is the termination route of the watchdog's own
    domain (transient systemd units): an argv `"systemctl"` or `"machinectl"`, any options between
    (`--user`, `--machine x`), then `stop` or `kill` (systemctl), `terminate` or `kill` (machinectl).
    Not matched, by design: `kill` or `pkill` in prose without those shapes (`kill <pid>`), `skill -`,
    `signal.signal(`, `"--signal"` in an unrelated argv (a `dbus-send` argument is legal outside
    containment.py).
- E10. **Lifecycle rules (every file except the one exception).** An argv that ends a container:
  an engine program, then optionally `"container"`, then one of `stop kill rm pause unpause restart`,
  all as quoted elements:
  - `engine-literal`: program `"podman"` or `"docker"`, or a path ending `/podman` or `/docker`;
  - `engine-variable`: a variable or attribute whose name ends in `engine`, in any case (`engine`,
    `container_engine`, `self.engine`, `ENGINE`) followed by `,` and the quoted verb.
    Between the program and the verb any options may stand, each a quoted element starting `-`
    with at most one quoted value after it (`"--remote"`, `"--url", "x"`, `"--connection=c"`), and
    then optionally `"container"`. Whitespace (newlines included) and `#` or `//` comments are
    allowed around the commas, so a commented wrapped argv is seen. Prose that tells a human
    `podman stop <container>` is not an argv and passes. *(B4: the variable form required a space or
    `(` before `engine`, so `[engine, "stop", cid]` passed the bash; its own fixture was green only
    because a second line matched. B7: spacing before the comma, `"container"`, `unpause`, a program path.)*
- E11. **The exception.** Exactly the path `helpers/containerwatch/containment.py` is exempt from the
  lifecycle rules, and from nothing else. It is judged by the signal rules plus these, each forbidden:
  `containment-verb`: a quoted `kill`, `rm`, `pause`, `unpause`, `restart`; `containment-force`:
  `--force` and a quoted `-f`; `containment-signal`: `--signal` (it turns a graceful stop into a
  kill); `containment-timeout`: a stop with a zero timeout, which is a kill at once (`--time 0`,
  `-t 0`, `--time=0`, `-t=0`, the zero quoted or not). A file of the same base name elsewhere (`sub/containment.py`) has no exemption: it is
  judged as any other. The file being absent is not an error (nothing to hold); *(B14: the bash did
  the same silently; stated now.)* `build_stop_argv` building `[engine, "stop", "--time", n, id]`
  passes.
- E12. Kill and stop words inside comments are judged like code: a comment that contains a forbidden
  call shape (`# os.kill(pid, 9)`) is a finding; the cure is to reword it. (Stripping comments would
  need a Python and JavaScript lexer; a string holding `#` would hide a call. Kept as the bash had it.)

### Output

- E13. The gate judges everything before it answers: all findings of all files are collected, not the
  first file's. *(B8: on a containment failure the bash returned before scanning any other file.)*
- E14. A finding is printed to stderr as `<relative path>:<line>: (<rule>) <line text>`, in path
  order then line order. *(B12: the bash printed absolute paths, a line once per matching pattern, no
  rule name.)* A violation ends with the summary
  `✗ no-kill gate: <N> forbidden call site(s) in <M> file(s)` and guidance on stderr, exit 1.
- E15. Clean: stdout carries `✓ containment.py: stop-only confirmed (the one audited exception)` when
  that file exists, then
  `✓ no-kill gate: <N> container-watch file(s) clean — no process-termination call site`
  where N counts every file judged, containment.py included. *(B10: the bash counted without it.)*
  The qa-all reader matches `[0-9]+ container-watch file[(]s[)] clean`.

## 3. Departures from the bash

| Id  | The bash                                                                                           | Now                                                  |
| --- | -------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| B1  | Python glob one level deep                                                                         | recursive (E4)                                       |
| B2  | passed with JavaScript and no Python                                                               | exit 2 (E6)                                          |
| B3  | scanned the disk, gitignored files included                                                        | what git would publish (E3, E5)                      |
| B4  | `[engine, "stop"` not matched (class lacked `[`)                                                   | matched (E10)                                        |
| B5  | single-line only: `subprocess.run([\n "kill"` or a wrapped argv passed                             | whole-file match, multi-line argv seen (E9, E10)     |
| B6  | no rule for `.kill(`, `.terminate(`, `from os import kill`, `killall`, `["sudo", "kill"]`          | rules added (E9)                                     |
| B7  | lifecycle: no space before comma, no `container` subcommand, no `unpause`, bare program names only | all covered (E10)                                    |
| B8  | containment failure returned before scanning anything else                                         | everything judged, all reported (E13)                |
| B9  | `--signal` allowed in containment.py                                                               | forbidden there (E11)                                |
| B10 | file count excluded containment.py                                                                 | included (E15)                                       |
| B11 | `--self-test` flag                                                                                 | unit tests; flag is an unknown argument (E1)         |
| B12 | absolute paths, duplicate lines per pattern                                                        | relative path, one line per finding, rule name (E14) |
| B13 | grep "Binary file matches" for a NUL-bearing file                                                  | text (E8)                                            |
| B14 | missing containment.py skipped without a word                                                      | stated as allowed (E11)                              |
| B15 | an unreadable file: grep rc 2 aborted mid-scan                                                     | exit 2 naming it (E7)                                |

Stated limits (review of this port, F7 and F8): `kill` followed by a space and no dash in a shell
string (`"kill %d" % pid`, `f"kill {pid}"`, a JavaScript template) is not matched, because it cannot
be told from prose by text, and `killall5` is not a name the rules know. Neither is a departure
from the bash, which had the same holes.

Known limit (not a departure): the rules see text, not syntax. `getattr(os, "ki" + "ll")`, a command
built by string concatenation, or `eval` evade any text rule; the watchdog's own review keeps those
out. The gate catches the accidental and the careless, not the adversarial.

## 4. Seams

The nested `src-go/qa` module's one process seam `qa/internal/proc` runs `git`. The package
`qa/nokill` is pure except for the injected `Env` (streams, working directory, `Open` for the file
system, `Published` for the file list). Command: `src-go/qa/cmd/qa-nokill-containerwatch`; wrapper
`scripts/qa-nokill-containerwatch.bash` via `scripts/lib/go-tool.bash`.
