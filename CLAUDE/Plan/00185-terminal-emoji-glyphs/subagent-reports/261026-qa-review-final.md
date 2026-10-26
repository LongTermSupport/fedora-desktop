STOPPING BECAUSE: the review is finished, but I did not write the report file. My rules for this review forbid creating files, and the dispatch also said "Read-only: change nothing". So the full report is below and nothing exists at `/workspace/CLAUDE/Plan/00185-terminal-emoji-glyphs/subagent-reports/261026-qa-review-final.md`. You'll need to save it there yourself.

**Verdict: CHANGES.** The delivered code is sound. What blocks Complete is plan state and the deploy queue, plus a few gaps in the proofs.

## QA Review — Plan 00185, whole delivered work on F44 (up to 6007fcf37)

### Should fix before Complete
1. **00185 is still in the deploy queue.** It sits at `CLAUDE/Plan/meta-deploy.list:36-43`, and the 18 of 18 host run is done. The rule in `CLAUDE/AgentNotes.md:109` is: "It has run and is done: remove it, in the commit that records the result."
2. **PLAN.md is behind the host result.** The status line (:3) says the fix "waits on its host run". Task 2.1 ends "Open: the host run", and Task 2.2 lists "Open: the meta-deploy run, qa-all.bash and the qa-reviewer agent". Both tasks are still in progress. The 18 of 18 result is not in the JOURNAL; the last entry (26-10-26 18:25) says "00185 stays in meta-deploy for the host run".
3. **The README row is stale.** `CLAUDE/Plan/README.md:109` still says "host run pending".
4. **Neither Success Criterion is met as written.**
   - Criterion 1 (the issue's `printf` draws correctly) and Task 1.2 are handed to Plan 00219. That plan does carry the work (00219 PLAN.md:12, :36, Task 4.2).
   - Criterion 2 says "the cause is named in the journal". `triage-analysis.md:79-86` ranks H1 as the most likely cause but says it cannot be confirmed until the screen check. What is confirmed and fixed is cause 4, the monochrome fallback.
   - To mark Complete honestly, either reword both criteria to "cause 4 fixed; cause 1 handed to Plan 00219 with the docs notice as its defence", or leave the plan open or Dormant waiting on 00219.
5. **The host acceptance never checks serif's ordered list.** Check 6 (`acceptance.bash:151`) checks only serif's single best match. The ordered-list checks 11, 12 and 14 cover only the monospace and terminal fonts. The host defect was in serif's ordered list (Symbola 8th, Noto Color Emoji 10th), so the 18 of 18 does not prove what a GTK program asking for serif walks. The fixture suite does check it, but against a model of the 65 file, not the real one.

### Nits
6. **The proof for the by-name guard depends on the fixture's rule order.** The fixture puts the 65 file's `<prefer>` before its `<default>` (`test-colour-emoji-fallback.bash:118-119`), with no binding attribute. If Fedora's real file has `default` first, then a request for Symbola becomes `[Symbola, Symbola, serif…]`. A `qual="any"` mutant would then leave Symbola first, and the five by-name checks would stay green. Production behaviour is correct either way. But nothing in the repo shows the real file's contents, and the suite says "as Fedora's gdouros-symbola-fonts ships it". The host agent could capture the real file.
7. **The fifth rule's comment overstates its scope a little** (`69-colour-emoji-fallback.conf:52`). "A request for Symbola by name keeps it" holds only when Symbola is the first family. A request like "Foo, Symbola, serif" also has Symbola moved to the end.
8. **`root-umask` does not look at `copy`, `template` or `get_url` without `mode:`.** Ansible creates those files under the inherited umask too, and `CLAUDE/QA.md:1210-1211` recommends these modules without saying `mode:` is what makes them safe. A probe found 175 root uses of these modules and only 2 unmoded writes into `/etc`, `/usr` or `/opt`, both in `optional/archived/play-tlp-battery-optimisation.yml`. So nothing is live today, but the rule does not cover this shape.
9. **`docs/playbooks.md:383` is incomplete.** It says the play rebuilds the caches when the file changes. It does not mention that the play also makes the system cache files 0644 on every run (`play-fonts.yml:62-76`).

### Checked and clean
- **Serif rule against fontconfig's behaviour.** `not_first` is a valid qualifier. A `delete` acts on the value matched by the first test on that element, which is why Symbola is tested before serif. `append_last` with a weak binding puts Symbola after Noto Color Emoji. The comments at `69-…conf:47-53` match this, apart from nit 7.
- **The suite exercises each rule.** It passes 104 of 104 when run here. The serif rule is covered by the without/with checks on serif's single best match and ordered list, and the guard by the five by-name Symbola checks. The JOURNAL records the mutants: 6 checks red with the rule removed, 5 red with `qual="any"`. It is a `count` row in `scripts/qa-gates.tsv:66` and runs in qa-all (`colour-emoji-fallback: passed: 104`).
- **IaC placement.** The work is in the play that already owns fonts (renamed, with the retired-plays map entry at `helpers/play_ledger/retired-plays.json:7`) and is imported in `playbook-main.yml:21`. No new play was created.
- **Fail-fast.** The handler uses `set -euo pipefail`, `/bin/bash` and `umask 022`. The cache-mode repair runs on every pass. There are no `failed_when` or `ignore_errors` entries.
- **Public-repo safety.** No usernames, home paths, hostnames, private IPs or emails in the plan folder, the conf, the suite, the play or the docs. `Owner: joseph` is the repo's convention (181 plans).
- **deploy.bash and acceptance.bash.** Both are built on the plan library. acceptance prints a `COVERAGE: n of 18` line and fails if any declared check did not run.
- **CCY version bump.** Not needed: nothing under `files/var/local/claude-yolo/` changed.

### Mechanical gates
- **`qa-all.bash`: incomplete.** My 1200 s timeout killed it (exit 124), so there is no final verdict line. Every gate it reached passed: about 49, including ansible (root-umask: "system files written as root set umask 022"), ansible-syntax (84), docs, bash-limits and colour-emoji-fallback (104). deployed-drift is skipped in the container, as designed. A full run is still needed before Complete.
- **`plan-qa --sweep`:** 22 advisory findings, 0 blocking, none about 00185.
- **`ansible-playbook --syntax-check playbooks/imports/play-fonts.yml`:** clean.
- **Conditional gates:** none triggered. The diff has no `helpers/`, `extensions/` or extension JavaScript changes.