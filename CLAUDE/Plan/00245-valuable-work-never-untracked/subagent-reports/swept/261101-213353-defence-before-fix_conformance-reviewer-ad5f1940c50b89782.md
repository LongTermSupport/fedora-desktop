**Reproduction**
- **Red, 4632f1333:** `go test ./all ./changed -run TestSuiteEnvIsolated` exits 1 in both packages. Every `scripts/qa-*.bash` row reports "runs in the machine's locale", "inherits LANG/exported functions" and "can reach an installed ccy-token-usable".
- **Green, d35b2e84e:** both tests pass.
- **`./scripts/qa-changed.bash`:** it exited 0 on the detached final commit, but it printed one line and had no diff to check, so it shows little.
- **Not reproduced:**
  - The hostile-env runs of the three suites. I did not repeat them, so they rest on the report (unverified).
  - The locale red (no en_GB in the container), as the report already says.

**Does not conform**
1. **§3.2 (high):** The detector is a Go test that runs the runners and checks the commands they build. §3.2 says a test must not serve as the detector, and the detector must read code. This repo has an analyzer home, `src-go/qa/rules`. The report records neither a bespoke analyzer nor a toolchain gap. The semgrep rejection argument covers semgrep only. An analyzer asserting that every process start goes through the isolated seam was not considered.
2. **§3.1/§3.4 (high):** The report names no two search techniques and says nothing about what each found that the other couldn't check. "Dispatched before the rule" is not a record. The count is also inconsistent: the search says "3 known", the instance list says 5 (2 seams and 3 suites). No sweep scope is given, for example how many `scripts/test-*.bash` were read. The only negative found is test-ccy-teams, so saturation is unshown.
3. **§3.1/§4 (medium):** The wider rule, isolating HOME, was rejected on cost ("caches live in real HOME"). That does not say the hazard cannot arise there. The suites set their own scratch HOME, but the report doesn't confirm that for every suite. As written, leaving it unbuilt is the Owner's decision.
4. **§3.4 (medium):** The report doesn't say which of the three suite fixes were examined individually and which received the change by pattern. The d35b2e84e message doesn't say either.
5. **§3.6/§8.3 (medium, unverified):** The identifier is a heading in `CLAUDE/QA.md` and the message prefix `suite-env-isolated:`. I found no toolchain command that resolves it. The report should show how a practitioner reaches it from the failure output.
6. **§3 (low):** No committed test pins the suite fixes. They were checked only by a manual hostile run.

**Conforms**
- The red proof is its own commit. It carries the helper and the docs, and the green is a separate commit.
- The two runner seams were fixed, and the rule is part of the `go` gate, so it blocks.
- No suppression or baseline was used.
- The suites were fixed at the cause (`LC_ALL=C` on the sorts, `env -i` on the front, an explicit PATH).
- The QA.md entry states what the rule is, why it exists and how to fix a finding.

Nothing was written. I left my worktree detached at d35b2e84e.