Verdict: FIX-FIRST

# Plan 00209 N0 area D: adversarial review, round 2

Reviewed: `DESIGN-v2-D-migration.md` at `caabbbf6` against `DESIGN-v2-FRAME.md`, the common
designer rules, CLAUDE.md, round 1's report, and the v1 code. `D:n` means a line of
`DESIGN-v2-D-migration.md`. Other paths are relative to the worktree.

Count: 2 blockers, 4 majors, 7 minors. Both blockers come from v1 facts the rewrite now relies
on that the v1 code contradicts. Most of round 1 is properly fixed.

## Round 1 status

| #   | Finding                                       | Status | Evidence                                                                                                                                                                                                                                                                                                                  |
| --- | --------------------------------------------- | ------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Rollback gives no working token               | PARTLY | The real verb is named (D:669). D17 is restated (D:45, D:679-680). P1 asserts a v1 send after R (D:893). But neither route works as written: `add-member --out=<existing dir>` is refused, and v1's LXC return deletes the guest's `state/`. See new findings 1 and 2.                                                    |
| 2   | Fence destroys the cut point; no device verb  | FIXED  | The cut is taken from `sync.json` in the snapshot. Remote and unreadable members use `last_send`/`join` with `possible_duplicate` (D:431). The device check is replaced by a whoami 401 proof (D:607 e). `/account/whoami` is a real v1 call (`helpers/agent_bus/admin.py:353-354`, `helpers/pingbus/matrix.py:241-242`). |
| 3   | Commit not atomic; set record unreadable      | FIXED  | One set record lives in the status dir, readable by the stamp group (D:176, D:511-514). Copies are derived from it, with a repair rule (D:550-559). Fault injection covers the gap (D:892). A residual ambiguity is new finding 9.                                                                                        |
| 4   | `human lock` may not end sessions             | FIXED  | The fence is lock plus `logout-all` (D:607 b); `logout-all` exists (`helpers/agent_bus/cli.py:42,141-142`). The M3 rule is precise (D:608, D:440). M8 re-checks (D:613). P1 probes lock alone (D:892). The rewrite introduced new finding 3.                                                                              |
| 5   | v1's own notices during the fence             | FIXED  | Row 7 gives them a fate (D:437), D:847-851 covers them, and P1 has a proof. Residual: new finding 7.                                                                                                                                                                                                                      |
| 6   | Inject records in a running container         | FIXED  | Names are read through `podman exec` as the user (D:434, D:651). Sessions that are not running are named in the report.                                                                                                                                                                                                   |
| 7   | Root reads can follow symlinks                | FIXED  | Seat state is read as the user. The guest is read with `RESOLVE_IN_ROOT` (D:649-650). Regular files only, `O_NONBLOCK`, size caps, and no content in errors (D:655-659). Red tests are named.                                                                                                                             |
| 8   | Static gate fails on v2's own units           | FIXED  | `v1names` and the allowlist are the homes for v1 references, and the allowlist cannot go stale (D:298-307). Shared code is guarded by v1's suites (D:308-311). A green fixture is named (D:325-326).                                                                                                                      |
| 9   | Checkout `.gitignore`, launcher, `cc --teams` | FIXED  | v2 has its own ignore file (D:194). The manifest covers `.claude/ccy/` and the image files (D:359-363). `cc --teams` refuses v2 teams and M0 refuses host members (D:267-269). v1 acceptance runs through both launchers (D:373-374).                                                                                     |
| 10  | No guest side to the footprint                | FIXED  | A guest manifest exists (D:364-366), and the image is compared by listing and sha256 (D:361-363).                                                                                                                                                                                                                         |
| 11  | Declaring a move lets a plain deploy build it | FIXED  | `teamchat_migrations` is D's own list (D:212, D:794-797), and C gates the shared name on migration state (D:397, D:1015-1017).                                                                                                                                                                                            |
| 12  | Element profile collision                     | FIXED  | The validator refuses, and the derived-name unit test checks it (D:218-225).                                                                                                                                                                                                                                              |
| 13  | `moved` notice contradicts a carried answer   | FIXED  | The settle set is built after the carry set, with a reply cross-check (D:435). P1 seeds the case (D:892).                                                                                                                                                                                                                 |
| 14  | `seat remove` runs as the user                | FIXED  | It runs per checkout as the user, its destruction of state is stated, and LXC is addressed (D:613, D:672). Residual: new finding 11.                                                                                                                                                                                      |
| 15  | Two confirmations                             | FIXED  | There is one confirmation, before M2 (D:600-601). M3 no longer prompts.                                                                                                                                                                                                                                                   |
| 16  | Rollback per phase and re-runs                | PARTLY | R is now per phase (D:724-727) and idempotent through `list` plus whoami (D:729, D:738-741). But a crash inside M2 is not covered (new finding 4), and the phase range is inconsistent (new finding 10).                                                                                                                  |
| 17  | Team names over 23 characters                 | FIXED  | M0 refuses them (D:605). The grammar is stated (D:180-181, D:1010-1011).                                                                                                                                                                                                                                                  |
| 18  | Missing name categories                       | FIXED  | Rows were added at D:176-178 and D:195.                                                                                                                                                                                                                                                                                   |
| 19  | Seam overreach                                | FIXED  | 5.2.1 now defers to section 12 (D:451-454). D:232 and D:330 are phrased as D's requirements on C, which is acceptable for coexistence.                                                                                                                                                                                    |
| 20  | Inbox items delivered after the snapshot      | PARTLY | The argument is now stated (D:422-427), but its citation is wrong, and one local path is not covered (new finding 8).                                                                                                                                                                                                     |
| 21  | Alternatives                                  | FIXED  | Commit (D:566-576), session move (D:853-858), fence (D:622-635) and proof (D:380-389) each have alternatives.                                                                                                                                                                                                             |

## New findings

### 1. Blocker: `ReturnSeat` cannot write into the seat's existing directory, because v1's wrapper refuses an `--out` that exists

- **Evidence.**
  - D:669 and D:730-731 have `ReturnSeat` run `add-member ... --out=<seat dir>` into "the seat's
    existing directory", citing `files/usr/local/bin/agent-bus:8-15,35`.
  - The same wrapper refuses that: for `add-member`, `[[ ! -e $out && ! -L $out ]] || die ... "$out already exists; add-member creates a new bundle directory"`
    (`files/usr/local/bin/agent-bus:96-99`).
  - The seat directory is the bundle directory. It also holds `seat.lock` and `state/`
    (`helpers/pingbus/seat.py:232-237`, `helpers/pingbus/config.py:106-111`). The fence's
    `park-member` leaves it in place, so at R it always exists.
  - The other v1 routes do not fit either:
    - `--out=-` gives a tar that the caller must place. Placing it would be v2 code writing v1
      files, which breaks D17.
    - `seat remove` then `seat take` rmtrees the whole seat dir, `state/` included
      (`helpers/agent_bus/checkout.py:1009`).
    - `rotate-token` refuses a parked member (`helpers/agent_bus/admin.py:850-851`).
- **Effect.**
  - Every ccy seat's return fails with exit 64, so R cannot complete for any team with a ccy seat.
  - The safety paragraph at D:758-761 ("the three bundle files that v1's own `add-member --out`
    replaces") is false.
- **Required fix.**
  - Add a second listed v1 change, tested in v1's suite, beside `SetRoomsReadOnly` (D:675-677).
    For example, `agent-bus seat return SEAT@TEAM`, run as the user in the checkout under the seat
    lock. It calls `add-member --seat ... --out=-` and replaces only `member.json`, `token` and
    `README`, each by a rename, leaving `state/` alone.
  - Or extend the wrapper with an `--replace-bundle=DIR` mode, under the same ownership checks
    that `rotate-token` uses (`agent-bus:101-105`).
  - Update 5.5, 5.7 step 1, the safety paragraph, D17's wording and RK1.

### 2. Blocker: v1's LXC apply deletes the guest's v1 `state/` when it returns a parked member, so LXC rollback loses unsynced items by design

- **Evidence.**

  - D:733-735 relies on `present_plan` for a member that is not active. It does return `add=True`,
    but also `remove_bundle=found.exists` (`helpers/agent_bus_lxc/members.py:640`).
  - `_join` then runs `_remove_bundle` before placing the new bundle (`members.py:1050-1051`).
    `_remove_bundle` runs `REMOVE_PY`, which does `shutil.rmtree` of `~/.config/pingbus/<team>`
    (`members.py:285-292, 989-990`).
  - That directory is the member's `bundle_dir`, and `state/` is its child
    (`helpers/pingbus/config.py:110-111`). `ReadGuestState` reads exactly that `state/` (D:650).
  - `PLACE_PY` then makes the directory fresh with `os.mkdir` (`members.py:274-281`).

- **Effect.**

  - For every LXC member, R deletes `sync.json`, `inbox/`, `consumed/` and `outbox.json`.
  - R step 4 (D:744-748) then always stops at its "alarm". RK1 (D:954) is not a risk but a
    certainty. v1's first sync after that uses `limit 0`, so the member's unsynced and synced but
    undelivered items are gone from v1. The only fallback is "tell members by hand", which is the
    silent-loss class that D8 and D9 forbid.
  - The pilot would find this, but the design states the opposite as its safety argument (D:758-761).

- **Required fix.** Give the LXC return a path that keeps `state/`. Two options:

  - a v1 change, listed and tested in v1's suite: for a parked member whose existing bundle names
    the same handle, replace only the three bundle files;
  - or a v1 verb the adapter calls per guest.

  Then correct D:733-735, D:758-761 and RK1, and keep P1's byte-identical assertion as the test
  of it.

### 3. Major: after `logout-all`, humans have no v1 password, but R and M8 both assume they log in again

- **Evidence.**
  - `human_logout_all` resets the password to a generated, discarded one: "the password is
    replaced by a discarded one too" (`helpers/agent_bus/admin.py:892-896`).
  - R step 3 (D:742-743) says the humans "log in again in their v1 Element window with their
    existing password". That password no longer exists.
  - M8 (D:613) and 5.6 (D:708-710) unlock the humans "so that they can log in and read" the
    archive, but give them no password. Their v1 Element profile was logged out at M2. The A9
    look at the screen (D:829) then fails.
- **Required fix.**
  - R step 3 and M8 issue `agent-bus human password` per human (an existing verb, `cli.py:42`),
    shown once through ccy-teams' login flow. Add it to the 5.5 verb list.
  - Correct RK10 (D:963), which says only "a fresh login".

### 4. Major: a crash inside M2 leaves members parked with no `fence.json`, and R from `staged` does not touch v1

- **Evidence.**
  - M2 writes `fence.json` last, at (g), after the locks, logouts and parks of (b) and (c) (D:607).
  - The phase only becomes `fenced` when M2 completes. R from `staged` is "nothing was done to
    v1: discard the v2 side" (D:724).
  - R's return list is "every member in `fence.json` that `agent-bus list` still shows as
    parked" (D:729). The `--repo/--host/--type/--role/--address` values the return needs are
    only in `fence.json` (D:520-522).
- **Effect.** If the run is killed or fails between the first park and (g), and the owner chooses
  R, v1 is left with locked and logged-out humans and parked members, and nothing records how to
  return them. D8 is violated. The P1 fault injection "at every step" (D:892) would probably catch
  it, but the design asserts the opposite.
- **Required fix.**
  - Write `fence.json` as an intent record before (a), with every member's return values from the
    M0 snapshot. Update it per member as each verb completes.
  - Or add a `fencing` phase that R treats like `fenced`.
  - Let R from any phase at or after `staged` consult `fence.json` and `agent-bus list`.

### 5. Major: A8 can never pass for a team with an LXC member

- **Evidence.**
  - A8 allows footprint-fence and the fresh footprint to differ only by "the removed LXC env
    lines and v1 services, and the `state: absent` LXC entries" (D:828).
  - The footprint includes the guest manifest, `~<user>/.config/pingbus/**` (D:364-366, D:377-378).
  - v1's absent handling, run in M6, removes the guest's whole `~/.config/pingbus/<team>/`
    (`absent_plan` sets `remove_bundle=found.exists`, `members.py:643-645`; executed at
    `members.py:1319-1320`). It also parks the handle again.
- **Effect.** A8 fails deterministically, and "fixed forward on v2" (D:612) cannot fix a v1
  change that M6 itself made.
- **Required fix.**
  - Add the removed guest bundle directories, and v1's registry `parked` changes, to A8's allowed
    difference.
  - Generate the allowed difference from M6's own log of what it changed, not as a hand list.

### 6. Major: after R, LXC (and host) members keep running with no v1 watcher

- **Evidence.**
  - At the fence, every v1 watcher's next sync gets 401. Exit 8 is in `SUPERVISE_FINAL`, so the
    supervisor exits for good (`helpers/pingbus/cli.py:2118, 2152-2155`).
  - R step 6 (D:752-753) restarts only ccy sessions.
  - v1's LXC play restarts the guest unit only when the unit file changed
    (`members.py:1101-1116`), and placing a bundle does not change it.
- **Effect.** After R, a running LXC member has a working bundle (its sends succeed, which is
  what P1 asserts at D:893) but no watcher. Its restored inbox and new pings are not delivered
  until something restarts the session. The items are not lost, but D8's "every queued item still
  deliverable" holds only on paper. P1's "delivered by v1 exactly once" would expose it.
- **Required fix.**
  - R restarts each running LXC member's v1 session, through a listed v1 route: a v1 play option
    or a guest `systemctl restart agent-bus-claude-<user>`, made a v1 verb.
  - Or R lists each such member in its report with the restart command.
  - Host `cc` members are excluded by M0, so they need nothing.

### 7. Minor: row 7's report timing is wrong, and the launcher skip is unneeded

- **Evidence.**
  - The carry plan's `reported` rows, including `v1_notice_failed`, are built and printed at M3
    (D:505-507). But the failures they list "from `disconnect.log`" (D:437) happen in M6, when the
    containers exit.
  - v1's `ccy_bus_disconnected` is already bounded by `timeout` and never blocks a restart: "A
    failure is not the launcher's: the session has ended, and a restart still goes ahead"
    (`files/var/local/claude-yolo/lib/agent-bus-membership.bash:87-111`).
  - So the new launcher skip (D:437, D:847-849, D:1027-1028) is a ccy change and a version bump
    with no defect to fix. It also removes the very log lines the report claims to list.
- **Required fix.**
  - Either drop the skip, or keep it with a stated reason (fewer 401s in the journal) and stop
    citing `disconnect.log` for the post-commit window.
  - Move the `v1_notice_failed` rows to the M7 or M8 report.

### 8. Minor: wrong citation for the re-fetch, and a local TIMEOUT path not covered

- **Evidence.**
  - D:424-425 cites `helpers/pingbus/cli.py:1120-1133` for "v1 prints an inbox item only after
    fetching the event again". Those lines are `_read_side_text` and `_no_token`. The re-fetch is
    `_refetch` and `_deliver` (`cli.py:1348-1365, 1378-1417`).
  - `_deliver` also emits "every TIMEOUT due" from local state (`cli.py:1381`).
- **Required fix.**
  - Correct the citation.
  - State, with a citation, that a fenced session cannot reach the TIMEOUT emission without a
    server call that first fails with 401. Otherwise row 5's `TIMEOUT` settle (D:435) can
    duplicate one that v1 already printed, and R step 4's byte check on `outbox.json` would alarm.
  - Add an overdue ping in a running session during the fence to P1's seeds.

### 9. Minor: 3.9's v2-native rule needs host_vars, and a re-migration leaves two set records naming the team

- **Evidence.**
  - D:248 makes a team v2-live when "the team is in `teamchat_teams` and was created on v2". But
    D:569-570 rejects host_vars parsing by the launcher.
  - After R, the set record is kept at `rolled-back` (D:750-751). A second migration of the same
    team creates another set record, and the design does not say which one decides.
- **Required fix.**
  - Decide v2-native from the status directory alone, for example a `<team>.json` with no set
    pointer.
  - Make `<team>.json`'s set pointer the only way to find the deciding set record.

### 10. Minor: R's starting phases are inconsistent

D:722 says R runs "from any phase from `staged` to `carried`". D:724 then handles "From
`preflighted` or `staged`", and the diagram (D:533-537) starts R at `staged`. Pick one range. If
`preflighted` is included, say that it only discards the migration state.

### 11. Minor: M8's `seat remove` needs the v1 server and sudo

`seat remove` parks each handle through `sudo agent-bus park-member` before it deletes the
directory (`helpers/agent_bus/checkout.py:996-1009`). M8 therefore needs the v1 archive server
running and the owner's sudo, from each checkout. That is fine under OD-D6 (a), but under (b) or
(c) the server is stopped at M8. State that `seat remove` runs before any archive-server stop,
and that it re-parks, harmlessly, a handle that is already parked.

### 12. Minor: `WhoAmI` "runs as user", but the LXC tokens are in the guest rootfs

D:652 runs `WhoAmI` as the user. M2 (e) (D:607) and R step 2 (D:738) need it for LXC members,
whose token is only readable through root's `ReadGuestState`. Say that the LXC check runs as root,
with the token read through the confined guest read and never logged.

### 13. Minor: commit as "rename ... from phase `carried` to `committed`" reads as renaming one file to another

D:541. Say that a new set record with phase `committed` is written to a temporary file and renamed
over the old one (as D:528 describes for state files). Name the precondition re-check as being
done under the set's lock, so that two `migrate` runs cannot interleave.
