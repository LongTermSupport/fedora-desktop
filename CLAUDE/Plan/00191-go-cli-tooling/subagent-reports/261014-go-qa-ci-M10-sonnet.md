# go-qa-ci M10: \[[stage]\] and \[[resolver]\]

## Built

- `internal/stage`: `Stage` (strict TOML), `Problems`, `MatchingFiles` (git work tree view or a walk,
  globs, shebangs through `env`, scope, symlinks excluded), `Argv` with `{files}` expansion relative to
  `dir` (nothing to check is a skip), `Doc`, `ParseFindings`/`ParseRules` (closed JSON contracts).
- `internal/stagerun`: runs a stage command in its directory; `LoadRules`.
- `internal/config`: `Stages`, `Resolvers`; validation of both (duplicates, globs, empty text).
- `internal/docs`: resolution order project page, stage name, stage rule `doc`, bundled, configured
  `[[resolver]]`; `ForConfig`; the docs detector requires each stage name and printed rule to
  resolve, and does not call their pages orphans. Rules commands run once per resolver.
- `internal/tools`: `Select` takes the whole config; stages rank after built-ins of their phase in
  declared order; `-t <stage>`; `runStage` (status or findings; record applied; stale findings
  exceptions for the stage's rules on whole runs); `CheckStages` (name clash).
- `internal/cli`: `tools` lists stages; configuration errors exit 2. `docs/stages.md`, README.

## Clauses served

TOOLING-SPEC 5.3 (project defences listed and recorded alongside bundled ones: stages are in `tools`,
subject to the record and the docs detector; the `rules` listing itself is M11), 4.2 (offline
resolution, now with a resolver order that includes project stages and configured globs), 4.5
(phase ordering and stop rule include stages), 6 (record applies to stage findings, staleness
checked), 9.2 (gaps recorded in `docs/stages.md`).

## Tests

Port of `stage_test.rs` (config read, validation, files by glob/shebang/scope, `{files}` expansion,
files_command, dir-relative files, doc, findings and rules JSON) plus executor tests: shellcheck-style
stage over the shell glob skipped with no matches (tool not run); status pass/fail; scoped run uses
`files_command`; dir cwd; missing program fails; findings reported with root-relative paths; record
applied; scope filter; exit 2, bad JSON, unknown field and exit 1 with no findings all fail; stale
exception only on whole runs; ordering; selection; docs and cli tests.

## Deviations

- Findings contract is stricter than the Rust port: unknown keys, absolute or escaping paths, and exit 1
  with an empty list fail rather than pass. Rust accepted absolute paths under the root and let exit 1 +
  `[]` pass.
- `summary_rules` is not ported (no consumer until M11); a stage using it is rejected as an unknown key.
- No per-directory toolchain notion (Rust-specific); `dir` is the working directory only.
- `rules` commands run only for the docs detector, resolution and whole-project findings runs.
- No new dependencies.
