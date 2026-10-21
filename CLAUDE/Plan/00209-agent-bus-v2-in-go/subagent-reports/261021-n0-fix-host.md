# Plan 00209 N0 fix pass, theme host (F3): disposition

Branch `feat/bus-v2-n0-f3-host`, cut from `feat/bus-v2-n0` at cf5c57d9. Two fixers worked it: the
first to e5fc8a32 (handoff `261021-n0-fix-host-handoff.md`), the second from there to the tip. The
decisions the handoff records were taken by the first fixer and not re-decided.

Owned and edited: `DESIGN-v2-C-host.md` (all of it), B 2.13 (LXC guest operations, first fixer
only), and these DESIGN-v2.md rows:

- seam contract: 17, 20, 24, 25, 27, 28, 32, 46, 53, and new 57 (the health tick's LXC
  disconnected) and 58 (`restore_epoch`);
- decisions: DC8, DC16, DC17, BD9, BD13, INT1, and new DC18, DC19, DC20;
- section 8 trace: R3, R10, R14.

Counts: 37 findings fixed, 0 rejected, 1 sent on as a cross-fixer request (security 12). Of the
other fixers' requests to C, every one is applied except two parts named below.

## Coherence (`261021-n0-review-coherence.md`)

| #   | Disposition                 | Where                         | Change                                                                                                                                                                                        |
| --- | --------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | fixed                       | C 3.9.2, B 2.13               | an LXC member becomes active through `guest membership` (the agent's own desired file) and its removal waits for the seat before `guest remove` and `member park`                             |
| 2   | fixed                       | C 3.9.1, 3.9.2, 3.9.4, B 2.13 | one mechanism: root writes the kit and the unit host-side; the agent user runs `guest check\|place-bundle\|membership\|remove`; `install-self` and `unit` ops deleted                         |
| 5   | fixed                       | C 3.6.3, 3.6.4                | the admin posts exactly `added parked removed role`, plus D's carried items and welcome                                                                                                       |
| 7   | fixed                       | C 3.6.1, 3.6.3, 3.6.4         | every member and role verb ends with the rooms step and the notice; the roster event derived per change; a parked coordinator is kicked from questions                                        |
| 8   | fixed (C part); B part to B | C 3.6.1, 3.7.3                | `internal/teamchat/state` and `internal/teamchat/seat`                                                                                                                                        |
| 15  | fixed                       | C 3.6.3, 3.7.2                | `human_text` lives in the roster only; dropped from `seat.json`                                                                                                                               |
| 16  | fixed                       | C 3.7.5, C 7 (B item 4)       | the worker posts `left released` and the seat's processes end; the keeper makes no network call                                                                                               |
| 17  | fixed                       | C 3.3                         | the `migrate` verbs are one per root act of D's steps (`preflight stage fence fence-status capture carry commit accept archive archive-check unfence rollback`); D 5.4 names the owner's acts |
| 18  | fixed (R3, R10, R14)        | C 3.2.5, DESIGN-v2 section 8  | no TLS stated; R10 cites C30; R14's 180 s wait is C 3.7.4. R19, R31, R41, R43, R44 are the member and protocol fixers'                                                                        |
| 19  | fixed (health part)         | C 3.11.2 step 3, C 3.6.4      | the questions room checked item by item (no worker, coordinator joined, form); the open/closed switch and re-invite in 3.6.4. Skill parts are not host                                        |
| 20  | fixed                       | C 3.6.2                       | `Desired(team, roster, rooms)` asked again after each create; A 5.2 sentence requested of A                                                                                                   |

Also touched: coherence 10's C part (C 3.9.4 now runs `teamchat run --kind lxc` directly, no
`teamchat-claude`), following the member fixer.

## Security (`261021-n0-review-security.md`)

| #   | Disposition     | Where                                  | Change                                                                                                                                                                                                                                                     |
| --- | --------------- | -------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | fixed           | C 3.7.1-3.7.3, 3.7.2, DC16, K13, H-C13 | DC16 (c'): the pool keeps a password; each take logs in with the seat's fixed device id, which kills the previous token; each release logs out; `released_ms` and `claim` in the session's `seat.json` bound the store-less start; K13 restated and closed |
| 2   | fixed           | C 3.5.1, DC18, C46, K9                 | security fields compared with a root-written `approved.json`; `SECURITY` lines in their own task, the play fails naming `ccy-teams approve`; `team apply` re-checks as root                                                                                |
| 3   | fixed           | C section 1, C10, DC19, K12            | C10 narrowed (the password does not guard seat placement); K12 widened (re-seat sessions, change desired teams); DC19 offers a pool under its own uid, default not built                                                                                   |
| 4   | fixed           | C 3.11.1, K14, H-C10                   | `CAP_SYS_PTRACE` in the health unit only, `ptrace`, `process_vm_readv`, `process_vm_writev` filtered; the other option weighed and rejected; H-C10 inside the sandbox                                                                                      |
| 6   | fixed           | C 3.7.1, C 7 D item, seam 53           | the launcher refuses any mount whose real source is an ancestor of, equal to, or inside the seat store root other than the two binds; VM-lab test                                                                                                          |
| 7   | fixed           | C 3.12.2 step 4, H-C5                  | staging in a transient unit with `PrivateNetwork=yes`: no port reachable from the host; no staging port                                                                                                                                                    |
| 8   | fixed           | C 3.9.1, K15                           | host-side writes refuse bind mounts and non-root directories (`RESOLVE_NO_XDEV`, ownership check); agent uid 0 or the desktop user's uid refused                                                                                                           |
| 9   | fixed           | C 3.10, C34                            | consent required for every Element profile write, with the text stating the exposure                                                                                                                                                                       |
| 11  | fixed (stated)  | DC1, DC2                               | one `teamchat-ctl` uid holds every team's admin token: DC1's per-team scope holds at the homeserver, not the controller                                                                                                                                    |
| 12  | cross-fixer (D) | D 3.5                                  | D's path row should point at C 3.2.2                                                                                                                                                                                                                       |
| 13  | fixed           | C 3.8, DC17                            | a go-deps rule keeps `go.yaml.in/yaml/v3` out of `teamchat-admin`; godbus reads capped and typed, guest PID 1 treated as untrusted                                                                                                                         |
| 14  | fixed           | C 3.2.5, K16                           | the bridge subnet only for teams with an LXC member; `allow_from` is not a team boundary; the token and login limits are. A 6.9 row requested of A                                                                                                         |
| 15  | fixed           | C 3.5.1 step 6                         | the pin printed in its own task when it changes; `update-versions` cross-checks upstream's published checksum                                                                                                                                              |

Security 5 and 10 are the migration fixer's.

## Operations (`261021-n0-review-operations.md`)

| #   | Disposition         | Where                            | Change                                                                                                                                                                                                      |
| --- | ------------------- | -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | fixed (C part)      | C 3.9.4, C 3.2.3, C 7 B item     | the LXC unit runs `teamchat run` in tmux with the default `KillMode`, `TimeoutStopSec=30`; homeservers `Before=lxc.service` so guests stop first; B's `run` handles the stop                                |
| 2   | fixed               | C 3.7.5, 3.11.2 step 6, C43, BD9 | ccy: the launcher (and the board) announce a crashed session's seats at `podman` exit and keep them held; LXC: the health tick posts with the guest token copy, `tc-disc-<run id>`                          |
| 3   | fixed               | C 3.12.2, C47, H-C14             | `restore_epoch` in the roster event; workers read it by a direct state request after every outage; lost events exported and counted; a backup older than `carried_at` refused without `--accept-carry-loss` |
| 4   | fixed               | C 3.12.2                         | record first; a failure before `M` discards the staging; from `M` on the staged copy is kept as `staged`, with `--resume` and `--discard`                                                                   |
| 5   | fixed               | C 3.12.2 step 5, DC8             | the staged copy is secured (revocations, roster event) before the swap; the block covers only the renames                                                                                                   |
| 6   | fixed               | C 3.9.2, B 2.13                  | with coherence 1 and 2                                                                                                                                                                                      |
| 8   | fixed (C part)      | C 3.9.2 step 3                   | a migrated member still on v1 is `DEFERRED (v1 not yet absent)`, read through D's `ReadGuestV1Session`                                                                                                      |
| 14  | fixed (C part)      | C 3.6.6                          | `sudo -n -v` before each root call and every 60 s while a change waits; D's part is the migration fixer's                                                                                                   |
| 17  | fixed               | C 3.12.1                         | D's migration files are not in the backup and stay on the same disk (DC14)                                                                                                                                  |
| 18  | fixed               | C 3.12.2 merged roster           | compared by `seq` (`cred_seq`, `revoked_seq` against the backup roster's `seq`), never by a clock                                                                                                           |
| 19  | fixed (health part) | C 3.11.2 step 4, 3.11.3          | free space on `/var/lib` is a problem on every team's record; `disk` in the record                                                                                                                          |
| 22  | fixed (play part)   | C 3.5.1                          | the play never touches `teamchat-fence@` instances; VM-lab test. The rest is the migration fixer's                                                                                                          |
| 24  | fixed               | C 3.15                           | C 3.15 is the one statement of resident cost; timers enabled only while a team is present; DESIGN-v2 section 6 and D 8 to cite it                                                                           |

## Quality (`261021-n0-review-quality.md`)

| #   | Disposition | Where              | Change                                                                                                                                                    |
| --- | ----------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 6   | fixed       | C 3.6.5, 3.3, DC20 | `teamchat-admin redact --team T --event ID --reason secret`, password sudo, by the admin only; the answer names the backups and export that keep the text |

## Other fixers' requests to C

- Migration 1-5: applied (C 3.9.2 `ReadGuestV1Session`; C 3.12.2 `carried_at`; C 3.3 verb list with
  `accept`; C 3.11.2 free space; C 3.8's id rule was already there).
- Delivery 1-8: applied (redact; `added`; `role`; one roster write then the rooms step; bootstrap
  order; no `human_text` in `seat.json`; the network not a boundary and the bridge subnet for LXC
  teams only; no `proto` or `features` key in the roster, C 3.6.1).
- Member 3: the LXC unit applied (C 3.9.4), with the default `KillMode`. The ccy `entrypoint.sh`
  and `podman run --stop-timeout 20` are the launcher's text, not C's: sent on to the build session.
- Member 4: applied for ccy (launcher at `podman` exit) and LXC (the tick, with a host-held guest
  token copy). Host-kind seats are refused until N5, so their bundle copy is left to N5.
- Member 5: applied, as DC16 (c') rather than (c): the fixed device id makes a logout enough, and a
  device per claim deleted by UIA is the fallback.
- Member 6: `ccy-teams restart` on an LXC member (`lxc restart`, C 3.9.4) and the goodbye wording
  applied. Homeservers stop after LXC guests (`Before=lxc.service`); ordering them against the
  owner's user manager is **rejected**: it would hold the desktop login behind a homeserver's
  start, and a ccy member that loses the race is announced as a crashed session.
- The member fixer's `seat disconnected --bundle DIR --since MS --seed ID` is the signature C now
  calls (C 3.7.5, 3.11.2, C 7).

## Cross-fixer requests

**To the member fixer (B):**

1. B 2.10 last paragraph: an LXC member's desired membership is written by `guest membership` as the
   agent user, not root-owned; the header's `<v2-desired>` LXC value says so; BD13's text to match.
2. B 2.15 guest-ops test row and B 6 items: no `sha256sum`, no `install-self`, no `unit` op.
3. `run` handles the LXC unit's stop under the default `KillMode` within its end bound (C 3.9.4).
4. A store-less start begins at the earlier of the member's own read receipt and `seat.json`'s
   `released_ms`; B reads `claim`, `released_ms` and `device_id` from `seat.json` (C 3.7.2).
5. Disconnected: `seat disconnected --since --seed` uses `tc-disc-<seed>`; the host passes the
   claim id (ccy) or the run id (LXC, the tick's child calling the same code as a package); the next
   LXC worker checks the room and uses `tc-disc-<run id>`; the run id is in the seat lock file in a
   grammar the host checks.
6. A restored ccy session the host announced posts A's `back` notice (the member fixer's item 7).
7. `restore_epoch`: on a change, drop `next_batch` and resume as a store-less seat; after every
   homeserver outage read the roster by a direct state request, not by sync (C 3.12.2).
8. `human_text` from the roster in B 2.6 step 10.
9. Package names `internal/teamchat/...` (coherence 8, B part).

**To the protocol fixer (A):**

1. The content of the admin's `added` and `role` notices.
2. `restore_epoch` in the roster (A 5.1), raised only by a restore.
3. A 5.2: the bootstrap order sentence (`Desired` asked again after each create).
4. A 4.2 and A 12.2: the admin, and only the admin, redacts one event, reason `secret` (DC20).
5. A 6.9: a threat row saying `allow_from` is not a boundary between teams on one host.
6. The `back` notice (with the member fixer's item 7).

**To the migration fixer (D):**

1. D 3.5's path row points at C 3.2.2 (security 12).
2. D 5.6: the export is not in C's backup and stays on the same disk (ops 17).
3. D 8 cites C 3.15 for resident cost (ops 24).
4. Confirm C 3.3's `migrate` verb list, and that D 5.4 names which acts run as the owner.
5. The set record's carry time is named `carried_at` (C 3.12.2 step 1).

**To the build session:**

1. The frame's "nothing an agent in a cc session can run changes membership" is narrowed per DC19.
2. DESIGN-v2 section 6 cites C 3.15.
3. The launcher (D, with B, N4): at `podman` exit, announce each seat whose lock is free for a
   session whose record remains and keep it held (C 3.7.5); the mount refusal of C 3.7.1; the
   member fixer's `entrypoint.sh` exec of `teamchat run --kind ccy` and `--stop-timeout 20`.
4. The "worker" to "link" sweep covers C's new text (3.7.5, 3.11.2, 3.12.2, section 7).
