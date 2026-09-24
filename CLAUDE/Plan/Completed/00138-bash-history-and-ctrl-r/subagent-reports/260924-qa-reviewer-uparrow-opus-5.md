# qa-reviewer: up-arrow local, Ctrl+R global (f091b588), opus-5, 2026-09-24

Saved by the coordinator.

**Verdict: FIX-BEFORE-MERGE.** No path lost data on the managed config; the defects were
root's Ctrl+R and the plan's acceptance script.

1. **Root's Ctrl+R searched only this terminal.** Root gets the snippet but not the ranker,
   and so does the desktop user without fzf. In a fresh shell, stock Ctrl+R failed to find
   an older command that the parent snippet found. Fix taken: the split moved into
   `history-search.bash`, applied only once its Ctrl+R is bound.
2. **The acceptance script failed on a correct deploy.** Checks 6 and 13 read HISTFILE
   from `bash -i -c`, which never reaches a prompt. Fix taken: the probe also reports the
   shared file. Check 6 expects `/dev/null` plus that file, and check 13 expects no split
   for root.
3. **Replacing PROMPT_COMMAND before the first prompt lost the whole session silently.**
   This is not reachable on the managed config, which only appends. Fix taken: an EXIT trap
   points HISTFILE at the shared file before bash's exit save, unless an EXIT trap is
   already set.
4. **Docs.** They say which surfaces see only this terminal: up-arrow, `history`,
   `!prefix` and `fc`.

Nit: a recorder comment said the first prompt sees history "loaded from the file". It is
reworded.

Checked and clean:
- **Exiting before the first prompt** truncated nothing.
- **Nested bash, `exec bash`, re-sourcing and leading-space commands** were all correct.
- **The recorder's first-prompt logic** is still correct.
- **The `-O` false branch** is unchanged.
- **The gate** failed on the parent snippet and exercises the rc-file path.
