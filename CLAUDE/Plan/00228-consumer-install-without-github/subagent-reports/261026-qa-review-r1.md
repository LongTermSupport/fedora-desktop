I did not write the report file. My operating rules forbid writing anything, including through redirection or heredocs, so the report is below.

**Verdict: CHANGES** (fix before merge; nothing blocking). The plays are sorted the way you asked and none of them needs a GitHub account. What needs fixing is mostly docs and plan text that don't match what was built, plus two run.bash behaviours.

**Findings, by severity:**

01. **Go is installed on every consumer box, but the docs say it isn't.** `run.bash:1301` installs `git golang`, and `play-golang.yml` is `audience: all` because the shared plays build Go helpers with it. Yet `docs/installation.md` says programming languages are left out, and the planned VM check (Task 3.1) asserts Go is absent, so that check will fail as written. Either say plainly that Go is the installer's own build tool or stop installing it. The research doc (line 102) still lists `play-golang.yml` as developer-only.
02. **The plan describes a design that wasn't built.** The README index row (line 37) says "Not Started (owner Decisions 1-10 open) … attended or headless". PLAN.md's Goals (line 30) and Design (lines 76-96: `RUN_BASH_INSTALL_AUDIENCE`, the bash predicate refactor) also don't match. As built, a headless consumer run is refused and the consumer path is a Go command.
03. **A consumer box still gets offered a GitHub issue.** `--changed`, `--rerun` and every single play still go through `run_playbook_with_issue_option` (`run.bash:1936-1989`). On a failure it asks to create a GitHub issue, which then fails with "GitHub CLI not found". That contradicts the plan (Design line 93, Task 6.3 "GitHub-free").
04. **The test only partly covers point 3.** `scripts/test-run-bash-consumer.bash:103-106` is headed "a single play and --changed", but it only checks that `--changed` is not handed over to the Go tool. It never runs a single play and never checks for GitHub.
05. **`--consumer` on an existing developer box changes things before refusing.** `run.bash:1300-1302` runs `sudo dnf install` and `git pull --ff-only` (over the developer's SSH origin) before the Go tool refuses at `app.go:333`. The docs say it "refuses to run". It should check `host_vars` and refuse first.
06. **No real run yet.** Phase 3 (VM acceptance) is open. Every Go test runs against fakes, and the run.bash test stubs out the Go tool. A main-playbook run with `install_audience: consumer` has never happened, so the plan must not be marked Complete.
07. **The audience gate doesn't check where the guard sits** (`src-go/qa/ansible/audience.go:41-75`). `CLAUDE/AnsibleStyle.md` says the guard is the first task, but the gate accepts it anywhere. All developer plays are correct today: I checked, and only `include_vars` pre_tasks run before the guard.
08. **A comment no longer sits on its code.** The headless "A single play run headless…" comment (`run.bash:1286-1291`) now heads the consumer block; the code it describes is at line 1306.
09. **The `--help` text has two faults.** `--changed` is now one column out of line (`run.bash:989`). And `--consumer` says "Later runs of ./run.bash … do the same", which isn't true for `--changed`, `--rerun` or a single play.
10. **One test name says the opposite of what it checks.** `TestTheInputEndingAtTheMenuIsACancel` expects `ExitFailure` even though `term.ExitCancelled` exists (`app_test.go:442`).
11. **One plan goal is worded wrongly.** The goal says a consumer run "never calls … the GitHub API". In fact Qobuz, photography and DisplayLink call `api.github.com` without logging in, and Galaxy clones its role from github.com over HTTPS. None of that needs an account, but the goal should say so.

**Checked and clean:**

- **Play sorting:**
  - Slack (`play-comms.yml`), Node, Python, Rust, VS Code, terminals, containers, Claude, the GitHub plays and everything experimental are developer-only.
  - Qobuz, photography, video, HD audio, VPNs and the hardware plays are offered to consumers.
- **Settings and vault:** the plays a consumer runs need nothing more than the `host_vars` the tool writes. On real Ansible 2.19, `ansible-config` gives the vault password file as an absolute path, as the Go code expects.
- **Go code:** no discarded errors, and every external command goes through execx.
- **Fail-fast:** no new `failed_when: false` or skip-and-continue paths.
- **run.bash:** version bumped to 1.33.0 with a changelog entry; the bash-limits record went down (2821 to 2818 statements).
- **Install-specific content:** none in the diff.
- **meta-deploy:** no entry needed, because developer machines behave as before.

**Mechanical gates:**

- **`qa-all.bash`:** every gate up to `ccy-token-import-cleanup` passed, then the run hit my 900-second cap (exit 124). So this is not a complete pass and needs a full re-run. The ones that passed include ansible, ansible-syntax (84 playbooks), the Go gate with coverage, and bash-limits.
- **`test-run-bash-consumer.bash`:** 18 of 18 pass.
- **`plan-qa --sweep`:** no blocking findings, and nothing about 00228.

Gate logs are in `<worktree>/untracked/scratch/qa-r1-all.log` and `<repo>/untracked/scratch/qa-r1-planqa.log` (untracked, not committed).
