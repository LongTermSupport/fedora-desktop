# go-qa-ci M3a report: project record

## Built

- `internal/record`: schema and strict loader (`Load`, `Parse`, `Check`), `Validate` (`gqaci.record.invalid`),
  `JustificationProblem` (GENERIC list of 22 phrases, placeholders, 25-char minimum), `Matcher`
  (`Suppression`, `Finding`, `Stale` giving `gqaci.record.stale`), `List`, `ListJSON`, `Explain`.
- `internal/glob` (path.Match per segment plus `**`), `internal/date`, `internal/finding` (compiler-style output).
- `go-qa record check | list [--json] | explain <rule|n>`; the `record` pipeline tool now runs for real
  (`tools.Executor`), other tools still fail as "not implemented yet".
- `docs/record.md`, README commands and status.

## Clauses served

- TOOLING-SPEC 6.1: path defined (`qa/record.toml`, configurable), loaded by the tool; unknown keys rejected.
- 6.2: justification required, no default; generic/placeholder/short rejected by a documented check
  (docs/record.md); states the check cannot verify truth.
- 6.3: `record list` (text and JSON) enumerates entries with justifications.

## Tests

Ports of record_test.rs (parse, malformed dates, unknown keys, generic/placeholder/short, per-exception findings,
empty decided_by, find) plus glob-without-policy, line numbers, matcher/stale, render, cli, executor.

## Deviations / gaps

- `gqaci.record.stale` is implemented as `Matcher.Stale` but no run reports it yet: it needs the detectors that
  apply the record (suppression, golangci, analyzers; M5-M7). Documented as a gap, no conformance claimed.
- Missing rule/path/justification keys are `gqaci.record.invalid` findings, not load errors (clearer, and 6.2 wording).
- Rule comparison is exact after trimming; rust's hyphen/case normalisation is clippy-specific and not ported.
- Budgets, tiers, expiry, ratchet are M3b/M3c; their config keys remain accepted but unenforced (documented).
