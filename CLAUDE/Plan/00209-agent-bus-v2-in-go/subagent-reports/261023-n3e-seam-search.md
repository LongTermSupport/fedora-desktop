The search found no unallowed instance of the class outside the agent package. Everything outside it is either an allowed check, a plain copy of the launch value, or a carrier whose value comes from the adapter. I did not run go build, go test or the detector.

**Rule exposure.** My first text search was run over all of `src-go` and printed lines from `src-go/qa/rules/testdata/src/fix/...` (`launchuse/*.go`, `state/state.go`, `wire/wire.go`, `cli/cli.go`, `elsewhereagent/elsewhere.go`). Those fixtures show the rule's flagged shapes and the rule id `project_agent_branch`. I did not open `agentbranch.go` or its testdata, and I excluded `src-go/qa/rules` from every finding below. The coordinator should know this search was not fully blind to the rule's examples. The agent package's own doc comment also names `project_agent_branch`.

**Searches run**

- Text search over `cmd` and `internal`, both test and non-test files, for these literals and constants:
  - `"claude"`, `"plain"`, `LaunchClaude`, `LaunchPlain`, `agent.Claude`, `agent.Plain`, `Launches`, `EqualFold`
  - `\.Launch\b`, `\.Mode\b`, `\bPlain\b`, `cfg.Launch`, `state.Launch`, and `Launch\b` over all non-member packages
- Reading of `link.go` `build()`, `keeper/loop.go`, `keeper/prepare.go`, `keeper/seats.go`, `cli/launch.go`, `wire/control.go`, and the engine, teamsync and wiring `Plain` flow.
- I looked for ToLower, HasPrefix, Contains, switch and map forms on the launch value. None exist outside the lines below.

**Non-test code**

Inside the agent package, not itemised: `agent.go` has the `Claude` and `Plain` constants and the `case Plain` at `agent.go:87`.

Allowed checks:

- `state/sessionpid.go:16-22` defines the constants and the `Launches` list.
- `state/sessionpid.go:57` is the session.pid value check, using `slices.Contains(Launches, p.Launch)`.
- `wire/control.go:304` is the launch-frame check, using `slices.Contains(state.Launches, l.Launch)`.
- `cli/launch.go:14-19` aliases `Mode` to `agent.Agent` and `LaunchClaude`/`LaunchPlain` to `agent.Claude`/`agent.Plain`.
- `cli/launch.go:132` is `if l.Mode == LaunchPlain`, inside `ParseLaunch`.
- `cli/launch.go:145-147` is `Launch.fromFlags` setting `Mode` from the `--claude` flag (`flagClaude`).
- `cli/launch.go:62-63` holds the flag spellings `"claude"` and `"plain"`.

Anywhere else (copies and dispatch, not decisions):

- `keeper/prepare.go:126` calls `agent.For(k.cfg.Launch.Mode)`, which dispatches through the agent package.
- `keeper/prepare.go:449` copies `string(k.cfg.Launch.Mode)` into the session.pid `Launch` field.
- `keeper/loop.go:293` copies `string(k.cfg.Launch.Mode)` into the `wire.Launch` frame.
- `link/link.go:270` calls `runagent.For(runagent.Agent(launch.Launch))`, again dispatching through the agent package.

No decision is made outside the agent package. Neither session.pid's launch field nor the frame's launch value is compared or switched on anywhere else.

**Carriers of a launch-derived boolean**

- The `Plain` flag is set in only one place. `link/link.go:301` sets it on `engine.Config` as `Plain: !l.caps.SessionStart`, and `link/link.go:426` sets it on the wiring config the same way. `l.caps` is `adapter.Capabilities` from `link.go:274`, so the flag comes from the adapter, not from a comparison of the launch value.
- `engine/engine.go:51` and `engine/engine.go:97` declare the field.
- `engine/teams.go:133` copies it.
- `wiring/wiring.go:60` copies it into the teamsync `Config`.
- `teamsync/session.go:395` branches on `!t.cfg.Plain`. The decision is on the carried boolean, not on the launch value.
- `keeper/loop.go:478`, `keeper/loop.go:535` and `keeper/seats.go:122` read `k.adapter.Capabilities.SessionStart` and `.Delivery`, which is the sanctioned adapter path.
- `link.go:275-281` derives `delivery` and `firstOnce` from `caps`.

**Unsure**

- `cli/launch.go:212`, `cli/launch.go:221` and `cli/launch.go:224` use `filepath.Base(a) == "claude"` to find Claude Code's argument in `Cmd`, and they use the resulting `Launch.Claude` index. This is `checkClaudeCmd`, called from `ParseLaunch` at `launch.go:138` on the Claude branch. It matches a program name in the command and does not read the launch value. It sits inside `cli`, in the parse path. I would not count it unless the class covers program-name matching.
- `host/lxc/unit/unit.go:68` passes the literal `"claude"` as an argument to `--resume-recorded --`. It is command text and decides nothing.

**Test files** (reported separately)

No branching on the launch value apart from these:

- `keeper/harness_test.go:287`: `w.launch.Mode == cli.LaunchClaude` decides whether the fake link sends a Started message. This is a real comparison outside the agent package.
- `keeper/prepare_test.go:312`: `pid.Launch != state.LaunchClaude` is an assertion.
- `keeper/launch_golden_test.go:99`: `pid.Launch != m.Launch.Launch` compares session.pid's launch with the frame's launch. It decides nothing about the agent.
- `state/sessionpid_test.go:22`: `p.Launch != LaunchClaude` is an assertion.
- `cli/parse_test.go:75`: `inv.Launch.Mode != LaunchPlain` is an assertion.
- `host/lxc/unit/unit_test.go:113`: `l.Mode != cli.LaunchClaude` is an assertion.
- `cli/launch_test.go`: `LaunchClaude`/`LaunchPlain` appear as table values, with no comparison.
- `keeper/launch_golden_test.go:181-299` uses `state.LaunchClaude`/`LaunchPlain` as table values and passes `m.Launch.Launch` through.
- `link/link_test.go:314,796,839`, `link/hooks_test.go:378`, `link/deliver_test.go:95` and `wire/control_test.go:43,47,207,230` build frames. `kit_test.go:31` and `prepare_test.go:343` hold the literals `"launch":"claude"` and `"launch":"plain"` in JSON.
- `teamsync/run_test.go:261` and `wiring/wiring_test.go:169,175` set or assert `Plain` directly.
- `agent/agent_test.go` is inside the agent package.

**What each technique found**

- **Text search** found all literals and constants, which cover every real comparison, including the test-file ones, and it proved nothing else compares them.
- **Reading** found things text search could not:
  - The `Plain` bool is derived from `caps.SessionStart` and is not a literal comparison.
  - The `string(Mode)` copies are plain copies.
  - `adapter.Capabilities` is the allowed path.
  - `cli.Launch.Claude` is the program-name index.
  - No `local := launch; if local ==` pattern exists, which text search alone could not rule out.
