# Closure assessment — plans 00122, 00075, 00063, 00082

Read-only assessment against the 2026-09-17 batch run. No file in the repo was
changed by this pass.

**Evidence availability.** Only **00122** produced a `20260917-*` run log
(`untracked/plan-runs/00122-…/{deploy,acceptance}/20260917-09*`). 00075's
`deploy.bash` is a legacy script that overwrites a single in-folder log — that log
carries today's date (07:57) and is usable. 00063's and 00082's `acceptance.bash`
are standalone scripts on no logging library: they reported PASS to the batch and
wrote nothing, so for those two the reasoning is from the code only.

---

## 00122 — lxcfreeze / podfreeze / the shared freeze library

**Verdict: CLOSEABLE.**

Today's `deploy.bash` ran `play-podfreeze.yml` then `play-lxcfreeze.yml`, both
`failed=0`. `acceptance.bash` created and destroyed its own throwaway LXC container
and reported `COVERAGE: 12 of 12 checks executed (33 passed, 0 failed) — ACCEPTED`.

### Unticked Success Criteria

| Criterion                                                                                                           | Disposition                                                                        | Evidence                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| ------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `lxcfreeze` freezes a running container and thaws it, verified with `lxc-info -s` rather than the tool's own report | **Now satisfied**                                                                  | Check [7] `PASS — lxc-info -s says FROZEN — the state really changed`; check [8] `PASS — lxc-info -s says RUNNING again`. The criterion's exact wording ("rather than by the tool's own report") is what those two lines assert.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| Running `lxcfreeze` twice toggles it                                                                                | **Now satisfied**                                                                  | Check [8] `PASS — lxc-info -s says RUNNING again — the same command toggled it back`                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| A refused or absent `sudo` produces a named failure, never an empty selection                                       | **Satisfied for "refused"; "absent" is not covered**                               | Check [10] `PASS — a refused sudo names itself and prints no list at all`. No check removes `sudo` from `PATH`. The criterion's two halves are not equally proven — the refused half is the one that actually happens on this host, and a machine with no `sudo` at all cannot run any of this plan's tooling, so the gap is theoretical. Tick with the wording narrowed to "refused", or accept as-is knowing which half the check covers.                                                                                                                                                                                                                                                                                                               |
| `lxc` not installed is reported as such, distinguishable from zero containers                                       | **Now satisfied**                                                                  | Check [11], both assertions: `PASS — it says LXC is not installed and names the play that installs it` and `PASS — and it does not claim the machine has no containers`. Two assertions for the two halves — this one is fully covered.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| After `lxcfreeze thaw`, ssh succeeds on the first attempt after a freeze longer than the one-hour lease             | **NOT establishable on this host in a test run** — should be RE-MARKED, not ticked | The script says so itself, in its own unnumbered "FOR THE HUMAN" block: *"Needs a real hour-long freeze and a container running sshd and NetworkManager. Check 8 proves the renewal runs on the thaw path and that a failed renewal is named — not that ssh then works."* Note check [6] deliberately used a container with **no** `nmcli`, so check [8] proves the renewal **fails** by name — it is the negative path. Task 5.2 already records a positive short-freeze observation on the host (dnsmasq `DHCPDISCOVER`→`DHCPACK` in the same second as the thaw) and says the overnight case is "confirmed the next time it happens". That is the honest disposition and the 26-09-16 journal handoff explicitly warns it "must not be waved through". |

### Unticked task

- **Task 4.6 (HOST)** — "both tools still behave as before, and `podfreeze`'s fzf path
  in particular". **Half satisfied.** The "both tools still start and run against the
  new shared library" half is now covered: check [1] proves all three deployed artefacts
  are byte-identical to their repo copies at the declared modes, [2] proves both tools
  resolve the library by the same relative hop, [4] proves `lxcfreeze list` and
  `podfreeze list` both ran and printed both sections, and [5] proves the shared
  decisions answer identically under both engines' state vocabularies. The **fzf
  keystroke** half is not and cannot be: check [3] asserts only that `fzf` is
  *installed*. The script's own human block says *"It needs a TTY and keystrokes … the
  keystroke behaviour itself is yours to try."* This is a **blocked-elsewhere** item —
  an interactive picker is not assertable by a non-interactive gate.

### JOURNAL handoff

`JOURNAL/00122-Journal-26-09-16.md` 13:20 names two outstanding items: Task 3.7 and
Task 4.6. **Task 3.7 is since resolved** — it is ticked ✅ and carried to Plan 00131,
with the stated reason that "a box that will never be ticked here makes a finished plan
read as incomplete". So the handoff is stale on 3.7 and live only on 4.6's fzf half.

The 16:55 entry records that `_planlib.inc.bash` gained `plan_on_cleanup` (1.2.0 →
1.3.0) as a by-product — already landed, not outstanding.

### What the last PLAN.md edit needs to say

1. Tick the four criteria today's run establishes (freeze/thaw by `lxc-info -s`;
   toggle; refused sudo; `lxc` absent), each citing the acceptance check number and
   the run `20260917-090221`.
2. Re-mark the >1-hour-lease ssh criterion as **🚫 not establishable by a gate** —
   needs a real hour-long freeze in a container running sshd and NetworkManager —
   rather than leaving an open box. Point at Task 5.2's short-freeze dnsmasq evidence
   as what IS established.
3. Re-mark Task 4.6 the same way: the library-resolution and both-tools-start half is
   discharged by checks [1][2][4][5]; the fzf keystroke half needs a TTY and is the
   owner's to try. Tick it as discharged-with-a-named-residue, or split it into 4.6a
   (done) and 4.6b (🚫 interactive).
4. Status → Complete; `git mv` to `Completed/`; update `CLAUDE/Plan/README.md`.

---

## 00075 — discarded failure signals: sweep and gate

**Verdict: CLOSEABLE, with one outward-facing residue that is the owner's to send.**

### Success Criteria

**All four are already ticked** and none depends on today's run. Today's `deploy.bash`
adds confirmation rather than closure: all four plays completed with `failed=0`
(recaps at log lines 246/350/490/557 — `ok=50 changed=3`, `ok=26 changed=7`,
`ok=34 changed=3`, `ok=10 changed=1`), so the host and repo now agree and the
`qa-deployed-drift.bash` failure the script itself warns about "BY DESIGN" is cleared.

### The one unticked task

- **Task 4.4** — post the drafted upstream report to the hooks-daemon tracker.
  **Genuinely outstanding, and correctly the owner's**: it is outward-facing (a public
  tracker), the text is written and scrubbed in `upstream-report.md`, and PLAN.md's own
  Delivery section already says *"Remaining: Task 4.4 only … it is the owner's to send"*.

  **Three things about it have drifted since it was drafted, and the plan does not say so:**

  1. **Finding 1 is still live, at a different line.** The report cites
     `.claude/init.sh` "lines 308–314". The daemon has been upgraded since; the code is
     now at **`.claude/init.sh:445-451`** and is byte-for-byte the same defect —
     `if PYTHON_CMD="$(resolve_venv_python …)"; then return 0; fi` followed by
     `local rv=$?`, which is always 0 because it reads the status of the `if`, not of
     its condition. That is incident #2 of this very plan, in daemon-owned code. The
     finding stands; the line numbers must be refreshed before filing or the issue
     points at unrelated code.
  2. **Finding 2 is unverified.** The `lint_on_edit` `exclude_paths` claim was
     established against an older daemon and was not re-checked in this pass.
  3. **A repo rule now governs how it may be filed.** `R-UPSTREAM-ISSUE-UNVERIFIED-BODY`
     requires the body of a hooks-daemon tracker issue to be produced by
     `hooks-daemon issue-report`, not hand-authored. A hand-drafted
     `upstream-report.md` pasted into `gh issue create` would be blocked. The draft is
     still useful as the source material, but the filing route has changed.

### JOURNAL handoff

`JOURNAL/00075-Journal-26-08-18.md` ends on the plan-script logging convention being
written into `PlanWorkflow.md` — a finished deliverable, not a handoff. Nothing
outstanding is named there beyond Task 4.4.

### What the last PLAN.md edit needs to say

Either (a) close the plan now, re-marking Task 4.4 as **🚫 owner's, outward-facing —
carried out of this plan** (the same treatment Task 3.7 got in 00122, and for the same
stated reason: an un-tickable box makes a finished plan read as incomplete), recording
the refreshed line numbers and the `issue-report` requirement so whoever files it is not
working from a stale draft; or (b) hold it open until the owner files. **(a) is
recommended** — every criterion is met, the gate shipped and is deployed, and the plan
is otherwise finished. If closed, the refreshed `init.sh:445-451` citation and the
`R-UPSTREAM-ISSUE-UNVERIFIED-BODY` note must travel into `Completed/` with it, or the
finding is effectively lost.

---

## 00063 — headless `run.bash` server/cloud provisioning

**Verdict: NOT YET closeable as "done" — but its Status line is right and the open items
are all one human host run.**

Today's `acceptance.bash` PASS establishes **nothing that was unticked**. Its own header
says so: it "exercises the HEADLESS PREFLIGHT fail-fast gates … as a NON-root user" and
"The full end-to-end unattended provision (gh token auth, ssh clone, ansible run) is
HOST-only (Phase 3) and is **NOT** covered here." So the PASS re-confirms Task 2.8, which
was already ✅.

### Status line, read in full

*"In Progress — code-complete and in production use: the owner reports the headless path
provisions the hosts of a separate infrastructure repository. Phase 3 is partly
discharged by Plan 00110's VM acceptance lab … Tasks 3.3 and 3.4 stay open and are to be
discharged properly rather than asserted."*

This is an accurate and unusually careful status. The plan explicitly refuses to let
production use stand in for the assertions, and says why: *"a real deployment proves the
clone works, which is not the same as proving the agent was torn down, the askpass helper
removed, or the secret files unlinked — none of which is visible from a run that
succeeded."* That reasoning is correct and should not be overridden.

### Unticked items

| Item                                                             | Disposition                                                                                                                                                                                                                                                                                                                                                                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Task 3.2** — desktop interactive path unchanged                | **Not establishable by any automated scenario.** The lab drives every guest with `RUN_BASH_HEADLESS=1` (`vmtest:918`), so no scenario — including `desktop-fresh-install` — exercises the interactive path. Needs a human at a terminal, by definition. Should be RE-MARKED 🚫 rather than left an open box.                                                                                                     |
| **Task 3.3** last sub-item — end-to-end with a real PAT          | **Still genuinely outstanding, with a built route.** `server-github-token` exists (Plan 00121, `e40d5c60`) and its checker is deployed at `files<home>/share/vmtest/guest-acceptance-server-github-token.bash`. It needs a human on the HOST with a scoped PAT on a throwaway account. The two sub-items above it are already ✅ by inspection.                                                            |
| **Task 3.4** — SSH path observations (4 sub-items)               | **Same route, same blocker.** Correctly argued as unreachable by the default six: they all set `RUN_BASH_GITHUB_ACCOUNTS=none` (confirmed at `vmtest:920` and `:1229`), so no key is loaded, no agent starts, and the assertions "would pass by absence". The `hl_ssh_agent_stop` code hole is closed and unit-tested (`scripts/test-run-bash-ssh-agent-teardown.bash`); what remains is observation in a guest. |
| **Criterion** — no secret bytes in env or cloud-init `user-data` | **Half satisfied, half needs the PAT run.** The env half is evidenced (`run.bash:373` unsets the literals, PAT on stdin at `:2284`). The `user-data` half cannot be proven by a scenario that never supplies a secret — the plan says exactly this. Assigned to `server-github-token`. Note the plan's own residual honesty: `hl_resolve_secret:134-140` still accepts a literal on a non-cloud box.             |
| **Criterion** — desktop interactive unchanged                    | Same as Task 3.2 — RE-MARK 🚫, not tick.                                                                                                                                                                                                                                                                                                                                                                         |

### JOURNAL handoff

`JOURNAL/00063-Journal-26-09-16.md` closes: *"No task moved. 3.2 needs a human at a
terminal, 3.3's last item needs a real PAT on the host and no agent handles one, 3.4
needs the same run. Still blocked — but blocked pointing at the right lines."* That
session's work was a citation audit (three of six references had drifted and were
corrected). The handoff is current and agrees with this assessment.

### The one thing standing in the way

**A human running `vmtest` `server-github-token` on the host with a scoped PAT on a
throwaway account.** That single run discharges Task 3.3's last item, all four of Task
3.4's sub-items, and the `user-data` half of the secrets criterion — its checker asserts
each obligation directly (agent gone, socket gone, askpass helper gone, transient
passphrase file gone, no secret bytes in any process environment, in cloud-init
`user-data`, or on disk). Task 3.2 and its matching criterion are **not** covered by it
and never will be; they should be re-marked as human-terminal-only.

---

## 00082 — `RUN_BASH_GITHUB_ACCOUNTS=none`

**Verdict: CLOSEABLE — and this is the finding of this pass. Both Phase 5 tasks were
discharged by Plan 00110's lab and nobody updated this plan.**

Today's `acceptance.bash` PASS re-confirms the already-ticked preflight criteria
(criterion 3, contradictory-config fail-fast; and the acceptance half of criterion 4).
It does not reach Phase 5 — it is preflight-only by design.

The closure comes from elsewhere. Plan 00110 (**Completed**) run
`20260913T170901Z-server-fast-provision` is a full headless provision of a fresh Fedora
44 Cloud Base guest with **`RUN_BASH_GITHUB_ACCOUNTS=none`** — the default is hardcoded
in `vmtest:920` (`session_provision`) and `:1229`, and only the opt-in
`SCENARIO_HOST_ONLY` branch at `:1230-1234` replaces it. The run produced
`RUN-BASH-EXIT 0`, a PLAY RECAP with work done, and 13 in-guest checks green; it was
repeated through the container-to-host bridge as `20260913T181420Z-server-fast-provision`.

### Unticked items

| Item                                                                                                                                     | Disposition                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| ---------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Task 5.1** — confirm on a HOST that `gh` is installed before `play-github-cli-multi.yml` runs, regardless of GitHub config             | **Now satisfied, and by something stronger than what the task asked for.** The task asked for `ansible-playbook --list-tasks`. What exists instead is a full `playbook-main.yml` run under `GITHUB_ACCOUNTS=none` that completed — which is the ordering claim proven by execution, not by a task listing. The specific assertion is `guest-acceptance-server.bash:158`, `check pass gh-cli-installed`, in a guest with no GitHub identity at all. If the static trace in the blocker analysis were wrong, that run would have aborted.                                                                                                                                                                                       |
| **Task 5.2** — full `GITHUB_ACCOUNTS=none` headless provision end-to-end                                                                 | **Now satisfied.** Run `20260913T170901Z-server-fast-provision`, plus the bridge repeat. The task's own note anticipated a *downstream* repo supplying this proof; the lab supplied it first, in-repo, with named assertions.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| **Criterion 1** — `none` passes preflight, proceeds through identity + vault + main-playbook with no GitHub/SSH setup, clones over HTTPS | **Now satisfied.** `guest-acceptance-server.bash` asserts `repo-cloned-at-pinned-commit` (:69), `localhost-yml-no-identity` — `grep -E '^github_accounts: \{\}$'` at :93, which is Task 2.3's exact deliverable — `no-ssh-passphrase-tmp-left` (:108), plus `ansible-available`, `provisioning-profile-server`, `podman-rootless-works`, `git-identity-configured`, `tmux-deployed`, `ccy-launcher-deployed`, `pyenv-installed`, `sshd-active` — i.e. the main playbook genuinely ran. **One precision**: the checker asserts a clone exists at the pinned commit; it does not name the transport. HTTPS is a sound inference (no SSH key is ever generated or loaded on this path) but it is an inference, not an assertion. |
| **Criterion 2** — existing GitHub-configured headless path and interactive path unchanged                                                | **Partly satisfied; the rest is blocked elsewhere and is 00063's blocker, not a defect here.** The `none` path not regressing the others is evidenced by both acceptance suites green in-container. The **GitHub-configured** path end-to-end is `server-github-token` — built, never run (00063 Task 3.3). The **interactive** path is covered by no scenario at all, since the lab forces `RUN_BASH_HEADLESS=1` (00063 Task 3.2). Both should be RE-MARKED as "covered by 00063 Phase 3, which owns them" rather than held open here — this plan added a branch alongside those paths and cannot be the plan that finally verifies them.                                                                                    |

### JOURNAL handoff

`JOURNAL/00082-Journal-26-08-24.md` ends at "Next: commit to
`task/run-bash-github-accounts-none` (local only, per the operator's earlier
instruction)". **That is stale and contradicts PLAN.md itself**: the Delivery section
still says the branch is "local, not yet pushed", while Task 5.2's own note records the
work as merged — `run.bash` downloaded "at this branch's merge commit (`68f1596`, PR
#33)". The Delivery & Milestones section must be corrected before closure, or the plan
travels into `Completed/` asserting its own work was never pushed.

### What the last PLAN.md edit needs to say

1. Tick **Task 5.1** and **Task 5.2**, citing Plan 00110 run
   `20260913T170901Z-server-fast-provision` (and the bridge repeat), the
   `GITHUB_ACCOUNTS=none` default at `vmtest:920`/`:1229`, and the named checks in
   `guest-acceptance-server.bash` — `gh-cli-installed:158` for 5.1,
   `repo-cloned-at-pinned-commit:69` + `localhost-yml-no-identity:93` for 5.2.
2. Tick **Criterion 1** on the same evidence, noting that HTTPS is inferred from the
   absence of any SSH key on that path rather than asserted by the checker.
3. Re-mark **Criterion 2**'s two residues as owned by 00063 Phase 3 (Tasks 3.2 and 3.3),
   with a cross-reference, rather than leaving an open box here.
4. **Fix the Delivery & Milestones section** — replace "local, not yet pushed" with the
   merge commit `68f1596` / PR #33.
5. Status → Complete; `git mv` to `Completed/`; update `CLAUDE/Plan/README.md`.

---

## Recommended order

1. **00082** — the highest-value and least-obvious close. Its Phase 5 has been
   dischargeable since 2026-09-13 and the plan never learned it. Fix the stale
   Delivery line in the same edit.
2. **00122** — a clean close off today's own 33/33 acceptance run, with two items
   re-marked as not-gate-establishable (the hour-long-lease ssh case, and fzf's
   keystrokes).
3. **00075** — close with Task 4.4 carried out as the owner's outward-facing residue,
   after refreshing the `init.sh` line numbers to `445-451` and recording that filing
   now goes through `hooks-daemon issue-report`.
4. **00063** — leave In Progress. Re-mark Task 3.2 and its matching criterion as
   human-terminal-only so the plan stops implying automatable work remains, then wait
   on the one `server-github-token` host run that closes everything else.

**Cross-cutting**: three of these four plans carry an item that is not establishable by
any gate on this host. Each should be re-marked in place, not left as an open checkbox —
00122's own Task 3.7 already sets the precedent and states the reason: *"a box that will
never be ticked here makes a finished plan read as incomplete."*
