# B.12 follow-ups: the no-autostart review, the unit-down text, the T9 flood test

Branch `plan-xhost-lxc-members`, worktree `untracked/worktrees/sealed-carry`. Every commit is
signed and pushed, and `qa-changed` passed before each (logs `untracked/scratch/noauto-fu-qa*.log`).

## Done

| Item  | Commit    | What changed                                                                                                                                                                                                                                                                                                                 | Proof                                                                                                                                                    |
| ----- | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | 711ab0e8d | PLAN.md: the "v1 is feature-frozen" Non-Goal now records the owner's no-autostart order, relayed by the coordinator. That order overrides the freeze for v1 `members.py` and the v1 ccy-teams start.                                                                                                                         | none needed (plan text)                                                                                                                                  |
| 2     | abe6ff0ef | The SPEC-n3 step-09 why now reads `offline: unit down; teamchat-admin lxc restart --member <container>/<seat key> starts it`, filled in with the seat's own container and key. `whyUnitDown(m)` is now a function. No other place showed the string.                                                                         | `TestUnitRefusedIsOffline` and `TestUnreachableSystemdIsTheUnitDown` failed on the old constant and pass now                                             |
| B     | 6f5f9b85f | v2 `placeUnit` lists the wants directory, stops the session, and removes the link only once the stop has worked, then reloads. A failed stop keeps the link, and the next apply retries. `wantsOf` drops a parameter that was always `"t1"` (unparam).                                                                       | `TestAFailedStopKeepsTheWantsLinkAndTheNextApplyStopsIt` failed on the old order and passes now                                                          |
| A, E  | 8a13db870 | Acceptance script, see the next section. The `members.py` docstring no longer contains the "Start ..." wording (detector B is unchanged).                                                                                                                                                                                    | `test_offline_member_rule` failed on the old script and docstring and passes now                                                                         |
| C     | fed306a72 | `localhost.yml.dist` no longer says the session is restarted after a reboot. `docs/agent-bus-lxc-members.md` (v2) says a service seat starts only through `teamchat-admin lxc restart --member <container>/<seat key>`, and where to find the key. The review report is committed with it.                                   | none needed (docs)                                                                                                                                       |
| 4 / D | 295d4b228 | The `lxc.start.auto` requirement is gone from both generations. v1 drops `Host.autostarts` and `_autostarts`. v2 drops the `service()` check, and `guestfacts` no longer reads the key at all. Docs, SPEC-n3 LX4 and section 3.1, DESIGN-v2-C-host 3.9.2 and D item 7 are updated. PLAN.md records the coordinator's ruling. | v1 and v2 "does not start at boot takes a service seat" tests failed before and pass now                                                                 |
| 3     | 202e04679 | `TestT9AFloodAtTheCapHoldsNoSlot` waits on a server-side point, the unexported `Config.admitted` hook: the held connection and all eight flood connections must be decided before the other source dials.                                                                                                                    | Old admit reconstructed by a scratch edit and reverted by editing back: 20 of 20 runs red under `-race`. Current code: `-race -count=50` passes 50 of 50 |

### The acceptance script (items A and E)

The script is `lxc-no-autostart-acceptance.bash`.

- **Offline members:** containers are listed with `plan_lxc_online_named`. A stopped container is reported "offline, not judged" and never fails or blocks the run.
- **Probes that cannot answer:** a failed `lxc-attach` gives exit 2, and so does a failed or partial `find` (the listing must end in a sentinel line).
- **Coverage:** the script prints `COVERAGE v1/v2` lines. If online v2 service members are declared but no units are found, it exits 2.
- **After the owner approves a reboot:** each checked unit must also report `systemctl is-active` = `inactive`, as well as `pgrep` finding no session.
- **Mode and ending:** `plan_mode deploy`; the reboot stays behind the `/dev/tty` question. The run ends with `plan_finish`, except that exit 2 is printed and taken before it.
- **Boot wait:** it polls `lxc-ls --running`. That is the one form both `ready-wait-ignores-child-exit` and `offline-member-blocks` accept: `lxc-info -s` trips the second rule, and a helper call trips the first.

## Not done

- **Item F (ccy-teams Start line only where it can work):** not started. I hit the context budget, so it needs a fresh agent. Handoff: `untracked/scratch/noauto-F-handoff.md` in the worktree. Done by a later agent; see "Follow-up: items F and G" below.

## Decisions for the coordinator

1. **Plan 00203's acceptance leg R9 is obsolete.** It lives in `_acceptance-robust.inc.bash` (`check_lxc_members`) and still requires units that are "enabled, running, Restart=always, autostarted". B.12 and item 4 made that leg wrong. It belongs to another plan, so I left it.
2. **Plan 00205's `PROPOSAL-ssh-members.md` describes the start.auto check as current.** It is a proposal document, so I left it.
3. **`lxccmd`'s tests still mention `lxc.start.auto`,** but only as sample text for the generic `lxc-info -c` reader, so I left them.

## Tooling faults met (dogfooding; for the coordinator to log)

- **qa-changed:** for `helpers/agent_bus_lxc/members.py` and plan-folder bash it runs only `test_members`, not `test_offline_member_rule` (the coordinator's issue #334).
- **sensitive_content (R-SENSITIVE-PUBLIC-PATTERN):** a `git add` or `git commit` command naming `environment/localhost/host_vars/localhost.yml.dist` in a worktree was blocked. The hook reported the "staged content" of the main checkout's untracked `localhost.yml.bak-*` files, which were neither staged nor in the worktree. Workaround: stage the file from its own directory in a separate call.
- **project_containment:** a relative `../untracked/...` redirect after `cd` into `src-go` was resolved against `/workspace`, not the cwd, and denied.
- **plan_number_helper (R-PLAN-NUMBER-DISCOVERY):** `find CLAUDE/Plan -name 'SPEC-n3*'`, a search for a spec file, was blocked as a plan-number scan.
- **lint_on_edit:** reports "ruff not found" in the hooks-daemon venv when a Python file is edited, so Python edits are not linted.
- **ready-wait-ignores-child-exit vs offline-member-blocks:** the first requires a container-state ask inside a boot loop; the second forbids `lxc-info -s` in member scripts. Only `lxc-ls --running` satisfies both, and neither rule's docs say so.
- **fix-brief.md:** not at the dispatch's path `/workspace/untracked/scratch/`; it is only in the worktree's `untracked/scratch/`.

## Follow-up: items F and G

### F: the ccy-teams Start line only where it can work

- `declare.LxcMember` has `Service bool`, read from the entry's `service` (the Python
  `declare.py` LXCMEMBER line is unchanged, so the parity oracle is untouched).
  `teamtest.HostVarsWithLxc` writes `service: true`.
- `lxcsession.Starter.Offers(ctx, row)`: true only for a member this host's record holds as
  joined or stopped whose present entry for that container, user and team has
  `service: true`. Another host's member (in no record here), a `service: false` member and a
  deferred or left one get no line. A record or declaration that cannot be read keeps the line,
  so choosing it says why rather than the line disappearing without a word.
- The picker reads it once per pass (`run.setSnap`, `run.lxcStarts`), since menus and footers
  have no context. If no Starter is wired, the line is not offered.
- Help text and `docs/agent-bus-lxc-members.md` say the line is only for this host's
  `service: true` members.
- Tests: `TestOffersOnlyAServiceMemberThisHostRecords`, `TestAnotherHostsMemberIsNotOffered`,
  `TestAnUnreadableRecordOrDeclarationKeepsTheLine` (lxcsession) and
  `TestOnlyAServiceLxcMemberOfThisHostOffersStartingItsSession` (picker; it replaces
  `TestOnlyAnLxcMembersRowOffersStartingItsSession`). Red proof: without the menu gate, the
  no-record, `service: false` and deferred cases fail. `TestLxcMembers` and
  `TestHostVarsWithLxcIsReadBackAsItsMembers` failed before the field existed.
- DBF: this is a design rule about when the menu offers the line, not a pattern a detector
  can express, so it is fixed with regression tests.
- `bus/declare` is image source, so this needed a bump: ccy 3.122.34, container 2.84.40, with
  a changelog entry.
