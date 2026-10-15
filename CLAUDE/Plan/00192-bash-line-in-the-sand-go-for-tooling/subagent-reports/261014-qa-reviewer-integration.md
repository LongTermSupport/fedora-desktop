## QA Review: `origin/F44...origin/integration/261014` (Plans 00192, 00177 Phase 9, 00194; meta-deploy.list)

**Verdict**: FIX-BEFORE-MERGE

I could not write the report to `CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/subagent-reports/261014-qa-reviewer-integration.md` because I have no write tool, so it is here instead.

Scope: 69 commits, 254 files. The local checkout is on F44 (`0700654d`) and nothing has the integration tip (`257c0e45`) checked out. I reviewed by `git show`/`git grep` against `origin/integration/261014`, and used GitHub Actions for the mechanical gates.

### Blocking
None.

### Should fix

1. **`meta-deploy.list` drops the 00161 acceptance, which is still pending.** `CLAUDE/Plan/meta-deploy.list:23-31`
   - Commit `f2744e28` ("after the 00161 acceptance…") removed `00161-agent-team-bus-matrix/acceptance.bash`. It also removed its warning to launch `ccy` interactively first, so the saved choices are re-saved before U20.
   - The acceptance has not run. `CLAUDE/Plan/00161-agent-team-bus-matrix/PLAN.md:100` and `:112` both say "Pending: acceptance.bash".
   - `CLAUDE/Plan/00177-ccy-teams-human-friendly/PLAN.md:223` (F6) still says the list has `play-claude-yolo.yml` "before the 00161 acceptance". The list no longer agrees with it.
   - The list's own rule (`meta-deploy.list:3`) is "exactly what needs running NOW". If this merges before the owner's run, the pending acceptance disappears from the queue.
   - Fix: put the 00161 entry back with its warning, in the order 00177 F6 states (after `play-claude-yolo.yml`), or record why it moves.

2. **The exit-2 message fix (B27) went into one row out of five.** `scripts/qa-gates.tsv:32-34,37`
   - The `ansible-syntax` row (line 36) got a message covering every exit-2 cause. The rows for `bash`, `python`, `patterns` and `js` still say "Missing required tools".
   - Their Go tools now exit 2 for many reasons that are not a missing tool:
     - qa-bash: discovery missed a tracked script; shellcheck printed no JSON array; the ratchet cannot find a base (`src-go/qa/bash/directives.go:341`, "origin's default branch is not known here").
     - qa-python: no `RUFF=` pin; ruff did not list a file; a template holds Jinja statements.
     - qa-patterns: the rule self-test failed; a script was not parsed.
   - The table header says stage status 2 aborts the whole run with this text. So a missing `origin/HEAD` aborts `qa-all` with "Install them and re-run", the misleading message B27 was written to remove.
   - Fix: give the four rows the B27 wording ("could not produce a result … see the ERROR line above") and pin the change as a deviation.

3. **In CI, the shellcheck suppression list (`scripts/shellcheck-disable.allowlist`) does not stop a push straight to F44.** `src-go/qa/bash/directives.go:332-347`
   - The base is `merge-base HEAD origin/HEAD`. On a push to F44, CI fetches `origin/F44` at HEAD (`.github/workflows/qa.yml:120-131`), so the base is HEAD itself and the allowlist is compared with itself. An added or raised record passes CI.
   - The bash-limits allowlist solved exactly this case with `PUSH_BEFORE` → `QA_BASH_LIMITS_BASE` (`qa.yml:109-132`). The new list ignores that variable.
   - `CLAUDE/QA.md:1008` says the list is "never raised or added to (the gate compares it with the merge-base)" and calls it "a ratchet like `scripts/bash-limits.allowlist`". On this repo's usual route (commits straight to F44), CI does not enforce that. Only a local `qa-all` run before the push does.
   - Fix: take the base from `QA_BASH_LIMITS_BASE` when it is set (one base variable for both lists), with a test for the default-branch case.

4. **The bash exclusion functions and the bash trees reader have no production caller, and the docs say they do.**
   - `scripts/qa-discovery.bash:22-23` says "The exclusion test stays in bash: qa-js asks it once per file". `CLAUDE/QA.md:921-922` says `qa-js` and `qa-ansible-syntax` ask `qa_is_excluded` per file. Both are Go now and call neither.
   - `git grep` on the tip finds `qa_is_excluded`/`qa_py_is_excluded` only in their definitions (`qa-discovery.bash:38-39`), `src-go/qa/discovery/realtree_test.go:129-132`, specs and reports.
   - `scripts/lib/qa-trees.bash` was hardened in this diff (+4 lines) and has a bash-vs-Go agreement test, but nothing in production uses its arrays.
   - Fix: delete the two functions, `_qa_path_in_dirs`, the `lib/qa-trees.bash` source and the reader with its agreement test (YAGNI), and correct QA.md:921 and the wrapper header. Otherwise name a real caller.

5. **Plan 00192's status no longer matches the branch.** `CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/PLAN.md`
   - "ported, not merged" appears at lines 106, 118, 138, 158, 177, 193, 213, 224 and 243. Every one of those ports is merged here, and qa-ansible (106) is already on F44 (`b4027b70`).
   - Line 98 still says qa-all is "merging into F44 … through that one integration branch".
   - Line 192 says the qa-ansible-syntax review is "re-review pending". The only report is `subagent-reports/261014-fable-review-qa-ansible-syntax.md`, so it was merged without the re-review it names. Either run the re-review or record why it was dropped.

6. **The "spec first" rule is broken for the new `yaml` discovery list.**
   - `src-go/qa/discovery/run.go:163` defines a `yaml` list, and `src-go/qa/ansiblesyntax/discover.go:97` depends on it.
   - `SPEC-qa-discovery.md` has no expectation for it. D16 (`js`) is there; `yaml` is not.
   - `src-go/CLAUDE.md:179` says "`SPEC-qa-discovery.md` (D1-D14)", but the spec has D15 and D16. `scripts/qa-discovery.bash:15` lists `shell | python | python-templates | js`.
   - Fix: add a D17 for `yaml` and correct the range in `src-go/CLAUDE.md`.

7. **The root reader calls `die()` before defining it.** `helpers/agent_bus_lxc/derive.py:63-67`
   - The container/user name check on line 63 calls `die(...)` on line 64, but `def die` is on line 67, so it raises `NameError`.
   - It still fails closed, but with a traceback instead of the intended message.
   - No test covers it: the message "not one the play accepts" appears nowhere in `tests/` or `src-go/`.
   - Fix: move `def die` above the check, and add a test with a bad container name passed straight to the script.

### Nits

8. **qa-changed routing is uneven** (`scripts/qa-routing.tsv:45,56`). A change to `src-go/qa/{bash,python,js,patterns}` runs that gate whole. A change to `src-go/qa/ansiblesyntax/*` or `src-go/qa/deployeddrift/*` runs only `qa-go`. `qa-all` still covers them.
9. **Plan 00194's relay code ships before its design is agreed.** `relay.py`, `sentlog.py` and the relay `PostToolUse` hook go into image 2.56 while Task 1.2 ("The owner agrees the design", `00194 PLAN.md:56`) is still open, and Task 2.1 waits on an upstream release. The code is inert until a launch sets `PINGBUS_DELIVERY=relay`. Confirm the owner wants unused code in the image now.
10. **`files/var/local/claude-yolo/.shellcheckrc` is now ignored.** The gate runs with `--norc`, but the file still reads as a place where suppressions could go.

### Checked and clean
- **CCY and container versions**:
  - `CCY_VERSION` is 3.98.0 and `REQUIRED_CONTAINER_VERSION` matches the Dockerfile LABEL at 2.56.
  - The kit change (hooks.json SessionEnd/PostToolUse, plugin 1.1.0) and the pingbus zipapp (bundles every `helpers/pingbus` and `helpers/agent_bus` `*.py`, so `relay.py`, `sentlog.py` and `checkout.py` are covered) are in the image and covered by the bump.
  - There is a changelog entry.
  - No `lib/*.bash` changed.
- **Order of the meta-deploy entries**: `play-agent-bus.yml` installs the LXC kit from `/usr/local/share/agent-bus/kit` and runs the new `members.py`, before `play-claude-yolo.yml` rebuilds the image and `ccy-teams`. The order is right; the only problem is finding 1.
- **IaC placement**: no new play. The LXC derivation lives in the play that owns LXC members (`members.py`, run by `play-agent-bus.yml`); `declare.py` imports `parse_members` instead of copying it.
- **Every gate in `qa-gates.tsv` still runs**: all wrappers exist. CI run 38639012307 on tip `257c0e45` passed, printing 92 ✓/✗ lines with the same counts as before the ports: bash 433, python 311 plus 1 template, patterns 433, ansible-syntax 83, js 30, nokill 7, helper-tests 4340 tests in 131 modules. gitleaks passed.
- **Gates not made weaker**:
  - The `deployed-drift` stage line is still required (`gates.Summarise`/`driftLine`).
  - qa-bash keeps its check that no tracked shell script is missed.
  - qa-python replaces its tracked-list comparison with ruff's `--show-files` proof, a deliberate change recorded as SPEC B3. Both lists used the same predicate, so the old check could not catch much.
  - shellcheck (`--norc`), ruff (`--config`) and ESLint (`--no-eslintrc`, `--no-inline-config`) are now pinned. That is stricter than before.
  - The new golangci exclusions are scoped to specific files and rule codes.
- **Old discovery function names**: no caller is left anywhere outside plan files.
- **Bash limits**: eight allowlist rows removed; each wrapper is a short `exec` of its Go tool.
- **Public-repo safety**: I scanned the added lines for home paths, IPs, emails and hostnames. Only placeholders turn up (`/home/user`, `Example-Org`, RFC test values). `2024box` is a made-up failing input in a review.
- **Docs for LXC members**: `localhost.yml.dist`, `docs/agent-bus-lxc-members.md` and `members.py` agree that `host` and `seat` are optional overrides and `checkout` is required for a present member (`members.py:339`). No stale `host: <role>` text remains.
- **Plan housekeeping**: no new plan folders, so no README index rows are owed; nothing was deleted.

### Mechanical gates
- **qa-all.bash**: not run locally, because the tip is not checked out anywhere and making a worktree would change state. CI ran it at `257c0e45` and it passed (run 38639012307). Before that, the qa-js and qa-python merge runs were cancelled and later runs passed.
- **plan-qa --sweep**: not run. It only reads the working tree, which is F44, not the branch. Run it on a checkout of the tip.
- **ansible-playbook --syntax-check**: no playbook changed in the diff. CI's ansible-syntax stage passed on all 83 playbooks.
- **qa-helper-tests.bash**: needed, because `helpers/` and `tests/helpers/` changed. It passed in CI (4340 tests, 131 of 131 modules tracked).
- **ESLint and the extension compat check**: only `extensions/.eslintrc.json` changed. ESLint runs inside the CI js stage, which passed (18 linted). No `metadata.json` changed, so the extension compat check was not needed.

Files referenced:
- /workspace/CLAUDE/Plan/meta-deploy.list
- /workspace/scripts/qa-gates.tsv
- /workspace/src-go/qa/bash/directives.go
- /workspace/.github/workflows/qa.yml
- /workspace/scripts/qa-discovery.bash
- /workspace/scripts/lib/qa-trees.bash
- /workspace/CLAUDE/QA.md
- /workspace/CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/PLAN.md
- /workspace/CLAUDE/Plan/00192-bash-line-in-the-sand-go-for-tooling/SPEC-qa-discovery.md
- /workspace/src-go/CLAUDE.md
- /workspace/src-go/qa/discovery/run.go
- /workspace/helpers/agent_bus_lxc/derive.py
- /workspace/CLAUDE/Plan/00177-ccy-teams-human-friendly/PLAN.md
- /workspace/CLAUDE/Plan/00161-agent-team-bus-matrix/PLAN.md
- /workspace/scripts/qa-routing.tsv

All paths are as they are on `origin/integration/261014`; the local checkout is F44.