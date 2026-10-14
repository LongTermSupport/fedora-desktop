# Adversarial review: `go-port/qa-js` against `integration/261014`

Reviewed commit: `e3bb0464` (one commit over the integration branch). Method: read the spec
(`SPEC-qa-js.md` J1-J16, B1-B9) and the Go against it, ran the built tool over the live tree,
compared the old bash's file set with the Go's on the same tree, and ran the real node 24 and the
project's ESLint 8.57.1 over scratch git repositories built to break each expectation. The
package tests (`js`, `cmd/qa-js`, `discovery`, `changed`) were run; `qa-all` was not.

## Verdict

**Mergeable after two fixes.** No input was found that lets bad JavaScript through `qa-all`: the
pins hold against every bypass tried, and the J11 proof turns every "ESLint did not read it" case
into exit 2. Two expectations are not met: J7/J16 (no checkout path in output) fails when the
checkout is reached through a symlink, and the B37 claim that `qa-changed`'s per-file ESLint
"cannot disagree" with the gate is false for a file ESLint ignores. Both are small, both have a
concrete failing input below, and neither is covered by a test.

## Findings, ranked

### 1. The checkout's real path leaks when the root is a symlink (J7, J16) — medium

`src-go/qa/js/node.go:75` strips `g.root + "/"` from node's stderr; `src-go/qa/js/run.go:107-123`
(`absolute`, `rootDir`) make the root absolute and clean it but never resolve symlinks. node
prints the file's **real** path in a parse error, so when the root is given through a symlink
the two differ and nothing is stripped.

Failing input: `ln -s <checkout> /tmp/view`; `tests/extensions/bad.mjs` containing
`export const = ;`; run `qa-js --root /tmp/view`. Observed stdout:

```
✗ js: tests/extensions/bad.mjs: /tmp/qajs-exp-XXXX/tests/extensions/bad.mjs:1
export const = ;
```

and the report's `failures[].error` carries the real path (2 occurrences). ESLint, by contrast,
echoes the path it was given, so the J11 proof still matches and lint findings are clean.

The tests cannot see this: `helpers_test.go` and `main_test.go` build the tree in `t.TempDir()`,
which has no symlink component on Linux. Fix: resolve the root once with `filepath.EvalSymlinks`
in `rootDir` (J1 "made absolute and cleaned once" becomes "and resolved"), or strip both the given
and the resolved root in `nodeMessage`; add a test whose root is a symlink to the real tree.

### 2. `qa-changed`'s per-file ESLint vouches for a file ESLint ignored (B37) — medium-low

`src-go/qa/changed/checks.go:224-233` (`runEslint`) runs ESLint with the gate's pins but judges by
exit status alone (`session.exec`, `checks.go:100-106`). ESLint 8 exits **0** for an explicitly
named file that an ignore pattern drops, printing only a warning
(`File ignored because of a matching ignore pattern`). The gate's `prove` (`eslint.go:119-131`)
turns the same case into exit 2.

Failing input: a changed `extensions/<ext>/wsi/helper.js` containing
`export const w = undefinedThing();` (`wsi` is in the config's `ignorePatterns`, see 3). Observed:
the per-file command exits 0 with the warning; the whole gate on the same tree exits 2
(`eslint ignored 1 file(s) it was given`). So a commit-time `qa-changed` passes what the push-time
gate refuses to run on. The same holds for a dotfile such as `extensions/<ext>/.hidden.js`
(ESLint's default ignore).

Fix options, cheapest first: add `--max-warnings 0` to the per-file argv (an ignored file then
exits 1 and so does any warning, matching J12); or run it `--format json` and reuse the `prove`
logic. Add the case to `checks_test.go` and amend the B37 row, which currently overstates what the
re-pin achieved.

### 3. `ignorePatterns: ["wsi", "wsi-transcribe"]` is stale and is now a trap — low

`extensions/.eslintrc.json:40`. Git history shows `extensions/speech-to-text@fedora-desktop/wsi`
and `.../wsi-transcribe` were deleted long ago; the patterns outlived them. Under the bash
(`eslint .`) they were harmless. Under the Go gate any future tracked `.js` beneath a directory
of either name makes the gate exit 2 (J11) instead of linting it, with a message pointing at
`ignorePatterns`. Not a Go defect, but the port's tightened contract makes the dead config
load-bearing; remove the two patterns (the new routing row re-runs the gate on that edit).

### 4. `/* global X */` and `/* eslint-env */` are silently dead, and the docs say nothing — low

`--no-inline-config` disables every inline directive, not only `eslint-disable`. Verified with
real ESLint 8: `/* global Foo */` then `Foo()` reports `'Foo' is not defined. (no-undef)` and
`/* eslint-env node */` then `process.env` reports `'process' is not defined.`; ESLint prints no
hint that the directive was ignored (that warning exists only for `noInlineConfig` set in the
config, not the CLI flag). No live file uses either form (grepped `extensions/` and
`tests/extensions/`), so there is no false failure today, but a GJS author's natural fix for a
missing global is the directive and the gate will reject it with a misleading message. The spec
(J9) and `CLAUDE/QA.md:67` mention only `eslint-disable`. Add one sentence to J9 and QA.md: globals
and envs live in `extensions/.eslintrc.json`, inline forms are inert.

### 5. `.eslintignore` moved from `extensions/` to the root — low, informational

ESLint 8 reads `.eslintignore` from its working directory. The bash ran in `extensions/`; the Go
runs from the root (J8). Verified: `extensions/.eslintignore` naming a file is now **inert**
(gate passes, file linted), and a root `.eslintignore` is live (gate exits 2 via J11). Nothing
hides either way, which is the point of J11, but the error text "any `.eslintignore`" could say
which directory is consulted, and SPEC J9/J11 could record the location change as a departure.

### 6. `node --check` on ESM `.js` depends on the node version — informational

A `.js` file using `import` outside any `"type": "module"` package scope passes `node --check`
only because node 22.7+ detects module syntax; on an older LTS it would fail. The nvm play pins
`lts/*`, so this holds on a current install, but J7 is node-version dependent while J4 pins the
ESLint major. No live file is in that position (all `.js` are under `extensions/package.json`'s
`"type": "module"`; everything else is `.mjs`). Worth a sentence in the spec, not a code change.

## What held up

- **File set.** On the live tree the old bash (`find` + `qa_is_excluded`) and the Go (`js` list,
  git-published) agree exactly: 30 files, 18 under `extensions/` linted, 12 `tests/extensions/*.mjs`
  parse-checked. The 31st tracked JS file, `.semgrep/speech-to-text.js`, is a rule fixture excluded
  by both. The one real departure (B1) is `extensions/tests/assets/` (gitignored, so no longer
  gated) and gitignored or untracked-ignored files generally; none exist today.
- **Pins, against real ESLint 8.** A nested `extensions/<ext>/.eslintrc.json` with
  `"root": true, "no-undef": "off"`, a `package.json` `eslintConfig` switching the rule off,
  `/* eslint-disable */` and `// eslint-disable-line` all left the `no-undef` finding standing
  (`main_test.go:158-173` and my `pkgconfig` case).
- **J11 proof.** Config `ignorePatterns`, a root `.eslintignore` and ESLint's default dotfile
  ignore each produce exit 2 naming the file; the unit tests cover a file left out, a file not
  given, a file reported twice and the "File ignored" warning (`run_test.go:275-319`).
- **J10 failure modes.** Status 2, empty stdout, non-array JSON, timeout, and status 1 with no
  finding are each exit 2 (`run_test.go:251-272`). A 20,000-finding file produced a multi-MB JSON
  that was consumed whole (`proc` buffers without a cap) and reported per line, exit 1.
- **J4 version check**, J3 node probe, J5 discovery failure, J6 nothing-to-lint, J2 closed stdout,
  exit 2 writing no report and printing nothing on stdout: all tested and behaving.
- **CommonJS under `extensions/`** fails `no-undef` on `require`/`module`, as it did before the
  port (config unchanged); not a regression.
- **`.mjs`.** Parse-checked as ESM by node; under `extensions/` it is now linted by name (B2),
  which `eslint .` never did.
- **Fail-fast and tooling.** Missing `extensions/node_modules` is exit 2 before any file is
  checked, naming `cd extensions && npm ci`; the wrapper is the standard `run_go_tool` build-and-run
  and its allowlist row is removed. Every process goes through `proc.Runner`; no `_ =` errors.
- **qa-changed wiring.** The new `js` whole-gate check, its routing rows and the classify test
  (`classify_test.go:330-353`) route the tool, wrapper, discovery, config and trees table to the
  whole gate while an ordinary extension file still gets only `node-check` and `eslint`. The golden
  deviations re-pin only the eslint argv under B37.
- **Tests assert behaviour**, not a bash replay: fakes over a real temporary tree plus five
  real-tool tests; red and green are recorded in the plan journal. Docs (`CLAUDE/QA.md`,
  `src-go/CLAUDE.md`, `SPEC-qa-discovery.md` D16, PLAN.md) describe the port as shipped.
