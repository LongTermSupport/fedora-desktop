# Defence Before Fix report: project_hidden_bindings

Branch `dbf/hidden-bindings` (from F44 at `741191c3`). The red commits are `8022a152` (Go
analyzer) and `9e40ea6e` (cross-language gate). The merge `64b8c020` brings in
fix/picker-visible-actions. The fix is `bc232d63`; `c384e235` makes a suite hermetic, and
`cf8419f0` fixes the catalogue's formatting. The conformance review's fixes are `8f32c584`. The
report and journal follow in their own commit.

## Specification followed

Defence Before Fix method specification 1.1.0. `refresh-spec.bash` printed `SPEC.md fetched 1.1.0 /root/.cache/defence-before-fix/spec/SPEC.md`, at the start and again before this report.
Sections 3, 4, 7 and 8 were read for this remediation, and section 3 again at each step.

The cache does not persist in this container. When the conformance review ran, it held nothing,
so the reviewer checked against the plugin's vendored copy, version 1.0.1. Its findings cite
1.0.1 clauses. Any clause that changed in 1.1.0 was checked only by the practitioner, not by
the reviewer.

## Toolchain

This project's Go toolchain is `go/analysis` analyzers in `src-go/qa/rules`. Each rule is
proven with `analysistest` fixtures under `src-go/qa/rules/testdata`. The driver
`src-go/qa/driver` is a unitchecker run as `go vet -vettool` by `scripts/qa-go.bash`, over
`src-go` and again over the nested `qa` module.

- **No manifest declaration found.** The project declares no go-qa-ci manifest and no
  known-gaps record in any spelling (`known-gaps`, `knownGaps`). The catalogue
  `CLAUDE/GoProjectRules.md` stands in for the tools register.
- **Listing:** the catalogue table, plus `go vet -vettool=<driver> help`.
- **Identifier resolution:** the analyzer `Name`, which every message carries. Each name
  resolves to the catalogue section with the same heading.
- **Single-rule harness:** `cd src-go/qa && go test ./rules -run TestHiddenBindings`.

Go cannot read shell or Python, so the rule has a second detector, built the way this project
builds gates. `src-go/qa/hiddenbindings` judges shell over the `mvdan.cc/sh` syntax tree.
`helpers/qa_environment/textual_bindings.py` judges Textual apps over the Python `ast`. Both
are run by `scripts/qa-hidden-bindings.bash`, a gate in `scripts/qa-gates.tsv` and in
qa-changed routing. Both halves read the same binding grammar and the same navigation set,
`src-go/qa/internal/fzfbind`.

The project had recorded no calibration. The calibration assumed is recorded in the catalogue:

- The sweep covers all first-party Go, shell and Python that git would publish, minus the
  trees excluded in `scripts/qa-trees.tsv`. The GNOME Shell extensions are read by hand.
- Display-only toggles, such as fzf `toggle-sort`, count as navigation.

## Defect and class

**Originating instance.** In the `ccy-teams` Running sessions view
(`src-go/internal/teams/picker/sessions.go`, keys at old lines 32-41, handled at 90-137),
Ctrl-A added a session to a team, Ctrl-X removed it and Ctrl-O changed its role. All three
went through fzf `--expect`, and the list itself offered no visible choice for any of them.
`ccy-sessions` had the same shape, with no menu at all: Ctrl-T took a session over, Ctrl-X
ended it and Ctrl-N started a new one.

**Class:** an action offered through a hidden key binding.

**Hazard, in the owner's words:** "TUI must be UX controls not magic keys - keys are oK but
MUST NOT BE THE ONLY CONTROL SURFACE". An action reachable only by a key cannot be found, and
it can be pressed by accident. A key is allowed only as an accelerator for an action that is
also a visible, selectable choice on the screen, printed beside that choice.

## Rule

- **Stable identifier:** `project_hidden_bindings`. The Go analyzer is
  `src-go/qa/rules/hiddenbindings.go`. The shell and Python gate reports under the same id
  (`hiddenbindings.Rule`).
- **Documentation:** the `project_hidden_bindings` section of `CLAUDE/GoProjectRules.md` (table
  row, section, Control paragraph), and rule 14 of `CLAUDE/InteractiveScripts.md`.
- **Go messages:**
  - "fzf --expect outside the ui Action builder: a key with no visible choice; offer it as a
    ui.Action"
  - "fzf --bind KEY:ACTION: a key bound to an action no visible choice offers; offer it as a
    ui.Action"
  - "fzf --bind with no binding beside it ...", "... built at run time ..." and "... binding
    after a run-time argument ...": a binding the rule cannot read.
  - "ui.Action with a Key and no Label ..."

### How wide it is drawn

**Go.** Every non-test package is judged, except `bindingSubjects`: the rules package and the
shell gate, which name the flags only as the text they search for. The rule flags:

- `--expect` anywhere except `ui.expectArg`, the one row in `expectBuilders`. Its reason: it
  takes its keys from `ui.Action` values, and ui lists each one with its Label.
- `--bind` binding a key to anything but navigation.
- Any binding the rule cannot read.
- A `ui.Action` with a Key and no Label.

The text is judged wherever it is spelt: a literal, a constant, a variable, split `--bind`
words, an `append` or `slices.Concat` element, a `Sprintf` format, or a folded concatenation.

The single builder works because of the ui API, `ui.Action{ID, Label, Key}` with per-row
`ui.Menu`. A key can reach fzf only as the Key of an Action that ui renders as a row or a menu
line.

**Shell.** In every tracked file that runs fzf, every `--expect` is flagged and `--bind` is
judged the same way as in Go. Shell has no `ui.Action`.

**Python.** In every tracked Textual app, a binding is flagged unless its action is navigation
or is the action of a `Button` on the same screen. That means a Button built in the `compose()`
of the class that binds the key, whose action runs there. The red and the review draft matched
Buttons per file. The conformance review showed that was narrower than the message, and
`8f32c584` scoped it to the screen. The Footer's binding hints do not count as the visible
choice: they are a hint line, not a control that takes focus and Enter.

### What it excludes as hazard-free

- **Navigation:**

  - moving the cursor or the page;
  - selecting rows (Tab's toggle);
  - editing the query;
  - display-only changes (sort, preview, wrap);
  - leaving (Esc, q, abort), always beside a visible Exit row;
  - Enter, Ctrl-M and a double click accepting the highlighted row;
  - fzf events (`start`, `focus`, `load`, ...), which are not keys;
  - Textual `focus*`, `scroll*` and `cursor*` actions.

  A Textual action that clears a filter from anywhere on the screen is not query editing. It
  changes what the list shows from outside the widget being typed in, so it is an action. fzf
  query editing always acts on the focused line.

- **Literals that read argv rather than build it:** comparisons, switch cases, and the
  searched-for arguments of `strings` and `slices` reader functions. The fake fzf in
  `teamtest/console.go` is the one real case. Of `Cut`, `CutPrefix` and `TrimPrefix`, only the
  separator or prefix counts as read; the first argument, and what a constant prefix leaves of
  it, is judged (`8f32c584`, after the review).

- `_test.go` files.

- Shell files that never run fzf.

- The fixture tree `tests/fixtures/hidden-bindings`.

### The next wider rule, considered and not built

"Every key a tool reads, by any means (`read -n1`, GNOME `Main.wm.addKeybinding`, a raw
terminal read), maps to an action that is also a visible control."

It was not built for two reasons. The key-to-control relation there is semantic: a printed
prompt naming the letters, or a panel menu item that does the same thing. It is not a shape in
the syntax tree. Also, the search found no instance in a terminal tool. Its only candidate is
the GNOME extension, referred below.

## Proof

**Fixture red, kept.** `cd src-go/qa && go test ./rules -run TestHiddenBindings` runs four
fixture packages.

- `testdata/src/fix/internal/badbind`:
  - `bad.go`: the originating Ctrl-A/X/O `--expect`; keys bound to `accept`, `execute`,
    `become` and `reload`; a chained `toggle+accept`; a split `--expect`.
  - `evade.go`: a constant, a variable, split `--bind` words, `slices.Concat`, an appended
    slice, `Sprintf`, a run-time spec, a folded `"--exp" + "ect=..."`, and a binding hidden
    after a run-time argument. Since `8f32c584` it also covers a flag handed back by
    `strings.Cut`, `CutPrefix` or `TrimPrefix`. Those lines were seen red before the analyzer
    was changed.
  - Navigation, events and comparisons are clean.
- `fix/internal/ui`: the builder is clean. A second `--expect` in ui is flagged, and so are
  Action literals with a key and no label.
- `fix/qa/rules` and `fix/qa/hiddenbindings`: the exclusions, which reach nothing else.

The shell and Python halves are proven against `tests/fixtures/hidden-bindings/fixture.bash`
and `fixture.py` on every gate run, by line marks. Each line that should be flagged is marked
`# finding`, and the gate fails unless it reports exactly those lines.

- `fixture.py` includes a binding whose only Button is on another screen.
- `fixture.bash` keeps unmarked lines of shapes the rule leaves out (`FZF_DEFAULT_OPTS`,
  `read -n1`), as the record of what is not caught.

**Planted controls.** Two were planted after the fix and then removed:

- `--bind=ctrl-a:accept` in `internal/ui`, reported by `scripts/qa-go.bash`;
- `--expect=ctrl-z` in `history-search.bash`, reported by `scripts/qa-hidden-bindings.bash`.

**Real-tree red.** Each red survives in its own commit:

- `8022a152`, through `./scripts/qa-go.bash`:
  `internal/ui/picker.go:105:23: fzf --expect outside the ui Action builder`. This was the one
  `--expect` every keyed picker went through.

- `9e40ea6e`, through `./scripts/qa-hidden-bindings.bash`, 10 findings:

  - `history-search.bash:69`: a binding built at run time;
  - `tmux-session.bash:397`: `--expect`;
  - `wsi-model-manager:179,180,181,240,241,242,243,245`.

  At this commit, the Go rule also fired 3 times on the new shell gate itself
  (`qa/hiddenbindings/shell.go:19,20,155`), where the flags are named as text to look for.
  `bc232d63` added `qa/hiddenbindings` to `bindingSubjects`, the same self-match exclusion the
  rules package has, with the fixture `fix/qa/hiddenbindings`. These were not instances, and
  they are not in the counts.

## Independent search

`defence-before-fix:independent-searcher` ran before the rule was written, from the class
description alone. It never opened `src-go/qa/rules`. Its report is kept at
`untracked/scratch/dbf-hidden-bindings-search.md`.

**What it looked for:**

- **By text:** `--bind`, `--expect`, `bind -x`, `bind-key`, `read -n`, `select`, `stty`,
  `termios`, `curses`, `prompt_toolkit`, `whiptail`, `dialog`, `x/term`, `bubbletea`,
  `MakeRaw`; fzf, gum and zenity users; `Keys:` and `PickSpec{` in Go; Textual `Binding(`,
  `BINDINGS` and `action_`; `addKeybinding` and `PopupMenuItem` in the extensions; tmux
  bindings.
- **By reading:** the ui picker; both ccy pickers in full; `tmux-session.bash`;
  `history-search.bash`; `tmux.conf`; `wsi-model-manager`; the speech-to-text extension menu.

**It found 9 instances, 3 of them confident:**

| Id  | Where                                            | Confidence                          |
| --- | ------------------------------------------------ | ----------------------------------- |
| A1  | ccy-sessions                                     | confident                           |
| A2  | `tmux-session.bash` Ctrl-N                       | confident                           |
| A3  | teams list                                       | unsure, mitigated by the Enter menu |
| A4  | Tab to Running sessions                          | unsure                              |
| A5  | Running sessions view (the originating instance) | unsure, mitigated by the Enter menu |
| A6  | `wsi-model-manager` d, r, F5, q                  | confident                           |
| A7  | its yes/no screen                                | unsure                              |
| A8  | GNOME speech-to-text global shortcuts            | unsure                              |
| A9  | history `toggle-sort`                            | unsure, low                         |

**Text search found what reading could not check:**

- A9;
- the Textual bindings (A6 and A7), which an fzf search cannot match;
- the GNOME `addKeybinding` calls (A8);
- the absence of whiptail, dialog, select, curses and x/term from first-party code;
- that `ccy_tmux_pick` takes keys from exactly one caller.

**Reading found what text search could not:**

- A1's keys sat in a variable joined at run time, so no literal appears outside tests.
- A3 and A5 already had menu lines on a second screen.
- Tab had no menu line.
- Enter on a model row was an unlabelled second route to download.
- The GNOME panel menu has no record item: a fact about what is missing.

**Saturation.** The search stopped when reading, the last technique, found no new instance.
Every one of A1 to A9 was first a text hit. Reading decided which hits were instances and how
mitigated each one was. It did not add new ones.

**Reconciliation:**

- **Go (A1, A3, A4, A5):** all went through the single `ui/picker.go:105` site the rule
  flagged. The owner's refinement made A3, A4 and A5 instances outright: the mitigating menu
  was a second screen, and Tab had no line at all.
- **A2:** flagged by the shell half.
- **A6 and A7:** the Go analyzer could not see Python. This instance is why the rule was widened
  into a cross-language gate before the red commit. The Python half reports all eight of their
  bindings.
- **A9:** calibrated as navigation, because it changes only the order of the same rows. The rule
  flagged `history-search.bash` for a different reason, a start-up binding it could not read.
  The search had judged that binding harmless, and it was made readable.
- **A8:** outside every detector; referred below.

No search instance remains that the detectors miss inside their scope.

## Sweep and count

**Scope:** all first-party Go (both modules), shell and Python that git would publish. Excluded:
the generated and vendored trees in `scripts/qa-trees.tsv`, `untracked/`, the hooks-daemon tree
and `CLAUDE/core`. JavaScript (GNOME Shell) was read by hand. No first-party JavaScript uses fzf
or Textual, so it has no machine sweep. This scope is recorded as the project's decision.

**Counts:**

| Detector                     | Findings | What they were                                    |
| ---------------------------- | -------- | ------------------------------------------------- |
| Go, `src-go` and `src-go/qa` | 1        | site of 3 PickSpecs in 2 packages: A1, A3, A4, A5 |
| shell                        | 2        | A2, and the unreadable history start binding      |
| Python                       | 8        | A6 and A7                                         |

That is 11 rule findings, covering 8 of the searcher's 9 instances, plus 1 binding the rule
could not read. A9 is calibrated as navigation, and A8 is referred.

**Final run:** 0 findings.

- `HIDDEN-BINDINGS-OK 438 shell file(s), 333 Python file(s)`
- `scripts/qa-go.bash`: project rules clean over 59 + 57 packages.

**Narrowings, each excluding only hazard-free code:**

- `bindingSubjects`: those packages build no fzf argv.
- Argv-reading literals: a comparison is never passed to fzf.
- `_test.go` files: never built into a tool.
- Shell files that never run fzf: `--expect` is also a flag of other programs.
- The fixture tree.

## Fixes

Every instance was examined and fixed individually. None was fixed by pattern.

- **ui:** `ui.Action`, `ui.Menu`, `PickSpec.Actions` and `PickSpec.Menus` were added, and
  `PickSpec.Keys` was removed. ui renders every action as a row, or as a line of the row's
  menu, with its key beside it. It accepts a key only where that line is offered; otherwise it
  returns `ui.NotOfferedError`, and the screen says so. Only `expectArg` builds `--expect`.
- **ccy-teams (A3, A4, A5):**
  - Each row's Enter menu is a `ui.Menu`.
  - "Running sessions: change a session's teams while it runs" is a listed choice, with Tab as
    its shortcut.
  - In the sessions view, every action, including Ctrl-A, Ctrl-X and Ctrl-O, is a menu line, and
    "What this list shows" (?) is a row.
- **ccy-sessions (A1):**
  - Enter on a session lists "Attach it here" (detached rows only), "Take it over here"
    (Ctrl-T) and "End it, and everything running in it" (Ctrl-X).
  - "Start a new ccy session in HERE" (Ctrl-N) is a row. Outside a project it says why it is
    unavailable.
- **A2:** `ccy_tmux_offer` lists a "Start a new session here" row, and `ccy_tmux_pick` takes no
  keys. It ships as CCY 3.99.12.9 (container 2.77.6, unchanged). The branch first used
  3.99.10.2, which F44 had already used; it was renumbered when F44 was merged.
- **History search:** the start-up binding is spelt in the call. Ctrl-R `toggle-sort` stays, as
  navigation.
- **wsi-model-manager (A6, A7):**
  - A button row: Download (d), Remove (r), Refresh (F5), Clear filter (Esc) and Quit (q), each
    with `action="app.*"`.
  - The remove question has Yes and No buttons.
  - The keys stay as shortcuts. `textual>=0.85.0` is pinned in `play-speech-to-text.yml`, for
    `Button(action=)`.
- **`ccy --teams`:** Tab multi-select is selection, and the screen says so: "Tab marks several,
  Enter takes them".

No instance was satisfied by suppression, by an allowance added for it, or by leaving the
hazard in place. The one allowance, `expectArg`, is the builder through which every visible
action passes.

**Incidental change, own commit `c384e235`:** `scripts/test-ccy-lifecycle.bash` inherited
`PINGBUS_MEMBERSHIP` when run from a team session. Three of its cases then failed, and they also
fail on a clean checkout of the previous commit. The suite now unsets it.

## Decisions referred to the owner

1. **GNOME speech-to-text global shortcuts (A8).** Toggle recording, toggle a Claude-processed
   recording (Ctrl+Insert), toggle the natural variant (Alt+Insert) and abort (Escape, while
   recording) are reachable only by desktop-wide keys. The panel menu has no item for them.
   - Fixed: 0 of 1. Remaining: 1.
   - What stopped the fixing: they are global desktop shortcuts, not a TUI screen. Whether the
     owner's rule covers panel UIs is the owner's call (section 4).
   - To fix: add panel menu items for start, stop and abort recording, then build the rule for
     `addKeybinding`. That rule is achievable: an ESLint custom rule with an allowance table,
     one row per keybinding naming its menu item, shaped like `expectBuilders`. It is left
     unbuilt pending the owner's decision on whether desktop-wide shortcuts are in the class
     (section 4). It is not a toolchain gap.
   - No rule was weakened. The GNOME tree is outside every detector's scope, and that is stated
     in the catalogue's "Not caught".
2. **The next wider rule** (any key read by any means). Not built, for the reasons in Rule. The
   owner decides whether A8 makes it worth building.
3. **Calibration.** fzf `toggle-sort` counts as navigation (display only). This is recorded as
   the project's calibration. If the owner wants it treated as an action, the history search
   would need a visible sort control.

The rule is merged at full width, with no known instance left inside its scope.

## Permanence

- `project_hidden_bindings` is in `rules.All()` (7 rules). The driver test pins it.
- `scripts/qa-go.bash` runs it over both modules. That script is a blocking gate in
  `qa-all.bash`, in `qa-changed.bash` for any `src-go/` change, and in CI.
- `scripts/qa-hidden-bindings.bash` is a row in `scripts/qa-gates.tsv`, so it is blocking in
  `qa-all`. It is also in `CLAUDE/QA.md` and in qa-changed routing for any shell or Python
  change (deviation B48, pinned).

**Green runs through the project's own entry points:**

- At `c384e235`:
  - `./scripts/qa-go.bash`: GO-OK, 59 packages; qa module 57 packages at 94.5% coverage;
    project rules clean.
  - `./scripts/qa-changed.bash`: passed over 36 changed files, including the `hidden-bindings`
    and `go` gates and every suite routed to the changed files.
- At `cf8419f0`: the conformance reviewer reproduced both green runs independently.
- At `8f32c584`: `./scripts/qa-changed.bash` passed, with the `go`, `hidden-bindings`, `ruff`
  and `docs` gates. `HIDDEN-BINDINGS-OK 438 shell file(s), 333 Python file(s)`. The helper tests
  `tests.helpers.qa_environment.test_textual_bindings` pass.

`qa-all` was not run, as instructed. It belongs to the coordinator.

## Toolchain and detector gaps

- **No go-qa-ci manifest or tools register.** The listing and identifier resolution above are
  this project's own conventions.
- **`go/analysis` sees only Go.** The rule needed a second detector for shell and Python. It
  shares the binding grammar (`fzfbind`) so the halves cannot disagree, but it is a separate
  program with its own fixtures.
- **The conformance reviewer could not run as defined.** Its `isolation: worktree` cannot be
  created from this nested worktree: the hooks daemon's seed entries resolve outside the
  nested root. It ran instead as a general-purpose agent given the reviewer's own instructions
  verbatim, read-only on the branch, working in scratch worktrees.
- **The `lint_on_edit` hook runs `shellcheck -x` from a directory where the suites' relative
  `source` paths do not resolve.** It reported SC1091 on unchanged lines of
  `test-ccy-lifecycle.bash`. The project's own shellcheck gate passed. This is a hook
  configuration gap, reported rather than worked around.

## The original defect

**The order was not the one Appendix A asks for, and this is recorded as a deviation.** The
originating instance was first fixed conventionally on a separate branch, in `238529e7`
(fix/picker-visible-actions, with its own tests). That branch had no defence. The fix came into
this branch through the merge `64b8c020`, before the sweep fix `bc232d63`. Both reds had been
committed before the merge. The Go red still fires at the merge, on the shared `--expect`, so
the proof survives. `bc232d63` then moved the originating instance onto `ui.Action` along with
every other instance. History is not rewritten to reorder this.

**Conventional fix.** The Running sessions view's Ctrl-A, Ctrl-X and Ctrl-O are now Key fields
of menu `ui.Action`s, alongside their unkeyed siblings (Move, Attach, Repair). Each is chosen
from the session's Enter menu, which prints the key beside it. The view is itself a listed
choice in the teams list. ccy-sessions was changed the same way.

**Tests that reproduce the defect and now pass:**

- `src-go/internal/teams/picker/sessions_test.go`:
  - `TestTheSessionMenuNamesEveryActionInWords`
  - `TestRunningSessionsIsAChoiceInTheTeamsList`
  - `TestTabIsTheShortcutShownBesideRunningSessions`
- `menu_test.go`: `TestEveryKeyIsInTheMenuOfTheRowsItActsOn`
- `keys_test.go`: `TestAKeyOrAMenuLineRoutesToTheFlowThatOwnsIt`
- `src-go/internal/sessions/picker_test.go`: `TestThePickerListsEveryActionAndTakesOnlyTheirKeys`
- `src-go/internal/ui/actions_test.go`:
  - `TestEveryActionIsAVisibleRowOrMenuLineWithItsKeyBesideIt`
  - `TestAKeyChoosesOnlyWhatTheScreenOrTheRowOffers`

The bash suites `test-ccy-teams.bash` (188), `test-ccy-sessions-take-over.bash` (82) and
`test-ccy-teams-command.bash` (369) drive the built tools through these menus.

## Conformance review

The reviewer reproduced both reds (`8022a152` through `qa-go.bash`; `9e40ea6e` through
`qa-hidden-bindings.bash`, the 10 findings exactly) and both greens at `cf8419f0` from fresh
checkouts. It also showed the rule active in the green tree: a binding planted with
`go vet -overlay` was reported. Its findings and their resolutions:

| Finding                                                           | Resolution                                                                    |
| ----------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| F1: original defect fixed before the sweep, not recorded          | recorded under "The original defect" as a deviation; history is not rewritten |
| F2: Python Buttons matched per file, not per screen               | fixed in `8f32c584` (tests first, and a fixture line)                         |
| F3: `Cut`, `CutPrefix`, `TrimPrefix` let a flag through           | fixed in `8f32c584` (`evade.go` red first)                                    |
| F4: clearing a filter flagged, though query editing is navigation | sentence recorded in the catalogue and above                                  |
| F5: no sentence on Footer hints                                   | sentence recorded in the catalogue and above                                  |
| F6: JavaScript recorded as a toolchain gap                        | reworded as an achievable rule pending the owner's decision (referral 1)      |
| F7: no fixture of what is not caught                              | unmarked lines in `fixture.bash` (`8f32c584`)                                 |
| F8: count, harness path, spec copy, saturation                    | corrected above                                                               |

## After Fable's review (FIX-FIRST, `2f09f717`)

F44 was merged twice, following Fable's resolution: `0020be0d` (F44 at `f01d993a`) and
`c0f9476e` (F44 at `2e4c9103`, which brought the team settings line and the bus status line).
The qa-changed routing deviation became B49, because F44 took B48.

- **B1, version:** CCY 3.99.12.9 with container 2.77.6 unchanged. The coordinator asked for
  3.99.12.6, but F44 reached 3.99.12.8 before the second merge.
- **B2, deploy queue:** every F44 entry is kept. Our text is folded into the
  `play-claude-yolo.yml` comment, and `play-speech-to-text.yml` and `play-basic-configs.yml`
  are appended.

`3b6c4c27` widens the rule, each part test first:

- **N1:** display-only fzf actions are navigation, and so is any key that accepts.
- **N2:** a scalar of options in shell is read word by word.
- **N3:** a Textual `key_NAME` method or `on_key` handler is a finding, and
  `dismiss`/`pop_screen` count as leaving.
- **N5:** the ccy-sessions header names Ctrl-T, Ctrl-X and Ctrl-N.
- **N4:** Plan 00204 Task 2.3 widens the rule to Bubble Tea key handlers before the first one
  lands.

**Green on the merged tree, with `PINGBUS_*` unset:**

- `qa-go.bash`: GO-OK, 62 packages; qa module 60.
- `qa-hidden-bindings.bash`: 446 shell files, 338 Python files.
- `qa-changed.bash`
- Suites: teams 191, take-over 82, reboot 286, tmux-targets 24, teams-command 371, lifecycle
  215, history 23 and 9, qa-changed 121.
