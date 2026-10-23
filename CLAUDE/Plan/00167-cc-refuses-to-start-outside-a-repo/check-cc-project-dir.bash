#!/usr/bin/env bash
# Plan 00167 — check-cc-project-dir.bash: one acceptance check of the INSTALLED cc-project-dir
# (the directory host cc starts its session in), run as a leg of acceptance.bash. It runs the
# binary only, never cc, so no Claude session and no tmux session is started. Its scratch
# directories are made with mktemp -d (outside every git work tree) and removed on exit.
#
#   no-stdin  outside a work tree with stdin </dev/null: exit 1, the refusal, no [Y/n] offer,
#             nothing on stdout
#   restore   outside a work tree with CCY_SESSION_RESTORE=1 and stdin the terminal: the same
#             refusal, never the offer (bounded by timeout, so a prompt cannot wait for ever)
#   git-top   in a subfolder of a scratch git repository: exit 0, the repository's top on
#             stdout, nothing asked
#   offer-yes outside a work tree on a pseudo-terminal, answering y: the [Y/n] offer once,
#             exit 0, and stdout exactly the checkout the offer named, a fedora-desktop
#             checkout (ansible.cfg at its git top)
#   offer-no  the same, answering n: the offer once, then the refusal, exit 1, empty stdout
#   offer-retry  the same, answering maybe then n: "Please answer y or n.", the offer twice,
#             then the refusal, exit 1, empty stdout
#
# The offer legs make their own pseudo-terminal with script(1), so they need no terminal from
# the caller. CHECK_CC_PROJECT_DIR_BIN runs every leg against a build instead of the install.
#
# Usage: check-cc-project-dir.bash no-stdin|restore|git-top|offer-yes|offer-no|offer-retry
set -euo pipefail

[[ $# -eq 1 ]] || {
    printf 'usage: check-cc-project-dir.bash no-stdin|restore|git-top|offer-yes|offer-no|offer-retry\n' >&2
    exit 64
}
check="$1"
bin="${CHECK_CC_PROJECT_DIR_BIN:-/var/local/claude-code/cc-project-dir}"
readonly OFFER="[Y/n]"

[[ -x "${bin}" ]] || {
    printf 'FAIL: %s is not installed; run playbooks/imports/play-claude-yolo.yml\n' "${bin}" >&2
    exit 1
}
scratch="$(mktemp -d)"
trap 'rm -rf -- "${scratch}"' EXIT
cd "${scratch}"

# refused NAME RC OUT ERR: the run was the refusal, with no offer and nothing on stdout.
refused() {
    printf '%s: exit %d, stdout %q, stderr: %s\n' "$1" "$2" "$3" "$4"
    [[ "$2" -eq 1 && -z "$3" && "$4" == *"is not in a git repository"* && "$4" != *"${OFFER}"* ]] || {
        printf 'FAIL: %s: want exit 1, the refusal, no %s offer and an empty stdout\n' "$1" "${OFFER}" >&2
        exit 1
    }
}

# on_pty ANSWERS: run the binary in the scratch directory with a pseudo-terminal as its stdin
# (so it offers), typing ANSWERS (printf %b), its stdout and stderr kept apart. Sets ptyRc,
# ptyOut, ptyErr and offers (how many times the offer was printed).
on_pty() {
    printf '%b' "$1" >"${scratch}/answers"
    rm -f -- "${scratch}/rc"
    timeout 10 script -qec "$(printf '%q' "${bin}") >$(printf '%q' "${scratch}/out") 2>$(printf '%q' "${scratch}/err"); echo \$? >$(printf '%q' "${scratch}/rc")" \
        /dev/null <"${scratch}/answers" >"${scratch}/typed" || {
        printf 'FAIL: script(1) did not finish the run within 10 s\n' >&2
        exit 1
    }
    [[ -s "${scratch}/rc" ]] || {
        printf 'FAIL: the binary never finished on the pseudo-terminal (no exit status)\n' >&2
        exit 1
    }
    ptyRc="$(<"${scratch}/rc")"
    ptyOut="$(<"${scratch}/out")"
    ptyErr="$(<"${scratch}/err")"
    offers=0
    local rest="${ptyErr}"
    while [[ "${rest}" == *"${OFFER}"* ]]; do
        rest="${rest#*"${OFFER}"}"
        offers=$((offers + 1))
    done
    printf 'answers %q: exit %s, stdout %q, offers %d, stderr:\n%s\n' "$1" "${ptyRc}" "${ptyOut}" "${offers}" "${ptyErr}"
}

# declined NAME WANT_OFFERS: the pty run offered WANT_OFFERS times, then refused.
declined() {
    [[ "${ptyRc}" -eq 1 && -z "${ptyOut}" && "${offers}" -eq "$2" && "${ptyErr}" == *"cc: not starting: "*"is not in a git repository"* ]] || {
        printf 'FAIL: %s: want the offer %d time(s), then exit 1, the refusal and an empty stdout\n' "$1" "$2" >&2
        exit 1
    }
}

rc=0
case "${check}" in
no-stdin)
    out="$(timeout 10 "${bin}" </dev/null 2>"${scratch}/err")" || rc=$?
    refused "stdin </dev/null" "${rc}" "${out}" "$(<"${scratch}/err")"
    ;;
restore)
    (exec </dev/tty) || {
        printf 'FAIL: no terminal to give as stdin, so the restore check cannot tell the restore flag from no terminal; run it in a terminal\n' >&2
        exit 1
    }
    out="$(CCY_SESSION_RESTORE=1 timeout 10 "${bin}" </dev/tty 2>"${scratch}/err")" || rc=$?
    refused "CCY_SESSION_RESTORE=1 at a terminal" "${rc}" "${out}" "$(<"${scratch}/err")"
    ;;
git-top)
    git init --quiet "${scratch}/repo"
    mkdir -p "${scratch}/repo/sub/deeper"
    top="$(cd "${scratch}/repo" && pwd -P)"
    cd "${scratch}/repo/sub/deeper"
    out="$(timeout 10 "${bin}" </dev/null 2>"${scratch}/err")" || rc=$?
    errText="$(<"${scratch}/err")"
    printf 'from sub/deeper: exit %d, stdout %q, stderr %q\n' "${rc}" "${out}" "${errText}"
    [[ "${rc}" -eq 0 && "${out}" == "${top}" && -z "${errText}" ]] || {
        printf 'FAIL: want exit 0, %s on stdout and nothing on stderr\n' "${top}" >&2
        exit 1
    }
    ;;
offer-yes)
    on_pty 'y\n'
    named=""
    if [[ "${ptyErr}" =~ fedora-desktop\ checkout,\ ([^$'\n']*),\ instead\? ]]; then
        named="${BASH_REMATCH[1]}"
    fi
    [[ "${ptyRc}" -eq 0 && "${offers}" -eq 1 && -n "${named}" && "${ptyOut}" == "${named}" ]] || {
        printf 'FAIL: want the offer once, exit 0, and stdout exactly the checkout it named [%s]\n' "${named}" >&2
        exit 1
    }
    gitTop="$(git -C "${named}" rev-parse --show-toplevel)"
    [[ "${gitTop}" == "${named}" && -e "${named}/ansible.cfg" ]] || {
        printf 'FAIL: %s is not a fedora-desktop checkout top (git top %s, ansible.cfg there?)\n' "${named}" "${gitTop}" >&2
        exit 1
    }
    ;;
offer-no)
    on_pty 'n\n'
    declined "answering n" 1
    ;;
offer-retry)
    on_pty 'maybe\nn\n'
    declined "answering maybe, then n" 2
    [[ "${ptyErr}" == *"Please answer y or n."* ]] || {
        printf 'FAIL: answering maybe: want "Please answer y or n." before the offer again\n' >&2
        exit 1
    }
    ;;
*)
    printf 'unknown check %s\n' "${check}" >&2
    exit 64
    ;;
esac
