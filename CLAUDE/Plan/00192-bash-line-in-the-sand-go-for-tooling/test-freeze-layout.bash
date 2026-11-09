#!/usr/bin/env bash
# Plan 00192 — test-freeze-layout.bash: the cases freeze-layout.bash must judge, on fixtures.
#
# Runs anywhere (no engine, no host): each case writes a baseline (what the bash tool printed at
# the first deploy) and a later Go output, then checks freeze-layout.bash's exit status. Rows
# added, removed, or changed in state, and counts that moved, must pass; a changed column header,
# a renamed heading or a stray line in stdout must fail.
#
# Usage: ./test-freeze-layout.bash      EXIT: 0 every case as expected; 1 otherwise.
set -euo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly LAYOUT="${scriptDir}/freeze-layout.bash"
readonly GOLDEN="${scriptDir}/../../../src-go/internal/freeze/golden/testdata/golden"
work="$(mktemp -d)"
trap 'rm -rf -- "${work}"' EXIT

passed=0
failed=0
# expect <want-status> <case> <empty-golden> — the baseline is ${work}/before, the Go output
# ${work}/after.
expect() {
    local want="$1" name="$2" empty="$3" got=0
    "${LAYOUT}" "${work}/before" "${work}/after" "${GOLDEN}/${empty}/stdout.golden" >"${work}/findings" || got=$?
    if [[ "${got}" -eq "${want}" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s (exit %s)\n' "${name}" "${got}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s: want exit %s, got %s; findings:\n' "${name}" "${want}" "${got}"
        cat "${work}/findings"
    fi
}

podBefore='=== frozen (1) ===
  NAME                               STATE    CCY  NETWORKS
  db                                 paused   -    appnet

  Thaw them all with: podfreeze thaw --all

=== running (2) ===
  NAME                               STATE    CCY  NETWORKS
  web                                running  -    appnet
  proj1_yolo                         running  CCY  podman                '
printf '%s\n' "${podBefore}" >"${work}/before"

cp -- "${work}/before" "${work}/after"
expect 0 "podfreeze list unchanged" pod-list-empty

# web removed, db thawed into running, two containers added; the frozen section now empty.
cat >"${work}/after" <<'EOF'
=== frozen (0) ===
  (none)

=== running (4) ===
  NAME                               STATE    CCY  NETWORKS
  db                                 running  -    appnet
  proj1_yolo                         running  CCY  podman
  proj4_yolo                         running  CCY  podman
  cache                              running  -    appnet
EOF
expect 0 "podfreeze list with containers added, removed and thawed" pod-list-empty

# Everything stopped since the baseline.
cat >"${work}/after" <<'EOF'
=== frozen (0) ===
  (none)

=== running (0) ===
  (none)
EOF
expect 0 "podfreeze list on a machine now empty" pod-list-empty

# A column renamed: the layout the parser feeds is not the one the bash tool printed.
cat >"${work}/after" <<'EOF'
=== frozen (1) ===
  NAME                               STATE    CCY  NETWORK
  db                                 paused   -    appnet

  Thaw them all with: podfreeze thaw --all

=== running (1) ===
  NAME                               STATE    CCY  NETWORK
  web                                running  -    appnet
EOF
expect 1 "podfreeze list with a changed column header" pod-list-empty

# An engine warning leaked into stdout.
cat >"${work}/after" <<'EOF'
WARN[0000] The cgroupv2 manager is set to systemd but there is no systemd user session
=== frozen (1) ===
  NAME                               STATE    CCY  NETWORKS
  db                                 paused   -    appnet

  Thaw them all with: podfreeze thaw --all

=== running (1) ===
  NAME                               STATE    CCY  NETWORKS
  web                                running  -    appnet
EOF
expect 1 "podfreeze list with a stray line on stdout" pod-list-empty

# A heading renamed.
cat >"${work}/after" <<'EOF'
=== paused (1) ===
  NAME                               STATE    CCY  NETWORKS
  db                                 paused   -    appnet

  Thaw them all with: podfreeze thaw --all

=== running (1) ===
  NAME                               STATE    CCY  NETWORKS
  web                                running  -    appnet
EOF
expect 1 "podfreeze list with a renamed heading" pod-list-empty

# lxcfreeze: rows added and removed, an address changed, the empty-machine note allowed.
cat >"${work}/before" <<'EOF'
=== frozen (1) ===
  NAME                               STATE    BRIDGE            IPV4
  zulu                               FROZEN   lxcbr0            192.0.2.11

  Thaw them all with: lxcfreeze thaw --all

=== running (1) ===
  NAME                               STATE    BRIDGE            IPV4
  alpha                              RUNNING  lxcbr0            192.0.2.12
EOF
cat >"${work}/after" <<'EOF'
=== frozen (0) ===
  (none)

=== running (2) ===
  NAME                               STATE    BRIDGE            IPV4
  alpha                              RUNNING  lxcbr0            192.0.2.40
  bravo                              RUNNING  virbr1            198.51.100.5
EOF
expect 0 "lxcfreeze list with containers added and removed" lxc-list-empty
cat >"${work}/after" <<'EOF'
=== frozen (0) ===
  (none)

=== running (0) ===
  (none)

  LXC is installed and has no running or frozen containers.
  (A STOPPED container is not counted — see 'sudo lxc-ls -f'.)
EOF
expect 0 "lxcfreeze list on a machine now empty" lxc-list-empty

# The dry run: its count moves with the machine.
cat >"${work}/before" <<'EOF'
DRY RUN — 2 container(s) would be FROZEN:
  NAME                               STATE    CCY  NETWORKS
  web                                running  -    appnet
  proj1_yolo                         running  CCY  podman
EOF
cat >"${work}/after" <<'EOF'
DRY RUN — 3 container(s) would be FROZEN:
  NAME                               STATE    CCY  NETWORKS
  proj1_yolo                         running  CCY  podman
  proj4_yolo                         running  CCY  podman
  cache                              running  -    appnet
EOF
expect 0 "podfreeze freeze --all -n with containers added and removed" pod-list-empty
cat >"${work}/after" <<'EOF'
DRY RUN — 1 container(s) would be THAWED:
  NAME                               STATE    CCY  NETWORKS
  web                                running  -    appnet
EOF
expect 1 "podfreeze freeze --all -n announcing the wrong verb" pod-list-empty

printf '\n%d passed, %d failed\n' "${passed}" "${failed}"
[[ "${failed}" -eq 0 ]]
