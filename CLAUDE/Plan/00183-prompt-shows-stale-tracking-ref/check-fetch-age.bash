#!/usr/bin/env bash
# Plan 00183 — check-fetch-age.bash: one acceptance check of the installed git-fetch-age per
# call, against scratch repositories made with mktemp -d and removed on exit.
#
# Usage: check-fetch-age.bash <check>
#   installed   /usr/local/bin/git-fetch-age is root-owned and executable, and the deployed
#               /var/local/ps1-prompt calls it
#   marks       the mark for a fresh fetch (none), a 3-day-old one, none recorded, and an
#               empty FETCH_HEAD (a fetch that failed or is running)
#   failed      a real git fetch that cannot reach its remote empties FETCH_HEAD, and the
#               mark then says so: the prompt's own failing fetch is not read as fresh (#52)
#   prompt      the deployed prompt's PS1, expanded by bash in a stale scratch checkout,
#               shows the mark
#
# Exit 0 when the check passed, 1 when it failed (the reason on stderr).
set -euo pipefail

check="${1:?usage: check-fetch-age.bash <installed|marks|failed|prompt>}"
# CHECK_FETCH_AGE_BIN runs the checks against a build instead, before it is deployed.
readonly BIN="${CHECK_FETCH_AGE_BIN:-/usr/local/bin/git-fetch-age}"
readonly PROMPT_FILE=/var/local/ps1-prompt
readonly FAILED_MARK=' [fetch failed or running]'

scratch="$(mktemp -d)"
cleanup() { rm -rf "${scratch}"; }
trap cleanup EXIT

fail() {
    printf '[FAIL] %s\n' "$1" >&2
    exit 1
}

# repo <name> — a checkout with a remote that is never reached, so nothing here fetches.
repo() {
    local dir="${scratch}/$1"
    git init -q "${dir}"
    git -C "${dir}" remote add origin "ssh://unreachable.invalid/never.git"
    printf '%s\n' "${dir}"
}

# expect <dir> <want> — git-fetch-age's whole stdout in dir is want.
expect() {
    local got
    got="$(cd "$1" && "${BIN}")"
    if [[ "${got}" != "$2" ]]; then
        fail "in $(basename "$1"): git-fetch-age printed [${got}], want [$2]"
    fi
    printf '  ok: %s -> [%s]\n' "$(basename "$1")" "${got}"
}

check_installed() {
    [[ -x "${BIN}" ]] || fail "${BIN} is missing or not executable; play-basic-configs.yml installs it"
    [[ "$(stat -c %U "${BIN}")" == root ]] || fail "${BIN} is not root-owned"
    grep -q 'git-fetch-age' "${PROMPT_FILE}" || fail "${PROMPT_FILE} does not call git-fetch-age"
    printf '  ok: %s root-owned and executable; %s calls it\n' "${BIN}" "${PROMPT_FILE}"
}

check_marks() {
    local fresh old none empty record
    record=$'0123456789abcdef0123456789abcdef01234567\t\tbranch '"'main'"$' of example\n'
    fresh="$(repo fresh)"
    printf '%s' "${record}" >"${fresh}/.git/FETCH_HEAD"
    expect "${fresh}" ""
    old="$(repo old)"
    printf '%s' "${record}" >"${old}/.git/FETCH_HEAD"
    touch -d '3 days ago' "${old}/.git/FETCH_HEAD"
    expect "${old}" " [fetched 3d ago]"
    none="$(repo none)"
    expect "${none}" " [no fetch recorded]"
    empty="$(repo empty)"
    : >"${empty}/.git/FETCH_HEAD"
    expect "${empty}" "${FAILED_MARK}"
}

check_failed() {
    local dir rc
    dir="$(repo failed)"
    # ssh replaced by false: the remote is never reached, as with no key in the agent.
    if GIT_SSH_COMMAND=false GIT_TERMINAL_PROMPT=0 git -C "${dir}" fetch --quiet origin < /dev/null; then
        fail "a fetch through a failing ssh succeeded; the check proves nothing"
    else
        rc=$?
    fi
    [[ -e "${dir}/.git/FETCH_HEAD" ]] || fail "the failed fetch left no FETCH_HEAD; git no longer empties it first"
    [[ ! -s "${dir}/.git/FETCH_HEAD" ]] || fail "the failed fetch left a non-empty FETCH_HEAD"
    printf '  ok: the failed fetch (rc=%s) left an empty, newly dated FETCH_HEAD\n' "${rc}"
    expect "${dir}" "${FAILED_MARK}"
}

check_prompt() {
    local dir shown
    dir="$(repo prompt)"
    : >"${dir}/.git/FETCH_HEAD"
    # The prompt file is sourced in a clean bash, as the login shell does, and PS1 expanded
    # as bash expands it before drawing.
    shown="$(cd "${dir}" && PROMPT_FILE="${PROMPT_FILE}" bash --norc --noprofile -s <<'EOF'
source "${PROMPT_FILE}"
printf '%s' "${PS1@P}"
EOF
)"
    [[ "${shown}" == *"${FAILED_MARK}"* ]] || fail "the expanded prompt does not show [${FAILED_MARK}]: ${shown}"
    printf '  ok: the deployed prompt shows%s in a checkout whose fetch failed\n' "${FAILED_MARK}"
}

case "${check}" in
    installed) check_installed ;;
    marks) check_marks ;;
    failed) check_failed ;;
    prompt) check_prompt ;;
    *)
        printf '[FATAL] unknown check: %s\n' "${check}" >&2
        exit 64
        ;;
esac
