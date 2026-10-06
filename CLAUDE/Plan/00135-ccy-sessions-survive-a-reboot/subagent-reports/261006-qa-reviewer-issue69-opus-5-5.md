**Verdict: FIX-BEFORE-MERGE.** There are no blockers. The code for all three defects is correct, and the security of the SSH key change holds up. Three things need fixing before merge: the 7.1 fix is not staged yet, the claim about Quick Launch is wrong for this very release, and the plan record contradicts itself.

### Blocking
None.

### Should fix
1. **The 7.1 fix and its test are not staged yet.** `git diff` (index against working tree) shows the `=${name}:` change at `files/home/.local/bin/ccy-sessions:597` only in the working tree. The same goes for the fake tmux at `scripts/test-ccy-sessions-reboot.bash:557-573`, the 3.82.0 changelog entry (`docs/ccy-changelog.md:37-40`) and the PLAN.md edits. Committing the index as it stands would ship 3.82.0 with verify-restore still broken. Run `git add` on all four first.

2. **The fix for the SSH key menu only reaches part of the affected sessions, and the docs say otherwise.** `docs/ccy.md:264-266` and the comment at `claude-yolo:1121-1124` say a session started with no flags "is restored through Quick Launch, which holds the key already."
   - That is wrong right after a deploy. `load_launch_config` deletes the saved Quick Launch settings whenever `CCY_VERSION` changes (`claude-yolo:477-489`), and 3.82.0 is such a change.
   - So the first reboot after this deploy (Task 7.4) will stop every no-flag session at the prompts. `_ccy_registry_args_with_ssh_key` returns 1 for those records (`session-registry.bash:386`), so they are never given the key.
   - The subagent report suspects this is the case the owner actually hit (`subagent-reports/261006-issue69-fixes-opus.md:64-68`).
   - Fix: correct the docs and the comment. In Task 7.2, say it covers only sessions started with `--token`, `--network` or `--no-network`. In Task 7.4, say that `WAITING-AT-PROMPT` is the expected result for no-flag sessions until Task 7.5 is decided.

3. **The plan record contradicts itself on 7.1.** PLAN.md marks Task 7.1 done. But the journal (`JOURNAL/00135-Journal-26-10-06.md:32-36`) and the subagent report (`:5`, "NOT FIXED, blocked") still say it is blocked, and no entry records the coordinator's fix. There is also no record of the decision to tighten the fake tmux instead of writing the real-tmux test that issue #69's acceptance asks for. Add a journal entry with `mkplan.bash --journal`.

4. **The launcher's call is checked only by reading the source.** `scripts/test-ccy-session-registry.bash:532-533` uses awk to check that the call comes after the key menu. That never runs the code, and it does not cover the agent-key exclusion or the session lookup. The report admits this (`:112-114`). Task 7.2's ✅ lists tests without saying so. Add that caveat to 7.2, and treat 7.4 as the only real proof.

5. **`meta-deploy.bash` was not updated for Task 7.4.** The PLANS comment at `CLAUDE/Plan/meta-deploy.bash:55` still describes the run as only `play-vm-test-lab.yml`. Task 7.4 needs `play-claude-yolo.yml`, which installs both the launcher and `ccy-sessions`. `run.bash --changed` may pick it up anyway, but the list and its comment should say exactly what will run.

### Nits
- **The suggested reboot command.** The refusal at `ccy-sessions:379` and `docs/ccy.md:329,1465` suggest `sudo reboot-with-update --in N`. The repo's own alias is just `reboot-with-update`, which already adds sudo and the full path (`files/home/bashrc-includes/shutdown-with-update.bash:7`). Typing `sudo reboot-with-update` only works if sudo's `secure_path` includes `/usr/local/bin`, which I could not check from the container. Suggest the alias or the full path instead.
- **"Login with no terminal" is too narrow.** `docs/ccy.md:327` describes `challenge` as what such a login gets, but an SSH login with a terminal is refused too (the report notes this at `:79`). Say "any login logind does not treat as the active local session".
- **A stale comment.** `lib/tmux-session.bash:861` says the record write there is "the only one". It now has two rewriters: `forget_network` and the new `record_ssh_key`.

### Checked and clean
- **7.1 target:** a sweep of `files/`, `scripts/` and `helpers/` finds no other pane command with an `=name` target. `attach-session`, `kill-session` and `list-clients` take a session target, where `=name` is correct.
- **7.2 security and correctness:**
  - Only the key's file path goes into the record, never the key itself.
  - The record is written with umask 077, whole and then moved into place.
  - A value containing a newline is refused (`session-registry.bash:236-240`).
  - The agent choice is excluded (`claude-yolo:1125`).
  - The paths come from `find "$HOME/.ssh"` or from `ssh -G`, which already checks the file exists (`ssh-handling.bash:197-212, 427`).
  - On replay, `--ssh-key` is kept with its value (`session-registry.bash:108`). The launcher checks the file with `-f` and stops with an error if it is missing (`claude-yolo:599-603`).
  - The test pins the restore arguments as `--token work --ssh-key /k/id_one --supervise --continue`.
  - cc records, records with `--no-ssh` or their own key, and words after `--` are all left alone.
- **7.3:** the permission check and the session check both run before the withdrawal trap is armed, and the dry run makes the same checks. A failure to reach busctl is a refusal. `shutdown-with-update` and `reboot-with-update` use `notify`, not `reboot`, so the new check cannot block them (`shutdown-with-update:132,256`).
- **Version bump:** `CCY_VERSION` goes from 3.81.0 to 3.82.0 with an updated comment. The image is unchanged, so the container stays at 2.43.
- **Public-repo safety:** I found no hostnames, usernames or home paths in the new plan files, issue text or tests.

### Mechanical gates
- qa-all.bash: not run, as you asked.
- plan-qa --sweep: 0 block and 11 advise findings, none for 00135.
- The three changed suites, run on the working tree: reboot 200 pass / 0 fail, registry 157 / 0, take-over 73 / 0.
- syntax-check: no playbooks changed. No `helpers/` or `extensions/` changes, so those gates don't apply.