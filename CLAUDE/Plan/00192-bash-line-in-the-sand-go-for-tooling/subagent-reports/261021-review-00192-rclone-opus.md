# Review: Plan 00192 rclone-cache-status Go port (9d120b8f)

Branch `agent-a059cadc8596a3efb-bec758b3` against F44. Read-only review, standing in for Fable.

VERDICT: FIX-FIRST (one blocking finding, small fix)

## Checked and sound

- Parity with the bash at F44 (`files/home/.local/bin/rclone-cache-status`): every report line,
  the order, the name-or-directory reading of TARGET, the default cache dir, the cache mirror
  path, the 80/95% colours, the RC reason texts and the numfmt byte format all match. Every
  difference I found is in B1-B10, except the caller impact below.
- Secret: the password only goes into the child's environment (`rclonerc.Client.Call`, Env is
  added to `os.Environ()`, never argv). The credential errors name the file path, never the value.
  rclone's own output on stderr does not echo the password. `TestSummary` asserts that "secret"
  never shows up in any recorded command line.
- Stderr hygiene: the report goes to stdout, every diagnostic goes to `rclone-cache-status: ...` on stderr,
  and a failed stdout write exits 1.
- Fail fast: a findmnt failure, an unreadable /proc or an incomplete walk all exit 1. RC
  failures stay report lines with exit 0 (D4, justified).
- The play imports `play-golang.yml` with tags `rclone` and `rclone-cache-status`, then runs the
  shared `build-go-command.yml` (become: false, reproducible build, a checksummed `copy`, so it is
  idempotent). `play-rclone.yml` has no `always`-tagged task, so `--tags rclone-cache-status`
  never reaches the unit or restart-handler tasks. The mounts stay up.
- `go-binary-suites.tsv` has its row. The allowlist row is removed. The `rclone-rc-auth.bash` edit
  only touches comments, so its allowlist counts are unchanged.
- The `meta-deploy.list` entry is the `<plan folder>/<script>.bash` kind and comes with a description.
- The deploy and acceptance scripts follow the R1 bootstrap, `plan_init`/`plan_mode`/`plan_require_host`/
  `plan_prime_sudo`/`plan_start_log` and exit 64 on a usage error. The acceptance script counts a
  check it cannot run as NOT ESTABLISHED, not as a pass.
- `go test -race` passes on rclonecache, rclonerc and cmd. `golangci-lint` reports 0 issues on the
  three packages.
- The tests fake only the seams (execx.Fake for findmnt/rclone, MapFS for /proc). The walk runs on
  real temp trees. There are no mocks of internals.
- No install-specific names in the diff.

## Blocking

1. **Caller exit-code change is not listed against `rclone-tail`.** `rclone-tail` (`set -euo pipefail`)
   calls `cache_status`, which runs `rclone-cache-status` with no args when no filter is given:

   - unguarded at its start (line 324)
   - inside its `on_exit` trap, before `exit 0`

   With no args, a working directory inside a mount gets the directory walk. Under E19/B7, a walk
   with any unreadable entry now exits 1. The bash always exited 0 there. So `rclone-tail`
   started from inside a mount now aborts before it monitors anything. On Ctrl-C it ends with
   status 1 instead of 0. B7 justifies the exit 1 for the tool on its own, but SPEC D1 and H3 say
   the callers "are unchanged" and still work, and nothing in B7, D4 or the journal deals with this.
   Fix, either:

   - (a) make `rclone-tail`'s `cache_status` pass `--no-dir` (its snapshot is a mount summary), or
   - (b) record the caller impact in B7/D4 and justify it.

   `rclone-cache-warm` passes a mount name, so it never walks a directory and is unaffected.

## Non-blocking

2. Plan 00131 Task 2.3 still names `rclone-cache-status` line numbers (126, 138, 163) in a file
   that is now deleted. Strike that part or point the task at its new state (plan drift).
3. `subject()` ignores an `EvalSymlinks` error on the working directory and uses the raw path.
   A comment explains it, and the effect is harmless (at worst the summary covers every mount).
4. A missing credential prints the full multi-line advice once per mount on stderr. That is
   repetitive with several mounts.
5. A bare `-` is taken as a TARGET, where the bash treated it as an unknown option. Trivial and
   not listed.
