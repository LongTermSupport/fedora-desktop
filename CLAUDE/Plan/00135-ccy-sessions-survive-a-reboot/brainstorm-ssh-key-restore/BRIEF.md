# Brainstorm brief: a restored ccy session is stuck at its SSH key passphrase

Each brainstorm agent reads this brief and works **independently**: don't read the other
agents' files in this folder. Be creative and go wide. No idea is too odd to write down.
Rank your ideas at the end.

## The problem

Plan 00135 restarts, after a reboot, every ccy session that was running before it
(`ccy-sessions restore`, a `systemd --user` unit, with linger so it can run at boot before
anyone logs in). The first real reboot showed a flaw. Every restored session stopped
inside ccy's own startup:

1. ccy's SSH key selection (which identity the container gets), then
2. the passphrase prompt for that key.

Neither prompt has a safe automatic answer, so no session resumed until the owner typed
the passphrase by hand.

The owner's steer: **restore that waits for a graphical login is fine for the desktop, but
not for a headless server**. There, sessions must come back after an unattended reboot
(for example, Plan 00137's self-update cycle) with nobody present.

## Facts to build on (check them in the repo; don't trust this summary blindly)

- **What the key is for.** ccy gives the container an SSH identity so the agent can
  `git push` and sign commits. Since Plan 00139 D5, commits are signed with the *login*
  keys through an ssh-agent: `~/.ssh/id`, or `~/.ssh/github_<alias>` per GitHub account.
  Those keys are passphrase-protected on purpose.
- **Passphrase-free keys were deliberately retired.** The owner rejected them, and D5
  deleted them. Signing keys must not bloat the ssh-agent.
- **The passphrase is already in the vault.** `github_ssh_passphrase` is in Ansible vault
  in `localhost.yml`, and `vault-pass.secret` sits on disk in the checkout.
  `scripts/gh-account-setup.bash` `decrypt_passphrase` reads it. `run.bash` also loads
  keys with it.
- **How ccy gets a key.** See `files/var/local/claude-yolo/lib/ssh-handling.bash` and the
  launcher `files/var/local/claude-yolo/claude-yolo`. It takes a key file (`--ssh-key`),
  or the session's forwarded agent (`--ssh-agent`, which runs with SELinux labelling off),
  or asks with a picker. A passphrase key file is unlocked into a private agent for the
  container.
- **How restore works.** `files/home/.local/bin/ccy-sessions` (`restore`,
  `verify-restore`) and `files/var/local/claude-yolo/lib/session-registry.bash` (the
  per-session record: name, dir, launcher, args, restore flag). Restore answers only
  prompts that have exactly one safe answer.
- **Rules that constrain any answer:** `CLAUDE.md`. In particular:
  - fail fast, never silently skip;
  - Infrastructure as Code only;
  - security first: never a secret in argv, never a secret in a log;
  - public repo: nothing install-specific.
- **Related plans:** 00137 (unattended server self-update, `CLAUDE/Plan/00137-*`), 00139
  (commit signing, `CLAUDE/Plan/00139-*`) and 00135's own `PLAN.md`.

## What to produce

Write ONE file in this folder: `<your-model>-<a-short-slug>.md` (for example
`sonnet-agent-gatekeeper.md`). In it:

1. **Ideas, as many as you can.** For each, give:
   - how it works;
   - what it needs;
   - how it behaves on the desktop and on a headless server;
   - its security cost, in plain words;
   - what could go wrong.
2. **Prior art.** How other tools solve "an unattended service needs a passphrase-protected
   SSH key" (search the web freely). For example: systemd credentials, TPM-sealed secrets,
   `systemd-creds`, gnome-keyring/`pam_ssh`, keychain, `ssh-agent` socket activation,
   hardware keys, SSH certificates, GitHub deploy keys or apps, and short-lived tokens.
   Cite what you find.
3. **Your top three, ranked**, with why. For each, say whether it fits the retired
   passphrase-free decision and the "signing keys don't bloat the agent" rule, or
   knowingly bends them.

## Rules for you

- Read anything in the repo. **Never** read `vault-pass.secret`, `localhost.yml` or
  anything under `environment/localhost/host_vars/` except `localhost.yml.dist`.
- Change nothing outside your one output file. No commits, no Ansible, no installs.
- Public repo: no hostnames, usernames, project names or paths from any real machine in
  your file.
