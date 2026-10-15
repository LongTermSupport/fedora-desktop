# The owner cannot reach a team room from Element: diagnosis

Symptom: the owner cannot reach an agent team's room from Element on the desktop, while the
agents of that host (ccy containers) use the room normally. Diagnosis from the code only; no
host access. Names are placeholders: team `team-a`, human `<name>`, bus address `192.0.2.1`.

## Verdict

No code defect found that blocks a human's Element. The network path from the desktop to the
homeserver admits the host's own Element (section "Ruled out"). The likeliest cause is that the
owner has **never been given a password for the newest team**. `ccy-teams create` stopped before
it could offer one, because apply failed on the bridge-subnet digest that was fixed today.
Nothing was changed in code.

## Ranked causes

### 1. The newest team: the owner has no password for it (most likely)

- `create/wizard.go`: after declaring the team and its Element entry, create runs
  `ApplyTeams`. Only when that succeeds does `afterCreate` offer "Sign <name> in to Element now?"
  (`login.NewPassword`) and "Open in Element". Today every apply ended in `out of date: config`
  (`ops.ApplyTeams`: play ran, status re-read, team not current), so create returned through
  `c.failed(...)`. No password was shown and Element was not opened.
- The play did run (status is read only after it), so `agent-bus bootstrap` made the human
  account. `admin.create_account` gives it a **random password that is thrown away**. The
  account exists and is invited to the room, but nobody knows its password.
- Each team is its own homeserver (`agent-bus-hs@<team>`, own database). A password the owner
  set for an older team does not work on the new one: Element answers "incorrect username or
  password", which reads as "it's all broken".
- The team's Element profile did not exist until the 10:40 run of the Element play, so
  `ccy-teams` could not have opened it earlier either. `ensureProfile` calls the play, which
  refused before acffaf40.
- Recovery needs no code change. In `ccy-teams`, press Enter on the team and choose **Open in
  Element**. `OpenElement` offers a password to any human whose sessions column reads `never signed in` (0 devices), then launches the profile.
- Confirm (read-only, NOPASSWD by `/etc/sudoers.d/agent-bus-read-only`):
  `sudo /usr/local/bin/agent-bus human devices team-a <name>`. Zero devices supports this cause.
  `sudo /usr/local/bin/agent-bus list team-a` should show `HUMAN <name> invite`: invited but
  never joined.

### 2. Element was started without the team's profile config, or before it existed

- Element reads `~/.var/app/im.riot.Riot/config/Element-<team>/config.json` only at startup.
  Without that file it falls back to the bundled element.io config, which offers matrix.org
  and allows custom servers. Today's run wrote the file for the first time.
- This cause applies if any of these happened:
  - an Element instance for that profile was started before 10:40 and is still running;
  - the owner opened the Flatpak's plain "Element" menu entry (no `--profile`, so the default
    profile);
  - the owner opened another team's launcher. Each profile is bound to its own team's
    homeserver (`disable_custom_urls: true`).
- Confirm: `ps -eo pid,lstart,args --no-headers | grep '[i]m.riot.Riot'`. This lists each
  running Element, its start time and its `--profile`. Look for an instance for `team-a`
  started before the 10:40 write, or one with no `--profile`.

### 3. The owner's sessions on an older team were signed out

- Every password set by `ccy-teams` calls `admin.reset_password(..., logout_devices=True)`
  (`human password`, `human logout-all`). This applies whether it came from create, Sign in,
  or the never-signed-in offer in Open in Element.
- Phase 6 records that the owner set passwords repeatedly. Each time, all of that team's
  Element sessions were signed out. Element then shows a signed-out or "session ended" screen.
- Confirm, per team: `sudo /usr/local/bin/agent-bus human devices <team> <name>`.

### 4. The profile's base_url does not point at the team's port

- The profile's `base_url` comes from the `agent_bus_element_teams` entry. `declare.py base_url` writes `http://<first listen or agent_bus_address>:<team port>`. A hand-written
  entry, or one that predates a change of address or port, would point Element at the wrong
  homeserver or at nothing.
- Confirm (from the checkout):
  `python3 -m helpers.agent_bus.declare element-teams --file environment/localhost/host_vars/localhost.yml`.
  Compare each `ELEMENT <team> <url>` with `ss -Hltn` (ports 6167-6169 on the bus address).

### 5. A recent Element Flatpak update (speculative, low)

- Newer Chromium (Electron) builds gate requests from a page to private-network addresses
  ("Local Network Access"). Element's permission handler could deny that, which would break
  every profile's connection to `http://192.0.2.1:<port>`. Agents use pingbus, not a browser,
  so they would be unaffected.
- This is unproven. It fits only if Element was updated after the owner's last good sign-in
  (Task 5.1).
- Confirm: `flatpak info im.riot.Riot`. Check the Version and Date against the last good
  sign-in. When ccy-teams launched Element, its output is in
  `~/.local/state/ccy-teams/element-team-a.log`.

## Ruled out from the code: the network path and today's bridge commit

- **The homeserver unit admits the host itself.** The unit drop-in (`render.py`) is
  `IPAddressDeny=any` with `IPAddressAllow=` loopback, then each listen address as a /32 or
  /128, then allow_from. A desktop connection to the bus address leaves from that same local
  address, so it is admitted whatever allow_from holds.
- **firewalld does not stand in the way.** Its rich rules (`sync_firewall`) admit allow_from
  CIDRs on the zone of the interface that routes to them. Host-local traffic goes through `lo`
  and never meets them.
- **84bd6798 changed no live state.** `bridge.py` and `declare.py` are only read by
  `play-agent-bus.yml` (when rendering) and by `declare.py status` (when digesting). The live
  homeserver config, drop-in and firewall rules are whatever the last play run rendered.
- **That last run was this morning, not a stale one.** `ccy-teams apply --all` runs the play
  first and checks status afterwards, so the play did run this morning with the old
  facts-path bridge code. It only failed the read-back. The live allow_from is that run's, and
  it is unchanged since.
- Confirm: `curl -sS -m 5 http://192.0.2.1:<port>/_matrix/client/versions`, run as the owner on
  the desktop. It takes the same source address and path as Element.

## Gap noted, not fixed

A create whose apply fails leaves the person without a password. The later
`ccy-teams apply <team>` that its error message recommends never offers the sign-in either.
Only the board's `never signed in` offer under Open in Element recovers it, and nothing points
the owner there.
