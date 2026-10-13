# Plan 00177 package B: ccy-teams create (Tasks 4.2, 4.3, 4.4, 4.7, 4.9)

Worktree branch agent-a3d3c457eb34336bd-169ab6dd. F44 (42918ac5, packages A and C) merged
cleanly, no conflicts.

## What was already built (earlier WIP commits) and is now verified

- 4.9 scope question in `create` (files/home/.local/bin/ccy-teams.j2 `create_wizard`): inside a
  checkout "One repository: owner/repo" is the default, then "Everything one GitHub owner
  owns: owner", then "Several repositories". Team name defaults to the repo name (declare.py
  `suggest-team`, lower-cased, valid). Repos are one comma-separated input; `check_repos`
  checks syntax, `gh api repos/<o>/<r> --jq .permissions.push` (existence and write access;
  no token is read), then the list against declare.py's limits (32 entries). Each failing
  entry is named with its reason; the input comes back as typed; 3 tries (MAX_TRIES).
  `branches` absent means any branch (378a8332, teamfile/protocol/forge).
- 4.7 follow-up: `offer_path_limits` at picker start, once per team (declined marker under
  `$XDG_STATE_HOME/ccy-teams/declined`), removal through `declare.py drop-limit`, then
  `apply_teams`.
- 4.2: `declare.py element` writes the `agent_bus_element_teams` entry; `apply_teams` runs
  play-agent-bus-element.yml after play-agent-bus.yml; `after_create` offers to open Element.
  No text tells the owner to edit localhost.yml (declare.py next_steps checked).
- 4.3: `after_create` offers each person's sign-in via the existing `new_password`.
- 4.4: `offer_join` (after create) and `offer_join_here` (picker) offer
  `ccy --teams <team>` (no seat) via CCY_TEAMS_LAUNCHER, in the checkout's top level.

## This session

- Summary states the scope in plain words: "only owner/repo, on any branch", "only these
  repositories, on any branch: a, b", "everything owner owns: any repository, any branch".
- Tests rewritten for the new flow (scripts/test-ccy-teams-command.bash, 282 checks): default
  scope, owner scope, several repos with a read-only, a missing and a malformed entry, the
  list returned as typed, sign-in taken up and declined, session offer started with
  `launcher --teams proj` and declined, path-limit offer accepted, declined and not repeated.
- Two stale tests from the F44 merge fixed: test_wrapper (usage lists `seat choose`),
  test_seat (protocol row `seat exec [--root DIR] [--home DIR] [--] CMD…`).
- docs/agent-bus.md Getting started steps 1 to 3 rewritten. The protocol doc needed no change
  (the repos/branches rules were updated in 378a8332).
- meta-deploy.bash: added playbooks/imports/optional/common/play-agent-bus-element.yml.
- A Jinja trap: `${#array[@]}` contains `{#` (a Jinja comment) and breaks the template; it is
  avoided in ccy-teams.j2.

## Tests

- scripts/test-ccy-teams-command.bash: 282 passed, 0 failed (includes shellcheck of the
  rendered script).
- tests/helpers/agent_bus: 373 passed. tests/helpers/pingbus: 974 passed (run separately: both
  directories hold a test_cli.py, so one pytest run over both collides).
- ruff clean on touched Python. Not run: qa-all, the real gum/gh/Element (fakes only).

## Owner plays

play-agent-bus.yml, play-agent-bus-element.yml, play-claude-yolo.yml (all in meta-deploy).

## Open questions

- Two test_cli.py files make `pytest tests/helpers/agent_bus tests/helpers/pingbus` fail at
  collection; qa-helper-tests.bash runs them by module name, so the gate is unaffected.
- The real gum `choose` (scope question) was not measured through a pty here; the fake gum
  stands in for it.
