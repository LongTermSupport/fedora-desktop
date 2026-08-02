#!/usr/bin/env bash
#
# Plan 00073 — acceptance tests for headless install with a SUDO PASSWORD.
#
# PURPOSE
#   Two groups, deliberately separated by what they need to be meaningful:
#
#   A. INERTNESS (no sudo binary needed) — the argv `_sudo` builds. This is the
#      container-measurable half of Success Criterion 2: the change must be inert on
#      every pre-existing path.
#   B. CREDENTIAL GATES (a real sudo binary needed) — each bad input must abort
#      non-zero with a message naming the fix. Plan 00063's acceptance.bash covers the
#      config/secret gates; this covers what Plan 00073 adds, plus one regression
#      assertion that 00063's final gate still fires.
#
#   Group B REFUSES rather than reports when sudo is absent — see the block below.
#
# WHERE TO RUN
#   Anywhere. It reaches no host, runs no Ansible and changes no state (gather mode).
#   In a CCY container only group A can run; group B needs a box with sudo installed.
#
# WHAT A GREEN RUN DOES NOT PROVE — read before trusting it.
#   The drop user (`nobody`) is not in sudoers AT ALL, so every group-B case fails at the
#   credential gate. That proves the gates fire and say the right thing. It proves NOTHING
#   about whether a CORRECT password actually obtains privilege, because a container has
#   no sudoers entry to authenticate against. Plan 00063 hit exactly this and wrote it
#   down: "END-TO-END execution is HOST-verified on a real server — in-container this is
#   bash -n + shellcheck + preflight acceptance only." Task 3.3 is the host run; this is
#   not a substitute for it.
#
# IDEMPOTENCE
#   Read-only and re-runnable. Every case aborts in preflight BEFORE any provisioning
#   action (no dnf, no clone, no ansible). Fixtures are mktemp files removed on exit.
#
# USAGE
#   ./CLAUDE/Plan/00073-headless-sudo-password-file/acceptance.bash [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR/..
# shellcheck source-path=SCRIPTDIR/../..
# shellcheck source=_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="Usage: acceptance.bash [-h|--help] [-y|--yes] [--check]

Plan 00073 acceptance: _sudo argv inertness, and the headless sudo-credential gates.
Read-only; reaches no host; safe in a container. The gate legs REFUSE (non-zero) where
sudo is not installed. A green run proves the GATES fire — NOT that a correct password
obtains privilege (that is Task 3.3, on a real host)."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
  printf '[FATAL] unexpected argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
  printf '%s\n' "${PLAN_USAGE}" >&2
  exit 64
fi
plan_start_log auto

RUN_BASH="${repoRoot}/run.bash"
DROP_USER="nobody"
readonly RUN_BASH DROP_USER

[[ -f "${RUN_BASH}" ]] || { printf '[FATAL] run.bash not found at %s\n' "${RUN_BASH}" >&2; exit 1; }

# Privilege-drop: prefer runuser (works as root without sudo), else sudo. Defined HERE
# rather than in the helper because the helper must not care how the drop is achieved.
if [[ -n "$(command -v runuser)" ]]; then
  drop_run() { runuser -u "${DROP_USER}" -- "$@"; }
elif [[ -n "$(command -v sudo)" ]]; then
  drop_run() { sudo -u "${DROP_USER}" -- "$@"; }
else
  drop_run() { return 127; }
fi

# Leg logic lives in a sourced helper so ShellCheck does not report every line of it as
# SC2317 — it cannot see through `plan_gather_leg "<name>" expect_fail …`, and suppression
# directives are banned. See the helper's header and plan-script-standards.md.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=_acceptance-cases.inc.bash
source "${scriptDir}/_acceptance-cases.inc.bash"

# Fixtures. `readable` is 0644 so $DROP_USER can read it; `unreadable` is 0600 owned by
# the invoking user, so it EXISTS but $DROP_USER cannot open it — the case that must not
# silently degrade to "no password supplied". SUDO_STUB_DIR holds the fake `sudo` the
# inertness legs measure against, plus the negative-control probe.
readable_secret="$(mktemp)"; printf 'sekret\n' > "${readable_secret}"; chmod 0644 "${readable_secret}"
unreadable_secret="$(mktemp)"; printf 'sekret\n' > "${unreadable_secret}"; chmod 0600 "${unreadable_secret}"
SUDO_STUB_DIR="$(mktemp -d)"
trap 'rm -rf "${readable_secret}" "${unreadable_secret}" "${SUDO_STUB_DIR}"' EXIT
make_sudo_stub "${SUDO_STUB_DIR}"

# The negative control's probe, in a quoted heredoc so nothing expands here — it must
# reach bash verbatim to exercise the idiom under test.
colondash_probe="${SUDO_STUB_DIR}/colondash-probe.bash"
cat > "${colondash_probe}" <<'COLONDASH_PROBE'
set -u
e=()
sudo "${e[@]:-}" dnf
COLONDASH_PROBE

# ── GROUP A: _sudo inertness (D2) — no sudo binary required ─────────────────────────

# The load-bearing one. EMPTY HL_SUDO_OPTS must produce argv identical to bare `sudo`,
# because that is every interactive run and every NOPASSWD headless run.
plan_gather_leg "empty HL_SUDO_OPTS -> byte-identical sudo argv" \
  expect_sudo_argv '[dnf][-y][install][git]' '' dnf -y install git

# And the new path adds exactly one option, in front.
plan_gather_leg "password HL_SUDO_OPTS -> sudo -A + the same command" \
  expect_sudo_argv '[-A][dnf][-y][install][git]' '-A' dnf -y install git

# Negative control: prove the idiom _sudo deliberately avoids really would break.
plan_gather_leg "the empty-array ':-' fallback would inject an empty argument" \
  expect_colondash_would_break "${colondash_probe}"

# ── GROUP B: the credential gates — a REAL sudo binary required ─────────────────────
#
# UNKNOWN IS A REFUSAL, not a pass. Measured on the first RED run of this harness: in a
# CCY container sudo is not installed, so run.bash's probe dies with "sudo: command not
# found" and EVERY case produces byte-identical output — including the regression case,
# which "passed" because the string NOPASSWD appears in a message emitted for entirely the
# wrong reason. That is a control that FIRES without DISCRIMINATING (bash-standards §9),
# in the very thing whose job is discrimination: it would have reported green after the
# implementation landed while proving nothing about it.
#
# `bash -c` and not a bare `command -v`: `command` is a SHELL BUILTIN, so
# `env -i … command -v sudo` fails with "env: 'command': No such file or directory"
# REGARDLESS of whether sudo exists — a probe that refuses everywhere, which is the same
# fires-without-discriminating defect. Caught by reading the refusal's stated reason
# rather than its exit code.
_gate_block=""
if ! _probe="$(drop_run true 2>&1)"; then
  _gate_block="cannot drop to ${DROP_USER} here (${_probe:-unknown reason}) — the gates need an unprivileged identity"
elif ! _sudo_present="$(drop_run env -i PATH="/usr/bin:/bin:/usr/sbin:/sbin" bash -c 'command -v sudo' 2>&1)"; then
  _gate_block="sudo is not available to ${DROP_USER} here (${_sudo_present:-not found}) — every case would fail with \"sudo: command not found\" and produce IDENTICAL output, so the gates could not be told apart"
fi

if [[ -n "${_gate_block}" ]]; then
  plan_gather_leg "credential gates NOT EVALUATED" \
    refuse "${_gate_block}. Run on a host with sudo installed (Plan 00073 Task 3.3); refusing rather than reporting."
else
  # The config prefix every sudo-gate case needs, so preflight REACHES the sudo gate
  # instead of aborting earlier on a missing email/account/token (Plan 00063's gates).
  VALID_CONFIG=(
    RUN_BASH_USER_EMAIL=name@example.com
    RUN_BASH_GITHUB_ACCOUNTS=alice
    RUN_BASH_GITHUB_TOKEN_FILE="${readable_secret}"
    RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE="${readable_secret}"
    RUN_BASH_VAULT_PASSWORD_FILE="${readable_secret}"
  )

  # REGRESSION (Plan 00063 gate 10). Valid config with NEITHER credential must still fail
  # at the sudo gate. This plan changes the message (it now names both remedies) but it
  # must still say NOPASSWD, because that remains one of the two ways to satisfy the gate.
  plan_gather_leg "regression: no credential still hits the sudo gate" \
    expect_fail "NOPASSWD" "${VALID_CONFIG[@]}"

  # 1. Neither credential ⇒ the message must name the NEW remedy too. A user with password
  #    sudo reading the old message is told to do the one thing this plan exists to avoid.
  plan_gather_leg "neither credential names the password-file remedy" \
    expect_fail "RUN_BASH_SUDO_PASSWORD_FILE" "${VALID_CONFIG[@]}"

  # 2. *_FILE set but unreadable ⇒ hard fail. Must NOT fall back to "no password supplied"
  #    and then report the NOPASSWD message, which sends the user to fix the wrong thing.
  plan_gather_leg "unreadable sudo password file" \
    expect_fail "is not a readable file" "${VALID_CONFIG[@]}" \
    RUN_BASH_SUDO_PASSWORD_FILE="${unreadable_secret}"

  # 3. Both literal and file forms set ⇒ hard fail, matching the guardrail the other three
  #    secrets already carry (Plan 00063 V3.10).
  plan_gather_leg "both sudo password forms set" \
    expect_fail "Both RUN_BASH_SUDO_PASSWORD" "${VALID_CONFIG[@]}" \
    RUN_BASH_SUDO_PASSWORD=lit RUN_BASH_SUDO_PASSWORD_FILE="${readable_secret}"

  # 4. A readable password file whose password does not authenticate ⇒ fail naming SUDO'S
  #    OWN reason, not a generic message. For $DROP_USER the real reason is "not in the
  #    sudoers file" rather than a bad password — which is exactly why the message must
  #    report what sudo said instead of guessing. Asserting the bare word "sudo" would pass
  #    on almost any output, so assert the marker the implementation must emit.
  plan_gather_leg "wrong sudo password reports sudo's own reason" \
    expect_fail "sudo said:" "${VALID_CONFIG[@]}" \
    RUN_BASH_SUDO_PASSWORD_FILE="${readable_secret}"
fi

printf '\nREMINDER: green group A proves the change is inert on the pre-existing path.\n'
printf 'Green group B proves the GATES fire. NEITHER proves a correct password obtains\n'
printf 'privilege — that needs the host run (Plan 00073 Task 3.3).\n'

plan_finish
