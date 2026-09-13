#!/usr/bin/env bash
# probe-vm-lab-host.bash — the facts the VM acceptance lab needs from the HOST, appended as
# markdown sections to the report file given as $1. Renders no verdict (R9).
#
# Normally invoked as a leg of triage.bash. Runnable standalone:
#   ./probe-vm-lab-host.bash /tmp/report.md
#
# Every question ends in one of three definite states, and only the third is a failure of
# this script:
#   - ANSWERED: the fact was read.
#   - ABSENT / DEFERRED: the tool is not installed yet (Phase 2 installs it), or the question
#     needs a running guest (a later phase boots one). Both are real answers.
#   - UNANSWERED: a tool that exists failed to answer. The script exits 1 so the fact-finding
#     is visibly incomplete.
#
# Read-only, with one exception stated in triage.bash: the reflink probe creates and removes
# two small files under the lab directory's nearest existing ancestor.
#
# Nothing here discards a command's stderr. Tool diagnostics flow to this script's stderr,
# where the caller's run log records them (R13).
#
# EXIT CODES:
#   0  every question reached a definite state
#   1  at least one question is UNANSWERED
#  64  usage error
set -euo pipefail

# ── R1 bootstrap ──────────────────────────────────────────────────────────────────────────
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

REPORT="${1:-}"
if [[ -z "${REPORT}" ]]; then
    printf 'usage: probe-vm-lab-host.bash <report-file>\n' >&2
    exit 64
fi

plan_require_host "it reads KVM, the lab filesystem, libvirt, group membership and linger state of the host"

INCOMPLETE=0
LAB_DIR="${HOME}/.local/share/vmtest"
MANIFEST="${PLAN_REPO_ROOT}/vars/vm-test-scenarios.yml"

out() { printf '%s\n' "$*" >>"${REPORT}"; }

# probe <label> <cmd...> — run a command, record its output and status as data. A non-zero
# status is a finding, not a failure of this script (CLAUDE/PlanTriage.md).
probe() {
    local label="${1:?probe requires a label}" rc=0 output=""
    shift
    if output="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    out "### ${label}  (rc=${rc})"
    out '```'
    out "${output:-(no output)}"
    out '```'
    out ""
    return 0
}

unanswered() {
    out "**UNANSWERED:** $*"
    out ""
    printf '[INCOMPLETE] %s\n' "$*" >&2
    INCOMPLETE=1
}

have_tool() {
    [[ -n "$(command -v "${1:?have_tool requires a name}")" ]]
}

# matching_lines <extended-regex> <text> — the matching lines, case-insensitive. grep's exit 1
# means "none", which is an answer; 2+ means grep itself failed, which is not.
matching_lines() {
    local pattern="${1:?matching_lines requires a pattern}" text="${2-}" rc=0 found=""
    found="$(printf '%s\n' "${text}" | grep -i -E "${pattern}")" || rc=$?
    case "${rc}" in
        0) printf '%s\n' "${found}" ;;
        1) printf '' ;;
        *)
            printf '[ERROR] grep failed with exit %s for pattern: %s\n' "${rc}" "${pattern}" >&2
            return "${rc}"
            ;;
    esac
}

# ── 1. KVM ────────────────────────────────────────────────────────────────────────────────

out ""
out "## 1. KVM — the decision gate's first input"
out ""

if [[ -e /dev/kvm ]]; then
    out "**/dev/kvm is present.**"
    probe "/dev/kvm ownership and mode" ls -l /dev/kvm
    if [[ -r /dev/kvm && -w /dev/kvm ]]; then
        out "**This user can open /dev/kvm read-write now** (no re-login needed)."
    else
        out "**This user cannot open /dev/kvm read-write in this session.** Fedora ships it"
        out "root:kvm 0660; play-vm-test-lab adds the user to the kvm group, and the new group"
        out "applies only after a re-login."
    fi
else
    out "**/dev/kvm is ABSENT.** Without it the lab would run on TCG emulation, which the"
    out "design refuses (T0.2)."
fi
out ""
probe "user's effective groups" id -nG
if cpuinfoFlags="$(grep -m1 -E '^flags' /proc/cpuinfo)"; then
    virtFlags="$(matching_lines '\b(vmx|svm)\b' "${cpuinfoFlags}")"
    if [[ -n "${virtFlags}" ]]; then
        out "**CPU virtualisation flag present** ($(printf '%s\n' "${cpuinfoFlags}" | grep -o -E '\b(vmx|svm)\b' | sort -u | tr '\n' ' '))."
    else
        out "**No vmx/svm flag in /proc/cpuinfo.** Hardware virtualisation is off or unavailable."
    fi
else
    unanswered "could not read CPU flags from /proc/cpuinfo"
fi
out ""
probe "logical CPUs" nproc

# ── 2. The lab filesystem (U1) ────────────────────────────────────────────────────────────

out ""
out "## 2. Lab filesystem at ${LAB_DIR} (U1)"
out ""

labProbeDir="${LAB_DIR}"
while [[ ! -d "${labProbeDir}" ]]; do
    labProbeDir="$(dirname "${labProbeDir}")"
done
if [[ "${labProbeDir}" == "${LAB_DIR}" ]]; then
    out "The lab directory exists."
else
    out "The lab directory does not exist yet (play-vm-test-lab creates it); probing its"
    out "nearest existing ancestor, **${labProbeDir}**, which is on the same filesystem."
fi
out ""
probe "filesystem type, source and mount options (look for discard)" findmnt -T "${labProbeDir}" -o TARGET,SOURCE,FSTYPE,OPTIONS
probe "free space" df -h "${labProbeDir}"
probe "block-device discard support (DISC-GRAN > 0 means the device accepts discards)" lsblk -D -o NAME,DISC-GRAN,DISC-MAX,MOUNTPOINTS

reflinkSrc="${labProbeDir}/.vmtest-reflink-probe-$$"
reflinkDst="${reflinkSrc}.clone"
if printf 'reflink probe\n' >"${reflinkSrc}"; then
    if cp --reflink=always "${reflinkSrc}" "${reflinkDst}"; then
        out "**cp --reflink=always SUCCEEDS on this filesystem** — a base refresh copy is O(1) metadata (§7)."
    else
        out "**cp --reflink=always FAILS on this filesystem** — a refresh pays a base-sized copy;"
        out "the qemu-img convert alternative in §7 applies."
    fi
    rm -f "${reflinkSrc}" "${reflinkDst}"
else
    unanswered "could not create a probe file under ${labProbeDir}; reflink support unknown"
fi
out ""

# ── 3. Headroom for three bases ───────────────────────────────────────────────────────────

out ""
out "## 3. Memory headroom against the manifest's guest sizing"
out ""
probe "memory (MiB)" free -m
if ramLines="$(grep -E '^\s+ram_mib:' "${MANIFEST}")"; then
    ramTotal=0
    while read -r _ value; do ramTotal=$((ramTotal + value)); done <<<"${ramLines}"
    out "The manifest declares **${ramTotal} MiB** of guest RAM if every base ran at once;"
    out "runs are single-flight per profile, so the realistic peak is the two largest."
else
    unanswered "could not read ram_mib values from ${MANIFEST}"
fi
out ""

# ── 4. Tooling inventory — what Phase 2 must install ─────────────────────────────────────

out ""
out "## 4. Lab tooling inventory (T2.1 package list; ABSENT here is expected before Phase 2)"
out ""
for pkg in libvirt-daemon-kvm libvirt-daemon-config-network libvirt-client virt-install \
    qemu-kvm qemu-img edk2-ovmf guestfs-tools virtiofsd cloud-utils xorriso osinfo-db lorax \
    swtpm swtpm-tools; do
    if rpm -q "${pkg}" >/dev/null; then
        out "- ${pkg}: **installed** ($(rpm -q "${pkg}"))"
    else
        out "- ${pkg}: absent"
    fi
done
out ""

# ── 5. systemd --user linger and libvirt session state (U2) ───────────────────────────────

out ""
out "## 5. User session: linger and qemu:///session (U2)"
out ""
probe "loginctl linger for this user" loginctl show-user "${USER}" -p Linger
if have_tool virsh; then
    probe "qemu:///session domains" virsh -c qemu:///session list --all
    probe "qemu:///session URI answers" virsh -c qemu:///session uri
else
    out "virsh is absent: the session-mode connection is **DEFERRED to Phase 2** (after"
    out "play-vm-test-lab installs libvirt-client). The host→guest port-forward test needs a"
    out "booted guest and is answered by the first server-fast run (Phase 3)."
    out ""
fi

# ── 6. virt-install capabilities (U2, U4, and the --cloud-init confirmation) ─────────────

out ""
out "## 6. virt-install option sets (U2 network, U4 video/graphics, --cloud-init confirmation)"
out ""
if have_tool virt-install; then
    probe "virt-install --cloud-init=?" virt-install --cloud-init=?
    probe "virt-install --video=?" virt-install --video=?
    probe "virt-install --graphics=?" virt-install --graphics=?
    probe "virt-install --network=?" virt-install --network=?
    out "Which video/graphics pair boots a GNOME Wayland session cleanly (U4) needs a booted"
    out "desktop guest: **DEFERRED to Phase 5**."
    out ""
else
    out "virt-install is absent: **DEFERRED to Phase 2**. U4 itself needs a desktop guest"
    out "(Phase 5)."
    out ""
fi

# ── 7. GNOME comps environment id (U5) ────────────────────────────────────────────────────

out ""
out "## 7. GNOME environment group id (U5) — only the liveimg fallback route depends on it"
out ""
# dnf5 keeps its system state (packages.toml, groups.toml) root-only 0600, so an unprivileged
# `dnf group list` fails before it reads a single repo. Read-only, but it needs root; the
# caller primes sudo before the run log opens (R3), and -n keeps this non-interactive.
if have_tool dnf; then
    if groupList="$(timeout 180 sudo -n dnf --quiet group list --hidden 2>&1)"; then
        if gnomeRows="$(matching_lines 'workstation|gnome' "${groupList}")"; then
            if [[ -n "${gnomeRows}" ]]; then
                out "Rows mentioning workstation/gnome in \`dnf group list --hidden\`:"
                out '```'
                out "${gnomeRows}"
                out '```'
            else
                out "\`dnf group list --hidden\` returned no row mentioning workstation or gnome."
            fi
        else
            unanswered "grep over the dnf group list failed"
        fi
    else
        unanswered "dnf group list --hidden failed or timed out"
    fi
    # `group list` shows groups; the kickstart's `@^…` syntax names an ENVIRONMENT, which
    # dnf5 lists separately. This is the row that answers U5 directly.
    if envList="$(timeout 180 sudo -n dnf --quiet environment list 2>&1)"; then
        if envRows="$(matching_lines 'workstation|gnome' "${envList}")"; then
            if [[ -n "${envRows}" ]]; then
                out "Rows mentioning workstation/gnome in \`dnf environment list\` (the \`@^\` ids):"
                out '```'
                out "${envRows}"
                out '```'
            else
                out "\`dnf environment list\` returned no row mentioning workstation or gnome."
            fi
        else
            unanswered "grep over the dnf environment list failed"
        fi
    else
        unanswered "dnf environment list failed or timed out"
    fi
else
    unanswered "dnf is not on PATH"
fi
out ""

# ── 8. CCY inside a guest (U7) — host-side facts only ─────────────────────────────────────

out ""
out "## 8. CCY prerequisites (U7) — host-side facts; the guest probes are DEFERRED"
out ""
if have_tool ccy; then
    out "- ccy: **on PATH** at $(command -v ccy)"
else
    out "- ccy: absent from PATH"
fi
if ccyVersion="$(grep -m1 -E '^(readonly )?CCY_VERSION=' "${PLAN_REPO_ROOT}/files/var/local/claude-yolo/claude-yolo")"; then
    out "- repo launcher version line: \`${ccyVersion}\`"
else
    out "- repo launcher version line: not found"
fi
if have_tool podman; then
    probe "podman rootless state" podman info --format '{{.Host.Security.Rootless}}'
else
    out "- podman: absent"
    out ""
fi
out "Starting ccy with no credential, placing a synthetic token, and \`ccy --rebuild\` are"
out "state changes on a workstation with live sessions and are **DEFERRED to the guest**"
out "(Phase 3's server-fast base is the first place they can run harmlessly)."
out ""

# ── 9. LUKS unlock route (U8) — what can be read without a guest ─────────────────────────

out ""
out "## 9. LUKS unattended unlock (U8) — host-side facts; the route selection is DEFERRED"
out ""
for pkg in swtpm swtpm-tools; do
    if rpm -q "${pkg}" >/dev/null; then
        out "- ${pkg}: **installed**"
    else
        out "- ${pkg}: absent (T2.1 installs it)"
    fi
done
if have_tool systemd-ask-password; then
    out "- systemd-ask-password: on PATH"
else
    out "- systemd-ask-password: absent from PATH"
fi
out ""
out "Whether the prompt reaches ttyS0 with plymouth.enable=0, whether a session-mode domain"
out "can attach a swtpm vTPM, and whether the wedge matcher separates a LUKS wedge from a"
out "slow boot all need a booted LUKS guest: **DEFERRED to Phase 5 (T5.1b)**."
out ""

# ── 10. virtiofsd for the DNF cache export (§7) ───────────────────────────────────────────

out ""
out "## 10. virtiofsd (DNF cache export, §7)"
out ""
if have_tool virtiofsd; then
    out "- virtiofsd: **on PATH** at $(command -v virtiofsd)"
elif [[ -x /usr/libexec/virtiofsd ]]; then
    out "- virtiofsd: **installed** at /usr/libexec/virtiofsd (not on PATH, which is normal)"
else
    out "- virtiofsd: absent (T2.1 installs it)"
fi
out ""

# ── 11. Confirmations settled from documentation (§0) ────────────────────────────────────

out ""
out "## 11. Confirmations (already settled from documentation; reported as confirmations)"
out ""
if have_tool systemd-escape; then
    escaped="$(systemd-escape --path "${PLAN_REPO_ROOT}")"
    unescaped="$(systemd-escape --unescape --path "${escaped}")"
    if [[ "${unescaped}" == "${PLAN_REPO_ROOT}" ]]; then
        out "- systemd-escape --path round-trips the checkout path exactly: **confirmed**"
        out "  (\`%f\` in a template unit applies the same unescaping, per systemd.unit(5))."
    else
        unanswered "systemd-escape --path did not round-trip: ${escaped} -> ${unescaped}"
    fi
else
    unanswered "systemd-escape is not on PATH"
fi
out ""

# ── closing ───────────────────────────────────────────────────────────────────────────────

out ""
out "---"
out "READ THIS FOR: the T0.2 gate is sections 1-3. Anything marked DEFERRED names the phase"
out "that answers it; anything marked UNANSWERED made this run exit non-zero."

if [[ "${INCOMPLETE}" -ne 0 ]]; then
    exit 1
fi
exit 0
