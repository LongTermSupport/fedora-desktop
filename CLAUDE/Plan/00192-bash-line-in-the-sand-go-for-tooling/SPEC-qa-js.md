# qa-js: specification of the Go port

Programme of [INVENTORY.md](INVENTORY.md), Task 3.3, after [SPEC-qa-python.md](SPEC-qa-python.md).
**The legacy bash is not an oracle.** This states the *intended* behaviour as numbered, testable
expectations; `scripts/qa-js.bash` (102 code lines, a `find` walk, an exclusion test per file, four
`jq` calls) was read only to find the requirements. Where it was wrong, the expectation says what
is right and section 3 lists the departure. Proof is the Go tests in `src-go/qa/js` (cited by
number), and a run of the Go tool and the bash over the live tree.

## 1. Role

The JavaScript gate: every JavaScript file git would publish must parse (`node --check`), and the
GNOME-extension project must pass ESLint under the repository's rules. `node` and ESLint stay the
engines; the ruleset is data (`extensions/.eslintrc.json`, ESLint 8). The Go tool owns what is
checked, the proof that ESLint read what it was given, the pins that stop anything but the
repository's config changing the rules, and the judgement. Callers: `qa-all` (`scripts/qa-gates.tsv`
stage `js`: run with `QA_JSON_OUT`, reads `.summary.{total,passed,failed}` and `.failures`; exit 2
aborts the run) and people. `qa-changed` runs `node --check` and ESLint per file itself, with the
same pins (section 4).

## 2. Expectations

### Command line

- J1. No argument is needed. `--root DIR` names the repository root; `--scripts DIR` names the
  scripts directory and the root is its parent; neither: the working directory. The root is made
  absolute and cleaned once. `-h`/`--help` prints the usage to stdout, exit 0. Any other argument,
  or a flag missing its value, is exit 2 with `ERROR:` and the usage on stderr. `QA_JSON_OUT` names
  the report (default `/tmp/qa-js-results.json`). The root is also symlink-resolved (a root that
  cannot be resolved is exit 2): node prints a file's real path, and J7 and J16 strip and forbid
  the root as the gate spells it.
  The root is resolved by the one shared helper `qa/internal/rootpath` (absolute, clean, symlink-free; a root that cannot be resolved is exit 2), and a symlinked-root test pins that no checkout path reaches stdout or the report.
- J2. Exit codes: 0 no finding, 1 at least one, 2 the run could not complete (a missing tool, a
  failed discovery, ESLint or node failing to run, an ESLint that did not read a file). Exit 2
  writes no report and prints nothing on stdout. Stdout closed (`>&-`) is exit 2.

### Tools

- J3. `node --version` must run, else exit 2: `ERROR: node not installed (install via play-nvm-install.yml)`.
- J4. ESLint is `<root>/extensions/node_modules/.bin/eslint`. It is always required: a missing
  binary is exit 2 before any file is checked, naming the path and the one-time setup
  (`cd extensions && npm ci`), never a skip and never an install. `eslint --version` must print
  `v8.<minor>.<patch>`: the pins of J9 are ESLint 8's, and any other major is exit 2 naming what it
  printed. The config `<root>/extensions/.eslintrc.json` must exist, else exit 2 naming it.

### Which files

- J5. Which files are checked is defined once, by [SPEC-qa-discovery.md](SPEC-qa-discovery.md)
  (D16): the `js` list, the `.js` and `.mjs` files git would publish outside the `shell` rows of
  `scripts/qa-trees.tsv`. This gate adds nothing to that definition. A failed or empty discovery
  is exit 2 with its message after `ERROR: discovery of js failed:` (nothing is checked).
- J6. The ESLint scope is the listed files under `extensions/` (the project that owns the config);
  files elsewhere (`tests/extensions/*.mjs`) are parse-checked only. No file under `extensions/`
  is exit 2 (`ERROR: no JavaScript file under extensions/ to lint`): a lint over nothing must not
  read as a pass.

### Parse check

- J7. Each file is run through `node --check <absolute path>` from the root (node takes one file
  per process). Status 0 passes. A non-zero status is the file's failure, its message being node's
  stderr, trimmed, with the checkout's path never in it. A `node` that cannot start, or times out,
  is exit 2. Stated limit: a `.js` file that uses `import` outside a `"type": "module"` package
  scope passes `node --check` only on node 22.7 or later, which detects module syntax; an older
  node fails it. The nvm play (`play-nvm-install.yml`, `node_version: "lts/*"`) installs the
  current LTS, which has that; no live file is in this position (every `.js` is under
  `extensions/package.json`, `"type": "module"`, and the rest are `.mjs`). The gate does not
  check the node version.

### ESLint

- J8. All files of J6 go to one ESLint process, as absolute paths, from the root:
  `eslint --no-eslintrc --config <root>/extensions/.eslintrc.json --no-inline-config --format json <files>`.
- J9. The pins. `--no-eslintrc` switches off every config lookup: a nested `.eslintrc.*` and a
  `package.json` `eslintConfig` cannot weaken the rules for their subtree. `--config` names the
  repository's. `--no-inline-config` makes `/* eslint-disable */` and its line forms inert, so a
  suppression directive hides nothing (CLAUDE/QA.md bans them). No `eslint-disable` exists in the
  tree today, so no allowlist is needed; there is none, and none may be added without this gate
  being relaxed on purpose. `--no-inline-config` disables every inline directive, not only
  `eslint-disable`: `/* global X */` and `/* eslint-env */` are inert too, and ESLint prints no hint
  that it ignored them (it warns only for `noInlineConfig` set in a config, not for the flag). A
  global or an environment a GJS file needs belongs in `extensions/.eslintrc.json`.
- J10. ESLint's status 0 or 1 is data; 2 or more, no status or a timeout is `ERROR: eslint invocation failed (status N):` with its stderr, exit 2. Its stdout must be a JSON array, else
  exit 2. Status 1 with no finding in the array is exit 2 (the two disagree).
- J11. Proof that ESLint read every file: each given file must be in the array, exactly once, and
  nothing else may be; and none may carry ESLint's "File ignored" warning (an ignore pattern or
  `.eslintignore` dropped it). Any such file is exit 2 naming them: a gate must not vouch for a
  file ESLint never read. ESLint 8 reads `.eslintignore` from its working directory, which J8
  makes the root: a root `.eslintignore` is live (and reaches this exit 2), while one under
  `extensions/`, where the bash ran, is inert (departure B10).
- J12. Every message on a file, warning or error, fails that file (`eslint:recommended` raises only
  errors; a warning that appears is a finding, not noise). A fatal parse error is a message.

### Output and report

- J13. A parse failure is `✗ js: <path>: <message>` on stdout; an ESLint message is
  `✗ js: <path>:<line>:<column>: <message> (<rule>)` (`<rule>` omitted for a parse error), in file,
  line and column order. All paths are relative to the root.
- J14. The last line is `✓ js: <N> files OK (<M> linted by eslint)` for a pass, else
  `✗ js: <F> failing file(s) of <N> → <report path>`. Exit 0 or 1.
- J15. The report is written for exit 0 and 1: `type` `js`; `status` `pass`/`fail`; `summary`
  `{total, passed, failed}` over the checked files; `results` one `{file, type, status, error?}`
  per file in path order (`error` joins the parse message and the ESLint messages with `; `);
  `failures` those that failed; `eslint` `{ran, status, error}` (`ran` true, `status`
  `pass`/`fail`, `error` the count of messages, empty for a pass).
- J16. The check writes nothing into the tree (no ESLint cache, no node output) and the report
  holds no checkout path.

## 3. Where the Go departs from the bash

| #   | Bash                                                                                                                                                                                       | Go                                                                                            | Expectation |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------- | ----------- |
| B1  | `find` over the tree: gitignored files and build output were checked, and `qa_is_excluded` ran once per file in bash                                                                       | what git would publish, from `qa/discovery`                                                   | J5          |
| B2  | `eslint .` in `extensions/`: ESLint 8 lints `.js` only, so `.mjs` there was never linted; the walk also took untracked ignorable files; the claim "every file was linted" was never proved | the listed files, `.js` and `.mjs`, passed by name; each proved read                          | J6, J8, J11 |
| B3  | nested `.eslintrc.*` files and `package.json` `eslintConfig` took part in the rules; `eslint-disable` comments silenced findings                                                           | `--no-eslintrc --config … --no-inline-config`                                                 | J9          |
| B4  | ESLint failure was one blob attributed to no file, and one error in the count                                                                                                              | per file, line and column                                                                     | J12, J13    |
| B5  | `eslint` was required only when `extensions/` existed                                                                                                                                      | always required; no file under `extensions/` is exit 2                                        | J4, J6      |
| B6  | the version of ESLint was not checked                                                                                                                                                      | ESLint 8 only, as the flags are                                                               | J4          |
| B7  | arguments ignored, no usage; stdout could be closed                                                                                                                                        | usage, exit 2 on misuse and on closed stdout                                                  | J1, J2      |
| B8  | the checkout path was in node's messages and the report                                                                                                                                    | relative paths only                                                                           | J7, J16     |
| B9  | output order was `find`'s                                                                                                                                                                  | sorted                                                                                        | J13, J15    |
| B10 | ESLint ran in `extensions/`, so `extensions/.eslintignore` was live                                                                                                                        | ESLint runs from the root: only a root `.eslintignore` is read, and a file it drops is exit 2 | J8, J11     |
| B11 | a root reached through a symlink was used as given                                                                                                                                         | resolved once; unresolvable is exit 2                                                         | J1, J7, J16 |

## 4. Go design and cut-over

`src-go/qa/js` (`run.go` command line and gate, `node.go` the parse check, `eslint.go` the lint,
proof and pins, `report.go`) and a thin `src-go/qa/cmd/qa-js`. Every process goes through
`proc.Runner`; unit tests substitute a fake node, ESLint and git over a real temporary tree, and a
few tests run the real node and ESLint. `scripts/qa-js.bash` becomes the build-and-run wrapper
(`lib/go-tool.bash`) and its allowlist row is removed. `qa-changed`'s `node-check` and `eslint`
checks use the same pins as J8 and J9, and the eslint check the same run and proof (J8-J12, through the exported `js.Findings`; SPEC-qa-changed B37); its routing rows are unchanged, and a change to the gate or
its wrapper reruns the Go gate (`scripts/qa-routing.tsv`).
