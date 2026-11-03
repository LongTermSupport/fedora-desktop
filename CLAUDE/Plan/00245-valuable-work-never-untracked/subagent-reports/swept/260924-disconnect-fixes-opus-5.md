# ccy --disconnect fix-forward (CCY 3.64.1), opus-5, 2026-09-24

Not plan work. This fixes the findings of the BLOCK review,
`untracked/agent-reports/260924-qa-reviewer-disconnect-opus-5.md`. It was copied here by
the coordinator from the fork's worktree, where it was gitignored.

- **Branch:** `agent-a7fb00b3b1b8338ac-693c0592`, based on `fdc81ac1`, commit `25620faa`.
  It was merged into F44 as `798374de`.
- **Versions:** `CCY_VERSION` 3.64.0 → 3.64.1. `network-management.bash` 1.9.0 → 1.9.1.

## Tests: RED, then GREEN

`scripts/test-ccy-network-disconnect.bash` grew from 45 cases to 82. Against the unchanged
library, 43 of the 82 failed; after the change all 82 pass. `test-ccy-session-registry.bash`
passes 128 of 128.

## Per item

1. **BLOCKING: an engine failure was read as "no containers".**
   - `_project_running_containers` now captures `container_cmd ps` on its own. If the
     engine fails, it prints `<engine> could not list the running containers` and returns 1.
   - The captured names are filtered with awk as a literal prefix. There is no
     `|| test $?` any more.
   - Both callers go through `_read_project_running_containers`. It fills an array and
     returns the status, so no `< <(…)` is left to hide a failure.
   - RED: with a `ps` stub returning 125, the old code let a bare `--disconnect` clear the
     saved default and exit 0.
   - GREEN, all with `ps` failing:
     - a bare `--disconnect` exits 1 and leaves the saved default;
     - a named `--disconnect` does the same, and calls the engine for nothing;
     - `--connect` exits 1 and saves nothing;
     - each one prints the engine's own error and names the failure.
2. **A container's last network.**
   - Every container on the target network is checked before anything is detached. If the
     network is any container's only one, nothing is detached.
   - The refusal says the session would lose all networking, the Claude API included. It
     names the container and says how to relaunch: `ccy --no-network`, or a plain `ccy`
     once no saved default names the network.
   - A saved default that names the network is still cleared, and the output says so. The
     command exits 1, because the container is still on that network.
   - Cases, each RED then GREEN:
     - a single-network container, with the matching default saved, and with a different
       default saved;
     - the same refusal when the network is picked from the list;
     - two containers where only one would be stranded, in which case neither is detached.
   - `docs/ccy.md` no longer implies that project networks are always safe to detach, and
     the troubleshooting row says the same.
3. **A bare `--disconnect` with no container running.**
   - It names the saved default, then asks `[y/N]` on `CCY_TTY`, which defaults to
     `/dev/tty`, so a piped stdin cannot answer for the user.
   - Only a yes clears the default. Each of these keeps it and exits 1: Enter, a no, end of
     input, three bad answers, or a terminal that cannot be opened.
   - Cases, all RED then GREEN:
     - y, n, Enter, and end of input;
     - a typo followed by y, and three bad answers;
     - no terminal;
     - a check that the answer comes from the terminal and not from stdin.
   - The prompt is registered in the session-registry test as unreachable from a restore,
     because `--disconnect` is dropped on replay.
4. **Nits.**
   - The `--custom` help line is aligned again.
   - Networks are read with `container inspect`.
   - The stub answers only the exact Go template, in the engine's real one-line output, so
     a changed template or call now fails the tests.

## QA

`./scripts/qa-all.bash` exits 0 on the fork, and again on F44 after the merge.

## Not done

- Nothing has run against a real podman. That needs the host deploy through
  `play-claude-yolo.yml`.
- podman's own behaviour when asked to detach a container's only network was not checked.
  The refusal happens before podman is called, so it does not depend on that.
