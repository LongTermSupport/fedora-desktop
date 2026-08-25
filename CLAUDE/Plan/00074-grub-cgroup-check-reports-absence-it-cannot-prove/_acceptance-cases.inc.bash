# _acceptance-cases.inc.bash — leg logic for Plan 00074's acceptance.bash.
#
# WHY THIS IS A SEPARATE FILE. `acceptance.bash` invokes these only indirectly, as
# `plan_gather_leg "<name>" expect_grub …`. ShellCheck cannot see through that indirection
# and reports every line as SC2317 ("Command appears to be unreachable"); suppression
# directives are BANNED, so the fix is structural. `.claude/rules/plan-script-standards.md`
# names this exact carve-out — "leg logic split out to avoid SC2317". Helpers get the
# line-level rules only, never the orchestrator ones.
#
# Sourced, never executed: no shell options are set here and nothing calls `exit`.

# make_grub_fixtures <dir> — write the stub `grubby` and the stub prelude into <dir>.
#
# The stub is the whole reason this plan is testable in a container. Real `grubby` is not
# installed here, and even on a real box one machine only ever exhibits ONE of the four
# states — so a stub is not a weaker substitute for a host run, it is the only thing that
# can drive the state space exhaustively.
#
# Both files are written from QUOTED heredocs: the bodies must reach bash verbatim, and a
# quoted heredoc is also the one way to write `"$@"` without ShellCheck reading it as an
# expansion that failed to happen (SC2016).
#
# The stub is a small state machine, because the block calls grubby MORE THAN ONCE and the
# second call's answer is the entire point of the removal-verify branch:
#   clean   -> --info=ALL exits 0, no legacy args.                    (working path 1)
#   legacy  -> --info=ALL shows legacy args; --update-kernel flips it
#              to `removed`, so the verify call comes back clean.     (working path 2)
#   stuck   -> --info=ALL shows legacy args; --update-kernel exits 0
#              and changes NOTHING, so the verify call still shows
#              them. This is a removal that verifiably failed.        (defect 2)
#   broken  -> --info=ALL exits 1 with a reason on stderr.            (defect 1)
make_grub_fixtures() {
    local dir="$1"
    cat > "${dir}/grubby" <<'GRUBBY_STUB'
#!/usr/bin/env bash
set -u
mode="$(cat "${GRUBBY_STUB_MODE_FILE}")"
legacy='args="ro rootflags=subvol=root rhgb quiet systemd.unified_cgroup_hierarchy=0"'
clean='args="ro rootflags=subvol=root rhgb quiet"'
case "${1:-}" in
  --info=ALL)
    case "${mode}" in
      clean|removed) printf 'index=0\nkernel="/boot/vmlinuz-6.14.0"\n%s\n' "${clean}" ;;
      legacy|stuck)  printf 'index=0\nkernel="/boot/vmlinuz-6.14.0"\n%s\n' "${legacy}" ;;
      broken)
        printf 'grubby: error: unable to open /boot/loader/entries: No such file or directory\n' >&2
        exit 1
        ;;
      *) printf 'stub grubby: unknown mode %s\n' "${mode}" >&2; exit 99 ;;
    esac
    ;;
  --update-kernel=ALL)
    # Only the `legacy` mode is actually fixable. `stuck` accepts the command, reports
    # success, and leaves the args in place — which is exactly how a real removal fails.
    if [[ "${mode}" == "legacy" ]]; then
      printf 'removed' > "${GRUBBY_STUB_MODE_FILE}"
    fi
    ;;
  *)
    printf 'stub grubby: unexpected argv: %s\n' "$*" >&2
    exit 98
    ;;
esac
GRUBBY_STUB
    chmod 0755 "${dir}/grubby"

    # The environment the extracted function expects: _sudo, the UI helpers, and the
    # colour/symbol constants. Defined so the child can run under `set -u` exactly as
    # run.bash's main() does — an unbound variable in the new code must fail here, not on
    # a user's machine.
    cat > "${dir}/prelude.bash" <<'PRELUDE'
RED='' GREEN='' YELLOW='' BLUE='' MAGENTA='' CYAN='' BOLD='' NC=''
CHECK='[ok]' CROSS='[xx]' ARROW='->' INFO='[i]' WARN='[!]' BUG='[bug]'
RUN_BASH_VERSION='test'
HEADLESS='false'
STEP_CURRENT=0
STEP_TOTAL=1
_sudo() { "$@"; }
title()   { printf '== %s\n' "$1"; }
info()    { printf 'INFO %s\n' "$1"; }
success() { printf 'OK %s\n' "$1"; }
warning() { printf 'WARN %s\n' "$1"; }
error()   { printf 'ERROR %s\n' "$1"; }
PRELUDE
}

# _extract_fn <name> — pull a top-level function's text out of run.bash.
#
# Extracted, never copied: a copy would prove only that the copy works. An awk range that
# matches nothing emits nothing and still exits 0, so the caller must check the result is
# non-empty — that check is what turns "the function is missing" into a visible failure
# instead of an empty script that trivially succeeds.
_extract_fn() {
    local name="$1"
    awk -v n="${name}" '$0 ~ "^" n "\\(\\) \\{", /^\}/' "${RUN_BASH}"
}

# expect_grub <mode> <want-rc> <must-contain> <must-not-contain> — drive the SHIPPED
# check_legacy_grub_cgroup through one state and assert all three of exit code, a message
# that must appear, and a message that must NOT.
#
# <want-rc> is `zero` or `nonzero`.
#
# The must-NOT-contain half is the one that catches this plan's actual defects, and it is
# the half a normal test would omit. Both bugs are FALSE SUCCESS: the broken state prints
# "No legacy cgroup configuration found", and the stuck state prints an error and then lets
# the run continue. An assertion that only checked for the new message would pass while the
# old, wrong message was still being printed right beside it.
expect_grub() {
    local mode="$1" want_rc="$2" want="$3" forbid="$4"
    local fatal_defn check_defn
    fatal_defn="$(_extract_fn fatal)"
    check_defn="$(_extract_fn check_legacy_grub_cgroup)"
    if [[ -z "${check_defn}" ]]; then
        printf '[FAIL] check_legacy_grub_cgroup() not found in %s\n' "${RUN_BASH}" >&2
        printf '       (the block is still inline top-level code — nothing can exercise it)\n' >&2
        return 1
    fi
    if [[ -z "${fatal_defn}" ]]; then
        printf '[FAIL] fatal() not found in %s\n' "${RUN_BASH}" >&2
        return 1
    fi

    printf '%s' "${mode}" > "${GRUB_STUB_DIR}/mode"
    local script out rc=0
    script="$(printf '%s\n' \
        'set -uo pipefail' \
        "source ${GRUB_STUB_DIR}/prelude.bash" \
        "${fatal_defn}" \
        "${check_defn}" \
        'check_legacy_grub_cgroup')"
    out="$(GRUBBY_STUB_MODE_FILE="${GRUB_STUB_DIR}/mode" PATH="${GRUB_STUB_DIR}:${PATH}" \
        bash -c "${script}" 2>&1)" || rc=$?

    local flat
    flat="$(tr '\n' '|' <<< "${out}")"
    if [[ "${want_rc}" == "zero" && "${rc}" -ne 0 ]]; then
        printf '[FAIL] mode=%s expected exit 0, got %s\n       got: %s\n' "${mode}" "${rc}" "${flat}" >&2
        return 1
    fi
    if [[ "${want_rc}" == "nonzero" && "${rc}" -eq 0 ]]; then
        printf '[FAIL] mode=%s expected NON-ZERO exit, got 0 — the run would carry on\n       got: %s\n' \
            "${mode}" "${flat}" >&2
        return 1
    fi
    if ! grep -qF -- "${want}" <<< "${out}"; then
        printf '[FAIL] mode=%s exit %s but missing: %s\n       got: %s\n' "${mode}" "${rc}" "${want}" "${flat}" >&2
        return 1
    fi
    if [[ -n "${forbid}" ]] && grep -qF -- "${forbid}" <<< "${out}"; then
        printf '[FAIL] mode=%s exit %s but STILL SAYS: %s\n       got: %s\n' "${mode}" "${rc}" "${forbid}" "${flat}" >&2
        return 1
    fi
    printf '[OK] mode=%s exit %s, says %s\n' "${mode}" "${rc}" "${want}"
    return 0
}
