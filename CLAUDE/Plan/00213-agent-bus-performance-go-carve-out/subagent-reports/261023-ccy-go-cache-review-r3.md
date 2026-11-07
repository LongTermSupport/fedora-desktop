# Review round 3: ccy Go caches on the host (F44...HEAD, 4e9d452c4 + 142682040)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Read-only adversarial review. Not plan work. Scope: `src-go/internal/teams/gocache/`, the
launcher hunk in `files/var/local/claude-yolo/claude-yolo`, `teams/app.go` wiring, usage text,
docs, changelog, allowlist, difftest scenario and goldens (spot-checked).

## What was run

- `go test -race -count=1 -cover ./internal/teams/gocache/ ./internal/teams/`: pass, 96.6 % / 100 %.
- `golangci-lint run` on both packages: 0 issues.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK; launcher record lowered 2782 -> 2777.
- Probes in `untracked/scratch/gocache-r3/` (a copy of `gocache.go` with the host adapter
  stubbed, because `ccy-teams` refuses to run inside a CCY container), plus a live go-build swap:
  - `mod/probe_test.go`: sparse stray file, planted lock link, held lock, replaced lock.
  - `swap.bash`: a running `go build -a ./cmd/...` whose GOCACHE is renamed aside and remade
    empty 4 s in.

## Verified claims

- **os.Root (Go 1.26.10):** `Mkdir`, `Lstat`, `Rename`, `Chmod`, `Remove`, `RemoveAll` and `FS`
  are all present. Links are followed only inside the root; absolute links and links that escape
  are refused. The documented caveat is that Root.Chmod on Unix can race and act on the link, not
  its target. The command only ever chmods to 0700 inside the root, so that race is harmless.
  The symlink tests (planted leftover, a link inside a pruned cache, a cache that is itself a
  link) cover the escapes that matter.
- **go-qa-ci (`v0.0.0-20261019185407-ccf4c1751f05`):** `internal/toolstore/store.go:25`
  `DirEnv = "GOQA_TOOLS_DIR"`. It must be absolute (the value passed is). Installs are built in
  `.install-*` scratch and placed with `os.Rename`. If the rename fails, `Resolve` is checked, so
  a concurrent install of the same version is tolerated. The "installs by rename" claim holds.
- **A running go build survives the prune's swap:** `swap.bash` gave `go build rc=0`. Cache
  writes after the swap are simply lost. "Loses only cache hits" holds for the build cache.
- **Launcher wiring:**
  - `CONTAINER_ENGINE` is exported (`lib/common.bash:51`, and `:697` for `--engine`).
  - `CCY_MOUNT_RELABEL` is `z` or empty (`lib/common.bash:95-97`), and agent forwarding clears
    it (`:1408`), so the Go side's `z`-only parse matches.
  - There is one `run` site (`:3882`), and a restart `exec`s the launcher again (`:3835`), so the
    restart and restore paths re-run go-cache and cannot reuse stale args.
  - `$(...) || exit 1` is correct under `set -e`.
  - ccy-teams is built by the same play as the launcher (`play-claude-yolo.yml:980`).
- **Docker** gets a named volume, the relabel is ignored for it, and no host directory is made.
  The test pins this.
- **Version:** CCY_VERSION is 3.100.20 and the changelog entry matches. No install-specific
  names were found in the diff.
- **Goldens:** spot-checked plain, network-missing, engine-flag-docker and selinux-enforcing
  (`go-cache difftest-project z`). The call is recorded once per launching scenario. The mounts
  and `-e` appear after the workspace and config binds, before the secrets.

## Blocking findings

### B1. The project lock lives inside the directory the untrusted container writes

`flock(filepath.Join(root, ".lock"))` (gocache.go:193, 363). `root` is the bind source, so the
container sees and owns `/var/cache/ccy-go/.lock`. The probe confirmed all three of these:

- **Held by the container:** the next launch of the project blocks forever on `LOCK_EX`, with
  nothing on stderr (`TestProbeLockHeld`: "still blocked after 2 s", stderr empty). It continues
  only when the holder lets go. This is a silent hang of every launch, which breaks fail-fast.
- **Unlinked by the container while a launch holds it:** a second launch creates a new inode and
  locks it at once (`TestProbeLockReplaced`: second flock `err=<nil>`). Two launches then prune
  concurrently, and one can `removeLeftovers` the other's in-flight `.prune-*`. The mutual
  exclusion that G4 promises is gone.
- **Replaced by a link or a directory:** every launch exits 1 ("too many levels of symbolic
  links"). That is fail-fast, but it is a lock the container decides.

A lock whose holder and identity the untrusted side controls is not a lock. Fix:

- Lock a host-only file, for example `~/.cache/ccy/<project>/go.lock`. The parent
  `~/.cache/ccy/<project>` is not bound. Do not lock the root directory's fd either: the
  container can flock its own mountpoint.
- Optionally use `LOCK_NB` with a bounded wait and a message naming the holder. Then a stuck
  host launch also fails loudly instead of hanging.

Add a test that holds the real lock from another fd and expects a bounded failure, or a test that
the lock path is outside `root`.

### B2. The bound is not enforced, and the launch carries on silently when it cannot be met

`totalSize` counts every regular file under the root, but only the four caches are ever cleared.
Two problems follow:

- **Anything else is never removed.** Examples are a file the container drops at
  `/var/cache/ccy-go/x`, or a tool that writes beside the caches. If that alone exceeds the
  limit, stage 1 and stage 2 clear all four caches on every launch, and the launch exits 0 still
  over the bound.
- **The size is apparent size (`info.Size()`), not size on disk**, although the docs, usage text
  and changelog all say "on disk". A sparse 9 GiB file that costs 0 bytes is enough.

The probe confirmed this (`TestProbeSparseStray`). Two consecutive launches each printed
"hold 9.0 GiB ... clearing go-build, golangci-lint" and then "... clearing mod, go-qa-tools".
Both exited 0, the module cache was wiped each time, and `du` showed 4 KiB actually used. The
result is permanent cache thrash, with full module re-downloads at every launch, while the bound
the requirement asks for is not met. Fix:

- Measure only the four cache directories, or treat unknown top-level entries explicitly by
  clearing them or failing and naming them.
- Use allocated blocks (`Stat_t.Blocks*512`) so "on disk" is true, or change the docs to say
  apparent size.
- If the total is still over after stage 2, exit 1 with what is left, instead of proceeding.
- Add a test for each of these.

## Non-blocking findings

1. **The bound is per project name, not global.**
   - Every worktree launched on its own gets its own PROJECT_NAME and so its own 8 GiB, and
     nothing ever removes the cache of a project or worktree that is gone. Ten worktrees can
     hold 80 GiB.
   - Two checkouts whose names collapse to the same name under generic parents (`get_project_name`)
     share one cache, and so one trust domain.
   - At least say this in `docs/ccy.md`, or add a reaper or a global cap.
2. **SELinux `:z` cost is unmeasured.** `:z` is a recursive relabel. It is not verified here
   whether podman skips the relabel when the top-level label already matches (no podman source
   is available in the container). If podman relabels each time, every launch walks the
   60-80k-file cache with setxattr, which moves some of the cost from exit to start. The
   `/workspace` bind already pays the same, so this is consistent. Measure a launch on an
   Enforcing host with a warm cache before calling the goal met.
3. **A bounded walk cost at every launch.** `totalSize` lstat-walks the whole cache (tens of
   thousands of entries, the read-only module tree included) under the lock, even when far under
   the bound. That is probably about 100 ms on a warm dentry cache. The container can make it
   arbitrarily long with millions of empty files, but only for its own project.
4. **Subuid-owned entries make every launch fail.** Root in the container can `chown` (for
   example `tar` extracting with ownership, or a test that chowns). That yields host-subuid-owned
   directories, which the host user cannot chmod or empty, so `prune` or the walk fail with
   EACCES on every launch. This is fail-fast and correct, but the error gives no recovery. Name
   `podman unshare rm -rf <path>` in the message or the docs.
5. **Pruning `go-qa-tools` under a running seat.** go-qa has put the pinned tools' directories on
   PATH. After a stage-2 prune, those tools' exec fails partway through that seat's QA run. The
   docs warn only about in-flight module downloads, so add the tools.
6. **Test quality.**
   - The fault seam is real: it wraps the production `*os.Root`, and each table row was checked
     to reach the operation it names. Two notes:
     - `faultyFS` embeds only `fs.FS`, which hides `ReadDirFS`. Under injection,
       `fs.ReadDir`/`WalkDir` take the `Open` fallback path, not the `ReadDir` path production
       uses. The un-wrapped tests do cover production, so this is minor.
     - `faulty.Mkdir` performs the real mkdir and also returns the error.
   - Not covered:
     - the lock outside the seam (B1);
     - residual over-bound (B2);
     - a real concurrent pair of launches with the real flock;
     - in difftest, any scenario where `ccy-teams internal go-cache` fails, so the launcher's
       `|| exit 1` is unpinned.
   - `TestAFileRemovedDuringTheWalkCountsAsNothing` unit-tests `fileSize` with a fake DirEntry,
     not a real race. That is acceptable.
7. **Launcher nit.** In `mapfile -t _go_cache_args <<< ... && DOCKER_MOUNTS+=`, a failing mapfile
   would skip the append silently, because the left side of `&&` is exempt from `set -e`. It
   cannot realistically fail. Two plain statements would hold the rule literally at no line
   cost, since the bash-limits count is per statement and the record has slack. On a successful
   empty output, mapfile would produce one empty argument, but go-cache never prints nothing on
   success.
8. **Docs.**
   - "8 GiB on disk" is wrong (B2).
   - In usage.txt, "print the podman run arguments" is printed for docker too (volume form).
   - Otherwise the docs, changelog and package doc match the code.

## Verdict

CHANGES: B1 (the lock is in the container-writable bind: hang, loss of exclusion), B2 (bound not
enforced or fail-fast; apparent size counted, unknown entries never cleared).
