# Task 2.30: the final qa-reviewer's findings at fe894347c

Branch `fix/00201-final-review-findings`, cut from F44 fe894347c. Source: the final
qa-reviewer report (FIX-BEFORE-COMPLETE, five should-fix items and two nits).

## Should fix

1. **Status line** (PLAN.md): Tasks 2.28 and 2.29 are said merged at fe894347c, each waiting
   on its host check, with the host agent's reruns listed after `play-claude-yolo.yml`.
2. **meta-deploy description** (`CLAUDE/Plan/meta-deploy.list`): the comment above
   `changed-plays` now names Tasks 2.28 and 2.29 (play-claude-yolo.yml rebuilds ccy-teams
   before the host reruns of steps 03, 10, 12 and 13 and lxc-add's Stop), Plan 00177 Task 9.9
   in the same ccy-teams build, CCY 3.100.33 / container 2.79.15, and Task 2.30's play text
   change. The entry itself is unchanged.
3. **Docs**, each checked against the code:
   - `docs/agent-bus.md` shutdown: the "no session running, nothing to shut down" result
     (`shutdown.go` `whyNoSession`, listed as idle, not counted as a failure).
   - A No or Stop exits 0: `docs/agent-bus.md` (the ccy-teams introduction: create, lxc-add,
     remove, shutdown, restart, wind-down; Esc/Ctrl-C 130; a declined create from
     `ccy --teams` goes back to the list; the `cc --teams` consent No stays exit 1, as Task
     2.29's report records) and `docs/agent-bus-lxc-members.md` (lxc-add's review No and its
     "Update entry / Stop").
   - `docs/ccy.md`: `~/.local/state/ccy/seat-checkouts`, beside the session records, with its
     purpose, its writers and the pruning below.
4. **Task 2.28** set to 🔄: built and merged, waiting on the host rerun of steps 03, 10 and 13.
5. **Success Criteria** ticked, each citing the d247e4b2f steps that prove it: 01, 02 and 04
   (uncovered checkout to a session on the team); 04 and 05 (two teams, launch block and
   `pingbus status`); 01, 04, 05 and 07 plus D1-D3 (descriptions). None depends on step 03's
   rerun, step 08's consent or check 11.

## Nits

6. **seat-checkouts never pruned.** Duplicates were already avoided (`Add` returns when the
   checkout is listed; only two concurrent writers can leave a line twice, read as once). Now
   an `Add` that finds a listed checkout gone from the disk (`Lstat` not-exist; a permission
   error keeps it) writes the list whole without it, renamed into place (`fsx.WriteAtomic`,
   0600). Test first: `TestAddDropsCheckoutsThatNoLongerExist` red (the gone line stayed,
   twice), then green. `TestAddKeepsEachCheckoutOnceInTheOrderAdded` now uses real directories,
   since `/home/u/...` would be pruned. Known limit: an append by another process between the
   read and the rename of a prune is lost; the next board pass re-adds a checkout whose session
   still runs.
7. **Assert messages name the wrong file.** With an override, `Config.PlayVars` now gives
   `-e @<file> -e {"agent_bus_host_vars_file":"<file>"}` (JSON-encoded); the three messages
   (`play-agent-bus.yml` agent_bus_teams and agent_bus_lxc_bridge,
   `play-agent-bus-element.yml` agent_bus_element_teams) name
   `agent_bus_host_vars_file | default('environment/localhost/host_vars/localhost.yml')`.
   Red: config and ops tests (five failures), then green; `scripts/test-ccy-teams-command.bash`
   fakes expect the new argument (`played_for` helper): 383 passed, 0 failed. Play change
   noted in meta-deploy.

## Versions

No CCY bump: `files/var/local/claude-yolo/claude-yolo` is unchanged and ccy-teams is not in
the launcher's build (the reviewer's `go list -deps` check). ccy-teams has no changelog or
version of its own.

## Not done here

The reviewer's unestablished gates (qa-all, qa-helper-tests for Task 2.27) are the
coordinator's: this worker ran `qa-changed.bash` only, as instructed.
