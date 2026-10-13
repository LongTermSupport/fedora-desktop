# go-qa-ci M2: Pipeline (commit 1f0c7fc)

## Built

- `internal/pipeline`: Phase, Step (Name, Phase, Rank), Outcome (passed/failed/skipped with reason), Executor, Report, `Execute` (clones, sorts, dedupes; detectors before runners; format or detector failure leaves runners unstarted; `--fail-fast`; runners stop at the first failure). `StageRank` keeps project stages after built-ins in a phase.
- `internal/tools`: Phase moved to pipeline; `Select` (enabled tools by default, `-t` names regardless of switch, unknown name lists known names), `Names`, `Tool.Step`, `Context` (Root, Config, Scope, CI, Fix), `Pending` default executor.
- `internal/root.Relative`: `-p` path to slash-separated path relative to the root; symlinks resolved; missing or outside-root path is an error.
- `internal/cli`: `go-qa run [-t]... [-p]... [--ci] [--no-fix] [--fail-fast]`, `RunWith` with an injectable executor factory, CI detection (`CI` set and not `false`/`0`, or `--ci`), exit codes 0/1/2, summary table with scope line, "runners were not started" notice, DBF pointer on failure only.

## Spec clauses served

- TOOLING-SPEC 4.5 (detectors before runners, detector failure stops runners): `pipeline.Execute`.
- 4.4 (local entry point, subset down to one file, identifiers unaltered): `run -p file`, scope relative to root passed to the executor.
- Exit codes and the DBF pointer in failure output (method pointer, SPEC section 2 terms).
- Not claimed: conformance. No tool engine exists yet; the default executor fails every step as "not implemented yet" so nothing is reported as passing (TOOLING-SPEC 9, gap to be recorded in M-later conformance docs).

## Tests added

- pipeline: all-pass order, detector failure, fail-fast, format failure, runner failure, skipped, input order/dedupe, no in-place sort, stages ordering, detector-stage failure, StoppedBeforeRunners false on runner failure, phase names.
- tools: Select default/named/unknown.
- root: Relative table (file, dir, root, uncleaned, outside, missing, cwd-relative).
- cli: run all-pass, detector failure leaves runners unstarted, fail-fast, -t dedupe/sort, config switches, unknown -t lists names (exit 2), `-p` one file reports only that file, outside-root scope, CI detection table, --no-fix, bad usage, default executor never passes.

## Deviations

- No `[[stage]]` config exists yet, so Step is a plain struct with Rank rather than the Rust enum; stages are supported by rank only (tested), wired in a later milestone.
- A failed Outcome may carry a Reason (shown in the summary) so the unbuilt-tool failure needs no writer.
- README updated for `run`.
