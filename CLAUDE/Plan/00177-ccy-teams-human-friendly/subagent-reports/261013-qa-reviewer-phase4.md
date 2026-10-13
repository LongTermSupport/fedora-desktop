# QA review: Plan 00177 Phase 4 (955eaeca..HEAD, without Plan 00171)

Reviewer: `qa-reviewer` agent. It cannot write files, so the coordinator saved its reply here.

**Verdict: FIX-BEFORE-MERGE.**

## FIX-BEFORE-MERGE

1. **QA is red on HEAD.** `qa-all3.log` ends `EXIT 1` (line 187, `ccy-compose-restore`). The new attended check at `claude-yolo:872` (commit dfba9fd9) satisfies the guard heuristic in `scripts/test-ccy-compose-restore.bash:377,386-392`, so removing the quick-launch guard at `claude-yolo:1267` is no longer caught. Before this range, the same test caught it at line 1251.
2. **The meta-deploy entry will fail on the host.** `CLAUDE/Plan/meta-deploy.bash:58-60` runs `play-agent-bus-element.yml`, which refuses to run with no `agent_bus_element_teams` (`play-agent-bus-element.yml:41-49`), and the host vars declare none. The play has not changed since e1d38711, and create and Ctrl-E run it themselves, so drop the entry.
3. **PLAN.md does not match the merged code.** Tasks 4.2, 4.3, 4.4 and 4.9 shipped in a705023f but still say "Not started" (`PLAN.md:72-96`). Task 4.10 still says the acceptance is "per seat" (`PLAN.md:108`), but d58c4350 made it once per user.
4. **cc takes the seat before the owner accepts.** In `cc-teams.bash:220-221` the seat is taken (one `sudo agent-bus add-member`) before the confirm. If the owner declines, the new member stays, while `docs/agent-bus.md:~391` says declining "starts nothing and records nothing". `cc_teams_prepare`, which sets this order, has no test.
5. **The write-access check covers only the interactive path.** `create --repo … --yes` writes repositories without asking GitHub for push access. The test at `test-ccy-teams-command.bash:851-856` passes that case, which contradicts `docs/agent-bus.md:31-33`.

## Advisories

- The acceptance file's path is defined twice (`cc-teams.bash:26-27` and `config.py:67-68`), and no test checks the two agree.
- The path-limit offer tells every limited team that the limit "was the default" (`ccy-teams.j2:~1517`), which is wrong for a limit set on purpose.
- Older code that predates this range (`declare.py:623-628`) still tells the owner to set a key in `localhost.yml`.
- The skill file and pingbus are built into the image, but its version label stays at 2.51.

## Clean

- The Element deny rules apply to cc only, through `--settings`; no project settings changed.
- Nothing the session reads mentions Element or the owner's sign-in.
- The Element acceptance is asked once per user.
- No new text tells anyone to edit `localhost.yml`.
- No install-specific names found.
- CCY 3.95.0 is bumped, with a changelog entry.
- The cc files are installed in the right place.

## Gates

`test-cc-teams.bash` 47/0, `test-ccy-teams-command.bash` 282/0, syntax-check OK, plan-qa 0 block / 15 advise.

## Coordinator's resolution

- Items 1 and 2: fixed in 335efdf0 (CCY 3.95.1).
- Item 3: fixed in the PLAN.md update.
- Items 4, 5 and the advisories: handed to a fix-up sub-agent.
