## 23:29 · action · —   — Task 9.5: the LXC member setup installs gh and names the owner's login command

Task 9.5, owner's "Make it IaC" (2026-10-28): the LXC member setup now installs gh and says the login.

- Where: `helpers/agent_bus_lxc/members.py` present phase, the IaC path `ccy-teams lxc-add`'s apply already runs (`play-agent-bus.yml`). No new play.
- Install: `GH_INSTALL_SH`, as root in the container, once every container has passed the checks that need no change: nothing when gh is on PATH; apt: GitHub's keyring and `cli.github.com/packages` source, then `apt-get install gh` (curl and ca-certificates first if curl is missing); dnf: GitHub's `gh-cli.repo` into `/etc/yum.repos.d`, then `dnf install gh`; apk: `github-cli`; none of the three: refused (exit 5) naming them. Prints the manager used, emitted as a CHANGED line.
- Login: `GH_CHECK_SH` now prints `logged-in` or `logged-out <what gh said>`; the token goes to /dev/null in the container. Every missing login across the run is collected into one MemberError (`gh_login_refusal`): per member `sudo lxc-attach -n <container> -- su - <user> -c 'gh auth login --hostname github.com --web'`, then "ccy-teams lxc-add <team> again, or ccy-teams apply <team>, finishes the join".
- lxc-add (Go): the failed-apply text no longer gives its own `su -` and "then gh auth login" line; it points at the play's command.
- Red: the members suite had 105 failing or erroring tests before the code existed (GH_INSTALL_SH missing), and the Go lxc-add test failed on the old text. Green: members suite 181 OK; `go test ./internal/teams/...` OK; qa-changed passed.
- Not exercised here: the apt and dnf branches write under /etc, so only their selection and the apk, present and none branches run in the tests (real sh, stub tools). The host run is the proof for apt.
- Left: host agent's meta-deploy (`play-claude-yolo.yml`, then `play-agent-bus.yml`); owner's device-code login; then lxc-add again.
