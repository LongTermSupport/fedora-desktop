# n3e-seam: project_agent_branch widened (conformance M1, M2, m1 to m4)

Branch `feat/bus-v2-n3e`, from `c7fb2321e`. Inputs:
`subagent-reports/261023-n3e-seam-conformance.md` (the conformance review) and
`subagent-reports/261023-n3e-seam-search.md` (the independent search), both brought into this
branch unchanged in content.

Commits: `d5ac63929` the red fixture (and the two reports), `c08aaf231` the widening, then the
docs (SPEC-n3 AS6, the catalogue) and this report, then the journal entry.

## M2: the rule widened, test first

### What the widened rule matches

A comparison (`==`, `!=`), a call, an index expression, or a `switch` tag and its cases is
reported when its operands hold both:

- a launch operand: the field `wire.Launch.Launch` or `state.SessionPID.Launch`, or a
  conversion to a string of an `agent.Agent`, directly or as an argument of a call that is
  itself an operand, through any depth of calls (`launchOperand` in
  `src-go/qa/rules/agentbranch.go`);
- a launch constant: a constant expression whose value is `"claude"` or `"plain"`, or a list
  split on `,`, `|` or `;` with one of them as a word (`namesLaunchValue`). Among a call's other
  arguments, and as the map of an index expression, a composite literal holding one also counts
  (`isLaunchLiteral`).

The forms it matched before are unchanged, and so are their messages. The new messages are
`<operand> given to a call with <value>`, `<operand> used as an index into a literal holding a launch value`, and `a call over <operand> compared with <value>`.

Declared gaps, in the doc string, SPEC-n3 AS6 and the catalogue:

- a value reached through a function's result;
- a copy of the field in a variable;
- a list or map held in a variable other than `state.Launches`;
- a part of a value (`strings.HasPrefix(l.Launch, "pla")`);
- a value inside a sentence (`"launch is not claude or plain"`).

The separators exclude a space on purpose: messages such as session.pid's check text would
otherwise read as lists. They exclude `/` too, so that a path ending in `claude` is not read as
a list.

### Red in analysistest (before the widening, at `d5ac63929`)

The new fixture is `testdata/src/fix/internal/teamchat/member/launchuse/widened.go`, and
`fix/cmd/teamchat` and `fix/cmd/teamchat-admin` are added to `TestAgentBranch`. Run as
`go -C src-go/qa test ./rules -run TestAgentBranch -count=1`; the log is
`untracked/scratch/n3e-widen-red-analysistest.log`.

```
--- FAIL: TestAgentBranch (0.58s)
    widened.go:16: no diagnostic was reported matching `the launch field given to a call with "plain" ...`      strings.EqualFold(l.Launch, "plain")
    widened.go:20: no diagnostic ... `the launch field given to a call with "plain,x" ...`                        strings.Contains("plain,x", p.Launch)
    widened.go:24: no diagnostic ... `the launch field given to a call with "claude" ...`                         strings.HasSuffix(l.Launch, "claude")
    widened.go:28: no diagnostic ... `a string of an agent.Agent given to a call with "plain" ...`                strings.HasPrefix(string(a), plainName)
    widened.go:32: no diagnostic ... `the launch field used as an index into a literal holding a launch value ...` map[string]bool{"plain": true}[l.Launch]
    widened.go:37: no diagnostic ... `a call over the launch field compared with "plain" ...`                     switch strings.ToLower(l.Launch) { case "plain": }
    widened.go:44: no diagnostic ... `a call over the launch field compared with "claude" ...`                    strings.TrimSpace(p.Launch) == "claude"
    widened.go:48: no diagnostic ... `a call over the launch field given to a call with "plain" ...`              strings.EqualFold(strings.TrimSpace(l.Launch), plainName)
    fix/cmd/teamchat/main.go:12: no diagnostic ... `the launch field compared with "plain" ...`
    fix/cmd/teamchat-admin/main.go:12: no diagnostic ... `the launch field compared with "claude" ...`
FAIL
```

(The right-hand column is the source line, added here; analysistest prints each miss twice,
once per package variant.) The red commit's `qa-changed` failed on this test alone, in go-qa's
test runner; every other stage passed (`untracked/scratch/n3e-widen-qa-red.log`).

### Green in analysistest (at `c08aaf231`)

`ok github.com/LongTermSupport/fedora-desktop/src-go/qa/rules`
(`untracked/scratch/n3e-widen-green-analysistest.log`). analysistest also fails on a diagnostic
no `want` names, so the green run also shows that these are not flagged:

- the two declared gaps at the end of `widened.go`;
- `good.go` (`l.Kind == "claude"`, `slices.Contains([]string{"claude", "plain"}, l.Kind)`,
  `filepath.Base(arg) == "claude"`, `agent.For(agent.Agent(l.Launch))`);
- the cli stand-in's `checkClaudeCmd` (`filepath.Base(a) == "claude"`).

`qa-changed` passed for the widening commit (`untracked/scratch/n3e-widen-qa-green.log`:
`✓ go: go-qa, both modules`, golangci included). An early draft of `checkAgentBranch` went over
cyclop's limit. The comparison branch moved into `checkLaunchBinary`, with no suppression.

## m3: the rule over the real tree, red and green

Driver: `go -C src-go/qa build -o untracked/scratch/n3e-widen-driver ./driver`, built from the
widened rule (`c08aaf231`'s `qa/`). Each tree is `git archive <rev> src-go` extracted under
`untracked/scratch/n3e-widen-rev/`, and each run is
`go -C <tree>/src-go vet -vettool=<driver> [-tags busfailpoints] <packages>`. One run at a time.

**Red, `8d2496eba` (the tree before the move), `./internal/teamchat/...`.** The untagged run
(`n3e-widen-tree-before-untagged.log`) and the `busfailpoints` run
(`n3e-widen-tree-before-busfailpoints.log`) are identical, exit 1, twelve findings and nothing
else:

```
internal/teamchat/member/keeper/loop.go:288:18: state.LaunchPlain named outside package agent (project_agent_branch): ask the run's agent.Adapter, or add the agent's adapter there
internal/teamchat/member/keeper/loop.go:289:30: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/loop.go:290:18: state.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/loop.go:482:42: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/loop.go:539:30: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/prepare.go:439:18: state.LaunchPlain named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/prepare.go:440:30: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/prepare.go:441:18: state.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/prepare.go:458:30: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/keeper/seats.go:122:34: cli.LaunchClaude named outside package agent (project_agent_branch): ...
internal/teamchat/member/link/link.go:268:11: the launch field compared with "plain" outside package agent (project_agent_branch): ...
internal/teamchat/member/link/link.go:423:34: the launch field compared with "plain" outside package agent (project_agent_branch): ...
exit 1
```

These are the same twelve sites that the narrow rule found and that the conformance review
reproduced. Widening the rule added no finding before the move.

**Green, HEAD.** The tree is `d5ac63929`, whose `src-go` outside `qa/` is that of `c7fb2321e`
and of `c08aaf231`. It was run over `./internal/teamchat/... ./cmd/teamchat/... ./cmd/teamchat-admin/...`. The untagged run (`n3e-widen-tree-head-untagged.log`) and the
`busfailpoints` run (`n3e-widen-tree-head-busfailpoints.log`) both exit 0 with no output. That
includes no finding on `cli/launch.go`'s `filepath.Base(a) == "claude"`, CL2's program-name
match: `a` is an element of CMD and no launch operand. It also includes no finding on
`state/sessionpid.go:57`'s message `"launch is not claude or plain"`, which is in an allowance
row and is a sentence, not a list.

## m4: how the twelve sites were changed

Each of the 12 sites was moved by hand, one by one, in `e248c1fe6`. None of them received the
change by pattern or by a mechanical rewrite. That commit touches `keeper/prepare.go`,
`keeper/loop.go`, `keeper/seats.go` and `link/link.go` (the twelve sites), together with
`agent/`, `cli/launch.go`, `state/sessionpid.go` and `wire/control.go`.

## m2: scope

The rule now judges `internal/teamchat`, `cmd/teamchat` and `cmd/teamchat-admin` and below
(`agentBranchTrees`). The two commands hold no launch value today, so they are green, as the
HEAD run above shows. The fixtures `fix/cmd/teamchat` and `fix/cmd/teamchat-admin` show that a
comparison there is now flagged. The catalogue says so.

## m1: the next wider rule, not built

This is named in the catalogue's `project_agent_branch` section. There are two candidates:

- A dataflow rule that follows a copy of the field (`x := l.Launch` and then `x == "plain"`, or
  a helper handed the field). It is not built: it would be an SSA pass over every teamchat
  function, for a shape the independent search found nowhere, and the seam leaves only plain
  copies of the value.
- A rule over every string-field-against-literal comparison in teamchat. It is not built: it
  would report every `Kind`, `Inbound` and `Desired` check, and the allowance table would stop
  saying where the agent is branched on.

## M1: the independent search reconciled with the rule's twelve findings

The search (`261023-n3e-seam-search.md`) ran over the tree after the move. It used two
techniques, text search and reading. It found no unallowed site in non-test code. The rule
agrees: green at HEAD, untagged and with `busfailpoints`, and now also over the two commands.

**What the search found that it was unsure of, and how each reconciles:**

- `cli/launch.go:212-224` (`filepath.Base(a) == "claude"` and the `Launch.Claude` index) is
  CL2's program-name match. It reads an argument of CMD, not the launch value, so it is not this
  class. SPEC-n3 AS6 says so, and the rule gives it no finding: the narrow rule never did, and
  the widened rule's green run above shows it still does not. The cli fixture keeps the same
  shape clean.
- `host/lxc/unit/unit.go:68` passes `"claude"` as command text. It has no launch operand, so it
  is not a finding.
- `keeper/harness_test.go:287` (`w.launch.Mode == cli.LaunchClaude`) is a real comparison, but
  it is in a test file. The rule excludes test files by design (AS6: "the non-test files"). The
  fake link that the harness drives must answer as the agent would, and a test asserting
  agent-specific behaviour is not a second place the product branches. The other test-file
  sites it lists are assertions or table values, excluded likewise.

**The search against the twelve red findings.** The search ran after the move, so it could not
see the twelve sites. What it confirms is the green side: the only remaining launch-value
references in non-test code are the six allowance rows, the owner, plain copies
(`string(Mode)` into session.pid and the frame), and dispatch through `agent.For`. The rule
reports exactly none of these. The red side rests on the rule's run at `8d2496eba`. Those twelve
findings are every site of SPEC-n3 AS1a (`link.go` 272 and 298 use the `plain` local from the
one comparison at 268).

**The search was not fully blind.** Its first text search, over all of `src-go`, printed lines
from the rule's fixtures under `src-go/qa/rules/testdata/src/fix/...`, so it saw the flagged
shapes and the rule id. It did not open `agentbranch.go` or its testdata, and it excluded
`src-go/qa/rules` from its findings. Its reading of the product code is independent of the
rule. Its list of shapes to look for (ToLower, HasPrefix, Contains, switch and map forms) may
have been primed by the fixture lines it saw. It found none of those shapes in the tree.

**What each technique caught that the other could not.** Text search found every literal and
constant, so it covers every real comparison. Reading showed four things. The `Plain` boolean
is derived from `caps.SessionStart`. The `string(Mode)` writes are copies. `adapter.Capabilities`
is the sanctioned path. No `local := launch; local == ...` copy exists, and that last one is
the rule's own declared gap, so it is the search's to answer and not the rule's.

## Not done

- `qa-all.bash` was not run (lead's rule). The tagged `tuwunel` build was not run over the
  real tree by me. The conformance review ran it for the narrow rule.
- No push.
