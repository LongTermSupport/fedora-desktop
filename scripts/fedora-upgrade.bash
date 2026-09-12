#!/usr/bin/env bash
set -euo pipefail
readonly upgradeVersion=44
if [[ "$(whoami)" != "root" ]];then
   echo "run this as root"
   exit 1;
fi
set +e
dnf check-update --refresh
checkUpdateExitCode=$?
set -e
if (( 0 < $checkUpdateExitCode )); then
   dnf upgrade
   reboot now
fi
dnf system-upgrade download --releasever=$upgradeVersion
dnf offline reboot
