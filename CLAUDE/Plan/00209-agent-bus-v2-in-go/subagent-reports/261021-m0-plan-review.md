# Plan 00209 M0: adversarial review of the plan bookkeeping

Branch `feat/bus-v2-m0-plan`, diff `f460abb7...HEAD -- CLAUDE/Plan` (M0 commit `cc2d7fa1`, with
the design and build-brief commits it carries). Reviewer is not the author.

## Verdict: FIX-FIRST

Nine findings. Finding 1 blocks any push. Findings 2 to 4 would mislead a later stage. The rest
are drift that should be fixed in the same pass.

## What holds

- **The Python scope verdict.** No bus module is Ansible-loaded. I checked:

  - `ansible.cfg` names only `callback_plugins = ./callback_plugins`.
  - Outside `untracked/` the tree has no `*_plugins`, `library`, `module_utils`,
    `ansible_collections` or `roles` directory.
  - `callback_plugins/play_ledger.py` imports only `helpers.play_ledger`, and that package has no
    bus reference.
  - No play uses `script:` on a bus module.
  - The bus packages import no helper from outside the bus.

  Every play reference is `python3 -m` through `command`, and the line numbers are right:
  `play-agent-bus.yml:143,243,290,362,375`, `play-agent-bus-element.yml:58,107`,
  `play-claude-yolo.yml:394` and `agent-bus-install:390`. The module list matches the tree
  (12 + 3 + 18 files), and so do the line totals (6,170 / 2,279 / 13,135).

- **PostToolUse.** It runs pingbus only in relay mode (`hooks.json:29`). This is correct, and so is
  the change of port order.

- **D16.** It is 25 s in PLAN.md, R26 and 8.2, and that matches `settings.json:6` and
  `seat.py:88`.

- **00211.** None of its files are touched, as BUILD-BRIEF.md requires.

- **Plan format.** The ❌ superseded tasks match the repo's Cancelled usage. There are no time
  estimates.

## Findings

1. **Public repo: the install's team name is committed.** `BUILD-BRIEF.md:3` gives the real team
   name and the coordinator's seat handle. `BUILD-BRIEF.md:103` gives a `pingbus --team … say --to …` line with the same two real values. (Both redacted here before this report was
   committed.) The author's report (finding 11) saw this
   and left it.

   - It was introduced in `35acceab`, so it is in the history of every local branch that carries
     that commit: `feat/agent-bus-v2-design-2`, and the m1-*, m2-* and s1-declare branches.
   - None of these branches is on the remote yet.
   - **Fix:** replace both with placeholders (`<team>`, `<coordinator-handle>`). An edit at HEAD
     still publishes the name through `35acceab` when the branch is pushed or merged. So the
     coordinator must also get the owner's decision on rewriting those unpushed branches before
     any of them is pushed or merged to F44.

2. **The 6 s / 8 s "mismatch" is a misreading. Left as it is, the stage-2 spec would change the
   wrong value.** Two different settings exist:

   - the plugin hook's own `"timeout": 6` (`hooks.json:50`);
   - the session-wide SessionEnd budget `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS: 8000`
     (`files/opt/claude-yolo/optional/agent-bus/settings.json:9`, `seat.py:83-84`, written into a
     seated session's settings).

   DESIGN.md 8.2's "the 8 s setting stays" (lines 56 and 709) refers to the second, so it is
   right. Wrong are:

   - `RESEARCH-python-scope.md:108-109`;
   - the report's finding 06 (`subagent-reports/261021-m0-plan-bookkeeping.md:62`);
   - the 16:15 journal entry of 26-10-21.

   **Fix:** rewrite the open point to say that the stage-2 spec states and keeps both values, and
   that the Go goodbye must finish inside the smaller one. Add a correction entry through
   `mkplan.bash --journal`, not an edit of the entry.

3. **DESIGN.md does not reflect the stage 6 split, or D17 and D20 as PLAN.md takes them.**

   - `DESIGN.md:932` says every decision is defaulted to its recommendation "except D16". But:
     - `PLAN.md:78` D17 has bumps at 2, 6a, 6b and 8, against `DESIGN.md:953`'s (2, 6, 8);
     - `PLAN.md:81` D20 adds the split.
   - Still unsplit in DESIGN.md:
     - 10.2 row 6 (`DESIGN.md:822`);
     - 8.2 "Stage 6 brings the keeper…" (`DESIGN.md:715`);
     - 10.3 "H9 … before stage 6 is accepted" (`DESIGN.md:840`), against `PLAN.md:262`'s 6b;
     - B10 "until stage 6" (`DESIGN.md:1025`). Under 6a the Go `pingbus` dispatches only `watch`,
       `seat exec` and `seat keep` to the zipapp.
     - the coexistence bullet "stages 2 and 6: the host's v2 `pingbus seat disconnected`"
       (`DESIGN.md:868`). Under PLAN.md that verb is Go only from 6a.
   - Stale 1 s wording: the D16 row still recommends "keep 1 s" (`DESIGN.md:952`), the D20 row
     says "a Python start per second" (`DESIGN.md:956`), and `DESIGN.md:52-53` is in the present
     tense.

   **Fix:**

   - Name D16, D17 and D20 as amended in the `:932` preface.
   - Add one sentence under 10.2: stage 6 is 6a plus 6b (PLAN.md), with B10 until 6b, H9 before
     6b, and `seat disconnected` Go from 6a.
   - Put the 1 s lines in the past tense.

4. **D4 contradicts the owner's own words, and is not raised.**

   - The owner's 00194 decisions A-G make relay the one delivery path. 00194 Goals:
     "One delivery path… The Claude Code inbox socket is not used… The supervisor plugin owns
     `pingbus watch`".
   - BUILD-BRIEF.md:39-41 requires that any decision which contradicts the owner's words goes to
     the coordinator. `PLAN.md:65` marks D4 only as defaulted. The report says "tell the owner",
     but the plan does not, while D9 is flagged.
   - The "Folded in" rows for 00194 2.1, 2.1b and 2.2 put 00194's whole delivery goal behind
     stage 6b without saying so.
   - `BUILD-BRIEF.md:25` still lists "the relay" in the session-side scope where no behaviour may
     be lost.
   - DESIGN's own D15 row (`DESIGN.md:951`, "which is why it is asked") is defaulted with no flag.

   **Fix:**

   - Mark D4 in `PLAN.md:65`, as D9 is marked: it overrides 00194's owner design, and it is
     raised with the coordinator before 6b, or before 00194 Task 2.1 resumes, whichever is first.
   - Mark D15 the same way, before 6b.
   - Change `BUILD-BRIEF.md:25` to "the relay's lock (relay mode itself dropped, D4)".

5. **The 00201 expectations are given too few stages.**

   - `PLAN.md:132-133` gives 2.11, 2.12 and 2.15 to stages 5 and 6b only. But:
     - 2.12 N5, the pre-swap backup on a rendered config change, is in `config_restart.py`, which
       Task 4.2 turns into an installer function, so it belongs to stage 3;
     - 2.15 S-F14, `removed_humans` and the name refused for good, lives in `teamfile.py` and
       `declare.py`, so the stage-1 decoders must accept it and the stage-3 installer validation
       must keep the refusal.
   - `PLAN.md:155` calls 2.17 and 2.18 "unaffected". They are Go code that v2 re-plumbs:
     - 2.17's `internal/hostcheckout` refusal and the single handle renderer are in
       `teams/board/collect.go` and the picker, which stage 1's in-process seats rewire;
     - 2.18's play-lock hold across the host_vars write and the apply (S-G1 to S-G11) must be kept
       by stage 5's `bus/declare` write side.

   **Fix:**

   - Add stage 3 to N5, and stages 1 and 3 to S-F14.
   - Move 2.17 (stage 1) and 2.18 (stage 5) to "stay in 00201; carried as expectations".

6. **Stage 2 has no coexistence test, but it shares state with v1 from day one.** `PLAN.md:198-204`
   (Task 3.1) and the M1 bullet say the Go SessionEnd hook charges the send gate in `outbox.json`
   under the outbox lock (R42, `DESIGN.md:652`). It does this beside v1's `send`, `say` and the
   watcher on one `state/`, which R47 shares from stage 2. It also writes `said-goodbye.json`,
   which the host's Python `seat disconnected` reads until 6a. PLAN.md names coexistence tests
   only in Tasks 7.1 and 7.2.

   **Fix:** add to Task 3.1 the outbox-lock coexistence test (a Go goodbye against a v1 `send` in a
   second real process) and the `said-goodbye.json` test against v1 `seat disconnected` (H11).

7. **`members.py` is needed at stage 5, not only at stage 7.** `RESEARCH-python-scope.md:70` gives
   stage 7 only. But `declare.py`'s `lxc-*` write side, at stage 5 (`:56`), calls
   `members.parse_members`, `resolve_members` and `MemberError` (`declare.py:1228,1258,1353`). The
   derive row (`:71`) already makes this point for `derive.py`.

   **Fix:** in row 70, say that the parse and resolve part comes to `bus/lxc` at stage 5 with
   `bus/declare`, and the apply at stage 7.

8. **The 00213 split leaves loose ends.**

   - (a) 00213 Task 1.2 is 🔄 (`00213 PLAN.md:67`), but it is named neither in what 00213 keeps
     (`00213 PLAN.md:54`, `00209 PLAN.md:81-84`) nor in Phase 3's preface.
   - (b) 00213's third Goal (`:44-46`) is now delivered by 00209. 00213 therefore cannot complete
     until 00209's stage 2 is done, which contradicts "avoid having two plans for one thing".
   - (c) 00213's Overview (`:15`) still says `refreshInterval: 1`.

   **Fix:**

   - Name Task 1.2 in both lists.
   - Move the hooks Goal to 00213's Non-Goals as 00209's work, or say that 00213 completes without
     it.
   - Change the Overview to "was 1, 25 since Task 1.3".

9. **The folds are recommendations only, and nobody is assigned to edit the plans they leave.**
   00194 2.0e and 2.0g, and 00201 2.8, are still ⬜ in their own plans (`PLAN.md:189-191`: "those
   plans' files are not edited"). Once the coordinator accepts them, each has an owner in two
   plans.

   **Fix:** add a line to the table that the coordinator marks each folded task ❌ (pointing to
   00209\) in 00194 and 00201 when accepting. Alternatively, open a task for it under M0.

The 00194 recommendations are otherwise right. 2.0c, 2.0d and 2.0f are v1 acceptance. 2.0h and
2.0i are done and correctly carried. 2.0e fits Task 3.1, because the hook records the goodbye as
said. 2.0g is a receive rule for 6b. The 00201 2.8 supersession by D4 is right, given finding 4.
