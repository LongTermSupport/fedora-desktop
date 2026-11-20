## QA Review: Plan 00257 Q52 D1-D3 (1177f2ea8, 5c41c33ce, 8e60b9232, 262793bdb)

**Verdict**: FIX-BEFORE-MERGE. Nothing blocks. The 300 s bound reaching panes that show no prompt is not a blocker, but it should be split or disclosed before landing.

### Blocking
None.

### Should fix

1. **D2: the 300 s bound now also covers panes that are not in a turn, and the code can tell the two apart.** The question is whether this degrades things.
   - **The classifier separates the cases.** `screen.ClaudeState` (`src-go/internal/screen/screen.go:178-188`) returns `Busy` only when "esc to interrupt" is in the status lines above a framed box. A pane with no framed box is `NotDrawn` (line 181).
   - **sessclose treats both the same.** `ready()` (`src-go/internal/sessclose/sessclose.go:614`) is `case screen.Busy, screen.NotDrawn:`, and both wait against one deadline.
   - **Where that deadline comes from.** It is set at `:356` (`c.Now().Add(c.Waits.Prompt)`), checked at `:525`, and expires in `timedOut` `:762-768`.
   - **Who now waits 300 s instead of 30 s:** a pane that never draws a box (starting, a dialog, an unrecognised or hung screen). This applies to `ccy-teams shutdown`, `ccy --stop` and ccy-sessions' Close.
   - **Launcher questions are unaffected.** They are handled earlier by `notClaude` (`:537-560`) and stopped at once.
   - **The post-compaction wait for `/exit` also became 300 s** (`:743`). That is harmless.
   - **The LXC note is now wrong for `NotDrawn` panes.** It says "busy past the bound" (`:765`), and TC12 explains that as "a turn still running", which is false for a pane with no box.
   - **Recommendation:** add `Waits.Turn: 300 s` and restore `Waits.Prompt` to 30 s. In `ready()` (`:614`), when the state is `Busy`, extend `j.deadline` once to the job's start plus `Turn`. A pane that is never seen busy then keeps the old 30 s. Keep `:743` on `Prompt`. Make `timedOut` (`:762-768`) word the note from the last state: "still in a turn after 300 s" versus "no prompt within 30 s".
     - Tests: a `NotDrawn` pane is stopped at 30 s, and a `Busy` pane is waited for up to 300 s.
     - Restore the 30 s wording in `TestAPromptThatNeverDrawsIsStopped` (`src-go/internal/teams/shutdown/stop_test.go:184`).
   - **Is it a merge blocker? No.** Every path is still bounded and still fails or stops loudly. The agent's report says so openly. The cost of a hung or blank pane is 270 s of extra waiting. A team shutdown watches all its sessions together, so that cost is paid once, not per session.
   - **Caveat:** `NotDrawn` also covers Claude's own dialogs in the middle of a turn. If the owner counts those as "a turn", the single bound is defensible. In that case, keep the code and fix items 2 and 3 instead.

2. **The progress and confirmation lines users see never mention the new wait of up to 300 s.** Only the CLI help and the picker help were updated.
   - `src-go/internal/ccylaunch/stop.go:55-57` (`ccy --stop`) says only that a compaction is waited for, 600 s at most.
   - `src-go/internal/sessions/picker.go:333` and `:346-348` (ccy-sessions Close and its question) say the same. A person at the picker can sit for 5 silent minutes on a busy session, including with `--no-compact`, which used to interrupt the turn at once.
   - `src-go/internal/teams/shutdown/shutdown.go:377-389`, the preview shown before "Close N sessions?", describes the compaction and exit bounds but not the busy wait.
   - Fix: add "a session in a turn is waited for, up to 300 s, never interrupted" to all three.

3. **The CCY 3.122.45 changelog does not state the full reach of the change** (`docs/ccy-changelog.md:21-29`).
   - It describes busy boxes only.
   - It omits that a pane that never draws a prompt (starting, a dialog) now waits 300 s, not 30 s, before it is stopped or failed.
   - It omits that `--no-compact` closes also wait out a turn now.
   - The agent's report states these; the changelog must too, or the item 1 split must make them untrue.

### Nits
- `src-go/internal/teams/picker/help.txt` (Compact paragraph) says "then /compact" unconditionally. A host session below the compact floor is not compacted; `docs/agent-bus.md:163` says so correctly.
- `src-go/internal/teams/picker/v2_test.go:26`: the comment still says "its Shut down among them".
- `docs/agent-bus.md:173`: the bold lead "Shut down a team's sessions" keeps the old wording (the CLI verb is still `shutdown`, so this is optional).
- `SPEC-team-close.md:14-19`: TC1 has a ragged rewrap ("(compact, then\n exit)" and `beside "Close" and`).

### Checked and clean
- **No Escape anywhere.** `SendEscape` is gone. The only remaining `send-keys` calls in `tmuxccy` are `-l` text and `Enter` (`panes.go:70`, `:78`). No `"Escape"`, `C-[` or `\x1b` key send remains in sessclose, winddown or shutdown production code; the only hits are test fakes counting presses. The fakes for both host and guest route through `press()`, so the `escapes == 0` assertions are meaningful.
- **Unsent text is never typed over.** `Busy` takes precedence over `Typed` (`screen.go:183`). A box holding text reads `Typed` once the turn ends, and `ready()` then fails it untouched (`sessclose.go:612-613`).
- **D1 is complete.** `git grep` finds the old row names only in historical reports, a completed task's description and the changelog's older entries. The crumbs (`<team> › Close` / `› Compact`), questions, footers, `usage.txt`, the picker help, `docs/agent-bus.md` and the bash suite's pick all match.
- **The Compact row's help is clear about what it sends.** The help paragraph and the confirmation preview (`winddown.go:452-459`) both say `/goal clear` and the stop-and-push instruction come first. The row name alone does not, but the owner chose it.
- **The specs match.**
  - Plan 00201: SD10's busy bullet, SD12's 300 s wording and acceptance step 12.
  - SPEC-team-close: TC1, TC6, TC8, TC9, TC12 and D1-D3 are updated.
  - Plan 00201's PLAN.md line 175 ("Escape first when busy") is a ticked task's history, not the spec.
- **Versions.** `CCY_VERSION` 3.122.44 then 3.122.45, with updated comments. The container version is correctly unchanged: `image-source-paths` names none of the changed packages.
- **Deploy.** `play-claude-yolo.yml` is already queued in `CLAUDE/Plan/meta-deploy.list`.

### Mechanical gates
- **Go tests:** `go test -race` over `./internal/sessclose/... ./internal/teams/... ./internal/tmuxccy/... ./internal/sessions/... ./internal/ccylaunch/...` exited 0, with 38 packages ok.
- **Bash suite:** `bash scripts/test-ccy-teams-command.bash` reported `passed: 397  failed: 0` and exit 0. `qa-changed` runs it through `scripts/go-binary-suites.tsv:15`.
- **plan-qa --sweep:** 2 block findings, both in other plans (00244 status token, 00238 header/body). None in 00257, 00201 or 00203.
- **qa-all.bash:** not run, because a sub-agent may not run the full suite (R-SUBAGENT-FULL-QA). The coordinator owns it.
- **syntax-check:** not applicable, since no playbook changed.