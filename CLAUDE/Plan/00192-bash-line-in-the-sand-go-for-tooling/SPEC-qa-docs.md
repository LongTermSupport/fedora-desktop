# qa-docs: specification of the Go port

Same method as [SPEC-qa-toolchain.md](SPEC-qa-toolchain.md): **the legacy bash is not an oracle.**
This states the *intended* behaviour as numbered, testable expectations; `scripts/qa-docs.bash`
(a 175-line wrapper over `helpers/docs/link_check.py`, 684 lines of Python with regular
expressions over Markdown, YAML and TSV text) was read only to find the requirements. Where it
was wrong the expectation says what is right and section 3 lists the departure. Proof is the Go
tests in `src-go/qa/docs` (cited by number), a real-git, real-file-system test in
`src-go/qa/cmd/qa-docs`, and a run of both tools over the live tree at cut-over.

## 1. Role

The documentation integrity gate of Plan 00070. Four checks, each of which would have caught a
real defect in that plan's hand audit:

1. every relative link target exists and is tracked, and every `#anchor` matches a real heading;
2. every playbook `playbooks/playbook-main.yml` imports is named in both `docs/playbooks.md` and
   `docs/architecture.md`;
3. every `CLAUDE/*.md` topic file has a row in `CLAUDE.md`;
4. every gate `scripts/qa-gates.tsv` runs has a row in the tables of `CLAUDE/QA.md`, and every row
   there is a gate that runs.

Callers: `qa-all` (`scripts/qa-gates.tsv`, kind `stage`: the report is merged, the output streams
live, exit 2 aborts the run with the row's text) and `qa-changed` (check `docs`, whole, run through
the wrapper with the report). Scope is the **core** documents only: `CLAUDE/Plan/**` is excluded on
principle, because a core gate that sweeps plan content makes core CI depend on plan content, so
archiving a plan could flip the core verdict without a core file changing.

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes, report

- D1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory (the
  trees table `qa-trees.tsv` is read there) and the root is its parent; neither: the working
  directory. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument beginning `-`,
  and any positional argument, is exit 2 with `ERROR: unknown argument '<arg>'` and the usage on
  stderr; a flag missing its value is exit 2. The root is resolved by the shared
  `qa/internal/rootpath` (absolute, clean, symlink-free; unresolvable is exit 2).
- D2. Exit codes: 0 no finding, 1 at least one finding, 2 the run could not complete (D1, D5,
  D6, D16-D21, a failed write). Every exit-2 message is on stderr and starts `ERROR:`. Nothing
  on stdout for exit 2 except what D22 prints before it could fail. A failed write to stdout is
  exit 2 with `ERROR: cannot write to stdout: <why>`; the command refuses to run at all when
  stdout was closed (`ERROR: stdout is closed (>&-)`). A gate that could not look is never exit
  0 or 1: an unread file, an unparsed file and an empty scope are all exit 2.
- D3. The machine report is written to `${QA_JSON_OUT:-/tmp/qa-docs-results.json}` on exit 0 and
  exit 1, never on exit 2 (a run that could not complete leaves no report that could be mistaken
  for a verdict). A report that cannot be written is exit 2. The report is the shape `qa-all` merges
  (D23).

### Scope

- D4. The population is the Markdown files git would publish (tracked, or untracked and not
  ignored, less those gone from the tree), outside the `shell` rows of `scripts/qa-trees.tsv`
  (the shared `markdown` list of `qa/discovery`), narrowed by this gate's own rule: `README.md`,
  `CLAUDE.md`, anything under `docs/`, anything under `.claude/rules/`, anything under `CLAUDE/`
  (the top-level topic files and the rest, **not** `CLAUDE/Plan/`), and any directory's own
  `CLAUDE.md`; and never anything under `.claude/agents/`. A linked worktree, an installed skill, the daemon's
  clone, `untracked/`, `roles/vendor/`, `.ansible/` and `node_modules` are outside because the
  trees table says so (one source), not because this gate lists them again.
- D5. A population of zero documents is exit 2: `ERROR: found 0 in-scope markdown files: ...`.
  A pass over nothing is not a pass.
- D6. Every document in the population, and every `.md` file a link points a `#anchor` into, is
  read. A file that cannot be read is exit 2 naming it. Nothing is skipped.
- D7. The stage line states its denominator: `N files (T tracked)`, T being how many of the N the
  repository tracks. Equal numbers say the machine and CI read the same set; `N > T` names an
  in-scope document nobody committed (scanned here, absent in CI), which is a true finding site
  and not excluded.

### Links and anchors

- D8. Links are read with a CommonMark parser (with GitHub tables), not a regular expression: an
  inline link, a reference link (`[x][ref]`, `[ref]`), with or without a title or an angle-bracket
  destination, anywhere a Markdown block can hold one (paragraph, list, quotation, table cell,
  heading). An image is not a link. Text in a code span or a code block is never a link, whatever it
  looks like. An autolink (`<https://...>`) and raw HTML are not checked.
- D9. A destination with a URL scheme (`https:`, `mailto:`) or a network-path (`//host`) is not
  checked. A destination that begins `/` is a finding (`link is an absolute path: use a path relative to this document`): it points outside what the repository can promise.
- D10. The destination is percent-decoded before use, for the path and for the anchor alike
  (`a%20b.md` is the file `a b.md`). One that is not valid percent-encoding is a finding
  (`link target is not valid percent-encoding`).
- D11. A path resolves against the directory of the document, lexically cleaned. The target must
  exist (`target does not exist`); a path that leaves the repository root is judged the same way
  (absent, or `target is not tracked by this repository`). Existence is asked first: a typo is both
  absent and untracked, and the message that helps is the first.
- D12. A target that exists must be tracked: a tracked file, or a directory that holds one, or the
  repository root. Otherwise the finding is `target is not tracked by this repository`; when a
  `.git` is found on the way up from the target, the target itself included (bounded by the root) the finding adds `— a repository is nested at <dir>; if it is vendored, declare it in the vendored roots (src-go/qa/docs)`. Trackedness, not ignoredness, is the question, because a file on disk that
  was never added passes here and fails in CI.
- D13. Two trees hold another repository vendored into this checkout, and a link into one is
  never a finding: `.claude/hooks-daemon/` and `roles/vendor/` (a path equal to the root, or under
  it; a sibling merely beginning the same letters is not). Each such link is counted: `ok` when
  the root is present and the target exists, `unverifiable` when the root is absent (CI), `broken`
  when the root is present and the target is not; a broken one is listed in the `⚠` block (D22). The
  verdict never depends on what is installed. An anchor into a vendored target is not followed.
  The roots are declared, not detected, and each is also outside the population (D4) so the gate
  never reads another repository's Markdown as its own.
- D14. The tracked set is the shared `tracked-files` list of `qa/discovery`. Directories are
  derived from it; `.` is the root. A repository that tracks nothing is exit 2 (the list is
  empty), never a verdict that every link is untracked.
- D15. A `#anchor` on a `.md` target (the document itself when there is no path) must match a heading of that
  file. Headings are read with the parser: ATX and Setext headings, in any block, never code.
  The slug is the heading's text content (code spans and link text kept; emphasis marks, images and
  raw HTML dropped; escapes and entities resolved), lower-cased, every character other than a
  letter, combining mark, number, underscore, space or hyphen deleted, and every white-space character becoming its
  own hyphen (runs are not collapsed: `A — B` is `a--b`); a heading whose slug is empty defines
  nothing; the second and later headings with a slug get `-1`, `-2`, ...; the anchor is compared as
  `slug(anchor)`. An anchor on a target that is not a Markdown file, or on a directory, is not
  checked. The target is read even when it is outside the population (D6).
- D16. A finding is `{file, line, target, problem}`; the line is the line the link text starts on
  (the first line of the paragraph when the text is empty); `target` is the destination as the
  parser gives it.

### The playbook catalogue

- D17. `playbooks/playbook-main.yml` is parsed as YAML. It must be a sequence; each item that is a
  mapping with an `import_playbook` key (a string) names a playbook by the base name of its value;
  other items (a play with `hosts:`) are not imports. A file that is missing, does not parse, is
  not a sequence, or has an `import_playbook` that is not a string, or an empty one, is exit 2 naming it; one with no
  import at all is exit 2 (`parsed 0 import_playbook entries: discovery is broken, not the tree`).
- D18. Each imported name must be mentioned in `docs/playbooks.md` and in `docs/architecture.md`
  as a whole token: not preceded by a letter, digit, underscore, dot or hyphen, nor followed by
  a letter, digit, underscore or hyphen (`play-a.yml` is not satisfied by `play-play-a.yml`).
  A missing mention is the finding `{file: <doc>, line: 0, target: <name>, problem: core playbook is absent from this document}`. A missing document is exit 2.

### The topic index

- D19. The topic files are the population's `CLAUDE/<name>.md` paths with no further directory.
  None is exit 2 (`found 0 topic files`). Each name must be mentioned in `CLAUDE.md` as a whole
  token (D18's rule). A missing mention is `{file: CLAUDE.md, line: 0, target: CLAUDE/<name>, problem: topic file has no row in the index}`. A missing `CLAUDE.md` is exit 2.

### The gate inventory

- D20. `scripts/qa-gates.tsv` is loaded by the shared strict loader (`qa/gates`); a refusal is
  exit 2 carrying the loader's message, line number included. A table with no gate is exit 2. Each gate is named as
  `CLAUDE/QA.md` names it: a command that runs a module (`-m helpers.x.y`) by its full dotted
  path, else a `scripts/*.bash` command by its file name; a command that is neither is exit 2
  (`cannot name the gate run by <command>`), because a row the inventory cannot name is a gate it never looked at.
- D21. The documented gates are the first cell of each body row of each table in `CLAUDE/QA.md` (the header row names columns) when
  that cell is one code span of `[A-Za-z0-9._-]+`; the tables are read with the parser, so a
  line in a code fence or in prose that merely looks like a row is not one. A document with no such
  row is exit 2. A gate that runs without a row is `{file: CLAUDE/QA.md, line: 0, target: <name>, problem: gate is run by qa-all.bash but has no row in this document}`; a row for a gate that
  does not run is `{..., line: <the row's line>, problem: this document lists the gate but qa-all.bash does not run it}`. Both directions: a one-way check lets a row stand for a gate that no longer runs.
  The set is derived, never re-enumerated here.

### Output

- D22. On a clean run stdout is the `⚠` block (empty, but built and printed on every run, so the
  formatting is exercised on the common path), then `✓ docs: N files (T tracked) OK (links, anchors, playbook catalogue, topic index, gate inventory) — VENDORED: <ok> verified, <unverifiable> unverifiable (repo absent), <broken> broken`. With findings it is the `⚠` block,
  then `✗ docs: F finding(s) across N files (T tracked) — VENDORED: ...`, then one line per
  finding `  <file>:<line>  <target>  — <problem>` (links in path then line order, then the
  catalogue, topic index and inventory findings in the order of D18-D21), then `  Details: jq '.failures[]' <report path>`. The `⚠` block is `⚠ docs: B link(s) into a PRESENT vendored repo are broken — it has probably moved the file:` and one indented `    <file>:<line>  <target>` line
  each; the detail lines carry no stage symbol in column 0.
- D23. The report is `{type: "docs", status: "pass"|"fail", summary: {total, passed, failed}, vendored: {ok, unverifiable, broken: [...]}, results: [findings], failures: [{file, type: "docs", status: "fail", error: "<target> — <problem>"}]}`; `failures` and `results` are arrays, never null. `failed` is the number of
  distinct files with a finding; `total` is the population plus the distinct finding files that are
  not in it (a finding on `scripts/qa-gates.tsv`), so `passed = total - failed` is never negative.
- D24. No checkout path reaches stdout, stderr or the report: every path is repository-relative,
  whatever spelling of the root the caller used (a symlinked root is resolved once and never printed).
- D25. Every process (git, through the discovery lists) goes through `qa/internal/proc`, bounded by
  a time limit; a process that cannot start, times out or leaves a child behind is exit 2.

## 3. Departures from the bash and its Python checker

| Id  | Before                                                                                                  | Now                                                                                  |
| --- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------ |
| B1  | the population was a file-system walk, so an ignored document was scanned here and absent in CI         | what git would publish, the shared `markdown` list and trees table (D4)              |
| B2  | links by regular expression over lines: reference links, titled links and links in nested blocks missed | a CommonMark parser (D8); headings in lists and quotations and Setext headings count |
| B3  | the destination was used as written                                                                     | percent-decoded; an undecodable one is a finding (D10)                               |
| B4  | an absolute `/path` link resolved against the file system root                                          | a finding of its own (D9)                                                            |
| B5  | catalogue and index by substring: `play-a.yml` satisfied by `play-play-a.yml`                           | whole token (D18, D19)                                                               |
| B6  | `import_playbook` lines found by regular expression, only those whose base name began `play-`           | the YAML parsed; every import is named by its base name (D17)                        |
| B7  | zero imports, zero topic files, zero gates were findings                                                | exit 2: discovery is broken, not the tree (D17, D19, D20)                            |
| B8  | the gate table was split on tabs by hand; QA.md rows by a line regular expression, fences included      | `qa/gates` (strict, the loader qa-all uses); tables read by the parser (D20, D21)    |
| B9  | a stale QA.md row was reported at line 0                                                                | at its own line (D21)                                                                |
| B10 | the summary could go negative when a finding sat in a file outside the population                       | `total` includes such files (D23)                                                    |
| B11 | the checker's crash was told from its findings by a two-hop JSON validation in the wrapper              | one program: exit 2 for any run that could not complete (D2)                         |
| B12 | the topic files were listed from the file system, ignored ones included                                 | from the same population as everything else (D19)                                    |
| B13 | a closed stdout exited 1 or lost the verdict                                                            | exit 2 (D2)                                                                          |
| B14 | the OK line named three checks                                                                          | names the fourth, the gate inventory (D22)                                           |
| B15 | an untracked nested repository's documents vanished from the walk unannounced                           | the shared discovery refuses it (exit 2) unless the trees table excludes it          |

## 4. Seams

Package `qa/docs` is pure except for the injected `Env` (streams, environment, working directory,
`proc.Runner` for the discovery lists, the report writer). The file system is read through the
root, and `Resolve` is the shared `rootpath`. Command: `src-go/qa/cmd/qa-docs` fills them with the
OS; wrapper `scripts/qa-docs.bash` via `scripts/lib/go-tool.bash`. The Python checker
(`helpers/docs/link_check.py` and its unit tests) is deleted: this package is the only
implementation. `scripts/test-qa-docs-exit-codes.bash` drives the built tool over fixture trees,
including the exit-2 trees, and stays as the contract suite.
