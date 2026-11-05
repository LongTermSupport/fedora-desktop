Verdict: MERGE

# Plan 00209 N1 Tasks 1.1 and 1.2: narrow check of the round-3 fixes (cb3736fa, tip 2f7b9320)

This is a narrow check of `261021-n1-paths-review-3.md`'s five findings and three named
questions. It is not a full review.

## What I ran

- `src-go`: `go test -race -count=1 ./internal/teamchat/paths/` passes.
  `golangci-lint run ./internal/teamchat/...` reports 0 issues.
- `src-go/qa`: `go test -race -count=1 ./teamchatseparate/ ./cmd/qa-teamchat-separate/` passes.
- `./scripts/qa-teamchat-separate.bash` exits 0 over 6 v2 files, 14 names and 0 allowed references.
- No probe files were written. The checks below are from reading the code.

## Round 3 findings

| #   | Severity | Status         | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| --- | -------- | -------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | major    | VERIFIED-FIXED | `ids.go` `must` and the per-type `text()` panic on `""`. Every derivation in `paths.go` reads an id through `text()`, and a kit, layout, seat, run or store through `must`. The only raw `.s` reads left are `String()` and `CCYLayout`'s `CCYKitDir().s`, which is a constant. `zero_test.go` `TestEveryFunctionOfAZeroValuePanics` calls every function. Its list is held complete in both directions by `guardedTakers`, which parses the package. `TestEveryMethodOfAZeroValuePanics` uses reflection over every method of the five holder types: a zero receiver panics, a valid receiver does not, and a valid receiver given a zero id panics. `TestADecodedMissingIDPanicsWhenUsed` covers `{}` and `null`. The package doc (`paths.go:9-13`) says all of this. |
| 2   | minor    | VERIFIED-FIXED | The fields of `Layout`, `Seat`, `Run` and `Store` are unexported, and accessors read them through `must`. These values come only from `CCYLayout`, `HomeLayout`, `Layout.Seat`, `Layout.Run` and `SeatStore`.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 3   | minor    | VERIFIED-FIXED | `Port` is `struct{ n uint16 }`. `UnmarshalJSON` goes through `ParsePort` and refuses `null`, a string and a fraction. `MarshalJSON` refuses zero, and `Int()` panics on zero. `TestPortDecodesOnlyThroughParsePort` covers each case.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| 4   | minor    | VERIFIED-FIXED | `References` drops one trailing `.` only when the path does not end in `/.` or `..`. `scope_test.go` has the `/..` and `/.` cases. `run_test.go:318` makes `files/teamchat/..` a K7 red case ("names files").                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| 5   | minor    | VERIFIED-FIXED | The prefix alternation now holds a backtick, `;` and `\$\{?\w+\}?/`. `scope_test.go` has the `$ROOT/`, `${ROOT}/`, backtick and `;` cases, and `run_test.go:319` has a `${ROOT}` K7 red case.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |

## The three questions

1. **Can a zero or unparsed id reach a path builder? No.**

   - Every id is made only by its Parse function. A failed Parse returns the zero value, which
     panics at first use.
   - `UnmarshalText` goes through Parse and leaves the target unchanged on error. A decoder
     that skips it (a missing or `null` JSON field, gob, which ignores unexported fields) leaves
     the zero value, and that panics.
   - Every exported function and method that takes an id goes through `text()`, and the
     reflection and AST tests hold that list complete. The base-directory functions also run
     `checkBase` first.

2. **Is the reserved-id list complete? Yes.** I listed every per-team root the package builds:

   - `/var/lib/teamchat`, `/var/lib/private/teamchat`, `/var/lib/teamchat-admin`,
     `/var/lib/teamchat-ctl` and `/var/lib/teamchat-status/teams`;
   - the two seat roots, `/run/teamchat` (through `RuntimeDirectory=`) and the probe root.

   Their fixed children are `_lxc` and `_sets` (an underscore, which no team id can start
   with), `seats` and `membership` under `/run/teamchat` (both reserved), and
   `conversation.json` (a `.`, which the grammar refuses).

   For the `teamchat-<team>` names (homeserver user, Element profile), every fixed
   `teamchat-<x>` tail is reserved: admin, backup, ctl, fence, health, install, server, seats
   and status. `TestReservedTeamIDsAreExactlyTheFixedNamesTails` derives these tails from the
   constants, so a new fixed name cannot go unreserved.

   The `teamchat-session-<user>` names are not constants and never take a team id. The other
   team-keyed names stay in their own namespaces:

   - `teamchat-server@<team>` and `teamchat-fence@<team>` are unit instances.
   - `teamchat_fence_<team>` is an nftables table.
   - `team-<team>.lock` is a file in `/run/teamchat-admin`, where no other fixed file lives.
   - `<team>.teamchat.internal` is a Matrix server name.

   None of them can equal a fixed name.

3. **Does the gate's red and green proof cover what D 4.1 lists? Part 1 and part 3, yes.
   Part 2 is not built here.**

   - Red: `TestK3AV1PathInAV2PlayFails` puts one v1 path in a v2 play and checks that the gate
     fails, naming the file and line. `TestK3EveryListedNameFailsEverywhereInScope` and the K7
     cases go further.
   - Green: `TestK4AUnitRenderedFromV1NamesPasses`.
   - Part 3 is in `paths/separation_test.go`.
   - D 4.1 also asks for a red case with one `os.WriteFile` in an adapter fixture. That case
     belongs to the part-2 `migrate/v1` analyzer, which this branch does not build.
     `261021-n1-paths.md:136,166` defers it to Task 6.1.
   - The fixtures are an in-memory fake world, not D 4.1's `testdata/`. They prove the same
     thing.

## New findings

No new major.

- **minor** (tracking): PLAN.md Task 6.1 (`PLAN.md:193-195`) does not yet name the D 4.1 part 2
  analyzer. The deferral is recorded only in `261021-n1-paths.md:166`, among the deferred design
  edits. **Fix**: land that line in Task 6.1 together with the other deferred design edits.
- **nit**: `String()` on a zero id, and `Kit.String()` on a zero kit, return `""` without
  panicking. A consumer that joins `t.String()` itself would bypass the guard. The package doc
  already says no other v2 package builds a name of its own, so this is low risk.
  **Fix (optional)**: route `String()` through `text()`, or say in the doc that `String()` is
  for display only.
- **nit**: `reference` still misses a path directly after a command substitution
  (`"$(pwd)/files/x"`), which is the same class as round-3 finding 5. No play uses this shape.
  **Fix (optional)**: add `\)/` to the prefix alternation, with a test case.
