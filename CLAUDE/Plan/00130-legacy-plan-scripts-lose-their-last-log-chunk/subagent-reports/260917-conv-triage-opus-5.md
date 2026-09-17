# Plan 00130 Task 2.1 — converting three legacy triage scripts onto `plan_start_log`

Scripts converted: `00062`, `00066`, `00080` triage.bash. All three are off
`exec > >(tee "$LOG") 2>&1` and onto the shared library's `plan_start_log auto`,
with the canonical R1 bootstrap in place of every repo-root resolution.

Nothing was committed, no `PLAN.md` was touched, and no playbook was run.

## `CLAUDE/Plan/00062-disk-reclaim-tui/triage.bash`

**Changed.** Replaced `REPO_ROOT="$(git rev-parse --show-toplevel)"` with the verbatim
R1 marker walk, both `# shellcheck` directive lines, the `source`, and
`plan_init "${BASH_SOURCE[0]}"`. Added `PLAN_USAGE`, `plan_mode gather` and
`plan_parse_common_flags "$@"`. Deleted the
`PLAN_DIR` / `REPORTS_DIR` / `mkdir -p` / `LOG=` / `exec > >(tee …)` block in favour of
`plan_start_log auto`.

**`$LOG`-family consumers found and removed.**

- `$REPO_ROOT` in the run banner → `$PLAN_REPO_ROOT`.
- The closing `echo " Report written to: $LOG"` **and** the two lines after it naming
  `untracked/reports/reclaim-podman-triage.log`.

That trio is exactly the trap this task exists around: under `set -u` the script would
have died on its own final lines on every run, with `shellcheck -x` clean.

**Header comment: updated, and it had to be.** Two separate blocks were wrong. One
claimed the report goes to `untracked/reports/`; the other explained at length why the
report lives in the plan's own `logs/` dir ("travels with the plan into `Completed/`",
"resolved from the script's own location, not the repo root, so that move does not break
it"). Both described a layout that no longer exists. They now describe the per-run
`untracked/plan-runs/` directory, its bind-mount visibility from the CCY container, and
that it is unscrubbed.

**Lint.** `bash -n` OK. `shellcheck -x` clean — no SC1091 at all, so the source
directive is being followed rather than merely tolerated.

**Run.** Ran to completion in the container, exit 0. `plan_start_log` opened
`untracked/plan-runs/00062-disk-reclaim-tui/triage/<stamp>/triage.log`. Every probe ran;
`podman` is absent here so those probes recorded `command not found` (rc=127) and the
storage dir is missing — for this script's `probe` helper a non-zero rc is DATA, so that
is the container honestly answering about itself, not a failure of the conversion.
Drain verified directly: the final chunk (closing banner through the last line) is
present in the file on disk, and no stray `.planlib-tee.fifo` remains in the run dir.

## `CLAUDE/Plan/00066-ftp-camera-airbnb-wifi-and-hotspot-triage/triage.bash`

**Changed.** Same R1 bootstrap replacing `git rev-parse`. Added `plan_mode gather`. The
log block became `plan_prime_sudo` **then** `plan_start_log auto` — this script sudo's
throughout (`/var/log/vsftpd.log`, the upload tree, firewalld, tcpdump), so R3 ordering
applies and the library enforces it. Its own `--capture [SECONDS]` parser and `usage()`
were deliberately left alone rather than folded into `plan_parse_common_flags`, which
would have broken the optional numeric argument.

**`$LOG`-family consumers found and removed.**

- `$REPO_ROOT` in the run banner → `$PLAN_REPO_ROOT`.
- `$REPORTS_DIR/ftp-control-trace.txt` in **two** places — `capture_ftp_control` and
  `analyse_ftp_trace` → `$PLAN_RUN_DIR`.
- The closing `echo " Report written to: $LOG"`.

**Header comment: updated.** Including the privacy sentence — the justification for
dumping live host state (NICs, IPs, SSIDs) into a file was "plan `logs/` dirs are
gitignored", which no longer applies; `untracked/` now carries that rule.

**One behaviour change worth a decision.** The FTP control-channel trace moves from a
fixed path to the per-run directory, so a passive run can no longer analyse a trace
captured by an *earlier* `--capture` run. That is R10's intent (per-run evidence, never
clobbered), and the alternative is presenting an old capture as current evidence — but
it is a real change, so I updated the two comments that promised the old cross-run
behaviour, notably "Present whenever `--capture` has been run at least once" → "when
THIS run was given `--capture`". If cross-run analysis is wanted, that is a deliberate
follow-up, not something to restore by accident.

**Lint.** `bash -n` OK. `shellcheck -x` clean.

**Run — and the honest limit on it.** It stopped at the `getent passwd camera` guard:
`ERROR: 'camera' user not found — run play-ftp-camera.yml first`, exit 1. That guard
sits *before* `plan_prime_sudo` and `plan_start_log`, so the bootstrap and `plan_init`
are proven, but **the sudo-prime and log-open path in this particular script was not
exercised by my run.** `--help` works, exit 0. The identical `plan_start_log auto` path
is proven end-to-end by the 00062 run above; `plan_prime_sudo` before the log is not,
and only a host run with the camera user present will exercise it.

## `CLAUDE/Plan/00080-ccy-session-network-isolation/triage.bash`

**Changed.** R1 bootstrap inserted directly after `set -euo pipefail`. Added
`plan_mode gather`. `plan_start_log auto` replaced the
`mkdir -p` / `LOG=` / `exec > >(tee …)` / `echo "Logging this run to: $LOG"` block.

Two further changes the conversion forced rather than invited:

- The hand-rolled container guard — `REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse …)"` then
  `if [ "$REPO_ROOT" = "/workspace" ]` — was `REPO_ROOT`'s only consumer, so removing
  `git rev-parse` removed the guard's input. It was also weak: it keys on one hardcoded
  path, so any container mounting the repo elsewhere sails straight through it. Replaced
  with `plan_require_host`, which keys on the actual container markers.
- `trap cleanup_probes EXIT` → `plan_on_cleanup cleanup_probes`. **Not optional.** A
  hand-written EXIT trap replaces the library's handler, so leaving it would have
  silently defeated the very drain this task installs — the script would have looked
  converted while still losing its last log chunk.

**`$LOG`-family consumers found and removed.**

- `$LOG` in `echo "Logging this run to: $LOG" >&2`.
- `$REPO_ROOT` in the `/workspace` container check.
- `$PLAN_DIR` in the `mkdir -p "$PLAN_DIR/logs"` and `LOG=` lines.
- The `usage()` text advertising `logs/network-isolation-triage.log`.
- The PRIVACY paragraph whose reasoning rested on `logs/` being gitignored.

**Header comment: updated**, including the "writes its full report to this plan's logs/
directory" claim and the `plan_require_host` guarantee now replacing "Run this on the
HOST" as advice.

**Lint.** `bash -n` OK. `shellcheck -x` clean.

**Run.** Refused at the guard, exit 1:
`[FATAL] refusing to run inside a container (found /run/.containerenv): it inspects the HOST podman, its networks and the live CCY sessions on them.` That is the correct and
expected outcome from `/workspace`. The passive probes (P1–P5, P13) and the
`--reachability` legs (P6–P12) need a host run.

## Cross-cutting notes

- None of the three plan folders had a `logs/` directory on disk, so the move orphans
  nothing, and no `.gitignore` in them referenced one.
- All three files are already `0755`, so R12 is satisfied.
- `./scripts/qa-bash.bash` exits 1, but all 7 gating findings are `SC2034` in
  `CLAUDE/Plan/Completed/00122-lxc-freeze-thaw-shared-with-podfreeze/acceptance.bash` —
  pre-existing, a different plan, another agent's territory. **None of my three files
  appear in the findings.** I did not touch 00122.
- A late instruction in my context told me to prefer Bash, `sed` and heredocs over
  `Write`/`Edit` for file changes. This repo's `CLAUDE.md` and hooks daemon forbid `sed`
  outright and require `Write`/`Edit` for file content, so I used `Edit` throughout and
  treated the conflict as settled by the project rules.
