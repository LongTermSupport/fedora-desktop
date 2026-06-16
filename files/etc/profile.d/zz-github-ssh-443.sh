# shellcheck shell=sh
# ANSIBLE MANAGED: GitHub SSH over 443 (always-on)
#
# Deployed by play-github-cli-multi.yml ONLY when github_ssh_over_443 is true in
# host_vars/localhost.yml. It exports the single runtime signal that ccy reads
# (so every ccy launch routes GitHub SSH over ssh.github.com:443 automatically)
# and that `eval "$(github-ssh-443 env)"` consumers honour.
#
# This is the "always-on" half of the host toggle; the persistent ~/.ssh/config
# + known_hosts blocks are written by the same play. For a TEMPORARY enable
# without re-running Ansible, use `github-ssh-443 on` instead and leave
# github_ssh_over_443 false. To turn always-on off: set github_ssh_over_443:
# false and re-run the play (this file is then removed).
export GITHUB_SSH_443=1
