#!/bin/bash
# The resource limits a ccy session container is started with. Each is passed explicitly on the
# session container's run, never left to the engine's default (Defence Before Fix rule
# container-engine-default-limit, .semgrep/bash-conventions.yml).
#
# The pids limit. A pid in the pids cgroup is a thread, not a process. Left unset, podman gives
# every container 2048, and a busy session reaches it: Go builds and tests, golangci-lint,
# Ansible forks and many background waiters each run hundreds of threads. At the limit the
# kernel refuses the fork ("fork rejected by pids controller"), Claude Code's hooks fail with
# EAGAIN on posix_spawn, and claude aborts, taking the session with it.
#
# The default, 16384, is eight times podman's, which busy sessions were measured reaching. It
# stays a limit on purpose: the containers run in the owner's user slice, whose task limit
# (systemd's TasksMax, a share of the kernel's thread limit, which grows with memory) is shared
# with the desktop, so a fork bomb in one session must stop at its own container well before it
# reaches the slice's.
#
# CCY_PIDS_LIMIT, exported on the host before launching, overrides it for that launch. A
# project's ccy.env cannot: it is read inside the container, after the container exists.
#
# Requires print_error (common-pure.bash).

CCY_PIDS_LIMIT_DEFAULT=16384

# ccy_container_limits — settle CCY_PIDS_LIMIT: the host's value, else the default. 1 for a value
# that is not a whole number from 1 to 4194304 (the kernel's largest pid_max): 0 and -1 mean no
# limit to the engine, and anything else would fail only after the session was built.
ccy_container_limits() {
    CCY_PIDS_LIMIT="${CCY_PIDS_LIMIT:-$CCY_PIDS_LIMIT_DEFAULT}"
    if ! [[ "$CCY_PIDS_LIMIT" =~ ^[1-9][0-9]{0,6}$ ]] || [ "$CCY_PIDS_LIMIT" -gt 4194304 ]; then
        print_error "CCY_PIDS_LIMIT=$CCY_PIDS_LIMIT is not a pids limit: a whole number from 1 to 4194304 (the default is $CCY_PIDS_LIMIT_DEFAULT; docs/ccy.md, \"Container limits\")."
        return 1
    fi
}
