# Plan 00067: rclone rc auth instead of no auth

**Status**: Complete (2026-08-08)
**Created**: 2026-07-30
**Owner**: joseph
**Priority**: Medium

## Overview

`playbooks/imports/optional/common/play-rclone.yml` renders `--rc-no-auth` into
every rclone mount's systemd unit. The in-repo justification (lines 250-255) is
that this is "safe here because `--rc-addr` binds to localhost only". That
premise does not hold: **loopback is not a user boundary.** Every local uid can
reach `127.0.0.1:5572`, and rclone's own documentation states that "Access to the
rc API is equivalent to shell access as the rclone user" — `--rc-no-auth` unlocks
`core/command` (run OS commands), `config/dump` (reads back the Google Drive
OAuth token), `operations/purge`, and `sync/*`.

On this host that is a concrete privilege-escalation path. The `camera` account
(uid 1001) exists to receive FTP uploads; it is the account that gets compromised
if vsftpd or the camera upload path is exploited. With `--rc-no-auth` deployed, a
compromised `camera` would gain shell-equivalent access as `joseph` — who has
passwordless sudo — plus the Drive credentials. Rootful Docker is also active, so
a `--network host` container reaches host loopback too.

The flag also buys very little, which is what settles the trade. The endpoints the
repo's tooling actually needs are already served **without** auth: `vfs/stats` and
`core/stats`. Across all four RC consumers only two auth-gated calls are used —
`vfs/refresh` (`rclone-cache-warm`) and `core/stats-reset` (cosmetic, in
`ftp-camera --copy`). This plan replaces blanket no-auth with real credentials so
those two work while removing the shell-equivalent exposure.

## Context & Background

Discovered while diagnosing `ftp-camera --copy` failing with "rclone remote
control not responding". Two independent faults were found:

1. **A preflight bug in `ftp-camera`** — it probed `core/pid`, an auth-gated
   endpoint the function never otherwise uses, so it failed closed on a mount
   that would serve `--copy` perfectly well. **Fixed and deployed ahead of this
   plan** (probe changed to `core/stats`); not part of this plan's scope.
2. **A deploy gap** — the running unit predates the `--rc-no-auth` commit
   (`9d365ec`, 2026-05-16), so the live mount has `--rc` without `--rc-no-auth`.
   Re-running the play as-written would have *introduced* the exposure above.
   This plan changes the play instead of deploying it unchanged.

### Endpoint auth matrix (measured on rclone v1.74.3, live mount)

| Endpoint           | Auth required | Used by                                        |
| ------------------ | ------------- | ---------------------------------------------- |
| `vfs/stats`        | no            | `rclone-tail`, `rclone-cache-status`, `--copy` |
| `core/stats`       | no            | `rclone-tail`, `--copy` (progress + preflight) |
| `core/version`     | no            | —                                              |
| `core/pid`         | **yes**       | nothing (was the buggy `--copy` preflight)     |
| `core/stats-reset` | **yes**       | `--copy` (cosmetic counter reset, best-effort) |
| `vfs/refresh`      | **yes**       | `rclone-cache-warm` — **currently broken**     |

### Flag/env facts (verified, not assumed)

- Server flags: `--rc-user` / `--rc-pass`. Client (`rclone rc`) flags: `--user` /
  `--pass` — **different names**, so no env-var collision.
- rclone maps flags to env vars as `RCLONE_<FLAG_WITH_UNDERSCORES>`. Confirmed
  empirically: `RCLONE_URL` drives the client's `--url`, and
  `RCLONE_USE_JSON_LOG` drives `--use-json-log`.
- Therefore the server can take credentials via `RCLONE_RC_USER` /
  `RCLONE_RC_PASS` in a systemd `EnvironmentFile=`, keeping the secret out of
  `ExecStart` (and so out of `ps` output and the unit file).

## Goals

- Remove `--rc-no-auth` from every rclone mount unit.
- Authenticate the RC with a host-local generated secret, never committed (this is
  a public repo).
- Keep the secret out of `ps` output and out of the unit file.
- Repair `rclone-cache-warm` (`vfs/refresh`) under authentication.
- Leave `ftp-camera --copy`, `rclone-tail`, and `rclone-cache-status` working.

## Non-Goals

- TLS on the RC socket. The bind stays loopback-only; TLS addresses a network
  threat that binding already excludes.
- Vaulting the RC secret into `host_vars`. It is a machine-local credential with
  no cross-host meaning, so generating it on the host is both simpler and safer
  than putting ciphertext in a public repo.
- Re-litigating the `ftp-camera` preflight fix — already delivered separately.
- Changing cache sizing, `vfs_write_back`, or any other mount tuning.

## Tasks

### Phase 1: Generate and wire the credential

- [x] ✅ **Task 1.1**: Add an idempotent task to `play-rclone.yml` that generates
  `~/.config/rclone/rc-auth.env` (mode `0600`, owner `{{ user_login }}`) with
  a random `RCLONE_RC_PASS` and a fixed `RCLONE_RC_USER`. **Built with the
  `ansible.builtin.password` lookup rather than the `creates:`-guarded shell
  command first sketched here** — the lookup writes on first use and reads back
  thereafter, so the credential is stable across runs with no shell block at all
  (avoiding the Ansible 2.19 `split_args` hazard, and `no_log: true` keeps it out
  of output). A separate `file` task pins the `.rc-pass` store to `0600`.
- [x] ✅ **Task 1.2**: Add `EnvironmentFile=` for that file to the mount unit
  template and **remove `--rc-no-auth`** from `ExecStart`.
- [x] ✅ **Task 1.3**: Replace the now-incorrect "safe because localhost" comment
  block with the real rationale (loopback is not a user boundary).

### Phase 2: Teach the RC clients to authenticate

- [x] ✅ **Task 2.1**: `rclone-cache-warm` — source the env file and pass
  `--user`/`--pass` to its `vfs/refresh` call. Fail with a clear, actionable
  message if the file is absent (naming the play to run), never silently.
- [x] ✅ **Task 2.2**: `ftp-camera` — authenticate the `core/stats-reset` call so
  the counters genuinely reset; leave the auth-free preflight and progress
  polling working with or without credentials.
- [ ] ❌ **Task 2.3**: ~~`rclone-tail` and `rclone-cache-status` — pass credentials
  when available so they keep working if upstream ever gates `vfs/stats`.~~
  **Cancelled — YAGNI.** Both use only `vfs/stats`, which rclone serves
  unauthenticated; they work today and are untouched by this plan. Plumbing
  credentials for a hypothetical future upstream change is speculative code the
  repo's YAGNI principle prohibits. If rclone ever does gate `vfs/stats`, both
  fail loudly and the fix is the same few lines already written for
  `rclone-cache-warm`. `acceptance.bash` (T3.3) still exercises both, so a
  regression would be caught.

### Phase 3: QA, deploy, verify

- [x] ✅ **Task 3.1**: Run QA: `./scripts/qa-all.bash`
- [x] ✅ **Task 3.2**: Write `triage.bash` — read-only endpoint auth matrix probe
  (per `CLAUDE/PlanTriage.md`), reporting which endpoints answer and which
  clients can authenticate. Redacts the RC password by substitution and
  **verifies** the redaction, deleting the report if the secret leaked.
- [x] ✅ **Task 3.3**: Write `acceptance.bash` — the pass/fail gate: no unit
  carries `--rc-no-auth`, every unit loads the `EnvironmentFile`, the credential
  is `0600` with both keys, unauthenticated `config/dump` is refused,
  authenticated calls succeed, and the client scripts work.
- [x] ✅ **Task 3.4**: (HOST) Run `deploy.bash` → `play-rclone.yml`. **Must not
  run while an `ftp-camera --copy` is in flight** — it restarts the mount and
  would interrupt the VFS write-back queue. `deploy.bash` now enforces this
  itself: it refuses to start if it finds a running `ftp-camera` process, and
  refuses to run inside the CCY container at all. **Ran on the HOST**; the unit
  was rendered correctly (no `--rc-no-auth`, `EnvironmentFile` present) but the
  credential it deployed was empty — see Task 3.6.
- [x] ✅ **Task 3.5**: (HOST) Run `acceptance.bash` — **NOT ACCEPTED, 7 passed,
  2 failed.** The gate did its job: it caught a broken deploy that every other
  signal reported as successful.

### Phase 4: Fix the two bugs acceptance exposed

- [x] ✅ **Task 4.1**: `play-rclone.yml` generated an **empty** RC password. A
  `file: state: touch` task created `.rc-pass` empty *before* the
  `ansible.builtin.password` lookup ran; that lookup only generates when the
  store is **absent** and otherwise reads the value back, so an empty store
  yields an empty password forever. Removed the pre-touch, added stat +
  `state: absent` recovery for an already-empty store, moved the owner/mode
  enforcement to *after* the lookup, and added a hard `assert` that the store is
  non-empty so the play fails fast instead of deploying an RC that refuses
  everyone.
- [x] ✅ **Task 4.2**: The play never restarted the mounts. The final systemd
  task uses `state: started` (a no-op on a running unit) and `daemon_reload`
  only re-reads definitions — so a changed `ExecStart` or `EnvironmentFile` sat
  on disk while systemd kept the old command line and environment until reboot.
  Added a `restart rclone mounts` handler notified by both the unit-file copy
  and the credential copy. The play had no handlers at all before this.
- [x] ✅ **Task 4.3**: Fixed two defects in `acceptance.bash` itself — it
  conflated "key absent" with "key present but empty" (producing a
  self-contradicting report), and check 4 was a **false pass** because a
  completely broken RC refuses everyone and therefore satisfies "unauthenticated
  access is refused". It now reports per key, prints the `.rc-pass` byte size,
  and cross-checks check 4 against whether authenticated access works.
- [x] ✅ **Task 4.4**: (HOST) Re-run `deploy.bash`, then `acceptance.bash` —
  **ACCEPTED, 11 passed, 0 failed** (2026-08-08 07:53). Both fixes are confirmed
  by evidence in the report, not by inference: `.rc-pass` is **33 bytes** where
  it was previously empty (so the lookup regenerated it), and the unit's
  `active since` is 07:52:52, seconds before the run — the new handler restarted
  it, which the old `state: started` never did.

## Technical Decisions

### Decision 1: real credentials, not a narrowed `--rc-no-auth`

**Context**: only two auth-gated endpoints are needed, so a narrower exemption
was considered.
**Options considered**: (a) keep `--rc-no-auth` — rejected, grants
shell-equivalent access for two minor calls; (b) drop RC auth needs entirely by
deleting `rclone-cache-warm` — rejected, it is a useful tool; (c) real
credentials via `EnvironmentFile` — chosen. rclone has no per-endpoint auth
exemption, so (c) is the only option that keeps the features without the
exposure.
**Decision**: (c).
**Date**: 2026-07-30

### Decision 2: host-generated secret, not vaulted

**Context**: the secret must reach both the service and the client scripts.
**Decision**: generate on the host into a `0600` file with `creates:` for
idempotence. It is machine-local, so committing ciphertext to a public repo would
add risk and management burden for no portability gain.
**Date**: 2026-07-30

## Success Criteria

All verified by `acceptance.bash` on the HOST — **ACCEPTED, 11 passed, 0 failed**
(`logs/rclone-rc-auth-acceptance.log`, 2026-08-08 07:53).

- [x] ✅ No mount unit contains `--rc-no-auth`
- [x] ✅ Unauthenticated `config/dump` is refused — **401**, not the 403 guessed
  when this criterion was written. The refusal is meaningful *because*
  authenticated calls succeed on the same run; a broken RC refuses everyone and
  would satisfy this check on its own (that false pass is now cross-checked).
  `core/command` was **not** probed — it executes OS commands, so firing it at a
  live mount to prove a point is not worth it; it is gated by the same global
  `--rc-user`/`--rc-pass` as `config/dump`.
- [x] ✅ Authenticated `vfs/refresh` succeeds. **Scope note**: this is the
  endpoint `rclone-cache-warm --fast` needs, and it was exercised directly with
  credentials. The script itself was run only as `--help` — `--fast` issues a
  real `vfs/refresh` against the mount, so it is left for the operator.
- [x] ✅ `rclone-tail` and `rclone-cache-status` run (both `--help`; both use
  only auth-free `vfs/stats`, unchanged by this plan). **`ftp-camera --copy` was
  not exercised** — it moves real data. Its RC calls are `core/stats` (auth-free)
  and `core/stats-reset` (gated by the same credential now proven working).
- [x] ✅ `~/.config/rclone/rc-auth.env` is mode `0600`; the secret is in no
  committed file (`CLAUDE/Plan/**/logs/` is gitignored and the report redacts by
  substitution then verifies), no unit file, and no `ps` output — it reaches
  rclone via `EnvironmentFile=`, never `ExecStart`.
- [x] ✅ QA passes (`./scripts/qa-all.bash`) — 423 files, `--syntax-check` clean.

## Risks & Mitigations

| Risk                                                  | Impact | Probability | Mitigation                                                                |
| ----------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------- |
| Mount restart interrupts an in-flight upload queue    | H      | M           | Gate deploy on `to upload 0, uploading 0`; never deploy during a `--copy` |
| 400G/42k-object VFS cache rescan delays service start | M      | H           | Already handled — unit sets `TimeoutStartSec=300`                         |
| Secret regenerated on re-run, breaking live clients   | M      | L           | `creates:` guard makes generation once-only                               |
| A client script left unauthenticated fails silently   | M      | M           | Clients fail loudly naming the play; `acceptance.bash` exercises all four |

## Delivery & Milestones

- Precursor (not this plan): `ftp-camera --copy` preflight probe fixed to
  `core/stats`, deployed via `play-ftp-camera.yml`
