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
