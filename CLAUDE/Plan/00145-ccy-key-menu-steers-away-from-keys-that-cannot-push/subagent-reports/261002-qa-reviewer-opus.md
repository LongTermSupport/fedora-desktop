## QA Review: Plan 00145, ccy key menu (uncommitted working tree)

**Verdict**: FIX-BEFORE-MERGE

**I did not write the report file.** You asked for it at `/workspace/CLAUDE/Plan/00145-ccy-key-menu-steers-away-from-keys-that-cannot-push/subagent-reports/261002-qa-reviewer-opus.md`, but I have no Write tool and my rules forbid writing files through Bash. The full report is below, ready to save there.

The tree changed while I was reviewing. `CLAUDE/Plan/README.md` gained the 00145 row, `scripts/test-ccy-session-registry.bash` gained the `Use it anyway?` entry, and the prompt line in `ssh-handling.bash` moved down one. The line numbers below are from the current tree.

### Blocking
None.

### Should fix

1. **A huge number typed at the menu silently picks a key, sometimes one that is not on the screen.** `files/var/local/claude-yolo/lib/ssh-handling.bash:594`
   - The check is `[[ ! "$selection" =~ ^[1-9][0-9]*$ ]] || [ "$selection" -gt ${#shown[@]} ]`. On an overflowing number, `[` fails with "integer expression expected" (status 2). So the `if` is false and the input is treated as valid. Line 601 then indexes `shown[]` with a wrapped value.
   - I reproduced it:
     - No key can push, input `99999999999999999999`: `✓ Selected: /h/.ssh/github_alpha`, `RC=0`. No error shown to the user.
     - Short list showing only `github_beta`: the same input offers the hidden `github_alpha` behind "Use it anyway?".
     - Input `9223372036854775809`: `shown: bad array subscript`.
   - This is a regression. The old code appended `2>/dev/null`, and an error there reached the "Invalid selection" branch.
   - Fix: limit the length (`^[1-9][0-9]{0,2}$`), or make the range test positive: `if ! { [[ … ]] && [ "$selection" -le N ]; }`. Add a test for it.

2. **The warning says the deploy key and the ssh-agent "cannot push", but the probe never checked them.** `ssh-handling.bash:607-608`, `:381`
   - `pushers` can only contain `github_*` keys, because `probe_gh_keys_for_remote` only probes those (`:347`). The remote's alias key and `SSH_AGENT_SENTINEL` are never probed.
   - Even so, picking either from the full list prints "cannot push to this remote … the probe found no push access for it, so git push from this session will be refused". The `a)` line also counts them as "without push access".
   - A read/write deploy key does push, which `docs/ccy.md:1150` itself says. The plan's own Non-Goals call these "unverified identities".
   - To your question: still being able to pick a deploy-key or agent candidate (`a`, then `y`) is an acceptable change. Telling the user something false about it is not.
   - Fix: give unverified candidates their own wording ("was not checked for push access"), and count only probed keys in the `a)` line. For example, track which entries were probed at all.

3. **After `a`, the default can be a key that then triggers the warning.** `ssh-handling.bash:550-556` with `:493-495`
   - When 2 or more keys can push, `suggested_key` falls back to `GITHUB_ALIAS_KEY`. So after `a`, ENTER selects the deploy key and goes straight into "cannot push … Use it anyway?".
   - I reproduced it with alias plus two pushers and input `a\n\n`: the menu shows `1) /h/.ssh/deploy … ← default`, then prints the warning.
   - With no alias and 2 or more pushers, the full list has no default at all. With exactly one pusher, the default is that key.
   - Decide one rule for the default after `a` (probably the first key that can push) and test it.

4. **The probe message contradicts the menu.** `ssh-handling.bash:487`
   - It still prints "N account keys have push access — pick manually", but the short list now defaults to 1. That auto-default reverses the old deliberate "no default when ambiguous" design, which the comment at `:463-467` still justifies (a wrong key "silently mis-routes git push to the wrong account").
   - The plan chose "ENTER takes the first", so the stale text and comment need to match. The alternative is no default when 2 or more keys can push.

5. **The tests miss the branches the change is most likely to get wrong.** `scripts/test-ccy-ssh-handling.bash:374-462`
   - The tests do call the real `discover_and_select_ssh_keys`, which is good. All 53 pass (`rc=0`). But nothing covers:
     - closed input returning 1 (claimed in the changelog);
     - a deploy-key alias or agent candidate when keys can push (findings 2 and 3);
     - the default after `a`;
     - overflowing input (finding 1).
   - The plan's success criterion "covers every branch of the new menu" is not met.

6. **The plan has not been updated for the work done.** `CLAUDE/Plan/00145-…/PLAN.md`
   - Task 1.1 is still 🔄 and Task 2.1 still ⬜, though tests, code, version bump and changelog all exist.
   - The JOURNAL has only the scaffold entry.
   - The plan folder is still untracked. The README row has since been added.

7. **`docs/ccy.md` is out of date on two points.**
   - `docs/ccy.md:1156` says the agent row "appears whenever `ssh-add -l` lists a key". When any key can push, it is now only behind `a`.
   - The remote-key bullet (`:1144-1153`) does not mention that it is hidden behind `a` in that case.

8. **The loop still has no retry limit.** `ssh-handling.bash:544`
   - It is `while true`, against `CLAUDE/InteractiveScripts.md` rule 02 (a limit, default 3). The old loop had none either, but this diff rewrote the loop, so it is in scope.
   - Closed input is now handled correctly (rule 03). A stream of bad input, or repeated `n` answers, still loops forever.

### Nits
- The new warning and "choose again" lines go to stdout. Only the closed-input error uses `>&2`. This matches the existing ccy pattern and stdout is not captured here, but the new warning really belongs on stderr (`CLAUDE/StderrHygiene.md`).
- The 3.71.0 bullet in `docs/ccy-changelog.md` is one unwrapped line, unlike its neighbours.
- `docs/ccy.md:1340` realigns an unrelated table row (formatter whitespace only).
- Uppercase `A` is rejected, while `Y` is accepted at the confirmation.

### Checked and clean
- **Shell options:** the launcher sets only `set -e` (`claude-yolo:75`), with no `set -u` anywhere. The call is `discover_and_select_ssh_keys "ccy" || exit 1` (`:1041`), so errexit is off inside the function. The `[ … ] && x` lines are safe, and on bash 5.2.15 an empty `"${pushers[@]}"` is fine anyway.
- **Closed input:** `return 1` reaches `|| exit 1` as intended.
- **Version bump:** `CCY_VERSION` 3.70.1 → 3.71.0 with a comment. No image file changed, so no container version bump is needed.
- **Export list:** `_ssh_key_menu_list` is not `export -f`'d, but its caller already relies on other unexported functions (`probe_gh_keys_for_remote`, `get_project_remote_url`), so nothing new breaks.
- **Prompt registry:** the session-registry test was updated for the new sub-prompt and passes.
- **IaC placement and public-repo safety:** no playbook changes, and no identifiers in the diff.

### Mechanical gates
- `qa-all.bash`: not run, as you asked (you run it).
- `test-ccy-ssh-handling.bash`: passed 53, failed 0, rc=0.
- `test-ccy-session-registry.bash`: rc=0.
- `plan-qa --sweep`: 11 findings (0 block, 11 advise), none about 00145.
- `--syntax-check`: not needed, no playbooks changed. `qa-helper-tests`, extension compat and ESLint: not triggered.