#!/usr/bin/env bash
# Plan 00139 — deploy.bash
#
# PURPOSE: turn on commit signing everywhere on this machine. HOST ONLY
# (CLAUDE/PlanScriptStandards.md R2) — Ansible never runs in the CCY container.
#
# THE LEGS, IN ORDER:
#   0. scripts/gh-account-setup.bash --setup-all — every GitHub account gets every scope in
#      vars/github-required-scopes.yml, the signing-key scope included, or leg 3's token
#      audit stops the deploy. Each account that lacks any is asked for all of them in one
#      browser authorisation; an account that has them all is left as it is. First, so the
#      only interactive step comes before anything changes, and a run at a desk needs no
#      second pass. A --check preview runs its read-only --check instead.
#   1. play-claude-yolo.yml — the ccy launcher (CCY 3.70.1 or later) whose containers sign
#      through the session's ssh-agent with the session's key. First, because a launcher
#      older than 3.70.0 cannot sign with a passphrase-protected login key, which is what
#      leg 2 switches to. Until leg 2 has run, a launch that has only a forwarded agent,
#      or --no-ssh, is refused: the host still names the passphrase-free key, which the
#      agent does not hold. A launch with a key file signs with that key throughout.
#   2. play-git-configure-and-tools.yml — signs with the login key ~/.ssh/id (or the key
#      git_signing_key names) through the ssh-agent, and sets gpg.format, user.signingkey,
#      commit.gpgsign and tag.gpgsign in ~/.gitconfig. It generates no key: it refuses
#      unless the key is a private key there. It removes the opt-in settings Plan 00137
#      wrote to the XDG config. Second, because from here on ~/.gitconfig asks for
#      signing, and an older launcher would start containers whose every commit fails. The
#      deploy stops at the first failing leg, so this order never leaves that state behind.
#   3. play-github-cli-multi.yml — each GitHub account's repositories sign with that
#      account's login key (~/.ssh/github_<alias>), and every key GitHub lacks as a
#      signing key is registered, ~/.ssh/id included. It then deletes the passphrase-free
#      signing keys earlier deploys made, from GitHub and from ~/.ssh. Last, because it
#      registers the key leg 2 signs with.
#
# A ccy session started before CCY 3.70.0 signs with a staged copy of a passphrase-free
# key (/tmp/claude-yolo-*/git-signing-key), and leg 3 deletes that key from GitHub, which
# would leave the session's later pushes Unverified. So the deploy refuses to start while
# any such session runs; stop them first. play-github-cli-multi.yml refuses the same.
#
# Usage: ./deploy.bash [-h|--help] [--check]
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

PLAN_USAGE="usage: deploy.bash [-h|--help] [--check]

Runs, on the HOST, in this order:

  scripts/gh-account-setup.bash --setup-all            (every account's scopes; a browser
                                                        authorisation per account lacking any)
  playbooks/imports/play-claude-yolo.yml               (ccy signs through the agent)
  playbooks/imports/play-git-configure-and-tools.yml   (sign everything with ~/.ssh/id)
  playbooks/imports/play-github-cli-multi.yml          (each account signs with its login
                                                        key; the old signing keys deleted)

The launcher goes first: on its own it changes nothing, while signing switched on under
an older launcher would start containers that cannot commit.

--check previews without changing anything; the account step runs its read-only --check.

Run it at a desk: an account lacking a scope needs its browser authorisation. Stop every
ccy session started before CCY 3.70.0 first; it refuses while one runs. Then run
acceptance.bash."

plan_mode deploy
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it runs Ansible against this machine's git config and ccy launcher"

# Refused before anything changes, rather than at leg 3 after two legs have run.
shopt -s nullglob
old_sessions=(/tmp/claude-yolo-*/git-signing-key)
shopt -u nullglob
if [[ "${#old_sessions[@]}" -gt 0 ]]; then
    printf '[FATAL] ccy sessions started before CCY 3.70.0 are still running; each signs with\n' >&2
    printf '        a passphrase-free key this deploy deletes from GitHub:\n' >&2
    printf '          %s\n' "${old_sessions[@]}" >&2
    printf '        Stop those sessions, then run this again. A directory left by a session\n' >&2
    printf '        that is no longer running goes at the next reboot, or can be removed.\n' >&2
    exit 1
fi
plan_prime_sudo
plan_start_log auto

if [[ "${PLAN_CHECK}" == "1" ]]; then
    gh_setup_mode="--check"
else
    gh_setup_mode="--setup-all"
fi
plan_deploy_leg "gh-account-setup.bash ${gh_setup_mode}" \
    bash "${repoRoot}/scripts/gh-account-setup.bash" "${gh_setup_mode}"

plan_deploy_leg "play-claude-yolo.yml" \
    plan_ansible_playbook playbooks/imports/play-claude-yolo.yml

plan_deploy_leg "play-git-configure-and-tools.yml" \
    plan_ansible_playbook playbooks/imports/play-git-configure-and-tools.yml

plan_deploy_leg "play-github-cli-multi.yml" \
    plan_ansible_playbook playbooks/imports/play-github-cli-multi.yml

printf '\n==> NEXT:\n'
printf '    1. ./acceptance.bash\n'
if [[ "${PLAN_CHECK}" != "1" ]]; then
    signer="$(git -C "${repoRoot}" config --get user.signingkey)"
    printf '    2. A self-update server (Plan 00137) verifies commits to this checkout, which now\n'
    printf '       sign with %s. Set the server'"'"'s self_update_signing_public_key to the\n' "${signer}"
    printf '       contents of %s.pub, then run the server'"'"'s deploy.\n' "${signer}"
fi
printf '\n'

plan_finish
