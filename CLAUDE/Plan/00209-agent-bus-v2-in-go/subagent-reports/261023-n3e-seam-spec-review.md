# Review: SPEC-n3 section 6.1, the agent seam (round 1)

Reviewed: SPEC-n3.md 6.1 at 24d9401ab (feat/bus-v2-n3e-seam), against Plan 00215's design
section 5 (F44) and the member code in this worktree. Read only; no test or QA run.

Verdict: the direction is right and the keeper half is close to buildable. Two rows (AS2, AS4)
contradict the section's own "only what the code branches on today" and "not one byte" rules
and must be rewritten before the build. The site list (AS1) and the rule (AS6) need concrete
lists. Findings are in order of severity.

## Where the code tells claude from plain today

Every site, from a search for the launch constants, `Plain` and `"claude"`/`"plain"`:

| Site                                                                                                                  | What it decides                                                                |
| --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| `keeper/prepare.go:439-441`                                                                                           | session.pid's `launch` string from `cli.Mode`                                  |
| `keeper/prepare.go:458`                                                                                               | claudeArgv: plain gets CMD alone                                               |
| `keeper/loop.go:288-290`                                                                                              | the launch frame's `launch` string from `cli.Mode`                             |
| `keeper/loop.go:482`                                                                                                  | CMD failing to start is `launch_failed` (claude only)                          |
| `keeper/loop.go:539`                                                                                                  | CMD ending before the first SessionStart is `launch_failed` (claude only)      |
| `keeper/seats.go:122`                                                                                                 | report.json `needs_relaunch`: a refuse launch whose desired now accepts        |
| `link/link.go:268-276`                                                                                                | delivery `none` for plain; refuse lowers to `wait`                             |
| `link/link.go:272`                                                                                                    | plain closes `first` at once (no SessionStart awaited)                         |
| `link/link.go:298`                                                                                                    | `engine.Config.Plain`                                                          |
| `link/link.go:423`                                                                                                    | `engine.TeamConfig.Plain` for an owed leave                                    |
| `engine/teams.go:133`, `engine/engine.go:51,97`                                                                       | the `Plain` flag carried to each team                                          |
| `wiring/wiring.go:60`                                                                                                 | the same flag into `teamsync.Config`                                           |
| `teamsync/session.go:395` (`teamsync.go:54`)                                                                          | `back` is posted without waiting for the first SessionStart                    |
| `cli/launch.go:131,144-146,211-227`                                                                                   | the spelling, `--resume-recorded` refused on plain, CL2's claude argument rule |
| `state/sessionpid.go:53`, `wire/control.go:304`                                                                       | record and frame checks of the value set                                       |
| tests: `keeper/harness_test.go:287`, `link/*_test.go` (5), `host/lxc/unit/unit_test.go:113`, cli and state/wire tests | fixtures and assertions using the constants (22 uses)                          |

Not branches, but claude-named and inert for plain because only hook facts feed them:
`wire.Launch.ClaudeConfigDir` (sent for every run, `loop.go:298`), the link's `claudeDir`
(`link.go:278-281`), `KindClaude`/`onClaude`/`claudeAlive` and the pidfd on Claude Code
(`loop.go:505-589`), session.json's `claude`, `KindStarted`. No hook op is refused on a plain run
today (`link/hooks.go:54-115` has no launch test). No code branches on the status line, the end
reason or transcript proof by launch: the status line is the kit settings' key, the SessionEnd
reason is recorded by the hook op, and proof runs only behind socket delivery.

## Findings

### F1 (blocker) AS2 lists fields the code does not branch on, and leaves the rest unnamed

AS2 says Capabilities has "only the fields the code branches on today", then lists `Facts`,
`Proof`, `StatusLine` and `EndReason`, none of which any site above branches on, and ends with
"the flags AS1's list needs", which an implementer must guess. Those four are opencode's
questions (the design's), so they are added with opencode, not ahead of it (AS1's own "never
ahead of them"). The two questions the code does ask are delivery and whether a SessionStart is
awaited; `needs_relaunch` follows from delivery.

Proposed AS2:

> **AS2. `Capabilities`** is two fields, the two questions the code asks today. `Delivery` is the
> agent's best mode, `state.DeliverySocket` or `state.DeliveryNone` (the strings `status` already
> shows); the run's `inbound: refuse` lowers `socket` to `wait` in the link, as today.
> `SessionStart` is whether the run awaits the agent's first SessionStart. `claude` is
> `{socket, true}`; `plain` is `{none, false}`. Fields for proof, facts, a status line or an end
> reason are added with the first adapter that answers them differently.

### F2 (blocker) AS4 changes a plain launch and refactors fixed, built delivery code

Two problems. (a) "The hook ops of HK1 to HK3 are accepted only on a run whose `Facts` is
`hooks`" refuses hook ops on a plain run, which today are accepted and answered
(`link/hooks.go:54-115`; CMD gets `TEAMCHAT_RUNTIME` on every launch, `loop.go:473`). That is a
changed answer (HK1's table becomes `{}`), against the section's opening "not one byte" and
against HK1 as fixed. (b) A `Channel` built "only once a hook has supplied the socket and a
transcript", with `Deliver`/`Written`/`Observations`, is a reshaping of the engine's delivery
loop: today the engine holds an `inbox.Dialer` from its start (`engine/engine.go:61`), judges
liveness in `live()` (`engine/deliver.go:268-274`) and owns the proof timers
(`engine.go:147-200`). Moving that behind a Channel changes `engine.Config` and so the engine
tests AS5 says pass unchanged. There is one delivery path today, so a Channel interface has one
implementation and nothing to shape it; opencode's (HTTP, an event stream) will decide its form.

Proposed AS4:

> **AS4. Delivery keeps its existing seam.** The engine's `inbox.Dialer` (`engine.Config.Inbox`)
> is the delivery seam; the link makes it when `Capabilities.Delivery` is `socket` and the run is
> not lowered to `wait`, as today. Transcript proof, the quiet clock and the hook ops stay where
> they are and are accepted on every run, as today. The `Channel` of Plan 00215's section 5.2,
> and the refusal of hook ops on a run whose facts come from its client, are written with the
> opencode adapter, the first that delivers another way; until then no adapter can reach the
> ledger, receipts, the flood budget, the end or the seats, because none is given them.

If the lead wants the Channel now regardless, AS5 must then say which engine tests change and
why, and HK1 must state a plain run's hook answer; as written the two rows cannot both hold.

### F3 (major) AS1's site list is incomplete and mis-describes one site

AS1 names `keeper` (three files), `link`, `cli` and "`engine`'s `Plain` flag". It misses
`teamsync/session.go:395` and `wiring/wiring.go:60`, which carry the same flag, and says the
engine flag "becomes a question put to the adapter", but the engine and teamsync are never given
an adapter. "whether the seats of a `refuse` launch wait" is not what `seats.go:122` does: it
sets report.json's `needs_relaunch`. "socket facts from hooks" and "transcript proof" name no
site. The two launch-string writes (`prepare.go:439`, `loop.go:288`) are not capabilities at all.
`engine`'s `Plain` is redundant with the link closing `first` at once (`link.go:272`), but
`teamsync/run_test.go:261` and `wiring/wiring_test.go:169-175` set and assert it, so removing it
would change tests; it should stay.

Proposed replacement for AS1's second half:

> The sites, and what each asks: `prepare.go:439` and `loop.go:288` write `string(agent)`;
> `prepare.go:458` calls the adapter's Preparer; `loop.go:482` and `loop.go:539` count a CMD
> that fails to start, or ends before the first SessionStart, as `launch_failed` when
> `SessionStart` is true; `seats.go:122` is `needs_relaunch` when `Delivery` is `socket`, the
> run is `refuse` and desired accepts; `link.go:268-276` takes `Delivery`, lowered by `refuse`;
> `link.go:272` closes `first` at once when `SessionStart` is false; `link.go:298` and
> `link.go:423` set `engine.Config.Plain` and `engine.TeamConfig.Plain` to `!SessionStart`.
> `engine`, `teamsync` and `wiring` keep their `Plain` boolean unchanged and never see the
> adapter. The claude-named facts that only hook ops feed (`wire.Launch.ClaudeConfigDir`, the
> link's Claude directory, `KindClaude` and the keeper's pidfd on Claude Code, session.json's
> `claude`) stay where they are.

### F4 (major) Where the adapter comes from, and the cli type, decide whether tests change

"The keeper and the link take the adapter they were given" reads as a new field in
`keeper.Config` and `link.Config`, which the keeper harness and link tests would then have to
set. "`cli` parses the spelling into an `Agent`" changes `cli.Launch.Mode`'s type, used in
`keeper/harness_test.go:287` and `host/lxc/unit/unit_test.go:113`. Either breaks AS5's "pass
unchanged".

Proposed text (AS1):

> The keeper resolves `agent.For` once, from `cfg.Launch`, at the start of `prepare`; the link
> once, from the launch frame's `launch`, in `build`. Neither config gains a field. `cli.Mode`
> becomes an alias of `agent.Agent` (`type Mode = agent.Agent`, `LaunchClaude = agent.Claude`,
> `LaunchPlain = agent.Plain`), so every test compiles unchanged. `agent.Claude` and
> `agent.Plain` are `state.LaunchClaude` and `state.LaunchPlain`, so the strings have one home;
> `agent` imports `state`, `paths` and `exits`, never `cli`, `keeper` or `link`.

The unknown-value error needs an exit too: the frame check (`wire/control.go:304`) and cli's
parse already refuse any other value, so `agent.For`'s error is unreachable from both; say the
keeper returns `fault(exits.CodeInternal, msgInternal, ...)` and the link `refuse(log, "start", err)`, the existing forms.

### F5 (major) AS3's "run facts" must be named; claudeArgv reads more than it lists

claudeArgv and resume read today: the kit's settings path and plugin dir (`k.kit`), a trusted
read (`trustedRead`, keeper's, also used for desired.json), `maxSettings` and `inboundKey`, the
inbound value, CMD, `ResumeRecorded`, the run's settings path (`k.run.Settings()`, the final run
dir, since claudeArgv runs after the rename), the state root opened lazily and cached for the
seats (`k.state()`, `prepare.go:549-559`, reused by `takeSeat` at 623), the conversation file
path, `HOME` and `CLAUDE_CONFIG_DIR` (also used by `launchMsg`), cwd, and `k.logf` for the
settings refusal and the five "no resume:" lines. Its refusal is keeper's
`fault(exits.CodeRuntime, msgSettings, ...)`. The Prepare signature can carry all of it without
a state root to write, but only if it is spelled out; otherwise the implementer will hand over
`*homefs.Root` (which can write) or the whole keeper.

Proposed AS3 (replacing the signature and its last sentence):

> `Prepare(f RunFacts) (Launch, error)`, where `RunFacts` is `{Cmd []string; Inbound string; ResumeRecorded bool; Kit paths.Kit; Run paths.Run; Cwd string; Home string; ClaudeConfigDir *string; ReadTrusted func(path string, limit int) ([]byte, error); Conversation func() (state.Conversation, error); Logf func(format string, args ...any)}`. `ReadTrusted` is the
> keeper's `trustedRead`. `Conversation` is the keeper's lazy state-root open and read; its error
> reads `the state root: <err>` or `the conversation record: <err>`, so the adapter's
> `no resume: <err>` line is today's text. `Launch` is `{Args []string; Files []RunFile}`, and a
> `RunFile` is `{Path string; Data []byte}`, whose `Path` must be a file directly under
> `Run.Dir()` (anything else is the keeper's internal error). `msgSettings`, `maxSettings` and
> `inboundKey` move to `agent`; the refusal stays `exits.CodeRuntime` with the same template.

### F6 (major) AS6 cannot go green as written, and does not say what it matches

(a) Exemptions: cli's parse compares the value (`cli/launch.go:131,144-146`) and AS1 keeps it
there, and 22 test uses of the constants exist (keeper, link, cli, state, wire, and
`host/lxc/unit` outside the member). Without a cli and a test exemption the rule fails on the
finished seam. (b) "any comparison against a launch value" is undefined: `"claude"` is also
`exits.CodeClaude`, `wire.KindClaude`, cli's `flagClaude`, `guest/check.go:106` and CL2's
`filepath.Base(a) == "claude"`, so a literal match is wrong and a constant-only match misses
`launch.Launch == "plain"`. (c) This repo's rule style needs an `analysistest` fixture red on
the ways round, not only red on the old tree, and exemptions as rows in `src-go/qa/rules/table.go`
with reasons (GoProjectRules.md lines 7-14).

Proposed AS6:

> **AS6. The defence.** A project rule, `project_agent_branch`, reports, in non-test files of
> `internal/teamchat/...`: any use of `state.LaunchClaude`, `state.LaunchPlain`,
> `cli.LaunchClaude`, `cli.LaunchPlain`, `agent.Claude` or `agent.Plain`; any `==`, `!=`, `switch`
> case or map key whose operand's type is `agent.Agent`; and any comparison of a string literal
> that is a launch value with the field `wire.Launch.Launch` or `state.SessionPID.Launch`.
> Outside package `agent`, its exemptions are rows in `qa/rules/table.go`, each with its reason:
> `state`'s session.pid check, `wire`'s launch-frame check, and `cli/launch.go` (the spelling and
> CL2's claude argument rule). The rule is proven red by `analysistest` on a fixture holding each
> form above and the ways round it (a local copy of a constant, a `switch` on the field, a
> `slices.Contains` over the values), red on the tree before the move with every site of AS1,
> and green after. Its catalogue row in GoProjectRules.md says it is found in the keeper and
> link launch comparisons the seam replaced.

### F7 (minor) keeper.log on a failed settings write differs

Today the settings file is written (`prepare.go:479`) before the resume rule runs, so a run
whose write fails logs no "no resume:" lines and never opens the state root there. With the
adapter computing and the keeper writing, the resume lines come first. The exit and message are
the same and nothing on disk changes, but the section claims not one byte of a launch. Either
say it, or have the adapter return its log lines in `Launch` for the keeper to write after
`Files`. Proposed (the simpler): add to AS3 "On a run whose settings write fails, keeper.log now
holds the resume rule's lines before the write's line; this is the seam's one difference, and no
exit, message or file changes."

### F8 (minor) `Launch.Env` is unused and nondeterministic

No adapter adds an environment variable today (`startCmd`, `loop.go:473`, adds only
`TEAMCHAT_RUNTIME` and the kit's), and a `map` appended to the environment has no fixed order,
which AS5's environment golden would catch only by luck. Drop `Env` (opencode adds it with its
password variable) and say AS5's environment golden pins the keeper's `TEAMCHAT_RUNTIME`, `PATH`
and kit variable for both launches.

### F9 (minor) Adding opencode still touches three value lists

"a second agent program is a new adapter and not a refactor" is true only in the sense of
adding a value: cli's spelling, `state/sessionpid.go:53` and `wire/control.go:304` each list the
values. Propose one list, `state.Launches = []string{LaunchClaude, LaunchPlain}`, read by both
checks, and AS1's "Other values are added with their adapters" extended to "...: a new value is
its adapter, one entry in `state.Launches` and its spelling in cli". CL2's CMD rule for a new
agent also lives in cli (moving the claude rule into Prepare would move a usage refusal from
parse time to after the kit is resolved and the run is made, a changed order and exit), which
AS1 should say is deliberate.

## Conflicts with section 6's fixed rows and Gate A

- HK1/HK2 against AS4: covered in F2; the hook-op gate would change HK1's answer on a plain run.
- DV1 ("never to a session launched `refuse`") agrees with AS2's lowering to `wait`.
- AS2 should reuse `state.Delivery*`: those are visible values (`status`, status.json); a new
  spelling such as the design's `push` would be an owner-visible change. The spec already uses
  `socket`; F1's text keeps it.
- Gate A (AS7): no conflict. `internal/teamchat/member/agent` and `project_agent_branch` are not
  owner-visible, and "agent" is not on `qa/teamchatseparate`'s v1 list. With F8's `Env` dropped,
  there is no route for an environment variable either.
