# Re-review: `fix/context-headings` at 7e6f2ca6 (Fable)

Verdict: **FIX-FIRST**. B1 is not closed: fzf still truncates the wrapped lines. Two more
should-fix items, one of them the reason the first slipped through.

Reviewed: `git diff 2a325942..7e6f2ca6` less the F44 merge 38eb4cf2. Earlier review:
`261016-context-headings-review-fable.md` (B1, N3, N5).

## Checks that passed

- `go mod verify`, `go build -mod=vendor ./...`, `go mod tidy -diff` (clean); the vendor tree
  is unchanged between 38eb4cf2 and 7e6f2ca6; `vendor/modules.txt` already listed
  `golang.org/x/sys/unix`. Only `src-go/internal/ui/heading.go` imports it. The allowlist row
  (`src-go/go-deps.allowlist:37`) names the new use. Policy-wise the promotion is allowed; see
  S2 for why it is not needed.
- `CLAUDE/Plan/meta-deploy.list` holds exactly `playbooks/imports/play-claude-yolo.yml` with
  its description. `git merge-tree --write-tree a153aede 7e6f2ca6` writes a tree with no
  conflicts. `git diff a153aede..7e6f2ca6 -- files/ playbooks/ tasks/` is empty, so the
  "no CCY version bump" claim is right.
- N5 done: `src-go/internal/ui/heading.go:84-90` refuses a whitespace-only crumb;
  `heading_test.go:175` covers `" \t"`. N3 done: the `c.say(h.Trail())` at
  `src-go/internal/teams/choose/choose.go:98` is gone, and its assertion with it.
- No install-specific names in the diff (`container-a`, `container-b`, `dev-session` are
  placeholders).
- QA: `scripts/qa-go.bash` exit 0; `scripts/qa-hidden-bindings.bash` exit 0;
  `go test ./internal/ui/... ./internal/teams/... ./internal/sessions/...` all ok;
  `test-ccy-teams`, `test-ccy-teams-command`, `test-ccy-session-registry`,
  `test-ccy-sessions-take-over` exit 0 with `PINGBUS_*` unset.

## Blockers

**B1 (still open) — fzf truncates the wrapped lines.** Measured on a pseudo-terminal with
the exact house frame from `src-go/internal/ui/picker.go:171-176` (`--border=rounded --margin=1,2 --padding=1,2 --pointer=▶ --header-first`), fzf 0.38:

| columns | header line of N runes | drawn                 |
| ------- | ---------------------- | --------------------- |
| 40      | 26                     | whole                 |
| 40      | 27, 28, 29, 30         | `…abcd..` (truncated) |
| 80      | 66                     | whole                 |
| 80      | 68, 69, 70             | `…abcd..` (truncated) |

The usable header width is `columns - 14`: margin 4, border 2, padding 4, plus fzf's own
two-column header indent (pointer and marker), plus two more. The fix uses
`headingMargin = 10` (`src-go/internal/ui/heading.go:209`), so `headingWidth(40) = 30`
and every line of 27-30 runes is cut off exactly as before, with the answers at the end of
the context line lost. `heading_test.go:30` asserts `30` for a 40-column terminal, so the
test encodes the wrong number. `fallbackHeadingWidth = 68` (`heading.go:210`) is itself two
columns too wide for an 80-column terminal, the common case the fallback is for.

Fix: measure against fzf, not against the argument list; `columns - 14` for the framed
picker (and keep a separate, smaller margin for `Picker.Choose`, which has no border or
margin, `choose.go:33`); fallback at most 66; make the test's expected numbers come from a
pseudo-terminal run or the measured table above, not from the constant.

**B1b — width is counted in runes, not display cells.** `wrap` and `render` use
`utf8.RuneCountInString` (`heading.go:128,131,134,138,176`), and the hard wrap cuts at
`[]rune` indexes (`heading.go:144-146`). A crumb or context with CJK, emoji or other wide
runes (team, container and session names are user-chosen) is twice as wide as counted:
measured, `abcd › ` plus 18 CJK runes (25 runes, 44 cells) at 40 columns draws as
`abcd › 日本語日本語日本..`. The hard wrap can also split a grapheme cluster (a combining
mark, an emoji sequence). `github.com/charmbracelet/x/ansi` is already a direct dependency
and has `StringWidth` and grapheme-aware `Truncate`/`Wrap`; `rivo/uniseg` is vendored too.
Use cells, and cut long pieces at grapheme boundaries. The property test at
`heading_test.go:33-49` should include a wide-rune heading, and compare `ansi.StringWidth`
not `RuneCountInString`.

## Should fix

**S1 — tests outside `ui` depend on the environment they run in.** `headingWidth(0)`
reads `$COLUMNS`, then the terminal. Every production constructor leaves `Columns` 0
(`src-go/internal/sessions/app.go:275`, `src-go/internal/teams/host/host.go:205,208`,
`src-go/internal/ftpcamera/args.go:206,212`, `src-go/internal/freeze/app/app.go:122`), and
the fakes in those packages' tests set nothing either, so the heading in their expected
`--header=` strings is wrapped at whatever width the shell exports. `COLUMNS=30 go test ./internal/...` fails ten tests in five packages: `sessions` (4, e.g.
`picker_test.go:526`), `ftpcamera` (`TestLauncherWithGum`), `freeze/golden`
(`TestTheGoToolsPrintWhatTheBashToolsPrinted`), `teams/create`
(`TestWizardInACheckoutAcceptsEveryDefault`), `teams/ops` (3). `ui`'s own `TestMain`
(`heading_test.go:19-22`) pins the width for `ui` only. Without `COLUMNS` the suites pass
on a 60-column pseudo-terminal because `go test` gives the binary no tty, so the exposure is
a shell that exports `COLUMNS` (`checkwinsize` plus `export`, or a CI image that sets it).
Fix: the width must come through something every test already fakes; see S2.

**S2 — a second source of the terminal's width, next to the one that exists.**
`src-go/internal/ui/gumx.go:59-69` already reads the width (`Gum.columns`, `tput cols`
through the Runner, `fallbackColumns = 80`, `narrowestColumn = 44`), and teamtest already
answers it (`console.go:127-128` returns 80, tested at `console_test.go:155`). The fix adds
a parallel reader (`heading.go:214-230`: `$COLUMNS`, then an ioctl on stderr, stdin,
stdout), its own fallback (68) and floor (16), a package variable for tests to swap, two new
`Columns` fields, and promotes `golang.org/x/sys` to a direct dependency. Two readers, two
fallbacks, two floors for one fact; and the new one is the unfakeable one (S1). Route the
heading width through the Runner the way `Explain` does (`Picker` has a Runner too), drop
`terminalColumns`, the `Columns` fields and the `x/sys` promotion, and the teamtest 400
becomes "answer `tput cols` with the width this test wants".

**S3 — no end-to-end test of wrapping through a real screen.** `wideTerminal = 400`
(`src-go/internal/teams/teamtest/console.go:20`) means no teamtest screen ever wraps, and
the lookalike Move coordinator heading in `heading_test.go:30-33` is built by hand, not by
`src-go/internal/teams/picker/sessions.go:383` or `:348`. ACCEPTANCE H10 asks the owner to
narrow to 40 columns and walk lxc-add to step 4; nothing automated does. Add one teamtest
(or lxcadd) case at a narrow width asserting `HeaderOf(n)` for step 4 holds every answer and
no `..`, and one for the Move coordinator heading, so B1 cannot regress silently. Doing S2
first makes this a one-line width answer per test.

## Nits

- **N1** `src-go/internal/teams/chooser/widen.go:50` still says `head.Trail()` and then
  `Gum.Choose` draws the same trail (the pattern N3 removed from `choose.go`). The 12:54
  journal entry says this is deliberate so the notes after the question sit under a trail;
  if so, say that where N3 was answered, since the two call sites now disagree.
- **N2** `CLAUDE/InteractiveScripts.md:121-122` says "fzf and gum cut off a header line".
  `gumx.go:51-52` says a gum header is never wrapped (which is not the same as cut off), and
  I could not reproduce a cut-off in `gum confirm` 0.17 at 40 columns. Verify or say fzf.
- **N3** `heading.go:139-141`: when `"· " + piece` is wider than the line the mark is dropped,
  so the continued line no longer shows it belongs to the context; and a context with an
  empty part (`a ·  · b`) draws a stray `· `. Minor.
- **N4** `src-go/CLAUDE.md:72-73`: the edited sentence runs to 103 columns where the file wraps
  at about 95; and once S2 lands, `Gum.Columns`/`Picker.Columns` is no longer the fact to state.

## What I ran

fzf and gum probes on a pseudo-terminal (`untracked/scratch/fzf-header-width.py`,
`gum-header-width.py`, not committed); `COLUMNS=30 go test` over sessions, ftpcamera,
freeze, teams; `go test -count=1` on a 60-column pseudo-terminal with `COLUMNS` unset; the
targeted QA listed above. `qa-all` was not run.

**Verdict: FIX-FIRST** — B1 (margin 14, fallback at most 66), B1b (cells and graphemes, not
runes), S1 (tests must not read the shell's width), S2 (one width reader, through the
Runner), S3 (one narrow-width end-to-end heading test). Then MERGE.
