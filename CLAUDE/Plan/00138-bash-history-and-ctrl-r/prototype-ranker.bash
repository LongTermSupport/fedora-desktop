#!/usr/bin/env bash
# prototype-ranker.bash — Plan 00138 Task 2.3: the Ctrl+R ranker, as a standalone prototype.
#
# Ranks every command in the history file and the context file for the directory given:
#   tier 2  ran in this directory
#   tier 1  ran inside this directory's git repository
#   tier 0  everything else (including all pre-recorder history, which has no directory)
# Within a tier: commands whose every recorded run failed sink; then most recent first.
#
# Usage: prototype-ranker.bash <context-file> <history-file> <directory>
#   context-file  NUL-terminated records: epoch TAB exit TAB cwd TAB command
#   history-file  a bash HISTFILE, with or without #epoch timestamp lines
#
# stdout: the payload — distinct commands, best first, each NUL-terminated (for fzf --read0).
# stderr: diagnostics only.
#
# EXIT CODES: 0 ranked; 64 usage error; 66 an input file is missing or unreadable.
set -euo pipefail

if [[ $# -ne 3 ]]; then
    printf 'usage: prototype-ranker.bash <context-file> <history-file> <directory>\n' >&2
    exit 64
fi
context_file="$1"
history_file="$2"
directory="$3"
for f in "${context_file}" "${history_file}"; do
    if [[ ! -r "${f}" ]]; then
        printf '[FATAL] cannot read %s\n' "${f}" >&2
        exit 66
    fi
done

# Not being inside a repository is an answer, not an error: tier 1 is then simply empty.
repo=""
if repo_out="$(git -C "${directory}" rev-parse --show-toplevel 2>&1)"; then
    repo="${repo_out}"
fi

# The history file becomes context-shaped records with no directory and no exit status.
# With timestamps, an entry is everything between two #epoch lines (lithist keeps
# embedded newlines); lines before the first timestamp are one entry each.
history_as_records() {
    gawk '
        function flush() { if (buf != "") printf "0\t\t\t%s%c", buf, 0; buf = "" }
        /^#[0-9]+$/ && length($0) >= 11 { flush(); stamped = 1; next }
        stamped { buf = (buf == "" ? $0 : buf "\n" $0); next }
        { printf "0\t\t\t%s%c", $0, 0 }
        END { flush() }
    ' "${history_file}"
}

# History first, context second: context rows are newer copies of the same commands and
# carry the directory and exit status, so they update recency and add the tier signals.
{ history_as_records; cat "${context_file}"; } |
    gawk -v cwd="${directory}" -v repo="${repo}" '
        BEGIN { RS = "\0"; FS = "\t"; ORS = "\0" }
        {
            cmd = $4
            for (i = 5; i <= NF; i++) cmd = cmd "\t" $i
            if (cmd == "") next
            last[cmd] = NR
            if ($3 == "") next
            runs[cmd]++
            if ($2 == "0") ok[cmd] = 1
            if ($3 == cwd) here[cmd] = 1
            else if (repo != "" && ($3 == repo || index($3, repo "/") == 1)) inrepo[cmd] = 1
        }
        END {
            for (cmd in last) {
                tier = (cmd in here) ? 2 : ((cmd in inrepo) ? 1 : 0)
                failed = (runs[cmd] > 0 && !(cmd in ok)) ? 1 : 0
                printf "%d\t%d\t%d\t%s\0", tier, failed, last[cmd], cmd
            }
        }
    ' |
    sort -z -t $'\t' -k1,1nr -k2,2n -k3,3nr |
    cut -z -f4-
