# Plan 00209: design text batch (offline members, builder proposals, small fixes)

Branch `feat/bus-v2-n3`, from 6a17bd4c8. Commits 8c18dee2c (the batch) and 2e8e10594 (review
round 1 fixes); this report and the round-2 review are committed after them. Design text only.

## Offline-members audit items

| Item | Where it landed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| B3   | C 3.11.3: `status` exits 1 only on a team problem (one with no `member`); a member's problem is on its row. The schema gains the optional problem `member` key (SPEC-n2e SR1), the `online` bullet says the launch-failed and unposted-disconnected problems carry it, and C 3.11.2 step 3 says the coordinator's own membership problem carries it.                                                                                                                                                                           |
| B4   | C 3.6.1 and 3.6.4: `coordinator_wanted` is team state, never a problem. C 3.11.3 gains `coordinator_wanted` (null or `{handle, fix}`).                                                                                                                                                                                                                                                                                                                                                                                         |
| B5   | C 3.9.1, 3.9.2 step 02, step 03 (container stopped during the apply), step 05, a new "A container that stops during the phase" paragraph (LX6), 3.9.3 and C32. A missing, stopped or frozen container is offline and SKIPPED or DEFERRED, never a phase failure. A missing one's line says `container missing`. Record status `offline` replaces `stopped`; set joined/offline/deferred/left. FRAME line 121 and SPEC-n3 LX2, LX6 and LX9 were aligned to the same vocabulary (SPEC's `missing` status folded into `offline`). |
| B9   | A 3.2 items 3-4 and the feature paragraph: only live members (seat lock held, R27) hold a raise back. Offline ones are named as lagging. The raise lists `proto` under `features`, so a returning old kit treats its team as untrusted until upgraded (A 3.1). A 6.6 follows.                                                                                                                                                                                                                                                  |
| B10  | D 5.4 M0: the rootfs refusal is dropped. Such a member is `state_read_from: server-only`, a named gap.                                                                                                                                                                                                                                                                                                                                                                                                                         |
| B11  | D 6.1 A7: it probes the coordinator if live, else the first live worker; with none it is `not run: no live member`.                                                                                                                                                                                                                                                                                                                                                                                                            |
| B12  | D 5.7 step 4: each member's session record or unit state is read once. An offline member is listed at once with its restart command. Only members whose process is up are waited for.                                                                                                                                                                                                                                                                                                                                          |
| B13  | D 5.4 M6: only the sessions running at M6 restart. A stopped one is recorded `moves when next started`, and a missed bound is a line in the M7 report. The "fails" column is updated. D 5.3:717 is about team-record disagreement and was left.                                                                                                                                                                                                                                                                                |

## Builder proposals

- **n3a (B 2.x and B11 ones):** B 2.2.2 error key order (both examples). B 2.2.3 the every-team-fails rule. B 2.2.4 the 78 row's codes and meaning, and row 1 now says "a bug, or an I/O failure the verb cannot recover". B 2.4.1 no-team exit 64. B 2.10 the `--team`-differs-from-ledger exit 64. B 2.3.3 session.json, keeper.json, report.json and desired `seat` text. B 2.9 `failed`. B 2.3.2 `gate.credit` and 13-digit times. B11 mode 0600. B 2.13 the 78 codes and exit 1.
- **n3a, not applied:** the proposals that cite SPEC rules (MS2, MS3, GU1-GU5, SA1, n3-D6, LP1, CL2) or no chapter (CLI 03, 06, 08-11; state 2-4; bundle 1 and 3-7; guest-check 3-7; guest-place; homefs; caps other than session.json's 8 KiB). These belong in SPEC-n3, not in a chapter. CLI 03 already matched B.
- **n2g:** C 3.12.2. On resume at `start`, a leftover block is removed first. `restore.check` is introduced in step 05 (it reads back the staged roster event) and step 09 (on the live server). It and `restore.secure` take the tar's `rooms.json`, as `restore.swap` does.

## Small fixes

- `gate.credit` is an integer: B 2.3.2 example `540000`, with the line "one token being 60 000 (`proto.Bucket`, P61)".
- `--kind other` was added in B 2.1, B 2.14 and the BD2 row.

## Not applied against the FRAME

None.

## Deviation to confirm

The B5 instruction (`offline` replaces `stopped`) disagreed with FRAME line 121 and SPEC-n3 LX2 and LX9 (`stopped`, `missing`). I followed the instruction and aligned the FRAME and SPEC-n3 lines.

## Reviews

- Round 1 (Opus, `261022-design-text-batch-review.md`): FIX-FIRST, 3 medium and 7 low. All were fixed in 2e8e10594.
- Round 2 (`261022-design-text-batch-review-2.md`): MERGE.

## Owed

SPEC-n2e SR1's strict decoder and the built n2e status codec do not know C 3.11.3's new `coordinator_wanted` key. They need it in the slice that builds B4. No writer emits it yet.
