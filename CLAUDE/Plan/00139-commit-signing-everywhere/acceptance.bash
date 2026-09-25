#!/usr/bin/env bash
# Plan 00139 — acceptance.bash
#
# PURPOSE: render the VERDICT on what deploy.bash left behind (CLAUDE/PlanScriptStandards.md
# R9): this machine's signing key needs no passphrase, git signs every commit and tag with
# it, the opt-in settings Plan 00137 wrote are gone, a real commit and tag made with the
# user's own config verify as good, and GitHub knows the key. HOST ONLY.
#
# It changes nothing outside its own run directory: the commit and tag are made in a
# scratch repository there, which is deleted at the end.
#
# NOT ESTABLISHABLE by a script, named for the human at the end: a commit made inside a
# ccy container started after the deploy, and a pushed commit shown as Verified.
#
# Usage: ./acceptance.bash [-h|--help]
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [-h|--help]

The Plan 00139 acceptance gate. Run deploy.bash first. It checks the signing key,
the global git config, that the old XDG settings are gone, a commit and a tag made in
a scratch repository with your own config, that GitHub lists the key, and that each
GitHub account's own key is registered on that account and picked in its repositories.

EXIT STATUS
  0  ACCEPTED — every declared check ran and passed
  1  REJECTED — a check failed, or a declared check never ran
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks this user's git config and signing key"
plan_start_log auto

readonly DECLARED=13
readonly XDG_CONFIG="${XDG_CONFIG_HOME:-${HOME}/.config}/git/config"
PASS=0
FAIL=0

ok() {
    PASS=$((PASS + 1))
    printf '✓ %s\n' "$1"
}
bad() {
    FAIL=$((FAIL + 1))
    printf '✗ %s\n' "$1"
    if [[ -n "${2:-}" ]]; then
        printf '    %s\n' "$2"
    fi
}
verdict() {
    if [[ "$2" == "$3" ]]; then
        ok "$1"
    else
        bad "$1" "want: $2 | got: $3"
    fi
}
# global_get <name> [--type=bool] — the value, or "unset". An unset key is an answer.
global_get() {
    local value rc
    value=$(git config --global "${@:2}" --get "$1") && rc=0 || rc=$?
    if [[ "${rc}" -eq 0 ]]; then
        printf '%s' "${value}"
    elif [[ "${rc}" -eq 1 ]]; then
        printf 'unset'
    else
        printf 'unreadable (git config exit %s)' "${rc}"
    fi
}

echo "== the global git config"
key="$(global_get user.signingkey)"
verdict "1. gpg.format is ssh" "ssh" "$(global_get gpg.format)"
verdict "2. commit.gpgsign is on" "true" "$(global_get commit.gpgsign --type=bool)"
verdict "3. tag.gpgsign is on" "true" "$(global_get tag.gpgsign --type=bool)"
case "${key}" in
    /*) ok "4. user.signingkey names a key file: ${key}" ;;
    *) bad "4. user.signingkey names a key file" "got: ${key}" ;;
esac

echo "== the key"
if [[ -f "${key}" && ! -L "${key}" ]] && [[ "$(stat -c '%a %U' "${key}")" == "600 ${USER}" ]] && [[ -f "${key}.pub" ]]; then
    ok "5. the key is a 0600 file owned by ${USER}, with its .pub beside it"
else
    bad "5. the key is a 0600 file owned by ${USER}, with its .pub beside it" \
        "$(stat -c '%a %U %F' "${key}" "${key}.pub" 2>&1 | tr '\n' ';')"
fi
derived="$(ssh-keygen -y -P "" -f "${key}" 2>&1)" && rc=0 || rc=$?
if [[ "${rc}" -eq 0 ]]; then
    ok "6. the key loads with no passphrase, so agents can sign"
else
    bad "6. the key loads with no passphrase, so agents can sign" "ssh-keygen -y exit ${rc}"
fi
if [[ -f "${key}.pub" ]]; then
    pub_fields="$(awk '{ print $1, $2 }' "${key}.pub")"
else
    pub_fields="no ${key}.pub"
fi
verdict "7. the .pub is the public half of this key" "$(awk '{ print $1, $2 }' <<<"${derived}")" "${pub_fields}"

echo "== the opt-in settings are gone"
# `git config --get` exits 1 for "not set" and only for that. Any other status is a file
# git could not read, which says nothing about whether the settings are gone.
left=""
for name in gpg.format user.signingkey alias.sign-deploy; do
    if [[ ! -f "${XDG_CONFIG}" ]]; then
        break
    fi
    git config --file "${XDG_CONFIG}" --get "${name}" >/dev/null && rc=0 || rc=$?
    case "${rc}" in
        0) left+="${name} " ;;
        1) ;;
        *) left+="${name}(unreadable: git config exit ${rc}) " ;;
    esac
done
verdict "8. ${XDG_CONFIG} holds none of gpg.format, user.signingkey, alias.sign-deploy" "" "${left}"

echo "== a real commit and tag, made with your own config"
scratch="${PLAN_RUN_DIR}/scratch-repo"
signers="${PLAN_RUN_DIR}/allowed_signers"
printf '%s namespaces="git" %s\n' "$(global_get user.email)" "${pub_fields}" >"${signers}"
git init -q "${scratch}"
# A commit or tag that cannot be signed fails outright, which is itself the finding.
if out="$(git -C "${scratch}" commit -q --allow-empty --no-verify -m "plan 00139 acceptance" 2>&1)" &&
    out="$(git -C "${scratch}" -c gpg.ssh.allowedSignersFile="${signers}" verify-commit HEAD 2>&1)"; then
    ok "9. a commit is signed, and verifies against this key"
else
    bad "9. a commit is signed, and verifies against this key" "$(tr '\n' ' ' <<<"${out}")"
fi
if out="$(git -C "${scratch}" tag -m "plan 00139 acceptance" acceptance 2>&1)" &&
    out="$(git -C "${scratch}" -c gpg.ssh.allowedSignersFile="${signers}" verify-tag acceptance 2>&1)"; then
    ok "10. a tag is signed, and verifies against this key"
else
    bad "10. a tag is signed, and verifies against this key" "$(tr '\n' ' ' <<<"${out}")"
fi
rm -rf "${scratch}"

echo "== GitHub"
# The public users/<login>/ssh_signing_keys endpoint needs no token scope, so this reads
# every account gh is logged in to without asking for one.
#
# What it proves: the key is registered as a SIGNING key on at least one of those accounts.
# What it cannot: that GitHub will mark a commit Verified. That also needs the committer
# email to be a verified email of the account holding the key, and reading an account's
# emails needs a scope this gate does not ask for. The push check at the end is the owner's.
# When no account could be read at all, the answer is "unknown", not "unregistered".
found=""
read_ok=0
read_failed=""
logins="$(gh auth status --json hosts --jq '.hosts["github.com"][].login' 2>&1)" && rc=0 || rc=$?
if [[ "${rc}" -ne 0 ]]; then
    bad "11. GitHub lists this key as a signing key" "could not ask gh which accounts it holds: gh auth status exit ${rc}: ${logins}"
else
    for login in ${logins}; do
        listed="$(gh api "users/${login}/ssh_signing_keys" --jq '.[].key' 2>&1)" && rc=0 || rc=$?
        if [[ "${rc}" -ne 0 ]]; then
            read_failed+="${login} (gh api exit ${rc}: $(tr '\n' ' ' <<<"${listed}")); "
            continue
        fi
        read_ok=$((read_ok + 1))
        if grep -qxF "${pub_fields}" <<<"$(awk '{ print $1, $2 }' <<<"${listed}")"; then
            found="${login}"
        fi
    done
    if [[ -n "${found}" ]]; then
        ok "11. GitHub lists this key as a signing key (account ${found})"
    elif [[ "${read_ok}" -eq 0 ]]; then
        bad "11. GitHub lists this key as a signing key" \
            "UNKNOWN: no account's signing keys could be read, so registration was not checked: ${read_failed:-gh reported no github.com account}"
    else
        bad "11. GitHub lists this key as a signing key" \
            "not on any of the ${read_ok} account(s) read; see docs/configuration.md \"Commit Signing\"${read_failed:+ (unreadable: ${read_failed})}"
    fi
fi

echo "== each GitHub account's own key"
# play-github-cli-multi.yml writes github_accounts (alias to login) to accounts.json.
# The registration read uses the same public endpoint as check 11. The pick is read in a
# scratch repository whose remote is the account's github.com-<alias> host.
accounts_file="${HOME}/.config/git-account-helper/accounts.json"
if ! accounts="$(jq -r 'to_entries[] | "\(.key) \(.value)"' "${accounts_file}" 2>&1)"; then
    bad "12. every account's own key is a signing key on that account" "could not read ${accounts_file}: ${accounts}"
    bad "13. git signs with each account's key in that account's repositories" "could not read ${accounts_file}: ${accounts}"
else
    unregistered=""
    wrong_pick=""
    checked=0
    pick_repo="${PLAN_RUN_DIR}/pick-repo"
    git init -q "${pick_repo}"
    git -C "${pick_repo}" remote add origin git@github.com:example/example.git
    while read -r -u 3 alias login; do
        if [[ -z "${alias}" ]]; then
            continue
        fi
        checked=$((checked + 1))
        account_key="${HOME}/.ssh/github_${alias}_signing"
        if [[ -f "${account_key}.pub" ]]; then
            account_fields="$(awk '{ print $1, $2 }' "${account_key}.pub")"
        else
            account_fields="no ${account_key}.pub"
        fi
        listed="$(gh api "users/${login}/ssh_signing_keys" --jq '.[].key' 2>&1)" && rc=0 || rc=$?
        if [[ "${rc}" -ne 0 ]]; then
            unregistered+="${alias} (unreadable: gh api exit ${rc}); "
        elif ! grep -qxF "${account_fields}" <<<"$(awk '{ print $1, $2 }' <<<"${listed}")"; then
            unregistered+="${alias} (not on ${login}); "
        fi
        git -C "${pick_repo}" remote set-url origin "git@github.com-${alias}:example/example.git"
        picked="$(git -C "${pick_repo}" config --get user.signingkey 2>&1)" || picked="unset"
        if [[ "${picked}" != "${account_key}" ]]; then
            wrong_pick+="${alias} signs with ${picked}; "
        fi
    done 3<<<"${accounts}"
    rm -rf "${pick_repo}"
    if [[ "${checked}" -eq 0 ]]; then
        bad "12. every account's own key is a signing key on that account" "${accounts_file} names no account"
        bad "13. git signs with each account's key in that account's repositories" "${accounts_file} names no account"
    else
        verdict "12. every account's own key is a signing key on that account (${checked} checked)" "" "${unregistered}"
        verdict "13. git signs with each account's key in that account's repositories" "" "${wrong_pick}"
    fi
fi

echo
ran=$((PASS + FAIL))
echo "COVERAGE: ${ran} of ${DECLARED} checks executed"
echo "NOT ESTABLISHABLE here — for the owner:"
echo "  - in a ccy session started AFTER the deploy, make a commit in a scratch repo and"
echo "    check that 'git cat-file commit HEAD' carries a gpgsig header. The ccy-git-signing"
echo "    QA gate proves the launcher stages the key; only a real container proves git there"
echo "    signs with it."
echo "  - push a commit and see GitHub mark it Verified. That also needs the committer email"
echo "    to be a verified email of the account holding the key."
if [[ "${ran}" -ne "${DECLARED}" ]]; then
    echo "VERDICT: REJECTED — ${ran} of ${DECLARED} declared checks ran; an incomplete gate establishes nothing."
    PLAN_FAILED_LEGS="coverage"
elif [[ "${FAIL}" -ne 0 ]]; then
    echo "VERDICT: REJECTED — ${FAIL} of ${ran} checks failed."
    PLAN_FAILED_LEGS="acceptance"
else
    echo "VERDICT: ACCEPTED — ${PASS} of ${DECLARED} checks passed."
fi
plan_finish
