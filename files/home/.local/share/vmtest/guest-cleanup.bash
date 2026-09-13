#!/usr/bin/bash
# guest-cleanup.bash — pre-snapshot hygiene, run as root INSIDE a guest that is
# about to become (or be re-flattened into) a base (Plan 00110, DESIGN.md §3.3).
#
# Deliberately narrow, and deliberately not virt-sysprep: sysprep's default
# operation set removes exactly what a base must keep (the created user, the
# lab's SSH key, the desktop autologin drop-in) and needs a libguestfs appliance.
# This does the explicit list and nothing else:
#
#   - the DNF cache, so the base does not carry gigabytes of RPMs;
#   - cloud-init's per-instance state, so a clone is not "the same instance";
#   - the SSH host keys, so every clone generates its own on first boot;
#   - /etc/machine-id, so every clone gets its own id (and its own journal);
#   - the journal and log files, and shell histories, so a run's transcript
#     starts clean and nothing from the build leaks into the evidence;
#   - fstrim, so the discarded blocks leave the base sparse.
#
# Copied into the guest and run over SSH by `vmtest build-base`. Fail-fast: any
# step that cannot complete aborts the build rather than snapshotting a base in
# an unknown state.
set -euo pipefail

if [[ "${EUID}" -ne 0 ]]; then
    echo "ERROR: guest-cleanup.bash must run as root inside the guest" >&2
    exit 1
fi
if [[ ! -r /etc/os-release ]] || ! grep -q '^ID=fedora$' /etc/os-release; then
    echo "ERROR: this does not look like a Fedora guest; refusing to clean it" >&2
    exit 1
fi

echo "==> cleaning DNF cache" >&2
dnf -y -q clean all

echo "==> clearing cloud-init instance state" >&2
rm -rf /var/lib/cloud/instances /var/lib/cloud/instance /var/lib/cloud/data /var/lib/cloud/sem

echo "==> removing SSH host keys (regenerated on next boot)" >&2
rm -f /etc/ssh/ssh_host_*

echo "==> resetting machine-id" >&2
truncate -s 0 /etc/machine-id

echo "==> emptying journal, logs and shell histories" >&2
journalctl --rotate
journalctl --vacuum-time=1s
find /var/log -type f \( -name '*.log' -o -name '*.log.*' -o -name 'messages*' -o -name 'secure*' \) -exec truncate -s 0 {} +
rm -f /root/.bash_history
for home in /home/*; do
    if [[ -d "${home}" ]]; then
        rm -f "${home}/.bash_history"
    fi
done

echo "==> trimming filesystems" >&2
fstrim -av
sync

echo "VMTEST-GUEST-CLEANUP-DONE"
