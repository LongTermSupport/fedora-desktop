# Fable review: fix/teams-full-handles at 117a1026 (Task 2.17)

Reviewed against F44 `a153aede` (branched from `7f8b1c23`). Developer report:
`261016-teams-full-handles-opus.md`. Everything below was read or run in a worktree on
`review/teams-full-handles`; file:line refer to `117a1026`.

## Verdict: FIX-FIRST

The code does what the three requirements ask and the tests prove it, but four things stand
between it and F44: the branch does not merge cleanly into the current F44 (the queue), the
passwordless sudo rule is more privilege and more machinery than the question needs, the
precedent for shipping a user-visible ccy-teams change (a CCY patch bump with a changelog
section) was not followed, and the one remaining truncation is exactly what requirement (c)
forbids unless the owner says otherwise.

### FIX-FIRST list

1. **Merge F44 `a153aede` and resolve `CLAUDE/Plan/meta-deploy.list` to ONE entry.**
   `git merge-tree --write-tree 117a1026 a153aede` conflicts on `CLAUDE/Plan/meta-deploy.list`
   (F44 emptied the queue at `a153aede`; the branch appended under the speech-to-text entry that
   is now gone). The resolved list must carry only the `playbooks/imports/play-claude-yolo.yml`
   block the branch adds. Nothing else conflicts with F44.
2. **Drop the sudoers rule; read root's LXC path without root** (judgement under 1 below).
   Replace `lxcfreeze.Lister.LxcPath` as the `hostcheckout.LxcPather` with a reader of
   `/etc/lxc/lxc.conf` (`lxc.lxcpath = ...`, else the compiled default `/var/lib/lxc`), delete
   `files/etc/sudoers.d/ccy-lxc-path.j2`, the play task at
   `playbooks/imports/play-claude-yolo.yml:868-882`, the suite check at
   `scripts/test-ccy-teams-command.bash:470-471` and the `sudo -n lxc-config` assertion at
   `:840`, and the "run sudo -v first" wording at `src-go/internal/hostcheckout/hostcheckout.go:65-66`.
   An unreadable `/etc/lxc/lxc.conf` stays an error (fail fast), never "the host's".
   If the owner overrules and keeps the rule, it is sound as written (1b below) and then
   needs items 3 and 5.
3. **Follow the ccy-teams release precedent: a changelog section and a CCY patch bump.**
   `docs/ccy-changelog.md:20-28` (3.99.12.10, commit `5d676753`) shipped a Go-only
   ccy-sessions/ccy-teams change with "the image is unchanged" and still bumped `CCY_VERSION`
   in the launcher and wrote a section. This branch changes every member row's text, adds a
   refusal, a new table layout and (as written) a root-owned file on every host, with no
   section and no bump. Add `3.99.12.11` (container unchanged) and the launcher bump, as that
   precedent did; the task note "no CCY or container bump" in `PLAN.md` and the developer report
   then change too.
4. **Remove the 36-character description cut, or record the owner's exception.**
   `src-go/internal/teams/board/table.go:79` (`descriptionWidth = 36`), `:116-118`, `:189-195`,
   `:215-222` (`cut`, ending in `…`) still truncate a team's description on a terminal.
   Requirement (c) is "no truncation"; the developer kept this as pre-existing behaviour. Either
   the constant goes (the column fits the longest description, like every other column) or the
   owner's exception is written into the package doc.
5. **(Only if the rule stays) document it.** `docs/playbooks.md:476-495` (play-claude-yolo)
   does not mention `/etc/sudoers.d/ccy-lxc-path`; the agent-bus rule is documented at `:752`.

### Should fix, not blocking

- Bare seat numbers still shown where the handle is in scope:
  `src-go/internal/teams/choose/choose.go:279` ("Seat N@team has no member" — `seat.Handle` is
  right there), `src-go/internal/teams/hot/role.go:275-285` ("Seat N is team's coordinator"
  in `ccy-teams session remove`'s question and summary — `handle` is in scope). `choose.go:267`
  and `seats/seats.go:147` ("Seat N@team new") are reasonable: a seat ccy has not made yet
  has no handle, and the developer rightly does not guess one.
- `table.go:224` counts code points, not display width: a CJK or emoji description misaligns
  every column after it. `github.com/charmbracelet/x/ansi` is already a direct dependency
  (`StringWidth`); one call. Handles are ASCII, so (a) is unaffected.

## 1. Sudoers

### 1a. Is passwordless sudo needed? No.

Root's LXC path has exactly one configuration home, `/etc/lxc/lxc.conf` (`lxc.lxcpath`), and
one default, the compiled-in `/var/lib/lxc`. This repository never sets it: no `lxcpath` or
`lxc.conf` anywhere under `playbooks/`, `tasks/`, `vars/` or `files/` (the only hit is the new
sudoers template). Where the lxc package ships the file it is `0644`. The reason the developer
reached for root is real but narrow: an unprivileged `lxc-config lxc.lxcpath` answers from
the *caller's* `~/.config/lxc/lxc.conf` and defaults to `~/.local/share/lxc`, so it cannot be
run as the user. Reading the root file (with the default) answers the same question with no
privilege at all.

What the sudo route costs, which the file read does not:

- a NOPASSWD root grant on every host the play runs on, for one read;
- `lxcfreeze.Lister.preflight` (`src-go/internal/lxcfreeze/lister.go:17,146-159`) demands
  `lxc-ls`, `lxc-attach`, `python3` **and `sudo`** before it will answer `LxcPath`, so a host
  with lxc installed and no sudo cannot start `ccy-teams` in any checkout
  (`hostcheckout.go:64-67` turns that into a refusal of every offering site);
- a window between deploying the binary and the rule where every run in a checkout refuses
  with "run sudo -v first" (`hostcheckout.go:65-66`) — closed only because both land in one
  play run;
- one `sudo -n` exec and one auth-log line per `ccy-teams`, `ccy-sessions` and `ccy --teams`
  start on an LXC host.

Judgement: the file read is simpler (one function, no template, no play task, no sudo seam in
the picker path) and least-privilege (none). YAGNI says the rule goes. `Lister.LxcPath` can
stay for `lxc-add`, which already runs as root, or call the same reader so there is one home
for "root's LXC path".

### 1b. If the rule stays, is it exact? Yes.

`files/etc/sudoers.d/ccy-lxc-path.j2:8`: `{{ user_login }} ALL=(root) NOPASSWD: /usr/bin/lxc-config lxc.lxcpath`. No wildcard; the argument is pinned, so `lxc-config -l`,
`lxc-config lxc.default_config` or anything else is refused; `lxc-config` with that argv prints
a directory name and changes nothing; the file name has no `.` (sudoers would skip it);
`user_login` is the play's own variable (57 uses). Play task
`play-claude-yolo.yml:868-882`: `become`, `owner/group root`, `mode "0440"`,
`validate: "visudo -cf %s"`, tags matching the binaries it serves; the same shape as
`play-agent-bus.yml:226-234`. In the IaC graph the drop-in is where its consumer is built,
which is right (lxc's own play is optional and earlier in `playbook-main.yml`).

Non-interactive: `sudo.Capture` (`src-go/internal/sudo/sudo.go:114-120`) always passes `-n`
and marks "a password is required" as `ErrNeedsPassword`; `hostcheckout.Find` wraps any error
and every site refuses, never allows (`hostcheckout.go:64-67`; picker frame `picker.go:177-179`;
`seats.go:153-155`; `offers.go:335-339`; `chooser.go:267-271`; `find.go:56-59`;
`wizard.go:942-945`; sessions `picker.go:123-125`). Fail fast holds. `visudo` is not in the
container, so the validate was not exercised here; `ansible-playbook --syntax-check` passes.

## 2. Path guard

- **Resolved paths**: `git rev-parse --show-toplevel` prints a real path, so a host symlink
  into a rootfs is caught on the checkout side; the LXC path is resolved with
  `filepath.EvalSymlinks` and both forms kept (`hostcheckout.go:83-88`), so a symlinked
  `/var/lib/lxc` is caught too (test `hostcheckout_test.go`, the `/srv/lxc` cases).
  `containerOf` (`:100-108`) uses `filepath.Rel` with `..` guards; a sibling such as
  `/srv/lxcother/proj` is not matched (tested).
- **Not covered, admitted or should be stated**: a bind mount of a rootfs elsewhere (no path
  relation survives a mount), and a checkout reached through `/proc/<pid>/root/...`
  (git's realpath of that magic link is the container-internal path, which the guard then
  compares against nothing useful). Both need a deliberate act by the person; low. Podman's
  storage roots: admitted. The bash `ccy` launcher run by hand in such a folder: not an
  *offer* (the person typed the path), and `ccy --teams` is covered because
  `choose-teams` refuses first (`chooser.go:96`, `:264-277`); plain `ccy` is not. I would
  accept the gap; the owner's "never offer" is met.
- **Every offering site covered**: the remaining `ProjectHere` uses are `chooser.go:282`
  (`checkoutRepo`, reached only after the refusal at `:96`) and `wizard.go:252`
  (`remoteRepo`, which derives a repository name for the scope question and starts nothing).
  `host.go:156` and sessions `picker.go:117` are `os.Getwd` for display only. No site takes a
  folder as a session's checkout without asking `hostcheckout`.
- **Fail-fast relaxations to note**: `board/collect.go:269-285` turns an unreadable checkout
  into a stderr line plus `Snapshot.Incomplete` (shown on every team row; `--list` then exits 1,
  `list.go:31`) where it used to stop the list; `picker/offers.go:335-339` reports and returns
  `nil` where it used to `Leave(ExitFailure)`. Both are probe-then-flag with the failure said
  and the picker still usable for other rows; the frame line already carries the same error.
  Acceptable, but it is a change in behaviour the report does not mention.

## 3. Handles

`agentbus.Label` (`src-go/internal/agentbus/label.go`) is the one renderer; `board.Row.Label`
(`table.go:84-89`) applies it to the MEMBER column, the menus' title and Remove line
(`picker/menu.go:146,180`), the attach refusal (`seats.go:78`) and the free-seat choices
(`seats.go:144`). LXC members (`<repo>.<seat>+<host>.lxc`) go through the same path — the suite
pins `a-very-long-repository-name.12+build-host.lxc` whole. Remaining bare seats: the
"Should fix" list above. The keys (`<seat>@<team>` tags) stay bare, correctly.

## 4. Columns

`board.Layout` (`table.go:108-124`) sizes each column to its longest value over the pass; a
handle is never cut; narrow terminals keep TEAM, MEMBER, KIND, STATE and move the rest to the
row header (picker) or an indented line (`--list`), `Rest` at `:158-168`. The only truncation
left is the description cut (FIX-FIRST 4) and the one `…` in the code is that function. If the
four kept columns alone exceed the width, fzf cuts the row (no wrap): an edge the developer
did not state. Wide runes: see "Should fix".

## 5. Versions

`.claude/rules/ccy-version-bump.md` and `CLAUDE/ContainerRules.md:59-116` mandate a bump
only for files the launcher or image carry; nothing under `files/var/local/claude-yolo/`
changed, so the hook does not fire. But the project's practice for ccy-teams is the
precedent in FIX-FIRST 3: a Go-only change with the image unchanged got `3.99.12.10` and a
section. A user-visible change with no changelog line and no version is the gap the rule file
warns about in prose ("a behaviour change shipped with an unchanged version").

## 6. Merge safety

| Against                                      | Conflicting files                                                                                                                                                                                                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| F44 `a153aede`                               | `CLAUDE/Plan/meta-deploy.list`                                                                                                                                                                                                             |
| `origin/fix/context-headings` `7e6f2ca6`     | `CLAUDE/Plan/meta-deploy.list`, `src-go/go-deps.allowlist`, `src-go/internal/teams/board/table.go`, `src-go/internal/teams/create/wizard.go`, `…/wizard_test.go`, `src-go/internal/teams/picker/picker.go`, `src-go/internal/ui/picker.go` |
| `origin/fix/lxc-checkout-typed` `90c02eed`   | `CLAUDE/Plan/meta-deploy.list`, `src-go/internal/ui/picker.go`                                                                                                                                                                             |
| `origin/fix/teams-wait-play-lock` `8da095cb` | `CLAUDE/Plan/meta-deploy.list`, `…/00201/PLAN.md`, `…/00201/JOURNAL/00201-Journal-26-10-16.md`, `src-go/internal/teams/create/wizard.go`, `…/wizard_test.go`                                                                               |

`fix/context-headings` is the costly one: both branches rework `board/table.go` and
`ui/picker.go`. Whichever lands second re-lays the table.

## 7. Names, fail-fast, stderr

- Added lines carry only placeholders: `proj`, `other`, `box`, `build-host`, `local`, `desk`,
  `/home/u`, `/srv/proj`, `/var/lib/lxc/box/rootfs/...`. No install-specific name landed.
- Stderr: every refusal goes through `Term.Printf`/`Say` (`term.go:137-148`, to `Err`),
  `Kit.Tell`, `c.say` or `ReportErr`; `--list`'s table alone is on stdout. Clean.
- Fail-fast: see the two relaxations in section 2; no `_ =`, no swallowed error.

## QA run (targeted only)

- `go test ./internal/hostcheckout ./internal/agentbus ./internal/sessions ./internal/teams/... ./internal/term ./internal/ui`: pass.
- `scripts/qa-go.bash`: one failure, `qa/cmd/qa-patterns` `TestRealSemgrepScansTestsDirectoriesAndLargeScripts`
  (semgrep `Timeout` on the large fixture) — a package this branch does not touch; the test
  passes when run alone (12.8 s). Load-dependent, not this branch.
- `scripts/qa-hidden-bindings.bash`: `HIDDEN-BINDINGS-OK`.
- `scripts/test-ccy-teams-command.bash` (380 passed, 0 failed), `test-ccy-teams.bash`,
  `test-ccy-container-name.bash`, `test-ccy-sessions-take-over.bash`,
  `test-ccy-sessions-reboot.bash`: all exit 0, with `PINGBUS_*` unset.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: pass.
  `ansible-lint` and `visudo` are not in the container; not run; no play was run.
