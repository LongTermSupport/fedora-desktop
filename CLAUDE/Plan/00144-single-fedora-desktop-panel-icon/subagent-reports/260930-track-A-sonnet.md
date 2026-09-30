# Track A report (Phases 1, 2, 5, 7 scripts)

Done: Tasks 1.1, 1.2, 1.3, 2.1, 2.2, 5.1, 5.2, 5.3, 7.1, 7.2 (marked done in PLAN.md; status header flipped to In Progress because the plan-QA hook requires it once boxes are ticked).

## Files
- CLAUDE/Plan/00144-single-fedora-desktop-panel-icon/{PLAN.md,DECISIONS.md,JOURNAL/00144-Journal-26-09-30.md} - D1-D3 recorded, journal entries appended (17:51 UTC)
- helpers/gnome/enabled_extensions.py: `retire(current, retired, declared=())` -> `RetireResult(values, changed, removed)`
- helpers/gnome/apply_enabled_extensions.py: repeatable `--retire-uuid`; `--uuid` optional but one of the two required; single write for add+retire; read-back failure `retirement-did-not-take`; marker `GNOME-EXT-RETIRED removed=...`
- tests/helpers/gnome/test_enabled_extensions.py, test_apply_enabled_extensions.py (written first)
- playbooks/imports/optional/common/play-container-watch.yml: extension vars + 3 extension tasks removed, header updated
- playbooks/imports/optional/common/play-fedora-desktop-panel.yml: `retired_extension_name` var; retire task (argv `--retire-uuid`), assert on markers, then `file: state: absent`; header/comments updated. No failed_when/ignore_errors.
- CLAUDE/Plan/00144-.../{triage.bash,probe-state.bash,deploy.bash,acceptance.bash} (executable, shellcheck clean; triage.bash ran on host, all legs OK; deploy/acceptance NOT run)
- extensions/scripts/gnome-shell-extract-js.bash: fixed a real defect found in Task 1.2

## Findings
- GNOME 50.5 extensionSystem.js `_onEnabledExtensionsChanged` (lines 562-596) disables loaded uuids removed from the key live. Confirmed.
- gnome-shell-extract-js.bash pinned a non-existent libshell-16.so (now libshell-18.so) and deleted the old extract before extracting. Now globs the soname (exactly one or fail) and cleans up only after success. Side effect: the untracked 48.7 extract was deleted by my first (failed) run; 50.5 is extracted.
- Before state on host (triage): both uuids enabled and deployed, report.json present, backend installed, timer enabled+active.

## QA
- ./scripts/qa-helper-tests.bash: 2248 tests OK.
- ./scripts/qa-all.bash: 2 gates failed, both the known ones: deployed-drift (vmtest only) and ccy-relabel-preflight. Everything else green (ansible, ansible-syntax, js, docs, nokill-containerwatch 6 files, plan-script-logging, extension-compat, panel-contract).
