# Typed state in src-go (Fable review finding 3)

Branch work, four commits on top of the Plan 00192 review fixes. Gates: `scripts/qa-go.bash` green
(coverage 94.5%, golangci-lint clean with `exhaustive` at `default-signifies-exhaustive: false`);
`scripts/qa-changed.bash --base <pre-work commit>` green, including all parity suites
(`test-ccy-sessions-reboot` 284, `-take-over` 74, `test-ccy-tmux-targets` 20, `test-ccy-teams-command` 323,
`test-ccy-restore-askpass` 90, `test-qa-changed` 69). No bash suite was edited.

## Done, by commit

1. restore typed state, `Entry.At`, compact floor
   - `restore.State`, `restore.ContainerState` (None/Up/Starting), `restore.GoingOutcome{Kind OutcomeKind, Detail}`
     replace the `"failed:<detail>"` / `"-"` / `"up"` strings. `Judge` and `GoingState` take and return them;
     `goingVerdict` is an exhaustive switch. `GoingOutcome.String()` renders the bash going word, pinned by a test.
   - `Entry.At` and `Change.At` are `time.Time` (zero = none). `AtSeconds()` is now `AtUnix()` (no error).
   - `sessions.Config.CompactFloor` is a `TokenFloor` (`ParseTokenFloor`, `.Tokens()`, `.String()`), parsed in
     `loadConfig`; a bad value is still reported by set-going at the point it was reported before, with the raw text.
   - `lookAt`'s switch over `restore.State` is exhaustive.
2. `term.ExitCode`
   - `type ExitCode int`, constants typed. Every `Run`/`run`/flow function in `sessions`, `teams` and both mains
     returns it; mains do `os.Exit(int(run(...)))`. Child-process statuses (`declare`, agent-bus, systemctl) are
     converted once with `term.ExitCode(x.Code)`. `leaveError`, `stop`, `step`, `Finish` carry it.
   - `inPicker`'s switch over `ExitCode` is exhaustive.
3. transcript
   - `[]map[string]any` replaced by `[]entry`, a struct of the fields read. Fields decode through
     `field[T]` (tolerant: wrong type or null never costs the entry its other fields; the reason is kept in `Err`)
     and `flag` (jq truth for `isSidechain`/`isMeta`). New characterisation test, written and run green against
     the old map code first, then the refactor.
   - `.golangci.yml`: tagliatelle excluded for this one file (the field names are Claude's wire format).
4. teams
   - `declare.ApplyState` (with `ApplyAbsent`, Current, Outdated, Unbuilt, Removed, Decide, Unreadable) is
     `Status.State`; the `st*` constants and every `"decide"`/`"removed"`/`"outdated"`/`"unbuilt"`/`""` literal
     are gone. All four switches over it are exhaustive.
   - `systemd.HomeserverUp` replaces the three `"up"` literals; `rowKind` type for `Row.Kind`.
   - Display text no longer parsed back: the new-session picker and the restore-backup picker pass `PickSpec.Tags`
     (`<seat>@<team>`, backup id) and read `Picked.Tag`; `backupIDOf` deleted. `Row.Record` carries the registry
     session name, so Ctrl-R no longer strips `"(stopped)"`; `SeatRestore` is `map[string]bool` (presence = a record
     names the seat), the RESTORE column text is derived from it.

## On-disk compatibility evidence

- Restore manifest: `TestAnExistingManifestLoadsAndWritesBackUnchanged` writes a literal old-format manifest
  (`at=1700000000`, empty `at=`, and `at=0`), loads it, writes it back and requires the bytes identical.
  `TestWriteThenReadRoundTripAndFormat`, `TestBashWritesGoReads` and `TestGoWritesBashReadsAndUpdates` (the
  real bash library in both directions) still pass; the bash restore suites above pass unchanged.
- Going words: `TestGoingOutcomeStringIsTheBashWord` and `TestVerdictAndGoingStateAgreeWithBash` (runs the bash
  `ccy_restore_going_state` / `ccy_restore_verdict`) pin the `-`, `setting-going`, `started`, `failed:<x>` forms.
- Compact floor: env var `CCY_RESTORE_COMPACT_FLOOR_TOKENS` unchanged; the existing set-going tests that feed
  `""` and bad values still pass with identical messages.
- Transcript: the existing jq-comparison test (`TestAgreesWithTheBashLibrary`) and the new tolerance test
  (truthy sidechain values, odd unrelated fields, null/non-number counts) pass.

## Deliberate behaviour differences (justified)

- A manifest line `at=<digits too large for int64>` is now rejected when the manifest is read (message
  "... not a time in seconds, and is not read"); before, `Read` accepted it and the later `AtSeconds()` call failed.
  Both are failures; the earlier, single point is the point of typing it. Test: `TestATimeTooLargeForSecondsIsNotRead`.
- `at=007` (leading zeros) is accepted and rewritten as `at=7`. The bash side reads `007` as octal and Go never
  wrote one; nothing produces it.
- `Judge` can no longer be handed an unknown going word, so the old `unknown-state-<word>` verdict is unreachable
  from typed input (test case dropped; `GoingState` still maps an unknown `Going` to `failed:unknown-going-<x>`).
- A picker answer with no tag is a reported failure (`names no seat` / `names no backup`); previously a row that
  parsed to nothing gave the same for seats and an empty backup id for backups.
- `encoding/json` matches struct keys case-insensitively; the old map lookup was exact. No transcript writer
  emits differently-cased duplicates of these keys.

## Not done / left

- `Row.Dir == "-"` sentinel, composite `"<dir>|<seat>@<team>"` string map keys, `Entry.Detail` split into tokens
  and reason, and `homeserverState`'s `"down (...)"` as a type: not in the brief, same family as finding 3.
- `qa/bashlimits` (nested module) keeps its own `ExitOK/ExitFailure/ExitUsage` untyped constants: it cannot import
  `internal/term`. Its `Run` returns `int`.
- `ui.exitCode` is gum/fzf's own exit status, not ours; left `int`.
- No JOURNAL entry or PLAN.md task update was made (journal entries are stamped by `mkplan.bash --journal`).

## What the other go-port branches need at merge

`go-port/freeze` and `go-port/agent-bus-install` use `term.ExitOK/ExitFailure/ExitUsage/ExitCancelled` as plain
ints. The constants are now typed `term.ExitCode`, so those branches will not compile until:

- their `run`/`Run`/exit-mapping functions return `term.ExitCode` (freeze's `errors.go` exit mapping and the app
  `Run`, bashlimits/lxcfreeze/podfreeze/qa mains, agent-bus-install `run` and `complain`), and their mains call
  `os.Exit(int(code))`;
- `go-port/agent-bus-install`'s extra constants (`term.ExitNotRoot`, `term.ExitConfig`) are declared as
  `ExitCode` in the same const block;
- tests comparing `code != 1`-style literals still compile (untyped constants convert), but helpers with `int`
  parameters/returns need `term.ExitCode`.

Source compatibility was not kept (typed constants are the point); the change per call site is mechanical.
