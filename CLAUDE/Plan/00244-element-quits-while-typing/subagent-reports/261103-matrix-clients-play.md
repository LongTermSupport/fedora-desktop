# Matrix clients play (Plan 00244 Task 3.1)

- New optional play `playbooks/imports/optional/common/play-matrix-clients.yml`, scope gnome,
  audience developer, independent of the agent bus.
- Installs Nheko from Flathub (`io.github.NhekoReborn.Nheko`). Flathub rather than the
  Fedora RPM: `play-comms.yml` installs Slack the same way, and the Flathub build follows
  upstream releases ahead of the RPM. Plain install, no profiles or launchers.
- `CLAUDE/Plan/meta-deploy.list` gains the play as a `playbooks/` entry below
  `changed-plays` (a new play has not run before, so changed-plays would skip it).
- Task 3.2 in PLAN.md records the TODO: Element's Flatpak install moves into this play, and
  `play-agent-bus-element.yml` keeps only its per-team profile wiring.
