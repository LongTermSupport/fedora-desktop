# QA Review: Q52 review fixes (03bedb9da, 0696f515d, 01597b4fe, dd4f72964)

**Verdict**: FIX-BEFORE-MERGE. The wait logic is correct. Two things must change before merge: the user-facing sentence contradicts what the host does, and the user doc did not move with the change. There is also one test gap.

Scope: I reviewed `git diff 43a7bd72b dd4f72964`. The range named in the brief, `262793bdb..dd4f72964`, also contains the merge 43a7bd72b (hooks daemon v3.75.0, about 40 files). That merge is not part of this change, so I left it out.

## Blocking

None.

## Should fix

### 1. `TurnWait` tells the user "it is never interrupted", but a host session still in its turn at 300 s is killed

Where:
- `src-go/internal/sessclose/sessclose.go:278-281` (TurnWait)
- `sessclose.go:786-792` (timedOut, then hardStop)

The sentence is "A session in the middle of a turn is given up to 300 s to finish it; it is never interrupted." It appears in three places:
- the ccy-sessions Close question and its progress line (`src-go/internal/sessions/picker.go:331-351`);
- `ccy --stop` (`src-go/internal/ccylaunch/stop.go:59`);
- the shutdown preview (`src-go/internal/teams/shutdown/shutdown.go:384`).

What the code does on the host: when the turn bound runs out, `hardStop` kills the tmux session and removes its record. The note is "a turn was still running after 300 s, so its tmux session was killed". Tests pin this:
- `sessclose_test.go` TestABusySessionPastTheBoundIsStoppedNeverInterrupted;
- `teams/shutdown/stop_test.go` TestABusySessionPastTheBoundIsStopped.

Neither prompt says the session will be killed:
- The shutdown preview says only "One that has not ended within 60 s of /exit is stopped".
- The Close question says nothing about stopping at all.

Failure scenario: a session is 2 minutes into a 10-minute turn. The user picks Close, reads "it is never interrupted", and confirms. About 5 minutes later the turn is cut off by `kill-session` (harder than the Escape that Q52 removed) and the session record is deleted.

This behaviour is what the spec says (SPEC-team-close D2 and C8 stop a host seat; D3 protects only LXC seats). The problem is the wording. The changelog does say "(then the session is stopped)", so the prompts and the changelog disagree.

Fix: make the sentence say what happens at the bound. For example: "...given up to 300 s to finish it, never interrupted before then; one still in its turn after that is stopped (its tmux session killed)". An LXC session would say "left running in its container" instead. The 30 s prompt bound is also not stated anywhere a user looks; add it to the same sentence.

### 2. The user docs for Close and `ccy --stop` were not updated

`docs/ccy.md:198-203` describes ccy-sessions' Close and `ccy --stop` and gives only the 600 s compaction bound. It does not mention:
- the 300 s turn wait;
- the 30 s prompt wait;
- the stop at either bound.

The changelog entry for 3.122.46 says this behaviour changed for both commands.

The same gap is in:
- the F23 package doc, `src-go/internal/ccylaunch/front.go:148-154`;
- the `--stop` help line, `src-go/internal/ccylaunch/help.txt:90`.

`docs/agent-bus.md` was updated for ccy-teams shutdown; `docs/ccy.md` was not.

### 3. No test has a pane that becomes busy after the wait starts, so the clock start and the reset after compaction are unproven

In every test that waits on a turn, the pane is busy at t=0:
- `sessclose_test.go:384, 402, 432, 451, 766, 779`;
- the fake's `look` at `sessclose_test.go:186-204` only ever moves Busy → Ready or Busy → afterTurn.

The tests therefore cannot tell these apart:
- **(a) Where the clock starts** (`sessclose.go:633`). With `waitFrom.Add(Turn)` replaced by `c.Now().Add(Turn)`, every test would give the same result, because Now == waitFrom on the first look. The owner's requirement that the bound is "counted from the start of the wait" is not held by any test. The case "busy first seen at 29 s" (deadline should become start + 300 s, not 329 s) is the untested edge.
- **(b) The reset after compaction** (`sessclose.go:766`). No test has a turn in the /exit phase after a compaction. If `j.inTurn = false` were dropped, this scenario would go wrong: a session busy before its compaction starts a new turn (a queued message) before /exit; no extension is granted; on the host it is killed 30 s after the compaction, with the note "a turn was still running after 300 s". Today's code is correct, but nothing pins it.

I worked these mutants out from the fixtures; I did not run them, because I am read-only here.

Fix: add two tests:
1. NotDrawn for 29 polls, then Busy for ever. Expect a stop at start + 300 s (within one poll) with the turn wording.
2. A turn, then the compaction, then Busy again before /exit. Expect /exit after that turn ends, and a 300 s bound counted from the compaction's end.

## Nits

- `sessclose.go:633`: the `by.After(j.deadline)` "never cut back" check can never matter while Turn > Prompt. Nothing checks that ordering (`TestLiveAndLimits` pins the values only), so the check is dead code in production.

## Checked and clean

- **Wait logic** (`sessclose.go:367-376, 540-547, 623-640, 757-809`):
  - Busy first seen at 29 s, or at exactly 30 s: `look` runs before the deadline check in `step`, so the turn extends the deadline before it can expire.
  - Busy then Ready: /exit is typed. Busy then NotDrawn: keeps 300 s, worded "no prompt ... within 300 s". Busy then Typed: fails untouched.
  - A pane that is never ready is stopped (host) or failed (LXC) at 30 s.
  - Every target shares one start time. After a compaction the wait starts again on the 30 s bound.
  - `notReady` only ever sees Busy or NotDrawn, because Ready types and Typed fails first.
- **No Escape**: no send of Escape in `sessclose.go` or `tmuxccy`. `SendKeys` is literal text (`-l`) and the only key otherwise sent is Enter. Each test checks `escapes == 0`.
- **LXC never killed**: the guest branches of `timedOut` (`sessclose.go:781-790`) only call `j.done(Failed, ...)`. The tests assert no `kill-session`.
- **Specs**: SPEC-team-close TC8, TC9, TC12 and D2, and SPEC-shutdown SD10 and SD12, state both bounds and the wording exactly as the code produces it. D3 covers LXC seats only, so stopping a host session matches the spec.
- **Changelog**: matches the code apart from finding 1. The cited rule `project_screen_state_compared` exists (`src-go/qa/rules/screenstate.go`, `CLAUDE/GoProjectRules.md`).
- **Version**: 3.122.45 → 3.122.46 (03bedb9da) → 3.122.47 (01597b4fe). Each has a changelog entry. The container LABEL is unchanged, which is correct: `files/var/local/claude-yolo/image-source-paths` covers none of the changed packages.
- **Help text change** (`teams/picker/help.txt`, the compact floor, LXC compacted whatever its size): matches `teams/winddown/compact.go:104-135`.
- **Public repo**: no home paths, hostnames, emails or IPs in the added lines; I grepped the whole diff.
- **Plan**: B.13 is ticked; the report section and the journal entry are committed. plan-qa reports nothing for 00257.

## Mechanical gates

- `go test -count=1` on sessclose, sessions, ccylaunch, teams/shutdown and teams/picker: all ok.
- `go vet ./internal/sessclose/`: ok. `gofmt -l` on the changed packages: no output.
- `hooks-daemon plan-qa --sweep`: exit 1, 31 findings. None are in 00257 or 00201. The two block findings are in unrelated plans (00244 status token, 00238 header).
- Full qa-all / qa-changed: not run. A sub-agent may not run the full suite (R-SUBAGENT-FULL-QA). The author's report says `qa-changed` exited 0 on 01597b4fe; I did not re-run it.
- Ansible syntax-check: does not apply, since no playbook changed.
- I broke the read-only rule once: the plan-qa output was redirected to `/workspace/untracked/scratch/q52-planqa.txt` (untracked, in the main checkout). It can be deleted.
