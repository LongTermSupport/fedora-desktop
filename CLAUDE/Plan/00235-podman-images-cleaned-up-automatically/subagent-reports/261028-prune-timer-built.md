# Plan 00235: the prune timer built (Phase 4), and ccy's self-prune (Phase 3)

## Phase 4: the daily catch-all

- `src-go/internal/engine/store.go`: the store questions on `engine.Engine` (so `podman` is
  still run only through `Engine.cmd`, the existing `dynamicNames` row): `StoreImages`,
  `StoreContainers`, `DanglingVolumes`, `RemoveImage` (unforced `image rm`),
  `RemoveExternalContainer` (`rm --force`, podman only), and for Phase 3 `RepositoryTags` and
  `ContainerImageIDs`.
- `src-go/internal/containerimages/prune.go`: `container-images prune [--yes]`, rules P1-P8 in
  the package doc. A dry run unless `--yes`; removals in passes until one removes nothing
  (bounded at 20); each removal logged to `~/.local/state/fedora-desktop/container-images.log`
  (tab-separated: time, kind, what, id, size, labels, why) only after it succeeded; a failed one
  named on stderr, not logged, exit 1; the log opened before any removal; dangling volumes
  listed, never removed; `podman system df` never asked.
- Task 3.4 (stale `claude-yolo:<project>` tags, Q1's 30 days) is in the same rules (P2).
  podman records no last use, so the age is the image's creation; `:base` and `:latest` are
  never touched; a tag of an image with other names only untags.
- Buildah working containers: podman lists another tool's container in the `storage` state
  (`ps --external`); one over a day old is removed only while no
  `$XDG_RUNTIME_DIR/ccy/image-builds/*.lock` is held (probed with a non-blocking shared flock).
- `play-podman.yml`: imports `play-golang.yml`; builds `container-images` into `~/.local/bin`
  via `tasks/build-go-command.yml`; installs `container-images-prune.service` (oneshot,
  `prune --yes`, nice/idle IO) and `.timer` (daily, `Persistent=true`); enables the timer;
  starts the service once when the service file did not exist before the play (a oneshot start
  waits, so a failing first run fails the play).
- `container_engine`: the timer prunes podman's store, which play-podman installs for every
  audience whatever `container_engine` says (ccc always runs in podman; Docker's store is
  DDEV's, a non-goal). The store lists are podman-only and refuse docker (`ErrPodmanOnly`).
- Docs: `docs/containerization.md` "The Daily Prune of the Podman Store" (what, when, never,
  the log, the dry run, the journal command); `CLAUDE/ContainerEngines.md` "The store is pruned
  daily" points at it.
- Plan scripts: `deploy.bash` (play-podman.yml), `acceptance.bash` + `check-prune.bash` (timer
  enabled with a next run; last run succeeded and the log exists; no unused dangling image over
  a day old, judged with podman directly; every container still has its image;
  `claude-yolo:base`/`:latest` exist; free space). The existing meta-deploy.list entry for this
  plan now runs triage (before), deploy, triage (after), acceptance, so free space is recorded
  before and after; its description was rewritten. No second entry was added: a plan-folder
  entry already runs all three scripts.
- Not done: Task 4.4, the orphaned volume's root removal. It deletes volume data as root
  inside the user's store and then needs `podman volume rm` as the user; that is not a clean
  addition to Task 4.2, so it is an unticked task. The prune does not depend on it.

### Podman output shapes the fakes assume (unverified against a live podman here)

- `podman images --format json`: a JSON list; keys `Id` (64 hex, no `sha256:`; a prefix is
  dropped if present), `Names` (absent or empty for a dangling image; local images named
  `localhost/claude-yolo:<tag>`), `Size` (bytes), `Labels`, `Containers` (podman's count),
  `Created` (Unix seconds).
- `podman ps --all --external --format json`: a JSON list; keys `Id`, `Names` (list),
  `ImageID` (64 hex), `State` (`storage` for a container another tool made), `Command` (list;
  `buildah` first for buildah's), `Created` (RFC 3339 in podman 4 and 5; Unix seconds also
  read).
- `podman volume ls --filter dangling=true --format '{{.Name}}'`: one name per line.
- `podman image rm <tag>` of an image with other names untags only; `podman image rm <id>`
  refuses an image a container uses or one with children (relied on as the second guard only).
- `podman rm --force <id>` removes a container podman did not create (podman-rm(1)).
- Phase 3's `images --no-trunc --format '{{.Repository}}:{{.Tag}} {{.ID}}' <repo>` (ID with
  `sha256:`), `ps --all --quiet --no-trunc` and `container inspect --format '{{.Image}}'`: the
  same templates in podman and docker.

## Gates

`golangci-lint run` and `golangci-lint fmt` over the changed packages: 0 issues;
`go test -race` green (coverage: containerimages 93.4%, engine 99.7%).
`./scripts/qa-changed.bash` before each commit. qa-all not run (not mine to run).
Nothing ran against a real podman or Ansible.
