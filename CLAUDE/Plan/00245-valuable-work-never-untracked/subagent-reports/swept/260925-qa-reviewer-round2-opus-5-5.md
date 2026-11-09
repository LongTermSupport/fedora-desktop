# QA Review round 2: CCY 3.69.1 (25657b76) against the round-1 findings

**Verdict**: PASS WITH NITS

All five round-1 findings (two blocking, three should-fix) are closed, and I verified each one.
The relabel gate is green in CI. F44 CI is still red, but because of another gate
(`self-update-cycle`) that was already failing before 3.68.0 (see the last section).

## Round-1 findings: closed

1. **The test now runs as any user.**
   - Reproduced from a world-readable copy with `setpriv --reuid=1000`: 57 passed, 0 failed.
     As root: 57 passed, 0 failed.
   - CI run 36136132926 shows `✓ ccy-relabel-preflight: passed: 57`.
   - Nothing is chowned any more. Foreign ownership comes from the stub map (`MAP_FOREIGN`
     leaves `$ME` out), and `test-ccy-relabel-preflight.bash:212` asserts that every fixture is
     owned by the user running the test.
   - Test run against 3.68.0's `common.bash` (52760692): 11 cases fail as uid 1000, 8 as root.
     So the new cases catch the old defects.
2. **Unreadable directories are pruned.** `lib/common.bash:122-126`
   - The expression groups correctly: `( -type d (!readable|!executable) -prune ( FOREIGN -print -o -true ) ) -o FOREIGN -print`.
     A pruned directory is still printed if it is foreign, and it never falls through to the
     second branch.
   - As uid 1000, a 0700 directory owned inside the map passes with no `Permission denied`.
     The same directory owned outside the map is listed and blocks (test :328-345).
3. **The walk's exit code is checked before and after the fix.** `lib/common.bash:166-171`,
   `:251-256`
   - Covered by the "walk fails" case (:321) and the "walk after fix fails" case (:283), which
     also asserts that "now yours" is not printed.
4. **The uid map is parsed from stdout only.** `lib/common.bash:154`
   - The stub-warning case (:315) passes. podman's stderr now reaches the terminal, and the
     error text says so.
5. **The docker comment** (`lib/common.bash:150-151`) now states the real reason: there is no
   `podman unshare` to read a map from. The skip is tested (:229-234).
6. **Forwarded agent.** `claude-yolo:1052-1056` clears `CCY_MOUNT_RELABEL` when
   `SSH_AGENT_FORWARDED=1`.
   - This comes after `build_ssh_mounts_and_validate` (:1050) and before the preflight and
     `DOCKER_MOUNTS`. So the workspace carries no `:z`, and the question of what podman does
     under `label=disable` no longer arises.
   - I checked the other consumers. `ssh-handling.bash:817` reads `CCY_SELINUX_MODE`, not the
     relabel variable, so key staging is unchanged. The launcher ordering is asserted
     structurally (test :365-375).

## The round-1 nits

- **Count:** fixed. The message now says "cannot relabel this project" plus "N entries belong
  to someone else" (the foreign count).
- **stat per entry:** fixed. The loop breaks at the first blocking entry.
- **Give-up message:** fixed. It has its own message (`common.bash` case `*`), tested at :257.
- **Colour guard:** kept as the existing ccy style. Reasonable: `print_error` and every ccy
  banner are also unguarded.
- **Journal sentence fragment:** not fixed. Acceptable, because the journal is append-only.

## New nits

- **As root, the pruning cases pass without testing pruning.** For root, a mode-000
  directory is `-readable`, so the prune branch is never taken.
  - Probe: `find -type d ! -readable` listed nothing as root.
  - Against 3.68.0's code as root, the "closed" cases (:335-344) pass.
  - So `qa-all.bash` in the CCY container (uid 0) proves nothing about pruning; only CI and the
    host (non-root) do.
  - Consider printing which mode the test ran in, or asserting `[ ! -r "$p/db-data" ]` and
    reporting the prune cases as not exercised when run as root.
- **The pruning blind spot is documented only in the code** (`common.bash:112-117`).
  - A foreign entry inside a subordinate-owned 0700 directory is not seen, and podman then
    fails with the raw `lsetxattr` error, as it did before 3.68.0. This is not a regression.
  - `docs/ccy.md:414-417` says such a directory "is passed over" and does not mention this
    limit. One sentence would be enough.

## Merge interaction (bf553ed1)

- `git diff 52760692 bf553ed1 -- files/var/local/claude-yolo/`: only Dockerfile,
  `agent-browser-session-guard` and 4 lines of the launcher (the version) changed. `common.bash`
  and the mount block are untouched.
- The changelog order (3.69.1 above 3.69.0) and the patch bump are correct.

## Mechanical gates

- **qa-all.bash (container, root):** rc=0, `✓ ccy-relabel-preflight: passed: 57`.
- **Non-root run of the test:** 57/57.
- **CI run 36136132926 (25657b76):** failed on `self-update-cycle` only. The relabel gate
  passed. The same gate also failed on 9e9ffa47 and 69a66358, which predate 3.68.0, so it is
  not caused by this series. F44 still needs it fixed separately.
- **plan-qa --sweep:** 0 blocking, 12 advisories, all already present.
- **syntax-check:** no playbook changed, so not triggered.

Probe logs are in `<repo>/untracked/scratch/relabel-review/` (`r2-*.log`).
