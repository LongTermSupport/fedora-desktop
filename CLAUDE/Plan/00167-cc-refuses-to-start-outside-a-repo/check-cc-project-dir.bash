#!/usr/bin/env bash
# Plan 00167 — check-cc-project-dir.bash: one acceptance check of the INSTALLED cc-project-dir
# (the directory host cc starts its session in), run as a leg of acceptance.bash. It runs the
# binary only, never cc, so no Claude session and no tmux session is started. Its scratch
# directories are made with mktemp -d (outside every git work tree) and removed on exit.
#
#   no-stdin  outside a work tree with stdin </dev/null: exit 1, the refusal, no [Y/n] offer,
#             nothing on stdout
#   restore   outside a work tree with CCY_SESSION_RESTORE=1 and stdin a pseudo-terminal,
#             typing y: the same refusal, never the offer
#   git-top   in a subfolder of a scratch git repository: exit 0, the repository's top on
#             stdout, nothing asked
#   offer-yes outside a work tree on a pseudo-terminal, answering y: the [Y/n] offer once,
#             exit 0, and stdout exactly the checkout the offer named, which is the checkout
#             this script is in (the play bakes in the checkout it runs from)
#   offer-no  the same, answering n: the offer once, then the refusal, exit 1, empty stdout
#   offer-retry  the same, answering maybe then n: "Please answer y or n.", the offer twice,
#             then the refusal, exit 1, empty stdout
#
# The restore and offer legs make their own pseudo-terminal with script(1), so no leg needs a
# terminal from the caller. Every run is bounded by timeout, so a prompt cannot wait for ever.
# CHECK_CC_PROJECT_DIR_BIN runs every leg against a build instead of the install.
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
command -v script >/dev/null || {
    printf 'FAIL: script(1) is not installed; run playbooks/imports/play-python.yml (util-linux-script)\n' >&2
    exit 1
}

# The checkout this script is in, by PlanScriptStandards R1's bounded walk to ansible.cfg: the
# play's root_dir is the directory of the ansible.cfg it ran with, so offer-yes wants this one.
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
checkout="${scriptDir}"
while [[ "${checkout}" != "/" ]] && [[ ! -e "${checkout}/ansible.cfg" ]]; do
    if [[ -e "${checkout}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${checkout}" >&2
        exit 1
    fi
    checkout="$(dirname "${checkout}")"
done
[[ -e "${checkout}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
readonly checkout
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
    local scriptRc=0
    # script -c runs its command with $SHELL; the command below is bash.
    SHELL=/bin/bash timeout 10 script -qec "$(printf '%q' "${bin}") >$(printf '%q' "${scratch}/out") 2>$(printf '%q' "${scratch}/err"); echo \$? >$(printf '%q' "${scratch}/rc")" \
        /dev/null <"${scratch}/answers" >"${scratch}/typed" || scriptRc=$?
    if [[ "${scriptRc}" -eq 124 ]]; then
        printf 'FAIL: the run on the pseudo-terminal did not finish within 10 s\n' >&2
        exit 1
    elif [[ "${scriptRc}" -ne 0 ]]; then
        printf 'FAIL: script(1) itself failed, exit %d\n' "${scriptRc}" >&2
        exit 1
    fi
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
    # A pseudo-terminal, so only the restore flag can stop the offer; y, so a binary that
    # offered anyway would take it and exit 0.
    CCY_SESSION_RESTORE=1 on_pty 'y\n'
    refused "CCY_SESSION_RESTORE=1 at a terminal" "${ptyRc}" "${ptyOut}" "${ptyErr}"
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
    namedReal="$(cd "${named}" && pwd -P)"
    [[ "${namedReal}" == "${checkout}" ]] || {
        printf 'FAIL: the binary offers %s, not this checkout %s; re-run play-claude-yolo.yml from this checkout\n' "${named}" "${checkout}" >&2
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
