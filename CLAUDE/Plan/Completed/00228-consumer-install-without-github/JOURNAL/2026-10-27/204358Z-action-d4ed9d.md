## 20:43 · action · —   — Final review fixes, tests first

The final qa-reviewer findings (subagent-reports/261027-final-qa-review.md) fixed, tests first:

- Server profile refused (Task 2.8): TestAServerProfileIsRefused, TestABootTargetThatCannotBeReadStopsTheRun and the two systemd DefaultTarget tests seen red (undefined method; refusals not made), then green. The refusal tests now count only commands other than the read-only `systemctl get-default` as changes.
- Empty vault file (Task 2.9): TestAnEmptyVaultPasswordIsMadeAgain and TestOptionalOnlyWithAnEmptyVaultPasswordIsRefused seen red, then green.
- Task 6.5 safe part: test_no_claude_says_post_processing_needs_the_developer_install seen red ("claude CLI not found in PATH"), then green. Decision: gate at wsi-claude-process, the one script every Claude item runs, which checks for `claude` before wsi-claude-token is reached, so wsi-claude-token is unchanged. Hiding the items was not chosen: clearing the keybindings makes the extension warn at every login that the keys do nothing, and hiding the menu items needs a new setting in the extension.
- The guest checker's podman check only looked at the rpm, but play-podman.yml installs podman-compose with pip --user; it now also checks ~/.local/bin/podman-compose.
