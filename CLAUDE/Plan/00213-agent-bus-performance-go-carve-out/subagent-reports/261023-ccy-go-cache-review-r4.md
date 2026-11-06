# Review round 4: ccy Go caches on the host (F44...HEAD at 351bf8598)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Read-only adversarial review. Not plan work. Scope: `git diff F44...HEAD` (gocache package and
tests, launcher hunk, teams wiring and usage.txt, docs/ccy.md, docs/ccy-changelog.md,
bash-limits allowlist, difftest scenario and goldens).

## What was run

- `go test -race -count=1 ./internal/teams/... ./internal/ccylaunch/...`: all pass.
  gocache coverage 96.0 %, teams 100 %.
- `golangci-lint run ./internal/teams/gocache/... ./internal/teams/`: 0 issues.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK. The launcher record shrinks (2775 -> 2770 code
  lines, 2707 -> 2706 statements).
- Probe (`untracked/scratch/gocache-r4/`, a copy of `gocache.go` with stub host/term packages):
  - 40 rounds of two concurrent launches over the bound (real flock, 4 MiB limit, 8 MiB
    prefilled), while a goroutine churns files in go-build, mod and golangci-lint. Run three
    times under -race: all launches exit 0.
  - With a 512 KiB limit and the same churn, launches fail with "still holds ... not a cache
    ccy keeps". The churn refills the caches faster than the limit between the prune and the
    final measure. See N3.
  - An unreadable (mode 000) directory planted in go-build: exit 0 here. That result means
    nothing, because the probe runs as uid 0 with CAP_DAC_OVERRIDE. Not verified (N6).

## Earlier blocking findings: status

- r1 B1 (a vanished entry fails the launch): fixed. `skipGone` handles it in the walk, the
  chmod and the stat, and the host-only lock means no other launch's `.prune-*` is seen. The
  concurrency probe confirms this.
- r1 B2 (docker leaves root-owned files): "fixed" with a named volume. The premise is false,
  see B1 below.
- r1 B3 (meta-deploy): the `changed-plays` entry on F44 picks up play-claude-yolo.yml. Its
  description is stale (N1).
- r2 B1 (a symlinked leftover gets the host to chmod outside the cache): fixed. Every
  operation goes through one `os.Root`, a non-directory leftover is only unlinked, and tests
  cover a planted leftover, a link inside a cache and a cache that is a link.
- r3 B1 (the lock is inside the bind): fixed. `~/.cache/ccy/<project>/go.lock` sits beside the
  bind and is opened with O_NOFOLLOW|O_CLOEXEC. The parent directory is not bound. A test pins
  the path.
- r3 B2 (bound not enforced; apparent size): fixed. It counts `Stat_t.Blocks*512`, still over
  after stage 2 is exit 1, and there are tests for a sparse file and a stray file.

## Verified

- Launcher: under `set -e`, `$(...) || exit 1` and then `mapfile` with `;` fail fast.
  `PROJECT_NAME` comes from `get_project_name`, which maps every name into `[a-z0-9_-]`, the
  same regex as claim-container-name. The call comes after `engine_assert_rootless`
  (claude-yolo:1815). The difftest goldens record the call once per launching scenario, and
  the selinux scenario passes `z`.
- The project name regex and the HOME check (`:`, `,`, newline) keep the `-v` spec and the
  one-per-line output unambiguous.
- Hostile container through the bind:
  - It cannot reach the root's own entry or the lock.
  - Links are never followed out of the root.
  - Hard links from /workspace are cross-mount (EXDEV).
  - FIFOs are only lstat'ed or unlinked.
  - What it can do is deny its own project's launches: a file or link in place of a cache,
    a chown to a subuid, an unreadable directory, or filling past the bound with non-cache
    files. All of these are exit 1 with a message, which is acceptable for a project's own
    container.
- CCY_VERSION is 3.100.22, above F44's 3.100.18. The changelog entry is newest first (3.100.22,
  then the "Host cc" note, then 3.100.18). The image is unchanged (container 2.79.9).
- No install-specific names in the diff. The difftest uses `difftest-project` and
  `/difftest-go-cache`.
- Go rules: no discarded errors, no nolint, exit codes are `term.ExitCode`, the payload goes to
  stdout and diagnostics to stderr.
- docs/ccy.md, usage.txt and the package doc match the podman code path: stage order, on-disk
  measure, fail when unmet, lock path, rename aside, `podman unshare` recovery, and the
  Dockerfile ENV/GOPATH note.

## Blocking findings

### B1. The docker branch rests on a false premise and leaves docker caches unbounded and persistent

The package doc (G2), the test comment in `TestUnderDockerANamedVolumeHoldsTheCaches`,
docs/ccy.md and the changelog all justify the docker volume the same way: "docker's container
root is the host's root, so a bind under HOME would fill with root-owned files" (changelog:
"Under rootful docker a bind would fill `~/.cache` with root-owned files").

ccy never reaches go-cache under rootful docker:

- claude-yolo:1815 runs `engine_assert_rootless` and exits before the go-cache call (:2545).
- For docker, `engine_rootless_verdict` (lib/common-pure.bash, docker case) accepts only a
  daemon reporting `name=rootless`.

So under every docker that reaches this code, container uid 0 is the host user, exactly as
under podman. The bind would be owned by the user and could be pruned. Rootless docker also
supports `:z`.

What the change does instead:

- It gives docker a named volume, kept in the user's docker data directory, that ccy never
  bounds and never removes.
- Before this change, docker caches went with the writable layer at exit. Now they grow
  without limit across sessions. That contradicts the goal ("bounded in size") on a supported
  engine.
- The four documents state a security rationale that the launcher's own rootless gate makes
  false.

Fix: drop the docker special case and use the same bind and bound for both engines (the
relabel already comes from `CCY_MOUNT_RELABEL`). Remove the rationale from G2, the test,
docs/ccy.md, the changelog entry and usage.txt ("unbounded"). If a volume is kept for some
other reason, state the real reason and bound it.

## Non-blocking findings

- N1. The `changed-plays` description in `CLAUDE/Plan/meta-deploy.list` still describes only
  CCY 3.100.18. The run will deploy 3.100.22 (the Go caches move to the host and the bound
  applies at launch), and the owner's description does not say so. Update it in the merge.
- N2. The bound is applied only at launch. A running session can grow the bind without limit
  until the next launch, on the host's home filesystem. The writable layer had the same
  exposure, but it ended at exit, while this persists. The docs say "each launch bounds
  them", which is accurate, but a sentence would help.
- N3. "Still over after every cache was cleared" is diagnosed as "not a cache ccy keeps".
  When a running seat refills the caches between the prune and the final measure (seen in the
  probe with a tiny limit), that is wrong. At 8 GiB it is practically unreachable.
- N4. The flock waits without bound and without a message while another host launch is
  pruning (which can take minutes for GBs). Consider `LOCK_NB` first, and say "waiting for
  another launch of <project> to finish clearing its Go caches".
- N5. Tests:
  - Nothing runs two concurrent `prepare` calls over the real flock. The probe here passes,
    so this is a coverage gap, not a defect.
  - `faultyFS` embeds only `fs.FS`, hiding `ReadDirFS`, so the injected-fault rows walk the
    `Open` fallback, not the production `ReadDir` path.
  - The vanished-file test is a unit test of `blocks` with a fake DirEntry.
  - The difftest has no scenario where go-cache fails, so the launcher's `|| exit 1` is
    unpinned.
- N6. Not verified here (running as root): a mode-000 directory the container plants should
  fail every launch on the host with EACCES. The docs give recovery only for the chown case.
- N7. The cost of the per-launch walk and of the `:z` recursive relabel over a warm cache
  (60-80k files) has not been measured on an Enforcing host.

## Verdict

CHANGES (B1: the docker named volume rests on a rootful premise that the launcher's rootless
gate rules out; docker caches become unbounded and persistent, and four documents state the
false rationale).
