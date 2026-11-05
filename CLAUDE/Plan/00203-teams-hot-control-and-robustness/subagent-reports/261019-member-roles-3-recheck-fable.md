# Re-check of fix/teams-member-roles-3 at 914e673f (Task 3.8, narrow)

Reviewer: Fable. Scope: only the four items the dispatch named, plus the merge-tree against
F44 e1e63c39 and the two test runs. Nothing else re-reviewed; nothing changed. The tree was
read from `git archive 914e673f` unpacked under `untracked/scratch/`; the tests ran there.

## 1. Findings 1 and 2 are fixed truthfully

- Finding 1 (H23 default). `SPEC-hot-control.md` H23 step 2 now reads "Interactively it is
  asked, and the question starts on No." `hot/role.go` line 279 comment says "starting on
  No"; the call at line 302-303 is `x.ask(..., false)`. The re-merge report's conflicts list
  was corrected to say F44's yes arrived with the question in 7b5ce2e2, not as a later F44
  change; I checked `git show 7b5ce2e2 -- src-go/internal/teams/hot/role.go` and the added
  call there ends `..., true)`. The correction is true.
- Finding 2 (unreadable-checkout error). `roles.go` identity() no longer says "it gives no
  seat"; the message is "its checkout is read to tell whether it is <handle>, and that read
  failed (...). Start container <c>" with ", or give each of team <t>'s entries its seat in
  the host_vars" appended only when `m.Seat == ""`. `TestAnEntryThatGivesItsSeatAndCannotBeReadIsNotToldToGiveItsSeat`
  pins the seat-giving case (asserts "Start container", no "its seat", no "gives no seat",
  no changes); the pre-existing test still pins the advice for a seatless entry. Fixed as
  asked.

## 2. picker/menu.go merge resolution

`memberActions` (lines 159-174): `handleless := Handle == "" || Handle == "-"`;
`seat := Kind == KindCcy && Dir != "-"`. Role action appended only when not handleless;
Park appended only when not seat and not handleless. So a handle-less row gets no role
action and no Park, for every kind. Verified by reading, and the two tests
(`TestADeferredLxcRowOffersNoParking`, now asserting no `actCoordinator`/`actDemote` and no
coordinator/worker wording; `TestASeatWithNoHandleYetOffersNoRoleButCanBeRemoved`).

The deviation (a handle-less ccy seat keeps attach/restore and "Remove seat") is right, not a
hole:

- `seats.RemoveOrPark` routes a ccy row with `Dir != "-"` to `removeSeat`, which calls
  `Bus.SeatRemove(ctx, row.Dir, Member@Team)`: it works from the folder and never reads the
  handle for anything but wording. Dropping it would strand a seat whose member ccy has not
  made yet, with no way to remove it from the board.
- `seats.AttachSeat` refuses on `Dir == "-"` and on no live session, so a handle-less seat
  without a session is refused cleanly, not attached to nothing.
- The resolution also closes a latent defect on the lxc-stopped parent: 6dfa7c9b's
  `seatActions` gave a ccy row with `Dir == "-"` and no handle a "Park " line with an empty
  name and a role action with an empty handle. Now that row gets attach/restore, open,
  start, help only.

One cosmetic wart, pre-existing on both parents and not this branch's to fix: `removeSeat`'s
confirmation text interpolates `row.Handle` ("… <handle> is parked (its token revoked …)"),
which for a handle-less seat reads " is parked". Note for a later tidy; not a blocker.

## 3. lxcadd/stopped_test.go fixtures

Real emitter (`helpers/agent_bus/declare.py` line 1356) prints nine fields:
`LXCMEMBER team container user host|- role state checkout|- seat|-`. The Go reader
(`internal/declare/lxc.go`) demands exactly 9 with `marker.Fields(out, "LXCMEMBER", 9)` and
maps "-" to "". The fixtures went from 7 to 9 by appending `\t/srv/agent\t-`: a checkout path
of the shape the Python tests use (`/srv/agent/one`) and a left-out seat, which is the common
case for an LXC entry (the seat is usually derived). That is the real shape, not a bend; the
7-field form would have been rejected by the reader, so the old fixtures were only valid
before this branch widened the line.

## 4. meta-deploy.list

- Header: lines 1-21 are byte-identical to F44 e1e63c39 and to 5ce37f5e (`cmp`), 21 lines.
- Order: one `playbooks/imports/play-agent-bus.yml` entry, then one
  `playbooks/imports/play-claude-yolo.yml`. Agent-bus first, as the 00194 comment requires.
- Text: agent-bus comment = 00194 2.0b, then 2.6 stopped-container text, then 3.8 kit text
  ("running container"). claude-yolo comment = 00194 3.99.12.14/2.77.10 text, then 2.6 text
  with "(no image change)" removed (correct: the 3.8 bump rebuilds), then 3.8 text naming
  3.99.12.15 / 2.77.11, which match `files/var/local/claude-yolo/claude-yolo` lines 17 and
  81 on the branch. Accurate.

### Merge against F44 (e1e63c39 already holds fix/lxc-stopped-member-3)

`git merge-tree --write-tree e1e63c39 914e673f` exits 1: one CONFLICT (content), in
`CLAUDE/Plan/meta-deploy.list` only; everything else merges clean (the lxc-stopped code is
identical on both sides, being the same commits). Cause: F44's e1e63c39 wrote 2.6's own
queue text (00194 lines removed as deployed by 5f93ea93; the 2.6 agent-bus comment ends
"Before play-claude-yolo.yml."; the claude-yolo comment reads "rebuilds ccy-teams (no CCY or
image change) … Running sessions are not touched."), while the branch's version still
carries the 00194 lines and merges 2.6 with the 3.8 text.

What the resolution should be, for whoever merges: the F44 side's 00194 text is gone because
00194 was deployed (5f93ea93), so keep the F44 base and add the 3.8 text to each entry;
drop "(no CCY or image change)" from the claude-yolo comment since 3.8 bumps to
3.99.12.15 / 2.77.11 and the image is rebuilt; keep "Before play-claude-yolo.yml." on the
agent-bus comment. The 21-line header is unchanged on both sides. This is the merger's
one-file edit; it does not need another round on the branch.

## Tests (branch tree, from the archive)

- `go test ./internal/teams/...`: exit 0, every package ok.
- `scripts/test-ccy-teams-command.bash`: passed 383, failed 0, exit 0.

## Verdict

MERGE. Findings 1 and 2 are fixed as stated; the menu.go resolution is right and its ccy
deviation is the correct call; the fixtures match the real nine-field line; the queue header
is exact and its text accurate. The one thing to carry into the merge is the expected
meta-deploy.list content conflict against e1e63c39, resolved as above.
