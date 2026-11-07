# Review: 4e9d452c4 — ccy Go caches on the host (CCY 3.100.20), round 2

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Reviewer: adversarial, read-only. Scope: the whole commit; difftest goldens spot-checked
(launch-plain, launch-network-missing, engine-flag-docker, launch-selinux-enforcing).

## Checks run

- `go test ./internal/teams/... ./internal/ccylaunch/...`: all ok (gocache, teams, difftest).
- `golangci-lint run ./internal/teams/gocache/... ./internal/teams/`: 0 issues.
- `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK. The claude-yolo record goes 2782 -> 2777 code
  lines (shrinks, functions and statements unchanged).
- External claim verified: go-qa-ci at the pin in `src-go/qa/go.mod`
  (`v0.0.0-20261019185407-ccf4c1751f05`), `internal/toolstore/store.go`: `DirEnv = "GOQA_TOOLS_DIR"`, `DefaultDir` uses it when set (must be absolute), and installs go to a
  `.install-*` scratch directory and `os.Rename(stage, final)`, tolerating a concurrent
  installer. The G3 claim holds.
- `CONTAINER_ENGINE` is exported (lib/common.bash:51, claude-yolo:697), so the Go side sees
  docker. `CCY_MOUNT_RELABEL` is exported as `z` or empty (common.bash:95-99) and set by
  `ccy_selinux_mode` (claude-yolo:1374) before the go-cache call (2545); the selinux golden shows
  `:z` arriving. The container runs as root (no `USER`, no `--userns`), so under rootless podman
  its files in the bind are the host user's and the host prune can remove them.
- Restart and restore re-enter the launcher, so they pass through the same go-cache call;
  nothing else builds a `run` argv for the session container.
- `$(...)` strips the trailing newline and `<<<` adds one back, so `mapfile -t` gets exactly the
  printed lines, one argv element each. `|| exit 1` keeps it fail-fast under `set -e`.

## Blocking

### B1. A symlink the container plants as `.prune-*` makes the host chmod a tree outside the cache

`removeLeftovers` removes every `root/.prune-*` entry through `removeTree`, which calls
`writable(dir)` -> `os.OpenRoot(dir)`. `os.OpenRoot` follows a symlink at the name it is
given. The bind root is writable by the container (it runs as root, which is the host user
under rootless podman), so any session can create
`/var/cache/ccy-go/.prune-x -> /home/<user>` (the target is resolved on the host). On the next
launch the host-side `ccy-teams` walks that target and chmods **every directory under it to
0700**, then `os.RemoveAll` unlinks only the link. Under that home the walk would also hit
subuid-owned rootless storage, get EPERM and fail the launch, after the chmods it had already
made.

I reproduced it with the same calls (`untracked/scratch/gocache-symlink/prog/main.go`):
`OpenRoot err: <nil>`, `chmod . <nil>`, `chmod sub <nil>`, and the victim dirs end up
`drwx------`.

This crosses the container-to-host boundary, and the code comment on `writable` claims this
case is handled ("through an os.Root so a symlink swapped in during the walk cannot lead it
outside dir"). The os.Root guards the walk below the top, not the top itself. The same race
exists for the trash directory that `prune` makes: between `MkdirTemp` and `OpenRoot(trash)` a
running seat can rename `.prune-XXXX` away and put a symlink in its place, because the name is
visible by readdir.

Fix: open `root` once as an `os.Root` and do all the leftover and prune work through it
(`r.OpenRoot(name)`, `r.Rename`, `r.RemoveAll`), which refuses to escape. Or at minimum
`Lstat` each `.prune-*` and handle a non-directory by unlinking it, not by walking it, and open
the trash with `O_NOFOLLOW` semantics. Add a test: a `.prune-x` symlink to a directory outside
the root must leave that directory's modes untouched.

## Non-blocking

- N1. The container can block future launches by putting a symlink or file at `go-build`,
  `mod`, and the other cache paths (makeDir -> exit 1), or a regular file named `.prune-*`
  (OpenRoot fails -> exit 1). Both fail fast with a message, which is acceptable, but the
  `.prune-*` file message ("clear ...: not a directory") does not tell the user what to move.
- N2. Launch cost: every launch walks the whole cache to lstat each file, on top of podman's
  `:z` relabel of the bind (a fresh `--rm` container always relabels, and go-selinux's fast
  mode still walks the tree). At 50-100k files that is likely about a second or two. Worth
  measuring once on the host. When the bound trips, the 8 GiB delete that used to happen at
  exit now happens synchronously at launch, rarely.
- N3. When a launch prunes go-build or golangci-lint while another seat is running, the path
  is recreated empty, without the `00`..`ff` subdirectories that `cache.Open` made in the
  running process. That process's Puts then fail with ENOENT until it restarts. cmd/go mostly
  ignores Put errors, so "loses only cache hits" is plausibly true. golangci-lint's behaviour
  was not verified.
- N4. `Dockerfile.example-golang` sets `GOPATH=/workspace/go`. The new `-e GOMODCACHE` now
  silently moves such a project's module cache out of `/workspace/go/pkg/mod`. The docs sentence
  "replace any ENV of the same name" does not cover a value derived from GOPATH. Worth one line
  in the docs.
- N5. The gopls file cache (`/root/.cache/gopls`) and `/root/go/bin` stay in the writable
  layer. They are not in the stated requirement list, but they are the next contributors to the
  `--rm` delete if exits are still slow.
- N6. Docker: the named volume's SELinux behaviour ("Docker labels its volumes itself") was not
  verifiable here. Low risk, since docker is the compatibility path only.
- N7. Tests: concurrency is covered only by the lock-order fake, a real-flock exclusivity test
  and a unit test of `blocks`. `TestTwoLaunchesAgree` is sequential. No test exercises two
  concurrent `prepare` calls, nor symlinked leftovers (see B1).
- N8. Hard-linked files are counted once per link. Harmless.

## Accurate

CCY_VERSION is 3.100.20, with a changelog entry; it explains why 16-19 are skipped. usage.txt,
docs/ccy.md and the package doc agree with the code (bound order, flock, rename-aside, docker
volume, override via ccy.env). No install-specific names. The goldens show the call placed
before the network checks and the printed lines appended into DOCKER_MOUNTS. meta-deploy.list
is untouched, as agreed.

VERDICT: CHANGES (B1)
