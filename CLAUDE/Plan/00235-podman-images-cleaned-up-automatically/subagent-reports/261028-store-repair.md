# 261028 store repair and timer gate

The first `container-images prune --yes` on the host (2026-10-28) freed 299G. Then it left three
c/storage temp dirs holding files owned by uids from the user's old subuid range. The rootless
user cannot unlink those, so c/storage failed every layer-store load and every image operation
failed, while the running containers carried on. These are the repair and the guard.

- **Branches:** `fix/00235-store-repair` (a7e284f06, Task 4.6; 966c40827, Task 4.5), then
  `fix/00235-timer-gate` (f0ec211a7, Task 4.8). The coordinator added `-ignore_readdir_race` to
  the probe at merge, because running containers delete files in the store while it walks.
- **Task 4.5:** `tasks/podman-store-repair.yml` removes each `temp-dir-N` and its `lock-N` as
  root, before the play's first podman call and again after the first prune. It refuses while a
  podman, buildah or container-images process of the user is building or removing. It runs
  before the first podman call because a failing `podman ps` probe would otherwise let the
  migrate step stop every running container.
- **Task 4.6:** the prune stops at the first removal c/storage refuses with permission denied,
  names the cause and the repair once, and exits 1. Tests first, journalled.
- **Task 4.8:** a root `find -quit` for a store file owned outside 0, the user and the current
  subuid range. If one is found, the timer is stopped and disabled, the first prune is skipped,
  and the play fails at its end, with the store already repaired.
- **Assumed, unverified:** no vendored c/storage was reachable. `lock-N` beside each `temp-dir-N`
  is assumed to be its fcntl lock, removed with it.
- **Task 4.4 is unsafe as written:** Plan 00062 found 9 out-of-range volumes, one holding real
  database data. That waits on the owner (Q2b, Q6).
- **meta-deploy carrying on after a failed entry** is by design (`CLAUDE/PlanWorkflow.md`: a
  failing plan does not abort the batch; the script exits 1 at the end).
- **Checks:** `go test -race` passes, coverage 94.6%, golangci-lint and fmt clean. qa-changed
  passes; ansible-syntax could not run in the workers' worktrees (no vault password file), so
  the full QA run is its first parse.
