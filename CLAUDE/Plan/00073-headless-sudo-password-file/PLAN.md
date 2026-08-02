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

- [x] ✅ **Task 1.1**: Plan-local `acceptance.bash` — written **before** the implementation, and
  currently **RED by design**: 1 regression leg passes, the 4 new gates fail because the feature
  does not exist yet. Built on `_planlib.inc.bash` (`plan_mode gather`, `plan_gather_leg` per
  case, `plan_finish` owning the exit code) per R1/R10, unlike Plan 00063's, which predates the
  standard and still uses the banned `git rev-parse --show-toplevel`.
  Leg logic is split into `_acceptance-cases.inc.bash` — ShellCheck cannot see through
  `plan_gather_leg "<name>" expect_fail …` and reported all 14 lines as SC2317; suppression is
  banned, so the structural carve-out the standards name ("leg logic split out to avoid SC2317")
  is the fix. `shellcheck -x` clean; `bash -n` clean **with empty stderr**, which is the check
  that matters since `bash -n` can print a diagnostic and still exit 0.
- [x] ✅ **Task 1.2**: Discrimination proven — and it caught two real defects in the harness
  itself before either could mislead. See the finding below; the harness now **refuses to run**
  where it cannot discriminate, rather than reporting.

> **The harness could not tell its own cases apart, and one leg passed for the wrong reason.**
> The first RED run failed all four new gates — correct — but every case emitted *byte-identical*
> output: `sudo: command not found`. **`sudo` is not installed in a CCY container at all.** So the
> regression leg's "PASS" was a **false pass**: it asserts the message contains `NOPASSWD`, and it
> did — in a message produced because the binary was missing, not because a credential was.
> After implementation this harness would have kept reporting green while proving nothing.
> That is "a control that FIRES is not necessarily a control that DISCRIMINATES", in a test
> harness, which is the worst place for it. Fixed by refusing outright when `sudo` is absent —
> the same *unknown is a refusal* decision Plan 00072 made for the rootless-engine guard.
>
> **Then the refusal probe was itself broken in the same class.** `env -i … command -v sudo`
> fails with `env: 'command': No such file or directory` because `command` is a shell BUILTIN —
> so it would have refused everywhere, including on boxes where sudo exists. Caught only by
> reading the refusal's *stated reason* instead of its exit code. Now `bash -c 'command -v sudo'`,
> and proven to discriminate by positive and negative control: finds `bash` (rc=0, prints
> `/usr/bin/bash`), does not find `sudo` (rc=1, empty).

### Phase 2: implementation

- [x] ✅ **Task 2.1**: `RUN_BASH_SUDO_PASSWORD_FILE` resolved through the existing
  `hl_resolve_secret`, so it inherits the file-precedence, both-set, unreadable and
  literal-on-cloud guardrails (V3.10) rather than re-implementing them. Resolved *before* the
  `unset` of literal secrets, and added to that `unset` list.
- [x] ✅ **Task 2.2**: `hl_sudo_askpass_start` + `hl_sudo_probe_password` + the three-way preflight
  branch (D1). One DELIBERATE difference from `hl_ssh_agent_start`, argued in the code: the helper
  interpolates the file **path** (`printf %q`) instead of reading an exported variable at askpass
  runtime. The path is not secret either way — what it buys is independence from whether sudo
  propagates the caller's environment to the askpass child, which is unverifiable in a container
  and would fail **silently** (unset var → `cat ""` → empty password → a wrong-password error
  blaming the operator's file). ssh-add's propagation is proven on this path; sudo's is not.
- [x] ✅ **Task 2.3**: The `_sudo` wrapper (D2) and **14** call sites, not 12 — see the finding
  below. Preflight also now reports *which* credential it proved (`sudo=NOPASSWD:ALL` /
  `sudo=password (…)`), because the two paths are otherwise indistinguishable in an unattended log.
- [x] ✅ **Task 2.4**: The three-branch Ansible invocations (D3). The new branch is checked
  **first**, so `sudo -k` never runs on the password path and never discards the timestamp the
  preflight probe just established. Both sites also stopped hiding the probe's stderr behind
  `2>/dev/null` — it is captured and now appears in the "sudo needs a password" message.
- [x] ✅ **Task 2.5**: `RUN_BASH_VERSION` → 1.11.0, `--help` + `--help-run-headless`,
  `docs/headless-server-install.md` (Step 0 split into 0a/0b, two new troubleshooting rows) and
  `docs/headless-provisioning.md` (preconditions + secret table). The `:249` row is no longer a
  dead end.

> **The call-site count was 12; the correct count was 14.** The plan's table categorised
> `sudo reboot now` (×2) separately as *"already skipped when `RUN_BASH_REBOOT=0`"*. That is true,
> and it is not a reason to leave them — with `RUN_BASH_REBOOT=1` on a password-sudo box they would
> hang on a prompt, which is precisely the failure this plan exists to remove. A true statement
> about the default configuration, used to justify a conclusion about all configurations.

### Phase 3: prove it

- [x] ✅ **Task 3.1**: `./scripts/qa-all.bash` green — 488 files, exit 0, and the shellcheck issue
  count **unchanged at 105**, which is the number that matters (a clean exit with a risen count
  would mean this change added debt the gate tolerates).
- [x] ✅ **Task 3.2**: Group A (`_sudo` argv inertness) green in-container, and **proven to
  discriminate** — see below. Group B (the credential gates) **refuses** here rather than
  reporting: sudo is not installed in a CCY container, so it moves to Task 3.3 by necessity, not
  by choice. The harness exits non-zero while that is true, so nothing downstream can read this
  run as a verification.
- [ ] ⬜ **Task 3.3**: **HOST verification on a real password-sudo box** — group B green, plus a
  real end-to-end run. Plan 00063's own note is both the precedent and the warning: *"END-TO-END
  execution is HOST-verified on a real server (Phase 3) — in-container this is `bash -n` +
  shellcheck + preflight acceptance only."* A container cannot exercise `sudo` against a real
  sudoers file, so nothing in Phase 3 so far is evidence that a correct password obtains privilege.

> **Success Criterion 2 is now measured rather than asserted.** "A NOPASSWD run behaves
> byte-identically" reduces, in a container, to one checkable claim: the argv `_sudo` builds. The
> leg extracts the **shipped** `_sudo` from `run.bash` (a copy would prove only that the copy
> works), runs it against a stub `sudo` first on `PATH`, and renders argv as `[a][b][c]` —
> bracketed, because the defect being excluded is an EMPTY argument, which in any other rendering
> vanishes into whitespace and makes `sudo "" dnf …` look identical to a correct call.
>
> **Discrimination proven by perturbation, not assumed.** Swapping `"${HL_SUDO_OPTS[@]}"` for the
> `[@]:-` idiom in a fixture fails **only** the empty-opts leg, with exactly the predicted
> `[][dnf][-y][install][git]`; the `-A` leg still passes (the fallback is harmless on a non-empty
> array); and deleting `_sudo` entirely trips the extraction guard with its own distinct message
> instead of passing. That surgical signature — one leg, predicted output — is what §9 asks for,
> as against the uniform-wrong-exit-code shape that proves nothing. A third leg asserts the
> `[@]:-` claim directly, so the comment justifying `_sudo`'s shape is tested rather than trusted.

## Open questions — owner

1. **Should a non-`ALL` sudo scope be accepted?** No, per D5 — but worth confirming, because it is
   the difference between "password sudo is supported" and "password sudo with full scope is
   supported", and only the second is true.
2. **A pre-existing false negative found while editing, deliberately NOT changed here.**
   `run.bash`'s legacy-cgroup check is `_sudo grubby --info=ALL 2>/dev/null | grep -q …`. When
   `grubby` *fails*, stderr is discarded, stdout is empty, the `grep` finds nothing, and the script
   reports `No legacy cgroup configuration found` — a check that could not look, reporting what a
   check that looked and found nothing reports. Same family as everything else in this plan.
   Fixing it means changing control flow on the **interactive** path too (abort where it currently
   continues), and a box with no bootloader would then fail a previously-working install. That is a
   separate decision with its own regression risk, so it is raised rather than bundled under this
   plan's banner. Worth its own plan.

## Dependencies

- **Blocks lts-infra Plan 00031 Task 3.2** (give the fedora-desktop runbook a caller), and
  therefore the "one archetype, three instances" goal there. Consumed via a `fedora_desktop_ref`
  bump.
- **Extends Plan 00063** D4 / Decision 5 / V3.7. Cited by number, not path — that folder is
  destined for `Completed/`.

## Success Criteria

- [ ] ⬜ A headless run completes unattended on a box with password sudo and no `NOPASSWD` entry.
  (Task 3.3 — a container cannot show this.)
- [x] ✅ A headless run on a NOPASSWD box behaves **byte-identically** to before — the argv half is
  proven and proven to discriminate (Task 3.2); the remaining half is that no other code path
  changed, which the three-branch Ansible edits keep true by checking the new branch first.
- [ ] ⬜ Missing both credentials fails loud and names both remedies. (Written; the leg asserting
  it is in group B, so it is unverified until Task 3.3.)
- [x] ✅ The password never appears on argv, in a child's environment as a literal, or on
  persistent storage — file-only via `hl_resolve_secret`, `RUN_BASH_SUDO_PASSWORD` added to the
  literal `unset`, both temp files registered in `HL_SECRET_FILES` for the existing EXIT-trap
  shred, and only the non-secret *path* is written into the askpass helper.
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
