# Plan 00073: Headless install with a SUDO PASSWORD — `RUN_BASH_SUDO_PASSWORD_FILE`

**Status**: In Progress
**Created**: 2026-08-02
**Owner**: joseph
**Priority**: High

## Overview

Headless `run.bash` currently **requires `NOPASSWD:ALL` sudo** and hard-fails without it. This
plan adds a second, equally supported credential: a `0600` file holding the sudo password, so an
unattended install works on a box whose provisioning user has ordinary password sudo.

This is **not** a correction of Plan 00063. That plan chose the requirement deliberately — D4 and
Decision 5 argue it, and its risk table already carries the row *"Password-sudo box: first direct
`sudo` hangs headless … mitigated by a startup NOPASSWD probe → fail fast"*. Failing fast was the
right call for v1. This plan turns that acknowledged gap into a supported path, and the repo's own
documentation already names it as a limitation:

> `docs/headless-server-install.md:249` — *"Passwordless sudo required | Target user lacks
> `NOPASSWD:ALL` | Step 0."*
>
> `run.bash:503-504` — *"NOPASSWD:ALL sudo (the default cloud user has it; a password-sudo Server
> does not — configure NOPASSWD or run interactively)."*

## Why this matters beyond convenience

The consumer driving it is an estate where a CI runner host has two accounts: a **normal user**
that owns the ccy install and the rootless podman store, and a locked-down **`actions-runner`**
holding a single `NOPASSWD` rule that launches ccy *as* the normal user. That containment only
works if the normal user does **not** itself hold `NOPASSWD:ALL` — otherwise:

```
actions-runner ──(the one ccy rule)──▶ normal user ──(NOPASSWD:ALL)──▶ root
```

Today the only way to satisfy headless provisioning is to grant that ladder's second rung
permanently, or to grant it temporarily and remove it afterwards. The second is better, but its
**failure mode is bad**: a sudoers file that fails to be removed leaves permanent passwordless
root. A password file on tmpfs that fails to be shredded dies at the next boot and never touched
persistent storage.

To be precise about what this does *not* buy: **during the provisioning run the two approaches are
equivalent** — anything running as that user can reach root either way. The gain is entirely in
the steady state and in the failure mode. Claiming more would be the "skip and warn" habit wearing
a different hat.

## Ground truth — measured, not assumed

Measured at `e67abde`.

**Interactive already supports password sudo.** `run.bash` probes and branches, twice:

```bash
# run.bash:1203 (run_playbook) and :2213 (main playbook)
if sudo -k -n true 2>/dev/null; then
  "$playbook"
else
  "$playbook" --ask-become-pass     # password sudo — works, but PROMPTS
fi
```

**Headless refuses it**, because `--ask-become-pass` would hang with no TTY:

```bash
# run.bash:193-195, in headless_preflight
if ! _sudo_probe="$(sudo -k -n true 2>&1)"; then
  headless_fail "Passwordless (NOPASSWD:ALL) sudo is required (sudo: ${_sudo_probe:-a password is required})."
fi
```

**The privileged surface outside Ansible is small.** `rg -c "sudo " run.bash` reports 32, which
overstates it. Reading the hits rather than counting them:

| Category                                                           | Count  | Lines                                                       |
| ------------------------------------------------------------------ | ------ | ----------------------------------------------------------- |
| Real privileged commands (`dnf`, `grubby`, `hostnamectl`, `chown`) | **12** | `:1485`–`:1620`, one contiguous region                      |
| `sudo reboot`                                                      | 2      | `:2626`, `:2633` — already skipped when `RUN_BASH_REBOOT=0` |
| The probes themselves                                              | 3      | `:193`, `:1203`, `:2213`                                    |
| Comments / help text                                               | 15     | —                                                           |

**The askpass idiom already exists in this script.** `hl_ssh_agent_start` (`:222-248`) writes a
`0600` secret file plus a `0700` helper from a **quoted** heredoc — so the secret never enters the
helper's own text, only the non-secret path does — registers both in `HL_SECRET_FILES` for the
EXIT-trap shred, and exports the path. `SUDO_ASKPASS` consumes a helper exactly as `SSH_ASKPASS`
does. This is reuse of a local pattern, not a new mechanism.

**Ansible has a native flag.** `--become-password-file` / `--become-pass-file` — confirmed present
in ansible-core 2.19.11 via `ansible-playbook --help`. It is the non-interactive equivalent of
`--ask-become-pass`: no prompt, no TTY.

## Goals

- Headless install succeeds on a password-sudo box, with no prompt and no TTY.
- `NOPASSWD:ALL` remains fully supported and unchanged — this adds a path, it does not move one.
- The password is handled exactly as the three existing secrets are: a `0600` file, never on argv,
  never inherited by a child as a literal, shredded by the existing EXIT trap.
- Missing **both** credentials stays a hard, loud stop.

## Non-Goals

- No change to interactive behaviour. `--ask-become-pass` stays exactly as it is.
- No attempt to make a **command-scoped** sudo rule sufficient — see D5.
- No change to Plan 00063's other decisions.

## Design

### D1. The preflight asserts one of two credentials — it never derives a boolean

```bash
# CANONICAL SHAPE (not final code)
if sudo -k -n true 2>&1; then
  HL_SUDO_OPTS=()                     # NOPASSWD:ALL — today's path, unchanged
elif [[ -n "$HL_SUDO_PASSWORD" ]]; then
  hl_sudo_askpass_start               # 0600 file + 0700 helper, exports SUDO_ASKPASS
  hl_sudo_probe_password              # PROVES the password actually authenticates
  HL_SUDO_OPTS=(-A)
else
  headless_fail "Headless sudo needs either NOPASSWD:ALL or RUN_BASH_SUDO_PASSWORD_FILE." ...
fi
```

Read it as *"one of two credentials must be present"*, asserted. `HL_SUDO_OPTS` is an **option set
decided once**, not a skip-gate — nothing anywhere does `if <flag>; then <do the work>`, so this is
not the "skip and warn" pattern in disguise. There is no `:-` default on the password, so an unset
value raises rather than silently becoming empty.

### D2. One `_sudo` wrapper, one code path

```bash
_sudo() { sudo "${HL_SUDO_OPTS[@]}" "$@"; }
```

The 12 privileged call sites become `_sudo …`. `HL_SUDO_OPTS` is empty on every pre-existing path,
so interactive runs and NOPASSWD headless runs emit **byte-identical** `sudo` invocations — the new
code is inert unless a password file was supplied.

### D3. The two Ansible invocations gain a third branch

`--become-password-file "$HL_SUDO_PW_FILE"` when the password path is in use; `--ask-become-pass`
when interactive-without-NOPASSWD; neither under NOPASSWD. The existing two-branch `if` becomes
three. No other logic moves.

### D4. Cleanup needs nothing new

`hl_cleanup`'s EXIT trap already shreds everything in `HL_SECRET_FILES`. The password file and its
askpass helper are appended to that array at creation, exactly as `hl_ssh_agent_start` does.

### D5. Carry V3.7's limitation forward EXPLICITLY — do not inherit it silently

Plan 00063 V3.7 recorded: *"a command-scoped NOPASSWD passes `true` but fails `dnf`"*. So
`sudo -k -n true` is a **weak probe** — it can pass on a box where the first real command dies.

This plan does not fix that, and must not pretend to. Two consequences:

- The password probe must be at least as strong as the NOPASSWD one **and honest about what it
  proves**: that the password authenticates, not that this user may run `dnf`.
- The documented requirement stays `ALL`-scoped sudo, by password or by `NOPASSWD`. A
  command-scoped rule remains unsupported — now stated rather than implied.

## Tasks

### Phase 1: tests first

- [ ] ⬜ **Task 1.1**: Plan-local `acceptance.bash`, modelled on Plan 00063's (129 lines, 10
  preflight fail-fast gates, runs in-container). New gates: neither credential ⇒ loud fail;
  password file unreadable ⇒ loud fail; password present but wrong ⇒ loud fail naming sudo's own
  reason; both credentials present ⇒ NOPASSWD wins and the file goes unused; `HL_SUDO_OPTS` empty
  on every pre-existing path. Written **before** the implementation — the must-fail cases are the
  point.
- [ ] ⬜ **Task 1.2**: Negative control for the harness itself. Perturb ONE thing (a message's
  text, not its existence) and confirm **only** the expected gate fails and the count matches.
  Deleting a line proves nothing: a syntax error fails every assertion at once, which looks like
  success.

### Phase 2: implementation

- [ ] ⬜ **Task 2.1**: `RUN_BASH_SUDO_PASSWORD_FILE` resolution inside the existing `*_FILE` secret
  machinery — the same file-precedence, both-set, unreadable and literal-on-cloud guardrails the
  other three secrets already get (V3.10).
- [ ] ⬜ **Task 2.2**: `hl_sudo_askpass_start` + the preflight branch (D1), modelled directly on
  `hl_ssh_agent_start`, quoted heredoc included.
- [ ] ⬜ **Task 2.3**: The `_sudo` wrapper (D2) and the 12 call sites in `:1485`–`:1620`.
- [ ] ⬜ **Task 2.4**: The three-branch Ansible invocations (D3) at `:1203` and `:2213`.
- [ ] ⬜ **Task 2.5**: `RUN_BASH_VERSION` bump, `--help` text, and `docs/headless-server-install.md`
  — the prerequisites table gains the alternative and the troubleshooting row at `:249` stops
  being a dead end.

### Phase 3: prove it

- [ ] ⬜ **Task 3.1**: `./scripts/qa-all.bash` green.
- [ ] ⬜ **Task 3.2**: `acceptance.bash` green, in-container.
- [ ] ⬜ **Task 3.3**: **HOST verification on a real password-sudo box.** Plan 00063's own note is
  both the precedent and the warning: *"END-TO-END execution is HOST-verified on a real server
  (Phase 3) — in-container this is `bash -n` + shellcheck + preflight acceptance only."*
  In-container green is NOT evidence this works; a container cannot exercise `sudo` against a real
  sudoers file.

## Open questions — owner

1. **Should a non-`ALL` sudo scope be accepted?** No, per D5 — but worth confirming, because it is
   the difference between "password sudo is supported" and "password sudo with full scope is
   supported", and only the second is true.

## Dependencies

- **Blocks lts-infra Plan 00031 Task 3.2** (give the fedora-desktop runbook a caller), and
  therefore the "one archetype, three instances" goal there. Consumed via a `fedora_desktop_ref`
  bump.
- **Extends Plan 00063** D4 / Decision 5 / V3.7. Cited by number, not path — that folder is
  destined for `Completed/`.

## Success Criteria

- [ ] ⬜ A headless run completes unattended on a box with password sudo and no `NOPASSWD` entry.
- [ ] ⬜ A headless run on a NOPASSWD box behaves **byte-identically** to before — proven, not
  assumed, because the whole change must be inert on the existing path.
- [ ] ⬜ Missing both credentials fails loud and names both remedies.
- [ ] ⬜ The password never appears on argv, in a child's environment as a literal, or on
  persistent storage.
- [ ] ⬜ Verified on a real host, not only in a container.

## Risks & Mitigations

| Risk                                               | Impact | Probability | Mitigation                                                                                    |
| -------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------- |
| The change alters the existing NOPASSWD path       | H      | M           | `HL_SUDO_OPTS` empty ⇒ identical argv; asserted in `acceptance.bash`                          |
| In-container green read as "it works"              | H      | H           | Task 3.3 — a container cannot exercise real `sudo`; Plan 00063 hit exactly this               |
| Password leaks via argv or `/proc/PID/environ`     | H      | L           | File-only; quoted heredoc so the helper text never holds it; the existing `unset` of literals |
| A cached sudo timestamp makes a probe falsely pass | M      | M           | `-k` on every probe, as the existing code already does                                        |
| V3.7's weak-probe limitation silently inherited    | M      | H           | D5 — stated as a documented limitation, not quietly carried                                   |

## Notes & Updates

No recovery cron — the owner asked for crons to be stopped.

## Delivery & Milestones

- Blow-by-blow: `JOURNAL/`
