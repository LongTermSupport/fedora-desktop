# Review: play ledger skips a play outside the repo root (a15a42c5)

Branch `fix/play-ledger-outside-root`, adversarial read-only review. Tests run:
`python3 -m unittest tests/helpers/play_ledger/test_*.py` — 331 tests, OK, none skipped; the
real-`ansible-playbook` outside-root case ran (ansible-playbook on PATH). `./scripts/qa-changed.bash`
exit 0.

## Verdict: FIX-FIRST

Two findings block. Both are small; the collector change itself is sound.

### Blocking 1 — `clear-ledger-broken.bash` clears the sentinel blind

`CLAUDE/Plan/00170-unifi-network-tooling/clear-ledger-broken.bash:44-46`

The script's safety argument ("the cause is fixed") rests on the sentinel holding the 10-12
outside-root reason, but the script never checks it. `check_freshness --clear-broken`
(`helpers/play_ledger/check_freshness.py:288-321`) unlinks whatever sentinel exists and only
prints the reason afterwards. `store.mark_broken` keeps the FIRST reason
(`helpers/play_ledger/store.py`), so if the host's sentinel predates 10-12 or was written by a
different failure, meta-deploy — which runs the queue unattended and logs to a file — clears a
hole whose cause is not fixed and reports PASS. That is a fallback dressed as a fix, which is
what the owner forbids.

Scenario: the host sentinel reads `<ts> ValueError: cannot write runs.jsonl: Permission denied`
from a run before 10-12 (the 10-12 outside-root raise then hit `FileExistsError` and was
dropped). The script clears it; check_freshness goes green on an unwritable ledger.

Fix: before the deploy leg, read the sentinel and refuse unless its reason names the fixed
cause. Plan scripts are exempt from the bash limits, so a short probe is allowed:

```bash
sentinel="$(python3 -c 'import os; from helpers.play_ledger import ledger
print(ledger.sentinel_path(ledger.ledger_dir(os.environ, os.path.expanduser("~"))))')"
if [[ ! -e "${sentinel}" ]]; then
  printf 'no BROKEN sentinel at %s; nothing to clear.\n' "${sentinel}"
  plan_finish
fi
if ! grep -qF 'is outside the repo root' "${sentinel}"; then
  printf '[FATAL] the sentinel reason is not the outside-root play this script fixes:\n  %s\nClear it by hand once its own cause is fixed.\n' "$(cat "${sentinel}")" >&2
  exit 1
fi
```

(The old `_resolve` message was `play '…' is outside the repo root '…'; the ledger records plays this repo owns`, wrapped by `_fail` as `ValueError: …`, so the `is outside the repo root`
substring is stable for the sentinel the script targets.) The second meta-deploy run then
takes the "nothing to clear" branch instead of re-running `--clear-broken`.

### Blocking 2 — docs drift: `docs/playbooks.md` lists what does not mark BROKEN, and this is missing

`docs/playbooks.md:946-951` says "An ad-hoc `ansible -m` or `ansible-console` command does not
mark it". The outside-root skip is a third such case with its own stderr marker and is only
described in the plugin's `DOCUMENTATION` block (`callback_plugins/play_ledger.py:54-55`). The
owner reads the docs, not the plugin docstring. Fix: one sentence in that bullet, e.g. "…nor
does a play outside the checkout (a private play run from a linked worktree): it is skipped
with one `LEDGER-PLAY-NOT-TRACKED` line on stderr and no record."

## Questions asked, answered

**Scope decision or error-swallowing?** Scope decision, correctly made. `_resolve`
(`helpers/play_ledger/collector.py:122-145`) has no `try`; the only branch that does not raise
is the prefix test, and it returns the bare path so `on_play_start` can name the play. OSError
(ELOOP) and ValueError from `realpath` propagate — tested at `test_collector.py:117-136`, and
the plugin's `except → _fail` path is unchanged and exercised end-to-end by
`test_source_position_against_real_ansible.py:239` (asserts `LEDGER-WRITE-FAILED` in stderr).
"Outside the root" is also the same boundary `check_freshness._drop_never_tracked` already
applies at report time, so the two ends of the pipeline now agree.

**How is it decided?** Both sides are realpath'd: `_REPO_ROOT` from the plugin file
(`play_ledger.py:38`), again in `RunCollector.__init__`, and the play path in `_resolve`. The
comparison is `startswith(root + os.sep)`, so `/repo-other` is outside (tested,
`test_collector.py:77-85`). No prefix confusion found.

**Could a play that SHOULD be tracked be classed outside?**

- Main checkout reached via a symlinked path: both realpaths land on the real location — tracked.
- Bind mount: realpath does not cross mounts, but plugin and play are both seen through the
  same mount, so the prefix matches — tracked. Only two different mount views of one tree on
  one host would mis-class, and that case previously marked BROKEN; now it skips loudly.
- CCY `/workspace`: ansible.cfg is found in `/workspace`, plugin root is `/workspace`, plays
  are `/workspace/...` — tracked (and Ansible is not run in the container anyway).
- Caveat on the journal's claim that "a private play under the main checkout's untracked/
  stays in scope": that holds only if `untracked/` (or the private subdir) is a real directory.
  If the owner's private plays dir is a symlink to another repo, realpath resolves outside and
  the play is skipped with the notice. Harmless (no wrong row, no BROKEN), but the journal
  statement is conditional. Not blocking.
- Pre-existing, not introduced here: linked worktrees live at `<root>/.claude/worktrees/…`,
  INSIDE the main root. A play run from the main checkout cwd but by its worktree path would
  be recorded against main's HEAD with a `.claude/worktrees/...` relative path. Out of scope
  for this fix; worth a plan note.

**Is the stderr line visible?** Yes. `_warn` writes `sys.stderr` and flushes
(`play_ledger.py:120-124`); callbacks run in the TaskQueueManager process, so it is the
terminal. `run.bash` invokes `ansible-playbook` with no stderr redirection; meta-deploy uses
`2>&1 | tee`. The real-ansible test asserts exactly one line in `result.stderr`
(`test_source_position_against_real_ansible.py:277-280`).

**Do the tests prove other errors still mark BROKEN?** The collector raising is proved for
OSError and ValueError via `mock.patch.object(collector.os.path, "realpath", …)`. The
plugin-side "raise → `_fail` → sentinel" path is unchanged and already covered by the existing
real-ansible failure test at line 239. Adequate.

**Is `clear-ledger-broken.bash` safe, host-only, a correct meta-deploy entry?** Host-only
yes (`plan_require_host`, R2); `plan_mode deploy`, `--check` honoured, `plan_deploy_leg` at top
level, `cd "${PLAN_REPO_ROOT}"` so `python3 -m helpers.…` resolves. Entry
`00170-unifi-network-tooling/clear-ledger-broken.bash` matches `UNIT_RE`
(`meta-deploy.bash:66`), file is mode 755. Idempotent on a second run. Not safe in the
sense claimed — see Blocking 1.

**Install-specific names?** None in the diff (code, tests, journal, plan, meta-deploy comment,
subagent report). `PLAN.md:5 **Owner**: joseph` predates this branch and appears in 101 plans —
pre-existing, out of scope.

## Non-blocking notes

- `_resolve` returning `tuple[str, str] | str` and `on_play_start` discriminating with
  `isinstance` is type-punning: "a string means skipped" is implicit. A clearer single path is
  `tuple[str, str | None]` (relative is `None` when not tracked) or a tiny `NamedTuple` with a
  `tracked` field. Cosmetic; no behavioural concern.
- 331 tests take 19 s, almost all in the real-ansible class; fine for the targeted suite.
