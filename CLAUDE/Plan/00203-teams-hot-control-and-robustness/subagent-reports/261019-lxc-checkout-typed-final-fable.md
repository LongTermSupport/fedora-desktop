# Final review: `fix/lxc-checkout-typed` at ebc35cb5 (Fable)

Re-review ef0d2ac2 was FIX-FIRST on B3 (per-rune wrapping in pathinput with its own cell
helper) and on the 665ce541 merge. Both are closed; one new defect found.

## Verdict: FIX-FIRST

1. **A pasted path does nothing.** Bubble Tea v2 enables bracketed paste by default
   (`vendor/charm.land/bubbletea/v2/cursed_renderer.go:140`), so a paste reaches the model as
   `tea.PasteMsg`, which `Model.Update` (`pathinput.go:146-159`) does not handle. Probe (scratch
   test, not committed): `Update(tea.PasteMsg{Content: "/srv/pasted"})` leaves `Line()` empty.
   Pasting is the normal way a path gets into a path line. Handle `PasteMsg` through the same
   path as typed text (clusters inserted at the cursor, control characters refused) and add a
   test.
2. **Version numbers.** CCY `3.99.12.15` / container `2.77.9` are placeholders. Local F44 is
   `3.99.12.11` / `2.77.7`, so the merge takes `3.99.12.12` / `2.77.8`, in
   `claude-yolo:17` and `:81`, `Dockerfile:36`, the changelog heading and its "Follows
   3.99.12.10 (container 2.77.7)" line (now follows 3.99.12.11), and the queue entry. **The
   container bump is still required**: the image copies the pingbus zipapp
   (`Dockerfile:405` `COPY optional/`), which `helpers/pingbus/bundle.py:39` builds from every
   `*.py` under `helpers/agent_bus/`, and the branch changes `helpers/agent_bus/declare.py`
   (11 lines). The changelog's reason is accurate.
3. **Queue resolution.** `git merge-tree --write-tree F44 ebc35cb5` against local F44 22d7fbb9
   conflicts only in `CLAUDE/Plan/meta-deploy.list`, `docs/ccy-changelog.md` and the two
   version lines of `files/var/local/claude-yolo/claude-yolo`; everything else is clean (the
   compose `CCY_COMPOSE_DECLINED` change in `lib/network-management.bash` comes in from F44
   untouched). F44's queue has two `play-claude-yolo.yml` entries (D4 and 3.99.12.11); the
   resolved queue is the Plan 00179 speech-to-text entry plus ONE `play-claude-yolo.yml`
   entry whose description covers D4, 3.99.12.11's compose fix and this work.

Non-blocking:

- **N1** No test pins that a control character in `KeyPressMsg.Text` is refused
  (`pathinput.go:320`). Probe: text with ESC or DEL is dropped whole; a bidi override (U+202E,
  Cf) is accepted. Add a one-line case to the cluster test.
- **N2** `scripts/test-ccy-restart-request.bash` is tracked mode 100644 (pre-existing, not this
  branch).

## Verified

- **ui.Width identical.** `git diff origin/fix/teams-full-handles ebc35cb5 -- src-go/internal/ui/width.go width_test.go` is empty.
- **One cell helper.** `heading.go` has no private `cells()`; every measure is `ui.Width`, and
  the wider-than-a-line cut is `Clusters(text)[0].Text` (was `string([]rune(text)[0])`,
  which split a flag). `ui.Clusters` (`clusters.go`) is `ansi.FirstGraphemeCluster` with
  `GraphemeWidth`; its test asserts each cluster's cells equal `Width`.
- **pathinput by cluster.** `Model.line` is `[]ui.Cluster`; insert, delete, Ctrl-W, Left/Right
  and `wrapAt` all index clusters. `wrapAt` takes the cursor from the same cell count as the
  rows. Probes at 14 columns: "Checkout: /abc" (14 cells) puts the cursor at row+1 col 0 on an
  empty row, as a terminal does; Left returns it to row 0 col 13; a two-cell cluster at the
  last column wraps whole with the cursor before it at col 0; width 1 gives a wide cluster a
  row of its own. `TestAGraphemeClusterIsWrappedWholeAndTheCursorCountsItsCells` pins the
  family (five runes, two cells) and flag cases at 14 columns, cursor and Backspace included.
- **ZWJ typeable.** Only `unicode.IsControl` text is refused; the family cluster is inserted as
  one cluster.
- **Heading cut matches context-headings' final.** `git diff 665ce541 ebc35cb5 -- heading.go`
  is only `cells`→`Width` (same `ansi.StringWidth`), the cluster cut above, and the new
  `Draw`. `framedMargin 14`, `plainMargin 4`, `gumMargin 2` and `measuredHeaderWidth {40: 26, 80: 66}` are unchanged, so 2b155a17's measurements stand.
- **Merge rules.** 665ce541 is an ancestor of ebc35cb5 and of F44; merge-base is 665ce541.
- **QA run**: `go test ./internal/ui/... ./internal/teams/lxcadd/...` ok; `go test ./rules/...`
  in `src-go/qa` ok (teakeys included); `scripts/qa-hidden-bindings.bash` HIDDEN-BINDINGS-OK;
  `scripts/test-ccy-teams-command.bash` with `PINGBUS_*` unset: passed 374, failed 0.
