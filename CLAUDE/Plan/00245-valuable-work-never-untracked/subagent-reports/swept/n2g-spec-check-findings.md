# SPEC-n2g narrow check (on 4ff789e72): FIX-FIRST, 2 high, 2 medium, 2 low

Round-2 fixes 1-20 all read as closed and coherent: RM11 revoke-all, the clean step, RS18 failed=stopped, the swap row, the rename fsync, RS3 verify, rev>=, the epoch range, RV7 start, the seams, the FS kill points, and the lows.

These are the new defects.

1. **(high) A kill between RS6.6's record=`start` write and the block removal leaves both.**

   - RS12 `start` runs RS7, and RS17 refuses any start while restore.block exists. So the resume is refused for ever, and --discard is refused at `start`: the team is down.
   - Design C step 08 has the same gap: it assumes "block gone" at `start`.
   - Fix: in RS12 `start`, remove restore.block if it is present, then run RS7 and RS8. This is safe because `start` is written only after the renames are fsynced and RC2 has run.
   - Test: add an SM5 test for this kill point.

2. **(high) RS6.1 raises min_backup above N under --roster-from-backup.**

   - RV4 does not exempt a resume. So a resume at swap, start, clean, or staged-after-revert refuses its own N, and discard is refused at swap, start and clean: the team is stuck.
   - After a revert and discard, min_backup stays raised for no reason, since the merged roster never stayed.
   - Fix: move the min_backup write to a new RS8 step 2, after `clean` and before the record is removed. Seqs are then reused only once the merged roster stays, and the guard blocks `backup` until the record goes.
     - Renumber RS8 to 7 steps: the failure range becomes 2-6, and refresh is 7.
     - RS6.1 leaves min_backup unchanged.
     - RV4's min_backup check runs on a fresh restore only.
     - Update RM11's bullet to match.

3. **(medium) The answer never states the online export gap.** C step 07 requires the answer to say that events posted between the export's end and the stop are lost. RS9's notes hold only RM7's and RO5's lines. Fix: add an online note quoting end_ms.

4. **(medium) RS3's verify reuses the staging unit name.**

   - RS18 accepts `failed`, and systemd-run refuses a name that is still loaded until --collect unloads it. So the verify fails spuriously while that race is lost. That is safe, but it can then never pass.
   - Fix: poll the unit on Clock.Sleep, up to 10 s, until it is not loaded, then start. If it is still loaded, step 05 fails.

5. **(low) The RM11 draw runs out of room.**

   - The draw is uniform in (max(H, backup epoch), 5e8). Each draw halves the remaining headroom on average, so about 29 --roster-from-backup restores exhaust it, and later ones are refused.
   - Fix: H is never below the live epoch, so bound the window to (m, min(m+1e6, 5e8)).

6. **(low) A revert from `start` puts back a restored copy that may hold events posted while it served.** A later --discard drops them without saying so. Fix: RS11.1 sets "served": true when it comes from `start`, and RS13's answer says those events are discarded.

## What holds

Epoch atomicity and durability for SY6/SY7 and gap 06:

- H is fsynced before `swap` (RS6.1), and RC1 writes the event last.
- RS3's verify plus syncfs comes before `staged`.
- H is at least every served epoch, so a merged or drawn value always differs from what members hold.

The offline restore, RO1-RO7, has no unstated loss beyond finding 6. The broken database's deletion is stated (RO5, D14).

## Handoff impact

- C5 gains tests for:
  - the kill between `start` and the block removal;
  - a resume under --roster-from-backup at swap, start and clean;
  - the verify's wait for the unit to unload;
  - served plus discard.
- C1's EpochFloor is written at RS8, for min_backup.
