# shellcheck shell=bash
# _bus-address.inc.bash — where Plan 00161's host scripts get the bus address from, one copy
# for deploy.bash (which puts it on agentbus0) and acceptance.bash (whose team listens on it).
#
# The address is install-specific, so it is not in this repository: --bus-address=<ip>, or
# else agent_bus_address in the untracked host_vars localhost.yml (see localhost.yml.dist).
# Sourced, never executed: no shell options, no `exit`.

readonly BUS_ADDRESS_VAR="agent_bus_address"

# resolve_bus_address — BUS_ADDRESS as given, else from the inventory; then held to the rule
# agent-bus-install applies to --bus-address (deploy_check.py syntax). Returns 64 when there
# is none or it is malformed, 1 when the inventory cannot be read.
resolve_bus_address() {
    local inventory
    if [[ -z "${BUS_ADDRESS}" ]]; then
        # ansible-inventory resolves host_vars exactly as the plays do; run from the repo root
        # for ansible.cfg's relative inventory path.
        inventory="$(cd "${PLAN_REPO_ROOT}" && ansible-inventory --host localhost </dev/null)" || return 1
        BUS_ADDRESS="$(python3 -I -c '
import json, sys
value = json.load(sys.stdin).get(sys.argv[1], "")
print(value if isinstance(value, str) else "")
' "${BUS_ADDRESS_VAR}" <<<"${inventory}")" || return 1
        if [[ -z "${BUS_ADDRESS}" ]]; then
            printf '[FATAL] no bus address: set %s: <ip> in environment/localhost/host_vars/localhost.yml (an address this host uses nowhere else), or pass --bus-address=<ip>\n' \
                "${BUS_ADDRESS_VAR}" >&2
            return 64
        fi
    fi
    if ! python3 -I "${PLAN_SCRIPT_DIR}/deploy_check.py" syntax "${BUS_ADDRESS}"; then
        return 64
    fi
}
