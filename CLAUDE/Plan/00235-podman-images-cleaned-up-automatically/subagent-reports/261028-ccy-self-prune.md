# Plan 00235 Phase 3: ccy self-prune

ccy now removes the images its own build replaced, right after every successful rebuild or
in-place update (Tasks 3.1 and 3.2).

- **The command** (`src-go/internal/containerimages/replaced.go`, rules R1-R5 in its doc).
  `container-images [--engine E] before <repo>` prints `<name> <id>` for each tag.
  `replaced <repo> <ids-file>` removes each recorded id that no tag names any more and no
  container (running or stopped) uses. It says on stderr what it removed or kept and why, and
  logs removals to `container-images.log`.
  - A removal podman refuses is reported, exit 0.
  - An unreadable record line (anything but a 64-hex id), a list the engine can't give, or a
    log it can't open is exit 1, before any removal.
  - If a container vanishes between `ps` and `inspect`, it asks once more.
  - An id still under all its old names produces no output, so a project build doesn't list
    every other project's tag.
  - The engine comes from `--engine`, else `CCY_CONTAINER_ENGINE`, else podman.
  - All the listed test cases were written first and seen failing (red and green are
    journalled). Package coverage is 94.3%, golangci-lint is clean, and the race tests pass.
- **The lock.** `image-build-lock.bash` was already at exactly 90 statements, and doing this
  inline took it to 98. So it now sources a new sibling, `lib/image-build-prune.bash`. Its
  `ccy_image_build_pruned` runs `before`, then the build, then `replaced` only if the build
  succeeded. The lock calls it in place of `"$@"` and stays at 58 code lines and 90 statements.
  - If `before` fails, the image is not built and the launch fails with a message.
  - A failed build removes nothing.
- **Install.** The launcher finds `container-images` on PATH, the same way it finds
  `ccy-token-usable`.
  - `tasks/claude-yolo-image.yml` (used by both `play-claude-yolo.yml` and the consumer play)
    builds it into `~/.local/bin` before the image build and copies the new lib.
  - The play's own build gets `CCY_CONTAINER_IMAGES` (the full path) and `CONTAINER_ENGINE`.
  - CCY is bumped to 3.102.0, and `CCY_LIBS` includes the new lib.
- **Tests run against the real command.**
  - `scripts/test-ccy-image-build-lock.bash` builds the real binary; its fake podman answers
    the new questions and its ids are now 64 digits. New cases: a rebuild removes the old
    `:latest` and `:base` once each, an update removes the old `:latest`, a failed rebuild
    removes nothing, and no record means no build. All 99 pass.
  - The launcher's parity goldens were re-recorded with `-update`. Three scenarios each gain
    only the `before`/`replaced` calls around their build.
  - `test-planlib.bash` and Plan 00201's `test_m7_check.py` run the lock with
    `CCY_CONTAINER_IMAGES=true`.

**Gate:** `qa-changed` passes everything except ansible-syntax. That one fails only because
this worktree has no vault password file, so it is environmental and I did not work around it.

**Task 3.3 (devtools) is not done.** It would need `play-golang` imported into an optional play
plus its own install of the command, which isn't small. The daily timer already catches its
dangling images.

Commits: a3cfb92ae (merge), 117319afa.
