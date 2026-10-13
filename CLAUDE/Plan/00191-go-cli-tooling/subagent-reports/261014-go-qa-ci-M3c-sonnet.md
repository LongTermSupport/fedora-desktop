# go-qa-ci M3c: ratchet with git history, prune lowers high_water

Built (internal/record): `ratchet.go` (Ceiling, ReviewRatchet: unsigned/overgrown/unsigned-raise/stale-signature checks, `gqaci.record.ratchet`), `edit.go` (SetHighWater, RemoveExceptions: layout- and comment-preserving), `gate.go` (Gate: validate + review + ratchet against the lower-ceiling record of HEAD and the merge base with `history_base`; mayLower rewrites high_water, otherwise the unlowered ceiling is a finding; Prune). `record.Check` now delegates to Gate (read-only). Executor lowers locally and fails in CI; `go-qa record prune <n>...` added.

Spec clauses: TOOLING-SPEC 6.4 (anti-bloat: signed ratchet, a raise needs a fresh human signature), 6.1 (record is the only exception home; prune removes without hand edits), 9.2 (history unavailable is a note, documented as a gap in docs/record.md).

Tests: ratchet_test.go (unit), edit_test.go, gate_test.go (tempdir git repos: unsigned raise fails, stale signature fails, fresh passes, raise committed on a branch caught via merge base, missing base ref falls back to HEAD, no-history note, lowering, prune rewrites high_water), plus executor and CLI cases.

Deviations: prune takes explicit entry numbers because stale detection needs the suppression guard (later milestone); rust-qa derives them from its scan. Git is run via `sh -c 'exec git "$@"'` to satisfy gosec G204 without suppression (repo idiom). Existing tests gained a [ratchet] table because a non-empty record now requires one.
