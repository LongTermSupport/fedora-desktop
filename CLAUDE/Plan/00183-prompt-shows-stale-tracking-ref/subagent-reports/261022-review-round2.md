Verdict: MERGE

The branch answers every finding from both earlier reports, and I found nothing new that blocks the merge. The syntax check passes (rc=0), `qa-version-pins.bash` prints VERSION-PINS-OK (13 of 13), and nothing install-specific appears anywhere in the diff.

**How each finding is answered**

- **qa-reviewer finding 1 (pin and assert):** answered.
  - The pin is a full SHA and the checkout runs with `update: true` and `force: false`. Because the play has `become: false` (play-git-configure-and-tools.yml:11), the checkout is owned by the login user, as the old `shell: git clone ~` was.
  - The assert matches `check_override` (check-fetch-age.bash:138-144). `select('match')` is anchored, so it does the job of the grep's `^`. "At least one call" and "none without `git fetch`" are both there. It fails closed if upstream renames the function or passes the command as a variable.
  - The assert runs before the bashrc block that writes the override.
- **qa-reviewer findings 2 and 3:** Success Criterion 1 now matches Goal 1. The milestones and Task 2.3 are brought up to date. The hashes c4cab141 and e40671403 both resolve.
- **qa-reviewer finding 4:** the passphrase point in docs/configuration.md is now one sentence.
- **qa-reviewer finding 5:** the play makes `~/.cache` itself (0700, login user) before the bashrc block, and the comment about the build task's side effect is gone.
- **Previous review finding 1 (FIX-FIRST):** answered. meta-deploy.list:154 is the play and :158 is the plan folder.
  - The order is right: the deploy comes before the read-only checks, and both come after play-python.yml, which also imports play-golang.yml.
  - The description is accurate: it moves the checkout to 4501746, every run fetches from GitHub, edited tracked files stop the play and untracked ones do not, it makes `~/.cache` at 0700, and it needs sudo once.
  - No second command is handed over. The hand check is a look at the screen, which the rules allow as a separate request.
- **Previous review finding 2 (FIX-FIRST):** answered. Task 2.3 now lists the meta-deploy run before the hand check. The JOURNAL corrects itself in a new `handoff` entry rather than by editing the earlier one, so it stays append-only. The 07:52 and 07:57 UTC times match the commits (08:52 and 08:58 at +01:00).
- **Previous review nits 3 and 4:** answered by the sentence added at docs/playbooks.md:267-268 and the comment at play:199-200.
- **vars/version-pins.yml:90-101:** the row is a valid manual row (`note` given, no `github`, `kind: untracked` with a `why`). Its wording is accurate.

**Findings**

1. **NIT** — CLAUDE/Plan/meta-deploy.list:155-158. The plan-folder entry runs `triage.bash` again as well as `acceptance.bash`. The triage already passed (Task 1.1), and the AgentNotes rule says "A run that would only repeat one already passed is not pending". The comment's reason ("so the acceptance's override and prompt legs judge the pinned checkout") only justifies the acceptance. The re-run also makes four network `ls-remote` calls the owner does not need.

   - Fix: change the entry to the single-script form `00183-prompt-shows-stale-tracking-ref/acceptance.bash`.
   - Then reword the comment, e.g. "Plan 00183 acceptance, after the play above, read-only: its override and prompt legs judge the pinned checkout. Afterwards, the owner's hand check (PLAN.md Task 2.3)."
   - If the triage's record of the new commit is actually wanted, keep the folder entry but say so in the comment.

2. **NIT** — PLAN.md:65-69 (Task 2.3 "Left:"). The list leaves out two steps that apply to these fixes: a full `qa-all.bash` run (it passed on c4cab141, which is before this branch) and a second `qa-reviewer` round on the fix branch. CLAUDE.md makes the qa-reviewer the required final step, and round 1 was FIX-FIRST.

   - Fix: add "`qa-all.bash` on the merged fixes, and `qa-reviewer` round 2" to the Left list, before the hand check.

3. **NIT** — JOURNAL/00183-Journal-26-10-22.md, 07:57 entry. "found ... that the earlier entry today overstated Task 2.3: only the hand check is NOT all that is left." The capitalised NOT reads as a garbled double negative.

   - The file is append-only, so leave it. Write any later entry plainly, e.g. "the hand check is not the only step left".

4. **NIT** — meta-deploy.list:13-14 says the `playbooks/<path>.yml` kind is for "a change no plan owns", and this play change belongs to Plan 00183. The play-python.yml entry for Plan 00191 (line 147) sets the same precedent, and adding a 00183 `deploy.bash` would put the deploy after the triage within that unit. So no change is needed now. If the rule is meant strictly, give 00183 a `deploy.bash` that runs the play.

The reviewed branch is fix/00183-review-findings in /workspace/.claude/worktrees/agent-a98896f05d7fae31a-5db0bfd3. The files the findings point at are /workspace/.claude/worktrees/agent-a98896f05d7fae31a-5db0bfd3/CLAUDE/Plan/meta-deploy.list and /workspace/.claude/worktrees/agent-a98896f05d7fae31a-5db0bfd3/CLAUDE/Plan/00183-prompt-shows-stale-tracking-ref/PLAN.md. I changed and wrote nothing.
