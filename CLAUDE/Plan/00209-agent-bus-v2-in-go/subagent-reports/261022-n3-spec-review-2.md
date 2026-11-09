# Adversarial review, round 2: SPEC-n3.md (commit d784cc85)

Plan 00209, Task 3.1. Checked against round 1 (`261022-n3-spec-review.md`), its fix log
(`261022-n3-spec-fixes-1.md`), the diff 683e5c86..d784cc85, B 2.1-2.15, A 7.5, C 3.7.5, 3.9,
3.11, PLAN.md (Task 2.4 notes, Task 3.1, the 2.0e/2.0g rows), and the code under
`src-go/internal/teamchat/` (`health/online.go`, `seat/lockprobe.go`, `seat/release.go`,
`proto/verdict.go`, `proto/escape.go`, `matrix/client.go`, `matrix/member.go`,
`matrix/fakehs/fakehs.go`, `matrix/fakehs/sync.go`) and `src-go/go-deps.allowlist`.

**Verdict: FIX-FIRST.** Two majors. One is a missing mechanism under round 1's M1 fix: the rule
is right, but nothing can decide it. The other is a lock race that B 2.5 already names. Every
other round-1 finding is closed by the text, not just mentioned. The slice order, TB1, TB2.4,
SU2, GU3, KT1, LP2, PT1, RS1-RS3 and the gate marks hold up.

Counts: BLOCKER 0, MAJOR 2, MINOR 9, NIT 5.

## Round 1, finding by finding

| R1    | Closed? | Evidence in d784cc85                                                                                                                                                                                                                    |
| ----- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| M1    | partly  | UH1 and gap 11 state the right rule (roster agent mentioned, `@room`, reply or thread root by a roster agent, edit). Nothing supplies the replied-to sender at a member that never delivered that event: **MAJOR 1**                    |
| M2    | yes     | DV6 with told-once, the prompt fallback and the restart case; section 11 maps B30                                                                                                                                                       |
| M3    | yes     | DV3 has the caps, humans first, the 10 s retry and cap-edge tests. DV2 gives the body cap to `proto.MaxBodyBytes`, which exists (`escape.go:14`, 2048)                                                                                  |
| M4    | yes     | TB2.4: a closed grammar per op, the host's own re-emission, a strict error object, hostile rows. LX6 and LX9 cite it                                                                                                                    |
| M5    | yes     | TB1's socket rule (`O_PATH`, `S_ISSOCK`, uid 0, connect through `/proc/self/fd/<n>`) and SU2 use it, with the relative and absolute symlink fixtures. Connecting through the magic link works: `unix_find_bsd` follows it               |
| M6    | yes     | GU3 sets the store aside on an identity change under the seat lock (75 if held). MS3 reads the pair with one retry. LN9 never stops on a torn pair. LX6 treats a present-phase 75 as `DEFERRED`. See MINOR 5 for what step 08 then does |
| M7    | yes     | LP2 builds the `member.disconnected` op and the announcer. It matches `health.Announcer`'s signature (`online.go:42`). T22 and E2 name the dependency on the wiring slice. See MINOR 7 for drift from PLAN.md                           |
| M8    | yes     | KT1 is in n3a, with the digest rule. n3b's VM-lab claim is limited to placement and an accepted start                                                                                                                                   |
| M9    | partly  | Gap 12 injects fakehs's hold timer. The client's 45 s is still real time: **MINOR 2**                                                                                                                                                   |
| M10   | yes     | P1-P11 are listed in B's words, with P3 mapped to SY5 and P6 to LK3. PT1 names B 2.15's 14 failpoints; the fix log is right that there are 14, not 15                                                                                   |
| M11   | yes     | n3f moves to `SPEC-n3f.md`, out of this MERGE                                                                                                                                                                                           |
| m1    | yes     | The reopen's flags, the exemption, the lock on the reopened fd, the opener-seam assertion                                                                                                                                               |
| m2    | yes     | LX4's `exe` check by device and inode. LX5's directory opens and entry grammar. `/proc/<pid>/stat` read by TB1                                                                                                                          |
| m3    | yes     | n3-D17 added, and n3-D1 cites B 2.13                                                                                                                                                                                                    |
| m4    | yes     | LP1 returns the record in both states                                                                                                                                                                                                   |
| m5    | partly  | Both timings and the 429 case are in, and gap 6 makes the epoch raise atomic. But the sentence on the deadline is wrong for the 429 resend: **MINOR 1**                                                                                 |
| m6    | yes     | RS2 lands the seam in n3c with a nil adapter. RS3 keeps the PLAN row open. RS1 states the harm                                                                                                                                          |
| m7    | yes     | CL1 uses `go_command_static`; gap 13 added (see NIT 2)                                                                                                                                                                                  |
| m8    | yes     | GU2 checks `gh` on the unit's `PATH`                                                                                                                                                                                                    |
| m9    | yes     | Exit 1 is kept, stated as a build-time placeholder, with an empty-list test before Task 3.3. That is acceptable                                                                                                                         |
| m10   | yes     | CL5, RN1, RN2 cover `--state`/`--seat`; RN1 has the B47 pipe test                                                                                                                                                                       |
| m11   | yes     | CL6, the B11 duplicate token, SY1 `fresh`, SY14, HK2, LN7                                                                                                                                                                               |
| m12   | yes     | SY12's thresholds and n3-D18                                                                                                                                                                                                            |
| m13   | yes     | LX2 and n3-D12                                                                                                                                                                                                                          |
| m14   | yes     | RC1's `missing`, n3-D19 (no host reader of `report.json` decodes the code set, so adding one breaks nothing)                                                                                                                            |
| n1-n5 | yes     | all applied                                                                                                                                                                                                                             |

## MAJOR

**MAJOR 1. No expectation supplies the replied-to event's sender: A 7.5's rules 3 and 4, and UH1's
"reached no agent" predicate, cannot be decided (SY4, UH1, gap 11; A 7.5 lines 1024-1033;
`proto/verdict.go:353-364`; B 2.3.2).**

`humanAddressed` decides rules 3 and 4 through `j.Lookup.Event(id)`. `proto.Lookup` is "the
member's store as the verdict reads it" (`verdict.go:113`). By B 2.3.2 the store holds:

- `ledger`: items this member delivered;
- `outbox.pings`: its own pings.

It holds no other own sends (`say`, `reply`), and no event this member ignored. A 7.5 says:
"Checking the replied-to event's sender takes one event fetch, which B caches". B never
specifies that cache, and neither does this spec. `matrix.GetEvent` exists (`member.go:258`), but
no expectation calls it. Two failures follow:

1. **Rules 3 and 4 fail for the member itself.** A human uses "Reply in thread" under agent Y's
   `say`. Y's store has no record of its own `say`, so `sentBySelf` is false and Y ignores the
   message as `not-addressed`. That is the v1 defect A 7.5 says rules 3 and 4 fix ("in v1 a human
   answering an agent inside a thread was unheard").
2. **UH1 counts wrongly.** A human replies inside agent X's thread. Y never delivered X's root
   event, so it cannot see that a roster agent sent it, and counts the message as unheard. That is
   round 1's M1 defect again, for threads and for replies without `m.mentions`. UH1's test "a reply
   to an event sent by agent X is not counted at Y" can pass only with a fake Lookup that knows
   things the production one does not.

**Fix:** add an expectation (SY4a, n3c) for the production `Lookup.Event`:

- Answer from the ledger and the outbox first.
- Then from a bounded in-memory cache of (event id → sender, room, verb, addressed-to-self).
- On a miss, call `matrix.GetEvent` once, only for a main-room human event that carries a reply,
  thread or edit relation, under the team's request deadline.
- Decide what a failed fetch means and say it. Recommended: a transient failure is SY8's transient
  path, so the batch is not persisted and is re-judged. It must never become `not-addressed`, which
  would silently drop a human's message. A 404 or 403 is "unknown sender": not addressed to this
  member, and not counted as unheard. That undercounts rather than miscounts; state it.
- Own non-ping sends must also be findable. Either the fetch covers them, or the cache is seeded at
  each send. State which.
- Add the fetch to 2.14's connection count as one line.
- Tests:
  - a thread reply under Y's own `say` is delivered at Y with a store that has no record of it;
  - a reply inside X's thread is not unheard at Y;
  - a transient fetch failure persists nothing;
  - a second event with the same relation is a cache hit (no second fetch).
- Gap 11 then names both pieces: the predicate in proto, and the Lookup that feeds it.

**MAJOR 2. The seat lock is acquired with no retry, against the host's momentary `LOCK_SH`
probes (LK1; B 2.5 "Acquisition"; C 3.9.2 L3, 3.7.3; LP1, LX8).**

LK1 retries only `keeper.lock` and `link.lock`. B 2.5 applies the retry "where a probe may hold a
momentary shared lock (L1, L3 ...)". L3 is the host's seat-lock probe, `LOCK_SH|LOCK_NB` (C
3.9.2, 3.7.5). That probe runs:

- every minute from the health tick (LP1);
- every second for 60 s in the absent phase (LX8);
- every minute from the owner's seat sweep for ccy seats.

Any acquire that meets the probe's shared lock gets `Busy`:

- RN2 fails a launch with 75 (the unit then restarts and uses up its `StartLimitBurst`);
- RC1 refuses a hot add as "held", with a NOTICE naming a holder that does not exist, and the seat
  stays refused until the next desired generation;
- GU3 and GU5 return 75, so LX6 and LX8 record `DEFERRED` for a whole apply cycle;
- SA1 fails the owner's repair.

These failures are rare but nondeterministic, and LK1's tests as written would pin the wrong
behaviour.

**Fix:**

- LK1: every acquire, the seat lock included, is tried three times 50 ms apart. The only
  shared-lock holder is a probe, and a probe holds it momentarily.
- LK3, GU3, GU5 and SA1 cite the same rule.
- Test with a second process: it holds `LOCK_SH` for 60 ms and the acquire succeeds; it holds
  `LOCK_SH` past all three tries and the acquire is `Busy`. Run both cases for `TakeSeat` and for
  `guest remove`.

## MINOR

**MINOR 1. SY7's deadline sentence is wrong for the 429 resend (SY7; `matrix/client.go:357-387`).**
"Each resend inherits what is left of the one 45 s deadline (`matrix/client.go`'s `once`)" holds
for the transport's own resend, which is inside one request and so one `tryCtx`. The 429 loop
(`answered`) calls `once` again, and each call makes a fresh `context.WithTimeout(ctx, limit)`.
**Fix:**

- Say the 45 s is per try. The transport's resend shares it. A 429 retry gets its own, bounded by
  `maxTries` and `maxRateWait`.
- Case 8 needs no timing claim beyond that.

**MINOR 2. SY7 case 3's late drop cannot run without sleeping (header line 11, SY7 case 3, gap
12).** Gap 12 puts fakehs's hold on the fake clock. The client's 45 s deadline is a real-time
context (`once`), so "the resend times out against the 45 s deadline" costs 45 real seconds.
**Fix:** extend gap 12 so the link's sync deadline is driven by CK1's clock, either as a deadline
seam in `matrix.Client` or as the link's own timer cancelling the request. Or state that the test
sets a short client `limit` while the fake hold never fires, and that this is the one bounded
real-time wait.

**MINOR 3. TB2's last sentence is contradicted by LX5 (TB2; LX5).** TB2 says "No member-written
value names a path the host opens". LX5 opens:

- `/proc/<pid>/stat` with the pid from a guest-written `session.pid`;
- run directories whose names the agent created.

Both are grammar-checked and their effect is bounded (a kept digest), but the sentence as written
is false. **Fix:**

- Reword: "beyond a grammar-checked pid or run id joined below a fixed, host-chosen directory and
  opened by TB1".
- Add hostile rows to TB2: pid 0, pid 1, a pid over `pid_max`, a run directory that is a symbolic
  link, and an entry name that fails the grammar.

**MINOR 4. LP1 leaves open where its inputs come from and what a refused open means (LP1;
`seat/lockprobe.go:16-27`, n2c-D14; `health/online.go:195-210`).** Three things are unstated:

- How the health unit gets the init pid and pidfd: LX3's rule, `lxc-info` inside the sandbox.
- Where the user's home comes from: the guest's `/etc/passwd` by TB1, grammar-checked.
- What a refused open answers: a symlinked `.config`, a FIFO `seat.lock`.

The tick treats a probe error as a "could not be probed" problem with the entry unchanged.
n2c-D14's rule for owner seats is "refused = free, no run". **Fix:** adopt n2c-D14: refused is
free with no record, so no run id is held and no notice is asked. Add those rows to LP1's tests.

**MINOR 5. A deferred `place-bundle` and step 08 (LX6, LX7; C 3.9.2 step 08).** When GU3's
identity change is `DEFERRED` because the old identity's seat is held, LX7 still writes the user's
desired file with "every team the user is placed in". If that seat is listed, the keeper keeps
running the old identity's bundle and store. **Fix:** leave a seat whose placement was deferred
this phase out of that apply's desired seats and out of the host copy's `seats`, or state why
listing it is safe. Test the deferred case through step 08.

**MINOR 6. Section 9 names no slice per gap, and two VM-lab claims rely on a later slice
(sections 0 and 9; GU1 T15; n3c's row).**

- Gaps 3, 4 and 8 are needed by n3a: MS3, LK4, and LK3's second-process tests.
- Gaps 5-7, 11 and 12 are needed by n3c.
- n3a's "in the VM lab from nothing (T15)" needs `teamchat` in the guest, which only n3b's LX5
  places.
- n3c's VM-lab claim says "with n3b".

**Fix:**

- Add a slice column to section 9.
- Say that n3a's VM-lab run places the binary through the lab's fixture, or move T15's VM half to
  n3b.
- Say that n3c's VM acceptance runs once n3b has merged. The unit tests have no such dependency.

**MINOR 7. Drift from PLAN.md over the announcer and godbus (LP2, gap 13; PLAN.md lines
308-315).** PLAN puts the units seam's godbus adapter and "the announcer adapter ... for the
health tick's LXC notices" in N2's later production-wiring slice. LP2 builds the announcer in n3b,
and gap 13 makes the godbus decision in n3b. **Fix:**

- Name one owner for each.
- Update PLAN's list in the commit that lands LP2, or have LP2 consume the wiring slice's adapter.
- Make the godbus module decision once, for both the units seam and SU2.

**MINOR 8. Checking "`tmux` on the guest `PATH`" under TB1 (LX4; C 3.9.2 step 03).** With
`RESOLVE_NO_SYMLINKS`:

- EL9's `/bin` is a link to `usr/bin`, so `/bin/tmux` fails with `ELOOP`;
- an alternatives-managed `tmux` is a link too.

**Fix:**

- `ELOOP` or not-a-regular-file at one `PATH` entry means "not here": try the next entry. It never
  refuses the phase.
- A `tmux` reachable only through a link is a miss that names the path.
- Test this on an EL9-shaped fixture tree.

**MINOR 9. D's v1 holdback adapter must keep TB1 (LX4; C 3.9.2 step 03, D §5.5).**
`ReadGuestV1Session` reads guest files "from the same root fd". Given a bare root fd, it can open
a FIFO, which TB1 forbids for every host read. **Fix:**

- The seam hands D's adapter TB1's opener, not the root fd.
- LX4's test plants a FIFO at the v1 env-file path.

## NIT

1. Section 11 row "T6 no team, T7 kit upgrade, T8 leftovers | RN2, RN1, LN7" is misaligned. It
   should read T6: RN2 and LN7 (the conversation record); T7: RN1 (`--kit`); T8: RN1 (leftover
   runtimes).
2. CL1 asks for "a go-deps `keepout` row". A keepout row names one module
   (`go-deps.allowlist:12`), so `./cmd/teamchat` needs two: YAML now, and godbus when gap 13
   allowlists it.
3. n3-D1 should say that `place-bundle` gains exit 75. B 2.13 gives 75 to `remove` only; GU1
   already says it.
4. SY7 case 3, "resumes from its marker and `released_ms`": an LXC `Bundle` has no `released_ms`,
   so it resumes from the marker alone (B 2.15's resume-bound row).
5. T23's last clause ("the host's health tick ... posts one `disconnected` per seat" on `failed`)
   needs LP1 and LP2 for LXC seats. Map it in section 11.

## Checked and sound

- **TB1.** The `O_PATH` and reopen mechanism is correct, including inside the health unit's
  sandbox (`CAP_DAC_READ_SEARCH` covers the `O_RDONLY` reopen). Every host-side guest read or
  connect in LX3, LX4, LX5, LP1, LX8 and SU2 goes through TB1, apart from MINOR 9's seam. No host
  step opens a FIFO, socket or device.
- **TB2.** TB2.1 matches C 3.7.5's bound on the run id; TB2.4 closes round 1's injection.
- **SY7's reasoning.**
  - Go retries a body-less GET on a reused connection that closed before the first response byte.
    fakehs's `DropAfter` (handled, then closed, `fakehs.go:136`) produces exactly that, so case 1
    sees the same `since` twice.
  - Within one database the resend is idempotent.
  - Across a restore, SY6's read before SY5 discards any answer from the restored server. Gap 6's
    atomic epoch raise is the order that makes this hold.
- **Slice order.** The LXC member runs end to end by n3c with n3b, and n3e puts it on a real team.
  Each slice merges inert (CL2's placeholder, with the empty-list test before Task 3.3).
- **2.0e.** RS1-RS3 state the dependency on hooks-daemon #127 for RS2 and on Task 4.2 for RS3. The
  PLAN row stays open. The table test runs on a fake seam.
- **2.0g.** UH1's answer ("yes, when it reached no agent") matches A 7.5 line 1014. Only the
  lookup behind it is missing (MAJOR 1).
- **Gate marks.** Every host run is **[A]**: n3b, KT1's placement, SU1, LP1, and CL1's play task.
  The one **[B]** is LN9's ccy hot-reissue host run in Task 4.4. The LXC path never gives a process
  a pool seat's password (token seats).
