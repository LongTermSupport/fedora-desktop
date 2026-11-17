# Task 3.3: fixes to the play side of LXC members, after review

Plan 00209 Task 3.3. Answers the QA review of `261117-lxc-play-report.md`. Placeholders only.
Every Go test below was seen red before its code, then green.

## 1. A team removed with its LXC entries (FIX-BEFORE-MERGE)

**Correction to the author's report**, finding 4 said "nothing breaks meanwhile". That was
wrong. A team set `state: absent` in the same run as its entries were deleted kept its guest
kit, bundles, session units and LXC record, because the absent phase acted only on present
teams and `team remove` never read the record. Its tokens were not revoked. A later redeclare
then failed every run on "no install stamp", because the absent phase met an unstamped team with
record entries before `team apply`.

**What it does now** (the clean removal, which the design supports: D 5.7 step 5 removes a team
and its entries in one step):

- `lxc apply --phase absent` acts on teams declared absent as well as present ones
  (`--only-teams` may name them). Every seat an absent team's record holds is undeclared, so it
  leaves by the existing steps (park, desired file, unit, L3 wait, `guest remove`) while the
  homeserver still runs. The play's absent phase gets `tc_lxc_absent_teams` = scoped present +
  `tc_scoped_absent`, and runs before `team remove`. So the D rollback's step 5 is now one run:
  set the team absent, delete its entries.
- A stopped container's seat is parked and kept `left`. The next run, with the team still
  declared absent, removes its guest files without parking again. This is tested.
- An absent team that is no longer installed (no stamp) cannot park. A seat not `left` is
  refused by name, with the way out given. A seat that is only `left` is cleaned from the guest.
- `team remove`, with or without `--purge`, refuses a team whose record holds a seat that is not
  `left`. The refusal is made before any change, at a new step `lxc seats` (drift), and names
  the seats and the play route. This guards the CLI and any wrong order. `left` seats do not
  stop the remove: their tokens are dead.
- A present team with no stamp (a first play, or a team redeclared after its removal) is passed
  by in the absent phase. Each seat that would leave prints `DEFERRED ... the team is not installed yet; the seat leaves at the next apply`. Before this change, a team with a record
  failed on "no install stamp".

Files: `host/lxc/absent.go` (`leavingTeams`, `leavingTeam`, `deferUninstalled`),
`host/lxc/apply.go` (`scope`, `installed`), `host/remove.go` (`lxcSeats`). Tests:
`TestAbsentTeamsSeatsLeaveBeforeItsRemoval`, `TestAbsentTeamsStoppedSeatIsParkedThenCleaned`,
`TestAbsentTeamNotInstalled`, `TestAbsentPassesByATeamNeverApplied` (extended), and
`TestRemoveRefusesATeamWithLiveLXCSeats`.

## 2. The url must be on the LXC bridge (FIX-BEFORE-MERGE)

- `declare.Input.BridgeURLProblem(bridge)`: for a team with a present LXC member, the url's host
  must be an IP address inside the bridge's (masked) subnet. A name is refused too. The bridge
  is asked only when such a member exists.
- `team apply` refuses at step 2 (refusals, drift) with "url ...: ... is not inside the LXC
  bridge's subnet ...; name the bridge's address in the url". The play's `team apply` task fails
  and shows it.
- `lxc apply --phase present` refuses at step 01, before any container is touched. This covers
  the CLI path.
- The play: the present task has `failed_when: rc != 0 or a line matches tc_reach_re`
  (`\toffline: guest check reach$`, the Go constant `lxc.WhyReach`). The new task "Show the LXC
  Members Held Back" prints both phases' SKIPPED/DEFERRED lines without `-v`. A stopped or
  missing container and a signed-out `gh` still never fail a run (n3b-D1).
- Test fixtures moved their team url onto their bridge: the lxc world (`192.0.2.1`), the admin
  LXC decl, and the host tests with an LXC member.

## 3. A committed render test (FIX-AFTER)

There was no Go Jinja renderer. The repository's pattern is `scripts/test-git-signing-declared.bash`,
which renders a play's own Jinja with the real ansible against localhost, under a sealed config.
The new `scripts/test-teamchat-play-jinja.bash` follows it. It is gate `teamchat-play-jinja`
(`scripts/qa-gates.tsv`, with a row in `CLAUDE/QA.md`), and `qa-changed` selects it for a change
to the play, which it names.

It reads the play as ansible parses it and builds a render playbook from the play's own vars,
input conditions, stdin templates, `failed_when` and held-back `msg`. It checks 43 things:

- every LXC input condition over 4 good and 11 bad cases, each bad case refused by its own
  condition (not a list, not a mapping, no seat or checkout, a string or boolean seat, a bad
  state, an absent or undeclared team, a duplicate, a duplicate with a different user);
- the teams each phase acts on, scoped and unscoped, and scoped to an absent team;
- the declaration's keys, with and without a site;
- the approve and apply stdin per team;
- `failed_when` over reach, stopped, gh and rc cases;
- the held-back lines.

Red proof: run with `TEAMCHAT_PLAY`, it fails on the commit before this one (the held-back task
is missing). A mutant play with `unique` and the absent teams dropped gives 6 failures.

## 4. NIT

`tc_lxc_absent_teams` now has a comment saying why it is spelled from `tc_present` and not
`tc_scoped_present`. The units step's `set_fact` replaces `tc_scoped_present` with every present
team after a base unit change, and a fact outranks the play variable. Following it would park
and remove the leaving seats of teams the run was not scoped to.

## Verification

- `go test ./internal/teamchat/...`: pass.
- golangci-lint over the changed packages: 0 issues.
- `ansible-playbook --syntax-check playbooks/imports/play-teamchat.yml`: pass. It was run with
  `ANSIBLE_VAULT_PASSWORD_FILE` pointing at a placeholder, because the worktree has no vault
  password file. That fault is already logged as issues #215 and #332, and the edit-time lint
  hook reports it on every play edit here.
- `scripts/test-teamchat-play-jinja.bash`: passed 43, failed 0.
- `./scripts/qa-changed.bash` passed, 24 files. The first run failed for two reasons. First, the
  qa-all golden replay needed the new gate listed in `src-go/qa/all/addedgates_test.go`, which
  is now done. Second, ansible-syntax could not run without the vault file. It passed once
  `ANSIBLE_VAULT_PASSWORD_FILE` was set to a placeholder, under the same issues #215 and #332.
- No play was run.

## Left

- The host run still needs the owner's host_vars, so nothing is queued in `meta-deploy.list`.
  This is unchanged from the author's report.
- An owner whose url was not on the bridge will now see `team apply` fail, naming the subnet.
  That is intended.
