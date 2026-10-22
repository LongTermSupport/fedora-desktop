# acceptance-s1 fix: S3 opened in a checkout, the not-judged wording, S4 with no live seat

Branch `fix/acceptance-s1-cwd`, cut from origin/F44. Commits: cc1904d3b (the fix), 9ca963c25
(review round 1 findings), then this report with review 2. Not pushed.

## Changes

- **S3 cwd.** `outside_checkout` makes an empty folder under TMPDIR (`mktemp -d -t`) and asks git
  whether it is in a work tree, as `gitx.ProjectHere` asks: exit 0 (TMPDIR inside a checkout, or
  GIT_DIR set) is refused as not judged (2), and an unexpected git error is a failure (1). Every
  open, old and new, in every round, starts in that one folder (`open_once` takes a cwd, `cd` in a
  subshell). `check_open` removes it with `rmdir` on every path after the rounds (now
  `open_rounds`), and fails if something wrote there. Confirmed against the source:
  `offerJoinHere` returns at `here.Top == ""`, which `hostcheckout.Find` gives when ProjectHere
  says no. The old binary's `offers.go`, `hostcheckout.go` and `ProjectHere` are the same at the
  pre-stage-1 revision.
- **Not-judged wording.** `_s1-shim.bash` now writes the first screen's arguments to
  `<ui>.argv`. `not_table` prints the screen, its arguments (the question, through `cat -v`) and
  the real causes from `pickerSession`: Create a team (no team), Folder limit, Description,
  Update (teams out of date or declared and not built), with what makes each one stop. The header of
  `acceptance-s1.bash` and the meta-deploy.list entry say the same, and no longer blame an
  unapplied update.
- **S4 fixture.** `check_report_per_hour` = `live_report_per_hour` (the old check, unchanged in what
  it judges) + `fixture_report_per_hour`. The fixture: `<run>/s4-fixture/state/sessions/` holds one
  session record (`ccy-session-record 1`, prefix ccy) naming `<run>/s4-fixture/checkout`, which holds
  `.claude/ccy/pingbus/seats/<team>/{1,2}/member.json`. The team is the first applied stamp in
  `${CCY_TEAMS_APPLIED_DIR:-/var/lib/agent-bus-applied}`; a team with a stamp is always on the
  table (`addStamped`). The installed `ccy-teams --list` runs with `CCY_STATE_DIR` pointing at that
  registry (`registry.Locate` honours it), from a second folder outside any checkout. Seat 1 sets
  `report_per_hour` 6: it must be on a table line with its team and not refused. Seat 2 sets 61: it
  must be refused with v1's words
  (`seat 2@<team>: team <team>: <path>/member.json: limits.report_per_hour must be an integer from 1 to 60`)
  and left off. That proves the binary read the fixture and judged the limit, so the check cannot
  pass vacuously. The production path, all real: registry List, then board `addRecord`, then
  `readSeats`, then `agentbus.SeatList`, then `seat.List` and `readMember`.
  The member.json was checked against the real `seat.List` with a throwaway Go test (deleted, not
  committed): seat 1 listed with its handle, seat 2 refused with exactly the expected line.

## How S4 is judged now

Always on the fixture, and the report says so ("S4 on a fixture, not a live seat"). Every live seat
whose member.json sets report_per_hour is judged as before. If there is no live seat, the report says
"S4 was judged on the fixture alone". NOT JUDGED only when there is no applied team for the fixture
and no live seat. An applied-state directory this user cannot read fails (1).

## Reviews

- Round 1 (Opus, `261022-acceptance-s1-fix-review-1.md`): MERGE, with four low findings: the
  "until it is answered" wording, an unreadable applied directory misread as no team, a fixture
  `--list` that did not start being blamed on the wrong check, and the header's not-judged causes.
  All four were fixed in 9ca963c25.
- Narrow check of the fixes (Opus, `261022-acceptance-s1-fix-review-2.md`): MERGE. A nit, not
  taken: the meta-deploy.list comment leaves out "declared and not built".

## Checks

`bash -n`, `shellcheck -x` and `./scripts/qa-changed.bash` pass on each commit. A local smoke run
over a stub binary exercised `fixture_report_per_hour`, `outside_checkout` and the shim's argv
capture. The script itself needs the host and was not run.
