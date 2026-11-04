## Requested changes

The class: an **unrecorded asset**, meaning valuable work kept only where git does not carry it to the remote.

You cannot tell scratch from an asset by reading the file. Headings, length and words like "report" or "decision" are trivially gamed, and journal and PR body files have them too. The robust signals are **consumption** (was the file used as input by a later command?) and **age**. Requested:

1. **A consumption ledger and a Stop/SubagentStop gate.** PostToolUse records every file a session creates under the scratch directory. It also records each one a later command consumes: `git commit -F`, `gh ... --body-file`, `mkplan.bash --journal`, `git apply`, or a capture read back with Read or grep. A Stop or SubagentStop with created-but-unconsumed scratch files is denied. The deny lists each file with three choices: consume it, move it to a tracked home, or delete it.

2. **A configurable scratch-shape allowlist and a registered-store list.** A PreToolUse deny covers any other new file under the ignored root, through Write/Edit and Bash-authored writes. The deny names the tracked home for the file's kind. Content signals only choose the suggestion, never the verdict.

3. **Stop steering into scratch.** `project_containment`'s guidance and source say the scratch directory is for consumed, short-lived files only, never durable work (today it says the opposite). `markdown_organization` stops listing the ignored root as a markdown home, except for the scratch shapes.

4. **`dispatch_declaration`:** a destination under an ignored path no longer counts as a declaration. The non-plan fallback becomes a configured *tracked* report directory, or a GitHub issue or PR comment.

5. **`subagent_report_persistence` stays as a crash net, never the record.** At SubagentStop, when the dispatch declared a tracked destination, check that the file exists (extend the report-path verifier). At SessionStart, report saved replies that have no tracked counterpart before retention prunes them.

6. **`destructive_git`:** deny `git worktree remove --force` while the worktree has uncommitted changes or commits no remote holds, as `git branch -D` is already denied for an unpushed branch. The patch-then-remove shape is in #152.

7. **A SessionStart sweep:**

   - scratch survivors older than a configurable age (24 hours by default);
   - unconsumed files from earlier sessions;
   - local branches with commits no remote holds;
   - dirty worktrees.

   Each comes with the same three choices.

Related: #152 covers the same loss through forgotten branches and worktrees.
