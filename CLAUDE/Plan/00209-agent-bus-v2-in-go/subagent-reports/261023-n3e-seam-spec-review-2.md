# Review: SPEC-n3 section 6.1, the agent seam (round 2)

Reviewed: SPEC-n3.md 6.1 at be375d544 (feat/bus-v2-n3e-seam), against round 1's report and the
code in this worktree (`src-go/internal/teamchat/member`, `teamsync`, `wiring`, `src-go/qa/rules`,
`CLAUDE/GoProjectRules.md`). Read only; no test, build or QA run.

Verdict: no blocker. Every file:line in 6.1 points at the code it describes. `type Mode = agent.Agent` works with nothing broken. Four major findings: a keeper test that would stop
compiling (R1), RunFacts' functions given no signatures (R2), and AS6, which does not match the
ways round that it claims to catch (R3) and names exemptions in a form `table.go` does not have
(R4). Five minor findings follow.

## 1. Did F1 to F9 land?

| Finding | Landed?   | Note                                                                                                                                                                                                                                                                                                                                  |
| ------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| F1      | yes       | AS2 is the proposed text.                                                                                                                                                                                                                                                                                                             |
| F2      | yes       | AS4 is the proposed text, plus "so HK1's answers do not change" and the reason the Channel waits.                                                                                                                                                                                                                                     |
| F3      | yes       | AS1a matches round 1's table, and every cite checks (section 2).                                                                                                                                                                                                                                                                      |
| F4      | yes       | "Neither config gains a field", the alias, and the unreachable-error forms are all in AS1.                                                                                                                                                                                                                                            |
| F5      | partly    | The fields are named, but the four functions have no types (R2). `Files []RunFile` became `WriteRunFile`, which is better because it keeps the log order (see F7). `Cmd` is listed but nothing reads it (R6).                                                                                                                         |
| F6      | yes, text | The proposed text is in. Checking it against the code turned up R3 and R4, which round 1 did not see.                                                                                                                                                                                                                                 |
| F7      | yes       | Fixed by design, not by the proposed note. Because the adapter writes through the keeper at the point where `prepare.go:479` writes today, the order is the same as today: settings read, refusal line, write line, then the `no resume:` lines. This holds only if `WriteRunFile` writes the `write <path>: <err>` line itself (R2). |
| F8      | yes       | `Env` is gone. `kitEnv` (`loop.go:161-167`) gives `PATH` and the kit variable, and `startCmd` (`loop.go:473`) gives `TEAMCHAT_RUNTIME`: the three names AS5 lists.                                                                                                                                                                    |
| F9      | yes       | `state.Launches` is in, and CL2 is kept in cli on purpose. The check messages are still literal text (R8).                                                                                                                                                                                                                            |

## 2. The cites, checked

Every cite matches the code:

- `prepare.go:439` and `loop.go:288`: the launch string.
- `prepare.go:458`: the plain early return.
- `loop.go:482`: `failedCmd`.
- `loop.go:539`: `failedCmd` before `started`.
- `seats.go:122`: `needsRelaunch`.
- `link.go:268-276`: delivery.
- `link.go:272`: `close(l.first)`.
- `link.go:298` and `link.go:423`: `Plain`.
- `teamsync/session.go:395`: `if !t.cfg.Plain`.
- `wiring.go:60`: `Plain: c.Plain`.

Other things checked:

- `msgInternal` (`keeper.go:150`), `refuse(log, what, err)` (`link.go:156`), `maxSettings`, `inboundKey` and `msgSettings` (`keeper.go:114,130,147`) all exist.
- `homefs.Root.Create` opens with `O_EXCL|O_NOFOLLOW` and sets the mode with fchmod (`homefs.go:167-188`).
- The link handles `owed` only after `build` (`link.go:182`, `link.go:322`, `link.go:359`), so an adapter resolved in `build` is always there at `link.go:423`.
- Every launch-dependent keeper method runs after `prepare` starts. `writeSessionPID(nil)` at `prepare.go:412` runs inside `makeRun`, before `claudeArgv`. The only `keeper` literal is `newKeeper` (`prepare.go:110`), so no test calls a method on a keeper that `prepare` has not set up.
- The import graph has no cycle. cli already imports `state`, `paths` and `exits`, and none of the three imports cli.

**RunFacts can carry `claudeArgv` and `resume` unchanged**, given R1 and R2:

- `trustedRead(path, limit)` is a plain function (`prepare.go:272`).
- `k.state()` caches the root it opens (`prepare.go:549-559`). `Conversation()` calls it, so `takeSeat` (`prepare.go:623`) sees the same cached root, or retries a failed open, exactly as today.
- The four `no resume:` texts (`prepare.go:497,502,507,513`) come out the same. The first two are `"no resume: %v"` over `Conversation()`'s wrapped error. The last two are the adapter's own lines.
- `claudeDir` and `claudeConfigDir` (`prepare.go:521-534`) read `HOME` and `CLAUDE_CONFIG_DIR` through `k.cfg.Env`. `Home` and `ClaudeConfigDir` carry those same values (R9).
- `projectDir` (`prepare.go:538`) is used only by `resume`, so it moves.
- The two raw `return err` lines after `json.Marshal` (`prepare.go:473,477`) pass through `prepare` to `launch refused: %v` (`keeper.go:165`) unchanged, as long as the adapter returns them as they are.

**`type Mode = agent.Agent`**: it compiles and nothing breaks.

- Switches: none. No non-test code switches on `Mode`; every use is `==`, `!=` or an assignment.
- Conversions: none. No `Mode(n)` anywhere.
- Printing: none. No format string prints a `Mode`.
- Tests that construct a `Mode`: `cli/launch_test.go:43-88`, `parse_test.go:75`, `keeper/harness_test.go:287` and `host/lxc/unit/unit_test.go:113` use only the named constants, and `launch_test.go:101` compares them with `!=`. All of these still compile.
- Zero value: it changes from `0` to `""`. Today `0` means "not claude", so plain everywhere. After the alias, `agent.For("")` errors. The only `cli.Launch` values built without `ParseLaunch` are in `keeper/main_test.go:57-96`, which call `production()` and never reach `prepare`. Every other keeper test and `child_test.go:37` parse.
- Precedent: `forge.go:67` already has `type Result = team.ForgeResult`.
- The one edit needed is to the const block: `LaunchClaude Mode = iota + 1` must become plain aliases (R7).

## Findings

### R1 (major) Moving `maxSettings` breaks a keeper test

`keeper/prepare_test.go:241` ("over the cap") uses `maxSettings`, which is unexported. AS3 moves it
to `agent`, so the keeper tests no longer compile. That contradicts AS5's "the keeper ... tests
pass unchanged" and AS1's "every test compiles unchanged". Keeping a copy in the keeper for one
test would leave keeper code that only a test reads.

Replace AS3's "`msgSettings`, `maxSettings` and `inboundKey` move to `agent`." with:

> `msgSettings` and `inboundKey` move to `agent`, and `maxSettings` moves as the exported
> `agent.MaxSettings`. `keeper/prepare_test.go`'s settings-cap case then reads
> `agent.MaxSettings`. This is the seam's one test edit: one identifier, no assertion.

In AS5, replace "The keeper, link, engine, teamsync, wiring, cli and PK1 tests pass unchanged"
with:

> The keeper, link, engine, teamsync, wiring, cli and PK1 tests pass with no change other than
> AS3's one identifier.

### R2 (major) RunFacts' functions have no signatures, and WriteRunFile's argument and log line are unstated

Round 1 asked for types. The four functions are named but untyped. `WriteRunFile(name, data)` is
the worst case:

- `paths.settingsFile` is unexported, so the adapter cannot spell a bare name. It can only pass
  `f.Run.Settings()`, which is a full path.
- Today the log line is `write <full path>: <err>` (`prepare.go:480`). F7's resolution keeps the
  order only if that line is written inside the function.
- Neither point is in the text.

Replace AS3's four bullets with:

> - `ReadTrusted func(path string, limit int) ([]byte, error)` is the keeper's `trustedRead`.
> - `WriteRunFile func(path string, data []byte) error` creates `path` through the keeper's run
>   root: mode 0600, `O_EXCL`. `path` must be a file directly under `Run.Dir()`, such as
>   `Run.Settings()`. Any other path is `fault(exits.CodeInternal, msgInternal, ...)`. On a
>   failed write it logs `write <path>: <err>` and returns
>   `fault(exits.CodeRuntime, msgRunDir, exits.Name(Run.Dir()))`, which the adapter returns
>   unchanged.
> - `Conversation func() (state.Conversation, error)` opens the state root through the keeper's
>   cached `state()` and reads the conversation record. Its error reads
>   `the state root: <err>` or `the conversation record: <err>`, and the adapter logs
>   `no resume: <err>`.
> - `Logf func(format string, args ...any)` is the keeper's `logf`.
>
> `Inbound` is a `string` (`state.InboundAccept` or `state.InboundRefuse`), `ResumeRecorded` a
> `bool`, and `Cwd` and `Home` are strings.

### R3 (major) AS6's match does not catch the ways round that its fixture says it is red on

The fixture is said to hold "a `switch` on the field" and "a `slices.Contains` over the values".
The three bullets miss these:

- `slices.Contains(state.Launches, l.Launch)` uses no listed name, so nothing reports it, and
  `state.Launches` is new in this very section. `slices.Contains([]string{"claude", "plain"}, x)`
  is missed as well.
- `const p = "plain"; launch.Launch == p` is not a string literal, so a match on the syntax misses
  it.
- `switch launch.Launch { case "plain": }` is a comparison only if the rule reads a case as one.
  The text does not say it does.
- `string(mode) == "claude"`, with `mode` an `agent.Agent`, has a `string` operand, not an
  `agent.Agent` one, and is not the field.

GoProjectRules.md lines 12-14 say a rule that holds only against the original spelling "is
documentation, not a gate".

Replace AS6's three bullets with:

> - any use of the object `state.LaunchClaude`, `state.LaunchPlain`, `state.Launches`,
>   `cli.LaunchClaude`, `cli.LaunchPlain`, `agent.Claude` or `agent.Plain`. A use is a
>   `types.Info.Uses` entry, so an unqualified use inside the package that declares it counts.
>   A declaration is not a use.
> - any `==` or `!=`, `switch` tag or map key type whose type is `agent.Agent`.
> - any `==` or `!=`, or `switch` case against a tag, where one operand is a constant expression
>   whose value (`types.Info.Types[e].Value`) is `"claude"` or `"plain"`, and the other is the
>   field `wire.Launch.Launch` or `state.SessionPID.Launch`, or a conversion to `string` of an
>   `agent.Agent`.
> - any call that has the field `wire.Launch.Launch` or `state.SessionPID.Launch` as one argument
>   and, as another, a composite literal holding a constant whose value is a launch value.

These cause no false positives on the finished tree:

- `cli/launch.go:211`'s `filepath.Base(a) == "claude"` compares with a call result.
- `exits.CodeClaude`, `wire.KindClaude` and `guest/check.go`'s literal are never compared with a
  launch field or an `agent.Agent`.

### R4 (major) Exemptions are `(pkg, fn)` rows, not files, and the list does not match the finished tree

`table.go:106-112`'s `allowance` is `{pkg, fn, reason}`, where `fn` is `"func"` or
`"Type.method"` and `""` means a package-level declaration. There is no file form, so
"`cli/launch.go`" cannot be a row.

With R3's first bullet, the finished tree needs these rows:

- state: the package-level declaration of `Launches`, and `SessionPID.check` (`sessionpid.go:50-53`).
- wire: `Launch.check` (`control.go:298-305`, which uses `state.Launches`).
- cli: the package-level `LaunchClaude`/`LaunchPlain` declaration, `ParseLaunch` (`launch.go:131`),
  and `Launch.fromFlags` (`launch.go:144-146`).

`checkClaudeCmd`, CL2's rule, compares no launch value, so it needs no row.

Without R3, wire's row would match nothing. `table.go` has no check for rows that match nothing,
so it would be a hole nobody sees.

Replace "Outside package `agent`, its exemptions are rows ... CL2's claude argument rule)." with:

> Package `agent` is its owner. Elsewhere its exemptions are `allowance` rows in
> `qa/rules/table.go`, each with its reason:
>
> - `{state, "", the one list of launch values}`
> - `{state, "SessionPID.check", session.pid's value check}`
> - `{wire, "Launch.check", the launch frame's value check}`
> - `{cli, "", cli's names for the launch values}`
> - `{cli, "ParseLaunch", --resume-recorded refused on plain}`
> - `{cli, "Launch.fromFlags", the spelling}`
>
> CL2's claude argument rule (`Launch.checkClaudeCmd`) compares no launch value and needs no row.

### R5 (minor) The adapter's type and `For`'s signature are not given

"`agent.For(Agent)` returns the adapter", but no type is named that holds both AS2's
`Capabilities` and AS3's `Preparer`. Add to AS1:

> `For(a Agent) (Adapter, error)`, where `Adapter` is
> `struct { Capabilities Capabilities; Preparer Preparer }` and `Preparer` is
> `interface { Prepare(f RunFacts) (Launch, error) }`. The keeper keeps the `Adapter` it
> resolves, and the link keeps the `Capabilities`.

### R6 (minor) `RunFacts.Cmd` is read by nothing

The keeper appends `Launch.Args` after CMD itself. Today `claudeArgv` only copies `Cmd`, and
`checkClaudeCmd` already ran in cli. Delete "`Cmd`," from AS3's field list, and add:

> The keeper's argv is CMD followed by `Launch.Args`.

### R7 (minor) The alias needs its const block, and the zero value should be stated

Replace AS1's "`cli.Mode` becomes an alias ... so every test compiles unchanged." with:

> `cli.Mode` becomes an alias, `type Mode = agent.Agent`, declared as
> `const (LaunchClaude = agent.Claude; LaunchPlain = agent.Plain)` (no `iota`). Every test that
> uses the constants compiles unchanged (see R1 for the one that does not). The zero `Mode` is
> `""`, which `For` refuses. Only `cli.ParseLaunch` makes a `Launch` that reaches the keeper's
> `prepare`, and it always sets one of the two.

### R8 (minor) Adding an agent touches more than AS1 says

Both checks still carry the literal text `"launch is not claude or plain"` (`sessionpid.go:53`,
`control.go:305`). An agent is also its `agent` constant and its case in `For`. Replace "A new
agent is its adapter, one entry in `state.Launches`, and its spelling in cli." with:

> A new agent is its constant, its case in `For` and its adapter in `agent`, one entry in
> `state.Launches`, the two check messages that name the values, and its spelling in cli.

### R9 (minor) `Home` and `ClaudeConfigDir` must be passed as the keeper reads them today

`claudeDir` joins `k.cfg.Env("HOME")` raw (`prepare.go:525`), even when it is `""`.
`claudeConfigDir` drops a relative or unclean value (`prepare.go:528-534`), and
`main_test.go:141-146` pins both. Add to AS3:

> `Home` is the launch's `HOME` as read, unchecked. `ClaudeConfigDir` is the keeper's
> `claudeConfigDir()`, the same value `launchMsg` sends. `claudeDir` and `projectDir` move to
> `agent` with `resume`.

## 3. New contradictions or vagueness

- R1: AS3's move contradicts AS5's "keeper tests pass unchanged".
- R3: AS6's fixture claim contradicts its own match bullets.
- R2, R5 and R4's file-form row are new vagueness.

Nothing else contradicts: AS2 and AS4 agree with AS1a, AS7 still holds, and DV1's "never to a
`refuse` launch" agrees with the lowering to `wait`.

## 4. Is `project_agent_branch` implementable?

Yes, with R3 and R4. It is a whole-package `go/analysis` pass in the style of
`keeperlogbarrier.go`:

- skip test files with `isTestFile`;
- skip the owner package and the `allowance` rows using `ownedBy` and `funcName`;
- resolve objects through `pass.TypesInfo.Uses`, keyed on the package-path suffix, so that a
  fixture under `testdata/src/.../internal/teamchat/member/{agent,state,wire,cli}` and the real
  module share the table.

It is free of false positives on the finished tree. The R4 rows cover every remaining non-test use
(found by searching every non-test file under `internal/teamchat` for the constants and for
literal comparisons with `.Launch`). It is red on the tree before the move, through every AS1a
site: the keeper's `cli.LaunchClaude` and `state.Launch*` uses, and the link's
`state.LaunchPlain`.

Gap it should declare: a value that reaches the field through a function's result, such as
`isPlain(l.Launch)` with `"plain"` inside `isPlain`, is caught only by the first bullet, and only
if `isPlain` uses a named constant.

## Not checked

- Plan 00215's `research/opencode-bus-member-design.md` section 5.2.
- HK1 to HK3's text in SPEC-n3.
- Whether the golden tests of AS5 can be written against the current harness.
- Nothing was compiled or run, so every "compiles" above is from reading the code.
