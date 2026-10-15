# shellcheck shell=bash
# _acceptance-u23.inc.bash — Plan 00161's acceptance.bash slice U23 (milestone M3a, DESIGN.md
# section 12): an LXC, a docker and a VM member join the acceptance team on this host and
# exchange a ping with the desktop's member a. A separate file for the reason
# _acceptance-steps.inc.bash is one. Sourced, never executed: no shell options, no `exit`;
# every function returns non-zero on its first failure. A check returns 0 PASS, 1 FAIL,
# 2 COULD NOT ESTABLISH, or 3 SKIPPED-NEEDS-OWNER (after needs_owner names the owner's step).
#
# EACH MEMBER FOLLOWS ITS README (files/opt/claude-yolo/optional/agent-bus/README.<type>) step
# by step, so the run proves the README rather than working around it: (1) the kit copied to
# /usr/local/share/agent-bus/kit, its two commands linked into /usr/local/bin; (2) the member's
# bridge subnet in allow_from; (3) `HOOKS_DAEMON_HOSTNAME=<role> pingbus suggest-handle` as the
# agent user in its checkout, whose --type must be the member's real encapsulation; (4)
# add-member with exactly those arguments, --role=worker and --address=<bus_ip>, the bundle
# extracted by the agent user (so it owns the 0700 directory and 0600 files), the run
# directory's token removed; (5) the env file, or for docker the container environment;
# (6) `pingbus config check`. Starting Claude through agent-bus-claude is U20's to prove.
# Each README's requirement, gh logged in as the agent user, is met by this harness, which sets
# the member up: gh installed (dnf, or apt for docker), then `gh auth login --with-token` with
# the desktop user's token on stdin (read_gh_login), kept in the agent's home in the member
# and gone with it at teardown.
#
# THE MEMBERS. lxc: a throwaway container, agent-bus-acceptance-u23, from lxc-create's download
# template (Fedora, this host's release: an image download), python3 and git-core installed by
# dnf inside it. docker: a throwaway container of the same name from python:3.13-bookworm
# (pinned by digest, pulled here; it has git, which suggest-handle needs) on docker's default
# bridge; only rootful Docker counts, not the podman-docker shim. With LXC or rootful Docker
# missing, that member's check is SKIPPED-NEEDS-OWNER. vm: a throwaway guest of the vmtest lab
# (Plan 00110, play-vm-test-lab.yml): `vmtest ensure-base server-fast` (the Fedora Cloud base,
# its published image verified by checksum and kept fresh by the lab's gate), then `vmtest
# member-up agent-bus-acceptance-u23 server-fast`, a rootless qemu:///session guest on passt
# driven by `vmtest member-ssh` with the lab's own key, git-core installed by dnf inside it;
# `vmtest member-down` removes guest, overlay and directory. No owner step: without the lab
# deployed the VM leg FAILS, naming the play (this plan's deploy.bash runs it). The agent is
# the user agent-bus-u23, made here, gone with its container or guest.
#
# ALLOW_FROM is discovered, never written down: for lxc and docker, the source address each
# member's kernel would use towards <bus_ip>, the host network holding it (which must be a
# bridge, the READMEs' route), added to the team file before agent-bus-install applies the
# team again. The vm member adds nothing, EXPECTED BUT NOT YET MEASURED: passt carries its
# connections out through sockets in the host's own network namespace, as pasta does for a
# ccy container, and probe H1 measured (for pasta only) that the homeserver then sees <bus_ip>
# itself, a listen address the installer always admits (DESIGN.md section 3.3). u23_vm_check
# records it: it samples this host's sockets towards the bus port during the vm member's join
# and ping, and FAILS unless every one passt held left from <bus_ip>, naming the source if not.
#
# Reads acceptance.bash's and _acceptance-steps.inc.bash's globals and functions: TEAM,
# BUS_ADDRESS, TEAM_FILE, MEMBER_DIR, CHECK, AGENT_BUS, PINGBUS, OWNER_NEEDS, A_HANDLE,
# REF_PATH, REF_BRANCH, SENT_ID, member_run, send_ping, show, expect_status, evidence, install_team, and the
# plan library's PLAN_RUN_DIR and PLAN_REPO_ROOT. Defines needs_owner for every slice.

readonly U23_NAME="agent-bus-acceptance-u23"
readonly U23_AGENT="agent-bus-u23"
readonly U23_HOME="/home/${U23_AGENT}"
#: The agent's checkout: suggest-handle derives --repo from its name (no git remote).
readonly U23_CHECKOUT="${U23_HOME}/acceptance"
#: HOOKS_DAEMON_HOSTNAME, the handle's <host>, as M1's members.
readonly U23_ROLE="acceptance"
readonly U23_KIT_PARENT="/usr/local/share/agent-bus"
#: python 3.13 on Debian bookworm (it has git and useradd), its multi-arch index digest.
readonly U23_DOCKER_IMAGE="docker.io/library/python@sha256:073ffebb96ae4d0ed73ccad59f08c47bd84af8a79f5258130f8458ac50ad9ff4"
readonly U23_DOCKER_PINGBUS_HOME="${U23_HOME}/pingbus"
readonly U23_GUEST_PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"
readonly U23_KINDS=(lxc docker vm)
#: The vmtest lab's CLI and state, as play-vm-test-lab.yml deploys them for the desktop user.
readonly U23_VMTEST="${HOME}/.local/bin/vmtest"
readonly U23_VMTEST_LAB_ENV="${HOME}/.local/share/vmtest/lab.env"
#: The lab's Fedora Cloud server base (vars/vm-test-scenarios.yml).
readonly U23_VM_BASE="server-fast"
readonly U23_VM_PLAY="playbooks/imports/optional/common/play-vm-test-lab.yml"

# The agent user's commands source its env file once it exists (README step 5), as
# agent-bus-claude does; before that (suggest-handle) the role is passed inline (step 3).
U23_AGENT_ENV="$(
    cat <<'EOF'
if [ -f "$HOME/.config/pingbus/env" ]; then set -a; . "$HOME/.config/pingbus/env"; set +a; fi; exec "$@"
EOF
)"
readonly U23_AGENT_ENV

#: README step 1's copy: the kit tar on stdin unpacked under argv[1], every directory set to
#: 0755 explicitly. tarfile's "data" filter creates directories with the caller's umask, and
#: lxc-attach under sudo inherits the host run's: under umask 077 the kit was 0700 and the
#: agent user could not run pingbus (exit 126). Files keep the installer's modes from the tar.
U23_KIT_UNPACK_PY="$(
    cat <<'EOF'
import os, sys, tarfile
parent = sys.argv[1]
os.makedirs(parent, exist_ok=True)
os.chmod(parent, 0o755)
with tarfile.open(fileobj=sys.stdin.buffer, mode="r|") as tar:
    tar.extractall(parent, filter="data")
kit = os.path.join(parent, "kit")
if not os.path.isfile(os.path.join(kit, "pingbus")):
    sys.exit("the kit tar made no " + os.path.join(kit, "pingbus"))
for top, dirs, files in os.walk(kit):
    os.chmod(top, 0o755)
EOF
)"
readonly U23_KIT_UNPACK_PY

# Run as root in a guest: refuse what is not this acceptance's, then make the agent user and
# its checkout, unpack the kit (stdin, a tar of kit/), and link its commands into
# /usr/local/bin (README step 1). Arguments: agent, home, checkout, kit parent, unpack script.
U23_GUEST_PREPARE="$(
    cat <<'EOF'
set -eu
agent=$1 home=$2 checkout=$3 parent=$4 unpack=$5
for path in "$parent" /usr/local/bin/pingbus /usr/local/bin/agent-bus-claude "$home"; do
    if [ -e "$path" ] || [ -L "$path" ]; then
        echo "$path already exists in this guest and is not this acceptance's: use a guest without an agent-bus kit" >&2
        exit 1
    fi
done
if getent passwd "$agent" >/dev/null; then
    echo "user $agent already exists in this guest" >&2
    exit 1
fi
useradd -m -d "$home" -U "$agent"
install -d -o "$agent" -g "$agent" -m 0700 "$checkout"
python3 -I -c "$unpack" "$parent"
ln -s "$parent/kit/pingbus" /usr/local/bin/pingbus
ln -s "$parent/kit/agent-bus-claude" /usr/local/bin/agent-bus-claude
echo "agent user $agent, the kit at $parent/kit and its two links are in place" >&2
EOF
)"
readonly U23_GUEST_PREPARE

#: The member's own source address towards the bus address (no packet is sent).
readonly U23_SOURCE_PY='import socket, sys; s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.connect((sys.argv[1], 9)); print(s.getsockname()[0])'
#: The agent extracts its bundle into its PINGBUS_HOME, as M1's members do.
readonly U23_PLACE_PY='import os, sys, tarfile; os.makedirs(sys.argv[1], mode=0o700, exist_ok=True); tarfile.open(fileobj=sys.stdin.buffer, mode="r|").extractall(sys.argv[1], filter="data")'
#: The env file, created 0600 by the agent, never over an existing one.
readonly U23_ENV_PY='import os, sys; fd = os.open(sys.argv[1], os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600); os.write(fd, sys.stdin.buffer.read()); os.close(fd)'

# needs_owner <step> — a check that cannot run without something only the owner can provide
# names it here, then returns 3; acceptance.bash's finish_needs_owner ends the run on
# OWNER_NEEDS after the teardown. Any slice's check may use it.
needs_owner() {
    OWNER_NEEDS+=("$1")
    printf '[SKIPPED-NEEDS-OWNER] %s\n' "$1" >&2
}

declare -A U23_SKIP=()    # kind -> what the owner must provide
declare -A U23_PRESENT=() # kind -> 1 once this run may have made something of it
declare -A U23_SUBNET=()  # kind -> the bridge network its member reaches the bus through
declare -A U23_HANDLE=()  # kind -> its member's handle

# ── running commands inside a member ────────────────────────────────────────────────────

# u23_vm_ssh <argv...> — argv in the vm member as its cloud user, quoted for that user's
# login shell (bash on Fedora); stdin passed through; returns argv's status.
u23_vm_ssh() {
    local quoted
    printf -v quoted '%q ' "$@"
    "${U23_VMTEST}" member-ssh "${U23_NAME}" "${quoted}"
}

# u23_lxc_attach <argv...> — argv in the LXC member, its stdin, stdout and stderr all pipes.
# lxc-attach makes the files it is handed as stdio the container root's own, mode 0600: the
# run directory's .out and .err would be unreadable to the owner, and a shared input such as
# /dev/null would be re-owned. Returns argv's status.
u23_lxc_attach() {
    local -a piped
    {
        (
            cat | sudo -n lxc-attach -n "${U23_NAME}" --clear-env -- "$@" 3>&- | cat 3>&-
            exit "${PIPESTATUS[1]}"
        ) 2>&1 1>&3 3>&- | cat >&2 3>&-
        piped=("${PIPESTATUS[@]}")
    } 3>&1
    if [[ "${piped[1]}" -ne 0 ]]; then
        printf '[FAIL] u23_lxc_attach: copying the member'\''s stderr failed\n' >&2
        return 1
    fi
    return "${piped[0]}"
}

# u23_exec <kind> <root|agent> <label> <stdin-file> <argv...> — argv inside the member, as
# root or as the agent user in its checkout. Its stdout and stderr are kept as <label>.out
# and <label>.err in the member's run directory. Returns the command's status.
u23_exec() {
    local kind="$1" who="$2" label="$3" input="$4" dir="${MEMBER_DIR}/$1"
    shift 4
    local -a inner=(/usr/bin/env -i "PATH=${U23_GUEST_PATH}" HOME=/root)
    if [[ "${who}" == "agent" ]]; then
        inner+=(runuser -u "${U23_AGENT}" -- env -i -C "${U23_CHECKOUT}" "PATH=${U23_GUEST_PATH}"
            "HOME=${U23_HOME}" sh -c "${U23_AGENT_ENV}" sh)
    fi
    mkdir -p -- "${dir}" || return 1
    case "${kind}/${who}" in
        docker/root)
            docker exec -i -u 0 "${U23_NAME}" "$@" ;;
        docker/agent)
            docker exec -i -u "${U23_AGENT}" -w "${U23_CHECKOUT}" "${U23_NAME}" "$@" ;;
        lxc/*)
            u23_lxc_attach "${inner[@]}" "$@" ;;
        vm/*)
            u23_vm_ssh sudo -n "${inner[@]}" "$@" ;;
        *)
            printf '[FAIL] u23_exec: no member kind %s\n' "${kind}" >&2
            return 1
            ;;
    esac <"${input}" >"${dir}/${label}.out" 2>"${dir}/${label}.err"
}

# u23_step <kind> <root|agent> <label> <stdin-file> <want-status> <argv...> — u23_exec, its
# output shown in the log, FAIL unless it exits <want-status>.
u23_step() {
    local kind="$1" who="$2" label="$3" input="$4" want="$5" status=0
    shift 5
    u23_exec "${kind}" "${who}" "${label}" "${input}" "$@" || status=$?
    show "${kind}" "${label}"
    expect_status "${kind}" "${label}" "${status}" "${want}"
}

# ── removing what a run made ─────────────────────────────────────────────────────────────

# u23_remove <kind> — remove the member's container or guest, and the run directory's bundle
# copies. Nothing there is not an error.
u23_remove() {
    local kind="$1" names
    case "${kind}" in
        lxc)
            if command -v lxc-ls >/dev/null; then
                names="$(sudo -n lxc-ls -1)" || return 1
                if grep -Fxq -- "${U23_NAME}" <<<"${names}"; then
                    sudo -n lxc-destroy -n "${U23_NAME}" -f || return 1
                    printf '==> destroyed the LXC container %s\n' "${U23_NAME}"
                fi
            fi
            ;;
        docker)
            if command -v docker >/dev/null; then
                names="$(docker ps -a --filter "name=^${U23_NAME}\$" --format '{{.Names}}')" || return 1
                if [[ "${names}" == "${U23_NAME}" ]]; then
                    docker rm -f "${U23_NAME}" || return 1
                    printf '==> removed the docker container %s\n' "${U23_NAME}"
                fi
            fi
            ;;
        vm)
            # Without the lab's CLI no member can have been made.
            if [[ -x "${U23_VMTEST}" ]]; then
                "${U23_VMTEST}" member-down "${U23_NAME}" || return 1
            fi
            ;;
    esac
    rm -f -- "${MEMBER_DIR}/${kind}/bundle.tar" "${MEMBER_DIR}/${kind}/${TEAM}/token" || return 1
    U23_PRESENT[${kind}]=0
}

# u23_teardown <label> — every U23 member, whatever state a run left it in.
u23_teardown() {
    local kind
    for kind in "${U23_KINDS[@]}"; do
        u23_remove "${kind}" || return 1
    done
    rm -f -- "${PLAN_RUN_DIR}/u23-kit.tar" || return 1
    printf '==> no U23 member remains (%s)\n' "$1"
}

# On the way out of any run that stopped early (plan_on_cleanup).
u23_teardown_after_stop() {
    local kind
    for kind in "${U23_KINDS[@]}"; do
        if [[ "${U23_PRESENT[${kind}]:-0}" -eq 1 ]]; then
            printf '==> the run stopped with U23 members in place: removing them\n'
            if ! u23_teardown after-stop; then
                printf '[FAIL] a U23 member could not be removed; run acceptance.bash again, which removes them first\n' >&2
                return 1
            fi
            return 0
        fi
    done
}

# ── preparing each member ────────────────────────────────────────────────────────────────

# u23_kit_tar — the host's member kit as one tar (kit/...), what README step 1 copies in. The
# installer leaves the kit world-readable, so no root is needed to read it.
u23_kit_tar() {
    if [[ ! -s "${PLAN_RUN_DIR}/u23-kit.tar" ]]; then
        tar -C "${U23_KIT_PARENT}" -c -f "${PLAN_RUN_DIR}/u23-kit.tar" kit || return 1
    fi
}

# u23_guest_prepare <kind> — the agent user, its checkout and the kit, inside the member.
u23_guest_prepare() {
    u23_kit_tar || return 1
    u23_step "$1" root prepare "${PLAN_RUN_DIR}/u23-kit.tar" 0 sh -c "${U23_GUEST_PREPARE}" sh \
        "${U23_AGENT}" "${U23_HOME}" "${U23_CHECKOUT}" "${U23_KIT_PARENT}" "${U23_KIT_UNPACK_PY}"
}

# u23_bridge <kind> — the network the member reaches <bus_ip> through: its source address,
# the host network holding it, which must be a bridge (README step 2).
u23_bridge() {
    local kind="$1" source found dev cidr
    u23_step "${kind}" root source /dev/null 0 python3 -I -c "${U23_SOURCE_PY}" "${BUS_ADDRESS}" || return 1
    source="$(<"${MEMBER_DIR}/${kind}/source.out")"
    ip -j -4 addr show >"${MEMBER_DIR}/${kind}/host-addresses.json" || return 1
    found="$("${CHECK[@]}" host-subnet "${source}" <"${MEMBER_DIR}/${kind}/host-addresses.json")" || return 1
    IFS=$'\t' read -r dev cidr <<<"${found}"
    if [[ ! -d "/sys/class/net/${dev}/bridge" ]]; then
        printf '[FAIL] the %s member (%s) reaches the bus through %s, which is not a bridge of this host; README.%s routes it through one\n' \
            "${kind}" "${source}" "${dev}" "${kind}" >&2
        return 1
    fi
    U23_SUBNET[${kind}]="${cidr}"
    printf '==> the %s member is %s, on bridge %s (%s)\n' "${kind}" "${source}" "${dev}" "${cidr}"
}

u23_prepare_lxc() {
    local version arch address="" tries state
    if ! command -v lxc-create >/dev/null || [[ ! -x /usr/share/lxc/templates/lxc-download ]]; then
        U23_SKIP[lxc]="LXC with its download template: run playbooks/imports/play-lxc-install-config.yml (playbook-main.yml imports it)"
        return 0
    fi
    version="$(awk -F= '$1 == "VERSION_ID" { gsub(/"/, "", $2); print $2 }' /etc/os-release)" || return 1
    case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64) arch=arm64 ;;
        *) arch="$(uname -m)" ;;
    esac
    U23_PRESENT[lxc]=1 # before the attempt: a half-made container still needs removing
    sudo -n lxc-create -n "${U23_NAME}" -t download -- --dist fedora --release "${version}" --arch "${arch}" || return 1
    sudo -n lxc-start -n "${U23_NAME}" -d || return 1
    sudo -n lxc-wait -n "${U23_NAME}" -s RUNNING -t 30 || return 1
    for ((tries = 0; tries < 60; tries++)); do
        # A container that stopped will never get an address: name its state now.
        state="$(sudo -n lxc-info -n "${U23_NAME}" -sH)" || return 1
        if [[ "${state}" != RUNNING ]]; then
            printf '[FAIL] the LXC container %s is %s while waiting for its IPv4 address\n' "${U23_NAME}" "${state}" >&2
            return 1
        fi
        address="$(sudo -n lxc-info -n "${U23_NAME}" -iH | awk '/^[0-9]+\./ { print; exit }')" || return 1
        if [[ -n "${address}" ]]; then
            break
        fi
        sleep 1
    done
    if [[ -z "${address}" ]]; then
        printf '[FAIL] the LXC container %s has no IPv4 address after 60 s (lxc-net DHCP)\n' "${U23_NAME}" >&2
        return 1
    fi
    printf '==> LXC container %s (Fedora %s) is running at %s\n' "${U23_NAME}" "${version}" "${address}"
    u23_step lxc root dnf-install /dev/null 0 dnf -y --setopt=install_weak_deps=False install \
        python3 git-core util-linux-core shadow-utils systemd gh || return 1
    u23_guest_prepare lxc
}

u23_prepare_docker() {
    if [[ ! -x /usr/bin/dockerd ]] || ! command -v docker >/dev/null; then
        U23_SKIP[docker]="rootful Docker (dockerd and the docker CLI; the podman-docker shim is not docker): run playbooks/imports/optional/common/play-docker.yml"
        return 0
    fi
    docker pull --quiet "${U23_DOCKER_IMAGE}" || return 1
    U23_PRESENT[docker]=1 # before the attempt
    # README.docker step 5: the team, its PINGBUS_HOME and the role in the container environment.
    docker run -d --name "${U23_NAME}" --network bridge -e "PINGBUS_TEAMS=${TEAM}" \
        -e "PINGBUS_HOME=${U23_DOCKER_PINGBUS_HOME}" -e "HOOKS_DAEMON_HOSTNAME=${U23_ROLE}" \
        "${U23_DOCKER_IMAGE}" sleep infinity || return 1
    u23_step docker root apt-install /dev/null 0 sh -c 'apt-get update -qq && apt-get install -y -qq --no-install-recommends gh' || return 1
    u23_guest_prepare docker
}

# u23_prepare_vm — the lab's base made usable (built or refreshed only when its gate says so),
# a throwaway guest booted from it, git-core installed. Without the lab deployed this FAILS,
# naming the play: the guest is IaC, never something the owner hands over.
u23_prepare_vm() {
    if [[ ! -x "${U23_VMTEST}" ]] || [[ ! -r "${U23_VMTEST_LAB_ENV}" ]]; then
        printf '[FAIL] the vm member is a vmtest lab guest, and the lab is not deployed (no %s or %s): run %s (this plan'\''s deploy.bash runs it)\n' \
            "${U23_VMTEST}" "${U23_VMTEST_LAB_ENV}" "${U23_VM_PLAY}" >&2
        return 1
    fi
    "${U23_VMTEST}" ensure-base "${U23_VM_BASE}" || return 1
    U23_PRESENT[vm]=1 # before the attempt: a guest that failed part-way still needs removing
    "${U23_VMTEST}" member-up "${U23_NAME}" "${U23_VM_BASE}" || return 1
    u23_step vm root dnf-install /dev/null 0 dnf -y --setopt=install_weak_deps=False install git-core gh || return 1
    u23_guest_prepare vm
}

# u23_prepare <kind> — the member's container or guest, ready for its README's steps, and its
# bridge; or, with its engine missing, the owner step its check will report. The vm member
# is on passt and adds nothing to allow_from (the header says why).
u23_prepare() {
    local kind="$1"
    "u23_prepare_${kind}" || return 1
    if [[ -n "${U23_SKIP[${kind}]:-}" ]]; then
        printf '==> no %s member on this host: its check will report SKIPPED-NEEDS-OWNER\n' "${kind}"
        return 0
    fi
    if [[ "${kind}" == "vm" ]]; then
        printf '==> the vm member is on passt: expected to reach %s from this host'\''s own network namespace, so nothing joins allow_from for it (U23.vm records the source it really uses)\n' "${BUS_ADDRESS}"
        return 0
    fi
    u23_bridge "${kind}"
}

# u23_widen_team — README step 2: each member's bridge network joins the team's allow_from,
# and the team is applied again (the installer restarts the homeserver for the new filter).
u23_widen_team() {
    local kind
    local -a cidrs=()
    for kind in "${U23_KINDS[@]}"; do
        if [[ -n "${U23_SUBNET[${kind}]:-}" ]]; then
            cidrs+=("${U23_SUBNET[${kind}]}")
        fi
    done
    if [[ "${#cidrs[@]}" -eq 0 ]]; then
        printf '==> no U23 member to admit: the team stays as it is\n'
        return 0
    fi
    "${CHECK[@]}" allow-from "${TEAM_FILE}" "${cidrs[@]}" || return 1
    printf '==> team file %s:\n' "${TEAM_FILE}"
    cat -- "${TEAM_FILE}"
    install_team team-u23
}

# ── the U23 checks ───────────────────────────────────────────────────────────────────────

# u23_join <kind> — README steps 3 to 6 inside the member, and its first recv (the join).
u23_join() {
    local kind="$1" dir="${MEMBER_DIR}/$1" suggested home status=0
    local -a args=()
    u23_step "${kind}" agent suggest /dev/null 0 env "HOOKS_DAEMON_HOSTNAME=${U23_ROLE}" pingbus suggest-handle || return 1
    suggested="$("${CHECK[@]}" suggested-args "${dir}/suggest.out" "${kind}")" || return 1
    mapfile -t args <<<"${suggested}"
    sudo -n "${AGENT_BUS}" add-member "${TEAM}" "${args[@]}" --role=worker "--address=${BUS_ADDRESS}" \
        "--out=${dir}/${TEAM}" || return 1
    U23_HANDLE[${kind}]="$("${CHECK[@]}" handle "${dir}/${TEAM}/member.json")" || return 1
    printf '==> %s member %s (worker), added with suggest-handle'\''s %s\n' "${kind}" "${U23_HANDLE[${kind}]}" "${args[*]}"
    tar -c --no-recursion -f "${dir}/bundle.tar" -C "${dir}" "${TEAM}" "${TEAM}/member.json" "${TEAM}/token" || return 1
    if [[ "${kind}" == "docker" ]]; then
        home="${U23_DOCKER_PINGBUS_HOME}"
    else
        home="${U23_HOME}/.config/pingbus"
    fi
    u23_step "${kind}" agent place "${dir}/bundle.tar" 0 python3 -I -c "${U23_PLACE_PY}" "${home}" || status=1
    rm -f -- "${dir}/bundle.tar" "${dir}/${TEAM}/token" || return 1
    if [[ "${status}" -ne 0 ]]; then
        return 1
    fi
    if [[ "${kind}" != "docker" ]]; then
        printf 'PINGBUS_TEAMS=%s\nHOOKS_DAEMON_HOSTNAME=%s\n' "${TEAM}" "${U23_ROLE}" >"${dir}/env" || return 1
        u23_step "${kind}" agent env-file "${dir}/env" 0 python3 -I -c "${U23_ENV_PY}" "${home}/env" || return 1
    fi
    # README's requirement, met here by the harness that set the member up: its gh logged in
    # with the desktop user's token, on stdin (read_gh_login), gone with the member.
    u23_step "${kind}" agent gh-login <(gh_token_pipe) 0 gh auth login --with-token --hostname github.com || return 1
    u23_step "${kind}" agent config-check /dev/null 0 pingbus config check || return 1
    u23_step "${kind}" agent join /dev/null 3 pingbus recv
}

# u23_ping <kind> — the member waits; a (on the desktop) sends it `review`; its wait prints
# exactly that PING; it acks, and a's recv prints exactly that ack.
u23_ping() {
    local kind="$1" dir="${MEMBER_DIR}/$1" status=0 waitPid reviewId ackId handle="${U23_HANDLE[$1]}"
    u23_exec "${kind}" agent wait-review /dev/null pingbus wait --timeout 180 &
    waitPid=$!
    # On a failed send the waiter is left to end with its member, which the teardown removes.
    send_ping a "send-review-${kind}" review "${REF_PATH}" --branch "${REF_BRANCH}" --to "${handle}" || return $?
    reviewId="${SENT_ID}"
    wait "${waitPid}" || status=$?
    show "${kind}" wait-review
    expect_status "${kind}" wait-review "${status}" 0 || return 1
    "${CHECK[@]}" expect-ping "${dir}/wait-review.out" "${TEAM}" "${reviewId}" "${A_HANDLE}" review \
        "${REF_PATH}" - || return 1
    evidence "U23 ${kind}: review ${reviewId} from a received by the ${kind} member's wait: members/a/send-review-${kind}.out, members/${kind}/wait-review.out"
    u23_step "${kind}" agent send-ack /dev/null 0 pingbus send ack --re "${reviewId}" --to "${A_HANDLE}" || return 1
    ackId="$("${CHECK[@]}" sent-event "${dir}/send-ack.out" "${TEAM}")" || return 1
    status=0
    member_run a "recv-ack-${kind}" /dev/null "${PINGBUS}" recv || status=$?
    show a "recv-ack-${kind}"
    expect_status a "recv-ack-${kind}" "${status}" 0 || return 1
    "${CHECK[@]}" expect-ping "${MEMBER_DIR}/a/recv-ack-${kind}.out" "${TEAM}" "${ackId}" "${handle}" \
        ack - "${reviewId}" || return 1
    evidence "U23 ${kind}: ack ${ackId} from the ${kind} member received by a's recv: members/${kind}/send-ack.out, members/a/recv-ack-${kind}.out"
}

# u23_sample_bus_sockets <port> <file> — every 0.2 s, for at most 400 s (the join, the
# 180 s wait and the acks fit inside it), both ends of this host's TCP connections on the
# bus port. A closed connection keeps a TIME-WAIT for 60 s on whichever end closed first,
# so sampling both ends catches even a short one. Ended by u23_vm_check's kill.
u23_sample_bus_sockets() {
    local port="$1" out="$2" i
    for ((i = 0; i < 2000; i++)); do
        ss -tnH state all "( dport = :${port} or sport = :${port} )" >>"${out}" || return 1
        sleep 0.2
    done
}

# u23_vm_check — the vm member's join and ping, with the evidence for README.vm's passt
# route recorded either way: the client address of each connection to the bus port, among
# which the bus address must be (passt's). A failed join then says whether the route was why.
u23_vm_check() {
    local port sampler status=0 sampled=0 samples="${MEMBER_DIR}/vm/bus-sockets.txt" sources
    port="$("${CHECK[@]}" team-port "${TEAM_FILE}")" || return 1
    mkdir -p -- "${MEMBER_DIR}/vm" || return 1
    : >"${samples}" || return 1
    u23_sample_bus_sockets "${port}" "${samples}" &
    sampler=$!
    { u23_join vm && u23_ping vm; } || status=$?
    kill "${sampler}" || sampled=$?
    wait "${sampler}" || sampled=$?
    # 143 is the sampler ended by that kill; anything else is ss failing, or it ran out.
    if [[ "${sampled}" -ne 0 && "${sampled}" -ne 143 ]]; then
        printf '[FAIL] sampling the sockets towards the bus port failed (%s)\n' "${sampled}" >&2
        return 1
    fi
    printf '==> connections to port %s while the vm member ran, as this host saw them (state, client):\n' "${port}"
    if ! sources="$("${CHECK[@]}" passt-sources "${BUS_ADDRESS}" "${port}" "${samples}")"; then
        printf '%s\n' "${sources}"
        return 1
    fi
    printf '%s\n' "${sources}"
    if [[ "${status}" -eq 0 ]]; then
        evidence "U23 vm: on passt the guest's connections reached the homeserver from ${BUS_ADDRESS} (nothing in allow_from for it): members/vm/bus-sockets.txt"
    fi
    return "${status}"
}

# u23_check <kind> — one encapsulation's check: SKIPPED-NEEDS-OWNER without its engine (lxc,
# docker), else the member joins by its README and exchanges a review and an ack with a.
u23_check() {
    local kind="$1"
    if [[ -n "${U23_SKIP[${kind}]:-}" ]]; then
        needs_owner "U23 ${kind} member: ${U23_SKIP[${kind}]}"
        return 3
    fi
    if [[ "${kind}" == "vm" ]]; then
        u23_vm_check
        return $?
    fi
    u23_join "${kind}" || return 1
    u23_ping "${kind}"
}
