## QA Review: fix round d7dc8881e..141e5bc53 (Plan 00257 B.12 follow-ups)

**Verdict: FIX-BEFORE-MERGE**

The six earlier findings and the nit are mostly resolved, each with a test that fails without its fix. Two checks can still pass on a genuinely wrong state, and the deploy queue text is stale.

### Should fix

1. **Plan 00203 R9 passes when the unit is missing.** `CLAUDE/Plan/00203-teams-hot-control-and-robustness/_acceptance-robust.inc.bash:266`: `r9_unit_judged` accepts any `UnitFileState=` value, including an empty one. I sourced the function and ran it:
   - `r9_unit_judged "Restart=no UnitFileState= " ""` printed **missing unit PASSES**.
   - The same call with `UnitFileState=masked` printed **masked PASSES**.

   The old rule (`ActiveState=active`) failed on a missing unit; the new one does not. I could not run `systemctl show` here (no systemd in this container). But for a unit with no file, systemd reports `LoadState=not-found`, `Restart=no` and an empty `UnitFileState`, so a play that never wrote the unit passes R9. The scratch harness (`untracked/scratch/r9-judge-test.bash:20-26`) has no missing or masked case. Fix: add `--property=LoadState` and require `LoadState=loaded`, and require `UnitFileState` to be `static` or `disabled`.

2. **The no-autostart acceptance only guards the zero case, not the partial one.** `lxc-no-autostart-acceptance.bash:260-264`: it gives up with "could not establish" only when `v2Found == 0`. If 3 v2 service seats are declared and 1 unit is found, it prints the two counts and passes. There are two more problems:
   - The v1 count is circular: `v1Found` goes up after every `checkUnit` (`:238`) whatever the result, so the v1 line always reads n of n.
   - With every declared container offline, it judges nothing and still prints `PASS` (`:283`). The offline report is listed, but the headline claims a proof that was never made.

3. **`meta-deploy.list` descriptions are stale. The file is not touched in this range.**
   - `CLAUDE/Plan/meta-deploy.list:69-71` still names ccy 3.122.32 / container 2.84.38 and an unconditional Start line. The final version is 3.122.34 / 2.84.40.
   - `:79-83` calls the acceptance "read-only" (the script now runs in deploy mode and may reboot). It also says exit 2 means "a container not running", but a stopped container is now named offline and not judged.
   - Order is fine: teamchat, then claude-yolo, then agent-bus, then the acceptance, with Q15 last.

4. **The 3.122.34 changelog entry states something false.**
   - `docs/ccy-changelog.md:22-26` says the image changes because "the declaration reader it shares gained the service flag". The report repeats it (`noauto-followup-report.md:73`: "bus/declare is image source").
   - `files/var/local/claude-yolo/image-source-paths` does not list `src-go/internal/bus/declare`, and `go list -deps` of the three image commands pulls in only `tlsterm` from the touched packages.
   - So the container bump to 2.84.40 is unneeded and forces one extra image rebuild. Either drop that bump or correct the entry. The CCY_VERSION bump for ccy-teams is fine.

### Nits

- `CLAUDE/Plan/00209-agent-bus-v2-in-go/SPEC-n3.md:1187` (n3-D22) still lists `lxc.start.auto` among the keys read.
- `PROPOSAL-ssh-members.md:24,53,75,94` still list autostart as current. Only §4.5 has the note.
- If `lxc-stop` or `lxc-start` fails (`lxc-no-autostart-acceptance.bash:198-199`), the script exits with that tool's code, which can read as the exit-1 "session started" verdict instead of exit 2.

### Checked and clean

- **v2 stop before unlink:** `apply.go:1317-1337` lists the wants directory, stops the session, and removes the link only once the stop worked. `TestAFailedStopKeepsTheWantsLinkAndTheNextApplyStopsIt` would fail on the old order. A missing wants directory reads as an empty listing (`guestroot/read.go:97`).
- **`lxc.start.auto` dropped:** v1 `_autostarts` and `Host.autostarts` are gone, and so are v2 `KeyStartAuto` and `Config.StartAuto`. The remaining mentions in `lxccmd_test.go` and `guestfacts_test.go:334` are sample text for the generic reader. No dead code is left.
- **tlsterm hook:** `admitted` is an unexported field, nil in production. The cost is one nil check, and it is set before serving, so there is no race. The test's buffer of 10 matches the 10 connections it decides.
- **Offline-member gate:** now passes; the script uses `plan_lxc_online_named`. Exit 2 when a probe cannot answer is wired through `ask()`. Plan mode is `deploy`, and the script ends with `plan_finish`.
- **ccy-teams Start line:** `Starter.Offers` with tests for another host's member, a `service: false` member and an unreadable record. The declaration is read in-process, so there is no subprocess per row.
- **Docs:** `localhost.yml.dist`, `docs/agent-bus-lxc-members.md`, SPEC-n3 LX4 and the step-09 row, and the DESIGN-v2-C-host updates all match the new behaviour.
- **Versions:** CCY_VERSION, the Dockerfile label and REQUIRED_CONTAINER_VERSION agree at every bump, with one changelog entry per version.
- **Public repo:** no leaks found in the added lines. `work-laptop` is a test fixture.

### Mechanical gates

- `python3 -m unittest tests.helpers.agent_bus_lxc.test_offline_member_rule tests.helpers.agent_bus_lxc.test_members`: 189 tests, OK.
- `go test -race -count=1` over `teamchat/host/lxc/...`, `tlsterm/...`, `teams/...`, `bus/declare/...` and `teamchat/admin/...`: all pass.
- shellcheck on both changed scripts: clean.
- plan-qa `--sweep`: the worktree has no daemon binary, so I ran the main checkout's copy, which swept `/workspace`, not this branch's tree. It reported 2 blocking findings in plans 00244 and 00238, which this range does not touch, and none for 00257.
- `qa-all.bash` was not run: sub-agents are barred from full QA runs (R-SUBAGENT-FULL-QA).

Files:
- /workspace/untracked/worktrees/sealed-carry/CLAUDE/Plan/00203-teams-hot-control-and-robustness/_acceptance-robust.inc.bash
- /workspace/untracked/worktrees/sealed-carry/CLAUDE/Plan/00257-cross-host-lxc-team-members/lxc-no-autostart-acceptance.bash
- /workspace/untracked/worktrees/sealed-carry/CLAUDE/Plan/meta-deploy.list
- /workspace/untracked/worktrees/sealed-carry/docs/ccy-changelog.md