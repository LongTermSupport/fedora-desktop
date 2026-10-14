# qa-ansible-pause: specification of the Go port

Same method as [SPEC-qa-nokill-containerwatch.md](SPEC-qa-nokill-containerwatch.md): **the legacy
bash is not an oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-ansible-pause.bash` (96 code lines) was read only to discover the requirements. Where
it was wrong the expectation says the right behaviour and a departure (B) says what the bash did.
The proof is the Go tests in `src-go/qa/ansiblepause` (one or more per expectation, cited by
number) and a run of the tool over the live tree at cut-over.

## 1. Role

The rule `pause-without-register` ([QA.md](../../QA.md#pause-without-register)). A play that stops on
`ansible.builtin.pause` hangs an unattended batch (`run.bash --changed --yes`,
`CLAUDE/Plan/meta-deploy.bash`) on a question nobody knows is owed. A pause task must `register:`
the answer the play goes on to use, or carry a `# PAUSE-OK: <reason>` comment saying why it has
to wait. Caller: `qa-all` (`scripts/qa-gates.tsv`, gate `pause-without-register`, count reader),
people. The scan is text, not a YAML load, because the annotation is a comment and a loader drops
comments.

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes

- E1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory and the
  root is its parent (the wrapper passes this); neither: the working directory. `-h`/`--help` prints
  the usage to stdout, exit 0. Any other argument beginning `-` is exit 2 with
  `ERROR: unknown argument '<arg>'` and the usage on stderr; a flag missing its value is exit 2.
  The root is resolved by the shared helper `qa/internal/rootpath` (absolute, clean, symlink-free;
  unresolvable is exit 2); no checkout path reaches stdout or stderr.
- E2. Exit codes: 0 clean, 1 at least one pause task without register or reason, 2 the run could not
  complete. Every exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is
  exit 2 with `ERROR: cannot write to stdout: <why>`.
- E3. Positional arguments are PATHs: only those files are scanned, instead of the discovered
  population. A PATH is relative to the root, or absolute and inside it once both are resolved through symlinks (a checkout reached by a link); one that escapes the root,
  or cannot be read, is exit 2 naming it.

### Which files are judged; when the run cannot complete (exit 2, never a pass)

- E4. **Population**: the YAML files (`.yml`, `.yaml`) of the shared discovery list `yaml` (what git
  would publish, minus the excluded trees) that lie under `playbooks/` or `tasks/`, at any depth,
  in path order. The discovery failing is exit 2 with its reason.
- E5. A population of zero files is exit 2 (`ERROR: no YAML found under playbooks/ or tasks/; nothing was checked`): a pass would vouch for nothing.
- E6. A file that cannot be read (unreadable, dangling symlink, listed but deleted) is exit 2 naming
  it. A symlink to a readable file is read as that file.
- E7. A line ends at `\n`; a last line without one counts; a `\r` before the `\n` is dropped. Tabs
  are not indent (YAML forbids them): indent is the count of leading spaces.

### The rule

- E8. **A pause key** is a line, outside any other judgement, that reads
  `[spaces][-<one or more spaces>][ansible.builtin.|ansible.legacy.]pause:` (the task's own keys
  then sit at the column after the dash and its spaces) followed by a space, a tab or the line's
  end. `pause` as a word inside prose or a longer key (`ansible.builtin.pause` in a message,
  `pausex:`, `unpause:`) is not a pause key.
- E9. **The task** of a pause key on a `- ` line is that list item: from the dash line down to, not
  including, the next non-blank line whose indent is at or left of the dash. A comment line at or
  left of the dash therefore ends the task. A pause key not on a dash line belongs to the item whose
  head is the nearest earlier line (blank and comment lines skipped) indented less than the key;
  with no such line the key is not judged. The task ends the same way, measured from the head's indent.
- E10. **Registered**: the task has a `register:` key at the task's own key indent (the column after
  `- ` on the dash line, else the pause key's indent), on the dash line itself or below. A
  `register:` nested deeper (a prompt text, an `args:` mapping), or belonging to another task, does
  not count; so a registering task after the pause does not save it.
- E11. **Annotated**: a comment `# PAUSE-OK: <reason>` with at least one non-blank character after
  the colon, on any line of the task, or on the unbroken run of comment lines directly above the
  dash line (a blank line breaks the run). An empty `# PAUSE-OK:` carries no reason and does not
  count. Nothing else (a `PAUSE-OK` in a comment further up) excuses a task.
- E12. A pause task is a finding unless registered (E10) or annotated (E11). One finding per pause
  key, reported at the pause key's line. A pause nested inside a `block:` is judged on its own task,
  not the block's.
- E13. Limit, stated: the rules see lines, not YAML. A line inside a block scalar that itself looks
  like a pause key is judged as one, and a `PAUSE-OK:` comment text inside a prompt counts as
  annotation. Neither is met in the tree; a text rule cannot tell them apart.

### Output

- E14. The tool judges every file before it answers: all findings of all files are collected. A
  finding is printed to stderr as `<relative path>:<line>: pause-without-register`, in path order
  then line order.
- E15. A violation ends with the summary `pause-without-register: <N> pause task(s) in <M> file(s) wait with no register and no '# PAUSE-OK: <reason>'. Use ansible.builtin.debug for status. See CLAUDE/QA.md "pause-without-register".` on stderr, exit 1.
- E16. stdout always carries the count line `passed: <P> failed: <F>` last, P the files scanned with
  no finding, F the files with at least one; the qa-all count reader takes it. A clean run's P is the
  whole population (never 0, E5).
- E17. **The rule proves itself**: the fixtures `tests/fixtures/ansible-pause/flagged.yml` (every
  line ending `# EXPECT-FINDING` is reported, and no other line) and `clean.yml` (nothing reported)
  are checked by the Go tests on every run of the Go gate; a fixture with no `EXPECT-FINDING` line
  fails. The fixtures live outside `playbooks/` and `tasks/`, so the tool never judges them.

## 3. Departures from the bash

| Id  | The bash                                                                                                            | Now                                                                               |
| --- | ------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| B1  | re-ran the fixtures on every invocation, so the gate failed (exit 1) with no pause in the tree if a fixture drifted | the fixtures are Go tests (E17); the tool does no self-test                       |
| B2  | listed by `git ls-files` of tracked files with pathspec globs                                                       | the shared discovery `yaml` list: published files outside the excluded trees (E4) |
| B3  | an unreadable file made awk warn and carry on                                                                       | exit 2 naming it (E6)                                                             |
| B4  | findings and verdict on stdout                                                                                      | findings and verdict on stderr, only the count line on stdout (E14-E16)           |
| B5  | a CRLF file's `pause:` line never matched (the `\r` followed the colon)                                             | the `\r` is dropped (E7)                                                          |
| B6  | PATH arguments were read relative to the working directory root silently                                            | explicit rule, escape and unreadable are exit 2 (E3)                              |
| B7  | the fixtures were required present at run time (exit 1 if missing)                                                  | not needed by the tool (E17)                                                      |

## 4. Seams

Package `qa/ansiblepause` is pure except for the injected `Env` (streams, working directory,
`Resolve`, `Open` for the file system, `Yaml` for the discovery list). Command:
`src-go/qa/cmd/qa-ansible-pause` fills `Yaml` by running the shared `qa/discovery` (`yaml`) over
`qa/internal/proc`; wrapper `scripts/qa-ansible-pause.bash` via `scripts/lib/go-tool.bash`.
