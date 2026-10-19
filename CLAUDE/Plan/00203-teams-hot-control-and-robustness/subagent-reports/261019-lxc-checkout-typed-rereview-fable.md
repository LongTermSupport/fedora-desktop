# Fable re-review: fix/lxc-checkout-typed at 4b408a7b (Plan 00203 Task 3.8, Plan 00204 Task 2.3)

Follows the first review (27fee2f6 on review/lxc-checkout-typed, FIX-FIRST: B1 wrap, B2 merge,
N1 Esc hint). Reviewed against origin/F44 at 0ce52a7e and origin/fix/context-headings at
665ce541. Every claim below was read in the code, run, or probed with a throwaway test in the
worktree (the probes were not committed).

## Verdict: FIX-FIRST

B1, B2 and N1 are fixed as described. One new finding is blocking by the brief's own question
("do wide runes and graphemes work; does pathinput duplicate a width or wrap helper that ui now
has"): pathinput wraps per rune and keeps a second cell counter, so a grapheme cluster is split
across rows and the cursor lands in the wrong column. Everything else holds.

## Blocking

**B3 — pathinput measures width per rune, not per grapheme cluster, with a second width helper
next to the one ui has.** `wrapAt` (`src-go/internal/ui/pathinput/pathinput.go:431-461`) walks
`[]rune` and adds `lipgloss.Width(string(r))` for each rune. A wide rune (CJK, width 2) works:
probed at widths 6-14, no row over width, cursor on the last row at its true column, and a wide
rune that does not fit the last cell goes to the next row with the cursor before it. A grapheme
cluster does not: for a ZWJ family emoji and a flag pair the probe found, at widths 5-10, rows cut
between the cluster's code points (`"/srv/👨‍" | "👩‍👧/si"`, `"/srv/🇬" | "🇧/site"`)
and a model cell count that disagrees with the terminal's (`per-rune 9, terminal 7` for
`"/srv/👨‍👩‍"`; `per-rune 12, terminal 10` for `"/srv/🇬🇧/si"`). The row is drawn narrower than
the model thinks, and `tea.NewCursor(col, row)` (`pathinput.go:182`) is placed from the model's
count, so on such a path the cursor sits two columns right of the text per cluster. Nothing is
truncated, so the owner's "never truncate" holds; it is the brief's wide-rune question and the
DRY one that fail. `heading.go` at 665ce541 does this right (`cells` = `ansi.StringWidth`,
`wrapper.fill` cuts with `ansi.Truncate`, "between grapheme clusters, never elided",
`heading.go:120`, `:171-190` in the merged tree), and after the merge both live in `internal/ui`:
`ui.cells`/`wrapper` unexported in `heading.go`, `pathinput.wrapAt` with `lipgloss.Width` next
door. Fix, after merging 665ce541: export one cell-based wrapper from `ui` (a plain-text form of
`wrapper.fill`, or `wrapAt` moved into ui on `ansi.StringWidth` + `ansi.Truncate`/`uniseg`
clusters), have pathinput call it for the line, the notes, the said lines, the suggestion rows and
the hint, compute the cursor column with the same function, and add one test with a ZWJ sequence
or a flag asserting no row boundary falls inside a cluster and `Cursor.X == ansi.StringWidth(row)`.

## Fixed and verified

- **B1 wrap** — `Update` takes `tea.WindowSizeMsg` (`pathinput.go:146-149`), `Asker.Ask` passes
  `tea.WithWindowSize` when the caller sets one (`:500-503`), `View` wraps every line (`:157-184`).
  `TestEveryLineIsWrappedToTheWindowNeverCut` (20 columns, asserts every rendered line is at
  most 20 wide and that the prompt+path, the note, the suggestion and the hint all survive
  joined; cursor on the last wrapped row at the row's width) and
  `TestTheRenderedScreenAtANarrowWidthLosesNothing` (Bubble Tea's real renderer at 24 columns,
  escapes stripped, the path and both hints present) are real width tests; the first would fail on
  the pre-fix code because `View` then emitted one long line. Cursor at a wrap boundary: probed
  `wrapAt("", "abcdefghij", 10, 10)` → row 1 col 0 (a cursor at the very end of a full row is on
  a row of its own, as the comment says), `cursor 9` → row 0 col 9, `cursor 10` of 11 runes →
  row 1 col 0; `TestTheCursorStaysOnItsWrappedRowWhenMovedBack` covers Home. Correct for
  single-width and double-width runes; the cluster case is B3.
- **B2 heading** — `pathinput.Spec.Heading` is `ui.Heading` (`pathinput.go:44`); a heading with no
  crumbs is refused before the program runs (`:497`, `TestALineWithNoHeadingIsRefusedBeforeItRuns`).
  `ui.Heading.Draw(colour, width)` (`heading.go`) is five lines over the existing `render`; it
  duplicates nothing in 665ce541, which auto-merges `heading.go` and keeps `Draw` calling the
  new cell-based `render`. The branch's own `heading.go` (from 7e6f2ca6) still counts runes
  (`utf8.RuneCountInString`, `heading.go:134-147`), so until 665ce541 is merged a CJK crumb
  overflows a heading line; the merge fixes that for the heading, not for pathinput (B3).
- **Merge with origin/fix/context-headings (665ce541).** `git merge-tree --write-tree 665ce541 4b408a7b`
  conflicts in four files, all resolvable without code change: `src-go/internal/ui/picker_test.go`
  (665ce541 edits `TestAPickThatMatchesNothingLeaves`, a typed-picker test this branch deletes
  along with `PickSpec.Typed/Prompt/Query`; the merged `picker.go` has no `Typed` or `Query`, so
  take this branch's side and drop the test), `src-go/internal/teams/lxcadd/checkout_test.go`
  (665ce541 adds `TestStepFoursHeadingWrapsOnANarrowTerminalAndKeepsEveryAnswer` using
  `r.Script.Columns = 40`, which the merged `teamtest/console.go` has, next to this branch's
  `TestTheCheckoutLineStartsWithTheFirstFound…`; keep both), `CLAUDE/Plan/meta-deploy.list`
  (take this branch's: speech-to-text first as F44 has it, one `play-claude-yolo.yml` line with
  the combined description), and the 00204 journal (keep both sides' entries). The merged tree
  with the conflicted test files left aside builds: `go build ./...` over the extracted result is
  clean, so there is no API clash with 665ce541's `Gum.columns(ctx)`/`Picker.headingWidth(ctx, margin)`
  (the branch's `Gum.Columns`/`Picker.Columns` fields and `terminalColumns` are gone in the
  merged `ui.go`/`heading.go` and nothing on this branch outside `teamtest` referenced them).
- **Merge with origin/F44 (0ce52a7e)**: `git merge-tree --write-tree origin/F44 4b408a7b` is
  clean (tree `61b6b0e4`, no conflicts).
- **N1** — `cancelKey = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("Esc", "cancels"))`
  (`pathinput.go:74`); `TestTheHeadingAndAHintLineSayingTabCompletesAreShown` asserts "Esc cancels".
- **N4** — `search_root` (`helpers/agent_bus_lxc/derive.py:345-366`) appends the root to
  `skipped` when a component on its way is a link and the JSON carries `"skipped"`;
  `parse_checkouts` validates it as a bounded list of absolute paths (`:520-533`); `declare.py`
  prints `SKIPPED\t<root>` (`:1360`); `declare/lxc.go:161-168` reads it into `Checkouts.Skipped`;
  `lxcadd/checkout.go:84-86` prints "`<root> in <container> was not searched: a symbolic link is on its way … type a path under it, or under its target`" (tested `checkout_test.go:255-262`,
  `lxc_test.go:173-175`, `test_derive.py:635-637`, `test_declare.py:1537-1540`). The only
  remaining silent returns in the walker are a missing or non-directory root (`derive.py:357-358`,
  `:363-364`, documented as "passed over" in `docs/agent-bus-lxc-members.md:103`), a
  `FileNotFoundError` race in `child` (`:103-104`) and `real_dir`'s marker probe (`:115-116`);
  the `continue` at `:339-340` is the hidden/`SKIPPED` directory filter. No unreadable directory
  is skipped: `child` dies on any other `OSError` (`:105-106`). One rough edge, not blocking: an
  `OSError` other than not-found in `search_root`'s own `os.open` (`:352`) is not caught, so it
  surfaces as a Python traceback rather than a `die` message; the Go side still reports the
  failure ("could not be listed"), nothing is swallowed.
- **The rule's evasion fixtures, red then green.** `git archive f66868db src-go/qa`, `go test ./rules/ -run TestHiddenBindings`:
  FAIL with exactly the 7 wants unmet the commit message names (`evasions.go:11,16,23,28,31`,
  `notab.go:9,12`). At 4b408a7b `go test ./rules/` passes and the driver over both modules is
  clean. Each fixture matches a real clause: `k := press.String()` and `switch s := press.String(); s`
  through `collect`/`noteKeyVar`/`keyVars` (`teakeys.go:70-101`, `:190-191`),
  `strings.HasPrefix`/`Contains` through `judgeStringsCall` over `argvReaders[stringsPkg]`
  (`HasPrefix, HasSuffix, Contains, Index, Cut, CutPrefix, TrimPrefix, EqualFold`;
  `hiddenbindings.go:289-292`), `key.Binding{}` through `judgeBindingLiteral` (`:158-165`).
- **The Tab exception.** `tabBound` (`teakeys.go:52`, set by `bindsTabWithHelp`, `:257-282`) only
  changes the verdict for `tab`/`shift+tab`/`tea.KeyTab` (`judgeKeyConst`, `:226-229`); any other
  non-line key compared by hand in a Tab-binding package is still "a key compared by hand"
  (fixture `teakeys.go:32,34,40` sits in a package that binds Tab with help and is flagged). So
  no, binding Tab does not buy a package the right to compare other keys. What it does not tie:
  that the Tab binding is drawn by the view, and that it is drawn on the same screen (per package,
  not per model); both are in the catalogue's "not caught" and the named next rule
  (`GoProjectRules.md`, Task 2.4). `lineKeys` still includes `space`, `pgup`, `pgdown` (first
  review's N2); unchanged, acceptable. Two further shapes not caught and not in the list: a key
  read indexed or sliced (`msg.String()[0] == 'c'`) and a second-hand alias assigned before the
  original in source order; worth a line in the catalogue, not a fixture now.
- **Versions.** `claude-yolo` changes only `CCY_VERSION` 3.99.12.10 → 3.99.12.15 and
  `REQUIRED_CONTAINER_VERSION` 2.77.7 → 2.77.9; `Dockerfile` only the label 2.77.7 → 2.77.9. The
  image bump is needed (the image bundles `helpers/agent_bus/*.py`, and `declare.py`'s markers
  changed `TIMEDOUT` → `PARTIAL` + `SKIPPED`); the launcher bump follows from the required
  version. `docs/ccy-changelog.md:20-39` names both, says why the image is rebuilt, and that
  3.99.12.14/2.77.8 were the unreleased first round; the two entry bodies are accurate against the
  code (typed line, Tab, roots, partial notice, skipped roots named, wrap, Esc cancels, Bubble Tea
  rule). Plan 00203 Task 3.8 names 3.99.12.15/2.77.9 (`PLAN.md:183`). The coordinator renumbers.
- **meta-deploy.list** holds the Plan 00179 speech-to-text entry (as F44) and one
  `play-claude-yolo.yml` with the combined 00204/00203 description; see the merge note above.

## Not blocking

- N8 — `wrapped()` for the notes and the hint cuts anywhere, including mid-word, where a space
  would do (`pathinput.go:424-426`); the heading's `fill` breaks at the last space. Cosmetic;
  folds into B3 if pathinput reuses ui's wrapper.
- N9 — `search_root` traceback on a non-ENOENT `OSError` (above).
- N10 — the Tab exception is per package, not per screen (catalogued; Task 2.4).

## Targeted QA run (never qa-all)

- `go test -race -count=1 ./...` over `src-go`: 70 packages ok. `go test ./rules/` in `src-go/qa`: ok.
- `scripts/qa-go.bash`: exit 1 at `TestRealSemgrepScansTestsDirectoriesAndLargeScripts` in
  `qa/cmd/qa-patterns` (semgrep Timeout on `scripts/big.bash`, the known host-load noise; 59
  packages ok before it). Because the script stops there, the later steps were run by hand:
  `golangci-lint fmt --diff` over both modules (no diff), `golangci-lint run` over both (0 issues),
  the project-rules driver built and `go vet -vettool` over `src-go` and over `src-go/qa` (clean).
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK 446 shell file(s), 342 Python file(s)`.
- `python3 -m unittest tests.helpers.agent_bus_lxc.test_derive tests.helpers.agent_bus.test_declare`
  (PINGBUS\_\* unset): 242 tests, OK.
- `scripts/test-ccy-teams-command.bash` (PINGBUS\_\* unset): passed 374, failed 0, exit 0.
- Red reproduction: `go test ./rules/ -run TestHiddenBindings` over `git archive f66868db`: FAIL,
  7 wants unmet (listed above).
- Merge probes: `git merge-tree --write-tree origin/F44 4b408a7b` clean;
  `… 665ce541 4b408a7b` four conflicts (above); `go build ./...` over the merged tree clean.

## Fix-first list

1. B3: wrap and measure pathinput's rows by grapheme cluster with one shared ui helper
   (`ansi.StringWidth`-based, as 665ce541's `heading.go`), cursor column from the same count;
   one test with a ZWJ or flag cluster at a narrow width.
2. On merging 665ce541: drop the deleted typed-picker test, keep both lxcadd tests, take this
   branch's `meta-deploy.list` (as above).
