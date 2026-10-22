# n3b-D2 handoff: LXC members keyed per seat (design only)

Plan 00209, slice n3b. Worktree `/workspace/untracked/worktrees/bus-v2-n3b-d2`, branch
`feat/bus-v2-n3b-d2`, cut from `7dccc2037`. The work stopped at the context budget. Review
round 2 returned FIX-FIRST, and its fixes are not yet made.

## The decision as recorded

The owner's requirement, relayed by the team coordinator: "it absolutely should be, no limits".
It is recorded in SPEC-n3 section 10, row n3b-D2, and in C 3.9.1, "What an LXC member is":

- An LXC member is (team, container, checkout, seat). The user is only who runs it.
- Any number of entries per user, per checkout and per container.
- `seat` is required. `lxc-add` writes the lowest number that is free across the whole team.
- (team, container, checkout, seat) is the duplicate key.
- The role belongs to the entry, through DC6's team `coordinator`, so there is one coordinator
  per team and no `role` key on an entry.
- The record and the host copy are keyed (container, checkout, seat).
- The offline rule is kept for each seat.

## The choices made

- **desired.json is per seat.** B's desired file holds one seat per team (B 2.10), and B's state
  root holds one directory per team. A file per checkout therefore could not hold a coordinator
  and a worker of one team. A file per seat mirrors ccy's `sessions/<session>/` and
  `control/<session>/` (C 3.7.1).
- **Names come from a seat key.** `<key>` is the first 16 hex of SHA-256 of
  `<team>\n<checkout>\n<seat>`. A collision inside one container is refused at step 01.
  - The repo label is the checkout's base name, put through `NormaliseRepo` and cut to 24
    characters. It is for display only.
  - The unit is `teamchat-session-<team>-<repo>-<seat>-<key>.service`, at most 94 bytes.
  - The tmux session is the unit's name without `.service`, at most 86 characters.
  - The tmux server is started with `-S /run/teamchat-session-<key>/tmux.sock`, which is 48
    bytes. There is one server per unit, so the units do not share a cgroup.
  - The unit sets `RuntimeDirectory=teamchat-session-<key>`, which is 38 bytes. B's `link.sock`
    path is then 78 bytes and the inbox socket path at most 60, both under the 107-byte limit.
  - The session name is `lxc-<key>`.
  - The guest's directories are `~/.config/teamchat/lxc/<key>/{seats,membership}`.
  - The host copy is `_lxc/<container>/<key>.json`.
  - Targets are written `--member <container>/<key>`.
  - An output line names the seat as `<user>@<container>/<repo>-<seat>-<key>`.
- **Launch is by key:** `run --kind lxc --key K` works out both roots from the key, and LXC has no
  default roots. The unit and a seat run by hand with `service: false` use the same line.
- **The seat is fixed by the entry.** `lxc apply` passes the entry's seat, and `--seat next` has
  no caller left.
  - A retired number is refused at step 03.
  - A deactivated account is that entry's `DEFERRED` at step 06.
- **User move:** when the declared `user` changes on a recorded key, the seat leaves under the
  old user and then joins for the new one with `return`.
- **A removal that completes drops the record entry.** An entry marked `left` takes no part in
  the key and handle rules.

## Where the edits landed

- C: 3.9.1 (the member paragraph, the names table, and why the desired file is per seat),
  3.9.2 steps 01, 03, 04, 05, 06, 07 and 08, the paragraph on a stop during a phase, the absent
  phase, and the L3 probe, then 3.9.3 and 3.9.4 (the unit text). Outside 3.9: 3.5.1's gated
  fields, 3.6.3's `--seat` bullet, the host_vars example, the bullet on what changed from v1,
  and the VM lab row.
- SPEC-n3: CL5, GU3-GU5, LX1, LX2, LX5, LX7, LX8, LX9, SU1, SU3, LP1, gap 18, n3-D3, n3-D8,
  n3-D13, n3b-D1's target, and the new row n3b-D2.
- SPEC-n2d: n2d-D15 and n2d-D21 amended.
- B: the path table and its note, the `run` row, 2.4.1's opening, the 2.13 operations
  (`--key`), and the unit-name lines.
- D: the path tables, the guest unit row, M1 (seat), and the move steps.
- DESIGN-v2.md: rows 25 and 28, the diagram, and the unit lines. PLAN.md: Task 3.3.
- Rework scope: `subagent-reports/261022-n3b-d2-rework.md`.

## Verdicts

- Review 1 (`261022-n3b-d2-review1.md`): FIX-FIRST, with 3 MAJOR and 4 MINOR. All were fixed in
  `cefd16c5e`, except the row-order nit, which was declined: n3b-D2 sits beside n3b-D1 as briefed.
- Review 2 (`261022-n3b-d2-review2.md`): FIX-FIRST, with 0 BLOCK, 4 MAJOR and 7 MINOR. **Not
  yet fixed.** The findings rose from 7 to 11, so the next agent should fix narrowly and then run
  one narrow re-check, not a full round.

## Left to do (review 2)

- **F-A.** Choose option (a): an explicit `add --seat S` whose account is deactivated records
  the handle `retired` and refuses with a distinct code, and `lxc apply` maps only that code to
  `DEFERRED`. Write it into MO4, n2d-D21, C step 06, LX7 and the rework file. This is a change in
  `apply/member.go`'s `admit` (today it records `retired` only when `req.Seat == 0`). Cover
  `return` of a deactivated account too.
- **F-B.** In 3.9.3 and LX9, a handle must be unique only among entries that are not `left`.
  LP1, the L3 paragraph and `ByHandle` map a handle to the entry that is not `left`. A `left`
  entry is never parked again. The rework file adds `lxcstate` `Record.check` and `ByHandle`.
  LX8 gets tests for a move whose old container is stopped, and for one whose old seat is still
  held.
- **F-C.** The rework file's `host/lxc/unit` item should say `ExecStart` is
  `teamchat run --claude --kind lxc --key <key> --resume-recorded -- claude`, and that the CL5
  test follows the `--key` rule.
- **F-D.** Amend SPEC-n2b's DR, AG1, AG2, AG3 and AG6 for the new key. AG3 needs an entry text
  that cannot be read two ways, for example `<container>/<seat>:<checkout>` with the checkout
  last, because a checkout may contain `,` or `=`. Make `user` a field of the entry, add
  SPEC-n2b to n3b-D2's list of amended chapters, and add the AG3 grammar to the rework file's
  `gate.go` item.
- **F-E to F-K (MINOR):** see the review. They are: a user move whose old seat is still held; a
  record line for a held-back entry that overwrites a `left` entry; `seat set-aside` cannot name
  an LXC seat; leftover launch lines in the verb table; the run root of a seat run by hand; a
  user move that skips the "origin changed" refusal; and a handle the roster holds `active` with
  no record entry, which should be refused at step 03.
- Then write `subagent-reports/261022-n3b-d2.md`, the final report: the decision, the choices,
  where the edits landed, the verdicts and the tip.

## State

The tip is `cefd16c5e`. Nothing is uncommitted except this handoff, which is committed with it.
qa-changed passed at each commit.
