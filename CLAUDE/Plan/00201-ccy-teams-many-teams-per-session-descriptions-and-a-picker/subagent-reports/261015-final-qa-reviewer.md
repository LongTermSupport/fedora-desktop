**Verdict: FIX-BEFORE-MERGE.** Nothing blocks, but one promised feature works only in this repository, and the docs and the owner's acceptance steps say otherwise. The plan is not ready to mark Complete.

**I did not write the report file.** My role rules forbid writes, including redirection, and I have no Write tool. The full findings are below. I did write one log file I shouldn't have: I sent the QA run's output to `/workspace/untracked/scratch/qa201-all.log`. It is untracked, and you can delete it.

### Should fix
1. **The `bus: A,B` status line only appears in fedora-desktop checkouts.** The `bus_teams` handler is registered only in this repository's `.claude/hooks-daemon.yaml:382-386`, and nothing under `playbooks/` or `files/` deploys it anywhere else. But `docs/ccy.md:1095-1096` and `docs/agent-bus.md:72` say every session shows it.
   - The owner's steps 05 and 09 (`ACCEPTANCE-owner.md:14,24`) expect it in "a GitHub checkout neither covers". Unless that checkout is fedora-desktop, those steps will see no segment.
   - The automated check M3 passed only because it ran in this checkout (`acceptance.bash:15-16`), the one place the handler exists.
   - Fix: either deploy the handler to the projects that use `--teams`, or scope the spec (S-C6), the docs and the owner steps to this repository.
2. **The status-line handler's tests run in no QA gate.** `PLAN.md:93-97` says they need an installed hooks daemon and leaves them out, with no follow-up task. I ran them in this container with the daemon's venv python (`python -m unittest test_bus_teams`): 14 tests, OK. So the daemon they need is installed here, and they could be wired into a gate or tracked as an open task.
3. **A failed per-session record looks the same as "no teams".** In `helpers/pingbus/hooks.py:484-489`, when `record_session_teams` raises `OSError` the SessionStart hook writes a warning to stderr and carries on. The status line then shows nothing, exactly as it does for a session on no team. That is the skip-and-warn pattern, so a missing segment cannot tell a broken record from an empty one.

### Nits
4. **The spec and a config comment still describe the old design.** `SPEC-many-teams.md:25` (Q7) and `:176-177` (S-C6) say the segment comes from `PINGBUS_TEAMS`, and so does the comment at `.claude/hooks-daemon.yaml:382`. It now reads a record kept per session. S-A8 was revised when this changed; S-C6 was not.
5. **The spec says `say` can infer the team from `--re`, but `say` has no `--re` option.** Only `validate` and `send` take `--re` (`helpers/pingbus/cli.py:1731,1749`), and the protocol doc correctly says "`send` only". The wrong claim is at `SPEC-many-teams.md:182`.
6. **`PLAN.md`'s "Delivery & Milestones" list stops at M7.** It leaves out the final review-fixes merge `3a4014b0` and the meta-deploy queue commit `43d6b861`.
7. **`cc --teams` creates `.claude/ccy/` before asking for consent.** `cc-teams.bash:161` runs before the consent question at `:163`, so a declined start still leaves the directory and its `.gitignore`. S-E4 says "nothing made".

### Checked and clean
- **Placement:** no playbook was added or changed. The chooser is in Go (`ccy-teams choose-teams`), both launchers call it, and the old bash chooser code is gone from the launcher, its libraries and `cc-teams.bash`.
- **Version bumps:** `CCY_VERSION` is 3.98.5. `REQUIRED_CONTAINER_VERSION` and the Dockerfile label both say 2.60. The changelog has an entry for every 3.97.x and 3.98.x release. The ccy play ran (`06914d00`), and the meta-deploy queue has nothing outstanding for 00201.
- **Spec coverage:**
  - S-A4 is tested in `test-agent-bus-install.bash:627-641`: no restart, and both config files unchanged byte for byte.
  - The descriptions, the multi-select chooser, scope widening (all added in one play run, and nothing started when the apply fails), the three-round limit and Esc (nothing chosen, exit 0) each have Go tests.
  - S-A15 and S-B15 (the picker's offers), `--re`/`--to` team inference, and the consent rules (symlinks, file mode, owner) are tested too.
- **Verification:**
  - The automated acceptance checks that the running daemon lists `bus-teams`, not only that the module imports.
  - The live check of S-C10 (a ping waking the session, and a reply inferred from `--re`) is left to owner step 06. That is correct, since Task 2.7 and the success criteria are still open.
- **Fail-fast in the plan scripts:** no `|| true`, `2>/dev/null` or `set +e` in `acceptance.bash` or `_acceptance-m7.inc.bash`.
- **Public-repo safety:** only placeholders (`example-org`, `alpha`/`beta`, `accept201a`/`accept201b`) appear in the plan folder, the docs and the `localhost.yml.dist` changes.
- **Plan state:** the README has the row, the status is In Progress with Task 2.7 open, and plan-qa reports nothing for 00201.

### Mechanical gates
- **`qa-all.bash`:** passed, exit 0, "QA passed: 1455 files checked". It printed warnings for shellcheck and semgrep's partial parsing, none for files this plan changed. The suites `ccy-teams` (166), `cc-teams` (57), `ccy-teams-command` (344), `agent-bus-install` (283) and helper-tests (4312) all passed.
- **`plan-qa --sweep`:** 0 block, 16 advise, none of them for 00201.
- **`--syntax-check`:** not needed, because the plan changed no playbook. `qa-all` ran its ansible-syntax gate anyway: 83 playbooks OK.
- **`qa-helper-tests`:** covered inside `qa-all`. The extension gates were not triggered.