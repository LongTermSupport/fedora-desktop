#!/bin/bash
# The limits every container ccy runs is started with, passed explicitly on each run and never
# left to the engine's default. Why, and the choice of default: docs/ccy.md, "Container limits".
# The rule that holds every ccy run to it: ccy-container-default-pids-limit (CLAUDE/QA.md,
# .semgrep/bash-conventions.yml); the Go front's copy of the bounds: src-go/internal/ccylaunch.
#
# CCY_PIDS_LIMIT on the host overrides the pids limit. Requires print_error (common-pure.bash).

CCY_PIDS_LIMIT_DEFAULT=4096
CCY_PIDS_LIMIT_MIN=256
CCY_PIDS_LIMIT_MAX=32768

# ccy_container_limits — settle CCY_PIDS_LIMIT: the host's value, else the default. 1 for a value
# that is not a whole number from MIN to MAX. The limit is what stops a fork bomb inside a session
# before it reaches the user slice's own task limit, so there is no way to turn it off: 0 and -1
# (no limit to the engine), words such as "max", and values past MAX are refused, and anything
# else would fail only after the session was built.
ccy_container_limits() {
    CCY_PIDS_LIMIT="${CCY_PIDS_LIMIT:-$CCY_PIDS_LIMIT_DEFAULT}"
    if ! [[ "$CCY_PIDS_LIMIT" =~ ^[1-9][0-9]{0,4}$ ]] || [ "$CCY_PIDS_LIMIT" -lt "$CCY_PIDS_LIMIT_MIN" ] || [ "$CCY_PIDS_LIMIT" -gt "$CCY_PIDS_LIMIT_MAX" ]; then
        print_error "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT is not a pids limit: a whole number from $CCY_PIDS_LIMIT_MIN to $CCY_PIDS_LIMIT_MAX; it cannot be turned off (the default is $CCY_PIDS_LIMIT_DEFAULT; docs/ccy.md, \"Container limits\")."
        return 1
    fi
}
