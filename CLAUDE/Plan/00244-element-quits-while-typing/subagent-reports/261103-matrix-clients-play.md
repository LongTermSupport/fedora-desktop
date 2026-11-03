# Matrix clients play (Plan 00244 Task 3.1)

- New optional play `playbooks/imports/optional/common/play-matrix-clients.yml`, scope gnome,
  audience developer, independent of the agent bus.
- Installs Nheko from Flathub (`im.nheko.Nheko`). Flathub rather than the
  Fedora RPM: `play-comms.yml` installs Slack the same way, and the Flathub build follows
  upstream releases ahead of the RPM. Plain install, no profiles or launchers.
- `CLAUDE/Plan/meta-deploy.list` gains the play as a `playbooks/` entry below
  `changed-plays` (a new play has not run before, so changed-plays would skip it).
- Task 3.2 in PLAN.md records the TODO: Element's Flatpak install moves into this play, and
  `play-agent-bus-element.yml` keeps only its per-team profile wiring.

## Correction: the Flathub ID (Nheko)

- The play declared an old ID that Flathub redirects. The host run installed
  `im.nheko.Nheko` 0.12.1, so the declared state named an app that was not installed. The
  play now declares `im.nheko.Nheko`.
- Class: a Flatpak install whose ID is not the app's current ID, so the play reports success
  on a different app. Defence: `tasks/flatpak-install.yml` installs, then asserts that
  `flatpak info --system <id>` succeeds (probe-then-fail). All four Flatpak-installing plays
  (comms, agent-bus-element, videography, matrix-clients) import it, so a stale ID stops the
  play and names the fix.
- Not done: a static gate forbidding the raw module outside that task file needs new Go QA
  wiring; left for the owner to decide.
- Not confirmable from the container (no Flathub access): `com.slack.Slack`, `im.riot.Riot`,
  `org.shotcut.Shotcut`. The new check flags any stale one on the next host run.
