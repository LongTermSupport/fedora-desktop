## QA Review: #294 ansible guard freeze (c68efa1bd, 3a9da1f9c, 449924f89)

**Verdict: FIX-BEFORE-MERGE.** I found no logical regression: every line the original denies is still denied. But the added patterns backtrack exponentially. On one line the original denies in 0.0s, the frozen handler takes 63s, which is longer than both hook timeouts. Everything else is clean or a nit.

### Should fix

1. **The added option patterns backtrack exponentially, and this can delay a deny the original made at once.** `ansible_enforcement.py:75` builds `(?:(?:-c|--releasever|…)\s+\S+\s+|-\S*\s+)*`. When a valued option's value itself starts with `-`, there are two ways to read every token, so a failed match retries an exponential number of splits. Measured with `scratch/qarev-294/r5/redos*.py`, the frozen handler on a line the original allowed: `systemctl` followed by `-H ` 26/28/30 times then `x` takes 1.5s, 3.8s and 10.0s. `dnf`, `yum`, `pip --proxy` and `rpm --root` grow the same way.
   - **How it reaches an original denial.** Take `systemctl -H ×35 x; sys\<newline>temctl restart sshd`. The original denied it in 0.0s, through the daemon's reading, which joins the line continuation. The frozen handler takes **62.97s** before it denies.
   - **Why.** `_offending` (`:190`) runs the slow added patterns over the raw text and its segments before it reaches the joined reading.
   - **The timeouts it crosses.** The relay has `--timeout-ms 30000` (`.claude/hooks/pre-tool-use:51`) and Claude Code's hook timeout is 60s (`.claude/settings.json:15`). I did not verify whether a timeout fails open. If it does, this is a real regression.
   - **Fix.** Make the valued branch unambiguous with `(?:{valued})\s+(?!-)\S+\s+`, or wrap the repetition in an atomic group (`(?>…)*`, Python 3.11+). Add a timing test so it stays fixed. The report's claim that the patterns are "purely additive by construction" holds only for the verdict, not for the time taken.

2. **The new `module.func` sink misses two ordinary spellings, and the docs do not say so.** I ran semgrep with `.semgrep/hook-handlers.yml` on `scratch/qarev-294/r5/sg/probe_handler.py`. It caught `import _line_reader as lr; lr.read(command)`, `from . import x` and a function-local `import`. It did **not** catch:
   - `import pkg.reader; pkg.reader.read(command)` (a dotted module), which reads like the documented "`import mod`" form;
   - `_kw_reader.read(line=command)` (the line passed as a keyword argument).

   The keyword gap applies to the plain-name `$HELPER` sink as well. `CLAUDE/QA.md` "What it does not see" lists only callbacks and `importlib`. Either catch these or add them to that list.

### Nits

- **The oracle models only one of the original's two readings.** `original_denies` searches the raw text (`test_ansible_enforcement.py:67`), but the original searched `get_bash_command`, which joins line continuations. The continuation case is covered by one hand-written test (`:573`), not by the invariant. Checking `original_denies(line) or original_denies(normalise_line_continuations(line))` would close that; the normaliser is daemon code, not the code under test.
- **`__all__` re-exports `judge_raw_command_line`** (`ansible_enforcement.py:38`) only so that `test_the_raw_judge_lives_in_its_own_module` can check it. That is a small YAGNI.
- **The judge's size is given two ways.** The journal entry says "about forty lines" and the report says "about 50"; the file is 49.

### The coordinator's questions, answered

**1. No regression.**
- **Probe run.** I loaded the original from `d3addfdf3^` and the frozen handler from HEAD, both on today's daemon. Corpus: 335 distinct templates (the `frozen_probe_shapes` set plus everything in `r3/shapes.py`, `r3/extra.py` and every `r4/attacks*` file). Each was filled with 17 changes, including `firewall-cmd`, `gconftool-2`, `groupdel` and `dnf updateinfo`, giving 5,695 lines. Result: old denied 5,629, new denied 5,644, **0 regressions**. `handle()` returns deny on every line the original denied.
- **Fuzz.** 40,000 random shell-ish strings: no exceptions from segmentation.
- **Line continuations.** `d\<newline>nf install` and three similar cases are denied by both versions.
- **Oracle list.** It is byte-identical to the original `FORBIDDEN_PATTERNS` block, after the 4-space class indent (I diffed both blocks). It also carries the original's comments.
- **Oracle independence.** `original_denies` uses only its own compiled list and never calls the code under test.
- **Leftover machinery.** None. `_raw_command_judge.py` (49 lines) searches the raw text first and returns on the first hit. Segments and resolved words are searched only after a raw miss, and the judge has no path that returns allow early. The only hazard left in it is the timing issue above.

**2. Over-blocking.** Apart from the additions, it is no worse than the original. Over the probe corpus, the only new denials are the 15 `echo x\<newline><change>` lines, from judging the raw text as well as the daemon's reading. These lines are allowed by the original and denied now, with no system change; each comes from an addition:

| Addition | New false positive |
| --- | --- |
| `snap refresh` | `snap refresh --list` (read-only) |
| npm `--global` | `npm install --global-style x` (`\b` matches before the `-`) |
| dnf options before the verb | `dnf --assumeno install x`, `dnf -q --downloadonly install x`, `grep -w dnf -e install f`, `ls dnf -l install`, `git log --grep dnf -i --grep install` |
| rpm options | `rpm --test -Uvh x.rpm` (dry run) |
| pip variants | `.venv/bin/pip uninstall -y x`, `python3 -m pip -q install -r requirements.txt`, `pip3.12 install x` (all venv-scoped) |
| flatpak options | `flatpak --user install …` (user scope) |
| systemctl value options | `systemctl --root=/mnt/img enable x`, `systemctl -M ct restart x` |
| gsettings / dconf reset | `gsettings reset org.x k`, `dconf reset -f /org/x/` (real changes, arguably correct) |

**3. Acceptance tests.** All five are harmless if the guard misses them:
- `dnf install --assumeno <nonexistent>`: answers no, and the package does not exist.
- `systemctl restart <nonexistent unit>`: fails with unit not found.
- `bash -n -c '…'`: parses only.
- `echo`: prints only.
- `dnf info <nonexistent>`: a query.

They name no file paths, so the absolute-path requirement does not apply. `dispatch_as_bash=True` is valid against the daemon's `AcceptanceTest` (`core/acceptance_test.py:260-279`). `test_acceptance_commands_get_the_verdict_they_expect` passes. I did not run the live daemon harness: the worktree has no installed daemon, and `/workspace` is on F44.

**4. Semgrep rules.**
- The red/green proof is meaningful: the new fixture lines are `.semgrep/hook-handlers.py:273` and `:279`.
- The gate passes and the rules are blocking. `qa-hook-handler-rules.bash` printed `proved: hook-path-resolved-against-cwd, hook-regex-over-command-parameter, hook-regex-over-raw-command, hook-sanctioned-name-impostor` and `passed: 423 failed: 0`.
- `module.func(command)` is caught for plain and aliased imports. The exceptions are in Should fix 2.
- The impostor rule's remaining path exclusion is correct, because the judge's home module has to define the name.

**5. Docs.** These match the code:
- `CLAUDE/QA.md` (gate row, the rule section, the impostor section);
- `qa-gates.tsv:87` and `run.go`;
- the `qa-hook-handler-rules.bash` header;
- the `checks.go` description, which only names the module and is still true;
- `get_claude_md`, the deny message and `_RULE.verbose`, which all state the KNOWN LIMIT and the `-F` / `--body-file` workaround;
- PLAN.md B.14 and the decision journal entry `073849Z-decision-66b8a4.md`.

No stale "all inert" or "excluded by path" claim is left for the taint rule (`git grep` over the tree, excluding JOURNAL and subagent-reports).

**6. Dead code.** None found. No `every_command_is_inert`, `head_is_inert`, `TOOLS`, `INERT_COMMANDS`, `round4_shapes`, `review_probe_shapes` or `ACCEPTED_OVERBLOCKING` remains outside the history and the test that asserts they are gone. `frozen_probe_shapes.py` is used by the test.

**7. Gates.**
- `qa-hook-handlers.bash`: `HOOK-HANDLERS-OK 1975 test(s)`, exit 0.
- `qa-hook-handler-rules.bash`: `passed: 423 failed: 0`, exit 0.

### `TestRefreshProbesTheTerminator` (#354): a flake in the test, not caused by tlsterm

- **Alone:** `go test -count=5 ./internal/teamchat/health/...` passed, and passed again with `-race -tags busfailpoints`.
- **Under load:** the same test and `TestOneClosedListener`, `-race -tags busfailpoints -count=30`, run while `go test -race -tags busfailpoints ./internal/teamchat/...` was running: passed. The load run itself also passed, 74 packages ok.
- **The logged failure** (`untracked/scratch/guard-freeze-qa*.log:73-74` in the worktree): the probe of the *stopped* terminator's port got `not a TLS server` instead of `connection refused`. That class is `classNotTLS` (`matrix/transport.go:143`), a TLS record-header error, which needs a peer that wrote non-TLS bytes.
- **tlsterm cannot produce that.** Its `Close` cancels, closes the listeners and waits for its goroutines before returning (`tlsterm.go:187-191`). A refused admission closes the connection without writing (`:295-300`).
- **The branch did not change the code involved.** The diff from the merge base `73bb544d5` touches none of `Close`, `shutdown` or `closeListener`, and nothing in `health/` or the probe code.
- **Most likely cause:** port reuse. The test stops a listener on an ephemeral port and expects that port to stay closed, so another process's plain-HTTP test server can take it under parallel QA load. I could not reproduce it.
- **Fix in the test:** keep the port held, or use an address that is reserved and never listened on, instead of a freed ephemeral port. b3eabaec0 and e67a8301d are not implicated.

### Checked and clean
- **IaC placement and version bumps:** not applicable. These are hook handlers, with no playbook and nothing under `claude-yolo`.
- **Public safety:** no names, hosts or paths in the diff beyond `<repo>`-style references.

Everything I wrote is in `/workspace/untracked/scratch/qarev-294/r5/`: the harness, `run_all.py`, `own.py`, `fp.py`, `redos*.py`, `sg/probe_handler.py`, and the `qa-hh.out`, `qa-hhr.out`, `health-*.out` and `load-teamchat.out` logs.