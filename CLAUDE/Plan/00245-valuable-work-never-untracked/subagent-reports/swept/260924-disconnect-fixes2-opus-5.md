# ccy --disconnect second fix-forward (CCY 3.64.2), opus-5, 2026-09-24

Not plan work. The coordinator copied this here from the fork's gitignored worktree
report. It fixes the review `untracked/agent-reports/260924-qa-reviewer-disconnect2-opus-5.md`.

- **Branch:** `agent-a9be2a4df8eede133-b13099ac`, commit `8c4a9814`, merged into F44 as
  `d67be5cd`.
- **Versions:** `CCY_VERSION` 3.64.1 → 3.64.2; `network-management.bash` 1.9.1 → 1.9.2.

## Tests

- `scripts/test-ccy-network-disconnect.bash` went from 82 cases to 129. Against 3.64.1,
  26 of the new cases failed and all 82 existing ones passed. Now all 129 pass.
- The registry test passes 128.
- `qa-all.bash` exits 0.

## Per item

1. **Quick Launch rejoined the network.** A later launch reads a network from three
   places. `--disconnect` now clears each of them when it names the network, through one
   routine, `_forget_network_for_later_launches`, which runs on every path:

   - the saved default;
   - the Quick Launch config's `LAST_NETWORK`, which is blanked only when it names the
     network. Every other line is kept byte for byte, and the rewrite is private and
     atomic. A rewrite that cannot be made fails the command and names the file.
   - `--network NET` in this project's restore records, through the new
     `ccy_registry_forget_network`.

   The message now reads: "Nothing saved for this project names NET now: a plain ccy, and
   a restore after a reboot, will not join it without asking." The "without asking" is
   deliberate, because compose auto-detection still offers a network with a Y/n prompt.

2. **No terminal to ask on:** a clear message with the reason, naming
   `ccy --disconnect <net>`.

3. **`connect_to_network` with `network ls`:** it lists the networks once, up front, and
   stops on an engine failure. Every later check reads that list, and the saved-default
   match is now exact.

## Other rejoin path

The session restore records were the one other place, and they are now covered. The only
writers of a network for a later launch are `save_network_preference`,
`save_launch_config` and the registry.

## Residual

- **A race with a closing session:** one whose trampoline removes its record between the
  read and the move gets the record back. The next restore would report it as a session
  that did not start. This is stated in the function's comment.
- **`get_expected_network_name`** still reads `network ls | grep`. It only picks the
  menu's suggested default, and the new up-front listing has already stopped on an engine
  failure before it is reached.
- **Real podman:** nothing has run against a real podman yet. That needs the host deploy.
