# Issue 248: a token seat placed into a ccy pool

Question: should `teamchat seat place-issued` accept a bundle issued with `--credential token`
for a ccy (pool) seat? Verdict: **no, not by design as it stands.** Nothing was built. The
intended path for a remote ccy seat is a pool **password** seat. A pool seat that holds no
password is DESIGN-v2.md 7.1 (d). The owner declined it at gate B, and it is a different
credential kind (`pool_token`), not `token`.

## The design record

- **INT6 / gate B (Plan 00209).** PLAN.md decision table, row INT6: "the owner chose the
  default 2026-10-30 (gate B met): owner-uid processes may hold a pool seat's password;
  DESIGN-v2.md 7.1 is not built". DONE-n0-n1.md, Task 0.6: "The owner's gate A and gate B
  answers (2026-10-30): A2 default (INT6 as built)".

- **DC16 (c') and DC19 (a)** (DESIGN-v2.md section 7, accepted). A pool seat keeps a
  **password** that is never mounted. Each take logs the seat's fixed device out and logs in
  again with that password. Each release logs out.

- **DESIGN-v2.md 7.1, (d) tokens only.** This is the one design for a pool seat with no
  password. It does not reuse `--credential token`. It adds a third kind,
  `--credential pool_token`, for two reasons:

  1. The roster's `cred` value must tell a pool seat from a guest seat. A restore marks every
     `password` (and, under 7.1, `pool_token`) handle `needs: reissue` and reissues those that
     have a pool record. Token handles are guest seats, and `lxc apply` handles them.
  2. A `token` issue writes the guest token copy `ctl/<team>/guest/<handle>.token`
     (`apply/member.go`, `credential` then `guestCopy`). The health tick reads that copy to
     announce LXC guest seats. A pool seat must not have one.

  7.1 also changes take step 3 (no login: (d1) reuses the token after a `whoami`, (d2) needs
  a reissue on each take after a release), release steps 4 and 5, placement and its
  quarantine, the hot reissue, and the restore's merge rule. Its last paragraph lists every
  change.

- **The code matches the decision.** `seat.Credential` has a single implementation,
  `seat.Password`. Its `Mint` logs in with the pool's secret. `placeissued.OS` says: "the pool
  password as the pool's credential (the only production credential; DESIGN-v2 7.1 (d1) and
  (d2) are the owner's to choose)". `seat/place.go` `checkIssued` refuses any other kind, and
  `placeissued/place_test.go` asserts that refusal.

- **X23 (SPEC-cross-network.md)** says: "The credential kind for a remote seat is the owner's
  choice at issue time (`--credential password|token`), **as for any seat**". For any seat,
  the kind follows the seat: a pool (ccy, host `cc`) seat is `password`, and an LXC guest seat
  is `token` (SPEC-n3: "an LXC seat is a token seat"). X23 does not make a token pool seat.

## Why accepting `token` in place-issued would be wrong, even as a stopgap

Suppose the token were simply stored in the pool. Every take of that seat would then fail:
`Password.Mint` would send the token as a password to `/login`, which is refused (M_FORBIDDEN),
and the take would answer `ErrReissue`. The token would never reach `held/token`, so no ccy
session could join with it. Making it work means building 7.1 (d1) or (d2) through the whole
take, release, reissue and restore path, under a new kind. That reverses an owner decision
(gate B), so it is the owner's to make.

The whole path of a placed seat uses that one credential. `seat place-issued` and the ccy
launcher's take (`teams/app.go` `storeParts`, used by `teamchatlaunch` and `seatsweep`) open
the store with `placeissued.OS(...).Credential`, which is `seat.Password`. So the take step
that would turn a pooled secret into `held/token` is a password login, the same in both.

**Urgent for the coordinator:** `x30-lab.bash` is queued in `CLAUDE/Plan/meta-deploy.list`, and
its `issue()` pipes `member add ... --credential token` into `place-issued`. On its first run
that step exits 78.

## Where the plans and docs are wrong (the defects behind the report)

The downstream followed text in this repository that names a command the code refuses.

| Where                                                                         | Says                                                                        | Fact                                                                                                                                                                         |
| ----------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `docs/agent-bus-cross-network.md`, "A member on another machine" (both forms) | `member add ... --credential token \| ... teamchat seat place-issued`       | exits 78, "a token, not this pool's password"                                                                                                                                |
| same section, last sentences                                                  | "If the ssh step fails ... reissue it by running the command again"         | running `member add` again claims a **new** handle; the first stays enrolled with a live credential nobody holds. The recovery is `member reissue --handle <handle>` (below) |
| `src-go/internal/teamchat/admin/rotate.go` (`team rotate-ca` help and stdout) | `member reissue ... --credential password\|token \| ... seat place-issued`  | the `token` half is refused for the same reason                                                                                                                              |
| Plan 00246 `INSTALL-PATH.md`, "The gates" and X30 item 3                      | the X30 remote seat is issued `--credential token`, so gate B does not bind | gate B was already met on 2026-10-30 (the owner confirmed INT6). A token seat cannot be placed in the pool, and X30's `teamchat run --plain` member takes from the pool      |
| Plan 00246 `x30-lab.bash` line 396                                            | `member add ... --kind host ... --credential token`                         | its `place-issued` step will exit 78 on the first lab run                                                                                                                    |

These were not changed here. The coordinator's brief was: if the design forbids the route, do
not build it, write the case and stop. Correcting them is one change. Every `place-issued`
pipeline should read `--credential password`, and the rotate-ca text likewise; `rotate.go` is in
the ccy image, so that change needs a CCY version bump. Defence Before Fix class: "operator
guidance that pipes a credential kind into `place-issued` that the pool refuses". A candidate
detector is a Go test that reads the docs, the plan scripts and the admin help text for
`place-issued` pipelines and holds the `--credential` value in each to the kinds that
`placeissued.OS` accepts. That is for the coordinator to dispatch, or the owner if 7.1 is chosen
(then the text becomes `pool_token`).

## The intended path for a remote ccy seat

Issue a pool password seat over X21's ssh pipe, and place it with `seat place-issued`:

```
read -rs pw; printf '%s\n' "$pw" | ssh <bus> sudo -S -p '' teamchat-admin member add --team <team> --prefix <prefix> --seat <n> --kind <kind> --human-text <bool> --credential password | ssh <member> teamchat seat place-issued --checkout <dir>; unset pw
```

The password is held only in the member machine's pool (0600, never mounted into a container).
Each ccy take logs in with it and each release logs out (DC16 (c')). This is the
security posture the owner accepted at gate B. A downstream that will not hold a pool password
needs DESIGN-v2.md 7.1 built, which is an owner decision, not a place-issued change.

## Recovery: a seat issued on the bus but never placed

Situation: `member add` (or `return` or `reissue`) ran on the bus host, so the member is
`enrolled` with a fresh credential, but the pipe to `place-issued` broke. The credential existed
only in the pipe.

1. **Find the handle.** It is not in the lost document. Use the `added` notice the issue posted
   in the team's main room, or the team's status record
   (`/var/lib/teamchat-status/<team>/status.json`, `members[].handle` with state `enrolled`,
   readable by the owner group). Do **not** run `member add` again: that claims a second
   handle.
2. **Reissue and place in one pipe:**
   `... sudo -S -p '' teamchat-admin member reissue --team <team> --handle <handle> --credential password | ssh <member> teamchat seat place-issued --checkout <dir>`.
   `member reissue` takes an `enrolled` member (`apply/member.go` `fromStates`). Its one step is
   `credential`: a password reset with device logout, which kills the lost credential and every
   token minted from it, then a fresh issue (DESIGN-v2-C-host.md 3.6.3 table: "revoke; issue the
   credential (the lost-bundle recovery, C21)"). `place-issued` accepts a member verb's answer
   as well as the bare document (`placeissued/doc.go`).
3. **Or revoke without placing:** `teamchat-admin member park --team <team> --handle <handle>`
   (a password reset to a value nobody keeps, so the member becomes `parked`), and later
   `member return --handle <handle> --credential password | ... place-issued`. Or use
   `member retire` to drop the seat for good.

All four verbs exist in `admin/run.go`'s table, `group(ClassRoot, "member", "add", "return", "park", "retire", "reissue", "set")`, and each has a runner (`admin/verbs.go` `rootRuns`). The
flags are `--team` plus `--handle` (and `--credential` for return and reissue).

## What was checked

Plan 00209 PLAN.md (INT6 row, Task 2.5 gate text, Task 3.1, Task 4.4), DONE-n0-n1.md (gate B
answer), DESIGN-v2.md sections 7 and 7.1, DESIGN-v2-C-host.md 3.6.3, SPEC-cross-network.md
X21 to X23, INSTALL-PATH.md, `seat/credential.go`, `seat/place.go`, `member/placeissued`,
`apply/member.go` (`credential`, `fromStates`), `admin/bundles.go`, `admin/run.go`,
`admin/verbs.go`, `admin/rotate.go`, `docs/agent-bus-cross-network.md`, `x30-lab.bash`. No code
was run. This is a reading of the design and the code, and no test was added because nothing
was built.
