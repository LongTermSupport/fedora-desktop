# Plan 00225 review r8 (narrow, adversarial, Opus)

Scope: commit 51deefebd, which holds the round-7 fixes. Checked against the hooks daemon's
`untracked/` as it is on disk (by kind only), `daemon/paths.py`, the daemon's `pyproject.toml`
and `src/`, `files/var/local/claude-yolo/{claude-yolo,lib/restart-request.bash}` and
`src-go/internal/teamchat/proto/filter.go`.

## Round 7 findings

- **r7 B1 (per-file binds inside a `ro` `untracked/`): resolved in shape.**
  - LZ17 (DESIGN:375-382) mounts a per-seat directory over the whole of the daemon's
    `untracked/`, with `bin/` bound `ro` over it.
  - The daemon then finds `untracked/` writable (`paths.py:804-811`). It names its
    hostname-suffixed runtime set itself (`paths.py:1785, 2093`) and can rename within the
    directory.
  - LZ20 has the same directory-level shape (DESIGN:461-466) and the PLAN risk row is replaced.
  - Task 0.1 now probes that the daemon starts and hooks fire. Task 5.1's fixture does too.
  - `bin/` holds only `hooks-relay` and its `.route` and `.sha256` files, so a single
    directory bind is enough.
  - One element of the new layout fails against the tree: the per-launch virtualenv build. This
    is the new blocker B1 below.
- **r7 s1 (executed code survives the reset): resolved.** The wipe is now inverted to a state
  allowlist and names `shell-snapshots/`, `__pycache__/` and a planted virtualenv
  (DESIGN:385-391).
- **r7 s2 (bash reads on the ratchet): resolved.**
  - DESIGN:398-402 names `claude-yolo` and `lib/restart-request.bash`.
  - The Go front prints the directory, at no net line, or the read moves to Go.
  - `qa-bash-limits` is the proof.
  - `CCY_RESTART_REQUEST_REL` (`restart-request.bash:34`) is a single constant, so a net-zero
    swap is plausible.
- **r7 s3 (SyncFilter): resolved.** `proto.SyncFilter` (`filter.go:26`) is named in DESIGN
  section 8 and in Task 1.1, with a golden for the no-liaison case (LZ0).
- **r7 m1 (token file): resolved.** The phrase is gone.
- **r7 m2 (mount order): resolved in DESIGN:370-372, but a leftover remains in PLAN:231.** See m1
  below.
- **r7 m3 (status line): resolved.**

## Blocking

**B1. The liaison cannot build its own virtualenv on a `ro` checkout, so under LZ17 as written
its daemon never starts and LZ15/LZ16's guard is absent.**

DESIGN:378 says the daemon "builds its own virtualenv there", in the per-seat `untracked/`.
DESIGN:387-388 wipes every virtualenv at each launch, so this build runs on every liaison
launch.

The daemon's venv is an *editable* setuptools install of `.claude/hooks-daemon/src`:

- `pyproject.toml` has `build-backend = "setuptools.build_meta"`.
- The venv holds `__editable__.claude_code_hooks_daemon-<ver>.pth`, which points at
  `.../hooks-daemon/src`.

Building it runs `egg_info`, which writes `src/claude_code_hooks_daemon.egg-info/` *inside the
checkout*. On disk, that directory's files carry the timestamp of the most recent venv build,
so every build rewrites them. For the liaison, `src/` is in the `ro` checkout. The build fails
with `EROFS`, the bootstrap reports failure, and no daemon runs. Without the daemon:

- no hook does anything;
- LZ15/LZ16's guard is gone;
- `R-*` safety rules are gone too.

Task 0.1's new probe would catch this, but the design states a mechanism that the tree already
shows cannot work. Task 0.1 also has no fallback to turn to when the probe fails.

The fix fits LZ17's own rule, because a virtualenv is configuration and is meant to be `ro`:

- Bind the checkout's container virtualenv (the one named after `/workspace`, which every ccy
  seat of the checkout shares) `ro` into the per-seat `untracked/`, beside `bin/`. Its `.pth`
  resolves to the `ro` `src/`, which is present.
- If that venv is missing or stale at launch, refuse the liaison launch and name it. Never build
  inside the liaison.
- Remove "builds its own virtualenv there" from DESIGN:378 and the "per-seat ... virtualenv"
  wording from Task 5.1 (PLAN:231).
- LXC (LZ20, Task 5.4) needs the same treatment: the guest's venv goes in `BindReadOnlyPaths=`.
- Task 0.1 should check that the daemon accepts the `ro` venv: that its metadata or fingerprint
  check does not trigger a rebuild, and that Python runs with no `__pycache__` writes.

## Should-fix

- **s1. DESIGN:356-359 still calls the daemon's "upgrade and supervisor directories" all
  configuration, but the new layout only binds executables `ro`.**
  - `supervise/` holds JSON status files that the in-container supervisor rewrites throughout a
    session. They are not executed, so by LZ17's rule they are state.
  - If Task 0.1 follows line 358 and binds them `ro` from the checkout, the liaison's supervisor
    cannot write its status and reads another seat's.
  - Reclassify `supervise/` as per-seat state, and decide `upgrade-gate/` and
    `upgrade-approvals/`. They hold JSON, so they belong in the per-seat directory, empty or
    seeded.
  - Bring line 358 into line with "`bin/` and the virtualenv `ro`; everything else per-seat".
- **s2. The daemon's world-writable transcript archives move out of reach of the launcher's
  permission repair.**
  - `claude-yolo` `repair_project_state_permissions` (around line 2240) exists because the
    daemon `umask(0)`s and writes `untracked/transcripts/` at 0666, inside `$PWD/.claude`.
  - The repair runs on every launch.
  - For a liaison, that archive is in the per-seat host directory outside the checkout, so the
    repair no longer covers it.
  - `launch-mounts` should create the per-seat root at 0700, or apply the same repair. Task
    5.1's fixture should check it.

## Minor

- m1. PLAN:231 still says "`ccy.env.local`'s `ro` bind ordered after it". Round 7 m2 was fixed
  in DESIGN only. Use "each destination and its options pinned", as at PLAN:241.

- m2. The wipe allowlist for the daemon directory is "logs and ledgers" (DESIGN:387). Other
  entries are not covered by that wording:

  - `secret-meta.key.secret`, the HMAC key, which is recreated per launch if wiped, so digests
    differ across launches;
  - `transcripts/`;
  - `events-<id>/`.

  Say that Task 0.1's classification is what fills the allowlist.

## Public repo

Nothing install-specific in DESIGN.md, PLAN.md or this report. The daemon's runtime files, the
container hostname and the virtualenvs are named by kind only.

VERDICT: CHANGES — 1. B1: LZ17/LZ20 have the liaison build its own virtualenv at every launch,
but that venv is an editable setuptools install whose build writes `src/*.egg-info` into the
`ro` checkout. The build fails, no daemon starts and LZ15/LZ16's guard is absent. Instead, bind
the checkout's container venv `ro` beside `bin/`, refuse the launch if it is missing or stale,
and never build inside the liaison.
