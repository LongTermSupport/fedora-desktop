# Plan 00215 rework review, round 6 (`feat/opencode`, `git diff F44...HEAD`)

Review only. The one change to the branch is this report, which is not committed. HEAD is
c8f04f2c6. F44 is now 9511c5b80, one commit past the merge base a7377f8c9. That commit touches
`CLAUDE/AgentNotes.md` only, and `git merge-tree --write-tree F44 HEAD` is clean.

Checks run in the worktree (logs under `untracked/scratch/r6/`). All passed:
`bash scripts/test-ccy-agent-choice.bash` (65 passed, 0 failed),
`scripts/test-ccy-image-build-lock.bash` (87/0), `scripts/test-ccy-teams.bash` (231/0),
`bash scripts/test-ccy-token-handoff.bash` (123/0), `scripts/test-meta-deploy.bash` (26 passed),
`go test -count=1 ./internal/ccylaunch/... ./internal/registry/...` (all ok),
`scripts/qa-bash-limits.bash` (BASH-LIMITS-OK, 509 files).

## The round 5 answers (c8f04f2c6)

- **Finding 1 (SHOULD-FIX, meta-deploy put the build at the next launch). The answer holds and is exact.**
  - Plan 00213's `deploy.bash:61-62` runs the whole `play-claude-yolo.yml`.
  - The play's locked build (`play-claude-yolo.yml:1263-1284`) always builds `claude-yolo:latest`. The Dockerfile's
    `LABEL claude-yolo-version` change (`Dockerfile:36`) comes before the opencode `RUN`
    (`:372-384`), so the layer cache is not reused, and that run downloads and checks opencode.
  - A failed curl or sha256 fails the task, then the play, then that `plan_deploy_leg`, then the
    entry. The 00213 entry now says this (`meta-deploy.list:48-50`).
  - The play-claude-yolo.yml entry (`:97-100`, `:134-135`) now says the image is already 2.79.1, built by the 00213 entry, and that the next launch rebuilds only a project's own image. This matches `validate_container_version` against the play-built labels. It also matches the 00211 entry (`:67-68`).
  - The closing "it must rebuild, start and attach" (`:137-138`) now means the project image, which is correct.
- **Finding 2 (NIT, `--debug` layer 3). The docs answer is adequate and true.**
  - `docs/ccy.md` ("opencode") now says layer 3 is Claude Code's `--debug`, does nothing for opencode, and that the chooser still ticks it (`claude-yolo:1029`, `:1070`). The CCY and entrypoint layers still apply.
  - Nothing breaks. The owner is told about the gap where the flag is documented.
  - Leaving the code alone to stay inside the bash limits is reasonable for a NIT.
- **Finding 3 (NIT, the play builds `:latest` only). The answer is adequate for a NIT, but the attribution is wrong. See finding 1 below.**
  - The facts hold. `ccy_base_image_build` (`lib/image-build.bash:25-31`) builds `:base` with `--target base` (`lib/common.bash:1031-1037`). The play does not.
  - opencode is in the `base` stage (`Dockerfile:24`, `:372`, before `FROM base AS full` at `:500`). So a `FROM claude-yolo:base` project keeps the old base without opencode, and `ccy -oc` there fails loudly (fail-fast holds).

## Re-review of the whole diff

These were re-read against the code and found consistent with it:

- The launcher hunks: the parse, `ccy_agent_options` placed before every image, lock and prompt step, `ccy_agent_command`, and `ccy_agent_container` with `-e NAME` only and `-x` paused around key reads.
- The suspect-flag list, the Quick Launch kept-team path, the registry keep and no-restore lists, and the Dockerfile block.
- `docs/ccy.md`, the changelog, `CLAUDE/QA.md` and the update-versions skill.

Versions agree: launcher 3.100.6, container 2.79.1 (the label and `REQUIRED_CONTAINER_VERSION`), and the changelog heading. Nothing install-specific is in the diff. No secret reaches any argv.

## Findings

1. **NIT: PLAN.md Task 1.4 and the journal blame Plan 00216 for the `:base` gap, but the gap is older.**

   - `PLAN.md` Task 1.4 says "a gap in the play's build from Plan 00216". The journal line `JOURNAL/00215-Journal-26-10-22.md:100` has the same attribution.
   - Before Plan 00216's merge (`git show 87e3e6689^1:playbooks/imports/play-claude-yolo.yml:1260-1267`), the play already built `-t claude-yolo:latest` only.
   - The `:base` tag dates from fe9fb6877 ("add LSP support with 2-stage image (base + full)"). The play was never taught to build it.
   - The note also says "until `:base` is rebuilt" but not how.

   **Fix:** in PLAN.md, say the play has built `:latest` only since the two-stage image (fe9fb6877), not "from Plan 00216". Name the way out: `ccy --rebuild` in that project builds both tags through `ccy_base_image_build`. The note "raised with the coordinator" lasts only for this session, so record the gap where it will outlive the plan: a GitHub issue, or a line in Plan 00216's follow-ups. The journal is append-only, so correct it with a new entry, not an edit.

2. **NIT: the changelog still says "the next launch rebuilds it once" (`docs/ccy-changelog.md:23`).**

   - That is the claim c8f04f2c6 removed from meta-deploy. The launcher is installed only by the play, and the play always builds the image, so for this release the image is rebuilt by the play, not at the next launch.
   - The phrase is the changelog's house style: F44's own 3.99.15.0 entry uses it (`:60`). It is general user-facing text, not the exact host-run list, so this does not block.

   **Fix (optional):** "The image changes; the play that installs this version rebuilds it."

VERDICT: MERGE
