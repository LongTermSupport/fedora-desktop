# qa-reviewer: ccy --disconnect (610c6d85, merged as 403c3572), opus-5, 2026-09-24

Saved by the coordinator. Not plan work.

**Verdict: BLOCK.** The change is already merged, so it gets a fix-forward.

## Blocking

1. **An engine error counts as "no containers", and then the saved default is deleted.**
   - Where: `lib/network-management.bash:131`, the line
     `container_cmd ps … | grep … || test $? -eq 1`.
   - If `ps` fails, grep exits 1 and the check passes; the `< <(…)` also throws the
     status away.
   - The reviewer reproduced it with a `ps` stub returning 125. A bare
     `disconnect_from_network` printed "Cleared the saved default network", returned rc=0,
     and the file was gone.
   - Fix: capture `ps` on its own and fail if it fails, then filter the captured text.
     `--connect` shares the helper, so it gets the fix too.
   - Add a test where `ps` fails. The stub at `scripts/test-ccy-network-disconnect.bash:70-72`
     always returns 0.

## Should fix

2. **The picker will detach a container's only network.**
   - With a saved default, the launcher runs the container with `--network <saved>`
     instead of `podman` (`claude-yolo:2831-2835`, `:3214`).
   - So in the usual undo case, the network being removed is the container's only one. The
     filter at `:487-492` does not protect it, and the session would lose its route to the
     API.
   - Unverified against real podman: the reviewer had no engine, so it could not check
     whether podman refuses this.
   - `docs/ccy.md` implies project networks are safe to detach, which is wrong in this case.
   - Fix: refuse, or first connect `podman`/`bridge`, when the target is the last network.
     Add a single-network test; every current fixture includes `podman`.
3. **A bare `--disconnect` with no container running clears any saved default without
   asking** (`:546-551`).
   - With a container running, a bare call always goes through the picker; with none, it
     deletes silently.
   - Undoing that needs `--connect` with a running container, the only thing that saves a
     default (`:412/440/451`).
   - Together with finding 1, a stopped engine becomes a silent delete.
   - Fix: without a name, ask y/N (from `/dev/tty`, EOF means No), or require the name.

## Nits

4. The `--custom` help line lost a space and is misaligned (`claude-yolo:229`).
5. `inspect "$1"` (`:498`) would be safer as `container inspect`. The Go template is never
   exercised by the stub.

## Checked and clean

- **Picker:** 3 tries, re-prompts on a bad choice, EOF cancels (rc 1), prompts on stderr.
- **Engine refuses a disconnect:** rc 1, the engine's message is shown, the default is kept,
  and partial progress is reported.
- **Stderr:** the output goes to a person, so stdout is allowed.
- **Several containers:** it detaches only those on the network, the same as `--connect`
  with a name.
- **DRY:** `--connect` uses the shared discovery helper.
- **Replay:** `--disconnect` is dropped on replay, with a test, and it exits before the
  tmux/registry step. Using it with `--connect` is refused.
- **Versions:** `CCY_VERSION` 3.64.0 and lib 1.9.0. No image change, so no container bump is
  needed.
- **Tests:** they source the real lib, not a copy.
- **Docs and changelog:** they match the code, apart from finding 2. No identifiers leaked.

## Gates

- `qa-all.bash`: exit 1. The only failure is `patterns` on the untracked
  `CLAUDE/Plan/00138-…/probe-history.bash`, which is another session's work.
  `ccy-network-disconnect` passed 45 and `ccy-session-registry` passed 128.
- `plan-qa --sweep`: 0 blocking, and none of the advisories are about this change.
