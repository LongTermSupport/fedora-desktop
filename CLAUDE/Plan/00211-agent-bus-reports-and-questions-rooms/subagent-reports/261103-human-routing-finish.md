# Task 2.14 finish: human routing branch

- Merged origin/F44 (conflicts: meta-deploy.list, ccy-changelog, claude-yolo). F44 sat at CCY
  3.104.17 and container 2.79.21; origin/feat/ccy-compact-close claims 3.105.1 and 2.79.21.
- Versions now CCY 3.104.18, container 2.79.22 (launcher, Dockerfile label), with a changelog
  entry above F44's 3.104.17. If compact-close lands first, its 3.105.x must be re-based above.
- meta-deploy.list: one `changed-plays` entry, naming play-agent-bus.yml and
  play-claude-yolo.yml.
- The WIP's unfinished QA surfaced two stale expectations in
  `tests/helpers/pingbus/test_plugin_contract.py` (the skill's rewritten rules and the example
  envelope's worker reply); updated to the new skill text. All 1853 pingbus tests were run
  before that fix with exactly those two failing; the module passes after it.
- Plan 00211 Task 2.14 left open only for the host run and a session restart.
