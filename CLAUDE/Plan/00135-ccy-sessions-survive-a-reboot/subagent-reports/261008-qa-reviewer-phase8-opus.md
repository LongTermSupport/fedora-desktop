# QA review: Plan 00135 Phase 8 (fedora-desktop#88), commit c08c1624

Copied from the qa-reviewer agent's report on branch `agent-ad6804cfea72018e3-c42d8461`
(c08c1624 on ba22402c). The fixes are in the plan journal (26-10-08) and the follow-up
commit.

**Verdict**: FIX-BEFORE-MERGE

The core mechanics are sound:

- Only fixed literals are typed, with `send-keys -l` to `=name:`.
- The set-going wait is bounded.
- No session is skipped silently: each one left alone is named, and the others carry on.
- The unit ordering keeps `default.target` and login free.
- The tests pass.

Four things block the merge: a version collision, a half-met spec, and two cases where
set-going picks the wrong conversation or drives one conversation twice.

## Should fix (before merge)

1. **Two restored sessions in one project are both set going, so two Claude processes drive
   one conversation in one checkout.** `files/home/.local/bin/ccy-sessions:691-760`
   (`cmd_set_going`) and `:663-664`.

   - Several sessions per project are supported (`lib/tmux-session.bash:22`). Both sessions
     `--continue` the newest conversation, which was already the case before this change.
   - Before, the second session sat idle at its prompt. Now `set_one_going` reads the same
     newest transcript for each session and types `/compact` or `continue` into both.
     Nothing removes duplicates by transcript file or by (prefix, dir).
   - The result is two agents doing the same work in one working tree at once, or two
     compactions of one file.
   - `verify-restore` makes it worse: `ccy_transcript_took_input_since` reads the shared
     file, so one session's input marks the other as started, which is a false OK.
   - Fix: in `cmd_set_going`, a second pending session whose resolved transcript (or prefix
     and dir) matches one already set going is left alone as `shares-conversation-with-<name>`,
     and a test covers it.
   - This is more than a nit because the change turns an idle hazard into concurrent writes.

2. **A session restored with `--resume <id>` is judged from the wrong conversation.**
   `files/var/local/claude-yolo/lib/session-registry.bash:531` and `:859`
   (`ccy_transcript_newest`).

   - The restore keeps `--resume <id>` and adds no `--continue`. ccy produces such launches
     itself: the restart-request relaunch writes `--resume "$sid"`, and ccy tells users to run
     `ccy --resume <id>`.
   - set-going ignores that id and takes the newest `*.jsonl`. The journal's premise
     ("`--continue` resumes the newest") only holds for `--continue`.
   - Result: the compact-or-continue decision uses another conversation's size, and verify
     watches the wrong file, giving a false `continue-not-started` (or a false OK).
   - Fix: when the recorded args name `--resume <id>`, `-r <id>` or `--resume=<id>`, use
     `<tdir>/<id>.jsonl`. The manifest needs that id, or set-going needs to read the record's
     args.

3. **Above the floor, a session with no supervisor is compacted and then left idle, while
   verify reports it OK.** `ccy-sessions:127`, `docs/ccy.md:302`.

   - The 2026-10-08 comment's main complaint is a session "idle until a person typed
     `continue`"; the fix it asks for is "at or above the floor, compact first".
   - The argument to `/compact` is a summarisation instruction and does not start a work
     turn.
   - Only the ccy supervisor continues after a compaction. Every restored `cc` session, and
     every ccy session started with `--no-supervise`, is compacted and then sits idle. This
     reproduces the reported bug for those sessions.
   - `verify-restore` then reports them OK, because the `/compact` user entry counts as
     "took input". That passes Task 8.5's acceptance wrongly.
   - Fix: for a session with no supervisor, wait for a `compact_boundary` entry after `at`,
     then type `continue`; until then it counts as SETTING-GOING, not OK.

4. **CCY_VERSION collides with a concurrent branch.**
   `files/var/local/claude-yolo/claude-yolo:17`.

   - The Plan 00163 branch bumps the same base, 3.86.3, to 3.87.0 and then 3.87.1.
   - Whichever branch merges second must take 3.88.0 (this is a feature) and move its
     changelog section to the top.

5. **The docs and journal promise that a failed verify can be cleared by a later pass, and
   the code mostly cannot do that.**

   - The claims: `docs/configuration.md:341-343` and `JOURNAL/00135-Journal-26-10-08.md:117`.
   - `helpers/self_update/cycle.py:490` calls `state.clear_owed()` before checking the result.
     So after a verify that exits 23, a re-run returns "nothing owed" and never reaches
     `reset_failed`.
   - Inside the unit, `reset_failed` is redundant, because systemd drops the failed state
     when the unit starts again. The only case it serves is an attempt killed by
     `TimeoutStartSec`.
   - Fix: either correct both texts to describe that one case, or keep the owed check after a
     verify-failed (a change to the Plan 00137 contract).

6. **Updates to the restore manifest can overwrite each other.** `ccy-sessions:716,727,752`
   and `session-registry.bash:728` (`ccy_restore_manifest_rewrite`).

   - set-going holds the manifest in memory (RM\_\*) for up to 20 minutes and writes the whole
     file back after each decision.
   - A second restore in the same boot is a supported case, and it rewrites the manifest with
     every running session as `going=none`.
   - The next rewrite by set-going then silently deletes that restore's new entries. A
     session missing from the manifest is never verified, which reads as a pass.
   - The reverse order is also wrong: the second restore marks sessions as `none` while
     set-going is still deciding them.
   - Fix: hold a lock (`flock`) around the read and write, and re-read the manifest at the
     top of each poll.

## Nits

07. **"Busy" matches `esc to interrupt` anywhere on the screen** (`session-registry.bash:827`).
    Fix: look only at the lines just above the box's top rule.
08. **"Ready" does not check whether a person is attached or the input box is empty**
    (`session-registry.bash:833`). A person attached and typing would have `continue` added
    to their text and submitted. Fix: leave an attached session alone as `attached`.
09. **set-going runs even when the restore failed.** The unit has no `Requires=`, and a
    failed restore produces a misleading "earlier boot" message. Fix: add
    `Requires=ccy-sessions-restore.service`.
10. **The play's read-back does not check the new unit**: it does not confirm that
    `ccy-sessions-set-going.service` is pulled in (`play-claude-yolo.yml:997-1010`).
11. **The prompt markers were measured only on Claude Code 2.1.293.** Have Task 8.5 capture a
    real pane as a test fixture.
12. **The new meta-deploy entry is partly redundant** with 00161's deploy, but defensible;
    keep it.
13. **A failed `reset_failed` warns and still exits 0** (`cycle.py:496-500`).

## Checked and clean

- **Injection:** only fixed literals are typed, and nothing from transcripts or the screen.
- **Unit ordering and lifetime:** `Wants=`/`Type=exec`, no `[Install]`, and the user
  daemon-reload runs after deploy.
- **Bounded waits:** 1200 s for set-going; verify's 1500 s cap is more than 1200 + 120 and
  less than 30 min, and a test reads all three values.
- **Fail-fast:** the manifest reader is strict, the floor is asserted, and the `| int`
  default does not reference itself.
- **Stderr hygiene:** set-going writes nothing to stdout.
- **Dependencies:** `jq` is installed by the same play; `CLAUDE_CONFIG_DIR` is unset
  everywhere, so the default is right.
- **Version bump:** CCY_VERSION is bumped (apart from the collision); the container version
  correctly stays 2.48.
- **Plan state:** Tasks 8.1-8.4 are ticked, 8.5 is open, and the journal exists.
- **Public repo:** clean.
- **Docs:** moved with the behaviour, apart from the overclaim in finding 5.

## Mechanical gates

- `qa-all.bash` in the worktree: exit 2. `ansible-syntax` failed for lack of a vault file
  and `js` for lack of eslint tooling; both are caused by the worktree, not by this diff.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml` with the main
  checkout's vault password file: passes.
- `plan-qa --sweep`: 0 block findings.
- These pass: `test-ccy-session-registry.bash`, `test-ccy-sessions-reboot.bash`,
  `test-self-update-cycle.bash`, `test_cycle` (110 tests), and `ruff check`.
- `qa-helper-tests.bash` is required because `helpers/self_update/cycle.py` changed; it was
  covered only by running `test_cycle` directly.
