# Handoff: ccy Go caches on the host (branch agent-ad49d4886a70d25db-01b66214)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Not plan work. Stopped at the owner's wind-down.

## Done

- Review round 3 fixes (2d2cb9e78): the project lock moved to `~/.cache/ccy/<project>/go.lock`,
  outside the bind. The bound counts allocated blocks, and a total still over after both stages
  is exit 1. The docs now state that the bound is per project and give the recovery for files
  owned by a subuid.
- F44 merged (351bf8598). CCY_VERSION is 3.100.22, because the hook requires a bump on the
  merge commit. The changelog entry sits above F44's host cc entry and 3.100.18. The launcher's
  bash-limits record was tightened to 2770/2706. The launch-usage-unknown golden was
  re-recorded.
- Review round 4 (Opus): CHANGES, one blocker, B1. The docker named volume was justified as
  "rootful docker root is host root". That reason is false, because the launcher refuses
  rootful engines before go-cache runs. As a result the volume was unbounded and never removed.
  The full report is `261023-ccy-go-cache-review-r4.md`.
- B1 fix, in the WIP commit that carries this file: docker now gets the same bind, relabel and
  bound as podman. `volumePrefix` is removed, and the test is now
  `TestUnderDockerTheSameBoundedBindHoldsTheCaches`. The package doc G1/G2, usage.txt,
  docs/ccy.md and the changelog are updated to match. The go tests for teams and gocache pass,
  and go vet passes.

## Not done

- `./scripts/qa-changed.bash` has not been run on the B1 WIP commit.
- The earlier merge run of qa-changed passed every gate except two, and both looked like
  load flakes (load average was about 70):
  - go-qa: a 10-minute timeout in `qa/changed`'s golden test. That package passes in 20 s
    when run alone.
  - `test-wsi-stop-grace.bash`: timing failures that differed between runs. The suite is
    pulled in by F44's execx change.

## Next steps

1. `cd` into the worktree and run `./scripts/qa-changed.bash` while the host is quiet. Fix
   anything real.
2. Run review round 5 with Opus (Agent tool, model opus, no isolation), giving it the
   `git diff F44...HEAD` and not these conclusions. Loop until the verdict is MERGE.
3. Non-blocking items from round 4 to consider:
   - The `changed-plays` description in `CLAUDE/Plan/meta-deploy.list` describes only
     3.100.18. Add the go-cache change to it.
   - The lock waits without a bound or a message.
   - The real flock is never tested with two concurrent launches.
   - The difftest never shows go-cache failing.
   - The cost of the `:z` relabel is unmeasured.
4. Then hand the branch to the coordinator. The host play is `play-claude-yolo.yml`, through
   meta-deploy.
