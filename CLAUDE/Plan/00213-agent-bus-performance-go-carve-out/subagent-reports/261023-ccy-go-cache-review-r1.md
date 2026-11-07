# Review r1: ccy Go caches on the host (staged diff, CCY 3.100.20)

Rescued 2026-11-06 from an agent worktree's untracked scratch (Plan 00250 Task 2.3); it was held by no ref.

Scope: `git diff --cached` in this worktree. I read gocache.go and its tests, the app wiring, usage.txt,
the claude-yolo hunk, the allowlist, docs/ccy.md, docs/ccy-changelog.md and scenarios.json, and
spot-checked three goldens (launch-plain, launch-network-missing, launch-selinux-enforcing).

## What I checked and found sound

- `go test ./internal/teams/... ./internal/ccylaunch/...`: all ok. `golangci-lint run` on gocache
  and teams: 0 issues. `scripts/qa-bash-limits.bash`: BASH-LIMITS-OK. The claude-yolo record drops
  from 2782 to 2777 code lines, so the ratchet shrinks and does not grow.
- GOQA_TOOLS_DIR is real. In go-qa-ci@v0.0.0-20261019185407-ccf4c1751f05,
  `internal/toolstore/store.go`: `DirEnv = "GOQA_TOOLS_DIR"`, and the path must be absolute. Tools
  are built in a `.install-*` scratch directory and placed with `os.Rename`. When the rename
  fails, the code checks whether another go-qa already installed the same version first. So the
  claim "go-qa installs by rename" is accurate, and sharing the tools between seats is safe.
- The CCY_VERSION bump is 3.100.20 and has a matching changelog entry. The image does not change.
- The bash argv handling is correct. `$(...)` strips the trailing newline and `<<<` adds one back,
  so `mapfile -t` yields exactly 10 elements. `|| exit 1` keeps set -e semantics. Passing
  `${CCY_MOUNT_RELABEL:+"$CCY_MOUNT_RELABEL"}` sends `z` (common.bash sets it to "z") or nothing.
  An SSH-agent-forwarded launch blanks it, which matches the unconfined container. `read -ra` on
  an empty here-string gives an empty array (read returns 0), so removing the `if` guards changes
  nothing except that the two debug lines are gone.
- The restart path goes back through the same launcher code, so the relaunch gets the bind again.
  The only other `run` calls are probes and image updates, and they need no caches.
- The deploy path is fine. `play-claude-yolo.yml` installs the launcher (line 572) and builds
  ccy-teams (line 980) in the same play, the same pattern claim-container-name used.
- A project mount cannot collide with the new bind: `/var` is already a reserved target in
  common-pure.bash.
- No install-specific names appear in the diff.
- The tests use a real temporary filesystem through teamtest.World. They cover the printed argv,
  modes, the symlink and file-in-place refusals, both prune stages (including a read-only module
  cache), leftover `.prune-*` cleanup and argument validation. They exercise the real code paths.

## Blocking

### B1. A cache being changed while the launch walks it fails the launch

`diskUsage`, `writable` (used by `removeLeftovers` and `prune`) and `filepath.WalkDir` return every
error. That includes `fs.ErrNotExist` for an entry that disappears between `ReadDir` and
`d.Info()` (documented on `DirEntry.Info`), or a directory that disappears before it is read.
The cache is shared and live while other seats run, and entries disappear all the time:

- go's own cache trim;
- `go mod download` temp files, which are renamed or removed;
- go-qa's `.install-*` scratch directories, removed after the rename;
- `go clean -cache` / `-modcache`;
- another launch of the same project that is pruning. Its `.prune-*` tree is being `RemoveAll`ed,
  and this launch's `removeLeftovers` walks that same tree as if it were an interrupted leftover.
  Its `diskUsage` also walks it, because only leftovers seen at the start are removed.

Any one of these turns a seat's launch into `exit 1` ("measure the Go caches ... no such file or
directory"). The likely trigger is starting a second seat while the first is running QA. This
breaks requirement G2, "safe when two seats share it", which the package doc and changelog both
claim.

Fix:

- In the `diskUsage` and `writable` callbacks, treat `errors.Is(err, fs.ErrNotExist)` as "nothing
  there": skip it, and return `fs.SkipDir` for a vanished directory. This is not error hiding: a
  file that is gone holds no bytes.
- Do not count or remove a `.prune-*` that another live launch owns. For example, take an flock
  on `<root>/.lock` for the whole of `prepare`, which also serialises two over-bound launches.
- Add a test that makes an entry vanish during the walk (a seam on the walk, or a fill that the
  test removes from a callback hook).

### B2. With Docker (rootful), root-owned cache files lock the project out

Under `--engine docker` / `CCY_CONTAINER_ENGINE=docker`, the container's root is the host's root.
Everything go writes under `go-build/xx/`, `mod/...` and `go-qa-tools/...` becomes root-owned
inside the user's `~/.cache/ccy/<project>/go`. Once that project goes over 8 GiB:

1. `prune` renames `go-build` aside. This still works: the directory itself is user-owned.
2. `writable` then calls `Chmod` on root-owned directories and gets EPERM.
3. `go-cache` exits 1, so every launch of that project fails, with Podman too, because the
   directory is shared.
4. Recovery needs sudo, outside IaC.

Even under the bound, the user cannot clear their own cache. The brief asked for Docker against
Podman to be judged explicitly, and `engine-flag-docker` is a recorded scenario, so Docker is a
supported path. Fix, any of:

- for docker, use a named volume instead of a home bind (`-v ccy-go-<project>:/var/cache/ccy-go`),
  bounded through the engine;
- or keep the caches in the writable layer under docker, as before, and say so;
- or bind a per-engine directory and prune it through a throwaway container.

Whichever is chosen needs a test for the docker variant (the engine could be passed as an
argument) and a line in docs/ccy.md.

### B3. meta-deploy.list is not updated

CLAUDE.md requires that whatever must run on the host goes into `CLAUDE/Plan/meta-deploy.list` in
the same commit. This change needs `play-claude-yolo.yml`, which installs the launcher and
rebuilds ccy-teams. The list holds only `play-speech-to-text.yml`. Add a described `changed-plays`
entry, or the play itself, in this commit.

## Non-blocking

- N1. A running seat can have its cache pulled away. When a launch prunes, a running seat's
  `GOCACHE` vanishes mid-build. go recreates it, and most cache Put failures are tolerated. Stage 2
  removes `GOMODCACHE` under a running `go build`/`go test`, which will fail that build. The docs'
  "no seat sees one half deleted" is true of a directory, not of a running seat. Either run stage 2
  only when no other container with `ccy-project=<project>` is running, or say this in docs/ccy.md.
- N2. The prune is synchronous at launch. An over-bound launch now waits about as long as the old
  exit did, and longer, since stage 1 can be GBs. That is acceptable, and the stderr line says it
  is clearing. docs/ccy.md could say the launch pauses while it does.
- N3. Every launch pays per-file costs. The measure lstats every file (about 60k at the measured
  sizes) on every launch. With `:z`, podman also relabels the whole tree recursively when it
  creates each container. I could not verify here whether podman skips the relabel when the label
  already matches. The workspace already pays the same kind of cost, but measure both on the host
  before calling the exit fix a net win for short sessions.
- N4. Image ENV is overridden silently. `-e` beats a project Dockerfile's `ENV`.
  `Dockerfile.example-golang` sets `GOPATH=/workspace/go`, so its module cache moves without
  notice. The docs mention only the ccy.env override; worth one sentence.
- N5. The `# Mounts:` comment above `DOCKER_MOUNTS` in claude-yolo (around line 2505) still lists
  only `/workspace` and `/tmp/claude-config-import`.
- N6. There is no global bound. The 8 GiB is per project, so the total grows with each project
  and checkout name, and nothing removes the directory of a project no longer used. Also,
  `XDG_CACHE_HOME` is ignored in favour of `$HOME/.cache`. Both are acceptable but worth a line in
  the docs.
- N7. Removing the two `debug` lines is a small loss of observability, done for line budget. It is
  acceptable and stated in the changelog.
- N8. The docs say golangci-lint "already trims entries unused for five days". I could not verify
  this for golangci-lint's cache fork offline. Drop it or cite a source.
- N9. The tests have no case for EPERM (the B2 shape), for concurrent mutation (B1), or for
  `removeLeftovers` meeting a `.prune-*` that is a file rather than a directory (currently exit 1;
  probably fine).

VERDICT: CHANGES (B1 concurrent mutation fails the launch; B2 docker root-owned files lock the
project; B3 meta-deploy.list not updated)
