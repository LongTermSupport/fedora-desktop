# qa-ansible-syntax: specification of the Go port

Programme P3 of [INVENTORY.md](INVENTORY.md). **The legacy bash is not an oracle.** This states
the *intended* behaviour as numbered, testable expectations; `scripts/qa-ansible-syntax.bash` (88
code lines, an associative array, five `jq` calls) was read only to find the requirements. Where it
was wrong, the expectation says what is right and section 3 lists the departure. Proof is the Go
tests in `src-go/qa/ansiblesyntax` (cited by number) and a run of the Go tool over the live tree.

## 1. Role

Every playbook the repository holds must parse: `ansible-playbook --syntax-check` stays the engine
(parse only, no task runs, so it is safe in the CCY container). The Go tool owns the rest: which
files are playbooks, the proof that none was missed, the check that the machine can run the engine
at all, and the judgement. Callers: `qa-all` (`scripts/qa-gates.tsv`, stage `ansible-syntax`: run
with `QA_JSON_OUT`, reads `.summary.{total,passed,failed}` and `.failures`; exit 2 aborts the run)
and people. `qa-changed` runs the engine per changed playbook itself (its own check) and does not
call this gate.

## 2. Expectations

### Command line, environment, exit codes

- S1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent (the wrapper passes this); neither: the working
  directory. The root is made absolute and cleaned. `-h`/`--help` prints the usage to stdout, exit
  0\. Any other argument, or a flag missing its value, is exit 2 with `ERROR:` and the usage on
  stderr.
- S2. `QA_JSON_OUT` is the report path; unset or empty is `/tmp/qa-ansible-syntax-results.json`.
  A report that cannot be written: `ERROR: cannot write the report <path>: <why>`, exit 2.
- S3. Exit codes: 0 every playbook parses, 1 at least one does not, 2 the run could not complete.
  Every exit-2 message is on stderr and starts `ERROR:`; no report is written on exit 2. A failed
  write to stdout or stderr is exit 2, and so is a closed stdout (`>&-`).

### What the machine must supply (checked first, fail fast)

- S4. `ansible-playbook --version` must run and exit 0, else `ERROR: ansible-playbook not installed` saying that `run.bash` installs it (pipx), exit 2.
- S5. `ansible.cfg` under the root must exist, else exit 2 naming it. It is pinned as
  `ANSIBLE_CONFIG` (absolute) for every engine run and the engine runs with the root as its
  working directory, so the verdict depends on the root alone, never on where the gate was
  started from (Ansible's own discovery looks at the working directory, and the playbooks'
  `root_dir` lookup of `CONFIG_FILE` crashes at parse time when it finds none).
- S6. Every password file the engine will look for must exist. Which files those are is asked of
  the engine, not parsed from the config: `ansible-config dump --only-changed --format json`
  (config pinned as in S5) reports each setting with the path the engine itself resolved, the
  environment override and relative paths included. Each setting whose name ends in
  `_PASSWORD_FILE` and whose value is a non-empty string must name a regular file with a
  non-empty body (the engine loads the file at start-up and rejects an empty one at every
  playbook), else exit 2: `ERROR: the password file <path> (<setting>) does not exist, is not a file, or is empty ...` saying what supplies it (a clean checkout and a bare `git worktree add`
  lack it) and that the gate never reads it. No such setting: nothing is required. A dump that
  cannot be had (the tool missing, a non-zero exit, text that is not JSON) is exit 2. Without
  this check every playbook fails with the same Ansible message and the report reads as N broken
  playbooks. The tool only asks the file system whether the file is there and how big it is; it
  never opens it.

### Which files: the shared discovery

- S7. A playbook is a YAML file (`.yml`/`.yaml`) with a line that is a list item whose first key
  is `hosts:` or `import_playbook:` (`^\s*-\s+(hosts|import_playbook):`). The second marker
  matters: an entrypoint that only composes other playbooks has no play of its own. The YAML
  files considered are those git would publish (tracked, or untracked and not ignored) outside the
  excluded trees, from the shared discovery (`yaml` list of [SPEC-qa-discovery.md](SPEC-qa-discovery.md)).
  A file that cannot be read is exit 2.
- S8. No playbook found is exit 2 (a broken discovery, not a repository without playbooks: a pass
  would vouch for nothing).
- S9. Coverage proof: every file the shared discovery lists as `tracked-playbooks` must be in the
  found population, else exit 2 listing the missing files and what to do (widen the marker, or
  exempt the file in `src-go/qa/discovery` with a reason).

### The check

- S10. Each playbook is checked with `ansible-playbook --syntax-check <absolute path>`, run in the
  root with `ANSIBLE_CONFIG` pinned (S5) and `ANSIBLE_NOCOLOR=1` (a colour setting of the caller
  must not change the report text). Exit 0 passes. Any other exit fails the playbook; its error is
  the engine's stderr with trailing blank space removed. The engine's stdout is not used. A
  failing engine with nothing on stderr has the error `ansible-playbook exited <n> with no output`.
- S11. The checks run in parallel (4 workers; one playbook takes about a third of a second and
  the population is large) and every playbook is checked, a failure never stopping the rest. The
  results are in path order whatever order they finished in, so the report and the stdout are
  identical from run to run.
- S12. An engine that cannot be started, that exceeds 5 minutes on one playbook, or that leaves a
  process holding its output open is exit 2 naming the playbook (the run did not judge it).

### Output and report

- S13. Each failed playbook, in path order, is printed to stdout as `✗ ansible-syntax: <path>`
  followed by its error text.
- S14. All pass: `✓ ansible-syntax: <N> playbooks OK (<A> under playbooks/imports/, <B> elsewhere)`,
  exit 0. The breakdown makes a change in coverage visible in passing output. Otherwise
  `✗ ansible-syntax: <F>/<N> playbooks failed → <report>`, exit 1.
- S15. The report is `{type:"ansible-syntax", status:"pass"|"fail", summary:{total,passed,failed}, results:[{file,type:"ansible-syntax",status[,error]}], failures:[...]}`, `results` in path order
  with repository-relative paths; `failures` the failing entries (an empty array, not null).
  Written on exit 0 and 1.

## 3. Departures from the bash

- D1. The population is what git would publish, not everything on disk. The bash walked the file
  system (minus the excluded trees) and so also parsed ignored scratch playbooks while other gates
  did not look at them. Every tracked playbook is still covered (S9).
- D2. The vault password file is checked up front (S6); the bash let all the playbooks fail one by
  one with the Ansible message. The pre-existing documented prerequisite is now an exit 2.
- D3. Checks run in parallel (S11); the bash ran them one after another (about 26 s).
- D4. A failing engine with empty stderr has a stated error (S10); the bash recorded an empty one.
- D5. `ANSIBLE_NOCOLOR=1` and the working directory pinned to the root (S5, S10).
- D6. Arguments are parsed (S1); the bash ignored them. A run that cannot complete reports
  `ERROR:` on stderr and writes no report; the closed-stdout and failed-write cases are exit 2.
- D7. Not required, though the task brief assumed it: `environment/localhost/host_vars/localhost.yml`.
  Measured: a checkout without it and with only a vault password file present parses every
  playbook, so requiring it would fail a working gate.
