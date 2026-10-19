# Fable: tip check of fix/lxc-checkout-typed at d57086bf

Narrow final check after the FIX-FIRST review at 32548dbd (review/lxc-checkout-typed-final):
(1) PasteMsg ignored, (2) renumber to 3.99.12.12 / 2.77.8, (3) one play-claude-yolo.yml queue
entry, plus the non-blocking control and bidi refusal. Local F44 is d57dab04; d57086bf is the
pushed tip of the branch (`origin/fix/lxc-checkout-typed`).

## The three blocking points

1. **PasteMsg handled.** `pathinput.Update` takes `tea.PasteMsg`: while a Tab listing is being
   read it is queued (`m.queue`) and `replay` applies keys and pastes in the order they arrived;
   otherwise `paste` goes through the same `insert` as typed text, which splits into grapheme
   clusters (`ui.Clusters`) and puts the cursor after them. A key-name-looking paste is text:
   `TestAPasteWhileAListingIsReadIsAppliedAfterItInOrder` pastes `"enter"` then types `z` during
   a listing and gets `/var/enterz`, with `Result()` still `errNotDone`, so the word was never
   taken as Enter. Four new tests (paste at the cursor, paste refused and said, typed text
   refused, paste queued during a listing); the journal records them red first.
2. **Versions.** CCY `3.99.12.12` at launcher line 17, `REQUIRED_CONTAINER_VERSION="2.77.8"` at
   line 81, Dockerfile label `2.77.8`, changelog heading `3.99.12.12 — container 2.77.8` with
   "Follows 3.99.12.11 (container 2.77.7)" above the 3.99.12.11 section, PLAN.md Task 3.8 and
   the queue description all agree. The only other `3.99.12.1x` mentions are historical journal
   and report text (3.99.12.10, 3.99.12.14 in the 26-10-16 journal), which is a record, not a
   claim about the tree.
3. **Queue.** `meta-deploy.list` holds exactly two entries: play-speech-to-text.yml (Plan 00179)
   and one play-claude-yolo.yml whose description folds in 3.99.12.11, Plan 00204 D4, Plan 00201
   Task 2.17 and Task 3.8 (the typed checkout line, paste included, Tab hint, the wider search).

## Control and bidi refusal

`typeable` refuses any rune for which `unicode.IsControl` or `unicode.Is(unicode.Bidi_Control)`
holds; a refused paste leaves the line and cursor untouched and sets a note, a refused typed
text leaves the line untouched. The `refused` fixture covers ESC sequence, `\n`, `\r`, tab, DEL,
U+202E (RLO), U+2066 (LRI), U+200F (RLM), U+061C (ALM), each asserted whole for paste and for
typing. ZWJ and variation selectors are neither control nor Bidi_Control, so a family cluster is
typed whole (`TestTypedTextWithAControlOrBidiCharacterIsRefusedWhole`, second half) and pasted
whole (`TestAPasteGoesInAtTheCursorAsTypedTextAndTheCursorEndsAfterIt`, where the cursor counts
the family as one cluster); the earlier wrap test still drives the family and a flag by key.

## The ui/picker.go resolution

`git diff F44 d57086bf -- src-go/internal/ui/picker.go` removes only the Typed-picker surface:
`PickSpec.Typed/Prompt/Query`, `Picked.Query`, the `fzfNoMatch && s.Typed` branch, `typingArgs`,
`typedOnly`, `splitLine`; the fixed `--prompt=filter > ` and `--bind=q:abort` go back inline.
F44's frame code is intact: `frameMargin`, `framePadding`, `RowChrome`, the `--margin`/`--padding`
arguments, `headerArgs` under a `Heading`, and the width-measured wrapping (3e817bb9, 7e6f2ca6,
117a1026) are all present unchanged. On F44 the Typed picker's only caller was
`internal/teams/lxcadd/checkout.go` (lines 300-322), which this branch replaces with
`ui/pathinput`; `git grep` over d57086bf finds no remaining `Typed:`, `.Query`, `typingArgs`,
`typedOnly` or `splitLine` in `src-go`. Removing a feature whose last user is gone is the right
resolution (YAGNI), not a loss.

## Merge

`git merge-tree --write-tree F44 d57086bf` against local F44 d57dab04 writes a tree with exit 0:
clean.

## Targeted QA (never qa-all)

- `go test ./internal/ui/... ./internal/teams/...`: every package ok (ui, ui/frame,
  ui/pathinput, teams and its 22 flow packages, lxcadd included).
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK 446 shell file(s), 342 Python file(s)`.
- `scripts/test-ccy-teams-command.bash` with PINGBUS_HOME, PINGBUS_TEAMS and
  PINGBUS_MEMBERSHIP unset: passed 381, failed 0.

## Non-blocking note

A variation selector (U+FE0F) is covered by reasoning (it is in neither Unicode class the filter
refuses), not by a dedicated test; the ZWJ family is the tested joiner case. Not worth a round.

## Verdict

MERGE
