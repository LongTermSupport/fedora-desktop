# SPEC-n3 review round 1: fix log

Review: `261022-n3-spec-review.md`. One line per finding: what changed in `SPEC-n3.md`, or why not.

## MAJOR

- **M1.** UH1 rewritten: `not-addressed` counts only when no roster agent is addressed (mentions, `@room`, reply or thread root by a roster agent; also an edit of a message addressed to one, as `humanAddressed` has); `type` and `empty` at every member, with why; test for X-only mention not counted at Y (and a reply to X's event). Gap 11 now adds proto's "reached no agent" predicate.
- **M2.** DV6 added (membership NOTICE per active-set or role change, `told` once per generation, UserPromptSubmit fallback, tests for join, leave reasons, role change, failed write then prompt, link restart). SY11 points its NOTICE at DV6. Section 11 maps B30 to DV6, SY11.
- **M3.** DV3 extended: link picks, proto renders; 8 entries and 12 KiB, rest next delivery; HUMAN before PEER/PING then time; connect/auth failure stays pending, retry 10 s; cap-edge tests. Body cap assigned to proto's renderer (`proto.MaxBodyBytes`, escape.go) in DV2. Section 11 maps B23, B24.
- **M4.** TB2.4 rewritten: closed stdout grammar per op, re-emitted only in the host's form; error object decoded strictly, only `exit` and `code` printed; hostile rows listed. LX6 and LX9 refer to it; GU1 names the fixed `what` set.
- **M5.** SU2 opens the guest socket by TB1's socket rule (O_PATH, `S_ISSOCK` owned by uid 0, connect via `/proc/self/fd/<n>`); TB1 states the rule for every guest socket and adds the symlinked `private` fixture (relative and absolute).
- **M6.** GU3: a different `user_id` or seat (or an undecodable `seat.json`) takes the seat lock (75 if held), sets the store aside as SA1, then places; tests for each case. MS3 reads token and `seat.json` as a pair (re-read check, one retry, then LN9's next event); "mismatch" defined. LN9 says a torn pair never leaves the team on 78. GU1 adds 75 for `place-bundle`; LX6 makes a present-phase 75 `DEFERRED`; LK3 lists GU3's identity change.
- **M7.** LP2 added to n3b (child's `member.disconnected` calling `notice.Disconnected` with the guest token copy, production `health.Announcer`); states the rest of the tick's wiring is N2's later wiring slice under gate A; T22's `lxc-stop -k` row and E2's next-start notice marked as depending on it. Gap 10 extended; slice table updated.
- **M8.** KT1 added to n3a (layout, `hooks.json` by absolute path, `settings.json` status line, `<digest>` = source digest; skill joins in n3e; host placement and `installed.json` are the play's, [A]). PK1 now refers to KT1 and proves the files in use. n3-D4 updated. n3b's VM claim: placement and an accepted unit start (or `service: false`), no running session until n3c.
- **M9.** Gap 12 added (injected timer for fakehs's long-poll hold); header and SY7 say the cases run on the fake clock; F1/F4 rows cite gap 12.
- **M10.** P1-P11 listed with B's wording in section 11; P3 mapped to SY5, P6 to LK3 (DV3 now P1, P2); PT1 added to n3e naming the failpoints one by one. The review says 15; B 2.15's list holds 14, and PT1 names all 14. Gap 9 says each slice adds its own failpoints.
- **M11.** Section 0 and section 7: n3f gets `SPEC-n3f.md`, reviewed to MERGE before n3f starts; this spec's MERGE covers n3a-n3e; SR1/SR2 kept as the scope list, with the reviewer's open points listed for the addendum.

## MINOR

- **m1.** TB1: reopen is a plain `openat(AT_FDCWD, ..)`, never `O_NOFOLLOW` or `openat2`+`RESOLVE_NO_MAGICLINKS`; exempt from TB4 and any project rule; locks on the reopened fd (LP1 says so); opener-seam assertion added to the tests.
- **m2.** LX4: systemd as init by host `/proc/<pid>/exe` O_PATH, device and inode against the guest's `/usr/lib/systemd/systemd` via TB1. LX5 and TB1: directory opens with `O_DIRECTORY`, entry names through the run-id grammar; guest `/proc/<pid>/stat` read by TB1 without `RESOLVE_NO_XDEV`. EN5 unchanged.
- **m3.** n3-D17 added (TB1's departure from C 3.9.2 step 03 and the L3 probe; the magic-link `exe` check). n3-D1 now cites B 2.13's `place-bundle` row.
- **m4.** LP1: the record is returned in both states (held for the run id, free for `launch_failed_ms`), as `health.GuestProbe` carries it.
- **m5.** SY7: the resend inherits the 45 s deadline; case 3 runs early-drop and late-drop timings; case 8 added for the 429 resend. Gap 6: `RestoreTo` raises `restore_epoch` in the same step as the rollback.
- **m6.** RS2: n3c lands the `RestartRecord` seam with a nil adapter and the table test (slice table too). RS3: the PLAN 2.0e row stays open until the adapter is wired after RS3. RS1 states the `left exit` with no following notice.
- **m7.** CL1: uses the existing `go_command_static: true`, no build-task change; `keepout` row for `./cmd/teamchat`. Gap 13 added for godbus (module decision, allowlist row, `teamchat-admin` only, capped reader since no read cap is relied on).
- **m8.** GU2: `gh` checked on the unit's `PATH` with `tmux` and `claude`.
- **m9.** Per the author's decision: exit 1 `internal` kept for an unbuilt verb, stated as a build-time placeholder with a test that the unbuilt list is empty before Task 3.3 installs the binary.
- **m10.** CL5 parses `--state DIR` and `--seat T/S` (with their 64 cases); RN1 covers `--state`, RN2 `--seat`; RN1 adds B47's no-terminal-output-after-step-7 test.
- **m11.** CL6 added (B5 `ambiguous-team` for every single-team verb; SD2 cites it); MS3 adds the duplicate token (B11); SY1 adds `fresh: true` re-running step 01; SY14 added (B 2.6 step 04 triggers); HK2 adds the team-failure and wait-mode 10-minute blocks and B26's "never for items the socket will still push"; LN7 adds B43's conversation record (T6 row cites it).
- **m12.** SY12 states thresholds (deadline overdue by more than 10 s; BOOTTIME minus MONOTONIC grown by more than 2 s) with threshold tests; recorded as n3-D18, since B37 gives no numbers.
- **m13.** n3-D12 and LX2 rewritten: a stopped container's entry stays (`left`, files pending); only a missing container's entry is dropped.
- **m14.** RC1: a deleted desired file under a live keeper keeps the last good membership with `desired.state` `refused`, `code` `missing`; n3-D19 records the new code, since B 2.3.3's closed set has none for it.

## NIT

- **n1.** Gap 1 now says `GuestRuntimeDir` and `SessionUnit` exist; the unit file path, wants-link path and the second runtime parent are what is missing.
- **n2.** n3-D5 notes resends are idempotent by construction and pins the largest roster's filter (16 humans, 64 members) as a URL in Task 3.4's `-tags tuwunel` suite.
- **n3.** n3-D14 says the code passes the clean absolute path with `RESOLVE_IN_ROOT`, and why `RESOLVE_BENEATH` (EXDEV on absolute) is not interchangeable as text.
- **n4.** KP1 adds the B15 rule (no `PR_SET_PDEATHSIG`, no `runtime.LockOSThread`) with a source-scan test.
- **n5.** GU2 says `gh auth status` is a network call to GitHub, so a firewalled guest fails with 78 `gh`.
