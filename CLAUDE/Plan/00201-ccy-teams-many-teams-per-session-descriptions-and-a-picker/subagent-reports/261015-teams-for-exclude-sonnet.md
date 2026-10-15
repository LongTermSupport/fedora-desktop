# teams_for ignored a team's `exclude` (Plan 00201)

Confirmed. `teams_for` in `helpers/agent_bus/declare.py` matched a team when its `org` equalled the
checkout's owner or the repository was listed, and never read `exclude`. A team trusting an org but
excluding a repository was therefore offered for that repository, although the bus refuses any ping
naming it (`excluded`).

Definition of `exclude`: `teamfile.py` `_exclude` and `protocol._parse_exclude` require an `org`,
accept lower-case repository names without owner, and forbid a name also listed in `repos`. The bus
applies it in `protocol.is_excluded(ref, org, exclude)`: `ref.owner == org and ref.repo in exclude`,
exact and case-sensitive, because the reference grammar admits only lower-case owner and repository
names. There is no case-insensitive matching to mirror.

Fix: `teams_for` builds a `Ref` with `protocol.parse_ref` and calls `protocol.is_excluded`, so there
is one definition. A listed repository still covers (the two cannot both name it).

Go side: `declare.TeamsFor` runs `declare.py teams-for` and parses its `TEAM` lines, so it needs no
change; its tests use a fake of that output. The new case is in the Python test.

Evidence: `TeamsForTest.test_an_org_team_does_not_cover_a_repository_it_excludes` failed before the
fix (`alpha` offered for `example-org/two`) and passes after.
