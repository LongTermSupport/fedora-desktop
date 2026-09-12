# Plan 00081 — Findings register

The measured facts behind every task in [PLAN.md](PLAN.md), extracted to keep that
document lean. Each was reproduced, not inferred; where a figure appears, the
command that produced it is named.

---

- **F1** — `pre-commit:89` listed staged files with `--diff-filter=ACM`. Rename
  detection is on by default (`diff.renames`, git ≥ 2.9), so a `git mv` plus an
  edit is classified `R` and was **excluded entirely — never scanned**.
  Reproduced on git 2.39.5: a 40-line file moved with one line appended reports
  `R098`, `--diff-filter=ACM` returns nothing, and the hook printed
  `✓ No files staged for commit` and exited 0. Moving a plan folder into
  `Completed/` is exactly this shape
- **F1b** — the hole **appears and disappears with file size**. A small file
  falls below the rename-similarity threshold and is recorded `D`+`A`, so the
  add *is* scanned. My own first reproduction used a 2-line file, passed, and
  would have let me dismiss a real security finding as unreproducible
- **F2** — the email whitelist filtered whole **lines** (`grep -v`), so a real
  address sharing a line with `git@github.com` was deleted along with it.
  Verified: that line matches the email pattern once and survives the chain with
  zero matches remaining. The file's own comment says a legitimate reference
  "no longer whitelists a genuine leak elsewhere in the same **file**" — that
  was fixed; the same-**line** case was not
- **F2b** — `CLAUDE/PlanTriage.md` already states the correct rule, learned from
  a real leak in Plan 00066: *"Redact by substitution, never by dropping
  anchored lines."* The scanner was doing the opposite of the repo's own
  documented lesson
- **F3** — `qa-python.bash` discovers by extension (`:31`) or **file mode**
  (`:48-56`). This is Plan 00076's bash-gate defect, unfixed in the Python gate:
  7 tracked repo-owned files, 4,003 lines, mode 0644 with a `python3` shebang,
  are never compiled or linted — and the repo's own ruff config finds **31
  violations** in them while the gate prints `✓ python: 35 files OK`.
  `CLAUDE/QA.md` names `wsi-stream` as *the* example of Python needing care; it
  is one of the seven
- **F4** — `qa-deployed-drift.bash:117-125` compares by basename, so
  `git-account-helper.j2` (deployed as `git-account-helper`) never matches, never
  increments `CHECKED`, and the pass line still claims the deployed scripts
  match. That is Plan 00094's failure on the one file in scope it cannot see
- **F5** — `pre-commit:341-348` harvests the denylist with
  `field.endswith("_username") or field.endswith("_account")`, but the file's
  convention is the **plural** (`github_accounts` had to be hardcoded), so
  `lastpass_accounts` and others are never harvested
- **F6** — `docker-health.bash:73` iterates `exited created` while its own
  comment and the user-facing message both name `dead`. Docker-only, rare, and
  unexercisable in a container — recorded, not fixed
- **F7** — **the CCY version-bump gate covers `claude-yolo` alone.**
  `pre-commit:105` keys on that one path, and the runtime hash
  (`claude-yolo:72`) is `md5sum` of `"$0"` — the launcher only. It sources six
  libraries totalling **212 KB against the launcher's 149 KB**. Measured: **71
  commits have touched `lib/`, and 22 of them touched `claude-yolo` not at
  all**, so no bump was ever required and none was made. A behaviour change in
  `lib/token-management.bash` therefore ships with an unchanged `CCY_VERSION`,
  an unchanged hash, `validate_ccy_integrity` reporting a match, and no
  changelog entry. `CLAUDE/ContainerRules.md` states the rule as "ANY code
  change requires a version bump"; enforcement covers one file of seven
- **F8** — **`commit-msg` has no `localhost.yml` denylist.** `pre-commit` runs
  static patterns **and** the SEC-02 dynamic denylist; `commit-msg` runs only
  the static patterns, then prints `✓ Commit message looks clean`. So a private
  identifier with no static pattern — an account alias, a machine hostname, a
  service username — is rejected in a staged *file* and accepted in a *commit
  message*. That is the worse of the two: **a commit message cannot be fixed by
  a follow-up commit**
- **F9** — `qa-ansible-syntax.bash:51-61` hardcodes discovery to
  `playbooks/imports` plus the entrypoint, so `playbooks/dev/play-collect-diagnostics.yml`
  is **never** `--syntax-check`ed — and it has no zero guard either.
  `AgentNotes.md` documents three 2.19 parse hazards that *only* this gate
  catches, on a play that runs during an incident. `qa-ansible.bash`'s greps DO
  cover `playbooks/dev/`, so the two Ansible gates disagree about the population
  and neither says so
- **F10** — `qa-ansible.bash:50`'s fail-fast regex accepts `yes` for
  `ignore_errors` but not for `ignore_unreachable`, and only `false` (not `no`)
  for `failed_when`. Both spellings are valid YAML booleans, so
  `failed_when: no` earns `✓ ansible: fail-fast patterns OK` — a green tick on
  the repo's #1 rule. The asymmetry sits inside a single regex
- **F12** — F8 is not hypothetical. Building the denylist and scanning history
  found a **private account alias published on `origin/F44` in four places**:
  two tracked files (`00049-full-repo-audit/research/security.md:188`,
  `00065-…/PLAN.md:117`), **the commit message of `fc20c5c9`**, and — added
  2026-09-10 — **the blob of `31f66d2f`** at
  `00079-podman-container-control/JOURNAL/00079-Journal-26-08-19.md:283`,
  surfaced by a `qa-reviewer` pass over Plan 00079 and confirmed here.
  **Corrected**: this cited `:233,283` until 2026-09-10; `git show --numstat`
  on `0369468b` reports `1 1` for that file, so exactly one line changed and
  `:233` was never touched. A citation nobody re-measures is how an inventory
  drifts into a claim. The working tree was scrubbed by `0369468b`; the
  published blob was not. The
  message is the F8 hole exactly: the identical string in a staged *file* would
  have been rejected by `pre-commit`'s denylist. The alias was in the denylist
  all along under `github_accounts` — `commit-msg` simply never consulted it.
  Rewriting published history is out of scope here (see Non-Goals) and remains
  the owner's call. **The working tree is now clean, and that was a separate
  question the original reasoning ran together with history.** "The files are
  un-editable without scrubbing, which is the gate working" describes the gate,
  not the tree: an identifier sitting in a tracked file is in the copy everyone
  clones today, and removing it is an ordinary commit — `0369468b` had already
  done exactly that for the 00079 journal. `security.md:188` is now redacted by
  substitution per [PlanTriage.md](../../PlanTriage.md), and the sweep below is
  the evidence rather than the intent.
  **Measured 2026-09-10 with the repo's own scanner** — `hook_build_private_denylist`
  feeding `hook_scan_text_for_private`, which report the source FIELD and never a
  value — over every file in `git ls-files`: **1 hit before, 0 after.**
  `00065-…/PLAN.md` had already gone clean, so F12's "two tracked files" was stale
  as well.
  **The count was three until the fourth was found by accident**, which is the
  useful lesson: this inventory is a record of what has been looked at, not a
  proof of what exists. Treat it as a floor.
  **And the published entries cannot be re-measured**, because the denylist is
  derived from a gitignored, mutable file: the pre-scrub blob at `0369468b~1`
  matches no token in today's denylist, and none of the entries in
  `.claude/public-token-allowlist.yml`. That is not a contradiction — a token can
  leave `localhost.yml` — but it does mean "the entry was right and the source
  moved on" and "the entry was wrong" are indistinguishable from here. Record new
  entries with their source field and date, as this one now is: `github_accounts`,
  established 2026-09-02, re-confirmed 2026-09-10
- **F13** — widening the harvest to plural fields is measurably safe, not
  merely plausible: against the real `localhost.yml` it takes the denylist from
  **8 to 10 tokens**, and the two new ones appear in **zero** tracked files and
  **zero** of the last 300 commit messages. A short, common-word token would
  have blocked every future commit, so this was checked rather than assumed
- **F14** — **a coverage LOSS can hide inside a rising count.** Rewriting
  `qa-ansible-syntax.bash` to derive its population from `- hosts:` dropped
  `playbooks/playbook-main.yml`, which contains no play of its own — and the
  reported total went **78 → 79**, reading as a clean gain. The same defect
  class wearing the opposite sign: every previous instance was a number that
  looked complete, this was a number that looked *improved*. Caught only by
  listing the population instead of trusting the total, which is now what the
  gate's own pass line does
- **F11** — `CLAUDE/QA.md` says "ALWAYS and ONLY use `./scripts/qa-all.bash`"
  and "NEVER use individual scripts directly", then documents
  `qa-helper-tests.bash` and `helpers.gnome.check_extension_compat` as gates.
  `qa-all.bash` runs neither. Following the stated rule, a `helpers/` change
  gets `✓ QA passed` with its unit suite never run
