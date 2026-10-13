# go-qa-ci M4: offline identifier resolution (commit b83570c)

## Built

- `internal/builtin`: the 12 `gqaci.*` identifiers (record x5, suppression.unrecorded/bare, summary.stale, docs.dangling/orphan, rules.unnamed/untested) with descriptions; `internal/finding` constants now alias it (one source).
- `ruledocs` (public): `//go:embed rules/*.md`, `Page`, `IDs`, `FS`; one page per builtin, each `# <id>` plus What it flags / Why / How to fix correctly / Suppression.
- `internal/docs`: `Resolver` (project page in `docs_dir`, exact name then case/`-`-vs-`_` alias, then bundled; reads via `os.Root`, ids with separators or dot segments never resolve, an unreadable project page is an error), `Orphans`, `Check` (dangling + bundled audit + orphan), `AuditBundled`, `Run` (adds record rules as documentable ids).
- CLI: `rule-doc <id>` (page on stdout, source on stderr, exit 1 when dangling, 2 on bad arity), `docs-check`. The `docs` tool in `run` now executes the detector.
- `docs/defences.md`: place for project pages, order, recorded gaps; README updated.

## Spec clauses served

- TOOLING-SPEC 4.2: every builtin id resolves offline; project pages have a documented place and are resolved by identifier; pages must state the correct construction (audit enforces a non-empty "How to fix correctly" section).
- TOOLING-SPEC 8.1: Go test (`TestEveryBuiltinHasAPageWithTheRequiredShape`, `TestAuditBundledIsCleanForTheShippedPages`) plus `docs-check` fail on a builtin without a page, a wrong first line, a missing/empty section, or an unpaired bundled page.
- DETECTOR-SPEC 6.3 (dangling reference) guarded for the toolchain's own rules.

## Tests added

builtin (namespace, uniqueness, sorted, copy), ruledocs (round trip, unsafe ids), docs (shape of every builtin, project wins, alias spelling, unknown/unsafe ids, trimming, unreadable page errors, orphans, missing dir, detector clean/orphan/dangling/order, record-rule pages, malformed record errors, audit defect table), cli (rule-doc bundled/project/configured dir/unknown exit 1/arity, docs-check clean/orphan/record rule/args), tools executor (docs pass/fail).

## Deviations and recorded gaps

- Third-party catalogues (golangci-lint, staticcheck, vet analyzers) and project stages/`[[resolver]]` are not in the resolution order yet; they belong with the detectors that run them. Recorded in `docs/defences.md` (TOOLING-SPEC 9.2); no conformance is claimed.
- No defence listing exists (M11), so the orphan check treats builtins plus record-exception rules as the identifiers the project can see; a page for any other project rule is reported as an orphan until M11 feeds the detector. Dangling is therefore enforced only for builtins and any `Input.Listed` the caller supplies.
- Pages for builtins whose detectors come later (suppression.*, summary.stale, rules.*) exist ahead of them so the audit has no excuse list.
- No new dependencies. No suppressions or new configuration exceptions.
