#!/usr/bin/env bash
# test-qa-gitleaks.bash — scripts/qa-gitleaks.bash fails on a planted secret (issue #329).
#
# The gate runs CI's gitleaks scan locally, over what a commit would carry. Driven over scratch
# git repositories that carry this repository's .gitleaks.toml, with a fake GitHub token built at
# run time (so no tracked file holds one):
#   - a clean tree passes, with its GITLEAKS-OK line;
#   - the token committed, in an uncommitted edit of a tracked file, or in an untracked file git
#     would add is a finding (exit 1), and the finding is redacted;
#   - the token in an ignored file (what untracked/ is) is not scanned, as CI never sees it;
#   - push mode scans each pushed commit's own tree: the token there is exit 1 whatever the work
#     tree holds, a clean commit passes, and a deleted ref scans nothing;
#   - no gitleaks on PATH, or bad usage, is exit 2, never a pass.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOL="${REPO_ROOT}/scripts/qa-gitleaks.bash"
TMPROOT="$(mktemp -d)"
trap 'rm -rf "${TMPROOT}"' EXIT
PASSED=0
FAILED=0

check() {
    if [[ "$2" == "$3" ]]; then
        printf 'PASS: %s\n' "$1"
        PASSED=$((PASSED + 1))
    else
        printf 'FAIL: %s\n  expected: %s\n  got:      %s\n' "$1" "$3" "$2"
        FAILED=$((FAILED + 1))
    fi
}

# fake_token is a GitHub personal access token in shape only: the prefix built in pieces, then
# 36 random letters and digits.
fake_token() {
    local alphabet="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789" body="" i
    for ((i = 0; i < 36; i++)); do
        body+="${alphabet:RANDOM % ${#alphabet}:1}"
    done
    printf '%s%s_%s' "gh" "p" "${body}"
}
TOKEN="$(fake_token)"

g() { git -c user.name=test -c user.email=test@example.com -c commit.gpgsign=false -c core.hooksPath=/dev/null "$@"; }

# new_repo makes a repository with this one's .gitleaks.toml, an ignored untracked/ and one
# clean commit.
new_repo() {
    local repo="$1"
    mkdir -p "${repo}"
    g -C "${repo}" init -q
    cp "${REPO_ROOT}/.gitleaks.toml" "${repo}/.gitleaks.toml"
    printf 'untracked/\n' >"${repo}/.gitignore"
    printf 'nothing secret here\n' >"${repo}/notes.txt"
    g -C "${repo}" add -A
    g -C "${repo}" commit -q -m clean
}

# scan runs the gate in a repository and prints its exit status, keeping its output in $OUT.
scan() {
    local repo="$1" rc=0
    shift
    OUT="$(cd "${repo}" && bash "${TOOL}" "$@" 2>&1)" || rc=$?
    printf '%d' "${rc}"
}

# count is how many lines of $OUT hold the text.
count() { awk -v text="$1" 'index($0, text) {n++} END {print n + 0}' <<<"${OUT}"; }

REPO="${TMPROOT}/clean"
new_repo "${REPO}"
check "a clean tree passes" "$(scan "${REPO}")" 0
check "a clean tree says so" "$(count "GITLEAKS-OK ")" 1

REPO="${TMPROOT}/committed"
new_repo "${REPO}"
printf 'token = "%s"\n' "${TOKEN}" >"${REPO}/config.txt"
g -C "${REPO}" add config.txt
g -C "${REPO}" commit -q -m leak
check "a committed token is a finding" "$(scan "${REPO}")" 1
check "the finding is redacted" "$(count "${TOKEN}")" 0

REPO="${TMPROOT}/edited"
new_repo "${REPO}"
printf 'token = "%s"\n' "${TOKEN}" >>"${REPO}/notes.txt"
check "a token in an uncommitted edit is a finding" "$(scan "${REPO}")" 1

REPO="${TMPROOT}/untracked"
new_repo "${REPO}"
printf 'token = "%s"\n' "${TOKEN}" >"${REPO}/new.txt"
check "a token in a file git would add is a finding" "$(scan "${REPO}")" 1

REPO="${TMPROOT}/ignored"
new_repo "${REPO}"
mkdir -p "${REPO}/untracked"
printf 'token = "%s"\n' "${TOKEN}" >"${REPO}/untracked/scratch.txt"
check "a token in an ignored file is not scanned" "$(scan "${REPO}")" 0

REPO="${TMPROOT}/push"
new_repo "${REPO}"
clean="$(g -C "${REPO}" rev-parse HEAD)"
printf 'token = "%s"\n' "${TOKEN}" >"${REPO}/config.txt"
g -C "${REPO}" add config.txt
g -C "${REPO}" commit -q -m leak
leak="$(g -C "${REPO}" rev-parse HEAD)"
g -C "${REPO}" rm -q config.txt
zero="0000000000000000000000000000000000000000"
check "push: a pushed commit with a token is a finding" \
    "$(scan "${REPO}" push origin url <<<"refs/heads/x ${leak} refs/heads/x ${zero}")" 1
check "push: a clean pushed commit passes" \
    "$(scan "${REPO}" push origin url <<<"refs/heads/x ${clean} refs/heads/x ${zero}")" 0
check "push: a deleted ref scans nothing" \
    "$(scan "${REPO}" push origin url <<<"(delete) ${zero} refs/heads/x ${leak}")" 0

REPO="${TMPROOT}/nogitleaks"
new_repo "${REPO}"
nopath="${TMPROOT}/bin"
mkdir -p "${nopath}"
for tool in git bash mktemp tar rm cat dirname; do
    ln -s "$(command -v "${tool}")" "${nopath}/${tool}"
done
check "no gitleaks is exit 2" "$(PATH="${nopath}" scan "${REPO}")" 2
check "bad usage is exit 2" "$(scan "${REPO}" sideways)" 2

printf '\npassed: %d   failed: %d\n' "${PASSED}" "${FAILED}"
[[ "${FAILED}" -eq 0 ]]
