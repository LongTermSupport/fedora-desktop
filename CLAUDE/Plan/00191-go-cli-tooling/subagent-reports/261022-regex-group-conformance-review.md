# Conformance review: regex-backref-escape (Plan 00191)

Spec: DBF method 1.1.0. Checkout read-only; only this file written. Branch fix/regex-group-block-scalar, defence 87bc54bbb, fix 5b78e94fb.

## Verdict

CONFORMS, with one medium and two low findings to record. No blocking finding.

## Reproduction (section 7)

- Red: `git archive 87bc54bbb` extracted into an empty dir under untracked/scratch, `git init` + `git add -A` there (the gate lists files through git), then `./scripts/qa-ansible.bash --root <dir>` from the checkout. Exit 1:
  `ERROR (regex-backref-escape): playbooks/imports/play-claude-yolo.yml:583: regex_search group argument '\\1' reaches the filter as 2 backslashes, not 1; in a block scalar's {{ }} write '\1'` plus the pointer `CLAUDE/QA.md#regex-backref-escape`. Summary line `1 regex backref`. Extracted dir deleted.
- Green: `./scripts/qa-ansible.bash` at 5b78e94fb: exit 0, the OK line names "regex backreferences escaped right", so the rule is loaded. `go test ./ansible -run RegexBackref` passes.
- Symptom: `regex_group_fixture_probe.py` prints "labels hold" (real Ansible 2.19.14 against flagged/clean). `regex_group_play_probe.py` renders ccy_front_version as `3.99.14.11`, and the line-692 changed_when as True/False on real input (so 692 is correctly untouched).
- Not reproduced: qa-all (forbidden), qa-changed, the ansible-core 2.21.5 host failure itself (only 2.19.14 available). Claims resting on the report alone: "qa-changed red on ansible-patterns only", 96.8% coverage.

## Findings

1. MEDIUM, clause 3.1 (Runner pin) and section 6, the reproducing test. The report cites `regex_group_play_probe.py` as the test that reproduces the original defect. That file is in untracked/scratch and gitignored (`git check-ignore` confirms), so it is not versioned and not re-runnable by the next person. What is committed is static: the `folded` fixture case and TestRegexBackrefNamesTheReceivedCountAndTheFix. No Runner check that executes the play's task survives. Resolve: either commit a retained Runner check (a plan-local script in the plan folder that renders the task through Templar) or state in the record that no Runner pin is kept and why.

2. LOW, clause 3.1 step 3 / report "Next wider rule". The report says the independent search "found no wrong pattern escape". The search report (independent-search.md:23-24) lists play-lxc-install-config.yml:455 and play-self-update.yml:85,87 as "wrong count by the stated rule ... Unsure". They work only because Jinja keeps unknown escapes. The report's sentence should say they were judged harmless and why, not that none were found. The pattern-escape widening is rejected with a stated reason (no single wrong count), which is acceptable, but the rejection should name those three sites.

3. LOW, section 4 / clause 8.7. The `.j2` next-wider rule is left unbuilt because no instance exists today. That reason makes it an owner decision. It is recorded in PLAN.md:626 and the journal (JOURNAL/00191-Journal-26-10-22.md:52), but only as prose under Task 4.5, not in the place the project enumerates decisions. Also CLAUDE/QA.md states "The rule does not read .j2 template files" (about line 947), which is the visible gap, good. Resolve: make sure the owner is told in the hand-off, not only in the plan.

## Checked and fine

- Class bounded both ways: fixtures flagged.yml (15 cases) and clean.yml (19) cover contexts and styles; the rule is wider than the one instance (7 sites judged, any scalar style, patterns, replacements, by-name form).
- Red proof is its own commit with the instance present; fix is a separate commit; both reachable.
- Independent search ran before the rule, without sight of it, with two named techniques and a record of what each could reach; reconciled in the rule's favour.
- Sweep: scope recorded, count 1 of 7 sites, corroborated. 6 other sites examined individually. No suppression, baseline or exclusion of a hazardous case.
- Permanent and blocking: part of qa-ansible, wired into qa-routing.tsv; message has stable identifier `regex-backref-escape` that resolves to CLAUDE/QA.md#regex-backref-escape, which states what, why, and the correct spelling table (8.4).
- Original defect fixed last (separate commit after the defence), 5b78e94fb changes only `'\\1'` to `'\1'` plus an explanatory comment.
- Toolchain gaps (no manifest, no listing command, anchor-only resolution) are reported, not worked around.
- Report template filled and matches the commits; note the report quotes the instance at :585 while the rule reports :583 (the scalar's key line), explained in the report.
