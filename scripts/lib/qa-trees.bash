#!/usr/bin/bash
# Reads scripts/qa-trees.tsv into the exclusion lists the QA gates share. SOURCE this; it is
# sourced by qa-discovery.bash, which documents what the lists mean.
#
# The trees the gates must not gate on are data, one list per language (shell and generic,
# python), with the reason for each tree in the file. The Go qa-changed reads the same file, so
# there is one list. QA_EXCLUDE_DIRS is the shell list, QA_PY_EXCLUDE_DIRS the narrower python
# one. A missing file or an unknown list name stops the gate (exit 2): an empty exclusion list
# would gate vendored trees and report their findings as ours.

_qa_trees_file="$(dirname "${BASH_SOURCE[0]}")/../qa-trees.tsv"
if [[ ! -r "$_qa_trees_file" ]]; then
    echo "✗ qa-trees: cannot read $_qa_trees_file" >&2
    exit 2
fi
QA_EXCLUDE_DIRS=()
QA_PY_EXCLUDE_DIRS=()
while IFS=$'\t' read -r _qa_list _qa_dir; do
    case "$_qa_list" in
        "" | "#"*) ;;
        shell) QA_EXCLUDE_DIRS+=("$_qa_dir") ;;
        python) QA_PY_EXCLUDE_DIRS+=("$_qa_dir") ;;
        *)
            echo "✗ qa-trees: unknown list '$_qa_list' in $_qa_trees_file" >&2
            exit 2
            ;;
    esac
done < "$_qa_trees_file"
unset _qa_trees_file _qa_list _qa_dir
