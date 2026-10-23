#!/usr/bin/env bash
# m2-check.bash — the probes behind acceptance.bash's M2 legs (SPEC-hot-control.md section 6,
# M2 a-f; H1, H2, H3, H33, H33a, H33b, H34). Each subcommand is one leg; it prints what it saw
# and exits 0 when the check holds, 1 when it does not, 2 when it could not look. Read-only: the
# only writes it attempts are the ones H3 says must fail (into the read-only membership mount).
#
# RUN ON THE HOST, as the desktop user, through acceptance.bash (_acceptance-m2.inc.bash), on
# the throwaway sessions it starts.
#
#   selinux <container>      (H1) the membership bind asks for Z and its files carry container_file_t
#                            at the container's level; Enforcing: the container reads them;
#                            Permissive: says enforcement was not exercised
#   plain <container>        (b) (c) the membership reads; a write and a remount fail; the kit is
#                            idle: no active flag, the guarded status line, inbound refused by
#                            what claude was started with (m2_inbound.py, in the container), a
#                            hook that starts no process; at most one pingbus process (the keeper)
#   nobus <container>        (e) PINGBUS_MEMBERSHIP=off, no mount, no pingbus, no plugin or settings
#   cpu <plain> <nobus>      (c) the plain session's idle CPU is the --no-bus one's, within a point
#   image <container>        (d) the session's image carries the M2 entrypoint, at the launcher's
#                            REQUIRED_CONTAINER_VERSION
#   spike <spec>             (f) the H34 settings-reload spike's result is recorded in the spec
set -euo pipefail

#: The deployed bash launcher, read as text: claude-yolo while it is a script (before CCY
#: 3.99.14.0, or rolled back), else claude-yolo.bash beside the Go front that claude-yolo became.
LAUNCHER=/var/local/claude-yolo/claude-yolo
[[ -r "${LAUNCHER}" ]] || { echo "ERROR: ${LAUNCHER} is missing or unreadable: re-run play-claude-yolo.yml" >&2; exit 1; }
[[ "$(head -c 2 "${LAUNCHER}")" == '#!' ]] || LAUNCHER=/var/local/claude-yolo/claude-yolo.bash
readonly LAUNCHER
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
#: The inbound probe, relative to the checkout, which a ccy container sees at /workspace.
readonly PROBE_REL="CLAUDE/Plan/${HERE#*/CLAUDE/Plan/}/m2_inbound.py"
#: The bus-process probe, likewise.
readonly PROCS_REL="CLAUDE/Plan/${HERE#*/CLAUDE/Plan/}/m2_procs.py"
# The checkout's top, by PlanScriptStandards R1's bounded walk.
repoRoot="${HERE}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    [[ ! -e "${repoRoot}/.git" ]] || { echo "ERROR: no ansible.cfg between ${HERE} and the repo root ${repoRoot}" >&2; exit 1; }
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { echo "ERROR: no ansible.cfg above ${HERE}" >&2; exit 1; }
readonly repoRoot

fail() {
    printf '[FAIL] %s\n' "$*" >&2
    exit 1
}
ok() { printf '[OK] %s\n' "$*"; }

readonly M7_CHECK=(python3 -I "${repoRoot}/CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/m7_check.py")
# names <want> <command> [<argument>...] — Plan 00201's m7_check <command>, read from this
# checkout's product sources: exactly <want> non-empty lines, into NAMES.
names() {
    local text
    text="$("${M7_CHECK[@]}" "${@:2}")" || fail "m7_check ${*:2} could not read the product (above)"
    mapfile -t NAMES <<<"${text}"
    if [[ "${#NAMES[@]}" -ne "$1" ]] || printf '%s\n' "${NAMES[@]}" | grep -qx ''; then
        fail "m7_check $2 printed ${text}, want $1 non-empty lines"
    fi
}
# The names the product owns, never copied: seat.py's session home and its settings file,
# membership.py's mount and desired file, and seat.py's active flag and inner status line.
names 4 session-paths
readonly HOME_IN="${NAMES[0]}" SETTINGS_IN="${NAMES[1]}" MOUNT="${NAMES[2]}" DESIRED="${NAMES[3]}"
names 3 session-names
readonly ACTIVE="${NAMES[0]}" INNER="${NAMES[1]}"
# membership.py's variable that carries a session's membership, and its value for none.
names 1 product-value helpers/pingbus/membership.py MEMBERSHIP_VAR
readonly MEMBERSHIP_VAR="${NAMES[0]}"
names 1 product-value helpers/pingbus/membership.py MEMBERSHIP_OFF
readonly MEMBERSHIP_OFF="${NAMES[0]}"

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

# cmd_selinux <container> — H1's relabel, proved by what holds in Enforcing and Permissive
# alike: the bind asks for the private relabel (Z), and the mount's directory and file on the
# host (the same inodes the container sees) carry container_file_t at the container's own MCS
# level, not user_tmp_t. Enforcing also proves the container reads through it. A container run
# with labelling disabled (an SSH key forwarded from the agent) is unconfined: the launcher asks
# no relabel, so its bind must carry none.
cmd_selinux() {
    local ctr="${1:?selinux needs the container}" mode json opts process src path context
    mode="$(getenforce)"
    [[ "${mode}" != Disabled ]] || fail "SELinux is Disabled on this host: there is no label to prove (H1)"
    json="$(podman inspect "${ctr}")"
    opts="$(jq -r --arg m "${MOUNT}" '.[0] | ([.HostConfig.Binds[]? | split(":") | select(.[1] == $m) | .[2] // ""]
        + [.Mounts[]? | select(.Destination == $m) | (.Options // []) | join(",")]) | join(",")' <<<"${json}")"
    process="$(jq -r '.[0].ProcessLabel // ""' <<<"${json}")"
    src="$(jq -r --arg m "${MOUNT}" '.[0].Mounts[]? | select(.Destination == $m) | .Source' <<<"${json}")"
    [[ -n "${src}" ]] || fail "(H1) nothing is mounted at ${MOUNT}"
    if [[ -z "${process}" ]]; then
        [[ ",${opts}," != *,Z,* ]] || fail "(H1) the container runs with labelling disabled, yet its membership bind asks for a relabel: ${opts}"
        ok "(H1) this session runs unconfined (labelling disabled, as for an SSH key forwarded from the agent), so its membership bind carries no relabel: ${opts:-no options}"
        return 0
    fi
    [[ ",${opts}," == *,Z,* ]] || fail "(H1) the membership bind does not ask for the private relabel Z: ${opts:-no options}"
    for path in "${src}" "${src}/${DESIRED}"; do
        context="$(stat -c %C -- "${path}")"
        [[ "$(cut -d: -f3 <<<"${context}")" == container_file_t ]] \
            || fail "(H1) ${path} is labelled ${context}, not container_file_t: the relabel did not happen"
        [[ "$(cut -d: -f4- <<<"${context}")" == "$(cut -d: -f4- <<<"${process}")" ]] \
            || fail "(H1) ${path} is labelled ${context}, not at the container's own level (${process}): not the private relabel"
    done
    ok "(H1) the membership bind asks for Z (${opts}); its directory and file are ${context}, the container's own level (${process})"
    if [[ "${mode}" == Enforcing ]]; then
        podman exec "${ctr}" cat "${MOUNT}/${DESIRED}" >/dev/null || fail "(H1) SELinux is Enforcing and the container cannot read its membership"
        ok "(H1) SELinux is Enforcing, and the container reads its membership through the relabel"
    else
        ok "(H1) SELinux is ${mode} on this host, so enforcement was not exercised: the labels above are what Enforcing would judge"
    fi
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
    local ctr="${1:?plain needs the container}" json settings out guard rc=0
    json="$(podman exec "${ctr}" cat "${MOUNT}/${DESIRED}")"
    [[ "$(jq -r '.v, (.seats | length)' <<<"${json}" | paste -sd' ')" == "1 0" ]] \
        || fail "the plain session's membership is not version 1 with no seat: ${json}"
    ok "(b) the membership reads from inside: ${json}"
    write_fails "${ctr}" "(b) touch ${MOUNT}/x" touch "${MOUNT}/x"
    write_fails "${ctr}" "(H3) a write over ${DESIRED}" sh -c "printf x > ${MOUNT}/${DESIRED}"
    write_fails "${ctr}" "(H3) mount -o remount,rw" mount -o remount,rw "${MOUNT}"
    [[ "$(podman exec "${ctr}" cat "${MOUNT}/${DESIRED}")" == "${json}" ]] || fail "the membership changed"
    if podman exec "${ctr}" test -e "${HOME_IN}/${ACTIVE}"; then fail "the plain session has the active flag"; fi
    settings="$(podman exec "${ctr}" cat "${SETTINGS_IN}")"
    # The guard is this checkout's seat.py STATUS_LINE_GUARD, read from it rather than copied.
    guard="$(python3 -I -c 'import sys; sys.path.insert(0, sys.argv[1]); from helpers.pingbus import seat; print(seat.STATUS_LINE_GUARD)' "${repoRoot}")" \
        || fail "STATUS_LINE_GUARD cannot be read from ${repoRoot}/helpers/pingbus/seat.py (above)"
    [[ -n "${guard}" ]] || fail "${repoRoot}/helpers/pingbus/seat.py has an empty STATUS_LINE_GUARD"
    jq -e --arg guard "${guard}" '.statusLine.command == $guard' <<<"${settings}" >/dev/null \
        || fail "the session's status line is not the shell guard this checkout's seat.py writes (H34), ${guard}: ${settings}"
    ok "(b) no active flag; the status line is the guard, so it runs the session's own: $(podman exec "${ctr}" cat "${HOME_IN}/${INNER}")"
    # Decision 1 (H34): Claude Code binds its inbox socket in every session, so the socket
    # proves nothing; what claude was started with decides whether a message on it is a turn.
    # m2_inbound.py reads that from /proc in the container (the checkout is /workspace there).
    out="$(podman exec "${ctr}" python3 -I "/workspace/${PROBE_REL}" inbound /proc /etc/claude-code "${SETTINGS_IN}")" \
        || fail "(b) the plain session can be written a turn through its inbox socket (the reason is above)"
    ok "(b) inbound refused at launch: ${out//$'\n'/; }"
    out="$(podman exec "${ctr}" sh -c 'd="${XDG_RUNTIME_DIR:-/run/user/$(id -u)}/cc-socks"; [ ! -d "$d" ] || find "$d" -name "*.sock"')"
    ok "(b) Claude Code's own inbox socket, present in every session and refusing here: ${out:-none}"
    # (c) the UserPromptSubmit hook as the plugin registers it, with nothing on PATH: had the
    # guard not ended it in the shell, `exec pingbus` would fail (127).
    local hook
    hook="$(podman exec "${ctr}" jq -r '.hooks.UserPromptSubmit[0].hooks[0].command' \
        /opt/claude-yolo/optional/agent-bus/plugin/pingbus/hooks/hooks.json)"
    out="$(podman exec -e PINGBUS_HOME="${HOME_IN}" -e PATH=/nonexistent "${ctr}" /bin/sh -c "${hook}" </dev/null 2>&1)" || rc=$?
    [[ "${rc}" -eq 0 && -z "${out}" ]] || fail "(c) a prompt's hook in the plain session did not end in the shell (exit ${rc}): ${out}"
    ok "(c) a prompt's hook ends in the shell: no python3, no pingbus, an empty answer"
    # A bus process is its program, never a word in its argv: claude's own names the plugin
    # directory .../plugin/pingbus (m2_procs.py).
    out="$(podman exec "${ctr}" python3 -I "/workspace/${PROCS_REL}" at-most-keeper /proc)" \
        || fail "(c) the plain session runs bus processes other than one keeper (above)"
    ok "(c) the plain session's only bus process is the keeper: ${out:-none}"
}

cmd_nobus() {
    local ctr="${1:?nobus needs the container}" mounts procs
    [[ "$(env_of "${ctr}" "${MEMBERSHIP_VAR}")" == "${MEMBERSHIP_OFF}" ]] || fail "(e) ${MEMBERSHIP_VAR} is not ${MEMBERSHIP_OFF}"
    mounts="$(podman inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "${ctr}")"
    if grep -qxF "${MOUNT}" <<<"${mounts}"; then fail "(e) the --no-bus session has the membership mount"; fi
    if podman exec "${ctr}" test -e /usr/local/bin/pingbus; then fail "(e) the --no-bus session has pingbus on PATH"; fi
    procs="$(podman exec "${ctr}" ps -eo args)"
    if grep -qE -- '--plugin-dir|--settings' <<<"${procs}"; then
        fail "(e) the --no-bus session loads the plugin or settings: $(grep -E -- '--plugin-dir|--settings' <<<"${procs}")"
    fi
    podman exec "${ctr}" python3 -I "/workspace/${PROCS_REL}" none /proc >/dev/null \
        || fail "(e) the --no-bus session runs a bus process (above)"
    ok "(e) --no-bus: ${MEMBERSHIP_VAR}=${MEMBERSHIP_OFF}, no mount, no pingbus, no plugin, no settings, no keeper"
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
    [[ -n "${required}" ]] || fail "(d) ${LAUNCHER} names no REQUIRED_CONTAINER_VERSION"
    [[ "${label}" == "${required}" ]] || fail "(d) the image ${image} is ${label}, the launcher requires ${required}"
    podman exec "${ctr}" grep -qF -- "\${${MEMBERSHIP_VAR}-" /usr/local/bin/entrypoint.sh \
        || fail "(d) the image's entrypoint does not read ${MEMBERSHIP_VAR}"
    ok "(d) the image is ${label}, which the launcher requires: an older image is rebuilt before any launch, and an older launcher (no ${MEMBERSHIP_VAR}) is refused by this entrypoint"
}

cmd_spike() {
    local line
    line="$(grep -m1 'M2 spike result' "${1:?spike needs the spec}")" || fail "(f) the H34 spike's result is not recorded in $1"
    ok "(f) ${line#"${line%%[![:space:]-]*}"}"
}

case "${1:-}" in
selinux | plain | nobus | cpu | image | spike)
    sub="$1"
    shift
    "cmd_${sub}" "$@"
    ;;
*)
    printf 'usage: m2-check.bash selinux|plain|nobus|cpu|image|spike [args]\n' >&2
    exit 64
    ;;
esac
