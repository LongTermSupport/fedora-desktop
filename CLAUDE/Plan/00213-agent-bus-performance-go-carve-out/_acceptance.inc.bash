# shellcheck shell=bash
# _acceptance.inc.bash — the checks of Plan 00213's acceptance.bash (Task 2.3, the status line's
# cut-over to Go). A separate file because the checks are called through plan_gather_leg, which
# ShellCheck cannot see through (SC2329), and suppression is banned. Sourced, never executed: no
# shell options, no `exit`. Every check returns non-zero with a message on stderr when it does
# not pass, and appends what it found to REPORT. The caller sets KIT_BIN, STAGED_BIN,
# HOST_PINGBUS, KIT_SETTINGS, STAGED_SETTINGS, BUILT, MEMBERS_JSON, RENDERS and REPORT.

# say_report <text...> — one line on stdout and in the report.
say_report() {
    printf '%s\n' "$*" | tee -a "${REPORT}"
}

# build_reference — this checkout's pingbus-status-line, built exactly as
# tasks/build-go-command.yml builds it with go_command_static (the build is reproducible:
# -trimpath and -buildvcs=false), so an installed copy is this checkout's when it is the same
# bytes.
build_reference() {
    (
        cd "${PLAN_REPO_ROOT}/src-go" &&
            env GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0 \
                go build -trimpath -buildvcs=false -o "${BUILT}" ./cmd/pingbus-status-line
    ) || return 1
    say_report "reference build: $(sha256sum "${BUILT}" | cut -d' ' -f1)"
}

# same_binary <label> <path> — <path> is a root-owned 0755 regular file, built static, with the
# reference build's bytes.
same_binary() {
    local label="$1" path="$2" meta
    if [[ ! -f "${path}" || -L "${path}" ]]; then
        printf '%s: %s is missing or not a regular file: its play has not run\n' "${label}" "${path}" >&2
        return 1
    fi
    meta="$(stat -c '%U:%G %a' "${path}")" || return 1
    say_report "- ${label}: ${path} ${meta} $(sha256sum "${path}" | cut -d' ' -f1)"
    if [[ "${meta}" != "root:root 755" ]]; then
        printf '%s: %s is %s, not root:root 755\n' "${label}" "${path}" "${meta}" >&2
        return 1
    fi
    if ! go version -m "${path}" | grep -qxP '\tbuild\tCGO_ENABLED=0'; then
        printf '%s: %s was not built static (CGO_ENABLED=0), so it may not run in the image or an LXC member\n' \
            "${label}" "${path}" >&2
        return 1
    fi
    if ! cmp -s "${BUILT}" "${path}"; then
        printf '%s: %s differs from this checkout'\''s build: it is older, or built another way (re-run the deploy)\n' \
            "${label}" "${path}" >&2
        return 1
    fi
}

# c1_installed — the member kit's binary (play-agent-bus.yml) and the ccy build context's
# (play-claude-yolo.yml) are this checkout's.
c1_installed() {
    local bad=0
    same_binary "member kit" "${KIT_BIN}" || bad=1
    same_binary "ccy build context" "${STAGED_BIN}" || bad=1
    return "${bad}"
}

# c2_settings — both copies of the kit settings run the Go binary by name.
c2_settings() {
    local path got bad=0
    for path in "${KIT_SETTINGS}" "${STAGED_SETTINGS}"; do
        got="$(jq -r '.statusLine.command' "${path}")" || return 1
        say_report "- ${path}: statusLine.command = ${got}"
        if [[ "${got}" != "pingbus-status-line" ]]; then
            printf '%s runs "%s", not pingbus-status-line: its play has not run\n' "${path}" "${got}" >&2
            bad=1
        fi
    done
    return "${bad}"
}

# read_members — agent_bus_lxc_members as the plays see it, through an ad-hoc Ansible copy into
# the run directory (0600): the inventory and vault are Ansible's, so nothing here parses YAML.
read_members() {
    local args
    args="$(jq -cn --arg dest "${MEMBERS_JSON}" \
        '{content: "{{ agent_bus_lxc_members | default([]) | to_json }}\n", dest: $dest, mode: "0600"}')" || return 1
    plan_ansible_adhoc localhost -m ansible.builtin.copy -a "${args}" || return 1
    jq -e 'type == "array"' "${MEMBERS_JSON}" >/dev/null
}

# c3_lxc — every container a present agent_bus_lxc_members entry names is RUNNING, and its
# copy of the kit (members.py copies the host kit in) holds the reference binary, which runs
# there. Every container is judged before the verdict, so one run names all that are down or
# behind.
c3_lxc() {
    local containers name state got want bad=0
    containers="$(jq -r '[.[] | select((.state // "present") == "present") | .container] | unique | .[]' "${MEMBERS_JSON}")" || return 1
    if [[ -z "${containers}" ]]; then
        say_report "no present agent_bus_lxc_members entry: nothing to check"
        return 0
    fi
    want="$(sha256sum "${BUILT}" | cut -d' ' -f1)" || return 1
    while IFS= read -r name; do
        state="$(sudo -n lxc-info -n "${name}" -sH)" || {
            printf 'lxc-info failed for container %s: is it the name of an LXC container here?\n' "${name}" >&2
            bad=1
            continue
        }
        if [[ "${state}" != "RUNNING" ]]; then
            printf 'container %s is %s, not RUNNING: start it, then run meta-deploy again (the play copies its kit only while it runs)\n' \
                "${name}" "${state}" >&2
            say_report "- container ${name}: ${state}"
            bad=1
            continue
        fi
        got="$(sudo -n lxc-attach -n "${name}" --clear-env -- env -i PATH=/usr/bin:/bin \
            sha256sum /usr/local/share/agent-bus/kit/pingbus-status-line)" || {
            printf 'container %s: its kit has no pingbus-status-line: re-run the deploy with it running\n' "${name}" >&2
            bad=1
            continue
        }
        got="${got%% *}"
        say_report "- container ${name}: ${got}"
        if [[ "${got}" != "${want}" ]]; then
            printf 'container %s: its kit'\''s pingbus-status-line is not this checkout'\''s build\n' "${name}" >&2
            bad=1
            continue
        fi
        # It runs there: an empty payload outside a bus session prints one empty line, exit 0.
        got="$(printf '{}' | sudo -n lxc-attach -n "${name}" --clear-env -- env -i PATH=/usr/bin:/bin HOME=/nonexistent \
            /usr/local/share/agent-bus/kit/pingbus-status-line | od -An -c | tr -d ' ')" || {
            printf 'container %s: pingbus-status-line does not run there\n' "${name}" >&2
            bad=1
            continue
        }
        if [[ "${got}" != '\n' ]]; then
            printf 'container %s: pingbus-status-line printed %s for an empty payload, not one empty line\n' "${name}" "${got}" >&2
            bad=1
        fi
    done <<<"${containers}"
    return "${bad}"
}

# golden <impl...> — the recorded Python goldens (src-go/internal/pingbus/statusline/golden)
# judge an installed command.
golden() {
    (
        cd "${PLAN_REPO_ROOT}/src-go" &&
            env GOTOOLCHAIN=local GOFLAGS=-mod=vendor CGO_ENABLED=0 \
                go test -count=1 ./internal/pingbus/statusline/golden "-impl=$*"
    ) || return 1
    say_report "- the goldens pass for: $*"
}

# c4_parity — the installed Python and the installed Go both print what the goldens recorded,
# so they print the same over every fixture (team records, roles, widths, a hung or failing
# inner command, the cache's fresh and invalidated steps).
c4_parity() {
    local bad=0
    golden "${HOST_PINGBUS} status-line" || bad=1
    golden "${KIT_BIN}" || bad=1
    golden "${STAGED_BIN}" || bad=1
    return "${bad}"
}

# cpu_per_render <label> <command...> — the CPU (user and system, of the command and every
# child) that RENDERS renders take, divided by RENDERS, in microseconds. Runs in the scratch
# world the caller made: no settings, so no inner status line; the environment the caller
# exported decides the segment. Prints the number alone.
cpu_per_render() {
    local label="$1" total
    shift
    total="$(
        cd "${PLAN_RUN_DIR}/world/project" || exit 1
        # From a file, not a pipe: a pipe's writer would be one more process in the count.
        for ((i = 0; i < RENDERS; i++)); do
            "$@" <"${PLAN_RUN_DIR}/world/payload.json" >/dev/null || exit 1
        done
        times
    )" || {
        printf '%s: a render failed\n' "${label}" >&2
        return 1
    }
    # `times` prints the shell's line, then the children's: "XmY.YYYs XmY.YYYs".
    awk -v n="${RENDERS}" 'NR == 2 {
        split($1, u, /[ms]/); split($2, s, /[ms]/)
        printf "%d\n", ((u[1] * 60 + u[2]) + (s[1] * 60 + s[2])) * 1000000 / n
    }' <<<"${total}"
}

# measure <case> — Python against Go for one case of the world, into the report.
measure() {
    local case="$1" python go
    python="$(cpu_per_render "python ${case}" "${HOST_PINGBUS}" status-line)" || return 1
    go="$(cpu_per_render "go ${case}" "${KIT_BIN}")" || return 1
    say_report "| ${case} | ${python} | ${go} |"
    if [[ "${python}" -le 0 ]] || ((go * 5 > python)); then
        printf '%s: the Go render costs %s us of CPU, the Python %s us: Go must cost under a fifth\n' \
            "${case}" "${go}" "${python}" >&2
        return 1
    fi
}

# c5_cpu — the success metric's per-render half (the plan's Goals): the CPU one render costs,
# before (the installed Python zipapp) and after (the installed Go binary).
c5_cpu() {
    mkdir -p "${PLAN_RUN_DIR}/world/project" "${PLAN_RUN_DIR}/world/home" "${PLAN_RUN_DIR}/world/sessions" || return 1
    printf '{"session_id": "sess-1"}' >"${PLAN_RUN_DIR}/world/payload.json" || return 1
    say_report ""
    say_report "CPU per render, microseconds, mean of ${RENDERS} (user + system, with children):"
    say_report ""
    say_report "| case | python (before) | go (after) |"
    say_report "| ---- | --------------- | ---------- |"
    (
        export HOME="${PLAN_RUN_DIR}/world/home" PATH=/usr/bin:/bin LANG=C.UTF-8 \
            PINGBUS_SESSIONS_DIR="${PLAN_RUN_DIR}/world/sessions"
        unset CLAUDE_CONFIG_DIR CLAUDE_PROJECT_DIR PINGBUS_TEAMS PINGBUS_MEMBERSHIP PINGBUS_HOME COLUMNS
        measure "no team" || exit 1
        export PINGBUS_TEAMS=alpha
        measure "a team, no session record" || exit 1
    )
}
