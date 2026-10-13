# go-qa-ci M3b: record anti-bloat controls

Built: `record.Review` (internal/record/policy.go) applying budgets (`gqaci.record.budget`), trust
tiers, review horizons, human co-sign (`gqaci.record.invalid`) and expiry (`gqaci.record.expired`),
with the caller supplying the clock. Policy helpers on `config.RecordPolicy` (TierOf, ReviewDays,
CountCap, IsDecider, CosignThreshold, Problems, LooksAutomated). `record.Check` now takes the policy
and a date and returns structural plus policy findings; `go-qa record check` and the `record` tool
both use it. Date gained Compare/Before/After/AddDays/DaysUntil/FromTime/Today.

Clauses served: TOOLING-SPEC 6.4 (record growth is bounded and entries expire), 6.1 to 6.3 stay
as in M3a. Ratchet (rqaci.record.ratchet equivalent) is deliberately not built (recorded as a gap
in docs/record.md).

Tests: policy_test.go ports the policy_test.rs budget, breadth, provenance, expiry, tier, horizon,
count-cap and co-sign cases against a fixed 2026-10-07 clock; config/policy_test.go ports tier
mapping, automation-identity and policy-validation cases; date, check, executor and CLI tests.

Deviations: braces are not glob syntax in go-qa's glob package so is_broad's `{` is dropped;
rule normalisation also lowercases (Go linter ids vary in case); glob-without-policy stays in
Validate (M3a) rather than being duplicated in Review; Status/set_high_water/remove_exceptions
not ported (ratchet milestone). Per-entry findings carry the entry's header line.
