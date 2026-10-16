# Fable review: dbf/hidden-bindings (project_hidden_bindings)

Branch `dbf/hidden-bindings` at `58b40f74`, reviewed against local F44 at `1d3c65bb` (the
coordinator two-paths merge). Nothing in the branch was changed except this report. Every run
below was made with the PINGBUS variables unset, through the project entry points, in this
worktree or in detached scratch worktrees under `untracked/scratch/fable-hb/` (removed after).

## Verdict: FIX-FIRST

The defence is sound and the fixes are right; what stops the merge is bookkeeping that would land
wrong: the CCY version number the branch carries is one F44 has already used, and its
`meta-deploy.list` would replace the F44 queue. Both are branch commits, not merge-time tidying.

## What was run

| Run                                | Where                            | Result                                                                                    |
| ---------------------------------- | -------------------------------- | ----------------------------------------------------------------------------------------- |
| `scripts/qa-go.bash`               | branch                           | `GO-OK 59 package(s), ... qa module 57 package(s) at 94.5%; project rules clean`           |
| `scripts/qa-hidden-bindings.bash`  | branch                           | `HIDDEN-BINDINGS-OK 438 shell file(s), 333 Python file(s)`                                |
| `test-ccy-teams.bash`              | branch                           | passed 188, failed 0                                                                      |
| `test-ccy-sessions-take-over.bash` | branch                           | passed 82, failed 0                                                                       |
| `test-ccy-teams-command.bash`      | branch                           | passed 369, failed 0                                                                      |
| `test-ccy-lifecycle.bash`          | branch                           | passed 215, failed 0                                                                      |
| Red, `qa-hidden-bindings.bash`     | scratch checkout of `9e40ea6e`   | reproduced: exit 1, the 10 findings exactly (history-search:69, tmux-session:397, wsi x8) |
| Rule over merged tree              | scratch merge of branch into F44 | `go vet -vettool` clean over `src-go` and `src-go/qa`; `HIDDEN-BINDINGS-OK 443 / 338`     |
| `go test` picker, ui, sessions     | scratch merge                    | all pass after the resolution in section 3                                                |

`qa-all` was not run, as instructed.

## 1. Does the rule catch the class?

Evasion probes were planted in a scratch checkout of the branch tip (a package `internal/probehb`,
never committed) and in `untracked/scratch/fable-hb/probe/`, then run through the driver and the
gate.

Go, flagged (9 of 13 shapes): a Ctrl-J bound to accept; an expect flag assembled with
strings.Join of the flag name and the key with an equals separator; an expect flag held in a
struct field and in a map value; toggle+accept on Ctrl-A; execute-silent; and the display-only
shapes in the next section. Not flagged: a Sprintf whose format is the flag with its name as the
argument, a strings.Replace of a misspelt flag, a byte-slice conversion, a `ui.Action` assembled
by field assignment (ui refuses it at run time: an empty Label is `ErrBadAction`), and a
`ui.Action` whose Label is one space. All of these are contrived; none is a shape a person writes
by accident, so none matters now.

Shell: expect and bind flags in call words and array elements are caught, including an array
appended with a bind to execute, and a printf -v that formats the flag (judged because every
command word is). Escapes: a scalar variable set to the two flags and spread unquoted into the
fzf call, because `ShellFindings` judges only array assignments (`shell.go`, the `syntax.Assign`
arm); an inline FZF_DEFAULT_OPTS assignment before fzf; a bash -c string that runs fzf; and a
file that calls an fzf wrapper defined elsewhere with the expect flag (the file-never-runs-fzf
narrowing). The tracked tree has none of these today: the one scalar match is an awk pattern in
a test suite, and `ccy_tmux_pick` has no caller outside `tmux-session.bash`.

Python (Textual): show=False does not hide a binding from the rule (good); a Button on another
screen does not pair (good, the fixture holds it); a non-literal `.bind` key is a finding (good).
Escapes, and not recorded in "Not caught": a `key_NAME(self)` method, which Textual dispatches
for that key with no `BINDINGS` entry at all, and an `on_key` handler that switches on
`event.key`. Both are first-class Textual key routes.

Bubbletea (Plan 00204): zero coverage, by construction: the analyzer reads fzf argv and
`ui.Action` literals only. The catalogue sentence that excuses this ("no first-party Go reads
raw keys; there is no terminal library in src-go") is true today and false the day 00204 lands,
and the Go gate will keep printing "project rules clean". See N4.

gum: `ui` owns gum; `gum choose` has no key bindings beyond navigation and `gum confirm`
shows its two answers as buttons. Nothing to catch.

`read -n1` menus: not caught, recorded, and the fixture keeps an unmarked line. Fine.

## 2. False positives on navigation

Arrows, Enter, Esc, Tab, q to abort, Ctrl-R to toggle-sort, Tab to toggle+down, and every fzf
event (start, focus, load) are clean in both halves, as the fixture proves.

The navigation set is narrower than the recorded calibration. The catalogue says display-only
changes are navigation, but these were flagged in both halves: change-preview-window,
toggle-track, preview. change-prompt, change-header, change-preview, toggle-search, track and
untrack would be too. A key other than Enter bound to accept is also flagged, though without the
expect flag fzf cannot tell the caller which key accepted, so it is the same as Enter. No tracked
file has any of these, so nothing fails today (N1).

In Textual, Escape bound to dismiss (leaving a screen) is flagged while the fzf abort is
navigation: the two halves disagree about leaving (N3).

## 3. Merge with F44

`git merge-tree F44 HEAD` conflicts in 11 files: `menu.go`, `sessions.go`, `sessions_test.go`,
`help.txt`, `qa/all/addedgates_test.go`, `scripts/test-ccy-lifecycle.bash`, `claude-yolo`,
`meta-deploy.list`, `docs/ccy-changelog.md`, `ACCEPTANCE.md`, the day journal. `keys.go` and
`offers.go` auto-merge but then do not compile against the resolved `menu.go` (below). The
resolution was carried out in a scratch worktree; with it, every picker, ui and sessions test
passes, the rule is clean over both modules and the gate is clean (443 shell, 338 Python).

### menu.go

- Imports: keep `coordinator` and `seats` from F44 beside the dbf `login` and `ui`; drop the F44
  `flow` and `term` (dbf removed `menu()`, their only user).
- The act-constant block gains `actAskDescription = "ask-description"` and
  `actAskLimit = "ask-limit"`. The F44 pseudo-keys (`keyAttach`, `keyCoordinator`, `keyLxc`,
  `keyElementAll`, `keyShutdown`, `keyAskDescription`, `keyAskLimit`) go: in dbf an ID is `id`,
  and a key is a real fzf key or empty.
- Drop the F44 `shown()` and `line()`; keep the dbf `ui()`. `footerPart` is the dbf one.
- `kindActions`, `KindTeam`: the dbf four-field rows with the F44 shape
  `append(append([]action{...}, r.askAgainActions(team)...), help)`.
- `askAgainActions`: the two rows become `action{actAskDescription, "", askDescriptionLabel(team), short}`
  and `action{actAskLimit, "", askLimitLabel(team), short}`: no key, as the earlier note said.
- `seatActions`: the F44 body (role before remove, takeover after it, the F44 wording
  "Remove seat MEMBER from TEAM") with the dbf four-field literals
  (`{actRemove, keyRemove, ...}`).
- `coordinatorAction` keeps the F44 `(action, bool)` signature and `coordinator.Holding`,
  `TakeoverChoice` and `NoneChoice`, each literal as `action{actCoordinator, "", label, short}`.

### keys.go and offers.go

- `keys.go`: `case actAskDescription, actAskLimit: return r.askAgainOf(ctx, id, row.Team)`.
- `offers.go` `askAgainOf`: `if key == actAskLimit`. `offers_test.go` loops over
  `actAskDescription, actAskLimit`.

### sessions.go

- Take the F44 imports (`coordinator`, `seats`), `roleTakeover`, `option.handle`, the
  `coordinator.Holding` and `current` in `addToTeam`, the `handle:` in `askSeat`, and the new
  `roleOptions(team, current string, holder coordinator.Holder)`; all auto-merge.
- `roleInTeam` keeps the dbf signature `(ctx, s, cell hot.Cell)` and title, with the F44 body:
  `holder := coordinator.Holding(r.snap.Members[cell.Team], r.seatHandle(s.Checkout, cell),
  seats.SnapshotRunning(r.snap))` and `roleOptions(cell.Team, cell.Role, holder)`; the
  `roleTakeover` branch that follows auto-merges.

### sessions_test.go

The new F44 tests script keys that dbf removed. A key fzf is not told about makes the fake return
it, ui answers `NotOfferedError`, and the test fails. Rewrite each to the menu route, and move
every `Rows[n]` index up by one (the menu is one more screen):

- `TestAddOfAChosenSeatAsCoordinatorDemotesTheCurrentOne`: `{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat dev", "|Replace the coordinator", "|ABORT", "|ABORT"}`, `wantTakeoverRows(t, h.Script.Rows[5], ...)`.
- `TestAddAsCoordinatorOfATeamWithNoneIsOfferedPlainly` (auto-merged, still keyed): same route with `"|alpha has none"`, rows at `Rows[5]`.
- `TestChangeRoleInATeamRunsSessionRole`: `{toSessions, "|ccy-proj", "|Change role in alpha", "|Replace the coordinator", "|ABORT", "|ABORT"}`; both the `worker (now)` check and `wantTakeoverRows` read `Rows[3]`.
- `TestRoleOnATeamWithNoCoordinatorIsOfferedPlainly` (auto-merged, keyed): `{toSessions, "|ccy-proj", "|Change role in alpha", "|alpha has none", ...}`, rows at `Rows[3]`.
- `TestAddOfASeatWhoseHandleIsTheCoordinatorSaysItIsOneNow` (Fable N1, auto-merged, keyed): `{toSessions, "|ccy-proj", "|Add to a team", "|alpha", "|seat 2", "|coordinator (now)", ...}`, rows at `Rows[5]`.
- Keep the dbf `TestMoveOfTheCoordinatorAsksWhereTheRoleGoes` beside them.
- `"tab|alpha"` scripts stay valid: Tab is still the shortcut of the Running sessions row.

### help.txt

Take the dbf Running sessions paragraph (the menu lines) and add the F44 sentence to its
"Change role in TEAM" line: on a team with none the choice is `Make it the coordinator`; on a
team that has one it is `Replace the coordinator`, which asks again and starts on no. The
coordinator paragraph F44 added below auto-merges.

### addedgates_test.go, test-ccy-lifecycle.bash

Keep both `addedAfterPort` rows (`ccy-container-name` and `hidden-bindings`). Take the broader F44
unset of every PINGBUS_, AGENT_BUS_ and CCY_LIFECYCLE_ variable and CCY_SESSION_NAME, which
covers the dbf unset of PINGBUS_MEMBERSHIP.

### Do the new F44 rows satisfy the rule?

Yes. "Ask me to describe TEAM again", "Ask me about the limit of TEAM again" and the takeover
line are menu lines with no key; after the resolution they are `ui.Action` values with an empty
Key, which `checkActions` accepts and the analyzer never sees as a binding. The rule ran clean
over the merged tree.

## 4. Versions and the queue (blockers)

B1. The CCY number collides with one F44 has used. `files/var/local/claude-yolo/claude-yolo:17`
sets CCY_VERSION to 3.99.10.2 with the comment "Container 2.75.1", and `docs/ccy-changelog.md:20`
adds the heading "3.99.10.2 — container 2.75.1". F44 already has a "3.99.10.2 — container 2.75.1"
heading (the container-name race, merge `ece5861c`) and is at 3.99.12.4 / 2.77.3; a merge would
leave two 3.99.10.2 headings and a launcher below the F44 number. The branch changes the launcher
library `lib/tmux-session.bash` only, nothing in the image, so the container number does not
move. With the LXC branch taking 3.99.12.5 / 2.77.4, this branch needs CCY 3.99.12.6, container
2.77.4 unchanged: line 17 (version and comment), the changelog heading placed above 3.99.12.4 (its
body says "container 2.77.4", unchanged), and the number in the `meta-deploy.list` description.
Renumber on the branch before the merge, as `1f8de214` did for the coordinator branch.

B2. `meta-deploy.list` must keep every F44 entry. The branch list is the emptied base plus
three entries, so the dbf side of the conflict has none of the five F44 entries. Resolve by
keeping the F44 list whole and: fold the hidden-bindings sentences into the existing F44
`play-claude-yolo.yml` description (one run of the play, with the new number), and append the two
new entries. Both are justified: `play-speech-to-text.yml` deploys `wsi-model-manager` (line 296)
and carries the pin of textual to 0.85.0 or later; `play-basic-configs.yml` deploys
`history-search.bash` (line 286). The "on ccy 3.99.12.3" wording of the `00203` acceptance entry
is for F44 to update when it renumbers.

## 5. Install-specific names

None found. The added lines of the diff were grepped for user names, home paths, host and
container names, `.local` hosts and checkout paths; the only hits are the repository file paths.
The test fixtures use `proj`, `alpha`, `beta`, `ccy-proj`.

## 6. Reds and green through the project entry points

- Red reproduced: `./scripts/qa-hidden-bindings.bash` at `9e40ea6e` exits 1 with the 10 findings
  the report lists, line for line.
- Green reproduced: both gates and all four suites on the branch tip (table above), and the
  analyzer plus gate on the merged tree.
- The Go red at `8022a152` was reproduced by the conformance reviewer from a fresh checkout and
  the rule was shown live on the branch tip by my planted package (section 1); I did not rerun
  `qa-go.bash` at `8022a152`.

## Non-blockers

- N1 Navigation set. Add the display-only actions (change-preview-window, change-preview,
  preview, toggle-track, track, untrack, toggle-search, change-prompt, change-header,
  change-border-label) to the `fzfbind` set, or write the sentence that says why they carry the
  hazard when toggle-preview does not; decide a non-Enter key bound to accept the same way.
  `src-go/qa/internal/fzfbind/fzfbind.go`, the `navigation` set.
- N2 Shell scalar. Judge the value of a scalar assignment too (split on whitespace), or record
  the scalar-spread shape under "Not caught" with the FZF_DEFAULT_OPTS sentence.
  `src-go/qa/hiddenbindings/shell.go`, the `syntax.Assign` arm.
- N3 Textual routes and leaving. Record (or read) `key_NAME` methods and `on_key` handlers; say
  that a Button paired by `id=` and `on_button_pressed` does not count (so older idiomatic code is
  a finding until it moves to `action=`); and decide whether dismiss and pop_screen on Esc are
  leaving, as abort is in fzf. `helpers/qa_environment/textual_bindings.py`,
  `NAVIGATION_PREFIXES`; catalogue "Not caught".
- N4 Plan 00204. Before the first Bubbletea key handler lands, widen the rule: every
  `key.Binding` has help text and is rendered by one help or menu component, and a `tea.KeyMsg`
  switch outside that component is a finding. Put it in the 00204 PLAN as a prerequisite; the
  catalogue sentence about no terminal library in `src-go` must change in the same commit.
- N5 Keys act from the list. A menu key (Ctrl-T, Ctrl-X in ccy-sessions; Ctrl-E, Ctrl-R in
  ccy-teams) acts on the row without the menu having been opened. The owner rule holds, since
  the menu line is the control surface and the key is printed beside it, and the ccy-teams footer
  names them per row; the ccy-sessions header (`internal/sessions/picker.go:125`) does not. A
  footer there would make the shortcut discoverable before Enter.
- N6 Report wording. The DBF report says "CCY was bumped to 3.99.10.2"; after B1 the report and
  the journal should name the number that ships, so the record matches the commits (spec section 7).
