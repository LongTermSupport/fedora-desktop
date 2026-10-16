# Plan 00204 research: what the Go tools' TUI should be

Research behind [PLAN.md](PLAN.md). It covers five things:

1. What the Go tools draw today, and why they look like the bash.
2. The hard constraint: a dependency-free `src-go/go.mod`.
3. The library options, judged on the same criteria.
4. A concrete screen design for `ccy-teams` and `ccy-sessions`, with before and after sketches.
5. What can be done now in fzf and gum, what needs a library, and how to migrate.

Sources are named inline. External versions were read from the Go module proxy
(`https://proxy.golang.org/<module>/@latest`) and the projects' GitHub releases. They are as of
the date this plan was created. Local facts carry file and line references on the branch this
plan was cut from.

## 1. What we use now, and why

### 1.1 The `internal/ui` package

`src-go/internal/ui` is the only package allowed to run `gum` and `fzf`. The project rule
`project_seam_commands` enforces this ([GoProjectRules.md](../../GoProjectRules.md), the
`commandOwners` table in `src-go/qa/rules/table.go`). Both programs run through
`execx.Runner`, so a test sees the exact argv.

| Widget                          | Program      | What it draws                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| ------------------------------- | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `Picker.Pick(PickSpec)`         | fzf          | One bordered box. The title sits on the frame (`--border-label`), a header block is above the rows, and an `Exit` row is always last. The fixed flags are `--ansi --height=~70% --layout=reverse --border=rounded --margin=1,2 --padding=1,2 --header-first --pointer=▶ --prompt="filter > " --bind=q:abort` (`ui/picker.go`, `Pick`). Keys besides Enter come back through `--expect`. The header can follow the cursor through a `transform-header` binding (fzf 0.45 or later). |
| `Picker.Confirm`                | fzf          | A picker with two rows, `Yes, <action>` and `Exit`.                                                                                                                                                                                                                                                                                                                                                                                                                                |
| `Picker.Choose(ChooseSpec)`     | fzf          | The plain legacy screen of the freeze tools: no border and no Exit row.                                                                                                                                                                                                                                                                                                                                                                                                            |
| `Gum.Confirm/Input/Choose/Spin` | gum          | One-shot questions printed below whatever was on screen before.                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `Gum.Box/Style/Explain`         | gum          | A rounded box, or wrapped text.                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| `term.Step` / `term.PrintError` | (Go, `term`) | `N/M  Title` in bold amber; `✓ value` in green; `ERROR:` in red. Colour is used only on a terminal, and never under `NO_COLOR`.                                                                                                                                                                                                                                                                                                                                                    |

There is one accent colour, terminal index 3 (amber), and nothing else is themed (`ui.Accent`,
`ui.gumEnv`). gum's key-help line is hidden because gum 0.17 cannot recolour it.

### 1.2 Why it looks like the bash: the decision

The look was never chosen as a design. It is a by-product of how the ports were tested.

- **Plan 00191, Non-Goals** ([PLAN.md](../00191-go-cli-tooling/PLAN.md), the "full-screen TUI
  redesign" bullet): *"the port keeps the same prompts and the gum/fzf widgets so the existing
  suite is the oracle. A Bubble Tea UI is a later phase, with the owner looking at it."* The
  reason given is the test oracle. `scripts/test-ccy-teams-command.bash` passing unchanged
  against the Go binary was the proof the port was complete. `internal/ui` was ported "argv for
  argv" from the bash (Plan 00191 Task 1.3).
- **Plan 00192, the freeze tools.** A golden replay compares the Go output with the recorded
  bash output byte for byte (`internal/freeze/golden`). `SPEC-freeze.md` §15 D2 keeps the
  legacy fzf flags "for parity" and leaves "the house style" for later. D2 was not the owner's
  decision. The plan journal for that day records that the coordinator took the spec's proposals
  as "parity-preserving and reversible".
- **The method has since been reversed for new ports.** [src-go/CLAUDE.md](../../../src-go/CLAUDE.md)
  says a port is built to a spec of intended behaviour, and *"Do not record the bash's output
  as an oracle for a new port."* The existing replays *"stay as regression tests; they are not
  extended. Where one gets in the way, replace it with expectation tests rather than recording
  again."*
- **Plan 00177** (ccy-teams, human friendly) did the UX work inside that constraint. It added
  numbered wizard steps, ✓ answers, a row menu so no action needs a hidden key (Phase 6), and
  colour. UX-REVIEW.md F22 records the owner asking for "a proper CLI UX system". The answer
  then was gum, with the fzf picker kept.

So the owner is right: the Go look is the bash look on purpose, so that the bash tests kept
passing. Nothing about the design itself was preserved deliberately. The "later phase" Plan
00191 promised is this plan.

### 1.3 What the owner sees: the gaps

From the screen inventory (section 4 has the literal screens):

- **No sense of place.** Each fzf screen is a separate process with its own box. Its title is
  one word on the frame ("Agent teams", "team alpha", "CCY sessions"). Nothing shows the path
  that led to the screen. A gum wizard prints below whatever was on screen, so after two
  screens the terminal is a scroll of old boxes.
- **A wizard loses its answers.** In lxc-add the answers are echoed as `✓ value` lines as the
  wizard goes. Each new gum prompt then redraws below them, so the answers scroll away, and
  there is no step bar.
- **Three different confirmations:**
  - gum confirm's two buttons;
  - fzf's `Yes, …` / `Exit` rows;
  - gum choose for a three-way choice.
    The same choice of coordinator role is asked in three ways: no question at all in lxc-add
    when the team already has one, gum choose `worker`/`coordinator` when it has none, and fzf
    rows with a hand-over in the running-sessions view.
- **Errors leave the screen.** `ERROR: …` lines and "Press Enter to go back to the list."
  print to stderr below the last box.
- **Hidden keys remain.** In `ccy-sessions`, Ctrl-T (take over), Ctrl-X (end) and Ctrl-N (new)
  are keys and nothing else (`internal/sessions/picker.go`). In `ccy-teams`, Tab (running
  sessions) has no menu row. That breaks the owner's rule. The rule is on branch
  `dbf/hidden-bindings` as InteractiveScripts.md rule 14: *"TUI must be UX controls, not magic
  keys. Keys are OK, but they MUST NOT BE THE ONLY CONTROL SURFACE."*
- **The header is a wall of text.** The ccy-teams list header has five lines: a key legend, a
  per-row footer, a Ctrl-N note, a RESTORE explanation and the column heading.

## 2. The hard constraint: a dependency-free `src-go/go.mod`

### 2.1 What the rule is and why

`src-go/go.mod` holds only `module` and `go 1.26`: no `require`, no `go.sum`, no `vendor/`.
The one reason given, repeated in five places, is in
[GoProjectRules.md](../../GoProjectRules.md) L15-17:

> the shipped binaries are built on the host with plain `go build` and `src-go/go.mod` must
> stay dependency-free.

The same wording is in `scripts/qa-go.bash`, `src-go/CLAUDE.md`, [QA.md](../../QA.md) and
`SPEC-qa-changed.md` §11.1. That is why the QA rules live in a nested module (`src-go/qa`),
which may have dependencies (goldmark, `mvdan.cc/sh`, `x/tools`, `x/mod`, `yaml/v3`; `go.sum`
committed, no vendor directory).

The rule does not say *why* a plain `go build` must stay plain: being offline, distrust of the
proxy, and the root-owned `watermark` binary are never stated. **No gate enforces it.** No test
looks for a `require` line in `src-go/go.mod`; it is held by the docs alone.

### 2.2 How the host build works today

`tasks/build-go-command.yml` runs as the login user, from `<checkout>/src-go`:

- argv: `go build -trimpath -buildvcs=false [-ldflags …] -o ~/.cache/ccy-go-build/<name> ./cmd/<name>`;
- environment: `GOTOOLCHAIN=local`, **`GOFLAGS=-mod=readonly`**, `GOCACHE`, `GOBIN`. No
  `GOPROXY` is set, so the default `https://proxy.golang.org,direct` applies;
- install: a checksum-compared `copy`. It is root-owned only for `watermark`
  (`go_command_owner: root`).

**The host already fetches modules from the proxy.** Plan 00199's `play-md-viewer.yml` builds
`github.com/LongTermSupport/md-viewer/cmd/md-viewer@<pinned>` with `go install` through the
same task file (`go_command_module`). So a "no network at build time" reading is already not
true of the host.

### 2.3 What relaxing it would cost, by route

| Route                                        | What changes                                                                                                                                                                                                                                               | Host build                                                                                           | Review surface                                                       |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- |
| **A. Vendored, committed** (`go mod vendor`) | `require` lines, `go.sum` and `src-go/vendor/` committed. `build-go-command.yml` sets `GOFLAGS=-mod=vendor` for src-go builds (see the trap below). `scripts/qa-trees.tsv` gets a `vendor` row, so the shell, Python, YAML and markdown discovery skip it. | Still a plain `go build`, **offline**: every byte compiled is in the checkout. Reproducible, as now. | Every dependency bump is a reviewable diff in this repository.       |
| B. Proxy and `go.sum`, not vendored          | `require` and `go.sum` committed. The host build downloads modules, verified by `sum.golang.org`.                                                                                                                                                          | Needs the network on the first build after a bump (md-viewer already does).                          | Only `go.sum` hashes in the diff; the code is not in the repository. |
| C. Keep the rule                             | Nothing. Only fzf and gum (section 5.1).                                                                                                                                                                                                                   | As now.                                                                                              | None.                                                                |

**The `-mod` trap in route A.** The Go reference says: *"if the go version in go.mod is 1.14 or
higher and a vendor directory is present, the go command acts as if -mod=vendor were used.
Otherwise … -mod=readonly"* ([go.dev/ref/mod#build-commands](https://go.dev/ref/mod#build-commands)).
`build-go-command.yml` sets `GOFLAGS=-mod=readonly` explicitly, and an explicit flag overrides
that default. Left as it is, the host build would ignore `vendor/` and go to the proxy. So
route A must change the flag to `-mod=vendor` for the in-repository branch of the task. The
`go_command_module` branch (md-viewer) keeps `readonly`. `vendor/modules.txt` is checked
against `go.mod` on every build, and a mismatch is a hard error (same page), so a vendor tree
that has drifted fails fast.

**The cost of the vendor tree, measured.** I built a scratch module (`untracked/scratch/`, not
committed) importing Bubble Tea v2.1.0, Bubbles v2.2.1, Lip Gloss v2.0.6 and Huh v2.0.3, with
Go 1.26.9:

- `go mod vendor` brings in **26 modules**: the 4 named, plus colorprofile, ultraviolet,
  `x/ansi`, `x/term`, `x/termios`, `x/windows`, `x/exp/ordered`, `x/exp/strings`, atotto/clipboard,
  catppuccin/go, clipperhouse/displaywidth and uax29, dustin/go-humanize, go-colorful,
  go-runewidth, hashstructure, cancelreader, rivo/uniseg, sahilm/fuzzy, xo/terminfo,
  `golang.org/x/sync` and `x/sys`.
- The tree is 15 MB on disk (2.6 MB gzipped): 638 `.go` files, 295 k lines. `golang.org/x/sys`
  is 8.9 MB of that and about 214 k of the lines; the rest is 81.5 k lines. 190 files are for
  other operating systems.
- Besides Go files it copies 18 `README.md`, 6 `.golangci.yml` and 19 other YAML or shell files.
  This is why the `qa-trees.tsv` row matters: the docs and YAML gates would otherwise scan them.
  The docs gate (`qa/docs`) narrows to core documents, but its `markdown` discovery list must
  exclude `vendor` too.
- **Binary size:** the scratch program is 5.8 MB, against 2.4 MB for a stdlib-only hello world,
  so about **+3.4 MB** per binary. Today `ccy-teams` is 6.0 MB and `ccy-sessions` 4.5 MB (same
  build flags).

Supply-chain tools that apply under either route:

- `govulncheck` (v1.9.0, `golang.org/x/vuln`) reports only the vulnerabilities the code
  actually reaches ([go.dev/doc/security/vuln](https://go.dev/doc/security/vuln/)). Plan 00191
  already lists it as a Go QA goal.
- `GOSUMDB=sum.golang.org` verifies every module download by default
  ([go.dev/ref/mod](https://go.dev/ref/mod)).
- A bump becomes a routine through the `/update-versions` skill: `go get <mod>@<v>`,
  `go mod vendor`, and a reviewed diff.

## 3. The options

### 3.1 The candidates

| Option                                         | Version (module path)                                                                                                                                                              | Licence           | Status                                                                                                                                                                                              |
| ---------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Charm: Bubble Tea, Bubbles, Lip Gloss, Huh** | Bubble Tea v2.1.0, 2026-10-08 (`charm.land/bubbletea/v2`); Bubbles v2.2.1 (`charm.land/bubbles/v2`); Lip Gloss v2.0.6 (`charm.land/lipgloss/v2`); Huh v2.0.3 (`charm.land/huh/v2`) | MIT               | v2 shipped 2026-02-24 (Huh 2026-03-09) with new `charm.land` paths. Very active. **gum itself is built on these libraries** (gum v2.0.0, 2026-08-20, "Bubble Tea and Lip Gloss v2, no UX changes"). |
| **tview** on tcell                             | tview v0.42.0, 2025-08-27, its only tag (`github.com/rivo/tview`); tcell v2.13.10 / v3.6.0                                                                                         | MIT / Apache-2.0  | Developed on master with no versioning policy; still on tcell v2.                                                                                                                                   |
| **fzf and gum, framed better**                 | fzf v0.74.5, 2026-10-11; gum v2.0.2, 2026-09-24                                                                                                                                    | MIT               | What we have. The host runs Fedora's fzf (0.70.0 when Plan 00039 checked); the ccy image has Debian's 0.38.0.                                                                                       |
| vaxis                                          | v0.17.1, 2026-07-28 (`git.sr.ht/~rockorager/vaxis`)                                                                                                                                | Apache-2.0        | Pre-v1. Asks the terminal for its capabilities rather than reading terminfo. Has a widget framework (vxfw). Small community.                                                                        |
| pterm                                          | v0.12.84, 2026-10-10                                                                                                                                                               | MIT               | Active, but printers and prompts, not a full-screen TUI. It would reproduce today's scroll-of-prompts problem.                                                                                      |
| gocui, termui, go-prompt                       | awesome-gocui v1.1.0 (2022); termui v3.1.0 (2019); go-prompt v0.2.6 (2021)                                                                                                         | BSD-3 / MIT / MIT | Dormant. Not credible for new work.                                                                                                                                                                 |

Sources: the module proxy for every version; GitHub releases for
[bubbletea](https://github.com/charmbracelet/bubbletea/releases),
[bubbles](https://github.com/charmbracelet/bubbles/releases),
[lipgloss](https://github.com/charmbracelet/lipgloss/releases),
[huh](https://github.com/charmbracelet/huh/releases),
[gum](https://github.com/charmbracelet/gum/releases),
[fzf](https://github.com/junegunn/fzf/releases);
[tview](https://github.com/rivo/tview);
[tcell CHANGESv3.md](https://raw.githubusercontent.com/gdamore/tcell/v3.6.0/CHANGESv3.md);
[vaxis](https://pkg.go.dev/git.sr.ht/~rockorager/vaxis); the GitHub API for licences and
activity.

### 3.2 Judged

| Criterion                                        | Charm (Bubble Tea v2 + Bubbles + Lip Gloss, Huh optional)                                                                                                                                                                                                                                                                                        | tview                                                                                                                                       | fzf and gum, framed                                                                                                                                                                                         |
| ------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Polish out of the box                            | High, and it is the look gum already has. Colour is detected and downsampled automatically (`colorprofile`), and the terminal background is detected so themes suit light and dark.                                                                                                                                                              | Functional and boxy: the classic widget-toolkit look. Polish is hand work.                                                                  | Good *per screen*: each screen is still a separate program, so the moments between screens cannot be polished.                                                                                              |
| Titles, breadcrumbs, a persistent context header | We draw it: one Lip Gloss frame (header, context, body, footer) around every screen, in one long-running program, so it never disappears between screens.                                                                                                                                                                                        | `Flex` and `Pages` give a fixed header and footer around switching pages.                                                                   | A breadcrumb in `--border-label`, context in `--header` with `--header-border`, actions in `--footer` (fzf 0.63 and later). Gum screens can only print a header *above* the prompt; nothing keeps it there. |
| Forms and wizards with step indicators           | Huh gives fields, validation and groups (pages). **Huh has no step indicator**, so a step bar is ours: a line in the frame. Huh's fields can be embedded in our model, or we use Bubbles' `textinput` and `list` directly.                                                                                                                       | `Form` with input, drop-down and checkbox; no step bar either.                                                                              | Steps are separate gum calls; our own `N/M` line is printed before each, and earlier answers scroll away. With a clear and redraw before each step they can be kept (section 5.1).                          |
| Mouse                                            | Bubble Tea v2 reports click, release, wheel and motion (`MouseModeCellMotion`). Hit-testing is ours (no Bubbles component handles `MouseClickMsg`, checked in the vendored v2.2.1 source), and simple in a frame we lay out ourselves. `bubblezone` adds named click zones, but its README warns it may not work with Lip Gloss v2's compositor. | `EnableMouse(true)`: widgets take clicks natively.                                                                                          | fzf takes clicks on rows and the wheel by default; `click-header` (0.52) and `click-footer` (0.65) events. Mouse support in gum was not verified.                                                           |
| Accessibility                                    | Huh's accessible mode (`form.WithAccessible(true)`, on by itself under `TERM=dumb`) turns a form into plain sequential prompts for screen readers. Bubble Tea itself has nothing more. `NO_COLOR` and `CLICOLOR` are honoured.                                                                                                                   | Nothing specific.                                                                                                                           | Nothing specific. fzf is a full-screen list.                                                                                                                                                                |
| Testability                                      | A model is `Update(msg) → (model, cmd)` and `View() → string`: a flow test sends key and click messages and reads the view, with no terminal or process. `teatest` adds golden files but is **experimental** (`github.com/charmbracelet/x/exp/teatest/v2`, untagged, not moved to `charm.land`), so we would not depend on it.                   | `tcell.SimulationScreen` on tcell v2. **tcell v3 removed it**, and its replacement is not public API, so tests would pin tview to tcell v2. | Today's: a fake `execx.Runner` answering the argv, and bash suites with fake `fzf`/`gum` on `PATH`.                                                                                                         |
| Binary size                                      | +3.4 MB per binary, measured (section 2.3).                                                                                                                                                                                                                                                                                                      | Not measured; a smaller dependency tree.                                                                                                    | +0.                                                                                                                                                                                                         |
| Dependencies and supply chain                    | 26 modules with Huh, measured; fewer without it. One vendor (Charm) for most, plus well-known small libraries (`x/sys`, `uniseg`, `go-runewidth`).                                                                                                                                                                                               | tview v0.42.0 requires tcell/v2, go-colorful and uniseg, plus 5 indirect modules.                                                           | Two external programs installed by `play-claude-yolo.yml`. Their versions differ between the host and the ccy image, and every option we use must exist in the oldest one.                                  |
| Fit with `project_seam_*`                        | `internal/ui` stays the owner. The owned thing changes from programs (`commandOwners`: gum, fzf) to imports: a new rule, *only `internal/ui` imports `charm.land/…`*, the same shape as the program rule.                                                                                                                                        | The same, for `github.com/rivo/tview`.                                                                                                      | Unchanged.                                                                                                                                                                                                  |

### 3.3 Reading of the comparison

- **fzf and gum cannot give the owner what he asked for.** The complaint is about place and
  continuity: "you get into a page and it's not keeping the context of what you're actually
  doing". Each fzf or gum call is a separate process that owns the screen while it runs and
  leaves it when it exits. A breadcrumb can be painted on each box, which helps (section 5.1),
  but the screen still blinks between boxes, a gum prompt cannot keep a frame, and a wizard
  cannot show its earlier answers *and* its current field in one view.

- **Charm over tview.** Both can draw a persistent frame. Charm wins on:

  - looks (the owner's "nicer default standard stuff");
  - testability: a pure model, against tview's simulation screen, which tcell v3 removed;
  - momentum: v2 is fresh, and tview has one tag in years;
  - continuity: gum *is* Charm, so the colours, input feel and confirm buttons the tools already
    have carry over.

  tview's one advantage is native mouse widgets, and that is cheap to add in a frame we lay out.

- **Huh is optional.** Its value is validated fields and accessible mode. Its group-per-page
  model has no step bar and owns its own layout. Recommended: use Bubbles (`list`,
  `textinput`, `help`, `key`, `spinner`, `viewport`) inside our own frame, and adopt Huh only
  for its accessible fallback if a test with a screen reader shows the need. Leaving Huh out
  also trims the vendor tree (it brings catppuccin, hashstructure and go-humanize).

## 4. The design for ccy-teams and ccy-sessions

### 4.1 The screen frame

Every screen, in every tool, is the same four bands. Only the body changes.

```text
╭─ ccy-teams ─────────────────────────────────────────────────────────────────╮
│ Teams › alpha › Add an LXC member                              step 3 of 4  │  1 breadcrumb (where am I) + progress
│ Team alpha · 2 seats up · coordinator: proj.1                               │  2 context line (what am I acting on)
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   body: a list, a table, a form step or a message                           │  3 body
│                                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│ [ Enter Choose ]  [ Back  Esc ]  [ Help  ? ]                                │  4 footer: visible actions
╰─────────────────────────────────────────────────────────────────────────────╯
```

1. **Breadcrumb.** The tool's name is on the frame, then the path: `Teams › alpha › Add an LXC member`. Each push onto the screen stack adds a crumb, and Back pops one. A wizard adds
   `step N of M` at the right.
2. **Context line.** One line saying what the screen is acting on: the team and its state, or
   the session and where it is open. It is never a key legend.
3. **Body.** The list or table, with a column heading that stays put while the rows scroll
   (Bubbles `table` / `list`, `viewport` for help text).
4. **Footer of actions.** Every action for the current screen (or the selected row) as a
   labelled button, its key beside it, clickable with the mouse. The footer *is* the
   `ui.Action` list (section 5.3): a Label with an optional Key. The rule of
   InteractiveScripts.md rule 14 holds by construction: an action that has a key is drawn,
   because the key and the drawing come from one value.

Colour, in one place (`internal/ui/theme`): the accent (index 3, as now) for the cursor,
the active crumb and the focused button; green, yellow and red for state words, as the board
already uses (`board/table.go`); dim for hints. Terminal palette indices, not RGB, so the
terminal's theme still decides the shades, which is today's choice and a good one. Under
`NO_COLOR`: no colour, the same layout.

### 4.2 Wizards

- **What is answered stays visible.** The body of a wizard step shows the answered steps
  above the current one, each a `✓ Label  value` line. A step can be revisited: Back on a step
  returns to the previous one with its answer filled in.
- **A step bar** in the breadcrumb row (`step 3 of 4`). Optionally a dotted bar
  `● ● ◉ ○ Container · User · Checkout · Role`, which also names what is still to come.
- **Validation in place.** A rejected input shows the reason in red under the field, and the
  field keeps what was typed. That is the InteractiveScripts.md "strict validation, friendly
  recovery" rule, without the three-tries count printing new lines.
- **The summary is the last step**, in the same frame: a review list and two buttons.

### 4.3 Confirmations: one component

One `Confirm` screen replaces gum confirm, fzf's `Yes, …`/`Exit` rows and the yes/no gum
choose:

- the question as the body title, then the consequences as short lines ("sudo asks for your
  password", "its token stays valid");
- buttons named for what they do, never Yes/No: `[ Add worker@container-a ]  [ Cancel ]`;
- **the safe button is focused** for anything destructive (take down, end, remove), as
  `CursorLast` does today. The action button is focused for a step the person came to take;
- **a choice between more than two outcomes is a list of labelled options, not buttons.**
  Each option carries its consequence on a second, dim line. The coordinator choice is the
  case in point (below).

**The coordinator choice.** Today lxc-add asks nothing when the team already has a
coordinator: it prints "Role: worker. alpha already has a coordinator" and the step count
drops from 4 to 3. When the team has none, it asks `worker`/`coordinator`. The running-sessions
view offers a hand-over. Proposed: the Role step is **always step 4 of 4**, with both paths
shown.

```text
│ Role in alpha                                                               │
│                                                                             │
│ ▶ Worker                                                                    │
│     Does the work it is given.                                              │
│   Coordinator                                                               │
│     Takes the humans' requests and dispatches them. alpha's coordinator is  │
│     proj.1 now; it would become a worker.                                   │
```

Whether lxc-add may take the role from a sitting coordinator is behaviour, not styling. It is
owner decision D6.

### 4.4 Errors

- **A recoverable mistake** (bad input, a stale row): a red `✗` line inside the frame, under
  the context line or the field. Nothing scrolls.

- **A failed operation:** a result screen in the frame. The breadcrumb shows where it
  happened, then:

  - `✗ What failed` in red;
  - the cause, as the tool prints it today;
  - `What to do`, the exact next step, as today;
  - buttons `[ Back to the list ]` and, when it makes sense, `[ Try again ]`.

  This replaces `ERROR: …` plus "Press Enter to go back to the list." printed below an old box.

- **Unchanged outside the TUI:** `--list`, the verbs run from a script and a missing terminal
  keep `ERROR:` on stderr and the exit codes of `term.ExitCode`
  ([StderrHygiene.md](../../StderrHygiene.md)).

- **Long output from a play** (apply, take down): today it streams to the terminal under a gum
  spinner. In the TUI it streams into a `viewport` in the body, with a spinner in the context
  line. The full log stays scrollable, and the result screen follows.

### 4.5 Screen 1: the teams list

**Before** (fzf, `internal/teams/picker/picker.go`, header from `picker.go` and `menu.go`):

```text
  ╭─ Agent teams ───────────────────────────────────────────────────────────────╮
  │                                                                             │
  │  ↑↓ choose   Enter lists what can be done to the row   Tab running sessio…  │
  │  This row: Ctrl-E open in Element   Ctrl-N start a session   take down vi…  │
  │  Ctrl-N starts a new ccy session in the row's team, in ~/work/project       │
  │  RESTORE: whether the seat's session is started again after a reboot; ses…  │
  │  TEAM                      MEMBER                                KIND    S… │
  │  filter >                                                                   │
  │▶ alpha                     Builds and reviews the example site   team    u… │
  │    proj.1 (coordinator)    1 (coordinator)                       ccy     a… │
  │    owner                   owner                                 human   *… │
  │  Exit                                                                       │
  ╰─────────────────────────────────────────────────────────────────────────────╯
```

The column heading sits inside the header text, so a narrow terminal cuts it off. The key
legend and an explanation of one column take four lines. Tab is a key with no row.

**After:**

```text
╭─ ccy-teams ─────────────────────────────────────────────────────────────────╮
│ Teams                                                                       │
│ 2 teams · 5 members · restore on · in ~/work/project                        │
├─────────────────────────────────────────────────────────────────────────────┤
│ [ Teams ]  Running sessions                                                 │  tabs: a visible control, also Tab
│                                                                             │
│   TEAM / MEMBER             KIND    STATE        SESSION        WHERE       │
│ ▶ alpha                     team    ● up         2 running      port 8448   │
│     Builds and reviews the example site                                     │
│     proj.1  coordinator     ccy     ● active     proj-1         ~/work/proj │
│     owner                   human   ● signed in  1 session      Element     │
│   beta                      team    ◐ out of date                           │
├─────────────────────────────────────────────────────────────────────────────┤
│ alpha: [ Open in Element  ^E ]  [ New session  ^N ]  [ More…  Enter ]        │
│        [ Help  ? ]  [ Quit  q ]                                             │
╰─────────────────────────────────────────────────────────────────────────────╯
```

- The column heading stays above the rows while they scroll, and is never cut.
- The footer is the selected row's actions, the same set as the Enter menu, with the commonest
  first. "More…" opens the full menu as a screen (breadcrumb `Teams › alpha`), not a new box.
- "Running sessions" is a visible tab, so Tab is an accelerator for a control on screen.
- The RESTORE explanation moves to Help, and the "This row" line to the footer.
- A `filter` field appears when the person types (Bubbles `list` filtering), instead of
  taking a line all the time.

### 4.6 Screen 2: the add-LXC-member wizard

**Before** (gum, `internal/teams/lxcadd/adder.go`): printed below the last fzf box, one prompt
at a time.

```text
1/4  Container
     The LXC container whose agent joins alpha. It must already exist and be
     running; this never makes one.
Container
> container-a
  container-b
     ✓ container-a

2/4  User
     The login user the agent runs as in the container, which must already
     exist. It has: agent, admin.
User in container-a
> agent
     ✓ agent

3/4  Checkout
     Where the agent's repository is checked out, inside the container: its
     top directory, an absolute path. The handle's repo, host and seat come
     from it as ccy derives them: …  On GitHub, in its home: /srv/agent/one (owner/repo).
Checkout path in container-a
> /srv/agent/one█
```

Step 4 is either a gum choose or the line "Role: worker. alpha already has a coordinator
(proj.1); a team has one.", so the count changes between 3 and 4. Then come a gum box and a gum
confirm.

**After:**

```text
╭─ ccy-teams ─────────────────────────────────────────────────────────────────╮
│ Teams › alpha › Add an LXC member                              step 3 of 4  │
│ ● Container  ● User  ◉ Checkout  ○ Role                                     │
├─────────────────────────────────────────────────────────────────────────────┤
│ ✓ Container   container-a                                                   │
│ ✓ User        agent                                                         │
│                                                                             │
│ Checkout path in container-a                                                │
│ ┃ /srv/agent/one█                                                           │
│   Found in agent's home: /srv/agent/one (owner/repo) · /srv/agent/two       │
│   The top directory of the agent's repository, an absolute path.            │
│   Its handle will be repo.N, the next free seat.                            │
│                                                                             │
├─────────────────────────────────────────────────────────────────────────────┤
│ [ Next  Enter ]  [ Back  Esc ]  [ Cancel  ^C ]                              │
╰─────────────────────────────────────────────────────────────────────────────╯
```

Then step 4 of 4 is the Role list from section 4.3. The last step is the review:

```text
│ Teams › alpha › Add an LXC member                                   review  │
│ Nothing is changed until you choose Add.                                    │
├─────────────────────────────────────────────────────────────────────────────┤
│ agent@container-a joins alpha                                               │
│   Container         container-a                                             │
│   User              agent                                                   │
│   Checkout          /srv/agent/one                                          │
│   Handle            one.2 (expected; the play makes the final choice)       │
│   Role              worker                                                  │
│ Saved to localhost.yml, then alpha is applied: sudo asks for your password. │
├─────────────────────────────────────────────────────────────────────────────┤
│ [ Add agent@container-a ]  [ Back ]  [ Cancel ]                             │
```

A wizard needing sudo first asks for it in the frame, with the reason ("LXC here is
rootful, so …") as today. The password prompt itself is sudo's own on the terminal, so the TUI
suspends around it: Bubble Tea releases the terminal to a child process and takes it back. The
same mechanism covers attaching a terminal to a tmux session and opening Element. Bubble Tea
v2 offers this two ways (`exec.go`): `tea.ExecProcess(*exec.Cmd, …)`, which would bypass the
`project_seam_exec` rule, and `tea.Exec(ExecCommand, …)`, which takes an interface. The
second is the one to use: `execx` provides the `ExecCommand`, so every process still starts
through the one seam and its fake.

### 4.7 Screen 3: the session menu (`ccy-sessions`)

**Before** (fzf, `internal/sessions/picker.go`):

```text
  ╭─ CCY sessions ──────────────────────────────────────────────────────────────╮
  │                                                                             │
  │  ↑↓ choose   Enter attach   Ctrl-T take over   Ctrl-X end   Ctrl-N new   E… │
  │  Ctrl-N starts a new ccy session in ~/work/project                          │
  │  "open elsewhere" is attached in another terminal: Ctrl-T detaches that t…  │
  │  CPU: since the list last redrew ("-" until it has), 100% is one core       │
  │  network, token, SSH key: what its container is on and was started with …   │
  │  filter >                                                                   │
  │▶ ccy-proj-1                    detached          3%  ccy-net-proj      to…  │
  │  ccy-proj-2                    open elsewhere    0%  ccy-net-proj      to…  │
  │  Exit                                                                       │
  ╰─────────────────────────────────────────────────────────────────────────────╯
```

The rows have no column heading (only `--list` prints one). Take over, end and new are keys
and nothing else. Enter on an "open elsewhere" row refuses and names Ctrl-T.

**After:**

```text
╭─ ccy-sessions ──────────────────────────────────────────────────────────────╮
│ Sessions                                                                    │
│ 2 running · new sessions start in ~/work/project                            │
├─────────────────────────────────────────────────────────────────────────────┤
│   SESSION         STATE             CPU  NETWORK        TOKEN     DIRECTORY │
│ ▶ ccy-proj-1      ○ detached         3%  ccy-net-proj   team      ~/work/p… │
│   ccy-proj-2      ● open elsewhere   0%  ccy-net-proj   default   ~/work/p… │
│                     on /dev/pts/3, idle 3 h                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│ ccy-proj-1: [ Attach  Enter ]  [ End…  ^X ]  [ New session here  ^N ]        │
│             [ Help  ? ]  [ Quit  q ]                                        │
╰─────────────────────────────────────────────────────────────────────────────╯
```

On an "open elsewhere" row the first button becomes `[ Take over  Enter ]`, and its
consequence line ("that terminal is detached, not closed") shows under the row. Enter does
what it says on that row instead of refusing. End opens the one Confirm screen
(`Sessions › ccy-proj-1 › End`, with the button `[ End ccy-proj-1 ]` and `[ Cancel ]`
focused). The CPU and network explanations move to Help. Nothing is reachable only by a key.

ccy-teams' Running sessions view (section 4.5's second tab) uses the same component and
footer. Its Add to a team, Remove, Move and Role questions become screens under
`Teams › Running sessions › <name>`.

## 5. What now, what later, and how to migrate

### 5.1 Low-hanging fruit inside fzf and gum, with no new dependency

These need only fzf and gum features that the host's Fedora fzf (0.70 or later) already has.
Each needs a check that the same binary never runs in the ccy image's fzf 0.38: `ccy-sessions`
and `ccy-teams` run on the host.

| Change                                                                                                                                                                                                                          | Where                            | fzf/gum needed                              |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------- | ------------------------------------------- |
| **A breadcrumb in every title**: `PickSpec.Crumbs []string`, rendered `ccy-teams › alpha › Add an LXC member` in `--border-label`. The path is threaded through `flow.Kit`, so every screen a flow opens extends it.            | `ui.PickSpec`, `teams/flow`      | `--border-label` (0.35)                     |
| **Header, list and footer as separate bands**: context in `--header` with `--header-border`; the column heading as a fixed line (`--header-lines` from the first row, so it lines up with the rows); the actions in `--footer`. | `ui.Pick`                        | `--header-border` (0.58), `--footer` (0.63) |
| **Clickable actions**: `click-footer` maps a click to the action under the pointer.                                                                                                                                             | `ui.Pick`                        | `click-footer` (0.65)                       |
| **Wizard steps keep earlier answers**: clear the screen before each step, then print the breadcrumb, the step bar and the `✓` lines answered so far, then the gum prompt.                                                       | `term.Step`, `lxcadd`, `create`  | none                                        |
| **One confirmation**: every yes/no through `Picker.Confirm` (rows named for the action, safe default), never gum confirm.                                                                                                       | `ui`, all flows                  | none                                        |
| **Column heading in ccy-sessions**: the `--list` heading shown in the picker too.                                                                                                                                               | `sessions/table.go`              | `--header-lines`                            |
| **No key-only actions**: Ctrl-T, Ctrl-X and Ctrl-N become row-menu lines in ccy-sessions, and Tab a row in ccy-teams. This is the `ui.Action` work already under way on `dbf/hidden-bindings`.                                  | `ui`, `sessions`, `teams/picker` | none                                        |
| **Trim the headers**: column explanations (RESTORE, CPU, network) move to `?` help.                                                                                                                                             | `teams/picker`, `sessions`       | none                                        |

What fzf and gum cannot do, which needs the library:

- one frame that persists across screens (no blink, no scroll of old boxes);
- a wizard showing its answers and its current field in one stable view, with Back to a
  filled-in earlier step;
- validation shown in place rather than as new lines;
- play output streamed inside the frame;
- tabs.

### 5.2 Migration: per screen, behind one API

**The seam first.** The flows do not call fzf or gum shapes today so much as `ui.PickSpec`
with keys, rows and header strings. The first step, useful whatever the library decision is,
gives `internal/ui` a semantic API:

- `ui.Screen{Crumbs, Context, Columns, Rows, Actions []ui.Action, RowActions}`;
- `ui.Wizard{Steps}`, `ui.Confirm{Question, Consequences, Do ui.Action, Safe bool}`;
- `ui.Result{Failed, Cause, Next}`.

The flows build these values. `internal/ui` renders them: with fzf and gum now (section 5.1),
with Bubble Tea after D1. The flow tests then script answers at this level ("choose the row
tagged 3", "action `end`"), not as fzf argv. That is better tests in any case, and it is what
makes the library swap invisible to the flows.

**Then per tool, not per screen.** A per-screen swap (one `tea.Program` per screen in place
of each fzf call) is possible and low-risk. But it keeps the blink between screens, which is
the owner's complaint. The persistent frame needs one program per tool run, with a screen
stack. So:

1. **`ccy-sessions` first.** It has one screen, a confirm and an exec, which makes it the
   smallest complete proof of the frame, the footer, mouse and `tea.Exec` for attach.
   The owner looks at it before the rest goes on (Plan 00191's "with the owner looking at it").
2. **`ccy-teams`**, flow by flow under one program: the list, the row menu, the Running
   sessions tab, lxc-add, create, login and take down. Until a flow moves, the program
   suspends (`tea.Exec`) and the old gum calls run as they do now, so the tool is never
   half-broken.
3. **The freeze tools last**, after their golden replay is replaced by expectation tests, as
   `src-go/CLAUDE.md` allows ("replace it with expectation tests rather than recording
   again"), with SPEC-freeze D2 closed by the owner.

`ftp-camera`'s launcher and any other `internal/ui` user follow the same API.

**Tests through the migration:**

- *Go flow tests* move from `teamtest.Script` (fzf `key|choice` and gum answer queues) to a
  scripted `ui` fake at the semantic level. Label and row assertions stay. Argv assertions
  (`start:last`, `--bind=q:abort`, `--ansi`) go, because they test fzf, not the flow.
- *The renderer* gets its own tests: send `KeyPressMsg` and `MouseClickMsg` to the model and
  assert on `View()`: the crumbs, a focused button, the footer for a row, the step bar. No
  teatest dependency.
- *The bash suites* that stub `fzf` and `gum` on `PATH` (`scripts/test-ccy-teams-command.bash`,
  `scripts/test-ccy-sessions-take-over.bash`) cannot drive an in-process TUI. Their picker paths
  are replaced by Go expectation tests in the same commit as each flow moves (src-go/CLAUDE.md
  requires that). Their non-interactive paths (`--list`, verbs, exit codes) stay. No hidden test
  mode goes into the shipped binary.

### 5.3 How `project_hidden_bindings` and `ui.Action` carry over

The rule on `dbf/hidden-bindings` (`src-go/qa/rules/hiddenbindings.go`) flags:

- `--expect` outside `ui.expectArg`;
- an fzf `--bind` that binds a key to anything except navigation;
- a binding it cannot read;
- a `ui.Action` with a Key and no Label.

`ui.Action{ID, Label, Key}` is the right unit for both renderers, and it should land first, on
fzf, as that branch intends.

- **On fzf:** the Labels become menu lines and footer text, and the Keys become `--expect`,
  built in one function.
- **On Bubble Tea:** the Labels become footer buttons and menu items, and the Keys become
  `key.Binding`s, built in one function inside `internal/ui` (for example `bindingsOf([]Action)`).
  The model's `Update` matches a key only through those bindings.
- **The rule's Go half changes target, not purpose.** `key.NewBinding` and
  `key.WithKeys` may be called only in `internal/ui`'s binding builder, the way `--expect` may
  only be built in `expectArg`. A `tea.KeyPressMsg` compared with a literal key string outside
  `internal/ui` is a finding, and `ui.Action` with a Key and no Label stays a finding.
  Navigation keys (arrows, Enter, Esc, Tab between tabs) are allowed through a fixed table, as
  `qa/internal/fzfbind` allows them today.
- **`project_seam_commands`** gains an import twin: only `internal/ui` imports `charm.land/…`
  (or `github.com/rivo/tview`). Once gum and fzf are gone from a tool, its `commandOwners`
  rows for them are removed.

Under the frame of section 4.1 the rule is close to tautological, which is the point: the
footer is drawn from the same `[]ui.Action` the bindings are built from. A key with no drawn
control cannot be written without going around `internal/ui`, and that is what the rule catches.
