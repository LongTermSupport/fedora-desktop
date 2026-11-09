# Narrow check: SPEC-n3 section 6.1 at 9f5bc45f9 against round 2's R1 to R9

Read only. Diff read: `git diff be375d544 9f5bc45f9`. Section 6 (DV1 to PT1) and 6.1 read
whole at 9f5bc45f9. Nothing compiled or run.

Verdict: no blocker, no major. One minor finding (N1).

## 1. Did R1 to R9 land?

| Finding | Landed? | Note                                                                                                                                                                                                                          |
| ------- | ------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| R1      | yes     | AS3 moves `msgSettings` and `inboundKey`, exports `agent.MaxSettings`, and names the one test edit. AS5 says "no change other than AS3's one identifier".                                                                     |
| R2      | yes     | All four signatures, `WriteRunFile`'s path rule, its `write <path>: <err>` line and its `msgRunDir` fault are in. `Inbound`, `ResumeRecorded`, `Cwd` and `Home` have types.                                                   |
| R3      | yes     | The four match bullets are the proposed text, with only punctuation changed. The fixture list now also names a named string constant, `slices.Contains` over `state.Launches` or over a literal list, and `string(agent) ==`. |
| R4      | yes     | The six `allowance` rows and the `checkClaudeCmd` sentence are in, word for word.                                                                                                                                             |
| R5      | yes     | `For`, `Adapter`, `Preparer`, and which side keeps what, are in AS1.                                                                                                                                                          |
| R6      | yes     | `Cmd` is gone from `RunFacts`. "The keeper's argv is CMD followed by `Launch.Args`" is in.                                                                                                                                    |
| R7      | yes     | The const block, no `iota`, the zero value and the `ParseLaunch` sentence are in. "(see R1 ...)" became "AS3 names the one identifier edit", which says the same thing.                                                       |
| R8      | yes     | The proposed sentence is in.                                                                                                                                                                                                  |
| R9      | yes     | `Home` and `ClaudeConfigDir` are in AS3 as proposed. "`claudeDir` and `projectDir` move to `agent` with `resume`" is in AS3's body.                                                                                           |

The round also added text that was not proposed: the `json.Marshal` sentence in AS3, the Plan
00215 Task 3.2 sentence in AS4, and "a failed settings write" in AS5's golden cases. All three
match round 2's section 2 and the code (`prepare.go:473,477,479-481`).

## 2. Contradictions

None with AS1a, AS2, AS4, AS5 or AS7, or with DV1 to PT1.

- DV1's "never to a session launched `refuse`" agrees with AS2's lowering to `wait`.
- PK1's appended `--plugin-dir <kit>/plugin/teamchat` agrees with AS3's order: `--resume`,
  `--plugin-dir`, `--settings`. This matches `prepare.go:484-487`, and `paths.Kit.PluginDir()`
  is at `paths.go:215`.
- AS3's "no adapter is given a root it can write" still holds. `WriteRunFile` is a function the
  keeper supplies, restricted to files directly under `Run.Dir()`.
- The new AS6 bullets against the six rows: every non-test use left after the move is covered.
  These are `cli/launch.go:17-18`, `131`, `144` and `146`, `sessionpid.go:53`, `control.go:304`
  and the new `state.Launches` declaration. The current keeper and link sites (`prepare.go:439-441`
  and `458`, `loop.go:288-290`, `482` and `539`, `seats.go:122`, `link.go:268` and `423`) are
  exactly AS1a's, so the rule is red before the move.

### N1 (minor) AS1 hands `For` a `cli.Launch` where its new signature takes an `Agent`

AS1 now gives `For(a Agent) (Adapter, error)`, but still says the keeper resolves it "from
`cfg.Launch`". `keeper.Config.Launch` is the `cli.Launch` struct, and its launch value is the
field `Mode` (`k.cfg.Launch.Mode`, used at `prepare.go:440` and `458`, `loop.go:289`, `482` and
`539`, and `seats.go:122`). As written, the call does not type-check. The link side, "the launch
frame's `launch`", is a `string` that needs `agent.Agent(...)`. This conversion is not one of
the forms AS6 reports, so it can stay implied.

Fix: in AS1, replace "The keeper resolves it once, from `cfg.Launch`, at the start of
`prepare`" with:

> The keeper resolves it once, from `cfg.Launch.Mode`, at the start of `prepare`

## 3. Cited code names

All exist as spelled, except `state.Launches`, which the spec introduces and so is new by design.

- `claudeConfigDir`: `keeper/prepare.go:528`, `func (k *keeper) claudeConfigDir() *string`.
  `launchMsg` sends it at `loop.go:298`.
- `launchMsg`: `keeper/loop.go:287`.
- `trustedRead`: `keeper/prepare.go:272`, `func trustedRead(path string, limit int) ([]byte, error)`.
  This matches `ReadTrusted`'s type.
- `msgRunDir`: `keeper/keeper.go:139`. `msgInternal`: `keeper/keeper.go:150`.
- `state.InboundAccept` and `state.InboundRefuse`: `member/state/values.go:18-19`.
- `paths.Run.Settings`: `internal/teamchat/paths/paths.go:567`, `func (r Run) Settings() string`.
  The package is `internal/teamchat/paths`, not one under `member/`, which agrees with AS1's
  `paths` import.
- `Launch.check`: `wire/control.go:298`, with a pointer receiver.
- `SessionPID.check`: `state/sessionpid.go:50`.
- `ParseLaunch`: `cli/launch.go:112`.
- `Launch.fromFlags`: `cli/launch.go:143`, with a pointer receiver.
- `Launch.checkClaudeCmd`: `cli/launch.go:210`, with a pointer receiver.
- `allowance`: `qa/rules/table.go:108-112`, `{pkg, fn, reason}`. `funcName` (`table.go:386-400`)
  strips a pointer receiver, so `"Launch.check"`, `"Launch.fromFlags"` and `"SessionPID.check"`
  match as written.
- Also checked: `state.Conversation` (`member/state/conversation.go`), the `logf` signature
  (`keeper.go:265`), and `k.state()` (`prepare.go:549`). `fault` is `exits.New` underneath
  (`keeper.go:259-261`), so the claude adapter can build the settings refusal without importing
  the keeper.
