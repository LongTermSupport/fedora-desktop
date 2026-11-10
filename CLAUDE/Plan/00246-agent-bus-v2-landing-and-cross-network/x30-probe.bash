#!/usr/bin/env bash
# Plan 00246 Task 8.8: one read-only leg of x30-triage.bash.
#
#   x30-probe.bash <leg> <report-file> [<run-dir>]
#
# Legs: versions, options, udp, routes. Each appends what it found to the report as a heading
# and a fenced block of the command's own output, and exits non-zero when a command it ran
# failed, so a fact-finding that did not complete says so (PlanScriptStandards R9, R11).
# Nothing here creates a libvirt object, a file outside the run directory or a network: the
# domain is only printed (virt-install --print-xml) and read back (virt-xml-validate,
# virsh domxml-to-native).
set -euo pipefail

leg="${1:?usage: x30-probe.bash <leg> <report-file> [<run-dir>]}"
report="${2:?usage: x30-probe.bash <leg> <report-file> [<run-dir>]}"
runDir="${3:-}"
uri="qemu:///session"
labHome="${VMTEST_HOME:-${HOME}/.local/share/vmtest}"

# The udp interface exactly as vmtest member-up --link builds it (memberNIC.network in
# src-go/internal/vmtest/link.go): keep the two in step. The ports and the MAC are fixed
# stand-ins; nothing listens on them.
udpPeer=41001
udpOwn=41000
udpMac="52:54:00:aa:bb:cc"
passt="type=user,backend.type=passt,portForward0.proto=tcp,portForward0.range0.start=40123,portForward0.range0.to=22"
udpDesigned="type=udp,source.address=127.0.0.1,source.port=${udpPeer},source.local.address=127.0.0.1,source.local.port=${udpOwn},model.type=virtio,mac.address=${udpMac}"
udpShort="type=udp,source.address=127.0.0.1,source.port=${udpPeer},source.local.address=127.0.0.1,source.local.port=${udpOwn},model=virtio,mac=${udpMac}"
probeName="x30-probe-no-such-guest"

# record <title> <command...>: run the command, append its output and status to the report,
# and return its status.
record() {
    local title="$1" out status=0 tick=$'\x60'
    local fence="${tick}${tick}${tick}"
    shift
    out="$("$@" 2>&1)" || status=$?
    printf '### %s\n\n%s%s%s exited %d:\n\n%s\n%s\n%s\n\n' \
        "${title}" "${tick}" "$*" "${tick}" "${status}" "${fence}" "${out}" "${fence}" >>"${report}"
    printf '%s: %s exited %d\n' "${title}" "$1" "${status}"
    return "${status}"
}

# note <text>: a line of the report that is not a command's output.
note() {
    printf '%s\n\n' "$1" >>"${report}"
    printf '%s\n' "$1"
}

probe_versions() {
    local failed=0
    printf '## Versions, memory and disk\n\n' >>"${report}"
    record "virt-install" virt-install --version || failed=1
    record "virsh client" virsh --version || failed=1
    record "libvirt and the hypervisor of the session" virsh -c "${uri}" version || failed=1
    record "passt" passt --version || failed=1
    record "host memory" grep -E '^(MemTotal|MemAvailable):' /proc/meminfo || failed=1
    record "disk under the lab" df -h --output=source,size,avail "${labHome}" || failed=1
    return "${failed}"
}

probe_options() {
    printf '## What virt-install says --network takes\n\n' >>"${report}"
    record "virt-install --network=?" virt-install --network=?
}

# domain_xml <network> <out-file>: print the domain a member with a passt NIC and this second NIC
# would have; nothing is defined.
domain_xml() {
    virt-install --connect "${uri}" --name "${probeName}" --memory 512 --vcpus 1 \
        --cpu host-passthrough --import --disk none --osinfo fedora-unknown --boot uefi \
        --memballoon virtio --network "${passt}" --network "$1" --print-xml --noautoconsole >"$2"
}

probe_udp() {
    local failed=0 label network xmlFile
    [[ -n "${runDir}" ]] || {
        printf '[FATAL] the udp leg needs the run directory\n' >&2
        return 64
    }
    printf '## The udp interface\n\n' >>"${report}"
    xmlFile=""
    for label in designed short; do
        case "${label}" in
            designed) network="${udpDesigned}" ;;
            short) network="${udpShort}" ;;
        esac
        if record "virt-install --print-xml, the ${label} spelling" domain_xml "${network}" "${runDir}/x30-domain-${label}.xml"; then
            xmlFile="${runDir}/x30-domain-${label}.xml"
            note "The ${label} spelling was accepted: \`--network ${network}\`."
            break
        fi
        note "The ${label} spelling was refused: \`--network ${network}\`."
    done
    record "no guest was defined" virsh -c "${uri}" list --all --name || failed=1
    if [[ -z "${xmlFile}" ]]; then
        note "No spelling was accepted; the options virt-install lists are in the previous leg."
        return 1
    fi
    if grep -q '<interface type="udp">' "${xmlFile}"; then
        note 'The domain has an interface of type udp.'
    else
        note 'The domain has NO interface of type udp.'
        failed=1
    fi
    record "the interfaces of the domain" grep -A8 '<interface ' "${xmlFile}" || failed=1
    record "libvirt's schema" virt-xml-validate "${xmlFile}" domain || failed=1
    record "the qemu command line libvirt would run" \
        virsh -c "${uri}" domxml-to-native qemu-argv --xml "${xmlFile}" || failed=1
    return "${failed}"
}

probe_routes() {
    local failed=0
    printf '## The controller'"'"'s routes, for choosing the lab subnets\n\n' >>"${report}"
    record "addresses" ip -4 -o addr show || failed=1
    record "routes" ip -4 route show table all || failed=1
    record "policy rules" ip rule show || failed=1
    return "${failed}"
}

case "${leg}" in
    versions) probe_versions ;;
    options) probe_options ;;
    udp) probe_udp ;;
    routes) probe_routes ;;
    *)
        printf '[FATAL] unknown leg: %s\n' "${leg}" >&2
        exit 64
        ;;
esac
