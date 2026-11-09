# N2 slice n2a: handoff (the builder stopped at its context budget)

Worktree `untracked/worktrees/bus-v2-n2a`, branch `feat/bus-v2-n2a`, stacked on
`feat/bus-v2-n1` at fe520f80. Tip at handoff: the commit that adds this file.

## Done, committed

- `SPEC-n2a.md` (plan folder): CA1-CA7 (verbs, caller classes, exit table, `version`),
  CC1-CC6 (the `teamchat-ctl` child's boundary), RE1-RE10 (the rooms engine), and decisions
  n2a-D1 to n2a-D13 where the design is silent.
- `src-go/internal/teamchat/admin` + `src-go/cmd/teamchat-admin`: C 3.3's verb table, the
  caller-class check (exit 77), every unbuilt verb exiting 1 "not built yet", `version`
  (baked digest via `-X main.SourceDigest`), the child's typed start spec, the child's check
  of how it was started, the request and answer codecs (both strict), and `ExitCodeOf`
  (drift 70, unreachable or rate-limited 69, else 1). Coverage 96.8%.
- `src-go/internal/teamchat/rooms`: the engine, tested end to end against fakehs: new team,
  idempotent second run, adoption after a crash at each room, two candidates refused, a side
  room linking another main refused, kicks before state before invites, re-invite after a
  leave, roster rev rule, invalid roster refused before any write, drift refused before any
  write, foreign-sender state rewritten, the failed step named, and the trust round trip
  (`proto.VerifyMain`/`VerifySide` accept the rooms). Coverage 98.4%.
- `scripts/go-binary-suites.tsv`: `teamchat-admin qa-go`.
- Every commit: `qa-changed.bash` red only on govulncheck GO-2026-6809; the
  teamchat-keeps-off-v1 gate green. The real-Tuwunel harness was not used.

## Left

1. PLAN.md: mark progress on Task 2.1 (covered: the command skeleton, caller classes, the
   child boundary as a spec, codecs and self-check; not covered: starting the child, which
   needs `execx` to carry a credential and a replaced environment; DC17's dependencies, not
   needed by this slice) and Task 2.3 (covered: the rooms engine of C 3.6.2 step 4 only; not
   covered: install and apply, the roster file and `rooms.json`, members, humans, roles, the
   reserved localparts).
2. The review loop: one Opus adversarial reviewer over `git diff fe520f80..HEAD` against
   C 3.3, C 3.6.2 step 4, A 4-5.3, seams 13, 14, 17 and SPEC-n2a.md, writing
   `subagent-reports/261021-n2a-review.md` (MERGE or FIX-FIRST); fix and loop, at most 3 rounds.
3. The report `subagent-reports/261021-n2a.md`, with the design-text fixes to propose:
   - C 3.6.2 step 4.1: main has no back-link (A 5.3); say how main is adopted (n2a-D7).
   - C 3.3: the child's failure answer shape and its classes (n2a-D4).
   - C 3.3 / SPEC-matrix: `matrix.Error.ExitCode` is v1's member table (7-10), not C 3.3's;
     the host maps by class (n2a-D13).
   - C 3.6.2 step 4.2: A gives no kick reason (n2a-D9).
