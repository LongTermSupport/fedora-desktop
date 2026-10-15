# pingbus: short `--to` refused and REPLY missing after an inbox read

Branch `fix/pingbus-short-name-reply`.

## Root cause (both defects)

The session's container image carries a pingbus zipapp built before M3 (container 2.56). Its
`_user_ids` turns each `--to` name into `@<name>:<server>` with no lookup, so a short
`<repo>.<seat>` is not a member and the validator refuses it as `target` (exit 4). It has no
`REPLY` line type at all, so `recv` prints `PEER` alone on every path, the inbox-read path
included. Running sessions keep the image they started on until relaunched.

## Evidence

Two new tests in `tests/helpers/pingbus/test_cli_reply.py`:

- `test_a_short_name_with_team_named_and_two_teams_active`: `say --team A --to peer.1` on two
  active teams.
- `test_recv_while_a_watcher_holds_the_lock_still_prints_the_reply`: an item in the inbox, a
  `watch` holding the sync lock, `recv` prints the "not syncing" line then `PEER`, `REPLY`.

They pass against `helpers/pingbus`. Run against the modules extracted from the installed
`/usr/local/bin/pingbus`, both fail with the live symptoms: `4 != 0 : pingbus: refused: target`
and `['PEER'] != ['PEER', 'REPLY']`. The current inbox-read path reads the cached record and
prints REPLY; it does not skip it.

## Short-name forms meant to work (S-D2)

A full user ID; an exact handle `<repo>.<seat>+<host>.<type>`; a human localpart; then a
short name `<repo>.<seat>` that matches exactly one agent of the team's verified record. Two
matches are exit 64 naming both; none is exit 64 listing the agents and humans.

## Fix made

A `target` refusal (what the validator refuses after resolution: `admin`, `@room`, a human
for a ping verb humans do not take, too many addressees, no coordinator for
`--to-coordinator`) now says what it got and who can be addressed:
`refused: target: --to admin cannot be addressed by say. Agents: … Humans: …`, the sender
left out, exit 4 unchanged. `protocol.addressable` is the one rule both target checks and
the message use; `Refusal` carries an optional `detail` built only from argv and the record.

CCY 3.98.6, container 2.61; `playbooks/imports/play-claude-yolo.yml` queued in
`meta-deploy.list`. Every ccy session must be relaunched to get the new image.

## Open, not fixed here

In relay mode `_deliver` writes the spool line and no REPLY line, so a relay-delivered PEER
or HUMAN reaches the session without its REPLY command. S-D1 says recv prints one; the fix
needs the plugin side of the spool too.
