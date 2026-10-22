# Adversarial review: SPEC-n3.md (commit 1adfe458)

Plan 00209, Task 3.1. Reviewed against B 2.1-2.15 and B1-B49, C 3.7.5, 3.9, 3.11.1-3.11.2 (and
3.5.1, 3.12.2 where they bear), A 7.5, PLAN.md Task 3.1 and the 2.0e/2.0g rows, and the code under
`src-go/internal/teamchat/` and `src-go/internal/execx` in the n3 worktree.

**Verdict: FIX-FIRST.** No blockers. Eleven majors, mostly missing expectations and two holes in
the trust boundary, plus one wrong rule (UH1). The structure is sound: the slices, TB1's O_PATH
mechanism, SY7's reasoning and most of the decisions hold up. Each fix below is an edit to the
spec. None needs a design change except where noted.

Counts: BLOCKER 0, MAJOR 11, MINOR 14, NIT 5.

## MAJOR

**M1. UH1 counts messages addressed to another agent as unheard (spec 467-475; A 7.5 line 1033;
v1 `helpers/pingbus/protocol.py` `reached_no_agent`, `UNHEARD_REASONS`).** A 7.5 says "a human
message addressed to no agent is counted as unheard, as v1 (P41)". v1 logs `not-addressed` only
when the message's mentions name no agent that holds a role. UH1 has every member count every
`not-addressed` human message. So a message to agent X counts as unheard at every other member,
and `unheard` no longer means "reached no agent". The rationale ("no member can tell whether it
was the addressee") is true only of `type` and `empty`, which `proto.DecodeHuman` refuses before
`humanAddressed` runs (verdict.go:322-332). For `not-addressed` the member can tell from the
mentions and the reply or thread target. **Fix:**

- Count `not-addressed` only when no agent of the verified roster is addressed: no roster agent
  in `m.mentions.user_ids`, no `@room`, and no reply or thread root sent by a roster agent.
- Keep `type` and `empty` counted at every member, and say why: their addressing is never
  computed.
- Gap 11 ("nothing in proto changes") then becomes false. proto must expose a "reached no agent"
  predicate on the ignore path. Add it to section 9.
- Add a test: a message mentioning only agent X is not counted at Y.

**M2. B30 (membership and role notices) has no expectation (B 2.10 lines 1780-1783; B 2.8
UserPromptSubmit row; B 2.3.3 `told`).** No expectation covers the MEMBERSHIP NOTICE delivery for
each change of the active set or a role:

- told once per generation through `session.json` `told`;
- the UserPromptSubmit fallback when the socket write failed;
- the role-change NOTICE. SY11 only rebuilds the filter.

**Fix:** add an RC2 or DV6 expectation with tests for each: a join, a leave with its reason, a
role change, a failed socket write followed by the prompt fallback, and nothing told twice across
a link restart. Map B30 in section 11.

**M3. The per-delivery caps and humans-first ordering (B23, B24, B 2.7.2) are unmapped.**
`proto.Delivery` renders. Nothing in proto picks items: there is no 8-entry or 12 KiB cap and no
HUMAN-first ordering in `proto/delivery.go` or `limits.go`. DV3 has only the 2 s pacing and the
choice of team. **Fix:** add to DV3:

- at most 8 entries and 12 KiB on the wire, the rest in the next delivery;
- HUMAN entries before PEER and PING within a delivery, then by time;
- the 2048-byte body cap: say who enforces it, proto or the link;
- the connect or auth failure that leaves items pending and retries in 10 s (B 2.7.3);
- tests at the cap edges.

**M4. Guest-written text reaches host output unchecked (TB2.4, LX6, LX9; C 3.5.1 "reports changed
only on a `^CHANGED\t` line").** TB2.4 says a guest operation's `CHANGED` lines are "reported
only". LX6 fails the phase "naming the op and its error line". Both are bytes the guest wrote. If
`lxc apply` passes them on, a guest can:

- print `CHANGED\t…` or `LXC-SUMMARY\t…` lines, which drive the play's changed detection;
- inject newlines or terminal escapes into the owner's play output.

**Fix:** state in TB2.4 that guest stdout is parsed against a closed grammar
(`CHANGED<TAB><what>`, `what` from a fixed set per op), counted, and re-emitted only in the host's
own `CHANGED|SKIPPED|DEFERRED` form. The error line is decoded strictly as B 2.2.2's error object;
only its `exit` and `code` (closed sets) are printed. Add hostile-value rows to TB2's table:
embedded newline, forged `LXC-SUMMARY`, ANSI escape, an over-long line.

**M5. SU2 connects to a guest socket path outside TB1 (SU2; C 3.9.4).**
`connect("/proc/<pid>/root/run/systemd/private")` resolves symlinks inside the guest tree, and an
absolute symlink resolves against the caller's root. A guest root that plants
`/run/systemd/private -> /run/systemd/private` points the host at the **host's** systemd, which
would then get `Reload`, `StartUnit` and `StopUnit` for `teamchat-session-<user>.service`. This
contradicts TB1's own "only through the guest root". **Fix:**

- Open the socket inode with `openat2(O_PATH, RESOLVE_IN_ROOT|RESOLVE_NO_SYMLINKS|RESOLVE_NO_MAGICLINKS)`.
- `fstat` it: `S_ISSOCK`, owned by uid 0.
- Connect through `/proc/self/fd/<n>`.
- Add a TB1 fixture with a symlinked `private`, and the same rule for any later guest socket.

**M6. An existing guest seat directory keeps a store of a different identity (GU3, n3-D1; C 3.9.2
steps 03 and 07).** n3-D1 is right that the directory swap would unlink a held `seat.lock`. It is
a real contradiction in C, and in B 2.13's `place-bundle` row, which n3-D1 does not cite. But
keeping `store.json` unconditionally is wrong when the new bundle names another `user_id` or seat.
That happens after a retire and add (n3-D10's own recovery) or a different seat number for the
same guest user and team. The link would then resume an old identity's position, ledger, outbox
and `run`, and EN3 would close the old run with the new token. **Fix:** in GU3, when the existing
`seat.json` decodes to a different `user_id` or seat:

- take the seat lock (75 if held);
- set the store aside as SA1 does;
- then place.

Test both cases. Also state that the link reads `token` and `seat.json` as a pair. A read between
the two renames must not leave a permanent 78 `bundle`; LN9 retries on the second event. Define
what "a mismatch" in MS3 compares.

**M7. The LXC disconnected path has no owner (T22 LXC rows, E2; C 3.11.2 step 6; PLAN Task 2.4
"not covered: … child and announcer adapters").** LP1 builds `health.Guests`. Nothing builds the
other pieces of that path:

- `health.Announcer` in production (`health/online.go:42`);
- the child's `member.disconnected` op, which `admin/verbs.go` lacks (only add, return, reissue,
  park, retire, set exist);
- the `health --all` wiring.

Yet section 11 sends T22's `lxc-stop -k` row and E2's "next start's disconnected notice" to Task
3.4. **Fix:** either add an expectation and slice for `member.disconnected` and its announcer
(reusing `notice.Disconnected` as a package, as B 2.11 says), or state the dependency on the
health production wiring's task by name, and mark those T22/E2 rows blocked on it.

**M8. Who builds the host kit is unstated (LX5, PK1, RN1; C 3.9.2 step 04; C 3.5.1 step 09).**
LX5 copies "the host's kit" (`bin/teamchat`, `plugin/`, `settings.json`). RN1 checks
`<kit>/bin/teamchat --version` against `<digest>`. PK1 renders the plugin in n3e. No expectation
covers:

- assembling `/usr/local/lib/teamchat/kit/<digest>/`;
- defining `<digest>` as the binary's source digest, which it must be, because `hooks.json`
  embeds the kit path;
- recording the digest in `installed.json`.

n3b's "LXC member placed end to end in the VM lab" therefore needs a kit that only n3e can
render. **Fix:**

- Add a KT1 expectation: the kit's layout, its digest rule and its build task, marked [A].
- Put a minimal kit (binary plus a placeholder plugin) in n3b, or move n3b's end-to-end claim
  after n3e.
- Also, n3b's started unit runs `teamchat run`, which is still "exit 1, not built" until n3c. The
  unit will flap into its start limit. Say that n3b's VM acceptance asserts placement and a unit
  start, not a running session (or uses `service: false`).

**M9. "No test sleeps" is not achievable on today's fakehs (section header line 11; CK1; SY7
cases 3 and 5; fakehs `sync.go:100`).** fakehs's long-poll waits on a real
`time.NewTimer(timeoutMS)`. The link always sends `timeout=30000` (SY2). H-C14 ("held to its
timeout") therefore costs 30 real seconds per case. `Options.Now` covers only timestamps. **Fix:**
add a gap 12: fakehs takes an injected timer for the long-poll hold (or the link's poll timeout
becomes a test seam). Then pin each restore case on the fake clock.

**M10. P1-P11 are defined nowhere, and two are mapped wrong (section 11 lines 538-539; DV3 "P1-P6";
B 2.15 lines 2154-2173).** B's properties are prose. The spec numbers them but never says which
is which. In B's order, P3 (position never persisted ahead of its items) is SY5's, and P6 (no two
processes write one store) is LK1-LK3's. The table gives both to DV3. B's core requirement, the
properties "across random interleavings of sync batches, recv, deliveries, socket and homeserver
faults and crashes at every failpoint", has no expectation for the harness: its driver, the seed
handling, the failpoint enumeration, and the slice it lands in. **Fix:**

- List P1-P11 with B's wording.
- Map P3 to SY5 and P6 to LK3.
- Add a PT1 expectation for the interleaving harness. It belongs in n3e, which is the first slice
  with all the pieces, failpoints running under `busfailpoints`. Name the 15 failpoints of F6 one
  by one.

**M11. The n3f expectations are too thin to write failing tests from (SR1, SR2).** "As B 2.2.1, A
10 and Plan 00211's SPEC as behaviour" leaves open, among others:

- `report`'s headline and body split and its `report-rate` (14);
- `thread Q<n> --ask` and `--close`;
- `ask --remind-after`;
- `questions --all` and the "unanswered" rule;
- the `question` record's fields;
- `history --room questions`;
- the reports room "verified at each send" failure (22).

**Fix:** enumerate them now, or state that n3f gets a SPEC-n3 addendum, reviewed before n3f
starts, and keep n3f out of this spec's MERGE.

## MINOR

**m1. TB1's reopen: say which flags it must not use.** The mechanism is correct on Linux:

- `openat2(O_PATH, RESOLVE_IN_ROOT|NO_SYMLINKS|NO_MAGICLINKS)`, then `fstat`;
- then a plain `open("/proc/self/fd/<n>", O_RDONLY|O_NONBLOCK|O_CLOEXEC)` and a second
  device and inode check.

The O_PATH fd pins the inode, so no FIFO or device is ever opened. But the reopen must be a plain
`open`/`openat`. Done through `openat2` with `RESOLVE_NO_MAGICLINKS`, or with `O_NOFOLLOW`, it
fails with `ELOOP`. TB4's blanket "O_NOFOLLOW on every member file", and any future project rule
enforcing it, must exempt this one call. Spell that out. Two more points:

- `flock` on an O_PATH fd is `EBADF`, so LP1 must lock the reopened fd.
- The FIFO-poller test only samples. Also assert through the opener seam that no non-O_PATH open
  reaches a non-regular inode.

**m2. Guest reads outside TB1, or TB1 underspecified for them.**

- LX4's "systemd as init": C 3.9.2 resolves `/proc/<init pid>/exe`, a magic link that TB1's
  `RESOLVE_NO_MAGICLINKS` refuses. Specify: open the host's `/proc/<pid>/exe` (a host path) with
  O_PATH, then compare device and inode with the guest's systemd binary opened by TB1.
- LX5's `session.pid` glob lists guest directories. Directory opens need the same `openat2`
  flags with `O_DIRECTORY`, and each entry name must pass the run-id grammar before it is joined
  into a path.
- `/proc/<pid>/stat` in the guest's `/proc` is read by TB1 (no `NO_XDEV`); say so.
- EN5 reads no guest file. It is correct as written.

**m3. Record TB1's departure from C 3.9.2 as a decision.** C 3.9.2 step 03 and the L3 probe open
`O_RDONLY|O_NONBLOCK` and then `fstat`. On a FIFO that open succeeds and makes the host a reader,
which is the hazard TB1 removes. Add an n3-D entry so the design and the spec do not disagree
silently. Do the same for n3-D1's correction of B 2.13's `place-bundle` row.

**m4. LP1 must return the record while the lock is held too (C 3.11.2 step 6).** The tick keeps
the run id read while that run held the lock, and uses `launch_failed_ms` only from the free lock.
"The free lock's record returned for the tick" reads as free-only. `health.GuestProbe` already
carries `Record` in both states; say so.

**m5. SY7: the 45 s budget covers the transport's resend (`matrix/client.go` `once`: one
`tryCtx` per try).** SY7's reasoning is correct. Go's transport replays a body-less GET on a
reused connection when the first response byte never came (`transportReadFromServerError`,
`errServerClosedIdle`), and the client's 429 loop is a second resend path with the same `since`.
But the resend inherits what is left of the 45 s. After a drop late in a 30 s hold, the restored
server's 30 s wait outlives the deadline. The outcome is then a transient timeout and SY6's
"before the first long-poll after a failed request" read, not a lower `next_batch`. Case 3 should
run both timings (an early drop gives the lower `next_batch`; a late drop gives the timeout). Add
a case for the 429 resend. fakehs's `RestoreTo` must raise `restore_epoch` atomically with the
rollback, so no request ever sees the restored database with the old epoch. C 3.12.2 stages
behind a private network namespace, so that is the real order.

**m6. 2.0e: say where the seam lands and keep the requirement open.** RS1-RS3 are correct, and
the dependency is stated (#127 and Task 4.2). Still missing:

- the slice that lands the `RestartRecord` seam and RS2's table test: n3c, with a nil adapter;
- that the PLAN "Requirements carried" 2.0e row stays open until the adapter is wired after RS3.

Also say that under RS1 a relaunch shows humans a `left exit` with no following notice (`back`
follows only `disconnected`). That is today's 2.0e harm, carried knowingly.

**m7. CL1's build-task claim is stale.** `tasks/build-go-command.yml` already has
`go_command_static` (`CGO_ENABLED=0`, lines 44 and 106). No play change is needed for it. The
godbus dependency SU2 needs is not in `go.mod` or `go-deps.allowlist`, and adding a module is a
decision (src-go/CLAUDE.md). Add it as gap 13, with the allowlist row and CL1's `keepout` row for
`./cmd/teamchat`. Also check that godbus offers a message-size cap at all; I found none, and a
capped reader on the transport may be needed.

**m8. GU2 resolves `gh` on the wrong PATH.** `lxc-attach` sets `PATH=/usr/local/bin:/usr/bin:/bin`
(C 3.9.1). The link runs `gh` under the unit's PATH, which includes `<home>/.local/bin`. Check
`gh` on the unit's PATH, as GU2 already does for `claude` and `tmux`.

**m9. CL2's "exit 1 internal naming the slice" contradicts B 2.2.4 ("1: a bug; never returned on
purpose").** Use 64 `usage` with a fixed "not built in this kit" message, or keep exit 1 and record
the departure as a decision.

**m10. RN1 and CL5 omit `run`'s `--state DIR` and `--seat T/S` flags (B 2.2.1, 2.4.1 step 6),
and B47's "the keeper writes nothing to the terminal after step 7".** Add each with a test.

**m11. Missing smaller requirements.**

- B5 in general: any single-team verb with more than one active team and no `--team` is 64
  `ambiguous-team`, listing the teams with their descriptions. SD2 covers only `--to`.
- B11's duplicate token across bundles.
- B 2.3.2's `fresh: true` means step 01 runs again.
- B 2.6 step 04's re-verification triggers: after a join, on a limited timeline, on a
  trust-bearing type.
- B 2.8's Stop and wait-mode cases: blocking once per 10 minutes for a team failure and for no
  running `wait`.
- B26's "never for items the socket will still push".
- B43's conversation record written by the link whenever the session id changes, teams or none.

**m12. SY12's thresholds are not stated.** "A deadline long overdue" and "a
`CLOCK_BOOTTIME`/`CLOCK_MONOTONIC` gap" need numbers before a test can be written.

**m13. n3-D12 is ambiguous about stopped containers.** "Treated as a stopped one … entry leaves
the record" can be read as covering stopped containers too. That would contradict C 3.9.2, under
which a stopped container's guest files are cleaned "at the next phase that finds the container
running", which needs the record entry kept. State that a stopped container's entry stays (`left`,
files pending) and only a missing container's entry is dropped.

**m14. RC1 does not cover a desired file deleted at run time (`IN_DELETE`).** n3-D16 makes a
missing file a launch fault. Say whether a deletion under a live keeper keeps the last good
membership with `desired.state` `refused`, and which `code` it gets.

## NIT

- n1. Gap 1 overstates: `paths.GuestRuntimeDir` and `paths.SessionUnit` exist
  (paths.go:150-153). The unit file path and the wants-link path are what is missing.
- n2. n3-D5's inline filter is the simpler choice, and it makes resends naturally idempotent. Pin
  the largest roster's filter (16 humans and 64 members) as a URL against Tuwunel in the
  `-tags tuwunel` suite.
- n3. n3-D14 is correct only because the path is made relative for `RESOLVE_BENEATH`; with
  `RESOLVE_IN_ROOT` an absolute path is fine. Say which form the code passes.
- n4. B15's ban on `PR_SET_PDEATHSIG` and `LockOSThread` has no expectation. A one-line rule or a
  grep test would hold it.
- n5. GU2's `gh auth status` makes a network call to GitHub from the guest. Say so, so that a
  firewalled guest's 78 is understood.

## Checked and sound

- **Gaps 2-10 verified against the code:**
  - 2: `seat.PlaceGuest` does exchange and remove, guest.go:55-62.
  - 3: only the two decoders exist, records.go.
  - 4: `seat.DecodeLockRecord` exists, but the record has no encoder.
  - 5: `RoomFilter` drops a zero limit and has no presence senders.
  - 6: no restore in fakehs.
  - 7: execx has `Run`, `Exec`, `ExtraFiles` and `NewSession`, but no `Start`.
  - 8: `cmdmaintest` is under `qa/internal`.
  - 9: no `busfailpoints` tag exists.
  - 10: no production `health.Guests` exists.
- **n3-D1** catches a real contradiction in the design (but see M6).
- **The gate marks** are right. The LXC path uses token seats, so the only [B] is LN9's host run.
  Every host run of n3b, the kit placement, the unit and LP1 are [A].
- **The slice order** is sound otherwise. n3b and n3c are independent after n3a, apart from M8's
  kit and unit caveat.
