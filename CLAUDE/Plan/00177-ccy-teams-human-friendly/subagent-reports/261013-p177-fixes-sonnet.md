# Plan 00177: qa-reviewer findings fixed (items 4, 5 and the advisories)

Branch commits, each test-first where a test could go red first:

1. **cc acceptance before the seat** (`cc-teams.bash`): `cc_teams_prepare` now runs seat choose, then
   the Element acceptance, then `seat take`. A decline creates nothing and makes no `add-member`
   call; an existing record is not asked about. `test-cc-teams.bash` has the order tests (declined
   means no seat call; accepted means record then seat; record present means no ask).
2. **`create --repo ... --yes` checks each repository**: it runs `check_repos` (existence and push
   access via gh, plus the format check) before declare.py writes. Every failing entry is named with
   its reason, exit 1, nothing written or installed. The test now asserts gh was asked and a
   read-only, a missing and a malformed repository fail the run.
3. **Acceptance path**: a test checks that `cc-teams.bash` and `helpers/pingbus/config.py` name the
   same path (in commit 1). It is a test, not a shared definition, because bash and Python cannot share a constant.
4. **Path-limit offer**: now says what the limit does and asks "Remove the limit, so any path in its
   repositories can be named?"; it no longer claims the limit was the default.
5. **declare.py and `localhost.yml`**: the text came from a missing `agent_bus_address`. That is a
   one-off fact about the machine (an IP the tool cannot know), so the tool asks for it. On a
   terminal, `ccy-teams create` asks (validated, bounded re-prompt, a step in the wizard) and passes
   `--address` to `declare.py create`, which writes it with the first team in one write (one backup).
   With `--yes` the refusal says to run `ccy-teams create` on a terminal; it never says to edit a
   file. New `declare.py address` and `check-address` commands; an existing address is never replaced.
6. **Container version**: the pingbus zipapp and the plugin skill are `COPY`ed into the image
   (`optional/`), and both changed since 955eaeca, so a bump is required: container 2.52, CCY 3.95.2,
   with the changelog entry.

Gates run: test-cc-teams 58/0, test-ccy-teams-command 301/0, test-ccy-teams 131/0, agent_bus pytest
379 passed, ruff and shellcheck clean. pingbus pytest: see the final message.
