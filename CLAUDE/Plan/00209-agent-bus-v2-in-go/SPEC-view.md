# SPEC-view: what ccy-teams reads, and how (Plan 00209)

Stage 1 of [DESIGN.md](DESIGN.md) section 10.2 (Task 2.2): `ccy-teams`' reads of the host_vars,
of a checkout's seats and of the homeserver units are made in-process, and one Python call is
kept for the APPLIED verdict. Stage 4 (the reader, Task 5.1) adds its part to this file later.

Each expectation is numbered (V1...) and names the v1 code it comes from. Departures from v1
are numbered B-S1... and say why. v1 file references are relative to the repository root;
`declare.py` is `helpers/agent_bus/declare.py`.

## 1. Where the code goes

| Package                 | Owns                                                                                                                                                                                                                                                                                                           |
| ----------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `internal/bus/pycompat` | the parts of Python 3's semantics the v1 readers depend on and a Go reader must reproduce while v1 lives: `str.isspace`/`strip`/`splitlines`, full lower-casing, `json.loads` (with its error classes) and `json.dumps` (both `ensure_ascii` forms), `str()`/`repr()`, truthiness, `ipaddress` canonical forms |
| `internal/bus/declare`  | the read side of `declare.py`: the line reader of the host_vars subset, every read query below, the grammars it needs (team name, OWNER/REPO, the issue reference, description, seat, host, repo normalisation) and the LXC member rules of `members.parse_members`                                            |
| `internal/bus/seat`     | a checkout's seats: the tree, each `member.json` (its own decoder, marked to move to `bus/state`), and the `flock` probe of each `seat.lock`                                                                                                                                                                   |
| `internal/declare`      | unchanged API; the read methods call `bus/declare`, `Overview`/`Status` keep the one Python call and reuse its answer (V40)                                                                                                                                                                                    |
| `internal/agentbus`     | `SeatList` calls `bus/seat` (no process for a named checkout)                                                                                                                                                                                                                                                  |

`go.yaml.in/yaml/v3` is **not** added to the main module at this stage. v1's reader is not a YAML
parser: it is a line reader of one subset (declare.py:1-10, 230-442) that refuses every other
shape, and the write side, still Python until stage 5, writes only that subset. A YAML library
would accept shapes the write side refuses and type values differently (`yes`, `0x1F`, anchors),
so the board and `ccy-teams create` would disagree about one file. D6's node editor arrives with
the write side at stage 5, when both sides change together.

The `declare.py` read side is a standalone CLI run by `ccy-teams` (`python3 -P -m helpers.agent_bus.declare`, `src-go/internal/declare/declare.go:581`); no play imports it, so
porting it does not touch the rule that Python Ansible loads stays Python. It stays in place for
its write side and for `status`.

## 2. The host_vars line reader (declare.py `parse`, `_items`, `_unique`, `_declared`)

- V1. The file is read as UTF-8 with universal newlines (`pathlib.Path.read_text`): `\r\n` and
  `\r` become `\n`; a byte sequence that is not UTF-8 is an error; a BOM is kept as a character.
  A missing file is refused (78): `<path> does not exist (copy localhost.yml.dist to make it)`
  (declare.py:1437-1441).
- V2. Lines are split as `str.splitlines(keepends=True)` splits them (also at `\v`, `\f`,
  `\x1c`-`\x1e`, `\x85`, U+2028, U+2029); "bare" is a line without its trailing `\r`/`\n`
  characters. Whitespace is Python's (`str.isspace`), in `strip`, in `\s` and in comments.
- V3. A non-comment line containing the list key anywhere must be exactly `<key>\s*:<rest>` at
  column 0, else refused: `line N names <key> in a form ccy-teams does not edit`; the key on two
  lines: `<key> is set more than once (lines A, B)` (declare.py:236-243). Then the bus address is
  read (V5), then the list (V4).
- V4. The list: its key's rest must be empty after the comment rule (V6) and V7's tag rule, else
  `... is written inline ...`; content lines are those after the key that are indented with a
  space or start `- `, blank and comment lines skipped; a tab in the leading whitespace is
  refused; the first other line ends the list (declare.py:248-269). Items are `- key: value`
  lines at the first line's indentation, keys at one indentation for all items, nested lines
  deeper; key names `[a-z_]+`; a key twice in one item, an item without `team`, the same team
  twice (except in `agent_bus_lxc_members`), a team name outside `[a-z][a-z0-9-]{0,23}`, a port
  that is not `str.isdigit`, a state not `present`/`absent` are refused with v1's words
  (declare.py:272-351). Error order is v1's: the structure of every line first, then team
  presence and duplicates item by item, then each item's team, port, state, server_name.
- V5. `agent_bus_address`: every line `agent_bus_address\s*:<rest>` at column 0; two are refused
  (`agent_bus_address is set more than once`); one is the canonical form of the IP address its
  scalar is (Python `ipaddress`, a scope ID kept), or none when the scalar is tagged or not an
  address (declare.py:218-227). IPv4-mapped IPv6 is written dotted (`::ffff:192.0.2.1`), as the
  host's Python (3.13 and later) writes it.
- V6. The comment rule: a value ends before the first `#` that starts the text or follows a
  whitespace character (`re.sub(r"(^|\s)#.*$", "", rest)`).
- V7. A scalar (`_scalar`, declare.py:208-215): comment removed, stripped; one starting `!` is
  refused as tagged (`<where> is tagged (vault-encrypted?); ccy-teams reads only plain values`,
  the VaultedError class); matching outer `"` or `'` quotes are removed with no unescaping.
- V8. A value (`_value`, `_block_list`, `_json_string`, declare.py:369-449) is, in v1's order: a
  block list of scalars or one-line mappings when the line has no value and lines are nested
  under it; a JSON string when the value starts with `"` and decodes as one (its tail empty or a
  comment); JSON (`json.loads`) when it starts `[` or `{`; a single-quoted scalar's text; `true`
  or `false`; an integer for `-?[0-9]+`; else the text. Errors use v1's `where` words. A block
  list's scalar items are judged during the scan and its mapping items after it, as v1 does.
- V9. Anchors, aliases, flow scalars and every other YAML form are text, as v1 reads them; a
  `!vault` value is tagged (V7) wherever a query reads it, and only there.

## 3. The read queries

Each answers exactly what the `declare.py` subcommand prints, as the typed value
`internal/declare` already returns; a refusal is the same exit class (64, 78) with the same
`declare: <message>` text.

- V10. `declared` (overview's first half): per entry of `agent_bus_teams`, in file order, the
  name, the port (none, or `str.isdigit`'s number), present or absent, the server name (its
  scalar, else `<team>.agent-bus.internal`), the URL `http://<host>:<port>` where the host is
  the first element of a flow `listen` list when that is a non-empty string, else the bus
  address, bracketed when it has a `:`, and none without a host or a port; the description
  (V11) (declare.py:1462-1469, 354-364, 1433-1434).
- V11. The description field: none when the entry has no `description`, vaulted when its value
  is tagged, invalid when the value cannot be read or fails `protocol.is_description` (1-120
  characters, stripped, no Cc or Cf character), else the text (declare.py:1444-1459,
  helpers/pingbus/protocol.py:420-424).
- V12. `limited`: per present team whose `path_prefixes` value is truthy, the prefixes, each as
  Python's `str()`, joined `, ` (a string is its characters, as v1 iterates it), and V14's scope
  words; an unreadable value is refused (declare.py:1480-1486).
- V13. `undescribed`: per present team with no `description` key, V14's words, or `a scope ccy-teams cannot read` when they cannot be read (declare.py:1487-1491, 837-841).
- V14. Scope words: `any repository of <str(org)>` when `org` is truthy, then each `repo` of the
  `repos` items that are mappings with a `repo` key; joined `or`; `no repository` when there
  are none (declare.py:829-834).
- V15. `teams-for --repo R`: R must be a lower-case OWNER/REPO the bus can name (64:
  `--repo: 'R' is not a lower-case OWNER/REPO`); the present teams whose standing to R is
  covers (declare.py:898-923).
- V16. Standing (declare.py:907-917): covers when R is among the `repo` values of the `repos`
  items that are mappings; else widen when `org` is not the owner; else excluded when the repo
  name is `in` the `exclude` value (a list's elements, a string's substrings, a mapping's keys:
  Python's `in`), else covers.
- V17. `choices --repo R`: every present team with its standing (unreadable when a value it
  needs is refused) and V11's description field (declare.py:926-939).
- V18. `element-teams`: per `agent_bus_element_teams` entry, the team and `str()` of its
  `base_url` value (`None` when it has none) (declare.py:1476-1479).
- V19. `address`: the bus address (V5), or none (declare.py:1669-1673).
- V20. `settings --team T`: T must be declared present with a port (64:
  `--team: team T is not declared present, with a port, in agent_bus_teams`); per key of
  SETTINGS_KEYS in order, the value as `json.dumps(value, ensure_ascii=False)`, `-` for none,
  vaulted or invalid; `server_name` is `json.dumps` of the server name with `ensure_ascii`
  (declare.py:1005-1035, 813-817).
- V21. `next-steps --team T`: v1's text for a present team with a port (declare.py:844-853).
- V22. `github-remote --url U`: v1's pattern over the stripped URL, lower-cased, kept only when it
  is a lower-case OWNER/REPO (declare.py:1418-1430).
- V23. `lxc-members`: every `agent_bus_lxc_members` entry read with V8 and judged by
  `members.parse_members` (each entry's keys, state, team, container, user, host normalised by
  `registry.resolve_host`, checkout, repo by `registry.normalise_repo`, seat, role with
  `orchestrator` read as coordinator, human_text, service; then the rules between entries), every
  refusal 78 with v1's words; the role, host, checkout and seat of each as LXCMEMBER prints them
  (declare.py:1351-1358, helpers/agent_bus_lxc/members.py:406-514,
  helpers/agent_bus/registry.py:70-95).

The other subcommands stay Python for now: `status` (V40), the edits, and the checks that need
the team file's validator (`check`, `suggest-team`, `check-address`) or derive.py (`lxc-check`,
`lxc-identity`, `lxc-derive`, `lxc-checkouts`, `lxc-script`); they are not on an open's path.

## 4. A checkout's seats (checkout.py `seat_list`, seat.py `seat_lines`)

- V30. A named checkout is read at that folder (its symbolic links resolved, as git reports a
  top level); the working directory (`""`) is resolved to its git top level, as v1 does
  (helpers/agent_bus/checkout.py:572-590). The top and its `.claude/ccy` must each be a real
  directory owned by the user, with v1's refusals (checkout.py:557-569, 585-590).
- V31. The seats are `.claude/ccy/pingbus/seats/<team>/<seat>/`, by team then seat in name
  order; no seats directory is no seats; a symlink or non-directory at any level, a team folder
  that is not a team name or a seat folder that is not a seat name refuses the whole list, with
  v1's words (helpers/pingbus/seat.py:236-256).
- V32. Each seat's `member.json` is decoded as `config.read_member` does: a regular file (it may
  be reached through a symlink), at most 65536 bytes, UTF-8 JSON with no duplicate key, an
  object with only v1's keys, each checked by v1's rules (protocol 1, team equal to the folder,
  DNS server name, `@<handle>:<server_name>`, admin, room ID, `token_file`, canonical
  `plain_http_hosts`, `base_url` by `check_base_url`, `human_text`, `limits` in bounds); its
  handle is the user ID's localpart (helpers/pingbus/config.py:299-302, 452-600,
  helpers/pingbus/limits.py:72-107).
- V33. Held: `seat.lock` is opened read-only without following a symlink and must be a regular
  file; a shared non-blocking `flock` that fails means held; no file means free
  (helpers/pingbus/inbox.py:784-794, 876-891, seat.py:275-277).
- V34. A seat whose bundle or lock is refused is left out and named, `seat <seat>@<team>: <reason>`, printed as `agent-bus: <line>` to stderr as v1's wrapper did, and the list is
  partial (v1's exit 78, `agentbus.Client.SeatList`'s unreadable); a refusal of the whole list
  is the same with no seats (cli.py:228-234, seat.py:295-309).
- V35. Self is never set: v1's `seat list` passes no session home (checkout.py:946-949).

## 5. Units and the APPLIED verdict

- V40. The APPLIED verdict is `declare status` (overview's second half), one Python run. Its
  answer is reused while every input it is computed from is unchanged: the host_vars bytes, the
  installed teams, every applied stamp's name and bytes, the LXC bridge's IPv4 addresses, and the
  path, size, mode and modification time of every file under the checkout's `helpers/pingbus`,
  `helpers/agent_bus` and `helpers/agent_bus_lxc` (which declare.py imports, declare.py:140) and
  of the installer and Tuwunel pin files
  (declare.py:461-470, 500-533, helpers/agent_bus/bridge.py:46-60). Any change runs it again.
- V41. The homeserver units are one `systemctl list-units` per pass, as today: it answers every
  unit at once and asks no property, so H 7.24's rule (ask a unit only its own properties) has
  nothing to apply to. An open's start-up (the first Refresh, the offers' Refresh and the first
  Collect) shares one read of the units, the declaration and the verdict unless something in
  between changed them (an apply, a remove, a create, an edit), when the next pass reads again.
  The shared start-up read is not re-judged against V40's key, so a change made outside ccy-teams
  during the start-up prompts is seen at the first pass after the start-up (the first Collect
  still draws the shared read).
- V42. An open with nothing to offer starts, for team reads: one `systemctl`, one Python
  (`declare status`), no Python seat list, no Python declare read; a redraw with nothing changed
  starts one `systemctl` and no Python. Pinned by a test over the whole open.

## 6. Departures

- B-S1. The verdict is reused while its inputs are unchanged (V40); v1 recomputed it on every
  pass. The inputs are judged by bytes for small files and by `stat` for the source tree, so an
  edit that keeps a file's size and modification time to the nanosecond is not seen until the
  next change.
- B-S2. A named checkout's seats are read at that folder, without asking git for its top level
  (V30): every caller names a checkout top it took from a record or a session.
- B-S3. Where v1 failed with a Python traceback on a value no v1 writer produces (a bare number
  where a list is iterated, a non-string repo name, a malformed bracketed `base_url`), the Go
  reader refuses with a message: the same exit class as a crash (`declare` exit 1; the whole
  seat list unreadable).
- B-S4. A value carrying a tab reaches the flows whole; v1's marker line split it and
  `internal/declare` refused the line.
- B-S5. IPv4-mapped IPv6 addresses are written dotted, as Python 3.13 and later (the host)
  write them; Python 3.11 wrote them as hex groups.
- B-S6. A port beyond the range of an `int`, which v1 printed, is refused as not a number (the
  Go client already refused it on parse).
