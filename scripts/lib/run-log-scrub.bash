# shellcheck shell=bash
#
# Run-log secret scrubber — Plan 00121.
#
# This repo scans for secrets at the GIT boundary: `scripts/git-hooks/lib/secret-scan.bash`
# and `.gitleaks.toml` stop one entering version control. Runtime artefacts — VM console
# logs, provisioning transcripts, run logs — live under `untracked/`, are never committed,
# and so are never looked at by any of it. Plan 00110 `DESIGN.md:329-337` keeps a
# PAT-bearing run's transcript off the shared mount because of that gap.
#
# **Redaction is by KNOWN VALUE, not by pattern.** A PAT has a recognisable shape
# (`ghp_`, `github_pat_`); an SSH passphrase is arbitrary text and has none, so pattern
# matching alone could never cover it. But a run SUPPLIED its secrets and therefore knows
# their exact bytes — matching those is reliable, and pattern matching is a backstop for a
# shape nobody anticipated (Task 1.4), never the primary mechanism.
#
# **`scrub_verify` is the point of the library, not `scrub_redact`.** Redacting is the easy
# half; a scrubber is fail-open by nature, writing a file it BELIEVES is clean, and a miss is
# silent. So nothing here publishes: `scrub_verify` re-reads the artefact and refuses if any
# supplied secret survives. A caller that redacts without verifying has a check that cannot
# fail.
#
# Secrets are passed as FILE PATHS, never as values — `CLAUDE/SecurityRules.md`: secrets
# never appear in argv, where `ps` would show them to any user on the box.
#
# Plan: CLAUDE/Plan/00121-run-log-secret-scrubber-and-token-scenario/PLAN.md

# scrub_redact <artefact> <secret-file> <label>
#
# Replace every occurrence of the secret with `[REDACTED:<label>]`, in place. The label names
# WHICH secret was removed, so a reader of the scrubbed log can tell a redacted token from a
# redacted passphrase without either being recoverable.
#
# Finding nothing is success: most artefacts of most runs contain no secret at all, and
# treating "clean" as an error would train the caller to ignore the exit status.
scrub_redact() {
    local artefact="$1" secret_file="$2" label="$3"

    if [ ! -f "$artefact" ]; then
        printf 'scrub_redact: artefact does not exist: %s\n' "$artefact" >&2
        return 1
    fi
    if [ ! -r "$secret_file" ]; then
        printf 'scrub_redact: secret file is not readable: %s\n' "$secret_file" >&2
        return 1
    fi

    python3 - "$artefact" "$secret_file" "$label" <<'PYEOF'
import os
import sys
import tempfile

artefact, secret_path, label = sys.argv[1], sys.argv[2], sys.argv[3]

with open(secret_path, "rb") as handle:
    # The trailing newline belongs to the FILE, not to the value: `printf '%s\n' "$pat" >
    # file` is how a secret file is written, and the secret in the log has no newline in it.
    secret = handle.read().rstrip(b"\r\n")

# An empty needle matches at every position. A permissive implementation would either corrupt
# the artefact or redact nothing and report success — a scrub that cannot fail, on the one
# input where the caller most needs to hear that something is wrong.
if not secret:
    sys.stderr.write(
        f"scrub_redact: the secret file is empty: {secret_path}\n"
        "  An empty secret cannot be redacted, and treating it as 'nothing to do' would\n"
        "  report a clean scrub over an artefact nothing was removed from.\n"
    )
    raise SystemExit(1)

with open(artefact, "rb") as handle:
    data = handle.read()

# Bytes, and a literal replace: an artefact may carry console control codes or invalid UTF-8,
# and a secret is an opaque byte string. Treating either as text or as a pattern would mangle
# the first and mis-match the second.
redacted = data.replace(secret, f"[REDACTED:{label}]".encode())

if redacted == data:
    raise SystemExit(0)

# Atomic: a reader must never see a half-scrubbed artefact, and a crash mid-write must leave
# the original rather than a file that is partly redacted and looks finished.
directory = os.path.dirname(os.path.abspath(artefact))
fd, temporary = tempfile.mkstemp(dir=directory)
try:
    with os.fdopen(fd, "wb") as handle:
        handle.write(redacted)
    os.chmod(temporary, os.stat(artefact).st_mode & 0o7777)
    os.replace(temporary, artefact)
except BaseException:
    os.unlink(temporary)
    raise
PYEOF
}

# scrub_verify <artefact> <secret-file>...
#
# Re-read the artefact and refuse if ANY supplied secret survives. This is what makes the
# library fail-closed: the caller publishes only on a zero exit, so a redaction that missed
# something stops the artefact rather than shipping it.
#
# It reports WHICH secret file was still present and never the value — the message is read by
# a human and may be pasted into a ticket, so echoing the secret would leak it into a second
# place while complaining that it leaked into the first.
scrub_verify() {
    local artefact="$1"
    shift

    if [ ! -f "$artefact" ]; then
        printf 'scrub_verify: artefact does not exist: %s\n' "$artefact" >&2
        return 1
    fi
    if [ "$#" -eq 0 ]; then
        printf 'scrub_verify: no secret files given, so this would verify nothing\n' >&2
        return 1
    fi

    python3 - "$artefact" "$@" <<'PYEOF'
import sys

artefact, secret_paths = sys.argv[1], sys.argv[2:]

with open(artefact, "rb") as handle:
    data = handle.read()

residual = []
for path in secret_paths:
    try:
        with open(path, "rb") as handle:
            secret = handle.read().rstrip(b"\r\n")
    except OSError as error:
        # Unreadable is not clean. A secret that could not be checked has not been shown to
        # be absent, and saying nothing here would publish on the strength of a check that
        # did not run.
        sys.stderr.write(f"scrub_verify: could not read secret file {path}: {error}\n")
        raise SystemExit(1)
    if not secret:
        sys.stderr.write(f"scrub_verify: the secret file is empty: {path}\n")
        raise SystemExit(1)
    if secret in data:
        residual.append(path)

if residual:
    sys.stderr.write(
        f"scrub_verify: REFUSED — {len(residual)} secret(s) still present in {artefact}\n"
    )
    for path in residual:
        sys.stderr.write(f"  still present: the secret from {path}\n")
    sys.stderr.write(
        "  The artefact has NOT been published. Redact the missing value and re-verify;\n"
        "  do not publish this file on the strength of a scrub that missed something.\n"
    )
    raise SystemExit(1)
PYEOF
}
