# qa-deployed-drift: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md). **Method: the legacy bash is not an oracle.** This
states the intended behaviour as testable expectations; `scripts/qa-deployed-drift.bash` (165 code
lines) was read only to find the requirements. Where it was wrong the expectation says the right
behaviour and a note says what the bash did. The proof is the Go tests in
`src-go/qa/deployeddrift` (one or more per expectation, cited by number) and a run over the live
tree at cut-over.

## 1. Role

The one QA gate whose subject is the host, not the source tree: repo-owned files that are
deployed by a play are compared byte for byte with their deployed copies, so a repo change that
was never deployed (Plan 00094's `ftp-camera`) is a failure. Callers: `qa-all` (row `drift deployed-drift` in `scripts/qa-gates.tsv`: its own output is the stage line, which must start
`✓ ` or `⚠ ` then `deployed-drift: `), `qa-changed` (per-file gate, via the same row), people.

The gate has two halves with different dependencies:

- **Source-tree half** (E4-E6): facts about the repo only. Runs everywhere, before any host check,
  so a container or CI checkout still verifies it.
- **Host half** (E7-E16): needs the deployed copies; skipped only for the three reasons in E7-E9,
  each printed with `⚠` and exit 0.

## 2. Expectations

### Command line and environment

- E1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory and the
  root is its parent (the wrapper passes this); neither: the working directory. The root is
  normalised once, before anything else uses it: made absolute against the working directory,
  cleaned, then symlinks resolved (a `--scripts` value is cleaned first, so a trailing slash
  cannot make the parent the directory itself). A root that cannot be resolved is exit 2 naming
  it. Nothing later depends on how the path was spelled. `-h`/`--help`
  prints usage to stdout, exit 0. Any other argument is exit 2 with `ERROR: unknown argument '<arg>'` and the usage on stderr; a flag missing its value is exit 2. *(Bash ignored arguments.)*
- E2. Exit codes: 0 in sync or skipped with a reason, 1 drift found, 2 the run could not complete.
- E3. stdout is the stage line (a pass or a skip, plus the template lines); drift findings and the
  failing summary go to stderr; an error that stops the run is `ERROR: ...` on stderr.

### Source-tree half

- E4. `files/home/.local/bin` missing or not a directory is exit 2, `ERROR: source directory not found: <path>`. `playbooks/` missing is exit 2 likewise. *(Bash: a missing playbooks/ made the
  grep fail and read as "no playbook", the same exit 2 by accident and with the wrong message.)*
- E5. Every regular file `NAME.j2` directly under `files/home/.local/bin` (not recursive, names
  beginning with `.` ignored, a symlink to a file counts; a dangling symlink, here or in a pair
  directory of E10, is exit 2 `ERROR: <path> is a dangling symlink (its target does not exist); remove it or fix its target`, a repo defect, where the bash skipped it) must be deployed by a playbook `dest:`
  line (a `*.yml` under `playbooks/`) that names `.local/bin/NAME` (the `.j2` stripped) followed
  by a quote, a space or the end of the line. A template with none is exit 2:
  `✗ deployed-drift: NAME.j2 has no playbook dest: deploying it as 'NAME'` and the explanation.
  The templates found are counted and listed in the pass and skip lines.
- E6. The extra pair table (E10) is checked here too: the named deploying play must exist, and a
  pair whose repo glob matches no file is exit 2 (`pair <glob> matches no repo file`), because a
  pair that compares nothing is a pass over nothing. *(Bash: silent; it would pass for ever after
  a rename.)*

### Skips (host half), each exit 0, mark `⚠`

- E7. This is a container: one of the container markers `/run/.containerenv` or `/.dockerenv`
  exists on the host file system (the same two files as `PLAN_CONTAINER_MARKERS` in
  `CLAUDE/Plan/_planlib.inc.bash`, so one rule says "container"). The spelling of the root is not
  consulted, and a host checkout that happens to sit at `/workspace` is compared, not skipped. A
  stat error other than not-exist on a marker is exit 2. A container has no deployed state:
  `⚠ deployed-drift: skipped (CCY container — no deployed copies to compare); N template(s) verified to map to a playbook dest:`.
  *(Bash and the first Go port keyed on the root being exactly `/workspace`.)*
- E8. The root is a linked git worktree (git dir differs from the common git dir): `⚠ deployed-drift: skipped (linked git worktree — not the deployed checkout); N template(s) ...`.
  If git cannot answer (not a repository, git missing) the run is exit 2 with the reason.
  *(Bash: any git failure fell through to comparing, so an unreadable checkout was compared as if
  it were the deployed one.)*
- E9. `$HOME/.local/bin` is not a directory: `⚠ deployed-drift: skipped (<path> does not exist); N template(s) ...`. HOME is cleaned first (a trailing slash or `//` is accepted); HOME unset or empty
  is exit 2 (`ERROR: HOME is not set`) and a relative HOME is exit 2 (`ERROR: HOME is not an absolute path`), never silently treated as no directory. HOME is read only here, after E7 and E8
  have been decided, so in a container or a linked worktree an unset HOME is a skip, not an error.
  A stat error other than not-exist is exit 2.
  Order: E4-E6, then E7, E8, E9.
  Every skip line states the template count; nothing else is compared.

### Comparison

- E10. Compared pairs: (a) each regular non-`.j2` file directly under `files/home/.local/bin`
  against `$HOME/.local/bin/<name>`, owner play found by E13; (b) the extra table, each entry a
  repo glob, a deployed directory and the deploying play. **The set of entries is the `pairs`
  table in `src-go/qa/deployeddrift/run.go` and only there**: this spec does not copy it, so it
  cannot drift from the code. It covers the VM test lab files (deployed by
  `play-vm-test-lab.yml`, under HOME and `/usr/local/lib/ccy-helpers`) and the agent bus files
  (`play-agent-bus.yml`, under `/usr/local` and `/etc/systemd/system`). A
  glob `*` matches regular files only (directories skipped, a symlink to a file counts) and not
  names beginning with `.`, as the shell does. `request.py` is excluded from the helpers entry
  only: the play deliberately never deploys it. *(Bash excluded that name in every entry.)*

- E11. A file whose deployed copy does not exist (a regular file, symlinks followed) is "not
  installed on this host": counted, never a failure, never compared. A machine that never
  installed a feature is not nagged. The count is always stated (E14). **Stated limit:** a file
  newly added to a play that was last run before the addition reads, on that host, as "not
  installed", never a failure: the gate cannot tell "feature not installed" from "feature
  installed, play stale" without a per-play signal. (A path that exists but is not a regular
  file is not "not installed": E17.)

- E12. A file with a deployed copy is compared byte for byte: equal is checked-in-sync, unequal is
  drift. A deployed copy that exists but cannot be read, or a repo file that cannot be read, is
  exit 2 naming the path and the error. *(Bash `cmp -s` failing for any reason, an unreadable
  file included, was reported as drift, sending the reader to deploy a file that was not the
  problem.)*

### Naming the play

- E13. For a drifted `files/home/.local/bin` file the finding lists the play(s) to run. Tier 1:
  every `*.yml` under `playbooks/` with a line holding `src:` then `files/home/.local/bin/NAME`
  followed by a quote, a space or end of line (NAME matched literally, so `.` is a dot and
  `ftp-camera` does not match `ftp-camera-foo`; a comment line that merely mentions the path is
  not a declaration). If none, tier 2: every `*.yml` that has a line with `files/home/.local/bin/{{`
  (a templated src) and also contains NAME as a whole word (not preceded or followed by
  `[A-Za-z0-9._-]`). If none: `(no playbook references this file — it may be orphaned)`. All
  plays listed sorted. *(Bash tier 2 used a bare substring, so `lan` matched `lan-scan`'s play;
  its order was the directory order.)* Extra-table drifts name the table's play (E10).

### Output

- E14. Drift (exit 1): stderr gets `✗ deployed-drift: repo changes that were never deployed`, a
  blank line, then per drifted file `  <repo-relative path>`, `    repo:     <path>`,
  `    deployed: <path>`, an optional `    problem:  <why>` (E17, E18), `    deploy it with:` and one `    ansible-playbook <play>` per play, a
  blank line; then the two-line advice (`The repo and the host disagree...`, `see CLAUDE/InfrastructureAsCode.md`), a blank line, and the summary `✗ deployed-drift: D of C deployed script(s) differ from the repo; U not installed on this host; T template(s) not byte-comparable`. Drift is listed in a stable order: the bin files by name, then the extra table
  in table order, each by name.
- E15. In sync (exit 0): stdout gets `✓ deployed-drift: C deployed script(s) match the repo; U not installed on this host; T template(s) not byte-comparable`, then one line `    template (rendered, so not compared): NAME.j2 -> NAME` per template, sorted. All three counts are stated
  even when zero. `C` counts every compared file (the bin files and the extra table).
- E17. A deployed path that exists but is not a regular file, a directory or a dangling symlink,
  is drift, not "not installed" (no play can have produced it; it is a wrecked deploy). It is
  listed like any drift with an extra line `    problem:  deployed path is a directory, not a file` (or `... a dangling symlink, not a file`), and counts in C. Only a path that is absent
  (`Lstat` not-exist) is "not installed" (E11).
- E18. Mode: a `files/home/.local/bin` script whose bytes match is also drift when the owner
  execute bit of the deployed copy differs from the mode the play sets: the task that names the
  file as `src:` (E13 tier 1) is read for a `mode: "0NNN"` line, the first play that states one
  wins. `    problem:  deployed copy is not executable but <play> sets mode 0755` (or `is executable but ... 0644`). Chosen over a full-mode comparison because the plays do not set modes
  consistently (scripts 0755, but sourced libraries deliberately 0644, and the repo's own file
  mode is not authoritative: git records 644 for scripts a play installs 0755); the exec bit is
  the one bit where the play's intent is unambiguous. No declared mode (a loop, a templated
  `src:`, tier 2) is not checked. The extra table (E10b) is not mode-checked, and ownership is
  never compared. Mode is checked only when the bytes already match, so a byte drift is reported
  once.
- E16. `.j2` templates are never byte-compared (the rendering differs by design); they are
  disclosed, not skipped silently.

## 3. Seams (section 4 of the method)

The package takes a repo `fs.FS`, a host `fs.FS` rooted at `/` (also where the E7 markers are
looked for), HOME, the working directory, a symlink resolver for the root (E1) and a
linked-worktree function; the command wires `os.DirFS`, `os.Getenv` and a git
call through `qa/internal/proc`. Tests use temp trees and fakes only; paths in tests and docs are
placeholders (`/home/user`, `/repo`).
