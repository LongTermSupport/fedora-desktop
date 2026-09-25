#!/usr/bin/env bash
# Plan 00139 — acceptance.bash
#
# PURPOSE: render the VERDICT on what deploy.bash left behind (CLAUDE/PlanScriptStandards.md
# R9): git signs every commit and tag with this machine's login key, through the ssh-agent
# that holds it (D5), the opt-in settings Plan 00137 wrote are gone, a real commit and tag
# made with the user's own config verify as good, GitHub knows every login key as a
# signing key, and the passphrase-free signing keys D5 retired are gone from disk and from
# GitHub. HOST ONLY.
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

readonly DECLARED=14
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
if [[ -f "${key}.pub" ]]; then
    pub_fields="$(awk '{ print $1, $2 }' "${key}.pub")"
else
    pub_fields="no ${key}.pub"
fi
# git signs through the agent, so the agent must hold every key git signs with: this one,
# and each account's github_<alias> (accounts.json, as checks 12-14 read it). Checks 9 and
# 10 then sign with the machine key for real, which also proves the .pub is its public
# half. ssh-add -L exits 1 when the agent holds nothing or is locked, 2 when there is none.
agent_keys="$(ssh-add -L 2>&1)" && rc=0 || rc=$?
signing_keys=("${key}")
accounts_read=""
if account_aliases="$(jq -r 'keys[]' "${HOME}/.config/git-account-helper/accounts.json" 2>&1)"; then
    while read -r alias; do
        if [[ -n "${alias}" ]]; then
            signing_keys+=("${HOME}/.ssh/github_${alias}")
        fi
    done <<<"${account_aliases}"
else
    accounts_read="could not read accounts.json: ${account_aliases}; "
fi
agent_fields=""
if [[ "${rc}" -eq 0 ]]; then
    agent_fields="$(awk '{ print $1, $2 }' <<<"${agent_keys}")"
fi
missing="${accounts_read}"
held=0
for signing_key in "${signing_keys[@]}"; do
    if [[ ! -f "${signing_key}.pub" ]]; then
        missing+="$(basename "${signing_key}") (no .pub); "
    elif grep -qxF "$(awk '{ print $1, $2 }' "${signing_key}.pub")" <<<"${agent_fields}"; then
        held=$((held + 1))
    else
        missing+="$(basename "${signing_key}"); "
    fi
done
label="6. the ssh-agent holds every key git signs with (${held} of ${#signing_keys[@]}), so a commit needs no passphrase"
if [[ -z "${missing}" ]]; then
    ok "${label}"
else
    case "${rc}" in
        0) why="it does not hold: ${missing}load each with: ssh-add <key>" ;;
        1) why="it holds no keys, or is locked (ssh-add -L exit 1); missing: ${missing}" ;;
        *) why="no agent answered (ssh-add -L exit ${rc}: $(tr '\n' ' ' <<<"${agent_keys}"))" ;;
    esac
    bad "${label}" "${why}"
fi
# D5 retired these; play-github-cli-multi.yml deletes them after the login keys are in use.
# A ccy session started before CCY 3.70.0 holds a staged copy in /tmp/claude-yolo-* while it
# runs; the play refuses to retire while one does, so none should be left either.
left_on_disk="$(find "${HOME}/.ssh" -maxdepth 1 \( -name 'github_*_signing*' -o -name 'id_ed25519_git_signing*' \) -printf '%f ' 2>&1)"
shopt -s nullglob
staged=(/tmp/claude-yolo-*/git-signing-key)
shopt -u nullglob
if [[ "${#staged[@]}" -gt 0 ]]; then
    left_on_disk+="staged by a running pre-3.70 ccy session: ${staged[*]}"
fi
verdict "7. no retired passphrase-free signing key is left in ~/.ssh or staged by a ccy session" "" "${left_on_disk}"

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
# GitHub marks a commit the machine key signs Verified only on the account that has the
# commit's email, user.email, as a verified address, so that is the account the key must
# be on. Each account's verified emails are read with its own token, which carries every
# scope in vars/github-required-scopes.yml. A noreply address belongs to the login it
# names, provided its <id>+ prefix, when it has one, is that account's id: GitHub
# attributes the address by the id, so a wrong one is Verified on no account. The signing
# keys are read from the public users/<login>/ssh_signing_keys endpoint.
# When an account could not be read, the answer is "unknown", not "not verified".
commit_email="$(global_get user.email)"
owner=""
read_failed=""
id_mismatch=""
logins="$(gh auth status --json hosts --jq '.hosts["github.com"][].login' 2>&1)" && rc=0 || rc=$?
if [[ "${rc}" -ne 0 ]]; then
    bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
        "could not ask gh which accounts it holds: gh auth status exit ${rc}: ${logins}"
else
    shopt -s nocasematch
    for login in ${logins}; do
        if [[ "${commit_email}" =~ ^(([0-9]+)\+)?${login}@users\.noreply\.github\.com$ ]]; then
            noreply_id="${BASH_REMATCH[2]}"
            if [[ -z "${noreply_id}" ]]; then
                owner="${login}"
            elif ! account_id="$(gh api "users/${login}" --jq '.id' 2>&1)"; then
                read_failed+="${login} (users/${login}: $(tr '\n' ' ' <<<"${account_id}")); "
            elif [[ "${account_id}" == "${noreply_id}" ]]; then
                owner="${login}"
            else
                id_mismatch="${commit_email} carries id ${noreply_id}, but ${login}'s id is ${account_id}"
            fi
            continue
        fi
        if ! token="$(gh auth token --hostname github.com --user "${login}" 2>&1)"; then
            read_failed+="${login} (no token: $(tr '\n' ' ' <<<"${token}")); "
            continue
        fi
        emails="$(GH_TOKEN="${token}" gh api user/emails --jq '.[] | select(.verified) | .email' 2>&1)" && rc=0 || rc=$?
        if [[ "${rc}" -ne 0 ]]; then
            read_failed+="${login} (user/emails exit ${rc}: $(tr '\n' ' ' <<<"${emails}")); "
            continue
        fi
        while read -r email; do
            if [[ -n "${email}" && "${email}" == "${commit_email}" ]]; then
                owner="${login}"
            fi
        done <<<"${emails}"
    done
    shopt -u nocasematch
    if [[ -z "${owner}" && -n "${id_mismatch}" ]]; then
        bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
            "${id_mismatch}, so GitHub verifies commits with that email on no account; fix user.email"
    elif [[ -z "${owner}" && -n "${read_failed}" ]]; then
        bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
            "UNKNOWN: could not read ${read_failed}and no readable account has ${commit_email} verified"
    elif [[ -z "${owner}" ]]; then
        bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
            "no account gh holds has ${commit_email} verified"
    elif ! listed="$(gh api "users/${owner}/ssh_signing_keys" --jq '.[].key' 2>&1)"; then
        bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
            "UNKNOWN: could not read ${owner}'s signing keys: $(tr '\n' ' ' <<<"${listed}")"
    elif grep -qxF "${pub_fields}" <<<"$(awk '{ print $1, $2 }' <<<"${listed}")"; then
        ok "11. the machine key is a signing key on the account with ${commit_email} verified (${owner})"
    else
        bad "11. the machine key is a signing key on the account with ${commit_email} verified" \
            "not on ${owner}; the play-github-cli-multi.yml leg registers it"
    fi
fi

echo "== each GitHub account's own key"
# play-github-cli-multi.yml writes github_accounts (alias to login) to accounts.json. Each
# account signs with its login key, github_<alias>. The registration read uses the same
# public endpoint as check 11. The pick is read in a scratch repository whose remote is
# the account's github.com-<alias> host. Check 14 reads each account's own signing keys
# with its token, for their titles: the retired keys' files are gone, so the titles the
# play gave them are what is left to recognise them by.
accounts_file="${HOME}/.config/git-account-helper/accounts.json"
# The play titles each registration "<hostname> <key name>", with Ansible's hostname fact:
# the first label of the node name. Only this machine's are this deploy's to retire; another
# machine's go when that machine is deployed.
this_host="$(uname -n)"
this_host="${this_host%%.*}"
if ! accounts="$(jq -r 'to_entries[] | "\(.key) \(.value)"' "${accounts_file}" 2>&1)"; then
    bad "12. every account's own key is a signing key on that account" "could not read ${accounts_file}: ${accounts}"
    bad "13. git signs with each account's key in that account's repositories" "could not read ${accounts_file}: ${accounts}"
    bad "14. no account still has a retired signing key registered" "could not read ${accounts_file}: ${accounts}"
else
    unregistered=""
    wrong_pick=""
    still_registered=""
    checked=0
    pick_repo="${PLAN_RUN_DIR}/pick-repo"
    git init -q "${pick_repo}"
    git -C "${pick_repo}" remote add origin git@github.com:example/example.git
    while read -r -u 3 alias login; do
        if [[ -z "${alias}" ]]; then
            continue
        fi
        checked=$((checked + 1))
        account_key="${HOME}/.ssh/github_${alias}"
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
        if ! token="$(gh auth token --hostname github.com --user "${login}" 2>&1)"; then
            still_registered+="${alias} (UNKNOWN: no token); "
        elif ! titles="$(GH_TOKEN="${token}" gh api --paginate user/ssh_signing_keys --jq '.[].title' 2>&1)"; then
            still_registered+="${alias} (UNKNOWN: user/ssh_signing_keys: $(tr '\n' ' ' <<<"${titles}")); "
        elif retired="$(grep -E "^${this_host} (github_.+_signing|id_ed25519_git_signing)\$" <<<"${titles}")"; then
            still_registered+="${alias}: $(tr '\n' ',' <<<"${retired}") "
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
        bad "14. no account still has a retired signing key registered" "${accounts_file} names no account"
    else
        verdict "12. every account's own key is a signing key on that account (${checked} checked)" "" "${unregistered}"
        verdict "13. git signs with each account's key in that account's repositories" "" "${wrong_pick}"
        verdict "14. no account still has a retired signing key of this machine (${this_host}) registered" "" "${still_registered}"
    fi
fi

echo
ran=$((PASS + FAIL))
echo "COVERAGE: ${ran} of ${DECLARED} checks executed"
echo "NOT ESTABLISHABLE here — for the owner:"
echo "  - in a ccy session started AFTER the deploy, make a commit in a scratch repo and"
echo "    check that 'git cat-file commit HEAD' carries a gpgsig header. The ccy-git-signing"
echo "    QA gate proves the launcher names the session's key; only a real container proves"
echo "    git there signs with it through the container's agent."
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
