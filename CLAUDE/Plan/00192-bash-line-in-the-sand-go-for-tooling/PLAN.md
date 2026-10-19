# Plan 00192: Line in the sand: tooling is Go, bash only for small helpers

**Status**: In Progress (every QA script but `qa-go.bash` ported; that one waits on the owner making go-qa-ci public, Plan 00191 Task 3.3)
**Created**: 2026-10-14
**Owner**: joseph
**Priority**: High

## Overview

The owner's decision (2026-10-14): no more tooling written in bash. Plan 00191 ported
`ccy-teams` and `ccy-sessions` to Go and showed why: bash binds names when a line runs, types
nothing, and its interactive tools can only be tested through a pseudo-terminal. From now on
bash is for very small helpers only; anything over a measured threshold is written in Go in
`src-go/`. The rule is written into the root `CLAUDE.md` and `README.md` and enforced by a QA
gate, with the existing large scripts on a ratchet allowlist that may only shrink.

The existing bash (and then the standalone Python the Go tools call) is inventoried and the
large tools are rewritten in Go, one feature branch per programme. The owner's method
(2026-10-14), from qa-ansible on: a spec of intended behaviour as clear expectations, each a
test written first; the bash is read for requirements and never treated as correct, and no
replay of it is recorded (src-go/CLAUDE.md, "How a port is built"). The earlier ports' replays
stay as regression tests.

go-qa-ci (Plan 00191 Phase 3) is reviewed for completeness, and this repository starts
recording its own Defence Before Fix rules for the defect classes the ports found.

## Goals

- The rule, with pragmatic thresholds, in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md`
  linked from the root `CLAUDE.md`.
- A QA gate in `qa-all.bash` and `qa-changed.bash` that fails a new bash file over the
  thresholds and an allowlisted file that grows; the allowlist only shrinks.
- An inventory of bash and Python with a ranked list of rewrite programmes.
- Each major rewrite on its own feature branch, merged after full QA and a review.
- Project DBF rules for the defect classes found in the ports.
- A Fable adversarial review of the whole and its findings fixed.

## Non-Goals

- Python that Ansible loads in-process (plugins, filters, modules, callbacks) or that imports
  Ansible: it stays Python.
- Bash that must run before Go exists (the bootstrap) or is sourced into a user's shell.
- The `ccy` launcher's port: assessed in Plan 00191 (`ASSESS-ccy-launcher.md`), waiting on
  the owner's decisions D1-D10 there.

## Tasks

### Phase 1: The rule

- [x] ✅ **Task 1.1**: Inventory of tracked bash and Python with sizes, invokers, tests and a
  port/keep class: [INVENTORY.md](INVENTORY.md).
- [x] ✅ **Task 1.2**: Thresholds chosen from the inventory's counts (60 code lines, 90
  statements, 4 functions, no associative arrays, no interactive UI, no data parsing past one `jq -r`); the
  rule in `CLAUDE.md` and `README.md`; `src-go/CLAUDE.md` linked from the root `CLAUDE.md`.
- [x] ✅ **Task 1.3**: The gate, in Go (`src-go/qa/cmd/qa-bash-limits`, `src-go/qa/bashlimits`), with its
  ratchet allowlist `scripts/bash-limits.allowlist` (records only move down; `--base` refuses
  an added entry or a raised record), wired into `qa-all.bash`, `qa-changed.bash` and CI;
  controls (over-long, too many functions, `declare -A`, `fzf`, growth, an added entry) fail.

### Phase 2: DBF rules and go-qa-ci

- [x] ✅ **Task 2.1**: go-qa-ci audited: [AUDIT-go-qa-ci.md](AUDIT-go-qa-ci.md). Complete with
  recorded gaps; its bundled rule set is one analyzer; this repo had no project rules.
- [x] ✅ **Task 2.2**: Project DBF rules as `go/analysis` analyzers in the nested module
  `src-go/qa` (so the shipped binaries stay dependency-free), run by `qa-go.bash`: one process
  seam, owned programs (sudo, tmux, gum, fzf, systemctl, git), tmux target forms, no wall clock in
  tests, one home abbreviation, a `WaitDelay` that names its error (six rules); catalogue [GoProjectRules.md](../../GoProjectRules.md). Each red on a fixture of
  the historical defect, green on the tree after fixing the real instances; on landing they
  caught the bash-limits gate running git outside `gitx`.

### Phase 3: Rewrites, one feature branch each

- [ ] 🔄 **Task 3.1**: Programmes from the inventory, ranked; each its own branch.
  `go-port/freeze` (P7), podfreeze and lxcfreeze: merged into F44 and pushed (`f0284848`). Spec
  [SPEC-freeze.md](SPEC-freeze.md); the library (`internal/freeze`), the two engines and the
  shared flow in Go; golden replay of the bash tools' recorded output
  (`internal/freeze/golden`); one task file builds any Go command
  (`tasks/build-go-command.yml`); the bash tools, library, suites and gates deleted. The Fable
  review's fixes are in (sudo asks on `/dev/tty`, Ctrl-C and a lapsed sudo timestamp end an act
  batch cleanly, the sudo `-n true` probe, the inventory generic over the engine's record).
  Host acceptance: deployed by `meta-deploy.bash`. Output parity with the bash tools is
  established: the first run matched byte for byte on three of the four commands (the fourth
  stopped on a sudo prompt the Go sudo probe raised wrongly, now fixed), and the second run,
  with no prompt, differed only by the containers started between the runs, every common row
  identical. A live comparison against a saved baseline cannot repeat on a machine whose
  containers change, so `acceptance.bash` is retired from the queue. Left for the owner: the
  freeze, thaw and menu look in [ACCEPTANCE-freeze.md](ACCEPTANCE-freeze.md) steps 3 and 4.
- [ ] 🔄 **Task 3.2**: `agent-bus-install` on `go-port/agent-bus-install`: spec
  `SPEC-agent-bus-install.md` on the branch; built, reviewed and its review fixed, not merged.
  It runs as root and changes behaviour (its D7), so it merges only after the owner's host test
  as root (the branch's `ACCEPTANCE-agent-bus-install.md`).
- [ ] 🔄 **Task 3.3**: The rest of the inventory's programmes, ranked there; the `ccy` launcher
  waits on Plan 00191's D1-D10. `qa-changed.bash` (P3, first of the QA orchestrators): done,
  merged into F44 and pushed (`de85afbc`); spec [SPEC-qa-changed.md](SPEC-qa-changed.md), nested
  `src-go/qa/changed`, routing and excluded trees as data, one process seam shared with the
  bash-limits gate, golden replay of the bash over 118 scenarios with 19 numbered deviations,
  the script a build-and-run wrapper. `qa-all.bash` (P3, the second orchestrator) on
  `go-port/qa-all`: ported, reviewed, its review fixed, and merged on the integration branch
  with the other ports (it lands in F44 after the owner's acceptance); spec
  [SPEC-qa-all.md](SPEC-qa-all.md); nested `src-go/qa/all`, `qa/gates`, `qa/cmd/qa-all`; the
  gate list is data (`scripts/qa-gates.tsv`), read by the runner, by `qa-changed` and by the
  QA.md inventory gate; a reader that finds nothing fails its gate; golden replay of the bash
  over 142 scenarios with 32 pinned deviations (B1-B6, B12, B13, B20-B23, B26); the script a
  build-and-run wrapper, its allowlist entry, the `qa-helper-summary` library, its suite and the
  call-site parser gone (their cases are Go tests). The full `qa-all` run comes before the merge.
  `qa-ansible.bash` (P3, third orchestrator) on `go-port/qa-ansible`: ported, merged on the integration branch; spec
  [SPEC-qa-ansible.md](SPEC-qa-ansible.md) states the intended behaviour as expectations E1-E19
  (the bash is not an oracle; 13 departures B1-B13 listed there); nested `src-go/qa/ansible`,
  `qa/cmd/qa-ansible`; the script a build-and-run wrapper, its allowlist row gone;
  `test-qa-ansible-failfast.bash` now drives the built tool. Same output and report as the bash on
  the live tree. Fable review (subagent-reports/261014-fable-review-qa-ansible.md) dispositions:
  F1 quoted values evaded the fail-fast rules, fixed (E11, B14; the live tree had no quoted
  violation); F2 symlink to a directory or dangling non-`.yml` link, fixed as exit 2 (E7, B16);
  F3 owner decision, the gate judges what git would publish and never reads gitignored files
  (E6a, B15); F4 `CLAUDE/QA.md` updated; F5 stdout write failure now an `ERROR:` line (E3, B17);
  F6 (`-x` is root's view, any execute bit) and F7 (four passes over each file) are info only,
  accepted, no change.
  `qa-nokill-containerwatch.bash` (P3) on `go-port/qa-nokill-containerwatch`: ported, merged on the integration branch;
  spec [SPEC-qa-nokill-containerwatch.md](SPEC-qa-nokill-containerwatch.md) (E1-E15, 15 departures
  B1-B15: recursive Python scope, no pass on no Python, what git would publish, the `[engine, "stop"`
  hole, multi-line argv, `.kill(`/`.terminate(`/`killall` rules, containment judged with everything
  else reported); nested `src-go/qa/nokill`, `qa/cmd/qa-nokill-containerwatch`; the script a
  build-and-run wrapper, its allowlist row gone; same verdict as the bash on the live tree (7 files
  counted against 6: containment.py is counted now). The cut-over also fixed a latent `qa-changed`
  defect: a changed gate script whose row reads a detail line was judged by the case-count reader
  and failed as "proved nothing"; it is now judged by its own reader (`qa/changed` `judgeSuite`).
  Fable review of that branch (`subagent-reports/261014-fable-review-qa-nokill.md`) dispositions:
  F1 and F10 fixed together (one shared judge, `gates.Summarise`, used by qa-all and qa-changed: a
  suite is judged by every reader of its row, a drift row by its stage line, a helpers or stage row
  is refused rather than passed on nothing); F2 (parenthesised `from os import (...)`), F3 (zero stop
  timeout in containment.py), F4 (options between the engine and its verb), F5 (engine variable in
  any case), F6 (service stop/kill and machine terminate/kill argvs, live tree clean) and F8
  (comments inside a wrapped argv, cheap, so fixed) fixed in the rules, each spec first and test
  first (E9-E11); F7 (`kill` without a dash in a shell string, `killall5`) recorded as a stated
  limit in the spec, no code change; F9 fixed as routing data plus a `nokill-containerwatch` check
  in `qa/changed` (`scripts/qa-routing.tsv`: the watchdog, its two panel files and the gate's own
  files run the gate).
  `qa-patterns.bash` (P3, fourth orchestrator) on `go-port/qa-patterns`: ported, merged on the integration branch; spec
  [SPEC-qa-patterns.md](SPEC-qa-patterns.md), expectations P1-P18, 14 departures B1-B14; nested
  `src-go/qa/patterns` and `qa/cmd/qa-patterns`; semgrep stays the engine for the rules, which stay
  data in `.semgrep/bash-conventions.yml`; the Go owns scope (what git would publish), the rule
  self-test, the mirror and the coverage assertions; the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gains a `patterns` check (SPEC-qa-changed B33). Same result and
  same 433 scanned files as the bash on the live tree. Fable review
  (subagent-reports/261014-fable-review-qa-patterns.md) dispositions, all fixed spec first and
  test first on `go-port/qa-patterns`: F1 the rule id is the bare id (rules copied beside the
  mirror, `--config ../rules.yml`), proven against real semgrep on the printed line and the report
  (P11, B13); F2 CRLF normalised to LF in the mirror, proven for both `$`-anchored rules (P10, B13);
  F3 a symlink whose target is not among the scanned scripts is scanned under its own name and
  named in the coverage line, which claims only what was checked (P8, P16, B14); F4 an empty
  `.semgrepignore` and `--max-target-bytes=0` keep `tests/`, `vendor/` and files over 1 MB in the
  scan (P10, P11, B13); F5 `qa-changed` runs `patterns` when the wrapper, `go-tool.bash` or
  `qa-trees.tsv` change, and a rules file change no longer prints `skipped (excluded tree)` beside
  `check patterns` (B33 pins updated); F6 SPEC section 1 now agrees with section 4; F7 the dead
  branch in `fail()` is gone, B7 reworded, an error with no path prints `(no path)`, and the gosec
  exclusion is narrowed from the whole package to the files that read by computed path. Live
  tree after the fixes: 433 scanned, 17 partial-parse notes, exit 0.
  `qa-python.bash` (P3, fifth gate) on `go-port/qa-python`: ported, merged on the integration branch; spec
  [SPEC-qa-python.md](SPEC-qa-python.md), expectations Y1-Y23, 17 departures B1-B17; nested
  `src-go/qa/python` and `qa/cmd/qa-python` (the file set comes from `qa/discovery`, called
  in-process);
  ruff and python3 stay the engines; the Go owns scope, the one-process compile (no `__pycache__`
  written), template substitution, the proof that `ruff --show-files` lists every file, and the
  judgement; the script a build-and-run wrapper, its allowlist row gone; `qa-changed` gains a
  `python` check (the tool, `ruff.toml`, `.qa-versions`, the wrapper or `qa-trees.tsv` change).
  Same 311 files and 1 template as the bash on the live tree, exit 0. Fable review
  (subagent-reports/261014-fable-review-qa-python.md) dispositions, fixed spec first and test
  first against real ruff: finding 1 (HIGH) every ruff call gets `--config <root>/ruff.toml`, so a
  nested config cannot swap the ruleset, and qa-changed's per-file ruff the same plus `--no-cache`
  (Y22, SPEC-qa-changed B34); finding 2 `--ignore-noqa` (Y23; the live tree has no `noqa`
  comment, so nothing to fix); finding 3 the root is made absolute and cleaned once (Y1); finding
  4 a Python `.j2` runs the `python` check (`@pytemplate` row ahead of `*.j2`); finding 5 a closed
  stdout is exit 2 (Y3; the Go runtime reopens fd 1 on /dev/null read-write, detected in the
  command); findings 6-8 recorded in the spec (python3 version unpinned and `.qa-versions` holds
  tool pins only; template false positives are fail-safe; the `syntax-error` text is reached only
  by an older ruff); finding 9 no change.
  `qa-ansible-syntax.bash` (P3) on `go-port/qa-ansible-syntax`: ported, merged on the integration branch; spec
  [SPEC-qa-ansible-syntax.md](SPEC-qa-ansible-syntax.md), expectations S1-S15, 10 departures
  D1-D10; nested `src-go/qa/ansiblesyntax` and `qa/cmd/qa-ansible-syntax` (the YAML file set is a
  new `yaml` list of `qa/discovery`, the `tracked-playbooks` yardstick unchanged);
  `ansible-playbook --syntax-check` stays the engine, run four at a time and reported in path
  order, config pinned (`ANSIBLE_CONFIG`, root as working directory, `ANSIBLE_NOCOLOR`); a missing
  `ansible-playbook`, `ansible.cfg` or password file (asked of `ansible-config`, never parsed or
  opened; the engine also rejects an empty one) is one exit 2, no longer 83 identical failures;
  the script a build-and-run wrapper, its allowlist row gone. Live tree: the same 83 playbooks
  and pass line as the bash, 26 s down to about 10 s; the missing-prerequisite path run on a
  clean archive. Not required, against the brief: `localhost.yml` (measured). Fable review fixed
  (F1-F7): every colour variable pinned off (a caller's `ANSIBLE_FORCE_COLOR` no longer reaches
  the output), only the vault password file required and its path never printed, plays found by a
  top-level scan (name-first plays count, a loop-list `- hosts:` does not), the qa-all exit-2
  stage text covers every cause (SPEC-qa-all B27), report mode 0644, symlink departure listed;
  no second review was run; the coordinator accepted it on the fixes plus their tests, and the
  integration review (subagent-reports/261014-qa-reviewer-integration.md) read it again.
  `qa-bash.bash` (P3, sixth gate) on `go-port/qa-bash`: ported, merged on the integration branch; spec
  [SPEC-qa-bash.md](SPEC-qa-bash.md), expectations S1-S15, 12 departures B1-B12; nested
  `src-go/qa/bash` and `qa/cmd/qa-bash` (the file set and the `tracked-shell` yardstick come from
  `qa/discovery`); bash and shellcheck stay the engines, shellcheck pinned to `--norc --external-sources` and an empty `SHELLCHECK_OPTS` so a nested `.shellcheckrc` or the
  environment cannot change the rules; the script a build-and-run wrapper, its allowlist row
  gone; `qa-changed` gains a `bash` check (tool, discovery, wrapper or `qa-trees.tsv` change) and
  its per-file shellcheck the same `--norc` (SPEC-qa-changed B35). Live tree: the same 433 files as
  the bash, exit 0; 5 fewer advisory findings (SC1091 info, resolved by `--external-sources`).
  Fable review (subagent-reports/261014-fable-review-qa-bash.md) fixed, each spec-first and
  test-first: suppression is gated (S16-S20, B13): a `# shellcheck disable=` directive fails
  unless the ratchet `scripts/shellcheck-disable.allowlist` records it (per-file count and codes,
  seeded with the 21 live directives, compared with the merge-base so it only moves down),
  `disable=all` never passes, and `qa-changed` gains a `shell-directives` check running the
  gate's `--directives` mode (SPEC-qa-changed B36); a `shell=` contradicting a shebang fails
  (S18); S9 states the SC1091-to-SC1094 promotion `--external-sources` causes and that findings
  inside followed files are not reported, and S10's "diagnostic on a file not given" check is kept
  as a loud defence, proven by a fake, not removed; the time limits are named (S8a) and the
  `TimedOut` branch tested; the leading-`[` check on shellcheck's stdout is kept, because `null`
  decodes without error (tested); the report is written with mode 0644 (S2, B14).
  `qa-discovery.bash` (P3, the shared library the bash gates source) on `go-port/qa-discovery`:
  ported, merged on the integration branch; spec [SPEC-qa-discovery.md](SPEC-qa-discovery.md), expectations D1-D17, 11
  departures B1-B11; nested `src-go/qa/discovery` and `qa/cmd/qa-discovery`, one list per call,
  scope what git would publish; the library is now a build-and-run wrapper (`qa_discover <list> <root>` fills `QA_FILES`; no bash exclusion test remains: the Go gates ask the Go discovery),
  its allowlist row gone; `qa-bash`, `qa-python`, `qa-ansible-syntax`, `qa-helper-tests` and
  `qa-ready-wait-rules` call `qa_discover` in place of the seven old functions. The seven lists the
  old library made on the live tree are identical to the Go tool's. Fable review
  (subagent-reports/261014-fable-review-qa-discovery.md) fixed, each spec-first and test-first:
  an untracked nested repository is exit 2 naming it (D15), `qa/patterns` calls the one exported
  `discovery.ShellShebang`, the bash trees reader refuses a table with no `shell` or no `python`
  row as the Go does (D14), a combined flag group such as `-ec` counts as `-c` (D6), and QA.md
  describes the Go readers.
  `qa-deployed-drift.bash` (P3) on `go-port/qa-deployed-drift`: ported, merged on the integration branch; spec
  [SPEC-qa-deployed-drift.md](SPEC-qa-deployed-drift.md), expectations E1-E18; nested
  `src-go/qa/deployeddrift`, `qa/cmd/qa-deployed-drift`; the script a build-and-run wrapper, its
  allowlist row gone, gate row and routing unchanged. The three host skips keep their `⚠` lines;
  departures: git failing is exit 2 (not "compare anyway"), an unreadable file is exit 2 (not
  drift), a pair matching no file or naming a missing play is exit 2, tier-2 play lookup matches
  whole words, `request.py` is excluded from the helpers entry only. Same verdict and stage line
  as the bash on a clean clone of the live tree (0 match, 96 not installed, 2 templates).
  Fable review (`subagent-reports/261014-fable-review-qa-deployed-drift.md`) disposition: F1 fixed
  (the root is made absolute, cleaned and symlink-resolved once; a container is the marker files
  `/run/.containerenv` or `/.dockerenv`, the rule `_planlib.inc.bash` uses, never the root's
  spelling); F2 fixed (HOME cleaned, a relative HOME refused); F3 fixed (a dangling symlink in
  the repo is exit 2 naming it); F4 fixed (E17: a deployed directory or dangling symlink is
  drift); F5 fixed as the owner exec bit against the play's declared `mode:` (E18), full mode not
  compared because the plays do not set it consistently; F6 recorded as a stated limit in E11, no
  code change; F7 the pair table lives only in `run.go` (the spec points at it), and the
  departure is that a stale pair row (E6) now turns every QA run red everywhere, container and CI
  included, not only on the deployed host; F8 E9 reworded to the code's order (HOME is read after
  the skips).
  `qa-js.bash` (P3) on `go-port/qa-js`: ported, merged on the integration branch; spec [SPEC-qa-js.md](SPEC-qa-js.md),
  expectations J1-J16, 11 departures B1-B11; nested `src-go/qa/js` and `qa/cmd/qa-js`; the file set is
  a new `js` list in `qa/discovery` (D16); node and ESLint 8 stay the engines, ESLint pinned with
  `--no-eslintrc --config extensions/.eslintrc.json --no-inline-config`, with the proof that it read
  every file it was given; no `eslint-disable` exists in the tree, so no allowlist; the script a
  build-and-run wrapper, its allowlist row gone; `qa-changed` gets a `js` whole-gate check and its
  per-file eslint the same pins (SPEC-qa-changed B37). Same 30 files and pass as the bash on the
  live tree (18 linted). Fable review (`subagent-reports/261014-fable-review-qa-js.md`)
  dispositions, fixed spec first and test first: F1 fixed (the root is symlink-resolved, J1, B11);
  F2 fixed (the per-file eslint runs the gate's `--format json` and is judged by the gate's own
  proof, `js.Findings`, so an ignored file fails it; B37 amended, the golden deviations re-pinned);
  F3 fixed (the dead `wsi` and `wsi-transcribe` `ignorePatterns` removed, no such tree exists);
  F4 documented (J9 and QA.md: `/* global */` and `/* eslint-env */` are inert, globals belong in
  the config); F5 recorded (J11, B10: `.eslintignore` is read from the root); F6 recorded as a
  stated limit in J7 (ESM `.js` outside a `type: module` scope needs node 22.7 or later; the nvm
  play installs `lts/*`).
  `qa-ansible-pause.bash` (P3) on `go-port/qa-ansible-pause`: ported, merged on the integration branch; spec
  [SPEC-qa-ansible-pause.md](SPEC-qa-ansible-pause.md), expectations E1-E17; nested
  `src-go/qa/ansiblepause` and `qa/cmd/qa-ansible-pause`; the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gets an `ansible-pause` whole-gate check (SPEC-qa-changed B38).
  `qa-gh-scopes.bash` (P3) on `go-port/qa-gh-scopes`: ported, merged on the integration branch; spec
  [SPEC-qa-gh-scopes.md](SPEC-qa-gh-scopes.md), expectations G1-G16; nested `src-go/qa/ghscopes`
  and `qa/cmd/qa-gh-scopes`; semgrep stays the engine; the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gets a `gh-scopes` whole-gate check. One Fable review of both
  (`subagent-reports/261014-fable-review-pause-ghscopes.md`): its two blockers (gh-scopes G-1, a
  fixture with no `ruleid` line passing the self-test; pause P-1, fixtures routed to a check that
  never reads them) and the FIX findings are fixed on the branch tips before this merge.
  `qa-ready-wait-rules.bash` (P3) on `go-port/qa-ready-wait-rules`: ported, merged on the integration branch; spec
  [SPEC-qa-ready-wait-rules.md](SPEC-qa-ready-wait-rules.md), expectations R1-R26; nested
  `src-go/qa/readywait` and `qa/cmd/qa-ready-wait-rules`; semgrep stays the Python engine and the
  shell half is a Go rule replacing `helpers/ready_wait`; the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gets a `ready-wait` whole-gate check (SPEC-qa-changed B39) and a
  listed `ready-wait-files` check over each changed Python and shell file (B40). Fable review
  (`subagent-reports/261014-fable-review-readywait.md`) found false passes in the hand lexer (F1,
  F2), arithmetic and quoting slips (F3, F4), a backgrounded-loop slip (F7) and the per-file
  scope with no caller (F15); the branch tip fixed them (the shell rule now reads the mvdan syntax
  tree, journal 22:37); the gate is green on the merged tree (739 files, 0 findings). Review G-7
  of the sibling gh-scopes port is recorded as SPEC-qa-changed B41, a stated limit.
  `qa-plan-script-logging.bash` (P3) on `go-port/qa-plan-script-logging`: ported, merged on the integration branch; spec
  [SPEC-qa-plan-script-logging.md](SPEC-qa-plan-script-logging.md), expectations L1-L14; nested
  `src-go/qa/planscriptlogging` and `qa/cmd/qa-plan-script-logging`; it walks the active plan folders
  on disk and parses each script with `mvdan.cc/sh`; the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gets a `plan-script-logging` whole-gate check (SPEC-qa-changed B42).
  `qa-toolchain.bash` (P3) on `go-port/qa-toolchain`: ported, merged on the integration branch; spec
  [SPEC-qa-toolchain.md](SPEC-qa-toolchain.md), expectations T1-T16; nested `src-go/qa/toolchain`
  and `qa/cmd/qa-toolchain`; `.qa-versions` is parsed with the `mvdan.cc/sh` parser and `go.mod` with
  `x/mod/modfile`, never sourced; the script a build-and-run wrapper, its allowlist row gone;
  `qa-changed` routes the pins, `src-go/go.mod` and the gate's own files to the `toolchain` check
  (B43).
  `qa-speech-to-text-rules.bash` (P3) on `go-port/qa-speech-to-text-rules`: ported, merged on the integration branch; spec
  [SPEC-qa-speech-to-text-rules.md](SPEC-qa-speech-to-text-rules.md), expectations S1-S17; nested
  `src-go/qa/sttrules` and `qa/cmd/qa-speech-to-text-rules`; semgrep stays the engine and one rule
  file is written per language; the script a build-and-run wrapper, its allowlist row gone;
  `qa-changed` gets a `stt-rules` whole-gate check and a listed `stt-rules-files` check over each
  changed JavaScript and Python file (B44). Fable review of the three
  (`subagent-reports/261015-fable-review-psl-toolchain-stt.md`) answers PSL-1, TC-1 and STT-1 with
  this entry. Its other findings were fixed on the branch tips; checked in the merged tree: a
  `todoruleid` test in `qa/sttrules` (STT-2), `python .claude/skills` in `scripts/qa-trees.tsv`
  (STT-3), no "aborts the whole run" wording left in the toolchain spec, wrapper or package doc
  (TC-2), and a test for a symlink named `logs` in `qa/planscriptlogging` (PSL-2). The remaining
  LOW findings (PSL-4, TC-3, TC-4, TC-6, STT-4 to STT-6) were not re-checked here.
  `qa-version-pins.bash` (P3) on `go-port/qa-version-pins`: ported, merged on the integration branch; spec
  [SPEC-qa-version-pins.md](SPEC-qa-version-pins.md), expectations V1-V25; nested
  `src-go/qa/versionpins` and `qa/cmd/qa-version-pins`; the manifest is parsed in Go, with a
  cross-check test against the Python manifest validator the host tools still read (review VP-2,
  `src-go/qa/cmd/qa-version-pins/crosscheck_test.go`); the script a build-and-run wrapper, its
  allowlist row gone; `qa-changed` gets a `version-pins` whole-gate check (B45).
  `qa-vmtest-manifest.bash` (P3) on `go-port/qa-vmtest-manifest`: ported, merged on the integration branch; spec
  [SPEC-qa-vmtest-manifest.md](SPEC-qa-vmtest-manifest.md), expectations V1-V16; nested
  `src-go/qa/vmtestmanifest` and `qa/cmd/qa-vmtest-manifest`; the manifest is read strictly in Go,
  the Python validator the host runs stays the one definition of a valid manifest, and the guest
  checkers are cross-checked in Go; the script a build-and-run wrapper, its allowlist row gone;
  `qa-changed` gets a `vmtest-manifest` whole-gate check (B46).
  `qa-helper-tests.bash` (P3) on `go-port/qa-helper-tests`: ported, merged on the integration branch; spec
  [SPEC-qa-helper-tests.md](SPEC-qa-helper-tests.md), expectations H1-H13; nested
  `src-go/qa/helpertests` and `qa/cmd/qa-helper-tests`, with a second reader of the counts file in
  `qa/gates`; the script a build-and-run wrapper, its allowlist row gone; `qa-changed` gets no
  check for it (spec B9: the suite is about 4.5 minutes, so `qa-all` owns it and `qa-changed`
  runs the mirrored unit-test module of each changed helper). Fable review of the three
  (`subagent-reports/261015-fable-review-pins-vmtest-helpers.md`) found no blocker. Checked in the
  merged tree: the cross-check test for VP-2 exists and "task or vars file" is gone from the
  version-pins spec (VP-1). The LOW findings (VP-3, VM-1 to VM-3, HT-2, HT-3, X-1) were not
  re-checked here.
  `qa-docs.bash` (P3) on `go-port/qa-docs`: ported, merged on the integration branch; spec
  [SPEC-qa-docs.md](SPEC-qa-docs.md), expectations D1-D25; nested `src-go/qa/docs` and
  `qa/cmd/qa-docs`, with the Markdown parsed by goldmark (v1.8.6 in `src-go/qa/go.mod`) and new
  `markdown` and `tracked-files` lists in `qa/discovery` (SPEC-qa-discovery D18, D19); `helpers/docs` (the Python
  link checker) is gone and the script a build-and-run wrapper; `scripts/test-qa-docs-exit-codes.bash`
  drives the wrapper. `qa-changed` gets a whole `docs` check that every deleted file owes
  (SPEC-qa-changed B47; the branch numbered it B48, renumbered here to keep the rows sequential,
  so the journal entries of that branch still say B48). Fable review
  (`subagent-reports/261015-fable-review-docs.md`) found no blocker. Checked in the merged tree:
  the `docs` stage row in `scripts/qa-gates.tsv` no longer names a checker crash, with SPEC-qa-all
  B32 (F1); anchors are compared verbatim (F2); `extension.Footnote` is enabled (F5); the rule in
  `.claude/rules/qa-gates.md` names `src-go/qa/docs/**` (F7); a test pins that any deleted file
  owes the docs check (F4); goldmark is v1.8.6 (F9). F3, F6, F8 and F10 were not re-checked here.
  Shared scaffolding (`go-port/qa-cmdmain`, merged): the command-line and output boilerplate the
  gates had each copied now lives in `src-go/qa/internal/cmdmain` (closed-stdout refusal, report
  writers), `emit` (the two-stream writer and the exit rule) and `rootargs` (the `-h`, `--root`,
  `--scripts` grammar): every one of the 22 `qa/cmd` mains uses `cmdmain`, and 12 gate packages
  use `rootargs` (9 of them also `emit`). Fable review
  (`subagent-reports/261015-fable-review-cmdmain.md`) found no blocker and no behaviour drift.
  Checked in the merged tree: `rootargs.Lenient` is gone, so `--root ""` is refused as
  `--root needs a directory` instead of silently falling back to another tree, and
  `rootargs_test.go` pins `--root ""` and `--scripts ""`; the `ansible-patterns` routing row for
  `qa/ansible`, `qa/cmd/qa-ansible` and the shared packages is in `scripts/qa-routing.tsv`; the
  cmdmain tests read the report back; `src-go/CLAUDE.md` describes the shared packages; all 22
  `qa/cmd` mains call `cmdmain.RefuseClosedStdout`; the departure rows for the Strict move exist
  (SPEC-qa-docs B16, qa-js B12, qa-python B18, qa-patterns B15, qa-gh-scopes B9, qa-ansible-syntax
  D11).
  Still on `scripts/bash-limits.allowlist`: `qa-go.bash`, the interim Go gate that Plan 00191's
  go-qa-ci toolchain is to replace. Owner's decision (2026-10-15): not ported; the owner makes
  `go-qa-ci` public, then Plan 00191 Task 3.3 pins it and retires `scripts/qa-go.bash`, which
  closes this task. Blocked on that owner action.
  `vmtest` (P5) on `go-port/vmtest`: ported and cut over, merged into F44 (`3474744f`); spec
  [SPEC-vmtest.md](SPEC-vmtest.md) (E1-E60, B1-B17), `src-go/cmd/vmtest` and
  `src-go/internal/vmtest`, the Python helpers kept behind one seam; the lab play builds the
  binary, the bash CLI and its four suites are gone (their cases are Go tests), and the qa-all
  golden restores their rows (`retiredAfterPort`). Booting is proven only on the host (SPEC
  section 7, H1-H6, queued in `meta-deploy.list`); report
  `subagent-reports/261015-vmtest-port-opus.md`.
  `ftp-camera` (P8, the ftp-camera part) on
  `go-port/ftp-camera`: ported and cut over, merged into F44 (`a069fd96`); spec
  [SPEC-ftp-camera.md](SPEC-ftp-camera.md) (E1-E66, B1-B25), `src-go/cmd/ftp-camera` and
  `src-go/internal/ftpcamera`, with `internal/rclonerc` (the Go twin of `rclone-rc-auth.bash`,
  which stays for the bash rclone helpers) and root systemctl verbs in `internal/systemd`;
  `play-ftp-camera.yml` builds the binary, the bash script and its allowlist row are gone.
  Proven only on the host with a camera (SPEC section 7, H1-H5, queued in `meta-deploy.list`);
  report `subagent-reports/261015-ftp-camera-port-opus.md`. Fable's review (FIX FIRST: 2
  Medium, 5 Low) is fixed on the branch, test first: Ctrl+C no longer drains the async backlog
  under the cancelled context (E66, B25), the test runner refuses a command under a finished
  context as `execx.OS` does, a same-named sorted file is never overwritten (B24, the bash lost
  it), the upload root's own read error is reported, an unreadable log line stops the follower
  at once, the docs and comments no longer describe the bash, and `--copy`/`--push` shipping
  root fragments is recorded as kept (spec section 4). Fable's re-review said MERGE with three
  Lows; two are fixed: a name collision no longer halts `--sort` (identical bytes are a
  reported duplicate, a different file a per-file refusal, exit 1 at the end), and the
  not-sorted count holds only distinct files still in the upload root.
  `watermark` and `wsi` (P8, the rest after ftp-camera) on `go-port/wsi-watermark`: merged
  into F44 (`8c2ec4cc`), its review follow-ups on `go-port/wsi-watermark-lows` too
  (`df5b3b2f`); spec
  [SPEC-wsi-watermark.md](SPEC-wsi-watermark.md) (W1-W35, M1-M14, B1-B30). `watermark` ported
  and cut over: `src-go/cmd/watermark`, `internal/watermark` (config files read as data, never
  sourced), shared `internal/shellword`; the play builds it root-owned into `/usr/local/bin`
  (`build-go-command.yml` gains `go_command_owner`); the bash and its allowlist row are gone.
  `wsi` ported and cut over: `src-go/cmd/wsi`, `internal/wsi` (one select loop over the stop
  signals, timers and `pw-record`); `play-speech-to-text.yml` builds it into `~/.local/bin`;
  `scripts/test-wsi-stop-grace.bash` drives the binary, `qa-stt-limits.bash` reads `limit.go`;
  the bash and its allowlist row are gone. Host checks H1-H3 pending.
  `open` (P6, the first of the interactive account/setup tools; chosen in its spec's section 1:
  no root, no credentials, all decisions over data a fake can pin) on `go-port/open`: ported
  and cut over, not merged; spec [SPEC-open.md](SPEC-open.md) (E1-E31, B1-B20),
  `src-go/cmd/open` and `src-go/internal/open` (handlers as an argv table with an optional
  pager over an `os.Pipe`, no shell; the chooser `ui.Picker` with a Cancel row, or a numbered
  menu on `/dev/tty`; desktop apps through `gio` alone); `play-cli-tools.yml` builds it, the bash
  and its allowlist row are gone (it had no suite). Host checks H1-H5 queued in
  `meta-deploy.list`; report `subagent-reports/261019-p6-port-opus.md`. Fable's review
  (`subagent-reports/261019-open-port-review-fable.md`, FIX-FIRST) is fixed on
  `go-port/open-2`: a handler's own Ctrl-C is a quiet cancel (130), other signals named
  (128 + n), an interrupt before the handler cancels (E29-E31).

### Phase 4: Review

- [ ] 🔄 **Task 4.1**: Fable adversarial review of the rule, gate, rules and rewrites; findings
  fixed. Three reviews (subagent-reports/261014-fable-review-{gate,go,gqaci}.md) found
  blockers in each: the gate is evadable and its CI ratchet bypassable; the DBF rules caught
  1 of 13 planted evasions and Ctrl-C can wedge the terminal; go-qa-ci cannot be adopted (no
  nested-module driver, disabled linters counted as suppressions) and ships one rule. Fixing in
  parallel: the gate on a real bash parser with a versioned ratchet; type-based rules; runtime
  correctness; go-qa-ci M15 (adoption, gate) and M16 (a bundled rule pack). All five are
  merged (go-qa-ci's on its own remote), and so are typed state, the ccy-sessions and
  ccy-teams redraw cost, and the `teams` package split. The ports get their own review before
  merge: freeze's found two blockers, fixed and merged; qa-changed's and qa-all's findings are
  fixed; agent-bus-install's are fixed on its branch.
- [ ] 🔄 **Task 4.2**: qa-reviewer over the plan: done once
  ([subagent-reports/261014-qa-reviewer-plan.md](subagent-reports/261014-qa-reviewer-plan.md));
  its fixes are on `go-port/qa-all` ([the fix report](subagent-reports/261014-qa-reviewer-fixes-sonnet.md)).
  Runs again over the finished plan.

## Success Criteria

- [ ] A new bash file over the thresholds fails QA; an allowlisted one that grows fails QA.
- [ ] Every port in Phase 3 passes its existing suite against the Go binary before cut-over.
  Where the bash tool and its suites were deleted, the oracle was replaced, not dropped: the
  freeze tools' white-box bash suites are replaced by a golden replay of the shipped bash tools'
  recorded output (stdout, stderr, status and commands over a scenario corpus) plus Go tests, and
  qa-all's by a golden replay of the legacy script plus the Go tests that took over the cases of
  the deleted `qa-helper-summary` suite. That meets the criterion because the replay compares the
  Go with the bash byte for byte, and a difference is allowed only as a numbered bug pinned with
  its reason, which a suite of a deleted tool could no longer do.
- [ ] Each defect class found in the ports has a rule, or a recorded reason it cannot
  ([GoProjectRules.md](../../GoProjectRules.md): `project_waitdelay` for the dropped capture
  (B25); the `sudo -n -v` probe is recorded as not a rule, with what holds it).

## Delivery & Milestones

- <!-- milestone or delivery commit hash -->
