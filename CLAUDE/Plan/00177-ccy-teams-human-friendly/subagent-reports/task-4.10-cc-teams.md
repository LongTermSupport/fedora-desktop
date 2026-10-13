# Task 4.10: `cc --teams <team>`, work package C

Branch: `agent-a533c8c64f82804b0-af5be87f`, from F44 at 9cd68fbb.

## What was built

- **cc** (`files/var/local/claude-code/cc`): `--teams <team>` is taken out of claude's
  arguments (one team, no seat; `@` or `,` refused 64). Before the token chooser, cc makes a
  private directory (`$XDG_RUNTIME_DIR/cc-teams.XXXXXX`, removed on exit), then
  `cc_teams_prepare`. The launch becomes
  `env PINGBUS_SEATS=<seat>@<team> <kit>/pingbus seat exec --root <checkout>/.claude/ccy/pingbus/seats --home <tmp>/pingbus-home -- claude --plugin-dir <kit>/plugin/pingbus --settings <tmp>/settings.json --append-system-prompt <one line> <args>`,
  with the kit first on PATH for the hooks. The credential-restore trap and the temp-dir
  cleanup now share one `cc_cleanup` EXIT trap; INT and TERM exit (130/143) through it.
- **cc-teams.bash** (new, sourced by cc): `agent-bus seat choose <team> --type=host` gives
  the DEFAULT seat and its handle; `agent-bus seat take <seat>@<team> --type=host` makes
  it (package A's interface, branch agent-a7b272b78e9b78e24-549b5445, commit 401caea5:
  choose offers only seats of the asked type, and take refuses an existing seat of the
  other type). Then the owner's confirm, then the merged settings.
- **The confirm**: asked once per seat with `gum confirm --default=false` on /dev/tty,
  before claude starts, worded for the human (runs as you, can reach your Element session,
  guidance not a wall). Yes writes `element-accepted` (0600, noclobber, refused through a
  symlink) in the seat directory. No: not started, nothing recorded.
- **cc-teams-settings.json** (new): `permissions.deny` `Read`/`Edit` of
  `~/.var/app/im.riot.Riot/**`, `~/.config/Element/**` and `~/.config/Element-*/**` (the
  last matches pingbus's own `Element-<profile>` rule). cc merges it with the kit's
  `settings.json` (`crossSessionInbound: accept`) using jq into one `--settings` file. The
  Claude Code docs (code.claude.com/docs/en/permissions, /settings) confirm `~/` is
  home-relative, deny arrays merge across sources, and `--settings` sits above project
  settings. Project settings are untouched.
- **The session's text**: only the appended line "You are <handle>, a member of the agent
  team <team>. Post to the team only with pingbus, as yourself; the pingbus skill says
  how." A test asserts it never names Element, sign-ins, owners, humans or anyone else's
  account, and that the plugin's text never mentions the owner's Element session.
- **pingbus** (`helpers/pingbus/config.py`): a `host` member reached through a session
  home's link (a checkout seat) loads beside an Element profile only when the seat holds
  `element-accepted`, a regular file owned by the user. A plain host bundle (a dedicated
  user's) is refused even with the record. A symlink, a directory or a file someone else
  owns does not count.
- **pingbus seat exec** (`helpers/pingbus/cli.py`): `--root DIR` and `--home DIR`
  (absolute, else 64) for a host launch; defaults stay ccy's. Protocol doc §12 and the
  §13 command table updated.
- **The bundle README** (`helpers/agent_bus/admin.py` NEXT_STEPS["host"]): it no longer
  says "never a user that holds a human's Matrix session". A cc seat's README is in the
  checkout the session reads. It now names `cc --teams` for a checkout seat and the
  dedicated-user steps otherwise.
- **Play**: play-claude-yolo.yml installs `cc-teams.bash` and `cc-teams-settings.json`
  to `/var/local/claude-code/` (0644) beside cc.
- **Docs**: docs/agent-bus.md has a new "A cc session" section, the host row of the table
  mentions a cc checkout seat, and "The dedicated agent user" points at it.
- **gh**: `pingbus seat exec` runs `forge_credential_source` as config check does, so the
  owner's logged-in gh satisfies it.
- checkout.py: not touched (A's).

## Tests

- `python3 -m unittest tests.helpers.pingbus.test_config` (95, OK): 5 new cases (accepted
  seat loads; not accepted refused; plain host bundle with the record refused; symlink or
  directory record ignored; foreign-owned record ignored).
- `tests.helpers.pingbus.test_seat`, `test_cli`, `test_cli_offline` (164, OK): 2 new
  cases (`--root`/`--home` used; relative path refused 64). `test_protocol_doc`,
  `test_plugin_contract` OK.
- `tests.helpers.agent_bus.test_admin`, `test_member_docs`, `self_update.test_affected_plays` OK.
- `scripts/test-cc-teams.bash`: 38 passed, 0 failed (it failed 6 before the library and
  play existed). Wired into qa-all.bash as the `cc-teams` gate.
- shellcheck clean on cc, cc-teams.bash, the test and qa-all.bash; ruff clean.
- Not run: qa-all (sub-agent rule), ansible syntax-check (no Ansible here). The YAML parses.

## Host runs needed (for meta-deploy; not edited here)

- `play-agent-bus.yml`: the kit's pingbus zipapp (seat exec `--root/--home`, the Element
  opt-in), the README text, and package A's `agent-bus seat choose/take --type`.
- `play-claude-yolo.yml`: cc, cc-teams.bash, cc-teams-settings.json.

## Open questions

1. The bundle README still ends "full steps in <kit>/README.host". That file, read only if
   the agent follows the pointer, explains the dedicated-user rule in terms of a human's
   Matrix session. Leave it (the dedicated-user guide needs it), or drop the pointer for
   checkout seats?
2. `seat take` still needs the checkout's `.claude/ccy/` (A's find_checkout), so cc in a
   repo never used with ccy is refused with "launch ccy in it once". Should cc create it?
3. The acceptance is per seat. "Once" per user (for example a record under
   `~/.config/pingbus/`) would be a small change if the owner prefers it.
4. Not verified on a real host: gum's `--default=false` on the installed gum, and the
   whole launch end to end. That needs a host run with a real team.
