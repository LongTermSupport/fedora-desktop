# Task 4.2, third part (Plan 00209): report

A ccy session can now join a v2 (teamchat) team by naming its seat, `ccy --teams <seat>@<team>`.
LAUNCH-ccy.md items 1-5 are built and committed on this branch. Item 6, the seat sweep and its
user units, is not done: see "Left". CCY is 3.116.0 and the container stays at 2.82.0 (see the
owner/coordinator decision below).

## Done

- **The deadcode fix is in the first commit.** `teamchatlaunch.StoreTaker.TakeAll` is reached
  from `ccy-teams internal launch-args`. It has real-store tests in `storetaker_test.go`. It
  also keeps any seat the launch's session already holds (decision L5): `seat.Take` refuses
  such a seat, so a relaunch or a restore could not take its own seats again.
- **The four verbs** are `teamchatlaunch.Verbs` (`verbs.go`), wired in `teams/app.go`. They
  have a usage entry, and stdout is diverted for `launch-args` and `seat-check`:
  - `launch-args` (item 1): for agent-bus, `membership.Init.Prepare` is split out of `Run`. For
    teamchat it runs `Launch.Run` over the real store.
  - `seat-check` and `seat-take` (item 2): for a v1 list they exec agent-bus. For a v2 list,
    `seat-check` prints the canonical list and `seat-take` does nothing.
  - `bus-disconnected` (item 3).
- **`describe-seats --block`** has a teamchat branch (`Choose.Teamchat`).
- **Bash:** each library swaps one command word, and the bash-limits gate passes.
  `ccy_membership_args` passes `DOCKER_MOUNTS`, `SSH_MOUNTS` and `GUI_MOUNTS` after `--` for
  the mount guard.
- **Test fakes (item 4):** `scripts/test-ccy-teams.bash` passes all 256 checks. New checks
  cover the launch-args argv and the status given at exit.
- **RS3 and RS2 (item 5):**
  - `seat.Store.Restarted` records `held/notice.json` with `why: restart`, so neither a release
    nor an announce posts a notice for that run.
  - `ends.restartSwitch` is now true, and RS1's pinned test is flipped.
  - PLAN.md's 2.0e row is closed.
- **Coverage:** `teamchatlaunch` is at 92.5% (it was about 78%), with no exclusions.
- **Docs:** LAUNCH-ccy.md (decisions L4 and L5, and the item 6 still left), PLAN.md Task 4.2,
  and the `docs/ccy-changelog.md` entry for 3.116.0.

## Decisions taken (L4, L5 in LAUNCH-ccy.md)

- **L4. What the exit decides from.** `ccy_bus_disconnected` passes the container id while the
  container still ran (the launcher left on a signal), and otherwise the launcher's
  `container_rc`:

  - 75: a relaunch (RS3);
  - any other number: the session is over. Its record is removed, then its held seats are
    released;
  - not a number: each seat is announced and kept.

  A crash that podman returns from is therefore released, not kept. This matches the registry,
  where a returned launcher is an ended session (the trampoline removes the record). The
  design's "crash with the record kept" case is the signal case.

- **L5.** A seat the launch's session already holds is kept, not taken again.

- **Release scope.** The exit releases every complete holding of the session in the checkout
  (`Holdings`), not the seats listed in `desired.json`. This also covers a seat whose leave
  timed out.

## For the coordinator / owner

- **Container bump.** RS2 lives in the image's `teamchat`. It needs the Dockerfile label and
  `REQUIRED_CONTAINER_VERSION` bumped (2.83.0), and I was told not to edit the Dockerfile. I
  messaged main. Until that bump, an old image posts `left exit` for a relaunch (RS1) and the
  host posts nothing for it. That is no worse than today.
- **Dogfooding fault, not filed yet.** The R-SECRET-BASH-MENTION guard blocked a Bash heredoc
  of Go source: a token spanning many lines matched the `*.vault-password` glob. The previous
  agent hit the same false positive. I worked around it with Edit, and it still needs filing.

## Left

- **Item 6: the seat sweep and its user units.** Neither `ccy-teams seats sweep` nor the
  `teamchat-seats.{service,timer}` units are built.
  - `teamchat/sweep.Sweep` exists, but nothing calls it.
  - The verb needs to walk each checkout under `pool/`, open a `seat.Store` per checkout, and
    provide a `sweep.Desired` that reads `control/<session>/desired.json`. That reader should
    treat a missing file as still desired, because a launch writes the file after its take.
    It matches the handle's seat part and the team.
  - The units need Go unit text and a play task in the owner's user directory, which the
    design notes as "not built yet".
- **Not done: a run in a real container.** No real ccy launch on a v2 team was run. That
  needs the host and a placed pool seat (Task 4.4, gate B).
