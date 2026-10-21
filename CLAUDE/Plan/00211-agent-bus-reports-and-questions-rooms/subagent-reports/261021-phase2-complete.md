# Plan 00211 Phase 2: written and queued as deploy 2, not merged or run

Phase 2 is on this branch and has not been merged or run. Deploy 1 and the spike were
accepted earlier (JOURNAL "Deploy 1 accepted; spike holds").

## Commits

| Commit   | What                                                                                                                               |
| -------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| 45b955af | Bookkeeping: Tasks 0.1, 0.2 and 1.13 ticked with their evidence; the spike and deploy-stage-1 entries removed from the queue       |
| 1462ff04 | RED: `tests/helpers/agent_bus/test_admin_side_rooms.py`, a check-rooms CLI test, existing admin tests updated; fake `joined_rooms` |
| efb18c6f | Tasks 2.1-2.4: `admin.py` side rooms, `list` ROOM lines, `check-rooms`; SPEC §5.4 corrected; CCY 3.99.13.9, container 2.78.9       |
| b6ab7e97 | Task 2.5: `acceptance.bash` stage 2, `--after-owner`, `_acceptance-stage2.inc.bash`, `ACCEPTANCE-owner.md`                         |
| daab3141 | Task 2.6: `deploy.bash --stage 2`, `deploy-stage-2.bash` queued, `acceptance-after-owner.bash` (not queued yet)                    |

## For the coordinator

- **Version re-bump at merge.** F44 is at CCY 3.99.13.8, container 2.78.8 (Plan 00213). This
  branch uses 3.99.13.9 and 2.78.9, and its changelog entry says it follows 3.99.13.7. Fix
  that line at merge.
- **Merge order.** Phase 1 is deployed, so Phase 2 may now be merged. The meta-deploy entry's
  description says to restart every team's sessions first.
- **Design choices to review:**
  - SPEC §5.4's `health.py` check became `agent-bus check-rooms`, because `health.py` has no
    Matrix credential.
  - A3's worker is a throwaway host member, made by `add-member` and `remove-member`, not a
    seat. Its deactivated account stays behind under a seat name used only once (`a` and the
    time).
  - The test question and A5 need an LXC member declared `role: coordinator`. With none, A5
    refuses and says why.
- **Not done:** `--syntax-check`, because no playbook changed. No Phase 3 or 4 work was done.
