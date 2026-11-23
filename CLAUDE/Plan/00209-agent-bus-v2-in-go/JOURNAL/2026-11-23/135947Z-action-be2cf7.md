## 13:59 · action · — — The v2 review fixed on v2-review-fixes

The independent review of the v2 integration branch (subagent-reports/v2-review.md, verdict
FIX-BEFORE-MERGE) was fixed on branch `v2-review-fixes`, cut from origin/v2:

- Blocker 1: the host kit is placed atomically. `kit.Place` assembles it in the sibling
  `<digest>.assembling` (cleared first), renames it into place, leaves a whole kit alone and
  replaces one that is not whole. `teamchat kit assemble` prints `assembled` or `whole`. The play
  dropped the digest mkdir and the `creates` guard, reads its change from stdout, and asserts
  every part (`kit.Parts`) before the `current` link moves. Tests cover each failure point.
- Finding 2: Plan 00263's PLAN.md and DESIGN.md took abf160389's text (D3 answered yes, Q64).
- Findings 3, 4, 9: the deny file names the heavy QA scripts and leaves
  `qa-deployed-drift.bash`. `run.bash qa` is gone. QA.md, DESIGN-host-cc-v2.md and
  agent-bus-cross-network.md say a host seat cannot run the heavy QA, why, and that the
  rules apply whatever the duty.
- Finding 5: one changelog entry for the v2 batch, CCY 3.124.7, container 2.84.58.
- Finding 6: the meta-deploy.list text names 3.124.7, seat duty and the R0 verbs.
  `acceptance-host-cc-v2.bash` runs its deployed checks unattended under meta-deploy
  (PLAN_ASSUME_YES, or no terminal) and exits 0 when they hold. It lists the team, checkout and
  section 8 steps as ATTENDED, which are run by hand.
- Finding 7: DESIGN-v2-B-member.md's agent verb table matches the code.
- Finding 8: Task 4.6 marked built; Task 4.7 says what is merged.
- Nit 10: `cc -- <prompt>` exited 64. Only `-- claude` opens the wrapper form now.
- Nit 11: the bash suite's greps of ccrun.go became a Go test that runs cc-run.

Not run: `qa-ansible-syntax.bash` on play-teamchat.yml. A bare `git worktree add` lacks the
vault password file the gate needs, and the secret guard refuses any command that names it,
so it cannot be linked in. `qa-ansible.bash` (static) passed.
