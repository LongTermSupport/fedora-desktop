I fixed both defects in three commits on `fix/ledger-leftovers` (worktree `<worktree>`). Nothing is pushed or merged. `qa-changed` passes, and the four related test packages (play_ledger, host_health, self_update, gnome: 1299 tests) pass.

**Commits**
- `4151de5bd`: fix for the stale handoff file.
- `367e478e7`: the new rule `play-helper-input-unreachable`, committed red on purpose.
- `c971d7637`: the input-walk fix (rule now green), its docs in `CLAUDE/QA.md`, and Plan 00141 Tasks 5.2 and 5.3.

**1. Stale handoff file**
- **Cause:** as you found, `record_host_state` wrote the handoff only `if findings:`, and nothing removed the old file on a clean run.
- **Fix:** `handoff.clear(base)` deletes the file; a missing file or directory counts as already clear, and any other error is raised. `record_host_state` calls it whenever no handoff was written, which covers a clean run and also a failed write. A failed removal is reported the same way as a failed write. `--no-handoff` still writes and removes nothing.
- **Tests:** 6 new, red then green, in `tests/helpers/host_health/test_handoff.py` and `test_login_report.py`.
- **Defence Before Fix:** I found no static rule that tells this class apart from an ordinary conditional write, so it is fixed the normal way with regression tests.
- **Sweep:** an independent search of `helpers/` and `src-go/` found no other instance. Two borderline cases are left unfixed and are only listed here, not recorded in the plan; both files carry a timestamp. A failed `status_document.write_atomic` leaves the previous document in place. `src-go/internal/vmtest/freshness.go:294` leaves the old `freshness-status.txt` when its probe fails.

**2. Ledger code counted as an input of those plays (false input)**
- **Chain, the same for all three plays:**
  1. The play runs `helpers.gnome.apply_enabled_extensions` (play-gnome-shell-extensions also runs `verify_extension`).
  2. Those modules import only `enabled_extensions`, `session_bus` and `extension_state`.
  3. The mapper (`affected_plays._add_package`) counted the whole `helpers/gnome/` package instead, and then every package any module in it imports.
  4. `helpers/gnome/check_panel_contract.py` is a QA gate that no play runs. It imports `play_ledger`, `host_health` and `containerwatch`.
  5. Those three bring in `self_update`, `play_lock` and `version_pins`.
- **Fix:** the mapper now works per module. It counts the module the play names, plus everything that module can run, read with `ast`: imports anywhere in the file (including relative ones), and any `helpers.x.y` or `helpers/...` path in a non-docstring string. That keeps the child process `-m helpers.vmtest.bridge_run`, and `pingbus/bundle.py` packaging `helpers/pingbus` and `helpers/agent_bus` whole.
- **Reads inside a package:** because a package is no longer counted whole, files a module reads from its own package are now counted as reads. I widened `test_program_read_rule.py` to judge every counted helper module and these reads, so the narrower inputs cannot drop one unnoticed.
- **New rule:** `tests/helpers/self_update/test_helper_reach_rule.py` computes on its own which modules each play can run, and requires the counted helper files to match exactly in both directions.
  - It was red on 10 plays: these three, speech-to-text, self-update, unifi-firmware, agent-bus, agent-bus-element, claude-code-consumer and claude-yolo.
  - It is now green, and it blocks through `qa-helper-tests`.
- **Effect:** the three plays now count only `helpers/gnome/*` modules they actually run, plus the `displaylink_recovery` files for displaylink. A new test against the real repo shows they are not affected by the ledger files named in the stale report.
- **Scope limit:** a helper that a deployed file runs later from the checkout (a systemd unit or a bash tool) is deliberately not counted as a play input.

**Not done:** the Defence Before Fix method calls for a final conformance-reviewer pass before reporting, and I did not run it. It can still be dispatched against commits `367e478e7` to `c971d7637`.