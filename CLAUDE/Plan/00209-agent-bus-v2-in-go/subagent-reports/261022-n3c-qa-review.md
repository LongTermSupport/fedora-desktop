# QA review: Plan 00209 N3 n3c (727087da9..d3e3bc045, 101 commits, 181 files)

The qa-reviewer has no Write tool, so the lead saved this text from its message.

**Verdict**: FIX-BEFORE-MERGE

## Blocking

1. **The new busfailpoints stage failed in qa-all: `--- FAIL: TestE2EKeeperKilled (32.15s)`.**
   - The test is at src-go/internal/teamchat/member/wiring/e2e_test.go:134. The go-qa summary
     reads `runners teamchat-failpoints-test FAILED`; every other go-qa stage passed.
   - It took 32 s, which matches `eventually`'s settle bound (e2e_world_test.go:332). So the
     `disconnected` notice never arrived: this was not a slow run that only missed the 5 s
     `linkEndBound`.
   - In isolation it passes: 7 of 7 runs (`-run TestE2EKeeperKilled$ -count=5`, then the whole
     tagged package at `-count=2`). It fails only under full-suite load, and the cause is not
     found. Until it is, treat it as a possible real race in the link's pidfd and getppid watch
     (LN1, T20), not only timing.
   - Since go-qa is part of qa-all, F44 cannot be pushed while this stage can go red.
   - Fix: reproduce it under load, for example `-count=20` while the rest of the teamchat stage
     runs in parallel. Capture the `not within` line and fix the cause. Do not raise settle.

## Should fix (plan and docs drift)

2. **23 n3c decisions are cited in production code but recorded only in subagent reports.**

   - The code cites n3c-D1 to n3c-D24, for example in state/report.go, link/link.go:38 and :282,
     wire/control.go:98, keeper/loop.go:204 and wiring/wiring.go:2.
   - They are defined only in subagent-reports/261022-n3c-handoff.md, and n3c-D24 only in the p3
     and p4 handoffs.
   - `grep -c n3c-D` gives 0 for DESIGN-v2-\*.md and PLAN.md, and 1 for SPEC-n3.md, inside the
     n3-D20 row only. n3a put its decisions into SPEC-n3's decision table as n3-D1 to n3-D20;
     n3c did not.

   Specific contradictions:

   - **report.json gains `why`** (state/report.go `Why *string json:"why"`, citing B 2.3.3). B
     2.3.3's schema block (DESIGN-v2-B-member.md:752-755) has no `why`, and area C reads this file
     (C8).
   - **n3-D2 is out of date.** SPEC-n3.md says `none` carries the reason `restart` only. The
     code's noneReasons is {restart, launch-failed} (state/store.go:87, :95), per n3c-D20.
   - **n3c-D7 is missing from the spec.** It sets wait's 110 s default and 3600 s maximum
     (client/client.go:35-36), which neither B 2.2.1 nor SPEC-n3 states. The fix report
     261022-n3c-fix.md:22 says the "n3c-D7 row" went into SPEC-n3.md. Commit d3d1eec22 added only
     W-D1 and W-D2 to SPEC-n3, and the row went into the handoff, so the report's claim is false.
   - **W-D3** (failpoints armed once per verb by the test binary) is not in the spec either.

   Fix: add n3c-D1 to D24 and W-D3 as rows in SPEC-n3's decision table. Fold the schema changes
   (report `why`, the launch-failed none-reason, wire.Launch.Seats) into B 2.3.3 and the related
   B sections, as n3-D19 did for desired codes.

3. **PLAN.md Task 3.2 has no n3c line, and no JOURNAL entry mentions n3c.**

   - PLAN.md:374-380 still ends "n3b to n3f are left".
   - `grep -c n3c` gives 0 for all three day-files, and N3 as a whole has no entry.
   - This was listed as known, but it still breaks the Plan Commit Rule for a slice of 101
     commits.
   - Add a coverage line like n3a's, and a journal entry made with mkplan.bash --journal. One
     coverage line exists, in 261022-n3c-w.md:53 (wiring 100, client 91.0, keeper 91.0, link
     91.1, and so on).
   - The go-qa floor is per module (the QA.md row, src-go/CLAUDE.md), not per package. The
     per-package 90% rule is met only because those reports measured it, so state the figures in
     PLAN.md.

4. **The CCY version collision is wider than 3.99.14.12 against 3.99.14.12.**

   - F44 is now at `CCY_VERSION="3.100.12"`, with REQUIRED_CONTAINER_VERSION 2.79.7, and its
     changelog head is "3.100.12 — container 2.79.7". The renumber at the F44 merge has to go
     above 3.100.12.
   - The changelog heading "container 2.78.14" (docs/ccy-changelog.md, the new entry) also has to
     become F44's container version.
   - The bump itself is correct: execx is in `go list -deps ./cmd/claude-yolo`, which
     scripts/git-hooks/lib/ccy-staged.bash checks. No other changed package is in that closure.

## Nits

5. src-go/internal/teamchat/failpoint/failpoint_test.go:52 `TestTheDeclaredPointsAreN3csTen`,
   and the comment "n3c-D2's tenth", say ten. names.go and the test's own list have nine points.

6. File names that are spec IDs or jargon, so a reader cannot guess what is in them:

   - teamsync/ln6.go (history and the verb error mapping);
   - engine/seam.go (the engine's store writes and floors);
   - keeper/kp_test.go and keeper/rc_test.go.

   Name them after what they do, for example history.go and storewrite.go.

7. The CLAUDE/QA.md qa-go-qa row describes the new stage as "go test -race", without
   `-shuffle=on`. src-go/CLAUDE.md does not mention the busfailpoints build tag, or the
   teamchatrelease and tuwunel tags, anywhere.

## Checked and clean

- **Failpoints.** All nine Name constants are Hit in production code: ends.go:195,
  recv.go:354, teams.go:203, loop.go:309 and :625, seats.go:320, link.go:378, server.go:224,
  session.go:454.
  - Each one is armed by at least one busfailpoints test; all nine are in
    wiring/e2e_failpoints_test.go.
  - The release-build gate test builds the real ./cmd/teamchat. It proves it read the binary,
    through a canary string, before asserting that no point name is in it. The inert build is
    tested.
- **The e2e harness runs real processes.** The test binary is copied into a kit as
  bin/teamchat, and `run --plain --kind other` is started through execx.OS{}.Start.
  - Its TestMain calls the same cli.Run(Env{Getenv, Verbs: Verbs()}) as cmd/teamchat, and
    cmd/teamchat/main_test.go holds that Verbs equals cli.Outside().
  - Only the plain launch is exercised; the claude launch is n3e's scope.
- **dynamicNames rows** (rules/table.go): reexec `command`, failpoint `runSelf`, keeper linkCmd,
  kitVersion, resolveKit and startCmd all exist. The rename from start to command is consistent.
- **IaC graph:** no playbook, unit, task or deploy changed, and meta-deploy needs no entry.
  Nothing is misplaced.
- **Public-repo safety:** the added lines contain no real usernames, hostnames, emails or IPs.
  - Matrix IDs are placeholders: @x:s, @alice: and \*.teamchat.internal.
  - Home paths are /home/u.
  - The only other paths are untracked/worktrees names in reports.
- **Container image:** nothing baked into the image changed, so no Dockerfile LABEL bump is
  needed.

## Mechanical gates

- **qa-all.bash: FAILED** at go (teamchat-failpoints-test, finding 1).
  - toolchain, go-deps, go-vuln, bash-limits, bash, python, patterns and ansible passed.
  - ansible-syntax could not judge in this worktree because the vault password file is absent;
    that is the worktree setup, not the slice.
  - The reviewer's output filter hid any gate lines after that error.
- **plan-qa --sweep:** 0 block, 20 advise, none for 00209.
- **syntax-check:** no playbook changed.
- **qa-helper-tests, extension compat and eslint:** not triggered, since nothing under helpers/,
  tests/helpers/ or extensions/ changed.
