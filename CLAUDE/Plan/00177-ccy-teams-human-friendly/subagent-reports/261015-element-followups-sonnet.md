# Task 7.3 Element review follow-ups

- Docs: the play header, docs/playbooks.md and Plan 00161 DESIGN.md (P8, U25) now point to
  `check_user` in `helpers/agent_bus/element.py` as the rule and name the `cc --teams` exception.
- `check_user` gives an invalid-acceptance-record case (folder holds only `element-accepted`,
  but it fails `element_accepted`) its own message, not "holds pingbus bundles".
- Tests added in `tests/helpers/agent_bus/test_element.py`: message for a world-readable record
  (red before the code change), symlinked folder, dotfile beside the record, record as a
  directory, record as a symlink. The four pure-refusal cases already held and pass.
- `python3 -m pytest tests/helpers/agent_bus/test_element.py`: 35 passed.
