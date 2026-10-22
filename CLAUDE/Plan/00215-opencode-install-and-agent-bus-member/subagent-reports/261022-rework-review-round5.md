# Plan 00215 rework review, round 5 (`feat/opencode`, `git diff F44...HEAD`)

Review only; nothing in the branch was changed. HEAD 5cccf5bd8, F44 a7377f8c9 (F44 is the merge
base, so the branch merges with no conflict: `git merge-tree --write-tree F44 HEAD` exit 0).

Checks run in the worktree (logs under `untracked/scratch/r5/`), all green:
`bash scripts/test-ccy-agent-choice.bash` (65 passed, 0 failed), `scripts/test-ccy-teams.bash`
(231/0), `scripts/test-ccy-session-registry.bash` (204/0), `scripts/test-ccy-image-build-lock.bash`
(87/0), `bash scripts/test-ccy-token-handoff.bash` (123/0), `go test -count=1 ./internal/ccylaunch/... ./internal/registry/... ./internal/sessions/... ./internal/teams/...`
(all ok), `go test ./all/` in `src-go/qa` (ok), `scripts/qa-bash-limits.bash` (BASH-LIMITS-OK),
`shellcheck -x` on `lib/agent-choice.bash` and its test (clean).

## The round 4 fixes hold (01ec713b1)

- **Finding 1 (provider key in `--debug`'s trace).** `ccy_agent_container`
  (`lib/agent-choice.bash:85-98`) notes whether `-x` is on, turns it off silently, reads the
  keys and sets each with `local -x`, turns `-x` back on, then calls `container_cmd "$@"`.
  `local -` restores the options when the function returns. The argv of the engine call holds
  only `-e NAME`. The test (`scripts/test-ccy-agent-choice.bash:194-198`) runs the function
  under `set -x` and checks three things: neither test key is in the trace, the
  `container_cmd run` line is still traced, and `-x` is on again afterwards. Holds.
- **Finding 2 (branch did not merge into F44).** Merged twice (a60d55d7f, 5cccf5bd8). F44 is
  now an ancestor of the branch. Holds.
- **Finding 3 (CRLF key).** The awk at `:50` refuses a file with a CR, more than one line, or
  only blanks. The `find -maxdepth 0 -type f -perm 600` refuses a link or the wrong mode.
  Holds.

## The merge resolution (5cccf5bd8)

Checked, and correct:

- **claude-yolo.** `CCY_VERSION="3.100.6"`. `CCY_LIBS` keeps F44's `image-build-lock image-build`
  and adds `agent-choice` at the end. The `source` lines run in the same order. Every hunk in
  `git diff F44 HEAD` comes from this branch. No line of F44's (the lock wrappers at
  `:1962`, `:2033-2061`, `:2145`, `:2189`, `:3798`) is changed or lost.
- **play-claude-yolo.yml.** The lib list has `image-build-lock.bash`, `image-build.bash` and
  `agent-choice.bash` (`:496-498`). The play's own locked build (`:1266-1284`) is unchanged.
- **Versions.** They agree everywhere: launcher 3.100.6 / `REQUIRED_CONTAINER_VERSION` 2.79.1,
  Dockerfile `LABEL claude-yolo-version="2.79.1"`, the changelog heading `3.100.6 — container 2.79.1` (follows 3.99.15.4), PLAN.md Task 1.3 (3.100.6) and Task 1.2 (2.79.1), and all three
  meta-deploy mentions. A grep finds 2.78.15 and 3.99.15.4 only in historical changelog entries
  and other plans' reports.
- **Registry / restore.** Bash `CCY_REGISTRY_DROP_FLAGS` has F44's `--build-lock`, and
  `KEEP_FLAGS` has `--opencode -oc`. The Go `dropFlags` and `keepFlags` match them
  (`registry/restore.go:46-49`).
- **addedgates_test.go / qa-gates.tsv.** Both have `ccy-image-build-lock` and `ccy-agent-choice`
  rows. **bash-limits.allowlist.** claude-yolo shrinks: 2782/12/2713, from F44's 2784/12/2714.
- **Goldens.** In the two re-recorded opencode goldens, only the `image inspect ... --format '{{.Id}}'` line changed, the same change F44 made to every launch golden.
- **--opencode and the build lock.** `ccy_agent_options` runs right after the parse
  (`claude-yolo:855`), so a missing or bad key exits before any image check, lock or build. An
  opencode launch then goes through the same `ccy_base_image_current` / project-image /
  daily-update locked paths as any launch. The flag does not change the image or its
  version/hash checks, and none of them needs to tell the two agents apart. opencode is
  installed in the `base` stage (Dockerfile `:369`, before `FROM base AS full` at `:500`), so
  `:latest` and project images built on it carry it. Restart-on-a-fresh-container (`:3798`) is
  reached only through the supervisor, which an opencode session does not have. The front's
  `--build-lock` answers only when it is the first argument (`front.go` Run), and refuses any
  other argument (64), so `-oc` cannot reach it.

## Findings

1. **SHOULD-FIX: meta-deploy.list says the opencode download happens at the next ccy launch.
   The play does it, and the first entry to run the play is Plan 00213's `deploy.bash`.**

   - `play-claude-yolo.yml:1263-1284` always builds `claude-yolo:latest` under the lock, with
     `DOCKERFILE_HASH` set. "The check is false: the play always builds."
   - The image that build makes carries the version label 2.79.1 and the matching hash label.
     So at the next launch, `validate_container_version` (`lib/common.bash:934`) returns 0 and
     the base image is not rebuilt. Only project images (FROM `:latest`) are rebuilt, because
     the base image's ID changed.
   - The play is first run inside the first entry, `00213-agent-bus-performance-go-carve-out/deploy.bash`
     (`meta-deploy.list:43-57`). That is the entry where opencode 1.19.3 is fetched from
     github.com and checked against its sha256. A changed asset or a network failure stops
     that entry. Its description is about the bus status line, and it says the image is
     "rebuilt once at the next ccy launch" (`:48-49`).
   - The play-claude-yolo.yml entry says the same: "Container 2.79.1: the next launch rebuilds
     the image once, for ... opencode 1.19.3 ... downloaded at build and stopped unless its
     sha256 is the pinned one" (`:96-98`), and again at `:132-133`.
   - The "next launch rebuilds" wording was already on F44 for 2.78.15. This branch adds the new
     failure mode, a download that has to pass a checksum, to text that puts the build in the
     wrong place. The list must be exact (AgentNotes), and the owner reading it would look for
     an opencode failure at the wrong step.

   **Fix:** in the 00213 entry (`:47-49`), say its play run builds the 2.79.1 image, which
   downloads opencode 1.19.3 from github.com and stops the entry if its sha256 is not the
   pinned one. In the play-claude-yolo.yml entry (`:96-98`, `:132-133`), say the image is
   already 2.79.1 by then (as the 00211 entry at `:66-67` already says correctly), and that the
   next launch rebuilds only a project's own image. Task 1.4's check (`ccy --opencode` starts
   opencode) is unchanged.

2. **NIT: the `--debug` chooser's layer 3 ("Claude Code (--debug flag to claude)", in the
   default "all") does nothing for an opencode launch, but still prints "✓ Claude Code
   (--debug)".**
   `claude-yolo:3541-3542` appends `--debug` to `CLAUDE_CMD_ARGS`, and `ccy_agent_command`
   builds opencode's command from `CLAUDE_ARGS` (`lib/agent-choice.bash:73`). Nothing breaks;
   the owner just gets no agent-level debug output and is told otherwise. **Fix:** for
   opencode, either map layer 3 to opencode's `--print-logs --log-level DEBUG`, or make the
   chooser say layer 3 does not apply to opencode.

3. **NIT, inherited from F44 and not this branch's to fix here: the play never retags
   `claude-yolo:base`.**
   The play builds `:latest` only (`play-claude-yolo.yml:1273-1282`). After the play, nothing
   rebuilds `:base` at the next launch, because `:latest` is current. So a project Dockerfile
   that opts into `FROM claude-yolo:base` (the lean base `build_container_with_hash` documents)
   gets a project image on the old 2.78.15 base, with no opencode in it. `ccy -oc` in that
   project would then fail loudly with opencode not found (fail-fast holds). The default
   project template uses `claude-yolo:latest` (`lib/dockerfile-custom.bash:202`), so only
   opted-in projects are hit. **Fix (separate, Plan 00216 territory):** have the play's locked
   build also build `--target base -t claude-yolo:base`, as `ccy_base_image_build` does. Or
   note the gap in Task 1.4's hand check.

VERDICT: FIX-FIRST
