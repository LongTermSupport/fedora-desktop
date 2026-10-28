#!/usr/bin/env bash
# Plan 00235 — triage-probe.bash: the probes behind triage.bash, one fact group per call.
#
# Usage: triage-probe.bash <probe> <report-file>
#   disk        df of / and /home, the filesystem under them, and the store's graph root
#   store       podman info's store figures and `podman system df` (its exit status recorded)
#   du          the store's size on disk, walked inside `podman unshare` (slow)
#   images      total, dangling, dangling used by no container, and their summed size
#   by-month    dangling images counted by the month they were created
#   labels      the newest 400 dangling images by title and claude-yolo-version label
#   tags        every tagged image, marked in use when a container (any state) uses it
#   containers  containers by state, and buildah working containers (--external)
#   volumes     volumes, and which are dangling
#   timers      user timers, and whether the deployed ccy tree removes any image
#
# Read-only: it lists and inspects; it removes, prunes, builds and pulls nothing. Each fact
# goes to stdout and is appended to the report. A probe that cannot establish its fact exits
# non-zero, so triage.bash names the leg. The report names this machine's images, projects
# and volumes: it stays in the untracked run directory.
set -euo pipefail

probe="${1:?usage: triage-probe.bash <probe> <report-file>}"
report="${2:?usage: triage-probe.bash <probe> <report-file>}"

readonly ENGINE="${CCY_CONTAINER_ENGINE:-podman}" # the launcher's own default (lib/common.bash)
readonly DEPLOYED_TREE="/var/local/claude-yolo"
readonly NEWEST_LABELLED=400

# fact <text> — one line of the report, on stdout too.
fact() {
    printf '%s\n' "$1" | tee -a "${report}"
}

# block <title> <text> — a fenced block of command output in the report.
block() {
    fact "### $1"
    fact '```'
    fact "${2:-(no output)}"
    fact '```'
}

# strip_ids — full image ids, without any sha256: prefix, one per line, sorted unique.
strip_ids() {
    awk '{ sub(/^sha256:/, ""); if ($0 != "") print }' | sort -u
}

graph_root() {
    "${ENGINE}" info --format '{{.Store.GraphRoot}}'
}

probe_disk() {
    local root out
    root="$(graph_root)"
    fact "- graph root: ${root}"
    out="$(df -h / /home "${root}")"
    block "df -h / /home <graph root>" "${out}"
    out="$(findmnt -no TARGET,SOURCE,FSTYPE / /home)"
    block "findmnt / /home" "${out}"
}

probe_store() {
    local out rc
    out="$("${ENGINE}" info --format 'driver={{.Store.GraphDriverName}} images={{.Store.ImageStore.Number}} containers={{.Store.ContainerStore.Number}}')"
    fact "- podman info: ${out}"
    out="$("${ENGINE}" version --format '{{.Client.Version}}')"
    fact "- podman version: ${out}"
    # A failing `system df` is a finding (exit 125 on a volume owned outside the subuid range),
    # not a failed probe: record its status and its message.
    if out="$("${ENGINE}" system df 2>&1)"; then rc=0; else rc=$?; fi
    block "podman system df (rc=${rc})" "${out}"
}

probe_du() {
    local root out
    root="$(graph_root)"
    out="$("${ENGINE}" unshare du -sx --block-size=1G "${root}/overlay" "${root}/overlay-images" "${root}/volumes")"
    block "du -sx (GiB) inside podman unshare" "${out}"
}

probe_images() {
    local all top dangling used unused_dangling bytes
    local -a unused_ids
    all="$("${ENGINE}" images -a -q --no-trunc | strip_ids)"
    top="$("${ENGINE}" images -q --no-trunc | strip_ids)"
    dangling="$("${ENGINE}" images -q --no-trunc --filter dangling=true | strip_ids)"
    used="$("${ENGINE}" ps -a --format '{{.ImageID}}' | strip_ids)"
    unused_dangling="$(comm -23 <(printf '%s\n' "${dangling}") <(printf '%s\n' "${used}") | awk 'NF')"
    fact "- images, all layers (images -a): $(printf '%s\n' "${all}" | awk 'NF' | wc -l)"
    fact "- images (images): $(printf '%s\n' "${top}" | awk 'NF' | wc -l)"
    fact "- dangling (<none>:<none>): $(printf '%s\n' "${dangling}" | awk 'NF' | wc -l)"
    fact "- images some container uses: $(printf '%s\n' "${used}" | awk 'NF' | wc -l)"
    fact "- dangling used by no container: $(printf '%s\n' "${unused_dangling}" | awk 'NF' | wc -l)"
    if [[ -z "${unused_dangling}" ]]; then
        fact "- size of those: 0 (none)"
        return 0
    fi
    # Each image's own Size counts the layers it shares with others again, so this is an upper
    # bound on what removing them frees, not the figure.
    mapfile -t unused_ids <<<"${unused_dangling}"
    bytes="$("${ENGINE}" image inspect --format '{{.Size}}' "${unused_ids[@]}" | awk '{ s += $1 } END { printf "%.1f", s / 1073741824 }')"
    fact "- summed Size of those, GiB (shared layers counted again: an upper bound): ${bytes}"
}

probe_by_month() {
    local out
    out="$("${ENGINE}" images --filter dangling=true --format '{{.CreatedAt}}' | awk '{ print substr($1, 1, 7) }' | sort | uniq -c)"
    block "dangling images by month created (count month)" "${out}"
}

probe_labels() {
    local ids out
    local -a id_list
    ids="$("${ENGINE}" images -q --no-trunc --filter dangling=true | awk -v n="${NEWEST_LABELLED}" 'NR <= n')"
    if [[ -z "${ids}" ]]; then
        fact "- no dangling images"
        return 0
    fi
    mapfile -t id_list <<<"${ids}"
    out="$("${ENGINE}" image inspect --format '{{index .Labels "org.opencontainers.image.title"}}|ccy={{index .Labels "claude-yolo-version"}}' "${id_list[@]}" | sort | uniq -c | sort -rn)"
    block "newest ${NEWEST_LABELLED} dangling images by title|ccy label (count title|ccy=version)" "${out}"
}

probe_tags() {
    local used out
    used="$("${ENGINE}" ps -a --format '{{.ImageID}}' | strip_ids)"
    out="$("${ENGINE}" images --filter dangling=false --no-trunc --format '{{.ID}} {{.Repository}}:{{.Tag}} {{.CreatedAt}} {{.Size}}' |
        awk -v used="${used}" 'BEGIN { n = split(used, u, "\n"); for (i = 1; i <= n; i++) inuse[u[i]] = 1 }
            { id = $1; sub(/^sha256:/, "", id); $1 = (id in inuse) ? "IN-USE" : "unused"; print }')"
    block "tagged images (in-use|unused repository:tag created size)" "${out}"
}

probe_containers() {
    local out
    out="$("${ENGINE}" ps -a --format '{{.State}}' | sort | uniq -c)"
    block "containers by state" "${out}"
    out="$("${ENGINE}" ps -a --format '{{.Names}} {{.Image}} {{.State}}')"
    block "containers (name image state)" "${out}"
    # --external adds the containers podman did not create (buildah's working containers, which
    # an interrupted build leaves); the names not in the plain listing are those.
    out="$(comm -13 <("${ENGINE}" ps -a --format '{{.Names}}' | sort) <("${ENGINE}" ps -a --external --format '{{.Names}}' | sort))"
    block "containers only --external lists (buildah working containers)" "${out}"
}

probe_volumes() {
    local all dangling
    all="$("${ENGINE}" volume ls -q)"
    dangling="$("${ENGINE}" volume ls -q --filter dangling=true)"
    fact "- volumes: $(printf '%s\n' "${all}" | awk 'NF' | wc -l)"
    fact "- dangling volumes (no container): $(printf '%s\n' "${dangling}" | awk 'NF' | wc -l)"
    block "dangling volumes" "${dangling}"
}

probe_timers() {
    local out rc
    out="$(systemctl --user list-timers --all --no-pager)"
    block "systemctl --user list-timers --all" "${out}"
    # grep exits 1 when nothing matches: that absence is the fact.
    if out="$(grep -rnE 'image prune|system prune|image rm|rmi ' "${DEPLOYED_TREE}")"; then rc=0; else rc=$?; fi
    if [[ "${rc}" -gt 1 ]]; then
        printf '[FAIL] grep exited %s over %s\n' "${rc}" "${DEPLOYED_TREE}" >&2
        return 1
    fi
    block "image removals in the deployed ccy tree (grep rc=${rc}; 1 = none)" "${out}"
}

case "${probe}" in
    disk) probe_disk ;;
    store) probe_store ;;
    du) probe_du ;;
    images) probe_images ;;
    by-month) probe_by_month ;;
    labels) probe_labels ;;
    tags) probe_tags ;;
    containers) probe_containers ;;
    volumes) probe_volumes ;;
    timers) probe_timers ;;
    *)
        printf '[FATAL] unknown probe: %s\n' "${probe}" >&2
        exit 64
        ;;
esac
