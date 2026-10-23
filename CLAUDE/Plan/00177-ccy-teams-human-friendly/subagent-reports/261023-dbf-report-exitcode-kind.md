# Defence Before Fix report: a child's exit code, and the board's KIND

Branch `agent-a8c69aa67110156ce-c48195fd`. Red `1824f6ed5`, green `37cbe9ae7`, the original
defect's fix `a86557067`, record and catalogue follow-up after it.

## Specification followed

Defence Before Fix method specification 1.1.0, fetched by the skill's refresh script into
`/root/.cache/defence-before-fix/spec/SPEC.md`.

## Toolchain

The project's own Go rule toolchain: `go/analysis` analyzers in the nested module `src-go/qa`
(`src-go/qa/rules`, registered in `rules/table.go`), run by go-qa's analyzers stage over both
modules. Listing and identifier resolution: the catalogue `CLAUDE/GoProjectRules.md` and the
driver's help text (each analyzer's doc string). Single-rule harness: `analysistest` fixtures,
`go test ./rules/ -run 'ChildExit|RowKind'` in `src-go/qa`. Calibration: the project's own
(fixtures carry `// want` lines; every rule blocks).

## Defect and class

The original instance: `ccy --teams <team-a>,<team-b>` with a team this machine does not have
ended with exit 78 from `agent-bus seat choose`, handed on by `teams/choose/choose.go:224`
(`term.ExitCode(cmd.Code)`); the shell's prompt said "Unknown error code" and the owner was
left with nothing to do next.

- Class A, `project_child_exit`: another program's exit code becomes the tool's exit status
  without translation. Hazard: a code the tool's documentation does not name reaches the shell,
  so a person or a caller cannot tell what happened.
- Class B, `project_row_kind`, found on the same investigation: a board row with a run-time
  handle and a constant KIND. Hazard: a host cc seat shown as `ccy`, so the person acts on the
  wrong kind of session.

The owner's complaint itself (an unknown team only errors, it should lead to creating it) is
the original defect, fixed last (below).

## Rule

- `project_child_exit` (`src-go/qa/rules/childexit.go`): a conversion to `term.ExitCode` of a
  `Code` field read from an error type, directly, inside an expression, or through a local.
  `internal/term` and test files are not judged. Fix shown in the message and the catalogue:
  `term.FromChild`.
- `project_row_kind` (`src-go/qa/rules/rowkind.go`): a `board.Row` literal with a run-time
  `Handle` and a constant or missing `Kind`, or unkeyed fields. Fix: `board.KindOf(handle)`.

Documentation: `CLAUDE/GoProjectRules.md`, both sections, with what each does not catch and the
next wider rule considered (every run-time conversion to `term.ExitCode`; any reading of a
handle's type word outside `agentbus.KindOf`) and why each was not built.

## Proof

Red survives on its own in `1824f6ed5` (rules and fixtures only, no instance fixed).
`./scripts/qa-changed.bash --base 1824f6ed5~1` at that commit exits 1 in go-qa's analyzers
stage with 14 `project_child_exit` and 1 `project_row_kind` findings (reproduced by the
conformance reviewer, `261023-dbf-conformance-exitcode-kind.md`). The fixtures
`fix/internal/badexit` and `fix/internal/badrowkind` (with `good.go` beside each) pass under
`go test ./rules/ -run 'ChildExit|RowKind'`.

## Independent search

`261023-dbf-search-exitcode-kind.md` (this folder; written before the rule commit, by an
agent given only the class descriptions, which did not open the detector). Text search gave
the complete set of `.Code`, `term.ExitCode(`, `os.Exit`, `KindCcy` and literal suffix sites
across both modules. Reading found what text cannot: the carrier types (`stop`, `failure`,
`exitStatusError`, `flow.Leave`) and which sites feed them a child's code, that `playExit` was
compared rather than returned, which conversions are the tool's own signal codes, and that
`seatRow` sets the kind by constant.

Reconciliation: every Class A site the search called confident is one of the rule's 14, and its
one Class B defect site is the rule's 1. Three sites the search listed are not judged by the
rule (a code passed as an int through a helper): `vmtest` `member-ssh` and `verify`, and
`open`'s `128 + n`. Each is a pass-through its spec requires (`SPEC-vmtest.md` E58, E60;
`SPEC-open.md` E30, B20), named per site in the catalogue's "Not caught"; whether to widen the
rule is referred to the owner below. The Class B duplicate readers (`roles.parseHandle`,
`seats.nextSeat`, `lxcadd`) were read and each is recorded with why no wrong kind is shown.

## Sweep and count

Scope: first-party Go in `src-go` and the nested `src-go/qa` module; test files, fixtures and
`vendor/` excluded (the project's decision, recorded in the catalogue). The bash and Python
callers are outside both classes' pattern (they read the code they are given). Count: 14 Class
A, 1 Class B.

## Fixes

All 15 examined one by one in `37cbe9ae7`: 12 through `term.FromChild`; `ops.playExit` became
`playStatus`, an int, as it never was the tool's exit; `ccy-sessions reboot` exits 1 and names
systemctl's code (departure recorded in Plan 00191 `SPEC-ccy-sessions.md` 1.3); `seatRow`
takes `KindOf(handle)`. The 13 `Kind == KindCcy` checks became `Kind.IsSession` by pattern:
before the fix every seat row was `KindCcy`, and `IsSession` is true for exactly those rows,
so each answers as before. No suppression, no baseline.

`FromChild` keeps 78 on purpose (the launcher reads it as "a team not running here"); the fix
for the person is the words: `ccy-teams: exit 78: ...` as the last line, and the usage text.

## Decisions referred to the owner

- Widen `project_child_exit` to an int helper that returns an error's `Code`? It would judge the
  three pass-throughs above (0 fixed, 3 remaining, all spec-required). Default: not widened; the
  rule is merged at full width with the three recorded in the catalogue.
- An attended `ccy --teams <seat>@<unknown-team>` names a seat, so the launcher does not call
  the chooser and agent-bus refuses it (78, with words). Default: leave it; the owner's form was
  a team named alone, and a named seat is the scripted form.

## Permanence

Both analyzers run in go-qa's analyzers stage, which `./scripts/qa-changed.bash` and
`./scripts/qa-all.bash` run and CI runs; a finding fails the stage. Green through the entry
point: `./scripts/qa-changed.bash --base 1824f6ed5~1` at `a86557067` exits 0.

## Toolchain and detector gaps

None found. `qa-changed.bash` diffs against HEAD by default, so a red commit is reproduced with
`--base <red>~1`; that is its documented behaviour, not a gap.

## The original defect

`a86557067`: in `chooser.resolve`, a team named alone with no homeserver up and no host_vars
entry is asked about (`src-go/internal/teams/chooser/unknown.go`): create it (the create wizard,
`RunForChooser(ctx, name)`, which offers the typed name as the Team name), choose one of this
machine's teams instead, start the rest without it, or start nothing. Leaving the wizard or the
choice comes back to the question, three rounds at most, then exit 1. Headless stays fail-fast
(`agent-bus seat check --resolved`). Tests written first and seen to fail to build against the
old signatures: `chooser/unknown_test.go` (12 tests) and create's
`TestWizardForTheChooserOffersTheNameTypedOnTheCommandLine`; all pass.
