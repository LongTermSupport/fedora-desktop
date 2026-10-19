# Fable review: `fix/context-headings` at aa2e57fa (context heading on every ui screen)

Verdict: **FIX-FIRST**, one blocker (B1), then MERGE. Everything else holds.

## What was checked

- `git diff F44...aa2e57fa` and `git diff 7f8b1c23 aa2e57fa --stat` are identical (F44's tip is
  the merge base); `git merge-tree --write-tree F44 aa2e57fa` writes a tree with no conflict.
  Nothing from F44 was dropped: `coordinator.go` still words the two paths (`NoneChoice`,
  `TakeoverChoice`, `OfferTakeover`); `choose.go` keeps them; every picker action is a visible
  row (`qa-hidden-bindings.bash` exit 0).
- `qa-go.bash` exit 0; `qa-hidden-bindings.bash` exit 0; every `scripts/test-ccy-*.bash` with
  `PINGBUS_*` unset exits 0 (2625 PASS lines), except `test-ccy-ssh-probe.bash`, which fails on
  this container having no `~/.ssh/github_<alias>` key: environmental, nothing in this diff.
- Read: `ui/heading.go`, `ui/ui.go`, `ui/picker.go`, `ui/choose.go`, `ui/actions.go`,
  `heading_test.go`, and the diffs of every flow package.

## Attack results

1. **Coverage.** No `execx.Cmd{Name: "gum"|"fzf"}` outside `internal/ui` (the owned-programs
   analyzer fences it). No `ui.Heading{}` literal outside `ui`'s own tests. `Plain` is wired
   from `term.ColourOn` in all four hosts (`ftpcamera/args.go:206`, `sessions/app.go:275`,
   `teams/host/host.go:205,208`). "Does not compile without a Heading" is true only of the
   signature: a zero `Heading{}` compiles. The run-time refusal is the real gate, and it is
   tested for all six entry points with both `Heading{}` and an empty crumb
   (`TestEveryScreenRefusesToDrawWithoutAHeading`, asserting nothing is drawn). Good.
   Left uncovered and declared so: the launcher's own bash picker in
   `files/var/local/claude-yolo/lib/tmux-session.bash` (see N2).
2. **Heading text.** Multi-team launch: `flow.LaunchTeam(i+1, n, team)` is built per team in
   `chooser.go:181` and `widen.go:49,114`; `multi_test.go:39,169` assert
   `Team 1 of 2: alpha` / `Team 2 of 2: beta` and the three-team order, and `HeadingAt`
   checks the Scope step carries the right team. Running sessions → Add to a team: steps 1-3
   are headed `Teams › Running sessions › <session> › Add to a team · step n/3` with the team
   on steps 2 and 3 (`sessions_test.go:226-228`); Remove, Move (+ Coordinator role) and Role
   name seat and team (`:387,:443,:444`). The lxc-add walker keeps the step 3/4 heading
   (`checkout.go:129,220`). No wrong-team path found.
3. **NO_COLOR / narrow terminals.** NO_COLOR: `Plain` drops every escape (tested). Narrow: see
   B1.
4. **F44 merge.** Only this branch's files changed; coordinator wording and visible action rows
   survived (above).
5. **Version bump.** `git diff F44...aa2e57fa -- files/ playbooks/` is empty; the only bash
   touched is `scripts/test-ccy-teams-command.bash` (a test suite). No bump needed.
6. **meta-deploy.** Keeps `play-speech-to-text.yml`, adds `play-claude-yolo.yml` (which
   builds and installs both `ccy-teams` and `ccy-sessions`, lines 842-861) with a description.
   Correct.
7. **Install-specific names.** Added lines scanned for home paths, emails, host-like names,
   private IPs: none. Docs and plan text use `alpha`, `container-a`, `<team>`.

## Blocker

**B1 — a long heading is truncated by fzf, never wrapped.**
`src-go/internal/ui/heading.go:91-104` (`render`) emits the trail and the context each as one
line of unbounded length. fzf does not wrap header lines; I ran fzf 0.38 under a 40-column pty
and it showed `Teams › alpha › Add LXC member · st..` and `into team alpha · container-a · use..`.
In `Picker.Pick` the frame costs 10 columns (`--margin=1,2 --padding=1,2` and the border), so an
80-column terminal shows 70 characters of each line. The lxc-add context at step 4,
`into team alpha · container <name> · user <name> · checkout <path>` (`lxcadd/adder.go:148-158`),
and the Move coordinator line `session <s> · from team <a> to team <b>` (`sessions.go:353`)
pass 70 easily, and what is lost is the answer just given, which is what the context line
exists to show. The owner has said truncation is not acceptable.

Fix (small, in `ui` only): in `render`, break the trail at `›` and the context at `·` into
lines no wider than a width that fits an 80-column terminal inside the picker frame (about 68),
continuation lines indented; size the rule to the longest line drawn; add a test with a long
context asserting no line exceeds the width and every part is still present. `gum` wraps on the
terminal itself, so the same text is safe there.

## Non-blocking

- **N1** `chooser.go:181`: `Team i of n` counts every item of `--teams`, including `seat@team`
  items that ask nothing, so a list `1@alpha,beta,gamma` heads beta as `Team 2 of 3`. Same as
  F44's string heading; the position is true to the list, so it is not wrong, only odd.
- **N2** The launcher's detached-session picker (`lib/tmux-session.bash`) has no heading and is
  outside this branch (it would need a CCY bump). The owner's requirement is "pervasive"; it
  wants a follow-up, probably when that picker moves to Go.
- **N3** `choose.go:98` says `h.Trail()` on its own line and the heading then draws it again on
  the gum prompt; deliberate (the team's notes sit under it), but the trail appears twice.
- **N4** gum rendering of the ANSI-coloured crumbs inside `--header` is not verified on a real
  gum here (the suite's gum is a stub); owner step H3 in `ACCEPTANCE.md` is the check.
- **N5** `Heading.check` refuses `""` crumbs but not whitespace-only ones.
