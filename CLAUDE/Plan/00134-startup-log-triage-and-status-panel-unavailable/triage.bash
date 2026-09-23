#!/usr/bin/env bash
# triage.bash — gather grounded FACTS about what the current boot's logs report, and
# about the state behind each finding in RESEARCH-startup-log-findings.md. Fact-finding
# only: renders no verdict (R9) and changes nothing. Read-only and safe to re-run.
#
# WHERE TO RUN: on the HOST, in a terminal. Enforced by plan_require_host (R2) — a
# container has no journal, no abrt spool and no user session to report on.
#
# The report is UNSCRUBBED host state (unit names, container names, paths). It is
# written under untracked/plan-runs/ by plan_start_log and must never be committed.
#
# Probes map to the research doc:
#   F1  status panel state + the host-health findings document it renders
#   F2  play-ledger sentinel, and the ad-hoc reproduction against a THROWAWAY state dir
#   F3  abrt backlog: total, unreported, newest
#   F4  WirePlumber Lua files present + the "NOT supported" journal line
#   F5  duplicate dnf repo ids
#   F6  units logging "RuntimeMaxSec= has no effect"
#   F7  SELinux AVC denials by source context and target, this boot and last 2 days
#   F8  autostart exec bit, firewalld/docker noise, orphan Thunar
#   T3.1  F7 in depth: each running container's workspace bind, relabel option and
#         SELinux labels; the audit log's AVCs by context/comm; each denial's MCS pair
#         matched to a container; the most-denied inodes resolved to host paths with
#         their current and policy-default labels
#   T3.2  Docker's firewall backend; whether the iptables DOCKER-USER chain that
#         lxc-docker-user-iptables-reconcile.bash edits is jumped to and counts packets;
#         which nftables tables hook forward; firewalld's per-boot failures
#
# Usage: ./CLAUDE/Plan/00134-startup-log-triage-and-status-panel-unavailable/triage.bash [-h|--help]
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

usage() {
    cat <<'EOF'
Usage: triage.bash [--help]

Read-only fact-gathering for Plan 00134 (startup log triage). Run on the HOST.
Writes a full report under untracked/plan-runs/ and names the path on the way out.

Options:
  --help    Show this help and exit (creates nothing).
EOF
}

# --help must work before any environment resolution (PlanTriage.md).
for arg in "$@"; do
    case "$arg" in
        --help | -h)
            usage
            exit 0
            ;;
        *)
            echo "Unknown option: $arg (see --help)" >&2
            exit 1
            ;;
    esac
done

plan_mode gather
plan_require_host "it reads the host journal, abrt spool, dnf repos and user session state"

# The audit log and the firewall tables need root. Primed here, before the tee (R3); every
# root probe below uses `sudo -n`, so an unprimed run records the refusal instead of
# prompting into the log.
plan_prime_sudo

plan_start_log auto

# Non-zero exit status is data, not failure (PlanTriage.md probe pattern).
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

ledgerDir="${XDG_STATE_HOME:-${HOME}/.local/state}/fedora-desktop/play-ledger"

boot_error_lines_by_unit() {
    journalctl --no-pager -b -p 4 -o short-iso | awk '{print $3}' | sort | uniq -c | sort -rn
}

extension_states() {
    local ext
    for ext in $(gnome-extensions list --enabled); do
        printf '%s  ' "$ext"
        gnome-extensions info "$ext" | grep -E '^ *State:'
    done
}

ledger_sentinel() {
    ls -la "$ledgerDir"
    echo "--- BROKEN:"
    cat "$ledgerDir/BROKEN"
}

# F2 reproduction: an ad-hoc play against a THROWAWAY XDG_STATE_HOME. The real
# ledger is never touched; the temp dir is removed before the probe returns.
adhoc_marks_throwaway_ledger() {
    local tmp
    tmp="$(mktemp -d)"
    (cd "${repoRoot}" && XDG_STATE_HOME="$tmp" ansible localhost -m ping < /dev/null)
    echo "--- throwaway ledger after the ad-hoc run:"
    ls -la "$tmp/fedora-desktop/play-ledger"
    cat "$tmp/fedora-desktop/play-ledger/BROKEN"
    rm -rf "$tmp"
}

abrt_summary() {
    echo "total:      $(abrt-cli list | grep -c '^Id')"
    echo "unreported: $(abrt-cli list --not-reported | grep -c '^Id')"
    echo "--- newest 5 (executable + time only):"
    abrt-cli list | grep -E '^(Executable|Component|Time)' | tail -n 10
}

wireplumber_lua_files() {
    ls -R "${HOME}/.config/wireplumber"
    echo "--- journal:"
    journalctl --no-pager --user -b | grep -E 'Lua configuration files are NOT supported'
}

duplicate_repo_ids() {
    grep -H '^\[' /etc/yum.repos.d/*.repo | awk -F: '{print $2}' | sort | uniq -d
    echo "--- dnf5daemon:"
    journalctl --no-pager -b | grep -E 'Id is present more than once'
}

oneshot_runtime_max() {
    journalctl --no-pager -b | grep -E 'RuntimeMaxSec= has no effect'
    echo "--- repo units declaring RuntimeMaxSec:"
    grep -rlE '^RuntimeMaxSec=' "${repoRoot}/files"
}

avc_by_context_this_boot() {
    journalctl --no-pager -b | grep 'avc:  denied' | grep -oE 'scontext=[^ ]+' | cut -d: -f1-3 | sort | uniq -c
}

avc_targets_two_days() {
    journalctl --no-pager --since '-2 days' | grep 'avc:  denied' \
        | grep -oE 'name="[^"]+"|path="[^"]+"' | sort | uniq -c | sort -rn | awk 'NR<=20'
}

checkout_labels() {
    ls -dZ "${repoRoot}" "${repoRoot}/.claude" "${repoRoot}/.claude/hooks-daemon/untracked"
}

firewalld_docker_noise() {
    journalctl --no-pager -b | grep -cE 'firewalld.*COMMAND_FAILED'
    journalctl --no-pager -b | grep -E 'NAME_CONFLICT'
}

# ── Task 3.1 — F7 in depth ───────────────────────────────────────────────────────────────
# Files the probes below write and read. They live in this run's untracked directory,
# because the probes run in subshells and cannot hand values back any other way.
avcRaw="${PLAN_RUN_DIR:?plan_start_log sets PLAN_RUN_DIR}/avc-boot.raw"
containerMap="${PLAN_RUN_DIR}/container-mcs.txt"
bindSources="${PLAN_RUN_DIR}/bind-sources.txt"
topInodes="${PLAN_RUN_DIR}/avc-top-inodes.txt"

engines_present() {
    local engine
    for engine in podman docker; do
        if command -v "$engine" >/dev/null; then echo "$engine"; fi
    done
}

# container_binds <engine> — per running container: its SELinux process and mount labels,
# its security options (label=disable turns confinement off), its bind specs as the engine
# recorded them (a `:z`/`:Z` suffix is the relabel), and the host label on each source.
container_binds() {
    local engine="$1" names name
    if ! names="$("$engine" ps --format '{{.Names}}' 2>&1)"; then
        printf 'could not list %s containers: %s\n' "$engine" "$names"
        return 1
    fi
    if [[ -z "$names" ]]; then
        echo "no running ${engine} container"
        return 0
    fi
    for name in $names; do
        echo "=== ${engine} ${name}"
        "$engine" inspect --format 'process-label={{.ProcessLabel}}  mount-label={{.MountLabel}}  security-opt={{.HostConfig.SecurityOpt}}' "$name"
        echo "--- binds as recorded (relabel option is the :z/:Z suffix):"
        "$engine" inspect --format '{{range .HostConfig.Binds}}{{.}}{{"\n"}}{{end}}' "$name"
        echo "--- mounts:"
        "$engine" inspect --format '{{range .Mounts}}{{.Type}} {{.Source}} -> {{.Destination}} rw={{.RW}} mode={{.Mode}}{{"\n"}}{{end}}' "$name"
        echo "--- host label on each bind source:"
        "$engine" inspect --format '{{range .Mounts}}{{if eq .Type "bind"}}{{.Source}}{{"\n"}}{{end}}{{end}}' "$name" |
            while read -r src; do
                if [[ -n "$src" ]]; then ls -dZ "$src"; fi
            done
    done
}

# One line per running container: "<MCS categories> <engine> <name>", from its process label
# (user:role:type:level:categories). An AVC's scontext carries the same categories, so this
# names the container behind each denial. Also collects every directory bind source.
record_container_map() {
    local engine names name label
    : >"$containerMap"
    : >"$bindSources"
    for engine in $(engines_present); do
        if ! names="$("$engine" ps --format '{{.Names}}' 2>&1)"; then
            printf 'could not list %s containers: %s\n' "$engine" "$names"
            continue
        fi
        for name in $names; do
            label="$("$engine" inspect --format '{{.ProcessLabel}}' "$name")"
            printf '%s %s %s\n' "$(awk -F: '{print ($5 == "" ? "-" : $5)}' <<<"$label")" "$engine" "$name" >>"$containerMap"
            "$engine" inspect --format '{{range .Mounts}}{{if eq .Type "bind"}}{{.Source}}{{"\n"}}{{end}}{{end}}' "$name" >>"$bindSources"
        done
    done
    sort -u -o "$bindSources" "$bindSources"
    echo "--- MCS → container:"
    cat "$containerMap"
    echo "--- distinct bind sources: $(awk 'END{print NR}' "$bindSources")"
}

# The audit subsystem's own answer, captured once for the aggregations below. `-ts boot`
# is this boot only; the pre-reboot flood is in the journal probes above.
capture_avc_boot() {
    if ! command -v ausearch >/dev/null; then
        echo "ausearch is not installed (audit package), so the audit log cannot be read"
        return 1
    fi
    echo "auditd: $(systemctl is-active auditd)"
    # The file is this user's; only the read needs root.
    sudo -n ausearch -m avc -ts boot 2>&1 | tee "$avcRaw" >/dev/null
    local rc="${PIPESTATUS[0]}"
    echo "ausearch rc=${rc} (1 with '<no matches>' means no AVC this boot)"
    echo "AVC records this boot: $(grep -c 'avc: *denied' "$avcRaw")"
    return "$rc"
}

# awk field extractor shared by the aggregations: k=value up to the next space.
AVC_AWK_FIELD="$(
    cat <<'AWK'
function f(k,  v) { if (match($0, k "=[^ ]+")) { v = substr($0, RSTART + length(k) + 1, RLENGTH - length(k) - 1); gsub(/"/, "", v); return v } return "-" }
function part(ctx, n,  a) { split(ctx, a, ":"); return (a[n] == "" ? "-" : a[n]) }
AWK
)"

avc_by_context_comm() {
    awk "${AVC_AWK_FIELD}"'
        /avc: *denied/ {
            s = f("scontext"); t = f("tcontext")
            printf "%s %s -> %s class=%s comm=%s\n", part(s, 3), part(s, 5), part(t, 3), f("tclass"), f("comm")
        }' "$avcRaw" | sort | uniq -c | sort -rn | awk 'NR<=40'
}

# Which running container each denying MCS pair belongs to; "-" = no running container has
# it (the container has gone, or it is not a container).
avc_by_container() {
    # The map is read in BEGIN, not with FNR == NR: an empty map would make that test true
    # for the AVC file and swallow it.
    awk -v map="$containerMap" "${AVC_AWK_FIELD}"'
        BEGIN { while ((getline line < map) > 0) { split(line, w, " "); owner[w[1]] = w[2] " " w[3] } }
        /avc: *denied/ {
            m = part(f("scontext"), 5)
            print ((m in owner) ? owner[m] : "- (no running container carries " m ")")
        }' "$avcRaw" | sort | uniq -c | sort -rn
}

# The 20 most-denied inodes: count, dev, inode, name, target type. Written to a file for the
# path resolution below as well as printed.
avc_top_inodes() {
    awk "${AVC_AWK_FIELD}"'
        /avc: *denied/ && /ino=/ { print f("dev"), f("ino"), f("name"), part(f("tcontext"), 3) }' "$avcRaw" |
        sort | uniq -c | sort -rn | awk 'NR<=20' | tee "$topInodes"
}

# Resolve each top inode to a path inside the running containers' bind sources (one find
# pass per source), then show the label the host sees, the label policy would restore
# (matchpathcon), and the parent directory's label. A path whose label differs from its
# parent's was created elsewhere and moved in, or relabelled after the fact.
resolve_denied_inodes() {
    local src expr=() ino
    if [[ ! -s "$topInodes" ]]; then
        echo "no inode-bearing AVC this boot, nothing to resolve"
        return 0
    fi
    if [[ ! -s "$bindSources" ]]; then
        echo "no running container has a bind mount, so there is nowhere to look; start the sessions that deny, then re-run"
        return 1
    fi
    while read -r ino; do
        if [[ "${#expr[@]}" -gt 0 ]]; then expr+=(-o); fi
        expr+=(-inum "$ino")
    done < <(awk '{print $3}' "$topInodes")
    while read -r src; do
        [[ -d "$src" ]] || continue
        find "$src" -xdev \( "${expr[@]}" \) -printf '%i %p\n'
    done <"$bindSources" | sort -u |
        while read -r ino path; do
            printf '=== inode %s %s\n' "$ino" "$path"
            printf '  now:     %s\n' "$(stat -c '%C' "$path")"
            if command -v matchpathcon >/dev/null; then
                printf '  policy:  %s\n' "$(matchpathcon -n "$path")"
            else
                printf '  policy:  matchpathcon not installed (libselinux-utils)\n'
            fi
            printf '  parent:  %s\n' "$(stat -c '%C' "$(dirname "$path")")"
        done
}

# ── Task 3.2 — Docker's nftables backend vs the iptables DOCKER-USER chain ──────────────────
docker_firewall_backend() {
    if ! command -v docker >/dev/null; then
        echo "docker is not installed: nothing edits DOCKER-USER on this host"
        return 0
    fi
    echo "server: $(docker version --format '{{.Server.Version}}' 2>&1)"
    echo "--- docker info, firewall lines:"
    docker info 2>&1 | grep -iE 'firewall|iptables|nftables|backend'
    echo "--- /etc/docker/daemon.json:"
    cat /etc/docker/daemon.json
}

# Whether FORWARD jumps to DOCKER-USER at all, and the chain's packet counters (they count
# since the chain was created, so zero means nothing forwarded has passed through it).
iptables_docker_user() {
    iptables --version
    echo "--- FORWARD (a jump to DOCKER-USER must appear here for the chain to be consulted):"
    sudo -n iptables -S FORWARD
    echo "--- DOCKER-USER with counters:"
    sudo -n iptables -L DOCKER-USER -n -v -x
    echo "--- nat POSTROUTING (the reconcile's MASQUERADE):"
    sudo -n iptables -t nat -S POSTROUTING
}

# Every nftables base chain on the forward hook, by table: each one is consulted in priority
# order, so this is what a forwarded packet actually meets. Then every chain that names
# DOCKER-USER, and the tables Docker's nftables backend owns.
nft_forward_path() {
    local ruleset
    if ! ruleset="$(sudo -n nft list ruleset 2>&1)"; then
        printf 'nft list ruleset failed: %s\n' "$ruleset"
        return 1
    fi
    echo "--- tables:"
    sudo -n nft list tables
    echo "--- base chains on the forward hook (table / chain / hook line):"
    awk '/^table / {t = $2 " " $3} /^\tchain / {c = $2} /hook forward/ {print t " / " c " / " $0}' <<<"$ruleset"
    echo "--- rules that jump to or name DOCKER-USER (table / chain / rule):"
    awk '/^table / {t = $2 " " $3} /^\tchain / {c = $2} /DOCKER-USER/ {print t " / " c " / " $0}' <<<"$ruleset"
    echo "--- docker-owned tables in full:"
    awk '/^table / && /docker/ {p = 1} p {print} /^}/ {p = 0}' <<<"$ruleset"
}

lxc_reconcile_state() {
    echo "--- lxcbr0:"
    ip -o -4 addr show dev lxcbr0
    echo "--- reconcile unit:"
    systemctl status lxc-docker-user-iptables-reconcile.service --no-pager
}

firewalld_boot_failures() {
    echo "COMMAND_FAILED this boot: $(journalctl --no-pager -b -u firewalld | grep -c 'COMMAND_FAILED')"
    echo "--- first 20:"
    journalctl --no-pager -b -u firewalld | grep 'COMMAND_FAILED' | awk 'NR<=20'
    echo "--- NAME_CONFLICT:"
    journalctl --no-pager -b -u firewalld | grep 'NAME_CONFLICT'
}

echo "================================================================"
echo "Plan 00134 triage — startup log facts"
echo "================================================================"

echo "### READ THIS FOR: did anything actually crash this boot?"
probe "coredumps this boot" coredumpctl list --no-pager --since "$(uptime -s)"
probe "failed system units" systemctl --failed --no-pager --no-legend
probe "failed user units" systemctl --user --failed --no-pager --no-legend
probe "gnome-shell JS ERROR count" bash -c 'journalctl --no-pager --user -b | grep -c "JS ERROR"'
probe "enabled extensions and their state" extension_states
probe "warning+ lines this boot, by unit" boot_error_lines_by_unit

echo "### READ THIS FOR: F1 — what the status panel is rendering"
probe "host-health findings document" cat "$ledgerDir/host-health-findings.md"

echo "### READ THIS FOR: F2 — the ledger sentinel, and whether an ad-hoc run recreates it"
echo "###   A BROKEN file in the throwaway dir = the callback still marks ad-hoc plays."
probe "play-ledger state dir" ledger_sentinel
probe "ad-hoc ansible against a throwaway state dir" adhoc_marks_throwaway_ledger

echo "### READ THIS FOR: F3 — the ABRT backlog the applet re-announces at login"
probe "abrt summary" abrt_summary
probe "abrt-applet assertions this boot" bash -c 'journalctl --no-pager --user -b | grep -c "g_app_info_should_show"'

echo "### READ THIS FOR: F4 — WirePlumber Lua files that 0.5 ignores"
probe "wireplumber user config + journal line" wireplumber_lua_files

echo "### READ THIS FOR: F5 — repo ids defined in more than one .repo file"
probe "duplicate repo ids" duplicate_repo_ids

echo "### READ THIS FOR: F6 — oneshot units with an ignored RuntimeMaxSec"
probe "RuntimeMaxSec on Type=oneshot" oneshot_runtime_max

echo "### READ THIS FOR: F7 — SELinux denials: who, against what"
probe "AVC by source context, this boot" avc_by_context_this_boot
probe "AVC top targets, last 2 days" avc_targets_two_days
probe "labels on this checkout" checkout_labels

echo "### READ THIS FOR: F8 — small repo-owned noise"
probe "autostart entries with exec bit" find "${HOME}/.config/autostart" -maxdepth 1 -name '*.desktop' -perm -u+x
probe "firewalld docker noise (count, conflicts)" firewalld_docker_noise
probe "Thunar installed / required by" bash -c 'rpm -q Thunar; rpm -q --whatrequires Thunar'

echo "### READ THIS FOR: Task 3.1 — which containers deny, through which mounts, on which labels"
echo "###   A workspace bind with no :z/:Z, or security-opt label=disable, explains its session."
echo "###   A path whose 'now' label is user_home_t under a :z-relabelled source was created or"
echo "###   restored on the host after the relabel; compare it with 'parent' and 'policy'."
echo "###   The raw capture and the intermediate files are in this run's directory."
probe "SELinux mode" getenforce
for engine in $(engines_present); do
    probe "${engine} containers: labels, binds, host labels" container_binds "$engine"
done
probe "running containers by MCS pair, and their bind sources" record_container_map
probe "audit log AVCs this boot (capture)" capture_avc_boot
probe "AVC by source type, MCS, target type, class, comm (top 40)" avc_by_context_comm
probe "AVC by the running container that owns the MCS pair" avc_by_container
probe "20 most-denied inodes (count dev ino name target-type)" avc_top_inodes
probe "most-denied inodes resolved to host paths and labels" resolve_denied_inodes

echo "### READ THIS FOR: Task 3.2 — is DOCKER-USER still on the forward path?"
echo "###   The reconcile script edits the iptables DOCKER-USER chain. With Docker's nftables"
echo "###   backend that chain matters only if FORWARD jumps to it; its counters say whether"
echo "###   anything forwarded has passed through since it was created."
probe "docker firewall backend" docker_firewall_backend
probe "iptables FORWARD, DOCKER-USER, nat POSTROUTING" iptables_docker_user
probe "nftables forward hooks and DOCKER-USER references" nft_forward_path
probe "lxcbr0 and the reconcile unit" lxc_reconcile_state
probe "firewalld failures this boot" firewalld_boot_failures

echo "================================================================"
echo "END OF REPORT — read F2 first: if the throwaway ledger holds a BROKEN"
echo "file, Task 1.1 is still open and the panel will go unavailable again"
echo "on the next ad-hoc ansible command."
echo "================================================================"
