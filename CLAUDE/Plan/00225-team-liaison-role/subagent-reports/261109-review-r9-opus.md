# Plan 00225 review r9 (narrow, adversarial, Opus)

Scope: commit 2ede9063d, which holds the round-8 fixes. Checked against the hooks daemon's
`untracked/` as it is on disk (by kind only), `daemon/paths.py` (`can_inline_bootstrap`,
`get_venv_path`, the venv resolver and `list-stale-venvs`), `daemon/cli.py`
(`_cleanup_proven_stale_venvs`), `daemon/metadata.py`, and DESIGN.md LZ17/LZ20 and PLAN.md Tasks
0.1, 5.1 and 5.4.

## Round 8 findings

- **r8 B1 (the liaison rebuilds the editable virtualenv into the `ro` `src/`): resolved.**
  - DESIGN:380-386 binds the checkout's container virtualenv `ro` beside `bin/`. It says the
    liaison never builds one, and that a missing or stale virtualenv refuses the launch.
  - The phrase "builds its own virtualenv there" is gone. Task 5.1 (PLAN:233-235) matches, and
    so does LZ20 (DESIGN:472-474).
  - The tree supports the shape:
    - The container venv's name carries a slug of `/workspace/.claude/hooks-daemon` and the
      image Python's fingerprint (`get_venv_path`, `paths.py:933-949`). A liaison container of
      the same image therefore looks for exactly that name.
    - Its editable `.pth` resolves to `/workspace/.claude/hooks-daemon/src`, which is present
      in the `ro` checkout.
    - Bytecode writes into a `ro` tree fail silently in CPython, so Task 0.1's "no bytecode
      writes" probe is a measurement, not a hazard.
    - The start-time cleanup (`cli.py:3489-3536`) handles an `rmtree` of a `ro`-bound venv by
      logging and continuing (`log_and_continue`), so it cannot stop the daemon.
  - Task 0.1 (PLAN:108-111) now probes acceptance with no rebuild.
- **r8 s1 (`supervise/` and the upgrade directories are state): resolved.** DESIGN:356-361 now
  makes only `hooks-relay` and the virtualenvs configuration. The supervisor's status, the
  upgrade gate and approvals and the transcript archives are named as state, and Task 0.1's
  classification is the list.
- **r8 s2 (permission repair out of reach): resolved in DESIGN.** The per-seat root is `0700`
  (DESIGN:386-388; LZ20:472). Task 5.1 says "root `0700`". Its tests-first list does not check
  it; see m1.
- **r8 m1 (PLAN:231 ordering leftover): resolved.** "ordered after it" is gone.
- **r8 m2 (the allowlist wording): resolved.** DESIGN:395 says "the state entries Task 0.1
  classifies".

## Blocking

None.

## Should-fix

- **s1. "stale" is undefined, and the daemon already uses the word for something else.**
  - DESIGN:385, LZ20:474 and Task 5.1 refuse a launch on a "missing or stale" virtualenv. The
    host-side Go launcher has no rule for deciding that.
  - In the daemon, `stale` (`VENV_STALE`, `paths.py:3032`, printed by `list-stale-venvs`) means
    "positively proven safe to remove". That is a different question from "the resolver would
    refuse this venv and rebuild it". The resolver's question is answered by its metadata check
    against `pyproject.toml`/`uv.lock` (`paths.py` around 1300-1390) and by the fingerprint in
    the name.
  - Two hazards:
    - A Go reimplementation drifts from the daemon's rule.
    - After an image Python bump, two `venv-<slug>-pyXY-*` directories exist, and the host
      cannot compute which fingerprint the image expects.
  - If the wrong one is bound, or a venv the resolver refuses is bound, the per-seat
    `untracked/` is writable, so `can_inline_bootstrap` (`paths.py:744-822`) allows a rebuild.
    That rebuild hits the `ro` `src/` or the `ro` mount point, which is the r8 B1 failure
    again, only later.
  - Fix: say that the judge is the daemon's own resolver (its stdlib-runnable `paths.py` CLI,
    run under the image's Python), so its answer picks the venv and decides whether to refuse.
    Never reimplement it in Go. Use a word other than "stale", or define it. Task 0.1 can
    measure the mechanics, but the plan should name the rule now.

## Minor

- m1. Task 5.1's tests-first list (PLAN:243-252) does not include two behaviours the task
  states: a missing or stale virtualenv refuses the launch (naming it), and the per-seat root
  is `0700`. r8 s2 asked for the fixture to check the second.
- m2. Task 5.4 (PLAN:263-267) does not carry LZ20's additions: the state directories created
  `0700`, and a missing or stale guest virtualenv failing the apply. "the executables" covers
  the venv's `BindReadOnlyPaths=`, but neither behaviour has a rendered-unit test.
- m3. DESIGN:388-390 has a stray line break left by the edit ("Directory-level, never per" /
  "file, ..."), and line 398 runs past the wrap width. Cosmetic only.
- m4. Task 0.1's parenthetical (PLAN:105-106) still names only "sockets, pid and lock files,
  logs and ledgers" as state. It does not contradict DESIGN:356-361, but it could point to
  DESIGN's list, so the two cannot drift.

## New contradictions

None found.

- LZ17's virtualenv rule (`ro`, never built), the wipe allowlist ("never `bin/` or a
  virtualenv, which are bound in, not kept"), Task 0.1's probe, Task 5.1 and LZ20 are
  consistent.
- "Directory-level ... does not depend on names known only after the container exists" still
  holds. The venv's name depends on the image, not the container instance. Choosing it is s1.

## Public repo

Nothing install-specific in the diff or this report. The virtualenvs are named by kind and by
the generic `/workspace` container path. The host checkout's own venv name, which embeds a host
path, appears nowhere.

VERDICT: MERGE
