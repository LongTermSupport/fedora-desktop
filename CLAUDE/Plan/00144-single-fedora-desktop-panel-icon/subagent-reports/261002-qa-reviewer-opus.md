## QA Review: Plan 00144 (e86d06a2, 972f832c)

**Verdict: PASS WITH NITS.** I found nothing at BLOCK or FIX-BEFORE-MERGE level. Those are the only two plan commits; neither git search turned up any others.

### BLOCK
- None.

### FIX-BEFORE-MERGE
- None.

### ADVISORY
1. **Nothing in the plan tracks removing the temporary retirement tasks.** The only record is a comment at `play-fedora-desktop-panel.yml:50-52` (and the tasks at `:141-180`). Add a follow-up line to PLAN.md, next to Task 7.4, so the tasks don't stay for ever.
2. **A host that only re-runs `play-container-watch.yml` keeps the old extension enabled, and nothing manages it any more.** This is the cost of the owner's choice D3 = A. The play header (`play-container-watch.yml:5-9`) says so, but `docs/playbooks.md:833` does not. One clause there would cover it.
3. **The no-kill gate's pass line gives one total, not separate counts.** `qa-nokill-containerwatch.bash:434` prints "6 container-watch file(s) clean". Today that is 4 Python files plus 2 JavaScript files, but the line can't show a JavaScript count of zero. Fail-on-missing (self-test h) covers the risk, so this is a nit.
4. **Task 7.4's contract gap is real, but nothing has drifted today.** I checked the constants by hand:
   - `SCHEMA_VERSION`, report path and DBus names in `containerReport.js:38,44-47,53-55` match `core.py:37` and `cli.py:51-52,547,792`.
   - The field names `containers.js` reads match `crashloop.py:254-265`, `restartpolicy.py:134-141`, `core.py:248-260` and `cli.py:516`.
5. **A stale report from a dead timer would still show as current.** `generated_at` is ignored, as the Non-Goals intend, but no follow-up plan is named for it. Add one to the plan's open items.

### Checked and clean
- **Where the work sits in the playbooks:** the retirement is in the play that deploys the replacement. Same `hosts: desktop` and `scope: gnome`, and on a server the play stops before it reaches the retirement. No new play, nothing added to the retired-plays ledger, and that is correct.
- **Retirement is fail-fast and safe to re-run:**
  - The helper fails if the key read-back still holds the uuid, and the assert checks its output markers. Neither task suppresses errors.
  - The uuid leaves `enabled-extensions` before its files are deleted.
  - A second run prints `ENABLED-UNCHANGED`, and deleting an already-absent directory changes nothing.
  - A retire-only run does not dedupe the user's own list, and a uuid that is both declared and retired is refused.
- **JavaScript:** sources start and stop in `enable()`/`disable()` (`extension.js:108,116`). Reads are async and cancellable, the poll timer and DBus subscription are removed in `stop()`, and the notification de-duplication key matches the old extension's. Every stylesheet class the section uses exists. No suppressions.
- **Tests run the shipped code:** the suites import the real `containerReport.js`, `sections/containers.js` and `extension.js` through the loader.
- **Docs:** `docs/playbooks.md`, `CLAUDE/QA.md:55` and the backend docstring are updated. Outside plan history, the old uuid survives only as the retire var.
- **Plan state matches reality:** Status is In Progress, and Tasks 7.3, 7.4 and 8.x are honestly open. The README row exists. The uncommitted README edit is Plan 00145's, not this plan's.
- **Public repo:** no paths, hosts, emails or IPs. "Owner: joseph" follows the convention in 102 other plans.

### Mechanical gates (full `qa-all.bash` not run, as instructed)
- `test-panel-sections.bash`: 126 passed, rc 0.
- ESLint on `extensions/`: rc 0.
- No-kill gate: pass on 6 files; `--self-test` passes (a) to (i).
- `check_extension_compat`: all 5 OK. `check_panel_contract`: OK.
- pytest on the two `tests/helpers/gnome/` test files: 80 passed. This is a targeted run, not `qa-helper-tests.bash`.
- `plan-qa --sweep`: 0 block, 11 advise, none of them about 00144.
- `--syntax-check` on both changed playbooks: rc 0; both are mode 100755 with the shebang.

Files:
- /workspace/playbooks/imports/optional/common/play-fedora-desktop-panel.yml
- /workspace/extensions/fedora-desktop@fedora-desktop/containerReport.js
- /workspace/scripts/qa-nokill-containerwatch.bash
- /workspace/docs/playbooks.md