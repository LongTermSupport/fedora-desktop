# go-qa-ci M7b report

Built: bundled analyzer `gqaci_sensitive_format` (`analyzers/sensitiveformat`), registered in the
go-qa vet tool, built-in identifier added, bundled page `ruledocs/rules/gqaci_sensitive_format.md`
(with the correct construction), docs/analyzers.md and docs/defences.md updated.

Spec clauses served: TOOLING-SPEC 4.1/4.2 (bundled defence with analysistest single-rule proof,
page that states the correct construction), 4.2 offline resolution, 7 (only exception is a record
entry, covers = findings), 8.1 (page audited), 9.2 (limits recorded in the page and docs).

Tests: analysistest red fixtures (alias, embedded, nested slice/map, unexported field, generic
instantiation, named non-struct, fmt, log, slog, indexed and starred verbs), clean fixture
(String, LogValue, GoString, Format, Error, promoted String, pointer fields, interface/type param
operands, non-%v verbs, spread args), configured patterns/redacting fixture, verb parser unit tests,
flag tests, and an end-to-end test that the built go-qa binary reports it as a vet tool.

Deviations: sinks beyond the brief: %w, Error() counts as redacting (fmt honours it), non-constant
format strings are read as %v. Identifier uses an underscore (analyzer name), so the builtin
namespace test accepts `gqaci_`. depguard allow-list gained go/types and go/constant (a dependency
allow-list entry, not a suppression). Config is via flags / `sensitiveformat.New`; no
qa/go-qa.toml key yet (recorded gap). Not followed: interface-typed and type-parameter operands,
verbs other than v/s/w.

## Fix round after verifier rejection (commit 2884524)

- Redaction is now decided per sink (exposure.go `sink`): fmt/log %v %+v %s %w and Print-style = String, Error or Format; %#v = GoString or Format; slog = LogValue or Error (slog never calls String; a JSON handler marshals fields). `-redacting` redacts everywhere.
- slog resolves only the top-level value, so nested fields are not treated as honoured under slog; fmt still honours exported nested fields.
- verbs.go now records the `#` flag (`verb{letter, sharp}`) so %#v is its own sink.
- Red fixture gained LogValue-only/GoString-only/String-only leaks (fmt, %#v, slog, wrapped); clean fixture now only prints each partial type to the sink it redacts; Credentials has String+GoString+LogValue.
- Rule page and analyzer Doc rewritten with the per-sink table and the corrected correct construction.
- Gates: gofmt, go vet, go test -race ./..., golangci-lint 0 issues.
