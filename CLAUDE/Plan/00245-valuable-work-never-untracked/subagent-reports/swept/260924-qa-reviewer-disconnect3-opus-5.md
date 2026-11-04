# qa-reviewer: ccy --disconnect 3.64.2 (8c4a9814, merged as d67be5cd), opus-5, 2026-09-24

Saved by the coordinator. Not plan work.

**Verdict: FIX-BEFORE-MERGE** (already merged, so these are fix-forwards). The reviewer
found no fourth place a network comes back from, and the three stores are covered. The
Quick Launch rewrite is atomic, private (mode 600) and fails fast.

## Should fix

1. **A record ending in an empty argument is rewritten wrongly.**
   - `lib/session-registry.bash:335-337` collects the walk with `walked=$(…)`, which
     drops trailing newlines, and then compares array lengths.
   - Run in memory, `--token t -- --append-system-prompt ""` went in as 5 words and came
     out as 4, with no `--network` in it.
   - So a ccy record for this project that ends in `""` loses that argument, and the output
     falsely says "Removed --network NET…".
   - `--check` also counts such a record as remembered (`network-management.bash:617`), so
     a named `--disconnect` with no container running takes the forget path when it should
     not.
   - Fix: fill `kept` directly, with a nameref or NUL-delimited output, and add a test.
2. **The comment describes the race wrongly** (`session-registry.bash:311-313`).
   - It says a resurrected record is "reported as a session that did not start".
     `ccy_registry_restore` (`:703-724`) in fact starts any record that is not running and
     whose directory exists, so a session the user ended comes back at the next boot.
   - The window is tiny and acceptable. Fix the comment, and optionally re-check
     `[[ -e "$file" ]]` just before the write.
3. **`--help` is out of date** (`claude-yolo:227-228`). It still says the command only
   clears the saved default; it should also name Quick Launch and the restore records.

## Nits

4. **Quick Launch can print "Cleared" when nothing was cleared.**
   - The read (`network-management.bash:128`) matches the first line starting
     `LAST_NETWORK="`, but the rewrite (`:142`) changes only an exact `LAST_NETWORK="NET"`
     line.
   - On a hand-edited line, such as one with a trailing space, a CR or a comment, nothing
     changes but "Cleared" still prints.
   - Fix: re-read after the `mv`, and fail if the file still names NET.
5. **Outside this diff:** `ccy_registry_restore_args` appends `--supervise` after a
   recorded `--`. The probe printed `--token t -- --model opus --supervise --continue`, so
   claude receives `--supervise`.

## Checked and clean

- **Stores:** only `save_network_preference`, `save_launch_config` and the registry write a
  network, and containers run with `--rm`.
- **Record walk:** it agrees with the launcher's parser on `--` and on value flags. It skips
  `cc` records and other projects' records.
- **Messages:** "without asking" is true, because every auto-detect and compose path
  prompts first.
- **Failures:** no new swallowed ones, and the `network ls` misread is fixed.
- **Versions and docs:** they match, apart from item 3.

## Gates

`qa-all.bash` rc=0, with 129 disconnect cases and 128 registry cases passing.
`plan-qa --sweep` shows 0 blocking. The reviewer wrote nothing and killed no processes.
