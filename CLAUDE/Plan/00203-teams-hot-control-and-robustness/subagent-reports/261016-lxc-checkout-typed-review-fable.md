# Fable review: fix/lxc-checkout-typed at 90c02eed (Plan 00203 Task 3.8, Plan 00204 Task 2.3)

Reviewed against F44 at a153aede. Commits 727810c8 (DBF red), 70c96773 (DBF green), 90c02eed
(the typed checkout line). Checked out in a worktree; every claim below was read in the code or
run, not taken from the commit messages.

## Verdict: FIX-FIRST

One blocking defect against the owner's "never truncate" (B1), one merge hazard the merger must
resolve by hand (B2). The rest is sound: the path safety is good, the DBF rule is red then green,
the versions are right, no install-specific name was added.

## Blocking

**B1 — a long path IS truncated at the terminal width.** Owner requirement: "a very long path
must wrap, never truncate". `pathinput.View`
(`src-go/internal/ui/pathinput/pathinput.go:145-161`) joins `Prompt+line`, every note, every
suggestion row and the hint line as single lines, never wraps them, and `Update`
(`pathinput.go:132-141`) ignores `tea.WindowSizeMsg`, so the model never knows the width.
Bubble Tea v2's renderer then draws the view through
`uv.NewStyledString(view.Content)` (`src-go/vendor/charm.land/bubbletea/v2/cursed_renderer.go:307`)
with `Wrap` left false, which `printString` takes as truncate
(`src-go/vendor/github.com/charmbracelet/ultraviolet/styled.go:61`, `:221`, `:229`): any line
wider than the terminal is cut off at the right edge. A checkout such as
`/var/www/vhosts/<site>/httpdocs/<deep>/…` on an 80-column terminal loses its tail, and
the cursor at `lipgloss.Width(prompt+line[:cursor])` (`pathinput.go:159`) is then placed past
the visible text. No test covers width: `pathinput_test.go` reads `View().Content` directly,
which is the untruncated string, so the suite cannot see this. Fix: take `tea.WindowSizeMsg`
into the model (and `Init` should ask for it or the program should start with the size),
wrap the prompt+line, the notes and the suggestion rows to that width (a path wrapped at `/`
is readable, as `ui/heading.go` on fix/context-headings wraps at its separators), place the
cursor on the wrapped row and column, and add a test that drives a `WindowSizeMsg` of width
40 with a 70-character path and asserts every character is on screen. Until this is fixed the
Bubble Tea screen is worse than the fzf picker it replaces on exactly the case the owner named.

**B2 — the merge with origin/fix/context-headings (7e6f2ca6) conflicts in six source files
and the merge with F44 conflicts in meta-deploy.list.** `git merge-tree --write-tree 90c02eed 7e6f2ca6` conflicts in: `src-go/internal/teams/lxcadd/checkout.go`, `checkout_test.go`,
`adder_test.go`, `src-go/internal/ui/picker.go`, `picker_test.go`,
`CLAUDE/Plan/00204-go-tui-polish/JOURNAL/00204-Journal-26-10-16.md` and
`CLAUDE/Plan/meta-deploy.list`. They are not textual accidents: context-headings makes every
screen take a `ui.Heading` value (`Picker.Pick(ctx, h Heading, s PickSpec)`,
`Gum.Choose(ctx, a.head, …)`) and deletes `Adder.context`/`Adder.header`, while this branch's
`pathinput.Spec.Heading` is a `string` filled by `a.context(ans, "Checkout")`
(`src-go/internal/teams/lxcadd/checkout.go:114`) and `TestEveryQuestionShowsTheTeam…`
(`checkout_test.go:99`) asserts that string. Whichever lands second must port the checkout
line to `ui.Heading` (render the heading's wrapped lines at the top of `View`, in colour as
`heading.go` does) and `picker.go` must drop the `Typed/Prompt/Query` members on the
context-headings side (this branch removes them, that branch keeps and re-signatures them).
`git merge-tree --write-tree 90c02eed a153aede` conflicts only in `CLAUDE/Plan/meta-deploy.list`:
F44 emptied the queue at a153aede after this branch forked from 7f8b1c23, so the branch still
carries the speech-to-text entry. The resolution is the one entry
`playbooks/imports/play-claude-yolo.yml` with this branch's Plan 00203 description
(`meta-deploy.list:30-34`); when context-headings is merged too, keep ONE play-claude-yolo.yml
line with both descriptions above it, not two entries. The 4-line removal in
`CLAUDE/Plan/00208-stt-paste-uses-stale-clipboard/JOURNAL/00208-Journal-26-10-16.md` in the
F44 diff is the same fork artefact and auto-merges; nothing on this branch touched it.

## Not blocking (N)

- **N1 — "Esc back" is not a back.** `pathinput.go:68` labels Esc "back" on the hint line, but
  `askCheckout` (`checkout.go:53-55`) turns `ui.ErrLeave` into `a.cancelled()`: the whole
  lxc-add run ends with exit cancelled, nothing written (tested at `checkout_test.go:229`).
  The hint should say what happens ("Esc cancel"), or the step should go back to User.
- **N2 — the Tab exception is not mechanically tied to a hint line.** `qa/rules/teakeys.go:26-31`
  puts `tab` (and `up`, `down`, `space`, `pgup`, `pgdown`, `ctrl+c`, …) in `lineKeys`
  unconditionally; nothing in the rule checks that a screen comparing `"tab"` by hand also
  draws a hint. The tie exists only in prose (`CLAUDE/GoProjectRules.md:255-259`) and in
  pathinput's own test (`pathinput_test.go:110`). The catalogue's "not caught" list says so
  (`GoProjectRules.md:343-345`), so it is honest, but the list is wide: an action on Space or
  Page Down compared by hand passes. Acceptable for now; narrow it when a second screen lands.
- **N3 — rule false negatives the spot check found** (recorded for the conformance reviewer):
  `k := msg.String(); if k == "ctrl+x"` and `switch s := msg.String(); s {…}` are not judged
  (the tag/operand is an identifier, not a key read; `teakeys.go:60-62`, `:97-105`);
  `strings.HasPrefix(msg.String(), "ctrl+")` and `slices.Contains(keys, msg.String())` are
  not judged. The common bypasses the brief named ARE caught: a `switch msg.String()` with
  constant cases (`judgeKeySwitch`), `key.NewBinding` without `WithHelp` (`judgeBinding`), a
  rune compare `msg.Code == 'x'` (non-string constant falls through to the report,
  `teakeys.go:126-137`), and `msg.Key().Code` (`Key` is in `teaKeyTypes`). A `key.Binding{}`
  literal cannot set keys (the real type's fields are unexported,
  `vendor/charm.land/bubbles/v2/key/key.go:43-47`), so that route is closed.
- **N4 — a search root that is a symbolic link is skipped with no word to the user.**
  `derive.py:344-354` (`search_root`) returns None for a link anywhere on a root's way, and
  `derive.py:355-358` searches the next root; `found.Partial` stays false, so lxc-add prints
  nothing about it. On a distribution where `/var/www` or `/srv` is a link this is a silent
  skip; the doc says it (`docs/agent-bus-lxc-members.md:104-105`) but the screen does not.
  Not a fail-fast violation (nothing depends on it; the path can be typed), but the owner
  asked for unreadable or unsearched places to be named: add the skipped roots to the
  reader's output and the notice.
- **N5 — in `dirs` mode a path that exists but is a file is reported as "does not exist"**
  (`derive.py:364-365` only sets `exists` for a directory; `checkout.go:127-129` wording).
  Cosmetic.
- **N6 — the cancelled-while-listing goroutine.** Esc or Ctrl-C during a Tab listing
  (`pathinput.go:165-170`) quits the program while the `tea.Cmd` from `complete()`
  (`pathinput.go:210-213`) is still running `sudo python3` under the 20 s `ReadTimeout`
  context (`checkout.go:166-168`). Not a leak in practice: `cancelled()` ends the process. A
  future reuse of `pathinput` inside a longer-lived flow should cancel the listing on quit.
- **N7 — ordering nit in `docs/ccy-changelog.md`:** the branch inserts a blank line before
  `## 3.99.12.9` (`ccy-changelog.md:45`), harmless.

## What was checked and holds

**1. Path safety.**

- A typed path cannot escape the rootfs. The Go side `path.Clean`s it (`pathinput.go:256`), so
  `/a/../..` is `/`; `declare lxc-check` refuses anything outside
  `/[A-Za-z0-9_./+@:-]*` (`helpers/agent_bus/declare.py:1186`, `:1216`) before any read; the
  reader's `split` dies on a relative path or any `.`/`..` component (`derive.py:265-271`);
  every component is opened `O_PATH|O_NOFOLLOW` relative to the previous descriptor and a
  symbolic link anywhere is a `die` naming it (`derive.py:99-109`, `:127-138`), never
  followed and never resolved onto the host. Directory listings go through the same `walk`
  (`derive.py:360-371`); `subdirs` lists only non-link directories (`derive.py:122-125`).
  Bind mounts are mapped only from the container's own LXC config (`derive.py:233-242`,
  `locate` at `:255-263`), which is the container's view, not an escape.
- The chosen checkout is validated inside the rootfs: `checkoutOK` (`adder.go:418-434`) runs
  lxc-check, then the `probe` mode through the same reader (`derive.py:300-311`), which dies
  on a link and requires `.git/config` to be a regular bounded file. Symlinks are refused
  rather than resolved, which is stricter than the brief asked.
- Stored as the container-side path: `ans.checkout = picked` (`checkout.go:64`) →
  `declare.LxcEntry{Checkout: a.checkout}` (`adder.go:134`); the summary and the next-steps
  text name it as the path in the container (`adder.go:544`, `:674`, `:698`).
- No host or podman session is offered: the reader reads only the container's rootfs (or its
  init's `/proc/<pid>/root` for unmounted stores, `derive.py:201-208`); the suggestions are
  only what `lxc-checkouts` returned for that container.
- Names from the container's tree: the reader's `die` and lxcfreeze's `plain` strip
  non-printables (`derive.py:81-83`, `lxcfreeze/lister.go:130-137`); `parseListing` refuses
  `.`, `..`, a `/`, an over-long name, and leaves out unprintable names with a count
  (`checkout.go:184-207`, tested `checkout_test.go:187-211`).

**2. Component behaviour.**

- Non-tty: `prepare` refuses when stdin or stderr is not a terminal (`adder.go:239-241`);
  the program reads `h.Stdin` and draws on `h.Stderr` (`adder.go:114`), so stdout stays the
  payload.
- Ctrl-C and Esc: `ui.ErrCancelled`/`ui.ErrLeave` → `term.ExitCancelled`, nothing written
  (`checkout.go:53-55`, `checkout_test.go:229-238`); `tea.ErrInterrupted` is mapped too
  (`pathinput.go:420-422`).
- Empty or relative submit: said on the line and kept editing (`pathinput.go:250-258`,
  `pathinput_test.go:208`); a nonexistent or non-checkout path is refused with the reader's
  reason and asked again from its parent directory, `maxTries` = 3, then "Giving up … nothing
  was changed" with exit failure (`checkout.go:51-71`, `adder_test.go:405-417`,
  `:651-670`). That is the bounded re-prompt InteractiveScripts.md asks for.
- Heading: `Spec.Heading` is drawn first (`pathinput.go:146`) and the lxcadd test asserts the
  full context string (`checkout_test.go:99`). Honoured as a string; see B2 for `ui.Heading`.
- Only `internal/ui` imports Charm: `pathinput.go`, `pathinput_test.go`, `frame.go`,
  `frame_test.go`; `qa/rules/teakeys.go` names the package paths as strings and
  `qa/godeps/run_test.go:352` has one inside an error-message fixture. No flow imports it.

**3. Discovery.**

- The time bound is real: the reader checks `time.monotonic() >= deadline` on entry to every
  directory (`derive.py:317-319`) and the Go side wraps the whole read in
  `context.WithTimeout(ctx, ReadTimeout)` (`checkout.go:77`), distinguishing the deadline
  from any other failure (`checkout.go:85-88`). No goroutine outlives the call (`defer cancel`).
- The cut-short notice is shown: `PARTIAL` → `Checkouts.Partial` → "stopped at its time or
  count limit, so it may have missed some" (`checkout.go:92-94`, `checkout_test.go:263`);
  the count limit is a `cut` too (`derive.py:325-327`, tested
  `tests/helpers/agent_bus_lxc/test_derive.py:636-643`).
- Unreadable directories: `child` dies on any `OSError` but `FileNotFoundError`
  (`derive.py:105-106`), naming the path; a listing that fails is said on the line
  (`pathinput.go:218-221`, `checkout_test.go:213-227`). `real_dir` swallows `OSError`
  (`derive.py:111-116`), used only for the `.claude`/`.git` markers and the `git` flag, where
  "not a directory" is the right answer. The silent case is N4.

**4. The DBF rule.** Spot check only: `go test ./rules/` over `git archive 727810c8` fails
`TestHiddenBindings` with the 10 wants unmet listed in the journal; at 90c02eed `go test` and
`scripts/qa-hidden-bindings.bash` pass. Bypass coverage is in N3; the Tab exception in N2.

**5. Versions.**

- 2.77.8 is required: the image's pingbus zipapp bundles every `*.py` under
  `helpers/agent_bus/` (`helpers/pingbus/bundle.py:5`, `:39`), so `declare.py`'s `PARTIAL`
  marker change changes the image content. Label and `REQUIRED_CONTAINER_VERSION` agree
  (`Dockerfile:36`, `claude-yolo:81`); CCY 3.99.12.14 at `claude-yolo:17`.
- Changelog entry present and accurate (`docs/ccy-changelog.md:20-34`).
- meta-deploy.list: see B2 for the resolution.

**6. Install-specific names, docs, plan.** No hostname, user, path or container name from an
install in the added lines (tests use `container-a`, `agent`, `example-org`). `docs/agent-bus-lxc-members.md:96-107`
and `src-go/internal/teams/usage.txt:107-110` describe the new step correctly. Plan 00203 Task
3.8 is 🔄 with the right versions (`PLAN.md:177-184`); Plan 00204 Task 2.3 ✅ with red/green
SHAs (`PLAN.md:169-171`); both journals carry the entries; the 00204 journal records honestly
that the conformance reviewer could not be dispatched.

## Targeted QA run (never qa-all)

- `go test -race` over `internal/ui/...`, `internal/teams/lxcadd`, `internal/declare`,
  `internal/teams/teamtest` and `qa/rules`: pass.
- `scripts/qa-go.bash`: pass. `scripts/qa-hidden-bindings.bash`: pass.
  `scripts/qa-go-deps.bash`: pass.
- `scripts/test-ccy-teams-command.bash` (PINGBUS\_\* unset): pass, exit 0.
- `python3 -m unittest tests.helpers.agent_bus_lxc.test_derive tests.helpers.agent_bus.test_declare`
  (PINGBUS\_\* unset): 241 tests, OK.

## Fix-first list

1. B1: wrap every drawn line of `pathinput` to the terminal width (`tea.WindowSizeMsg`), put
   the cursor on the wrapped row, add the width test.
2. B2: when merging, resolve `CLAUDE/Plan/meta-deploy.list` to the single
   `play-claude-yolo.yml` entry with its description; when fix/context-headings merges, port
   `pathinput.Spec.Heading` to `ui.Heading` and keep one play-claude-yolo.yml line.
3. N1: the Esc hint says "cancel" (or Esc goes back a step).
