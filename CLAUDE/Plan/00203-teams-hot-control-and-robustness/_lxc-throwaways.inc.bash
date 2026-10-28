# shellcheck shell=bash
# _lxc-throwaways.inc.bash — the legs of lxc-throwaways-up.bash and lxc-throwaways-down.bash
# (Task 3.23), and what they share: the two throwaway containers' names, the guard that refuses
# any other name, and running a command inside one. Sourced, never executed: no shell options,
# no `exit`; every function returns non-zero on its first failure.
#
# The way into a container is Plan 00161's (_acceptance-u23.inc.bash, u23_lxc_attach): every
# stdio of lxc-attach is a pipe, because lxc-attach makes the files it is handed the container
# root's own, mode 0600, and would re-own a shared input such as /dev/null.

#: Every container these scripts make or remove starts with this; no other name is touched.
readonly LT_PREFIX="t00203-"
#: The coordinator's container first: Task 3.23 adds it first, the worker second.
readonly LT_NAMES=("${LT_PREFIX}coord" "${LT_PREFIX}worker")
#: The agent user in each container and its checkout. With no git remote, the handle's repo is
#: the directory's name (helpers/agent_bus_lxc/derive.py), so the two members are seats 1 and 2
#: of the same repo.
readonly LT_AGENT="agent"
readonly LT_AGENT_HOME="/home/${LT_AGENT}"
readonly LT_CHECKOUT="${LT_AGENT_HOME}/t00203-work"
readonly LT_GUEST_PATH="/usr/local/bin:/usr/bin:/bin:/usr/local/sbin:/usr/sbin:/sbin"

# lt_name_ok <name> — refuse a name that is not one of this task's throwaways.
lt_name_ok() {
    if [[ "$1" != "${LT_PREFIX}"?* ]]; then
        printf '[FAIL] %s does not start with %s: these scripts make and remove only their own throwaway containers\n' \
            "$1" "${LT_PREFIX}" >&2
        return 1
    fi
}

# lt_exists <name> — 0 when root's LXC has a container of that name, 1 when not, 2 when the
# list could not be read.
lt_exists() {
    local names
    names="$(sudo -n lxc-ls -1)" || return 2
    grep -Fxq -- "$1" <<<"${names}"
}

# lt_none_left — every throwaway is absent: up starts only from nothing, and down ends there.
lt_none_left() {
    local name found
    for name in "${LT_NAMES[@]}"; do
        found=0
        lt_exists "${name}" || found=$?
        case "${found}" in
            0)
                printf '[FAIL] the LXC container %s exists (lxc-throwaways-down.bash removes it)\n' "${name}" >&2
                return 1
                ;;
            1) ;;
            *) return 1 ;;
        esac
    done
    printf '==> no LXC container named %s\n' "${LT_NAMES[*]}"
}

# lt_attach <name> <root|agent> <argv...> — argv in the container with a cleared environment,
# as root, or as the agent user in its checkout; stdin is the caller's. Returns argv's status.
lt_attach() {
    local name="$1" who="$2"
    shift 2
    lt_name_ok "${name}" || return 1
    local -a inner=(/usr/bin/env -i "PATH=${LT_GUEST_PATH}" HOME=/root) piped
    if [[ "${who}" == "agent" ]]; then
        inner+=(runuser -u "${LT_AGENT}" -- env -i -C "${LT_CHECKOUT}" "PATH=${LT_GUEST_PATH}" "HOME=${LT_AGENT_HOME}")
    fi
    {
        (
            cat | sudo -n lxc-attach -n "${name}" --clear-env -- "${inner[@]}" "$@" 3>&- | cat 3>&-
            exit "${PIPESTATUS[1]}"
        ) 2>&1 1>&3 3>&- | cat >&2 3>&-
        piped=("${PIPESTATUS[@]}")
    } 3>&1
    if [[ "${piped[1]}" -ne 0 ]]; then
        printf '[FAIL] lt_attach: copying the stderr of %s failed\n' "${name}" >&2
        return 1
    fi
    return "${piped[0]}"
}

# lt_destroy <name> — stop and remove the container when it exists; one that is not there is
# said and is not an error.
lt_destroy() {
    local name="$1" found=0
    lt_name_ok "${name}" || return 1
    lt_exists "${name}" || found=$?
    case "${found}" in
        0)
            sudo -n lxc-destroy -n "${name}" -f || return 1
            printf '==> destroyed the LXC container %s\n' "${name}"
            ;;
        1) printf '==> no LXC container %s: nothing to remove\n' "${name}" ;;
        *) return 1 ;;
    esac
}

# ── lxc-throwaways-up.bash ───────────────────────────────────────────────────────────────

MADE=()    # the containers this run may have made, before each attempt: a half-made one goes too
UP_DONE=0  # set once both are up; a run that stops before then removes what it made
GH_TOKEN_HELD=""

up_after_stop() {
    local name status=0
    if [[ "${UP_DONE}" -eq 1 ]]; then
        return 0
    fi
    for name in "${MADE[@]+"${MADE[@]}"}"; do
        lt_destroy "${name}" || status=1
    done
    return "${status}"
}

# up_prerequisites — LXC with its download template, and the desktop user's gh login to lend.
up_prerequisites() {
    if ! command -v lxc-create >/dev/null || [[ ! -x /usr/share/lxc/templates/lxc-download ]]; then
        printf '[FAIL] LXC with its download template is needed: run playbooks/imports/play-lxc-install-config.yml (playbook-main.yml imports it)\n' >&2
        return 1
    fi
    if ! command -v gh >/dev/null; then
        printf '[FAIL] gh is needed, logged in: run gh auth login --hostname github.com as %s\n' "${USER}" >&2
        return 1
    fi
    if ! GH_TOKEN_HELD="$(gh auth token --hostname github.com)" || [[ -z "${GH_TOKEN_HELD}" ]]; then
        printf '[FAIL] gh is not logged in to github.com: run gh auth login --hostname github.com as %s\n' "${USER}" >&2
        return 1
    fi
    printf '==> the desktop user'\''s gh login for github.com is lent to the throwaway agents\n'
}

# gh_token_pipe — the token on a pipe, for a login's stdin (printf is a builtin: no argv).
gh_token_pipe() {
    printf '%s\n' "${GH_TOKEN_HELD}"
}

# up_create <name> — the container from the download template, started, with an IPv4 address.
up_create() {
    local name="$1" version arch address="" tries state
    lt_name_ok "${name}" || return 1
    version="$(awk -F= '$1 == "VERSION_ID" { gsub(/"/, "", $2); print $2 }' /etc/os-release)" || return 1
    case "$(uname -m)" in
        x86_64) arch=amd64 ;;
        aarch64) arch=arm64 ;;
        *) arch="$(uname -m)" ;;
    esac
    MADE+=("${name}")
    sudo -n lxc-create -n "${name}" -t download -- --dist fedora --release "${version}" --arch "${arch}" || return 1
    sudo -n lxc-start -n "${name}" -d || return 1
    sudo -n lxc-wait -n "${name}" -s RUNNING -t 30 || return 1
    for ((tries = 0; tries < 60; tries++)); do
        # A container that stopped will never get an address: name its state now.
        state="$(sudo -n lxc-info -n "${name}" -sH)" || return 1
        if [[ "${state}" != RUNNING ]]; then
            printf '[FAIL] the LXC container %s is %s while waiting for its IPv4 address\n' "${name}" "${state}" >&2
            return 1
        fi
        address="$(sudo -n lxc-info -n "${name}" -iH | awk '/^[0-9]+\./ { print; exit }')" || return 1
        if [[ -n "${address}" ]]; then
            break
        fi
        sleep 1
    done
    if [[ -z "${address}" ]]; then
        printf '[FAIL] the LXC container %s has no IPv4 address after 60 s (lxc-net DHCP)\n' "${name}" >&2
        return 1
    fi
    printf '==> LXC container %s (Fedora %s) is running at %s\n' "${name}" "${version}" "${address}"
}

# up_prepare <name> — the packages, the agent user and its checkout, its gh login, then a check
# that gh says it is logged in.
up_prepare() {
    local name="$1"
    lt_attach "${name}" root dnf -y --setopt=install_weak_deps=False install \
        python3 git-core util-linux-core shadow-utils gh </dev/null || return 1
    lt_attach "${name}" root useradd --create-home "${LT_AGENT}" </dev/null || return 1
    lt_attach "${name}" root runuser -u "${LT_AGENT}" -- git init -q "${LT_CHECKOUT}" </dev/null || return 1
    lt_attach "${name}" agent gh auth login --with-token --hostname github.com < <(gh_token_pipe) || return 1
    lt_attach "${name}" agent gh auth status --hostname github.com </dev/null || return 1
    printf '==> %s: user %s, checkout %s, gh logged in\n' "${name}" "${LT_AGENT}" "${LT_CHECKOUT}"
}

up_all() {
    local name
    for name in "${LT_NAMES[@]}"; do
        up_create "${name}" || return 1
        up_prepare "${name}" || return 1
    done
    UP_DONE=1
}

# ── lxc-throwaways-down.bash ─────────────────────────────────────────────────────────────

# down_prerequisites — LXC's tools, to list and destroy with.
down_prerequisites() {
    if ! command -v lxc-ls >/dev/null || ! command -v lxc-destroy >/dev/null; then
        printf '[FAIL] lxc-ls and lxc-destroy are needed: run playbooks/imports/play-lxc-install-config.yml (playbook-main.yml imports it)\n' >&2
        return 1
    fi
}

# down_all — destroy each throwaway that is there.
down_all() {
    local name
    for name in "${LT_NAMES[@]}"; do
        lt_destroy "${name}" || return 1
    done
}
