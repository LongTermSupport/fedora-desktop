# 00228 VM acceptance: Tasks 4.2, 3.1, 3.2

Branch `fix/00228-vm-acceptance`, cut from F44 at 842d88ddd. Not pushed or merged.

## Commits

- `c65421e1d` Task 4.2: README links "Install for Someone Who Does Not Write Code";
  `docs/playbooks.md` names the install audience beside the provisioning profile.
- `5bdfcc892` Task 3.1. **Its subject line is wrong**: it says Task 4.2 because a stale
  message file was reused. The content is Task 3.1. Amending is not allowed, so the journal
  records the correction.
- the Task 3.2 commit: plan-local scripts, the meta-deploy entry, this report.

## Built

- **The manifest key `audience: developer|consumer`** goes in `helpers/vmtest/scenarios.py`,
  test first. It is allowed only on a desktop scenario with no `run_env` and no `host_only`.
  `validate_manifest` prints it as `audience=` and `facts.go` reads it strictly.

- **The pty driver.** vmtest had none: desktop runs go through the session runner, headless.
  `src-go/internal/vmtest/attend.go`, test first, runs `ssh -tt -e none` with an `os.Pipe` as
  stdin. It drops CR from the output and answers each known prompt only once that prompt has
  appeared. A sudo, BECOME, git credential or passphrase prompt ends the run as a fail.

- **`consumer.go`** drives the consumer run:

  1. It waits for the seat0 session and its `session.env`.
  2. It clones over HTTPS, pinned at the run's commit. The branch tracks a local ref, so
     run.bash's `git pull --ff-only` does nothing.
  3. It runs `~/run.bash --consumer` with the session's bus and display variables.
  4. It re-runs `~/Projects/fedora-desktop/run.bash`, teed to `/var/lib/vmtest/rerun.log`.

  The run reports three new divergences: `run-bash-attended-over-ssh-terminal`,
  `checkout-pinned-by-harness` and `lab-sudoers-nopasswd`.

- **The scenario** is `desktop-consumer-install` in `vars/vm-test-scenarios.yml`, with
  `reboot_before_checks: true`. Its checker is
  `files/home/.local/share/vmtest/guest-acceptance-desktop-consumer-install.bash`, with 17
  checks. The plan called it `-desktop-consumer.bash`, but vmtest finds a scenario's own
  checker by the scenario id. The checks are: pinned commit, Fedora version, HTTPS origin,
  consumer host_vars, no key pair, no gh login, no NOPASSWD block, consumer apps (Firefox,
  Chrome, RPM Fusion), Go, setup marker, Node, Claude Code, ccy, VS Code and LXC absent, the
  podman play not run, and every re-run recap `changed=0 failed=0`.

- **Coverage.** On F44 the vmtest package was at 86.9%, below go-qa's 90% floor, which diff
  mode applies to any package a change touches. It is now 90.5%, through two additions:

  - a fault-injection test over five whole runs: every command of a passing run is failed in
    turn, and the run must end with an exit code it owns, give a reason, and remove the guest
    it booted;
  - tests of the `Env` pieces.

- **Docs.** `docs/vm-acceptance-testing.md` has a new section, "The consumer install
  scenario".

- **Plan scripts.** `deploy.bash` runs play-vm-test-lab.yml and has no acceptance leg
  (STANDARD-EXCEPTION(R9), the same as 00161). `acceptance.bash` refuses an unpushed HEAD,
  ensures the desktop and server-fast bases, runs the three scenarios with `--commit HEAD`,
  and writes `acceptance-report.md` with `COVERAGE: N of 3`. It exits 0, 1 or 2.

## Verification here

- The Python vmtest tests and the Go vmtest tests pass, with `-race`.
- `qa-vmtest-manifest.bash` passes: 9 scenarios agree with their checkers.
- Both plan scripts are clean under bash -n and shellcheck -x, and both refuse to run in the
  container.
- `meta-deploy.bash --list` validates the queue.
- `qa-changed.bash` passed before each commit.

## Host

The plan is queued in `CLAUDE/Plan/meta-deploy.list` after `changed-plays`. The owner runs
`./CLAUDE/Plan/meta-deploy.bash`. HEAD must be pushed first, because the guest fetches the
commit.

## Open

- Tasks 3.1 and 3.2 still need their host run (pending in the plan).
- Things the host run may expose:
  - a main-playbook task that is not idempotent, which fails `rerun-changed-nothing`;
  - a prompt the driver does not know, which waits out the 90-minute provisioning limit.
- Tasks 5.1 (qa-all) and 5.2 (qa-reviewer) are still to do.
