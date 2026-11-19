## 18:33 · decision · —

The blanket grant described in RESEARCH.md §4 has changed (owner's Q15 ruling, option 1).

- `<user> ALL=(ALL) NOPASSWD: ALL` in /etc/sudoers is no longer set by play-basic-configs.yml. The new last play of a main run, `playbooks/imports/play-ZZZ-passwordless-sudo.yml`, sets it.
- On a developer desktop it is present only when host_vars declares `passwordless_sudo_for_unattended_deploys: true`. There is no default: an undeclared developer desktop fails an assertion, early in play-basic-configs and again in the final play.
- Server and consumer installs still have it removed.
- The kickstart's `/etc/sudoers.d/99-fedora-desktop-setup` lasts for first boot only. Its wrapper removes it after run.bash, and the final play removes it on every host.
- It is set last because run.bash decides once, before a run, whether Ansible needs the sudo password. Revoking the grant mid-run would fail every later become task.

For this plan's decisions D1-D9: the laptop keeps the grant (`true`, the owner's line), so the broker's present path ("NOPASSWD:ALL or RUN_BASH_SUDO_PASSWORD_FILE") is unchanged there. A host that declares false has no blanket rule. Its Go callers that rely on it (agentbus `park-member`, `set-role`) then need a password or a scoped rule.
