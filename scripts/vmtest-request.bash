#!/usr/bin/bash
# Ask the host's VM acceptance lab to do something, from inside the sandbox
# (Plan 00110). This is the container side of the bridge: it writes a request
# into untracked/vmtest-bridge/ and waits for the host's answer.
#
#   ./scripts/vmtest-request.bash list-scenarios
#   ./scripts/vmtest-request.bash run-scenario server-fast-provision
#   ./scripts/vmtest-request.bash run-scenario server-fast-provision --timeout 5400
#
# Exit 0 ONLY when the host reports a finished pass. Every other outcome —
# fail, error, rejected, no answer, bridge not running, bridge wedged, host
# process died, malformed response — is a distinct non-zero code (1..8, 64
# usage) with its reason on stderr.
#
# The response is signed by the host, and this side cannot verify it: the key
# never leaves the host. The reader says so and prints the `vmtest verify`
# line for a human to run there.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT_DIR"

exec python3 -m helpers.vmtest.request --checkout "$ROOT_DIR" "$@"
