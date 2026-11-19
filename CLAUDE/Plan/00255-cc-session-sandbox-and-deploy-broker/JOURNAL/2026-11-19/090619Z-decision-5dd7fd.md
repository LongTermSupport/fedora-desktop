## 09:06 · decision · —

The qa-reviewer's pass over PR #349 found two blocking defects. Both are fixed here.

1. The declaration with no default stopped every interactive developer-desktop install that had no saved config, kickstart first boot included. run.bash keeps an existing `localhost.yml` only when it holds `!vault` or `github_accounts`, and the kickstart's file holds neither. So "Configure fresh" rewrote the file without the switch, and the assertion stopped the run.
   - The coordinator ruled that the stop was their design choice, not the owner's, and chose option (B): an undeclared developer desktop gets no grant, the same as false, and nothing stops.
   - The assertion (`tasks/assert-passwordless-sudo-declared.yml`) is removed, and so is the kickstart's `false` line, which run.bash's fresh path dropped anyway.
   - The owner is unaffected: this host declares true.
   - The headless gap (#348) goes with it, because the lab's desktop scenario no longer stops.
2. `play-ZZZ-passwordless-sudo.yml` could strand its own last task. On a host whose only grant was the `/etc/sudoers` block and which does not declare true, removing the block left the next task, which removes the kickstart's rule, with no grant and no password.
   - Each removal now runs only where its target exists, found by a probe while sudo still works.
   - When both exist, the block goes first, while the rule still grants.
   - `scripts/test-kickstart-sudo.bash` now walks the play's tasks over every combination of the starting grants. It went red on the old order: 11 cases, each stranded at the rule's removal.

Also fixed:

- The first-boot wrapper removes the rule with `sudo -n`, which never prompts. A refusal means no passwordless rule is in effect, and the wrapper says so instead of reporting an error.
- The docs that still described the old blanket grant now describe the declared one.
