I did not write any file in this review; everything is delivered inline here. The stop hook (`subagent_report_path_verifier`) flagged a path that does not exist, `-script-wrapper.md`. It seems to have cut a hyphenated filename at a hyphen and read it as a claim that I wrote a file. The file I actually mentioned was written by the fixing agent before this review, and it is on disk: `untracked/scratch/upstream-written-script-wrapper.md` in the sealed-carry worktree. This review cannot file issues, so please log the hook's misreading as an issue.

## QA Review: issue #294 fix round 2 (dc9c9a9da..72129493f, `plan-xhost-lxc-members`)

**Verdict**: BLOCK

Round 2 opens new regressions in the same class that round 1 blocked on. Several commands the original handler (`d3addfdf3^`) denied are now allowed, and they really run something. Method: I loaded both versions of `matches()` in memory from `git show` and ran about 260 cases through each. I wrote no files.

### Blocking

1. **The `-n` reader treats commands that do run as syntax checks.** `/workspace/untracked/worktrees/sealed-carry/.claude/hooks/handlers/pre_tool_use/ansible_enforcement.py:213-219`
   - Line 217 checks `"n" in word[1:]` on a word starting with `+`, so `+n` (which turns noexec **off**) counts as `-n`.
   - Line 213 returns at the first `-o noexec` and never reads a later `+o noexec`.
   - I ran these with `echo`: `bash +n -c '…'`, `bash -n +n -c '…'` and `bash -o noexec +o noexec -c '…'` all executed.
   - All three are now allowed with `dnf install` in the body, as are their `sudo` and `env` forms. The original denied them.
   - No test covers `+n`, and none covers `noexec` at all.
   - Fix: honour the last setting of `n` and `noexec` in option order, and treat a `+` word as clearing it.

2. **A written script run behind a wrapper still gets through. The fix is a closed wrapper list, not a fix for the class.** `ansible_enforcement.py:131-141`, `:264-268`
   - With a written `s.sh` that holds `dnf install`, the original denied all of these and round 2 allows them:
     - `script -c 'bash s.sh'`, `script -qc ./s.sh /dev/null`
     - `watch bash s.sh`
     - `find … -exec bash {} \;`
     - `systemd-run bash s.sh`
     - `su -c 'bash s.sh'`, `runuser -u root -- bash s.sh`
     - `flatpak-spawn --host bash s.sh`
     - `busybox sh s.sh`
     - `perl -e 'system("bash s.sh")'`, `python3 -c 'os.system(open("s.sh").read())'`
     - `make -E 'x:;bash s.sh'`
   - **The worst one has no wrapper at all:** `echo 'dnf install vim'>s.sh && bash s.sh`, with no space before `>`.
     - `_REDIRECT_TO_FILE.fullmatch` (`:147`, `:276`) never sees the redirect, because the token resolves as the single word `dnf install vim>s.sh`.
     - The daemon's own view also blanks it: `command_position_code_segments` returns `[' echo _ ', ' bash s.sh']`.
   - None of these shapes is in the tests.
   - Fix: once a line writes a file, every later segment counts as running it unless its head is known to be inert. Recognise redirects glued to a word, or judge only the written body.
   - Out of scope, because they do not run on this host: `ssh localhost bash < s.sh` and `toolbox run bash s.sh`.

### Should fix

3. **The fallback that judges the line as written is too wide.** `:303-325`, `:494-497`
   - It fires when two things are both true:
     - any redirect writes to a file, `2>/tmp/e` included;
     - any later segment starts a shell, or a wrapper with a shell name anywhere in its arguments.
   - It then regex-searches the whole raw line, commit messages included.
   - Each of these is denied, with no system change anywhere:
     - `echo 'dnf install vim' > notes.txt && bash other.sh`
     - `git commit -m 'playbook: dnf install foo' > /tmp/c.log && bash scripts/qa-changed.bash`
   - This is #294's symptom again. The original also denied these, so they are not regressions. Fix: search only the text written to the basename that is later run.

4. **The Semgrep rules can be escaped by renaming.** `/workspace/untracked/worktrees/sealed-carry/.semgrep/hook-handlers.yml`
   - Red/green is real: on the original they report `ansible_enforcement.py:88` and `hidden_git_mutation.py:225`. The tip is green, the fixture passes 3/3, and they are wired into `qa-all` and `qa-changed`.
   - But `hook-regex-over-command-parameter` is turned off by renaming a parameter to `text`. The one raw-line search left in the repo (`_offending_text` → `self._search(command)`, `:495`) is invisible to both rules.
   - That is a suppression nobody can see, and narrowing a rule is the owner's decision. Fix: a visible marker that names the reason, put to the owner.

5. **New gaps (the original also allowed these):**
   - `pip3.12 install` (`:101`)
   - `dnf -d 0 install`: `-d` and `-e` are not listed as options that take a value.
   - `systemctl --user --system enable foo`
   - Quote splitting: `d''nf`, `in''stall`.
   - The report's "found, not fixed" verb list has no issue or plan reference; the dogfooding rule requires it be logged.

6. **The upstream issue body needs one addition.**
   - It was produced by the generator: the `body_sha256` matches the body when I recompute it. It has no install-specific names.
   - It says a bare `bash s.sh` keeps the whole text. The glued-redirect case shows the daemon loses a bare run too; add it.
   - Two smaller problems: `sudo source s.sh` is not a valid command, and the handler field `sudo_pip` misleads.

### Nits

7. **Gate descriptions still name only the old path rule:**
   - `scripts/qa-gates.tsv:87`
   - `src-go/qa/changed/checks.go:102` (the qa-changed pass line)
   - `src-go/qa/sttrules/run.go:155-159`
   - `CLAUDE/QA.md:171`, which links only the old rule
   - the header of `scripts/qa-hook-handler-rules.bash`

   The new rule sections in QA.md do match the rules.
8. The rules gate prints `passed: 421 failed: 0` but never names which rules it proved.

### Checked and clean

- **No regression on direct forms:** none of the brief's bypass shapes without a written file got through, and the newly listed spellings are denied.
- **Allowed now, as intended:** quoted or text-only mentions.
- **Acceptance tests:** the probes are harmless if the guard misses them, and use the absolute `$CLAUDE_PROJECT_DIR/untracked/acceptance/` form the daemon expands.
- **`hidden_git_mutation`:** the changes are correct.

### Mechanical gates

- `qa-hook-handlers.bash`: 749 passed.
- `qa-hook-handler-rules.bash`: passed.
- `qa-changed.bash --files` over the 8 changed files: all green, exit 0.
- `qa-all.bash`: not run. A sub-agent may not run the full suite (`R-SUBAGENT-FULL-QA`).
- `plan-qa --sweep`: 2 blocking findings, both in other plans (00244, 00238); none in 00257.
- `ansible-playbook --syntax-check`: not needed; no playbook changed.