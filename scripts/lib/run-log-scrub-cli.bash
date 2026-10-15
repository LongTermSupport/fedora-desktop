# shellcheck shell=bash
# run-log-scrub-cli.bash — the run-log scrubber as a command, for the Go vmtest (Plan 00192).
#
#   bash run-log-scrub-cli.bash redact <artefact> <secret-file> <label>
#   bash run-log-scrub-cli.bash verify <artefact> <secret-file>...
#
# Run with bash, like the library it fronts it is deployed 0644.
#
# The scrubber stays one bash library (run-log-scrub.bash, which sources the commit gate's
# detection engine); this front only names its two functions as verbs, so the Go caller runs
# it through its one process seam. The exit status is the library's; anything else is a usage
# error, exit 64. Secrets are passed as file paths, never values.
set -euo pipefail

# shellcheck source=SCRIPTDIR/run-log-scrub.bash
source "$(dirname "$(realpath "${BASH_SOURCE[0]}")")/run-log-scrub.bash"

case "${1:-}" in
    redact)
        shift
        scrub_redact "$@"
        ;;
    verify)
        shift
        scrub_verify "$@"
        ;;
    *)
        printf 'usage: run-log-scrub-cli.bash redact <artefact> <secret-file> <label> | verify <artefact> <secret-file>...\n' >&2
        exit 64
        ;;
esac
