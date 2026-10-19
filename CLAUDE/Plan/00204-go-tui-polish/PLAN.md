# Plan 00204: go tui polish

**Status**: In Progress (D1 and D2 decided; Task 2.1, the dependency infrastructure, built; D3-D8 open)
**Created**: 2026-10-16
**Owner**: joseph
**Priority**: Medium

## Overview

The owner, on the Go tools' screens:

> It's not actually clear from a user point of view what level you're in. You get into a page
> and it's not keeping the context of what you're actually doing, so there should be some
> kind of titles or something. Where's the design come from? I think there was a weird
> tangent that you took earlier where you tried to make the Go stuff look like the bash
> stuff, but the bash stuff always looked pretty crap. I am wondering if the Go TUI library
> that we're using can actually have some kind of nicer default standard stuff. There was no
> value in preserving how it looks before particularly; we just need a really nice clean TUI.
> I've got a feeling that we don't have that, and we're missing some probably quite easy
> low-hanging fruit to make this whole thing feel more real and fully polished.

And, binding: "TUI must be UX controls not magic keys. Keys are OK but MUST NOT BE THE ONLY
CONTROL SURFACE" (InteractiveScripts.md rule 14, on branch `dbf/hidden-bindings`).

The Go tools have no TUI library. `src-go/internal/ui` shells out to fzf and gum, one process
per screen. The look is the bash look on purpose. Plan 00191 kept "the same prompts and the
gum/fzf widgets so the existing suite is the oracle", and deferred "a Bubble Tea UI" to "a
later phase, with the owner looking at it". The freeze tools' golden replay pinned the legacy
fzf flags as well. Nobody chose the look as a design. This plan is that later phase. It is a
proposal: the research is in [RESEARCH.md](RESEARCH.md), and nothing is built until the owner
decides.

The hard constraint was the rule that `src-go/go.mod` stays dependency-free, "because the
shipped binaries are built on the host with plain `go build`". A vendored dependency keeps
that true: the build stays plain, offline and reproducible. The cost is one flag in
`tasks/build-go-command.yml` and a vendor tree in the repository (RESEARCH §2). The owner
allowed it (D2, below).

## Goals

- Every screen shows where the person is (a breadcrumb), what it acts on (a context line) and
  what they can do (a footer of labelled actions, each with its key beside it, clickable).
- A wizard keeps its answered steps on screen, shows `step N of M`, and lets the person go
  back to a filled-in step.
- One confirmation component, one error display and one colour theme across `ccy-teams`,
  `ccy-sessions`, the freeze tools and every other `internal/ui` user.
- No action reachable only by a key. A key is drawn because its action is.
- The library, if adopted, stays behind `internal/ui`. The flows build semantic screens and
  are tested without a terminal, as now.

## Non-Goals

- Changing what the tools do. The exception is the coordinator step in lxc-add, which is
  decision D6.
- A GUI. The Claude Code mod surface is Plan 00202.
- Porting the remaining bash tools: that is Plan 00192.

## Options

Full judgement: [RESEARCH §3](RESEARCH.md#3-the-options).

| Option                                                              | In one line                                                                                                                                                                                                                                                 |
| ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **A. Charm: Bubble Tea v2.1.0 + Bubbles v2.2.1 + Lip Gloss v2.0.6** | One program per tool run, with a persistent frame and a screen stack; the best default look; pure-model tests; mouse; the engine gum already runs on. Costs a vendored dependency (26 modules and +3.4 MB per binary, measured with Huh; fewer without it). |
| B. tview v0.42.0 on tcell                                           | Can draw a persistent frame, with native mouse widgets. Boxy look; one tag in years; its test screen is gone in tcell v3.                                                                                                                                   |
| C. fzf and gum, framed better                                       | No dependency. Breadcrumbs on every box, separate header, list and footer bands (fzf 0.58 and 0.63), and clickable footers. Still one process per screen: the blink between screens, the scroll of old boxes, and wizards that lose their answers stay.     |
| Not credible                                                        | vaxis (pre-v1), pterm (prompts, not a TUI), gocui, termui and go-prompt (dormant).                                                                                                                                                                          |

## Recommendation

**Option A, Bubble Tea v2 with Bubbles and Lip Gloss, vendored. Huh is left out for now.
Before the library lands, the semantic `internal/ui` API and `ui.Action` go in on fzf.**

Confidence:

- **High** that option C cannot meet the owner's complaint about context. That complaint is
  about continuity between screens, which separate processes cannot give.
- **Medium-high** on Charm over tview. Charm wins on looks, testability and momentum, and the
  tools already wear gum's style. tview's edge (native mouse) is cheap to add to a frame we
  lay out ourselves.

The design is in [RESEARCH §4](RESEARCH.md#4-the-design-for-ccy-teams-and-ccy-sessions):

- the four-band frame;
- wizards that keep their answers;
- one Confirm;
- errors in the frame;
- before and after sketches of the teams list, the add-LXC-member wizard and the
  `ccy-sessions` menu.

## Decisions

The owner, on D1 and D2 (speech to text, verbatim):

> vendor dependencies are fine. I'm coming from PHP where we'd have a composer.json file and
> then we'd have all of our dependencies very clearly laid out. What is the Go equivalent? I
> don't like the idea of having to install stuff at a system level, I think that's ugly, and
> you end up into dependency problems. so just go with Go best practice and bring in
> dependencies by all means as long as they're high quality and we're being sensible and
> secure and we're pinning them as well.

- **D2 is approved: vendored, pinned dependencies.** Built in Task 2.1.
- **D1: the recommended stack (Bubble Tea v2, Bubbles, Lip Gloss) is accepted** as "go with
  Go best practice". The owner may reverse it; nothing outside `internal/ui/frame` uses it yet.
- **D3-D8 stay open.**

**The Go equivalent of Composer**, for the owner:

| PHP                                      | Go, in `src-go/`                                                                                                                                                    |
| ---------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `composer.json` (what we depend on)      | `go.mod`: each module and its exact version                                                                                                                         |
| `composer.lock` (exactly what was taken) | `go.sum`: a checksum of every module's content, checked on every download against Go's public checksum database                                                     |
| a committed `vendor/`                    | `vendor/`, written by `go mod vendor` and committed: the dependencies' source is in the repository, and every bump is a reviewable diff                             |
| an allowlist you would review by hand    | `go-deps.allowlist`: every module with the reason it is here; a gate fails on any module not listed, an unpinned (untagged) version or a `replace` without a reason |

Nothing is installed system-wide. The host builds from `vendor/` with no network, so every
binary carries exactly its pinned versions. The scanner, `govulncheck`, is itself pinned in
the QA module's `go.mod` and runs as `go tool govulncheck`.

### The decisions as proposed

| #   | Decision                                                                                                              | Recommendation                                                                                                                                                                                                                                                                                                                                                                                                       |
| --- | --------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| D1  | **Adopt a TUI library?** A Charm, B tview, C stay on fzf and gum.                                                     | A.                                                                                                                                                                                                                                                                                                                                                                                                                   |
| D2  | **Allow dependencies in `src-go/go.mod`, and how?** Vendored and committed; `go.sum` and the proxy; or keep the rule. | **Vendored and committed.** The build stays a plain `go build`, now with `GOFLAGS=-mod=vendor`, offline, and every compiled byte is reviewable in the repository. A new gate fails when `vendor/` is out of step with `go.mod`, or when a module outside an allowlist is required. `govulncheck` runs over the module. The rule's wording changes from "dependency-free" to "dependencies vendored and allowlisted". |
| D3  | Huh (forms, accessible mode)?                                                                                         | Not now. Use Bubbles inside our own frame; Huh has no step bar and lays itself out. Revisit if a screen-reader user needs its accessible mode.                                                                                                                                                                                                                                                                       |
| D4  | Do the fzf and gum low-hanging fruit now, before D1 is built?                                                         | Only the parts that survive a library swap: the semantic `ui` API, `ui.Action` (the `dbf/hidden-bindings` work), breadcrumbs in titles, one Confirm, no key-only actions, trimmed headers. If D1 is C, do all of RESEARCH §5.1.                                                                                                                                                                                      |
| D5  | Migration order.                                                                                                      | `ccy-sessions` first (one screen), and the owner looks at it. Then `ccy-teams`, flow by flow under one program. The freeze tools last, after their golden replay becomes expectation tests.                                                                                                                                                                                                                          |
| D6  | lxc-add's Role step when the team already has a coordinator.                                                          | Always show it as step 4 of 4, with Coordinator offered and its consequence spelled out ("proj.1 would become a worker"), as the Running sessions view already offers a hand-over. Today lxc-add silently forces worker, and the step count changes.                                                                                                                                                                 |
| D7  | Mouse on by default?                                                                                                  | On: clicks on rows, tabs and footer buttons. Capturing the mouse means text selection needs Shift-drag in most terminals; the Help screen says so.                                                                                                                                                                                                                                                                   |
| D8  | The freeze tools' house style (SPEC-freeze D2, never decided by the owner).                                           | Close it in favour of the shared frame, at their turn in D5.                                                                                                                                                                                                                                                                                                                                                         |

## Tasks

### Phase 0: Research and decisions

- [x] ✅ **Task 0.1**: Research: the current UI and its history, the dependency constraint
  and the cost of vendoring (measured), the options, the screen design with sketches, the
  quick wins and the migration. [RESEARCH.md](RESEARCH.md).
- [ ] 🔄 **Task 0.2**: The owner decides D1-D8. D1 and D2 decided (above); D3-D8 open.

### Phase 1: The seam, on fzf and gum (D4)

- [x] ✅ **Task 1.0**: The context heading, ahead of D1 because the owner asked for it twice
  ("adding a container to multiple teams it was not clear which team im adding to").
  `ui.Heading` (crumbs, a wizard's step, a context line with the answers so far) is a
  required argument of every `ui` question and pick, drawn the same way on fzf's `--header`
  and gum's header or prompt, and refused when it names no place. Threaded through
  `ccy-teams`, `ccy --teams` (`New session › Team 2 of 3: <team>`), `ccy-sessions`, the freeze
  tools and `ftp-camera`. InteractiveScripts.md rule 15; owner steps in
  [ACCEPTANCE.md](ACCEPTANCE.md); report `subagent-reports/261016-context-headings-opus.md`.
  Branch `fix/context-headings`, from `dbf/hidden-bindings`. A heading wider than the
  terminal (less the picker's frame) is wrapped at its `›` and `·` separators, never
  truncated (Fable's review, B1).

- [ ] ⬜ **Task 1.0a**: Head the bash pickers too. `lib/tmux-session.bash`'s screens still have
  no heading (Fable's review of Task 1.0, N2); give them the same trail, context line and rule,
  or move them onto the Go `ui` when that library is ported.

- [x] ✅ **Task 1.1**: `ui.Action{ID, Label, Key}` lands with `project_hidden_bindings`
  (branch `dbf/hidden-bindings`). `ccy-sessions`' Ctrl-T, Ctrl-X and Ctrl-N, and `ccy-teams`'
  Tab, become visible choices.

- [ ] ⬜ **Task 1.2**: The semantic API in `internal/ui` (`Screen`, `Wizard`, `Confirm`,
  `Result`, with `Crumbs`), rendered on fzf and gum. The flows move onto it, and their tests
  script answers at that level instead of fzf argv.

- [ ] ⬜ **Task 1.3**: Breadcrumbs in every title, one Confirm everywhere, and column
  explanations moved to help.

### Phase 2: The dependency route (D2 approved)

- [x] ✅ **Task 2.1**: The dependency infrastructure, on branch `feat/go-vendored-deps`:
  - `src-go/go.mod` requires Bubble Tea v2.1.0, Bubbles v2.2.1 and Lip Gloss v2.0.6 (18
    modules in all), with `go.sum` and a committed `vendor/`; `internal/ui/frame` (a Lip
    Gloss breadcrumb title and a minimal Bubble Tea model, unit tested) makes the tree real.
  - The gates `qa-go-deps` (allowlist `src-go/go-deps.allowlist`, pinning, `go mod verify`,
    tidy, `vendor/` in sync) and `qa-go-vuln` (govulncheck v1.9.0, pinned by the `tool`
    directive of `src-go/qa/go.mod`), test first, in `qa-gates.tsv`, `qa-routing.tsv` and
    `qa-changed`: [SPEC-qa-go-deps.md](SPEC-qa-go-deps.md).
  - `-mod=vendor` in `tasks/build-go-command.yml` for in-repository builds (md-viewer keeps
    `-mod=readonly`); `src-go/vendor` in `qa-trees.tsv` and the bash-limits exemptions; the
    `toolchain go1.26.9` line CI's `setup-go` reads.
  - The rule's new wording in GoProjectRules.md, src-go/CLAUDE.md and QA.md.
- [ ] ⬜ **Task 2.2**: The `project_seam_imports` rule (only `internal/ui` imports the TUI
  library), and `project_hidden_bindings` extended to `key.Binding` (RESEARCH §5.3).
- [x] ✅ **Task 2.3** (red `727810c8`, green the next commit; the rule checks that every binding
  has constant help, and ui's tests hold that ui draws it; Tab in a text input is navigation,
  the owner's decision, recorded in the catalogue): Before the first Bubble Tea key handler lands (it must precede Task
  3.1), widen `project_hidden_bindings` to Bubble Tea (Fable's N4 on `dbf/hidden-bindings`):
  every `key.Binding` has help text and is rendered by the one help or menu component in
  `internal/ui`, and a `tea.KeyMsg` switch outside that component is a finding. Red first on a
  fixture with a key-only `KeyMsg` case. In the same commit, the catalogue sentence in
  GoProjectRules.md that excuses Go key reads ("no first-party Go reads raw keys; there is no
  terminal library in `src-go`") changes: it stops being true when Bubble Tea is a dependency.
  The reviewers' evasions (a key read in a variable or a switch init, `strings` searches on one,
  a `key.Binding{}` literal, Tab by hand with no Tab binding) were closed red `f66868db`, green
  the next commit; what is still not caught is listed in the catalogue.
- [ ] ⬜ **Task 2.4**: The next wider Bubble Tea rule (named in GoProjectRules.md): every
  `key.Binding` a ui model matches in `Update` is one its `View` hands the help view, so the
  Tab exception is tied to the hint on the same screen, not to the package. DBF, red first on a
  model that matches a binding it never draws.

### Phase 3: The frame, `ccy-sessions` first (D1, D5)

- [ ] ⬜ **Task 3.1**: The Bubble Tea renderer of the semantic API: frame, theme, footer
  actions, mouse, Confirm, Result, and suspension through `tea.Exec` over `execx`. Model
  tests on `View()`.
- [ ] ⬜ **Task 3.2**: `ccy-sessions` on it. The bash take-over suite's picker paths become
  Go expectation tests. Then the owner looks at it on the host, through meta-deploy.

### Phase 4: `ccy-teams`, then the freeze tools

- [ ] ⬜ **Task 4.1**: `ccy-teams` under one program: the list and tabs, the row menu, then
  each flow (lxc-add, create, login, take down, the Running sessions questions).
- [ ] ⬜ **Task 4.2**: The freeze tools: the golden replay is replaced by expectation tests,
  then the frame (D8).
- [ ] ⬜ **Task 4.3**: `qa-reviewer` over the branch.

## Dependencies

- Plan 00191 deferred the Bubble Tea UI to this plan. Its Non-Goal is closed by this plan once
  D1 is decided.
- Plan 00177 (ccy-teams, human friendly) built the current wizard steps and the row menu
  (Phase 6). Its open host tests stand on today's look; this plan redraws the same screens.
- Branch `dbf/hidden-bindings` (`ui.Action`, `project_hidden_bindings`, InteractiveScripts.md
  rule 14) is Task 1.1 and should merge first.

## Success Criteria

- [ ] The owner has decided D1-D8.
- [ ] Every screen of `ccy-sessions` and `ccy-teams` shows a breadcrumb, a context line and a
  footer of labelled actions. No action is reachable only by a key (the rule passes on the
  real tree).
- [ ] The add-LXC-member wizard shows its answered steps and `step N of M` throughout.
- [ ] The host build stays a plain, offline `go build`: from `vendor/`, with the gates green
  (proven in the container with `GOFLAGS=-mod=vendor GOPROXY=off`; the host's first run is the
  next `play-claude-yolo.yml`).
- [ ] The owner, at a host terminal, calls it clean.

## Delivery & Milestones

- Research and proposal: the first commit on branch `research/go-tui`.
- The dependency infrastructure (Task 2.1): branch `feat/go-vendored-deps`.
