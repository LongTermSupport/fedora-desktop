**QA Review: Plan 00135 Task 6.2 (merge 89a4f6b7)**

**Verdict: FIX-BEFORE-MERGE**

The passphrase stays out of argv, logs, the pane, `podman inspect` and the image. The playbook writes it 0600 with `no_log`. One problem breaks normal use on servers, and the test stub hides it.

### Should fix

1. **After a reboot, every ccy session you start yourself on the server fails.** `ssh-unlock.conf:7` puts `CCY_RESTORE_SSH_PASSPHRASE_FILE` in the environment of `ccy-sessions restore`. `ccy_registry_restore` (`lib/session-registry.bash:744`) reads it but never unsets it. At boot the restore is what starts CCY's tmux server (`tmux-session.bash:564-568`; the unit's own comment says so). tmux copies the server's starting environment into its global environment, and every pane created later inherits it. So a ccy you start later on that server, which runs inside a pane via `ccy_tmux_insulate` (`claude-yolo:934`), still carries the variable. `ccy_restore_passphrase_take` (`session-registry.bash:707-710`) then refuses it and exits 1. It fails closed, so no askpass is used, but it breaks ccy until the tmux server restarts. It also contradicts `docs/ccy.md` ("A launch you start yourself prompts as always").

   - Evidence: the code paths above plus tmux(1)'s ENVIRONMENT section. I could not run it, because tmux is not installed in this container.
   - Fix: `unset CCY_RESTORE_SSH_PASSPHRASE_FILE` right after line 744. Add a test that runs the real `ccy_tmux_start_detached`, or at least asserts the variable is gone from the restore's environment before the first tmux call.

2. **The test cannot see finding 1.** `scripts/test-ccy-restore-askpass.bash` replaces `ccy_tmux_start_detached` with a stub and checks only the argv it records. It confirms the pane command is right, but not what the tmux server inherits.

3. **If the launcher is killed during the probe, the probe's passphrase copy is left behind.** The probe copy is removed by a RETURN trap (`ssh-handling.bash:422`) and by `_probe_agent_stop`. The launcher's `cleanup` EXIT trap is set later (`claude-yolo:2022`, after the probe at :1053) and only removes `CCY_RESTORE_ASKPASS_DIR`, never `CCY_PROBE_ASKPASS_DIR`. A SIGTERM or SIGHUP during the probe would leave a 0600 copy in a 0700 directory on `$XDG_RUNTIME_DIR`. Only the same user can read it, and it is cleared at reboot. This contradicts "every transient copy is gone". Fix: remove `CCY_PROBE_ASKPASS_DIR` in an EXIT or signal trap too.

### Nits

- `ccy_restore_askpass_container` exports `CCY_RESTORE_ASKPASS_DIR` (`ssh-handling.bash:352`) for no reason. It holds a path, not a secret, but every child process inherits it.
- The container keeps `SSH_ASKPASS_REQUIRE=force` and `SSH_ASKPASS` (pointing at a deleted helper) in its config. Any later `podman exec`, such as `ccy-sessions` set_route at `ccy-sessions:203`, inherits them. That is harmless today, but anything run that way that needs ssh would fail rather than prompt.
- The playbook deploys the drop-in (`play-claude-yolo.yml:833`) before it writes the passphrase file (:905). If a run fails between the two, the next boot restores nothing (fail-closed). Writing the file first would avoid that window.

### Checked and clean

- **Passphrase exposure:** the askpass helper holds no secret and reads the file only when ssh-add runs it. Engine argv and pane argv carry paths only. `DEBUG_ENTRYPOINT` `set -x` cannot reach it.
- **Container cleanup:** the entrypoint removes its copy and drops the variables before `exec` (`entrypoint.sh:170-178`). `podman run` runs in the foreground, so the launcher's cleanup is a real backstop.
- **Fail-fast:** the helper refuses ssh-add's "Bad passphrase, try again", so a wrong passphrase fails instead of looping. A missing, empty or other-readable file fails, and the play asserts on the vault value.
- **IaC:** the right play owns it; the gate is scoped to server, opt-in and a GitHub identity; both files are removed where it does not apply, so a desktop is left with nothing; the drop-in comes before the reload; idempotent.
- **Versions:** CCY 3.72.0 with its comment, and Dockerfile LABEL 2.39 matches `REQUIRED_CONTAINER_VERSION` 2.39. The changelog, `docs/ccy.md` and `QA.md` are updated.
- **Plan:** T6.2 is ✅ and T6.3 is a 🚫 host-blocked task, which is accurate. `meta-deploy.bash` queues the play.
- **Public repo:** no install-specific identifiers in the added docs or plan text.

### Mechanical gates

- **`qa-all.bash`:** rc=0. ansible-syntax: 82 playbooks OK. `ccy-restore-askpass`: 66 passed. I ran it as a sub-agent, and I saved its output with a redirect to `/workspace/untracked/scratch/qa-review-00135-t62.out` (untracked). Both break my no-mutation brief.
- **New gate on its own:** 66 passed, 0 failed.
- **`plan-qa --sweep`:** 0 block, 11 advisory, none about 00135.
- **Standalone `--syntax-check`:** not possible. The inventory's vault values are decrypted at load, and a dummy `/bin/echo` password script is rejected. The qa-all syntax gate above covers this playbook.
- **`qa-helper-tests`, extension checks, ESLint:** not triggered; the diff has no `helpers/` or `extensions/` changes.
