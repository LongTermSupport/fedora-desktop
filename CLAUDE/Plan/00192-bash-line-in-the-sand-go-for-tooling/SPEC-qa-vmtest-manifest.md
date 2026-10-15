# qa-vmtest-manifest: specification of the Go port

Same method as [SPEC-qa-toolchain.md](SPEC-qa-toolchain.md): **the legacy bash is not an
oracle.** This states the *intended* behaviour as testable expectations;
`scripts/qa-vmtest-manifest.bash` (125 code lines, three run-time controls) was read only to
discover the requirements. Where it was wrong the expectation says the right behaviour and a
departure (B) says what the bash did. The proof is the Go tests in `src-go/qa/vmtestmanifest` and
`src-go/qa/cmd/qa-vmtest-manifest` (one or more per expectation, cited by number) and a run over
the live tree at cut-over.

## 1. Role

`vars/vm-test-scenarios.yml` is the source of the VM-acceptance bridge's scenario allowlist.
Ansible renders it to JSON on the host and `helpers/vmtest/validate_manifest.py` validates that
JSON, so a malformed manifest would otherwise be found at deploy time, on the host. The gate
finds it at commit time, in three steps:

1. the manifest, read as YAML, is handed (as JSON) to the same validator the host runs, so there
   is one definition of a valid manifest;
2. every runnable scenario's `planned` equals the `PLANNED` its guest checker declares;
3. every `guest-prepare-<scenario>.bash` fixture belongs to a scenario that reboots before its
   checks (otherwise `vmtest` never runs it), and no guest script is left over for a scenario or
   profile that no longer exists.

Callers: `qa-all` (`scripts/qa-gates.tsv`, gate `vmtest-manifest`, two `detail:` readers on the
stdout lines) and `qa-changed` (check `vmtest-manifest`, a whole gate).

The 1/2 split is the usual one: exit 1 is the repository (the manifest is invalid, a count or a
fixture disagrees, a guest script is orphaned); exit 2 is the gate being unable to judge (an
input unread or unparsed, a tool missing, the command line). `qa-all` fails the gate either way;
`qa-changed` reports exit 2 as "could not run".

## 2. Expectations

Numbers are cited by the tests.

### Command line, streams, exit codes

- V1. `--root DIR` names the repository root; `--scripts DIR` names the scripts directory and the
  root is its parent (the wrapper passes this); neither: the working directory. `-h`/`--help`
  prints the usage to stdout, exit 0. Any other argument beginning `-`, and any positional
  argument (the gate judges the repository's manifest, it takes no PATH), is exit 2 with
  `ERROR: unknown argument '<arg>'` and the usage on stderr; a flag missing its value is exit 2.
  The root is resolved by the shared `qa/internal/rootpath` (absolute, clean, symlink-free;
  unresolvable is exit 2).
- V2. Exit codes: 0 the manifest is valid and agrees with its guest scripts, 1 the manifest or a
  guest script is wrong (V6, V10-V12), 2 the run could not complete (V1, V3-V5, V7, V9, V13,
  V14, V17, a failed write). Every exit-2 message is on stderr and starts `ERROR:`. A failed
  write to stdout is exit 2 with `ERROR: cannot write to stdout: <why>`. All exit-1 findings of
  the cross-checks are reported, not the first.

### The inputs

- V3. `vars/fedora-version.yml` and `vars/vm-test-scenarios.yml` are read under the root. A
  missing, unreadable or non-regular file is exit 2 naming it. Nothing is skipped.
- V4. Both files are parsed as YAML with the shared YAML library, strictly: exactly one document
  (an empty file, a file of only comments or a second document is exit 2), a mapping at the top,
  string keys only, no key twice in one mapping (PyYAML kept the last), and every value
  expressible as JSON (no `.nan`, no `.inf`). A parse failure is exit 2 naming the file and the
  library's reason (line and column when it gives them).
- V5. `fedora_version` in `vars/fedora-version.yml` must be present and a YAML integer greater
  than zero (not a string, not a bool, not a float). Anything else is exit 2 naming the file. It
  is the number the validator composes base names with.

### The validator

- V6. The manifest is converted to JSON and piped to
  `python3 -m helpers.vmtest.validate_manifest --fedora-version <N>`, run in the root, in a
  process group that is killed on timeout, with `PYTHONDONTWRITEBYTECODE=1` (the gate leaves no
  `__pycache__` in the tree). Outcomes: exit 0 with the OK marker (V7) is a valid manifest; exit
  1 with a line `VMTEST-MANIFEST-INVALID` on stdout is an invalid manifest: exit 1 with the
  validator's stderr lines that start `ERROR: ` (and only those; a Python warning is not a
  finding) on stderr as `ERROR: vars/vm-test-scenarios.yml: <reason>`, and the
  cross-checks are not run (their input is not a coherent manifest). Every other outcome (python3
  absent or not startable, a non-zero exit without the INVALID marker, a Python traceback,
  exit 0 without the OK marker, a timeout, a leftover child) is exit 2 naming the cause, with
  the validator's stderr.
- V7. The OK marker is exactly one stdout line
  `VMTEST-MANIFEST-OK scenarios=<n> runnable=<n> bridge=<n> host_only=<n> bases=<n>` (decimal
  integers). The gate prints its counts, so a validator that exited 0 without judging cannot pass
  silently. Any other stdout is exit 2.

### The cross-checks

Both read the manifest as the validator accepted it, and the guest scripts of
`files/home/.local/share/vmtest/`.

- V8. The guest scripts are the files git would publish (tracked, or untracked and not ignored),
  found with the shared `qa/discovery` `shell` list, whose names are `guest-acceptance-*.bash`
  and `guest-prepare-*.bash` directly in that directory. A discovery failure is exit 2 with its
  message. A script present on disk that git would not publish does not exist for this gate.
- V9. A runnable scenario is one whose `planned` is not null; a scenario with `planned: null`
  is not checked (V10). The script that judges a scenario is resolved as `vmtest` resolves it:
  `guest-acceptance-<scenario-id>.bash` when published, else
  `guest-acceptance-<profile>.bash`, the profile being that of the scenario's base. None found
  is a finding (V10). A script that exists but cannot be read, or that the bash parser rejects,
  is exit 2 naming it. The count a script declares is read from its syntax tree (the shared
  `mvdan.cc/sh` parser): the value of the simple assignment `PLANNED=<digits>` among its
  top-level statements (also the same assignment given to any declaration builtin: `readonly`, `declare`, `typeset`, `export`, `local`), never from
  text in a comment, a string, a heredoc or a function. No such assignment, a second one, or a
  value that is not a plain decimal integer literal (`$((...))`, an expansion, a quoted number)
  is a finding.
- V10. Findings, one `ERROR:` line each, in scenario-id order, exit 1:
  `<id>: no guest checker at <path of the profile script>`;
  `<id>: no PLANNED=<n> line in <file>`; `<id>: <file> assigns PLANNED more than once`;
  `<id>: PLANNED in <file> is not a plain integer`;
  `<id>: manifest planned=<m> but <file> declares PLANNED=<n>`. The stdout line on success is
  `vars/vm-test-scenarios.yml: <k> scenario(s) agree with their guest checker`, `k` the number
  checked. No runnable scenario at all (k = 0) is exit 2: the check examined nothing and a pass
  would vouch for nothing.
- V11. For every published `guest-prepare-<id>.bash`: the scenario `<id>` must exist in the
  manifest, and its `reboot_before_checks` must be the boolean `true`, because `vmtest` runs the
  fixture only on the reboot branch. Findings: `<id>: <file> exists but reboot_before_checks is <value>, so the fixture would never run`; `<file> belongs to no scenario in the manifest, so it would be deployed and never run`. The stdout line on success is
  `vars/vm-test-scenarios.yml: <k> scenario fixture(s) run before a declared reboot`; zero
  fixtures is a legitimate repository (a fixture is optional), unlike V10.
- V12. For every published `guest-acceptance-<name>.bash`, `<name>` must be a scenario id or the
  profile of some base of the manifest; otherwise
  `ERROR: <file> is the checker of no scenario and of no base profile, so it would be deployed and never run`.

### Output

- V13. A process the gate starts (python3, git through discovery) that cannot start, runs past
  its bound, or leaves a child holding its output, is exit 2; each python3 run is bounded in time.
- V14. A mapping the validator accepted but whose shape the cross-checks cannot read (a scenario
  that is not a mapping, a `planned` that is not an integer or null) is exit 2 naming the
  scenario; it is the validator and this gate disagreeing, not a finding.
- V15. On success stdout is exactly three lines, in this order, and stderr is empty:
  `vars/vm-test-scenarios.yml: scenarios=<n> runnable=<n> bridge=<n> host_only=<n> bases=<n>`
  (the validator's counts), then the V10 line, then the V11 line. The `qa-all` `detail:` readers
  take the first two. On any exit 1 or 2 nothing is written to stdout.
- V16. No checkout path reaches stdout or stderr: the checkout path is taken out of the
  validator's text, and every file is named relative to the root.

## 3. Departures from the bash

| Id  | The bash                                                                                                                              | Now                                                                                         |
| --- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| B1  | converted with `python3 -c 'yaml.safe_load'`: a duplicate key silently kept the last, a second document was an error only by accident | the Go YAML library, strictly: duplicate keys, a second document and `.nan` are exit 2 (V4) |
| B2  | three run-time controls fed a deliberately broken manifest to the validator and to each cross-check, every run                        | the controls are Go tests (V6, V10, V11); each run asserts the OK marker's grammar (V7)     |
| B3  | `fedora_version` found by `grep` and `awk`, so a quoted number or a commented line matched by accident                                | read from the parsed YAML as an integer (V5)                                                |
| B4  | `PLANNED` found by a regular expression over the text: a comment or a string at column 0 counted, a second assignment was ignored     | read from the bash syntax tree; exactly one top-level assignment (V9)                       |
| B5  | a checker that exists on disk counted, tracked or not                                                                                 | only a file git would publish counts (V8)                                                   |
| B6  | a `guest-prepare-*` or `guest-acceptance-*` file with no scenario was deployed and never run, unnoticed                               | refused (V11, V12)                                                                          |
| B7  | zero runnable scenarios printed `0 scenario(s) agree`                                                                                 | exit 2 (V10)                                                                                |
| B8  | python3 absent, a validator traceback or exit 0 without its marker was a bash `set -e` exit 1 or an `ERROR` line                      | exit 2 naming the cause (V6, V13)                                                           |
| B9  | the cross-checks ran after the validator even on an invalid manifest                                                                  | not run on an invalid manifest (V6)                                                         |
| B10 | PyYAML was a dependency of the gate                                                                                                   | none: the YAML is read in Go                                                                |
| B11 | no bound on a run, output unscrubbed                                                                                                  | bounded, checkout path removed (V13, V16)                                                   |
| B12 | the pass lines were printed as each step finished, so a later failure left partial stdout                                             | stdout only when the whole run passed (V15)                                                 |

The validator itself stays Python: `helpers/vmtest/scenarios.py` is what the host runs at deploy
time, and re-implementing it in Go would put two definitions of a valid manifest in the
repository. The gate therefore needs `python3`; a host without it fails V6 loudly.

## 4. Seams

Package `qa/vmtestmanifest` takes an injected `Env` (streams, working directory, a
`proc.Runner` for python3 and, through `qa/discovery`, git). The repository is read from the file
system under the root. Command: `src-go/qa/cmd/qa-vmtest-manifest`; wrapper
`scripts/qa-vmtest-manifest.bash` via `scripts/lib/go-tool.bash`. The python3 run needs the
process's standard input, which `qa/internal/proc` gains as `Cmd.Stdin`.

Routing (SPEC-qa-changed B46): the check is a whole gate. It runs when the manifest (covered),
`vars/fedora-version.yml`, the validator (`helpers/vmtest/scenarios.py`, `upstream.py`,
`validate_manifest.py`), a guest script, the wrapper, the Go package or command, the shared
discovery, the trees table or the wrapper library change; a deleted input owes it.
