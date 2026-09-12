#!/usr/bin/env bash
#
# fedora-upgrade.bash — upgrade this machine to the Fedora release THIS BRANCH
# targets.
#
# The target version is read from vars/fedora-version.yml, the same file that
# run.bash, scripts/setup.bash, the GNOME compat helper and eight playbooks
# read. Hardcoding it here would make this a second place the current release
# lives, and the two would drift the moment a new branch is cut. Check out the
# branch for the release you want, then run this.
#
# This is the machine half of a Fedora release; the repo half (new branch, new
# default branch, banners on the retired branches) is in docs/development.md
# under "Creating New Version Branch".
#
# Run as root. Reboots the machine.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly REPO_ROOT
readonly VERSION_FILE="$REPO_ROOT/vars/fedora-version.yml"

if [[ "$(whoami)" != "root" ]]; then
    echo "run this as root" >&2
    exit 1
fi

if [[ ! -f "$VERSION_FILE" ]]; then
    echo "ERROR: $VERSION_FILE not found — it is the single source of truth for the target version" >&2
    exit 1
fi

upgradeVersion="$(grep 'fedora_version:' "$VERSION_FILE" | awk '{print $2}')"
if [[ ! "$upgradeVersion" =~ ^[0-9]+$ ]]; then
    echo "ERROR: no numeric fedora_version in $VERSION_FILE (read: '$upgradeVersion')" >&2
    exit 1
fi
readonly upgradeVersion

echo "Target: Fedora $upgradeVersion (from $VERSION_FILE)" >&2

# Probe-then-fail. `dnf check-update` overloads its exit status: 0 = nothing to
# do, 100 = updates are available, anything else = the check itself failed. The
# status is data here, so it is captured explicitly and then dispatched — an
# error must not be read as "updates available" and silently reboot the machine.
checkUpdateExitCode=0
dnf check-update --refresh || checkUpdateExitCode=$?

case "$checkUpdateExitCode" in
    0)
        echo "Already up to date; proceeding to the release upgrade." >&2
        ;;
    100)
        echo "Pending updates found — applying them and rebooting." >&2
        echo "Re-run this script after the reboot to do the release upgrade." >&2
        dnf upgrade
        reboot now
        ;;
    *)
        echo "ERROR: dnf check-update failed (exit $checkUpdateExitCode)" >&2
        exit 1
        ;;
esac

dnf system-upgrade download --releasever="$upgradeVersion"
dnf offline reboot
