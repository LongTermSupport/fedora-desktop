#!/usr/bin/env bash
# wifi-dscp-probe.bash — does this link carry packets in every Wi-Fi priority queue?
#
# Wi-Fi sends a packet through one of four queues (WMM access categories) picked from
# its DSCP marking. A radio that has stopped serving one queue drops every packet
# marked for it while unmarked traffic to the same host gets through — which is how
# OpenSSH, marking EF by default, timed out on a link plain TCP crossed fine. This pings
# each target once per marking and prints the loss, so a dead queue shows as one column
# of 100%. Read-only.
#
# Usage: wifi-dscp-probe.bash [target ...]   default: the default gateway
set -euo pipefail

for tool in ping iw; do
    command -v "${tool}" >/dev/null || { printf '[FATAL] %s not found — an IaC gap, not something to skip\n' "${tool}" >&2; exit 1; }
done

# TOS bytes covering the low, middle and high DSCP classes; EF is OpenSSH's default.
markings=(0x00 0x20 0x48 0x88 0xb8)
labels=("none" "CS1" "AF21" "AF41" "EF")

targets=("$@")
if [[ ${#targets[@]} -eq 0 ]]; then
    gateway="$(ip -4 route show default | awk '{print $3; exit}')"
    [[ -n "${gateway}" ]] || { printf '[FATAL] no IPv4 default route to probe\n' >&2; exit 1; }
    targets=("${gateway}")
fi

device="$(ip -4 route get "${targets[0]}" | awk '{for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1)}')"
[[ -n "${device}" ]] || { printf '[FATAL] no route to %s\n' "${targets[0]}" >&2; exit 1; }
link="$(iw dev "${device}" link | awk '/SSID|freq/ {printf "%s ", $0}')"
[[ -n "${link}" ]] || { printf '[FATAL] %s is not a connected Wi-Fi interface; this probe measures Wi-Fi queues\n' "${device}" >&2; exit 1; }
printf 'Interface %s: %s\n\n' "${device}" "${link}"

for target in "${targets[@]}"; do
    printf '%s\n' "${target}"
    for i in "${!markings[@]}"; do
        # ping exits 1 when replies are lost, which is a result here, not a failure.
        result="$(ping -c 4 -W 1 -i 0.3 -Q "${markings[i]}" "${target}")" || [[ $? -eq 1 ]]
        loss="$(grep -oE '[0-9]+% packet loss' <<<"${result}")" || {
            printf '[FATAL] ping to %s printed no loss summary:\n%s\n' "${target}" "${result}" >&2
            exit 1
        }
        printf '  %-5s %-20s %s\n' "${markings[i]}" "${labels[i]}" "${loss}"
    done
done
