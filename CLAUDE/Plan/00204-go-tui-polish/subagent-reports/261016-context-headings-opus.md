# Context headings on every ui screen (Plan 00204 D4 quick fix)

Branch `fix/context-headings`, based on `origin/dbf/hidden-bindings` (it built), with that
branch's later commits merged in (the `ui.Action` / row-menu rework) and `origin/research/go-tui`
merged for this plan's folder. F44 is **not** merged: see "Overlap" below.

## What was built

- **`ui.Heading`** (`src-go/internal/ui/heading.go`): `Crumbs`, `Step`/`Steps`/`StepName`,
  `Context`. Built with `ui.Here(...)`, `.Push(...)`, `.AtStep(n, m, name)`, `.About(line)`.
  Drawn as the trail (`Teams › alpha › Add LXC member · step 3/4 Checkout`, crumbs bold in the
  accent), the context line, then a rule of `─`; plain when `Plain` is set (hosts set it from
  `term.ColourOn`, so NO_COLOR and pipes get no escapes).
- **Required in the API.** It is a positional argument of `Gum.Confirm`, `Gum.Input`,
  `Gum.Choose`, `Picker.Pick`, `Picker.Confirm` and `Picker.Choose`, so a call without one does
  not compile; a zero or empty-crumbed heading is refused with `ui.ErrNoHeading` before
  anything runs (`TestEveryScreenRefusesToDrawWithoutAHeading`). No `project_*` analyzer was
  added: the type-level argument plus the run-time refusal, which every flow test exercises,
  already holds the class; a rule against a literal `ui.Heading{}` would add little.
- **Rendering:** fzf: on top of `--header`, including every per-row header the cursor brings
  (`transform-header`) and the legacy `ChooseSpec` screen (plain there; it has no `--ansi`).
  gum: on top of `--header` (input, choose) and the prompt (confirm). A row's `ui.Menu` (from
  `dbf/hidden-bindings`) gained `Heading`; without one it is the list's heading plus the
  menu's title.
- **Frame title of `Picker.Confirm`** is now the heading's first crumb (it took a free title
  before).

## Screens covered (each asserted in a test)

| Tool                       | Screens                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| -------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ccy-teams                  | Teams list (`Teams`, team count, teams not up); row menus (`Teams › alpha`, `Teams › alpha › seat 1`); Running sessions list, session menu, Add to a team steps 1-3 (team named on the seat and role steps), Remove, Move (+ Coordinator role), Role                                                                                                                                                                                                                                    |
| ccy-teams flows            | lxc-add steps 1-4 + Review (`into team alpha · container … · user … · checkout …`); create steps + Review, Sign in, Open in Element, New session; take down (Sessions in it, step 1/2 Backup, step 2/2 Its data, typed name); restore (steps 1-2); seats New session, Remove, Park, Coordinator; Element password, Copy, Sign in, Element profile; shutdown; offers (create, folder limit, description, update, new session); `ccy-teams session add/remove/move/role/repair` questions |
| ccy --teams (choose-teams) | list (`New session › Teams`, the checkout, teams already marked); each team's Scope, Seat and Role questions headed `New session › Team 2 of 3: beta` (`Team alpha` for one); Element consent                                                                                                                                                                                                                                                                                           |
| ccy-sessions               | list (`Sessions`, count, where a new one starts); session menu; Start ccy, End, Take over                                                                                                                                                                                                                                                                                                                                                                                               |
| freeze tools               | group menu (`lxcfreeze`, group count); drill-down (`lxcfreeze › bridge: lxcbr0`, container count)                                                                                                                                                                                                                                                                                                                                                                                       |
| ftp-camera                 | launcher steps 1-3 (the old `Step N/3 —` header prefix dropped: the heading carries it)                                                                                                                                                                                                                                                                                                                                                                                                 |

Tests: `teamtest.Script.Headings` / `HeadingAt` record each screen's heading;
`GumLog`, `HeaderOf` and `teamtest.Unheaded` read the screen without it, so the existing
question assertions stand. The ccy-teams bash suite's gum stub logs the heading on its own line
(`heading …`) and gained three heading checks (372 pass); ccy-sessions take-over (82),
restore-askpass, reboot and tmux-targets suites pass. The freeze golden replay pins the new
headers as SPEC-freeze bug 27 (9 scenarios, calls stream only).

## Left out

- **The ccy launcher's own detached-session picker** (bash, `tmux-session.bash`): not `ui`.
- **Bubble Tea**: not touched, as asked (Plan 00204 phase 3).
- **Spin, Box, Explain, Style** take no heading: they ask nothing.
- **Plan 00203 ACCEPTANCE.md** row M5 (b) not edited (that file is the other branch's); the
  owner steps for headings are in this plan's `ACCEPTANCE.md` (H1-H7).
- `play-claude-yolo.yml` was already queued; its comment now names the headings. The freeze
  tools and ftp-camera are rebuilt only by their optional plays, which are not queued.

## Overlap with fix/coordinator-two-paths (now in F44)

F44 already carries that branch (`1d3c65bb`). It threads a **string** heading through the
launch flow: `chooser.SeatFunc(ctx, team, memberType, heading string)`, `heading(at, n, team)`
= `"Team 2 of 3: beta"`, and `choose.PickSeat(..., heading)` prefixing gum headers with
`heading + " · seat: …"` / `" · role of seat …"`, with a comment saying "a shared screen heading
in the ui layer would take this text". This branch has `SeatFunc(ctx, h ui.Heading, team, memberType)` and `flow.LaunchTeam(i, n, team)` with the same words. Merging F44 conflicts in
choose.go, chooser.go/\_test, consent.go, widen.go, lxcadd/adder.go, seats/role.go and
picker/{menu,offers,sessions}.go, plus unrelated CCY version, changelog and journal files; the
merge was tried and aborted, left to the coordinator. Resolution: keep their question wording
and coordinator logic, pass `flow.LaunchTeam(...)` as the `ui.Heading` instead of the string,
and drop the `heading +` prefixes from their gum headers (the heading now draws it).

## Pre-existing QA failures (not from this change)

The first `qa-changed` run (over the whole change, before `dbf/hidden-bindings`' DBF-green
commits were merged) failed only in `docs` (QA.md:114 anchor `#project_hidden_bindings`), `go` (the
nested `qa/cmd/qa-patterns` real-semgrep test) and `hidden-bindings` (10 findings in
`history-search.bash`, `tmux-session.bash`, `wsi-model-manager`), all on `dbf/hidden-bindings`
files untouched here. After that merge: the whole `src-go` module's tests, vet and
golangci-lint are green, the ccy-teams (372) and ccy-sessions take-over (82) suites pass, and
`qa-changed` passes on the last commit's files. `qa-all` was not run (by instruction).
