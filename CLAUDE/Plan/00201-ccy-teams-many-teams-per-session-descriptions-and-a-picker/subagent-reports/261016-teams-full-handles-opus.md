# Task 2.17: full handles, no host session in a container's checkout, columns sized to fit

## After Fable's FIX-FIRST (review `261016-teams-full-handles-review-fable.md`)

- F44 `fa26fc66` merged; `meta-deploy.list` keeps F44's speech-to-text entry and one
  play-claude-yolo.yml block.
- The sudoers rule is gone (template, play task, suite checks). `hostcheckout` reads root's LXC
  path from the world-readable `/etc/lxc/lxc.conf` (`lxc.lxcpath`, last one wins; comments,
  blank lines and whitespace allowed; a line with no `=`, an empty or relative path, or an
  unreadable file is an error and a refusal), else `/var/lib/lxc`. No sudo, no `lxcfreeze`
  preflight, no "run sudo -v first". The parity suite points the binary built with
  `-tags ccyteamstest` at a stand-in file (`CCY_TEAMS_TEST_LXC_CONF`, ignored by every other
  build, tested both ways).
- No CCY bump (the coordinator's correction: nothing under `files/var/local/claude-yolo/`).
- Nothing is cut: a team's description widens MEMBER like any value; when the table is too
  wide for the terminal it moves off the row whole, as `ABOUT <description>`, with the other
  moved columns. Widths are terminal cells (`ui.Width`, x/ansi `StringWidth`).
- `choose-seat` ("Seat <handle> is new" / "has no member") and `session remove` (the role mark
  and the coordinator question and summary) now name the handle through `agentbus.Label`.

The sections below describe the first round; where they mention the sudoers rule,
`lxc-config` or the 36-character cut, the bullets above replace them.

Branch `fix/teams-full-handles` from F44 `7f8b1c23`. ccy-teams and ccy-sessions only; no CCY or
container bump (nothing under `files/var/local/claude-yolo/` changed).

## 1. Every seat by its whole handle

Cause: `board.seatRow` set the MEMBER column to the bare seat (`sa.seat.Seat`) whenever the
seat's checkout was known; only members with no local record showed their handle. Two
checkouts of different repositories both have a seat `1`, so their rows read the same.

Fix: one renderer, `agentbus.Label(handle, role)` (`src-go/internal/agentbus/label.go`): the
whole handle, with ` (coordinator)` beside the coordinator's. `board.Row.Label()` applies it to
a row (a person's row stays the name, a team row its description). The MEMBER column, the
row menus' title and Remove seat line (`picker/menu.go`), the seat refusal and the free-seat
choices (`seats/seats.go`) all use it. `Row.Member` keeps the bare seat as the key flows use
(`<seat>@<team>` refs), but is no longer shown for a member. Settings, lxc-add and the
coordinator wording already showed handles. A new seat that ccy has not made yet still reads
`Seat N@team new`: its handle is given by the launch and is not guessed.

## 2. Ctrl-N offered a host session in an LXC container's files

Verified as a real defect, in the code below. The host agent established the trigger: no
host_vars value holds a container path; ccy-teams was started with its working folder inside
`<lxcpath>/<container>/rootfs/<path>`, a real git checkout whose origin a team covers.

Cause: every place ccy-teams and ccy-sessions take the working folder as a checkout asked git
alone (`ProjectHere` then `TopLevel`), so a checkout in a container's files was a host checkout:

- `src-go/internal/teams/picker/picker.go:162-173` (F44 `7f8b1c23`), `pickerFrame`: the header
  line "Ctrl-N starts a new ccy session in the row's team, in <top>". This is the line reported.
- `src-go/internal/teams/seats/seats.go:151-171`, `NewSession`: what Ctrl-N runs; it execs the
  launcher in that top.
- `src-go/internal/teams/picker/offers.go:335-360`, `offerJoinHere`: the start-up offer.
- `src-go/internal/teams/board/collect.go:279-291`, `askWorkTree`: read that checkout's seats as
  host seats.
- `src-go/internal/teams/chooser/chooser.go:263-273` (`choose-teams`, run by `ccy --teams`),
  `src-go/internal/teams/find/find.go:55-64` (`for-checkout`),
  `src-go/internal/teams/create/wizard.go:935-955` (the wizard's offer to join),
  `src-go/internal/sessions/picker.go:119-124` (ccy-sessions' Ctrl-N row).

Neither `helpers/agent_bus_lxc/derive.py` nor the lxc-add checkout walker leaks the path: both
stay inside lxc-add, and the team scope holds repositories, not folders.

Fix: `src-go/internal/hostcheckout` (`Finder.Find`) is the one answer to "which checkout is this
folder in, and may a host or podman session run there": git's top, judged against root's LXC
path (`lxcfreeze.Lister.LxcPath`, `lxc-config lxc.lxcpath` as root, resolved through a link,
asked once, only when the folder is in a checkout and LXC is installed). A top under the LXC
path is container `<c>`'s: every site above now asks it, never offers or starts a session
there, and says "`<top>` is LXC container `<c>`'s checkout ... Add the container's agent to a
team with ccy-teams lxc-add <team>, or run agent-bus-claude (or ccy) inside the container."
An LXC path that cannot be read is a refusal naming the reason, never taken as the host's.
`board` leaves such a checkout out of the seats it reads. A new sudoers rule,
`files/etc/sudoers.d/ccy-lxc-path.j2` (exactly `/usr/bin/lxc-config lxc.lxcpath`, installed by
play-claude-yolo.yml with `visudo -cf`), lets both commands ask without a password.

host_vars: nothing to re-run. No key holds a container path (the host agent's read-only check).

The Ctrl-N action is a visible menu row ("Start a ccy session in <team>, in this folder") and
the `project_hidden_bindings` rule is in F44 (`src-go/qa/hiddenbindings`).

Not covered: other container roots (podman's storage); the `ccy` launcher itself, which is
bash, is not guarded when run by hand in such a folder.

## 3. Columns sized to their longest value

`board.Layout(rows, width)` lays a pass's rows out together: each column as wide as its
longest value (a team description counts up to 36 characters and is cut to the column on a
terminal, whole when piped, as before). A handle is never cut. When the table is wider than
the terminal (`term.FileColumns`, the window-size ioctl; the picker takes `ui.RowChrome` off for
fzf's frame), the table is narrow: rows keep TEAM, MEMBER, KIND and STATE, and APPLIED, SESSION,
RESTORE and WHERE move to the row's header in the picker ("This row: APPLIED ...") and to an
indented line under the row in `--list`. `golang.org/x/sys` is now a direct requirement
(already vendored; allowlist reason updated).

## Tests

Test first for each: `hostcheckout`, `agentbus.Label`, `board` (table, `--list` narrow,
collect), `picker` (header in a container checkout, whole handles, narrow rows), `seats`,
`chooser`, `find`, `create`, `sessions`, `ui.RowChrome`, `term.FileColumns`.
`scripts/test-ccy-teams-command.bash` now pins handles instead of bare seats, fakes LXC
(`lxc-config`) and checks the container-checkout refusals; it, `test-ccy-teams.bash`,
`test-ccy-container-name.bash` and the ccy-sessions take-over and reboot suites pass with
`PINGBUS_*` unset.
