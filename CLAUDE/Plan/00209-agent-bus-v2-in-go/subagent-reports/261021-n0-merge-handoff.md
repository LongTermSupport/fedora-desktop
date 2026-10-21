# N0 merge handoff (Plan 00209), from the merge agent

Worktree `/workspace/untracked/worktrees/bus-v2-n0`, branch `feat/bus-v2-n0`, tip 255df20d (this
handoff is committed on top). Not pushed. Use `git -C <literal path>` for commits (the
conflict-marker gate refuses a variable path). No sed: use Edit/Write, or python with asserted
exact matches.

## Done

- 13a361f5, f1508b66, ca3e87cc: merges of f1 delivery, f2 member, f4 migration.
- d41c9a6f: the first at-merge items (a-h of the lead's first message).
- 72a3667b: `261021-n0-merge.md` (the merge report, to update).
- 255df20d: merge of `feat/bus-v2-n0-f3-host` (5d50ed12). DESIGN-v2.md conflicts resolved by keeping
  both: host rows 17, 24, 25, 28, 53, 57, 58, BD13, DC8, DC16-DC20 taken; rows 20, 27, 32, 46, BD9,
  INT1 changed on both sides and combined (27 keeps f2's title "Announcing a session that ended
  without its notice" with f3's ccy release and podman-exit detail; BD9 keeps f2's link-and-host
  rule plus f3's per-kind detail); 57 and 58 say "link". f3's B 2.13 rewrite auto-merged.

## Left (the lead's second and third messages)

1. B 2.13 (from f3's auto-merge) lists `bin/teamchat-claude` in the LXC kit: remove it (D 3.3 has
   no `teamchat-claude`).
2. Check the C list in the merge report against the merged chapter C; fix only what is missing.
   f3 says it applied the f1, f2 and f4 requests (link, `released_ms`, `human_text` out, the
   disconnected flags). Then sweep C for "worker" in the process sense only (the team role stays).
   Also check that C posts A 7.4 `moved` from `migrate carry` (D 5.6, the welcome) and allows A7's
   probe runtime (`teamchat run --plain --kind host`, D 6.1); both were decided at this merge,
   after f3 was cut.
3. Apply the host fixer's cross-fixer requests from `261021-n0-fix-host.md`, as one commit:
   - B: the LXC desired membership is written by `guest membership` as the agent (2.10, BD13,
     header); sweep 2.15 and B 6 for `install-self`; `run` handles the unit stop; a store-less
     start resumes from min(receipt, released_ms) (B 2.6 step 01 b may already say so); txn
     `tc-disc-<seed>`, for LXC `tc-disc-<run id>`; `back` after a restore; `restore_epoch` read by
     a direct state request; `human_text` from the roster (the member fixer did B 2.6 step 10;
     check); the package names.
   - A: the `added` and `role` content; `restore_epoch` (A 5.1); the 5.2 bootstrap order; admin
     redact (4.2, 12.2); a 6.9 threat row; `back` (A 7.4 has it since d41c9a6f; align C's and B's
     use with it).
   - D: the 3.5 path row; 5.6 export not backed up (f4 may have done it; check); 8 cites C 3.15
     (f4 may have done it; check); confirm D 12's `migrate` verbs against C 3.3; `carried_at`.
   - Build-session items: narrow the frame sentence per DC19 in `DESIGN-v2-FRAME.md`;
     DESIGN-v2.md section 6 cites C 3.15; the launcher crash notice, the mount refusal and the
     stop timeout wherever the host disposition places them (seam row 53 has the mount refusal).
     Where a request conflicts with another fixer's decision, keep the better one and record it.
4. Update `261021-n0-merge.md`: add the f3 merge (255df20d) and its resolutions (above), the host
   requests applied, and remove the C-list items now done.
5. Run `./scripts/qa-changed.bash` before each commit; messages end with
   `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never push, no qa-all, no Ansible.
6. Final message to team-lead under 1000 characters: the tip, the conflicts resolved, the items
   done, anything left.
