# Conformance review: offline-member-blocks (spec 1.1.0, sections 3, 4, 7, 8)

Reviewed: branch `fix/offline-members-never-block`, defence commit `2e0c8aa6e`, final `2f3dd9e3a`
(no report commit yet on the branch; the draft DBF report is untracked in the dispatching worktree,
so it was read from there and is not in the diff).

## Reproduction

- RED at `2e0c8aa6e` (fresh detached worktree): `python3 -m unittest
  tests.helpers.agent_bus_lxc.test_offline_member_rule` exits 1. Five fixture tests pass;
  `Sweep.test_no_finding` fails with 19 findings in 10 files (A x6, B x12, C x1), each message
  carrying the id and `CLAUDE/QA.md "offline-member-blocks"`. Matches the draft's count. VERIFIED.
- GREEN at `2f3dd9e3a`: same module, 7 of 7 pass; the rule is active in that run (the fixture tests
  fire the detectors in the same run, and `test_the_scan_finds_the_member_code` proves the scan
  reaches the member files). VERIFIED.
- Project entry point at final: NOT VERIFIED. `scripts/qa-helper-tests.bash` hit my 580 s timeout on
  the first run (150 of 150 modules listed, no failure shown); a second background run had not
  finished when I wrote this (dots only, no failure so far). `./scripts/qa-changed.bash --base F44`
  exited 2 on "eslint for extensions/ is not set up (cd extensions && npm ci)", a missing dependency in
  this checkout, before it ran tests; its plan did list `test_offline_member_rule`. So clause 3.5's
  "green through the entry point" rests on the draft alone. Reported, not worked around.
- Originating symptom (s2_check refusing a stopped container, `acceptance.bash --stage 1 --fake`
  offline leg): needs the LXC/host tooling; not reproduced. Red and green verified alone. UNVERIFIED.

## Findings, by severity

### 1. HIGH. A search-found instance is left outside the rule on a judgement the search called unsure (3.1, 3.4, section 4)

Independent search item 5, `src-go/internal/teams/restart/jobs.go:265`, still present at final: a seat
whose container is not running gets `resultFailed` ("its container is not running ... end it in
ccy-sessions, then run restart again to start it"), and restart exits 1 on a failed job. The search
marked it "INSTANCE, unsure". The draft closes it alone ("a live tmux session whose container is gone
is broken, not offline") and says "None outside authority". Under 3.1/3.4 the search is the authority;
a rule left narrower than the search showed, or an instance accepted unfixed, is the Owner's decision
(section 4, and "uncertainty is itself an escalation trigger"). The sentence is not confirmed by
search, and the message asks the owner to start something, which is the hazard. Detector B does not
match its wording. Resolve: fix it as an instance (name the seat offline, do not fail) and widen B, or
refer it to the Owner as a known instance (count 1) with the doubt stated.

### 2. HIGH. The rule was narrowed inside the first fix commit (3.3 Parts A and B)

`978d02f45` (fix) edits `test_offline_member_rule.py` (+38/-4): `program_asks`, `plan_folder`,
`folders_asking`. A file that enters a container is now exempt when any file in its plan folder
mentions `plan_lxc_online` (the match is on the whole text, comments included). This is a Narrowing
made after the red, in a commit that also carries fixes, with no commit of its own and no recorded
sentence or search of what it excludes in the report (QA.md describes the new behaviour but gives no
reason). I checked the case it was made for: stage 2 asks `online_containers` (line 141) before
`guest_home` (152), so the sentence holds for that file today; but any new file in an asking folder that
enters an unlisted container is now invisible. The red commit's rule is not the green rule. Resolve: own
commit for the Narrowing, with the sentence in QA.md confirmed by search; limit `HELPER_CALL` to
non-comment lines; keep a fixture on which the narrower rule still fires.

### 3. MEDIUM. The originating defect is fixed first and has no separate reproducing step (3.4, section 6)

`978d02f45` is the first fix commit and holds stage 1 `s2_check` (the originating instance), stage 2,
00213, the rule edit, pre-flight, `_members.inc.bash` and its tests together. The expected order
(defence, sweep, pattern fixes, original defect last with a test) is not followed, and the fake offline
leg cannot be shown to fail before and pass after. Resolve: show the stage 1 fake leg red at
`2e0c8aa6e` and green at final, or say plainly why not.

### 4. MEDIUM. Narrowings and the wider rule are asserted, not recorded as confirmed sentences (3.1 lower bound, 3.3 Part B)

- Exclusions without a sentence in the report or QA.md: completed/cancelled plans, `tests/`,
  `_test.go`, `testdata`, `vendor`, `JOURNAL`, `subagent-reports`, Go outside
  `src-go/internal/teams` for C, Python/Go outside `MEMBER_CODE_ROOTS` for B ("non-member code" is the
  only justification). The excluded code was not searched to the 3.1 standard.
- Next wider rule is named ("any lxc-attach/lxc-info anywhere, or any 'not running' text") with the
  reason that lab/VM tools are not team members: plausible, but it joins two different widenings and is
  not confirmed by a search.
- Detector B is a fixed phrase list; the Class is "blocks on offline". Record that width, and leaving
  paraphrases open is an Owner decision.
Resolve: put the sentences and confirming searches in the CLAUDE/QA.md section (8.5, 8.7).

### 5. MEDIUM. The detector is a unittest module (3.2: "A test MUST NOT serve as the Detector")

It reads code, is fixture-proven and runs in `qa-helper-tests`, so the substance holds; the form is a
test. The project has Go analyzers, semgrep rules and `qa-gates.tsv`. The draft calls it "the project's
tests-as-rules pattern" and records neither a reason a gate was impractical nor a mechanism gap. Also not
verified: that `qa-routing.tsv` runs the module for a change to every scanned root (for example
`files/usr/local/sbin/agent-bus-*`, `src-go/internal/teams/**`); if not, `qa-changed` would not enforce it
and only `qa-all` would. Resolve: record the reason and the routing.

### 6. MEDIUM. Identifier and enumeration are prose (3.6, 8.3, 8.5)

The id resolves by a file the practitioner can read, and the section states what, why and the construction
(8.4 met). There is no command to list the defences, and the message gives no anchor path; the draft
admits the missing resolver but names no place where the gap is recorded (3.2). Resolve: name where it
sits.

### 7. LOW. Fix record lacks the examined / by-pattern split (3.4)

"Each examined individually" for 19 findings, the Go sites done by a sub-agent. The bash "start it"
messages in stage 1, 2 and 00213 are one pattern. No sample record, and no sentence saying what absence
means where a default or NOT JUDGED is used.

### 8. LOW. Search order is a claim (3.1)

The search file is committed in the red commit together with the rule, so "before the rule, without sight
of it" rests on its own statement. Content passes (two techniques, what each found that the other could
not, unsure items marked). Point to the dispatch or journal that dates it.

### 9. LOW. Sweep scope

Python/Go outside the named roots (for example `lxcfreeze`, `ccylaunch`, `files/` Python tools) are not
swept by B or C; the search read some of them by grep only. State why they cannot carry the hazard.
The 19/10 count itself is correct.

## Checked and conforming

- Red is a commit of its own: parent F44, adds only the test, QA.md, AgentNotes.md and the search file.
- Fixtures with `ruleid`/`ok` marks, asserted to fire exactly there; near misses kept; the module's own
  text does not trip it.
- Count reproduces at red; 0 at final; rule active in the green run.
- Blocking: a unittest failure; no warn mode, skip, baseline file, `noqa` or suppression found in the rule
  or the fix diffs.
- QA.md section states what, why and how to fix with the correct construction (`plan_lxc_online`,
  container list, NOT JUDGED), and says a running member with the wrong binary still fails.
- Calibration assumed (owner's rule in AgentNotes.md) is recorded and discoverable.
- Toolchain gaps (no manifest, no resolver) reported, not worked around.

## Template completeness

Missing in the draft: the Narrowing sentences (4), the by-pattern split (7), the restart/jobs.go referral
(1), the reason for a test-form detector (5), and the green entry-point output (it names the run but
quotes none).

## Verdict

NOT YET CONFORMING. Red and green of the rule reproduce. Findings 1 and 2 must be resolved (an unresolved
search-found instance; a narrowing made in a fix commit without its own proof), and 4, 5 and 7 recorded.
The entry-point green could not be reproduced here (timeout, missing eslint dependency in the checkout).
