# go-qa-ci M9 fix: mutants gate could not fail

Cause: gremlins v0.6.0 ignores `--threshold-efficacy` (flag value read back as 0), so mode diff/full always passed.

Fix: `runners.Mutants` writes a gremlins config file (`unleash.threshold.efficacy`, `unleash.timeout-coefficient`) into a temp dir and passes `--config`. gremlins fails when efficacy \<= threshold and 0 means no gate, so N is written as N - 0.0001 (default 100 now passes a perfect run, fails any survivor). Timeout coefficient 10 stops timed-out mutants (not counted as killed) causing false failures under load.

Clauses served: TOOLING-SPEC Blocking / fail-fast (a gate that cannot fail is a silent pass); 9 (docs no longer claim an unverified flag).

Tests: MutantsConfig unit test, args test with --config, and TestMutantsThresholdFailsARealGremlinsRun (real gremlins, skipped only when absent from PATH; thresholds 100/34 fail, 33/0 pass on a 33.33% fixture; run repeatedly, stable). docs/runners.md corrected.

Deviation: the real-gremlins test is not run in CI (gremlins not installed there); recorded in docs.
