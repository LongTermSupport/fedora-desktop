# qa-reviewer — diff b25070ed..566fb9c3 (Plans 00134 and 00135)

The reviewer's role has no write tool, so the coordinator saved this condensed copy of
its returned report.

**Verdict: FIX-BEFORE-MERGE.** The `play-claude-code.yml` → `play-claude-yolo.yml` merge
is sound; two defects need fixing now.

## FIX-BEFORE-MERGE

1. Plan 00098's `deploy.bash:59-70` stops at once with "playbook not found", and
   `acceptance.bash:14,264` tell the user to run the deleted play. The plan is In
   Progress, and its success criterion runs through `acceptance.bash`.
2. `check_freshness.py:199-200` retires a GONE play on the successor's latest run
   without checking its outcome, and the ledger records failed runs. A successor run
   that fails at the engine check, before the `cc` tasks, would drop the finding for
   good while `cc` was never deployed.

## Should fix

3. "Old play missing at that commit" does not prove the successor already has its
   tasks. At 7963ec7a the old play is gone and `play-claude-yolo.yml` has no `cc`
   tasks. Nothing is exposed today, because both commits were pushed together, but the
   rule assumes the deletion and the merge land in one commit. Document that, or record
   the merge commit in the map.
4. The ad-hoc skip is barely tested: `test_plugin_support.py:206-207` compares a
   constant with itself. Ansible 2.19.13 does set `__adhoc_playbook__`
   (`cli/adhoc.py:164,197`), but no test runs the callback path.
5. `ansible-console` still marks the ledger BROKEN. It sends no playbook-start event
   (`cli/console.py:211-231`), which contradicts the plan goal at `PLAN.md:30`.
6. Tags can deploy the lib and `cc` apart: the lib is `[scripts, library]` and `cc` is
   `[scripts, wrapper]`, so `--tags library` recreates the 3.60.3 break.

## Nits

07. Both 26-09-23 journal files open with a "plan scaffolded" entry, but both plans were
    created on 26-09-22.
08. Task 1.1's text still says ad-hoc runs are recognised by the name `Ansible Ad-Hoc`.
09. `login_report.py:418` puts the real checkout path into the finding, which "Copy these
    findings" copies. `plugin_support.py:24-29` records that this command was once pasted
    into a public issue.
10. The retired-plays rule appears only in `docs/playbooks.md`; no agent-facing doc
    points at it.

## Checked and clean

- Task order in the merged play, and nothing depended on the removed preflight.
- Same `hosts`, `become` and `scope` as before.
- No references to the deleted play outside history and plan docs.
- `run.bash` and `shutdown-with-update` do not list play files.
- Docs and anchors are updated.
- The CCY 3.60.3 bump is present; no image bump is needed.
- Shebang and exec bit are fine.
- Real-git checks of `path_exists_at` pass.

## Gates

- `qa-all.bash` exited 0.
- `--syntax-check` passed on both changed playbooks.
- ESLint passed.
- `plan-qa --sweep` gave 7 advisories, none for 00134 or 00135.
