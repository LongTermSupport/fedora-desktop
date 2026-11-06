# Task 8.8, S14: the builder's report

Branch `feat/crossnet-s14`, from `feat/crossnet-s12-int`. X33 is built and tested in commit
`fafa17b13`; `6cf3553a2` fixes the v1 play names the gate refused. This report is committed after
both, with the gate results.

## What is built

- **`teamchat-admin team rotate-ca --team <team>`**, a root verb beside `team apply`
  (`admin/rotate.go`; it is in the verb table, `run_test.go`'s copy of C 3.3, and
  DESIGN-v2-C-host.md's root verb row). It takes the team lock with the host's guard, so a
  recorded restore refuses it (exit 70, naming `--resume` and `--discard`) and nothing changes.
  It reads the applied `team.json` (an unapplied team is refused), the names the last apply made
  the leaf for (`host.AppliedLeafNames`), the roster through the child's `team.view`, and the
  team's LXC record.
- **`host.CARotation.Rotate`** (`host/rotate.go`) does the rotation:
  1. It makes the new material: a CA made with `pki.Maker.NewCA` under the name apply uses
     (`caName`, now shared), and a leaf for the applied names made by **`leafStep.make`**.
     `leafStep.make` and `leafStep.files` are now part of S12's shared step: `leafStep.ensure`
     (apply and the health tick) calls the same two functions, so no second copy of the leaf
     code exists.
  2. It checks the material. The leaf must verify under the new CA and must not verify under
     the old one, and the two fingerprints must differ. It reads the bytes that every file to be
     replaced holds now.
  3. Only then does it replace anything. The order is: CA key, CA certificate, leaf key, leaf
     certificate, then the caller's files (the LXC record). Each file is replaced whole by
     `state.WriteFile`, with apply's modes (keys 0600, certificates 0644; nothing is widened).
     If a write fails, the old bytes of the failed file and of every file before it are written
     back, last first, and the error says "every file is put back and nothing is replaced". The
     failed file is put back too, because a write can land and still report failure. If a file
     cannot be put back, it is named, and the error says the files are mixed and must be
     restored from a team backup.
  4. Afterwards an active terminator is restarted. An inactive one is not started: it reads the
     new leaf when it starts. If the restart fails, the rotation stays in place, and the error
     says so; the health tick restarts a terminator that is older than its leaf (X31).
- **Output.** stdout is the handles of the seats the host does not place, one per line, and
  nothing else. It is empty when the rotation replaced nothing. stderr has:
  - the old and the new CA fingerprints (`old CA sha256:…, new CA sha256:…`; no key is ever
    printed);
  - the reissue pipe for each listed handle:
    `sudo teamchat-admin member reissue --team T --handle <handle> --credential password|token | [ssh <alias>] teamchat seat place-issued --checkout <dir>`;
  - the LXC seats that the next `lxc apply` reissues;
  - that the Element play (`paths.ElementPlayFile`) must be run again (see finding 4);
  - that a backup taken before the rotation holds the old CA, so after restoring one,
    `team rotate-ca` must be run again.
- **`team rotate-ca --help`** prints the verb's text on stdout. It covers the hard cut, the
  pipe, the LXC seats, the Element play, and the instruction to rotate again after restoring a
  backup taken before the rotation.
- **The LXC side** (`host/lxc/rotated.go`). `lxc.CARotated` marks each record entry that holds
  a handle and has not left as `deferred` with `lxc.WhyCARotated`. `member.needsReissue` now
  counts that reason, so the next `lxc apply` reissues the seat with a bundle that carries the
  new CA, and places it. `lxcstate.RecordMode` is exported so the rotation writes the record
  with the record's own mode (0640, owner group).
- **Production wiring.** `admin.System.RotateWrite` is set to `state.WriteFile` in
  `admin/wire`.

## How reachable and remote seats are told apart

**The records do not record "placed over ssh", and that flag would not help.** On this branch
the host places pool seats for no one. Every podman, docker, vm or host seat, on the homeserver
host or on another machine, reaches its pool through the owner's own process: the
`seat place-issued` pipe (X22; DESIGN-v2-C-host 3.7.3, "the owner's process places any bundle
itself"). The pool is the owner's. Root does not hold the credential, which goes from the child
to root's stdout and on to the owner. So a root verb cannot rewrite those seats even on the
homeserver host, wherever they were issued. Marking "issued for ssh" at issue time would change
nothing about what root can reach.

**The seats root does place are the LXC seats.** `lxc apply` places them through
`guest place-bundle` and records each one in the host's LXC record. So the split the verb uses
is the one the records already hold:

- **reachable**: the roster kind `lxc`, as recorded in `lxcstate`;
- **listed on stdout**: every enrolled agent member of any other kind.

Parked, lost and retired seats hold no working credential, and their next `return` issues a
bundle with the new CA, so they are not listed. That is a **FINDING**: the spec's premise, that
"the seats on the homeserver host" are reachable, does not hold for owner-placed seats. A
podman seat on the homeserver host is listed, and the owner places it again with the same pipe
without ssh.

**A second reading follows from this, and the coordinator may overturn it.** A placed LXC
bundle can be rewritten only with a credential (`guest place-bundle` takes the token), and a
reissue revokes the old one on the homeserver (`apply.run.credential` resets the password). A
reissue therefore cannot be undone if a later step fails. To keep "a failure leaves everything
as it was", the verb does not reissue. It marks the LXC seats in the record, and that record
write is part of the same all-or-nothing replacement as the CA and leaf files. The next
`lxc apply` makes the reissue and places the new bundle, through the path already tested for a
bundle that was never placed.

So "every reachable seat's `tls_ca` is the new CA" holds after the next `lxc apply`, not when
the verb returns. It is proven as a chain: the record is marked; a marked seat makes `joinOf`
answer `member reissue`; and `host.CAPEM`, which the bundles and the reach facts are issued
from, is the new CA. There is no end-to-end test through `lxc apply`.

## Tests

The X33 list, and where each item is tested:

| X33 expectation                                     | Test                                                                                                                                                                                                                                                                                                                                             |
| --------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| the new CA's fingerprint differs                    | `host` `TestX33ARotationMakesANewCAAndALeafOnlyItSigns`; `admin` `TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace`                                                                                                                                                                                                                  |
| the new leaf verifies under the new CA, not the old | `host` `TestX33ARotationMakesANewCAAndALeafOnlyItSigns`                                                                                                                                                                                                                                                                                          |
| every reachable seat's `tls_ca` is the new CA       | `lxc` `TestX33ARotationMarksEverySeatHoldingAHandleForAReissue`, `TestX33AMarkedSeatIsReissuedAtTheNextApply`; `admin` (record marked, `host.CAPEM` is the new CA)                                                                                                                                                                               |
| the remote seats are listed and no other            | `admin` `TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace` (enrolled podman listed; parked podman and the LXC seat not listed)                                                                                                                                                                                                       |
| an old bundle refuses the new leaf with X16's text  | `host` `TestX33AClientHoldingTheOldCARefusesTheNewLeaf`: a real `matrix` member client against `tlsterm` serving the new leaf; the error is `TLS verification of <addr> failed against the pinned CA sha256:<old>: unknown authority`; the new CA gets past TLS                                                                                  |
| a failure at each step leaves everything as it was  | `host` `TestX33AFailureAtEveryWriteLeavesEverythingAsItWas` (each of the 5 writes, failed and landed-then-failed, 10 cases); `TestX33AFailureWhileMakingWritesNothing` (randomness, names); `admin` `TestX33TheVerbFailingAtEachWriteChangesNothing` (the same 10 cases through the verb, the LXC record included, no handle printed, lock free) |
| a restore recorded refuses                          | `admin` `TestX33TheVerbIsRefusedWhileARestoreIsRecorded`                                                                                                                                                                                                                                                                                         |
| stdout holds only the handles                       | `admin` `TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace` (exact stdout)                                                                                                                                                                                                                                                            |
| stderr names the Element play                       | the same test                                                                                                                                                                                                                                                                                                                                    |

Other tests:

- the order of the writes: `TestX33TheFilesAreWrittenKeyFirstThenTheCallersOwn`;
- an inactive terminator is not started, and a failed restart leaves the rotation in place:
  `TestX33TheTerminatorIsRestartedOnlyWhenActiveAndAFailedRestartLeavesTheRotation`;
- a team with no usable CA is refused: `TestX33ATeamWithNoUsableCAIsRefused`;
- `--help`: `TestX33TheHelpSaysToRotateAgainAfterRestoringAnOlderBackup`;
- strict flags and an unapplied team: `TestX33TheVerbsRefusals`.

All tests run under `-race -shuffle=on`. The host tests use the world's fake clock and its
deterministic randomness; the terminator test uses `clock.NewFake`. No test waits on the wall
clock.

## Planted defects, each seen red and restored

Each defect was planted by a scratch script, which ran the X33 tests and wrote the file back.
Three plants first failed to compile (an unused import); each was planted again in a form
that compiles and was then seen red, as listed.

| Planted                                                   | Red                                                                 |
| --------------------------------------------------------- | ------------------------------------------------------------------- |
| the rollback skips the failed file (`j := i - 1`)         | `TestX33AFailureAtEveryWriteLeavesEverythingAsItWas` (landed cases) |
| no rollback at all                                        | `TestX33AFailureAtEveryWriteLeavesEverythingAsItWas`                |
| restart whether or not the terminator is active           | `TestX33TheTerminatorIsRestartedOnlyWhenActive...`                  |
| CA certificate written before the CA key                  | `TestX33TheFilesAreWrittenKeyFirstThenTheCallersOwn`                |
| leaf signed by the old CA                                 | all five host X33 tests                                             |
| leaf signed by the old CA, and the old CA kept as the new | all five host X33 tests                                             |
| the CA files left out of the replacement                  | all five host X33 tests                                             |
| a failed restart reported as success                      | `TestX33TheTerminatorIsRestartedOnlyWhenActive...`                  |
| `WhyCARotated` not counted by `needsReissue`              | `TestX33AMarkedSeatIsReissuedAtTheNextApply`                        |
| `CARotated` skips offline entries instead of left ones    | `TestX33ARotationMarksEverySeatHoldingAHandleForAReissue`           |
| LXC seats listed on stdout                                | `TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace`      |
| every roster state listed (parked included)               | the same                                                            |
| the LXC record not part of the replacement                | the same, and `TestX33TheVerbFailingAtEachWriteChangesNothing`      |
| handles printed after a failed rotation                   | `TestX33TheVerbFailingAtEachWriteChangesNothing`                    |
| the Element play left out of stderr                       | `TestX33TheVerbRotatesAndListsOnlyTheSeatsTheHostDoesNotPlace`      |
| the lock taken unguarded                                  | `TestX33TheVerbIsRefusedWhileARestoreIsRecorded`                    |

**Not proven by a plant.** The check in `checkRotation` that refuses "the new leaf verifies
under the old CA" cannot be reached on its own: a leaf from a fresh CA never verifies under the
old one. The plants above that reach it (leaf signed by the old CA) are also caught by later
assertions. That check is a guard, and no test proves it alone.

## Gate results

- By hand, at `fafa17b13`:
  - `go test -race -shuffle=on ./internal/teamchat/...`: passed.
  - `go test -race -shuffle=on -tags busfailpoints` over `host/...` and `admin/...`: passed.
  - `golangci-lint run ./internal/teamchat/...`: 0 issues, untagged and with `busfailpoints`.
  - `gofmt -l`: clean.
- `./scripts/qa-changed.bash --base feat/crossnet-s12-int`, run in a detached worktree:
  - at `fafa17b13`: exit 1. docs passed; go (go-qa, both modules, diff mode) passed;
    `test-ccy-container-version-hook.bash` passed 121; `teamchat-keeps-off-v1` FAILED, with 4
    v1 references (the v1 Element play's file name and "agent-bus play") in `admin/rotate.go`
    and `rotate_test.go`. Fixed in `6cf3553a2`, which uses `paths.PlayFile` and
    `paths.ElementPlayFile`; the Element-line plant is red again after the fix.
  - at `6cf3553a2`: exit 0. `✓ qa-changed passed: 16 changed file(s)`: docs, go,
    teamchat-keeps-off-v1, and test-ccy-container-version-hook.bash (121).
  - The known `qa/cmd/qa-js` failure (issue #135) did not come up: go-qa's diff mode did not
    run that package.

## Readings where the spec was silent

- **The verb's name and place.** It is `team rotate-ca`, a team verb run by root. Its lock
  verb is `team.rotate-ca`, because the lock's grammar has no space. It waits `host.PlayWait`,
  as the team verbs do.
- **The names of the new leaf** are those the last apply made the leaf for, from `units.json`
  and `team.json` (`host.AppliedLeafNames`), the same source as the tick. A rotation never
  derives listen addresses itself.
- **The old leaf must be there.** A team with no leaf, or with a CA that apply would refuse
  (for example a lone certificate), is refused: "team apply makes it".
- **Fingerprints are the log.** The verb has no log file. The fingerprints go on stderr, which
  the caller can keep; the status record is refreshed but not given an event.
- **A crash between two writes** (power loss, not an error) can leave the files mixed. This is
  the same window apply's two-file CA write has. `loadCA` then refuses a CA key and certificate
  that do not match, by name, and a leaf the CA did not sign is made again by the next apply.
  The recovery for a mixed CA is to restore a backup, then rotate again. No journal was added
  (YAGNI). The coordinator may want one.
- **The backup.** The verb does not take a backup. The next scheduled backup carries the new CA
  (`host.CABackupFiles` reads the files on disk). The help and stderr say that a backup taken
  before the rotation undoes it.

## Findings

1. **Owner-placed seats are not reachable by root**, even on the homeserver host (above). The
   spec's "seats on the homeserver host" should read "the LXC seats, which the host places".

2. **`admin/wire` still serves `teamCANotMade`** for `System.TeamCA` and for `SeatBundles.CA`
   ("until slice S7a"). In the production build, no `https` bundle or LXC reach fact can be
   issued at all, before or after a rotation. `host.CAPEM(root, team)` is the provider it
   needs. This was not changed here, because `wire_test` pins the current behaviour and the
   change belongs to the slice that owns it. Until it is fixed, the LXC reissue that X33 relies
   on fails in production at the bundle step.

3. **The LXC seats' reissue revokes their old tokens.** Between the rotation and the next
   `lxc apply`, those seats are cut off anyway (the hard cut), so nothing more is lost. But the
   reissue is not undoable, which is why the verb does not make it itself.

4. **The Element play's name.** The verb names the plays with v2's names, `paths.PlayFile`
   and `paths.ElementPlayFile` (`play-teamchat-element.yml`). The first draft wrote the v1 file
   name, and `teamchat-keeps-off-v1` refused it. But X34 was built into v1's
   `play-agent-bus-element.yml`, and `play-teamchat-element.yml` does not exist yet. Until the
   Element step is ported, the play the verb names is not the play that holds X34's tasks.

## Not done

- No end-to-end test through `lxc apply` after a rotation: the chain is proven piecewise (see
  above).
- No host run (no v2 `https` team is deployed), so nothing is queued in `meta-deploy.list`.
- SPEC-cross-network X33's "as built" paragraph and PLAN.md's task state are left to the
  coordinator, as for S12.
