# go-qa-ci M14 (polish): disposition of every deferred item

Commits on main (all pushed): A 7becfc8, B 539f2ca, C ecceb20, D ae10c0e, E bc9f5b4, F a87b8ae, G 3ed8797, H e2d0e9d.

Gates at the end: gofmt clean, `go vet ./...`, `go test -race ./...` all ok, golangci-lint 0 issues, `go-qa run --ci` on itself green (format, record, suppression, summary, docs, golangci, analyzers, deadcode, vuln, mod, test, coverage passed; ast-grep skipped as the bundled pack is empty).

Dispositions: FIXED (test first, commit), DONE-BY (already fixed by a later milestone), RECORDED (where), OWNER (stays an owner action), ACCEPTED (no change, reason).

## Run 2 (M0 to M3c)

| #   | Item                                                                                  | Disposition                                                                                                                                                                |
| --- | ------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | M0 GQACI_DENYLIST secret unset; no test of write-denylist.sh                          | OWNER for the secret (docs/PUBLICATION-CHECKLIST.md); the missing test RECORDED in docs/CONFORMANCE.md (H)                                                                 |
| 2   | M0 commits carry the owner's real git identity                                        | OWNER (publication checklist step 3)                                                                                                                                       |
| 3   | M0 `--staged` scan reports a diff line, not file:line                                 | FIXED D (reports `staged <file>:<line>`, `staged path #n` for names, withholds a name that matches the list; tests/scripts updated)                                        |
| 4   | M0 checkout and gitleaks pins unverified offline                                      | DONE-BY M11/M12 (the workflows ran in CI with those pins; only Public safety is red, for the secret)                                                                       |
| 5   | M0 no test runs the built binary                                                      | ACCEPTED: main is `os.Exit(run(...))`; run() is tested in-process                                                                                                          |
| 6   | M0 depguard with no rules                                                             | DONE-BY M5 (allow-list in the overlay and .golangci.yml)                                                                                                                   |
| 7   | M1 table and key names match case-insensitively                                       | FIXED A (internal/config/keys.go: a key spelt in another case is a config error naming the documented spelling)                                                            |
| 8   | M1 negative record caps accepted                                                      | FIXED A (every cap, horizon and tier limit must be non-negative)                                                                                                           |
| 9   | M1 problem messages in random order                                                   | FIXED A (sorted keys, sorted models; test runs 20 loads)                                                                                                                   |
| 10  | M1 trust.models accepts `unknown`/`human`                                             | DONE-BY M3b (modelProblems rejects both)                                                                                                                                   |
| 11  | M1 absolute `--config` fails with an os.Root message                                  | FIXED A (absolute path inside the root is accepted; outside says so)                                                                                                       |
| 12  | M1 README test misses a default left out                                              | FIXED F (TestReadmeNamesEveryConfigurationKey; it immediately exposed the cosign listing, which the regexp now reads from the commented example)                           |
| 13  | M2 `-p` one file filters diagnostics: only the scope reaching the executor was tested | DONE-BY M5 to M7a (engine tests drop findings outside the scope)                                                                                                           |
| 14  | M2 stop notice says "detectors failed" for a format failure                           | FIXED D ("format or detector failed: runners were not started")                                                                                                            |
| 15  | M2 DBFPointer says each failure names an identifier                                   | FIXED D                                                                                                                                                                    |
| 16  | M2 compare() ties on Name                                                             | ACCEPTED: the config rejects two stages with one name, so Name is the identity                                                                                             |
| 17  | M3a header line detection with comments or spaces                                     | FIXED C (line numbers and `record prune` now use the go-toml parser, so `[[exception]] # c`, `[[ exception ]]` and a multi-line string that looks like a header are right) |
| 18  | M3a matcher also matches entries Validate rejects                                     | FIXED E (tools.Select adds the record check to any `-t` selection naming a tool other than fmt, when record is on; test; docs/record.md corrected)                                                                            |
| 19  | M3a 6.3 listing via `tools` vs `record list`                                          | DONE-BY M11 (`rules`, `record list`, docs/enumeration.md)                                                                                                                  |
| 20  | M3a record.md "Not yet built" wording                                                 | ACCEPTED: still true; kept                                                                                                                                                 |
| 21  | M3b 6.4 cited as if it required budgets and ratchet                                   | FIXED C (policy.go, ratchet.go and docs/record.md say the controls serve 6.4 as toolchain defaults)                                                                        |
| 22  | M3b negative caps                                                                     | FIXED A (same as 8)                                                                                                                                                        |
| 23  | M3b unmapped model counted under no tier but reviewed as low                          | RECORDED docs/record.md "Order of checks" (G); the entry is already `gqaci.record.invalid`                                                                                 |
| 24  | M3b ratchet cases not ported                                                          | DONE-BY M3c                                                                                                                                                                |
| 25  | M3b CLI tests use the real clock                                                      | ACCEPTED: dates are relative to `date.Today()`; the only exposure is a UTC midnight crossing inside one test                                                               |
| 26  | M3c gates green                                                                       | no action (informational)                                                                                                                                                  |
| 27  | M3c acceptance tests present                                                          | no action (informational)                                                                                                                                                  |
| 28  | M3c no test for subdirectory history lookup or raise-from-zero                        | FIXED C (TestAModuleInARepositorySubdirectoryFindsItsOwnHistory, TestCreatingARecordWithAStaleSignatureIsARaiseFromZero)                                                   |
| 29  | M3c no history passes in CI                                                           | FIXED C (a finding in CI, a note locally; `record check` is strict)                                                                                                        |
| 30  | M3c unparseable HEAD record reads as "no history"                                     | FIXED C (its own message, same CI/local treatment)                                                                                                                         |
| 31  | M3c history_base passed to git without `--end-of-options`                             | FIXED C (`--end-of-options`; config rejects a `history_base` or `mutants.base` starting with `-`)                                                                          |
| 32  | M3c README lacks `record prune`, says only `.invalid`                                 | FIXED E (rows corrected)                                                                                                                                                   |
| 33  | M3c a raise on a long-lived branch goes stale after 14 days                           | FIXED C (stated in docs/record.md)                                                                                                                                         |

## Run 3 (M3c to M5)

| #   | Item                                                         | Disposition                                                                                                                                                                         |
| --- | ------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | M3c no-history gate exits 0 in CI, docs say gap not pass     | FIXED C (see 2.29, 2.30)                                                                                                                                                            |
| 2   | M3c same-day second raise can never pass                     | FIXED C (accepted when decided_by or reason changed; documented)                                                                                                                    |
| 3   | M3c `prune 2 2` prints "removed 2"                           | FIXED C (Prune returns removed and remaining; repeated index counts once)                                                                                                           |
| 4   | M3c subdirectory history lookup untested                     | FIXED C                                                                                                                                                                             |
| 5   | M3c no test of notes on stderr or of mayLower=!CI end to end | ACCEPTED, not added: `record check` is strict and non-mutating so it prints no notes; the executor's `mayLower=!CI` is a one-line argument covered by the gate tests on both values |
| 6   | M3c ratchet cites 6.4                                        | FIXED C                                                                                                                                                                             |
| 7   | M4 defences.md says tests and self-gate fail the release     | FIXED E (reworded: CI jobs fail on every push; no release process refuses a tag, the declared 8.1 gap)                                                                              |
| 8   | M4 README "no declaration or gap list exists yet"            | DONE-BY M12 (README points to CONFORMANCE.md and the declaration)                                                                                                                   |
| 9   | M4 a project page can shadow a bundled one unchecked         | RECORDED docs/defences.md (E)                                                                                                                                                       |
| 10  | M4 bundled `gqaci.ast.*` identifiers must join builtin.All   | RECORDED docs/defences.md (E)                                                                                                                                                       |
| 11  | M4 symlinked page never an orphan                            | RECORDED docs/defences.md (E)                                                                                                                                                       |
| 12  | M4 rule-doc dangling message untrimmed                       | FIXED E                                                                                                                                                                             |
| 13  | M5 README gap pointer                                        | DONE-BY M12                                                                                                                                                                         |
| 14  | M5 consumer .golangci.yml exclusions bypass the record       | RECORDED docs/golangci.md (E); the suppression tool reads those routes since M6                                                                                                     |
| 15  | M5 nested module ignores a parent .golangci.yml              | RECORDED docs/golangci.md (E)                                                                                                                                                       |
| 16  | M5 codePrefixed is five linters                              | ACCEPTED: already a recorded DETECTOR-SPEC 4.3 partial gap                                                                                                                          |

## Run 4 (M5 to M12)

| #   | Item                                                            | Disposition                                                                                                       |
| --- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| 1   | M5 8.2 gap list omits G204 and G306                             | FIXED E (docs/golangci.md, defence-before-fix.toml 8.2 summary, CONFORMANCE.md)                                   |
| 2   | M5 overlay "cannot change a baseline value" overstates          | FIXED E (docs/golangci.md and the merge() comment)                                                                |
| 3   | M5 consumer config can hide findings, not recorded              | RECORDED docs/golangci.md (E)                                                                                     |
| 4   | M5 codePrefixed                                                 | ACCEPTED (as above)                                                                                               |
| 5   | M6 `//nolint:all` counts as a named rule                        | FIXED B (bare, unlicensable; docs/suppression.md)                                                                 |
| 6   | M6 unset issue caps flagged although go-qa forces both to 0     | FIXED B (only a cap above 0 is a route; doc corrected)                                                            |
| 7   | M6 no recorded/unrecorded pair for several routes               | FIXED F (new-from-merge-base, presets, paths, linters.disable, cap above zero, three staticcheck.conf whitelists) |
| 8   | M6 path-only exclusion rules all become `exclusions.rules::*`   | RECORDED docs/suppression.md (F)                                                                                  |
| 9   | M6 unparseable file has no line                                 | ACCEPTED: the parser's own `file:line:col` is in the message; now pinned by a test and stated (B)                 |
| 10  | M6 README "fails every route"; record.md table                  | FIXED E (README), C/G (record.md table aligned, blank line)                                                       |
| 11  | M6 golangci tests need the binary                               | RECORDED docs/golangci.md contributor note (E)                                                                    |
| 12  | M6 later gaps (go:build ignore, -c, settings, fieldalignment)   | ACCEPTED: already in docs/suppression.md and the declaration                                                      |
| 13  | M7a no CLI test of `[analyzers] driver` wiring                  | RECORDED docs/analyzers.md (G); verified by hand and in pieces                                                    |
| 14  | M7a exceptions for unregistered analyzers never stale           | RECORDED docs/analyzers.md (E)                                                                                    |
| 15  | M7a `-p` filters after a whole-module vet                       | RECORDED docs/analyzers.md (E)                                                                                    |
| 16  | M7a README `rules` row misaligned                               | FIXED E (table formatter run on edit)                                                                             |
| 17  | M7a tests need golangci-lint on PATH                            | RECORDED docs/golangci.md (E)                                                                                     |
| 18  | M7b project driver can silently drop a Blocking bundled defence | FIXED E (driver.Main always registers the bundled analyzer; only sensitiveformat.New replaces it, via sensitiveformat.Built; New panics on an empty pattern list; a same-named impostor panics in the driver; tests, docs) |
| 19  | M7b slog does not follow a pointer nested in a value            | RECORDED rule page Limits, now accurate for fmt versus slog (E)                                                   |
| 20  | M7b MarshalJSON/MarshalText not accepted as redacting for slog  | FIXED E                                                                                                           |
| 21  | M7b `%q %x %X %d` unchecked                                     | FIXED E (`%q %x %X` redact via String/Error/Format, `%d` via Format only; red and clean fixtures)                 |
| 22  | M7b go test needs golangci-lint                                 | RECORDED docs/golangci.md (E)                                                                                     |
| 23  | M8 README status and gap index stale for ast-grep               | DONE-BY M12 (README no longer carries that paragraph; docs/astgrep.md is linked)                                  |
| 24  | M8 ast-grep silently passes under a gitignored path             | RECORDED docs/astgrep.md (F), not fixed: no ast-grep binary here to prove a `--no-ignore` change                  |
| 25  | M8 scan_exclude covers the bundled pack only                    | RECORDED docs/astgrep.md (F)                                                                                      |
| 26  | M8 binary tests skip without ast-grep                           | ACCEPTED: recorded already; CI installs the pinned binary                                                         |
| 27  | M9 runners.md row still shows the ignored flag                  | FIXED E                                                                                                           |
| 28  | M9 vuln dedup lets a path-scoped exception license another file | FIXED D (one finding per OSV, module and call-site file)                                                          |
| 29  | M9 `error[coverage]` etc. resolve to nothing                    | RECORDED docs/runners.md (E)                                                                                      |
| 30  | M9 real gremlins proof skips in CI                              | ACCEPTED: recorded; gremlins is pinned in tools.lock, the self-gate keeps mutants off                             |
| 31  | M9 README says merge base for the diff ref                      | FIXED E (README and runners.md no longer promise it)                                                              |
| 32  | M9 golangci tests need PATH                                     | RECORDED docs/golangci.md (E)                                                                                     |
| 33  | M10 findings file outside `dir` rejected                        | FIXED D (inside the project root is enough)                                                                       |
| 34  | M10 status stage runs whole under `-p`                          | RECORDED docs/stages.md (F)                                                                                       |
| 35  | M10 findings stage without `rules` never stale                  | RECORDED docs/stages.md (F)                                                                                       |
| 36  | M10 record applied when invalid                                 | RECORDED docs/stages.md (F): not a pass leak                                                                      |
| 37  | M11 CI red on stdlib advisories                                 | DONE-BY M12 (toolchain pin)                                                                                       |
| 38  | M11 ci.yml header comment stale                                 | DONE-BY M12 (comment current)                                                                                     |
| 39  | M11 summary.go cites 5.3                                        | FIXED D (7.1 and 7.2)                                                                                             |
| 40  | M11 golangci lines reuse upstream descriptions (7.1)            | RECORDED defence-before-fix.toml 7.1 and CONFORMANCE.md (E)                                                       |
| 41  | M11 rules/defences tests need golangci-lint                     | RECORDED docs/golangci.md (E)                                                                                     |
| 42  | M11 `rules` text omits justifications                           | FIXED F                                                                                                           |
| 43  | M12 Public safety red until the secret is set                   | OWNER                                                                                                             |
| 44  | M12 9.1 second grading basis missing                            | RECORDED docs/CONFORMANCE.md (E)                                                                                  |
| 45  | M12 install-tools does not re-check the resolved binary         | FIXED F (fails naming the shadowing copy; test with stubs)                                                        |
| 46  | M12 artefact rows "as the project level"                        | RECORDED docs/CONFORMANCE.md (E)                                                                                  |
| 47  | M12 owner's identity on commits                                 | OWNER                                                                                                             |
| 48  | M12 ast-grep step "skipped" without saying the pack is empty    | DONE-BY M12 (the skip reason already says so)                                                                     |
| 49  | M12 Public safety red since creation (duplicate of 43)          | OWNER                                                                                                             |
| 50  | M11 Public safety job (duplicate of 43)                         | OWNER                                                                                                             |

## Run 5 (M12 fix and M13)

| #   | Item                                                        | Disposition                                                                                                   |
| --- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------- |
| 1   | Public safety workflow red                                  | OWNER                                                                                                         |
| 2   | actionlint inline pin, shellcheck unpinned                  | RECORDED docs/CONFORMANCE.md (E)                                                                              |
| 3   | install_tools_test covers no download path                  | RECORDED docs/CONFORMANCE.md (H)                                                                              |
| 4   | `[go]` version probe reads the main module line             | RECORDED docs/CONFORMANCE.md (H)                                                                              |
| 5   | Reconcile does not compare gap summaries with evidence text | RECORDED docs/CONFORMANCE.md (H)                                                                              |
| 6   | M13 vuln/mod skip reason and README row overstate safety    | FIXED B (both run whenever any Go package is affected, skipped only when none changed; README, comment, test) |
| 7   | M13 `--changed` ignores testdata and embedded files         | FIXED A (a testdata file runs the owning package; an embedded file runs its package and importers; README)    |
| 8   | M13 absolute or `./` `--config` does not widen              | FIXED A (root-relative, slash-separated before comparing; four spellings tested)                              |
| 9   | M13 no engine test for analyzers narrowing                  | FIXED F (TestDiffAnalyzersVetsTheChangedPackagesAndImportersOnly)                                             |
| 10  | M13 narrowed coverage uses the module-wide threshold        | RECORDED README diff-mode table and docs/runners.md (E)                                                       |

## Left as owner actions (not attempted)

The GQACI_DENYLIST repository secret, the commit author identity, publication, tags.
