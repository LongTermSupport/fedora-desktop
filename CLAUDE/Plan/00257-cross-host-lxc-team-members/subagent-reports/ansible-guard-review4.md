## QA Review: #294 round 4 (d7189e3a9, ea1c10071, 1663f28a4) on `plan-xhost-lxc-members`

**Verdict**: BLOCK

The claim "nothing the original denies can now pass unless the line is harmless" is false. I found three separate regressions. In each one, a line the original handler at `d3addfdf3^` denies is now allowed, and it runs the change. I proved each one in bash with a harmless `touch` in place of the change.

My probes and sandboxes are in `/workspace/untracked/scratch/qarev-294/r4/`. Run `harness.py` with `attacks1.py` to `attacks7.py`, then `gitprobe.sh`, `gitprobe2.sh`, `hd.sh` and `hd2.sh`.

### Blocking

1. **A misread heredoc delimiter hides real commands as heredoc body.** `_raw_command_judge.py:201`, `_HEREDOC_OPERATOR`.
   - The regex reads only the first `[A-Za-z_]\w*` word, or one quoted run, as the delimiter. Bash uses the whole word with its quotes removed.
   - `cat <<EOF-\nx\nEOF-\ndnf install -y foo\nEOF`: the scanner ends the body at the line `EOF`, so the `dnf` line is hidden as data and the line counts as all inert. Bash ends the body at `EOF-` and then runs `dnf`.
   - The same happens with `<<E"OF"`, `<<'E'OF`, `<<E'OF'`, `<<E\OF` and `<<EOF.x`. All 18 probes are old=deny, new=allow. `hd.sh` and `hd2.sh` created `PWNED_HEREDOC` and `PWNED_HEREDOC2`.
   - Fix: resolve the delimiter the way bash does (the full word, quotes removed, quoted if any quote appears). If the word is not a plain `[A-Za-z_]\w*` or a fully quoted form, the line is not inert.

2. **`>&` followed by a digit and then more text is a file write, but the judge reads it as a descriptor copy.** `_raw_command_judge.py:202`, `:366-369`.
   - `_FD_DUPLICATE` (`\d+-?|-`) matches the `2` of `>&2f` and returns. Bash writes stdout and stderr to the file `2f` (confirmed: files `2f` and `1x` were created).
   - Exploit in this repo's layout: `cd CLAUDE/Plan && git log -1 --format='[core]%n%x09fsmonitor = dnf install -y foo' >&00257-cross-host-lxc-team-members/../../../.git/config; git status` is allowed by the new handler and denied by the old one. `gitprobe2.sh` ran the same chain and created `PWNED2`.
   - Fix: a `>&` target counts as a descriptor only when the whole word is `\d+-?` or `-`. Anything else is a file.

3. **The judge treats `--pretty` as taking the next word, but git does not, so `--output` is hidden.** `_raw_command_judge.py:166`, `:173`, `long_value={"--format": 1, "--pretty": 1}`.
   - For a bare `--pretty`, the judge consumes the next word as its value. Git takes `--pretty` with no value, so the next word is a separate option.
   - `git log -1 --pretty --output=.git/config --format='[core]%n%x09fsmonitor = dnf install -y foo'; git status` is allowed. `gitprobe.sh` confirmed that git writes `.git/config` and that `git status` then runs the fsmonitor (`PWNED` created).
   - `git show --pretty --output=f …` and `git log --pretty --ext-diff -p …` pass the same way.
   - `--format v` is also modelled wrongly: git rejects `--format` without `=` (it fails closed, so it is harmless, but the model is wrong).
   - Fix: list `--pretty` only in the `=` form. Better, accept only `--format=`/`--pretty=` and never a value in the next word for an option whose value is optional.

4. **The invariant tests cannot catch any of these, because they check the code with itself.** `tests/test_ansible_enforcement.py:896-900` and `:969-973`.
   - When the original denies and the new handler allows, the test asserts `every_command_is_inert(line)`. That is the function that produced the allow, so the assertion is true by construction. It can never fail except for `DOCUMENTED_NARROWING`.
   - The "independent" oracle `only_inert_words` (`:537-555`) is not independent:
     - It returns `True` for any line containing `<<`, so finding 1 cannot be caught.
     - Its redirect regex has the same `(?!&[0-9-])` blind spot as the code, so finding 2 cannot be caught.
     - It never looks at options, so finding 3 cannot be caught.
   - The 8,247 passing tests therefore assert real behaviour only in the hand-written lists (`round4_shapes.DENIED_NEVER_INERT` and `INERT_ALLOWED`).
   - Fix: build the oracle on the actual shell, for example `bash -n` combined with a parse that does not share the judge's code (`bashlex`, or an AST from `shfmt --tojson`). Add the three shapes above to `DENIED_NEVER_INERT`.

### Should fix

5. **The docs say any renamed look-alike is caught, but one called through its module is not.** `CLAUDE/QA.md` ("Renaming a parameter in a helper does not hide a search, because the call that hands the line over is the finding"; "A copy or a renamed look-alike … is a finding"), `.semgrep/hook-handlers.yml` comment at about line 110, and report item 4.
   - Proof: a copy of the judge renamed to `_line_reader.read_line_as_written`, in a handler that does `command = get_bash_command(h); return _line_reader.read_line_as_written(command, …)`.
   - Semgrep flags only the bare-name call (`fake_handler.py:14`), not the `module.func(command)` call on line 10. The helper sink regex (`(?:self\.)?name`) skips calls made through another object, and nothing flags the look-alike module itself.
   - Either add a sink for `$MOD.$F(..., $X, ...)` where `$MOD` is a module the file imports, or correct the docs to say this case is not seen. The case is also missing from the fixture.

6. **Excluding the judge module by path makes no difference.** I ran a copy of `_raw_command_judge.py` that is not excluded, in `post_tool_use/`. `hook-regex-over-raw-command` reported nothing on it; only `hook-sanctioned-name-impostor` flagged it, by name. The judge takes `line`, which is not a taint source.
   - So "excluded by path, a rename cannot fake a path" is not what protects anything. The trust still comes from the name at the call site plus the impostor rule's check on where it is imported from.
   - Say that in the yml, QA.md, the qa-gates.tsv description, `checks.go` and `run.go`. Otherwise drop the exclusion as dead configuration.

7. **The `qa-changed.bash` result for the round-4 commits is not recorded.** The report says "see the issue comment for the verdict". The plan file should state the result itself.

### Nits

- `_PRINT_FREE` (`_raw_command_judge.py:200`) holds substitution markers. A name like `_SUBSTITUTION_MARKERS` would say what it is. Note that the check at `:302` is load-bearing: it is what stops `for y in 'a[$(…)]'; do for ((…y…))` arithmetic injection, which I probed and found denied. The judge should carry a comment saying so, because the module docstring rule 2 says single quotes are exempt.
- `_COMMENT_START_AFTER` is identical to `_WORD_END` (`:205-206`).
- `test_head_is_inert_returns_a_bool` has a `[]` case that returns before asserting anything (`:999`). `test_the_closed_list_machinery_is_gone` only checks that deleted names stay deleted (`hasattr`).
- `ansible_enforcement.py:141-143` has only one blank line before `class`.
- B.14's sentence "a line is judged as written unless it is all inert … in the module `_raw_command_judge`" reads as covering all three swept handlers. Only `ansible_enforcement` uses the judge; `hidden_git_mutation` uses `command_position_segments`.

### Pre-existing gaps (not regressions; noted, not blocking)

- `git status`, `diff`, `log -p`, `add` and `commit` run fsmonitor, textconv, external diff, filters and hooks that are already configured, and `git commit -S` runs the configured gpg program. The original allows those lines too.
- `git commit -Sm 'x'`: git reads `m` as `-S`'s optional key id, so no message is given and the configured editor is started. The judge reads `-m` as taking the message. This does not run the line's text.

### Checked and clean

- **Round-3 corpus (121 shapes × 12 changes = 1,452 cases)**: 188 lines the old handler denied now pass. Every one is a harmless text mention (echo, printf, grep, `git commit -m`, `gh … --body`, quoted heredoc to stdout, here-string). None of them executes anything.
- **Writes without a redirect token**: all of these are denied: `cp`, `mv`, `install`, `printf -v`, `tee`, `cat>f`, `>f` at line start, `{ …; } >f`, `exec >f`, `2>f`, `&>f`, `>|f`, `>&f`, `<>`, `>/dev/fd/3` and `/dev/tcp`.
  - `jq --rawfile`, `-f`, `-L` and `--from-file` are not listed, so the line is denied.
  - `gh --body-file`, `git commit -F`, `jq input_filename`, `echo -e` and here-strings are allowed and harmless.
- **Environment prefixes**: every probe is denied, because an assignment becomes the head and is not on `TOOLS`. Probed: `GIT_EXTERNAL_DIFF`, `GIT_PAGER`, `PAGER`, `GIT_SSH_COMMAND`, `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT`/`KEY_0`/`VALUE_0`, `LESSOPEN`, `GREP_OPTIONS`, `RIPGREP_CONFIG_PATH`, `JQ_LIBRARY_PATH`, `BASH_ENV`, `GH_*`, `EDITOR`, `IFS`, `PATH`, `export` and `declare`.
- **Global and program-running options**: git global options, `-p`/`--paginate`, `--ext-diff`, `--textconv`, abbreviated options, `gh alias set`, `gh <alias>`, `--web`/`-w`/`-e`, `rg --pre`/`-z` and `grep -P`/`-f` are all denied.
- **Other probed shapes**: the remaining shell-syntax probes of the round 3 corpus are denied.
- **False positives (#294)**: still pass: `git commit -m`, `-am`, `-m'…'`, echo, `grep -rn`, `gh issue comment --body-file`, and a quoted heredoc to `git commit -F -`.
- **Semgrep gate**: blocking (`gate: true` in `checks.go`, row in `qa-gates.tsv`). The fixture proves all four rules. A copy of the judge with the same name in another directory is red under the impostor rule.
- **PLAN.md B.14**: in the plan's task format, and the "three Semgrep rules" count is accurate.

### Mechanical gates

- `qa-hook-handlers.bash`: rc=0, 8247 passed.
- `qa-hook-handler-rules.bash`: rc=0. `proved:` names hook-path-resolved-against-cwd, hook-regex-over-command-parameter, hook-regex-over-raw-command and hook-sanctioned-name-impostor; `passed: 424 failed: 0`.
- `plan-qa --lint PLAN.md`: 0 findings. I had to run `/workspace/.claude/hooks-daemon/bin/hooks-daemon plan-qa --project-root <worktree> --lint …`. This is a tooling fault worth logging: the worktree has no `.claude/hooks-daemon/bin`, and without `--project-root` the command fails with "not a plan document".
- `qa-all.bash` and `plan-qa --sweep`: not run. A full QA run inside a sub-agent is barred, and this review was asked for the two handler gates.
- `--syntax-check`: not applicable, no playbooks changed.