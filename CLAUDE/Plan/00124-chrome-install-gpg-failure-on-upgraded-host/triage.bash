#!/usr/bin/env bash
# Triage: why does installing Google Chrome fail its OpenPGP check on this host?
#
# HOST-ONLY and READ-ONLY. It installs nothing, removes nothing and changes no
# configuration — every probe below is a query. Run it, then paste the whole
# output; it gathers in one pass what would otherwise be a dozen commands typed
# on a machine that is not the one the agent is on (issue #45).
#
# WHY EACH PROBE IS HERE. The failure is:
#
#   OpenPGP check for package "google-chrome-stable-…" from repo "@commandline"
#   has failed: The repository does not have any OpenPGP keys configured
#
# `@commandline` is the synthetic repo dnf uses for a package handed to it as a
# URL or a path. dnf5 validates a package against keys configured ON ITS REPO,
# and `@commandline` has none — so the fix installs from Google's real repo,
# where a key can be configured. That makes the FIRST question simply whether
# this checkout has that change: the old task reproduces the old error exactly,
# and no amount of host state explains that away.
#
# The rest establish what an F41 → F44 upgrade may have left behind. A
# `dnf remove` of Chrome does NOT delete /etc/yum.repos.d/google-chrome.repo:
# that file is written by the package's post-install scriptlet and is owned by
# no package, so it survives the removal of the thing that created it.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The repo root by a script-relative, .git-bounded walk — never `git rev-parse`,
# which answers about the CWD and so resolves to a different repository when this
# script is run by path from elsewhere (CLAUDE/PlanScriptStandards.md R1).
REPO_ROOT="${SCRIPT_DIR}"
while [ "${REPO_ROOT}" != "/" ] && [ ! -d "${REPO_ROOT}/.git" ]; do
    REPO_ROOT="$(dirname "${REPO_ROOT}")"
done
if [ ! -d "${REPO_ROOT}/.git" ]; then
    echo "TRIAGE-ABORT: no .git above ${SCRIPT_DIR}; run this from inside the checkout" >&2
    exit 1
fi

PLAY="${REPO_ROOT}/playbooks/imports/play-browsers.yml"

rule() { printf '\n== %s ==\n' "$1"; }

# probe <label> <command>... — run a read-only query and report ALL THREE
# outcomes distinctly. A command that FAILED, a command that succeeded and found
# NOTHING, and a command that found something are three different facts, and a
# triage script that collapses them reports a host it never actually asked
# about. This is the same distinction the rest of this repo keeps having to
# relearn, so it is made once, here, rather than at each call site.
probe() {
    local label="$1"
    shift
    local out rc=0
    out="$("$@" 2>&1)" || rc=$?
    if [ "${rc}" -ne 0 ]; then
        printf -- '- %s: COMMAND FAILED (exit %d): %s\n' "${label}" "${rc}" "${out:-(no output)}"
    elif [ -z "${out//[[:space:]]/}" ]; then
        printf -- '- %s: ran cleanly and found nothing\n' "${label}"
    else
        printf -- '- %s:\n%s\n' "${label}" "${out}"
    fi
}

echo "TRIAGE — chrome install GPG failure (Plan 00124, issue #45)"
probe "host kernel" uname -r
probe "host release" grep -E '^(NAME|VERSION_ID)=' /etc/os-release

rule "1. does THIS checkout carry the fix?"
# The decisive probe. `google-chrome-stable` = fixed (installs from the repo);
# a `https://…rpm` URL = the old task, which reproduces the old error by design.
printf -- '- checkout: %s\n' "${REPO_ROOT}"
probe "commit" git -C "${REPO_ROOT}" log --oneline -1
probe "branch and tracking" git -C "${REPO_ROOT}" status --short --branch
if [ ! -r "${PLAY}" ]; then
    echo "  VERDICT: play-browsers.yml is NOT READABLE at ${PLAY}"
elif grep -qE '^[[:space:]]*name: google-chrome-stable[[:space:]]*$' "${PLAY}"; then
    echo "  VERDICT: FIXED — this checkout installs from Google's repo."
    echo "           If the error still names @commandline, something else ran this play."
elif grep -qE 'name: https://dl\.google\.com/.*\.rpm' "${PLAY}"; then
    echo "  VERDICT: STALE — this checkout still installs from the direct .rpm URL."
    echo "           That is the old task and it reproduces the old error exactly."
    echo "           Fix: git -C ${REPO_ROOT} pull"
else
    echo "  VERDICT: UNRECOGNISED — the Chrome task matches neither shape."
    probe "the task as it stands" grep -n -A6 'name: Install Google Chrome' "${PLAY}"
fi

rule "2. leftover repo files (an upgrade or a dnf remove leaves these)"
probe "chrome/google repo files" sh -c 'ls -la /etc/yum.repos.d/ | grep -iE "chrome|google"'
for repo_file in /etc/yum.repos.d/google-chrome*.repo; do
    if [ -r "${repo_file}" ]; then
        printf -- '\n--- %s ---\n' "${repo_file}"
        cat "${repo_file}"
    fi
done

rule "3. Google keys in the RPM keyring"
# The package in Google's repo is signed by subkey FD533C07C264648F of primary
# 7721F63BD38B4796. Both are in dl.google.com/linux/linux_signing_key.pub —
# checked against the repo's own package, so a MISSING key here is a real
# finding and a present one exonerates the key as the cause.
probe "google gpg-pubkey entries" sh -c \
    "rpm -qa gpg-pubkey --qf '%{version}-%{release}  %{summary}\n' | grep -i google"

rule "4. is Chrome installed right now?"
probe "google-chrome-stable" rpm -q google-chrome-stable

rule "5. what dnf itself says"
probe "repos dnf knows about" sh -c 'dnf -q repolist --all | grep -iE "repo id|chrome"'
probe "dnf's view of the package" sh -c 'dnf -q --refresh info google-chrome-stable 2>&1 | head -20'

rule "6. dnf version (the behaviour that changed is dnf5's)"
probe "dnf --version" sh -c 'dnf --version | head -3'
probe "dnf packages" sh -c 'rpm -q dnf dnf5 libdnf5 | grep -v "not installed"'

echo ""
echo "TRIAGE COMPLETE — nothing was changed. Paste the whole output above."
