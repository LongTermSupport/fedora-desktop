# Plan 00254 Tasks 5.2 to 5.4: implementation report

Branch `plan-00254-phase5`, pushed. Nothing merged, nothing pushed to F44 or a `ci/` branch.
No host run: the play is queued in `CLAUDE/Plan/meta-deploy.list`. Tasks 5.2, 5.3 and 5.4 are
marked 🔄 in PLAN.md ("done in code, not established on a host").

## Commits

One commit, `Plan 00254 Tasks 5.2 to 5.4: per-team Lightning instances, the play split by client`, which also carries this report. A first, smaller commit of the element.py change alone
was rejected by the ccy image hook, because element.py is baked into the image. That change was
folded in here with the one version bump, so the image is bumped once. What the commit holds:

1. All of it:
   - **element.py `client`**: `PROFILE_KEYS` gains `client`. Its values are `element` (the
     default when absent) and `lightning`, and any other value is refused. Only Element entries
     are written. When a team moves to Lightning, element.py removes that team's Element
     launcher if the launcher is recognisably its own, and keeps the team's profile.
   - **Declare code**: declare.py `set_element` and `element-teams` and Go
     `HostVars.ElementTeams` already read past an unknown key and keep it. Nothing in them
     rejected `client:`, so no code changed. New tests show that `client: lightning` survives a
     base_url update and is read past.
   - **5.2**: `src-go/cmd/agent-bus-lightning`, a thin main over `src-go/internal/lightning`.
     The package doc lists its expectations as L1-L14, and each one has a test. The refusal of a
     user who holds pingbus bundles is `check_user` ported. Its consent rule is now exported once
     as `matrixclients.List.Accepted`, a port of `config.accepted_clients`. The base_url check
     reuses the bus/seat port of `check_base_url`, now exported as `seat.CheckBaseURL`, with
     `seat.IPHost` as the port of `_ip_hosts`. The command is in `scripts/go-binary-suites.tsv`
     (qa-go) and in the layout list of `src-go/CLAUDE.md`.
   - **5.3**: changes to `play-agent-bus-element.yml`:
     - Each entry's client is resolved with Jinja: the team's `client:`, else
       `agent_bus_matrix_client`, else `element`. Checked with `ansible localhost -m debug`.
     - The first read-only task after the list check is an assert that refuses any value other
       than `element` or `lightning`, and names the team.
     - element.py is given the resolved list.
     - Element's CA tasks get only the element teams, and Lightning's CA tasks only the
       lightning teams. The `ca_given` lists are split the same way.
     - After Lightning's CA proof, the play resolves the home, builds `agent-bus-lightning` with
       `tasks/build-go-command.yml` into `~/.local/libexec/agent-bus-lightning`, and runs
       `apply` as user_login. In --check it is skipped. The tags `agent-bus-element` and
       `agent-bus-lightning` are unchanged.
     - Docs: `localhost.yml.dist`, `docs/agent-bus.md`, `docs/agent-bus-cross-network.md` and
       `docs/playbooks.md`.
   - **5.4**: Lightning's `native` entry in both list copies is
     `{parent: .var/app/org.lightning_matrix.Lightning, name: teams, profiles: false}`. The cc
     deny rules (`cc-teams-settings.json`) and the kit launcher's `MATRIX_PROFILE_PATHS` now
     name it, as the #251 guard tests require. Two tests cover it:
     `TestLightningsTeamInstancesAreListed` in matrixclients, and
     `TestL14TheListCoversTheTeamRoots` in lightning, which ties the command's layout to the
     list.
   - **Version bump**: ccy 3.122.7, container 2.84.19 (`docs/ccy-changelog.md`).
   - **Deploy queue**: a `meta-deploy.list` entry for the play with
     `--tags agent-bus-element,agent-bus-lightning`.
   - **Plan state**: PLAN.md marks, and a journal entry written through
     `mkplan.bash --journal`.
2. This report.

## Tests and lint

- `cd src-go && go test -count=1 ./internal/lightning/ ./internal/matrixclients/ ./internal/bus/seat/ ./internal/bus/declare/ ./cmd/agent-bus-lightning/`
  passes. Coverage is 88.4% for lightning, 92.6% for matrixclients and 96.0% for seat. The
  floor is per module.
- `golangci-lint run` over those packages reports 0 issues.
- `python3 -m unittest tests.helpers.pingbus.test_matrix_clients tests.helpers.pingbus.test_config tests.helpers.pingbus.test_bundle tests.helpers.agent_bus.test_element tests.helpers.agent_bus.test_declare`
  ran 376 tests: OK, 1 skipped.
- `./scripts/qa-changed.bash` was run on the commit 1 set, then on the whole staged change.
  - First run: passed.
  - Second run: every check passed except `ansible-syntax`, including the go-qa diff mode, the
    bash limits, plan-tests and every suite. `ansible-syntax` could not run (exit 2) because a
    linked worktree has no vault password file. This is the known issue #215.
  - In its place, `ansible-playbook --syntax-check` was run on the play with
    `ANSIBLE_VAULT_PASSWORD_FILE` pointing at a dummy non-empty file under `untracked/scratch/`,
    and it passes. `ANSIBLE_VAULT_PASSWORD_FILE=<dummy> ./scripts/qa-changed.bash --files playbooks/imports/optional/common/play-agent-bus-element.yml`
    then passed: ansible-syntax, the other gates, and test-ccy-teams-command.bash (395).
- TDD:
  - element.py: the tests were written first and seen red (6 failures, 6 errors), then green.
  - Go: each test file was written before its source.
    - lightning: green on the first build, so the only red was the missing package.
    - matrixclients `Accepted`: red as undefined, then green.

## Decisions not answered by the design note (all in the journal entry)

- A KEPT line names the team's instance folder `teams/<team>`, on every run, for a team the
  list does not declare. A team declared for Element keeps its instance without a KEPT line.
- A team's Element profile is kept if the team was last declared for Lightning and then dropped
  from the list. Prune finds teams by their Element launcher, and the switch to Lightning
  removed that launcher.
- A home path that an Exec line would have to quote is refused, but only when a Lightning team
  is to be written, so an Element-only host is never failed by it.
- The Lightning command runs after the CA proof, so no launcher is written for an untrusted CA.
- If Lightning creates the settings file between the command's check and its write, the
  command leaves that file alone. It writes by link, never by rename.
- An entry without `client` counts as an Element team in both element.py and the Go command.
- Container 2.84.19 skips 2.84.18 in case the branch taking ccy 3.122.6 also takes 2.84.18.
- Lightning's app id is read from the matrixclients list, not spelled in the package, so the
  named-by-hand rule needs no new row.

## Left undone

- Task 5.5 (acceptance.bash) was not mine to do.
- Host runs: the deploy is queued. The team's host agent runs `./CLAUDE/Plan/meta-deploy.bash`.
- The existing private ports of `accepted_clients`, in pingbus statusline and the ccy-teams
  chooser, were not changed to use `List.Accepted`. Each is shaped for its caller: statusline's
  parity world, and the chooser's reasons.
