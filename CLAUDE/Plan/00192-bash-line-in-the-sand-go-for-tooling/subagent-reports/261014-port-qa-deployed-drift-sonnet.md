# qa-deployed-drift port (Task 3.3), branch go-port/qa-deployed-drift

Spec `SPEC-qa-deployed-drift.md` (E1-E16), code `src-go/qa/deployeddrift`, command
`src-go/qa/cmd/qa-deployed-drift`; the script is a build-and-run wrapper and its allowlist row is
gone. The gate row in `scripts/qa-gates.tsv` (kind `drift`) and the `qa-routing.tsv` entry are
unchanged: the command name, stage line and exit codes are the same.

Departures from the bash, each with its reason in the spec: git failing is exit 2 (the bash
compared as if the checkout were the deployed one); an unreadable file is exit 2 (the bash
reported it as drift); a pair naming a missing play, or matching no repo file, is exit 2 (the bash
passed over it silently); tier-2 play lookup matches whole words (the bash used a substring);
`request.py` is excluded from the helpers pair only (the bash excluded the name everywhere); a
missing `playbooks/` or unset `HOME` has its own message.

Contract kept: the source-tree half (every `.j2` has a playbook `dest:`) runs before any host
check; the host half is skipped only in the ccy container, a linked worktree, or where
`~/.local/bin` is absent, each with a `⚠` line stating the reason and the template count.

Live tree: in the linked worktree the tool skips with the worktree reason (as the bash does). On a
clean clone of the tree, run in this container, both print the same line: 0 match, 96 not
installed, 2 templates. A real drift could not be shown live without writing into the container's
home; the drift, ordering, play-naming and exit-2 cases are covered by the unit tests.
