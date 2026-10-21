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
#
# Usage: check-cc-project-dir.bash no-stdin|restore|git-top
set -euo pipefail

[[ $# -eq 1 ]] || {
    printf 'usage: check-cc-project-dir.bash no-stdin|restore|git-top\n' >&2
    exit 64
}
check="$1"
bin=/var/local/claude-code/cc-project-dir

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
    [[ "$2" -eq 1 && -z "$3" && "$4" == *"is not in a git repository"* && "$4" != *"[Y/n]"* ]] || {
        printf 'FAIL: %s: want exit 1, the refusal, no [Y/n] offer and an empty stdout\n' "$1" >&2
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
*)
    printf 'unknown check %s\n' "${check}" >&2
    exit 64
    ;;
esac
