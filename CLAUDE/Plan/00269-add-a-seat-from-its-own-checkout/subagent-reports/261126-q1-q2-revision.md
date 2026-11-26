# Q1 and Q2 revision (team coordinator's decisions, 26 Nov 11:07Z)

PLAN.md and RESEARCH.md revised; no code changed.

- **Q1 (D1):** ssh as the owner's login, then `sudo -n` of one fixed command, no password feed.
  Rule for the serving host's own IaC:
  `<owner-login> ALL=(root) NOPASSWD: /usr/local/sbin/teamchat-admin member serve ""`.
  `member serve` is a new no-argument `ClassRoot` verb reading one strict JSON request on stdin
  (`add` with `--seat next --avoid`, `reissue`, `list`; type podman or host; credential password).
  A rule over `member add`/`reissue` directly cannot be narrow: their values are free text and
  sudoers' `*` matches spaces (RESEARCH.md section 6). Path checked: `paths.AdminBinaryPath`,
  `play-teamchat.yml` `tc_admin_link`.
- **Q2 (D2):** `served_by: <ssh-host-alias>` on the laptop's `teamchat_teams` entry; such an entry
  installs nothing and takes no install field; `system.Life` gains a fourth answer; an undeclared
  remote team is refused by `seats add` naming the entry; the alias joins the private-names pin.
- ssh kept over a bus request (Decisions, D1).
- Non-goal reworded: this repository writes no sudoers rule; the serving host's IaC does.
- Tasks renumbered in Phase 2: 2.2 `served_by`, 2.3 `member serve`, 2.4 `SSHIssuer`, 2.5 `Adder.Add`,
  2.6 `Adder.NewSeat`. Status, questions and risk rows updated.
- Follow-up for Task 4.2: `docs/agent-bus-cross-network.md` still says "no NOPASSWD rule"; it
  must be rewritten with the verb.
