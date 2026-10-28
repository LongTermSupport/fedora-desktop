# The owner's answers of 2026-10-28, recorded in the plans

The owner answered the unblocking list with "all recommended". Every recommendation in part A,
and part C's decisions, are now recorded in the plans they belong to. The work is in eleven
commits on `plan/00203-wind-down-compact`, on top of `fb1b3a32d`. Nothing is pushed or merged.
Each plan got a `decision` journal entry through `mkplan.bash --journal` that quotes "all
recommended" and names its item. `qa-changed.bash` passed before every commit.

## One failure: A3's switch-on (Plan 00191 Task 3.8)

- **Signed.** The `[ratchet]` table is at the top of `src-go/qa/record.toml`: `high_water = 27`, `decided_by = "repository-owner"`, `decided_on = 2026-10-28`, and Task 3.8's reason. The
  tier is unchanged (medium). The entries are not re-dated, since signing came one day after
  they were written.
- **Switch-on backed out.** With `[tools] record = true` and `suppression = true`, the command
  `scripts/qa-go-qa.bash -t record -t suppression` gave exit 1:
  - record passed (`go-qa record check`: 27 exceptions, all valid);
  - suppression FAILED, `gqaci.suppression.unrecorded` on two `.golangci.yml` exclusions added
    on 2026-10-27, after the record was written:
    - `.golangci.yml:225`: `internal/netpolicy/podman\.go` tagliatelle, rule
      `golangci.yml::exclusions.rules::tagliatelle/'(Pid|Running|State|ResolvConfPath|Privileged|HostConfig|EffectiveCaps|Networks|NetworkSettings)'`
      (Plan 00232, b1f8fa51c);
    - `.golangci.yml:271`: `cmd/reclaim/main_test\.go` gosec, rule
      `golangci.yml::exclusions.rules::gosec/G304|G306` (Plan 00192, e156f6aae).
- Both switches are back off, so `go-qa.toml` is unchanged and nothing was weakened.
- Recording the two exclusions takes the record to 29. That is past the signed `high_water = 27` and the `max_total` / `max_count = 27` caps, and raising the high water is the owner's
  call. An unticked sub-task under Task 3.8 holds the switch-on. Small leftover: the comment
  in `go-qa.toml` still says "Until it is signed the record fails". Reword it when switching on.

## Where each item went

| Item       | Plan: change                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A1         | New **Plan 00236** `subagents-cannot-speak-for-their-seat` (mkplan, README row): Phase 1, a DBF project PreToolUse handler, tests first, sub-agent scope as in R-SUBAGENT-FULL-QA / R-SUBAGENT-CRON-DELETE (`HandlerScope.SUB`), the command lists fail closed; Phase 2, the "no fork sub-agents for worker tasks" sentence in pingbus `role_coordinator` (chosen over AgentNotes: it reaches every coordinator); Phase 3, the review |
| A2         | 00203 Task 3.23: seat 2 accepts two throwaway LXC members, one coordinator and one worker. It is in 00203 because that plan built lxc-add's Role step and holds acceptance row M4 (e2). 00204 D6 is that step's design decision, still open there and not edited                                                                                                                                                                      |
| A3         | 00191 Task 3.8, above                                                                                                                                                                                                                                                                                                                                                                                                                 |
| A4         | 00234 Decision 1 approved, its success criterion ticked, Status and README updated; Task 2.2 (the model manager screen) left open                                                                                                                                                                                                                                                                                                     |
| A5, C5     | 00228 Task 6.5 is now the build task: hide post-processing on consumer installs, plus the VM check. Task 6.6 is now the build task: optional plays go on, with a summary and a non-zero exit, and main plays still stop. Decisions 4-9 stand                                                                                                                                                                                          |
| A6, C7, A4 | 00235: Q1-Q5 marked decided; new Q2b and new Task 4.4 (probe-then-remove root step in `play-podman.yml`, finding the volume by its property, logging what it removed, `podman system df` exits 0); Task 7.1 acceptance and the Non-Goals updated                                                                                                                                                                                      |
| A7         | 00201: an unticked sub-task under Task 2.7 makes step 08 the host agent's (rename and restore the consent file); Status updated                                                                                                                                                                                                                                                                                                       |
| A8         | 00192 Task 3.3 ssh-with-key entry, and SPEC-ssh-with-key.md H5: the refusal stands; seat 2's `ssh -G` check comes first                                                                                                                                                                                                                                                                                                               |
| C2         | 00194 Status and Task 2.0: H7 not applicable, Q2 closed                                                                                                                                                                                                                                                                                                                                                                               |
| C3         | 00192 Task 3.7: Q3 closed; follow-up Task 3.11                                                                                                                                                                                                                                                                                                                                                                                        |
| C4         | 00177 Task 10.6 (build, unticked); the Restore toggle decision recorded                                                                                                                                                                                                                                                                                                                                                               |
| C6         | 00203: the "Open" headings now say decided (alternatives taken for D5 and D6); Tasks 3.21 and 3.22; DECISIONS-lifecycle.md and SPEC-team-add-and-restart.md section 5 updated                                                                                                                                                                                                                                                         |
| C8         | 00214 Phase 5, Tasks 5.1 and 5.2. 00214 owns `changed_plays.judge`, the judgement `--changed`, the report and the panel share; 00141 was not edited                                                                                                                                                                                                                                                                                   |
| C9         | 00192 Task 3.12 (file the upstream issue), and PORTS.md's nord paragraph                                                                                                                                                                                                                                                                                                                                                              |
| vmtest H4  | 00192 Status and Task 3.3: H4 PASS on the host on 2026-10-28 by the host agent, with the attempt 1 timeout, attempt 2 at about 192 s, and Ctrl-C keeping `members/<name>`; FOLLOW-UP Tasks 3.13 to 3.15; a `finding` journal entry                                                                                                                                                                                                    |

## Notes

- Journal body files are in this worktree's `untracked/scratch/`, not `/workspace/untracked/scratch/`.
  The worktree write guard denied the path outside the worktree.
- The dedupe scout mkplan suggests was not dispatched, since starting sub-agents was ruled out.
  A grep of the README for sub-agent found no overlapping plan.
- The read-only reviewer's untracked file under 00203's `subagent-reports/` was left unstaged.
