# qa-version-pins: specification of the Go port

Same method as [SPEC-qa-plan-script-logging.md](SPEC-qa-plan-script-logging.md): **the legacy bash
is not an oracle.** This states the *intended* behaviour as numbered, testable expectations;
`scripts/qa-version-pins.bash` (146 code lines, four self-controls, a Python validator behind a
PyYAML conversion) was read only to find the requirements. Where it was wrong the expectation says
what is right and section 3 lists the departure. Proof is the Go tests in `src-go/qa/versionpins`
(cited by number), one on the real file system in `src-go/qa/cmd/qa-version-pins`, and a run of the
tool over the live tree at cut-over.

## 1. Role

`vars/version-pins.yml` says where every pinned upstream version lives (a playbook and the var that
holds it) and what upstream it tracks. Two consumers read it, `scripts/check-pinned-versions.bash`
(pin against upstream) and the installed-against-pinned check (Plan 00109), and neither runs in
`qa-all`: one needs an authenticated `gh`, the other a real host. Without this gate a manifest that
had drifted from the playbooks surfaces only when somebody runs a review tool. The gate judges two
things, and the second is the one that rots: the manifest's own shape, and that every row still
points at a file that exists and still declares its var (a renamed var would report the old value for
ever). Callers: `qa-all` (`scripts/qa-gates.tsv`, gate `version-pins`, kind `detail:`, reading the OK
line) and `qa-changed` (check `version-pins`, whole). The tool reads files and starts no process.

The manifest's row shape is also enforced, for the host-side consumers, by
`helpers/version_pins/manifest.py` (kept: those consumers and `tests/helpers/version_pins` use it).
This gate no longer calls it; the two validators state one shape (see Stated limits).

Exit codes: 0 clean; 1 the manifest or a row is wrong (a content finding); 2 the gate cannot judge
(bad usage, a file it could not read, a file it could not parse, nothing to judge).

## 2. Expectations

### Command line, streams, exit codes

- V1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the scripts
  directory and the root is its parent (the wrapper passes this); neither: the working directory.
  `-h`/`--help` prints the usage to stdout, exit 0. Any other argument, or a flag missing its value, is
  exit 2 `ERROR: ...`, the usage on stderr for an unknown argument.
- V2. Every exit-2 message is on stderr and starts `ERROR:`. A failed write to stdout is exit 2
  `ERROR: cannot write to stdout: <why>`. Exit 1 prints nothing on stdout.
- V3. The root is resolved by `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is
  exit 2). Paths printed are repository-relative; no checkout path reaches either stream.

### The manifest is read and parsed (exit 2 when it cannot be)

- V4. The manifest is `vars/version-pins.yml`. Absent, unreadable, a directory or a symlink is exit 2
  naming it (a link is how the file would leave the repository).
- V5. It is parsed by a YAML library (`go.yaml.in/yaml/v3`, node API). A file that is not YAML, or
  that holds more than one document, is exit 2 naming the file and the parser's message.
- V6. Nothing to judge is exit 2 `ERROR: vars/version-pins.yml declares no pins; nothing was checked`:
  an empty document, no `version_pins` key, a null value, or an empty list. A gate that passes having
  checked nothing is the failure this plan exists to prevent.

### The manifest's shape (exit 1; every finding is reported, none stops the others)

A finding is `vars/version-pins.yml:<line>: <what>` and names the row as `version_pins[<i>]`.

- V7. The document is a mapping whose only key is `version_pins`; the value is a sequence; each row is
  a mapping. Anything else is a finding.
- V8. A mapping with a repeated key (the manifest's rows, an `installed` block, the top level) is a
  finding naming the key. An alias is followed; the merge key `<<` is an unknown key.
- V9. A row's keys are among `playbook`, `var`, `github`, `tag_prefix`, `note`, `installed`; any other
  key is a finding (a typo'd `githug:` would demote an automated pin to a manual one nobody queries).
- V10. `playbook` and `var` are required and not empty after trimming. Every other string field
  (`github`, `tag_prefix`, `note`) is a string or null (null reads as empty). A non-string scalar or a
  collection in a string field is a finding naming the field and its kind. Values are trimmed.
- V11. No string field contains `|`, a newline or a carriage return: `check-pinned-versions.bash` reads
  rows as pipe-delimited lines, where such a character silently produces a different row.
- V12. `github`, when present, is `owner/repo` (`[A-Za-z0-9._-]+/[A-Za-z0-9._-]+`), nothing else.
- V13. A row with no `github` has a `note` saying how to check it by hand.
- V14. A `(playbook, var)` pair is declared once; the second is a finding naming the first's row.
- V15. Every row has an `installed` block (silence is not a decision): a mapping with keys among
  `kind`, `name`, `why`; `kind` is one of `dkms`, `rpm`, `command`, `untracked` (never defaulted);
  `untracked` needs a `why`; the other three need a `name`.
- V16. A tracked pin is one whose `kind` is `dkms`, `rpm` or `command`. With **no** tracked pin the
  installed-against-pinned check compares nothing and cannot fail: a finding naming the pin count,
  reported alongside any other finding (a row with another finding still counts as tracked).

### Every row still resolves (exit 1 for a wrong row, exit 2 for a file the gate cannot read)

- V17. `playbook` is a repository-relative path in `io/fs` form: not absolute, no `..` element, no
  empty or `.` element. Anything else is a finding; the file is not opened.
- V18. The named file must exist (a finding `names a playbook that does not exist`), be a regular file
  (a directory or a symlink is a finding: a link's target can lie outside the tree), and be readable (a
  read error is exit 2 naming the file).
- V19. The var is declared in the form the consumer reads (`helpers/version_pins/check_pins.py pinned_value`): a line matching `^\s*<var>\s*:(.*)$`, the var taken **literally** (not as a regex),
  the first such line used. A file with no such line is a finding `names '<var>', which <file> no longer declares`. A line whose value, trimmed of blanks and quotes, is empty is a finding `declares '<var>' with no value` (the consumer would compare against an empty version).
- V20. A file named `*.yml` or `*.yaml` is also parsed as YAML (same library); a file that does not
  parse is exit 2 naming the file. The var must also be a mapping key, at any depth, whose value is a
  scalar; if it is not (the text of the var appears only in a block scalar or a comment-like string, or
  its value is a collection) it is a finding. A file of another name (the `tuwunel.pin` line form) is
  judged by V19 alone.
- V21. Each distinct file is read once however many rows name it.

### Coverage and output

- V22. Coverage is **counted, not declared**: the number of rows that reached the on-disk check is
  held against the number of pins declared. They differ only through a defect (a row skipped), and then
  the gate is exit 2 `ERROR: <D> pin(s) declared but <R> reached the on-disk check`. The counts printed
  are the ones counted.
- V23. Clean: stdout is `vars/version-pins.yml: VERSION-PINS-OK <N> pin(s), <T> with install state tracked, <U> declared untracked — COVERAGE: <N> of <N> resolve to a live playbook var`, exit 0. The
  qa-all `detail:` reader matches `vars/[a-z-]+[.]yml: VERSION-PINS-OK .+`.
- V24. A finding run prints each finding to stderr (indented two spaces) under
  `ERROR: <F> version-pin finding(s) across <N> pin(s):`, sorted by line then text, exit 1. If any
  problem of exit 2 occurred it is printed too, and the exit is 2.
- V25. The gate's own controls are Go tests, not run-time work: an empty manifest is exit 2 for being
  empty (not for some other reason), a row at a missing file and a row naming an undeclared var fail
  for **different** reasons (V18, V19), and the row verdict names its reason on stderr (V24).

## 3. Departures from the bash

| Id  | The bash                                                                                           | Now                                                                         |
| --- | -------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| B1  | converted with PyYAML `safe_load`: a repeated key kept the last silently                           | repeated keys are findings (V8)                                             |
| B2  | an empty manifest was exit 1 (a finding)                                                           | exit 2: nothing to judge (V6)                                               |
| B3  | a manifest that is not YAML was exit 1                                                             | exit 2: the gate could not read it (V5)                                     |
| B4  | the var went into `grep -E` unescaped: `a.b` matched `axb`, a metacharacter could error            | taken literally (V19)                                                       |
| B5  | a var declared with an empty value passed                                                          | a finding (V19)                                                             |
| B6  | a var found by text only: a line inside a block string counted                                     | a `.yml` target must hold the var as a parsed key with a scalar value (V20) |
| B7  | the playbook path was opened as given: absolute and `..` paths, symlinks and directories were read | refused (V17, V18)                                                          |
| B8  | the first bad row stopped the run                                                                  | every finding is reported (V7-V19, V24)                                     |
| B9  | the top level could hold any other key                                                             | only `version_pins` (V7)                                                    |
| B10 | four throwaway self-controls run on every invocation                                               | Go tests (V25); a run is not slowed to repeat them                          |
| B11 | the counts were parsed back out of a marker line with `awk` and compared                           | counted once where the rows are resolved (V22)                              |
| B12 | arguments ignored                                                                                  | an unknown argument is exit 2 (V1)                                          |
| B13 | no tracked pin: exit 1 with the message; unchanged in effect                                       | kept (V16), one of the findings                                             |

## 4. Stated limits

- The shape of a row is stated twice: here (Go, for the gate) and in `helpers/version_pins/manifest.py`
  (Python, for the host-side consumers). `TestGoAndPythonAgreeOnTheShapeOfTheManifest` in
  `src-go/qa/cmd/qa-version-pins` pins the two to the same verdict over a table of manifests (the
  good one and one per shape rule) and over the live manifest. The rules only the Go enforces, B9
  (other top-level keys) and V16 (a tracked pin), are named in that test, which asserts the Python
  accepts them so a rule later added to the Python is noticed. A repeated key (V8) cannot reach the
  Python, since the YAML library will not decode it into JSON; the test asserts that too.
- Only the first `var:` line is read, as the consumer does; a later declaration of the same var in the
  same file is not compared with it.
- `qa-changed` runs the gate whole when the manifest, a playbook under `playbooks/`, the
  `tuwunel.pin` file under `files/usr/local/share/agent-bus/`, or the gate's own script, Go package
  or command changes or is deleted. A pin may name any repository file, so a change to a file the
  manifest names that is outside those paths (a `tasks/` or other `vars/` file) is judged by
  `qa-all`.
