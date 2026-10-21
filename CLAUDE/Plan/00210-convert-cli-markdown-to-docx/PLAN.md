# Plan 00210: convert, a Go CLI that turns a file into its supported formats (Markdown to DOCX first)

**Status**: In Progress
**Created**: 2026-10-19
**Owner**: joseph
**Priority**: Medium

Addresses #102.

## Overview

Issue #102 asks for one command that turns file X into one of the formats X can become:
`convert notes.md --to docx`, several at once with `--to docx,pdf`, and `convert notes.md` to
list what it can become. It starts with Markdown to DOCX only; other pairs are added when they
are needed.

The tool owns the table of conversions and the checks around a run (input type, output
collisions, missing engines, half-written files). The conversion itself is done by a proven
converter. Per the tooling-is-Go rule it is a Go command in `src-go/cmd/convert` over
`src-go/internal/convert`, unit tested through `execx.Fake`, and installed by
`play-cli-tools.yml` through `tasks/build-go-command.yml`, with its engine as a declared
package dependency of the same block.

The behaviour is specified in [SPEC-convert.md](SPEC-convert.md) (expectations E1-E15). The
table design, the CLI surface and the exit codes are there; this file holds the decisions and
the tasks.

## Goals

- `convert file.md --to docx` writes `file.docx` beside it, with headings, lists, tables,
  bold/italic and code blocks preserved.
- `convert file.md` lists the available outputs; an unsupported `--to` exits non-zero with the
  supported list.
- A missing engine fails loudly before anything runs, naming the play that installs it.
- The conversion table is data: a new pair is one row plus an engine and its tests.

## Non-Goals

- Any pair other than Markdown to DOCX (odt, html, epub, pdf, LibreOffice office formats).
- Pandoc options beyond the reader and writer (`--reference-doc` for house Word styles is a
  later row option, added when a template exists).
- An output directory option, or stdin/stdout streaming.

## Decisions

- **D1 Engine: pandoc.** No play installs pandoc or LibreOffice today: a case-insensitive
  search for `pandoc`, `libreoffice` and `soffice` over every `*.yml` finds nothing. LibreOffice
  is present on a Workstation install and imports Markdown, but it is not declared in IaC, it
  is large, and a headless run is handed to an already-open instance unless given a private
  profile. Pandoc is the proven document converter the issue names, is a Fedora package, is
  headless-safe and needs no profile. It becomes a declared dependency of the
  `convert-command` block of `play-cli-tools.yml`, which also asserts `pandoc --version` runs.
- **D2 Home: `play-cli-tools.yml`.** `convert` is a small self-contained CLI (a binary and one
  package, no services or secrets), which AnsibleStyle.md sends to that play as a tagged block,
  beside `open`. The play is headless-safe and in the server-recommended bundle; pandoc is
  headless too.
- **D3 Name: `convert` (settled by the issue's wording).** The owner's issue names the
  command `convert`. Disclosed: it is installed to `~/.local/bin/convert`, which precedes
  `/usr/bin` on the login user's PATH, so it shadows ImageMagick's legacy `/usr/bin/convert`
  alias in that user's shells. Nothing in this repository calls ImageMagick as `convert`
  (every caller uses `magick`), and `magick` remains the way to run ImageMagick. The play,
  docs/playbooks.md and the meta-deploy entry say so.
- **D4 Fail before acting.** Every requested format, output collision and engine is checked
  before the first conversion, so a request that cannot complete changes nothing (E7, E9, E10).

## Tasks

### Phase 1: Spec and build

- [x] ✅ **Task 1.1**: Spec: [SPEC-convert.md](SPEC-convert.md), the table design, CLI and E1-E15.
- [x] ✅ **Task 1.2**: `src-go/internal/convert` test first (table invariants, argument grammar,
  every expectation through `execx.Fake` and a temporary directory), then the code;
  `src-go/cmd/convert` thin `main`; a `scripts/go-binary-suites.tsv` row (`qa-go`).
- [x] ✅ **Task 1.3**: `play-cli-tools.yml` block tagged `convert-command`: pandoc package,
  `pandoc --version` probe, build-go-command install to `~/.local/bin/convert`; the
  `play-golang.yml` import also tagged `convert-command`; docs (`docs/playbooks.md`,
  `src-go/CLAUDE.md`).
- [x] ✅ **Task 1.4**: Queue `play-cli-tools.yml` in `meta-deploy.list` with what it does,
  as its own entry, followed by this plan folder (its `acceptance.bash`).
- [x] ✅ **Task 1.5**: [acceptance.bash](acceptance.bash) (gather legs through
  [check-convert.bash](check-convert.bash)): the installed `~/.local/bin/convert` lists docx,
  refuses `--to pdf` with exit 64, and converts [fixture.md](fixture.md) to a DOCX that pandoc
  reads back (JSON syntax tree and plain text) with headings, both lists, the table, bold,
  italic and the code block. This is the real path the fake runner never exercises.

### Phase 2: Deploy and accept

- [x] ✅ **Task 2.1**: Owner runs `./CLAUDE/Plan/meta-deploy.bash` (the CLI tools play, then
  this plan's acceptance). Ran at F44 c3391ba9; all 8 units passed.
- [x] ✅ **Task 2.2**: `acceptance.bash` passes on the host (Task 1.5's checks): docx is
  listed, `--to pdf` exits 64, and every fixture element survives.
- [ ] ⬜ **Task 2.3**: `qa-all.bash` and the `qa-reviewer` agent.

## Success Criteria

- [ ] The issue's three acceptance points hold on the host after the play runs.
- [ ] pandoc is installed by the play, never assumed; a host without it is told which play.
- [ ] `go-qa` passes with the package at or above the coverage floor.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00210-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed from #102.
