# fieldalignment: size half on, pointer-ordering half excluded under a dated entry

Coordinator's decision of 2026-11-17 (see the Plan 00245 journal entry of the same day).

## Configuration (src-go/.golangci.yml, which src-go/qa/.golangci.yml links to)

- `linters.settings.govet.disable: [fieldalignment]` was removed. `enable-all: true` now runs
  the analyzer in both modules, and in the `teamchat-failpoints-lint` stage too.

- One exclusion was added (golangci-lint v2). golangci prefixes a govet diagnostic with the
  analyzer's name, so the text is matched whole:

  ```yaml
  - linters: [govet]
    text: '^fieldalignment: \S+ has \d+ leading bytes of pointer data but optimal value is \d+$'
  ```

  The analyzer (x/tools v0.50 in golangci-lint 2.14) gives the pointer message only when the
  struct is already at its optimal size. That makes the two halves disjoint: no size finding
  can match this text.

- go-qa-ci's suppression detector (`internal/suppression/golangci.go`, `exclusionRules`) keys an
  exclusion rule by `exclusions.rules::<linters joined by +>/<text>`. The record rule is
  therefore `golangci.yml::exclusions.rules::govet/<the regex above>`, written as a TOML literal
  string.

## Record entry (src-go/qa/record.toml)

The record entry was edited in place, so the count stays at 30 (`max_total` 30, ratchet 30). It
keeps the same decider (`repository-owner`), `added_by_model = "claude-opus-5-5"` (the medium
tier stays at 29 of its `max_count` of 29), `decided_on` 2026-10-27 and `review_by` 2026-11-26.
It is not made permanent. Its justification names only the pointer-ordering half and cites the
analyzer's Doc ("very rarely indicate a significant problem", "not included in typical suites
such as vet or gopls").

- `go -C src-go/qa tool go-qa --root src-go record check`: `record: 30 exceptions in qa/record.toml, all valid`, exit 0.
- `go-qa run --no-fix -t suppression`: record passed, suppression passed. The new key matches,
  and no entry is stale.

## Test: src-go/qa/lintconfig (test-only package, with its fixture under testdata/fieldalignment)

The test runs the real golangci-lint (through `qa/internal/proc`) over the fixture, copied into
a module of its own:

- `TestFieldalignmentControl` uses a bare config with fieldalignment only. The fixture draws both
  diagnostics: `Wasteful has size 24 but the optimal size is 16` and `PointerLate has 16 leading bytes of pointer data but optimal value is 8`.
- `TestFieldalignmentProjectConfig` uses the project's `.golangci.yml`. The size finding must be
  reported, and the pointer finding must not be.

Red, before the config change: `.golangci.yml does not report a struct that wastes memory on padding ("fieldalignment: Wasteful has size 24 but the optimal size is 16"); it reported []`
(FAIL). Green, after the change: both tests PASS. The control proves the absence of the pointer
finding is the exclusion's doing.

## The 101 size-waste structs

The first qa-changed run found two more structs. They are in test files built only with
`-tags busfailpoints` (`wiring/pt1_model_test.go` `pt1Seen`, `wiring/pt1_run_test.go`
`pt1Runner`), which the untagged scan missed; the `teamchat-failpoints-lint` stage caught them.
golangci-lint with govet alone is clean under `busfailpoints`, `tuwunel`, `ccyteamstest` and
`difftest`. All 103 were reordered by hand, without -fix, and every field comment was kept. Where a comment
covers fields that are now apart, it says where they went (teams.go `held`, teamsync `Team`,
winddown `job` and `session`, link `link.renderDirty`, ui `Lines.Colour`). Afterwards
`fieldalignment -test ./...` finds no size waste in either module, and `go vet` is clean in both
modules and under `-tags busfailpoints`.

Consequences checked:

- **Positional literals**, reordered with the struct: proto `Verbs` (verbs.go) and its test `row`
  (verbs_test.go); the table structs in ccylaunch/inrepo_test.go, lxc/apply_test.go and
  term/term_test.go. All other literals are keyed.

- **No binary layouts** (no `encoding/binary`, gob or unsafe on these types).

- **The freeze golden, the ccylaunch difftest and the statusline golden** record program output,
  not struct dumps. The test suite confirms them.

- **JSON-written structs.** Your rule was: reorder unless a reader depends on the bytes. All three
  were reordered:

  - `state.StatusTeam` (status.json): read by `DecodeStatus` and the status line through
    `decodeStrict`/`sameJSON`, whose `proto.Value.Equal` compares object members by key. The
    test's `statusTeam` doc is a decode input, so the change is order-free.
  - `proto.IndexedQuestion` (questions index): `DecodeQuestionsState` uses the same `sameJSON`.
    The "byte for byte" lines in SPEC-n3f (QX7) compare two writes by the same binary. QX6 counts
    sizes. Nothing hashes or byte-compares the index.
  - `health.onlineEntry` (lxc-online.json): written and read by health only, with a plain
    `json.Decoder`.

  No spec text (SPEC-n3.md, SPEC-n3f.md G4, DESIGN-v2-B-member.md) makes their key order
  normative. **No struct is INHERENT.**

## CCY bump and host run

`execx`, `registry`, `ui` and the teamchat packages are ccy image sources, so the pre-commit
hook required a version bump: CCY 3.122.22, container 2.84.30 (launcher, Dockerfile label, and
a `docs/ccy-changelog.md` entry). The rebuild changes no behaviour. It rides the existing
`playbooks/imports/play-claude-yolo.yml` entry in `CLAUDE/Plan/meta-deploy.list`, whose
description now names it. No new entry was added.

## Tool fault seen, not filed: please log it

The hooks daemon's `lint_on_edit` runs `golangci-lint run` without `--allow-parallel-runners`.
When another golangci-lint is running, it reports "parallel golangci-lint is running" as a
**lint failure** (R-LINT-FAILURE) on a file that is clean. It is a false deny. It is an upstream
handler, so this seat cannot file it.
