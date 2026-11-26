# Search report: shell consuming ansible stdout as data

The dispatch named no rule or pattern; I searched from the class description alone. I did not open the detector's rule or config directories and ran no detector. I skipped untracked/ and the 00262 plan folder. I wrote one scratch file (a gitignored grep dump under untracked/scratch/) besides this report. No Write tool was available, so this report was written with a shell heredoc.

## Forms expected before searching

- ansible piped to jq, awk, grep or python; x=$(ansible ...) then a parse; process substitution from ansible
- the same through plan_ansible_adhoc and plan_ansible_playbook (adhoc runs `ansible -v`, which prints "Using .../ansible.cfg as config file" to stdout)
- ansible redirected to a log, then grep or awk on the log; 2>&1 merges
- ansible-inventory --host/--list, ansible-config dump, ansible-doc, ansible-vault
- ANSIBLE_STDOUT_CALLBACK set to json or minimal; recap greps (changed=0, failed=0)
- ansible run from Python or Go

## Searches run

- git grep over the tree (minus excluded paths) for the ansible command family, plan_ansible_adhoc, plan_ansible_playbook, and the stdout-callback variable and setting: about 5,700 lines in non-md, non-go, non-py files.
- failed=0 and the run_ansible function name.
- ansible followed by a pipe, a redirect, 2>&1, &> or process substitution.
- A filter of those hits for command substitution, pipes to jq, awk, grep, sed, read, while, python, head, tail, cut, tr, sort, wc, xargs, and mapfile, readarray.
- Every call site of the two planlib wrappers.
- Python and Go files for subprocess or exec calls to ansible: none.

## A. Instances: ansible stdout parsed as data

### A1. Originating instance, polluted, confident

CLAUDE/Plan/00257-cross-host-lxc-team-members/lxc-no-autostart-acceptance.bash:82-84, in members(): the json callback variable set, then plan_ansible_adhoc localhost -m debug -a ... piped to jq -r on .plays.

- Found by text search (the callback variable), then reading.
- plan_ansible_adhoc does `exec ansible -v` (CLAUDE/Plan/\_planlib.inc.bash:674). -v prints the "Using ... as config file" line to stdout before the JSON, so jq fails. The json callback does not remove it.

### A2. ansible-inventory --host localhost into a Python JSON parse (5 sites)

- CLAUDE/Plan/00161-agent-team-bus-matrix/\_bus-address.inc.bash:19 (parsed 20-24)
- CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/\_acceptance-m7.inc.bash:358 (parsed 359-363)
- CLAUDE/Plan/00203-teams-hot-control-and-robustness/\_acceptance-m3.inc.bash:230
- CLAUDE/Plan/00203-teams-hot-control-and-robustness/\_acceptance-m4.inc.bash:180
- CLAUDE/Plan/00203-teams-hot-control-and-robustness/\_acceptance-m6.inc.bash:206

For m3, m4 and m6 I confirmed only the capture line, not the parse.

- Found by text search, then reading (00161 and m7 read in full).
- Shape: capture of `ansible-inventory --host localhost` with stdin closed, run from the repo root, then json.load(sys.stdin).
- Not polluted by -v: there is no -v and no 2>&1, so warnings go to stderr and stay out of the capture. I did not verify whether any stdout-borne warning can still occur (inventory plugins, vault). Unsure; the coordinator should judge.

### A3. Vault decrypt through the minimal callback into a Python regex

vault.bash:63-78, \_decrypt_var: the minimal stdout callback, `ansible localhost -m debug` with stderr to /dev/null, piped to python3 -c doing a re.search for the "=> {json}" part.

- Found by text search, then reading.
- No -v, stderr discarded. The regex tolerates leading chatter, but when it does not match the script prints the raw output as the value, so chatter would become the value. Unsure.

## B. Verdict and recap greps over ansible stdout (listed separately)

- B1. files/home/.local/share/vmtest/guest-acceptance-desktop-consumer-install.bash:216-232: greps RERUN_LOG for the recap line (ok=N changed=N) and tests changed=0 and failed=0. Anchored on the recap format. I did not trace how the log is produced.
- B2. CLAUDE/Plan/Completed/00142-qa-tool-pin-does-not-converge-under-pipx-uv/reproduce-pipx-uv.bash:284-287 (changed_count, used at 299 and 370): awk over the captured output for the localhost recap line, from a sealed run with the default callback. I did not read run_tasks. Probably benign.
- B3. scripts/test-kickstart-sudo.bash:183-187: ansible-playbook redirected to render.out with 2>&1, then grep -o on the RESULT marker, counted with wc -l. Stdout and stderr merged, pattern-anchored, sealed config.
- B4. CLAUDE/Plan/00061-headless-server-provisioning/acceptance.bash:59-65 (real_tasks, used at 89 and 124): awk over the TASK header lines of a log from ansible-playbook redirected with 2>&1 (lines 88, 123).
- B5. Same file, lines 74-76 and 110: ansible desktop -m debug redirected to a log with 2>&1, then grep -qE and grep -oE on the provisioning_profile value; line 110 greps the log for "is not recognised". Anchored.
- B6. CLAUDE/Plan/Completed/00077-ansible-inject-facts-as-vars-deprecation/acceptance.bash:142-147: dump=$(ansible-config dump --only-changed 2>&1), then grep -q anchored on ^INJECT_FACTS_AS_VARS and False. Merges stderr, anchored on the line start.
- B7. scripts/lint:241: ansible-lint --fix with 2>&1 piped to grep -v on WARNING and DEPRECATION, used as an if condition. This is ansible-lint, not ansible; include only if the class covers it.

## C. Checked and clean, or display-only

- CLAUDE/Plan/Completed/00124-chrome-install-gpg-failure-on-upgraded-host/acceptance.bash:786-817: json callback stdout goes to one file and stderr to another; plan_ansible_playbook has no -v; the file is parsed by parse_check_run, which I did not read. Clean with respect to mixing, and the correct counterpart to A1. Not verified: whether any stdout banner precedes the JSON.
- CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/acceptance.bash:639: list_out from plan_ansible_playbook --list-tasks with 2>&1 is displayed on failure only. The verdict is ledger file size.
- The run_ansible wrappers use 2>&1 on a sealed config (env -i, default callback). Their captures are only echoed on failure; the data comes from files the plays write:
  - scripts/test-git-signing-declared.bash:66,128,183
  - scripts/test-qa-tool-pin-converges.bash:96,203
  - scripts/test-teamchat-play-jinja.bash:69,225
  - CLAUDE/Plan/Completed/00142-qa-tool-pin-does-not-converge-under-pipx-uv/reproduce-pipx-uv.bash:96,172
- ansible --version shown through grep -m1: files/home/.local/share/vmtest/guest-acceptance-desktop.bash:85-86 and guest-acceptance-server.bash:100-101.
- Display-only tee: CLAUDE/Plan/00062-disk-reclaim-tui/deploy.bash:43; run.bash:3530 (ansible-galaxy install).
- Pure runs with no pipe or capture: the many deploy.bash calls, plus the adhoc copy calls with || return 1 at CLAUDE/Plan/00203-teams-hot-control-and-robustness/\_acceptance-robust.inc.bash:279, CLAUDE/Plan/00211-agent-bus-reports-and-questions-rooms/\_acceptance-stage1.inc.bash:107 and \_acceptance-stage2.inc.bash:90, CLAUDE/Plan/00213-agent-bus-performance-go-carve-out/\_acceptance.inc.bash:82, and the playbook calls at CLAUDE/Plan/00161-agent-team-bus-matrix/\_deploy-steps.inc.bash:133 and CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/x30-lab.bash:375.
- ansible-vault encrypt_string (run.bash:3467, vault.bash:153, fedora-install/push.bash:324): ansible-vault, not ansible; I did not read how the output is consumed.
- run.bash:2318 (command -v) and run.bash:2651 (pipx json): not ansible stdout.
- Python and Go: no code runs ansible and parses its stdout, except helpers/self_update/cycle.py:613-636, which parses an ansible-config dump in json format. I did not read where that string is captured. Unverified.
- Playbook shell blocks: the only ansible text was ansible-vault in play-rclone.yml, play-self-update.yml, play-unifi-controller.yml and tasks/save-secret.yml. I did not read each.

## D. What each technique found

Text search found:

- The wrapper call sites and the bare ansible commands.
- The three callback-variable sites: A1, A3, 00124.
- The ansible-inventory captures.

Reading found what text search could not:

- The consumer is often on another line from the producer. out=$(run_ansible ...) looks like a capture but is display-only (section C).
- plan_ansible_adhoc adds -v; that is visible only inside \_planlib.inc.bash:674, not at any call site.
- run_ansible hides 2>&1 and the sealed config inside a wrapper.
- A1's pipe to jq sits on a continuation line, so a single-line grep for ansible and jq misses it.
- 00124 separates stdout and stderr into different files.

## E. Gaps

- I did not read the parse code for A2's m3, m4 and m6, or parse_check_run and run_tasks.
- I did not trace how B1's RERUN_LOG is produced.
- The planlib leg runners (plan_leg, plan_deploy_leg) were not read; I treated them as display.
- A multi-line pipe whose ansible command shares no line with its consumer, other than A1, may exist that my filters missed. I found none.

## F. The draft rule (not active; carried from the paused branch)

This draft was on the branch `plan-xhost-lxc-members` (commit efb4593dc), written for
`.semgrep/bash-conventions.yml` mid Defence Before Fix: it has no fixture yet, was never proven
red or swept, and is therefore kept here rather than in the rule file, where an unproven rule
would fail its gate. When Plan 00257 resumes this class, move it back with its fixture.

```yaml
  # bash-ansible-stdout-as-data (Plan 00257): the stdout of an `ansible` or `ansible-playbook`
  # run, or of the planlib wrappers `plan_ansible_adhoc` (which runs `ansible -v`) and
  # `plan_ansible_playbook`, piped onward or captured by `$(...)` / `<(...)`. That stdout is
  # Ansible's report to a human, not a payload: `-v` writes "Using <path> as config file" ahead
  # of a json callback's document, so jq read "Using" and the acceptance run died.
  - id: bash-ansible-stdout-as-data
    patterns:
      - pattern-either:
          # A pipe (not `||`) after the command, on its line or a `\` continuation of it.
          - pattern-regex: '(?m)^[^\n#]*(?<![\w.$/-])(?:plan_ansible_adhoc|plan_ansible_playbook|ansible-playbook|ansible)[ \t](?:\\\n|[^\n|#])*\|(?!\|)'
          # The command inside a command or process substitution, past anything before it.
          - pattern-regex: '(?m)^[^\n#]*(?:\$\(|<\()[^\n)#]*?(?<![\w.$/-])(?:plan_ansible_adhoc|plan_ansible_playbook|ansible-playbook|ansible)[ \t]'
    message: >
      bash-ansible-stdout-as-data: Ansible's stdout is its report, not data: '-v' (which
      plan_ansible_adhoc passes) prints "Using <path> as config file" first, and warnings land
      there too. Have Ansible write the value to a file and read the file (ansible.builtin.copy
      with content and dest in the run directory), or check the run's exit status.
    languages: [bash]
    severity: ERROR
    metadata:
      category: correctness
      confidence: HIGH
```
