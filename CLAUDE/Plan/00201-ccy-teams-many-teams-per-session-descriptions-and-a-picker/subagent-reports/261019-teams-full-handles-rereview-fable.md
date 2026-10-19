# Fable re-review: fix/teams-full-handles at 4fec538e (Task 2.17)

Re-review of `85d74dff` (the fixes) merged with F44 `0ce52a7e` as `4fec538e`. First review:
`261016-teams-full-handles-review-fable.md` (`d309ed70`). Everything below was read or run in a
worktree on `review/teams-full-handles-rereview`; file:line refer to `4fec538e`.

## Verdict: FIX-FIRST

All five items of the first review are answered as the developer says, the merge against F44 is
clean, and the targeted QA is green. One thing stands in the way, and it is the branch's own
requirement (a): three menu lines in `src-go/internal/teams/picker/sessions.go` still name a
member by its bare seat while the handle is in the same function. Everything else is advisory,
most of it for whoever merges `fix/context-headings` second.

### FIX-FIRST list

1. **Bare seats in the session menus where the handle is in scope.**
   `src-go/internal/teams/picker/sessions.go`:

   - `:267` `"... is already in " + team + " as seat " + seat.Seat + ...` — `seat` is an
     `agentbus.Seat` and `seat.Handle` is right there;
   - `:362` `"(seat " + cell.Seat + " becomes a worker)"` and `:368` `"Its seat " + cell.Seat + " is " + cell.Team + "'s coordinator"` — `handle := r.seatHandle(s.Checkout, cell)` is at
     `:359`, non-empty on that path (`:360` returns otherwise);
   - `:379` `"Role of seat " + cell.Seat + " in " + cell.Team` — `r.seatHandle(s.Checkout, cell)` is
     computed one line earlier for `holder`.

   `hot.Cell` (`hot/list.go:39`) carries no handle, which is why `cell.Seat` was reached for; the
   handle is nevertheless already resolved in each function. Use `agentbus.Label(handle, "")` as
   `choose.go:279` and `hot/role.go:273` now do. `sessions.go:306` (`"seat N (state, handle)"`,
   the seat chooser) shows both and is fine; `hot/add.go:448-478` ("as seat N") are the
   non-interactive `session add` summaries where `s` is a `membership.Seat` with no handle — the
   same "ccy has not made it yet" case the first review accepted.

### Advisory (not blocking)

- **Merging with `fix/context-headings` (`665ce541`).** `git merge-tree --write-tree origin/fix/context-headings 4fec538e` conflicts in five files: `src-go/internal/ui/picker.go`
  (`fzfArgs` signature vs the `RowChrome` constants), `src-go/internal/teams/board/table.go`
  (context-headings' `Crumb`/`memberCell` vs this branch's `measure`), `src-go/internal/teams/picker/picker.go`
  (imports; `Kit.Choose(ctx, r.listHeading(), …)` vs `Kit.Choose(ctx, …)`), `create/wizard.go`
  and `create/wizard_test.go` (imports). `meta-deploy.list` and `go-deps.allowlist` now merge
  clean (they conflicted in the first review).

  The semantic duplications, beyond the textual conflicts:

  - **Width helper**: context-headings has `cells(text string) int { return ansi.StringWidth(text) }`
    unexported in `src-go/internal/ui/heading.go:110` (six callers, all in that file); this branch
    has exported `ui.Width` in `src-go/internal/ui/width.go:7` with the same body, used by
    `board/table.go` from another package. **Keep `ui.Width`, delete `cells`**: the exported one
    is the only form `board` can reach, and the two live in one package so the rename is six
    call sites and no import.
  - **Truncation comes back**: context-headings' `table.go:53-63` still has `memberWidth = 36` and
    `cut(...) + "…"` (counting runes, not cells), which this branch removed. Whoever merges
    second must keep this branch's `Layout`/`measure`/`Rest` and drop `cut`, `memberWidth`,
    `coordinatorMark`, `memberCell`, `rowFormat`.
  - **`Row.Crumb()`** (context-headings `table.go:94-99`) returns `"seat " + r.Member` for a ccy
    row: a bare seat in every heading crumb (`picker/menu.go:199`, `seats/seats.go:250,268`). On
    merge it must become `r.Label()` (or `agentbus.Label(r.Handle, "")`), or requirement (a) is
    undone in the headings the moment the two land together.

- **lxc.conf parsing vs LXC's own.** `hostcheckout.lxcPath` (`hostcheckout.go:94-121`) trims the
  line, skips `#` lines, splits on the first `=`, trims key and value, last `lxc.lxcpath` wins,
  refuses a line with no `=`, an empty value or a relative one. Against liblxc's
  `lxc_global_config_init`: key spacing and `=` spacing agree; a trailing `# comment` on a value
  line is kept as part of the value by both (neither strips it); multiple lines, last wins in
  both; `lxc.include` is a container-config directive, not read by the global-config loader, and is
  rightly ignored here as "another key". Two deliberate departures, both in the fail-closed
  direction: liblxc **skips** a line with no `=`, and an indented `# comment` is a non-match
  rather than a comment there (same outcome). A `/etc/lxc/lxc.conf` that LXC accepts but holds a
  stray word on a line would make ccy-teams refuse every checkout with the file's line number in
  the message (`hostcheckout_test.go:117-136`). Acceptable: fail-fast, named, and the file is
  distro-shipped and nearly all comments. Fail closed holds: every error from `Find` reaches a
  refusal, never an offer (sites unchanged from the first review).

- **Symlinks.** The configured path and its `EvalSymlinks` form are both compared
  (`hostcheckout.go:83-88`), against git's realpath of the top (`TestTheLxcPathIsMatchedThroughALink`).
  Still outside the guard, as before: a container whose rootfs is **not under lxcpath**
  (`lxc.rootfs.path` pointing elsewhere, an LVM/zfs/btrfs backing mounted elsewhere, a bind mount,
  `/proc/<pid>/root`). The package doc says "its root filesystem seen from the host under root's
  LXC path", which is the honest scope.

- **Narrow terminal, rows still too wide.** If TEAM+MEMBER+KIND+STATE alone exceed
  `columns - ui.RowChrome`, fzf cuts the row (`ui/picker.go:169-171` says so). Not a truncation
  this code performs; the edge is now documented in the constant's comment, which is what the
  first review asked.

- `golang.org/x/sys` moves from indirect to direct in `go.mod` for `term.FileColumns`'s
  `TIOCGWINSZ`; the allowlist row says why. `vendor/` unchanged (the module was already there).

## Items of the first review

| #   | Item                                | State                                                                                                                                                                                                                                                                                                                                                                                                                                            |
| --- | ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Queue                               | `CLAUDE/Plan/meta-deploy.list` at `4fec538e` holds the Plan 00179 `play-speech-to-text.yml` entry and one `play-claude-yolo.yml` block, each with its description comment. `git merge-tree --write-tree origin/F44 4fec538e` is clean (tree `8ad77f55`).                                                                                                                                                                                         |
| 2   | Sudoers dropped                     | No `sudoers.d` template, no play task, no `sudo -n lxc-config` in the suite (`git diff 0ce52a7e 4fec538e` touches no `files/` or `playbooks/` path). `hostcheckout.Finder.ReadFile` reads `/etc/lxc/lxc.conf`; `ErrNotExist` is the default `/var/lib/lxc`; any other read error, a line with no `=`, an empty or relative path is an error wrapped as "whether <top> is an LXC container's checkout could not be told" and every site refuses.  |
| 3   | No CCY bump                         | `files/var/local/claude-yolo/` is byte-identical to F44 (`git diff --stat` empty for that tree). The coordinator's ruling that the mandate covers that tree only is noted; the first review's precedent point stands as an opinion the owner has overruled.                                                                                                                                                                                      |
| 4   | No truncation                       | `cut`, `memberWidth`/`descriptionWidth` and the `…` are gone from `board/table.go`; `grep -n '…\|cut(\|[Tt]runc' internal/teams/board internal/teams/picker internal/ui` finds nothing. `Layout` measures every column to its longest value with `ui.Width` (`table.go:163-177`); when the full line exceeds the row width it re-measures without descriptions and `Rest` moves ABOUT plus the remaining columns off the row whole (`:146-159`). |
| 5   | `agentbus.Label` in choose and role | `choose.go:267,279` and `hot/role.go:270,273` use it. The three `sessions.go` sites above are the same class, missed.                                                                                                                                                                                                                                                                                                                            |

## Test seam

- `src-go/internal/teams/host/lxcconf_default.go` is `//go:build !ccyteamstest` and returns
  `os.ReadFile` ignoring its `getenv` argument; `lxcconf_tagged.go` is `//go:build ccyteamstest`
  and is the only reader of `CCY_TEAMS_TEST_LXC_CONF`. `TestTheUntaggedBuildReadsTheRealLxcConf`
  pins that the untagged reader ignores the variable.
- Production build: `tasks/build-go-command.yml:83` builds `go build -trimpath -buildvcs=false [-ldflags …]` with `GOFLAGS=-mod=vendor`; no `-tags` anywhere in it, in
  `playbooks/imports/play-claude-yolo.yml`, or in any other playbook (`git grep -n -- '-tags'`
  over `playbooks/ tasks/` finds only the two `--tags` prose comments). The only `-tags ccyteamstest`
  in the tree is `scripts/test-ccy-teams-command.bash:70`. `go test -tags ccyteamstest ./internal/teams/host ./internal/teams/ops` and `go vet -tags ccyteamstest` pass, so the tagged
  half compiles too.

## Merge safety

| Against                                  | Result                                                                                                                                                                                                   |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `origin/F44` `0ce52a7e`                  | clean                                                                                                                                                                                                    |
| `origin/fix/context-headings` `665ce541` | conflicts: `src-go/internal/ui/picker.go`, `src-go/internal/teams/board/table.go`, `src-go/internal/teams/picker/picker.go`, `src-go/internal/teams/create/wizard.go`, `…/wizard_test.go` (see Advisory) |

## Names, stderr, plan state

- Added lines carry only placeholders (`box`, `proj`, `Example-Org`, `/srv/lxc`, `/home/u`); the
  suite's stand-in `lxc.conf` is written under its scratch folder.
- Refusals go to stderr through `Term`/`Kit.Tell`/`ReportErr`; `--list`'s table is the only stdout.
- `PLAN.md` and `JOURNAL/00201-Journal-26-10-16.md` are updated in the same commit as the code.

## QA run (targeted only)

- `go test ./internal/hostcheckout ./internal/agentbus ./internal/sessions ./internal/teams/... ./internal/term ./internal/ui`: pass. `go test -tags ccyteamstest ./internal/teams/host ./internal/teams/ops` and `go vet -tags ccyteamstest ./internal/teams/host`: pass.
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK`.
- `scripts/test-ccy-teams-command.bash`: exit 0, 378 passed, 0 failed, with every `PINGBUS_*`
  variable unset (a first attempt left three set and was stopped and rerun).
- `scripts/test-ccy-teams.bash`: exit 0, 191 passed, 0 failed, same environment.
- `qa-all` not run (re-review scope). No play was run.
