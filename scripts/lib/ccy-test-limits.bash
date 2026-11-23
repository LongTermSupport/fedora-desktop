# shellcheck shell=bash
# Sourced by the ccy test suites. ccy settles the container limits (lib/container-limits.bash)
# before it starts any container: it refuses to run without the agents slice's unit, and caps
# memory at the slice's MemoryMax unless CCY_MEMORY_MAX says otherwise.
#
# ccy_test_limits <home> — installs the slice's unit where the play puts it, under
# <home>/.config, points XDG_CONFIG_HOME there and exports a memory cap every machine allows.
# A run under `env -i` keeps the unit through HOME=<home>, and passes CCY_MEMORY_MAX itself.
ccy_test_limits() {
    mkdir -p "$1/.config/systemd/user" || return 1
    printf '[Slice]\n' >"$1/.config/systemd/user/agents.slice" || return 1
    export XDG_CONFIG_HOME="$1/.config" CCY_MEMORY_MAX=4g
}
