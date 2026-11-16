# Which Matrix client a team opens in (Task 5.1, decision D3)

The owner's decision D3: one home-level setting picks the client for every team, and a team may
override it. There is no second team list. This note fixes the shapes that Tasks 5.2 to 5.4 and
Phase 6 build on.

## The setting

- `agent_bus_matrix_client` in the untracked host vars: `element` or `lightning`.
- A team entry in `agent_bus_element_teams` may carry `client: element` or `client: lightning`,
  which wins for that team.
- Any other value fails the play, in the first, read-only task, naming the team and the value.
- **The default stays `element` until Task 7.1.** D3 names Lightning as the client the estate
  moves to. Phase 7 is where the default changes, after the owner's live check (7.2). Until then a
  host that sets nothing gets exactly what it has today. The laptop, or any host trying Lightning
  first, sets `agent_bus_matrix_client: lightning` or a team's `client:`.

The list keeps its name, `agent_bus_element_teams`. It is read and written in several places:

- `helpers/agent_bus/element.py`;
- the Go and Python declare parsers, and their parity fixtures;
- ccy-teams, which writes entries.

A rename is a change of its own, with no behaviour in it, and is left out. Docs describe the list
as "the teams this desktop's human watches", whatever the client.

## One play, the list split by client

`play-agent-bus-element.yml` stays the one play. ccy-teams runs it on its own, and a renamed or
second play would be one more thing for the play ledger and ccy-teams to know. Its first task
gives every entry its resolved `client`. Each client's steps then get the whole list, with that
key on every entry:

- **Element** (`element.py apply`) writes the profile and launcher of each `element` team.
- **Lightning** (the Task 5.2 command) writes the directories, settings file and launcher of each
  `lightning` team.
- **CA tasks:** Element's run for the `element` teams and Lightning's for the `lightning` teams.
  An https team's CA is placed and proved only for the client that opens it.

Each writer sees the full list, so it can tell the two ways a team leaves it:

| The team is                                                    | Its launcher for this client | Its data for this client                                                                      |
| -------------------------------------------------------------- | ---------------------------- | --------------------------------------------------------------------------------------------- |
| declared, for the other client                                 | removed                      | **kept** (the human's signed-in session; switching back finds it)                             |
| no longer declared (Element)                                   | removed                      | removed, as `element.py` prune does today                                                     |
| no longer declared (Lightning)                                 | removed                      | **kept**, and named on a `KEPT<TAB><path>` line; only the owner deletes a signed-in data root |
| moved to Lightning, then no longer declared (its Element side) | already gone                 | **kept**, and named on a `KEPT<TAB><path>` line (issue #299)                                  |

Prune finds a team by its launcher, which the move to Lightning removed, so the last row needs
another sign that a profile is `element.py`'s: each profile it writes holds a marker file,
`agent-bus-team`, naming its team, and a profile written before the marker is adopted when its
`config.json` is exactly the one written for that team. A marked profile of a team no longer
declared is named on a `KEPT` line and shown by the play; the human's own profiles carry no
marker and are never named.

So `element.py` learns the `client` key: `PROFILE_KEYS` gains it, values `element` and
`lightning`, and only `element` entries are written. A `lightning` entry protects its profile
from prune. Its launcher is removed, because one team gets one launcher.

## The Lightning command (Task 5.2)

`src-go/cmd/agent-bus-lightning`, with `check` and `apply`. It takes the same JSON list on stdin,
the shape `element.py` reads plus `client`. It prints `CHANGED<TAB><path>` for each change, as
`element.py` does, and `KEPT<TAB><path>` for each kept data root. It refuses with exit 78, the
helper's code, in these cases:

- it is run as root;
- the user holds pingbus bundles. The rule and its one exception are `check_user` in
  `element.py`; port it, or reuse a Go port if one exists, and give it one test per case.
- an entry is invalid: its keys and values are checked as `element.py` checks them.

For each `lightning` team, under `~/.var/app/org.lightning_matrix.Lightning/`:

- **Directories:** `teams/<team>/{data,config,cache}`, mode 0700.

- **Settings file:** `teams/<team>/config/MatrixClient/matrix-client.conf`, holding
  `[homeserver]` and `url=<base_url>`. It is written only when absent, mode 0600, because
  Lightning rewrites it and owns it afterwards (RESEARCH-integration.md section 2).

- **Launcher:** `~/.local/share/applications/agent-bus-<team>-lightning.desktop`, rewritten when
  it differs. It sets `Name=<team> team (Lightning)` and `Icon=org.lightning_matrix.Lightning`.
  Its `Exec=` is the Phase 3 form, with the Phase 4 variables and every path absolute:

  ```
  flatpak run --command=env org.lightning_matrix.Lightning
    XDG_DATA_HOME=<root>/teams/<team>/data XDG_CONFIG_HOME=<root>/teams/<team>/config
    XDG_CACHE_HOME=<root>/teams/<team>/cache
    SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=<root>/teamchat-ca
    lightning-matrix
  ```

  The two TLS variables are set for every team, http ones included. The runtime's public bundle
  plus a folder of team CAs can only add trust, so there is one launch form, not two.

A stale launcher is recognised as `element.py` recognises its own: by its name, a valid team
name, a regular file, and the `Exec=` line this command writes. A file the human wrote is never
touched.

## Sessions and guards (Task 5.4)

Lightning's per-team roots go in its `native` entry in `src-go/internal/matrixclients/clients.json`,
so every guard from #251 sees a Lightning session under `teams/*/data` as it sees one under the
default root.

## Left to Phase 6

ccy-teams opens a team's launcher for its resolved client. The resolution, the team's `client:`
or else the home-level setting or else `element`, is computed in one place in Go, from the same
host vars. The play's Jinja does the same. Both are tested against the same three cases: no
setting, the home-level setting only, and a team override.
