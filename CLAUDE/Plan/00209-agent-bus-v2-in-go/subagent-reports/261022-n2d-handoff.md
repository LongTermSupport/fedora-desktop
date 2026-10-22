# N2 slice n2d: handoff 1

The lead stopped at its context budget. Branch `feat/bus-v2-n2d`, worktree
`/workspace/untracked/worktrees/bus-v2-n2d`, tip after this file's commit. The contract is
`SPEC-n2d.md`: sections 1-3 are the lead's, section 4 the host half's.

## Done

- `SPEC-n2d.md` (MO1-MO8, CV1-CV6, n2d-D1 to D7; section 4 HS1-HS14 and n2d-H1 to H16).
- **The host half**, built by a sub-agent on `feat/bus-v2-n2d-host` and merged by fast-forward
  (ab500e32): the team lock, `host.Apply` (team apply steps 1-9 and 11, step 10 a function in
  `host.Deps.Bootstrap`), `host.Remove`, `paths/host.go`. Its report is
  `subagent-reports/261022-n2d-host.md`, with the exported API. Coverage: host 93.3%, paths 100%.
- **Step 1 (member operations)**, commit 91032ce9, in `internal/teamchat/apply`:
  - `member.go`: `RunMember(ctx, deps, team, MemberRequest)`, `RoleApply(ctx, deps, team)`,
    the step machine (`opSteps`, `resume`, `rollForward`), the credential (`Credential` hides
    its secret in every format), and the admin's notices composed by `proto.Composer.Notice`
    and sent with a transaction ID from the pending record (`noticeTxn`).
  - `apply.go`: `Admin` gained `ResetPassword`, `SetDeactivated`, `LoginDevice`, `Send`.
    `Error` gained `Op`, and `Result` gained `Handle`, `Credential` and `Notes`. `newRun`
    is shared. n2b-D22's refusal is replaced by `rollForward` in `load`. A role change is now
    a pending step `role <change> <handle>` that posts the `role` notice (`role`, `finishRole`),
    so n2b-D9 is superseded. The recorder leaves another op's pending record alone during a create.
  - `member_test.go`: every op, refusals, seat next, promotion of the wanted coordinator, and a
    crash at each step (the `crashing` admin wrapper: fail the n-th call of a method, with its
    effect landed or not) rolled forward by the same verb (a working credential, one notice)
    and by another verb (revoked, `needs: reissue`, no credential). Also team apply rolling a
    pending add forward, a role step finished after a crash, and a pending team.apply finished
    by a member verb.
  - `go test -race -cover`: green, 90.7%.

## Left

1. **Lint** on 91032ce9: 12 golangci-lint findings in `apply/member.go` (`target` cyclop 17 > 15:
   split the add branch out) and `apply/member_test.go` (named returns in the `crashing`
   closures; `hit` after the exported methods; a blank line after the embedded `Admin`; the
   embedded `crash` first in the park/retire table; cyclop or gocognit of `TestMemberAdd`,
   `TestMemberLifecycle` and `TestAddRollsForwardFromEveryStep`: split them). Then run
   `./scripts/qa-changed.bash` and commit.
2. **Step 2, the verbs** (SPEC CV1-CV5), in `internal/teamchat/admin`:
   - the child serving `member.*`, `role.apply` and `team.apply`, with strict per-op args
     (`team_file` decoded by `declare.DecodeTeamFile`; for `team.apply` the input and the
     approval);
   - the answer codec gaining `credential` and `notes` (CV2);
   - the root verbs `member add|return|reissue|park|retire|set` and `role apply` (flags, the
     team lock `host.TakeLock` with `*host.BusyError` mapped to 75, the child seam, the
     CHANGED lines, the JSON answer with `bundle` from a `Bundles` seam for the seat side);
   - `team approve` (CV4: `declare.Approve`, `declare.FileApprovals`, and an `OwnerGroup`
     seam);
   - `team apply` and `team remove` over `host.Apply` and `host.Remove` (CV5);
   - `admin.ExitCodeOf` taught `host.Error` and `host.BusyError`;
   - the verb table rows (`buildTable`) get their `run` functions.
     The child start stays a seam (n2a-D6): an interface whose test form serves the request
     in-process through `EncodeRequest`, `DecodeRequest`, `EncodeAnswer` and `DecodeAnswer`.
3. **Step 3**: the end-to-end tests through the verbs on fakehs (CV6). Also an optional tagged
   Tuwunel test of add, role and retire, beside `apply/tuwunel_test.go`.
4. The review loop (Opus, at most 3 rounds), the report `subagent-reports/261022-n2d.md`
   (merge in the host report's decisions), and PLAN.md Task 2.3's progress. The report must
   name the n2b decisions superseded (D9, D22) and propose these design-text fixes:
   - C 3.6.3: the step names and the role step;
   - C 3.6.3: `--seat next` (n2d-D3);
   - A 7.4: the notice bodies (n2d-D5);
   - C 3.3: the child's request args (n2d-D7).

## Notes for the next builder

- The guest token copy (`ctl/<team>/guest/<handle>.token`) is not written by `credential`;
  it is the seat side's (n2c `GuestTokenCopy`) or `lxc apply`'s. Say so in the report.
- `seatDevice` in `apply/member.go` duplicates n2c's `paths.SeatDevice`; replace it at merge.
- The coverage floor is per package by `go test -race -cover` (90%). go-qa stops at govulncheck
  GO-2026-6809 (Task 1.6), the only acceptable red; say so in each commit message.
