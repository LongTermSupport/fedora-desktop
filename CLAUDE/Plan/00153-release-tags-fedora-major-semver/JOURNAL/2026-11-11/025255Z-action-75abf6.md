## 02:52 · action · PR 232   — PR 232 merged onto F44 in a work branch, CCY 3.118.2

PR 232 (feat/release-version) merged into a work branch off F44 with a merge commit, for the
coordinator to land on F44. The branch's unreleased CCY 3.110.1 bump is re-taken as 3.118.2
(container unchanged at 2.84.1), its changelog entry carried over. The four conflicts kept both
sides: QA.md's gate table from F44 (the branch's one cell fix was already there),
go-binary-suites.tsv with F44's teamchat rows and the branch's release-plan row.

The branch's last CI failure (ccy-teams, "the launch's start") was the real-clock window check
F44 replaced with a fake clock in Plan 00194; the merged suite passes. project_walk_open, a rule
that landed on F44 after this branch, flagged the release workflow test reading a globbed
action.yml with no regular-file check; it now refuses a non-regular entry, with a test.

Nothing was tagged or released.
