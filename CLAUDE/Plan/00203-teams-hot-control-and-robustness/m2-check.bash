#!/usr/bin/env bash
# m2-check.bash — the probes behind acceptance.bash's M2 legs (SPEC-hot-control.md section 6,
# M2 a-f; H1, H2, H3, H33, H33a, H33b, H34). Each subcommand is one leg; it prints what it saw
# and exits 0 when the check holds, 1 when it does not, 2 when it could not look. Read-only: the
# only writes it attempts are the ones H3 says must fail (into the read-only membership mount).
#
# RUN ON THE HOST, as the desktop user, through acceptance.bash.
#
#   find                     one running session of each kind, as "<kind> <container> <session>"
#                            lines on stdout: plain (the kit, no seat), seated, nobus
#   selinux                  the host is Enforcing
#   plain <container>        (b) (c) the membership reads; a write and a remount fail; the kit is
#                            idle: no active flag, the guarded status line, a hook that starts no
#                            process; at most one pingbus process (the keeper, once M1 ships)
#   seated <container>       (a) every desired seat is held, the active flag is up
#   nobus <container>        (e) PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin or settings
#   cpu <plain> <nobus>      (c) the plain session's idle CPU is the --no-bus one's, within a point
#   image <container>        (d) the session's image carries the M2 entrypoint, at the launcher's
#                            REQUIRED_CONTAINER_VERSION
#   spike <spec>             (f) the H34 settings-reload spike's result is recorded in the spec
set -euo pipefail

readonly MOUNT=/run/ccy/membership
readonly HOME_IN=/tmp/pingbus-home
readonly LAUNCHER=/var/local/claude-yolo/claude-yolo

fail() {
    printf '[FAIL] %s\n' "$*" >&2
    exit 1
}
ok() { printf '[OK] %s\n' "$*"; }

# env_of <container> <name> — the value the container was started with, or "unset".
env_of() {
    local line
    while IFS= read -r line; do
        if [[ "${line%%=*}" == "$2" ]]; then
            printf '%s\n' "${line#*=}"
            return 0
        fi
    done < <(podman inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$1")
    printf 'unset\n'
}

seat_count() { podman exec "$1" cat "${MOUNT}/membership.json" | jq '.seats | length'; }

cmd_find() {
    local id session membership seats found_plain="" found_seated="" found_nobus=""
    while read -r id session; do
        [[ -n "${id}" ]] || continue
        membership="$(env_of "${id}" PINGBUS_MEMBERSHIP)"
        if [[ "${membership}" == off ]]; then
            [[ -n "${found_nobus}" ]] || found_nobus="nobus ${id} ${session}"
        elif [[ "${membership}" == "${MOUNT}" ]]; then
            seats="$(seat_count "${id}")"
            if [[ "${seats}" == 0 ]]; then
                [[ -n "${found_plain}" ]] || found_plain="plain ${id} ${session}"
            else
                [[ -n "${found_seated}" ]] || found_seated="seated ${id} ${session}"
            fi
        fi
    done < <(podman ps --filter label=ccy-session --format '{{.ID}} {{index .Labels "ccy-session"}}')
    local missing=""
    [[ -n "${found_plain}" ]] || missing+=" a plain ccy (no --teams)"
    [[ -n "${found_seated}" ]] || missing+=" a ccy --teams <seat>@<team>"
    [[ -n "${found_nobus}" ]] || missing+=" a ccy --no-bus"
    if [[ -n "${missing}" ]]; then
        printf '[COULD NOT ESTABLISH] start, in ccy'"'"'s tmux (ccy-sessions lists them), and leave running:%s\n' "${missing}" >&2
        exit 2
    fi
    printf '%s\n%s\n%s\n' "${found_plain}" "${found_seated}" "${found_nobus}"
}

cmd_selinux() {
    local mode
    mode="$(getenforce)"
    [[ "${mode}" == Enforcing ]] || fail "SELinux is ${mode}; M2's acceptance is on an Enforcing host (H1's relabel)"
    ok "SELinux is Enforcing"
}

# write_fails <container> <what> <command...> — the command, run in the container, must fail
# with a read-only or permission error and change nothing.
write_fails() {
    local ctr="$1" what="$2" out rc=0
    shift 2
    out="$(podman exec "${ctr}" "$@" 2>&1)" || rc=$?
    [[ "${rc}" -ne 0 ]] || fail "${what} succeeded inside the container: the membership mount is writable"
    [[ "${out}" == *"Read-only file system"* || "${out}" == *"ermission denied"* || "${out}" == *"not permitted"* ]] \
        || fail "${what} failed, but not for read-only or permission reasons: ${out}"
    ok "${what} refused: ${out}"
}

cmd_plain() {
    local ctr="${1:?plain needs the container}" json settings out rc=0 procs keepers
    json="$(podman exec "${ctr}" cat "${MOUNT}/membership.json")"
    [[ "$(jq -r '.v, (.seats | length)' <<<"${json}" | paste -sd' ')" == "1 0" ]] \
        || fail "the plain session's membership is not version 1 with no seat: ${json}"
    ok "(b) the membership reads from inside: ${json}"
    write_fails "${ctr}" "(b) touch ${MOUNT}/x" touch "${MOUNT}/x"
    write_fails "${ctr}" "(H3) a write over membership.json" sh -c "printf x > ${MOUNT}/membership.json"
    write_fails "${ctr}" "(H3) mount -o remount,rw" mount -o remount,rw "${MOUNT}"
    [[ "$(podman exec "${ctr}" cat "${MOUNT}/membership.json")" == "${json}" ]] || fail "the membership changed"
    if podman exec "${ctr}" test -e "${HOME_IN}/active"; then fail "the plain session has the active flag"; fi
    settings="$(podman exec "${ctr}" cat "${HOME_IN}/settings.json")"
    jq -e '.statusLine.command | startswith("[ -e \"$PINGBUS_HOME/active\" ] || exec sh")' <<<"${settings}" >/dev/null \
        || fail "the session's status line is not the shell guard (H34): ${settings}"
    ok "(b) no active flag; the status line is the guard, so it runs the session's own: $(podman exec "${ctr}" cat "${HOME_IN}/statusline-inner")"
    # (c) the UserPromptSubmit hook as the plugin registers it, with nothing on PATH: had the
    # guard not ended it in the shell, `exec pingbus` would fail (127).
    local hook
    hook="$(podman exec "${ctr}" jq -r '.hooks.UserPromptSubmit[0].hooks[0].command' \
        /opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json)"
    out="$(podman exec -e PINGBUS_HOME="${HOME_IN}" -e PATH=/nonexistent "${ctr}" /bin/sh -c "${hook}" </dev/null 2>&1)" || rc=$?
    [[ "${rc}" -eq 0 && -z "${out}" ]] || fail "(c) a prompt's hook in the plain session did not end in the shell (exit ${rc}): ${out}"
    ok "(c) a prompt's hook ends in the shell: no python3, no pingbus, an empty answer"
    procs="$(podman exec "${ctr}" ps -eo args)"
    keepers="$(awk '/[p]ingbus/ { n++ } END { print n + 0 }' <<<"${procs}")"
    if [[ "${keepers}" -gt 1 ]] || { [[ "${keepers}" -eq 1 ]] && ! grep -q '[p]ingbus seat keep' <<<"${procs}"; }; then
        fail "(c) the plain session runs bus processes other than one keeper: $(grep '[p]ingbus' <<<"${procs}")"
    fi
    ok "(c) ${keepers} pingbus process in the plain session (the keeper, once M1 ships; none before)"
}

cmd_seated() {
    local ctr="${1:?seated needs the container}" checkout json seats seat list
    json="$(podman exec "${ctr}" cat "${MOUNT}/membership.json")"
    checkout="$(podman inspect --format '{{range .Mounts}}{{if eq .Destination "/workspace"}}{{.Source}}{{end}}{{end}}' "${ctr}")"
    list="$(cd -- "${checkout}" && agent-bus seat list)"
    seats="$(jq -r '.seats[] | "\(.team)\t\(.seat)"' <<<"${json}")"
    while IFS=$'\t' read -r team seat; do
        grep -qP "^SEAT\t${team}\t${seat}\theld\t" <<<"${list}" || fail "(a) seat ${seat}@${team} is desired but not held: ${list}"
        ok "(a) seat ${seat}@${team} is held"
    done <<<"${seats}"
    podman exec "${ctr}" test -f "${HOME_IN}/active" || fail "(a) the seated session has no active flag"
    ok "(a) the active flag is up, so the hooks and pingbus status-line run as before"
}

cmd_nobus() {
    local ctr="${1:?nobus needs the container}" mounts procs
    [[ "$(env_of "${ctr}" PINGBUS_MEMBERSHIP)" == off ]] || fail "(e) PINGBUS_MEMBERSHIP is not off"
    mounts="$(podman inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "${ctr}")"
    if grep -qxF "${MOUNT}" <<<"${mounts}"; then fail "(e) the --no-bus session has the membership mount"; fi
    if podman exec "${ctr}" test -e /usr/local/bin/pingbus; then fail "(e) the --no-bus session has pingbus on PATH"; fi
    procs="$(podman exec "${ctr}" ps -eo args)"
    if grep -qE -- '--plugin-dir|--settings|[p]ingbus' <<<"${procs}"; then
        fail "(e) the --no-bus session loads the plugin or settings, or runs pingbus: $(grep -E -- '--plugin-dir|--settings|[p]ingbus' <<<"${procs}")"
    fi
    ok "(e) --no-bus: PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin, no settings, no keeper"
}

# cpu_of <container> — the container's CPU percentage, averaged over six samples ten seconds apart.
cpu_of() {
    local i total=0 value
    for i in 1 2 3 4 5 6; do
        value="$(podman stats --no-stream --format '{{.CPUPerc}}' "$1")"
        total="$(awk -v t="${total}" -v v="${value%\%}" 'BEGIN { printf "%.2f", t + v }')"
        [[ "${i}" -eq 6 ]] || sleep 10
    done
    awk -v t="${total}" 'BEGIN { printf "%.2f", t / 6 }'
}

cmd_cpu() {
    local plain nobus
    plain="$(cpu_of "${1:?cpu needs the plain container}")"
    nobus="$(cpu_of "${2:?cpu needs the --no-bus container}")"
    awk -v p="${plain}" -v n="${nobus}" 'BEGIN { exit !(p <= n + 1.0) }' \
        || fail "(c) the idle plain session uses ${plain}% CPU over a minute, the --no-bus one ${nobus}%"
    ok "(c) idle CPU over a minute: plain ${plain}%, --no-bus ${nobus}% (leave both sessions idle while this runs)"
}

cmd_image() {
    local ctr="${1:?image needs the container}" image label required
    image="$(podman inspect --format '{{.ImageName}}' "${ctr}")"
    label="$(podman image inspect --format '{{index .Labels "claude-yolo-version"}}' "${image}")"
    required="$(awk -F'"' '/^REQUIRED_CONTAINER_VERSION=/ { print $2; exit }' "${LAUNCHER}")"
    [[ "${label}" == "${required}" ]] || fail "(d) the image ${image} is ${label}, the launcher requires ${required}"
    podman exec "${ctr}" grep -q 'PINGBUS_MEMBERSHIP-' /usr/local/bin/entrypoint.sh \
        || fail "(d) the image's entrypoint does not read PINGBUS_MEMBERSHIP"
    ok "(d) the image is ${label}, which the launcher requires: an older image is rebuilt before any launch, and an older launcher (no PINGBUS_MEMBERSHIP) is refused by this entrypoint"
}

cmd_spike() {
    local line
    line="$(grep -m1 'M2 spike result' "${1:?spike needs the spec}")" || fail "(f) the H34 spike's result is not recorded in $1"
    ok "(f) ${line#"${line%%[![:space:]-]*}"}"
}

case "${1:-}" in
find | selinux | plain | seated | nobus | cpu | image | spike)
    sub="$1"
    shift
    "cmd_${sub}" "$@"
    ;;
*)
    printf 'usage: m2-check.bash find|selinux|plain|seated|nobus|cpu|image|spike [args]\n' >&2
    exit 64
    ;;
esac
