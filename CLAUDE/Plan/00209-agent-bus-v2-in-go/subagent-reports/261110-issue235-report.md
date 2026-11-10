# Issue 235: the ccy image builds its own generated files (report)

Plan 00209 Task 4.2 owns this. It was done on the worktree branch named in the commit.

## What was wrong

The ccy image's build context (`/opt/claude-yolo/`, filled by `tasks/claude-yolo-image.yml`)
held four files that only the play produces. A pipeline that stages the context from repository
files alone could not build the image. The sweep found all four:

- `ccy-start-claude`: Go, built by `tasks/build-go-command.yml`.
- `optional/agent-bus/pingbus-status-line`: Go, the same way.
- `optional/agent-bus/pingbus`: the zipapp, built by `helpers.pingbus.bundle`.
- `optional/teamchat/bin/teamchat`: Go, built by `tasks/teamchat-ccy-kit.yml` (Plan 00209
  Task 4.1).

Nothing else in the play writes generated content where a `COPY` takes from the context.

## Defence Before Fix

- **Class:** the build context holds image content that is not a tracked repository file.
- **Hazard:** the image cannot be built from the repository's files alone.
- **Detector:** `scripts/test-ccy-container-version-hook.bash`. This is the blocking QA gate
  `ccy-container-version-hook` in `qa-gates.tsv`. The test already walked the play's tasks. Now it
  also:
  - fills each `import_tasks` call's `vars` into the imported file. Without this, the copy in
    `build-go-command.yml` (dest `{{ go_command_dest }}`) never matched, which is how
    `ccy-start-claude` slipped through.
  - lists every task that writes anything except a copy of a tracked file where a Dockerfile
    `COPY` takes from the context. That covers a `remote_src` or untracked copy, a command, and
    content.
  - checks that this list is empty.
- **Red:** on the old tree the detector named exactly the four files above.
- **Green:** after the fix.
- **Not done:** I wrote no semgrep or Go analyzer. The class is cross-file (play, its imports and
  their vars, Dockerfile COPY sources, git's tracked list), and no semgrep rule can express that.
  The existing walker test was the natural home.

## Fix

**`files/var/local/claude-yolo/Dockerfile`:** a new `ccy-builder` stage, modelled on
`phpantom-builder`.

- It uses `FROM golang:1.26.10`, which is the `toolchain` line of `src-go/go.mod`. The build
  fails if the two differ, and `GOTOOLCHAIN=local` means it never downloads another toolchain.
- It installs `python3` and `git`.
- `ARG CCY_SOURCE_COMMIT` must be a full 40-hex id. The build fails with a separate message for
  a missing value and for a malformed one.
- It runs `git fetch --depth 1` of that commit from the public GitHub repository. If the fetch
  fails, the build fails with "is it pushed?". It then checks out the commit and verifies HEAD.
- It builds `ccy-start-claude`, `pingbus-status-line` and `teamchat` with `CGO_ENABLED=0`,
  `-mod=vendor`, `-trimpath` and `-buildvcs=false`, for the build's own architecture.
  `teamchat` gets `-X main.SourceDigest=$(git rev-parse HEAD:src-go)`, the same digest the play
  used.
- It builds the pingbus zipapp with `python3 -B -s -m helpers.pingbus.bundle`, as the play did.

The runtime stage now does:

- `COPY --from=ccy-builder /out/ccy-start-claude /usr/local/bin/ccy-start-claude`
- `COPY --from=ccy-builder /out/optional/ /opt/claude-yolo/optional/`, after `COPY optional/`.

The existing `chmod`s and the `teamchat version` check are unchanged.

**Play (`tasks/claude-yolo-image.yml`, `tasks/teamchat-ccy-kit.yml`):**

- Removed the bundle task, the two `build-go-command` imports into the context and the teamchat
  build block.
- Added a `state: absent` task for the stale copies on hosts that already have them.
- Reads `git rev-parse --verify HEAD^{commit}`, asserts 40-hex, and writes it to
  `/opt/claude-yolo/source-commit`. No `COPY` takes that file.
- The build argv now passes `--build-arg CCY_SOURCE_COMMIT=...` next to `DOCKERFILE_HASH`.

**Launcher (`lib/common.bash` `build_container_with_hash`):** the launcher builds the image
itself on a missing image, a version bump, `--rebuild` and `--no-cache`. Those builds would
otherwise fail with no commit. They now pass the context's `source-commit`. If the file is
missing, the launcher stops with an error that names the file and the play. I refactored the
function into one argv array so `common.bash` stays under its bash-limits ratchet. Its allowlist
record went down (697 to 686 code lines).

**Rebuild trigger:** the commit is not part of the Dockerfile hash, and does not need to be:

- The play always builds. A changed ARG invalidates the builder stage, and `COPY --from` brings
  in the new files.
- The launcher's check compares the Dockerfile hash label. Folding the commit into that label
  would only add launcher rebuilds whenever the commit changes without the Dockerfile.

## Tests (TDD)

- **`scripts/test-ccy-image-build-lock.bash`:** the fake podman records `CCY_SOURCE_COMMIT`.
  New cases: both launcher builds get the context's commit, a missing `source-commit` builds
  nothing and names the file, and the play's argv carries the commit. All failed before the
  change and pass now (132/132).
- **`src-go/internal/ccylaunch/difftest`:** the sandbox stages a `source-commit`. Goldens
  `launch-image-missing` and `launch-image-older` were re-recorded from the bash with `-update`.
  The only diff is the added `--build-arg`. `go test ./internal/ccylaunch/...` passes.
- **`scripts/test-ccy-agent-bus.bash`:** its "the play builds the zipapp and status line" checks
  now check the builder stage (56/56).
- **`scripts/test-ccy-container-version-hook.bash`:** 126/126.
- **Builder smoke test:** I ran the stage's build step outside a container on a `git archive`
  export of `origin/F44` (c5c2b96a3) with the local go1.26.10. All four files built,
  `teamchat version` printed the tree id, and `pingbus --help` ran. I could not test the fetch
  by sha here: the worktree guard refuses git on other directories. No podman build was run
  (not allowed here).

## Versions and docs

- CCY 3.116.0, container 2.83.0 (launcher `CCY_VERSION` and `REQUIRED_CONTAINER_VERSION`,
  Dockerfile LABEL), with an entry in `docs/ccy-changelog.md`.
- Docs updated: `CLAUDE/ContainerRules.md` (new bullet: the context is repository files only,
  and HEAD must be pushed), `docs/ccy.md`, `CLAUDE/QA.md` (two suite rows), `src-go/CLAUDE.md`,
  the pre-commit hook comment, and the kit test comment.
- `CLAUDE/Plan/meta-deploy.list` describes the change under `changed-plays`, which picks up
  `play-claude-yolo.yml` and `play-claude-code-consumer.yml`.
- The pre-commit gate still treats `helpers/pingbus` and `helpers/agent_bus` as image content.
  The drift test now finds them through the Dockerfile's bundle call.

## For the owner or coordinator

- **Behaviour change:** the play's image build now needs the checkout's HEAD pushed to GitHub.
  A local-only commit fails the build at the fetch, with a message saying so. The brief asked
  for this, and the repository's "always push" rule normally covers it. The owner should know
  that deploying an unpushed commit no longer works.
- **Cost:** a new commit rebuilds the builder stage from scratch: an apt layer (cached) plus
  three cold Go builds, roughly a minute or two. I did not add a build cache mount, to keep the
  Dockerfile portable.
- **Faults found while dogfooding the hooks**, filed as issue #236:
  1. `lint_on_edit` denies an edit to `scripts/test-ccy-image-build-lock.bash` over a
     pre-existing shellcheck *info* finding (SC2016 at its `flock ... bash -c '...$1...'`
     line). `qa-bash` treats info as advisory.
  2. `secret_file_guard` denied an Edit whose awk regex contained `.*CGO_ENABLED`, matching it
     to the `.vault-pass*` glob. This is a false positive.
  3. The worktree isolation guard refused `./scripts/qa-changed.bash --files ...` because a
     path argument contained `git-hooks`. Plain `qa-changed.bash` was used instead.
